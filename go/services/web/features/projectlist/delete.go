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
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

var delPat = regexp.MustCompile(`^/Project/([^/]+)$`)
var restPat = regexp.MustCompile(`^(?i)/project/([^/]+)/restore$`)

// pidHexOf — ObjectID → 24-hex (the store key shape).
func pidHexOf(o bson.ObjectID) string { return o.Hex() }

// countOrphans — residue the 5b cascade must drive to zero (the L6
// "residue table" in spec terms): docs by project_id, ydoc by the known
// key shapes, chunks by projectId.
func countOrphans(ctx context.Context, db *mongo.Database, oid bson.ObjectID, pidHex string) (int64, error) {
	t, _ := db.Collection("docs").CountDocuments(ctx, bson.D{{Key: "project_id", Value: oid}})
	// chunks: the PRODUCT contract is the historyv1 soft-delete (state →
	// "deleted"); orphans = rows still active/closed (all live rows carry
	// projectId as ObjectID — 2026-10-14 live check: 3446/3446).
	stateActive := bson.D{{Key: "state", Value: bson.D{{Key: "$in", Value: []string{"active", "closed"}}}}}
	ys := []any{oid, pidHex}
	if t2, err := db.Collection("ydoc").CountDocuments(ctx,
		bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: ys}}}}); err == nil {
		t += t2
	}
	if t3, err := db.Collection("ydoc").CountDocuments(ctx,
		bson.D{{Key: "_id", Value: bson.D{{Key: "$regex", Value: "^" + pidHex + "-"}}}}); err == nil {
		t += t3
	}
	if t4, err := db.Collection("chunks").CountDocuments(ctx, append([]bson.E{{Key: "projectId", Value: oid}}, stateActive...)); err == nil {
		t += t4
	}
	if t5, err := db.Collection("chunks").CountDocuments(ctx, append([]bson.E{{Key: "projectId", Value: pidHex}}, stateActive...)); err == nil {
		t += t5
	}
	return t, nil
}

// historyDelete — the PRODUCT chunk/blob purge: DELETE {V1_HISTORY_URL}/api
// projects/{pid} (historyv1). Soft-deletes the project's chunk rows
// (state: active|closed → deleted) via the ObjectID-aware chunk backend AND
// removes its history blobs. Live-verified 2026-10-14: 204, the fixture's
// active chunk flipped to state=deleted (active=0). Basic auth is required
// (401 without) — the same credentials the 63b admin-user purge path uses
// (admin.go: "staging" : STAGING_PASSWORD).
func historyDelete(ctx context.Context, pidHex string) error {
	base := strings.TrimSpace(os.Getenv("V1_HISTORY_URL"))
	if base == "" {
		base = "http://127.0.0.1:3100/api"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, strings.TrimSuffix(base, "/")+"/projects/"+pidHex, nil)
	if err != nil {
		return err
	}
	user := strings.TrimSpace(os.Getenv("V1_HISTORY_USER"))
	if user == "" {
		user = "staging"
	}
	pass := os.Getenv("STAGING_PASSWORD")
	if pass == "" {
		pass = os.Getenv("V1_HISTORY_PASSWORD")
	}
	req.SetBasicAuth(user, pass)
	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(rsp.Body, 1<<20))
	_ = rsp.Body.Close()
	if rsp.StatusCode >= 400 {
		return fmt.Errorf("historyv1 delete: HTTP %d", rsp.StatusCode)
	}
	return nil
}

// flushRoomsNow — POST the project's rooms (root + per-doc) to the collab
// /flushroom surface (the same call compile makes, so the debounced room
// write-back lands on disk immediately) — best-effort; any error is
// swallowed, the callers treat the flush as a hint, not a gate.
func flushRoomsNow(ctx context.Context, pidHex string, docIDHexes []string) {
	rooms := []string{pidHex}
	for _, h := range docIDHexes {
		if h != "" {
			rooms = append(rooms, pidHex+"-"+h)
		}
	}
	b := strings.TrimSpace(os.Getenv("COLLAB_URL"))
	if b == "" {
		return
	}
	body, _ := json.Marshal(map[string]any{"rooms": rooms})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(b, "/")+"/flushroom", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(rsp.Body, 1<<20))
	_ = rsp.Body.Close()
}

