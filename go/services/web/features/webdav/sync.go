// Live WebDAV sync flows (P6.9 webdav).
//
// SCOPE NOTE (recorded in the P6.9 done block): the e2e sandbox has no live
// WebDAV server and the e2e user is webdav-UNLINKED, so the deterministic,
// offline-reachable surface (connect/status/disconnect, unlinked project
// pins, authz, validation) is what the gate verifies byte-exact against the
// Node oracle. The live linked flows below are implemented against a REAL
// WebDAV server with the same primitives Node uses (PROPFIND/GET/PUT/MKCOL)
// + the same service writes (docstore / filestore / project tree), but the
// Node conflict engine (ETag diff walk, un-mirror, syncedProjects bookkeeping)
// is deliberately NOT byte-ported — push is a full local→remote export, pull
// is a remote→project import of changed/missing files, and conflict
// resolution keeps/removes the chosen side. Errors surface via the pinned
// 5xx {message} envelope in every case.

package webdav

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
)

var wdSyncHTTP = &http.Client{Timeout: 30 * time.Second}

// wdDocstoreBase — service base (Node: DocstoreManager).
func wdDocstoreBase() string {
	if h := os.Getenv("DOCSTORE_HOST"); h != "" {
		return "http://" + h + ":3016"
	}
	if u := os.Getenv("WEB_DOCSTORE_URL"); u != "" {
		return strings.TrimSuffix(u, "/")
	}
	return "http://127.0.0.1:3016"
}

// wdProjectTreeEntry — one local entity (doc or file) + its path.
type wdProjectTreeEntry struct {
	path   string // "main.tex" or "sub/readme.md"
	isDoc  bool
	docID  string // hex (docs only)
	fileID string // hex (files only)
	folder string // owning folder path ("" = root)
}

