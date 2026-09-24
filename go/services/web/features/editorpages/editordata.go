// P5.1a — editor-page EditorData assembly (Go parity of the Node
// ProjectController.loadEditor + EditorHelper locals, pinned oracle
// 2026-09-15).
//
// The renderer (views.EditorPage) fills 68 <meta name="ol-…"> slots. This
// package supplies the exact per-request values:
//
//   - pinned config/build JSON (pinned.go) → ol-ExposedSettings,
//     ol-splitTestVariants, ol-languages, ol-editorThemes, … (byte-identical
//     to the Node server-ce config for this stack);
//   - per-user values read from the users doc → ol-user (serializeUser),
//     ol-userSettings (buildUserSettings), ol-learnedWords,
//     ol-inactiveTutorials, ol-compileSettings, ol-hasTrackChangesFeature,
//     ol-gitBridgeEnabled, ol-ownerHasSharingUpdates;
//   - per-project values → ol-project_id, ol-projectName, ol-projectTags;
//   - per-request values → csrf, nonce, ol-navbar.currentUrl,
//     ol-navbar.sessionUser, siteUrl.
//
// Rendering rules (views.EditorPage): a slot whose value is ABSENT from the
// matching map renders BARE (no content attribute) — that is how Node's
// JSON.stringify/pug semantics drop undefined values (ol-brandVariation,
// ol-wsUrl, ol-detachRole on the main shell, ol-gitBridgePublicBaseUrl,
// ol-compilesUserContentDomain).

package editorpages

