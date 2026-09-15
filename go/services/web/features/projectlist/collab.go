// P4.10a — collaborator permission writes, access requests, leave, removal,
// ownership transfer. Node oracle (services/web/app/src/Features/):
//
//	Collaborators/CollaboratorsRouter.mjs     (route + middleware order)
//	Collaborators/CollaboratorsController.mjs  (zod schemas, 403/404 mapping)
//	Collaborators/CollaboratorsHandler.mjs     (mongo update shapes)
//	Collaborators/OwnershipTransferHandler.mjs (transfer sequence + emails)
//	Contacts/ContactManager.mjs                (touchContact both directions)
//
// Contracts (pinned by tests/e2e/specs/parity/web-go-p4col-flip.test.e2e.ts
// against the LIVE Node oracle, 2026-09-15):
//
//   - POST /project/:id/leave            (login only; $pull; 204 always)
//   - PUT  /project/:id/users/:uid       (admin; setLevel; 204)
//   - DELETE /project/:id/users/:uid     (admin; $pull; 204)
//   - POST /project/:id/request-access   (reader; 204; request-mail if new)
//   - DELETE /project/:id/access-requests/:uid (admin; 204; decline-mail)
//   - POST /project/:id/access-requests/:uid/grant (admin; 204; grant-mail)
//   - POST /project/:id/transfer-ownership   (admin; 204; 2 mails unless skip)
//
// Error shapes (exact, byte-pinned in the gate):
//
//   - Validation errors:
//     {"error":"Validation error: <msg> at \"<path>\"","statusCode":N}
//     body.* paths -> 400 ; params.* paths -> 404
//   - Simple messages:  {"message":"restricted"} (403)
//     {"message":"not found"} (PUT-set unmatched — JSON)
//     {"message":"user not found: <hex>"} (transfer)
//     {"message":"user <hex> should be a collaborator ..."} (403)
//   - 404 page (text/html general/404) when the PROJECT is missing under an
//     authorizing middleware, and for grant when the requester is no
//     member (Node global error path).
//   - Anonymous POST/PUT/DELETE: 403 text/plain "Forbidden" (core csrf first).
//
// Deferred to P4.10b (invite flows own these): grant's
// revokeInviteForUser is skipped here — the gate fixtures carry no invites
// and Node's revokeInvite no-ops on an empty invite set.
package projectlist

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

var (
	leavePat    = regexp.MustCompile(`^/project/([^/]+)/leave$`)
	reqAccPat   = regexp.MustCompile(`^/project/([^/]+)/request-access$`)
	userPat     = regexp.MustCompile(`^/project/([^/]+)/users/([^/]+)$`)
	accDeclPat  = regexp.MustCompile(`^/project/([^/]+)/access-requests/([^/]+)$`)
	accGrantPat = regexp.MustCompile(`^/project/([^/]+)/access-requests/([^/]+)/grant$`)
	xferPat     = regexp.MustCompile(`^/project/([^/]+)/transfer-ownership$`)
)

// ---------- wire error builders (byte-pinned) ----------

func colVa(msg, path string, status int) []byte {
	esc := func(x string) string {
		return strings.ReplaceAll(x, `"`, `\"`)
	}
	return []byte(`{"error":"Validation error: ` + esc(msg) + ` at \"` + esc(path) + `\"","statusCode":` + fmt.Sprintf("%d", status) + `}`)
}

const (
	colRestricted = `{"message":"restricted"}`
	colNotFound   = `{"message":"not found"}`
)

func colUserNotFound(hex string) []byte {
	return []byte(`{"message":"user not found: ` + hex + `"}`)
}

func colNotCollaborator(target, project string) []byte {
	return []byte(`{"message":"user ` + target + ` should be a collaborator in project ` + project + ` prior to ownership transfer"}`)
}

// ---------- shared gates (Node middleware order) ----------

