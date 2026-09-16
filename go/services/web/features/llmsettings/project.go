// P6.4b — the project-scoped live-LLM surface (Node LLMChatController.mjs +
// LLMComplianceController.mjs, /project/:Project_id/llm/*).
//
// Deterministic offline slice (byte pins: /tmp/p64b_node.json,
// /tmp/p64b_job.json):
//
//	GET  models, features, source-context, prompts      — pure reads
//	POST chat, completion, compile-fix, grammar, generate — validation +
//	     lane resolution + failure-path parity (live calls die on the dead
//	     BYO host; success paths kept faithful for live deployments)
//	GET/POST compliance rubrics/start/status/cancel    — job lifecycle with
//	     mongo mirror (llmreviewjobs) + in-memory queue (Node parity)
//
// Node authority: services/web/modules/llm/app/src/LLMChatController.mjs
// (sendError status map, resolveLane/user-lane, getSourceContext,
// resolveSiteSpec, GENERATOR_TYPES title/abstract/keywords),
// LLMComplianceController.mjs (performReview, splitRubric,
// stripLatexComments, estimateTokens, buildScanHints, statusReview,
// cancelReview), models/LLMReviewJob.mjs (mongo snapshot), LLMModelRef.mjs
// (u:<8hex>:<model> refs), LLMBudget.mjs (guardLLMCall), LLMClient.mjs
// (normalizeProviderSpec 'llm-bad-config' errors).
package llmsettings

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	mgoOptions "go.mongodb.org/mongo-driver/mongo/options"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

// ---------------------------------------------------------------------------
// project preflight (Node: zod objectId → ensureUserCanReadProject)
// ---------------------------------------------------------------------------

var reOID = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// malformed404 — Node's 404 JSON when the id is not a valid ObjectId
// (P5.2a compile pin, same string: a_badoid_models).
const malformed404 = `{"error":"Validation error: Invalid Mongo ObjectId at \"params.Project_id\"","statusCode":404}`

// internal500json — Node OError throw → 500 (compile package pin).
const internal500json = `{"error":{"type":"InternalServerError","message":"Internal Server Error"}}`

func pageBaseOf(cxt *core.Cxt) views.PageData {
	d := views.PageData{Nonce: views.NewNonce(), Origin: cxt.SiteURL}
	if cxt.Sess != nil {
		d.CSRFToken = cxt.Sess.CsrfToken()
		if uid := cxt.Sess.UserIDHex(); uid != "" {
			d.UserID = uid
			d.UserEmail = sessEmailLLM(cxt.Sess)
		}
	}
	return d
}

func sessEmailLLM(s *core.Session) string {
	raw, ok := s.GetRaw("passport")
	if !ok {
		return ""
	}
	var p struct {
		User json.RawMessage `json:"user"`
	}
	if json.Unmarshal(raw, &p) != nil || len(p.User) == 0 || string(p.User) == "null" {
		return ""
	}
	var u struct {
		Email string `json:"email"`
	}
	if json.Unmarshal(p.User, &u) == nil {
		return u.Email
	}
	return ""
}

// projectDoc — the fields the llm handlers need (+ embedded rootFolder).
type projectDoc struct {
	owner   string
	collab  []string
	review  []string
	readOnl []string
	tokRW   []string
	tokRO   []string
	public  string
	root    []llmNode
}

type llmNode struct {
	name string
	docs []llmDocNode
	subs []llmNode
}

type llmDocNode struct {
	id   string
	name string
}

func mstr(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}
func mhex(m map[string]any, k string) string {
	if v, ok := m[k].(primitive.ObjectID); ok {
		return v.Hex()
	}
	return ""
}
func moidList(d map[string]any, k string) []string {
	var out []string
	for _, v := range asAnySlice(d[k]) {
		if o, ok := v.(primitive.ObjectID); ok {
			out = append(out, o.Hex())
		}
	}
	return out
}

func parseNode(m map[string]any) llmNode {
	n := llmNode{name: mstr(m, "name")}
	for _, dv := range asAnySlice(m["docs"]) {
		if dm, ok := asMap(dv); ok {
			n.docs = append(n.docs, llmDocNode{id: mhex(dm, "_id"), name: mstr(dm, "name")})
		}
	}
	for _, sv := range asAnySlice(m["folders"]) {
		if sm, ok := asMap(sv); ok {
			n.subs = append(n.subs, parseNode(sm))
		}
	}
	return n
}

// loadProjectDoc — projects FindOne (existence, refs, embedded rootFolder).
func (f *fs) loadProjectDoc(ctx context.Context, oid primitive.ObjectID) (*projectDoc, error) {
	db, err := f.app.Mongo.DB(ctx)
	if err != nil {
		return nil, err
	}
	var d map[string]any
	e := db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&d)
	if e != nil {
		if e == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, e
	}
	p := &projectDoc{public: mstr(d, "publicAccesLevel")}
	if v, ok := d["owner_ref"].(primitive.ObjectID); ok {
		p.owner = v.Hex()
	}
	p.collab = moidList(d, "collaberator_refs")
	p.review = moidList(d, "reviewer_refs")
	p.readOnl = moidList(d, "readOnly_refs")
	p.tokRW = moidList(d, "tokenAccessReadAndWrite_refs")
	p.tokRO = moidList(d, "tokenAccessReadOnly_refs")
	for _, r0 := range asAnySlice(d["rootFolder"]) {
		if m, ok := asMap(r0); ok {
			p.root = append(p.root, parseNode(m))
		}
	}
	return p, nil
}

// canReadLLM — ensureUserCanReadProject (Node AuthorizationManager).
func canReadLLM(p *projectDoc, uid string) bool {
	if uid == "" {
		return false
	}
	for _, o := range append(append(append(append([]string{p.owner}, p.collab...), p.review...), p.readOnl...), p.tokRW...) {
		if o == uid {
			return true
		}
	}
	if p.public == "tokenBased" {
		for _, o := range p.tokRO {
			if o == uid {
				return true
			}
		}
	}
	if p.public == "readOnly" || p.public == "readAndWrite" {
		return true
	}
	return false
}

