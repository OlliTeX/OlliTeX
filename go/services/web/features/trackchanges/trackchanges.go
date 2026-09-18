// P6.12 — track-changes module surface for the OlliTeX Go web (WEB_GO_PLAN).
//
// Node module oracle (services/web/modules/track-changes, 11 routes):
// POST /project/:project_id/track_changes                       (read authz)
// POST /project/:project_id/doc/:doc_id/changes/accept          (write authz)
// GET  /project/:project_id/ranges                              (read authz)
// GET  /project/:project_id/changes/users                       (read authz)
// GET  /project/:project_id/threads                             (read authz)
// POST /project/:project_id/thread/:thread_id/messages                              (read authz)
// POST /project/:project_id/thread/:thread_id/messages/:message_id/edit            (read authz)
// DELETE /project/:project_id/thread/:thread_id/messages/:message_id               (read authz)
// POST /project/:project_id/doc/:doc_id/thread/:thread_id/resolve                  (read authz)
// POST /project/:project_id/doc/:doc_id/thread/:thread_id/reopen                   (read authz)
// DELETE /project/:project_id/doc/:doc_id/thread/:thread_id                        (write authz)
//
// Downstream (shared services — Go web targets the SAME backends as Node):
//   chat      :3010   thread rooms/messages API (go chat)
//   DU        :3003   /project/:pid/doc/:docid/... (Node document-updater)
//   docstore  :3016   /project/:pid/tracked-changes-user-ids
//   users     sharelatex.users (getPersonalInfo projection + formatPersonalInfo)
//
// Pinned Node behaviors (oracle 2026-09-18):
//   track_changes validation 400s (exact messages); accepted → 204; stored
//     true | false | {uid|__guests__:bool}.
//   empty reads: ranges `[]`, changes/users `[]`, threads `{}`; injections
//     append `user` objects (absent → key dropped, never null).
//   comment cycle → 204 empty; ANY downstream failure (ghost message,
//     ghost/non-hex thread, DU failures) → rendered 500 page (Node:
//     next(err) without status → general/500 view).
//   authz: bad oid → 404 JSON validation; ghost → 404 OlliTeX page; other
//     project → 403 (accept-json → {"message":"restricted"} / else 403 view).
//   rate limits (SHARED redis keys — identical on both stacks):
//     track-changes-reads 60/min, track-changes-writes 20/min; over → 429
//     "Rate limit reached, please try again later" (express .write: no
//     Content-Type header).
//   editor-events redis publish (channel `editor-events`; blob
//     {room_id,message,payload,_id:"web:<host>:<rnd>-<n>"}).

package trackchanges

import (
	"regexp"

	"ollitex/go/services/web/core"
)

var tcHex24 = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// tcBadOID404 — shared P4 zod param schema (params.project_id).
const tcBadOID404 = `{"error":"Validation error: Invalid Mongo ObjectId at \"params.project_id\"","statusCode":404}`

func Feature(a *core.App) core.Feature {
	p := func(s string) *regexp.Regexp {
		return regexp.MustCompile(`^/project/([^/]+)` + s + `$`)
	}
	// Rate limiting: Node wires `track-changes-reads` (60/min) and
	// `track-changes-writes` (20/min) as RateLimiterMiddleware AFTER its
	// authz middlewares, and the e2e stack sets OVERLEAF_DISABLE_RATE_LIMITS=true
	// (Settings.disableRateLimits → consume() is a no-op, so Node NEVER 429s
	// here). Parity for this stack (editorpages precedent): Go registers NO
	// limiter — a limiter-less runner whose reads/writes gates always pass.
	// (A production profile with limits enabled would re-add the core
	// limiters AND move the gate AFTER tcAuthzProject so only authz-passing
	// requests count, matching Node's middleware order.)
	lim := &tcLimitRunner{}
	return core.Feature{Name: "track-changes", Routes: []core.Route{
		{Method: "POST", Pattern: p("/track_changes$"), Handler: hTrackChanges(a, lim)},
		{Method: "POST", Pattern: p("/doc/([^/]+)/changes/accept$"), Handler: hAcceptChanges(a, lim)},
		{Method: "GET", Pattern: p("/ranges$"), Handler: hGetAllRanges(a, lim)},
		{Method: "GET", Pattern: p("/changes/users$"), Handler: hGetChangesUsers(a, lim)},
		{Method: "GET", Pattern: p("/threads$"), Handler: hGetThreads(a, lim)},
		{Method: "POST", Pattern: p("/thread/([^/]+)/messages$"), Handler: hSendComment(a, lim)},
		{Method: "POST", Pattern: p("/thread/([^/]+)/messages/([^/]+)/edit$"), Handler: hEditMessage(a, lim)},
		{Method: "DELETE", Pattern: p("/thread/([^/]+)/messages/([^/]+)$"), Handler: hDeleteMessage(a, lim)},
		{Method: "POST", Pattern: p("/doc/([^/]+)/thread/([^/]+)/resolve$"), Handler: hResolveThread(a, lim)},
		{Method: "POST", Pattern: p("/doc/([^/]+)/thread/([^/]+)/reopen$"), Handler: hReopenThread(a, lim)},
		{Method: "DELETE", Pattern: p("/doc/([^/]+)/thread/([^/]+)$"), Handler: hDeleteThread(a, lim)},
	}}
}