// gateAdmin: login -> malformed Project_id (404 JSON) -> project load
// (404 HTML general/404) -> owner-or-site-admin (403 JSON restricted).
func gateAdmin(a *core.App, cxt *core.Cxt, res *core.Res) (string, *primitive.D, bool) {
	uid := gatedLogin(cxt, res)
	if uid == "" {
		return "", nil, false
	}
	oid, ok := paramProject(cxt.Params["1"], res)
	if !ok {
		return "", nil, false
	}
	doc, lerr := loadProjectFull(a, cxt, oid)
	if lerr != nil {
		res.JSON(500, []byte("internal error"))
		return "", nil, false
	}
	if doc == nil {
		views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
		return "", nil, false
	}
	if !canAdmin(uid, loadUserAdmin(a, cxt, uid), *doc) {
		if core.AcceptsJSON(cxt.Req) {
			res.JSON(403, []byte(colRestricted))
		} else {
			views.Restricted403(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
		}
		return "", nil, false
	}
	return uid, doc, true
}

// gateRead: same as gateAdmin but the access test is canRead (any reader) —
// the request-access route's ensureUserCanReadProject.
func gateRead(a *core.App, cxt *core.Cxt, res *core.Res) (string, *primitive.D, bool) {
	uid := gatedLogin(cxt, res)
	if uid == "" {
		return "", nil, false
	}
	oid, ok := paramProject(cxt.Params["1"], res)
	if !ok {
		return "", nil, false
	}
	doc, lerr := loadProjectFull(a, cxt, oid)
	if lerr != nil {
		res.JSON(500, []byte("internal error"))
		return "", nil, false
	}
	if doc == nil {
		views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
		return "", nil, false
	}
	if !canRead(uid, loadUserAdmin(a, cxt, uid), *doc) {
		if core.AcceptsJSON(cxt.Req) {
			res.JSON(403, []byte(colRestricted))
		} else {
			views.Restricted403(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
		}
		return "", nil, false
	}
	return uid, doc, true
}

func gatedLogin(cxt *core.Cxt, res *core.Res) string {
	if cxt.Sess == nil || cxt.Sess.UserIDHex() == "" {
		if core.AcceptsJSON(cxt.Req) {
			res.SendStatus(401)
		} else {
			res.Redirect(cxt.Req, 302, "/login")
		}
		return ""
	}
	return cxt.Sess.UserIDHex()
}

func paramProject(v string, res *core.Res) (primitive.ObjectID, bool) {
	return paramHex(v, "Project_id", res)
}

func paramUser(v string, res *core.Res) (primitive.ObjectID, bool) {
	return paramHex(v, "user_id", res)
}

func paramHex(v, name string, res *core.Res) (primitive.ObjectID, bool) {
	if !validOID.MatchString(v) {
		res.JSON(404, []byte(malformedMsg(name)))
		return primitive.ObjectID{}, false
	}
	o, err := primitive.ObjectIDFromHex(strings.ToLower(v))
	if err != nil {
		res.JSON(404, []byte(malformedMsg(name)))
		return primitive.ObjectID{}, false
	}
	return o, true
}

// ---------- body parsing (zod strictObject shape) ----------

// colBody returns the decoded body map (nil when the body is empty) after
// answering Node's express.json strict error (400 `{}`) for non-object roots.
func colBody(cxt *core.Cxt, res *core.Res) (map[string]any, bool) {
	raw, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
	if strings.TrimSpace(string(raw)) == "" {
		return nil, true
	}
	var bm map[string]any
	if err := json.Unmarshal(raw, &bm); err != nil || bm == nil {
		res.JSON(400, []byte("{}"))
		return nil, false
	}
	return bm, true
}

func colUnknownKey(bm map[string]any, allowed ...string) (string, bool) {
	for k := range bm {
		hit := false
		for _, a := range allowed {
			if k == a {
				hit = true
				break
			}
		}
		if !hit {
			return k, true
		}
	}
	return "", false
}

func colLevel(bm map[string]any, res *core.Res, levels []string) (string, bool) {
	var s string
	if v, ok := bm["privilegeLevel"]; ok {
		sv, isStr := v.(string)
		if !isStr {
			res.JSON(400, colVa("Invalid input: expected string, received "+zodReceived(v, true), "body.privilegeLevel", 400))
			return "", false
		}
		s = sv
	}
	for _, l := range levels {
		if s == l {
			return l, true
		}
	}
	joined := ""
	for i, l := range levels {
		if i > 0 {
			joined += "|"
		}
		joined += `"` + l + `"`
	}
	res.JSON(400, colVa("Invalid option: expected one of "+joined, "body.privilegeLevel", 400))
	return "", false
}

// colBoolOpt returns (value, ok). ok=false means the caller already wrote
// the zod error response and must return.
func colBoolOpt(bm map[string]any, res *core.Res, key string) (val bool, ok bool) {
	v, present := bm[key]
	if !present {
		return false, true
	}
	if b, isBool := v.(bool); isBool {
		return b, true
	}
	recv := "string"
	switch v.(type) {
	case float64, int, int64:
		recv = "number"
	case []any:
		recv = "array"
	case nil:
		recv = "null"
	case map[string]any:
		recv = "object"
	}
	res.JSON(400, colVa("Invalid input: expected boolean, received "+recv, "body."+key, 400))
	return false, false
}

// colStringOID validates body.user_id (transfer) -> (hex, ok); ok=false
// means the zod error response was already written.
func colStringOID(bm map[string]any, res *core.Res) (string, bool) {
	v, present := bm["user_id"]
	if !present {
		res.JSON(400, colVa("Invalid input: expected string, received undefined", "body.user_id", 400))
		return "", false
	}
	s, isStr := v.(string)
	if !isStr || !validOID.MatchString(s) {
		res.JSON(400, colVa("Invalid Mongo ObjectId", "body.user_id", 400))
		return "", false
	}
	return strings.ToLower(s), true
}

// ---------- mail (recipient + exact subject — gate parity) ----------

type colUserMail struct{ first, last, email string }

func colLoadUserMail(a *core.App, cxt *core.Cxt, hex string) (colUserMail, bool) {
	if a.Mongo == nil || hex == "" {
		return colUserMail{}, false
	}
	oid, err := primitive.ObjectIDFromHex(hex)
	if err != nil {
		return colUserMail{}, false
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 5*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return colUserMail{}, false
	}
	var d primitive.D
	opts := options.FindOne().SetProjection(bson.D{
		{Key: "first_name", Value: 1},
		{Key: "last_name", Value: 1},
		{Key: "email", Value: 1},
	})
	if db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}, opts).Decode(&d) != nil {
		return colUserMail{}, false
	}
	return colUserMail{
		first: asStr(dget(d, "first_name")),
		last:  asStr(dget(d, "last_name")),
		email: asStr(dget(d, "email")),
	}, true
}

