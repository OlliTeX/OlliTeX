// Package projectlist — TPDS third-party-sync update endpoints (Dropbox +
// GitHub), api-profile only (APIOnly; the web profile skips all of these —
// Node web :4000 404s them).
//
// Node: router.mjs privateApiRouter (requirePrivateApiAuth):
//
//	POST/DELETE  /user/:user_id/update/:path(.+)
//	             -> TpdsController.mergeUpdate / deleteUpdate   (Dropbox)
//	POST/DELETE  /project/:project_id/user/:user_id/update/:path(.+)
//	             -> TpdsController.mergeUpdate / deleteUpdate   (Dropbox)
//	POST/DELETE  /project/:project_id/contents/:path(.+)
//	             -> TpdsController.updateProjectContents / deleteProjectContents (GitHub)
//
// Logic: TpdsUpdateHandler.newUpdate -> UpdateMerger.mergeUpdate (file upsert:
// write body, classify doc/file, upsert) ; deleteUpdate ->
// EditorController.deleteEntityWithPath (or markAsDeleted when path=="/").
//
// Pinned Node :3000 wire (2026-09-23, live):
//
//	mergeUpdate unauth/wrong -> 401 "Unauthorized" (12B) + WA + XPB
//	  bad user_id   -> 404 JSON VA params.user_id    (91B)
//	  bad project_id-> 404 JSON VA params.project_id (94B)
//	  ghost project -> 200 {"status":"rejected"}     (21B)
//	  valid (new doc)  -> 200 {"status":"applied","projectId","entityId",
//	    "entityType":"doc","folderId","rev":"1"}     (164B)
//	  valid (new file) -> 200 {...,"entityType":"file","folderId","rev":"0"} (165B)
//	updateProjectContents unauth -> 401; bad project_id -> 404 VA (94B);
//	  ghost project -> 404 text/plain "Not Found" (9B); valid (new doc) -> 200
//	  {"entityId":"<24hex>","rev":1} (47B; rev is a NUMBER).
//	deleteUpdate (Dropbox)  -> ALWAYS 200 "OK" (2B, sendStatus) for every state.
//	deleteProjectContents   -> 200 {} (ghost) | 200 {"entityId":"<24hex>"} (valid).
//
// The 200-applied upsert reuses the proven P4 upload primitives (upClassify,
// upDocstorePut->rev, upPutBlob, upPushDoc/upPushFile, upSwapFileToDoc/
// upSwapDocToFile/upReplaceFile, upUpdateStructure) but responds in the TPDS
// wire (status/projectId/entityId/entityType/folderId/rev-string or the GitHub
// entityId/rev-number), not the upload wire.
package projectlist

import (
	"context"
	"io"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
)

var (
	muDboxPat = regexp.MustCompile(`^/user/([^/]+)/update/(.+)$`)
	muPidPat  = regexp.MustCompile(`^/project/([^/]+)/user/([^/]+)/update/(.+)$`)
	ghContPat = regexp.MustCompile(`^/project/([^/]+)/contents/(.+)$`)
)

const tpdsRejected = `{"status":"rejected"}`

// tpdsZeroUID — GitHub updateProjectContents/deleteProjectContents call
// UpdateMerger with userId=null in Node; a zero-oid stands in for that (it is
// not part of the wire, so its exact value is invisible to the parity gate).
const tpdsZeroUID = "000000000000000000000000"

func tpdssyncXPB(r *core.Res) { r.W.Header().Set("X-Powered-By", "Express") }

func tpdssyncCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 15*time.Second)
}

// syncSplitPath — Node TpdsController.splitPath (project_id truthiness +
// Path.join('/', path) normalization). Returns filePath + projectName.
func syncSplitPath(hasId bool, pathStr string) (filePath, projectName string) {
	np := path.Join("/", pathStr)
	if hasId {
		return np, ""
	}
	rem := np[1:]
	if j := strings.Index(rem, "/"); j == -1 {
		return "/", rem
	} else {
		idx := j + 1
		filePath = np[idx:]
		projectName = strings.TrimPrefix(np[:idx], "/")
	}
	return filePath, projectName
}

// resolveSyncProject — Node findProjectByIdWithRWAccess (by id): load; require
// owner OR RW collaborator AND not archived/trashed; null on miss.
func resolveSyncProject(a *core.App, c *core.Cxt, uid primitive.ObjectID, pidS string) (primitive.D, bool) {
	oid, _ := primitive.ObjectIDFromHex(strings.ToLower(pidS))
	ctx, cancel := tpdssyncCtx()
	defer cancel()
	var doc primitive.D
	if db, err := a.Mongo.DB(ctx); err == nil &&
		db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&doc) == nil && doc != nil {
		ow, _ := dgetOID(doc, "owner_ref")
		if ow == uid || darrContainsOID(doc, "collaborator_refs", uid) {
			return doc, tpdsProjectActive(doc, uid)
		}
	}
	return nil, false
}

