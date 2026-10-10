package wakatime

// Credential store (reference TokenManager.mjs +
// AccessTokenEncryptorHelper.mjs parity) + the feature gate
// (zotero pattern: env seed + site_settings.global.wakatime.enabled).

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"ollitex/go/libraries/accesstoken"
	"ollitex/go/services/web/core"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

const (
	cipherLabel    = "OL_CEP_wakatime-v3"
	cipherFileMode = 0o600
)

type svc struct {
	a        *core.App
	client   *wakaClient
	mongoTTL time.Duration

	// seams (tests overwrite)
	storeCreds func(ctx context.Context, uid, apiURL, apiKey string) error
	loadCreds  func(ctx context.Context, uid string) (wakaCreds, bool, error)
	delCreds   func(ctx context.Context, uid string) error
	// waka* — thin seams over *wakaClient
	verify      func(ctx context.Context, cr wakaCreds) error
	wakaOne     func(ctx context.Context, cr wakaCreds, hb map[string]any) error
	wakaBulk    func(ctx context.Context, cr wakaCreds, hbs []map[string]any) error
	wakaSummary func(ctx context.Context, cr wakaCreds, project string, days int) (int64, error)
	// wakaUserSummary — all-projects total (the /user-settings view).
	wakaUserSummary func(ctx context.Context, cr wakaCreds, days int) (int64, error)
	// loadProjectName (mongo `projects` → name) for server-side project fill
	loadProjectName func(ctx context.Context, pid string) (string, bool, error)
	// gateOverride — full project-gate replacement (tests); nil = real
	// gateProject (mongo owner/collaborator/admin read parity).
	gateOverride func(cxt *core.Cxt) gateResult

	encryptor *accesstoken.Encryptor
}

func newSvc(a *core.App) *svc {
	c := newWakaClient()
	s := &svc{a: a, client: c, mongoTTL: 5 * time.Second}
	s.storeCreds = s.storeCredsDefault
	s.loadCreds = s.loadCredsDefault
	s.delCreds = s.delCredsDefault
	s.verify = c.verifyCredentials
	s.wakaOne = c.sendHeartbeat
	s.wakaBulk = c.sendHeartbeatsBulk
	s.wakaSummary = c.projectSummary
	s.wakaUserSummary = c.userSummary
	s.loadProjectName = s.loadProjectNameDefault
	s.encryptor = bootEncryptor()
	return s
}

// encryptorSecret — the credential-encryptor password source, exposed for
// stableProvisionPassword (provision.go): same secret the per-user keys
// are sealed with (env or the persisted bootstrap file).
func (s *svc) encryptorSecret() string {
	return os.Getenv("WAKATIME_TOKEN_CIPHER_PASSWORD")
}

// bootEncryptor — cipher password from WAKATIME_TOKEN_CIPHER_PASSWORD (or
// TOKEN_CIPHER_PASSWORD), else an auto-generated file in the persistent
// data dir (reference _getEncryptorData parity; file survives container
// rebuilds).
func bootEncryptor() *accesstoken.Encryptor {
	password := os.Getenv("WAKATIME_TOKEN_CIPHER_PASSWORD")
	if password == "" {
		password = os.Getenv("TOKEN_CIPHER_PASSWORD")
	}
	cipherFile := os.Getenv("WAKATIME_TOKEN_CIPHER_FILE")
	if cipherFile == "" {
		cipherFile = os.Getenv("TOKEN_CIPHER_FILE")
	}
	if cipherFile == "" {
		cipherFile = "/var/lib/overleaf/data/.wakatime-token-cipher.json"
	}
	if password == "" {
		password = bootstrapCipherFile(cipherFile)
	}
	e, err := accesstoken.New(accesstoken.Config{
		CipherLabel:     cipherLabel,
		CipherPasswords: map[string]string{cipherLabel: password},
	})
	if err != nil {
		// last-resort: a fixed per-process secret keeps the feature alive
		// (credentials will not survive a restart; never silent-swallow).
		tmp, _ := accesstoken.New(accesstoken.Config{
			CipherLabel:     cipherLabel,
			CipherPasswords: map[string]string{cipherLabel: "ollitex-wakatime-fallback"},
		})
		if tmp != nil {
			return tmp
		}
		panic("wakatime: cannot initialise the credential encryptor: " + err.Error())
	}
	return e
}