// colSendMail delivers synchronously (Node fires async; the gate reads the
// sink after the whole battery, so delivery order is unobservable).
func colSendMail(to, subject, text string) {
	if to == "" {
		return
	}
	m := core.NewMail()
	_ = m.Send(to, subject, text, "<p>"+strings.ReplaceAll(text, "&", "&amp;")+"</p>")
}

// ---------- contacts (ContactManager.touchContact both directions) ----------

func colTouchContact(a *core.App, cxt *core.Cxt, userHex, contactHex string) {
	if a.Mongo == nil || userHex == "" || contactHex == "" {
		return
	}
	uo, err1 := primitive.ObjectIDFromHex(userHex)
	if err1 != nil {
		return
	}
	if _, err2 := primitive.ObjectIDFromHex(contactHex); err2 != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return
	}
	now := time.Now()
	_, _ = db.Collection("contacts").UpdateOne(
		ctx,
		bson.D{{Key: "user_id", Value: uo}},
		bson.D{
			{Key: "$inc", Value: bson.D{{Key: "contacts." + contactHex + ".n", Value: 1}}},
			{Key: "$set", Value: bson.D{{Key: "contacts." + contactHex + ".ts", Value: now}}},
		},
		options.Update().SetUpsert(true),
	)
}

func colAddContact(a *core.App, cxt *core.Cxt, aHex, bHex string) {
	colTouchContact(a, cxt, aHex, bHex)
	colTouchContact(a, cxt, bHex, aHex)
}

