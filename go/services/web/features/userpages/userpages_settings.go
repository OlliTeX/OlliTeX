package userpages

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// ---------- GET /user/settings ----------

func getSettings(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		uid := cxt.Sess.UserIDHex()
		doc, ok := userDoc(a, ctx, uid)
		if !ok {
			clearAllUserSessions(a, uid)
			res.Redirect(cxt.Req, 302, "/")
			return
		}
		email, _ := doc["email"].(string)

		// Node order: ssoError deleted; four messages popped; saml read+
		// deleted; samlBeta KEPT (rendered via ExposedSettings + meta).
		samlBeta := ""
		if s, ok := cxt.Sess.GetRaw("samlBeta"); ok {
			var v string
			if json.Unmarshal(s, &v) == nil {
				samlBeta = v
			}
		}
		cxt.Sess.Del("ssoError")
		ssoMsg, _ := popString(cxt.Sess, "ssoErrorMessage")
		syncOK, _ := popString(cxt.Sess, "projectSyncSuccessMessage")
		syncErr, _ := popString(cxt.Sess, "projectSyncErrorMessage")
		refErr, _ := popString(cxt.Sess, "referenceLinkingErrorMessage")
		cxt.Sess.Del("saml")

		hasPassword := false
		if hp, ok := doc["hashedPassword"].(string); ok && hp != "" {
			hasPassword = true
		}
		showAI := false
		if aif, ok := doc["aiFeatures"].(map[string]any); ok {
			showAI, _ = aif["enabled"].(bool)
		}

		d := pageBase(cxt, email, mustObjectID(uid).Hex())
		d.UserMetaJSON = userMetaJSON(doc, mustObjectID(uid).Hex(), email)
		d.SamlBeta = samlBeta
		d.HasPassword = hasPassword
		d.ShowAiFeatures = showAI
		d.SsoErrorMessage = ssoMsg
		d.ProjectSyncSuccessMessage = syncOK
		d.ProjectSyncErrorMessage = syncErr
		d.ReferenceLinkingErrorMessage = refErr
		views.SettingsPage(res.W, d)
		a.CommitSess(cxt.Sess, res.W)
	}
}

// userMetaJSON — the settings-page ol-user meta (Node controller order,
// undefined fields OMITTED — pug/JSON.stringify semantics):
//
//	id, isAdmin, email, allowedFreeTrial?, first_name, last_name,
//	alphaProgram, betaProgram, labsProgram,
//	features {dropbox?, github?, mendeley?, zotero?, papers?, references?}
//	(undefined entries omitted, declared order),
//	refProviders {mendeley, zotero, papers} (always boolean)

