// outputFile_web.go: the ABSORBED output file routes (Node oracle
// junk/services-web/app/src/router.mjs L716/L724,
// CompileController.getFileFromClsi -> _downloadFromClsiNginx):
//
//	  GET /project/:Project_id/build/:build_id/output/:file(.+)
//	  GET /project/:Project_id/user/:user_id/build/:build_id/output/:file(.+)
//
// These are the URLs the compile response advertises in outputFiles
// (`/project/{pid}/user/{uid}/build/{bid}/output/output.pdf`) — the editor
// PDF pane fetches them, NOT the /download/... button URL. In the Node
// world the shared clsi (tex) service served the file on the common data
// volume; in the single Go deployment the typst compiler's service
// (clsitypst :3014) must serve it — closed 2026-10-05 (typst-t2 "PDF
// artifact not ready" live catch).
//
// Dispatch (same rule as the synctex proxy, synctex.go L148-151):
// project compiler typst -> clsiTypstBase, else clsiBase.
//
// Streaming parity (Route A, download.go): forward the upstream status on
// failure (404 etc. stays 404, body discarded), forward Content-Length/
// Content-Type when present, stream otherwise. Rate limiter is NOT
// ported (consistent with every other Go route in this feature — the
// miscOutputDownload limiter is a known feature-wide gap, not unique).
package compile

import (
	"context"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"ollitex/go/services/web/core"
)

var (
	outputFileProjectPat = regexp.MustCompile(`^/project/([^/]+)/build/([^/]+)/output/([^/]+)$`)
	outputFileUserPat    = regexp.MustCompile(`^/project/([^/]+)/user/([^/]+)/build/([^/]+)/output/([^/]+)$`)
)

func outputFileHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		p := cxt.Req.URL.Path
		var pid, bid, fn, uid string
		var hasUser bool
		if m := outputFileUserPat.FindStringSubmatch(p); m != nil {
			pid, uid, bid, fn, hasUser = strings.ToLower(m[1]), m[2], m[3], m[4], true
		} else if m := outputFileProjectPat.FindStringSubmatch(p); m != nil {
			pid, bid, fn = strings.ToLower(m[1]), m[2], m[3]
		} else {
			res.JSON(404, []byte(malformed404))
			return
		}
		if !buildIDRe.MatchString(bid) {
			res.JSON(404, []byte(`{"error":"Validation error: Invalid buildId at \"params.build_id\"","statusCode":404}`))
			return
		}

		// ensureUserCanReadProject (router middleware before the handler).
		proj, ok := preflight(cxt, res, a, pid)
		if !ok {
			return
		}

		// dispatch target (synctex rule): typst -> clsitypst service.
		compiler, _ := dget(*proj, "compiler").(string)
		base := clsiBase()
		if compiler == "typst" {
			base = clsiTypstBase()
		}

		up := base + "/project/" + pid
		if hasUser {
			up += "/user/" + url.PathEscape(uid)
		}
		up += "/build/" + url.PathEscape(bid) + "/output/" + url.PathEscape(fn)

		// headers BEFORE the fetch (Route A order): CT by extension; the
		// upstream CT (when present) overrides.
		h := res.W.Header()
		if ct := mime.TypeByExtension(filepath.Ext(fn)); ct != "" {
			h.Set("Content-Type", ct)
		}
		h.Set("X-Accel-Buffering", "no")

		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 120*time.Second)
		defer cancel()
		upreq, err := http.NewRequestWithContext(ctx, http.MethodGet, up, nil)
		if err != nil {
			res.SendStatus(500)
			return
		}
		upr, err := http.DefaultClient.Do(upreq)
		if err != nil {
			res.SendStatus(500)
			return
		}
		defer upr.Body.Close()
		if upr.StatusCode >= 400 {
			// upstream failure status passes through (404 stays 404,
			// body discarded — the service renders its own 404).
			io.Copy(io.Discard, io.LimitReader(upr.Body, 1<<20))
			res.W.WriteHeader(upr.StatusCode)
			return
		}
		if v := upr.Header.Get("Content-Type"); v != "" {
			res.W.Header().Set("Content-Type", v)
		}
		if v := upr.Header.Get("Content-Length"); v != "" {
			res.W.Header().Set("Content-Length", v)
		}
		res.W.WriteHeader(http.StatusOK)
		io.Copy(res.W, upr.Body)
	}
}