// ---------- core updates ----------

// colPullUser mirrors CollaboratorsHandler.removeUserFromProject (10 keys).
func colPullUser(a *core.App, cxt *core.Cxt, proj primitive.ObjectID, target string) {
	if a.Mongo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 8*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return
	}
	toid, err := primitive.ObjectIDFromHex(target)
	if err != nil {
		return
	}
	_, _ = db.Collection("projects").UpdateOne(ctx,
		bson.D{{Key: "_id", Value: proj}},
		bson.D{{Key: "$pull", Value: bson.D{
			{Key: "collaberator_refs", Value: toid},
			{Key: "readOnly_refs", Value: toid},
			{Key: "reviewer_refs", Value: toid},
			{Key: "pendingEditor_refs", Value: toid},
			{Key: "pendingReviewer_refs", Value: toid},
			{Key: "editAccessRequests", Value: bson.D{{Key: "userId", Value: toid}}},
			{Key: "tokenAccessReadOnly_refs", Value: toid},
			{Key: "tokenAccessReadAndWrite_refs", Value: toid},
			{Key: "archived", Value: toid},
			{Key: "trashed", Value: toid},
		}}},
	)
}

// colSetLevel mirrors setCollaboratorPrivilegeLevel. Returns matched>0.
func colSetLevel(a *core.App, cxt *core.Cxt, proj primitive.ObjectID, doc *primitive.D, target, level string) (bool, error) {
	if a.Mongo == nil {
		return false, nil
	}
	uid, err := primitive.ObjectIDFromHex(target)
	if err != nil {
		return false, nil
	}
	add := level
	var pulls bson.D
	switch level {
	case "readAndWrite":
		add = "collaberator_refs"
		pulls = bson.D{
			{Key: "readOnly_refs", Value: uid},
			{Key: "pendingEditor_refs", Value: uid},
			{Key: "reviewer_refs", Value: uid},
			{Key: "pendingReviewer_refs", Value: uid},
			{Key: "editAccessRequests", Value: bson.D{{Key: "userId", Value: uid}}},
			{Key: "tokenAccessReadOnly_refs", Value: uid},
			{Key: "tokenAccessReadAndWrite_refs", Value: uid},
		}
	case "review":
		add = "reviewer_refs"
		pulls = bson.D{
			{Key: "readOnly_refs", Value: uid},
			{Key: "pendingEditor_refs", Value: uid},
			{Key: "collaberator_refs", Value: uid},
			{Key: "pendingReviewer_refs", Value: uid},
			{Key: "editAccessRequests", Value: bson.D{{Key: "userId", Value: uid}}},
			{Key: "tokenAccessReadOnly_refs", Value: uid},
			{Key: "tokenAccessReadAndWrite_refs", Value: uid},
		}
	case "readOnly":
		add = "readOnly_refs"
		pulls = bson.D{
			{Key: "collaberator_refs", Value: uid},
			{Key: "reviewer_refs", Value: uid},
			{Key: "editAccessRequests", Value: bson.D{{Key: "userId", Value: uid}}},
			{Key: "tokenAccessReadOnly_refs", Value: uid},
			{Key: "tokenAccessReadAndWrite_refs", Value: uid},
			{Key: "pendingEditor_refs", Value: uid},
			{Key: "pendingReviewer_refs", Value: uid},
		}
	default:
		return false, nil
	}
	setPart := bson.D{}
	if level == "review" {
		// Node: setCollaboratorPrivilegeLevel — if the project has a
		// track_changes object that ALREADY truthy-references the target,
		// replace the whole field with {target:true}; otherwise dot-key $set
		// (preserving other entries).
		whole := false
		if v, present := colTCValue(doc); present {
			switch m := v.(type) {
			case primitive.D:
				for i := range m {
					if m[i].Key == target {
						if b, ok := m[i].Value.(bool); ok && b {
							whole = true
						}
						break
					}
				}
			case map[string]any:
				if b, ok := m[target].(bool); ok && b {
					whole = true
				}
			}
		}
		if whole {
			setPart = bson.D{{Key: "track_changes", Value: bson.D{{Key: target, Value: true}}}}
		} else {
			setPart = bson.D{{Key: "track_changes." + target, Value: true}}
		}
	}
	update := bson.D{
		{Key: "$pull", Value: pulls},
		{Key: "$addToSet", Value: bson.D{{Key: add, Value: uid}}},
	}
	if len(setPart) > 0 {
		update = append(update, bson.E{Key: "$set", Value: setPart})
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 8*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false, err
	}
	res, err := db.Collection("projects").UpdateOne(ctx,
		bson.D{{Key: "_id", Value: proj}, {Key: "$or", Value: bson.A{
			bson.D{{Key: "collaberator_refs", Value: uid}},
			bson.D{{Key: "readOnly_refs", Value: uid}},
			bson.D{{Key: "reviewer_refs", Value: uid}},
			bson.D{{Key: "tokenAccessReadOnly_refs", Value: uid}},
			bson.D{{Key: "tokenAccessReadAndWrite_refs", Value: uid}},
		}}},
		update,
	)
	if err != nil {
		return false, err
	}
	return res.MatchedCount > 0, nil
}