// userMetaJSON — the settings-page ol-user meta (Node controller order,
// undefined fields OMITTED — pug/JSON.stringify semantics):
//
//	id, isAdmin, email, allowedFreeTrial?, first_name, last_name,
//	alphaProgram, betaProgram, labsProgram,
//	features {dropbox?, github?, mendeley?, zotero?, papers?, references?}
//	(undefined entries omitted, declared order),
//	refProviders {mendeley, zotero, papers} (always boolean)
func userMetaJSON(doc map[string]any, uidHex, email string) string {
	get := func(k string) (any, bool) { v, ok := doc[k]; return v, ok }
	s := strings.Builder{}
	s.WriteString(`{"id":"` + uidHex + `"`)
	// Node serializeUser: user.isAdmin — the CE users doc carries it as
	// doc.isAdmin (live A/B pin U9: admin true / user false); the admin /
	// adminRoles shapes stay as fallbacks for legacy stacks.
	s.WriteString(`,"isAdmin":` + b(boolOf(doc["isAdmin"]) || str(doc["admin"]) || rolesInclude(doc["adminRoles"], "admin")))
	s.WriteString(`,"email":"` + jsonEscape(email) + `"`)
	if v, ok := get("allowedFreeTrial"); ok && v != nil {
		s.WriteString(`,"allowedFreeTrial":` + b(boolOf(v)))
	}
	s.WriteString(`,"first_name":"` + jsonEscape(asStr(doc["first_name"])) + `"`)
	s.WriteString(`,"last_name":"` + jsonEscape(asStr(doc["last_name"])) + `"`)
	s.WriteString(`,"alphaProgram":` + b(boolOf(doc["alphaProgram"])))
	s.WriteString(`,"betaProgram":` + b(boolOf(doc["betaProgram"])))
	s.WriteString(`,"labsProgram":` + b(boolOf(doc["labsProgram"])))
	features := map[string]bool{}
	if f, ok := doc["features"].(map[string]any); ok {
		for _, k := range []string{"dropbox", "github", "mendeley",
			"zotero", "papers", "references"} {
			if v, ok2 := f[k]; ok2 && v != nil {
				features[k] = boolOf(v)
			}
		}
	}
	s.WriteString(`,"features":{`)
	first := true
	for _, k := range []string{"dropbox", "github", "mendeley",
		"zotero", "papers", "references"} {
		if !features_has(k, features) {
			continue
		}
		if !first {
			s.WriteString(",")
		}
		first = false
		fmt.Fprintf(&s, `%q:%v`, k, features[k])
	}
	s.WriteString(`}`)
	refp := func(k string) bool {
		if r, ok := doc["refProviders"].(map[string]any); ok {
			v, ok2 := r[k]
			if ok2 {
				return boolOf(v)
			}
		}
		return false
	}
	s.WriteString(`,"refProviders":{"mendeley":` + b(refp("mendeley")))
	s.WriteString(`,"zotero":` + b(refp("zotero")))
	s.WriteString(`,"papers":` + b(refp("papers")) + `}`)
	s.WriteString(`}`)
	return s.String()
}

// ---------- POST /user/settings (zod-strict mirror) ----------

var allowedSettingsKeys = []string{
	"first_name", "last_name", "role", "institution", "email", "mode",
	"editorTheme", "editorLightTheme", "editorDarkTheme", "overallTheme",
	"fontSize", "autoComplete", "autoPairDelimiters", "spellCheckLanguage",
	"pdfViewer", "syntaxValidation", "previewTabs", "fontFamily", "lineHeight",
	"mathPreview", "breadcrumbs", "editorTabs", "nonBlinkingCursor",
	"referencesSearchMode", "darkModePdf", "floatingMenu",
	"customKeybindings", "zotero", "mendeley", "papers",
}

