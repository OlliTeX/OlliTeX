package core

import (
	"net/http"
	"os"
)

// Private-API (basic-auth) trio primitives — U10.3r (2026-09-23, pinned live
// against the node web profile ol-e2e-overleaf-1 :4000 with full header
// capture).
//
// Node's WEB stack runs express-session + the full helmet baseline on the
// privateApiRouter routes as well: every gate response (401, 302) and the
// POST-block 403 carries a FRESH overleaf.sid, and redis gets a sess:<sid>
// doc (cookie + csrfSecret + validationToken — verified live).
//
//	Wire matrix (both GET routes — doc GET /project/:pid/doc/:did and
//	tag GET /user/:userId/tag; the two POST doc routes):

// privateAPICreds — WEB_API_USER / WEB_API_PASSWORD parity with Node
// Settings.httpAuthUsers for the basic-auth gate.
func privateAPICreds() (user, pass string) {
	return os.Getenv("WEB_API_USER"), os.Getenv("WEB_API_PASSWORD")
}

// NewAPISessionCookie issues the fresh overleaf.sid that Node's session
// middleware attaches to these routes (web profile only; no-op elsewhere).
// It mirrors sess:<sid> creation: a fresh doc whose csrfSecret allocation is
// what makes express-session save+cookie.
func (a *App) NewAPISessionCookie(cxt *Cxt, res *Res) {
	if a == nil || a.Store == nil || a.Cfg.Profile != "web" {
		return
	}
	sess, err := a.Store.StartAnonymous(cxt.Req)
	if err != nil || sess == nil || sess.SessID == "" {
		return
	}
	_ = sess.CsrfSecret() // modification → express-session save-on-write
	a.CommitSess(sess, res.W)
}

// apiGateHelmet — the web-baseline helmet set that Node attaches to the GET
// gate responses. requireGlobalLogin (401/302) is a global middleware that
// runs AFTER Server.mjs's helmet middleware, so it carries the full set.
// The POST 403 (csrf) does NOT — it is emitted BEFORE helmet — so
// APISend403 deliberately omits this. no-cache headers are absent for a
// private-API doc path (noCacheFor is false there, no login) — pinned:
// Node's gate 401/302 carry no Cache-Control/Expires/Pragma/Surrogate-Control.
// APIHelmet — exported form of the private-API web-baseline helmet set
// (see apiGateHelmet). Feature handlers call it on the valid-cred path
// (rendered 404 page / 200 body), which otherwise would ride the NoSession
// route and miss the baseline. POST-block 403 intentionally does NOT use this
// (csrf fires before helmet → no baseline).
func (a *App) APIHelmet(res *Res, req *http.Request) {
	a.setWebBaseline(res.W, req, false)
}

// APISend403 — the POST-block wire. Pinned live: 403 text/plain "Forbidden"
// + X-Powered-By (the route keeps the apiXPB wrap) + default CSP + a fresh
// overleaf.sid, and NONE of the helmet set (csrf fires before helmet).
func (a *App) APISend403(cxt *Cxt, res *Res) {
	a.NewAPISessionCookie(cxt, res)
	res.W.Header().Set("Content-Type", "text/plain; charset=utf-8")
	res.W.Header().Set("Content-Length", "9")
	res.W.Header().Set("ETag", EtagWeakBody("Forbidden"))
	res.W.WriteHeader(403)
	_, _ = res.W.Write([]byte("Forbidden"))
}

// Wire matrix (both GET routes — doc GET /project/:pid/doc/:did and
// tag GET /user/:userId/tag; the two POST doc routes). Pinned live
// 2026-09-23 on the web profile with full header capture:
//
//	any state + POST                → 403 text/plain "Forbidden" (X-Powered-By
//	                                + CSP + fresh sid, NO helmet set) — Node's
//	                                csrf chain blocks before basic auth.
//	unauth + Accept→json            → 401 "Unauthorized" + WWW-Authenticate:
//	                                OverleafLogin + FULL helmet + fresh sid,
//	                                NO X-Powered-By.
//	unauth + Accept→html/none       → 302 Location /login + helmet + fresh sid
//	                                + Vary: Accept + negotiated body, NO XPB.
//	wrong basic (ANY Accept)        → 401 (requireBasic failure never
//	                                content-negotiates: html accept → 401 too).
//	right basic                     → handler runs (the valid-cred wire is
//	                                sandbox-interceptor-blocked in e2e — a
//	                                network guard drops any request whose
//	                                Authorization carries the WEB_API password;
//	                                covered by the Go unit test instead).
//
// Login-state note (pinned): a no-Authorization request behaves identically
// whether or not a logged-in session cookie is present (json→401, html→302),
// so the gate branches on Authorization presence, not login state.
func (a *App) APIBasicGate(cxt *Cxt, res *Res, req *http.Request) bool {
	send401 := func() {
		a.NewAPISessionCookie(cxt, res)
		a.APIHelmet(res, req)
		res.W.Header().Set("WWW-Authenticate", "OverleafLogin")
		res.W.Header().Set("Content-Type", "text/plain; charset=utf-8")
		res.W.Header().Set("Content-Length", "12")
		res.W.Header().Set("ETag", EtagWeakBody("Unauthorized"))
		res.W.WriteHeader(401)
		_, _ = res.W.Write([]byte("Unauthorized"))
	}
	if au, p, has := req.BasicAuth(); has {
		eu, ep := privateAPICreds()
		if eu != "" && au == eu && p == ep {
			return true
		}
		send401() // wrong basic → unconditional 401 (no Accept negotiation)
		return false
	}
	if AcceptsJSON(req) {
		send401()
		return false
	}
	a.NewAPISessionCookie(cxt, res)
	a.APIHelmet(res, req)
	res.Redirect(req, 302, "/login")
	return false
}
