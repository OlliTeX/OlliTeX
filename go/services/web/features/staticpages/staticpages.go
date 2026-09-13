// Package staticpages ports the NonCE StaticPages surface (P1 wave):
//
//	GET /            → logged-in: 302 /hub (owner 2026-09 #34); else 302 /login
//	GET /learn* etc. → 301 https://www.overleaf.com<originalUrl> (CE marketing
//	                    passthrough; pinned)
//
// The external home/about/privacy views are ABSENT in this build (the pug
// files do not ship), so Node's anonymous '/' already 302s to /login — the
// parity surface is exactly the two redirects above.
package staticpages

import (
	"net/http"

	"ollitex/go/services/web/core"
)

const marketingBase = "https://www.overleaf.com"

func Feature(a *core.App) core.Feature {
	return core.Feature{
		Name: "staticpages",
		Routes: []core.Route{
			{Method: "GET", Path: "/", NoLogin: true, Handler: home(a)},
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
