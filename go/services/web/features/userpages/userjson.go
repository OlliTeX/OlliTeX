// U8 (P7, post-U2) — user-family JSON READS, Node parity:
//
//   GET /user/contacts   (login)  ContactController.getContacts +
//                                 ContactManager.getContactIds
//                                 (services/web/app/src/Features/Contacts)
//   GET /user/emails     (login)  UserEmailsController.list →
//                                 UserGetter.getUserFullEmails (CE branch:
//                                 no affiliations feature)
//                                 (services/web/app/src/Features/User)
//   GET /user/features   (login)  UserInfoController.getUserFeatures →
//                                 UserGetter.getUserFeatures →
//                                 FeatureSets.computeFeatureSet (CE: no
//                                 module-provided feature sets)
//
// Pinned Node behaviors (live oracle 2026-09-22, e2e stack):
//   contacts:  {"contacts":[...]} — entries sorted n DESC then ts DESC,
//     top 50, holdingAccount users DROPPED (filter happens AFTER the 50
//     slice), output rows in contact order, each
//     {"id","email","first_name","last_name","type":"user"} — absent name
//     fields → "".
//   emails:    [ { "email", "reversedHostname", "createdAt" (ISO ms),
//     "_id", "default", "emailHasInstitutionLicence", "lastConfirmedAt" } ]
//     — DB array order; default = (email === user.email); CE:
//     emailHasInstitutionLicence always false (no affiliations / no
//     confirmedAt); lastConfirmedAt = reconfirmedAt || confirmedAt || null.
//   features:  stored key order (BSON insertion order) IS the wire order;
//     missing user.features → {}; CE single-source merge is identity for
//     every key EXCEPT compileGroup with a non-'priority' tier → 'standard'
//     (mergeFeatures quirk: 'standard' stays 'standard', 'priority' stays,
//     'alpha' → 'standard').
//
// Auth/failure chains: login required (core global gate; anon GET → 302
// /login, matching requireLogin). Wrong method → core 404 (Node registers
// GET only). User document missing → 500 page (Node throws 'User not
// Found' → express error → general/500 view).

package userpages

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"ollitex/go/services/web/core"
)

const contactsMax = 50 // ContactController.MAX_CONTACTS

// ---------- ordered user-doc access (BSON key order is wire order) ----------

func userDocOrdered(a *core.App, ctx context.Context, uid string) (bson.D, bool) {
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, false
	}
	doc := bson.D{}
	if err := db.Collection("users").FindOne(
		ctx, bson.D{{Key: "_id", Value: mustObjectID(uid)}}).Decode(&doc); err != nil {
		return nil, false
	}
	return doc, true
}

func dGet(d bson.D, k string) (any, bool) {
	for _, e := range d {
		if e.Key == k {
			return e.Value, true
		}
	}
	return nil, false
}

func dStr(d bson.D, k string) string {
	if v, ok := dGet(d, k); ok {
		return asStr(v)
	}
	return ""
}

func dTimeV(v any) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t, !t.IsZero()
	case primitive.DateTime:
		tt := t.Time()
		return tt, !tt.IsZero()
	case string:
		if t2, err := time.Parse(time.RFC3339Nano, t); err == nil {
			return t2, true
		}
	}
	return time.Time{}, false
}

func dTime(d bson.D, k string) (time.Time, bool) {
	v, ok := dGet(d, k)
	if !ok {
		return time.Time{}, false
	}
	return dTimeV(v)
}

func dIDStr(d bson.D, k string) string {
	v, ok := dGet(d, k)
	if !ok {
		return ""
	}
	switch t := v.(type) {
	case primitive.ObjectID:
		return t.Hex()
	case string:
		return t
	}
	return ""
}

// jsonTime — Node JSON.stringify(Date) → "2006-01-02T15:04:05.000Z".
func jsonTime(t time.Time) string {
	t = t.UTC()
	ms := t.Nanosecond() / 1e6
	return t.Format("2006-01-02T15:04:05.") + fmt.Sprintf("%03dZ", ms)
}

// reversedHostname — Node: email.split('@')[1].split(”).reverse().join(”)
func reversedHostname(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	dom := []byte(email[at+1:])
	for i, j := 0, len(dom)-1; i < j; i, j = i+1, j-1 {
		dom[i], dom[j] = dom[j], dom[i]
	}
	return string(dom)
}

// ---------- GET /user/contacts ----------

type contactEntry struct {
	id   string
	n    int64
	ts   time.Time
	tsOK bool
}

