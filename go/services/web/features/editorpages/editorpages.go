// Package editorpages ports the editor PAGE routes (P5.1a flip unit):
//
//	GET /editor/:Project_id     (ProjectController.loadEditor)
//	GET /Project/:Project_id     (legacy prefix, same controller)
//
// U2 (2026-09-22): Node/Express routing is case-INsensitive, so BOTH
// prefixes — /editor and /project — in ANY case, plus the project id in
// either hex case, all drive the SAME handler (pinned live); a non-empty
// invalid id answers Node's exact 404 JSON instead (`editorBadId`), while
// the empty-id forms keep their split behaviour (Node truth): /editor/
// → generic 404 page, /Project/ → the dashboard 301 (projectlist).
//
// Node ground truth: services/web/app/src/Features/Project/ProjectController
// .mjs (loadEditor) + views chain layout-base → layout-react → ide-react +
// project/editor/_meta.pug. The page is a single ~35 kB HTML line whose
// per-request surface is (a) the per-request CSP nonce, (b) the session
// csrf token, (c) the project-name title, (d) the request-specific
// ol-navbar.currentUrl, and (e) 68 <meta name="ol-…"> bootstrap slots the
// React client reads before it boots.
//
// Contracts pinned live 2026-09-15 (P5.1a oracle: /tmp/p51/owner-editor.
// html): 200 text/html; charset=utf-8, ~35.4 kB, editor CSP
// (script-src nonce + strict-dynamic + img-src 'self' data: blob:), weak
// ETag, the full helmet baseline (core.setWebBaseline). The Playwright
// gate (web-go-p51a-flip) is the authority.
//
// Deliberately deferred to P5.1b (different template: ide-react-detached):
//	  GET /editor/:Project_id/:detachRole(detacher|detached)
//	  GET /Project/:Project_id/:detachRole(detacher|detached)
//
// Rate limiting: Node wraps these routes in openProjectRateLimiter
// (15pts/60s, keyed by Project_id); WITH the e2e OVERLEAF_DISABLE_RATE_
// LIMITS=true it is a no-op, so Go registers NO limiter (parity for this
// stack; a rate-limited production profile would need the limiter added).

package editorpages

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/features/sitesettings"
	"ollitex/go/services/web/features/templates"
	"ollitex/go/services/web/views"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// editorPagePattern — both main-shell prefixes (Node/Express routing is
// case-INsensitive: /editor|/project in ANY case) + a 24-hex project id in
// either hex case; optional detach suffix (P5.1b).
var editorPagePattern = regexp.MustCompile(`^/(?i:editor|project)/(?P<id>[0-9a-fA-F]{24})(?P<role>/detacher|/detached)?$`)

// editorBadIdPattern — the same route family with a NON-empty, non-valid
// ObjectId (Node: loadEditorSchema logs the fallback, the objectId param
// validator 404s: {"error":"Validation error: Invalid Mongo ObjectId at
// \"params.Project_id\"","statusCode":404}, application/json — pinned
// live 2026-09-22 U2). Empty id (/editor/ or /Project/) does NOT hit this:
// /editor/ falls through to the generic 404 page (Node truth), /Project/
// matches the dashboard 301 (projectlist) — both pinned in the U2 gate.
var editorBadIdPattern = regexp.MustCompile(`^/(?i:editor|project)/(?P<id>[^/]+)(?P<role>/detacher|/detached)?$`)

// Feature registers the editor page routes.
func Feature(a *core.App) core.Feature {
	return core.Feature{
		Name: "editorpages",
		Routes: []core.Route{
			{Method: "GET", Pattern: editorPagePattern, Handler: editorPage(a)},
			{Method: "GET", Pattern: editorBadIdPattern, Handler: editorBadId},
		},
	}
}

// cspEditor — the editor/ide-react view CSP (Node layout-react csp, pinned
// live 2026-09-15): the React nonce + strict-dynamic policy PLUS the
// img-src 'self' data: blob: allowance the editor uses for rendered images.
func cspEditor(nonce string) string {
	return "script-src 'nonce-" + nonce + "' 'unsafe-inline' 'strict-dynamic' https: 'report-sample'; " +
		"object-src 'none'; base-uri 'none'; img-src 'self' data: blob:"
}

