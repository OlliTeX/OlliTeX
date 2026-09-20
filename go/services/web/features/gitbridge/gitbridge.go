// P6.19 — git-bridge web module (Node services/web/modules/git-bridge).
//
// Node contract (pinned 2026-09-20 from the module sources):
//
// privateApiRouter (NO session; PAT bearer auth; caller = git-bridge svc):
//
//	GET    /api/v0/docs/:project_id                 (read)   getDoc
//	GET    /api/v0/docs/:project_id/saved_vers      (read)   getSavedVers
//	GET    /api/v0/docs/:project_id/snapshots/:ver  (read)   getSnapshot
//	POST   /api/v0/docs/:project_id/snapshots       (write)  postSnapshot
//
// publicApiRouter:
//
//	GET /oauth/token/info   (rate limit 30/60s; validates a PAT)
//
// webRouter (session user):
//
//	GET    /git-bridge/personal-access-tokens
//	POST   /git-bridge/personal-access-tokens   (>=10 -> 403; async security email)
//	DELETE /git-bridge/personal-access-tokens/:token_id
//
// PAT shape (mongo `oauthAccessTokens`):
//
//	{ _id, accessToken: sha256hex(token), accessTokenPartial: first 8,
//	  user_id (ObjectId), type: "personal_access_token", scope: "git_bridge",
//	  createdAt (Date), expiresAt (Date, +1y), lastUsedAt? }
//
// token = "olp_" + 36 alphanumerics. Errors: express sendStatus shapes
// (400/401/403/404/500) + postSnapshot 409 {"code":"outOfDate"}.

package gitbridge

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"ollitex/go/services/web/core"
	"ollitex/go/services/web/features/projectlist"
)

// ---------- routes (Node registration order preserved) --------------------

var (
	docPat      = regexp.MustCompile(`^/api/v0/docs/([^/]+)$`)
	savedPat    = regexp.MustCompile(`^/api/v0/docs/([^/]+)/saved_vers$`)
	snapPat     = regexp.MustCompile(`^/api/v0/docs/([^/]+)/snapshots/([^/]+)$`)
	snapPostPat = regexp.MustCompile(`^/api/v0/docs/([^/]+)/snapshots$`)
	tokenInfoRe = regexp.MustCompile(`^/oauth/token/info$`)
	patListRe   = regexp.MustCompile(`^/git-bridge/personal-access-tokens$`)
	patDelRe    = regexp.MustCompile(`^/git-bridge/personal-access-tokens/([^/]+)$`)
	// Node: `scope: /\bgit_bridge\b/` (regex field match).
	patQuery = primitive.Regex{Pattern: `\bgit_bridge\b`}
)

// Feature registers the git-bridge web routes.
//
// Node gates the whole module on GIT_BRIDGE_ENABLED=true (modules/git-bridge/index.mjs)
// — the Go feature mirrors that (env unset → the feature registers no routes,
// so both stacks 404 identically).
func Feature(a *core.App) core.Feature {
	if os.Getenv("GIT_BRIDGE_ENABLED") != "true" {
		return core.Feature{Name: "gitbridge"}
	}
	limit := core.NewRateLimiter(a.Redis, "oauth-token-info", 30, 60)
	mail := core.NewMail()
	p := &pats{app: a, mail: mail}
	gb := &gb{app: a}
	return core.Feature{
		Name: "gitbridge",
		Routes: []core.Route{
			{Method: "GET", Pattern: docPat, NoLogin: true, NoSession: true,
				Handler: p.auth("read", gb.getDoc)},
			{Method: "GET", Pattern: savedPat, NoLogin: true, NoSession: true,
				Handler: p.auth("read", gb.getSavedVers)},
			{Method: "GET", Pattern: snapPat, NoLogin: true, NoSession: true,
				Handler: p.auth("read", gb.getSnapshot)},
			{Method: "POST", Pattern: snapPostPat, NoLogin: true, NoSession: true,
				Handler: p.auth("write", gb.postSnapshot)},
			{Method: "GET", Pattern: tokenInfoRe, NoLogin: true, NoSession: true,
				Handler: gb.tokenInfo(limit)},
			{Method: "GET", Pattern: patListRe, Handler: p.patList},
			{Method: "POST", Pattern: patListRe, Handler: p.patCreate},
			{Method: "DELETE", Pattern: patDelRe, Handler: p.patDelete},
		},
	}
}

