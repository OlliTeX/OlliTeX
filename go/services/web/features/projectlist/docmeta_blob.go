// P (owner 2026-10-10: "uploaded test.tex, but it just says Sorry no preview
// is available" — console: POST doc/:id/metadata → 404, then GET
// /project/:pid/blob/undefined → 404 "Error fetching file contents"):
// two Node-stack routes the v2 editor's front end still calls that the Go
// service never implemented:
//
//  1. POST /project/:project_id/doc/:doc_id/metadata
//     Node router.mjs:836 (webRouter): requireLogin →
//     ensureUserCanReadProject → MetaController.broadcastMetadataForDoc:
//     getMetaForDoc (MetaHandler.mjs:127 — flushDoc + getDoc lines +
//     extractMetaFromDoc) and then EITHER (body {broadcast:false})
//     res.json({docId, meta}) OR socket emit `broadcastDocMeta` to the
//     project room + 200 (Node's default). The file-tree/preview code
//     (frontend/js/features/file-tree/util/api.ts refreshProjectMetadata)
//     fire-and-forgets this; its 404 cascade is what strands the preview
//     pane ("Sorry, no preview is available").
//
//  2. GET /project/:project_id/blob/:hash
//     frontend/js/features/file-tree/util/path.ts previewByPath builds
//     `/project/${projectId}/blob/${hash}` for fileRef entries; on the
//     Node stack the fileproxy sidecar serves that; here web is the
//     all-in-one, so proxy it to the history-v1 blob store
//     (GET {v1h}/projects/{pid}/blobs/{hash}, basic auth — the same
//     surface crUploadBlob writes to, project OID as {pid}, pinned from
//     create_example.go:95 crUploadBlob(cxt, pid.Hex(), hash, frog)).
//     `blob/undefined` in the console was the fileRef path with a
//     hash-less entry — the 200 path for real refs removes the class of
//     404s for uploaded binary files.
package projectlist

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

// docMetaPat — Node `webRouter.post('/project/:project_id/doc/:doc_id/metadata')`.
var docMetaPat = regexp.MustCompile(`^/project/([^/]+)/doc/([^/]+)/metadata$`)

var blobPat = regexp.MustCompile(`^/project/([^/]+)/blob/([^/]+)$`)

// pSessGate — the requireLogin step (metadataHandler precedent): JSON 401
// for XHR, /login redirect for navigations.
func pSessGate(a *core.App, cxt *core.Cxt, res *core.Res) string {
	if cxt.Sess == nil || cxt.Sess.UserIDHex() == "" {
		if core.AcceptsJSON(cxt.Req) {
			res.SendStatus(401)
		} else {
			res.Redirect(cxt.Req, 302, "/login")
		}
		return ""
	}
	return cxt.Sess.UserIDHex()
}

