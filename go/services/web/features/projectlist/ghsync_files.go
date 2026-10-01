// gsync seams continued: current project contents (export + merge engines).
//
// Node HistoryManager (latestVersion / getPathsAtVersion /
// getProjectFileTreeDiff / getProjectFileBuffer) reads the history API. In
// the D41 Go stack the docstore (current lines) + v1 blobs (file bytes) are
// the read path (same sources the P4.12a file proxy + doc download use), and
// the project structure is the mongo rootFolder tree. These exported seams
// give the gsync engines that exact view, per file path.
package projectlist

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"ollitex/go/services/web/core"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// GSProjFile — one current project file (path = /-separated, no leading
// slash; Data = exact bytes: doc = lines joined with \n, file = blob).
type GSProjFile struct {
	Path string
	Data []byte
}

// GSExportFiles — walk the current project tree (rootFolder) and materialize
// every file. Doc = docstore current lines; binary file = v1 blob by hash.
// ok=false on a missing project or unreadable content.
func GSExportFiles(a *core.App, cxt *core.Cxt, pid bson.ObjectID) ([]GSProjFile, bool) {
	oid := pid
	if oid.IsZero() {
		oid, _ = bson.ObjectIDFromHex(pidStr(pid))
	}
	doc, err := loadProjectFull(a, cxt, oid)
	if err != nil || doc == nil {
		return nil, false
	}
	root := dget(*doc, "rootFolder")
	if root == nil {
		return []GSProjFile{}, true
	}
	hid := fproxyHistoryID(*doc)
	var out []GSProjFile
	// rootFolder is an array of root-level folder nodes (the array-of-1
	// wrapper holds the "rootFolder" node). Each node carries name,
	// docs[], fileRefs[], folders[] — the P4.12a fproxy shape. Node
	// HistoryManager.getPathsAtVersion lists repo-relative paths: the
	// rootFolder WRAPPER NAME is NOT part of any path (main.tex, not
	// rootFolder/main.tex); nested folders keep their names.
	var walkNode func(v any, prefix string, selfName bool) bool
	walkNode = func(v any, prefix string, selfName bool) bool {
		if !entIsDocObj(v) {
			return true
		}
		nm := asStr(entFld(v, "name"))
		p := prefix
		if selfName && nm != "" {
			if p != "" {
				p += "/"
			}
			p += nm
		}
		child := func(nm2 string) string {
			if p == "" {
				return nm2
			}
			return p + "/" + nm2
		}
		// docs of this folder node
		for _, dv := range entArr(entFld(v, "docs")) {
			if !entIsDocObj(dv) {
				continue
			}
			dname := asStr(entFld(dv, "name"))
			if dname == "" {
				continue
			}
			idHex := entHexOf(dv)
			if idHex == "" {
				return false
			}
			lines := gsDocLinesReq(cxt, oid.Hex(), idHex)
			if lines == nil {
				return false
			}
			out = append(out, GSProjFile{Path: child(dname), Data: []byte(strings.Join(lines, "\n"))})
		}
		// fileRefs of this folder node (hash at top level or legacy data.data.hash)
		for _, fv := range entArr(entFld(v, "fileRefs")) {
			if !entIsDocObj(fv) {
				continue
			}
			fname := asStr(entFld(fv, "name"))
			if fname == "" {
				continue
			}
			hash := asStr(entFld(fv, "hash"))
			if hash == "" {
				hash = asStr(entFld(entFld(fv, "data"), "hash"))
			}
			if hash == "" {
				if dvv := entFld(entFld(fv, "data"), "data"); dvv != nil {
					hash = asStr(entFld(dvv, "hash"))
				}
			}
			if hash == "" {
				return false
			}
			b, ok4 := gsBlobReq(cxt, oid.Hex(), hid, hash)
			if !ok4 {
				return false
			}
			out = append(out, GSProjFile{Path: child(fname), Data: b})
		}
		// nested folders (their own name IS part of child paths)
		for _, kv := range entArr(entFld(v, "folders")) {
			if !walkNode(kv, prefix, true) {
				return false
			}
		}
		return true
	}
	for _, v := range entArr(root) {
		if !walkNode(v, "", false) {
			return nil, false
		}
	}
	return out, true
}

func pidStr(oid bson.ObjectID) string { return oid.Hex() }

func gsDocLinesReq(cxt *core.Cxt, pidHex, docID string) []string {
	base := crDocstoreBase()
	req, err := http.NewRequestWithContext(cxt.Req.Context(), http.MethodGet,
		strings.TrimSuffix(base, "/")+"/project/"+pidHex+"/doc/"+docID, nil)
	if err != nil {
		return nil
	}
	resp, err := crHTTP.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var dj struct {
		Lines []string `json:"lines"`
	}
	if json.Unmarshal(b, &dj) != nil {
		return nil
	}
	return dj.Lines
}

func gsBlobReq(cxt *core.Cxt, pidHex, hid, hash string) ([]byte, bool) {
	base := crV1HistoryBase()
	req, err := http.NewRequestWithContext(cxt.Req.Context(), http.MethodGet,
		strings.TrimSuffix(base, "/")+"/projects/"+hid+"/blobs/"+hash, nil)
	if err != nil {
		return nil, false
	}
	req.SetBasicAuth(crV1HistoryUser(), crV1HistoryPass())
	resp, err := crHTTP.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	return b, true
}

var _ = context.Background