// =================================================================== //
// shared                                                              //
// =================================================================== //

type gb struct {
	app *core.App
}

func (g *gb) ctx() context.Context { return context.Background() }

func (g *gb) db(cxt *core.Cxt) (*mongo.Database, error) {
	return g.app.Mongo.DB(cxt.Req.Context())
}

func jsDate(v any) string {
	if t, ok := v.(time.Time); ok {
		return t.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	return fmt.Sprintf("%v", v)
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

// oj2 — ordered JSON for nested response objects (fixed key list).
func oj2(pairs ...any) []byte { return oj(pairs...) }

// oj — ordered JSON object serializer (Node res.json preserves insertion
// order; Go map marshaling alphabetizes — the pinned byte contract uses
// Node's order). pairs alternate "key", value.
func valJSON(v any) []byte {
	// pre-serialized JSON passes through untouched (mustJSON would base64-encode []byte)
	if bb, ok := v.([]byte); ok {
		t := strings.TrimSpace(string(bb))
		if t != "" && (t[0] == '{' || t[0] == '[' || t[0] == '"' || t[0] == '-' || t[0] >= '0' || t == "true" || t == "false" || t == "null") {
			return bb
		}
	}
	return mustJSON(v)
}

func oj(pairs ...any) []byte {
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(jsonKey(pairs[i].(string)))
		b.WriteByte(':')
		b.Write(valJSON(pairs[i+1]))
	}
	b.WriteByte('}')
	return []byte(b.String())
}

func jsonKey(k string) string {
	if b, err := json.Marshal(k); err == nil {
		return string(b)
	}
	return `"` + k + `"`
}

var gbHTTP = &http.Client{Timeout: 60 * time.Second}

func gbFetchJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := gbHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 32*1024*1024))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gb: http %d", resp.StatusCode)
	}
	return json.Unmarshal(b, out)
}

func gbPostback(ctx context.Context, url string, body map[string]any) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(b)))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
}

func projectHistoryBase() string {
	if v := strings.TrimSpace(os.Getenv("WEB_PROJECT_HISTORY_URL")); v != "" {
		return v
	}
	// Node: settings.apis.project_history.url = http://127.0.0.1:3054 (no /api)
	return "http://127.0.0.1:3054"
}

func v1HistoryBase() string {
	// Node: settings.apis.v1_history.url (already includes the /api prefix).
	if v := strings.TrimSpace(os.Getenv("WEB_V1_HISTORY_URL")); v != "" {
		return v
	}
	return "http://127.0.0.1:3100/api"
}

func projectIDOf(cxt *core.Cxt) string {
	if cxt.Params["1"] != "" {
		return cxt.Params["1"]
	}
	return cxt.Params["2"]
}

func projectExists(g *gb, cxt *core.Cxt, projID string) bool {
	if projID == "" {
		return false
	}
	o, oerr := primitive.ObjectIDFromHex(projID)
	if oerr != nil {
		return false
	}
	db, err := g.db(cxt)
	if err != nil {
		return false
	}
	var d bson.D
	err = db.Collection("projects").FindOne(cxt.Req.Context(), bson.M{"_id": o}).Decode(&d)
	return err == nil
}

// =================================================================== //
// PAT manager (mongo `oauthAccessTokens`)                             //
// =================================================================== //

type pats struct {
	app  *core.App
	mail *core.Mail
}

func (p *pats) tokens(ctx context.Context) (*mongo.Collection, error) {
	db, err := p.app.Mongo.DB(ctx)
	if err != nil {
		return nil, err
	}
	return db.Collection("oauthAccessTokens"), nil
}

