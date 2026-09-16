// P4.12a — file proxy: GET (and HEAD) /Project/:Project_id/file/:File_id
// Node oracle (FileStore/FileStoreController.mjs getFile/getFileHead),
// pinned live 2026-09-15:
//
//	order: anonymous → 302 /login (requireGlobalLogin, core bounce) →
//	       zz.objectId(params) → 404 JSON VA (params.Project_id /
//	       params.File_id, statusCode 404) → project load (ghost → 404 app
//	       page) → ensureUserCanReadProject (non-member → 403 restricted
//	       page, or JSON {"message":"restricted"} on accept:json) →
//	       findElement file (missing → 404 EMPTY, no body/CT) →
//	       GET {v1_history}/api/projects/{hid}/blobs/{hash} (basic auth
//	       staging:$V1_HISTORY_PASSWORD; 404 → web 404 empty; other → 500
//	       empty) → 200 with:
//	           Content-Disposition: attachment; filename="<name>"
//	           Cache-Control: private, max-age=3600
//	           Content-Length only when the upstream provided one
//	           **no Content-Type** (express never sets one — pinned)
//	           body = the blob
//
//	HEAD quirk (pinned): history-v1 registers NO HEAD blob route in this
//	fork, so the Node HEAD → history 404 → web **404 empty** — replicated
//	directly (same observable result).
package projectlist

import (
	"io"
	"net/http"
	"regexp"
	"strings"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

var fproxyPat = regexp.MustCompile(`^/Project/([^/]+)/file/([^/]+)$`)

func fproxyEmpty(res *core.Res, code int) {
	// Node: res.status(code).end() → empty body, no content-type.
	res.W.Header().Del("Content-Type")
	res.W.WriteHeader(code)
}

// fproxyFindFile walks the raw rootFolder tree returning name+hash+found
// for the given file id (element-arrays before recursive folders, mirror of
// ProjectLocator.findElement order).
func fproxyFindFile(root any, fidHex string) (fname, fhash string, found bool) {
	arr := entArr(root)
	if arr == nil {
		return "", "", false
	}
	var walk func(v any) bool
	walk = func(v any) bool {
		if !entIsDocObj(v) {
			return false
		}
		for _, fv := range entArr(entFld(v, "fileRefs")) {
			if entIsDocObj(fv) && entHexOf(fv) == fidHex {
				fname = asStr(entFld(fv, "name"))
				fhash = asStr(entFld(fv, "hash"))
				return true
			}
		}
		for _, sv := range entArr(entFld(v, "folders")) {
			if walk(sv) {
				return true
			}
		}
		return false
	}
	for _, v := range arr {
		if walk(v) {
			return fname, fhash, true
		}
	}
	return "", "", false
}

func entHexOf(v any) string {
	if !entIsDocObj(v) {
		return ""
	}
	return oidHex(entFld(v, "_id"))
}

func fproxyHistoryID(doc any) string {
	ov := entFld(doc, "overleaf")
	if ov == nil {
		return ""
	}
	hv := entFld(ov, "history")
	if hv == nil {
		return ""
	}
	return oidHex(entFld(hv, "id"))
}

func fileProxyHandler(a *core.App) func(cxt *core.Cxt, res *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		req := cxt.Req
		mm := fproxyPat.FindStringSubmatch(req.URL.Path)
		if mm == nil {
			views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(req.URL.Path, "/")))
			return
		}
		pidHex, fidHex := mm[1], mm[2]

		// zz.objectId(params) — probe-pinned: 404 JSON VA, statusCode 404.
		if !delHex24(pidHex) {
			res.JSON(404, delParamVA("Project_id"))
			return
		}
		if !delHex24(fidHex) {
			res.JSON(404, delParamVA("File_id"))
			return
		}
		oid, _ := primitive.ObjectIDFromHex(pidHex)
		oidF, _ := primitive.ObjectIDFromHex(fidHex)
		_ = oidF

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

		// HEAD: history-v1 has no HEAD blob route in this fork → Node 404
		// empty (pinned); replicate directly.
		if req.Method == http.MethodHead {
			fproxyEmpty(res, http.StatusNotFound)
			return
		}

		fname, fhash, found := fproxyFindFile(dget(*doc, "rootFolder"), fidHex)
		if !found {
			fproxyEmpty(res, http.StatusNotFound)
			return
		}
		hid := fproxyHistoryID(*doc)

		base := strings.TrimSuffix(crV1HistoryBase(), "/")
		up, err := http.NewRequestWithContext(req.Context(), http.MethodGet,
			base+"/projects/"+hid+"/blobs/"+fhash, nil)
		if err != nil {
			fproxyEmpty(res, http.StatusInternalServerError)
			return
		}
		up.SetBasicAuth(crV1HistoryUser(), crV1HistoryPass())
		resp, err := crHTTP.Do(up)
		if err != nil {
			fproxyEmpty(res, http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusOK:
			h := res.W.Header()
			// express setContentDisposition: attachment; filename="<name>" (pinned plain-quoted form)
			h.Set("Content-Disposition", "attachment; filename=\""+fname+"\"")
			h.Set("Cache-Control", "private, max-age=3600")
			// Node 200 is chunked with NO Content-Length (pinned) — do not
			// forward the upstream CL; stream the body (Go → chunked).
			h.Del("Content-Length")
			h.Del("Content-Type")
			res.W.WriteHeader(http.StatusOK)
			_, _ = io.Copy(res.W, resp.Body)
		case resp.StatusCode == http.StatusNotFound:
			fproxyEmpty(res, http.StatusNotFound)
		default:
			fproxyEmpty(res, http.StatusInternalServerError)
		}
	}
}
