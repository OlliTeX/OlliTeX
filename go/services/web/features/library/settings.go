package library

import (
	"encoding/json"
	"time"
)

// ---------- UserSettingsHelper.mjs port (library-page ol-userSettings) ----------
//
// The /library page carries the LEGACY (non-editor) userSettings derivation:
// `UserSettingsHelper.buildUserSettings(req, res, user)` — raw ace values
// (UNDEFINED KEYS DROP OUT under pug's JSON.stringify), a handful of
// `??`/`||` defaults, plus the 2026-03-02 system-theme cutoff. This is a
// DIFFERENT builder from the editor page's buildUserSettings (which applies
// ACE defaults) — pinned on the oracle capture:
//
//	{previewTabs:false,fontFamily:"lucida",lineHeight:"normal",
//	 overallTheme:"system",editorTabs:true,nonBlinkingCursor:false,
//	 darkModePdf:false,floatingMenu:true,customKeybindings:{}}

// systemThemeCutoff — `new Date(Date.UTC(2026, 2, 2, 12, 0, 0))`.
var systemThemeCutoff = time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC)

// legacyUserSettings — buildUserSettings(user) (user = lean users doc).
// Returns JSON exactly as JSON.stringify would emit it (undefined values →
// key dropped; explicit null/false/0 kept).
func legacyUserSettings(user map[string]any) string {
	if user == nil {
		user = map[string]any{}
	}
	ace, _ := user["ace"].(map[string]any)
	get := func(k string) (any, bool) {
		if ace == nil {
			return nil, false
		}
		v, ok := ace[k]
		return v, ok && v != nil
	}
	has := func(k string) bool {
		_, ok := get(k)
		return ok
	}
	bv := func(k, def string, d bool) string {
		// `?? def` — null/undefined → def, else the bool
		if v, ok := get(k); ok {
			if b, ok := v.(bool); ok {
				return boolS(b)
			}
		}
		return boolS(d)
	}
	sb := func(k, def string) string {
		// `|| def` — falsy (missing, "", ...) → def
		if v, ok := get(k); ok {
			if s, ok := v.(string); ok && s != "" {
				return jstr(s)
			}
		}
		return jstr(def)
	}

	parts := []string{}
	if has("mode") {
		parts = append(parts, `"mode":`+jsonVal(ace, "mode"))
	}
	for _, k := range []string{"editorTheme", "editorLightTheme", "editorDarkTheme", "pdfViewer"} {
		if has(k) {
			parts = append(parts, `"`+k+`":`+jsonVal(ace, k))
		}
	}
	if has("fontSize") {
		parts = append(parts, `"fontSize":`+jsonVal(ace, "fontSize"))
	}
	for _, k := range []string{"autoComplete", "autoPairDelimiters"} {
		if has(k) {
			parts = append(parts, `"`+k+`":`+jsonVal(ace, k))
		}
	}
	if has("syntaxValidation") {
		parts = append(parts, `"syntaxValidation":`+jsonVal(ace, "syntaxValidation"))
	}
	parts = append(parts, `"previewTabs":`+bv("previewTabs", "", false))
	parts = append(parts, `"fontFamily":`+sb("fontFamily", "lucida"))
	parts = append(parts, `"lineHeight":`+sb("lineHeight", "normal"))
	parts = append(parts, `"overallTheme":`+jstr(overallThemeOf(user, ace)))
	if has("mathPreview") {
		parts = append(parts, `"mathPreview":`+jsonVal(ace, "mathPreview"))
	}
	if has("breadcrumbs") {
		parts = append(parts, `"breadcrumbs":`+jsonVal(ace, "breadcrumbs"))
	}
	parts = append(parts, `"editorTabs":`+bv("editorTabs", "", true))
	parts = append(parts, `"nonBlinkingCursor":`+bv("nonBlinkingCursor", "", false))
	if has("referencesSearchMode") {
		parts = append(parts, `"referencesSearchMode":`+jsonVal(ace, "referencesSearchMode"))
	}
	parts = append(parts, `"darkModePdf":`+bv("darkModePdf", "", false))
	parts = append(parts, `"floatingMenu":`+bv("floatingMenu", "", true))
	parts = append(parts, `"customKeybindings":`+customKeybindingsJSON(ace))
	for _, k := range []string{"zotero", "mendeley", "papers"} {
		if has(k) {
			parts = append(parts, `"`+k+`":`+refProviderJSON(ace[k]))
		}
	}
	return "{" + stringsJoin(parts, ",") + "}"
}