// S4 (owner RETIRE document-updater): the DU service base + the delete-flow
// DEL {DU}/project/{pid} flush fallback are removed — content truth is the
// docstore; the OT in-memory state DU flushed no longer exists in the Yjs
// world.

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
func loadProjectFull(a *core.App, cxt *core.Cxt, oid bson.ObjectID) (*bson.D, error) {
	if a.Mongo == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 10*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, err
	}
	var d bson.D
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
func deleteProjectExec(a *core.App, cxt *core.Cxt, oid bson.ObjectID, project bson.D, uid, ip, reason string) {
	pid := oid.Hex()
	hist := strings.TrimSuffix(crHistoryBase(), "/")
	ds := strings.TrimSuffix(crDocstoreBase(), "/")

	// 2. D41 (owner RETIRE document-updater): Node's chain
	//    "DU flush+delete — on failure { DU flush → project-history flush →
	//     on failure project-history resync(force) → DU flush+delete retry }"
	//    reduces, with the DU steps removed, to the history-side "try harder"
	//    path — flush the project-history service (pending OT updates that may
	//    still queue there during the OT→Yjs retention window, D41 hybrid)
	//    then force-resync if that fails. No DU doc remains to delete; the
	//    collab room (Y.Doc) unloads with the project, and docstore archival
	//    (step 3) is the content side effect.
	if !fireHTTP(cxt, "POST", hist+"/project/"+pid+"/flush", nil) {
		fireHTTP(cxt, "POST", hist+"/project/"+pid+"/resync", []byte(`{"force":true}`))
	}

	// 3. docstore archive (best-effort; no-op persistor backend in this build).
	fireHTTP(cxt, "POST", ds+"/project/"+pid+"/archive", nil)

	// 4. per-member tag pulls (owner + collaborators + read-only + reviewers).
	{
		members := []bson.ObjectID{}
		if v, ok := dget(project, "owner_ref").(bson.ObjectID); ok {
			members = append(members, v)
		}
		for _, key := range []string{"collablator_refs", "readOnly_refs", "reviewer_refs"} {
			if arr, ok := dget(project, key).(bson.A); ok {
				for _, m := range arr {
					if mo, ok := m.(bson.ObjectID); ok {
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
	deleterData := ddBuildDeleterData(oid, project, reason, uid, ip)
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
				options.UpdateOne().SetUpsert(true))
		}
	}

	// 5b. content cascade (owner adjudication 2026-10-14, Q4/a — hard delete
	//     must clean the sub-stores; nothing else in this build will: the
	//     docstore archive above (step 3) is a NO-OP persistor by design —
	//     D41 retired document-updater — and there is no reaper service to
	//     sweep orphans afterwards).
	//
	//     Key shapes (live-verified 2026-10-14, L6 residue table + import
	//     probe):
	//       docs    {project_id: id}
	//       ydoc    _id = STRING project id (root room {pid}), "<pid>-<docid>"
	//               (per-doc rooms), or a doc _id; both string and
	//               ObjectID encodings occur → delete all shapes.
	//       chunks  projectId = ObjectID, and the PRODUCT purge is the
	//               historyv1 service (DELETE /api/projects/{pid}) — soft-
	//               delete (state → "deleted") + blob removal; 401 without
	//               basic auth (staging : STAGING_PASSWORD). Raw deletes
	//               remain as belt-and-braces.
	//     The room server's write-back is DEBOUNCED and the room may still
	//     be open (last-peer grace close): late chunk finalizations land
	//     20-120s post-delete (proven across L6 runs), so: in-request
	//     (historyv1 + docs/ydoc purge) then bounded delayed re-sweeps at
	//     +12s/+60s (idempotent) for the late landings.
	if a.Mongo != nil {
		var docIDHexes []string
		{ ctx, cancel := context.WithTimeout(cxt.Req.Context(), 5*time.Second);
			defer cancel()
			if db, err := a.Mongo.DB(ctx); err == nil {
				cursor, cerr := db.Collection("docs").Find(ctx, bson.D{{Key: "project_id", Value: oid}})
				if cerr == nil {
					type docRow struct {
						ID any `bson:"_id"`
					}
					var rows []docRow
					if cursor.All(ctx, &rows) == nil {
						for _, r := range rows {
							if o, ok := r.ID.(bson.ObjectID); ok {
								docIDHexes = append(docIDHexes, o.Hex())
							} else if s, ok := r.ID.(string); ok && s != "" {
								docIDHexes = append(docIDHexes, s)
							}
						}
					}
					_ = cursor.Close(ctx)
				}
			}
		}

		// flush the project's rooms + the PRODUCT history purge (chunks →
		// state=deleted, blobs removed) so the debounced room write-back and
		// late chunk finalizations land on already-deleted state.
		{
			fctx, fcancel := context.WithTimeout(cxt.Req.Context(), 6*time.Second)
			flushRoomsNow(fctx, pidHexOf(oid), docIDHexes)
			_ = historyDelete(fctx, pidHexOf(oid)) // best-effort; raw deletes + sweeps cover failure
			fcancel()
		}

		purge := func(ctx context.Context) (int64, error) {
			db, err := a.Mongo.DB(ctx)
			if err != nil {
				return 0, err
			}
			_, _ = db.Collection("docs").DeleteMany(ctx, bson.D{{Key: "project_id", Value: oid}})
			yids := []any{oid, pidHexOf(oid)}
			for _, h := range docIDHexes {
				yids = append(yids, h)
			}
			_, _ = db.Collection("ydoc").DeleteMany(ctx,
				bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: yids}}}})
			_, _ = db.Collection("ydoc").DeleteMany(ctx,
				bson.D{{Key: "_id", Value: bson.D{{Key: "$regex", Value: "^" + pidHexOf(oid) + "-"}}}})
			_, _ = db.Collection("chunks").DeleteMany(ctx,
				bson.D{{Key: "projectId", Value: bson.D{{Key: "$in", Value: []any{oid, pidHexOf(oid)}}}})
			return countOrphans(ctx, db, oid, pidHexOf(oid))
		}

		residual := int64(-1)
		for i := 0; i < 3; i++ {
			ctx, cancel := context.WithTimeout(cxt.Req.Context(), 8*time.Second)
			if i > 0 {
				select {
				case <-time.After(800 * time.Millisecond):
				case <-cxt.Req.Context().Done():
				}
			}
			residual, _ = purge(ctx)
			cancel()
			if residual == 0 {
				break
			}
		}

		// 5c. delayed reaper (owner adjudication Q4): the LAST peer's room
		//     grace-close can flush chunks/state AFTER the converge loop
		//     above — the L6 live table proved chunks:1→1 surviving the
		//     request even after the room was idle. One bounded final
		//     flush+purge after the grace window (best-effort; a
		//     context-independent background context so it outlives the
		//     response and cannot be cancelled by the request finishing).
		if residual > 0 {
			go func() {
				sweep := func() {
					ctx3, cancel3 := context.WithTimeout(context.Background(), 8*time.Second)
					defer cancel3()
					flushRoomsNow(ctx3, pidHexOf(oid), docIDHexes)
					_ = historyDelete(ctx3, pidHexOf(oid))
					if db, err := a.Mongo.DB(ctx3); err == nil {
						_, _ = db.Collection("docs").DeleteMany(ctx3, bson.D{{Key: "project_id", Value: oid}})
						yids := []any{oid, pidHexOf(oid)}
						for _, h := range docIDHexes {
							yids = append(yids, h)
						}
						_, _ = db.Collection("ydoc").DeleteMany(ctx3,
							bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: yids}}}})
						_, _ = db.Collection("ydoc").DeleteMany(ctx3,
							bson.D{{Key: "_id", Value: bson.D{{Key: "$regex", Value: "^" + pidHexOf(oid) + "-"}}}})
						_, _ = db.Collection("chunks").DeleteMany(ctx3,
							bson.D{{Key: "projectId", Value: bson.D{{Key: "$in", Value: []any{oid, pidHexOf(oid)}}}})
					}
				}
				time.Sleep(12 * time.Second)
				sweep()
				// second sweep: the room's grace-close flush can land well after the
				// first one (L6 live data: chunk row still landing 20-40s post-delete);
				// the later write wins, so sweep again after the grace window.
				time.Sleep(60 * time.Second)
				sweep()
			}()
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
func ddBuildDeleterData(pid bson.ObjectID, project bson.D, reason, uid, ip string) bson.D {
	fields := bson.D{bson.E{Key: "deletedAt", Value: time.Now().UTC()}}
	if uuid, err := bson.ObjectIDFromHex(uid); err == nil {
		fields = append(fields, bson.E{Key: "deleterId", Value: uuid})
	}
	if ip != "" {
		fields = append(fields, bson.E{Key: "deleterIpAddress", Value: ip})
	}
	if reason != "" {
		fields = append(fields, bson.E{Key: "deletedReason", Value: reason})
	}
	fields = append(fields,
		bson.E{Key: "deletedProjectId", Value: pid})
	if v, ok := dget(project, "owner_ref").(bson.ObjectID); ok {
		fields = append(fields, bson.E{Key: "deletedProjectOwnerId", Value: v})
	}
	for _, rr := range ddRefs {
		val := []bson.ObjectID{}
		if arr, ok := dget(project, rr.src).(bson.A); ok {
			for _, m := range arr {
				if mo, ok := m.(bson.ObjectID); ok {
					val = append(val, mo)
				}
			}
		}
		fields = append(fields, bson.E{Key: rr.key, Value: val})
	}
	if overleaf, ok := dget(project, "overleaf").(bson.D); ok {
		if ov, ok := dget(overleaf, "id").(string); ok && ov != "" {
			fields = append(fields, bson.E{Key: "deletedProjectOverleafId", Value: ov})
		}
		if hist, ok := dget(overleaf, "history").(bson.D); ok {
			if hid, ok := dget(hist, "id").(string); ok && hid != "" {
				fields = append(fields, bson.E{Key: "deletedProjectOverleafHistoryId", Value: hid})
			}
		}
	}
	if tokens, ok := dget(project, "tokens").(bson.D); ok {
		if rw, ok := dget(tokens, "readAndWrite").(string); ok && rw != "" {
			fields = append(fields, bson.E{Key: "deletedProjectReadWriteToken", Value: rw})
		}
		if ro, ok := dget(tokens, "readOnly").(string); ok && ro != "" {
			fields = append(fields, bson.E{Key: "deletedProjectReadOnlyToken", Value: ro})
		}
	}
	// Node: deletedProjectLastUpdatedAt: project.lastUpdated (BSON date).
	// driver v1 decodes dates to bson.DateTime into bson.D.
	switch v := dget(project, "lastUpdated").(type) {
	case bson.DateTime:
		fields = append(fields, bson.E{Key: "deletedProjectLastUpdatedAt", Value: v.Time()})
	case time.Time:
		fields = append(fields, bson.E{Key: "deletedProjectLastUpdatedAt", Value: v})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Key < fields[j].Key })
	return append(bson.D{bson.E{Key: "_id", Value: bson.NewObjectID()}}, fields...)
}

// delAuthGate runs the route guards in Node's middleware order: login ->
// malformed-404 -> 404 (missing project) -> 403 (canAdmin fail). `full`
// selects the unprojected load (delete) vs the access-projection load
// (restore). On any early response the handler must return without writing
// anything further.
func delAuthGate(a *core.App, cxt *core.Cxt, res *core.Res, full bool) (uid string, oid *bson.ObjectID, doc *bson.D, okFlag bool) {
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
	o, err := bson.ObjectIDFromHex(strings.ToLower(param))
	if err != nil {
		res.JSON(404, []byte(malformedMsg("Project_id")))
		return
	}
	var d *bson.D
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
		deleteProjectExec(a, cxt, *oid, *doc, cxt.Sess.UserIDHex(), core.ClientIP(cxt.Req), "user")
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