func colTCValue(doc *primitive.D) (any, bool) {
	for i := range *doc {
		if (*doc)[i].Key == "track_changes" {
			return (*doc)[i].Value, true
		}
	}
	return nil, false
}

// colTCMembersState: Node convertTrackChangesToExplicitFormat(track_changes===true)
// — every member at owner/readAndWrite/review gets true (gate-uncovered).
func colTCMembersState(doc *primitive.D, target string) bson.D {
	out := bson.D{}
	seen := map[string]bool{}
	addOne := func(hex string) {
		if hex == "" || seen[hex] {
			return
		}
		seen[hex] = true
		out = append(out, bson.E{Key: hex, Value: true})
	}
	addOne(oidHex(dget(*doc, "owner_ref")))
	for _, k := range []string{"collaberator_refs", "reviewer_refs"} {
		if arr, ok := dget(*doc, k).(primitive.A); ok {
			for _, m := range arr {
				addOne(oidHex(m))
			}
		}
	}
	addOne(target)
	return out
}

// ---------- handlers ----------

// POST /project/:id/leave — login only; unconditional $pull; 204.
func leaveHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		uid := gatedLogin(cxt, res)
		if uid == "" {
			return
		}
		oid, ok := paramProject(cxt.Params["1"], res)
		if !ok {
			return
		}
		colPullUser(a, cxt, oid, uid)
		res.NoContent()
	}
}

// PUT /project/:id/users/:uid — admin; setLevel; 204 / 404 JSON "not found".
func setUserLevelHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		_, doc, ok := gateAdmin(a, cxt, res)
		if !ok {
			return
		}
		_, okU := paramUser(cxt.Params["2"], res)
		if !okU {
			return
		}
		target := strings.ToLower(strings.TrimSpace(cxt.Params["2"]))
		bm, okB := colBody(cxt, res)
		if !okB {
			return
		}
		if k, bad := colUnknownKey(bm, "privilegeLevel"); bad {
			res.JSON(400, colVa(`Unrecognized key: "`+k+`"`, "body", 400))
			return
		}
		level, okL := colLevel(bm, res, []string{"readOnly", "readAndWrite", "review"})
		if !okL {
			return
		}
		// doc was loaded by the gate; re-derive the project oid via the doc id.
		proj, _ := (*doc)[0].Value.(primitive.ObjectID)
		matched, err := colSetLevel(a, cxt, proj, doc, target, level)
		if err != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		if !matched {
			res.JSON(404, []byte(colNotFound))
			return
		}
		res.NoContent()
	}
}

