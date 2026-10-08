package compile

import (
	"regexp"
	"sort"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"

	"ollitex/go/services/web/core"
)

// Owner 2026-10-07 item P — rail "Search in project" returned NOTHING for
// words the user could see in the editor. Root cause (verified live against
// the ollitex DB):
//
//   - The front-end ProjectSnapshot builds its corpus from the V1-history
//     plane (GET /project/:id/latest/history + /blob/:hash).
//   - For S2 (Yjs/ygo) projects the V1 history is EMPTY:
//       projectHistoryChunks / projectHistoryBlobs / rooms / docSnapshots = 0
//     for the project, so the snapshot degrades to empty (audit-020).
//   - The ACTUAL doc text lives in the docstore (the `docs` collection,
//     project_id = ObjectId) — the same source compile reads (getDocLines) —
//     and all 5 of the owner's docs are present there with full text.
//
// The two pieces the corpus needs are already in this package:
//   - walk(rootFolder)   -> []treeDoc{Path, ID}   (path + doc id)
//   - getDocLines(pid)   -> map[docID][]string     (doc id -> text lines)
//
// This endpoint joins them and serves a plain {path, content} corpus the
// front-end can feed into full-project search (and any other snapshot
// consumer) WITHOUT the empty V1-history plane.
//
// Wire:
//
//	GET /project/:id/search-corpus   (session, read-auth — same gate as
//	                                  POST /compile via preflight/canRead)
//	  -> 200  [{ "path":"main.tex","content":"\\documentclass{article}\n..."}, ...]
//	  -> 401/302/403/404 (same shapes as the other project routes)
var searchCorpusPat = regexp.MustCompile(`^/project/([^/]+)/search-corpus$`)

// corpusOut — one editable doc (path + full text). Blank docs are omitted
// (same as the compile resource builder: a doc with no bytes contributes
// nothing to search and Node omits stub entries).
type corpusOut struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// searchCorpusHandler — see the package comment in this file. It reuses the
// exact project-load + read-authz preflight used by POST /compile and the
// exact tree-walk + docstore read that assembles the compile body, so the
// corpus is provably the same text the compiler sees.
func searchCorpusHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		param := cxt.Params["1"]
		if cxt.Sess == nil || cxt.Sess.UserIDHex() == "" {
			denyRead(cxt, res)
			return
		}
		// preflight: invalid ObjectId (404) -> absent project (404) ->
		// !canRead (403), exactly like POST /compile.
		p, ok := preflight(cxt, res, a, param)
		if !ok {
			return
		}
		ctx := cxt.Req.Context()
		pid := strings.ToLower(param)

		rootFolder, _ := dget(*p, "rootFolder").(bson.A)
		docs, _ := walk(rootFolder)
		lines, derr := getDocLines(ctx, pid)
		if derr != nil {
			res.JSON(500, []byte(internal500))
			return
		}

		out := make([]corpusOut, 0, len(docs))
		for _, d := range docs {
			l, ok := lines[d.ID]
			if !ok {
				continue // doc not (yet) in the docstore -> no text to search
			}
			content := strings.Join(l, "\n")
			if content == "" {
				continue // blank doc contributes nothing
			}
			out = append(out, corpusOut{Path: d.Path, Content: content})
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
		res.JSON(200, core.JSON(out))
	}
}