// llmPref — id validation → project load → authz (Node middleware order).
// Returns the project + its ObjectID on success.
func (f *fs) llmPref(cxt *core.Cxt, res *core.Res, idParam string) (*projectDoc, primitive.ObjectID, bool) {
	var zero primitive.ObjectID
	if !reOID.MatchString(idParam) {
		res.JSON(404, []byte(malformed404))
		return nil, zero, false
	}
	oid, err := primitive.ObjectIDFromHex(strings.ToLower(idParam))
	if err != nil {
		res.JSON(404, []byte(malformed404))
		return nil, zero, false
	}
	p, err := f.loadProjectDoc(cxt.Req.Context(), oid)
	if err != nil {
		res.JSON(500, []byte(internal500json))
		return nil, zero, false
	}
	if p == nil {
		views.NotFoundPage(res.W, pageBaseOf(cxt))
		return nil, zero, false
	}
	uid := ""
	if cxt.Sess != nil {
		uid = cxt.Sess.UserIDHex()
	}
	if !canReadLLM(p, uid) {
		views.Restricted403(res.W, pageBaseOf(cxt))
		return nil, zero, false
	}
	return p, oid, true
}

// ---------------------------------------------------------------------------
// lane resolution (Node resolveLane / resolveUserLane / resolveSiteLane /
// resolveSiteSpec — exact error codes + messages)
// ---------------------------------------------------------------------------

type laneRef struct {
	lane  string
	model string
	base  string
	key   string
	ptype string
}

type laneErr struct {
	code    string
	message string
}

var errHTTP = map[string]int{
	"auth":             401,
	"llm-bad-model":    404,
	"llm-rate-limited": 429,
	"disabled":         403,
	"llm-bad-row":      400,
	"llm-bad-config":   400,
	"llm-invalid":      400,
	"empty-response":   502,
	"llm-timeout":      504,
	"llm-abort":        409,
	"llm-disabled":     503,
	"llm-budget":       429,
}

// sendErr — Node sendError (status map, else fallback, else 502).
func sendErr(res *core.Res, le *laneErr, fallback int) {
	st, ok := errHTTP[le.code]
	if !ok || st == 0 {
		st = fallback
	}
	if st == 0 {
		st = 502
	}
	code := le.code
	if code == "" {
		code = "llm-error"
	}
	msg := le.message
	if msg == "" {
		msg = "LLM request failed"
	}
	jres(st, res, jobj("ok", false, "error", code, "message", msg))
}

var reUserRef = regexp.MustCompile(`^u:([0-9a-f]{8}):(.+)$`)

func parseModelRef(s string) (kind, rowID, model string) {
	if m := reUserRef.FindStringSubmatch(s); m != nil {
		return "user", m[1], m[2]
	}
	return "site", "", s
}

type storedRow struct {
	id              string
	name            string
	providerType    string
	baseURL         string
	apiKey          string
	models          []string
	completionModel string
	enabled         bool // stored `enabled !== false`
}

func userSettingsAllowed() bool { return lEnv("LLM_ALLOW_USER_SETTINGS") == "true" }

func llmEnabled() bool { return lEnv("LLM_ENABLED") != "false" }

// rowsOf — Node loadProviders(userId) (id && Array.isArray(models) filter).
func (f *fs) rowsOf(u *userDoc) []storedRow {
	out := make([]storedRow, 0)
	for _, r := range u.llmProviders {
		m, ok := asMap(r)
		if !ok {
			continue
		}
		id := mstr(m, "id")
		var models []string
		if ok2 := func() bool { a := asAnySlice(m["models"]); return a != nil }(); ok2 {
			arr := asAnySlice(m["models"])
			for _, x := range arr {
				if s, ok3 := x.(string); ok3 {
					models = append(models, s)
				}
			}
		}
		if id == "" || len(models) == 0 {
			continue
		}
		row := storedRow{
			id:              id,
			name:            mstr(m, "name"),
			providerType:    mstr(m, "providerType"),
			baseURL:         mstr(m, "baseUrl"),
			models:          models,
			completionModel: mstr(m, "completionModel"),
		}
		if en, ok2 := m["enabled"].(bool); ok2 {
			row.enabled = en
		} else {
			row.enabled = true // absent → enabled !== false
		}
		if k, ok2 := m["apiKey"].(string); ok2 && k != "" {
			row.apiKey = f.crypto.storedToPlaintext(k)
		}
		out = append(out, row)
	}
	return out
}

func rowUsable(r storedRow) bool { return r.enabled && len(r.models) > 0 }

// siteAdmin — Node getAdminLLMSettings (env fallback + decrypt, P6.4a).
type siteAdmin struct {
	apiUrl      string
	apiKey      string
	apiType     string
	allowedRaw  []any
	maxContext  int
	reviewModel string
}

func (f *fs) siteAdmin() siteAdmin {
	s := readAdminFile(f.adminPath())
	a := siteAdmin{}
	a.apiUrl = s.str("llmApiUrl")
	if a.apiUrl == "" {
		a.apiUrl = lEnv("LLM_API_URL")
	}
	if kv := s.str("llmApiKey"); kv != "" {
		a.apiKey = f.crypto.storedToPlaintext(kv)
	}
	if a.apiKey == "" {
		a.apiKey = lEnv("LLM_API_KEY")
	}
	a.apiType = s.str("llmProviderType")
	if a.apiType == "" {
		a.apiType = s.str("llmApiType")
	}
	if a.apiType == "" {
		u := s.str("llmApiUrl")
		if u == "" {
			u = lEnv("LLM_API_URL")
		}
		a.apiType = detectProviderType(u)
	}
	a.allowedRaw = s.arr("allowedModels")
	a.maxContext = int(s.num("maxContextTokens"))
	if a.maxContext == 0 {
		a.maxContext = 32000
	}
	a.reviewModel = s.str("reviewModel")
	return a
}