func parseContacts(cdoc bson.D) []contactEntry {
	sub, ok := dGet(cdoc, "contacts")
	if !ok {
		return nil
	}
	var entries []contactEntry
	appendEntry := func(id string, n any, tsVal any) {
		e := contactEntry{id: id}
		switch v := n.(type) {
		case int32:
			e.n = int64(v)
		case int64:
			e.n = v
		case int:
			e.n = int64(v)
		case float64:
			e.n = int64(v)
		}
		if t, ok2 := dTimeV(tsVal); ok2 {
			e.ts, e.tsOK = t, true
		}
		entries = append(entries, e)
	}
	switch sd := sub.(type) {
	case bson.D:
		for _, se := range sd {
			var n, ts any
			if inner, ok2 := se.Value.(bson.D); ok2 {
				for _, ie := range inner {
					switch ie.Key {
					case "n":
						n = ie.Value
					case "ts":
						ts = ie.Value
					}
				}
			}
			appendEntry(se.Key, n, ts)
		}
	case map[string]any:
		// Defensive fallback only: FindOne decodes subdocs to bson.D via
		// the default registry, so ordered entries are the normal shape.
		// (Map iteration is unordered — acceptable here because this
		// branch is unreachable with the current decoder.)
		for id, raw := range sd {
			var n, ts any
			if m, ok2 := raw.(map[string]any); ok2 {
				n, ts = m["n"], m["ts"]
			}
			appendEntry(id, n, ts)
		}
	}
	return entries
}

// sortContacts — Node: a.n === b.n ? b.ts - a.ts : b.n - a.n (stable).
func sortContacts(entries []contactEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, bb := entries[i], entries[j]
		if a.n != bb.n {
			return a.n > bb.n
		}
		if a.tsOK != bb.tsOK {
			return a.tsOK // timestamped ranks before untimestamped
		}
		return a.ts.UnixNano() > bb.ts.UnixNano()
	})
}

func getContacts(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		uid := cxt.Sess.UserIDHex()
		ctx := cxt.Req.Context()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			res.SendStatus(500)
			return
		}
		cdoc := bson.D{}
		err = db.Collection("contacts").FindOne(
			ctx, bson.D{{Key: "user_id", Value: mustObjectID(uid)}}).Decode(&cdoc)
		if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
			res.SendStatus(500)
			return
		}
		entries := parseContacts(cdoc)
		// Node sortContacts: n DESC, then ts DESC.
		sortContacts(entries)
		limit := len(entries)
		if limit > contactsMax {
			limit = contactsMax
		}
		if limit == 0 {
			res.JSON(200, []byte(`{"contacts":[]}`))
			return
		}
		ids := make([]primitive.ObjectID, 0, limit)
		for _, e := range entries[:limit] {
			if oid, oerr := primitive.ObjectIDFromHex(e.id); oerr == nil {
				ids = append(ids, oid)
			}
		}
		if len(ids) == 0 {
			res.JSON(200, []byte(`{"contacts":[]}`))
			return
		}
		cur, qerr := db.Collection("users").Find(ctx,
			bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: ids}}}})
		if qerr != nil {
			res.SendStatus(500)
			return
		}
		udocs := []bson.D{}
		if derr := cur.All(ctx, &udocs); derr != nil {
			res.SendStatus(500)
			return
		}
		byID := map[string]bson.D{}
		for _, u := range udocs {
			byID[dIDStr(u, "_id")] = u
		}
		// Node: join → drop holdingAccount (AFTER the 50-slice) → rows in
		// contact order → _formatContact (absent fields → "", type "user").
		rows := []string{}
		for _, e := range entries[:limit] {
			u, ok2 := byID[e.id]
			if !ok2 {
				continue // not in users collection → absent
			}
			if row, ok3 := contactsRowJSON(e, u); ok3 {
				rows = append(rows, row)
			}
		}
		res.JSON(200, []byte(`{"contacts":[`+strings.Join(rows, ",")+`]}`))
	}
}

// contactsRowJSON — one _formatContact row, or skip (holding account).
func contactsRowJSON(e contactEntry, u bson.D) (string, bool) {
	if v, ok := dGet(u, "holdingAccount"); ok && boolOf(v) {
		return "", false
	}
	return fmt.Sprintf(
		`{"id":"%s","email":"%s","first_name":"%s","last_name":"%s","type":"user"}`,
		jsonEscape(e.id),
		jsonEscape(dStr(u, "email")),
		jsonEscape(dStr(u, "first_name")),
		jsonEscape(dStr(u, "last_name"))), true
}

// ---------- GET /user/emails ----------

// Node decorateFullEmails (CE: affiliations=[] samlIdentifiers=[]):
//
//	default                  = (emailData.email === defaultEmail)
//	emailHasInstitutionLicence = emailHasLicence(...)  → false in CE
//	lastConfirmedAt          = (reconfirmedAt || confirmedAt) → Date | null
//
// Wire order per record: email, reversedHostname, createdAt, _id, default,
// emailHasInstitutionLicence, lastConfirmedAt (oracle 2026-09-22).
type userEmailsDoc struct {
	Email  string      `bson:"email"`
	Emails []emailDocV `bson:"emails"`
}

