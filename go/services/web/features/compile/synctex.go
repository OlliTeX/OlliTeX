// Package compile — synctex proxy (port of the Node web's
// CompileController.proxySyncCode / proxySyncPdf, the web→clsi layer that the
// P5.2a compile port initially left out). These are the CROWN-JEWEL routes:
// click-to-PDF ( /sync/code -> { pdf:[...] } ) and click-to-source
// ( /sync/pdf  -> { code:[{file,line}] } ).
//
// The synctex data comes from the CLSIDE, which is per-compiler:
//   - tex   -> clsitex  (.synctex / output.synctex.gz)
//   - typst -> clsitypst (output.sourcemap.json, the d101c2f5 fork)
//
// Both services emit the SAME wire ({pdf:[...]} for code, {code:[...]} for
// pdf), so this proxy is compiler-agnostic EXCEPT for the base URL dispatch —
// the exact same `if dispatch=="typst" { base=clsiTypstBase() }` rule used by
// compile + wordcount. See docs/clsi-typst-integration.md (c08).
//
// Oracle (services/web/app/src) — CONTRACT PINS:
//
//	router.mjs (session router, ensureUserCanReadProject):
//	  GET /project/:Project_id/sync/code  -> CompileController.proxySyncCode
//	  GET /project/:Project_id/sync/pdf   -> CompileController.proxySyncPdf
//
//	CompileController.proxySyncCode (validated query):
//	  file (required, a non-rooted simple path — /./ collapses allowed),
//	  line  (^\d+$), column (^\d+$), buildId (required), editorId?,
//	  clsiserverid?   ->  direction "code", validatedOptions {file,line,column}
//	CompileController.proxySyncPdf (validated query):
//	  page (^\d+$), h (^-?\d+(\.\d+)?$), v (^-?\d+(\.\d+)?$),
//	  buildId (required), editorId?, clsiserverid?
//	    ->  direction "pdf", validatedOptions {page,h,v}
//	  Invalid param -> throw -> 500 (NOT 400; logOnly phase). buildId is required
//	  (zz.buildId()), so a missing buildId is a 500 in the Node oracle too.
//
//	ClsiManager.syncTeX -> _getCompilerUrl(..., userId, `sync/${direction}`):
//	  <clsi|clsi_typst>/project/<pid>/user/<uid>/sync/<code|pdf>
//	    ?compileBackendClass=<..>&compileGroup=<..>
//	    &compileFromClsiCache=<bool>   (saas + alpha/priority only — CE: false)
//	    &imageName=<..>                (when the project has one; typst: N/A)
//	    &<validatedOptions...>         (file/line/column or page/h/v)
//	    &editorId=<..>&buildId=<..>&clsiserverid=<..>
//	  response body (JSON) -> res.json(body) passthrough (key shape owned by the
//	  clsi: {pdf:[...]} or {code:[{file,line,?column}]}).
//	  clsi 404 -> NotFoundError -> res 404 (empty). other error -> throw -> 500.
package compile

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"ollitex/go/services/web/core"

	"go.mongodb.org/mongo-driver/v2/bson"
)

var (
	syncCodePat = regexp.MustCompile(`^/[Pp]roject/([^/]+)/sync/code$`)
	syncPdfPat  = regexp.MustCompile(`^/[Pp]roject/([^/]+)/sync/pdf$`)

	reDigits  = regexp.MustCompile(`^\d+$`)
	reNumSign = regexp.MustCompile(`^-?\d+(\.\d+)?$`)
)

// validPathParam mirrors the Node `Path.resolve('/', testPath) === '/'+testPath`
// guard (a non-rooted, no-.. simple path; /./ is allowed).
func validPathParam(p string) bool {
	if p == "" {
		return false
	}
	if strings.HasPrefix(p, "/") {
		return false
	}
	testPath := strings.ReplaceAll(p, "/./", "/")
	// any remaining /.. or absolute path fails parity.
	if strings.HasPrefix(testPath, "../") || strings.Contains(testPath, "/../") {
		return false
	}
	return true
}

