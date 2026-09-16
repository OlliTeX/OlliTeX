// Package hub ports the ollitex-hub module's server surface (P6.1):
//
//	GET    /hub               (requireLogin)     → the React hub page
//	GET    /hub/admin         (requireLogin)     → 302 /hub (legacy URL)
//	GET    /hub/workspace     (requireLogin)     → 302 /hub (legacy URL)
//	GET    /api/hub-theme     (requireLogin)     → JSON null | {version,light,dark}
//	PUT    /api/hub-theme     (requireLogin, site admin) → JSON theme
//	                                             | 400 {"error":"…"}
//	DELETE /api/hub-theme     (requireLogin, site admin) → {"ok":true}
//	GET    /api/hub/health    (requireLogin, site admin) → core JSON
//	GET    /api/hub/notes     (global login gate) → docs/RELEASE_NOTES.md
//
// Node ground truth (2026-09-16 oracle, /tmp/p61):
//
//	modules/ollitex-hub/app/src/{HubRouter,HubController,HubTheme}.mjs
//
// Pins:
//   - CSRF (core mustCsrf) runs BEFORE site-admin authorization: anon or
//     token-less PUT/DELETE → 403 "Forbidden" (pinned live P6.1).
//   - site-admin denial → 302 /restricted?from=<enc(path)> with the
//     Accept-negotiated body (core restrictedBounce; pinned P3.1).
//   - theme JSON key order: version, light, dark; per mode: primary,
//     background, surface, text, dimmed, border, button, buttonText,
//     fontFamily, fontSize, radius (Node insertion order, mirrored).
//   - 400 contract: {"error":"light.primary: must be a hex color
//     (#rrggbb)"} — exact message strings from HubTheme.mjs.
//   - malformed JSON PUT → 400 `{}` (express.json error, X-Powered-By,
//     no web baseline — core BareWrite, pinned live).
//   - health: generatedAt / uptimeSec / platform.node / platform.pid /
//     mongo.* are process- or driver-dependent — the gate normalizes them;
//     instance.*, featureGates (values + key order) are byte-compared.

package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/features/editorpages"
	"ollitex/go/services/web/views"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// mode — a theme light/dark mode (validated map, canonical key order at
// serialization time).
type mode = map[string]any

// ---------- routes ----------

// Feature registers the ollitex-hub routes (P6.1).
func Feature(a *core.App) core.Feature {
	return core.Feature{
		Name: "ollitex-hub",
		Routes: []core.Route{
			{Method: "GET", Path: "/hub", Handler: hubPage(a)},
			{Method: "GET", Path: "/hub/admin", Handler: redirectToHub},
			{Method: "GET", Path: "/hub/workspace", Handler: redirectToHub},
			{Method: "GET", Path: "/api/hub-theme", Handler: getTheme(a)},
			{Method: "PUT", Path: "/api/hub-theme", Handler: saveTheme(a)},
			{Method: "DELETE", Path: "/api/hub-theme", Handler: clearTheme(a)},
			{Method: "GET", Path: "/api/hub/health", Handler: hubHealth(a)},
			{Method: "GET", Path: "/api/hub/notes", Handler: releaseNotes()},
		},
	}
}

func redirectToHub(cxt *core.Cxt, res *core.Res) {
	res.Redirect(cxt.Req, 302, "/hub")
}

// ---------- /hub page ----------

// hubPage — Node HubController.hubPage (buildLocals + res.render).
// Envelope pinned live 2026-09-16: 200 text/html; charset=utf-8;
// CSP = core.CSPViewPolicy (script-src nonce + strict-dynamic, NO
// img-src — differs from the editor page); web baseline; weak ETag.
func hubPage(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if cxt.Sess == nil || !cxt.Sess.IsLoggedIn() {
			// the global gate normally bounces first; mirror Node's
			// requireLogin 302 for public-access profiles.
			res.Redirect(cxt.Req, 302, "/login")
			return
		}
		ctx := cxt.Req.Context()
		uid := cxt.Sess.UserIDHex()
		udoc, aceRaw, ok := loadUserDoc(a, ctx, uid)
		if !ok {
			res.SendStatus(500)
			return
		}
		email, _ := udoc["email"].(string)
		isAdmin := a.Mongo != nil && boolOf(udoc["isAdmin"])
		hubTheme := themeSnapshotJSON(a, ctx) // "null" or {version,light,dark}

		d := views.HubData{
			Nonce:     views.NewNonce(),
			CSRF:      cxt.Sess.CsrfToken(),
			Origin:    cxt.SiteURL,
			HubAdmin:  isAdmin,
			GitBridge: os.Getenv("OVERLEAF_GITBRIDGE_ENABLED") == "true",
			JSON: map[string]string{
				"ol-ExposedSettings": editorpages.ExposedSettingsJSON(cxt.SiteURL, isAdmin),
				"ol-navbar":          hubNavbar(email, isAdmin),
				"ol-footer":          editorpages.HubFooterJSON(cxt.SiteURL),
				"ol-user":            serializeHubUser(uid, email, udoc, aceRaw),
				"ol-userSettings":    editorpages.BuildUserSettings(udoc),
				"ol-hub-theme":       hubTheme,
			},
			Raw: map[string]string{
				"ol-csrfToken":  cxt.Sess.CsrfToken(),
				"ol-usersEmail": email,
				"ol-user_id":    uid,
			},
		}

		html := views.HubPage(d)
		w := res.W
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", core.CSPViewPolicy(d.Nonce))
		w.Header().Set("Permissions-Policy", core.PinnedPermissionsPolicy)
		w.Header().Set("ETag", core.EtagWeakBody(html))
		w.WriteHeader(200)
		_, _ = io.WriteString(w, html)
		a.CommitSess(cxt.Sess, w)
	}
}

