// Package staticpages ports the NonCE StaticPages surface (P1 wave):
//
//	GET /            → logged-in: 302 /hub (owner 2026-09 #34); else 302 /login
//	GET /home        → logged-in: 302 /login (HomeController.home — the
//	                   external home pug is absent in this build; Node oracle
//	                   2026-09-22: 302 "Found. Redirecting to /login"),
//	                   anonymous: global gate (401 accept-json / 302 html)
//	GET /learn* etc. → 301 https://www.overleaf.com<originalUrl> (CE marketing
//	                    passthrough; pinned)
//
// The external home/about/privacy views are ABSENT in this build (the pug
// files do not ship), so Node's anonymous '/' already 302s to /login — the
// parity surface is exactly the two redirects above.
package staticpages

import (
	"net/http"
	"regexp"
	"strings"

	"ollitex/go/services/web/core"
)

const marketingBase = "https://www.overleaf.com"

// homeRe — /home (case-insensitive + Express trailing-slash tolerant; U9 oracle).
var homeRe = regexp.MustCompile(`^/(?i:home)/?$`)

// uniPageRe — /university/<anything> (Node UniversityController.getPage,
// U10.2): 302 to /i/university/<segment-lowercased, first ".html" stripped>.
// Express route paths are case-insensitive + trailing-slash tolerant.
var uniPageRe = regexp.MustCompile(`^/(?i:university)/(.+?)/?$`)

func Feature(a *core.App) core.Feature {
	return core.Feature{
		Name: "staticpages",
		Routes: []core.Route{
			{
				// U9: Node's '/' is NOT on the global-login whitelist — anonymous
				// JSON gets the gate 401 (live-pinned 2026-09-22: N anon /
				// Accept:application/json -> 401 Unauthorized; the Go NoLogin
				// branch wrongly 302ed the JSON caller), anonymous HTML gets the
				// gate 302 /login; logged-in passes the gate into home() -> 302
				// /hub (owner 2026-09 #34).
				Method:  "GET",
				Path:    "/",
				Handler: home(a),
			},
			// U9 (Node oracle 2026-09-22): /home = HomeController.home — the
			// external home pug does NOT ship in this build, so logged-in
			// requests 302 to /login. Not on the global-login whitelist:
			// anonymous hits the gate first (401 accept-json / 302 html). Case-
			// insensitive (Express default).
			{Method: "GET", Pattern: homeRe, Handler: homeToLogin},
			// U10.2 — StaticPagesRouter UniversityController:
			//   GET /university      -> 302 /i/university      (getIndexPage)
			//   GET /university/<x>  -> 302 /i/university/<x'> (getPage)
			// x' = x lowercased with the FIRST ".html" occurrence stripped
			// (req.url.toLowerCase().replace('.html','')). Login-gated like
			// every webRouter route (anonymous → the global gate, pinned U10.2).
			{Method: "GET", Path: "/university", Handler: universityIndex},
			{Method: "GET", Pattern: uniPageRe, Handler: universityPage},
			// LOGIN-REQUIRED (pinned: the gate runs before these — anonymous
			// /learn → 302 /login; logged-in → the 301 below).
			{Method: "GET", Path: "/learn", Handler: marketingRedirect},
			{Method: "GET", Path: "/blog", Handler: marketingRedirect},
			{Method: "GET", Path: "/latex", Handler: marketingRedirect},
			{Method: "GET", Path: "/contact", Handler: marketingRedirect},
		},
	}
}

// home — Node HomeController.index: logged-in users go to /hub (owner
// decision 2026-09-13, hub issues #34); anonymous to the (absent) homepage
// → /login.
func home(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if cxt.Sess != nil && cxt.Sess.IsLoggedIn() {
			res.Redirect(cxt.Req, 302, "/hub")
			return
		}
		res.Redirect(cxt.Req, 302, "/login")
	}
}

// homeToLogin — Node HomeController.home, CE branch (homepage feature off /
// pug absent): res.redirect('/login'). Logged-in only reach it (the global
// gate bounces anonymous first — pinned in the U9 gate battery).
func homeToLogin(cxt *core.Cxt, res *core.Res) {
	res.Redirect(cxt.Req, 302, "/login")
}

// marketingRedirect — router.mjs:1370 passthrough (/learn*, /blog*, /latex*,
// /for/*, /contact* → 301 www.overleaf.com + verbatim originalUrl).
func marketingRedirect(cxt *core.Cxt, res *core.Res) {
	r := cxt.Req
	// Node's `req.originalUrl` = the path as addressed (including any prefix
	// nginx preserved — under the flip the path is unprefixed).
	orig := r.URL.Path
	if r.URL.RawQuery != "" {
		orig += "?" + r.URL.RawQuery
	}
	res.W.Header().Set("Location", marketingBase+orig)
	res.W.WriteHeader(301)
	_, _ = res.W.Write([]byte("Moved Permanently. Redirecting to " + marketingBase + orig))
}

var _ = http.StatusOK

// universityIndex — Node UniversityController.getIndexPage:
// res.redirect('/i/university').
func universityIndex(cxt *core.Cxt, res *core.Res) {
	res.Redirect(cxt.Req, 302, "/i/university")
}

// universityPage — Node UniversityController.getPage:
//
//	url = req.url.toLowerCase().replace('.html', '')
//	res.redirect('/i' + url)
//
// req.url is the full request path (express hands the router the sub-path
// relative to the router mount; these routes are mounted at the app root,
// so req.url == "/university/<seg>" here — pinned by the live oracle:
// /university/Foo → /i/university/foo, /university/a.html → /i/university/a).
// Express matches case-insensitively, so the Go pattern is (?i).
func universityPage(cxt *core.Cxt, res *core.Res) {
	raw := strings.TrimSuffix(cxt.Params["1"], "/")
	// Node: req.url.toLowerCase() then replace the FIRST '.html'.
	// req.url includes the '/university' prefix; Express matches the path
	// case-insensitively and strips the trailing slash before the handler,
	// so the raw param may carry a trailing '/' that req.url would not.
	seg := strings.ToLower("/university/" + raw)
	if i := strings.Index(seg, ".html"); i >= 0 {
		seg = seg[:i] + seg[i+len(".html"):]
	}
	res.Redirect(cxt.Req, 302, "/i"+seg)
}