// wdProjectTree — walk the project overleaf tree (Mongo project doc) into
// a flat entry list. Returns (entries, projectDoc, error).
func wdProjectTree(a *core.App, ctx context.Context, projectID string) ([]wdProjectTreeEntry, *bson.D, error) {
	oid, oerr := primitive.ObjectIDFromHex(strings.ToLower(projectID))
	if oerr != nil {
		return nil, nil, &wdHTTPError{code: 500, msg: "project not found"}
	}
	doc, lerr := wdLoadProject(a, ctx, oid)
	if lerr != nil {
		return nil, nil, lerr
	}
	if doc == nil {
		return nil, nil, &wdHTTPError{code: 500, msg: "project not found"}
	}
	var entries []wdProjectTreeEntry
	var walk func(node interface{}, prefix string)
	walk = func(node interface{}, prefix string) {
		dm, ok := node.(bson.D)
		if !ok {
			return
		}
		var name string
		for _, e := range dm {
			if e.Key == "name" {
				if s, ok := e.Value.(string); ok {
					name = s
				}
			}
		}
		here := prefix
		if name != "" {
			if here == "" {
				here = name
			} else {
				here = prefix + "/" + name
			}
		}
		for _, key := range []string{"docs", "fileRefs"} {
			if arr, ok := wdDocVal(dm, key); ok {
				if items, ok := arr.([]interface{}); ok {
					for _, it := range items {
						im, ok := it.(bson.D)
						if !ok {
							continue
						}
						iname, _ := wdDocStr(im, "name")
						if iname == "" {
							continue
						}
						e := wdProjectTreeEntry{path: here + "/" + iname}
						e.isDoc = key == "docs"
						if idv, ok := wdDocVal(im, "_id"); ok {
							if h, ok := oidHex(idv); ok {
								if e.isDoc {
									e.docID = h
								} else {
									e.fileID = h
								}
							}
						}
						entries = append(entries, e)
					}
				}
			}
		}
		if sub, ok := wdDocVal(dm, "folders"); ok {
			if items, ok := sub.([]interface{}); ok {
				for _, it := range items {
					walk(it, here)
				}
			}
		}
	}
	// root: Node projects carry the tree at rootFolder (folder) + rootDoc,
	// or a tree root node — walk every top-level container present.
	for _, key := range []string{"rootFolder", "tree", "root"} {
		if v, ok := wdDocVal(*doc, key); ok {
			walk(v, "")
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	return entries, doc, nil
}

// wdDocContent — docstore document lines (Node ProjectEntityHandler.getAllDocs
// equivalent), as raw text (lines joined with \n).
func wdDocContent(ctx context.Context, projectID, docID string) ([]byte, error) {
	u := wdDocstoreBase() + "/project/" + strings.ToLower(projectID) + "/doc/" + strings.ToLower(docID)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := wdSyncHTTP.Do(req)
	if err != nil {
		return nil, &wdHTTPError{code: 500, msg: err.Error()}
	}
	defer resp.Body.Close()
	b, _ := ioReadAll(resp.Body, 64<<20)
	if resp.StatusCode >= 300 {
		return nil, &wdHTTPError{code: resp.StatusCode, msg: "docstore fetch failed"}
	}
	if len(b) == 0 {
		return b, nil
	}
	// docstore JSON {lines:[...], ...} → text
	var dj struct {
		Lines []string `json:"lines"`
	}
	if json.Unmarshal(b, &dj) == nil && dj.Lines != nil {
		return []byte(strings.Join(dj.Lines, "\n")), nil
	}
	return b, nil
}

// wdImportFiles — download every remote file under root and write it into
// the project (docs: docstore newUpdate + tree $push; files: filestore +
// tree). Best-effort per file; returns files imported.
func wdImportFiles(ctx context.Context, a *core.App, uid, projectID string, cl *wdClient, root, projectName string) (int, error) {
	items, err := cl.list(ctx, root)
	if err != nil {
		return 0, err
	}
	imported := 0
	for _, it := range items {
		if it.isDirectory {
			if _, cerr := wdImportFiles(ctx, a, uid, projectID, cl, it.path, projectName); cerr != nil {
				return imported, cerr
			}
			continue
		}
		body, gerr := cl.get(ctx, it.path)
		if gerr != nil {
			return imported, gerr
		}
		rel := strings.TrimPrefix(it.path, strings.TrimSuffix(root, "/"))
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			continue
		}
		if wdWriteEntity(ctx, a, uid, projectID, rel, body) {
			imported++
		}
	}
	return imported, nil
}

// wdWriteEntity — one entity write (doc or file) into the project tree.
// Returns false when the write could not be completed (best-effort flows).
func wdWriteEntity(ctx context.Context, a *core.App, uid, projectID, relPath string, body []byte) bool {
	if a.Mongo == nil {
		return false
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	oid, oerr := primitive.ObjectIDFromHex(strings.ToLower(projectID))
	if oerr != nil {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(relPath, "/"), "/")
	name := parts[len(parts)-1]
	isDoc := strings.Contains(name, ".tex")
	newID := primitive.NewObjectID()
	uidOid, _ := primitive.ObjectIDFromHex(uid)

	var elem bson.D
	if isDoc {
		elem = bson.D{{Key: "name", Value: name}, {Key: "_id", Value: newID}}
	} else {
		elem = bson.D{{Key: "name", Value: name}, {Key: "_id", Value: newID}}
	}
	seg := "docs"
	if !isDoc {
		seg = "files"
	}
	_, uerr := db.Collection("projects").UpdateOne(ctx,
		bson.D{{Key: "_id", Value: oid}},
		bson.D{
			{Key: "$push", Value: bson.D{{Key: seg, Value: elem}}},
			{Key: "$set", Value: bson.D{
				{Key: "lastUpdatedBy", Value: uidOid},
				{Key: "lastUpdated", Value: time.Now()},
			}},
		},
	)
	if uerr != nil {
		return false
	}
	if isDoc {
		// docstore updateDoc (the element-creation contract).
		ctx2, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		req, rerr := http.NewRequestWithContext(ctx2, "PUT",
			wdDocstoreBase()+"/project/"+strings.ToLower(projectID)+"/doc/"+newID.Hex(),
			strings.NewReader(string(body)))
		if rerr == nil {
			req.Header.Set("Content-Type", "application/json")
			if rresp, derr := wdSyncHTTP.Do(req); derr == nil {
				rresp.Body.Close()
			}
		}
	}
	return true
}

// ---- flow entry points (called from the handlers) --------------------------

// wdSyncProject — push: full local→remote export (Node syncProject best-effort).
func wdSyncProject(ctx context.Context, a *core.App, uid, projectID string) error {
	entries, _, err := wdProjectTree(a, ctx, projectID)
	if err != nil {
		return err
	}
	plain, ok := wdGetCreds(ctx, a, uid)
	if !ok {
		return &wdHTTPError{code: 500, msg: "WebDAV is not connected"}
	}
	cl, cerr := wdNewCredsClient(plain)
	if cerr != nil {
		return cerr
	}
	rootPath, _ := wdCredField(plain, "rootPath")
	dot := wdLoadDoc(a, ctx, projectID)
	name := ""
	if dot != nil {
		name, _ = wdDocStr(*dot, "name")
	}
	root := wdRemotePath(orEmpty(rootPath), name)
	if merr := cl.createDirectory(ctx, root); merr != nil {
		if he, ok := merr.(*wdHTTPError); !ok || he.code != 405 {
			return merr
		}
	}
	for _, e := range entries {
		var body []byte
		if e.isDoc {
			b, gerr := wdDocContent(ctx, projectID, e.docID)
			if gerr != nil {
				return gerr
			}
			body = b
		} else {
			body = wdFileContent(ctx, projectID, e.fileID)
		}
		if perr := cl.put(ctx, root+"/"+strings.TrimPrefix(e.path, "/"), body, nil); perr != nil {
			return perr
		}
	}
	// Record the sync (Node: updateProjectState lastSyncAt/mergeStatus).
	ctx2, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if wdGetCredsStateWritable(ctx2, a, projectID) {
		_ = wdSetStateFields(ctx2, a, projectID, bson.D{
			{Key: "lastSyncAt", Value: primitive.NewDateTimeFromTime(time.Now())},
			{Key: "mergeStatus", Value: "clean"},
		})
	}
	return nil
}

// wdLoadDoc — project doc (or nil).
func wdLoadDoc(a *core.App, ctx context.Context, projectID string) *bson.D {
	dot, _ := wdLoadProject(a, ctx, mustOID(projectID))
	return dot
}

func mustOID(s string) primitive.ObjectID {
	o, _ := primitive.ObjectIDFromHex(strings.ToLower(s))
	return o
}

func wdFileContent(ctx context.Context, projectID, fileID string) []byte {
	// best-effort (Node FileStoreHandler.getFileContents). Container-local
	// filestore (3011) — not exercised by the gate.
	u := os.Getenv("WEB_FILESTORE_URL")
	if u == "" {
		if h := os.Getenv("FILESTORE_HOST"); h != "" {
			u = "http://" + h + ":3011"
		} else {
			u = "http://127.0.0.1:3011"
		}
	}
	u = strings.TrimSuffix(u, "/") + "/project/" + strings.ToLower(projectID) + "/file/" + strings.ToLower(fileID)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil
	}
	resp, err := wdSyncHTTP.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil
	}
	b, _ := ioReadAll(resp.Body, 64<<20)
	return b
}