// DELETE /project/:id/users/:uid — admin; $pull; 204 (unknown user no-op).
func removeUserHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		_, doc, ok := gateAdmin(a, cxt, res)
		if !ok {
			return
		}
		if cxt.Params["2"] != "" {
			if _, okU := paramUser(cxt.Params["2"], res); !okU {
				return
			}
		} else {
			res.JSON(404, []byte(malformedMsg("user_id")))
			return
		}
		target := strings.ToLower(cxt.Params["2"])
		proj, _ := (*doc)[0].Value.(primitive.ObjectID)
		colPullUser(a, cxt, proj, target)
		res.NoContent()
	}
}

// POST /project/:id/request-access — reader; upsert editAccessRequests.
func requestAccessHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		uid, doc, ok := gateRead(a, cxt, res)
		if !ok {
			return
		}
		bm, okB := colBody(cxt, res)
		if !okB {
			return
		}
		if k, bad := colUnknownKey(bm, "privilegeLevel"); bad {
			res.JSON(400, colVa(`Unrecognized key: "`+k+`"`, "body", 400))
			return
		}
		level, okL := colLevel(bm, res, []string{"readAndWrite", "review"})
		if !okL {
			return
		}
		// requestableLevels keyed by CURRENT level (Node controller):
		// readOnly -> [rw, review]; review -> [rw]; owner/editor -> none.
		cur := currentPrivLevel(uid, *doc)
		allowed := false
		if cur == "readOnly" {
			allowed = level == "readAndWrite" || level == "review"
		} else if cur == "review" {
			allowed = level == "readAndWrite"
		}
		if !allowed {
			if core.AcceptsJSON(cxt.Req) {
				res.JSON(403, []byte(colRestricted))
			} else {
				views.Restricted403(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
			}
			return
		}
		proj, _ := (*doc)[0].Value.(primitive.ObjectID)
		// rebuild editAccessRequests = (existing minus self) + new entry.
		old := reqEntries(*doc)
		kept := []bson.D{}
		isNew := true
		for _, e := range old {
			if eid, _ := e[0].Value.(primitive.ObjectID); eid.Hex() == uid {
				isNew = false
				continue
			}
			kept = append(kept, e)
		}
		uto, _ := primitive.ObjectIDFromHex(uid)
		kept = append(kept, bson.D{
			{Key: "userId", Value: uto},
			{Key: "privilegeLevel", Value: level},
			{Key: "requestedAt", Value: time.Now()},
		})
		if a.Mongo != nil {
			ctx, cancel := context.WithTimeout(cxt.Req.Context(), 8*time.Second)
			defer cancel()
			if db, err := a.Mongo.DB(ctx); err == nil {
				_, _ = db.Collection("projects").UpdateOne(ctx,
					bson.D{{Key: "_id", Value: proj}},
					bson.D{{Key: "$set", Value: bson.D{{Key: "editAccessRequests", Value: kept}}}},
				)
			}
		}
		if isNew {
			name := asStr(dget(*doc, "name"))
			roleWord := "editor"
			if level == "review" {
				roleWord = "reviewer"
			}
			reqMail, _ := colLoadUserMail(a, cxt, uid)
			oHex := oidHex(dget(*doc, "owner_ref"))
			ownerMail, _ := colLoadUserMail(a, cxt, oHex)
			if ownerMail.email != "" && reqMail.email != "" {
				subj := fmt.Sprintf("%s %s (%s) requested %s access to %s - OlliTeX",
					reqMail.first, reqMail.last, reqMail.email, roleWord, name)
				colSendMail(ownerMail.email, subj, subj)
			}
		}
		res.NoContent()
	}
}

func reqEntries(d primitive.D) []bson.D {
	v := dget(d, "editAccessRequests")
	arr, ok := v.(primitive.A)
	if !ok {
		return nil
	}
	out := []bson.D{}
	for _, m := range arr {
		if ed, ok2 := m.(primitive.D); ok2 {
			out = append(out, bson.D(ed))
			continue
		}
		// re-encode non-D shapes (gate: never happens in practice)
		if bdata, err := bson.Marshal(m); err == nil {
			var ed primitive.D
			if bson.Unmarshal(bdata, &ed) == nil {
				out = append(out, bson.D(ed))
			}
		}
	}
	return out
}

