// Package authpages ports the Node web authentication surface (P1/P2 wave).
// Contract pins are cited per route against the live Node service (e2e,
// 2026-09-13); the e2e gate re-proves them on every flip.

package authpages

import (
	"net/http"
	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

// Feature wires the auth routes.
func Feature(a *core.App) core.Feature {
	return core.Feature{
		Name: "authpages",
		Routes: []core.Route{
			{Method: "GET", Path: "/login", NoLogin: true, Handler: pageHandler(views.LoginPage)},
			{Method: "POST", Path: "/login", NoLogin: true, Handler: postLogin(a)},
			// GET /register → registrationpage feature (P3.4, P3.3-era move).
			{Method: "GET", Path: "/logout", Handler: getLogoutPage},
			{Method: "POST", Path: "/logout", Handler: postLogout(a)},
			{Method: "GET", Path: "/restricted", Handler: pageHandler(views.RestrictedPage)},
		},
	}
}

// ---- view pages ----

// ---- view pages ----

func pageData(cxt *core.Cxt) views.PageData {
	tok := ""
	if cxt.Sess != nil {
		tok = cxt.Sess.CsrfToken()
	}
	// Views' origin (alternate link, footer URLs, siteUrl) is the
	// configured site URL — Node reads settings.siteUrl, not the
	// per-request Host (pinned: via nginx Node renders :7420 even when
	// the proxied Host is portless).
	origin := cxt.SiteURL
	if origin == "" {
		origin = originOfReq(cxt)
	}
	return views.PageData{CSRFToken: tok, Nonce: views.NewNonce(), Origin: origin}
}

func pageHandler(f func(http.ResponseWriter, views.PageData)) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) { f(res.W, pageData(cxt)) }
}

func getLogoutPage(cxt *core.Cxt, res *core.Res) {
	// Node UserPagesController.logout: logged-in → confirmation page, else
	// 302 '/'. (The global gate bounces anonymous first, so the else branch
	// is the session-expired edge.)
	if cxt.Sess != nil && cxt.Sess.IsLoggedIn() {
		views.LogoutPage(res.W, pageData(cxt))
		return
	}
	res.Redirect(cxt.Req, 302, "/")
}

// POST /logout — Node UserController.logout:
// passport.logout + session.destroy (DEL doc) → 302 (body.redirect || "/login").
// Pinned: 302 Location /login, "Found. Redirecting to /login", old cookie dead.

const (
	bodyInvalidPassword    = `{"message":{"type":"error","key":"invalid-password-retry-or-reset"}}`
	bodyInvalidEmailNested = `{"message":{"message":"This SSO login option is not enabled.","type":"error","key":"invalid-password-retry-or-reset"}}`
)
