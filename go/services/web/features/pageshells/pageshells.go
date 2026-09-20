// Package pageshells — Go parity implementation of the Node
// services/web/modules/page-shells module (PSH, UI-R10 W8; WEB_GO_PLAN
// P6.18). The legacy shell pages were removed (the hubs are the single
// settings surfaces — owner queue 4, 2026-09-10 / late item 2026-09-12)
// and the routes are pure 301 redirects:
//
//	GET /user/mysettings  (requireLogin)           → 301 /hub#/mysettings.account
//	GET /admin/panel      (ensureUserIsSiteAdmin)  → 301 /hub#/overview
//	                                              non-admin → 302 /restricted?from=%2Fadmin%2Fpanel
//
// The core chain supplies the anonymous behavior (302 /login, 401 for
// JSON accept; POST/PUT/PATCH/DELETE → 403 Forbidden via the CSRF gate,
// all pinned P1/P3.1 and re-verified live for these exact paths) and the
// core RequireSiteAdmin supplies the non-admin bounce (P3.1 pin,
// /admin/editor-state precedent). res.Redirect mirrors express
// res.redirect's Accept negotiation (html → "<p>…</p>" text/html,
// text/*|*/* → plain text/plain, JSON/form-only accept → empty body with
// no Content-Type, Vary: Accept; no X-Powered-By, no ETag — pinned
// 2026-09-19).
//
// Path variants (case differences, trailing slashes, double slashes) are
// NOT served by Go: the flip conf uses nginx exact-match locations, so
// the variants fall through to Node, which answers them itself (parity
// by construction — express is case-insensitive and slash-tolerant there).
// Subpaths (/user/mysettingsx, /user/mysettings/extra) 404 on both
// (Node 404 page — verified live).
package pageshells

import (
	"ollitex/go/services/web/core"
)

func Feature(a *core.App) core.Feature {
	return core.Feature{
		Name: "pageshells",
		Routes: []core.Route{
			{
				Method: "GET",
				Path:   "/user/mysettings",
				Handler: func(cxt *core.Cxt, res *core.Res) {
					// Node PageShellsRouter (2026-09-12 owner late item):
					// res.redirect(301, '/hub#/mysettings.account').
					res.Redirect(cxt.Req, 301, "/hub#/mysettings.account")
				},
			},
			{
				Method: "GET",
				Path:   "/admin/panel",
				Handler: func(cxt *core.Cxt, res *core.Res) {
					// Node PageShellsRouter: ensureUserIsSiteAdmin then
					// res.redirect(301, '/hub#/overview').
					if !a.RequireSiteAdmin(cxt, res) {
						return
					}
					res.Redirect(cxt.Req, 301, "/hub#/overview")
				},
			},
		},
	}
}
