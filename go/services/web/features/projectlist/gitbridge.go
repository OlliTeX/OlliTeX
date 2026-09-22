package projectlist

// P6.19 — git-bridge push pipeline entry points.
//
// Node (GitBridgeHandler.pushUpdate) drives UpdateMerger:
//   - mergeUpdate  = EditorController.upsertDocWithPath / upsertFileWithPath
//     (= mkdirp of the relative parent chain + the SAME upsert core as the
//     P4.13a upload route; DU source string: 'git-bridge')
//   - deleteUpdate = EditorController.deleteEntityWithPath (P4d core)
//
// The write mechanics (mongo shapes, docstore/v1-history PUTs, DU ops) are
// the pinned ones from upload.go / delent.go; only the source string and
// path resolution differ. Node's "missing entity" list is docs+files only
// (folders are not deleted — pinned in UpdateMerger.pushUpdate).

import (
	"context"
	"net/http"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"ollitex/go/services/web/core"
)

// gctx — a core.Cxt bound to an explicit context (the async push pipeline
// runs after the response; Node's continuation is also req-independent).
func gctx(ctx context.Context) *core.Cxt {
	req := &http.Request{}
	return &core.Cxt{Req: req.WithContext(ctx)}
}

type sinkW struct{}

func (s *sinkW) Header() http.Header         { return http.Header{} }
func (s *sinkW) WriteHeader(int)             {}
func (s *sinkW) Write(p []byte) (int, error) { return len(p), nil }

// GBWriteBytes writes `data` into project pj at (folder=root, relDir chain,
// name). Node _determineFileType table (pinned): existing file stays a file;
// existing doc: text?doc:file; neither: binary?file:doc.
// source = DU source string (Node: 'git-bridge').
func GBWriteBytes(ctx context.Context, a *core.App, pj primitive.ObjectID, uid, relDir, name string, data []byte, source string) bool {
	cxt := gctx(ctx)
	docPtr, lerr := loadProjectFull(a, cxt, pj)
	if lerr != nil || docPtr == nil {
		return false
	}
	doc := *docPtr
	if !entCleanName(name) {
		return false
	}
	root := entParseTree(entFld(doc, "rootFolder"))
	if len(root) == 0 {
		return false
	}
	tgt := upResolveFolder(root, root[0].idHex, name)
	if tgt == nil {
		return false
	}
	if relDir != "" {
		nd, nt, ok := upMkdirp(a, uid, docPtr, tgt, relDir)
		if !ok {
			return false
		}
		docPtr = nd
		doc = *nd
		tgt = upResolveFolder(entParseTree(entFld(doc, "rootFolder")), nt.folderID, name)
		if tgt == nil {
			return false
		}
	}
	res := &core.Res{W: &sinkW{}}
	failed := false
	fail := func() { failed = true }
	kind, lines := upClassify(data, name, tgt.existingDoc != nil)
	track := upTrackChanges(entFld(doc, "track_changes"), uid)
	if tgt.existingFile == nil && kind == "doc" {
		upDoDoc(a, cxt, res, fail, pj.Hex(), tgt, name, lines, uid, doc, track, source)
		return !failed
	}
	upDoFile(a, cxt, res, fail, pj.Hex(), tgt, name, data, uid, doc, source)
	return !failed
}

// GBCollectEntityPaths — Node `entityPaths` (docs + files, root-relative,
// leading "/", folders NOT included).
func GBCollectEntityPaths(ctx context.Context, a *core.App, pj primitive.ObjectID) []string {
	cxt := gctx(ctx)
	docPtr, lerr := loadProjectFull(a, cxt, pj)
	if lerr != nil || docPtr == nil {
		return nil
	}
	out := []string{}
	for _, e := range collectEntities(docPtr) {
		out = append(out, e.path)
	}
	return out
}