func postSettings(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		uid := cxt.Sess.UserIDHex()
		body, _, kind, ok, rawJSON := readBody(cxt.Req)
		switch {
		case !ok && (kind == "scalar" || kind == "array"):
			// Node: array/null reach zod → 400 JSON error; bare scalars hit
			// the body-parser first (P3.1 pin: 400 {}, no web headers).
			if kind == "array" {
				res.JSON(400, []byte(validationError(
					`Invalid input: expected object, received array at "body"`)))
			} else if kind == "null" {
				res.JSON(400, []byte(validationError(
					`Invalid input: expected object, received null at "body"`)))
			} else {
				res.BareWrite(400, []byte("{}"))
			}
			return
		}

		// Validation contract (pinned against Node live): field-level issues
		// in schema definition order, then the unrecognized-key issue — all
		// reported, joined with "; ". validateSettings bundles both.
		if msg := validateSettings(body, rawJSON); msg != "" {
			res.JSON(400, []byte(validationError(msg)))
			return
		}

		doc, ok := userDoc(a, ctx, uid)
		if !ok {
			res.SendStatus(500)
			return
		}

		// ---- apply (Node handler order) ----
		set := bson.D{}
		strSet := func(key, docKey string, v any) {
			if s, isStr := v.(string); isStr {
				set = append(set, bson.E{Key: docKey, Value: strings.TrimFunc(sanitizeCtrl(s), jStrTrim)})
			}
		}
		strSet("first_name", "first_name", body["first_name"])
		strSet("last_name", "last_name", body["last_name"])
		strSet("role", "role", body["role"])
		strSet("institution", "institution", body["institution"])

		aceSet := func(aceKey string, v any) {
			if _, present := body[aceKey]; present {
				set = append(set, bson.E{Key: "ace." + aceKey, Value: v})
			}
		}
		plain := map[string]string{
			"mode": "mode", "editorTheme": "theme",
			"editorLightTheme": "lightTheme", "editorDarkTheme": "darkTheme",
			"overallTheme": "overallTheme", "fontSize": "fontSize",
			"autoComplete": "autoComplete", "autoPairDelimiters": "autoPairDelimiters",
			"spellCheckLanguage": "spellCheckLanguage", "pdfViewer": "pdfViewer",
			"syntaxValidation": "syntaxValidation", "fontFamily": "fontFamily",
			"lineHeight": "lineHeight", "mathPreview": "mathPreview",
		}
		for f, d := range plain {
			if v, ok2 := body[f]; ok2 && v != nil {
				set = append(set, bson.E{Key: "ace." + d, Value: v})
			}
		}
		_ = aceSet
		// Boolean()-coerced six (any value → JS Boolean, never rejected).
		for _, f := range []string{"previewTabs", "breadcrumbs", "editorTabs",
			"nonBlinkingCursor", "darkModePdf", "floatingMenu"} {
			if v, ok2 := body[f]; ok2 && v != nil {
				set = append(set, bson.E{Key: "ace." + f, Value: jsBool(v)})
			}
		}
		if v, ok2 := body["referencesSearchMode"]; ok2 && v != nil {
			mode := "advanced"
			if s, isStr := v.(string); isStr && s == "simple" {
				mode = "simple"
			}
			set = append(set, bson.E{Key: "ace.referencesSearchMode", Value: mode})
		}
		if v, ok2 := body["customKeybindings"]; ok2 && v != nil {
			if isJSONObj(v) {
				kkeys := []string{}
				var kb map[string]any
				if rawJSON != nil {
					// recover the customKeybindings object in JSON order
					if sub, okk := subObjectRaw(rawJSON, "customKeybindings"); okk {
						if m2, k2, ok3 := orderedPairs(sub); ok3 {
							kb, kkeys = m2, k2
						}
					}
				}
				bindings := map[string]string{}
				for idx, k := range kkeys {
					if idx >= 64 {
						break // Node: .slice(0, 64) BEFORE the filter
					}
					if k == "" || kb == nil {
						continue
					}
					val := kb[k]
					target := ""
					if val == nil {
						target = ""
					} else if s, isStr := val.(string); isStr && len(s) > 0 && len(s) <= 24 {
						target = s
					} else {
						continue
					}
					bindings[k] = target
				}
				set = append(set, bson.E{Key: "ace.customKeybindings", Value: bindings})
			}
		}
		// ref providers: shallow merge into existing ace.<p>.
		for _, p := range []string{"zotero", "mendeley", "papers"} {
			v, ok2 := body[p]
			if !ok2 || v == nil {
				continue
			}
			obj, isObj := v.(map[string]any)
			if !isObj {
				continue // validation already rejected non-objects
			}
			existing := map[string]any{}
			if ace, ok3 := doc["ace"].(map[string]any); ok3 {
				if cur, ok4 := ace[p].(map[string]any); ok4 {
					for k, val := range cur {
						existing[k] = val
					}
				}
			}
			for k, val := range obj {
				// zod strips unknown provider keys before save — overlay
				// only the three schema keys.
				if refProviderAllowed[k] {
					existing[k] = val
				}
			}
			set = append(set, bson.E{Key: "ace." + p, Value: existing})
		}

		db, err := a.Mongo.DB(ctx)
		if err != nil {
			res.SendStatus(500)
			return
		}
		if len(set) > 0 {
			if _, uerr := db.Collection("users").UpdateOne(ctx,
				bson.D{{Key: "_id", Value: mustObjectID(uid)}},
				bson.D{{Key: "$set", Value: set}}); uerr != nil {
				res.SendStatus(500)
				return
			}
		}

		// ---- email branch (after save — Node order) ----
		// Node: newEmail = body.email?.trim().toLowerCase();
		//   absent/non-string | === user.email | extAuth(systemUsed) → 200 OK
		//   no '@' (INCLUDING the empty string) → 400 sendStatus
		//   taken by another account → 409 HttpErrorHandler.conflict
		var newEmail *string
		if v, ok2 := body["email"]; ok2 {
			if s, isStr := v.(string); isStr {
				t := strings.ToLower(strings.TrimFunc(sanitizeCtrl(s), jStrTrim))
				newEmail = &t
			}
		}
		currentEmail, _ := doc["email"].(string)
		firstName := asStr(doc["first_name"])
		lastName := asStr(doc["last_name"])
		if n, isStr := body["first_name"].(string); isStr {
			firstName = strings.TrimFunc(sanitizeCtrl(n), jStrTrim)
		}
		if n, isStr := body["last_name"].(string); isStr {
			lastName = strings.TrimFunc(sanitizeCtrl(n), jStrTrim)
		}

		if newEmail == nil || *newEmail == currentEmail {
			sessionUserUpdate(cxt.Sess, map[string]any{
				"first_name": firstName,
				"last_name":  lastName,
			})
			res.SendStatus(200)
			return
		}
		if !strings.Contains(*newEmail, "@") {
			res.SendStatus(400)
			return
		}
		{
			// ensureUniqueEmailAddress → 409 (pinned; no audit, no write).
			taken := emailTakenByOther(db, ctx, *newEmail, uid)
			if taken {
				msg := "This email address is already associated with a different Overleaf account."
				if core.AcceptsJSON(cxt.Req) {
					res.JSON(409, []byte(`{"message":"`+jsonEscape(msg)+`"}`))
				} else {
					res.PlainText(409, "conflict")
				}
				return
			}
			// Full changeEmailAddress success path: deferred to the
			// UserEmails family (WEB_GO_PLAN P3.3 note). Node would 200 with
			// mails; a fresh valid email cannot be pinned without that path,
			// so Go answers 500 — the gate only drives the pinned 200/400/409
			// states + the taken-409.
			res.SendStatus(500)
			return
		}
	}
}