// hubNavbar — the HUB page's ol-navbar meta (pinned live 2026-09-16 from
// /hub member + admin captures). Differs from the EDITOR navbar: no
// "title" key, hideLogo:true, suppressNavContentLinks:true; admin flips
// canDisplayAdminMenu + canDisplayProjectUrlLookup. Items identical for
// both roles (Library + Templates).
func hubNavbar(email string, isAdmin bool) string {
	b := "false"
	if isAdmin {
		b = "true"
	}
	return `{"customLogo":"/logo_full.svg` +
		`","customLogoDark":"/logo_full.svg` +
		`","canDisplayInstanceStats":true` +
		`,"hideLogo":true` +
		`,"canDisplayAdminMenu":` + b +
		`,"canDisplayAdminRedirect":false` +
		`,"canDisplayProjectUrlLookup":` + b +
		`,"canDisplaySplitTestMenu":false` +
		`,"canDisplaySurveyMenu":false` +
		`,"canDisplayScriptLogMenu":false` +
		`,"suppressNavbarRight":false` +
		`,"suppressNavContentLinks":true` +
		`,"showSignUpLink":false` +
		`,"currentUrl":"/hub"` +
		`,"sessionUser":{"email":"` + jsString(email)[1:len(jsString(email))-1] + `"` +
		`},"items":` +
		`[{"text":"Library","url":"/library","class":"subdued","translatedText":"Library"},` +
		`{"text":"Templates","url":"/templates","class":"subdued","only_when_logged_in":true,"translatedText":"Templates"}]` +
		`}`
}

// ---------- theme validation (HubTheme.mjs parity) ----------

var colorRe = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)

var colorKeys = []string{
	"primary", "background", "surface", "text",
	"dimmed", "border", "button", "buttonText",
}

func colorHex(v string) bool { return colorRe.MatchString(v) }

// cloneMode — test helper.
func cloneMode(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// validateMode mirrors HubTheme.mjs validateMode (message strings ARE the
// 400 contract). JS `!mode || typeof mode !== 'object'`: arrays pass the
// object check, then fail on the first absent color key.
func validateMode(v any, where string) (map[string]any, string) {
	if _, isArr := v.([]any); isArr {
		return nil, where + ".primary: must be a hex color (#rrggbb)"
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, where + ": must be an object"
	}
	out := map[string]any{}
	for _, key := range colorKeys {
		val, ok2 := m[key]
		s, ok3 := val.(string)
		if !ok2 || !ok3 || !colorHex(s) {
			return nil, where + "." + key + ": must be a hex color (#rrggbb)"
		}
		out[key] = s
	}
	ff, ok2 := m["fontFamily"].(string)
	if !ok2 || strings.TrimSpace(ff) == "" || utf8.RuneCountInString(ff) > 300 {
		return nil, where + ".fontFamily: must be a non-empty css font stack"
	}
	out["fontFamily"] = ff
	fs, ok2 := m["fontSize"].(float64)
	if !ok2 || fs < 12 || fs > 22 {
		return nil, where + ".fontSize: must be a number between 12 and 22"
	}
	out["fontSize"] = fs
	rad, ok2 := m["radius"].(float64)
	if !ok2 || rad < 2 || rad > 24 {
		return nil, where + ".radius: must be a number between 2 and 24"
	}
	out["radius"] = rad
	return out, ""
}

// validateTheme — light fully before dark (Node saveHubTheme order).
func validateTheme(light any, dark any) (map[string]any, map[string]any, string) {
	l, err := validateMode(light, "light")
	if err != "" {
		return nil, nil, err
	}
	d, err := validateMode(dark, "dark")
	if err != "" {
		return nil, nil, err
	}
	return l, d, ""
}

// ---------- ordered JSON serialization ----------

// jsString — JS JSON.stringify string escaping (no HTML escaping; control
// chars → \uXXXX or short escapes).
func jsString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func jsonNum(v any) string {
	switch t := v.(type) {
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 32)
	case int64:
		return strconv.FormatInt(t, 10)
	case int32:
		return strconv.FormatInt(int64(t), 10)
	case int:
		return strconv.Itoa(t)
	}
	return "null"
}

