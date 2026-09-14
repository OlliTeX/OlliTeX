package projectlist

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

// P4.3 — GET /project/:Project_id/members  (the share-modal / "all active
// members" list). Node sources (oracle, pinned live 2026-09-14):
//
//	router.mjs:545  webRouter.get('/project/:Project_id/members',
//	                          requireLogin, blockRestrictedUserFromProject,
//	                          ensureUserCanReadProject, getAllMembers)
//	CollaboratorsController.getAllMembers
//	  -> CollaboratorsGetter.getAllInvitedMembers
//	       -> ProjectAccess.loadInvitedMembers
//	            = _getMemberIdsWithPrivilegeLevelsFromFields(...)  filtered to
//	              source !== TOKEN && privilegeLevel !== OWNER
//	       -> ProjectEditorHandler.buildUserModelView (per member)
//
// Contract (pinned against the LIVE Node oracle):
//
//   - 200 application/json  { "members": [ <member>, … ] }
//     member (key order)  : _id, first_name, last_name, email, privileges,
//     signUpDate [, pendingEditor: true] [,
//     pendingReviewer: true]
//   - privileges strings : readAndWrite / review / readOnly  (NEVER "owner")
//   - order (exact)      : collaborators → reviewers → readOnly
//     (each in its refs array's order; NO dedup, NO sort)
//   - the OWNER (owner_ref, source OWNER) and TOKEN members are OMITTED.
//   - a member whose user doc is GONE is dropped (Node drops null users).
//   - pendingEditor/pendingReviewer appear ONLY when the readOnly member is
//     listed in pendingEditor_refs / pendingReviewer_refs (else omitted).
//   - signUpDate is an ISO-8601 string with ms (Node JSON.stringify(Date)).
//   - valid id, project absent            -> 404 HTML general/404 (NOT accept-dep)
//   - invalid (non-hex) id                 -> 404 application/json malformed (NOT accept-dep)
//   - present, !canRead                    -> 403  json {"message":"restricted"} | html Restricted
//   - anon  accept json -> 401 ;  accept html -> 302 /login  (requireLogin gate)
//
// blockRestrictedUserFromProject 403s a no-access / token-RO-uncertified edge
// that, in this deployment, resolves to the same 403 canRead already emits for
// the non-member case, so the canRead gate below covers the observable contract.
var memPat = regexp.MustCompile(`^/project/([^/]+)/members$`)

type memberRow struct {
	uid             string
	priv            string
	pendingEditor   bool
	pendingReviewer bool
}

// invitedMemberRows mirrors _getMemberIdsWithPrivilegeLevelsFromFields,
// restricted to the INVITE records (loadInvitedMembers drops OWNER and
// TOKEN-sourced rows): collaborators → readAndWrite, reviewers → review,
// readOnly → readOnly (with pendingEditor/pendingReviewer flags). Each refs
// array is emitted in its stored order (no dedup, no sort).
func invitedMemberRows(doc *primitive.D) []memberRow {
	pe := map[string]bool{}
	for _, id := range asOIDList(dget(*doc, "pendingEditor_refs")) {
		pe[id] = true
	}
	pr := map[string]bool{}
	for _, id := range asOIDList(dget(*doc, "pendingReviewer_refs")) {
		pr[id] = true
	}
	var out []memberRow
	for _, id := range asOIDList(dget(*doc, "collaberator_refs")) {
		out = append(out, memberRow{uid: id, priv: "readAndWrite"})
	}
	for _, id := range asOIDList(dget(*doc, "reviewer_refs")) {
		out = append(out, memberRow{uid: id, priv: "review"})
	}
	for _, id := range asOIDList(dget(*doc, "readOnly_refs")) {
		r := memberRow{uid: id, priv: "readOnly"}
		if pe[id] {
			r.pendingEditor = true
		} else if pr[id] {
			r.pendingReviewer = true
		}
		out = append(out, r)
	}
	return out
}

