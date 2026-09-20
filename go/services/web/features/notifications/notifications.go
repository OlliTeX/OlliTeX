// Package notifications — P6.14: the /notifications/preferences family +
// /user/notification-preferences redirects + /user/send-test-email
// (services/web/modules/notifications — NotificationsPreferencesRouter.mjs /
// NotificationsPreferencesController.mjs / NotificationsPreferencesHandler.mjs /
// PreferenceNormalizer.mjs).
//
// Node oracle (2026-09-18, live-pinned on the Node leg; see WEB_GO_PLAN.md):
//
// Global (requireLogin):
//
//	GET  /notifications/preferences
//	    → 200 {"muteAllNotifications":<bool>,"notificationDelayMinutes":<int|null>,
//	      "commentOnOwnProject":…, … 12 keys …}
//	    (normalizeGlobalPreferences(doc or {}); key order pinned; delay
//	    normalizes out-of-range / wrong-type values to null)
//	POST /notifications/preferences
//	    zod { body: { muteAllNotifications: bool (REQUIRED),
//	    notificationDelayMinutes: int 1..10080 nullable optional } };
//	    200 echo {"muteAllNotifications":<bool>[,"notificationDelayMinutes":<int|null>]}
//	    (delay key only when present in the input); $set {mute, delay|null, the
//	    12 default keys} upserted on {user_id, project_id: null}.
//	    400 shapes (zod messages pinned verbatim; envelope
//	    {"error":"Validation error: <msg>","statusCode":400}):
//	      mute missing  → Invalid input: expected boolean, received undefined at "body.muteAllNotifications"
//	      mute "x"      → … received string …   mute null → … received null …
//	      delay "7"     → Invalid input: expected number, received string at "body.notificationDelayMinutes"
//	      delay 2.5     → Invalid input: expected int, received number …
//	      delay 0       → Too small: expected number to be >=1 …
//	      delay 10081   → Too big: expected number to be <=10080 …
//	    invalid-JSON / JSON-string / JSON-null bodies → 400 EMPTY {} ;
//	    JSON-array body → 400 "Invalid input: expected object, received array at \"body\""
//	    (Express/zod quirks, mirrored verbatim — all pinned).
//
// Project (requireLogin + _hasProjectAccess: owner_ref ∪ collabelator_refs ∪
// readOnly_refs ∪ tokenAccessReadAndWrite_refs ∪ tokenAccessReadOnly_refs):
//
//	GET  /notifications/preferences/project/:projectId
//	    → 200 {12 keys: projectDoc[key] ?? globalDoc[key] ?? true} +
//	      muteAllNotifications (global) APPENDED LAST (pinned order);
//	POST → 200 body exactly `null` (z.looseObject — any object; $set 12
//	      normalized bool keys upserted on {user_id, project_id});
//	    bad hex id   → 500 rendered page (Node: new ObjectId('zz') CastError)
//	    ghost id     → 404 OlliTeX rendered page (BOTH accepts, pinned)
//	    non-member   → 403 Restricted403 page (BOTH accepts, pinned — unlike
//	    the track-changes json-403 family)
//
// Legacy + email:
//
//	GET|POST /user/notification-preferences → 301 /hub#/mysettings.email
//	    (express redirect Accept matrix — core.Redirect; P6.5 precedent)
//	POST /user/send-test-email → 200 {"message":"Email Sent"} (+ one test
//	    mail to the session user's email; missing user/email → 500 view —
//	    Node: plain Error → next(err) → the 500 page)
//
// Anonymous (global chain, pinned): GET+json → 401 Unauthorized; GET bare →
// 302 /login; non-GET → 403 Forbidden. (Route.NoLogin=false.)
//
// Storage (mongo `notificationsPreferences`, migration 20251110151140):
//   - global:  { user_id, project_id: null, muteAllNotifications,
//     notificationDelayMinutes, <12 keys> }
//   - project: { user_id, project_id, <12 keys> }
// unique index user_id_1_project_id_1.

package notifications

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/features/templates"
	"ollitex/go/services/web/views"
)

