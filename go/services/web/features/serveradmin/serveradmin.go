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
	"ollitex/go/services/web/core"
	"os"
	"regexp"
	"strings"
	"time"
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
			// U9: the /admin shell page (Express case + slash tolerant;
			// canonical alternate link stays path-aware).
			{Method: "GET", Pattern: adminPageRe, Handler: adminPage(a)},
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

// ---------- validation helpers (zod strictObject parity, pinned P3.1) ----------

func boolJSON(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func isString(v any) bool { _, ok := v.(string); return ok }

func isBool(v any) bool { _, ok := v.(bool); return ok }

func asString(v any) string {
	s, _ := v.(string)
	return s
}

// zodType — the "received <type>" token (zod vocabulary).

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

// ---------- mongo (shared `system_messages` collection) ----------

func ctxOf(cxt *core.Cxt) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 5*time.Second)
	return ctx, cancel
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