type emailDocV struct {
	Email            string              `bson:"email"`
	ReversedHostname string              `bson:"reversedHostname"`
	CreatedAt        *primitive.DateTime `bson:"createdAt"`
	ID               primitive.ObjectID  `bson:"_id"`
	ConfirmedAt      *primitive.DateTime `bson:"confirmedAt"`
	ReconfirmedAt    *primitive.DateTime `bson:"reconfirmedAt"`
}

func getEmails(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		uid := cxt.Sess.UserIDHex()
		ctx := cxt.Req.Context()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			res.SendStatus(500)
			return
		}
		ub := userEmailsDoc{}
		err = db.Collection("users").FindOne(
			ctx, bson.D{{Key: "_id", Value: mustObjectID(uid)}}).Decode(&ub)
		if err != nil {
			// Node: getUserFullEmails throws 'User not Found' → 500 view.
			render500(a, cxt, res)
			return
		}
		rows := []string{}
		for _, em := range ub.Emails {
			rows = append(rows, emailJSONStruct(em, ub.Email))
		}
		res.JSON(200, []byte("["+strings.Join(rows, ",")+"]"))
	}
}

// emailJSONStruct — one decorated email record (CE branch).
// Node JSON.stringify drops ABSENT source keys (undefined): a record
// without createdAt / _id omits those keys entirely (pinned: tpladmin
// fixture email record — {"email","reversedHostname","default",...}).
func emailJSONStruct(ed emailDocV, defaultEmail string) string {
	createdAt := ""
	if ed.CreatedAt != nil && !ed.CreatedAt.Time().IsZero() {
		createdAt = `,"createdAt":"` + jsonTime(ed.CreatedAt.Time()) + `"`
	}
	idKey := ""
	if ed.ID != primitive.NilObjectID {
		idKey = `,"_id":"` + ed.ID.Hex() + `"`
	}
	lastConfirmed := "null"
	if ed.ReconfirmedAt != nil && !ed.ReconfirmedAt.Time().IsZero() {
		lastConfirmed = `"` + jsonTime(ed.ReconfirmedAt.Time()) + `"`
	} else if ed.ConfirmedAt != nil && !ed.ConfirmedAt.Time().IsZero() {
		lastConfirmed = `"` + jsonTime(ed.ConfirmedAt.Time()) + `"`
	}
	return fmt.Sprintf(
		`{"email":"%s","reversedHostname":"%s"%s%s,"default":%s,"emailHasInstitutionLicence":false,"lastConfirmedAt":%s}`,
		jsonEscape(ed.Email),
		jsonEscape(ed.ReversedHostname),
		createdAt,
		idKey,
		b(ed.Email == defaultEmail && defaultEmail != ""),
		lastConfirmed)
}

// ---------- GET /user/features ----------

// computeFeatureSet([user.features, ...moduleFeatures]); CE module
// features = [] → one merge into {}. mergeFeatures, single source: every
// key passes through unchanged EXCEPT compileGroup — the merge computes
// 'priority' iff EITHER side is 'priority', else 'standard' — so a stored
// tier of 'standard' stays 'standard', 'priority' stays, anything else
// (e.g. 'alpha') collapses to 'standard'. The mendeley/referencesSearch/
// zotero backfill cannot change a stored set (it only SETS references=true
// when three other keys are already true — value-identical for booleans).
func getFeatures(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		uid := cxt.Sess.UserIDHex()
		ctx := cxt.Req.Context()
		udoc, ok := userDocOrdered(a, ctx, uid)
		if !ok {
			render500(a, cxt, res)
			return
		}
		fv, ok := dGet(udoc, "features")
		if !ok || fv == nil {
			res.JSON(200, []byte(`{}`))
			return
		}
		fd, isD := fv.(bson.D)
		if !isD {
			res.JSON(200, []byte(`{}`))
			return
		}
		rows := []string{}
		for _, fe := range fd {
			if fe.Key == "compileGroup" {
				cg := "standard"
				if asStr(fe.Value) == "priority" {
					cg = "priority"
				}
				rows = append(rows, `"compileGroup":"`+cg+`"`)
				continue
			}
			rows = append(rows, `"`+jsonEscape(fe.Key)+`":`+jsonAny(fe.Value))
		}
		res.JSON(200, []byte(`{`+strings.Join(rows, ",")+`}`))
	}
}
func jsonNum(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%g", f)
}

func jsonAny(v any) string {
	switch t := v.(type) {
	case bool:
		return b(t)
	case nil:
		return "null"
	case string:
		return `"` + jsonEscape(t) + `"`
	case int32:
		return fmt.Sprintf("%d", t)
	case int64:
		return fmt.Sprintf("%d", t)
	case int:
		return fmt.Sprintf("%d", t)
	case float64:
		return jsonNum(t)
	}
	return "null"
}