import (
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// jesc — JSON string escape (Node JSON.stringify: \" \\ \n \t and
// \uXXXX for C0; does NOT escape < > &, which the renderer then HTML-escapes).

func jesc(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// ---- small typed readers over a decoded Mongo doc (map[string]any) ----

func s(v any) string {
	if x, ok := v.(string); ok {
		return x
	}
	return ""
}
func b(v any) bool {
	if x, ok := v.(bool); ok {
		return x
	}
	return false
}
func n(v any) int64 {
	switch x := v.(type) {
	case int32:
		return int64(x)
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		return int64(x)
	}
	return 0
}
func subM(m map[string]any, k string) map[string]any {
	if x, ok := m[k].(map[string]any); ok {
		return x
	}
	return nil
}
func has(m map[string]any, k string) bool {
	_, ok := m[k]
	return ok && m[k] != nil
}

// boolJSON — "true"/"false" (JSON).
func boolJSON(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// isoDate — Node `user.signUpDate` is serialized as an ISO-8601 UTC string
// with 3-digit milliseconds (moment .toISOString()). The Go driver decodes
// BSON dates into map[string]any as primitive.DateTime (int64 ms epoch);
// tolerate time.Time as well.
func isoDate(v any) string {
	switch t := v.(type) {
	case primitive.DateTime:
		return time.UnixMilli(int64(t)).UTC().Format("2006-01-02T15:04:05.000Z")
	case time.Time:
		return t.UTC().Format("2006-01-02T15:04:05.000Z")
	default:
		return s(t)
	}
}

// arrJSON — render a Go []any of scalars as a compact JSON array string.
func jsonArr(vals []any) string {
	if vals == nil {
		return "[]"
	}
	parts := make([]string, 0, len(vals))
	for _, v := range vals {
		switch x := v.(type) {
		case string:
			parts = append(parts, `"`+jesc(x)+`"`)
		case bool:
			parts = append(parts, boolJSON(x))
		default:
			parts = append(parts, fmt.Sprint(v))
		}
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// serializeUser — Node UserHelper.serializeUser for the editor ol-user meta
// (order + undefined-omission pinned from the oracle).
func serializeUser(uid, email string, doc map[string]any) string {
	var sstr strings.Builder
	sstr.WriteString(`{"id":"` + jesc(uid) + `"`)
	sstr.WriteString(`,"email":"` + jesc(email) + `"`)
	sstr.WriteString(`,"first_name":"` + jesc(s(doc["first_name"])) + `"`)
	sstr.WriteString(`,"last_name":"` + jesc(s(doc["last_name"])) + `"`)
	if rid := s(doc["referal_id"]); rid != "" {
		sstr.WriteString(`,"referal_id":"` + jesc(rid) + `"`)
	}
	if su, ok := doc["signUpDate"]; ok && su != nil {
		sstr.WriteString(`,"signUpDate":"` + jesc(isoDate(su)) + `"`)
	}
	af := true // Node: allowedFreeTrial defaults true (absent in CE docs)
	if _, ok := doc["allowedFreeTrial"]; ok {
		af = b(doc["allowedFreeTrial"])
	}
	sstr.WriteString(`,"allowedFreeTrial":` + boolJSON(af))
	sstr.WriteString(`,"hasPaidSubscription":` + boolJSON(b(doc["hasPaidSubscription"])))

	// features — declared order; undefined entries omitted (Node user.get
	// returns the doc value when present).
	feat := subM(doc, "features")
	var fp []string
	if feat != nil {
		for _, k := range []string{"collaborators", "versioning", "dropbox",
			"github", "gitBridge", "compileTimeout", "compileGroup",
			"references", "trackChanges", "aiUsageQuota", "offlineMode"} {
			if fv, ok := feat[k]; ok && fv != nil {
				switch t := fv.(type) {
				case string:
					fp = append(fp, `"`+jesc(k)+`":"`+jesc(t)+`"`)
				case bool:
					fp = append(fp, `"`+jesc(k)+`":`+boolJSON(t))
				default:
					fp = append(fp, `"`+jesc(k)+`":`+fmt.Sprint(n(fv)))
				}
			}
		}
	}
	sstr.WriteString(`,"features":{` + strings.Join(fp, ",") + `}`)

	// featureUsage ({} unless set)
	fu := subM(doc, "featureUsage")
	var fuPairs []string
	if fu != nil {
		// preserve doc order for known CE keys
		for _, k := range []string{"wordCount"} {
			if fv, ok := fu[k]; ok && fv != nil {
				fuPairs = append(fuPairs, `"`+jesc(k)+`":`+fmt.Sprint(n(fv)))
			}
		}
	}
	sstr.WriteString(`,"featureUsage":{` + strings.Join(fuPairs, ",") + `}`)

	// refProviders {mendeley, zotero, papers} (default false)
	rp := subM(doc, "refProviders")
	rpg := func(k string) bool {
		if rp != nil {
			if v, ok := rp[k]; ok {
				return b(v)
			}
		}
		return false
	}
	sstr.WriteString(`,"refProviders":{"mendeley":` + boolJSON(rpg("mendeley")))
	sstr.WriteString(`,"zotero":` + boolJSON(rpg("zotero")))
	sstr.WriteString(`,"papers":` + boolJSON(rpg("papers")) + `}`)

	// writefull {autoCreatedAccount}
	wf := subM(doc, "writefull")
	ac := false
	if wf != nil {
		if v, ok := wf["autoCreatedAccount"]; ok {
			ac = b(v)
		}
	}
	sstr.WriteString(`,"writefull":{"autoCreatedAccount":` + boolJSON(ac) + `}`)

	sstr.WriteString(`,"alphaProgram":` + boolJSON(b(doc["alphaProgram"])))
	sstr.WriteString(`,"betaProgram":` + boolJSON(b(doc["betaProgram"])))
	sstr.WriteString(`,"labsProgram":` + boolJSON(b(doc["labsProgram"])))

	inact := anySlice(doc["inactiveTutorials"])
	sstr.WriteString(`,"inactiveTutorials":` + jsonArr(inact))
	sstr.WriteString(`,"isAdmin":` + boolJSON(b(doc["isAdmin"])))

	pc := s(doc["planCode"])
	if pc == "" {
		pc = "personal"
	}
	pn := s(doc["planName"])
	if pn == "" {
		pn = "Personal"
	}
	sstr.WriteString(`,"planCode":"` + jesc(pc) + `"`)
	sstr.WriteString(`,"planName":"` + jesc(pn) + `"`)
	sstr.WriteString(`,"isProfessionalGroupPlan":false`)
	sstr.WriteString(`,"isMemberOfGroupSubscription":false`)
	sstr.WriteString(`,"hasInstitutionLicence":false`)
	sstr.WriteString(`,"activeProfessionalGroupSubscriptions":[]`)
	sstr.WriteString(`}`)
	return sstr.String()
}

func anySlice(v any) []any {
	if x, ok := v.([]any); ok {
		return x
	}
	return []any{}
}

// refProviderJSON — Node buildUserSettings ref-provider block.
func refProviderJSON(doc map[string]any, p string) string {
	ace := subM(doc, "ace")
	var pr map[string]any
	if ace != nil {
		if x, hasK := ace[p]; hasK && x == nil {
			return "null" // explicit null stored → Node returns null
		}
		if m, ok := ace[p].(map[string]any); ok {
			pr = m
		}
	}
	en := true // default enabled
	if pr != nil && has(pr, "enabled") {
		en = b(pr["enabled"])
	}
	dpl := false
	if pr != nil && has(pr, "disablePersonalLibrary") {
		dpl = b(pr["disablePersonalLibrary"])
	}
	groups := `[]`
	if pr != nil {
		if g, ok := pr["groups"].([]any); ok {
			var gp []string
			for _, it := range g {
				if m, ok := it.(map[string]any); ok {
					id := s(m["id"])
					gp = append(gp, `{"id":"`+jesc(id)+`"}`)
				}
			}
			groups = "[" + strings.Join(gp, ",") + "]"
		}
	}
	return `{"enabled":` + boolJSON(en) + `,"disablePersonalLibrary":` + boolJSON(dpl) + `,"groups":` + groups + `}`
}

// buildUserSettings — Node UserSettingsHelper.buildUserSettings(user): read
// user.ace.* with the CE defaults (pinned from the oracle; ace is empty in
// the fixture so every field is the default).
func buildUserSettings(doc map[string]any) string {
	ace := subM(doc, "ace")
	getS := func(k, def string) string {
		if ace != nil {
			if v, ok := ace[k]; ok && v != nil {
				return s(v)
			}
		}
		return def
	}
	getB := func(k string, def bool) bool {
		if ace != nil {
			if v, ok := ace[k]; ok && v != nil {
				return b(v)
			}
		}
		return def
	}
	getN := func(k string, def int64) int64 {
		if ace != nil {
			if v, ok := ace[k]; ok && v != nil {
				return n(v)
			}
		}
		return def
	}

	var sstr strings.Builder
	sstr.WriteString(`{"mode":"` + jesc(getS("mode", "none")) + `"`)
	sstr.WriteString(`,"editorTheme":"` + jesc(getS("theme", "textmate")) + `"`)
	sstr.WriteString(`,"editorLightTheme":"` + jesc(getS("lightTheme", "textmate")) + `"`)
	sstr.WriteString(`,"editorDarkTheme":"` + jesc(getS("darkTheme", "overleaf_dark")) + `"`)
	sstr.WriteString(`,"fontSize":` + fmt.Sprint(getN("fontSize", 12)))
	sstr.WriteString(`,"autoComplete":` + boolJSON(getB("autoComplete", true)))
	sstr.WriteString(`,"autoPairDelimiters":` + boolJSON(getB("autoPairDelimiters", true)))
	sstr.WriteString(`,"pdfViewer":"` + jesc(getS("pdfViewer", "pdfjs")) + `"`)
	// syntaxValidation — NO schema default: present only when stored
	// (2026-09-16 P6.1 pin: admin fixture emits it between pdfViewer and
	// previewTabs, the member fixture does not).
	if ace != nil {
		if v, ok := ace["syntaxValidation"]; ok && v != nil {
			sstr.WriteString(`,"syntaxValidation":` + boolJSON(b(v)))
		}
	}
	sstr.WriteString(`,"previewTabs":` + boolJSON(getB("previewTabs", false)))
	sstr.WriteString(`,"fontFamily":"` + jesc(getS("fontFamily", "lucida")) + `"`)
	sstr.WriteString(`,"lineHeight":"` + jesc(getS("lineHeight", "normal")) + `"`)
	// overallTheme — Node getOverallTheme(user): ace.overallTheme when
	// stored (may be '' for the dark default), else the
	// signUpDate < 2026-03-02T12:00:00Z cutoff ('' | 'system').
	otStored := ""
	otOk := false
	if ace != nil {
		if v, ok2 := ace["overallTheme"]; ok2 && v != nil {
			otStored, otOk = s(v), true
		}
	}
	ot := "system"
	if otOk {
		ot = otStored
	} else {
		// JS: `user.signUpDate < CUTOFF` — null/undefined → false → "system"
		var sdMs int64
		sdOk := false
		switch t := doc["signUpDate"].(type) {
		case primitive.DateTime:
			sdMs, sdOk = int64(t), true
		case int64:
			sdMs, sdOk = t, true
		case int32:
			sdMs, sdOk = int64(t), true
		case time.Time:
			sdMs, sdOk = t.UnixMilli(), true
		}
		if sdOk && sdMs < int64(1772452800000) { // Date.UTC(2026, 2, 2, 12, 0, 0)
			ot = ""
		}
	}
	sstr.WriteString(`,"overallTheme":"` + jesc(ot) + `"`)
	sstr.WriteString(`,"mathPreview":` + boolJSON(getB("mathPreview", true)))
	sstr.WriteString(`,"breadcrumbs":` + boolJSON(getB("breadcrumbs", false)))
	sstr.WriteString(`,"editorTabs":` + boolJSON(getB("editorTabs", true)))
	sstr.WriteString(`,"nonBlinkingCursor":` + boolJSON(getB("nonBlinkingCursor", false)))
	sstr.WriteString(`,"referencesSearchMode":"` + jesc(getS("referencesSearchMode", "advanced")) + `"`)
	sstr.WriteString(`,"darkModePdf":` + boolJSON(getB("darkModePdf", false)))
	sstr.WriteString(`,"floatingMenu":` + boolJSON(getB("floatingMenu", true)))
	// customKeybindings (doc.ace.customKeybindings ?? {})
	ckb := `{}`
	if ace != nil {
		if m, ok := ace["customKeybindings"].(map[string]any); ok && m != nil {
			var pairs []string
			// Node preserves the object's insertion order; the only key in the
			// fixture set is via the settings page. Use a stable order for the
			// known keys, then any extras.
			if len(m) > 0 {
				var keys []string
				for k := range m {
					keys = append(keys, k)
				}
				// deterministic: sort for stable output (Node order = insertion;
				// parity for the fixture is the empty object).
				sortStrings(keys)
				for _, k := range keys {
					if sv, ok := m[k].(string); ok {
						pairs = append(pairs, `"`+jesc(k)+`":"`+jesc(sv)+`"`)
					}
				}
			}
			ckb = "{" + strings.Join(pairs, ",") + "}"
		}
	}
	sstr.WriteString(`,"customKeybindings":` + ckb)
	sstr.WriteString(`,"zotero":` + refProviderJSON(doc, "zotero"))
	sstr.WriteString(`,"mendeley":` + refProviderJSON(doc, "mendeley"))
	sstr.WriteString(`,"papers":` + refProviderJSON(doc, "papers"))
	sstr.WriteString(`}`)
	return sstr.String()
}

func sortStrings(x []string) {
	for i := 1; i < len(x); i++ {
		for j := i; j > 0 && x[j] < x[j-1]; j-- {
			x[j], x[j-1] = x[j-1], x[j]
		}
	}
}

// navbarJSON — ol-navbar (static config + dynamic currentUrl + sessionUser).
func navbarJSON(siteURL, currentURL, email string, isAdmin, showSignUp bool) string {
	var sstr strings.Builder
	sstr.WriteString(`{"customLogo":"/logo_full.svg"`)
	sstr.WriteString(`,"customLogoDark":"/logo_full.svg"`)
	sstr.WriteString(`,"canDisplayInstanceStats":true`)
	sstr.WriteString(`,"title":"OlliTeX"`)
	sstr.WriteString(`,"hideLogo":false`)
	sstr.WriteString(`,"canDisplayAdminMenu":` + boolJSON(isAdmin))
	sstr.WriteString(`,"canDisplayAdminRedirect":false`)
	// canDisplayProjectUrlLookup — Node (layout-react.pug):
	// settings.adminPrivilegeAvailable && canDisplayAdminMenu &&
	// hasAdminCapability('view-project-setting', false). In this stack the
	// admin-privilege terms are all true for site admins, so it equals
	// isAdmin (U2 gate pin 2026-09-22: Node admin ⇒ true / member ⇒ false).
	sstr.WriteString(`,"canDisplayProjectUrlLookup":` + boolJSON(isAdmin))
	sstr.WriteString(`,"canDisplaySplitTestMenu":false`)
	sstr.WriteString(`,"canDisplaySurveyMenu":false`)
	sstr.WriteString(`,"canDisplayScriptLogMenu":false`)
	sstr.WriteString(`,"suppressNavbarRight":false`)
	sstr.WriteString(`,"suppressNavContentLinks":false`)
	// showSignUpLink — Node: hasFeature('registration-page') =
	// boolFromEnv(OVERLEAF_ENABLE_REGISTRATION_PAGE) ?? !(sso-saml||sso-ldap||
	// sso-oidc site_settings enabled), computed per request by the caller
	// (U9 helper sitesettings.RegistrationEnabled; e2e stack: SAML IdP on
	// -> false — U2 gate pin holds).
	sstr.WriteString(`,"showSignUpLink":` + boolJSON(showSignUp))
	sstr.WriteString(`,"currentUrl":"` + jesc(currentURL) + `"`)
	sstr.WriteString(`,"sessionUser":{"email":"` + jesc(email) + `"`)
	sstr.WriteString(`},"items":[{"text":"Library","url":"/library","class":"subdued","translatedText":"Library"},`)
	sstr.WriteString(`{"text":"Templates","url":"/templates","class":"subdued","only_when_logged_in":true,"translatedText":"Templates"}]`)
	sstr.WriteString(`}`)
	return sstr.String()
}