// emailTakenByOther — ensureUniqueEmailAddress via getUserByAnyEmail:
// the email matches another user's MAIN email or any of their emails.*
// entries (pinned 409 state: e2e-admin owns e2e-admin@e2e.test).

// emailTakenByOther — ensureUniqueEmailAddress via getUserByAnyEmail:
// the email matches another user's MAIN email or any of their emails.*
// entries (pinned 409 state: e2e-admin owns e2e-admin@e2e.test).
func emailTakenByOther(db *mongo.Database, ctx context.Context, email, selfUID string) bool {
	if db == nil {
		return false
	}
	cursor, err := db.Collection("users").Find(ctx, bson.D{
		{Key: "$and", Value: bson.A{
			bson.D{{Key: "$or", Value: bson.A{
				bson.D{{Key: "email", Value: email}},
				bson.D{{Key: "emails.email", Value: email}},
			}}},
		}},
	})
	if err != nil {
		return false
	}
	self := mustObjectID(selfUID)
	var hits []struct {
		ID primitive.ObjectID `bson:"_id"`
	}
	if err := cursor.All(ctx, &hits); err != nil {
		return false
	}
	for _, h := range hits {
		if h.ID != self {
			return true
		}
	}
	return false
}

func isJSONObj(v any) bool {
	_, ok := v.(map[string]any)
	return ok
}

// subObjectRaw — extract the raw JSON object for key (balance scan,
// string-aware): recovers JSON key ORDER for customKeybindings.