// writeEditor — render + the exact Node response envelope.
func writeEditor(w http.ResponseWriter, d views.EditorData, html string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", cspEditor(d.Nonce))
	w.Header().Set("Permissions-Policy", core.PinnedPermissionsPolicy)
	w.Header().Set("ETag", core.EtagWeakBody(html))
	w.WriteHeader(200)
	_, _ = io.WriteString(w, html)
}

func pageBase(cxt *core.Cxt) views.PageData {
	d := views.PageData{Nonce: views.NewNonce(), Origin: cxt.SiteURL}
	if cxt.Sess != nil {
		d.CSRFToken = cxt.Sess.CsrfToken()
	}
	return d
}

func editorPage(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if cxt.Sess == nil || !cxt.Sess.IsLoggedIn() {
			// anonymous — the global gate normally bounces here, but a
			// public-access profile reaches the handler: mirror Node's
			// requireLogin 302 /login.
			res.Redirect(cxt.Req, 302, "/login")
			return
		}
		ctx := cxt.Req.Context()
		id := cxt.Params["id"]
		oid := primitiveObjectID(id)
		// P5.1b — detachRole: "" (main), "detacher" or "detached". Only
		// "detached" changes the chrome; "detacher" + main share the ide-react
		// chrome and differ only in ol-detachRole + currentUrl (both derived
		// from the request path / the ol-detachRole slot below).
		detachRole := strings.TrimPrefix(cxt.Params["role"], "/")

		// load project first (Node order: requireLogin → canRead → load
		// user).
		pdoc, ok := loadProject(a, ctx, oid)
		if !ok {
			db := pageBase(cxt)
			views.NotFoundPage(res.W, db)
			return
		}

		uid := cxt.Sess.UserIDHex()
		isAdmin := a.Mongo != nil && projectlistUserIsAdmin(a, ctx, uid)
		if !projectCanRead(uid, isAdmin, pdoc) {
			db := pageBase(cxt)
			views.Restricted403(res.W, db)
			return
		}

		udoc, ok := loadUserDoc(a, ctx, uid)
		if !ok {
			res.SendStatus(500)
			return
		}
		email, _ := udoc["email"].(string)
		pid := oid.Hex()
		projName := strOf(pdoc["name"])
		ownerRef := oidHexOf(pdoc["owner_ref"])
		sharingUpdates := ownerHasSharingUpdates(a, ctx, ownerRef)

		currentURL := cxt.Req.URL.Path // "/editor/<id>" or "/Project/<id>"

		compileTimeout := int64(500)
		if f := subM(udoc, "features"); f != nil {
			if v, ok := f["compileTimeout"]; ok && v != nil {
				if n2, ok := v.(int64); ok {
					compileTimeout = n2
				} else if n2, ok := v.(int32); ok {
					compileTimeout = int64(n2)
				}
			}
		}
		trackChanges := false
		gitBridge := false
		if f := subM(udoc, "features"); f != nil {
			trackChanges, _ = f["trackChanges"].(bool)
			gitBridge, _ = f["gitBridge"].(bool)
		}
		learned := anySlice(udoc["learnedWords"])
		inactive := anySlice(udoc["inactiveTutorials"])
		projTags := anySlice(pdoc["tags"])

		d := views.EditorData{
			Nonce: views.NewNonce(),
			CSRF:  cxt.Sess.CsrfToken(),
			// Node <title> = "<projectName> - OlliTeX, Online LaTeX Editor"
			// (pinned oracle: /editor, /Project AND the detach shells).
			Title:       projName + " - OlliTeX, Online LaTeX Editor",
			ProjectName: projName,
			Origin:      cxt.SiteURL,
			CurrentURL:  currentURL,
			InitTheme:   initialLoadingScreenTheme(udoc),
			Detached:    detachRole == "detached",
		}

		// ---- raw slots ----
		d.Raw = map[string]string{
			"ol-csrfToken":        d.CSRF,
			"ol-baseAssetPath":    "/",
			"ol-mathJaxPath":      "/js/libs/mathjax-4.1.2/tex-svg.js",
			"ol-dictionariesRoot": "/js/dictionaries/0.0.3/",
			"ol-usersEmail":       email,
			"ol-user_id":          uid,
			"ol-project_id":       pid,
			"ol-projectName":      projName,
		}

		// ---- boolean slots (true → bare `content` attr; absent → bare) ----
		// showTemplatesServerPro — Node (ProjectController.loadEditor:
		// ~line 895): Features.hasFeature('templates-server-pro') =
		// Boolean(site_settings.templates — the section EXISTs in this stack)
		// AND (hasAdminAccess(user) || Settings.templates?.nonAdminCanManage
		// || Settings.templates.user_id === userId). In this stack
		// nonAdminCanManage is unseeded (never grants) and templates.user_id
		// is unset, so it resolves to site-admin ⇒ true (bare `content` —
		// pegged in the U2 gate 2026-09-22) / member ⇒ absent.
		showTemplatesServerPro := showTemplatesServerProFor(a, ctx, isAdmin, uid)
		d.Bool = map[string]bool{
			"ol-ownerHasSharingUpdates": sharingUpdates,
			"ol-latexEditorAvailable":   true,
			"ol-gitBridgeEnabled":       gitBridge,
			"ol-useShareJsHash":         true,
			"ol-showSymbolPalette":      true,
			"ol-symbolPaletteAvailable": true,
			"ol-hasTrackChangesFeature": trackChanges,
			"ol-customerIoEnabled":      true,
			"ol-showTemplatesServerPro": showTemplatesServerPro,
			// false (absent in oracle): isManagedAccount, canUseClsiCache,
			// canUsePng2Pdf, anonymous, isTokenMember,
			// isRestrictedTokenMember, wikiEnabled, debugPdfDetach,
			// showAiFeatures, showAiFeaturesDisabled, hasUnlimitedAi,
			// hasAiFreeTier, showUpgradePrompt, showSupport,
			// isSaas, shouldLoadHotjar,
			// ro-mirror-on-client-no-local-storage
		}

		// ---- string/number (typed) slots ----
		d.Typed = map[string]string{
			"ol-maxDocLength":                     "2097152",
			"ol-maxReconnectGracefullyIntervalMs": "30000",
			"ol-otMigrationStage":                 "0",
			"ol-defaultLatexCompiler":             "pdflatex",
			"ol-loadingText":                      "Loading",
			"ol-translationIoNotLoaded":           "Could not connect to WebSocket server",
			"ol-translationLoadErrorMessage":      "Could not load translations",
			"ol-translationUnableToJoin":          "Could not connect to collaboration server",
		}
		// P5.1b — ol-detachRole: empty on main (bare meta), "detacher"/
		// "detached" on the detach shells (content=…).
		if detachRole != "" {
			d.Typed["ol-detachRole"] = detachRole
		}

		// ---- json slots ----
		d.JSON = map[string]string{
			"ol-ab":                 pinned_ol_ab,
			"ol-i18n":               pinned_ol_i18n,
			"ol-ExposedSettings":    ExposedSettingsJSON(cxt.SiteURL, templates.MenuGrant(ctx, cxt)),
			"ol-splitTestVariants":  pinned_ol_splitTestVariants,
			"ol-splitTestInfo":      pinned_ol_splitTestInfo,
			"ol-navbar":             navbarJSON(cxt.SiteURL, currentURL, email, isAdmin, sitesettings.RegistrationEnabled(a, ctx)),
			"ol-footer":             withSiteURL(pinned_ol_footer, cxt.SiteURL),
			"ol-userSettings":       buildUserSettings(udoc),
			"ol-user":               serializeUser(uid, email, udoc),
			"ol-learnedWords":       jsonArr(learned),
			"ol-inactiveTutorials":  jsonArr(inactive),
			"ol-capabilities":       pinned_ol_capabilities,
			"ol-grammarSettings":    pinned_ol_grammarSettings,
			"ol-wsRetryHandshake":   pinned_ol_wsRetryHandshake,
			"ol-imageNames":         pinned_ol_imageNames,
			"ol-languages":          pinned_ol_languages,
			"ol-editorThemes":       pinned_ol_editorThemes,
			"ol-legacyEditorThemes": pinned_ol_legacyEditorThemes,
			"ol-projectTags":        jsonArr(serializeTags(projTags)),
			"ol-compileSettings":    fmt.Sprintf(`{"compileTimeout":%d}`, compileTimeout),
			"ol-overallThemes":      pinned_ol_overallThemes,
		}

		writeEditor(res.W, d, views.EditorPage(d))
		a.CommitSess(cxt.Sess, res.W)
	}
}

