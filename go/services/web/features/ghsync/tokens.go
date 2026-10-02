package gsync

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"ollitex/go/services/web/core"
	sitesettings "ollitex/go/services/web/features/sitesettings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ---------- config ----------

type gsCfg struct {
	histBase string // Node Settings.apis.project_history.url
}

func gsConfig() *gsCfg {
	c := &gsCfg{
		histBase: strings.TrimRight(os.Getenv("PROJECT_HISTORY_URL"), "/"),
	}
	if c.histBase == "" {
		c.histBase = "http://127.0.0.1:3054"
	}
	return c
}

var (
	gsCfgOnce sync.Once
	gsCfgV    *gsCfg
	gsBridgeV *bridgeClient
	gsHistV   *histClient
)

func gsC() *gsCfg {
	gsCfgOnce.Do(func() {
		c := gsConfig()
		gsCfgV = c
		gsBridgeV = newBridge(c)
		gsHistV = newHist(c)
	})
	return gsCfgV
}

// gsBridge / gsHist — accessor forms used by the controllers.
func gsBridge() *bridgeClient {
	gsC()
	return gsBridgeV
}
func gsHist() *histClient {
	gsC()
	return gsHistV
}

// gsWithUserLock — Node GS-07: serialize per-user credential RMW with a
// per-user mutex (Node: promise-chain userLocks map).
func gsWithUserLock(uid string, fn func() error) error {
	m := gsLock(uid)
	m.Lock()
	defer m.Unlock()
	return fn()
}

// simpler lock: per-key mutex map.
var gsLockTable = map[string]*sync.Mutex{}
var gsLockMapMu sync.Mutex

func gsLock(uid string) *sync.Mutex {
	gsLockMapMu.Lock()
	defer gsLockMapMu.Unlock()
	m, ok := gsLockTable[uid]
	if !ok {
		m = &sync.Mutex{}
		gsLockTable[uid] = m
	}
	return m
}

// ---------- cipher (Node AccessTokenEncryptorHelper parity) ----------
//
//	Node: encryptJson(pat) / decryptToJson — a JSON-string payload. In the
//	Go sitesettings cipher that is the TEXT bridge (EncryptText =
//	JSON-quoted payload; EncryptRaw = bare-JSON-object payload).
//	Key resolution (AccessTokenEncryptorHelper._getEncryptorData):
//	  1. GITHUB_TOKEN_CIPHER_PASSWORD || TOKEN_CIPHER_PASSWORD env (the
//	     base64 string is the password, verbatim)
//	  2. file GITHUB_TOKEN_CIPHER_FILE || TOKEN_CIPHER_FILE ||
//	     /var/lib/overleaf/data/.token-cipher.json — created on first use
//	     (random 32B base64, mode 0600).
//	 label: GITHUB_TOKEN_CIPHER_LABEL || TOKEN_CIPHER_LABEL || OL_CEP-v3.

type gsCipherBridge interface {
	EncryptText(plain string) (string, error)
	DecryptText(tok string) (string, bool)
}

var (
	gsCipherOnce sync.Once
	gsCipherInst gsCipherBridge
)

func gsCipher() gsCipherBridge {
	gsCipherOnce.Do(func() { gsCipherInst = resolveGsCipher() })
	return gsCipherInst
}

func resetGsCipher() { gsCipherOnce = sync.Once{}; gsCipherInst = nil }

const (
	gsDefaultLabel = "OL_CEP-v3"
	gsDefaultKeyFi = "/var/lib/overleaf/data/.token-cipher.json"
)

func resolveGsCipher() gsCipherBridge {
	label := strings.TrimSpace(os.Getenv("GITHUB_TOKEN_CIPHER_LABEL"))
	if label == "" {
		label = strings.TrimSpace(os.Getenv("TOKEN_CIPHER_LABEL"))
	}
	if label == "" {
		label = gsDefaultLabel
	}

	if pw := strings.TrimSpace(firstOf(os.Getenv("GITHUB_TOKEN_CIPHER_PASSWORD"), os.Getenv("TOKEN_CIPHER_PASSWORD"))); pw != "" {
		return sitesettings.OpenProvider(label, []byte(pw))
	}

	file := strings.TrimSpace(firstOf(os.Getenv("GITHUB_TOKEN_CIPHER_FILE"), os.Getenv("TOKEN_CIPHER_FILE")))
	if file == "" {
		file = gsDefaultKeyFi
	}
	if raw, err := os.ReadFile(file); err == nil {
		var doc struct {
			CipherLabel     string            `json:"cipherLabel"`
			CipherPasswords map[string]string `json:"cipherPasswords"`
		}
		if json.Unmarshal(raw, &doc) == nil {
			useL := doc.CipherLabel
			if useL == "" {
				useL = label
			}
			for _, k := range []string{useL, label} {
				if pw, ok := doc.CipherPasswords[k]; ok && pw != "" {
					return sitesettings.OpenProvider(k, []byte(pw))
				}
			}
		}
	}
	// Node: create + persist the key file (random 32B base64, 0600).
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		key = make([]byte, 32)
	}
	enc := base64.StdEncoding.EncodeToString(key)
	d := struct {
		CipherLabel     string            `json:"cipherLabel"`
		CipherPasswords map[string]string `json:"cipherPasswords"`
	}{CipherLabel: label, CipherPasswords: map[string]string{label: enc}}
	b, _ := json.MarshalIndent(d, "", "  ")
	if dir := path.Dir(file); dir != "" {
		_ = os.MkdirAll(dir, 0o700)
	}
	_ = os.WriteFile(file, b, 0o600)
	return sitesettings.OpenProvider(label, []byte(enc))
}