// syncUpsert — UpdateMerger.mergeUpdate 200-applied core (new/replace/swap).
func syncUpsert(a *core.App, uidHex string, pd *primitive.D, pid primitive.ObjectID, fullPath string, data []byte) (entityID, folderID, kind string, rev int64, hasRev, ok bool) {
	seg := upSegments(fullPath)
	if len(seg) == 0 {
		return "", "", "", 0, false, false
	}
	name := seg[len(seg)-1]
	parentSegs := seg[:len(seg)-1]
	root := entParseTree(entFld(*pd, "rootFolder"))
	if len(root) == 0 || root[0].idHex == "" {
		return "", "", "", 0, false, false
	}
	parentID := root[0].idHex
	if len(parentSegs) > 0 {
		createdID, _, mok := tpdsMkdirp(a, uidHex, pd, pid, "/"+strings.Join(parentSegs, "/"))
		if !mok || createdID == "" {
			return "", "", "", 0, false, false
		}
		parentID = createdID // tpdsMkdirp already refreshes *pd
	}
	root = entParseTree(entFld(*pd, "rootFolder"))
	tgt := upResolveFolder(root, parentID, name)
	if tgt == nil {
		return "", "", "", 0, false, false
	}
	folderID = tgt.folderID
	kind, lines := upClassify(data, name, tgt.existingDoc != nil)
	now := time.Now()
	uidl := strings.ToLower(uidHex)
	docD := *pd

	if kind == "doc" {
		if tgt.existingDoc != nil {
			if !upDUSetDoc(pid.Hex(), tgt.existingDoc.idHex, lines, uidl, upTrackChanges(entFld(docD, "track_changes"), uidl), "github") {
				return "", "", "", 0, false, false
			}
			return tgt.existingDoc.idHex, folderID, "doc", 0, false, true
		}
		newDocID := primitive.NewObjectID()
		r2, dok := upDocstorePut(pid.Hex(), newDocID.Hex(), lines)
		if !dok {
			return "", "", "", 0, false, false
		}
		p := tgt.fsPath + "/" + name
		if tgt.existingFile != nil {
			if !upSwapFileToDoc(a, pid.Hex(), tgt.mongoPath, tgt.existingFile.idHex, newDocID.Hex(), name, r2, uidl, now) {
				return "", "", "", 0, false, false
			}
			if !upUpdateStructure(pid.Hex(), uidl, upVersion(docD)+1, upHistoryID(docD), bson.A{upDelOp("file", tgt.existingFile.idHex, p), upAddOpDoc(newDocID.Hex(), p, strings.Join(lines, "\n"), upRangsup(docD))}, "github") {
				return "", "", "", 0, false, false
			}
		} else {
			if !upPushDoc(a, pid.Hex(), tgt.mongoPath, name, newDocID.Hex(), r2, uidl, now) {
				return "", "", "", 0, false, false
			}
			if !upUpdateStructure(pid.Hex(), uidl, upVersion(docD)+1, upHistoryID(docD), bson.A{upAddOpDoc(newDocID.Hex(), p, strings.Join(lines, "\n"), upRangsup(docD))}, "github") {
				return "", "", "", 0, false, false
			}
		}
		return newDocID.Hex(), folderID, "doc", r2, true, true
	}

	hash := upGitBlobHash(data)
	hist := upHistoryID(docD)
	if hist != "" && !upPutBlob(hist, hash, data) {
		return "", "", "", 0, false, false
	}
	newFileID := primitive.NewObjectID()
	p := tgt.fsPath + "/" + name
	switch {
	case tgt.existingFile != nil:
		if !upReplaceFile(a, pid.Hex(), tgt.mongoPath, newFileID.Hex(), hash, uidl, now, tgt.fileIdx) {
			return "", "", "", 0, false, false
		}
		if !upUpdateStructure(pid.Hex(), uidl, upVersion(docD)+1, hist, bson.A{upDelOp("file", tgt.existingFile.idHex, p), upAddOpFile(newFileID.Hex(), p, hash, upRangsup(docD))}, "github") {
			return "", "", "", 0, false, false
		}
	case tgt.existingDoc != nil:
		newFile := bson.D{
			{Key: "name", Value: name}, {Key: "created", Value: now}, {Key: "rev", Value: 0},
			{Key: "linkedFileData", Value: nil}, {Key: "hash", Value: hash},
			{Key: "_id", Value: mustObjectID(newFileID.Hex())},
		}
		if !upSwapDocToFile(a, pid.Hex(), tgt.mongoPath, tgt.existingDoc.idHex, newFile, uidl, now) {
			return "", "", "", 0, false, false
		}
		if !upUpdateStructure(pid.Hex(), uidl, upVersion(docD)+1, hist, bson.A{upDelOp("doc", tgt.existingDoc.idHex, p), upAddOpFile(newFileID.Hex(), p, hash, upRangsup(docD))}, "github") {
			return "", "", "", 0, false, false
		}
	default:
		if !upPushFile(a, pid.Hex(), tgt.mongoPath, name, newFileID.Hex(), hash, uidl, now) {
			return "", "", "", 0, false, false
		}
		if !upUpdateStructure(pid.Hex(), uidl, upVersion(docD)+1, hist, bson.A{upAddOpFile(newFileID.Hex(), p, hash, upRangsup(docD))}, "github") {
			return "", "", "", 0, false, false
		}
	}
	return newFileID.Hex(), folderID, "file", 0, true, true
}

