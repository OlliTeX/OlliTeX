// Package devcsrf implements GET /dev/csrf (router.mjs:1239):
//
//	webRouter.get('/dev/csrf', (req, res) => {
//	  plainTextResponse(res, res.locals.csrfToken)
//	})
//
// — i.e. a fresh token derived from the session's csrfSecret
// (res.locals.csrfToken = req.csrfToken() in ExpressLocals.mjs:294;
// csurf lazy-init: the secret is allocated into the session on first use
// and stored as session.csrfSecret).
//
// The route is NOT on the anonymous whitelist (registered after the
// webRouter.all('*', requireGlobalLogin) at router.mjs:215) → anonymous
// callers get the 302 → /login bounce; a logged-in caller gets the
// token as text.
//
// This endpoint is the session/CSRF interop oracle of the P0 gate:
// both stacks derive the token from the SAME session document, so a
// Node-created token verifies on Go and vice versa.
package devcsrf

import "ollitex/go/services/web/core"

func handler(cxt *core.Cxt, res *core.Res) {
	token := cxt.Sess.CsrfToken()
	res.PlainText(200, token)
}

var Feature = core.Feature{
	Name: "devcsrf",
	Routes: []core.Route{
		{Method: "GET", Path: "/dev/csrf", NoLogin: false, Handler: handler},
	},
}