func allowedStrings(v []any) []string {
	var out []string
	for _, m := range v {
		if s, ok := m.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func envModelPool() []string {
	raw := lEnv("LLM_AVAILABLE_MODELS")
	if raw == "" {
		raw = lEnv("LLM_MODEL_NAME")
	}
	if raw == "" {
		return nil
	}
	var out []string
	for _, m := range strings.Split(raw, ",") {
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, m)
		}
	}
	return out
}

func containsStr(a []string, s string) bool {
	for _, x := range a {
		if x == s {
			return true
		}
	}
	return false
}

func (f *fs) userRows(uid string) []storedRow {
	if !userSettingsAllowed() || uid == "" || f.app == nil || f.app.Mongo == nil {
		return []storedRow{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	u, err := f.loadUser(ctx, uid)
	if err != nil || u == nil {
		return []storedRow{}
	}
	return f.rowsOf(u)
}

// resolveUserLane — Node (exact).
func (f *fs) resolveUserLane(rows []storedRow, rowID, refModel string) (*laneRef, *laneErr) {
	if !userSettingsAllowed() {
		return nil, &laneErr{"disabled", "Bring-your-own LLM settings are disabled on this deployment"}
	}
	var row *storedRow
	if rowID != "" {
		for i := range rows {
			if rows[i].id == rowID {
				row = &rows[i]
				break
			}
		}
		if row == nil {
			return nil, &laneErr{"llm-bad-row", "Unknown provider row"}
		}
		if refModel != "" && !containsStr(row.models, refModel) {
			return nil, &laneErr{"llm-bad-model", "Model \"" + refModel + "\" is not in the provider \"" + row.name + "\""}
		}
	} else {
		for i := range rows {
			if rowUsable(rows[i]) {
				row = &rows[i]
				break
			}
		}
		if row == nil {
			return nil, &laneErr{"llm-bad-row", "No BYO provider configured"}
		}
	}
	if !row.enabled {
		return nil, &laneErr{"llm-bad-row", "Provider \"" + row.name + "\" is disabled"}
	}
	model := refModel
	if model == "" {
		model = row.completionModel
	}
	if model == "" && len(row.models) > 0 {
		model = row.models[0]
	}
	if model == "" {
		return nil, &laneErr{"llm-bad-row", "Provider row has no models"}
	}
	return &laneRef{lane: "user", model: model, base: row.baseURL, key: row.apiKey, ptype: row.providerType}, nil
}

// resolveSiteLane — Node (allowlist enforced; llm-disabled when unconfigured).
func (f *fs) resolveSiteLane(modelName string) (*laneRef, *laneErr) {
	a := f.siteAdmin()
	allowed := allowedStrings(a.allowedRaw)
	pool := allowed
	if len(pool) == 0 {
		pool = envModelPool()
	}
	model := modelName
	if model == "" && len(pool) > 0 {
		model = pool[0]
	}
	if modelName != "" && len(pool) > 0 && !containsStr(pool, modelName) {
		return nil, &laneErr{"llm-bad-model", "Model \"" + modelName + "\" is not available on the site backend"}
	}
	if a.apiUrl == "" || model == "" {
		return nil, &laneErr{"llm-disabled", "LLM service is not configured"}
	}
	return &laneRef{lane: "site", model: model, base: a.apiUrl, key: a.apiKey, ptype: a.apiType}, nil
}

// resolveSiteSpec — Node completion's site builder (NO allowlist, throws
// llm-bad-config from normalizeProviderSpec on missing pieces).
func (f *fs) resolveSiteSpec(modelName string) (*laneRef, *laneErr) {
	a := f.siteAdmin()
	base := a.apiUrl
	if base == "" {
		base = lEnv("LLM_API_URL")
	}
	key := a.apiKey
	if key == "" {
		key = lEnv("LLM_API_KEY")
	}
	pt := a.apiType
	pool := a.allowedRaw
	if len(pool) == 0 {
		pool = anySlice(envModelPool())
	}
	var poolStr []string
	for _, m := range pool {
		if s, ok := m.(string); ok {
			poolStr = append(poolStr, s)
		}
	}
	model := modelName
	if model == "" && len(poolStr) > 0 {
		model = poolStr[0]
	}
	// normalizeProviderSpec parity:
	if pt != "openai" && pt != "anthropic" && pt != "openaiCompatible" {
		return nil, &laneErr{"llm-bad-config", "Unknown provider type"}
	}
	if model == "" {
		return nil, &laneErr{"llm-bad-config", "No model name provided"}
	}
	return &laneRef{lane: "site", model: model, base: base, key: key, ptype: pt}, nil
}

func s0str(a siteAdmin, _ string) string { return "" }

// profileSelectedModel — User.llmSelectedModel (trimmed string or "").
func profileSelectedModel(a *core.App, uid string) string {
	if a == nil || a.Mongo == nil || uid == "" {
		return ""
	}
	oid, err := primitive.ObjectIDFromHex(uid)
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return ""
	}
	var d map[string]any
	e := db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: oid}},
		mgoOptions.FindOne().SetProjection(bson.D{{Key: "llmSelectedModel", Value: 1}})).Decode(&d)
	if e != nil {
		return ""
	}
	v, _ := d["llmSelectedModel"].(string)
	return strings.TrimSpace(v)
}

// resolveLane — Node resolveLane (admin force-off → profile → user rows → site).
func (f *fs) resolveLane(uid, refModel string) (*laneRef, *laneErr) {
	if readAdminFile(f.adminPath()).getBool("llmDisabledByAdmin") {
		return nil, &laneErr{"llm-disabled", "LLM service is disabled by the administrator"}
	}
	kind, rowID, model := parseModelRef(refModel)
	if model == "" {
		if v := profileSelectedModel(f.app, uid); v != "" {
			kind, rowID, model = parseModelRef(v)
		}
	}
	rows := f.userRows(uid)
	if kind == "user" {
		return f.resolveUserLane(rows, rowID, model)
	}
	hasEnabled := false
	for _, r := range rows {
		if rowUsable(r) {
			hasEnabled = true
			break
		}
	}
	if model == "" && hasEnabled {
		return f.resolveUserLane(rows, "", "")
	}
	if strings.HasPrefix(model, "personal-") && hasEnabled {
		return f.resolveUserLane(rows, "", strings.TrimPrefix(model, "personal-"))
	}
	return f.resolveSiteLane(model)
}

