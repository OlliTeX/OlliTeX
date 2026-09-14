package projectlist

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

// P4.4 — GET /project/:Project_id/access-requests  (the owner/admin-facing
// list of pending access requests). Node sources (oracle, pinned live
// 2026-09-14):
//
//	CollaboratorsRouter: webRouter.get('/project/:Project_id/access-requests',
//		requireLogin, ensureUserCanAdminProject, getAccessRequests)
//	CollaboratorsController.getAccessRequests
//	  -> CollaboratorsGetter.getProjectAccess(projectId).loadAccessRequestsView()
//	  -> res.json({ editAccessRequests })
//
// Contract (live oracle):
//
//   - 200 application/json  { "editAccessRequests": [ <row>, … ] }   (empty -> [])
//     row (key order) : _id, email, first_name, last_name, privilegeLevel,
//     currentPrivilegeLevel, requestedAt
//   - _id                  : the requester's user id (hex string)
//   - privilegeLevel       : the level they REQUESTED (readAndWrite|review|readOnly)
//   - currentPrivilegeLevel: their CURRENT level in the project
//     (owner|readAndWrite|review|readOnly) or false
//     (PrivilegeLevels.NONE) when they are NOT a member
//   - requestedAt          : ISO-8601 (Node JSON.stringify(Date), ms precision)
//   - order = the project.editAccessRequests array order (no sort/dedup)
//   - rows whose user doc is GONE are dropped
//   - present, not admin  -> 403  json {"message":"restricted"} | html Restricted
//     (guard = canUserAdminProject = OWNER or site-admin-with-'modify-project-setting');
//     in this deployment ADMIN_PRIVILEGE_AVAILABLE=true, so owner OR user.isAdmin.
//   - valid id, project absent -> 404 HTML general/404 (NOT accept-dep)
//   - INVALID id                -> 404 application/json malformed (NOT accept-dep)
//   - anon  accept json -> 401 ; accept html -> 302 /login
var arPat = regexp.MustCompile(`^/project/([^/]+)/access-requests$`)

// adminPrivilegeAvailable mirrors Settings.adminPrivilegeAvailable
// (= ENV ADMIN_PRIVILEGE_AVAILABLE === 'true'); gates the site-admin branch.
func adminPrivilegeAvailable() bool { return os.Getenv("ADMIN_PRIVILEGE_AVAILABLE") == "true" }

// currentPrivLevel mirrors ProjectAccess.privilegeLevelForUser: the
// member-record level for uid (owner | readAndWrite | review | readOnly; token
// refs only count when publicAccesLevel == tokenBased), else NONE (false).
func currentPrivLevel(uid string, d primitive.D) any {
	if oidHex(dget(d, "owner_ref")) == uid {
		return "owner"
	}
	if inOIDList(dget(d, "collaberator_refs"), uid) {
		return "readAndWrite"
	}
	if inOIDList(dget(d, "reviewer_refs"), uid) {
		return "review"
	}
	if inOIDList(dget(d, "readOnly_refs"), uid) {
		return "readOnly"
	}
	if asStr(dget(d, "publicAccesLevel")) == "tokenBased" {
		if inOIDList(dget(d, "tokenAccessReadAndWrite_refs"), uid) {
			return "readAndWrite"
		}
		if inOIDList(dget(d, "tokenAccessReadOnly_refs"), uid) {
			return "readOnly"
		}
	}
	return false
}