// subObjectRaw — extract the raw JSON object for key (balance scan,
// string-aware): recovers JSON key ORDER for customKeybindings.
func subObjectRaw(raw []byte, key string) ([]byte, bool) {
	i := 0
	for {
		idx := bytes.Index(raw[i:], []byte(`"`+key+`"`))
		if idx == -1 {
			return nil, false
		}
		abs := i + idx
		end := abs + len(key) + 2
		for end < len(raw) && (raw[end] == ' ' || raw[end] == '\t') {
			end++
		}
		if end < len(raw) && raw[end] == ':' {
			end++
			for end < len(raw) && (raw[end] == ' ' || raw[end] == '\t') {
				end++
			}
			if end < len(raw) && raw[end] == '{' {
				depth, start := 0, end
				inStr, esc := false, false
				for j := start; j < len(raw); j++ {
					c := raw[j]
					if inStr {
						if esc {
							esc = false
						} else if c == '\\' {
							esc = true
						} else if c == '"' {
							inStr = false
						}
						continue
					}
					switch c {
					case '"':
						inStr = true
					case '{':
						depth++
					case '}':
						depth--
						if depth == 0 {
							return raw[start : j+1], true
						}
					}
				}
			}
		}
		i = abs + 1
	}
}

// sessionUserUpdate — Node SessionManager.setInSessionUser: patch the
// session doc's passport.user (best effort; parity state for re-login).

// sessionUserUpdate — Node SessionManager.setInSessionUser: patch the
// session doc's passport.user (best effort; parity state for re-login).
func sessionUserUpdate(s *core.Session, patch map[string]any) {
	if s == nil {
		return
	}
	raw, ok := s.GetRaw("passport")
	if !ok {
		return
	}
	var passport map[string]any
	if json.Unmarshal(raw, &passport) != nil {
		return
	}
	user, ok := passport["user"].(map[string]any)
	if !ok {
		return
	}
	for k, v := range patch {
		user[k] = v
	}
	merged, err := json.Marshal(map[string]any{"user": user})
	if err != nil {
		return
	}
	s.Set("passport", json.RawMessage(merged))
}

// ---------- zod-strict validation (Node oracle: UserController.mjs) ----------
// updateUserSettingsSchema + parseReq (REQ_VALIDATION_MODE=enforce-log).
// All issues are reported (not just the first), joined with "; ":
// field issues in schema definition order, then the unrecognized-key
// (strict) issue. Messages pinned live (P3.3 battery + corner probes).

// ---------- zod-strict validation (Node oracle: UserController.mjs) ----------
// updateUserSettingsSchema + parseReq (REQ_VALIDATION_MODE=enforce-log).
// All issues are reported (not just the first), joined with "; ":
// field issues in schema definition order, then the unrecognized-key
// (strict) issue. Messages pinned live (P3.3 battery + corner probes).

func validateSettings(body map[string]any, rawJSON []byte) string {
	var issues []string
	add := func(msg string) { issues = append(issues, msg) }
	invalidStr := func(v any, at string) {
		add(`Invalid input: expected string, received ` + zodType(v) + ` at "` + at + `"`)
	}

	allowed := map[string]bool{}
	for _, k := range allowedSettingsKeys {
		allowed[k] = true
	}

	for _, f := range allowedSettingsKeys { // schema definition order
		v, has := body[f]
		if !has {
			continue
		}
		at := "body." + f
		switch {
		case f == "first_name" || f == "last_name":
			// z.string().max(255).nullish() — null valid
			if s, isStr := v.(string); isStr {
				if len(s) > 255 {
					add(`Too big: expected string to have <=255 characters at "` + at + `"`)
				}
			} else if v != nil {
				invalidStr(v, at)
			}
		case coerceSix[f]:
			// z.coerce.boolean() — any value passes validation
		case f == "fontSize":
			if _, isNum := v.(float64); !isNum {
				add(`Invalid input: expected number, received ` + zodType(v) + ` at "` + at + `"`)
			}
		case f == "autoComplete" || f == "autoPairDelimiters" ||
			f == "syntaxValidation" || f == "mathPreview":
			if _, isBool := v.(bool); !isBool {
				add(`Invalid input: expected boolean, received ` + zodType(v) + ` at "` + at + `"`)
			}
		case f == "customKeybindings":
			if v == nil {
				break // nullish
			}
			obj, isObj := v.(map[string]any)
			if !isObj {
				add(`Invalid input: expected record, received ` + zodType(v) + ` at "` + at + `"`)
				break
			}
			_ = obj
			if sub, okk := subObjectRaw(rawJSON, "customKeybindings"); okk {
				if m2, k2, ok3 := orderedPairs(sub); ok3 {
					for _, k := range k2 {
						val := m2[k]
						if val == nil {
							continue
						}
						if _, isStr := val.(string); !isStr {
							t := zodType(val)
							add(`Invalid input: expected string, received ` + t +
								` at "` + at + `.` + k + `" or Invalid input: expected null, received ` + t +
								` at "` + at + `.` + k + `"`)
						}
					}
				}
			}
		case f == "zotero" || f == "mendeley" || f == "papers":
			issues = append(issues, refProviderIssues(v, rawJSON, f)...)
		default:
			// z.string().optional() — null is INVALID here
			if _, isStr := v.(string); !isStr {
				invalidStr(v, at)
			}
		}
	}

	// unrecognized keys — AFTER all field issues, body insertion order.
	var unknown []string
	if rawJSON != nil {
		if _, k2, ok := orderedPairs(rawJSON); ok {
			for _, k := range k2 {
				if !allowed[k] {
					unknown = append(unknown, k)
				}
			}
		}
	}
	if len(unknown) == 1 {
		add(`Unrecognized key: "` + unknown[0] + `" at "body"`)
	} else if len(unknown) > 1 {
		parts := make([]string, len(unknown))
		for i, k := range unknown {
			parts[i] = `"` + k + `"`
		}
		add("Unrecognized keys: " + strings.Join(parts, ", ") + ` at "body"`)
	}
	if len(issues) == 0 {
		return ""
	}
	return strings.Join(issues, "; ")
}