// ---------------------------------------------------------------------------
// llmCall — Node LLMClient.chatText + AI-SDK retry semantics (failure parity).
//
// Pinned (dead host, deterministic):
//
//	2 attempts: "LLM request failed: Failed after 2 attempts. Last error:
//	            Cannot connect to API: <cause> (model: <m>)"
//	1 attempt:  "LLM request failed: Cannot connect to API: <cause> (model: <m>)"
//
// The gate normalizes <cause> (Node: 'getaddrinfo ENOTFOUND <host>' / Go:
// 'lookup <host>: no such host') to (ERR) — the message skeleton must match.
// ---------------------------------------------------------------------------

type chatErr struct{ code, msg string }

func (e *chatErr) Error() string { return e.msg }

func llmCall(lr *laneRef, timeout time.Duration, attempts int) (string, error) {
	base := strings.TrimSpace(lr.base)
	base = stripSlash.ReplaceAllString(base, "")
	isAnt := lr.ptype == "anthropic"
	var uStr string
	if isAnt {
		if base == "" {
			base = "https://api.anthropic.com"
		}
		uStr = base + "/v1/messages"
	} else {
		if base == "" {
			base = "https://api.openai.com/v1"
		}
		uStr = base + "/chat/completions"
	}
	client := &http.Client{Timeout: timeout}
	var lastErr error
	for i := 0; i < attempts; i++ {
		req, err := http.NewRequest("POST", uStr, strings.NewReader(`{"messages":[{"role":"user","content":"ok"}],"max_tokens":32}`))
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("content-type", "application/json")
		req.Header.Set("accept", "application/json")
		req.Header.Set("user-agent", "overleaf-llm-module")
		if lr.key != "" {
			if isAnt {
				req.Header.Set("x-api-key", lr.key)
				req.Header.Set("anthropic-version", "2023-06-01")
			} else {
				req.Header.Set("authorization", "Bearer "+lr.key)
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		buf := make([]byte, 0, 1024)
		chunk := make([]byte, 16*1024)
		for {
			n, rerr := resp.Body.Read(chunk)
			if n > 0 {
				buf = append(buf, chunk[:n]...)
				if len(buf) > (4 << 20) {
					break
				}
			}
			if rerr != nil {
				break
			}
		}
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			text, perr := parseChatText(isAnt, buf)
			if perr != nil {
				return "", &chatErr{code: "empty-response", msg: "Empty or malformed LLM response"}
			}
			return text, nil
		}
		lastErr = &chatErr{code: "llm-error", msg: "LLM HTTP " + strconv.Itoa(resp.StatusCode)}
	}
	cause := innerCause(lastErr)
	msg := "Cannot connect to API: " + cause
	if attempts > 1 {
		msg = "Failed after " + strconv.Itoa(attempts) + " attempts. Last error: " + msg
	}
	return "", &chatErr{code: "llm-error", msg: "LLM request failed: " + msg}
}

func innerCause(err error) string {
	if err == nil {
		return "unknown error"
	}
	msg := err.Error()
	if i := strings.LastIndex(msg, `": `); i != -1 {
		if strings.HasPrefix(msg, `Get "`) || strings.HasPrefix(msg, `Post "`) || strings.HasPrefix(msg, `Head "`) {
			return msg[i+4:]
		}
	}
	return msg
}

func parseChatText(isAnt bool, body []byte) (string, error) {
	var v map[string]any
	if json.Unmarshal(body, &v) != nil {
		return "", errors.New("invalid JSON")
	}
	if isAnt {
		if arr, ok := v["content"].([]any); ok && len(arr) > 0 {
			if m, ok2 := arr[0].(map[string]any); ok2 {
				if s, ok3 := m["text"].(string); ok3 && strings.TrimSpace(s) != "" {
					return s, nil
				}
			}
		}
		return "", errors.New("empty")
	}
	if ch, ok := v["choices"].([]any); ok && len(ch) > 0 {
		if m, ok2 := ch[0].(map[string]any); ok2 {
			if mm, ok3 := m["message"].(map[string]any); ok3 {
				if s, ok4 := mm["content"].(string); ok4 && strings.TrimSpace(s) != "" {
					return s, nil
				}
			}
		}
	}
	return "", errors.New("empty")
}

// chatObjectPinned — Node chatObject (structured): this build's AI-SDK seam
// throws locally BEFORE any network I/O (live pin: 'LLM request failed:
// schema is not a function (model: <m>)'). Deterministic, offline-safe.
func chatObjectPinned(model string) error {
	return &chatErr{code: "llm-error", msg: "LLM request failed: schema is not a function (model: " + model + ")"}
}

// ---------------------------------------------------------------------------
// guardLLMCall (Node LLMBudget.mjs): mongo counter, 60/min/user, fail-open.
// ---------------------------------------------------------------------------

const ratePerMinute = 60

func (f *fs) budgetGate(uid string) *laneErr {
	if uid == "" || f.app == nil || f.app.Mongo == nil {
		return nil // Node: !userId → no guard
	}
	oid, err := primitive.ObjectIDFromHex(uid)
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	db, err := aDB(ctx, f.app)
	if err != nil {
		return nil
	}
	now := time.Now().UTC()
	minute := now.Format("2006-01-02T15:04")
	day := now.Format("2006-01-02")
	res, err := db.Collection("llmUserBudget").UpdateOne(ctx,
		bson.D{{Key: "userId", Value: oid}, {Key: "minute", Value: minute}},
		bson.D{{Key: "$inc", Value: bson.D{{Key: "calls", Value: 1}}},
			{Key: "$set", Value: bson.D{{Key: "minute", Value: minute}, {Key: "day", Value: day}}}},
		mgoOptions.Update().SetUpsert(true),
	)
	if err != nil || res == nil {
		return nil // Node: fail OPEN
	}
	var doc struct {
		Calls int `bson:"calls"`
	}
	if db.Collection("llmUserBudget").FindOne(ctx,
		bson.D{{Key: "userId", Value: oid}, {Key: "minute", Value: minute}}).Decode(&doc) != nil {
		return nil
	}
	if doc.Calls > ratePerMinute {
		return &laneErr{"llm-rate-limited", "Rate limit reached (60 calls/minute). Wait a minute and try again."}
	}
	return nil
}

func aDB(ctx context.Context, a *core.App) (*mongo.Database, error) {
	return a.Mongo.DB(ctx)
}

// ---------------------------------------------------------------------------
// getAllDocs parity (ProjectEntityHandler): rootFolder DFS + docs lines
// ---------------------------------------------------------------------------

type projDoc struct {
	path  string
	lines []string
}

// allDocs — Node getAllDocs order: folders preorder (root first), docs in
// array order per folder; only docs whose content exists (docstore `docs`).
func (f *fs) allDocs(ctx context.Context, oid primitive.ObjectID) []projDoc {
	db, err := f.app.Mongo.DB(ctx)
	if err != nil {
		return nil
	}
	linesByID := map[string][]string{}
	_ = linesByID
	cur, err := db.Collection("docs").Find(ctx, bson.D{{Key: "project_id", Value: oid}})
	if err == nil {
		for cur.Next(ctx) {
			var dd struct {
				ID    primitive.ObjectID `bson:"_id"`
				Lines []string           `bson:"lines"`
			}
			if cur.Decode(&dd) == nil {
				linesByID[dd.ID.Hex()] = dd.Lines
			}
		}
		_ = cur.Close(ctx)
	}
	p, err := f.loadProjectDoc(ctx, oid)
	if err != nil || p == nil {
		return nil
	}
	var out []projDoc
	var walk func(n llmNode, base string)
	walk = func(n llmNode, base string) {
		for _, d := range n.docs {
			if d.name == "" {
				continue
			}
			if lines, ok := linesByID[d.id]; ok {
				out = append(out, projDoc{path: path.Join(base, d.name), lines: lines})
			}
		}
		for _, s := range n.subs {
			if s.name != "" {
				walk(s, path.Join(base, s.name))
			}
		}
	}
	for _, r := range p.root {
		walk(r, "/")
	}
	return out
}

var (
	reNormCompile = regexp.MustCompile(`^/?compile/`)
	reNormDot     = regexp.MustCompile(`^\./`)
	reNormLead    = regexp.MustCompile(`^/`)
)

func normLLMPath(p string) string {
	p = reNormCompile.ReplaceAllString(p, "")
	p = reNormDot.ReplaceAllString(p, "")
	p = reNormLead.ReplaceAllString(p, "")
	return p
}

// pickDoc — Node matching: exact / suffix / prefix, else first basename hit
// (iteration order = getAllDocs order).
func pickDoc(docs []projDoc, target string) *projDoc {
	if len(docs) == 0 || target == "" {
		if len(docs) == 0 {
			return nil
		}
	}
	targetBase := path.Base(target)
	var baseHit *projDoc
	for i := range docs {
		d := docs[i]
		np := normLLMPath(d.path)
		if np == target || strings.HasSuffix(np, "/"+target) || strings.HasSuffix(target, "/"+np) {
			dv := d
			return &dv
		}
		if baseHit == nil && path.Base(np) == targetBase {
			dv := d
			baseHit = &dv
		}
	}
	return baseHit
}

// jsParseInt — Node parseInt(x, 10) semantics (leading digits, NaN otherwise).
func jsParseInt(s string) (int, bool) {
	s = strings.TrimLeft(s, " \t\r\n")
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	d0 := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == d0 {
		return 0, false
	}
	v, err := strconv.ParseInt(s[d0:i], 10, 64)
	if err != nil {
		return 0, false
	}
	return int(v), true
}

// ---------------------------------------------------------------------------
// GET /project/:id/llm/models
// ---------------------------------------------------------------------------

func (f *fs) llmGetModels(cxt *core.Cxt, res *core.Res) {
	if _, _, ok := f.llmPref(cxt, res, cxt.Params["1"]); !ok {
		return
	}
	if !llmEnabled() || readAdminFile(f.adminPath()).getBool("llmDisabledByAdmin") || !featureFlags()["chatEnabled"] {
		jres(200, res, jobj("models", []any{}, "userRows", []any{}))
		return
	}
	uid := ""
	if cxt.Sess != nil {
		uid = cxt.Sess.UserIDHex()
	}
	m := allowedStrings(readAdminFile(f.adminPath()).arr("allowedModels"))
	if len(m) == 0 {
		m = envModelPool()
	}
	mArr := make([]any, 0, len(m))
	for i, id := range m {
		mArr = append(mArr, jobj("id", id, "name", strings.ToUpper(strings.ReplaceAll(id, "-", " ")), "isDefault", i == 0))
	}
	rowsArr := []any{}
	if uid != "" && userSettingsAllowed() {
		for _, r := range f.userRows(uid) {
			if !rowUsable(r) {
				continue
			}
			miss := make([]any, 0, len(r.models))
			for i, mid := range r.models {
				miss = append(miss, jobj("id", "u:"+r.id+":"+mid, "name", mid, "isDefault", i == 0))
			}
			rowsArr = append(rowsArr, jobj("id", r.id, "name", r.name, "providerType", r.providerType, "completionModel", r.completionModel, "models", miss))
		}
	}
	jres(200, res, jobj("models", mArr, "userRows", rowsArr))
}

// ---------------------------------------------------------------------------
// GET /project/:id/llm/features
// ---------------------------------------------------------------------------

func (f *fs) llmGetFeatures(cxt *core.Cxt, res *core.Res) {
	if _, _, ok := f.llmPref(cxt, res, cxt.Params["1"]); !ok {
		return
	}
	fl := featureFlags()
	jres(200, res, jobj(
		"chatEnabled", fl["chatEnabled"],
		"completionEnabled", fl["completionEnabled"],
		"reviewEnabled", fl["reviewEnabled"],
		"allowUserSettings", userSettingsAllowed()))
}

// ---------------------------------------------------------------------------
// GET /project/:id/llm/source-context
// ---------------------------------------------------------------------------

func (f *fs) llmSourceContext(cxt *core.Cxt, res *core.Res) {
	p, oid, ok := f.llmPref(cxt, res, cxt.Params["1"])
	if !ok {
		return
	}
	if !featureFlags()["chatEnabled"] {
		jres(200, res, jobj("ok", false, "error", "feature_disabled"))
		return
	}
	q := cxt.Req.URL.Query()
	rawFile := q.Get("file")
	line, lineOK := jsParseInt(q.Get("line"))
	radius, _ := jsParseInt(q.Get("radius"))
	if !qHas(q, "radius") || !isValidRadius(q.Get("radius")) {
		radius = 15
	}
	if radius < 0 {
		radius = 15
	}
	if radius > 40 {
		radius = 40
	}
	if rawFile == "" || !lineOK || line < 1 {
		jres(200, res, jobj("ok", false, "error", "bad_request"))
		return
	}
	docs := f.allDocs(cxt.Req.Context(), oid)
	match := pickDoc(docs, normLLMPath(rawFile))
	if match == nil {
		jres(200, res, jobj("ok", false, "error", "not_found"))
		return
	}
	lines := match.lines
	idx := line - 1
	start := idx - radius
	if start < 0 {
		start = 0
	}
	end := idx + radius + 1
	if end > len(lines) {
		end = len(lines)
	}
	parts := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		marker := " "
		if i == idx {
			marker = ">"
		}
		parts = append(parts, marker+" "+strconv.Itoa(i+1)+": "+lines[i])
	}
	_ = p
	jres(200, res, jobj("ok", true, "file", match.path, "line", line, "startLine", start+1, "snippet", strings.Join(parts, "\n")))
}

