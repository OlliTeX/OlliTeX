// Package wakatime — candidate F (owner-adopted 2026-09-29, queue
// B→G→F): WakaTime / Wakapi activity tracking for OlliTeX.
//
// Reference: the wakatime module of yu-i-i/overleaf-cep#249
// (services/web/modules/wakatime in the reference checkout) — ported to the
// Go web (which is the OlliTeX runtime). House rules kept 1:1:
//
//   - OPT-IN: the feature is registered only when WAKATIME_INTEGRATION_ENABLED
//     != "false" (reference default ON) — OlliTeX default OFF per owner
//     directive ("opt-in only, off-by-default"); env or
//     site_settings.global.wakatime.enabled can turn it on (zotero pattern).
//   - Per-user credentials: the user pastes an API key from their OWN
//     wakatime.com (or self-hosted Wakapi) account settings; it is stored
//     ENCRYPTED (access-token-encryptor v3 scheme, label
//     OL_CEP_wakatime-v3, password from WAKATIME_TOKEN_CIPHER_PASSWORD or
//     auto-generated in the persistent data dir, 0o600).
//   - Egress: heartbeats are relayed by the server to the user's own
//     WakaTime/Wakapi endpoint with the user's key; the browser never talks
//     to wakatime.com and never sees the key. Air-gapped instances degrade
//     gracefully: the relay fails, the browser logs silently and the editor
//     is never blocked (reference behaviour — no queue; documented).
//
// Routes (reference parity):
//
//	GET    /user/wakatime/status                    (login)
//	PUT    /user/wakatime                           (login) {apiUrl, apiKey}
//	DELETE /user/wakatime                           (login)
//	POST   /project/:id/wakatime/heartbeat          (login + read)
//	POST   /project/:id/wakatime/heartbeats/bulk    (login + read) [≤50]
//	GET    /project/:id/wakatime/summary            (login + read)
//
// WakaTime API (v1 surface, wakatime.com + Wakapi compatible), 15 s timeout:
//	GET  {base}/users/current                    (verify/status)
//	POST {base}/users/current/heartbeats         (single)
//	POST {base}/users/current/heartbeats.bulk    (array)
//	GET  {base}/users/current/summaries?start=&end=&project=
//	headers: Authorization: Basic base64(apiKey)
//	          User-Agent: wakatime/1.0.0 (linux) ollitex/1.0 ollitex-wakatime/1.0
//	error mapping: timeout 504 / 401|403 → 403 / 404 → 404 / 429 → 429 /
//	else 500 — body {"message": ...} (reference controller shape).

package wakatime

import (
	"net/http"
	"regexp"

	"ollitex/go/services/web/core"
)

var (
	pj           = `[a-fA-F0-9]{24}`
	heartbeatPat = regexp.MustCompile(
		`^/project/(?P<1>` + pj + `)/(?i:wakatime)/(?i:heartbeat)/?$`)
	bulkPat = regexp.MustCompile(
		`^/project/(?P<1>` + pj + `)/(?i:wakatime)/(?i:heartbeats)/(?i:bulk)/?$`)
	summaryPat = regexp.MustCompile(
		`^/project/(?P<1>` + pj + `)/(?i:wakatime)/(?i:summary)/?$`)
	statusPat = regexp.MustCompile(`^/(?i:user)/(?i:wakatime)/(?i:status)/?$`)
	userPat   = regexp.MustCompile(`^/(?i:user)/(?i:wakatime)/?$`)
	// owner 2026-10-10 (Q2): the user's own cross-project summary for the
	// /user-settings WakaTime view.
	userSummaryPat = regexp.MustCompile(`^/(?i:user)/(?i:wakatime)/(?i:summary)/?$`)

	// G (owner 2026-10-09): the admin section endpoints.
	wakaCheckPat     = regexp.MustCompile(`^/(?i:admin)/(?i:wakatime)/(?i:check)/?$`)
	wakaProvisionPat = regexp.MustCompile(`^/(?i:admin)/(?i:wakatime)/(?i:provision)/?$`)
)

// MAX_BULK_HEARTBEATS — reference controller constant.
const maxBulkHeartbeats = 50

// Feature is the core.Feature registration (the instance-level opt-in flag
// is resolved per request by the handlers — OlliTeX default OFF per the
// 2026-09-29 owner directive: "opt-in only, off-by-default").
func Feature(a *core.App) core.Feature {
	return core.Feature{Name: "wakatime", Routes: newSvc(a).Routes()}
}

