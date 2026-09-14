// Package serveradmin ports the CE ServerAdmin leaf routes (P3.1 flip
// unit) — the system-message CRUD and the editor-gate trio:
//
//	GET    /admin/editor-state            (admin, CE)
//	POST   /admin/openEditor              (admin, CE)
//	POST   /admin/closeEditor             (admin, CE)
//	POST   /admin/messages                (admin)
//	POST   /admin/messages/clear          (admin)
//	PATCH  /admin/messages/:message_id    (admin)
//	DELETE /admin/messages/:message_id    (admin)
//
// Node ground truth (services/web/app/src/Features/ServerAdmin/
// AdminController.mjs + SystemMessages/SystemMessageManager.mjs +
// AuthorizationMiddleware.ensureUserIsSiteAdmin), every response shape
// pinned live against the running e2e stack on 2026-09-13 (p3pin*.json);
// the pinned strings below are cited verbatim.
package serveradmin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
)

// adminEmail mirrors settings.defaults.js adminEmail
// (process.env.ADMIN_EMAIL || 'placeholder@example.com') — the general/500
// contact link.
func adminEmail() string {
	if v := os.Getenv("ADMIN_EMAIL"); v != "" {
		return v
	}
	return "placeholder@example.com"
}

func Feature(a *core.App) core.Feature {
	return core.Feature{
		Name: "serveradmin",
		Routes: []core.Route{
			{Method: "GET", Path: "/admin/editor-state", Handler: editorState(a)},
			{Method: "POST", Path: "/admin/openEditor", Handler: openEditor(a)},
			{Method: "POST", Path: "/admin/closeEditor", Handler: closeEditor(a)},
			{Method: "POST", Path: "/admin/messages", Handler: createMessage(a)},
			{Method: "POST", Path: "/admin/messages/clear", Handler: clearMessages(a)},
			{
				Method:  "PATCH",
				Pattern: regexp.MustCompile(`^/admin/messages/(?P<id>[^/]+)$`),
				Handler: updateMessage(a),
			},
			{
				Method:  "DELETE",
				Pattern: regexp.MustCompile(`^/admin/messages/(?P<id>[^/]+)$`),
				Handler: deleteMessage(a),
			},
		},
	}
}

// ---------- handlers ----------

func editorState(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		// `!== false` semantics for both fields (pinned:
		// {"editorIsOpen":true,"siteIsOpen":true}).
		body := `{"editorIsOpen":` + boolJSON(core.EditorOpen()) +
			`,"siteIsOpen":` + boolJSON(core.SiteOpen()) + `}`
		res.JSON(200, []byte(body))
	}
}

func openEditor(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		// Node: no body validation (openEditor(req,res) ignores body).
		drainBody(cxt.Req)
		core.SetEditorOpen(true, true)
		res.Redirect(cxt.Req, 302, "/admin#open-close-editor")
	}
}

func closeEditor(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		body, keyOrder, kind, ok := readBody(cxt.Req)
		if !ok {
			if kind == "scalar" {
				res.BareWrite(400, []byte("{}"))
				return
			}
			res.JSON(400, validationError(`Invalid input: expected object, received `+kind+` at \"body\"`))
			return
		}
		var issues []string
		isOpen, present := body["isOpen"]
		if present && !isBool(isOpen) {
			t := "undefined"
			if isOpen != nil {
				t = zodType(isOpen)
			}
			issues = append(issues, `Invalid input: expected boolean, received `+t+` at \"body.isOpen\"`)
		}
		if u, bad := unrecognized(body, keyOrder, "isOpen"); bad {
			issues = append(issues, u)
		}
		if len(issues) > 0 {
			res.JSON(400, validationError(joinIssues(issues)))
			return
		}
		if present && isOpen != nil {
			core.SetEditorOpen(isOpen.(bool), true)
		} else {
			// Node: `Settings.editorIsOpen = body.isOpen` — missing (or
			// null) → the /status reader sees CLOSED while editor-state
			// reports OPEN (pinned live: the tri-state divergence).
			core.SetEditorOpen(false, false)
		}
		res.Redirect(cxt.Req, 302, "/admin#open-close-editor")
	}
}