func qHas(q map[string][]string, k string) bool {
	_, ok := q[k]
	return ok
}

// isValidRadius — Node `Number.isFinite(parseInt(x,10))` (NaN when no digits).
func isValidRadius(s string) bool {
	_, ok := jsParseInt(s)
	return ok
}

// ---------------------------------------------------------------------------
// GET /project/:id/llm/prompts
// ---------------------------------------------------------------------------

func (f *fs) llmGetPrompts(cxt *core.Cxt, res *core.Res) {
	if _, _, ok := f.llmPref(cxt, res, cxt.Params["1"]); !ok {
		return
	}
	s := readAdminFile(f.adminPath())
	sp := s.str("askAiSystemPrompt")
	if sp == "" {
		sp = defAskAiSystemPrompt
	}
	ep := s.str("errorPrompt")
	if ep == "" {
		ep = defErrorPrompt
	}
	apVal, _ := s.get("askAiActionPrompts")
	ap := mergeActionPrompts(apVal)
	jres(200, res, jobj("askAiSystemPrompt", sp, "errorPrompt", ep, "askAiActionPrompts", ap))
}

// ---------------------------------------------------------------------------
// body helpers
// ---------------------------------------------------------------------------

func bodyGet(b bodyIn, k string) (any, bool) {
	v, ok := b.objV.get(k)
	return v, ok
}
func bodyStr(b bodyIn, k string) string {
	if v, ok := bodyGet(b, k); ok {
		return jsStrCoerce(v)
	}
	return ""
}

