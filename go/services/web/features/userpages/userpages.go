// Package userpages ports the Node user settings + sessions family (P3.3
// flip unit):
//
//	GET    /user/settings          (login; React view)
//	POST   /user/settings          (login+csrf; zod-strict update)
//	GET    /user/sessions          (login; website-redesign view)
//	GET    /user/sessions/list     (login; JSON twin)
//	POST   /user/sessions/clear    (login+csrf; 201 + audit + mail + purge)
//
// Node ground truth: services/web/app/src/Features/User/{UserPagesController,
// UserController, UserSessionsManager, UserUpdater, UserGetter}.mjs +
// views/user/{settings,sessions}.pug. Contracts pinned live 2026-09-14
// (p33 pin report); the flip gate spec is the authority.
//
// Deliberately deferred in this leaf (WEB_GO_PLAN.md P3.3):
//   - the email-CHANGE success path (add/setDefault/remove emails + mails);
//     the 409 conflict / invalid / own-no-op branches ARE implemented;
//   - the general/400 HTML render for a 409 with Accept: text/html.
package userpages

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

// ---------- routes ----------

func Feature(a *core.App) core.Feature {
	mail := core.NewMail()
	return core.Feature{
		Name: "userpages",
		Routes: []core.Route{
			{Method: "GET", Path: "/user/settings", Handler: getSettings(a)},
			{Method: "POST", Path: "/user/settings", Handler: postSettings(a)},
			{Method: "GET", Path: "/user/sessions", Handler: getSessionsPage(a)},
			{Method: "GET", Path: "/user/sessions/list", Handler: getSessionList(a)},
			{Method: "POST", Path: "/user/sessions/clear", Handler: postClear(a, mail)},
		},
	}
}

// ---------- shared small helpers ----------

// ErrSessions500 — the Node getAllUserSessions JSON.parse fault contract
// (corrupt session doc → 500 view for ALL sessions endpoints). The
// handlers call a.Render500 directly (same shape as serveradmin).
// (Kept as a sentinel for tests.)
var ErrSessions500 = fmt.Errorf("userpages: sessions store corrupt (node parity 500)")

func render500(a *core.App, cxt *core.Cxt, res *core.Res) {
	if a.Render500 != nil {
		a.Render500(cxt, res)
		return
	}
	res.SendStatus(500)
}

func mustObjectID(hex string) primitive.ObjectID {
	oid, _ := primitive.ObjectIDFromHex(hex)
	return oid
}

// popString — Node popSessionValue: read a truthy string, delete it.
func popString(s *core.Session, key string) (string, bool) {
	raw, ok := s.GetRaw(key)
	if !ok {
		return "", false
	}
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return "", false
	}
	if v == "" {
		return "", false
	}
	s.Del(key)
	return v, true
}

func pageBase(cxt *core.Cxt, email, uid string) views.PageData {
	d := views.PageData{
		Nonce:     views.NewNonce(),
		Origin:    cxt.SiteURL,
		UserEmail: email,
		UserID:    uid,
	}
	if cxt.Sess != nil {
		d.CSRFToken = cxt.Sess.CsrfToken()
	}
	return d
}