// refProviderAllowed — refProviderSettingsSchema (z.strictObject) keys.

// refProviderAllowed — refProviderSettingsSchema (z.strictObject) keys.
var refProviderAllowed = map[string]bool{
	"enabled":                true,
	"groups":                 true,
	"disablePersonalLibrary": true,
}

// coerceSix — z.coerce.boolean() settings (never rejected by validation).

// coerceSix — z.coerce.boolean() settings (never rejected by validation).
var coerceSix = map[string]bool{
	"previewTabs":       true,
	"breadcrumbs":       true,
	"editorTabs":        true,
	"nonBlinkingCursor": true,
	"darkModePdf":       true,
	"floatingMenu":      true,
}

// refProviderIssues — z.strictObject({enabled?: bool; groups?:
// [{id: string}] — z.object items STRIP unknown keys;
// disablePersonalLibrary?: bool}).optional().

// refProviderIssues — z.strictObject({enabled?: bool; groups?:
// [{id: string}] — z.object items STRIP unknown keys;
// disablePersonalLibrary?: bool}).optional().
func refProviderIssues(v any, rawJSON []byte, key string) []string {
	at := "body." + key
	var is []string
	add := func(m string) { is = append(is, m) }
	if v == nil {
		add(`Invalid input: expected object, received null at "` + at + `"`)
		return is
	}
	obj, isObj := v.(map[string]any)
	if !isObj {
		add(`Invalid input: expected object, received ` + zodType(v) + ` at "` + at + `"`)
		return is
	}
	// strict unknown keys (object insertion order, via raw).
	var unknown []string
	if rawJSON != nil {
		if sub, okk := subObjectRaw(rawJSON, key); okk {
			if _, k2, okk2 := orderedPairs(sub); okk2 {
				for _, k := range k2 {
					if !refProviderAllowed[k] {
						unknown = append(unknown, k)
					}
				}
			}
		}
	}
	// field issues in schema order: enabled, groups, disablePersonalLibrary
	if e, has := obj["enabled"]; has {
		if _, b := e.(bool); !b {
			add(`Invalid input: expected boolean, received ` + zodType(e) + ` at "` + at + `.enabled"`)
		}
	}
	if g, has := obj["groups"]; has {
		arr, isArr := g.([]any)
		if isArr {
			for i, it := range arr {
				imp, isObj2 := it.(map[string]any)
				if !isObj2 {
					add(`Invalid input: expected object, received ` + zodType(it) +
						fmt.Sprintf(` at "%s.groups[%d]"`, at, i))
					continue
				}
				if idv, idHas := imp["id"]; idHas {
					if _, s := idv.(string); !s {
						add(`Invalid input: expected string, received ` + zodType(idv) +
							fmt.Sprintf(` at "%s.groups[%d].id"`, at, i))
					}
				} else {
					add(`Invalid input: expected string, received undefined at ` +
						fmt.Sprintf(`"%s.groups[%d].id"`, at, i))
				}
				// item unknown keys: stripped by zod (non-strict z.object)
			}
		} else {
			add(`Invalid input: expected array, received ` + zodType(g) + ` at "` + at + `.groups"`)
		}
	}
	if d, has := obj["disablePersonalLibrary"]; has {
		if _, b := d.(bool); !b {
			add(`Invalid input: expected boolean, received ` + zodType(d) + ` at "` + at + `.disablePersonalLibrary"`)
		}
	}
	if len(unknown) == 1 {
		add(`Unrecognized key: "` + unknown[0] + `" at "` + at + `"`)
	} else if len(unknown) > 1 {
		parts := make([]string, len(unknown))
		for i, k := range unknown {
			parts[i] = `"` + k + `"`
		}
		add("Unrecognized keys: " + strings.Join(parts, ", ") + ` at "` + at + `"`)
	}
	return is
}