// syncProxy is the web->clsi synctex proxy (direction "code" or "pdf").
func syncProxy(a *core.App, direction string) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if cxt.Sess == nil || cxt.Sess.UserIDHex() == "" {
			denyRead(cxt, res)
			return
		}
		uid := cxt.Sess.UserIDHex()
		p, ok := preflight(cxt, res, a, cxt.Params["1"])
		if !ok {
			return
		}
		pid := strings.ToLower(cxt.Params["1"])
		ctx := cxt.Req.Context()
		q := cxt.Req.URL.Query()

		// ---- oracle param validation (invalid -> throw -> 500; logOnly era)
		if direction == "code" {
			if q.Get("file") == "" || !validPathParam(q.Get("file")) {
				res.JSON(500, []byte(internal500))
				return
			}
			if !reDigits.MatchString(q.Get("line")) ||
				!reDigits.MatchString(q.Get("column")) {
				res.JSON(500, []byte(internal500))
				return
			}
		} else {
			if !reDigits.MatchString(q.Get("page")) ||
				!reNumSign.MatchString(q.Get("h")) ||
				!reNumSign.MatchString(q.Get("v")) {
				res.JSON(500, []byte(internal500))
				return
			}
		}
		if q.Get("buildId") == "" {
			res.JSON(500, []byte(internal500)) // zz.buildId() required
			return
		}

		// ---- limits (compileBackendClass / compileGroup) from the owner, same
		//      rule as compile/wordcount.
		ownerHex := oidHex(dget(*p, "owner_ref"))
		var ownerDoc *bson.D
		if ownerHex != "" && a.Mongo != nil {
			if ooid, oerr := bson.ObjectIDFromHex(ownerHex); oerr == nil {
				if db, derr := a.Mongo.DB(ctx); derr == nil {
					var od bson.D
					if db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: ooid}}).Decode(&od) == nil {
						ownerDoc = &od
					}
				}
			}
		}
		projectCompiler, _ := dget(*p, "compiler").(string)
		lim := computeLimits(ownerDoc, projectCompiler)
		dispatch := lim.CompilerRaw
		if dispatch != "pdflatex" && dispatch != "latex" &&
			dispatch != "xelatex" && dispatch != "lualatex" &&
			dispatch != "typst" {
			dispatch = "pdflatex"
		}

		// ---- base (typst -> clsitypst; else clsitex) — the crown-jewel dispatch
		base := clsiBase()
		if dispatch == "typst" {
			base = clsiTypstBase()
		}

		// ---- forward the client's synctex params verbatim (they carry exactly
		//      file/line/column or page/h/v + editorId + clsiserverid), append the
		//      limits + required buildId. The service re-parses what it needs.
		fwd := url.Values{}
		for _, k := range []string{"file", "line", "column", "page", "h", "v",
			"editorId", "clsiserverid"} {
			if v := q.Get(k); v != "" {
				fwd.Set(k, v)
			}
		}
		fwd.Set("buildId", q.Get("buildId"))
		fwd.Set("compileBackendClass", lim.BackendClass)
		fwd.Set("compileGroup", lim.CompileGroup)
		// CE: no saas feature -> compileFromClsiCache false (matches Node, where
		// it is only true for the saas alpha/priority groups).
		fwd.Set("compileFromClsiCache", "false")

		u := base + "/project/" + pid + "/user/" + uid + "/sync/" + direction +
			"?" + fwd.Encode()
		req, err := http.NewRequest("GET", u, nil)
		if err != nil {
			res.JSON(500, []byte(internal500))
			return
		}
		req.Header.Set("Accept", "application/json")
		cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		cresp, cerr := http.DefaultClient.Do(req.WithContext(cctx))
		if cerr != nil {
			res.JSON(500, []byte(internal500))
			return
		}
		defer cresp.Body.Close()

		// clsi 404 -> web 404 (empty), per Node NotFoundError mapping.
		if cresp.StatusCode == http.StatusNotFound {
			res.SendStatus(404)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(cresp.Body, 8<<20))
		if cresp.StatusCode >= 400 {
			res.JSON(500, []byte(internal500))
			return
		}
		res.JSON(200, body)
	}
}