// modeJSON — validated mode in canonical key order.
func modeJSON(m map[string]any) string {
	parts := make([]string, 0, 11)
	for _, k := range colorKeys {
		parts = append(parts, `"`+k+`":`+jsString(m[k].(string)))
	}
	parts = append(parts, `"fontFamily":`+jsString(m["fontFamily"].(string)))
	parts = append(parts, `"fontSize":`+jsonNum(m["fontSize"]))
	parts = append(parts, `"radius":`+jsonNum(m["radius"]))
	return "{" + strings.Join(parts, ",") + "}"
}

// themeJSON — {version:1, light, dark} (saveTheme response + page meta).
func themeJSON(light, dark map[string]any) string {
	return `{"version":1,"light":` + modeJSON(light) + `,"dark":` + modeJSON(dark) + `}`
}

// modeFromStored — GET /api/hub-theme + the page meta: the STORED value.
// Stores only contain validated docs (canonical order) or the schema
// defaults (null) — same bytes as Node's JSON.stringify of the lean doc.
func modeFromStored(v any) string {
	if v == nil {
		return "null"
	}
	if d, ok := v.(bson.D); ok { // driver decodes `any` fields to bson.D
		return bsonDJSON(d) // PRESERVES STORED KEY ORDER (Node lean order)
	}
	m, ok := v.(map[string]any)
	if !ok {
		switch t := v.(type) {
		case string:
			return jsString(t)
		case bool:
			if t {
				return "true"
			}
			return "false"
		}
		return jsonNum(v)
	}
	parts := make([]string, 0, 11)
	for _, k := range append(append([]string{}, colorKeys...), "fontFamily", "fontSize", "radius") {
		vv, ok2 := m[k]
		if !ok2 {
			continue // Node: JSON.stringify omits absent keys
		}
		switch t := vv.(type) {
		case nil:
			parts = append(parts, `"`+k+`":null`)
		case string:
			parts = append(parts, `"`+k+`":`+jsString(t))
		case bool:
			if t {
				parts = append(parts, `"`+k+`":true`)
			} else {
				parts = append(parts, `"`+k+`":false`)
			}
		default:
			parts = append(parts, `"`+k+`":`+jsonNum(vv))
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// ---------- theme handlers ----------

type themeDoc struct {
	Version any    `bson:"version"`
	Light   bson.D `bson:"light"`
	Dark    bson.D `bson:"dark"`
}

// bsonDJSON — ordered (stored) serialization of a bson.D subdocument.
func bsonDJSON(d bson.D) string {
	parts := make([]string, 0, len(d))
	for _, el := range d {
		parts = append(parts, `"`+jesc(el.Key)+`":`+anyJSON(el.Value))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func loadThemeDoc(a *core.App, ctx context.Context) (*themeDoc, error) {
	if a.Mongo == nil {
		return nil, nil
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, err
	}
	var doc themeDoc
	if err := db.Collection("hubthemes").FindOne(ctx,
		bson.D{{Key: "documentId", Value: "default"}}).Decode(&doc); err != nil {
		return nil, err // ErrNoDocuments when absent
	}
	return &doc, nil
}

func numValue(v any) (int, bool) {
	switch t := v.(type) {
	case int64:
		return int(t), true
	case int32:
		return int(t), true
	case int:
		return t, true
	case float64:
		if t == float64(int64(t)) {
			return int(t), true
		}
	}
	return 0, false
}

// getTheme — 200 `null` (pinned: 4 B) or the stored theme JSON.
func getTheme(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		doc, err := loadThemeDoc(a, cxt.Req.Context())
		if err != nil || doc == nil {
			// Node getHubTheme: read failures → null (logged).
			res.JSON(200, []byte("null"))
			return
		}
		ver := 1
		if n, ok := numValue(doc.Version); ok && n != 0 { // Node: Number(v) || 1
			ver = n
		}
		body := fmt.Sprintf(`{"version":%d,"light":%s,"dark":%s}`,
			ver, modeFromStored(doc.Light), modeFromStored(doc.Dark))
		res.JSON(200, []byte(body))
	}
}

// parseBody — Node express.json (CT json) / express.urlencoded (CT form)
// parity for req.body. Returns (object, hasBody, syntaxError).
func parseBody(r *http.Request) (map[string]any, bool, bool) {
	ct := strings.ToLower(r.Header.Get("Content-Type"))
	raw, _ := io.ReadAll(r.Body)
	r.Body = http.NoBody
	switch {
	case strings.Contains(ct, "json"):
		if len(strings.TrimSpace(string(raw))) == 0 {
			return nil, false, false // req.body undefined → handler {}
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, false, true // SyntaxError → express 400 {}
		}
		if m, ok := v.(map[string]any); ok {
			return m, true, false
		}
		// array (passes express.json strict): req.body = [] → light
		// access undefined → object error below. Scalars are 400 {}'d by
		// the core pre-check (pinned P3.3); handled here defensively.
		return map[string]any{}, true, false
	case strings.Contains(ct, "urlencoded"):
		vals, err := parseForm(string(raw))
		if err != nil {
			return nil, false, false
		}
		return vals, len(vals) > 0, false
	}
	return nil, false, false
}

// parseForm — body-parser qs subset: light[field]=value → nested map;
// values keep their strings (Node fontSize-as-string → the "must be a
// number" 400, pinned live).
func parseForm(raw string) (map[string]any, error) {
	out := map[string]any{}
	for _, pair := range strings.Split(raw, "&") {
		if pair == "" {
			continue
		}
		key, val := pair, ""
		if eq := strings.IndexByte(pair, '='); eq >= 0 {
			key = pair[:eq]
			val = pair[eq+1:]
		}
		uk, err := url.QueryUnescape(key)
		if err != nil {
			return nil, err
		}
		uv, err := url.QueryUnescape(val)
		if err != nil {
			return nil, err
		}
		uk = strings.ReplaceAll(uk, "+", " ")
		uv = strings.ReplaceAll(uv, "+", " ")
		if i := strings.IndexByte(uk, '['); i >= 0 {
			base, rest := uk[:i], uk[i+1:]
			if j := strings.LastIndexByte(rest, ']'); j >= 0 {
				sub, _ := out[base].(map[string]any)
				if sub == nil {
					sub = map[string]any{}
					out[base] = sub
				}
				sub[rest[:j]] = uv
				continue
			}
		}
		out[uk] = uv
	}
	return out, nil
}

// saveTheme — PUT: site admin → validate → upsert hubthemes → theme
// JSON; validation failure → 400 {"error":"…"}; malformed JSON → 400 {}.
func saveTheme(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		body, has, syntaxErr := parseBody(cxt.Req)
		if syntaxErr {
			// express.json syntax error → 400 {} (pinned live P6.1) — runs
			// BEFORE the admin gate (parser is app-level in Node).
			res.BareWrite(400, []byte("{}"))
			return
		}
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		var light, dark any
		if has && body != nil {
			light = body["light"]
			dark = body["dark"]
		}
		l, d, msg := validateTheme(light, dark)
		if msg != "" {
			res.JSON(400, []byte(`{"error":"`+jsEscapeErr(msg)+`"}`))
			return
		}
		if a.Mongo != nil {
			ctx := cxt.Req.Context()
			db, err := a.Mongo.DB(ctx)
			if err == nil {
				// Node: updateOne(…, { $set: value }, { upsert: true }) —
				// the Upsert OPTION (a $setOnInsert-only update would no-op
				// on a missing doc, P6.1 gate catch). light/dark stored as
				// ORDERED docs (canonical key order) — Node stores the
				// validated object in that order and GETs lean the stored
				// order byte-for-byte; a Go map $set would randomize it.
				_, _ = db.Collection("hubthemes").UpdateOne(ctx,
					bson.D{{Key: "documentId", Value: "default"}},
					bson.D{{Key: "$set", Value: bson.D{
						{Key: "version", Value: 1},
						{Key: "light", Value: orderedMode(l)},
						{Key: "dark", Value: orderedMode(d)},
						{Key: "updatedAt", Value: time.Now().UTC()},
					}}},
					options.Update().SetUpsert(true))
			}
		}
		res.JSON(200, []byte(themeJSON(l, d)))
	}
}

// orderedMode — canonical key order (COLOR_KEYS then fontFamily,
// fontSize, radius) as an ordered bson.D for storage.
func orderedMode(m map[string]any) bson.D {
	d := bson.D{}
	for _, k := range colorKeys {
		d = append(d, bson.E{Key: k, Value: m[k]})
	}
	d = append(d, bson.E{Key: "fontFamily", Value: m["fontFamily"]})
	d = append(d, bson.E{Key: "fontSize", Value: m["fontSize"]})
	d = append(d, bson.E{Key: "radius", Value: m["radius"]})
	return d
}

func jsEscapeErr(s string) string {
	return strings.NewReplacer(`"`, `\"`, `\`, `\\`).Replace(s)
}

// clearTheme — DELETE: site admin → delete → 200 {"ok":true}.
func clearTheme(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		if a.Mongo != nil {
			ctx := cxt.Req.Context()
			db, err := a.Mongo.DB(ctx)
			if err == nil {
				_, _ = db.Collection("hubthemes").DeleteOne(ctx,
					bson.D{{Key: "documentId", Value: "default"}})
			}
		}
		res.JSON(200, []byte(`{"ok":true}`))
	}
}

// themeSnapshotJSON — the ol-hub-theme page meta (Node: hubTheme ?
// JSON.stringify(hubTheme) : 'null').
func themeSnapshotJSON(a *core.App, ctx context.Context) string {
	doc, err := loadThemeDoc(a, ctx)
	if err != nil || doc == nil {
		return "null"
	}
	ver := 1
	if n, ok := numValue(doc.Version); ok {
		ver = n
	}
	return fmt.Sprintf(`{"version":%d,"light":%s,"dark":%s}`,
		ver, modeFromStored(doc.Light), modeFromStored(doc.Dark))
}

// ---------- /api/hub/health ----------

var processStart = time.Now()

// hubHealth — Node HubController.hubHealth (ordered keys). The gate
// normalizes the implementation-dependent fields (comment, package).
func hubHealth(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		appName := os.Getenv("APP_NAME")
		if appName == "" {
			appName = "OlliTeX"
		}
		arch := runtime.GOARCH
		switch arch {
		case "amd64":
			arch = "x64"
		case "arm64":
			arch = "arm64"
		}
		var b strings.Builder
		b.WriteString(`{"ok":true`)
		b.WriteString(`,"generatedAt":"` + time.Now().UTC().Format("2006-01-02T15:04:05.000Z") + `"`)
		b.WriteString(`,"uptimeSec":` + strconv.Itoa(int(time.Since(processStart).Seconds())) +
			`,"platform":{"node":"` + runtime.Version() + `","arch":"` + arch +
			`","pid":` + strconv.Itoa(os.Getpid()) + `,"os":"` + runtime.GOOS + `"}`)
		// instance: Settings.appName (env APP_NAME || 'OlliTeX'),
		// Settings.env ('server-ce' in CE defaults), siteUrl,
		// overleaf (SaaS flag; null in CE → false).
		b.WriteString(`,"instance":{"appName":` + jsString(appName) +
			`,"env":"server-ce","siteUrl":` + jsString(cxt.SiteURL) + `,"overleaf":false}`)
		b.WriteString(`,"mongo":` + mongoProbe(a, cxt.Req.Context()) +
			`,"featureGates":` + featureGates() + `}`)
		res.JSON(200, []byte(b.String()))
	}
}

// mongoProbe — Node: mongoose readyState + admin ping. Go: driver ping;
// failures mirror the Node catch shape
// ({"readyState":null,"state":"probe-failed","error":"…"}).
func mongoProbe(a *core.App, ctx context.Context) string {
	if a.Mongo == nil {
		return `{"readyState":null,"state":"probe-failed","error":"mongodb not configured"}`
	}
	pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(pctx)
	if err != nil {
		return `{"readyState":null,"state":"probe-failed","error":` + jsString(err.Error()) + `}`
	}
	t0 := time.Now()
	if err := db.Client().Ping(pctx, nil); err != nil {
		return `{"readyState":null,"state":"probe-failed","error":` + jsString(err.Error()) + `}`
	}
	return fmt.Sprintf(`{"readyState":2,"state":"connected","pingMs":%d}`,
		int(time.Since(t0)/time.Millisecond))
}

// featureGates — mirrors HubController.hubHealth featureGates derivation
// (env-pinned 2026-09-16; the admin settings file may override the
// *DisabledByAdmin flags / llmApiUrl+Key):
//
//	llmEnabled:            LLM_ENABLED === 'true'
//	llmAdminEnabled:       !adminFile.llmDisabledByAdmin
//	llmAllowUserSettings:  LLM_ALLOW_USER_SETTINGS === 'true'
//	llmServerConfigured:   (file.llmApiUrl && file.llmApiKey) ||
//	                      (LLM_API_URL && LLM_API_KEY)
//	zotero:                ZOTERO_CLIENT_KEY && ZOTERO_CLIENT_SECRET
//	webdav:                WEBDAV_ENABLED === 'true' (!!Settings.webdav)
//	mendeley:              MENDELEY_CLIENT_ID
//	dropbox:               DROPBOX_APP_KEY && DROPBOX_APP_SECRET
//	githubSync:            GITHUBSYNC_CLIENT_ID && GITHUBSYNC_CLIENT_SECRET
//	languageToolAvailable: !file.languageToolDisabledByAdmin &&
//	                      (LANGUAGE_TOOL_URL | LANGUAGE_TOOL_HOST |
//	                       LANGUAGE_TOOL_PORT | LANGUAGETOOL_URL)
//	email:                 SMTP_URL (Settings.email.SMTP_URL)
func featureGates() string {
	admin := readLLMAdminFile()
	get := os.Getenv
	vals := map[string]bool{
		"llmEnabled":           get("LLM_ENABLED") == "true",
		"llmAdminEnabled":      !asBool(admin, "llmDisabledByAdmin"),
		"llmAllowUserSettings": get("LLM_ALLOW_USER_SETTINGS") == "true",
		"llmServerConfigured": (adminStr(admin, "llmApiUrl") != "" && adminStr(admin, "llmApiKey") != "") ||
			(get("LLM_API_URL") != "" && get("LLM_API_KEY") != ""),
		"zotero":     get("ZOTERO_CLIENT_KEY") != "" && get("ZOTERO_CLIENT_SECRET") != "",
		"webdav":     get("WEBDAV_ENABLED") == "true",
		"mendeley":   get("MENDELEY_CLIENT_ID") != "",
		"dropbox":    get("DROPBOX_APP_KEY") != "" && get("DROPBOX_APP_SECRET") != "",
		"githubSync": get("GITHUBSYNC_CLIENT_ID") != "" && get("GITHUBSYNC_CLIENT_SECRET") != "",
		"languageToolAvailable": !asBool(admin, "languageToolDisabledByAdmin") &&
			(get("LANGUAGE_TOOL_URL") != "" || get("LANGUAGE_TOOL_HOST") != "" ||
				get("LANGUAGE_TOOL_PORT") != "" || get("LANGUAGETOOL_URL") != ""),
		"email": get("SMTP_URL") != "",
	}
	order := []string{
		"llmEnabled", "llmAdminEnabled", "llmAllowUserSettings",
		"llmServerConfigured", "zotero", "webdav", "mendeley", "dropbox",
		"githubSync", "languageToolAvailable", "email",
	}
	parts := make([]string, 0, len(order))
	for _, k := range order {
		if vals[k] {
			parts = append(parts, `"`+k+`":true`)
		} else {
			parts = append(parts, `"`+k+`":false`)
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// readLLMAdminFile — Node readAdminSettings (LLMAdminController.mjs):
// LLM_ADMIN_SETTINGS_PATH || /var/lib/overleaf/data/llm-admin-settings.json
// (null on read/parse failure — the env-only view).
func readLLMAdminFile() map[string]any {
	p := os.Getenv("LLM_ADMIN_SETTINGS_PATH")
	if p == "" {
		p = "/var/lib/overleaf/data/llm-admin-settings.json"
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	v := map[string]any{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}

func asBool(m map[string]any, k string) bool {
	if m == nil {
		return false
	}
	b, _ := m[k].(bool)
	return b
}

func adminStr(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	s, _ := m[k].(string)
	return s
}

// ---------- /api/hub/notes ----------

// releaseNotes — Node getReleaseNotes: docs/RELEASE_NOTES.md (6 levels up
// from the module dir → /overleaf/docs/RELEASE_NOTES.md in the container),
// 200 text/markdown; charset=utf-8; 404 {"error":"release notes not
// available"} when the file is absent (other deployments).
const defaultReleaseNotesPath = "/overleaf/docs/RELEASE_NOTES.md"

func releaseNotes() func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		p := os.Getenv("RELEASE_NOTES_PATH")
		if p == "" {
			p = defaultReleaseNotesPath
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			res.JSON(404, []byte(`{"error":"release notes not available"}`))
			return
		}
		w := res.W
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("ETag", core.EtagWeakBody(string(raw)))
		w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
		w.WriteHeader(200)
		_, _ = w.Write(raw)
	}
}

// loadUserDoc — Node User.findById(userId, 'ace email first_name last_name').
// Returns the doc (for the other meta builders) AND the stored `ace` as
// raw BSON (key order preserved — the hub ol-user meta is the stored ace
// serialized verbatim, user-specific fields and all; pinned live: the
// admin user's ace carries syntaxValidation + overallTheme, the member's
// does not). The Go bson→map decode loses order, so the ace is decoded
// hubAceCanonical — the fixed post-provider key order of the hydrated
// ace subdocument (User.mjs ace schema order, providers excluded — they
// are always pinned first by Node, empirically pinned live 2026-09-16).
var hubAceCanonical = []string{
	"mode", "theme", "overallTheme", "lightTheme", "darkTheme",
	"fontSize", "autoComplete", "autoPairDelimiters",
	"spellCheckLanguage", "pdfViewer", "syntaxValidation",
	"fontFamily", "lineHeight", "previewTabs", "mathPreview",
	"breadcrumbs", "editorTabs", "nonBlinkingCursor",
	"referencesSearchMode", "darkModePdf", "floatingMenu",
	"customKeybindings",
}

// hubAceDefaults — schema-default JSON literals for ace paths that have
// them (no-default paths: overallTheme, syntaxValidation, fontFamily,
// lineHeight — omitted when not stored).
var hubAceDefaults = map[string]string{
	"mode":                `"none"`,
	"theme":               `"textmate"`,
	"lightTheme":          `"textmate"`,
	"darkTheme":           `"overleaf_dark"`,
	"fontSize":            "12",
	"autoComplete":        "true",
	"autoPairDelimiters":  "true",
	"spellCheckLanguage":  `"en"`,
	"pdfViewer":           `"pdfjs"`,
	"previewTabs":         "false",
	"mathPreview":         "true",
	"breadcrumbs":         "false",
	"editorTabs":          "true",
	"nonBlinkingCursor":   "false",
	"referencesSearchMode": `"advanced"`,
	"darkModePdf":         "false",
	"floatingMenu":        "true",
	"customKeybindings":   "{}",
}

// hubAceProviders — the three ref-provider blocks, always rendered first.
var hubAceProviders = []string{"zotero", "mendeley", "papers"}

// hubAceProviderDefault — the schema-default provider block (member
// fixture shape, pinned live): enabled:true, disablePersonalLibrary:false,
// groups:[] — in that key order.
const hubAceProviderDefault = `{"enabled":true,"disablePersonalLibrary":false,"groups":[]}`

// strOf — best-effort string extraction for scalar doc fields.
func strOf(v any) string {
	t, _ := v.(string)
	return t
}

func serializeHubUser(uid, email string, doc map[string]any, ace bson.Raw) string {
	els, elErr := ace.Elements()
	getEl := func(k string) (bson.RawValue, bool) {
		for _, el := range els {
			if el.Key() == k {
				return el.Value(), true
			}
		}
		return bson.RawValue{}, false
	}
	stored := func(k string) (string, bool) {
		v, ok := getEl(k)
		if !ok || elErr != nil {
			return "", false
		}
		if v.Type == bson.TypeNull || v.Type == bson.TypeUndefined {
			return "", false
		}
		return bsonToJSONValue(v), true
	}
	parts := make([]string, 0, len(hubAceCanonical)+len(hubAceProviders))
	for _, p := range hubAceProviders {
		v, ok := getEl(p)
		if ok && v.Type == bson.TypeEmbeddedDocument && len(v.Document()) > 0 {
			// stored provider subdoc — its own stored key order is
			// authoritative (pinned: admin stored order enabled,groups,
			// disablePersonalLibrary renders verbatim).
			parts = append(parts, `"`+p+`":`+bsonToJSON(v.Document()))
		} else {
			parts = append(parts, `"`+p+`":`+hubAceProviderDefault)
		}
	}
	known := map[string]bool{}
	for _, k := range hubAceCanonical {
		known[k] = true
	}
	for _, k := range hubAceProviders {
		known[k] = true
	}
	for _, k := range hubAceCanonical {
		if sv, ok := stored(k); ok {
			parts = append(parts, `"`+k+`":`+sv)
			continue
		}
		if d, hasD := hubAceDefaults[k]; hasD {
			parts = append(parts, `"`+k+`":`+d)
			continue
		}
		// not stored and no schema default → omitted (Node behaviour)
	}
	for _, el := range els {
		if known[el.Key()] {
			continue
		}
		v := el.Value()
		if v.Type == bson.TypeNull || v.Type == bson.TypeUndefined {
			continue
		}
		parts = append(parts, `"`+jesc(el.Key())+`":`+bsonToJSONValue(v))
	}
	var b strings.Builder
	b.WriteString(`{"ace":{`)
	b.WriteString(strings.Join(parts, ","))
	b.WriteString(`},"_id":"` + jesc(uid) + `"`)
	b.WriteString(`,"email":"` + jesc(email) + `"`)
	b.WriteString(`,"first_name":"` + jesc(strOf(doc["first_name"])) + `"`)
	b.WriteString(`,"last_name":"` + jesc(strOf(doc["last_name"])) + `"}`)
	return b.String()
}

// bsonToMap — ordered-elements → Go map (order not preserved; use only
// for the sparse canonical path where the output order is fixed).
func bsonToMap(raw bson.Raw) map[string]any {
	m := map[string]any{}
	if len(raw) == 0 {
		return m
	}
	vals, err := raw.Elements()
	if err != nil {
		return m
	}
	for _, el := range vals {
		m[el.Key()] = bsonValueToAny(el.Value())
	}
	return m
}

func bsonValueToAny(v bson.RawValue) any {
	switch v.Type {
	case bson.TypeEmbeddedDocument:
		return bsonToMap(v.Document())
	case bson.TypeArray:
		vals, _ := v.Array().Elements()
		out := []any{}
		for _, el := range vals {
			out = append(out, bsonValueToAny(el.Value()))
		}
		return out
	case bson.TypeString:
		return v.StringValue()
	case bson.TypeDouble:
		return v.Double()
	case bson.TypeInt32:
		return int64(v.Int32())
	case bson.TypeInt64:
		return v.Int64()
	case bson.TypeBoolean:
		return v.Boolean()
	case bson.TypeNull, bson.TypeUndefined:
		return nil
	}
	return nil
}

func anyJSON(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return jsString(t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(t)
	case int32:
		return strconv.FormatInt(int64(t), 10)
	case int64:
		return strconv.FormatInt(t, 10)
	case uint32:
		return strconv.FormatUint(uint64(t), 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bson.D:
		return bsonDJSON(t)
	case map[string]any:
		parts := []string{}
		for k, vv := range t {
			parts = append(parts, `"`+jesc(k)+`":`+anyJSON(vv))
		}
		return "{" + strings.Join(parts, ",") + "}"
	case []any:
		parts := []string{}
		for _, vv := range t {
			parts = append(parts, anyJSON(vv))
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	return "null"
}

// jesc — JSON string content escaping without the surrounding quotes
// (same rules as jsString; JS JSON.stringify semantics).
func jesc(s string) string {
	out := jsString(s)
	return out[1 : len(out)-1]
}

// bsonToJSON — BSON value → JSON string, preserving DOCUMENT KEY ORDER
// (the Go bson.M decode loses order; the hub ol-user meta is the stored
// ace byte-verbatim — pinned live against both fixture users).
func bsonToJSON(raw bson.Raw) string {
	if len(raw) == 0 {
		return "null"
	}
	vals, err := raw.Elements()
	if err != nil {
		return "null"
	}
	parts := make([]string, 0, len(vals))
	for _, el := range vals {
		parts = append(parts, `"`+jesc(el.Key())+`":`+bsonToJSONValue(el.Value()))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func bsonJSONArray(raw bson.Raw) string {
	if len(raw) == 0 {
		return "null"
	}
	vals, err := raw.Elements()
	if err != nil {
		return "null"
	}
	parts := make([]string, 0, len(vals))
	for _, el := range vals {
		parts = append(parts, bsonToJSONValue(el.Value()))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func bsonToJSONValue(v bson.RawValue) string {
	if v.Type == bson.TypeEmbeddedDocument {
		return bsonToJSON(v.Document())
	}
	if v.Type == bson.TypeArray {
		return bsonJSONArray(v.Array())
	}
	switch v.Type {
	case bson.TypeString:
		s, ok := v.StringValueOK()
		if !ok {
			return "null"
		}
		return jsString(s)
	case bson.TypeDouble:
		f, ok := v.DoubleOK()
		if !ok {
			return "null"
		}
		return strconv.FormatFloat(f, 'f', -1, 64)
	case bson.TypeInt32:
		n, ok := v.Int32OK()
		if !ok {
			return "null"
		}
		return strconv.FormatInt(int64(n), 10)
	case bson.TypeInt64:
		n, ok := v.Int64OK()
		if !ok {
			return "null"
		}
		return strconv.FormatInt(n, 10)
	case bson.TypeBoolean:
		if b, ok := v.BooleanOK(); ok {
			if b {
				return "true"
			}
			return "false"
		}
		return "null"
	case bson.TypeObjectID:
		if o, ok := v.ObjectIDOK(); ok {
			return jsString(o.Hex())
		}
		return "null"
	case bson.TypeDateTime:
		if t, ok := v.DateTimeOK(); ok {
			return jsString(time.UnixMilli(int64(t)).UTC().Format("2006-01-02T15:04:05.000Z"))
		}
		return "null"
	case bson.TypeNull, bson.TypeUndefined:
		return "null"
	}
	return "null"
}

func boolOf(v any) bool {
	b, _ := v.(bool)
	return b
}

// loadUserDoc — Node User.findById(userId, 'ace email first_name last_name'):
// the doc (other meta builders) + the stored `ace` as raw BSON (see
// serializeHubUser; the projection above is for parity with Node's read
// path — the ace is decoded separately to keep key order).
func loadUserDoc(a *core.App, ctx context.Context, uid string) (map[string]any, bson.Raw, bool) {
	if a.Mongo == nil || uid == "" {
		return nil, nil, false
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, nil, false
	}
	oid, err := primitive.ObjectIDFromHex(uid)
	if err != nil {
		return nil, nil, false
	}
	var aceRes struct {
		Ace *bson.Raw `bson:"ace"`
	}
	var aceRaw bson.Raw
	if err := db.Collection("users").FindOne(ctx,
		bson.D{{Key: "_id", Value: oid}},
		options.FindOne().SetProjection(bson.D{{Key: "ace", Value: 1}})).Decode(&aceRes); err == nil && aceRes.Ace != nil {
		aceRaw = *aceRes.Ace
	}
	doc := map[string]any{}
	if err := db.Collection("users").FindOne(ctx,
		bson.D{{Key: "_id", Value: oid}}).Decode(&doc); err != nil {
		return nil, nil, false
	}
	return doc, aceRaw, true
}