func zodType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

func validationError(msg string) string {
	// JSON-escape the inner message (Node JSON.stringify: escapes ", \
	// and control chars — but NOT < > &: the pinned wire for
	// `<=255 characters` keeps a literal '<').
	var b strings.Builder
	for _, r := range msg {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r < 0x20:
			b.WriteString(fmt.Sprintf(`\u%04x`, r))
		default:
			b.WriteRune(r)
		}
	}
	return `{"error":"Validation error: ` + b.String() + `","statusCode":400}`
}

// ---------- body reading (order-aware) ----------

// readBody mirrors the P3.1 pattern: (fields, keyOrder, kind, ok) + raw
// JSON (needed for ORDER-sensitive nested objects like customKeybindings).
func readBody(r *http.Request) (map[string]any, []string, string, bool, []byte) {
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return map[string]any{}, nil, "", true, nil
	}
	if trimmed == "null" {
		return nil, nil, "null", false, nil
	}
	if strings.HasPrefix(trimmed, "[") {
		return nil, nil, "array", false, nil
	}
	if strings.HasPrefix(trimmed, `"`) || trimmed == "true" || trimmed == "false" {
		return nil, nil, "scalar", false, nil
	}
	dec := json.NewDecoder(strings.NewReader(trimmed))
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, "scalar", false, nil
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return nil, nil, "scalar", false, nil
	}
	body := map[string]any{}
	var order []string
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, nil, "scalar", false, nil
		}
		key, _ := kt.(string)
		var val any
		if err := dec.Decode(&val); err != nil {
			return nil, nil, "scalar", false, nil
		}
		body[key] = val
		order = append(order, key)
	}
	return body, order, "object", true, []byte(trimmed)
}

// orderedPairs — JSON object key/value pairs in insertion order (decode
// one top-level object from raw). Used for customKeybindings where Node
// slices Object.entries order (max 64).

// orderedPairs — JSON object key/value pairs in insertion order (decode
// one top-level object from raw). Used for customKeybindings where Node
// slices Object.entries order (max 64).
func orderedPairs(raw []byte) (map[string]any, []string, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, false
	}
	d, ok := tok.(json.Delim)
	if !ok || d != '{' {
		return nil, nil, false
	}
	out := map[string]any{}
	var keys []string
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, nil, false
		}
		key, _ := kt.(string)
		var val any
		if err := dec.Decode(&val); err != nil {
			return nil, nil, false
		}
		out[key] = val
		keys = append(keys, key)
	}
	return out, keys, true
}

// ---------- JS semantics ----------

// jsBool — Boolean(value) for the six coerced settings (any input type).