// GBDeleteEntityAtPath deletes the doc or file at (root-relative) fullPath
// (Node deleteEntityWithPath for a doc/file — P4d pinned sequence).
func GBDeleteEntityAtPath(ctx context.Context, a *core.App, pj primitive.ObjectID, uid, fullPath string) bool {
	cxt := gctx(ctx)
	docPtr, lerr := loadProjectFull(a, cxt, pj)
	if lerr != nil || docPtr == nil {
		return false
	}
	doc := *docPtr
	root := entParseTree(entFld(doc, "rootFolder"))
	if len(root) == 0 {
		return false
	}
	uidObj, uerr := primitive.ObjectIDFromHex(uid)
	if uerr != nil {
		return false
	}
	// descend folders by exact name to the parent folder
	rest := trimSlash(fullPath)
	var cur *entFolder
	cur = &root[0]
	for i := 0; i+1 < len(splitPath(rest)); i++ {
		seg := splitPath(rest)[i]
		var found *entFolder
		for j := range cur.fold {
			if cur.fold[j].name == seg {
				found = &cur.fold[j]
				break
			}
		}
		if found == nil {
			return false
		}
		cur = found
	}
	leaf := splitPath(rest)[len(splitPath(rest))-1]
	var (
		elemHex  string
		parentEl string
	)
	mp, _, _, ok := entFindLoc(root, cur.idHex)
	if !ok {
		return false
	}
	for i := range cur.docs {
		if cur.docs[i].name == leaf {
			elemHex = cur.docs[i].idHex
			parentEl = mp + ".docs"
			break
		}
	}
	if elemHex == "" {
		for i := range cur.files {
			if cur.files[i].name == leaf {
				elemHex = cur.files[i].idHex
				parentEl = mp + ".fileRefs"
				break
			}
		}
	}
	if elemHex == "" {
		return false
	}
	eid, _ := primitive.ObjectIDFromHex(elemHex)
	rootDoc := parentEl == mp+".docs" && oidHex(entFld(doc, "rootDoc_id")) == elemHex

	// Node _cleanUpEntity: the deleted doc (self) gets docstore PATCH +
	// DU deleteDoc (both best-effort).
	var subDocs []entElement
	if parentEl == mp+".docs" {
		subDocs = []entElement{{idHex: elemHex, name: leaf}}
	}

	// Node deleteEntity sequence (ProjectEntityUpdateHandler.deleteEntity,
	// pinned A/B: the DU structure delete op is what makes project-history
	// record a deletion version — the doc DELETE docstore/DU calls alone do
	// not; verified: without it, v1-history keeps stale versions).

	// 1. flushProjectToMongo before the mongo write (DU POST /flush).
	_ = fireHTTP(cxt, http.MethodPost, cduBase()+"/project/"+pj.Hex()+"/flush", nil)

	// 2. mongo $pull + $inc version (+ $unset rootDoc_id).
	if !mongoPullEntity(ctx, a, pj, parentEl, eid, uid, uidObj, rootDoc) {
		return false
	}

	// 3. DU structure op: rename-{doc|file} with newPathname '' (= delete)
	//    → project-history queues the deletion version.
	kind := "file"
	if parentEl == mp+".docs" {
		kind = "doc"
	}
	upds := bson.A{upDelOp(kind, eid.Hex(), "/"+trimSlash(fullPath))}
	_ = upUpdateStructure(pj.Hex(), strings.ToLower(uid), upVersion(doc)+1, upHistoryID(doc), upds, "git-bridge")

	// 4. doc cleanup (docs only): docstore PATCH {deleted,deletedAt,name} +
	//    DU doc DELETE (whose finally flushes project-history).
	for _, d := range subDocs {
		_ = entPatchDoc(pj.Hex(), d.idHex, d.name)
		_ = fireHTTP(cxt, http.MethodDelete, cduBase()+"/project/"+pj.Hex()+"/doc/"+d.idHex, nil)
	}
	return true
}

func trimSlash(s string) string {
	out := ""
	for _, c := range s {
		if c == '/' && out == "" {
			continue
		}
		out += string(c)
	}
	return out
}

func splitPath(s string) []string {
	out := []string{}
	cur := ""
	for _, c := range s {
		if c == '/' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(c)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// mongoPullEntity — the P4d pinned $pull + $inc + $set (+ $unset rootDoc).
func mongoPullEntity(ctx context.Context, a *core.App, pj primitive.ObjectID, parentEl string, eid primitive.ObjectID, uid string, uidObj primitive.ObjectID, rootDoc bool) bool {
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	upd := bson.D{
		{Key: "$pull", Value: bson.D{{Key: parentEl, Value: bson.D{{Key: "_id", Value: eid}}}}},
		{Key: "$inc", Value: bson.D{{Key: "version", Value: 1}}},
		{Key: "$set", Value: bson.D{{Key: "lastUpdated", Value: time.Now()}, {Key: "lastUpdatedBy", Value: uidObj}}},
	}
	if rootDoc {
		upd = append(upd, primitive.E{Key: "$unset", Value: bson.D{{Key: "rootDoc_id", Value: 1}}})
	}
	ur, err := db.Collection("projects").UpdateOne(ctx, bson.D{{Key: "_id", Value: pj}}, upd)
	return err == nil && ur.MatchedCount == 1
}