func createMessage(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		body, keyOrder, kind, ok := readBody(cxt.Req)
		if !ok {
			if kind == "scalar" {
				res.BareWrite(400, []byte("{}"))
				return
			}
			res.JSON(400, validationError(`Invalid input: expected object, received `+kind+` at \"body\"`))
			return
		}
		issues := createIssues(body, keyOrder)
		if len(issues) > 0 {
			res.JSON(400, validationError(joinIssues(issues)))
			return
		}
		content, _ := body["content"].(string)
		placements := []string{}
		if pl, present := body["placements"]; present {
			if arr, ok := pl.([]any); ok {
				placements = make([]string, 0, len(arr))
				for _, v := range arr {
					placements = append(placements, asString(v))
				}
			}
		}
		if !insertMessage(a, cxt, content, placements) {
			serve500(a, cxt, res)
			return
		}
		notifyMessageRefresh(a)
		res.Redirect(cxt.Req, 302, "/admin#system-messages")
	}
}

// notifyMessageRefresh mirrors SystemMessageManager.notifyOtherPods —
// publish on the 'refresh-system-messages' channel (empty message) so the
// Node pods refresh their cached /system/messages list.
func notifyMessageRefresh(a *core.App) {
	if a.Redis != nil {
		_ = a.Redis.Publish("refresh-system-messages", "")
	}
}

func clearMessages(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		// Node clearMessages: no body validation at all.
		drainBody(cxt.Req)
		if !clearAll(a, cxt) {
			serve500(a, cxt, res)
			return
		}
		notifyMessageRefresh(a)
		res.Redirect(cxt.Req, 302, "/admin#system-messages")
	}
}

func updateMessage(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		body, keyOrder, kind, ok := readBody(cxt.Req)
		if !ok {
			if kind == "scalar" {
				res.BareWrite(400, []byte("{}"))
				return
			}
			res.JSON(400, validationError(`Invalid input: expected object, received `+kind+` at \"body\"`))
			return
		}
		var issues []string
		// placements: REQUIRED array (strictObject; pinned 'received
		// undefined' shape).
		if placements, present := body["placements"]; !present || placements == nil {
			t := "undefined"
			if present {
				t = zodType(placements)
			}
			issues = append(issues, `Invalid input: expected array, received `+t+` at \"body.placements\"`)
		} else if l, bad := placementsIssues(placements); bad {
			issues = append(issues, l...)
		}
		if u, bad := unrecognized(body, keyOrder, "placements"); bad {
			issues = append(issues, u)
		}
		if len(issues) > 0 {
			res.JSON(400, validationError(joinIssues(issues)))
			return
		}
		placements := make([]string, 0)
		for _, v := range body["placements"].([]any) {
			placements = append(placements, asString(v))
		}
		id := cxt.Params["id"]
		if id == "" {
			serve500(a, cxt, res)
			return
		}
		if !patchMessage(a, cxt, id, placements) {
			serve500(a, cxt, res) // cast error OR missing doc → 500 page (pinned)
			return
		}
		notifyMessageRefresh(a)
		finishAction(cxt, res)
	}
}

func deleteMessage(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		drainBody(cxt.Req)
		id := cxt.Params["id"]
		if id == "" {
			serve500(a, cxt, res)
			return
		}
		if !removeMessage(a, cxt, id) {
			serve500(a, cxt, res)
			return
		}
		notifyMessageRefresh(a)
		finishAction(cxt, res)
	}
}

// finishAction — PATCH/DELETE success branch (Node AdminController):
//
//	if (req.xhr || req.headers.accept?.includes('application/json'))
//	  → 200 {"success":true}    else    302 /admin#system-messages
func finishAction(cxt *core.Cxt, res *core.Res) {
	r := cxt.Req
	xhr := r.Header.Get("X-Requested-With") == "XMLHttpRequest"
	if xhr || strings.Contains(r.Header.Get("Accept"), "application/json") {
		res.JSON(200, []byte(`{"success":true}`))
		return
	}
	res.Redirect(r, 302, "/admin#system-messages")
}

// ---------- validation helpers (zod strictObject parity, pinned P3.1) ----------