// serializeTags — Node project.tags: array of {id,name} or strings; the
// fixture project has no tags → [].
func serializeTags(tags []any) []any {
	if tags == nil {
		return []any{}
	}
	out := make([]any, 0, len(tags))
	out = append(out, tags...)
	return out
}

func withSiteURL(pinned, siteURL string) string {
	if siteURL == "" {
		return pinned
	}
	// The pinned JSON embeds the stack siteUrl; keep parity by substituting
	// the configured siteUrl (identical value for this stack).
	return strings.ReplaceAll(pinned, "http://127.0.0.1:7420", siteURL)
}

// ---------- mongo loads ----------

func loadUserDoc(a *core.App, ctx context.Context, uid string) (map[string]any, bool) {
	if a.Mongo == nil {
		return nil, false
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, false
	}
	doc := map[string]any{}
	if err := db.Collection("users").FindOne(
		ctx, bson.D{{Key: "_id", Value: primitiveObjectID(uid)}}).Decode(&doc); err != nil {
		return nil, false
	}
	return doc, true
}

type projDoc struct {
	owner_ref any
	name      any
	tags      any
	refs      map[string]any // collab/review/readonly/token refs + publicAccesLevel
}

func loadProject(a *core.App, ctx context.Context, oid primitive.ObjectID) (map[string]any, bool) {
	if a.Mongo == nil {
		return nil, false
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, false
	}
	doc := map[string]any{}
	if err := db.Collection("projects").FindOne(
		ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&doc); err != nil {
		return nil, false
	}
	return doc, true
}

