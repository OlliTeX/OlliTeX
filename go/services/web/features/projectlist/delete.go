package projectlist

// P4 delete/restore — DELETE /Project/:Project_id (hard delete) and
// POST /Project/:Project_id/restore.
//
// Node sources (oracle, pinned live 2026-09-14):
//
//	router.mjs
//	  webRouter.delete('/Project/:Project_id', requireLogin, ensureUserCanAdminProject, ProjectController.deleteProject)
//	  webRouter.post('/Project/:Project_id/restore', requireLogin, ensureUserCanAdminProject, ProjectController.restoreProject)
//	ProjectController.deleteProject
//	  -> ProjectDeleter.deleteProject(projectId, {deleterUser, ipAddress: req.ip, deletedReason: 'user'})
//	  -> ProjectAuditLogHandler.addEntryIfManagedInBackground('project-deleted')  (NO-OP in free build)
//	  -> res.sendStatus(200)                      (200 text/plain "OK")
//	ProjectDeleter.deleteProject
//	  1. Project.findOne (the canAdmin middleware already 404s missing projects first)
//	  2. flushProjectToMongoAndDelete = DELETE {document-updater}/project/{id}  (204)
//	     fallback (only on failure): POST {document-updater}/project/{id}/flush
//	       -> POST {project-history}/project/{id}/flush
//	           (on failure) POST {project-history}/project/{id}/resync {force:true}
//	       -> retry DELETE {document-updater}/project/{id}
//	  3. DocstoreManager.archiveProject = POST {docstore}/project/{id}/archive  (best-effort)
//	  4. per member (owner+collab+ro+reviewer): tags.updateMany({user_id}, {$pull: {project_ids: id}})
//	      (no tags collection in this build -> no-op)
//	  5. deletedProjects.updateOne(
//	       { deleterData.deletedProjectId: id },
//	       { project: <full project doc>, deleterData: {...} }, { upsert: true }  (+Mongoose __v:0)
//	  6. Project.deleteOne({_id})
//	  7. hooks 'projectDeleted'  (no listeners in this build -> no-op)
//	ProjectController.restoreProject
//	  -> Project.updateOne({_id}, {$unset: {archived: true}})
//	  -> audit (no-op) -> res.sendStatus(200)
//
// Live-oracle pins (A/B, node side):
//   - 200 text/plain "OK"                           (owner or server-admin, existing project)
//   - 404 text/html NotFoundPage                    (missing project — canAdmin middleware)
//   - 404 application/json {"error":"Validation error: Invalid Mongo ObjectId at
//     \"params.Project_id\"","statusCode":404}      (malformed id)
//   - 403 application/json {"message":"restricted"} (logged-in non-member, non-admin)
//   - 403 text/plain "Forbidden"                    (anonymous — CSRF before the route guards)
//   - restore on an archived project removes the `archived` field entirely ($unset).
//
// The cross-service side effects (document-updater flush/delete, docstore
// archive) hit the SAME endpoints Node calls, so their state effect is
// identical for both implementations (no-op persistor backend in this build).

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

var delPat = regexp.MustCompile(`^/Project/([^/]+)$`)
var restPat = regexp.MustCompile(`^/Project/([^/]+)/restore$`)

// cduBase — document-updater service base (same env naming as create.go's
// WEB_DOCSTORE_URL / WEB_PROJECT_HISTORY_URL).
func cduBase() string { return crEnvOr("WEB_DOCUMENT_UPDATER_URL", "http://127.0.0.1:3003") }