// userIDForToken — Node GitBridgePATManager.getUserId.
func (p *pats) userIDForToken(ctx context.Context, token string) (string, error) {
	if !strings.HasPrefix(token, "olp_") {
		return "", nil
	}
	sum := sha256.Sum256([]byte(token))
	coll, err := p.tokens(ctx)
	if err != nil {
		return "", err
	}
	var row struct {
		ID     primitive.ObjectID `bson:"_id"`
		UserID primitive.ObjectID `bson:"user_id"`
	}
	if err := coll.FindOne(ctx, bson.M{
		"accessToken": hex.EncodeToString(sum[:]),
		"type":        "personal_access_token",
		"scope":       patQuery,
		"expiresAt":   bson.M{"$gt": time.Now()},
	}).Decode(&row); err != nil {
		if err == mongo.ErrNoDocuments {
			return "", nil
		}
		return "", err
	}
	db, err := p.app.Mongo.DB(ctx)
	if err != nil {
		return "", err
	}
	var u struct {
		ID primitive.ObjectID `bson:"_id"`
	}
	if err := db.Collection("users").FindOne(ctx, bson.M{"_id": row.UserID}).Decode(&u); err != nil {
		if err == mongo.ErrNoDocuments {
			return "", nil // deleted user -> token invalid
		}
		return "", err
	}
	// non-blocking lastUsedAt (Node fire-and-forget)
	now := time.Now()
	go func() {
		ctx2, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		c, err2 := p.tokens(ctx2)
		if err2 != nil {
			return
		}
		_, _ = c.UpdateOne(ctx2, bson.M{"_id": row.ID},
			bson.M{"$set": bson.M{"lastUsedAt": now}})
	}()
	return row.UserID.Hex(), nil
}

// =================================================================== //
// PAT auth (privateApiRouter) — ensureTokenProjectAccess              //
// =================================================================== //

type gbRoute func(*core.Cxt, *core.Res, string)

// auth — bearer parse -> PAT -> user -> privilege. Node semantics:
// missing/malformed token -> 401; unknown PAT -> 401; no privilege -> 403;
// ANY thrown error (incl. invalid ObjectId / mongo failure inside the
// privilege check) -> 500 (middleware catch-all).
func (p *pats) auth(perm string, h gbRoute) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		projID := projectIDOf(cxt)
		if projID == "" {
			res.SendStatus(400)
			return
		}
		raw := cxt.Req.Header.Get("Authorization")
		parts := strings.Fields(strings.TrimSpace(raw))
		if len(parts) < 2 || !strings.EqualFold(parts[0], "bearer") {
			res.SendStatus(401)
			return
		}
		uid, err := p.userIDForToken(cxt.Req.Context(), parts[1])
		if err != nil {
			res.SendStatus(500)
			return
		}
		if uid == "" {
			res.SendStatus(401)
			return
		}
		ok, checkErr := gbCanAccess(cxt, p, uid, projID, perm)
		if checkErr != nil {
			res.SendStatus(500)
			return
		}
		if !ok {
			res.SendStatus(403)
			return
		}
		h(cxt, res, uid)
	}
}

// gbCanAccess — canUserReadProject/canUserWriteProjectContent
// (ignoreSiteAdmin:true, token=null — token refs never apply).
func gbCanAccess(cxt *core.Cxt, p *pats, uid, projID string, perm string) (bool, error) {
	o, err := primitive.ObjectIDFromHex(projID)
	if err != nil {
		return false, nil // Node parse throw -> 500 (mapped by the caller)
	}
	uoid, uerr := primitive.ObjectIDFromHex(uid)
	if uerr != nil {
		return false, uerr
	}
	db, err := p.app.Mongo.DB(cxt.Req.Context())
	if err != nil {
		return false, err
	}
	var proj bson.D
	if err := db.Collection("projects").FindOne(cxt.Req.Context(), bson.M{"_id": o}).Decode(&proj); err != nil {
		if err == mongo.ErrNoDocuments {
			// Node: getProject throws NotFound inside the check -> 500 here
			return false, fmt.Errorf("gitbridge: project lookup failed")
		}
		return false, err
	}
	m := dval(proj)
	member := func(v any) bool {
		switch x := v.(type) {
		case primitive.ObjectID:
			return x == uoid
		case string:
			if x == "" {
				return false
			}
			xo, e := primitive.ObjectIDFromHex(x)
			return e == nil && xo == uoid
		}
		return false
	}
	inArr := func(arr any) bool {
		a, ok := arr.(primitive.A)
		if !ok {
			return false
		}
		for _, v := range a {
			if member(v) {
				return true
			}
		}
		return false
	}
	level := "none"
	switch {
	case member(m["owner_ref"]):
		level = "owner"
	case inArr(m["collaberator_refs"]):
		level = "readAndWrite"
	case inArr(m["reviewer_refs"]):
		level = "review"
	case inArr(m["readOnly_refs"]):
		level = "readOnly"
	}
	fmt.Fprintf(os.Stderr, "GBDBG level=%s perm=%s\n", level, perm)
	switch perm {
	case "read":
		if level == "owner" || level == "readAndWrite" || level == "readOnly" || level == "review" {
			return true, nil
		}
		return gbAdminCap(cxt, p, uid, "view-project-content")
	case "write":
		if level == "owner" || level == "readAndWrite" {
			return true, nil
		}
		return gbAdminCap(cxt, p, uid, "modify-project-content")
	}
	return false, nil
}