// overallThemeOf — getOverall-theme(user): ace.overallTheme (non-null) else
// (signUpDate < cutoff ? ” : 'system').
func overallThemeOf(user, ace map[string]any) string {
	if ace != nil {
		if v, ok := ace["overallTheme"]; ok && v != nil {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	if sd, ok := user["signUpDate"].(time.Time); ok && sd.Before(systemThemeCutoff) {
		return ""
	}
	return "system"
}

// customKeybindingsToPlain — Mongoose Map → plain {id: string} (string
// values only); absent → {}.
func customKeybindingsJSON(ace map[string]any) string {
	var ck map[string]any
	if ace != nil {
		ck, _ = ace["customKeybindings"].(map[string]any)
	}
	if ck == nil {
		return "{}"
	}
	parts := []string{}
	for k, v := range ck {
		if s, ok := v.(string); ok {
			parts = append(parts, `"`+jescKey(k)+`":`+jstr(s))
		}
	}
	return "{" + stringsJoin(parts, ",") + "}"
}

// buildRefProviderSettings — {enabled, disablePersonalLibrary,
// groups:[{id}]} (undefined sub-keys dropped; the caller guarantees the
// setting object exists).
func refProviderJSON(v any) string {
	settings, ok := v.(map[string]any)
	if !ok {
		return "null"
	}
	parts := []string{}
	if ev, ok := settings["enabled"]; ok && ev != nil {
		parts = append(parts, `"enabled":`+anyJSON(ev))
	}
	if pv, ok := settings["disablePersonalLibrary"]; ok && pv != nil {
		parts = append(parts, `"disablePersonalLibrary":`+anyJSON(pv))
	}
	groups := []string{}
	if garr, ok := settings["groups"].([]any); ok {
		for _, g := range garr {
			gm, ok := g.(map[string]any)
			if !ok {
				continue
			}
			if id, ok := gm["id"]; ok && id != nil {
				groups = append(groups, `{"id":`+anyJSON(id)+`}`)
			}
		}
	}
	parts = append(parts, `"groups":[`+stringsJoin(groups, ",")+`]`)
	return "{" + stringsJoin(parts, ",") + "}"
}

// ---------- JSON value marshaling (Node JSON.stringify subset) ----------

func boolS(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func stringsJoin(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}

// jsonVal — the value for key k (caller already checked presence).
func jsonVal(ace map[string]any, k string) string {
	return anyJSON(ace[k])
}

// anyJSON — JSON for a driver-decoded scalar (string/bool/int*/float64/
// nil) — the ace values in play; documents are the settings sub-objects
// handled by refProviderJSON/customKeybindingsJSON.
func anyJSON(v any) string {
	switch t := v.(type) {
	case string:
		return jstr(t)
	case bool:
		return boolS(t)
	case int32:
		return itoa(int64(t))
	case int64:
		return itoa(t)
	case float64:
		return ftoa(t)
	case nil:
		return "null"
	}
	return "null"
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func ftoa(f float64) string {
	// JSON.stringify of a finite JS number: integer value → no decimal
	// point; shortest representation otherwise (encoding/json's shortest
	// form matches for realistic sizes).
	if f == float64(int64(f)) && f >= -1e15 && f <= 1e15 {
		return itoa(int64(f))
	}
	b, _ := json.Marshal(f)
	return string(b)
}

// jescKey — JSON string escaping for object keys (same set as values).
func jescKey(s string) string {
	return jstr(s)[1 : len(jstr(s))-1]
}
