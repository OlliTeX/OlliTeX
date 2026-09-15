package projectlist

// P4.7b — POST /project/new  { template: 'example' }.
//
// Node source (oracle, pinned live 2026-09-14) —
//   ProjectCreationHandler.createExampleProject
//     = _createBlankProject (same project doc + project_history init as basic)
//     + _addExampleProjectFiles(project)          (ProjectCreationHandler.mjs:197-232)
//         -> _buildTemplate('main.tex')  -> _createRootDoc (docstore rev0 + setRootDoc)
//         -> _buildTemplate('sample.bib') -> ProjectEntityUpdateHandler.addDoc (docstore rev0)
//         -> addExampleProjectFiles -> addProjectFile(frog.jpg)
//              ProjectEntityUpdateHandler.addFile
//                beforeLock: _uploadFile -> FileStoreHandler.uploadFileFromDisk
//                            -> FileHashManager.computeHash (= git blob SHA1)
//                            -> HistoryManager.uploadBlobFromDisk
//                               PUT {v1_history}/projects/{historyId}/blobs/{hash}
//                               (basicAuth staging:$V1_HISTORY_PASSWORD)
//                withLock:   addFile -> fileRef pushed to rootFolder.fileRefs
//                (then)      updateProjectStructure (history)  +  TPDS (no-op in free build)
//     + populateClsiCacheForExampleProject   (FIRE-AND-FORGET clsi cache warm — a
//                                               performance optimisation, NOT a
//                                               creation contract; deferred)
//
// The captured live oracle (basic + example on the same user) fixes the shape:
//   - rootFolder.docs   = [ {name:'main.tex', _id}, {name:'sample.bib', _id} ]
//   - rootFolder.fileRefs = [ {name:'frog.jpg', created, rev:0,
//                              linkedFileData:null, hash:<git-blob-sha1>, _id} ]
//   - rootDoc_id        = main.tex doc id
//   - docstore          : main.tex (118 lines) + sample.bib (10 lines), rev 0
//   - history-v1 blob   : the frog.jpg bytes under /projects/{id}/blobs/{hash}
//
// All content is deterministic (static template files; hash = f(content)), so a
// fresh Go-created example project is byte-for-byte comparable to the Node one.

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
)

// crCreateExampleProject builds an 'example' project: main.tex + sample.bib
// documents (docstore rev 0 each), the frog.jpg file (history-v1 blob +
// rootFolder.fileRefs entry) and the shared project-history initialisation.
// The caller owns the response.
func crCreateExampleProject(a *core.App, cxt *core.Cxt, name, uid string, u crOwnerUser) primitive.ObjectID {
	pid := primitive.NewObjectID()
	mainDocID := primitive.NewObjectID()
	bibDocID := primitive.NewObjectID()
	rootID := primitive.NewObjectID()

	dir := crExampleProjectDir()
	mainLines := crTemplateLines(dir + "/main.tex")
	bibLines := crTemplateLines(dir + "/sample.bib")
	frog, frogErr := os.ReadFile(dir + "/frog.jpg")

	var fileRefs bson.A
	hash := ""
	if frogErr == nil {
		hash = crGitBlobHash(frog)
		fileRefs = bson.A{bson.D{
			{Key: "name", Value: "frog.jpg"},
			{Key: "created", Value: time.Now().UTC()},
			{Key: "rev", Value: 0},
			{Key: "linkedFileData", Value: nil},
			{Key: "hash", Value: hash},
			{Key: "_id", Value: primitive.NewObjectID()},
		}}
	}
	docs := bson.A{
		bson.D{{Key: "name", Value: "main.tex"}, {Key: "_id", Value: mainDocID}},
		bson.D{{Key: "name", Value: "sample.bib"}, {Key: "_id", Value: bibDocID}},
	}

	// Same project doc + project-history init as basic (order: init before the
	// blob PUT, mirroring Node — _createBlankProject runs before _addExample
	// ProjectFiles, so the history exists when the frog.jpg blob lands).
	// version 3 = blank (0) + 2 addDocs (main.tex, sample.bib) + 1 addFile (frog.jpg),
	// each Node structural edit is $inc version:1 (pinned: Node oracle project version = 3).
	crInsertProject(a, cxt, pid, rootID, &mainDocID, name, uid, u.spellCheckLanguage, docs, fileRefs, len(docs)+len(fileRefs))
	crInitHistory(cxt, pid.Hex())

	crCreateDocRevision(cxt, pid, mainDocID, mainLines)
	crCreateDocRevision(cxt, pid, bibDocID, bibLines)

	if frogErr == nil && hash != "" {
		crUploadBlob(cxt, pid.Hex(), hash, frog)
	}
	return pid
}

// crTemplateLines reads a template file and splits it exactly like Node's
// `fs.readFileSync(p,'utf8').split('\n')`. Go's strings.Split behaves the
// same on '\n' (a trailing newline yields the extra empty element Node
// produces), so parity holds without special-casing.
func crTemplateLines(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return []string{}
	}
	return strings.Split(string(b), "\n")
}

// crGitBlobHash reproduces Node's FileHashManager.computeHash:
// sha1( "blob " + <decimal byte length> + "\x00" + <content> )  (git hash-object).
func crGitBlobHash(data []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// crUploadBlob stores the file bytes in the project's history-v1 blob store:
// PUT {v1_history}/projects/{historyId}/blobs/{hash}, basic auth.
func crUploadBlob(cxt *core.Cxt, historyID, hash string, data []byte) bool {
	base := strings.TrimSuffix(crV1HistoryBase(), "/")
	req, err := http.NewRequestWithContext(cxt.Req.Context(), "PUT",
		base+"/projects/"+historyID+"/blobs/"+hash, bytes.NewReader(data))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.SetBasicAuth(crV1HistoryUser(), crV1HistoryPass())
	resp, err := crHTTP.Do(req)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}