// gbAdminCap — hasAdminProjectCapability (adminPrivilegeAvailable gate +
// user.admin + adminRoles check).
func gbAdminCap(cxt *core.Cxt, p *pats, uid, capName string) (bool, error) {
	if !core.AdminPrivilegeAvailable() {
		return false, nil
	}
	o, err := primitive.ObjectIDFromHex(uid)
	if err != nil {
		return false, err
	}
	db, err := p.app.Mongo.DB(cxt.Req.Context())
	if err != nil {
		return false, err
	}
	var u struct {
		Admin     bool     `bson:"admin"`
		AdminCaps []string `bson:"adminCapabilities"`
	}
	if err := db.Collection("users").FindOne(cxt.Req.Context(), bson.M{"_id": o}).Decode(&u); err != nil {
		if err == mongo.ErrNoDocuments {
			return false, nil
		}
		return false, err
	}
	if !u.Admin {
		return false, nil
	}
	if os.Getenv("ADMIN_ROLES_ENABLED") == "true" {
		for _, c := range u.AdminCaps {
			if c == capName {
				return true, nil
			}
		}
		return false, nil
	}
	return true, nil
}

func dval(d bson.D) map[string]any {
	m := map[string]any{}
	for _, e := range d {
		m[e.Key] = e.Value
	}
	return m
}

// =================================================================== //
// publicApiRouter: GET /oauth/token/info                              //
// =================================================================== //

func (g *gb) tokenInfo(limit *core.RateLimiter) func(*core.Cxt, *core.Res) {
	p := &pats{app: g.app}
	return func(cxt *core.Cxt, res *core.Res) {
		if limit != nil && !limit.Consume(core.ClientIP(cxt.Req)) {
			core.Send429(res, "Rate limit reached, please try again later")
			return
		}
		raw := cxt.Req.Header.Get("Authorization")
		parts := strings.Fields(strings.TrimSpace(raw))
		if len(parts) < 2 || !strings.EqualFold(parts[0], "bearer") {
			res.SendStatus(401)
			return
		}
		uid, err := p.userIDForToken(cxt.Req.Context(), parts[1])
		if err != nil {
			res.SendStatus(500)
			return
		}
		if uid == "" {
			res.SendStatus(401)
			return
		}
		res.SendStatus(200)
	}
}

// =================================================================== //
// webRouter: PAT CRUD                                                 //
// =================================================================== //

var patAlnum = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

func genToken() (string, error) {
	nb := make([]byte, 36)
	if _, err := rand.Read(nb); err != nil {
		return "", err
	}
	out := make([]rune, 36)
	for i := range nb {
		out[i] = patAlnum[int(nb[i])%len(patAlnum)]
	}
	return "olp_" + string(out), nil
}

func (p *pats) sessUser(cxt *core.Cxt) (string, string, bool) {
	if cxt.Sess == nil || !cxt.Sess.IsLoggedIn() {
		return "", "", false
	}
	uid := cxt.Sess.UserIDHex()
	email := ""
	if raw, ok := cxt.Sess.GetRaw("user"); ok {
		var u struct {
			ID    string `json:"_id"`
			Email string `json:"email"`
		}
		if json.Unmarshal(raw, &u) == nil {
			if u.ID == "" {
				u.ID = uid
			}
			uid = u.ID
			email = u.Email
		}
	}
	return uid, email, true
}