// DELETE /project/:id/access-requests/:uid — admin; decline; 204.
func declineReqHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		_, doc, ok := gateAdmin(a, cxt, res)
		if !ok {
			return
		}
		_, okU := paramUser(cxt.Params["2"], res)
		if !okU {
			return
		}
		target := strings.ToLower(cxt.Params["2"])
		bm, okB := colBody(cxt, res)
		if !okB {
			return
		}
		if k, bad := colUnknownKey(bm, "notify"); bad {
			res.JSON(400, colVa(`Unrecognized key: "`+k+`"`, "body", 400))
			return
		}
		notify, okN := colBoolOpt(bm, res, "notify")
		if !okN {
			return
		}
		proj, _ := (*doc)[0].Value.(primitive.ObjectID)
		old := reqEntries(*doc)
		kept := []bson.D{}
		removed := false
		for _, e := range old {
			if eid, _ := e[0].Value.(primitive.ObjectID); eid.Hex() == target {
				removed = true
				continue
			}
			kept = append(kept, e)
		}
		if removed {
			if a.Mongo != nil {
				ctx, cancel := context.WithTimeout(cxt.Req.Context(), 8*time.Second)
				defer cancel()
				if db, err := a.Mongo.DB(ctx); err == nil {
					_, _ = db.Collection("projects").UpdateOne(ctx,
						bson.D{{Key: "_id", Value: proj}},
						bson.D{{Key: "$set", Value: bson.D{{Key: "editAccessRequests", Value: kept}}}},
					)
				}
			}
			if notify {
				name := asStr(dget(*doc, "name"))
				reqMail, okM := colLoadUserMail(a, cxt, target)
				if okM && reqMail.email != "" {
					subj := "Your access request to " + name + " was declined - OlliTeX"
					colSendMail(reqMail.email, subj, subj)
				}
			}
		}
		res.NoContent()
	}
}

// POST /project/:id/access-requests/:uid/grant — admin; setLevel + notify.
func grantReqHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		_, doc, ok := gateAdmin(a, cxt, res)
		if !ok {
			return
		}
		_, okU := paramUser(cxt.Params["2"], res)
		if !okU {
			return
		}
		target := strings.ToLower(cxt.Params["2"])
		bm, okB := colBody(cxt, res)
		if !okB {
			return
		}
		if k, bad := colUnknownKey(bm, "privilegeLevel", "notify"); bad {
			res.JSON(400, colVa(`Unrecognized key: "`+k+`"`, "body", 400))
			return
		}
		level, okL := colLevel(bm, res, []string{"readAndWrite", "review"})
		if !okL {
			return
		}
		notify, okN := colBoolOpt(bm, res, "notify")
		if !okN {
			return
		}
		proj, _ := (*doc)[0].Value.(primitive.ObjectID)
		// hadRequest up front (Node: captured before setLevel clears it).
		hadRequest := false
		for _, e := range reqEntries(*doc) {
			if eid, _ := e[0].Value.(primitive.ObjectID); eid.Hex() == target {
				hadRequest = true
				break
			}
		}
		matched, err := colSetLevel(a, cxt, proj, doc, target, level)
		if err != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		if !matched {
			// Node: NotFoundError escapes to the global error handler —
			// the gate pins the HTML 404 page (accept-independent here).
			views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
			return
		}
		if hadRequest && notify {
			name := asStr(dget(*doc, "name"))
			reqMail, okM := colLoadUserMail(a, cxt, target)
			if okM && reqMail.email != "" {
				subj := "Your access request to " + name + " was granted - OlliTeX"
				colSendMail(reqMail.email, subj, subj)
			}
		}
		res.NoContent()
	}
}

