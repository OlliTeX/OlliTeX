// Package tokenaccess ports the CE token access + link-sharing consent
// surface (P2):
//
//	GET  /<rw-token>                      (token page, legacy view)
//	GET  /read/<ro-token>                 (token page, legacy view)
//	POST /<rw-token>/grant                (rate-limited rw grant)
//	POST /read/<ro-token>/grant           (rate-limited ro grant)
//	GET  /project/<id>/sharing-updates   (consent page)
//	POST /project/<id>/sharing-updates/join  (move rw→editor, 204)
//	POST /project/<id>/sharing-updates/view  (move rw→readOnly, 204)
//
// Node contract pins (source + live, 2026-09-14):
//
//   - token shapes: rw = ^[0-9]+[a-z]{6,12}$, ro = ^[a-z]{12}$
//   - project lookup (fork): rw → tokens.readAndWritePrefix (+
//     constantTimeEqual on tokens.readAndWrite), ro → tokens.readOnly
//   - token gate: publicAccesLevel === 'tokenBased'
//   - anonymous rw grant (CE disabled): session.postLoginRedirect=
//     '/restricted', body {redirect:'/restricted', anonWriteAccessDenied:true}
//   - anonymous ro grant (enabled): session.anonTokenAccess[pid]=token,
//     body {redirect:'/project/<pid>', grantAnonymousAccess:'readOnly'}
//   - higher-privilege shortcut: {redirect:'/project/<pid>',
//     higherAccess:true}
//   - unconfirmed: {requireAccept:{projectName}}
//   - confirmed rw: refs check → audit 'join-via-token' {privileges:
//     'readAndWrite'} → $addToSet tokenAccessReadAndWrite_refs →
//     {redirect:'/project/<pid>'}
//   - confirmed ro: refs check → audit 'join-via-token' {privileges:
//     'readOnly'} → $addToSet tokenAccessReadOnly_refs →
//     {redirect:'/project/<pid>', tokenAccessGranted:'readOnly'}
//   - grant limiters: 'grant-token-access-read-write' / '…-read-only',
//     10 points / 60s, key rate-limit:<name>:<ip>
//   - consent page gate: requireLogin → ensureUserCanReadProject
//     (no access → 403 Restricted view) → rw-token-member (else {redir}
//     JSON / 302) → not invited member (else same redirect)
//   - join: audit 'accept-via-link-sharing' {privileges:'readAndWrite',
//     tokenMember:true, invitedMember:<bool>} + token-ref pull +
//     membership grant → 204
//   - view: audit 'readonly-via-sharing-updates' + ($pull rw refs,
//     $addToSet ro refs) → 204

package tokenaccess

import (
	"encoding/json"
	"io"
	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
	"regexp"
)

var (
	rwPagePat  = regexp.MustCompile(`^/([0-9]+[a-z]{6,12})$`)
	roPagePat  = regexp.MustCompile(`^/read/([a-z]{12})$`)
	rwGrantPat = regexp.MustCompile(`^/([0-9]+[a-z]{6,12})/grant$`)
	roGrantPat = regexp.MustCompile(`^/read/([a-z]{12})/grant$`)
	// group 1 = project id (page / join / view each carry the same group 1)
	sharePagePat = regexp.MustCompile(`^/project/([0-9a-f]{24})/sharing-updates$`)
	shareJoinPat = regexp.MustCompile(`^/project/([0-9a-f]{24})/sharing-updates/join$`)
	shareViewPat = regexp.MustCompile(`^/project/([0-9a-f]{24})/sharing-updates/view$`)
)

func Feature(a *core.App) core.Feature {
	rwLim := core.NewRateLimiter(a.Redis, "grant-token-access-read-write", 10, 60)
	roLim := core.NewRateLimiter(a.Redis, "grant-token-access-read-only", 10, 60)

	limit := func(l *core.RateLimiter, fn func(cxt *core.Cxt, res *core.Res)) func(*core.Cxt, *core.Res) {
		return func(cxt *core.Cxt, res *core.Res) {
			if l != nil && !l.Consume(core.ClientIP(cxt.Req)) {
				core.Send429(res, "Rate limit reached, please try again later")
				return
			}
			fn(cxt, res)
		}
	}

	page := func(ro bool) func(cxt *core.Cxt, res *core.Res) {
		return func(cxt *core.Cxt, res *core.Res) {
			token := cxt.Params["1"]
			postURL := "/" + token + "/grant"
			if ro {
				postURL = "/read/" + token + "/grant"
			}
			views.TokenAccessPage(res.W, pageBase(cxt, func(d *views.PageData) {
				d.PostURL = postURL
			}))
		}
	}

	return core.Feature{
		Name: "tokenaccess",
		Routes: []core.Route{
			{Method: "GET", Pattern: rwPagePat, Handler: page(false)},
			{Method: "GET", Pattern: roPagePat, Handler: page(true)},
			{Method: "POST", Pattern: rwGrantPat, Handler: limit(rwLim, grant(a, true))},
			{Method: "POST", Pattern: roGrantPat, Handler: limit(roLim, grant(a, false))},
			{Method: "GET", Pattern: sharePagePat, Handler: consent(a, "page")},
			{Method: "POST", Pattern: shareJoinPat, Handler: consent(a, "join")},
			{Method: "POST", Pattern: shareViewPat, Handler: consent(a, "view")},
		},
	}
}

// pageBase builds the shared PageData (session slots + origin).

// pageBase builds the shared PageData (session slots + origin).
func pageBase(cxt *core.Cxt, tweak func(*views.PageData)) views.PageData {
	d := views.PageData{Nonce: views.NewNonce()}
	origin := cxt.SiteURL
	if origin == "" {
		origin = "http://" + cxt.Req.Host
	}
	d.Origin = origin
	if cxt.Sess != nil {
		d.CSRFToken = cxt.Sess.CsrfToken()
		d.UserEmail, d.UserID = core.PageUserSlots(cxt.Sess)
	}
	if tweak != nil {
		tweak(&d)
	}
	return d
}

// ---------- project helpers (shared with P3/P4 authorization) ----------

// ---------- checkAndGet (CE parity) ----------

const (
	actNone         = ""
	actAnonRoGrant  = "anonRoGrant"
	actAnonRwDenied = "anonRwDenied"
	actHigherAccess = "higherAccess"
)

func respondJSON(res *core.Res, code int, body string) {
	res.W.Header().Set("Content-Type", "application/json; charset=utf-8")
	res.W.WriteHeader(code)
	_, _ = io.WriteString(res.W, body)
}

func write404(res *core.Res) {
	respondJSON(res, 404, `{"message":"Not found."}`)
}

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