func (p *pats) patList(cxt *core.Cxt, res *core.Res) {
	uid, _, ok := p.sessUser(cxt)
	if !ok {
		res.SendStatus(401)
		return
	}
	coll, cerr := p.tokens(cxt.Req.Context())
	if cerr != nil {
		res.SendStatus(500)
		return
	}
	cur, err := coll.Find(cxt.Req.Context(), bson.M{
		"user_id": uid,
		"scope":   patQuery,
		"type":    "personal_access_token",
	}, options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: 1}}).
		SetProjection(bson.M{"accessTokenPartial": 1, "createdAt": 1, "expiresAt": 1, "lastUsedAt": 1}))
	if err != nil {
		res.SendStatus(500)
		return
	}
	var rows []bson.M
	if err := cur.All(cxt.Req.Context(), &rows); err != nil {
		res.SendStatus(500)
		return
	}
	// Node/mongoose row order: _id first, then the projection fields
	// (lastUsedAt omitted when absent).
	var lb strings.Builder
	lb.WriteByte('[')
	for i := 0; i < len(rows); i++ {
		r := rows[i]
		if i > 0 {
			lb.WriteByte(',')
		}
		if _, has := r["_id"]; !has {
			lb.WriteString(`{}`)
			continue
		}
		lb.WriteByte('{')
		lb.WriteString(`"_id":`)
		lb.Write(valJSON(r["_id"]))
		for _, k := range []string{"accessTokenPartial", "createdAt", "expiresAt", "lastUsedAt"} {
			if v, has := r[k]; has {
				lb.WriteByte(',')
				lb.WriteString(jsonKey(k))
				lb.WriteByte(':')
				if t, ok := v.(time.Time); ok {
					lb.Write(mustJSON(jsDate(t)))
				} else {
					lb.Write(mustJSON(v))
				}
			}
		}
		lb.WriteByte('}')
	}
	lb.WriteByte(']')
	res.JSON(200, []byte(lb.String()))
}

func (p *pats) patCreate(cxt *core.Cxt, res *core.Res) {
	uid, email, ok := p.sessUser(cxt)
	if !ok {
		res.SendStatus(401)
		return
	}
	ctx := cxt.Req.Context()
	coll, cerr := p.tokens(ctx)
	if cerr != nil {
		res.SendStatus(500)
		return
	}
	count, err := coll.CountDocuments(ctx, bson.M{
		"user_id": uid,
		"scope":   patQuery,
		"type":    "personal_access_token",
	})
	if err != nil {
		res.SendStatus(500)
		return
	}
	if count >= 10 {
		res.SendStatus(403)
		return
	}
	token, err := genToken()
	if err != nil {
		res.SendStatus(500)
		return
	}
	now := time.Now().UTC()
	exp := now.AddDate(1, 0, 0) // Node: createdAt.setFullYear(+1)
	sum := sha256.Sum256([]byte(token))
	ins, ierr := coll.InsertOne(ctx, bson.M{
		"accessToken":        hex.EncodeToString(sum[:]),
		"accessTokenPartial": token[:8],
		"user_id":            uid,
		"type":               "personal_access_token",
		"scope":              "git_bridge",
		"createdAt":          now,
		"expiresAt":          exp,
	})
	if ierr != nil {
		res.SendStatus(500)
		return
	}
	// Node returns result.insertedId — the REAL row _id; responding with a
	// separately generated ID would break delete-by-id / list _id parity.
	id := ins.InsertedID.(primitive.ObjectID)
	if email != "" && p.mail != nil {
		go func() {
			_ = p.mail.SendExact(email, "",
				"Overleaf security note: new Git authentication token generated\n\n"+
					"A new Git authentication token has been generated for your account "+email+". "+
					"If you did not do this, disable the token in your account settings and "+
					"change your password as soon as possible.")
		}()
	}
	// Node createToken return order: _id, accessToken, accessTokenPartial, createdAt, expiresAt.
	res.JSON(200, oj(
		"_id", id.Hex(),
		"accessToken", token,
		"accessTokenPartial", token[:8],
		"createdAt", jsDate(now),
		"expiresAt", jsDate(exp),
	))
}

func (p *pats) patDelete(cxt *core.Cxt, res *core.Res) {
	uid, _, ok := p.sessUser(cxt)
	if !ok {
		res.SendStatus(401)
		return
	}
	tokenID := cxt.Params["1"]
	if tokenID == "" {
		res.SendStatus(400)
		return
	}
	toid, terr := primitive.ObjectIDFromHex(tokenID)
	if terr != nil {
		res.SendStatus(500) // Node: new ObjectId(bad) throws -> 500
		return
	}
	coll, cerr := p.tokens(cxt.Req.Context())
	if cerr != nil {
		res.SendStatus(500)
		return
	}
	rr, err := coll.DeleteOne(cxt.Req.Context(), bson.M{
		"_id":     toid,
		"user_id": uid,
	})
	if err != nil {
		res.SendStatus(500)
		return
	}
	if rr.DeletedCount == 0 {
		res.SendStatus(404)
		return
	}
	res.SendStatus(200)
}

// =================================================================== //
// privateApiRouter handlers (GitBridgeController)                    //
// =================================================================== //