func syncMergeRoute() func(a *core.App) func(c *core.Cxt, r *core.Res) {
	return func(a *core.App) func(c *core.Cxt, r *core.Res) {
		return func(c *core.Cxt, r *core.Res) {
			if c.A.Cfg.Profile != "api" {
				tpdssyncXPB(r)
				apiText(r, 404, "Not Found")
				return
			}
			req := c.Req
			if !a.APIBasicGate401(c, r, req) {
				return
			}
			p := req.URL.Path
			var uidS, pidS, pathS string
			var hasPid bool
			if m := muPidPat.FindStringSubmatch(p); m != nil {
				pidS, uidS, pathS = m[1], m[2], m[3]
				hasPid = true
			} else if m := muDboxPat.FindStringSubmatch(p); m != nil {
				uidS, pathS = m[1], m[2]
			} else {
				tpdssyncXPB(r)
				apiText(r, 404, "Not Found")
				return
			}
			pathS, _ = url.PathUnescape(pathS)
			uidl := strings.ToLower(uidS)
			var verrs []string
			if !delHex24(uidl) {
				verrs = append(verrs, `Invalid Mongo ObjectId at \"params.user_id\"`)
			}
			if hasPid && pidS != "" && !delHex24(strings.ToLower(pidS)) {
				verrs = append(verrs, `Invalid Mongo ObjectId at \"params.project_id\"`)
			}
			if len(verrs) > 0 {
				tpdssyncXPB(r)
				r.JSON(404, []byte(`{"error":"Validation error: `+strings.Join(verrs, "; ")+`","statusCode":404}`))
				return
			}
			uid, _ := primitive.ObjectIDFromHex(uidl)
			hasId := hasPid && pidS != ""
			filePath, projectName := syncSplitPath(hasId, pathS)
			data, _ := io.ReadAll(io.LimitReader(req.Body, 64<<20))

			var pd primitive.D
			found := false
			if hasId {
				pd, found = resolveSyncProject(a, c, uid, pidS)
			} else if projectName != "" {
				pd, found = tpdsGetOrCreateByName(a, c, uid, projectName)
			}
			if !found {
				tpdssyncXPB(r)
				r.JSON(200, []byte(tpdsRejected))
				return
			}
			if tpdsShouldIgnore(filePath) {
				tpdssyncXPB(r)
				r.JSON(200, []byte(tpdsRejected))
				return
			}
			pid, _ := dgetOID(pd, "_id")
			eid, foid, kind, rev, hasRev, upOK := syncUpsert(a, uidl, &pd, pid, filePath, data)
			if !upOK {
				tpdssyncXPB(r)
				r.SendStatus(500)
				return
			}
			b := `{"status":"applied","projectId":"` + pid.Hex() + `","entityId":"` + eid + `","entityType":"` + kind + `","folderId":"` + foid + `"`
			if hasRev {
				b += `,"rev":"` + strconv.FormatInt(rev, 10) + `"`
			}
			b += `}`
			tpdssyncXPB(r)
			r.JSON(200, []byte(b))
		}
	}
}