// fireHTTP is a best-effort service call (result ignored — Node's
// fetchNothing-and-warn semantics for the delete flow).
func fireHTTP(cxt *core.Cxt, method, url string, body []byte) bool {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(cxt.Req.Context(), method, url, rd)
	if err != nil {
		return false
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := crHTTP.Do(req)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// loadProjectFull returns the UNPROJECTED project doc (the delete record must
// embed the whole document), or (nil, nil) when it does not exist.
func loadProjectFull(a *core.App, cxt *core.Cxt, oid primitive.ObjectID) (*primitive.D, error) {
	if a.Mongo == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 10*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, err
	}
	var d primitive.D
	if err := db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&d); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

// deleteProjectExec mirrors ProjectDeleter.deleteProject side effects (steps
// 2–6 above). The caller verified canAdmin + project existence.
func deleteProjectExec(a *core.App, cxt *core.Cxt, oid primitive.ObjectID, project primitive.D, uid, ip string) {
	pid := oid.Hex()
	hist := strings.TrimSuffix(crHistoryBase(), "/")
	ds := strings.TrimSuffix(crDocstoreBase(), "/")

	// 2. document-updater flush+delete, with Node's exact fallback chain.
	if !fireHTTP(cxt, "DELETE", cduBase()+"/project/"+pid, nil) {
		fireHTTP(cxt, "POST", cduBase()+"/project/"+pid+"/flush", nil)
		if !fireHTTP(cxt, "POST", hist+"/project/"+pid+"/flush", nil) {
			fireHTTP(cxt, "POST", hist+"/project/"+pid+"/resync", []byte(`{"force":true}`))
		}
		fireHTTP(cxt, "DELETE", cduBase()+"/project/"+pid, nil)
	}

	// 3. docstore archive (best-effort; no-op persistor backend in this build).
	fireHTTP(cxt, "POST", ds+"/project/"+pid+"/archive", nil)

	// 4. per-member tag pulls (owner + collaborators + read-only + reviewers).
	{
		members := []primitive.ObjectID{}
		if v, ok := dget(project, "owner_ref").(primitive.ObjectID); ok {
			members = append(members, v)
		}
		for _, key := range []string{"collablator_refs", "readOnly_refs", "reviewer_refs"} {
			if arr, ok := dget(project, key).(primitive.A); ok {
				for _, m := range arr {
					if mo, ok := m.(primitive.ObjectID); ok {
						members = append(members, mo)
					}
				}
			}
		}
		if a.Mongo != nil {
			ctx, cancel := context.WithTimeout(cxt.Req.Context(), 5*time.Second)
			defer cancel()
			if db, err := a.Mongo.DB(ctx); err == nil {
				for _, m := range members {
					_, _ = db.Collection("tags").UpdateMany(
						ctx,
						bson.D{{Key: "user_id", Value: m}},
						bson.D{{Key: "$pull", Value: bson.D{{Key: "project_ids", Value: oid}}}})
				}
			}
		}
	}

	// 5. deletedProjects upsert (Node: {project, deleterData} + Mongoose __v).
	deleterData := ddBuildDeleterData(oid, project, "user", uid, ip)
	if a.Mongo != nil {
		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 8*time.Second)
		defer cancel()
		if db, err := a.Mongo.DB(ctx); err == nil {
			_, _ = db.Collection("deletedProjects").UpdateOne(
				ctx,
				bson.D{{Key: "deleterData.deletedProjectId", Value: oid}},
				bson.D{{Key: "$set", Value: bson.D{
					{Key: "project", Value: project},
					{Key: "deleterData", Value: deleterData},
					{Key: "__v", Value: 0},
				}}},
				options.Update().SetUpsert(true))
		}
	}

	// 6. remove the project doc.
	if a.Mongo != nil {
		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 5*time.Second)
		defer cancel()
		if db, err := a.Mongo.DB(ctx); err == nil {
			_, _ = db.Collection("projects").DeleteOne(ctx, bson.D{{Key: "_id", Value: oid}})
		}
	}
}

// ddRef is a (deleterData key, project ref-array field) pair — the exact
// spelling of the project-side key matters (node schema typo 'collablator').
type ddRef struct{ key, src string }

var ddRefs = []ddRef{
	{"deletedProjectCollaboratorIds", "collablator_refs"},
	{"deletedProjectReadOnlyIds", "readOnly_refs"},
	{"deletedProjectReviewerIds", "reviewer_refs"},
	{"deletedProjectReadWriteTokenAccessIds", "tokenAccessReadAndWrite_refs"},
	{"deletedProjectReadOnlyTokenAccessIds", "tokenAccessReadOnly_refs"},
}