// getDoc — GET /api/v0/docs/:project_id
func (g *gb) getDoc(cxt *core.Cxt, res *core.Res, uid string) {
	projID := projectIDOf(cxt)
	if !projectExists(g, cxt, projID) {
		res.SendStatus(400)
		return
	}
	var ver struct {
		Version   int64    `json:"version"`
		Timestamp string   `json:"timestamp"`
		V2Authors []any    `json:"v2Authors"`
	}
	if err := gbFetchJSON(cxt.Req.Context(), projectHistoryBase()+"/project/"+projID+"/version", &ver); err != nil {
		res.SendStatus(400)
		return
	}
	name, email := "Anonymous", "anonymous@nowhere"
	if len(ver.V2Authors) > 0 {
		if s, ok := ver.V2Authors[0].(string); ok && s != "" {
			if u, ok := loadGBUser(g, cxt, s); ok {
				email = u.Email
				name = gbDisplayName(u)
				if email == "" {
					email = "anonymous@nowhere"
				}
			}
		}
	}
	timestamp := ver.Timestamp
	if timestamp == "" {
		timestamp = jsDate(time.Now().UTC())
	}
	res.JSON(200, oj(
		"latestVerId", ver.Version,
		"latestVerAt", timestamp,
		"latestVerBy", oj2("email", email, "name", name),
	))
}

// getSavedVers — GET .../saved_vers
func (g *gb) getSavedVers(cxt *core.Cxt, res *core.Res, uid string) {
	projID := projectIDOf(cxt)
	if !projectExists(g, cxt, projID) {
		res.SendStatus(400)
		return
	}
	type label struct {
		Version   int64  `json:"version"`
		Comment   string `json:"comment"`
		CreatedAt any    `json:"created_at"`
		UserID    string `json:"user_id"`
	}
	var labels []label
	if err := gbFetchJSON(cxt.Req.Context(), projectHistoryBase()+"/project/"+projID+"/labels", &labels); err != nil {
		res.SendStatus(400)
		return
	}
	seen := map[string]string{}
	for _, l := range labels {
		if l.UserID != "" {
			seen[l.UserID] = l.UserID
		}
	}
	users := map[string]gbUser{}
	for u := range seen {
		if du, ok := loadGBUser(g, cxt, u); ok {
			users[u] = du
		}
	}
	var out strings.Builder
	out.WriteByte('[')
	for i, l := range labels {
		if i > 0 {
			out.WriteByte(',')
		}
		u, ok := users[l.UserID]
		name, email := gbDisplayName(u), u.Email
		if !ok || email == "" {
			if email == "" {
				email = "anonymous@nowhere"
			}
		}
		if !ok {
			name = "Anonymous"
		}
		var row strings.Builder
		row.WriteString(`{"versionId":`)
		row.Write(mustJSON(l.Version))
		row.WriteString(`,"comment":`)
		row.Write(mustJSON(l.Comment))
		row.WriteString(`,"createdAt":`)
		row.Write(valJSON(l.CreatedAt))
		row.WriteString(`,"user":`)
		row.Write(oj2("name", name, "email", email))
		row.WriteByte('}')
		out.WriteString(row.String())
	}
	out.WriteByte(']')
	res.JSON(200, []byte(out.String()))
}

// getSnapshot — GET .../snapshots/:version
func (g *gb) getSnapshot(cxt *core.Cxt, res *core.Res, uid string) {
	projID := projectIDOf(cxt)
	version := cxt.Params["2"]
	if version == "" {
		res.SendStatus(400)
		return
	}
	if !projectExists(g, cxt, projID) {
		res.SendStatus(400)
		return
	}
	type fileData struct {
		Content *string `json:"content"`
		Hash    string  `json:"hash"`
	}
	var snap struct {
		Files map[string]struct {
			Data fileData `json:"data"`
		} `json:"files"`
	}
	if err := gbFetchJSON(cxt.Req.Context(),
		projectHistoryBase()+"/project/"+projID+"/version/"+version, &snap); err != nil {
		res.SendStatus(400)
		return
	}
	srcs, atts := []any{}, []any{}
	for pathname, file := range snap.Files {
		if file.Data.Content == nil {
			if file.Data.Hash == "" {
				res.SendStatus(400) // both content and hash missing
				return
			}
			tok, err := signProjectJWT(projID)
			if err != nil {
				res.SendStatus(400)
				return
			}
			atts = append(atts, []any{
				v1HistoryBase() + "/projects/" + projID + "/blobs/" + file.Data.Hash + "?token=" + tok,
				pathname,
			})
			continue
		}
		if !gbValidatePath(pathname) {
			res.SendStatus(400)
			return
		}
		srcs = append(srcs, []any{*file.Data.Content, pathname})
	}
	// Node: res.json({ srcs, atts }) — insertion order.
	var sb strings.Builder
	sb.WriteString(`{"srcs":`)
	sb.Write(mustJSON(srcs))
	sb.WriteString(`,"atts":`)
	sb.Write(mustJSON(atts))
	sb.WriteByte('}')
	res.JSON(200, []byte(sb.String()))
}