// loadUsersHex batch-loads user docs by hex id (Node: UserGetter.getUsers).
func loadUsersHex(a *core.App, cxt *core.Cxt, uids []string, proj bson.D) map[string]primitive.D {
	out := map[string]primitive.D{}
	if a.Mongo == nil || len(uids) == 0 {
		return out
	}
	oids := make([]primitive.ObjectID, 0, len(uids))
	seen := map[string]bool{}
	for _, h := range uids {
		if seen[h] {
			continue
		}
		seen[h] = true
		if o, err := primitive.ObjectIDFromHex(h); err == nil {
			oids = append(oids, o)
		}
	}
	if len(oids) == 0 {
		return out
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 10*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return out
	}
	cur, err := db.Collection("users").Find(ctx,
		bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: oids}}}},
		options.Find().SetProjection(proj))
	if err != nil {
		return out
	}
	var docs []primitive.D
	if err := cur.All(ctx, &docs); err != nil {
		cur.Close(ctx)
		return out
	}
	cur.Close(ctx)
	for i := range docs {
		if h := oidHex(dget(docs[i], "_id")); h != "" {
			out[h] = docs[i]
		}
	}
	return out
}

// writeJSONVal writes a compact JSON value (string/bool/number/null).
func writeJSONVal(b *strings.Builder, v any) {
	switch x := v.(type) {
	case bool:
		if x {
			b.WriteString(`true`)
		} else {
			b.WriteString(`false`)
		}
	case string:
		b.WriteString(jstr(x))
	case nil:
		b.WriteString(`null`)
	default:
		bb, err := json.Marshal(v)
		if err != nil {
			b.WriteString(`null`)
			return
		}
		b.Write(bb)
	}
}

func accessRequestsHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
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
		// canUserAdminProject = OWNER or (adminPrivilegeAvailable && siteAdmin)
		canAdmin := oidHex(dget(*doc, "owner_ref")) == uid
		if !canAdmin && adminPrivilegeAvailable() && loadUserAdmin(a, cxt, uid) {
			canAdmin = true
		}
		if !canAdmin {
			if core.AcceptsJSON(cxt.Req) {
				res.JSON(403, []byte(`{"message":"restricted"}`))
			} else {
				views.Restricted403(res.W, pageBase(cxt, relPath))
			}
			return
		}

		reqs, _ := dget(*doc, "editAccessRequests").(primitive.A)
		uids := make([]string, 0, len(reqs))
		type reqRow struct {
			uid            string
			privilegeLevel any
			requestedAt    any
		}
		var rows []reqRow
		for _, rq := range reqs {
			r, ok := rq.(primitive.D)
			if !ok {
				continue
			}
			u := oidHex(dget(r, "userId"))
			if u == "" {
				continue
			}
			rows = append(rows, reqRow{uid: u, privilegeLevel: dget(r, "privilegeLevel"), requestedAt: dget(r, "requestedAt")})
			uids = append(uids, u)
		}
		users := loadUsersHex(a, cxt, uids, bson.D{
			{Key: "_id", Value: 1}, {Key: "email", Value: 1},
			{Key: "first_name", Value: 1}, {Key: "last_name", Value: 1},
		})

		var b strings.Builder
		b.WriteString(`{"editAccessRequests":[`)
		first := true
		for _, r := range rows {
			u, ok := users[r.uid]
			if !ok {
				continue // Node drops rows whose user doc is missing
			}
			if !first {
				b.WriteString(",")
			}
			first = false
			b.WriteString(`{"_id":`)
			b.WriteString(jstr(r.uid))
			b.WriteString(`,"email":`)
			b.WriteString(jstr(asStr(dget(u, "email"))))
			b.WriteString(`,"first_name":`)
			b.WriteString(jstr(asStr(dget(u, "first_name"))))
			b.WriteString(`,"last_name":`)
			b.WriteString(jstr(asStr(dget(u, "last_name"))))
			b.WriteString(`,"privilegeLevel":`)
			writeJSONVal(&b, r.privilegeLevel)
			b.WriteString(`,"currentPrivilegeLevel":`)
			writeJSONVal(&b, currentPrivLevel(r.uid, *doc))
			b.WriteString(`,"requestedAt":`)
			if iso, ok := signUpDateISO(r.requestedAt); ok {
				b.WriteString(jstr(iso))
			} else {
				b.WriteString(`null`)
			}
			b.WriteString(`}`)
		}
		b.WriteString(`]}`)
		res.JSON(200, []byte(b.String()))
	}
}