func syncGHUpdateRoute() func(a *core.App) func(c *core.Cxt, r *core.Res) {
	return func(a *core.App) func(c *core.Cxt, r *core.Res) {
		return func(c *core.Cxt, r *core.Res) {
			if c.A.Cfg.Profile != "api" {
				tpdssyncXPB(r)
				apiText(r, 404, "Not Found")
				return
			}
			req := c.Req
			if !a.APIBasicGate401(c, r, req) {
				return
			}
			m := ghContPat.FindStringSubmatch(req.URL.Path)
			if m == nil {
				tpdssyncXPB(r)
				apiText(r, 404, "Not Found")
				return
			}
			pidS, pathS := m[1], m[2]
			pathS, _ = url.PathUnescape(pathS)
			pidl := strings.ToLower(pidS)
			if !delHex24(pidl) {
				tpdssyncXPB(r)
				r.JSON(404, delParamVA("project_id"))
				return
			}
			pid, _ := primitive.ObjectIDFromHex(pidl)
			ctx, cancel := tpdssyncCtx()
			var pd primitive.D
			ok := false
			if db, err := a.Mongo.DB(ctx); err == nil {
				ok = db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: pid}}).Decode(&pd) == nil && pd != nil
			}
			cancel()
			if !ok {
				tpdssyncXPB(r)
				apiText(r, 404, "Not Found")
				return
			}
			data, _ := io.ReadAll(io.LimitReader(req.Body, 64<<20))
			eid, _, _, rev, _, upOK := syncUpsert(a, tpdsZeroUID, &pd, pid, path.Join("/", pathS), data)
			if !upOK {
				tpdssyncXPB(r)
				r.SendStatus(500)
				return
			}
			tpdssyncXPB(r)
			r.JSON(200, []byte(`{"entityId":"`+eid+`","rev":`+strconv.FormatInt(rev, 10)+`}`))
		}
	}
}

// syncLocate — find the doc/file entity at fullPath in the project tree.
func syncLocate(root []entFolder, fullPath string) (kind, eid, arrPath, name string, found bool) {
	seg := upSegments(fullPath)
	if len(seg) == 0 || len(root) == 0 || root[0].idHex == "" {
		return "", "", "", "", false
	}
	cur := root[0]
	fpath := "rootFolder.0"
	last := seg[len(seg)-1]
	for i := 0; i < len(seg)-1; i++ {
		var nx *entFolder
		for idx := range cur.fold {
			if strings.EqualFold(cur.fold[idx].name, seg[i]) {
				nx = &cur.fold[idx]
				fpath += ".folders." + strconv.Itoa(idx)
				break
			}
		}
		if nx == nil {
			return "", "", "", "", false
		}
		cur = *nx
	}
	for di, d := range cur.docs {
		if strings.EqualFold(d.name, last) {
			return "doc", d.idHex, fpath + ".docs." + strconv.Itoa(di), d.name, true
		}
	}
	for fi, fl := range cur.files {
		if strings.EqualFold(fl.name, last) {
			return "file", fl.idHex, fpath + ".fileRefs." + strconv.Itoa(fi), fl.name, true
		}
	}
	return "", "", "", "", false
}

// syncDeleteEntity — find the entity at fullPath and remove it (mongo $pull +
// docstore delete for docs + DU structure del-op). Returns the entity id.
func syncDeleteEntity(a *core.App, uidHex string, pd *primitive.D, pid primitive.ObjectID, fullPath string) (kind, eid string, found bool) {
	root := entParseTree(entFld(*pd, "rootFolder"))
	kind, eid, arrPath, name, ok := syncLocate(root, fullPath)
	if !ok {
		return "", "", false
	}
	ctx, cancel := tpdssyncCtx()
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return kind, eid, false
	}
	parts := strings.Split(arrPath, ".")
	arrayPath := strings.Join(parts[:len(parts)-1], ".")
	_, _ = db.Collection("projects").UpdateOne(ctx,
		bson.D{{Key: "_id", Value: pid}},
		bson.D{
			{Key: "$pull", Value: bson.D{{Key: arrayPath, Value: bson.D{{Key: "_id", Value: mustObjectID(eid)}}}}},
			{Key: "$inc", Value: bson.D{{Key: "version", Value: 1}}},
		})
	if kind == "doc" {
		entPatchDoc(pid.Hex(), eid, name)
	}
	if hist := upHistoryID(*pd); hist != "" {
		upUpdateStructure(pid.Hex(), strings.ToLower(uidHex), upVersion(*pd)+1, hist, bson.A{upDelOp(kind, eid, fullPath)}, "github")
	}
	return kind, eid, true
}

