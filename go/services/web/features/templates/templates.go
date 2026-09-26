// Package templates — the template gallery surface of the Node web
// (services/web/modules/template-gallery), oracle-pinned 2026-09-18
// (P6.13) on the e2e stack:
//
//	redirects (logged in) : /templates  /templates/  /templates/:category
//	                        /template/:id        → 301 /hub#/templates.all
//	                        /templates/manage    → 301 /hub#/site.general.managetpl
//	public JSON (logged in): GET /api/template?key=_id|name&val=… (doc | null)
//	                         GET /api/template/categories           (seed list)
//	                         GET /api/templates?category=…&by=…&order=…
//	assets                : GET /template/:id/preview?version=…&style=preview|thumbnail
//	                        GET /template/:id/bundle (template.json + source.zip [+ output.pdf])
//	management (403 in this profile: no site admin and no user carries
//	flags.canManageTemplates; the gate pins it) :
//	                        POST  /template/new/:Project_id
//	                        POST  /template/:id/edit
//	                        DELETE /template/:id/delete
//	                        POST  /template/bundle/import
//	                        POST  /template/bundle/import-url
//	                        GET   /api/templates/admin-list
//
// Profile facts (pinned live):
//   - OVERLEAF_TEMPLATE_GALLERY=true (env seed → gallery ENABLED; the
//     stored site_settings doc has no `templates` section). boolFromEnv
//     semantics: 'true'→true, 'false'→false, else undefined→seed false
//     → `res.status(404).send('Not Found')` on the gallery-gated routes.
//   - Rate limiters (create-template-from-project 20/60, template-gallery
//     60/60, template-gallery-thumbnails 240/60) are NO-OPS here:
//     OVERLEAF_DISABLE_RATE_LIMITS=true → Settings.disableRateLimits.
//     Go therefore registers no limiter (editorpages / P6.12 precedent);
//     a future enabled profile must count only authz-passing requests
//     (Node wires the limiter AFTER its authz middleware).
//   - hasTemplateAdminAccess (site admin | OVERLEAF_TEMPLATES_USER_ID |
//     flags.canManageTemplates | section.allUsersCanManageTemplates) can
//     hold for NO user in this profile → every management route answers
//     403 {"message":"restricted"} (json) / the restricted view (html).
//     The manager WRITE flows behind those routes (create / edit / delete
//     / import + bundle validation) are the documented follow-up if
//     management rights are ever granted (P6.11 routing-only precedent).
package templates

import (
	"regexp"

	"ollitex/go/services/web/core"
)

var tplHex24 = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

var tplOidHexLower = regexp.MustCompile(`^[0-9a-f]{24}$`)

var _ = tplOidHexLower

// Feature registers the template-gallery routes (Node router order —
// /templates/manage before the generic /templates/:category? so the
// admin slug never shadows the hub redirect; the [^/]+ segment capture
// never matches an empty or multi-segment remainder, pinned 404).
func Feature(a *core.App) core.Feature {
	anchor := func(s string) *regexp.Regexp { return regexp.MustCompile(`^` + s + `$`) }
	return core.Feature{Name: "template-gallery", Routes: []core.Route{
		{Method: "POST", Pattern: anchor(`/template/new/([^/]+)`), Handler: hCreateNew(a)},
		{Method: "GET", Pattern: anchor(`/template/([^/]+)`), Handler: hRedirect(`/hub#/templates.all`)}, {Method: "GET", Path: "/api/templates/admin-list", Handler: hAdminList(a)},
		{Method: "GET", Pattern: anchor(`/template/([^/]+)/bundle`), Handler: hBundle(a)},
		{Method: "POST", Path: "/template/bundle/import", Handler: hImport(a)},
		{Method: "POST", Path: "/template/bundle/import-url", Handler: hImportUrl(a)},
		{Method: "GET", Path: "/templates/manage", Handler: hRedirect(`/hub#/site.general.managetpl`)},
		{Method: "POST", Pattern: anchor(`/template/([^/]+)/edit`), Handler: hEdit(a)},
		{Method: "DELETE", Pattern: anchor(`/template/([^/]+)/delete`), Handler: hDelete(a)},
		// Node: webRouter.get('/templates/:category?') — matches /templates,
		// /templates/ and /templates/<one-segment>; /templates//x and
		// /templates/a/b do NOT match (pinned 404 page); /templates/manage
		// (registered above) wins first (pinned 301 hub admin leaf).
		{Method: "GET", Pattern: anchor(`/templates(?:/[^/]*)?`), Handler: hRedirect(`/hub#/templates.all`)},
		{Method: "GET", Path: "/api/template", Handler: hGetTemplate(a)},
		{Method: "GET", Path: "/api/template/categories", Handler: hCategories(a)},
		{Method: "GET", Path: "/api/templates", Handler: hList(a)},
		{Method: "GET", Pattern: anchor(`/template/([^/]+)/preview`), Handler: hPreview(a)},
	}}
}