func bootstrapCipherFile(cipherFile string) string {
	if b, err := os.ReadFile(cipherFile); err == nil {
		var m map[string]any
		if json.Unmarshal(b, &m) == nil {
			if pws, ok := m["cipherPasswords"].(map[string]any); ok {
				if pw, ok := pws[cipherLabel].(string); ok && pw != "" {
					return pw
				}
			}
		}
	}
	pwBytes := make([]byte, 32)
	if _, err := rand.Read(pwBytes); err != nil {
		panic("wakatime: cannot generate cipher password")
	}
	pw := base64.StdEncoding.EncodeToString(pwBytes)
	if err := os.MkdirAll(path.Dir(cipherFile), 0o700); err == nil {
		_ = os.WriteFile(cipherFile, []byte(fmt.Sprintf(
			`{"cipherLabel":%q,"cipherPasswords":{%q:%q}}`,
			cipherLabel, cipherLabel, pw)), cipherFileMode)
	}
	return pw
}

// ---- mongo credential store (collection wakaTimeUserCredentials) ---------

// storeCredsDefault — upsert {userId, apiUrl, apiKeyEncrypted}.
func (s *svc) storeCredsDefault(ctx context.Context, uid, apiURL, apiKey string) error {
	if s.a == nil || s.a.Mongo == nil {
		return errors.New("wakatime: app mongo not wired")
	}
	enc, err := s.encryptor.EncryptJson(apiKey)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.mongoTTL)
	defer cancel()
	db, err := s.a.Mongo.DB(ctx)
	if err != nil || db == nil {
		return fmt.Errorf("wakatime: mongo db: %w", err)
	}
	oid, oerr := bson.ObjectIDFromHex(uid)
	if oerr != nil {
		return oerr
	}
	doc := bson.D{
		{Key: "_id", Value: "waka_" + uid},
		{Key: "userId", Value: oid},
		{Key: "apiUrl", Value: apiURL},
		{Key: "apiKeyEncrypted", Value: enc},
	}
	if _, ierr := db.Collection("wakaTimeUserCredentials").InsertOne(ctx, doc); ierr != nil {
		if !isDuplicateKey(ierr) {
			return ierr
		}
		set := bson.D{
			{Key: "$set", Value: bson.D{
				{Key: "userId", Value: oid},
				{Key: "apiUrl", Value: apiURL},
				{Key: "apiKeyEncrypted", Value: enc},
			}},
		}
		_, uerr := db.Collection("wakaTimeUserCredentials").UpdateOne(ctx,
			bson.D{{Key: "_id", Value: "waka_" + uid}}, set)
		return uerr
	}
	return nil
}

func isDuplicateKey(err error) bool {
	return err != nil && strings.Contains(err.Error(), "E11000")
}

// loadCredsDefault — decrypt or (reference parity) treat as not linked.
func (s *svc) loadCredsDefault(ctx context.Context, uid string) (wakaCreds, bool, error) {
	if s.a == nil || s.a.Mongo == nil {
		return wakaCreds{}, false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.mongoTTL)
	defer cancel()
	db, err := s.a.Mongo.DB(ctx)
	if err != nil || db == nil {
		return wakaCreds{}, false, fmt.Errorf("wakatime: mongo db: %w", err)
	}
	var d bson.D
	err = db.Collection("wakaTimeUserCredentials").
		FindOne(ctx, bson.D{{Key: "_id", Value: "waka_" + uid}}).Decode(&d)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return wakaCreds{}, false, nil
		}
		return wakaCreds{}, false, err
	}
	url, _ := dgetS(d, "apiUrl")
	enc, _ := dgetS(d, "apiKeyEncrypted")
	plain, derr := s.encryptor.DecryptToJson(enc)
	if derr != nil {
		// reference: "failed to decrypt … treating as not connected"
		return wakaCreds{}, false, nil
	}
	str, ok := plain.(string)
	if !ok {
		return wakaCreds{}, false, nil
	}
	return wakaCreds{APIURL: url, APIKey: str}, true, nil
}