// POST /project/:id/transfer-ownership — admin; see handler for the oracle
// sequence (remove new owner -> set owner -> re-add prev owner as editor +
// contacts -> 2 emails unless skipEmails).
func transferOwnerHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		_, doc, ok := gateAdmin(a, cxt, res)
		if !ok {
			return
		}
		bm, okB := colBody(cxt, res)
		if !okB {
			return
		}
		if k, bad := colUnknownKey(bm, "user_id", "skipEmails"); bad {
			res.JSON(400, colVa(`Unrecognized key: "`+k+`"`, "body", 400))
			return
		}
		to, okT := colStringOID(bm, res)
		if !okT {
			return
		}
		skipEmails, okS := colBoolOpt(bm, res, "skipEmails")
		if !okS {
			return
		}

		proj, _ := (*doc)[0].Value.(primitive.ObjectID)
		ownerHex := oidHex(dget(*doc, "owner_ref"))
		if to == ownerHex {
			res.NoContent()
			return
		}
		// user must exist (Node: _getUser -> user not found 404 JSON).
		toMail, okU := colLoadUserMail(a, cxt, to)
		if !okU {
			res.JSON(404, colUserNotFound(to))
			return
		}
		// collaborator check (site-admin bypass — allowTransferToNonCollaborators)
		isCollab := inOIDList(dget(*doc, "collaberator_refs"), to) ||
			inOIDList(dget(*doc, "reviewer_refs"), to) ||
			inOIDList(dget(*doc, "readOnly_refs"), to)
		siteAdmin := false
		if cxt.Sess != nil {
			siteAdmin = loadUserAdmin(a, cxt, cxt.Sess.UserIDHex())
		}
		if !isCollab && !siteAdmin {
			if core.AcceptsJSON(cxt.Req) {
				res.JSON(403, colNotCollaborator(to, proj.Hex()))
			} else {
				views.Restricted403(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
			}
			return
		}

		// 1) remove the new owner from every ref array.
		colPullUser(a, cxt, proj, to)
		// 2) new owner.
		if a.Mongo != nil {
			ctx, cancel := context.WithTimeout(cxt.Req.Context(), 8*time.Second)
			defer cancel()
			if db, err := a.Mongo.DB(ctx); err == nil {
				_, _ = db.Collection("projects").UpdateOne(ctx,
					bson.D{{Key: "_id", Value: proj}},
					bson.D{{Key: "$set", Value: bson.D{{Key: "owner_ref", Value: toObjectID(to)}}}},
				)
			}
		}
		// 3) previous owner back as editor (unless already a member) + contacts.
		if ownerHex != "" &&
			!inOIDList(dget(*doc, "collaberator_refs"), ownerHex) &&
			!inOIDList(dget(*doc, "reviewer_refs"), ownerHex) &&
			!inOIDList(dget(*doc, "readOnly_refs"), ownerHex) {
			colAddContact(a, cxt, to, ownerHex)
			if a.Mongo != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				if db, err := a.Mongo.DB(ctx); err == nil {
					_, _ = db.Collection("projects").UpdateOne(ctx,
						bson.D{{Key: "_id", Value: proj}},
						bson.D{{Key: "$addToSet", Value: bson.D{{Key: "collaberator_refs", Value: toObjectID(ownerHex)}}}},
					)
				}
			}
		}
		// 4) background flush to the document updater (Node TpdsProjectFlusher;
		// needs overleaf.history.id — real projects only — best-effort, pinned
		// by the state gate, not by service state).
		_ = fireHTTP(cxt, "POST", cduBase()+"/project/"+proj.Hex()+"/flush", nil)
		// 5) confirmation mails to BOTH sides (Node _sendEmails).
		if !skipEmails {
			prevMail, _ := colLoadUserMail(a, cxt, ownerHex)
			subj := "Project ownership transfer - OlliTeX"
			if prevMail.email != "" {
				colSendMail(prevMail.email, subj, subj)
			}
			if toMail.email != "" {
				colSendMail(toMail.email, subj, subj)
			}
		}
		res.NoContent()
	}
}

func toObjectID(hex string) primitive.ObjectID {
	o, _ := primitive.ObjectIDFromHex(hex)
	return o
}