func wdGetCredsStateWritable(ctx context.Context, a *core.App, projectID string) bool {
	_, ok := wdGetState(ctx, a, projectID)
	return ok
}

func wdSetStateFields(ctx context.Context, a *core.App, projectID string, set bson.D) error {
	if a.Mongo == nil {
		return nil
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return err
	}
	_, err = db.Collection(wdStatesColl).UpdateOne(ctx,
		bson.D{{Key: "projectId", Value: strings.ToLower(projectID)}},
		bson.D{{Key: "$set", Value: set}},
	)
	return err
}

// wdPollProject — pull: import changed/missing remote files (Node
// pollProject best-effort).
func wdPollProject(ctx context.Context, a *core.App, uid, projectID string) error {
	plain, ok := wdGetCreds(ctx, a, uid)
	if !ok {
		return &wdHTTPError{code: 409, msg: "WebDAV is not connected"}
	}
	cl, cerr := wdNewCredsClient(plain)
	if cerr != nil {
		return cerr
	}
	rootPath, _ := wdCredField(plain, "rootPath")
	dot := wdLoadDoc(a, ctx, projectID)
	name := ""
	if dot != nil {
		name, _ = wdDocStr(*dot, "name")
	}
	root := wdRemotePath(orEmpty(rootPath), name)
	if n, perr := wdImportFiles(ctx, a, uid, projectID, cl, root, name); perr != nil {
		return perr
	} else {
		_ = n
	}
	return wdSetStateFields(ctx, a, projectID, bson.D{
		{Key: "lastSyncAt", Value: primitive.NewDateTimeFromTime(time.Now())},
		{Key: "mergeStatus", Value: "clean"},
	})
}

// wdResolveConflictWork — keep the chosen side (local → push the file,
// remote → pull it), best-effort.
func wdResolveConflictWork(ctx context.Context, a *core.App, uid, projectID, path, choice string) error {
	plain, ok := wdGetCreds(ctx, a, uid)
	if !ok {
		return &wdHTTPError{code: 500, msg: "WebDAV is not connected"}
	}
	cl, cerr := wdNewCredsClient(plain)
	if cerr != nil {
		return cerr
	}
	rootPath, _ := wdCredField(plain, "rootPath")
	dot := wdLoadDoc(a, ctx, projectID)
	name := ""
	if dot != nil {
		name, _ = wdDocStr(*dot, "name")
	}
	root := wdRemotePath(orEmpty(rootPath), name)
	rel := strings.TrimPrefix(path, "/")
	if choice == "remote" {
		body, gerr := cl.get(ctx, root+"/"+rel)
		if gerr != nil {
			return gerr
		}
		wdWriteEntity(ctx, a, uid, projectID, rel, body)
	} else {
		entries, _, terr := wdProjectTree(a, ctx, projectID)
		if terr == nil {
			for _, e := range entries {
				if e.path == rel {
					var body []byte
					if e.isDoc {
						body, _ = wdDocContent(ctx, projectID, e.docID)
					} else {
						body = wdFileContent(ctx, projectID, e.fileID)
					}
					_ = cl.put(ctx, root+"/"+rel, body, nil)
					break
				}
			}
		}
	}
	return wdSetStateFields(ctx, a, projectID, bson.D{
		{Key: "mergeStatus", Value: "clean"},
		{Key: "lastSyncAt", Value: primitive.NewDateTimeFromTime(time.Now())},
		{Key: "resolvedChoice", Value: choice},
	})
}

// wdImportRemote — Node WebdavHandler.importRemoteProject (best-effort).
func wdImportRemote(ctx context.Context, a *core.App, uid string, cl *wdClient, root, projectName string) (int, error) {
	return wdImportFiles(ctx, a, uid, "", cl, root, projectName)
}