// projectKeys — PreferenceNormalizer _defaultProjectPreferences key order
// (pinned; all default true).
var projectKeys = []string{
	"commentOnOwnProject",
	"commentOnInvitedProject",
	"repliesOnAuthoredThread",
	"repliesOnParticipatingThread",
	"commentResolvedOnAuthoredThread",
	"commentResolvedOnParticipatingThread",
	"commentReopenedOnAuthoredThread",
	"commentReopenedOnParticipatingThread",
	"trackedChangesOnOwnProject",
	"trackedChangesOnInvitedProject",
	"trackChangesAcceptedOnAuthoredChange",
	"trackChangesRejectedOnAuthoredChange",
}

const (
	delayMin = 1
	delayMax = 10080
)

var projectP = regexp.MustCompile(`^/notifications/preferences/project/([^/]+)/?$`)

var upsertOpts = options.Update().SetUpsert(true)

// ---------- Feature ----------------------------------------------------------

func Feature(a *core.App) core.Feature {
	mail := core.NewMail()
	return core.Feature{
		Name: "notifications",
		Routes: []core.Route{
			{Method: "GET", Path: "/notifications/preferences", Handler: hGetGlobal(a)},
			{Method: "POST", Path: "/notifications/preferences", Handler: hSaveGlobal(a)},
			{Method: "GET", Pattern: projectP, Handler: hGetProject(a)},
			{Method: "POST", Pattern: projectP, Handler: hSaveProject(a)},
			{Method: "GET", Path: "/user/notification-preferences", Handler: hRedirectHub()},
			{Method: "POST", Path: "/user/notification-preferences", Handler: hRedirectHub()},
			{Method: "POST", Path: "/user/send-test-email", Handler: hTestEmail(a, mail)},
		},
	}
}

// ---------- shared plumbing --------------------------------------------------

func ntfUID(cxt *core.Cxt) string {
	if cxt.Sess == nil {
		return ""
	}
	return cxt.Sess.UserIDHex()
}

func ntfOid(uid string) primitive.ObjectID {
	oid, err := primitive.ObjectIDFromHex(strings.ToLower(uid))
	if err != nil {
		return primitive.ObjectID{}
	}
	return oid
}

func ntfPageData(cxt *core.Cxt) views.PageData {
	d := views.PageData{Nonce: views.NewNonce(), Path: strings.TrimPrefix(cxt.Req.URL.Path, "/")}
	origin := cxt.SiteURL
	if origin == "" {
		origin = "http://" + cxt.Req.Host
	}
	d.Origin = origin
	if cxt.Sess != nil {
		d.CSRFToken = cxt.Sess.CsrfToken()
		d.UserEmail, d.UserID = core.PageUserSlots(cxt.Sess)
		// P6.13 per-user page slots (the captured page skeletons carry the
		// NAVADMIN / CANMGTPL slots — Node renders both on every page render,
		// including the error pages, per the live user).
		d.CanManageTemplateMenu = templates.SessionMenuGrant(cxt.Sess)
		if templates.SessionIsAdmin(cxt.Sess) {
			d.NavAdmin = views.AdminNavFragment
		}
	}
	return d
}

func ntfErr500(cxt *core.Cxt, res *core.Res) {
	views.Error500Page(res.W, ntfPageData(cxt))
}

func ntfDocVal(doc bson.D, key string) (interface{}, bool) {
	for _, e := range doc {
		if e.Key == key {
			return e.Value, true
		}
	}
	return nil, false
}

func ntfOidHex(v interface{}) (string, bool) {
	switch t := v.(type) {
	case string:
		return strings.ToLower(t), true
	case primitive.ObjectID:
		return t.Hex(), true
	}
	return "", false
}

// ntfBool — Node Boolean(value): true → true; everything else → false.
func ntfBool(v interface{}) bool {
	b, _ := v.(bool)
	return b
}

// ntfDelay — Node normalizeGlobalDelayMinutes on a STORED value: null → null;
// integer within 1..10080 → the int; anything else → null.
func ntfDelay(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int64:
		if i := int(n); i >= delayMin && i <= delayMax {
			return i, true
		}
	case int32:
		if i := int(n); i >= delayMin && i <= delayMax {
			return i, true
		}
	case float64:
		if i := int(n); float64(i) == n && i >= delayMin && i <= delayMax {
			return i, true
		}
	case int:
		if n >= delayMin && n <= delayMax {
			return n, true
		}
	}
	return 0, false
}