func boolJSON(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func isString(v any) bool { _, ok := v.(string); return ok }
func isBool(v any) bool   { _, ok := v.(bool); return ok }
func asString(v any) string {
	s, _ := v.(string)
	return s
}

// zodType — the "received <type>" token (zod vocabulary).
func zodType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

// validationError — parseReq/handleValidationError output (pinned exact):
// {"error":"Validation error: <issues>","statusCode":400}
func validationError(issues string) []byte {
	out := `{"error":"Validation error: ` + issues + `","statusCode":400}`
	return []byte(out)
}

func joinIssues(issues []string) string { return strings.Join(issues, "; ") }

// unrecognized — the trailing strictObject issue (pinned shapes):
//
//	one:   Unrecognized key: "zz" at "body"
//	many:  Unrecognized keys: "zzz", "aaa" at "body"   (body insertion order)
func unrecognized(body map[string]any, keyOrder []string, allowed ...string) (string, bool) {
	allow := map[string]bool{}
	for _, a := range allowed {
		allow[a] = true
	}
	var unknown []string
	for _, k := range keyOrder {
		if !allow[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return "", false
	}
	if len(unknown) == 1 {
		return `Unrecognized key: \"` + unknown[0] + `\" at \"body\"`, true
	}
	parts := make([]string, len(unknown))
	for i, k := range unknown {
		parts[i] = `\"` + k + `\"`
	}
	return `Unrecognized keys: ` + strings.Join(parts, ", ") + ` at \"body\"`, true
}

// placementsIssues — z.array(z.enum(['editor','hub','auth'])).max(4)
// (pinned, incl. the STRING-input quirk where the max check is measured
// in characters and rides AFTER the type issue):
//
//	string len>4 → type + "Too big: expected string to have <=4 characters at \\"body.placements\\\""
//	string len≤4 → type issue only
//	other non-array → type issue only
//	array → per-index "Invalid option: expected one of \\"editor\\\"|\\\"hub\\\"|\\\"auth\\\" at \\\"body.placements[i]\\\""
//	         then (len>4) "Too big: expected array to have <=4 items at \\"body.placements\\\""
func placementsIssues(v any) ([]string, bool) {
	switch t := v.(type) {
	case string:
		out := []string{`Invalid input: expected array, received string at \"body.placements\"`}
		if len(t) > 4 {
			out = append(out, `Too big: expected string to have <=4 characters at \"body.placements\"`)
		}
		return out, true
	case []any:
		var out []string
		valid := map[string]bool{"editor": true, "hub": true, "auth": true}
		for i, e := range t {
			s, _ := e.(string)
			if !valid[s] {
				out = append(out, `Invalid option: expected one of \"editor\"|\"hub\"|\"auth\" at \"body.placements[`+strconv.Itoa(i)+`]\"`)
			}
		}
		if len(t) > 4 {
			out = append(out, `Too big: expected array to have <=4 items at \"body.placements\"`)
		}
		if len(out) == 0 {
			return nil, false
		}
		return out, true
	default:
		return []string{`Invalid input: expected array, received ` + zodType(v) + ` at \"body.placements\"`}, true
	}
}

// ---------- body reading (presence + order aware) ----------

// readBody decodes the JSON body. Returns (fields, keyOrder, kind, ok):
// ok=false only for non-object JSON (array/scalar) — kind names it.
// Empty/absent body → empty object (node body-parser req.body={} — pinned:
// missing-content 400 shape), scalars/arrays → !ok.
func readBody(r *http.Request) (map[string]any, []string, string, bool) {
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return map[string]any{}, nil, "", true
	}
	if trimmed == "null" {
		// express.json accepts null; zod then reports 'received null'
		return nil, nil, "null", false
	}
	if strings.HasPrefix(trimmed, "[") {
		return nil, nil, "array", false
	}
	if strings.HasPrefix(trimmed, `"`) {
		return nil, nil, "scalar", false
	}
	if trimmed == "true" || trimmed == "false" {
		return nil, nil, "scalar", false
	}
	var probe any
	if err := json.Unmarshal(raw, &probe); err != nil {
		// unparseable → node body-parser entity.parse.failed → 400 {} (pinned)
		return nil, nil, "scalar", false
	}
	obj, ok := probe.(map[string]any)
	if !ok {
		// number / true / false / other non-object root → node express.json
		// strict mode rejects before helmet → 400 {} (pinned live 2026-09-14)
		return nil, nil, "scalar", false
	}
	return obj, keyOrder(raw), "", true
}

// keyOrder — top-level key order from the raw JSON text (zod reports
// unknown keys in document order; Go maps lose it).
func keyOrder(raw []byte) []string {
	var order []string
	for _, pair := range topLevelKeys(raw) {
		order = append(order, pair)
	}
	return order
}

// topLevelKeys walks the raw object JSON via a streaming decoder.
func topLevelKeys(b []byte) []string {
	var keys []string
	dec := json.NewDecoder(strings.NewReader(string(b)))
	tok, _ := dec.Token()
	if tok != json.Delim('{') {
		return nil
	}
	for dec.More() {
		kt, _ := dec.Token()
		if ks, ok := kt.(string); ok {
			keys = append(keys, ks)
		}
		var v any
		_ = dec.Decode(&v)
	}
	return keys
}

func drainBody(r *http.Request) {
	if r.Body != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	}
}