// delCredsDefault — DELETE /user/wakatime.
func (s *svc) delCredsDefault(ctx context.Context, uid string) error {
	if s.a == nil || s.a.Mongo == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.mongoTTL)
	defer cancel()
	db, err := s.a.Mongo.DB(ctx)
	if err != nil || db == nil {
		return fmt.Errorf("wakatime: mongo db: %w", err)
	}
	_, err = db.Collection("wakaTimeUserCredentials").
		DeleteOne(ctx, bson.D{{Key: "_id", Value: "waka_" + uid}})
	return err
}

// loadProjectNameDefault — project name (server-side fill; the client never
// supplies it — reference parity).
func (s *svc) loadProjectNameDefault(ctx context.Context, pid string) (string, bool, error) {
	if s.a == nil || s.a.Mongo == nil {
		return "", false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.mongoTTL)
	defer cancel()
	db, err := s.a.Mongo.DB(ctx)
	if err != nil || db == nil {
		return "", false, fmt.Errorf("wakatime: mongo db: %w", err)
	}
	oid, oerr := bson.ObjectIDFromHex(pid)
	if oerr != nil {
		return "", false, oerr
	}
	var d bson.D
	ferr := db.Collection("projects").
		FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&d)
	if ferr != nil {
		if errors.Is(ferr, mongo.ErrNoDocuments) {
			return "", false, nil
		}
		return "", false, ferr
	}
	name, _ := dgetS(d, "name")
	return name, true, nil
}

// dgetS — first string value for key (house minimalism, no options pkg).
func dgetS(d bson.D, key string) (string, bool) {
	for _, e := range d {
		if e.Key == key {
			if s, ok := e.Value.(string); ok {
				return s, true
			}
		}
	}
	return "", false
}

// ---- feature gate (zotero pattern: env seed + site_settings merge) -------

// enabled resolves wakatime "enabled" exactly like the zotero gate:
//
//	seed   = WAKATIME_INTEGRATION_ENABLED (bool; "false" → false;
//	         OlliTeX default FALSE — owner directive opt-in/off-by-default,
//	         reference repo default ON)
//	merged = stored site_settings.global.wakatime.enabled (bool) wins when
//	         present (admin UI / config surface, zotero parity).
func (s *svc) enabled(ctx context.Context) bool {
	// 2026-10-07 (owner directive, supersedes the earlier off-by-default
	// pin): WakaTime tracking is ENABLED BY DEFAULT — a logged-in user
	// without linked credentials gets quiet 204 no-ops (no external calls;
	// the relay only ever dials an endpoint the user linked after a
	// successful verify, and the host allowlist still applies). An admin
	// opts out via WAKATIME_INTEGRATION_ENABLED=false or
	// site_settings.global.wakatime{.enabled:false}.
	enabled := true
	if raw := strings.TrimSpace(os.Getenv("WAKATIME_INTEGRATION_ENABLED")); raw != "" {
		if b, err := strconv.ParseBool(raw); err == nil {
			enabled = b
		}
	}
	if s.a == nil || s.a.Mongo == nil {
		return enabled
	}
	ctx, cancel := context.WithTimeout(ctx, s.mongoTTL)
	defer cancel()
	db, err := s.a.Mongo.DB(ctx)
	if err != nil || db == nil {
		return enabled
	}
	var doc struct {
		Wakatime any `bson:"wakatime"`
	}
	if db.Collection("site_settings").FindOne(ctx,
		bson.D{{Key: "_id", Value: "global"}}).Decode(&doc) != nil {
		return enabled
	}
	if doc.Wakatime == nil {
		return enabled
	}
	switch v := doc.Wakatime.(type) {
	case string:
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	case bool:
		return v
	case bson.D:
		for _, e := range v {
			if e.Key == "enabled" {
				if b, ok := e.Value.(bool); ok {
					return b
				}
			}
		}
	}
	return enabled
}

// debugLogging — WAKATIME_DEBUG_LOGGING === "true" (reference parity).
func DebugLogging() bool { return os.Getenv("WAKATIME_DEBUG_LOGGING") == "true" }

// ResolveEnabled — the merged wakatime "enabled" flag (env seed +
// site_settings override), available to other features (the IDE
// ol-ExposedSettings gate). OlliTeX: ON by default (owner directive);
// admins can force it off via env or site settings.
func ResolveEnabled(ctx context.Context, a *core.App) bool {
	return newSvc(a).enabled(ctx)
}