func ntfBoolJSON(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func ntfColl(a *core.App, cxt *core.Cxt) (*mongo.Collection, error) {
	if a.Mongo == nil {
		return nil, errors.New("mongo disabled")
	}
	db, err := a.Mongo.DB(cxt.Req.Context())
	if err != nil {
		return nil, err
	}
	return db.Collection("notificationsPreferences"), nil
}

func ntfAppName() string {
	if n := strings.TrimSpace(os.Getenv("OVERLEAF_NAME")); n != "" {
		return n
	}
	return "OlliTeX"
}

// ---------- GET /notifications/preferences -----------------------------------

// getGlobalJSON — ordered {muteAll, delay, 12 keys} from a doc (empty = all
// defaults).
func getGlobalJSON(doc bson.D) []byte {
	ma, _ := ntfDocVal(doc, "muteAllNotifications")
	out := `{"muteAllNotifications":` + ntfBoolJSON(ntfBool(ma))
	if v, ok := ntfDocVal(doc, "notificationDelayMinutes"); ok {
		if i, has := ntfDelay(v); has {
			out += `,"notificationDelayMinutes":` + fmt.Sprint(i)
		} else {
			out += `,"notificationDelayMinutes":null`
		}
	} else {
		out += `,"notificationDelayMinutes":null`
	}
	for _, k := range projectKeys {
		v, ok := ntfDocVal(doc, k)
		// Node normalizeProjectPreferences: missing key → default true;
		// present key (even null) → Boolean(value).
		b := true
		if ok {
			b = ntfBool(v)
		}
		out += `,"` + k + `":` + ntfBoolJSON(b)
	}
	out += `}`
	return []byte(out)
}

// hGetGlobal — Node getGlobalPreferences: normalizeGlobalPreferences(doc or
// {}) → {muteAll, delay, 12 keys} in that order.
func hGetGlobal(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		uid := ntfUID(cxt)
		if uid == "" {
			res.SendStatus(500)
			return
		}
		coll, err := ntfColl(a, cxt)
		if err != nil {
			ntfErr500(cxt, res)
			return
		}
		var doc bson.D
		ferr := coll.FindOne(cxt.Req.Context(),
			bson.D{{Key: "user_id", Value: ntfOid(uid)}, {Key: "project_id", Value: nil}}).Decode(&doc)
		if ferr != nil && !errors.Is(ferr, mongo.ErrNoDocuments) {
			ntfErr500(cxt, res)
			return
		}
		if errors.Is(ferr, mongo.ErrNoDocuments) {
			doc = bson.D{}
		}
		res.JSON(200, getGlobalJSON(doc))
	}
}

// ---------- POST /notifications/preferences ----------------------------------

// ntfValErr — the zod error envelope. Pinned on the Node leg: the message's
// field-path quotes are DOUBLE-escaped in the raw body (NODE bytes:
// ...at \"body.muteAllNotifications\"... — a fromZodError/escape artifact),
// so the inner quotes ship as literal backslash+quote.
func ntfValErr(res *core.Res, msg string) {
	esc := strings.NewReplacer(`"`, `\"`).Replace(msg)
	res.JSON(400, []byte(`{"error":"Validation error: `+esc+`","statusCode":400}`))
}

// ntfReadBody — the raw POST body capped like the rest of the service.
func ntfReadBody(cxt *core.Cxt) []byte {
	if cxt.Req == nil || cxt.Req.Body == nil {
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(cxt.Req.Body, 4<<20))
	if err != nil {
		return nil
	}
	return b
}

// ntfBodyClass — the pinned POST body matrix (oracle on the Node leg):
//
//	"" / invalid json / "str" / null → "garbage" (400 EMPTY {})
//	[1,2]                            → "array"   (400 received-array msg)
//	{…}                              → "object"  (validate fields)
func ntfBodyClass(raw []byte) (string, map[string]interface{}) {
	t := strings.TrimSpace(string(raw))
	if t == "" {
		return "empty", nil
	}
	if t[0] == '{' {
		obj := map[string]interface{}{}
		if json.Unmarshal(raw, &obj) != nil {
			return "garbage", nil
		}
		return "object", obj
	}
	var probe interface{}
	if json.Unmarshal(raw, &probe) == nil {
		if _, isArr := probe.([]interface{}); isArr {
			return "array", nil
		}
		return "garbage", nil
	}
	return "garbage", nil
}