// postSnapshot — POST .../snapshots
func (g *gb) postSnapshot(cxt *core.Cxt, res *core.Res, uid string) {
	projID := projectIDOf(cxt)
	body, err := io.ReadAll(io.LimitReader(cxt.Req.Body, 12*1024*1024))
	if err != nil {
		res.SendStatus(400)
		return
	}
	var req struct {
		LatestVerId int64 `json:"latestVerId"`
		Files       []struct {
			Name string  `json:"name"`
			URL  *string `json:"url"`
		} `json:"files"`
		PostbackURL string `json:"postbackUrl"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.PostbackURL == "" {
		res.SendStatus(400)
		return
	}
	if !projectExists(g, cxt, projID) {
		res.SendStatus(404)
		return
	}
	var ver struct {
		Version int64 `json:"version"`
	}
	if err := gbFetchJSON(cxt.Req.Context(), projectHistoryBase()+"/project/"+projID+"/version", &ver); err != nil {
		res.SendStatus(500)
		return
	}
	if req.LatestVerId != ver.Version {
		res.JSON(409, mustJSON(map[string]string{"code": "outOfDate"}))
		return
	}
	// Node: respond FIRST, then pushUpdate runs async (un-awaited).
	res.JSON(200, mustJSON(map[string]string{"code": "accepted"}))
	files := req.Files
	go gbPushUpdate(g.app, projID, uid, files, req.PostbackURL)
}

// =================================================================== //
// helpers                                                             //
// =================================================================== //

type gbUser struct {
	FirstName string `bson:"first_name"`
	LastName  string `bson:"last_name"`
	Email     string `bson:"email"`
}

func loadGBUser(g *gb, cxt *core.Cxt, uid string) (gbUser, bool) {
	var u gbUser
	o, oerr := primitive.ObjectIDFromHex(uid)
	if oerr != nil {
		return u, false
	}
	db, err := g.db(cxt)
	if err != nil {
		return u, false
	}
	if err := db.Collection("users").FindOne(cxt.Req.Context(), bson.M{"_id": o}).Decode(&u); err != nil {
		return u, false
	}
	return u, true
}

func gbDisplayName(u gbUser) string {
	full := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if full != "" {
		return full
	}
	if u.Email != "" {
		return u.Email
	}
	return "Anonymous"
}

// gbValidatePath — Node normalizeAndValidateFilePath (posix + .git).
func gbValidatePath(fileName string) bool {
	if fileName == "" || strings.Contains(fileName, "\x00") {
		return false
	}
	norm := cleanPosix(fileName)
	if norm == "" || strings.HasPrefix(norm, "/") || strings.HasPrefix(norm, "..") || norm == "." {
		return false
	}
	if norm == ".git" || strings.HasPrefix(norm, ".git/") {
		return false
	}
	return true
}

// cleanPosix — path.posix.normalize (Go path.Clean with a leading-slash
// context to mirror the ".." dropping rules).
func cleanPosix(s string) string {
	seg := []string{}
	for _, p := range strings.Split(s, "/") {
		switch p {
		case "", ".":
		case "..":
			if len(seg) > 0 && seg[len(seg)-1] != ".." {
				seg = seg[:len(seg)-1]
			} else {
				seg = append(seg, "..")
			}
		default:
			seg = append(seg, p)
		}
	}
	return strings.Join(seg, "/")
}

// signProjectJWT — JsonWebToken.sign({project_id}, {expiresIn:'10m'})
// (HS256 from OT_JWT_AUTH_KEY/ALG; jsonwebtoken string-key semantics).
func signProjectJWT(projID string) (string, error) {
	key := os.Getenv("OT_JWT_AUTH_KEY")
	alg := os.Getenv("OT_JWT_AUTH_ALG")
	if alg == "" {
		alg = "HS256"
	}
	if key == "" || alg != "HS256" {
		return "", fmt.Errorf("gitbridge: missing JWT configuration")
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	// Node jsonwebtoken serializes in insertion order: project_id, then exp
	// (json.Marshal(map) would alphabetize — the raw token bytes must match).
	payload := base64.RawURLEncoding.EncodeToString([]byte(
		`{"project_id":"` + projID + `","exp":` + fmt.Sprintf("%d", time.Now().Add(10*time.Minute).Unix()) + `}`))
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(header + "." + payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return header + "." + payload + "." + sig, nil
}

// =================================================================== //
// pushUpdate (Node GitBridgeHandler.pushUpdate, async)               //
// =================================================================== //

func gbPushUpdate(app *core.App, projID, uid string, files []struct {
	Name string  `json:"name"`
	URL  *string `json:"url"`
}, postbackURL string) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	pj, perr := primitive.ObjectIDFromHex(projID)
	if perr != nil {
		gbPostback(ctx, postbackURL, map[string]any{"code": "error"})
		return
	}

	// 1. validate names (Node validateFileName)
	invalid := []map[string]any{}
	for _, f := range files {
		if v := gbValidateFileName(f.Name); v != nil {
			invalid = append(invalid, v)
		}
	}
	if len(invalid) > 0 {
		gbPostback(ctx, postbackURL, map[string]any{"code": "invalidFiles", "errors": invalid})
		return
	}

	// 2. delete entities present in the project but missing from the update
	//    (Node: entityPaths = docs + files; folders are NOT deleted)
	paths := projectlist.GBCollectEntityPaths(ctx, app, pj)
	pushSet := map[string]any{}
	for _, f := range files {
		pushSet["/"+f.Name] = true
	}
	missing := 0
	for _, pth := range paths {
		if pushSet[pth] != nil {
			continue
		}
		if !projectlist.GBDeleteEntityAtPath(ctx, app, pj, uid, pth) {
			gbPostback(ctx, postbackURL, map[string]any{"code": "error"})
			return
		}
		missing++
	}
	_ = missing

	// 3. apply file updates (Node mergeUpdate per file with a url)
	for _, f := range files {
		if f.URL == nil || *f.URL == "" {
			continue
		}
		resp, err := gbHTTP.Get(*f.URL)
		if err != nil {
			gbPostback(ctx, postbackURL, map[string]any{"code": "error"})
			return
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 128*1024*1024))
		resp.Body.Close()
		if err != nil {
			gbPostback(ctx, postbackURL, map[string]any{"code": "error"})
			return
		}
		name := strings.TrimPrefix(f.Name, "/")
		relDir, leaf := splitLeaf(name)
		if !projectlist.GBWriteBytes(ctx, app, pj, uid, relDir, leaf, data, "git-bridge") {
			gbPostback(ctx, postbackURL, map[string]any{"code": "error"})
			return
		}
	}

	// 4. version + postback
	var ver struct {
		Version int64 `json:"version"`
	}
	if err := gbFetchJSON(ctx, projectHistoryBase()+"/project/"+projID+"/version", &ver); err != nil {
		gbPostback(ctx, postbackURL, map[string]any{"code": "error"})
		return
	}
	gbPostback(ctx, postbackURL, map[string]any{"code": "upToDate", "latestVerId": ver.Version})
}

func splitLeaf(p string) (relDir, leaf string) {
	seg := []string{}
	cur := ""
	for _, c := range p {
		if c == '/' {
			if cur != "" {
				seg = append(seg, cur)
			}
			cur = ""
			continue
		}
		cur += string(c)
	}
	if cur != "" {
		seg = append(seg, cur)
	}
	if len(seg) == 0 {
		return "", ""
	}
	if len(seg) == 1 {
		return "", seg[0]
	}
	return strings.Join(seg[:len(seg)-1], "/"), seg[len(seg)-1]
}

// gbValidateFileName — Node GitBridgeHandler.validateFileName.
func gbValidateFileName(name string) map[string]any {
	norm := cleanPosix(name)
	if norm == "" || norm == "." || strings.HasPrefix(norm, "..") || norm == ".git" || strings.HasPrefix(norm, ".git/") {
		return map[string]any{"file": name, "state": "error"}
	}
	if norm != name {
		return map[string]any{"file": name, "cleanFile": norm}
	}
	return nil
}