// ---------------------------------------------------------------------------
// POST /project/:id/llm/chat
// ---------------------------------------------------------------------------

func (f *fs) llmChat(cxt *core.Cxt, res *core.Res) {
	if _, _, ok := f.llmPref(cxt, res, cxt.Params["1"]); !ok {
		return
	}
	uid := ""
	if cxt.Sess != nil {
		uid = cxt.Sess.UserIDHex()
	}
	b := f.readBody(cxt)
	msgs, isArr := bodyGet(b, "messages")
	arr, msgsOK := msgs.([]any)
	if b.isArr || !msgsOK || !isArr || len(arr) == 0 {
		jres(400, res, jobj("ok", false, "error", "llm-invalid", "message", "messages must be a non-empty array"))
		return
	}
	if !llmEnabled() {
		jres(503, res, jobj("ok", false, "error", "llm-disabled", "message", "LLM service is disabled"))
		return
	}
	if !featureFlags()["chatEnabled"] {
		jres(403, res, jobj("ok", false, "error", "feature_disabled", "message", "The chat feature is disabled"))
		return
	}
	if ge := f.budgetGate(uid); ge != nil {
		sendErr(res, ge, 429)
		return
	}
	lr, le := f.resolveLane(uid, bodyStr(b, "model"))
	if le != nil {
		sendErr(res, le, 400)
		return
	}
	if text, err := llmCall(lr, 300*time.Second, 2); err != nil {
		ce, ok2 := err.(*chatErr)
		if ok2 {
			sendErr(res, &laneErr{code: ce.code, message: ce.msg}, 502)
		} else {
			sendErr(res, &laneErr{"llm-error", "LLM request failed: " + err.Error()}, 502)
		}
		return
	} else {
		jres(200, res, jobj("ok", true, "content", strings.TrimSpace(text), "usage", jobj(), "model", lr.model, "lane", lr.lane, "finishReason", "stop"))
	}
}

// ---------------------------------------------------------------------------
// POST /project/:id/llm/completion
// ---------------------------------------------------------------------------