// hSaveGlobal — Node updateGlobalPreferences (parseReq zod → handler →
// res.json(body)).
func hSaveGlobal(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		raw := ntfReadBody(cxt)
		class, obj := ntfBodyClass(raw)
		switch class {
		case "empty":
			ntfValErr(res, `Invalid input: expected boolean, received undefined at "body.muteAllNotifications"`)
			return
		case "array":
			ntfValErr(res, `Invalid input: expected object, received array at "body"`)
			return
		case "garbage":
			res.JSON(400, []byte(`{}`))
			return
		}
		mute, hasMute := obj["muteAllNotifications"]
		if !hasMute {
			ntfValErr(res, `Invalid input: expected boolean, received undefined at "body.muteAllNotifications"`)
			return
		}
		muteB, ok := mute.(bool)
		if !ok {
			typ := "string"
			switch mute.(type) {
			case nil:
				typ = "null"
			case float64:
				typ = "number"
			case []interface{}:
				typ = "array"
			case map[string]interface{}:
				typ = "object"
			}
			ntfValErr(res, `Invalid input: expected boolean, received `+typ+` at "body.muteAllNotifications"`)
			return
		}
		delayStored := interface{}(nil) // null by default
		delayEcho := ""
		if dv, hasDelay := obj["notificationDelayMinutes"]; hasDelay {
			switch t := dv.(type) {
			case nil: // nullable ✓ — stored + echoed as null
				delayEcho = `,"notificationDelayMinutes":null`
			case float64:
				if t != float64(int64(t)) {
					ntfValErr(res, `Invalid input: expected int, received number at "body.notificationDelayMinutes"`)
					return
				}
				i := int(t)
				if i < delayMin {
					ntfValErr(res, `Too small: expected number to be >=1 at "body.notificationDelayMinutes"`)
					return
				}
				if i > delayMax {
					ntfValErr(res, `Too big: expected number to be <=10080 at "body.notificationDelayMinutes"`)
					return
				}
				delayStored = int64(i)
				delayEcho = fmt.Sprintf(`,"notificationDelayMinutes":%d`, i)
			case string:
				ntfValErr(res, `Invalid input: expected number, received string at "body.notificationDelayMinutes"`)
			case bool:
				ntfValErr(res, `Invalid input: expected number, received boolean at "body.notificationDelayMinutes"`)
			case []interface{}:
				ntfValErr(res, `Invalid input: expected number, received array at "body.notificationDelayMinutes"`)
			case map[string]interface{}:
				ntfValErr(res, `Invalid input: expected number, received object at "body.notificationDelayMinutes"`)
			default:
				ntfValErr(res, `Invalid input: expected number, received string at "body.notificationDelayMinutes"`)
			}
		}
		uid := ntfUID(cxt)
		if uid == "" {
			res.SendStatus(500)
			return
		}
		coll, cerr := ntfColl(a, cxt)
		if cerr != nil {
			ntfErr500(cxt, res)
			return
		}
		// Node $set = normalizeGlobalPreferences(body): {mute, delay|null,
		// 12 default keys} (the zod shape strips unknown keys, so the 12
		// stored keys are always the defaults for this route).
		set := bson.D{
			{Key: "muteAllNotifications", Value: muteB},
			{Key: "notificationDelayMinutes", Value: delayStored},
		}
		for _, k := range projectKeys {
			set = append(set, bson.E{Key: k, Value: true})
		}
		if _, uerr := coll.UpdateOne(cxt.Req.Context(),
			bson.D{{Key: "user_id", Value: ntfOid(uid)}, {Key: "project_id", Value: nil}},
			bson.D{{Key: "$set", Value: set}},
			upsertOpts,
		); uerr != nil {
			ntfErr500(cxt, res)
			return
		}
		res.JSON(200, []byte(`{"muteAllNotifications":`+ntfBoolJSON(muteB)+delayEcho+`}`))
	}
}

// ---------- /notifications/preferences/project/:projectId ---------------------

