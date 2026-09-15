// P4.11b — editor entity DELETION: DELETE /project/:id/{doc,file,folder}/:entity_id
// Node oracle (Editor/EditorRouter DELETE routes → ProjectEntityUpdateHandler.
// deleteEntity → ProjectEntityMongoUpdateHandler.deleteEntity →
// _removeElementFromMongoArray → _cleanUpEntity/_cleanUpDoc), pinned live
// 2026-09-15:
//
//	  order: authorization (anon → 401 sendStatus; non-member → 403
//	         "restricted") → zz.objectId(params) → 404 JSON
//	         {"error":"Validation error: Invalid Mongo ObjectId at \"params.
//	         <Project_id|entity_id>\"","statusCode":404} (probe-pinned) →
//	         flushProjectToMongo (dead in this stack; failures tolerated —
//	         deletions still 204) → project load (→ 404 page) → root-folder
//	         guard (folder == rootFolder[0]._id → 422 text/plain "cannot delete
//	         root folder") → findElement (missing or wrong type → 404 page) →
//	         findOneAndUpdate({_id: pid}, {$pull: {<parentArrayPath>: {_id}},
//	         $inc: {version: 1}, $set: {lastUpdated, lastUpdatedBy},
//	         [$unset: {rootDoc_id: 1}]}) (MatchedCount 0 → 500) →
//	         _cleanUpEntity: per doc in the deleted subtree → docstore PATCH
//	         /project/:pid/doc/:did {deleted:true, deletedAt, name}; docstore
//	         404 → NotFoundError → HTTP 404 page AFTER the tree write committed
//	         (pinned: state mutated + 404) → 204 No Content (no body, no CT).
//
//	NOT replicated (consistent with earlier units): realtime emitToRoom
//	(redis publish), TpdsUpdateSender.deleteEntity, updateProjectStructure
//	(no-op in this build). document-updater deleteDoc is best-effort
//	fire-and-forget (service dead in this stack; Node tolerates the failure).
package projectlist

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

func delHex24(s string) bool {
	if len(s) != 24 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}

// Node: zz.objectId failure on route params → 404 JSON (probe-pinned).
func delParamVA(key string) []byte {
	return []byte(`{"error":"Validation error: Invalid Mongo ObjectId at \"params.` + key + `\"","statusCode":404}`)
}

// delLoc: found element's parent-array mongo path + doc ids in the deleted
// subtree (self included when a doc) + root-doc flag.
type delLoc struct {
	parentPath string
	subDocs    []entElement
	rootDoc    bool
}

func delSubtreeDocs(f entFolder) []entElement {
	out := append([]entElement{}, f.docs...)
	for i := range f.fold {
		out = append(out, delSubtreeDocs(f.fold[i])...)
	}
	return out
}

// delFind mirrors ProjectLocator.findElement: element arrays checked before
// recursive folders, folders in index order; doc→docs, file→fileRefs,
// folder→folders.
func delFind(f *entFolder, fpath, kind, eid, rootDocHex string) *delLoc {
	switch kind {
	case "doc":
		for _, d := range f.docs {
			if d.idHex == eid {
				return &delLoc{parentPath: fpath + ".docs", subDocs: []entElement{d}, rootDoc: rootDocHex != "" && rootDocHex == eid}
			}
		}
	case "file":
		for i := range f.files {
			if f.files[i].idHex == eid {
				return &delLoc{parentPath: fpath + ".fileRefs"}
			}
		}
	case "folder":
		for i := range f.fold {
			if f.fold[i].idHex == eid {
				return &delLoc{parentPath: fpath + ".folders", subDocs: delSubtreeDocs(f.fold[i])}
			}
		}
	}
	for i := range f.fold {
		if r := delFind(&f.fold[i], fpath+".folders."+strconv.Itoa(i), kind, eid, rootDocHex); r != nil {
			return r
		}
	}
	return nil
}