func userDoc(a *core.App, ctx context.Context, uid string) (map[string]any, bool) {
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, false
	}
	doc := map[string]any{}
	err = db.Collection("users").FindOne(
		ctx, bson.D{{Key: "_id", Value: mustObjectID(uid)}}).Decode(&doc)
	if err != nil {
		return nil, false
	}
	return doc, true
}

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
func userMetaJSON(doc map[string]any, uidHex, email string) string {
	get := func(k string) (any, bool) { v, ok := doc[k]; return v, ok }
	s := strings.Builder{}
	s.WriteString(`{"id":"` + uidHex + `"`)
	s.WriteString(`,"isAdmin":` + b(str(doc["admin"]) || rolesInclude(doc["adminRoles"], "admin")))
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

func str(v any) bool {
	b, _ := v.(bool)
	return b
}

func rolesInclude(v any, role string) bool {
	rl, ok := v.([]any)
	if !ok {
		return false
	}
	for _, e := range rl {
		if e == role {
			return true
		}
	}
	return false
}

func asStr(v any) string {
	s, _ := v.(string)
	return s
}

func boolOf(v any) bool {
	b, _ := v.(bool)
	return b
}

func b(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func features_has(k string, m map[string]bool) bool {
	_, ok := m[k]
	return ok
}

func jsonEscape(s string) string {
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

// ---------- sessions helpers (UserSessionsManager parity) ----------

type sessionEntry struct {
	IPAddress      string `json:"ip_address"`
	SessionCreated string `json:"session_created"`
}

// allUserSessions — Node getAllUserSessions: smembers, exclude current,
// GET each, missing skipped, JSON.parse (corrupt → ok=false → 500 view),
// passport.user | legacy .user → {ip_address, session_created}.
func allUserSessions(rdb *core.RedisClient, uid, curSid string) ([]sessionEntry, bool) {
	keys, err := rdb.SMEMBERS(core.UserSessionsKey(uid))
	if err != nil {
		return nil, false
	}
	excl := core.SessionDocKey(curSid)
	var out []sessionEntry
	for _, k := range keys {
		if k == excl {
			continue
		}
		raw, found, err := rdb.GET(k)
		if err != nil || !found {
			continue // node: `if (!session) continue`
		}
		if !json.Valid([]byte(raw)) {
			return nil, false // node JSON.parse throws → 500 (pinned)
		}
		var doc map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			return nil, false
		}
		pu := nestedUser(doc, "passport", "user")
		if pu == nil {
			continue
		}
		e := sessionEntry{}
		e.IPAddress, _ = pu["ip_address"].(string)
		e.SessionCreated, _ = pu["session_created"].(string)
		out = append(out, e)
	}
	if out == nil {
		out = []sessionEntry{}
	}
	return out, true
}

// purgeSessions — Node removeSessionsFromRedis(user, retain): DEL each
// referenced doc, SREM the set, PEXPIRE refresh.
func purgeSessions(rdb *core.RedisClient, uid, retain string) int {
	keys, err := rdb.SMEMBERS(core.UserSessionsKey(uid))
	if err != nil || len(keys) == 0 {
		return 0
	}
	keep := core.SessionDocKey(retain)
	var del []string
	for _, k := range keys {
		if k != keep {
			del = append(del, k)
		}
	}
	if len(del) == 0 {
		return 0
	}
	for _, k := range del {
		_ = rdb.DEL(k)
	}
	_ = rdb.SREM(core.UserSessionsKey(uid), del...)
	_ = rdb.PEXPIRE(core.UserSessionsKey(uid), core.CookieSessionLengthMs())
	return len(del)
}

// clearAllUserSessions — Node removeSessionsFromRedis(user, null) edge
// (settingsPage: user just deleted → kill ALL tracked sessions).
func clearAllUserSessions(a *core.App, uid string) {
	if uid == "" {
		return
	}
	keys, err := a.Redis.SMEMBERS(core.UserSessionsKey(uid))
	if err != nil || len(keys) == 0 {
		return
	}
	for _, k := range keys {
		_ = a.Redis.DEL(k)
	}
	_ = a.Redis.SREM(core.UserSessionsKey(uid), keys...)
}

func nestedUser(doc map[string]json.RawMessage, first, second string) map[string]any {
	for _, name := range []string{first, second} {
		sub, ok := doc[name]
		if !ok {
			continue
		}
		var wrap struct {
			User any `json:"user"`
		}
		if json.Unmarshal(sub, &wrap) == nil && wrap.User != nil {
			if m, ok := wrap.User.(map[string]any); ok {
				return m
			}
		}
		var m map[string]any
		if json.Unmarshal(sub, &m) == nil {
			return m
		}
	}
	return nil
}

func currentUserEntry(s *core.Session) sessionEntry {
	e := sessionEntry{}
	if s == nil {
		return e
	}
	raw, ok := s.GetRaw("passport")
	if !ok {
		raw, ok = s.GetRaw("user")
		if !ok {
			return e
		}
		var m map[string]any
		if json.Unmarshal(raw, &m) == nil {
			e.IPAddress, _ = m["ip_address"].(string)
			e.SessionCreated, _ = m["session_created"].(string)
			return e
		}
	}
	var wrap struct {
		User map[string]any `json:"user"`
	}
	if json.Unmarshal(raw, &wrap) == nil && wrap.User != nil {
		e.IPAddress, _ = wrap.User["ip_address"].(string)
		e.SessionCreated, _ = wrap.User["session_created"].(string)
	}
	return e
}

// ---------- GET /user/sessions ----------

func getSessionsPage(a *core.App) func(*core.Cxt, *core.Res) {
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
		others, ok2 := allUserSessions(a.Redis, uid, cxt.Sess.SessID)
		if !ok2 {
			render500(a, cxt, res)
			return
		}
		cur := currentUserEntry(cxt.Sess)
		d := pageBase(cxt, email, mustObjectID(uid).Hex())
		d.SessionsCurrentRow = sessionRow(cur.IPAddress, cur.SessionCreated)
		d.SessionsOtherRows = otherRows(others)
		views.SessionsPage(res.W, d)
	}
}

func getSessionList(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		uid := cxt.Sess.UserIDHex()
		if _, ok := userDoc(a, ctx, uid); !ok {
			clearAllUserSessions(a, uid)
			res.Redirect(cxt.Req, 302, "/")
			return
		}
		others, ok := allUserSessions(a.Redis, uid, cxt.Sess.SessID)
		if !ok {
			render500(a, cxt, res)
			return
		}
		cur := currentUserEntry(cxt.Sess)
		body, _ := json.Marshal(map[string]any{
			"currentSession": map[string]string{
				"ip_address":      cur.IPAddress,
				"session_created": cur.SessionCreated,
			},
			"sessions": others,
		})
		res.JSON(200, body)
	}
}

