package projectlist

import (
	"context"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

// P4.6 — project flag-writes (per-user archived / trashed sets). Node sources
// (oracle, pinned live 2026-09-14):
//
//	router.mjs:
//	  POST   /Project/:Project_id/archive  (requireLogin, ensureUserCanReadProject)  -> archiveProject
//	  DELETE /Project/:Project_id/archive  (same guard)                              -> unarchiveProject
//	  POST   /project/:project_id/trash    (same guard)                              -> trashProject
//	  DELETE /project/:project_id/trash    (same guard)                              -> untrashProject
//	ProjectController.*:
//	  ProjectDeleter.<op>(projectId, userId)  (mutates the per-user set)
//	  -> ProjectAuditLogHandler.addEntryIfManagedInBackground(...)   (NO-OP in the
//	     free build: findManagedSubscriptions returns [] -> NO audit entry)
//	  -> res.sendStatus(200)                                       (body "OK")
//	ProjectDeleter (ProjectDeleter.mjs:156-188):
//	  archive   : $addToSet {archived: uid} , $pull  {trashed:  uid}
//	  unarchive : $pull     {archived: uid}
//	  trash     : $addToSet {trashed:  uid} , $pull  {archived: uid}
//	  untrash   : $pull     {trashed:  uid}
//
// `archived` / `trashed` are ARRAYS of user ObjectIDs (per-user sets), NOT
// booleans — each user's view is archived/trashed independently. The same
// $addToSet/$pull operations are executed by both stacks against the same
// document, so the resulting Mongo state is identical.
//
// Contract (live oracle): 200 text/plain "OK" (each op, when permitted);
// the per-user set is updated as above; NO audit entry. Errors:
//   - anon  -> 403 text/plain "Forbidden" (CSRF before requireLogin)
//   - invalid (non-hex) id -> 404 application/json malformed (not accept-dep)
//   - valid id, absent     -> 404 HTML general/404 (not accept-dep)
//   - present, no read access -> 403 json {"message":"restricted"} | html Restricted
var archPat = regexp.MustCompile(`^/Project/([^/]+)/archive$`)
var trashPat = regexp.MustCompile(`^/project/([^/]+)/trash$`)

// flagOp is one of the four set operations (the exact Node $addToSet/$pull).
type flagOp struct {
	add       string // field to $addToSet uid ("" = none)
	pull      []string
	paramName string // param name for the malformed-404 message (Project_id | project_id)
}

var (
	opArchive   = flagOp{add: "archived", pull: []string{"trashed"}, paramName: "Project_id"}
	opUnarchive = flagOp{add: "", pull: []string{"archived"}, paramName: "Project_id"}
	opTrash     = flagOp{add: "trashed", pull: []string{"archived"}, paramName: "project_id"}
	opUntrash   = flagOp{add: "", pull: []string{"trashed"}, paramName: "project_id"}
)

// malformedMsg mirrors Node's 404 JSON whose message embeds the route's param
// name (archive/unarchive -> params.Project_id; trash/untrash -> params.project_id).
func malformedMsg(paramName string) string {
	return `{"error":"Validation error: Invalid Mongo ObjectId at \"params.` + paramName + `\"","statusCode":404}`
}

func flagHandler(a *core.App, op flagOp) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		// POST/DELETE: the core's csrf check runs FIRST and bounces anonymous
		// to 403 (Forbidden) before this handler; requireLogin (401/302) is the
		// route guard but is unreachable for anon here. Kept for completeness.
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
			res.JSON(404, []byte(malformedMsg(op.paramName)))
			return
		}
		oid, err := primitive.ObjectIDFromHex(strings.ToLower(param))
		if err != nil {
			res.JSON(404, []byte(malformedMsg(op.paramName)))
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
		// guard = ensureUserCanReadProject = canUserReadProject (mirror canRead).
		if !canRead(uid, loadUserAdmin(a, cxt, uid), *doc) {
			if core.AcceptsJSON(cxt.Req) {
				res.JSON(403, []byte(`{"message":"restricted"}`))
			} else {
				views.Restricted403(res.W, pageBase(cxt, relPath))
			}
			return
		}

		applyFlagOp(a, cxt, oid, op, uid)
		res.SendStatus(200)
	}
}

// applyFlagOp executes the exact Node $addToSet/$pull update for the per-user
// archived/trashed sets.
func applyFlagOp(a *core.App, cxt *core.Cxt, oid primitive.ObjectID, op flagOp, uid string) {
	if a.Mongo == nil || uid == "" {
		return
	}
	uidOID, err := primitive.ObjectIDFromHex(uid)
	if err != nil {
		return
	}
	var update bson.D
	if op.add != "" {
		update = append(update, bson.E{Key: "$addToSet", Value: bson.D{{Key: op.add, Value: uidOID}}})
	}
	// combine any $pull fields into a single $pull document (Node may pull
	// archived+trashed in one update; both map to one $pull op).
	if len(op.pull) > 0 {
		pd := bson.D{}
		for _, f := range op.pull {
			pd = append(pd, bson.E{Key: f, Value: uidOID})
		}
		update = append(update, bson.E{Key: "$pull", Value: pd})
	}
	if len(update) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 8*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return
	}
	_, _ = db.Collection("projects").UpdateOne(ctx, bson.D{{Key: "_id", Value: oid}}, update)
}