// createIssues implements createMessageSchema (z.strictObject) issue
// collection, pinned order: content, then placements, then unknown keys.
func createIssues(body map[string]any, keyOrder []string) []string {
	var issues []string
	if content, present := body["content"]; !present || content == nil || !isString(content) {
		t := "undefined"
		if present {
			t = zodType(content)
		}
		issues = append(issues, `Invalid input: expected string, received `+t+` at \"body.content\"`)
	}
	if placements, present := body["placements"]; present {
		if l, bad := placementsIssues(placements); bad {
			issues = append(issues, l...)
		}
	}
	if u, bad := unrecognized(body, keyOrder, "content", "placements"); bad {
		issues = append(issues, u)
	}
	return issues
}

// ---------- mongo (shared `system_messages` collection) ----------

func ctxOf(cxt *core.Cxt) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 5*time.Second)
	return ctx, cancel
}

func insertMessage(a *core.App, cxt *core.Cxt, content string, placements []string) bool {
	if a.Mongo == nil {
		return false
	}
	ctx, cancel := ctxOf(cxt)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	_, err = db.Collection("systemmessages").InsertOne(ctx, bson.D{
		{Key: "_id", Value: primitive.NewObjectID()},
		{Key: "content", Value: content},
		{Key: "placements", Value: placements},
		{Key: "__v", Value: 0},
	})
	return err == nil
}

func patchMessage(a *core.App, cxt *core.Cxt, id string, placements []string) bool {
	if a.Mongo == nil {
		return false
	}
	ctx, cancel := ctxOf(cxt)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return false // Node: CastError → 500 page (pinned 'abc')
	}
	res, err := db.Collection("systemmessages").UpdateOne(ctx,
		bson.D{{Key: "_id", Value: oid}},
		bson.D{
			{Key: "$set", Value: bson.D{{Key: "placements", Value: placements}}},
			{Key: "$inc", Value: bson.D{{Key: "__v", Value: 1}}},
		})
	if err != nil {
		return false
	}
	return res.MatchedCount > 0 // missing doc → Node error → 500 page (pinned)
}

func removeMessage(a *core.App, cxt *core.Cxt, id string) bool {
	if a.Mongo == nil {
		return false
	}
	ctx, cancel := ctxOf(cxt)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return false
	}
	res, err := db.Collection("systemmessages").DeleteOne(ctx,
		bson.D{{Key: "_id", Value: oid}})
	if err != nil {
		return false
	}
	return res.DeletedCount > 0
}

func clearAll(a *core.App, cxt *core.Cxt) bool {
	if a.Mongo == nil {
		return false
	}
	ctx, cancel := ctxOf(cxt)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	_, err = db.Collection("systemmessages").DeleteMany(ctx, bson.D{})
	return err == nil
}

// serve500 — the Node 500 for this surface is the RENDERED general/500
// page (pinned: nonce CSP + PP + 681-byte body, not the plain 500 text).
func serve500(a *core.App, cxt *core.Cxt, res *core.Res) {
	if a.Render500 != nil {
		a.Render500(cxt, res)
		return
	}
	res.SendStatus(500)
}