// ddBuildDeleterData mirrors the Node storred deleterData: `_id` FIRST, the
// remaining present keys in ASCII-sorted order (Node/Mongoose stores it that
// way — pinned live: [_id, deletedAt, deletedProjectCollaboratorIds, ...]).
// The optional keys Node drops when undefined (deletedProjectOverleafId and
// the two token keys) are omitted here as well.
func ddBuildDeleterData(pid primitive.ObjectID, project primitive.D, reason, uid, ip string) primitive.D {
	fields := bson.D{bson.E{Key: "deletedAt", Value: time.Now().UTC()}}
	if uuid, err := primitive.ObjectIDFromHex(uid); err == nil {
		fields = append(fields, bson.E{Key: "deleterId", Value: uuid})
	}
	if ip != "" {
		fields = append(fields, bson.E{Key: "deleterIpAddress", Value: ip})
	}
	fields = append(fields,
		bson.E{Key: "deletedReason", Value: reason},
		bson.E{Key: "deletedProjectId", Value: pid})
	if v, ok := dget(project, "owner_ref").(primitive.ObjectID); ok {
		fields = append(fields, bson.E{Key: "deletedProjectOwnerId", Value: v})
	}
	for _, rr := range ddRefs {
		val := []primitive.ObjectID{}
		if arr, ok := dget(project, rr.src).(primitive.A); ok {
			for _, m := range arr {
				if mo, ok := m.(primitive.ObjectID); ok {
					val = append(val, mo)
				}
			}
		}
		fields = append(fields, bson.E{Key: rr.key, Value: val})
	}
	if overleaf, ok := dget(project, "overleaf").(primitive.D); ok {
		if ov, ok := dget(overleaf, "id").(string); ok && ov != "" {
			fields = append(fields, bson.E{Key: "deletedProjectOverleafId", Value: ov})
		}
		if hist, ok := dget(overleaf, "history").(primitive.D); ok {
			if hid, ok := dget(hist, "id").(string); ok && hid != "" {
				fields = append(fields, bson.E{Key: "deletedProjectOverleafHistoryId", Value: hid})
			}
		}
	}
	if tokens, ok := dget(project, "tokens").(primitive.D); ok {
		if rw, ok := dget(tokens, "readAndWrite").(string); ok && rw != "" {
			fields = append(fields, bson.E{Key: "deletedProjectReadWriteToken", Value: rw})
		}
		if ro, ok := dget(tokens, "readOnly").(string); ok && ro != "" {
			fields = append(fields, bson.E{Key: "deletedProjectReadOnlyToken", Value: ro})
		}
	}
	// Node: deletedProjectLastUpdatedAt: project.lastUpdated (BSON date).
	// driver v1 decodes dates to primitive.DateTime into primitive.D.
	switch v := dget(project, "lastUpdated").(type) {
	case primitive.DateTime:
		fields = append(fields, bson.E{Key: "deletedProjectLastUpdatedAt", Value: v.Time()})
	case time.Time:
		fields = append(fields, bson.E{Key: "deletedProjectLastUpdatedAt", Value: v})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Key < fields[j].Key })
	return append(primitive.D{bson.E{Key: "_id", Value: primitive.NewObjectID()}}, fields...)
}

// delAuthGate runs the route guards in Node's middleware order: login ->
// malformed-404 -> 404 (missing project) -> 403 (canAdmin fail). `full`
// selects the unprojected load (delete) vs the access-projection load
// (restore). On any early response the handler must return without writing
// anything further.
func delAuthGate(a *core.App, cxt *core.Cxt, res *core.Res, full bool) (uid string, oid *primitive.ObjectID, doc *primitive.D, okFlag bool) {
	if cxt.Sess == nil || cxt.Sess.UserIDHex() == "" {
		if core.AcceptsJSON(cxt.Req) {
			res.SendStatus(401)
		} else {
			res.Redirect(cxt.Req, 302, "/login")
		}
		return
	}
	uid = cxt.Sess.UserIDHex()
	param := cxt.Params["1"]
	if !validOID.MatchString(param) {
		res.JSON(404, []byte(malformedMsg("Project_id")))
		return
	}
	o, err := primitive.ObjectIDFromHex(strings.ToLower(param))
	if err != nil {
		res.JSON(404, []byte(malformedMsg("Project_id")))
		return
	}
	var d *primitive.D
	var lerr error
	if full {
		d, lerr = loadProjectFull(a, cxt, o)
	} else {
		d, lerr = loadProject(a, cxt, o)
	}
	if lerr != nil {
		res.JSON(500, []byte("internal error"))
		return
	}
	if d == nil {
		views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
		return
	}
	if !canAdmin(uid, loadUserAdmin(a, cxt, uid), *d) {
		if core.AcceptsJSON(cxt.Req) {
			res.JSON(403, []byte(`{"message":"restricted"}`))
		} else {
			views.Restricted403(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
		}
		return
	}
	oid = &o
	doc = d
	okFlag = true
	return
}

func delProjectHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		_, oid, doc, ok := delAuthGate(a, cxt, res, true)
		if !ok {
			return
		}
		deleteProjectExec(a, cxt, *oid, *doc, cxt.Sess.UserIDHex(), core.ClientIP(cxt.Req))
		res.SendStatus(200)
	}
}

func restoreProjectHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		_, oid, _, ok := delAuthGate(a, cxt, res, false)
		if !ok {
			return
		}
		if a.Mongo != nil {
			ctx, cancel := context.WithTimeout(cxt.Req.Context(), 8*time.Second)
			defer cancel()
			if db, err := a.Mongo.DB(ctx); err == nil {
				_, _ = db.Collection("projects").UpdateOne(
					ctx,
					bson.D{{Key: "_id", Value: *oid}},
					bson.D{{Key: "$unset", Value: bson.D{{Key: "archived", Value: true}}}})
			}
		}
		res.SendStatus(200)
	}
}