// ---------- POST /user/sessions/clear ----------

func postClear(a *core.App, mail *core.Mail) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		uid := cxt.Sess.UserIDHex()
		doc, ok := userDoc(a, ctx, uid)
		if !ok {
			res.SendStatus(401)
			return
		}
		email, _ := doc["email"].(string)

		// 1) getAllUserSessions (Node order) — corrupt → 500 BEFORE audit.
		others, ok2 := allUserSessions(a.Redis, uid, cxt.Sess.SessID)
		if !ok2 {
			render500(a, cxt, res)
			return
		}

		// 2) audit entry (pinned doc shape, insertion order).
		sessDocs := make([]bson.D, 0, len(others))
		for _, e := range others {
			sessDocs = append(sessDocs, bson.D{
				{Key: "ip_address", Value: e.IPAddress},
				{Key: "session_created", Value: e.SessionCreated},
			})
		}
		entry := bson.D{
			{Key: "userId", Value: mustObjectID(uid)},
			{Key: "info", Value: bson.D{{Key: "sessions", Value: sessDocs}}},
			{Key: "initiatorId", Value: mustObjectID(uid)},
			{Key: "ipAddress", Value: core.ClientIP(cxt.Req)},
			{Key: "operation", Value: "clear-sessions"},
			{Key: "timestamp", Value: time.Now().UTC()},
			{Key: "__v", Value: 0},
		}
		db, dbErr := a.Mongo.DB(ctx)
		if db != nil {
			if _, insErr := db.Collection("userAuditLogEntries").InsertOne(ctx, entry); insErr != nil {
				log.Printf("webgo: audit insert failed: %v", insErr)
			}
		} else if dbErr != nil {
			log.Printf("webgo: audit db unavailable: %v", dbErr)
		}

		// 3) purge other sessions (keep current).
		purgeSessions(a.Redis, uid, cxt.Sess.SessID)

		// 4) security alert mail (Node: send, log on error, never 500).
		now := time.Now().UTC()
		subject := "Overleaf security note: active sessions cleared"
		desc := fmt.Sprintf("active sessions were cleared on your account %s", email)
		text := "Hi there,\n\nActive sessions cleared\n\n" +
			now.Format("Monday 2 January 2006") + " at " + now.Format("15:04") + "\n\n" +
			desc + "\n\n" +
			"Quick guide: " + siteURL() + "/learn/how-to/Keeping_your_account_secure\n\n" +
			"Thanks,\nOlliTeX Team\n"
		html := "<html><body><h1>Active sessions cleared</h1>" +
			"<p>" + now.Format("Monday 2 January 2006") + " at " + now.Format("15:04") + "</p>" +
			"<p>" + desc + "</p>" +
			"<p><a href=\"" + siteURL() + "/learn/how-to/Keeping_your_account_secure\">quick guide</a></p>" +
			"</body></html>"
		if err := mail.Send(email, subject, text, html); err != nil {
			// Node logs the SMTP failure and still 201s (pinned: mail never
			// breaks the response).
			log.Printf("webgo: sessions-clear mail to %s: %v", email, err)
		}

		res.SendStatus(201)
	}
}