// Routes — the route table on this svc instance (tests route against the
// SAME instance they seed seams on).
func (s *svc) Routes() []core.Route {
	return []core.Route{
		{Method: "GET", Pattern: statusPat, Handler: s.status, NoLogin: false},
		{Method: "GET", Pattern: userSummaryPat, Handler: s.userSummaryHandler, NoLogin: false},
		{Method: "PUT", Pattern: userPat, Handler: s.link, NoLogin: false},
		{Method: "DELETE", Pattern: userPat, Handler: s.unlink, NoLogin: false},
		// Candidate F (owner report 2026-10-07): the tracker is a fire-and-forget
		// fetch that deliberately sends NO CSRF token (raw fetch, credentials
		// same-origin — reference PR#249 design). Requiring CSRF here made
		// EVERY heartbeat a 403 "Forbidden" (console noise on each editor
		// flush). These are login-gated + project-read-gated, and the payload
		// only relays the caller's own usage to the caller's own WakaTime
		// account (no attacker benefit), so NoCSRF is the correct parity.
		{Method: "POST", Pattern: heartbeatPat, Handler: s.heartbeat, NoLogin: false, NoCSRF: true},
		{Method: "POST", Pattern: bulkPat, Handler: s.heartbeatBulk, NoLogin: false, NoCSRF: true},
		{Method: "GET", Pattern: summaryPat, Handler: s.summary, NoLogin: false},
		// G (owner 2026-10-09): admin surface (requireLogin + site admin
		// inside the handlers — the RequireSiteAdmin mirror lives there so
		// non-members get the Node-parity restricted bounce).
		{Method: "POST", Pattern: wakaCheckPat, Handler: s.adminCheck, NoLogin: false},
		{Method: "POST", Pattern: wakaProvisionPat, Handler: s.adminProvision, NoLogin: false},
	}
}

// ErrWaka carries the mapped WakaTime API error (status + message).
type ErrWaka struct {
	Status  int
	Message string
}

func (e *ErrWaka) Error() string { return e.Message }

func errWaka(status int, msg string) *ErrWaka {
	return &ErrWaka{Status: status, Message: msg}
}

var (
	errNotLinked = errWaka(http.StatusBadRequest, "WakaTime not linked")
	errTimeout   = errWaka(http.StatusGatewayTimeout, "WakaTime request timed out")
	errForbidden = errWaka(http.StatusForbidden, "Access denied")
	errNotFound  = errWaka(http.StatusNotFound, "Not found")
	errTooMany   = errWaka(http.StatusTooManyRequests, "Rate limit exceeded")
	errBadAPI    = errWaka(http.StatusInternalServerError, "Something wrong with WakaTime request")
	errUpstream  = errWaka(http.StatusInternalServerError, "WakaTime request error")

	// audit 034-N4 (LOW, dead-shape cleanup): both sentinels intentionally
	// collapse to 500 at the response boundary (Node oracle: the
	// controller catch-all renders every non-mapped failure as 500). The
	// sentinels stay DISTINCT in-process: errBadAPI = the request itself
	// was bad (undialable URL shape / marshal failure), errUpstream = the
	// upstream connection existed but the response read failed. Tests and
	// logging rely on that distinction; the HTTP surface parity does not.

	// audit 034-N1: endpoint policy rejects (user-supplied API URL failed
	// the instance policy — shape, host lockdown, or private-IP rejection).
	// 400 shape: the REQUEST is what's bad, not the upstream.
	errInvalidURL  = errWaka(http.StatusBadRequest, "Invalid WakaTime API URL")
	errHostBlocked = errWaka(http.StatusBadRequest, "WakaTime API host is not allowed on this instance")
)

func writeWakaError(res *core.Res, err error) {
	var e *ErrWaka
	if ok := asErrWaka(err, &e); !ok {
		e = errWaka(http.StatusInternalServerError, err.Error())
	}
	res.JSON(e.Status, []byte(`{"message":`+qstr(e.Message)+`}`))
}

func asErrWaka(err error, e **ErrWaka) bool {
	pe, ok := err.(*ErrWaka)
	if !ok {
		return false
	}
	*e = pe
	return true
}
