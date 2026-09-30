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
		{Method: "PUT", Pattern: userPat, Handler: s.link, NoLogin: false},
		{Method: "DELETE", Pattern: userPat, Handler: s.unlink, NoLogin: false},
		{Method: "POST", Pattern: heartbeatPat, Handler: s.heartbeat, NoLogin: false},
		{Method: "POST", Pattern: bulkPat, Handler: s.heartbeatBulk, NoLogin: false},
		{Method: "GET", Pattern: summaryPat, Handler: s.summary, NoLogin: false},
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