func siteURL() string {
	if v := strings.TrimSpace(os.Getenv("OVERLEAF_SITE_URL")); v != "" {
		return v
	}
	return "http://127.0.0.1:7420"
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
var refProviderAllowed = map[string]bool{
	"enabled":                true,
	"groups":                 true,
	"disablePersonalLibrary": true,
}

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

func contains(ks []string, v string) bool {
	for _, k := range ks {
		if k == v {
			return true
		}
	}
	return false
}

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
func jsBool(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case float64:
		return t != 0 && t == t // NaN → false
	}
	return true
}

// sanitizeCtrl — Node sanitizeControlCharacters: [\u0000-\u001F\u007F-
// \u009F\u200B\u200C\u200D\u2060\uFEFF] → "\uXXXX" (4-hex escape text).
func sanitizeCtrl(s string) string {
	var out strings.Builder
	for _, r := range s {
		if isControlChar(r) {
			fmt.Fprintf(&out, `\u%04x`, r)
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func isControlChar(r rune) bool {
	return (r >= 0x00 && r <= 0x1F) || (r >= 0x7F && r <= 0x9F) ||
		r == 0x200B || r == 0x200C || r == 0x200D || r == 0x2060 || r == 0xFEFF
}

// jStrTrim — JS String.prototype.trim (Unicode whitespace + BOM + NBSP).
func jStrTrim(r rune) bool {
	return unicode.IsSpace(r) || r == 0x00A0 || r == 0xFEFF ||
		r == 0x2028 || r == 0x2029 || r == 0x180E
}

// ---------- moment ----------

func sessionRow(ip, createdISO string) string {
	mid, err := time.Parse(time.RFC3339, createdISO)
	return "<tr><td>" + ip + "</td><td>" + momentUTC(mid, err == nil) + " UTC</td></tr>"
}

func otherRows(in []sessionEntry) string {
	var b strings.Builder
	for i := range in {
		b.WriteString(sessionRow(in[i].IPAddress, in[i].SessionCreated))
	}
	return b.String()
}

// momentUTC — moment(t).utc().format('Do MMM YYYY, h:mm a') (pinned).
func momentUTC(t time.Time, valid bool) string {
	if !valid {
		return "Invalid date"
	}
	t = t.UTC()
	day := t.Day()
	suf := "th"
	switch {
	case day%10 == 1 && day%100 != 11:
		suf = "st"
	case day%10 == 2 && day%100 != 12:
		suf = "nd"
	case day%10 == 3 && day%100 != 13:
		suf = "rd"
	}
	mm := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun",
		"Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}[int(t.Month())-1]
	hour := t.Hour()
	ampm := "am"
	h12 := hour % 12
	if h12 == 0 {
		h12 = 12
	}
	if hour >= 12 {
		ampm = "pm"
	}
	return fmt.Sprintf("%d%s %s %d, %d:%02d %s",
		day, suf, mm, t.Year(), h12, t.Minute(), ampm)
}