// projectCanRead — mirror projectlist.canRead over a decoded map doc.
func projectCanRead(uid string, isAdmin bool, d map[string]any) bool {
	if uid == "" {
		return false
	}
	if oidHexOf(d["owner_ref"]) == uid {
		return true
	}
	for _, k := range []string{"collaberator_refs", "reviewer_refs", "readOnly_refs"} {
		if oidInList(d[k], uid) {
			return true
		}
	}
	pal, _ := d["publicAccesLevel"].(string)
	if pal == "tokenBased" {
		if oidInList(d["tokenAccessReadAndWrite_refs"], uid) || oidInList(d["tokenAccessReadOnly_refs"], uid) {
			return true
		}
	}
	if pal == "readOnly" || pal == "readAndWrite" {
		return true
	}
	return isAdmin
}

func ownerHasSharingUpdates(a *core.App, ctx context.Context, ownerRef string) bool {
	// Node: the project owner has sharingUpdates enabled (CE default true):
	// sharingUpdates !== false. True for the fixture owner.
	if ownerRef == "" || a.Mongo == nil {
		return true
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return true
	}
	doc := map[string]any{}
	if err := db.Collection("users").FindOne(
		ctx, bson.D{{Key: "_id", Value: primitiveObjectID(ownerRef)}}).Decode(&doc); err != nil {
		return true
	}
	if v, ok := doc["sharingUpdates"]; ok && v != nil {
		return b(v)
	}
	return true // CE default: enabled
}