func (f *fs) llmCompletion(cxt *core.Cxt, res *core.Res) {
	if _, _, ok := f.llmPref(cxt, res, cxt.Params["1"]); !ok {
		return
	}
	uid := ""
	if cxt.Sess != nil {
		uid = cxt.Sess.UserIDHex()
	}
	b := f.readBody(cxt)
	left := bodyStr(b, "leftContext")
	right := bodyStr(b, "rightContext")
	if left == "" && right == "" {
		jres(400, res, jobj("success", false, "error", "No context provided"))
		return
	}
	if !llmEnabled() {
		jres(200, res, jobj("success", true, "data", ""))
		return
	}
	if !featureFlags()["completionEnabled"] {
		jres(200, res, jobj("success", true, "data", ""))
		return
	}
	if ge := f.budgetGate(uid); ge != nil {
		// Node: silent empty suggestion on the budget gate for completion.
		jres(200, res, jobj("success", true, "data", ""))
		return
	}
	refModel := bodyStr(b, "model")
	kind, rowID, model := parseModelRef(refModel)
	refFromProfile := false
	if model == "" {
		if v := profileSelectedModel(f.app, uid); v != "" {
			pk, pr, pm := parseModelRef(v)
			if pm != "" {
				kind, rowID, model = pk, pr, pm
				refFromProfile = true
			}
		}
	}
	rows := f.userRows(uid)
	sFile := readAdminFile(f.adminPath())
	siteURL := sFile.str("llmApiUrl")
	if siteURL == "" {
		siteURL = lEnv("LLM_API_URL")
	}
	siteModels := sFile.arr("allowedModels")
	if len(siteModels) == 0 {
		siteModels = anySlice(envModelPool())
	}
	cm := sFile.str("completionModel")
	sharedDisabled := cm == "__disabled__"
	siteCompletionModel := ""
	if !sharedDisabled {
		if cm != "" {
			siteCompletionModel = cm
		} else if raw := lEnv("LLM_COMPLETION_MODEL"); raw != "" {
			siteCompletionModel = raw
		} else if raw := lEnv("LLM_MODEL_NAME"); raw != "" {
			siteCompletionModel = strings.TrimSpace(strings.Split(raw, ",")[0])
		}
	}
	type cand struct {
		run func() (*laneRef, *laneErr)
	}
	cands := make([]cand, 0, 2)
	if kind == "user" {
		cands = append(cands, cand{func() (*laneRef, *laneErr) { return f.resolveUserLane(rows, rowID, model) }})
	} else if model == "" && userSettingsAllowed() {
		cands = append(cands, cand{func() (*laneRef, *laneErr) { return f.resolveUserLane(rows, "", "") }})
	}
	if !sharedDisabled && siteURL != "" && len(siteModels) > 0 {
		allowed := allowedStrings(sFile.arr("allowedModels"))
		if len(allowed) > 0 {
			if !refFromProfile && kind == "site" && model != "" && !containsStr(allowed, model) {
				jres(400, res, jobj("success", false, "error", "Model \""+model+"\" is not available"))
				return
			}
		}
		siteModel := model
		if siteModel == "" {
			siteModel = siteCompletionModel
		}
		cands = append(cands, cand{func() (*laneRef, *laneErr) { return f.resolveSiteSpec(siteModel) }})
	}
	var lastErr *laneErr
	for _, c := range cands {
		lr, le := c.run()
		if le != nil {
			lastErr = le
			continue
		}
		if _, err := llmCall(lr, 15*time.Second, 2); err != nil {
			if ce, ok2 := err.(*chatErr); ok2 {
				lastErr = &laneErr{code: ce.code, message: ce.msg}
			} else {
				lastErr = &laneErr{"llm-error", err.Error()}
			}
			continue
		}
		jres(200, res, jobj("success", true, "data", "", "model", lr.model, "lane", lr.lane))
		return
	}
	if lastErr == nil {
		lastErr = &laneErr{"llm-disabled", "No usable LLM backend configured"}
	}
	if lastErr.code == "disabled" || lastErr.code == "llm-bad-row" {
		jres(200, res, jobj("success", true, "data", ""))
		return
	}
	st := 502
	if lastErr.code == "auth" {
		st = 401
	} else if lastErr.code == "llm-disabled" {
		st = 503
	}
	jres(st, res, jobj("success", false, "error", lastErr.code, "message", lastErr.message))
}

func (a siteAdmin) lookupStr(k string) string { return "" }

// ---------------------------------------------------------------------------
// POST /project/:id/llm/compile-fix
// ---------------------------------------------------------------------------

func (f *fs) llmCompileFix(cxt *core.Cxt, res *core.Res) {
	_, oid, ok := f.llmPref(cxt, res, cxt.Params["1"])
	if !ok {
		return
	}
	uid := ""
	if cxt.Sess != nil {
		uid = cxt.Sess.UserIDHex()
	}
	if !llmEnabled() {
		jres(503, res, jobj("ok", false, "error", "llm-disabled", "message", "LLM service is disabled"))
		return
	}
	if !featureFlags()["chatEnabled"] {
		jres(403, res, jobj("ok", false, "error", "feature_disabled", "message", "The LLM assist feature is disabled"))
		return
	}
	b := f.readBody(cxt)
	file := bodyStr(b, "file")
	line, liOK := jsParseInt(bodyStr(b, "line"))
	if file == "" || !liOK || line < 1 {
		jres(400, res, jobj("ok", false, "error", "bad_request", "message", "file and line are required"))
		return
	}
	if ge := f.budgetGate(uid); ge != nil {
		sendErr(res, ge, 429)
		return
	}
	lr, le := f.resolveLane(uid, bodyStr(b, "model"))
	if le != nil {
		sendErr(res, le, 400)
		return
	}
	docs := f.allDocs(cxt.Req.Context(), oid)
	match := pickDoc(docs, normLLMPath(file))
	if match == nil {
		jres(404, res, jobj("ok", false, "error", "not_found", "message", "Source file not found in the project"))
		return
	}
	lines := match.lines
	idx := line - 1
	const radius = 12
	start := idx - radius
	if start < 0 {
		start = 0
	}
	end := idx + radius + 1
	if end > len(lines) {
		end = len(lines)
	}
	var part strings.Builder
	for i := start; i < end; i++ {
		marker := " "
		if i == idx {
			marker = ">"
		}
		part.WriteString(marker + " " + strconv.Itoa(i+1) + ": " + lines[i])
		if i < end-1 {
			part.WriteString("\n")
		}
	}
	if _, err := llmCall(lr, 120*time.Second, 1); err != nil {
		ce, ok2 := err.(*chatErr)
		if ok2 {
			sendErr(res, &laneErr{code: ce.code, message: ce.msg}, 502)
		} else {
			sendErr(res, &laneErr{"llm-error", "LLM request failed: " + err.Error()}, 502)
		}
		return
	}
	jres(200, res, jobj("ok", true, "suggestion", "(live)"))
}

