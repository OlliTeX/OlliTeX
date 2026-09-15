// P4.12b — document download: GET (and HEAD) /Project/:Project_id/doc/:Doc_id/download
// The "/download" suffix distinguishes this public route from the private
// API `doc/:doc_id` (router.mjs:642 comment).
//
// Node oracle (DocumentUpdaterController.getDoc), pinned live 2026-09-15:
//
//	  order: anonymous → 401 'Unauthorized' (accept json) / 302 /login
//	         (core global login bounce) → zz.objectId(params) → 404 JSON VA
//	         (params.Project_id / params.Doc_id, statusCode 404 — the
//	         enforce-log mode enforces logOnly schemas too, P1 pin) →
//	         project load (ghost → 404 app page) → ensureUserCanReadProject
//	         (non-member → 403 JSON {restricted} / restricted page) →
//	         findElement type doc (missing → 404 sendStatus = text/plain
//	         'Not Found') →
//	         DU GET {cdu}/project/{pid}/doc/{did}?fromVersion=-1
//	         (DU 404/error → web 500 sendStatus 'Internal Server Error')
//	         → 200 with:
//	             Content-Type: text/plain; charset=utf-8
//	             Content-Disposition: attachment; filename="<doc.name>"
//	             body = lines.join('\n')
//
//	  HEAD: express auto-maps HEAD→GET — Node returns 200 with the same
//	  headers (ETag/CL present) and NO body.
package projectlist

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

var docdlPat = regexp.MustCompile(`^/Project/([^/]+)/doc/([^/]+)/download$`)

// docdlFindDoc walks the raw rootFolder for a doc element id — element
// arrays before recursive folders (ProjectLocator.findElement order).
func docdlFindDoc(root any, didHex string) (name string, ok bool) {
	var walk func(v any) bool
	walk = func(v any) bool {
		if !entIsDocObj(v) {
			return false
		}
		for _, d := range entArr(entFld(v, "docs")) {
			if entIsDocObj(d) && oidHex(entFld(d, "_id")) == didHex {
				name = asStr(entFld(d, "name"))
				return true
			}
		}
		for _, f := range entArr(entFld(v, "folders")) {
			if walk(f) {
				return true
			}
		}
		return false
	}
	for _, v := range entArr(root) {
		if walk(v) {
			return name, true
		}
	}
	return "", false
}

func docDownloadHandler(a *core.App) func(cxt *core.Cxt, res *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		req := cxt.Req
		mm := docdlPat.FindStringSubmatch(req.URL.Path)
		if mm == nil {
			views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(req.URL.Path, "/")))
			return
		}
		pidHex, didHex := mm[1], mm[2]

		// zz.objectId(params) — probe-pinned 404 JSON VA (enforce-log).
		if !delHex24(pidHex) {
			res.JSON(404, delParamVA("Project_id"))
			return
		}
		if !delHex24(didHex) {
			res.JSON(404, delParamVA("Doc_id"))
			return
		}
		oid, _ := primitive.ObjectIDFromHex(pidHex)

		uid := ""
		if cxt.Sess != nil {
			uid = cxt.Sess.UserIDHex()
		}

		doc, lerr := loadProjectFull(a, cxt, oid)
		if lerr != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		if doc == nil {
			views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(req.URL.Path, "/")))
			return
		}
		if !canRead(uid, loadUserAdmin(a, cxt, uid), *doc) {
			if core.AcceptsJSON(req) {
				res.JSON(403, []byte(colRestricted))
			} else {
				views.Restricted403(res.W, pageBase(cxt, strings.TrimPrefix(req.URL.Path, "/")))
			}
			return
		}

		dname, dok := docdlFindDoc(dget(*doc, "rootFolder"), didHex)
		if !dok {
			// Node: res.sendStatus(404) → text/plain 'Not Found'.
			res.PlainText(404, "Not Found")
			return
		}

		base := cduBase()
		up, err := http.NewRequestWithContext(req.Context(), http.MethodGet,
			base+"/project/"+pidHex+"/doc/"+didHex+"?fromVersion=-1", nil)
		if err != nil {
			res.PlainText(500, "Internal Server Error")
			return
		}
		resp, err := crHTTP.Do(up)
		if err != nil {
			res.PlainText(500, "Internal Server Error")
			return
		}
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<24))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			res.PlainText(500, "Internal Server Error")
			return
		}
		var dj struct {
			Lines []string `json:"lines"`
		}
		if err := json.Unmarshal(buf, &dj); err != nil || dj.Lines == nil {
			res.PlainText(500, "Internal Server Error")
			return
		}
		body := strings.Join(dj.Lines, "\n")

		// Node: setContentDisposition('attachment', {filename: doc.name}).
		res.W.Header().Set("Content-Disposition", "attachment; filename=\""+dname+"\"")

		if req.Method == http.MethodHead {
			// express HEAD → same headers, no body.
			h := res.W.Header()
			h.Set("Content-Type", "text/plain; charset=utf-8")
			h.Set("ETag", core.EtagWeakBody(body))
			h.Set("Content-Length", fmt.Sprint(len([]byte(body))))
			res.W.WriteHeader(http.StatusOK)
			return
		}
		res.PlainText(http.StatusOK, body)
	}
}