// entPatchDoc: Node DocstoreManager.deleteDoc → PATCH
// {docstore}/project/:pid/doc/:did {deleted:true, deletedAt, name}; 204.
// ok=false on any non-2xx (Node: 404 → NotFoundError → web 404 after the
// tree write).
func entPatchDoc(projectHex, docHex, name string) bool {
	u := entDocstoreURL() + "/project/" + projectHex + "/doc/" + docHex
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	body, _ := json.Marshal(map[string]any{"deleted": true, "deletedAt": now, "name": name})
	req, err := http.NewRequest(http.MethodPatch, u, bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("content-type", "application/json")
	resp, err := entHTTP.Do(req)
	if err != nil {
		return false
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	resp.Body.Close()
	return resp.StatusCode == http.StatusNoContent
}

// Patterns use ([^/]+) so malformed ids still reach the handler (Node's
// Express :params + zz.objectId → 404 JSON VA); hex validation lives in the
// handler (delParamVA).
var (
	delDocPat    = regexp.MustCompile(`^/project/([^/]+)/doc/([^/]+)$`)
	delFilePat   = regexp.MustCompile(`^/project/([^/]+)/file/([^/]+)$`)
	delFolderPat = regexp.MustCompile(`^/project/([^/]+)/folder/([^/]+)$`)
)

func delEntityHandler(a *core.App, kind string) func(cxt *core.Cxt, res *core.Res) {
	pat := delDocPat
	if kind == "file" {
		pat = delFilePat
	} else if kind == "folder" {
		pat = delFolderPat
	}
	return func(cxt *core.Cxt, res *core.Res) {
		req := cxt.Req
		path := req.URL.Path

		// Anonymous write → 401 sendStatus (pinned P4.11a pattern for the same
		// middleware).
		uid := ""
		if cxt.Sess != nil {
			uid = cxt.Sess.UserIDHex()
		}
		if uid == "" {
			res.SendStatus(401)
			return
		}

		mm := pat.FindStringSubmatch(path)
		if mm == nil {
			views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(path, "/")))
			return
		}
		pidHex, eidHex := mm[1], mm[2]
		// Node: zz.objectId(params) → 404 JSON (identify the malformed param).
		if !delHex24(pidHex) {
			res.JSON(404, delParamVA("Project_id"))
			return
		}
		if !delHex24(eidHex) {
			res.JSON(404, delParamVA("entity_id"))
			return
		}
		oid, _ := primitive.ObjectIDFromHex(pidHex)
		oidE, _ := primitive.ObjectIDFromHex(eidHex)
		uidObj, uerr := primitive.ObjectIDFromHex(uid)
		if uerr != nil {
			res.SendStatus(401)
			return
		}

		doc, lerr := loadProjectFull(a, cxt, oid)
		if lerr != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		if doc == nil {
			views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(path, "/")))
			return
		}
		if !entCanWrite(uid, *doc) {
			if core.AcceptsJSON(req) {
				res.JSON(403, []byte(colRestricted))
			} else {
				views.Restricted403(res.W, pageBase(cxt, strings.TrimPrefix(path, "/")))
			}
			return
		}

		// Node refreshes before the guards (dead in this stack; tolerated).
		fireHTTP(cxt, http.MethodPost, cduBase()+"/project/"+pidHex+"/flush", nil)

		roots := entParseTree(dget(*doc, "rootFolder"))
		if len(roots) == 0 {
			res.JSON(500, []byte("internal error"))
			return
		}
		// root-folder guard: Node checks project.rootFolder.some(id match).
		if kind == "folder" {
			for i := range roots {
				if roots[i].idHex == eidHex {
					res.PlainText(422, "cannot delete root folder")
					return
				}
			}
		}
		rootDocHex := oidHex(dget(*doc, "rootDoc_id"))
		loc := delFind(&roots[0], "rootFolder.0", kind, eidHex, rootDocHex)
		if loc == nil {
			views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(path, "/")))
			return
		}

		ctx := req.Context()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		updDoc := bson.D{
			{Key: "$pull", Value: bson.D{{Key: loc.parentPath, Value: bson.D{{Key: "_id", Value: oidE}}}}},
			{Key: "$inc", Value: bson.D{{Key: "version", Value: 1}}},
			{Key: "$set", Value: bson.D{{Key: "lastUpdated", Value: time.Now()}, {Key: "lastUpdatedBy", Value: uidObj}}},
		}
		if loc.rootDoc {
			updDoc = append(updDoc, primitive.E{Key: "$unset", Value: bson.D{{Key: "rootDoc_id", Value: 1}}})
		}
		resT, err := db.Collection("projects").UpdateOne(ctx, bson.D{{Key: "_id", Value: oid}}, updDoc)
		if err != nil || resT.MatchedCount == 0 {
			// Node: null newProject → OError → 500.
			res.JSON(500, []byte("internal error"))
			return
		}

		// _cleanUpEntity: every doc in the deleted subtree (self included);
		// first failure → 404 page AFTER the write (probe-pinned).
		for i := range loc.subDocs {
			d := loc.subDocs[i]
			if !entPatchDoc(pidHex, d.idHex, d.name) {
				views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(path, "/")))
				return
			}
			// document-updater deleteDoc (dead; best-effort; Node tolerates).
			fireHTTP(cxt, http.MethodDelete, cduBase()+"/project/"+pidHex+"/doc/"+d.idHex, nil)
		}

		res.NoContent()
	}
}