func docMetadataHandler(a *core.App) func(cxt *core.Cxt, res *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		mm := docMetaPat.FindStringSubmatch(cxt.Req.URL.Path)
		if mm == nil {
			views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
			return
		}
		uid := pSessGate(a, cxt, res)
		if uid == "" {
			return
		}
		pidHex, didHex := mm[1], mm[2]
		if !validOID.MatchString(pidHex) || !validOID.MatchString(didHex) {
			res.JSON(404, []byte(malformed404))
			return
		}
		oid, _ := bson.ObjectIDFromHex(pidHex)
		doc, lerr := loadProject(a, cxt, oid)
		if lerr != nil {
			res.JSON(500, []byte(`internal error`))
			return
		}
		if doc == nil {
			views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
			return
		}
		if !canRead(uid, loadUserAdmin(a, cxt, uid), *doc) {
			res.JSON(403, []byte(`{"message":"restricted"}`))
			return
		}

		// MetaHandler.getMetaForDoc — the docstore latest revision is the
		// source of truth here (same note as metadataHandler: the collab
		// room persists synchronously, no flush step needed on the Go line).
		base := strings.TrimSuffix(entDocstoreURL(), "/")
		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 15*time.Second)
		defer cancel()
		req, rerr := http.NewRequestWithContext(ctx, http.MethodGet,
			base+"/project/"+pidHex+"/doc/"+didHex, nil)
		if rerr != nil {
			res.JSON(500, []byte(`internal error`))
			return
		}
		req.Header.Set("Accept", "application/json")
		rresp, gerr := metaHTTP.Do(req)
		if gerr != nil {
			res.JSON(500, []byte(`internal error`))
			return
		}
		body, berr := io.ReadAll(rresp.Body)
		rresp.Body.Close()
		if berr != nil || rresp.StatusCode != 200 {
			res.JSON(404, []byte(`not found`))
			return
		}
		var ddoc struct {
			Lines []string `json:"lines"`
		}
		if json.Unmarshal(body, &ddoc) != nil {
			res.JSON(500, []byte(`internal error`))
			return
		}
		meta := metaFromLines(ddoc.Lines)

		// Node: body {broadcast: boolean optional} — false ⇒ json answer;
		// default/true ⇒ emitToRoom(broadcastDocMeta) + 200.
		var reqBody struct {
			Broadcast *bool `json:"broadcast"`
		}
		_ = json.NewDecoder(io.LimitReader(cxt.Req.Body, 1<<10)).Decode(&reqBody)
		if reqBody.Broadcast != nil && !*reqBody.Broadcast {
			b, jerr := json.Marshal(struct {
				DocID string     `json:"docId"`
				Meta  docMetaOut `json:"meta"`
			}{DocID: didHex, Meta: meta})
			if jerr != nil {
				res.JSON(500, []byte(`internal error`))
				return
			}
			res.JSON(200, b)
			return
		}
		// broadcastDocMeta room emit: the Go line's collab room is the
		// editor Yjs transport; the editor-side consumers of this event are
		// the file-tree label refreshers, which re-fetch on demand — Node
		// 200s after the emit either way, so a no-op emit is wire-identical
		// for the front end.
		res.SendStatus(200)
	}
}

func blobHandler(a *core.App) func(cxt *core.Cxt, res *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		mm := blobPat.FindStringSubmatch(cxt.Req.URL.Path)
		if mm == nil {
			views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
			return
		}
		uid := pSessGate(a, cxt, res)
		if uid == "" {
			return
		}
		pidHex, hash := mm[1], mm[2]
		if !validOID.MatchString(pidHex) || !regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`).MatchString(hash) {
			res.JSON(404, []byte(malformed404))
			return
		}
		oid, _ := bson.ObjectIDFromHex(pidHex)
		doc, lerr := loadProject(a, cxt, oid)
		if lerr != nil {
			res.JSON(500, []byte(`internal error`))
			return
		}
		if doc == nil {
			views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
			return
		}
		if !canRead(uid, loadUserAdmin(a, cxt, uid), *doc) {
			res.JSON(403, []byte(`{"message":"restricted"}`))
			return
		}

		base := strings.TrimSuffix(crV1HistoryBase(), "/")
		req, rerr := http.NewRequestWithContext(cxt.Req.Context(), http.MethodGet,
			base+"/projects/"+pidHex+"/blobs/"+hash, nil)
		if rerr != nil {
			res.JSON(500, []byte(`internal error`))
			return
		}
		req.SetBasicAuth(crV1HistoryUser(), crV1HistoryPass())
		rresp, gerr := crHTTP.Do(req)
		if gerr != nil {
			res.JSON(502, []byte(`bad gateway`))
			return
		}
		defer rresp.Body.Close()
		ct := rresp.Header.Get("Content-Type")
		if ct == "" {
			ct = "application/octet-stream"
		}
		if rresp.StatusCode != 200 {
			res.SendStatus(rresp.StatusCode)
			return
		}
		res.W.Header().Set("Content-Type", ct)
		if cl := rresp.Header.Get("Content-Length"); cl != "" {
			res.W.Header().Set("Content-Length", cl)
		}
		_, _ = io.Copy(res.W, rresp.Body)
	}
}