// showTemplatesServerProFor — Node
// `Features.hasFeature('templates-server-pro') && (hasAdminAccess(user) ||
// Settings.templates?.nonAdminCanManage || Settings.templates.user_id ===
// userId)` (hasFeature = Boolean(site_settings.templates — section exists)).
// One site_settings read; section existence gates everything (Node ANDs the
// feature flag first).
func showTemplatesServerProFor(a *core.App, ctx context.Context, isAdmin bool, uid string) bool {
	if a == nil || a.Mongo == nil {
		return false
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	var doc bson.M
	if db.Collection("site_settings").FindOne(ctx, bson.D{{Key: "_id", Value: "global"}}).Decode(&doc) != nil {
		return false
	}
	sec, ok := doc["templates"].(bson.M)
	if !ok {
		return false // hasFeature('templates-server-pro') = false
	}
	if isAdmin {
		return true
	}
	if nonAdmin, _ := sec["nonAdminCanManage"].(bool); nonAdmin {
		return true
	}
	nodeUID, _ := sec["user_id"].(string)
	return nodeUID != "" && nodeUID == uid
}

// initialLoadingScreenTheme — Node (UserSettingsHelper):
// getInitialTheme(getOverallTheme(user)) — ace.overallTheme if set (the
// odd 'light-' enum value maps to 'light'; 'system'→system; ”→dark;
// anything else → dark); otherwise signUpDate < 2026-03-02T12:00Z → dark
// else system (Node: undefined signUpDate < Date is false ⇒ system).
func initialLoadingScreenTheme(doc map[string]any) string {
	has := false
	overall := ""
	if ace, ok := doc["ace"].(map[string]any); ok {
		if v, ok2 := ace["overallTheme"].(string); ok2 {
			has, overall = true, v
		}
	}
	if !has {
		cutoff := time.Date(2026, time.March, 2, 12, 0, 0, 0, time.UTC).UnixMilli()
		if ms, ok := millisOf(doc["signUpDate"]); ok && ms < cutoff {
			return "dark"
		}
		return "system"
	}
	switch overall {
	case "light-":
		return "light"
	case "":
		return "dark"
	case "system":
		return "system"
	default:
		return "dark"
	}
}

func millisOf(v any) (int64, bool) {
	switch t := v.(type) {
	case time.Time:
		return t.UnixMilli(), true
	case primitive.DateTime:
		return int64(t), true
	case int64:
		return t, true
	case int32:
		return int64(t), true
	case float64:
		return int64(t), true
	}
	return 0, false
}

func projectlistUserIsAdmin(a *core.App, ctx context.Context, uid string) bool {
	if a.Mongo == nil || uid == "" {
		return false
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	doc := map[string]any{}
	if err := db.Collection("users").FindOne(
		ctx, bson.D{{Key: "_id", Value: primitiveObjectID(uid)}}).Decode(&doc); err != nil {
		return false
	}
	if b(doc["isAdmin"]) {
		return true
	}
	return false
}

// editorBadId — Node contract for /editor|/project/<invalid ObjectId> in ANY
// case (U2 oracle, 2026-09-22): the express route matches, loadEditorSchema
// logs the enforced fallback, then the objectId param validator answers
// 404 JSON on the WIRE (application/json; charset=utf-8 + weak ETag).
// Anonymous is bounced to /login FIRST (global gate) — pinned in the gate.
func editorBadId(cxt *core.Cxt, res *core.Res) {
	res.JSON(404, []byte(`{"error":"Validation error: Invalid Mongo ObjectId at \"params.Project_id\"","statusCode":404}`))
}

// ---------- tiny decoders ----------

func primitiveObjectID(hex string) primitive.ObjectID {
	oid, _ := primitive.ObjectIDFromHex(strings.ToLower(hex))
	return oid
}

func strOf(v any) string {
	if x, ok := v.(string); ok {
		return x
	}
	return ""
}

func oidHexOf(v any) string {
	switch t := v.(type) {
	case primitive.ObjectID:
		return t.Hex()
	case string:
		return t
	}
	return ""
}

func oidInList(v any, uid string) bool {
	if uid == "" {
		return false
	}
	arr, ok := v.([]any)
	if !ok {
		return false
	}
	for _, x := range arr {
		if oidHexOf(x) == uid {
			return true
		}
	}
	return false
}