// ntfAuthzProject — Node _ensureProjectMembership with the Node error family
// (pinned): bad hex → new ObjectId throws → 500 page; missing → 404 OlliTeX
// page (BOTH accepts); non-member → 403 Restricted403 page (BOTH accepts —
// json accept included, unlike the track-changes json-403 family).
func ntfAuthzProject(a *core.App, cxt *core.Cxt, res *core.Res) (string, bool) {
	seg := cxt.Params["1"]
	oid, err := primitive.ObjectIDFromHex(strings.ToLower(seg))
	if err != nil {
		ntfErr500(cxt, res)
		return "", false
	}
	if a.Mongo == nil {
		ntfErr500(cxt, res)
		return "", false
	}
	db, err := a.Mongo.DB(cxt.Req.Context())
	if err != nil {
		ntfErr500(cxt, res)
		return "", false
	}
	var doc bson.D
	if ferr := db.Collection("projects").FindOne(cxt.Req.Context(),
		bson.D{{Key: "_id", Value: oid}}).Decode(&doc); ferr != nil {
		if errors.Is(ferr, mongo.ErrNoDocuments) {
			views.NotFoundPage(res.W, ntfPageData(cxt))
			return "", false
		}
		ntfErr500(cxt, res)
		return "", false
	}
	uid := ntfUID(cxt)
	if uid == "" || !ntfHasAccess(uid, doc) {
		// Node: Errors.ForbiddenError → ErrorController.forbidden →
		// res.render('user/restricted') with NO title local → the layout
		// default (appName) title — the P6.14 title variant (pinned A/B).
		views.Restricted403AppTitle(res.W, ntfPageData(cxt))
		return "", false
	}
	return oid.Hex(), true
}

// ntfHasAccess — Node _hasProjectAccess: owner_ref ∪ collaberator_refs ∪
// readOnly_refs ∪ tokenAccessReadAndWrite_refs ∪ tokenAccessReadOnly_refs
// (exactly these five — no reviewer_refs).
func ntfHasAccess(uidHex string, doc bson.D) bool {
	uidHex = strings.ToLower(uidHex)
	if v, ok := ntfDocVal(doc, "owner_ref"); ok {
		if s, ok := ntfOidHex(v); ok && s == uidHex {
			return true
		}
	}
	for _, key := range []string{"collaberator_refs", "readOnly_refs", "tokenAccessReadAndWrite_refs", "tokenAccessReadOnly_refs"} {
		v, ok := ntfDocVal(doc, key)
		if !ok {
			continue
		}
		items, ok := v.([]interface{})
		if !ok {
			continue
		}
		for _, it := range items {
			if s, ok := ntfOidHex(it); ok && s == uidHex {
				return true
			}
		}
	}
	return false
}

// hGetProject — Node getProjectPreferences: per key projectDoc ?? globalDoc
// ?? default(true); muteAll (global) appended LAST (pinned order).
func hGetProject(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, ok := ntfAuthzProject(a, cxt, res); !ok {
			return
		}
		uid := ntfUID(cxt)
		coll, err := ntfColl(a, cxt)
		if err != nil {
			ntfErr500(cxt, res)
			return
		}
		ctx := cxt.Req.Context()
		uidO := ntfOid(uid)
		pid := ntfOid(cxt.Params["1"])
		var projDoc, globalDoc bson.D
		if ferr := coll.FindOne(ctx, bson.D{{Key: "user_id", Value: uidO}, {Key: "project_id", Value: pid}}).Decode(&projDoc); ferr != nil && !errors.Is(ferr, mongo.ErrNoDocuments) {
			ntfErr500(cxt, res)
			return
		}
		if ferr := coll.FindOne(ctx, bson.D{{Key: "user_id", Value: uidO}, {Key: "project_id", Value: nil}}).Decode(&globalDoc); ferr != nil && !errors.Is(ferr, mongo.ErrNoDocuments) {
			ntfErr500(cxt, res)
			return
		}
		ma, _ := ntfDocVal(globalDoc, "muteAllNotifications")
		var sb strings.Builder
		sb.WriteString("{")
		for i, k := range projectKeys {
			if i > 0 {
				sb.WriteString(",")
			}
			v, okP := ntfDocVal(projDoc, k)
			if !okP {
				if gv, okG := ntfDocVal(globalDoc, k); okG {
					v = gv
				} else {
					v = true
				}
			}
			sb.WriteString(`"` + k + `":` + ntfBoolJSON(ntfBool(v)))
		}
		sb.WriteString(`,"muteAllNotifications":` + ntfBoolJSON(ntfBool(ma)) + "}")
		res.JSON(200, []byte(sb.String()))
	}
}