func firstOf(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func gsEncrypt(plain string) (string, error) { return gsCipher().EncryptText(plain) }
func gsDecrypt(tok string) (string, bool)    { return gsCipher().DecryptText(tok) }

// ---------- credential store (githubSyncUserCredentials) ----------
//
//	Node document (githubSyncUserCredentials.mjs + TokenManager.mjs):
//
//	{ userId,
//	  github: "encString" | { token: enc, username, linkedAt },
//	  tokens: { provider: { serverUrl: "enc" | { username: "enc" } } },
//	  servers: { provider: { serverUrl: {username:{createdAt,lastUsedAt}} } },
//	  lastUsedAt, createdAt }
//
//	userId is a HEX STRING (Node stores req.user._id stringified) — Go must
//	store/query strings (same as webdav).

// gsTokAny — a bucket value: either a legacy string or a username->enc map.
type gsCredsDoc struct {
	UserID     string                    `bson:"userId"`
	GitHub     any                       `bson:"github,omitempty"`
	Tokens     map[string]map[string]any `bson:"tokens,omitempty"`
	Servers    map[string]map[string]any `bson:"servers,omitempty"`
	LastUsedAt time.Time                 `bson:"lastUsedAt"`
	CreatedAt  time.Time                 `bson:"createdAt"`
}

// gsServerEntry — servers[p][url][username].
type gsServerEntry struct {
	CreatedAt  time.Time `bson:"createdAt"`
	LastUsedAt time.Time `bson:"lastUsedAt"`
}

var (
	errGsStore   = errors.New("github sync credential store unavailable")
	errGsDecrypt = errors.New("failed to decrypt token")
)

// Node oracle: services/web/app/src/infrastructure/mongodb.mjs —
// `githubSyncUserCredentials: internalDb.collection('githubSyncUserCredentials')`
// (the 's' collection, NOT 'sc(s)redits' — a rename would orphan every
// credential doc linked under the Node era).
const gsCredsColl = "githubSyncUserCredentials"

func gsGetCredsDoc(ctx context.Context, a *core.App, uid string) *gsCredsDoc {
	if a.Mongo == nil || uid == "" {
		return nil
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil
	}
	var d gsCredsDoc
	if err := db.Collection(gsCredsColl).FindOne(ctx, bson.D{{Key: "userId", Value: uid}}).Decode(&d); err != nil {
		return nil
	}
	return &d
}

func gsUpsertCredsDoc(ctx context.Context, a *core.App, d *gsCredsDoc) error {
	if a.Mongo == nil {
		return errGsStore
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return err
	}
	d.LastUsedAt = time.Now().UTC()
	_, err = db.Collection(gsCredsColl).UpdateOne(ctx,
		bson.D{{Key: "userId", Value: d.UserID}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "github", Value: d.GitHub},
			{Key: "tokens", Value: d.Tokens},
			{Key: "servers", Value: d.Servers},
			{Key: "lastUsedAt", Value: d.LastUsedAt},
		}}},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}

func gsRemoveCredsDoc(ctx context.Context, a *core.App, uid string) error {
	if a.Mongo == nil || uid == "" {
		return nil
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return err
	}
	_, err = db.Collection(gsCredsColl).DeleteOne(ctx, bson.D{{Key: "userId", Value: uid}})
	return err
}

// gsServerId — Node serverId(provider, url, username).
func gsServerId(provider, url, username string) string { return provider + ":" + url + ":" + username }

// gsEnumerateBucket — Node enumerateBucket(bucket, legacyUsername).
func gsEnumerateBucket(bucket any, legacyUsername string) []gsPatEntry {
	switch v := bucket.(type) {
	case string:
		return []gsPatEntry{{Username: legacyUsername, Value: v}}
	case map[string]any:
		out := []gsPatEntry{}
		names := make([]string, 0, len(v))
		for u, val := range v {
			if _, ok := val.(string); ok {
				names = append(names, u)
			}
		}
		gsSortStrings(names)
		for _, u := range names {
			out = append(out, gsPatEntry{Username: u, Value: v[u].(string)})
		}
		return out
	case bson.D:
		// mongo-driver v2 decodes embedded documents to bson.D when the
		// target type is `any` — the username→enc map lands here.
		out := []gsPatEntry{}
		for i := range v {
			if s, ok := v[i].Value.(string); ok {
				out = append(out, gsPatEntry{Username: v[i].Key, Value: s})
			}
		}
		return out
	}
	return nil
}

func gsSortStrings(s []string) {
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

type gsPatEntry struct {
	Username string
	Value    string
}
