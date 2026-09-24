package core

import (
	"net/http"
	"os"
	"strconv"
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

// APIBasicGate401 — the api-profile (ENABLED_SERVICES=api) private-API gate.
// Distinct from the web gate (APIBasicGate): the api process never
// content-negotiates an UNAUTHENTICATED request into a 302/login redirect or a
// csrf 403 — it always answers 401, for any Accept and any method. Pinned live
// 2026-09-23 against Node api :3000 (doc-trio GET json+html, no-auth POST all
// → 401 "Unauthorized") with full header capture. The api 401 carries:
//
//	WWW-Authenticate: OverleafLogin, X-Powered-By: Express,
//	Content-Security-Policy <fixed api policy> (set globally in serve()),
//	Content-Type text/plain; charset=utf-8, weak ETag over "Unauthorized",
//	Content-Length 12 — and NONE of the web helmet baseline and NO session
//	cookie (the api profile mounts no session middleware).
//
// Right credentials → true (the handler runs: GET → 200/404, POST → the
// setDocument/reject logic). Wrong/absent credentials → false (401 sent).
func (a *App) APIBasicGate401(cxt *Cxt, res *Res, req *http.Request) bool {
	send401 := func() {
		res.W.Header().Set("WWW-Authenticate", "OverleafLogin")
		res.W.Header().Set("X-Powered-By", "Express")
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
		send401()
		return false
	}
	send401()
	return false
}

// SendRestricted403JSON — the WEB-profile /project/:id (editorPage) restricted
// 403 for a request that accepts JSON (Node AuthorizationMiddleware restricted
// content-negotiation): 24B `{"message":"restricted"}` + the fixed CSP
// (CSPDefaultPolicy) + the web helmet baseline (incl. nosniff; the no-cache set
// applies because noCacheFor matches the project page) + the weak ETag over the
// body (W/"18-WWEMZJpglINrlZwTEutmkjASLhM"). Returns false (write nothing) when
// the request does NOT accept JSON, so the caller falls back to the "Restricted"
// 403 page (views.Restricted403).
func (a *App) SendRestricted403JSON(res *Res, req *http.Request) bool {
	if !a.acceptsJSON(req) {
		return false
	}
	body := []byte(`{"message":"restricted"}`)
	res.W.Header().Set("Content-Type", "application/json; charset=utf-8")
	res.W.Header().Set("Content-Security-Policy", CSPDefaultPolicy)
	a.setWebBaseline(res.W, req, false)
	res.W.Header().Set("Content-Length", strconv.Itoa(len(body)))
	res.W.Header().Set("ETag", EtagWeakBody(string(body)))
	res.W.WriteHeader(403)
	_, _ = res.W.Write(body)
	return true
}

// basicAuthValid — pure (no write) private-API basic-credential check, the
// same comparison APIBasicGate401 uses: req carries a valid WEB_API_USER /
// WEB_API_PASSWORD Basic cred. Used by App.webAuthed to decide, for a
// web-profile request, whether a present Authorization header authenticates it
// (Node requireGlobalLogin: Authorization present → basic decision rules).
func (a *App) basicAuthValid(req *http.Request) bool {
	if req == nil {
		return false
	}
	au, p, has := req.BasicAuth()
	if !has {
		return false
	}
	eu, ep := privateAPICreds()
	return eu != "" && au == eu && p == ep
}