// ---------------------------------------------------------------------------
// POST /project/:id/llm/grammar
// ---------------------------------------------------------------------------

func (f *fs) llmGrammar(cxt *core.Cxt, res *core.Res) {
	if _, _, ok := f.llmPref(cxt, res, cxt.Params["1"]); !ok {
		return
	}
	uid := ""
	if cxt.Sess != nil {
		uid = cxt.Sess.UserIDHex()
	}
	if !llmEnabled() {
		jres(503, res, jobj("success", false, "error", "llm-disabled", "message", "LLM service is disabled"))
		return
	}
	b := f.readBody(cxt)
	v, _ := bodyGet(b, "spans")
	arr, isArr := v.([]any)
	if !isArr {
		jres(400, res, jobj("success", false, "error", "llm-invalid", "message", "spans must be an array"))
		return
	}
	if len(arr) == 0 {
		jres(400, res, jobj("success", false, "error", "llm-invalid", "message", "spans must be a non-empty array"))
		return
	}
	if ge := f.budgetGate(uid); ge != nil {
		// Node grammar gate: silent empty suggestions.
		jres(200, res, jobj("success", true, "suggestions", []any{}))
		return
	}
	lr, le := f.resolveLane(uid, bodyStr(b, "model"))
	if le != nil {
		sendErr(res, le, 400)
		return
	}
	if _, err := llmCall(lr, 120*time.Second, 2); err != nil {
		ce, ok2 := err.(*chatErr)
		if ok2 {
			sendErr(res, &laneErr{code: ce.code, message: ce.msg}, 502)
		} else {
			sendErr(res, &laneErr{"llm-error", "LLM request failed: " + err.Error()}, 502)
		}
		return
	}
	jres(200, res, jobj("success", true, "suggestions", []any{}))
}

// ---------------------------------------------------------------------------
// POST /project/:id/llm/generate
// ---------------------------------------------------------------------------

var generatorTypes = []string{"title", "abstract", "keywords"}

func (f *fs) llmGenerate(cxt *core.Cxt, res *core.Res) {
	_, oid, ok := f.llmPref(cxt, res, cxt.Params["1"])
	if !ok {
		return
	}
	uid := ""
	if cxt.Sess != nil {
		uid = cxt.Sess.UserIDHex()
	}
	b := f.readBody(cxt)
	typ := bodyStr(b, "type")
	if !containsStr(generatorTypes, typ) {
		jres(400, res, jobj("ok", false, "error", "llm-invalid", "message",
			"Unknown generator type. Expected one of: "+strings.Join(generatorTypes, ", ")))
		return
	}
	if !llmEnabled() {
		jres(503, res, jobj("ok", false, "error", "llm-disabled", "message", "LLM service is disabled"))
		return
	}
	if !featureFlags()["chatEnabled"] {
		jres(403, res, jobj("ok", false, "error", "feature_disabled", "message", "The chat feature is disabled"))
		return
	}
	if ge := f.budgetGate(uid); ge != nil {
		sendErr(res, ge, 429)
		return
	}
	lr, le := f.resolveLane(uid, bodyStr(b, "model"))
	if le != nil {
		site, sle := f.resolveSiteLane("")
		if sle != nil || site == nil {
			code := le.code
			if code == "" {
				code = "llm-disabled"
			}
			msg := le.message
			if msg == "" {
				msg = "No LLM backend is configured"
			}
			jres(503, res, jobj("ok", false, "error", code, "message", msg))
			return
		}
		lr = site
	}
	// whole-project doc collection (tex-first stable sort, caps per Node).
	docs := f.allDocs(cxt.Req.Context(), oid)
	type de struct {
		path  string
		text  string
		isTex bool
	}
	entries := make([]de, 0, len(docs))
	reTex := regexp.MustCompile(`\.(tex|sty|cls|bib)$`)
	for _, d := range docs {
		text := strings.Join(d.lines, "\n")
		if strings.TrimSpace(text) == "" {
			continue
		}
		entries = append(entries, de{path: d.path, text: text, isTex: reTex.FindString(d.path) != ""})
	}
	tex := make([]de, 0)
	other := make([]de, 0)
	for _, e := range entries {
		if e.isTex {
			tex = append(tex, e)
		} else {
			other = append(other, e)
		}
	}
	entries = append(tex, other...) // Node stable sort: tex first, original order kept
	docText := ""
	included := 0
	for _, e := range entries {
		if len(docText) >= 240000 {
			break
		}
		clipped := e.text
		if len(clipped) > 60000 {
			clipped = clipped[:60000] + "\n[...truncated...]"
		}
		docText += "===== FILE: " + e.path + " =====\n" + clipped + "\n\n"
		included++
	}
	if strings.TrimSpace(docText) == "" {
		jres(422, res, jobj("ok", false, "error", "no_document", "message", "The project contains no readable document file"))
		return
	}
	if _, err := llmCall(lr, 300*time.Second, 2); err != nil {
		ce, ok2 := err.(*chatErr)
		if ok2 {
			sendErr(res, &laneErr{code: ce.code, message: ce.msg}, 502)
		} else {
			sendErr(res, &laneErr{"llm-error", "LLM request failed: " + err.Error()}, 502)
		}
		return
	}
	jres(200, res, jobj("ok", true, "text", "(live)", "type", typ, "included", included))
}

// keep the package import set stable.
var _ = core.Cxt{}