// hSaveProject — Node saveProjectPreferences: loose object body; $set the 12
// normalized keys (body values Boolean-coerced, missing → default true)
// upserted; 200 body exactly `null`.
func hSaveProject(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, ok := ntfAuthzProject(a, cxt, res); !ok {
			return
		}
		body := ntfReadBody(cxt)
		class, obj := ntfBodyClass(body)
		switch class {
		case "array":
			ntfValErr(res, `Invalid input: expected object, received array at "body"`)
			return
		case "garbage":
			res.JSON(400, []byte(`{}`))
			return
		}
		if class == "empty" {
			obj = map[string]interface{}{}
		}
		set := bson.D{}
		for _, k := range projectKeys {
			v, okk := obj[k]
			b := true
			if okk {
				b = ntfBool(v)
			}
			set = append(set, bson.E{Key: k, Value: b})
		}
		uid := ntfUID(cxt)
		coll, cerr := ntfColl(a, cxt)
		if cerr != nil {
			ntfErr500(cxt, res)
			return
		}
		if _, uerr := coll.UpdateOne(cxt.Req.Context(),
			bson.D{{Key: "user_id", Value: ntfOid(uid)}, {Key: "project_id", Value: ntfOid(cxt.Params["1"])}},
			bson.D{{Key: "$set", Value: set}},
			upsertOpts,
		); uerr != nil {
			ntfErr500(cxt, res)
			return
		}
		// Node res.json(null) — the literal body is "null".
		res.JSON(200, []byte(`null`))
	}
}

// ---------- /user routes ------------------------------------------------------

func hRedirectHub() func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		// Node res.redirect(301, '/hub#/mysettings.email') — express renders
		// the Accept matrix (html → "<p>Moved Permanently. …</p>"; json →
		// empty body; text/*|*/* → plain; Vary: Accept). core.Redirect
		// mirrors it (P6.5 instance-stats precedent).
		res.Redirect(cxt.Req, 301, "/hub#/mysettings.email")
	}
}

// hTestEmail — Node sendTestEmail: the session user's email from the DB; one
// test mail (Node ctaTemplate 'testEmail': subject "A Test Email from
// <app>"; CTA "Open <app>" → siteUrl); 200 {"message":"Email Sent"}; missing
// user/email → 500 (Node: plain Error → next(err) → the 500 view).
func hTestEmail(a *core.App, mail *core.Mail) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		uid := ntfUID(cxt)
		if uid == "" || a.Mongo == nil {
			ntfErr500(cxt, res)
			return
		}
		db, err := a.Mongo.DB(cxt.Req.Context())
		if err != nil {
			ntfErr500(cxt, res)
			return
		}
		var udoc bson.D
		if ferr := db.Collection("users").FindOne(cxt.Req.Context(),
			bson.D{{Key: "_id", Value: ntfOid(uid)}}).Decode(&udoc); ferr != nil && !errors.Is(ferr, mongo.ErrNoDocuments) {
			ntfErr500(cxt, res)
			return
		}
		email, hasEmail := ntfDocVal(udoc, "email")
		emailStr, _ := email.(string)
		if !hasEmail || emailStr == "" {
			ntfErr500(cxt, res)
			return
		}
		appName := ntfAppName()
		site := cxt.SiteURL
		if site == "" {
			site = "http://" + cxt.Req.Host
		}
		subject := "A Test Email from " + appName
		text := "Hi,\n\nThis is a test Email from " + appName + "\n\nOpen " + appName + ": " + site + "\n\nRegards,\nThe " + appName + " Team - " + site + "\n"
		html := "<p>Hi,</p>" +
			"<p>This is a test Email from " + appName + "</p>" +
			`<p><a href="` + site + `">Open ` + appName + `</a></p>` +
			"<p>Regards,<br/>The " + appName + " Team - " + site + "</p>"
		if mail != nil {
			if serr := mail.Send(emailStr, subject, text, html); serr != nil {
				ntfErr500(cxt, res)
				return
			}
		}
		res.JSON(200, []byte(`{"message":"Email Sent"}`))
	}
}