// loadUsers batch-loads the member user docs (Node: UserGetter.getUsers with
// projection {_id, email, first_name, last_name, signUpDate}).
func loadUsers(a *core.App, cxt *core.Cxt, rows []memberRow) map[string]primitive.D {
	uids := make([]string, 0, len(rows))
	for _, r := range rows {
		uids = append(uids, r.uid)
	}
	return loadUsersHex(a, cxt, uids, bson.D{
		{Key: "_id", Value: 1}, {Key: "email", Value: 1},
		{Key: "first_name", Value: 1}, {Key: "last_name", Value: 1},
		{Key: "signUpDate", Value: 1},
	})
}

func membersHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		// Global login gate (Route.NoLogin=false) bounces anonymous to
		// 401(json)/302(html) first — mirroring Node's requireLogin. Defensive:
		if cxt.Sess == nil || cxt.Sess.UserIDHex() == "" {
			if core.AcceptsJSON(cxt.Req) {
				res.SendStatus(401)
			} else {
				res.Redirect(cxt.Req, 302, "/login")
			}
			return
		}
		uid := cxt.Sess.UserIDHex()
		param := cxt.Params["1"]
		relPath := strings.TrimPrefix(cxt.Req.URL.Path, "/")

		if !validOID.MatchString(param) {
			res.JSON(404, []byte(malformed404))
			return
		}
		oid, err := primitive.ObjectIDFromHex(strings.ToLower(param))
		if err != nil {
			res.JSON(404, []byte(malformed404))
			return
		}
		doc, lerr := loadProject(a, cxt, oid)
		if lerr != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		if doc == nil {
			views.NotFoundPage(res.W, pageBase(cxt, relPath))
			return
		}
		if !canRead(uid, loadUserAdmin(a, cxt, uid), *doc) {
			if core.AcceptsJSON(cxt.Req) {
				res.JSON(403, []byte(`{"message":"restricted"}`))
			} else {
				views.Restricted403(res.W, pageBase(cxt, relPath))
			}
			return
		}

		rows := invitedMemberRows(doc)
		users := loadUsers(a, cxt, rows)

		var b strings.Builder
		b.WriteString(`{"members":[`)
		first := true
		for _, r := range rows {
			u, ok := users[r.uid]
			if !ok {
				continue // Node drops members whose user doc is missing
			}
			if !first {
				b.WriteString(",")
			}
			first = false
			b.WriteString(`{"_id":`)
			b.WriteString(jstr(r.uid))
			b.WriteString(`,"first_name":`)
			b.WriteString(jstr(asStr(dget(u, "first_name"))))
			b.WriteString(`,"last_name":`)
			b.WriteString(jstr(asStr(dget(u, "last_name"))))
			b.WriteString(`,"email":`)
			b.WriteString(jstr(asStr(dget(u, "email"))))
			b.WriteString(`,"privileges":`)
			b.WriteString(jstr(r.priv))
			b.WriteString(`,"signUpDate":`)
			if iso, ok := signUpDateISO(dget(u, "signUpDate")); ok {
				b.WriteString(jstr(iso))
			} else {
				b.WriteString(`null`)
			}
			if r.pendingEditor {
				b.WriteString(`,"pendingEditor":true`)
			}
			if r.pendingReviewer {
				b.WriteString(`,"pendingReviewer":true`)
			}
			b.WriteString(`}`)
		}
		b.WriteString(`]}`)
		res.JSON(200, []byte(b.String()))
	}
}

// jstr returns a JSON-encoded string literal (quoted + escaped).
func jstr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// signUpDateISO formats a BSON Date (which the driver may decode to
// time.Time, primitive.DateTime, or an int64 ms epoch depending on the target
// type) as Node's JSON.stringify(Date): ISO-8601 UTC with milliseconds,
// e.g. 2026-09-12T02:28:05.099Z.
func signUpDateISO(v any) (string, bool) {
	f := func(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }
	switch x := v.(type) {
	case time.Time:
		return f(x), true
	case primitive.DateTime:
		return f(time.UnixMilli(int64(x))), true
	case int64:
		return f(time.UnixMilli(x)), true
	case int:
		return f(time.UnixMilli(int64(x))), true
	}
	return "", false
}