func syncDeleteRoute() func(a *core.App) func(c *core.Cxt, r *core.Res) {
	return func(a *core.App) func(c *core.Cxt, r *core.Res) {
		return func(c *core.Cxt, r *core.Res) {
			if c.A.Cfg.Profile != "api" {
				tpdssyncXPB(r)
				apiText(r, 404, "Not Found")
				return
			}
			req := c.Req
			if !a.APIBasicGate401(c, r, req) {
				return
			}
			p := req.URL.Path
			var uidS, pidS, pathS string
			var hasPid bool
			if m := muPidPat.FindStringSubmatch(p); m != nil {
				pidS, uidS, pathS = m[1], m[2], m[3]
				hasPid = true
			} else if m := muDboxPat.FindStringSubmatch(p); m != nil {
				uidS, pathS = m[1], m[2]
			} else {
				tpdssyncXPB(r)
				r.SendStatus(200) // Node still sendStatus(200)
				return
			}
			pathS, _ = url.PathUnescape(pathS)
			uidl := strings.ToLower(uidS)
			var verrs []string
			if !delHex24(uidl) {
				verrs = append(verrs, `Invalid Mongo ObjectId at \"params.user_id\"`)
			}
			if hasPid && pidS != "" && !delHex24(strings.ToLower(pidS)) {
				verrs = append(verrs, `Invalid Mongo ObjectId at \"params.project_id\"`)
			}
			if len(verrs) > 0 {
				tpdssyncXPB(r)
				r.JSON(404, []byte(`{"error":"Validation error: `+strings.Join(verrs, "; ")+`","statusCode":404}`))
				return
			}
			uid, _ := primitive.ObjectIDFromHex(uidl)
			hasId := hasPid && pidS != ""
			filePath, projectName := syncSplitPath(hasId, pathS)
			if hasId {
				if pd, found := resolveSyncProject(a, c, uid, pidS); found {
					pid, _ := dgetOID(pd, "_id")
					syncDeleteEntity(a, uidl, &pd, pid, filePath)
				}
			} else if projectName != "" {
				// Node deleteUpdate looks up (does NOT create) by name.
				pd, found := resolveByNameActive(a, c, uid, projectName)
				if found {
					pid, _ := dgetOID(pd, "_id")
					syncDeleteEntity(a, uidl, &pd, pid, filePath)
				}
			}
			tpdssyncXPB(r)
			r.SendStatus(200)
		}
	}
}

// syncGHDeleteRoute — deleteProjectContents (GitHub DELETE): project absent ->
// 200 {} (entityId undefined); entity deleted -> 200 {"entityId":"<24hex>"}.
func syncGHDeleteRoute() func(a *core.App) func(c *core.Cxt, r *core.Res) {
	return func(a *core.App) func(c *core.Cxt, r *core.Res) {
		return func(c *core.Cxt, r *core.Res) {
			if c.A.Cfg.Profile != "api" {
				tpdssyncXPB(r)
				apiText(r, 404, "Not Found")
				return
			}
			req := c.Req
			if !a.APIBasicGate401(c, r, req) {
				return
			}
			m := ghContPat.FindStringSubmatch(req.URL.Path)
			if m == nil {
				tpdssyncXPB(r)
				apiText(r, 404, "Not Found")
				return
			}
			pidS, pathS := m[1], m[2]
			pathS, _ = url.PathUnescape(pathS)
			if !delHex24(strings.ToLower(pidS)) {
				tpdssyncXPB(r)
				r.JSON(404, delParamVA("project_id"))
				return
			}
			pid, _ := primitive.ObjectIDFromHex(strings.ToLower(pidS))
			ctx, cancel := tpdssyncCtx()
			var pd primitive.D
			ok := false
			if db, err := a.Mongo.DB(ctx); err == nil {
				ok = db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: pid}}).Decode(&pd) == nil && pd != nil
			}
			cancel()
			if !ok {
				tpdssyncXPB(r)
				r.JSON(200, []byte(`{}`))
				return
			}
			_, eid, found := syncDeleteEntity(a, tpdsZeroUID, &pd, pid, path.Join("/", pathS))
			tpdssyncXPB(r)
			if found {
				r.JSON(200, []byte(`{"entityId":"`+eid+`"}`))
				return
			}
			r.JSON(200, []byte(`{}`))
		}
	}
}

// resolveByNameActive — Node ProjectGetter.findUsersProjectsByName (owned OR
// RW collaborator), filtered to non-archived/trashed; returns the active one
// (does NOT create). Multiple -> Node fires duplicate handling and stops.
func resolveByNameActive(a *core.App, c *core.Cxt, uid primitive.ObjectID, name string) (primitive.D, bool) {
	lower := strings.ToLower(name)
	var active []primitive.D
	for _, pd := range tpdsOwnedOrRWProjects(a, c, uid) {
		if strings.ToLower(dspellLang(pd)) == lower && tpdsProjectActive(pd, uid) {
			active = append(active, pd)
		}
	}
	if len(active) == 1 {
		return active[0], true
	}
	return nil, false
}
