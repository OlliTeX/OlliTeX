// P5.2b — web compile OUTPUT READ routes (Node→Go cutover, WEB_GO_PLAN.md
// P5.2b). Node oracle (live, 2026-09-16, /tmp/p52b/oracle.json + probes):
//
//	GET /download/project/:Project_id/build/:build_id/output/output.pdf
//	  (CompileController.downloadPdf — the download-PDF button; the PDF
//	   preview pane is the nginx→clsi fast path and does NOT touch the
//	   web app — overleaf.conf proxies /project/…/output.output.* to
//	   clsi-nginx :8080)
//	    anon            → 302 /login (or 401 "Unauthorized" when
//	                      Accept: application/json — requireGlobalLogin,
//	                      router.mjs:215, P4-pinned in core)
//	    non-member      → 403 (accept-dependent: user/restricted HTML /
//	                      {"message":"restricted"} — ensureUserCanReadProject)
//	    bad project id  → 404 JSON invalid-oid / 404 HTML general/404
//	                      (absent) — BEFORE param validation (preflight)
//	    bad build_id    → 404 JSON {"error":"Validation error: Invalid
//	                      buildId at \"params.build_id\"","statusCode":404}
//	    bad clsiserverid→ 400 JSON …Invalid clsiServerId at
//	                      \"query.clsiserverid\" (QUERY errors 400,
//	                      PARAM errors 404 — parseReq, node-pinned)
//	    bad editorId    → 400 JSON …Invalid UUID at \"query.editorId\"
//	    build missing   → 404, CT application/pdf + Content-Disposition
//	                      (inline|attachment) set BEFORE the proxy, empty
//	                      body, no Content-Length (Node: res.status(404)
//	                      .end() in _proxyToClsi's error branch)
//	    success         → 200, CT application/pdf (clsi's if it differs),
//	                      CL from clsi, Content-Disposition
//	                      inline; filename="SafeName.pdf" (attachment
//	                      when ?popupDownload), body streamed from
//	                      downloadHost (:8080 = clsi-nginx);
//	                      X-Accel-Buffering: no (consumed by the front
//	                      nginx — invisible client-side)
//
//	GET /project/:Project_id/output/cached/output.overleaf.json
//	  (ClsiCacheController.getLatestBuildFromCache — clsi-cache DISABLED
//	   in this stack (CLSI_CACHE_INSTANCES=[]) → Node answers 404
//	   text/plain "Not Found" (res.sendStatus(404)) deterministically)
//
//	GET /download/project/:Project_id/build/:editorBuildId/output/cached/:filename
//	  (ClsiCacheController.downloadFromCache — filename allow-list
//	   validated FIRST: disallowed → 404 JSON
//	   …Path is not allowed at \"params.filename\"; valid name + cache
//	   disabled → 404, NO Content-Type, empty body (res.status(404).end))
//
// Safe-name parity: Node `_getSafeProjectName` =
//
//	name.replace(/[^\p{L}\p{Nd}]/gu, '_')  → "P52b Oracle" → P52b_Oracle
package compile

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"

	"ollitex/go/services/web/core"
)

// --- pinned validation shapes (parseReq, node-pinned 2026-09-16) ---------

var (
	buildIDRe       = regexp.MustCompile(`^[0-9a-f]+-[0-9a-f]+$`)
	editorBuildIDRe = regexp.MustCompile(`^[a-f0-9-]{36}-[0-9a-f]+-[0-9a-f]+$`)
	clsiServerIDRe  = regexp.MustCompile(`^[a-z0-9-]+$`)
)

// clsiCacheAllowed — ClsiCacheHandler.isAllowedFilename (keep in sync).
func clsiCacheAllowed(fn string) bool {
	switch fn {
	case "output.blg", "output.log", "output.pdf", "output.synctex.gz",
		"output.overleaf.json", "output.tar.gz":
		return true
	}
	return strings.HasSuffix(fn, ".blg")
}

// safeProjectName — Node `name.replace(/[^\p{L}\p{Nd}]/gu, '_')`:
// keep Unicode letters + decimal digits, replace everything else.
func safeProjectName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if unicode.Is(unicode.L, r) || unicode.Is(unicode.Nd, r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// clsiDownloadHost — Settings.apis.clsi.downloadHost:
//
//	CLSI_LB_IP/CLSI_LB_HOST → http://<v>:80  else  http://<DOWNLOAD_HOST|127.0.0.1>:8080
func clsiDownloadHost() string {
	if v := envOr("CLSI_LB_IP", os.Getenv("CLSI_LB_HOST")); v != "" {
		return "http://" + v + ":80"
	}
	return "http://" + envOr("DOWNLOAD_HOST", "127.0.0.1") + ":8080"
}

// isUUID — z.uuid() (Node accepts hex uuids).
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if r != '-' {
				return false
			}
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// --- Route A: GET /download/project/:pid/build/:bid/output/output.pdf ----

var pdfDownloadPattern = regexp.MustCompile(`^/download/project/([^/]+)/build/([^/]+)/output/output\.pdf$`)

func pdfDownloadHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		m := pdfDownloadPattern.FindStringSubmatch(cxt.Req.URL.Path)
		if m == nil {
			res.JSON(404, []byte(malformed404))
			return
		}
		pid, bid := strings.ToLower(m[1]), m[2]

		// ensureUserCanReadProject FIRST (router middleware order —
		// invalid-oid 404 JSON / absent 404 HTML / non-member 403 BEFORE
		// any param/query validation; node-pinned via P5.2a preflight).
		p, ok := preflight(cxt, res, a, pid)
		if !ok {
			return
		}

		// downloadPdf param validation (params → 404):
		if !buildIDRe.MatchString(bid) {
			res.JSON(404, []byte(`{"error":"Validation error: Invalid buildId at \"params.build_id\"","statusCode":404}`))
			return
		}
		// query validation (query → 400), schema order clsiserverid → editorId:
		q := cxt.Req.URL.Query()
		if csid := q.Get("clsiserverid"); csid != "" && !clsiServerIDRe.MatchString(csid) {
			res.JSON(400, []byte(`{"error":"Validation error: Invalid clsiServerId at \"query.clsiserverid\"","statusCode":400}`))
			return
		}
		if eid := q.Get("editorId"); eid != "" && !isUUID(eid) {
			res.JSON(400, []byte(`{"error":"Validation error: Invalid UUID at \"query.editorId\"","statusCode":400}`))
			return
		}

		// Project name for the attachment filename (Node fetches {name:1}).
		name, _ := dget(*p, "name").(string)
		filename := safeProjectName(name) + ".pdf"
		h := res.W.Header()
		h.Set("Content-Type", "application/pdf")
		if q.Get("popupDownload") != "" {
			h.Set("Content-Disposition", `attachment; filename="`+filename+`"`)
		} else {
			h.Set("Content-Disposition", `inline; filename="`+filename+`"`)
		}
		h.Set("X-Accel-Buffering", "no")

		// clsi URL (ClsiURLHelpers.getFilePath): /project/<pid>[/user/<uid>]
		// /build/<bid>/output/output.pdf  — userId = session user (per-user
		// compiles ON in this stack).
		uid := cxt.Sess.UserIDHex()
		up := clsiDownloadHost() + "/project/" + pid
		if uid != "" {
			up += "/user/" + uid
		}
		up += "/build/" + url.PathEscape(bid) + "/output/output.pdf"
		if csid := q.Get("clsiserverid"); csid != "" {
			up += "?clsiserverid=" + url.QueryEscape(csid)
		}

		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 60*time.Second)
		defer cancel()
		upreq, _ := http.NewRequestWithContext(ctx, http.MethodGet, up, nil)
		upr, err := pdfProxyClient.Do(upreq)
		if err != nil {
			// Node: non-RequestFailed error before streaming → 500 empty.
			res.W.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer upr.Body.Close()
		if upr.StatusCode >= 400 {
			// Node _proxyToClsi: !streamingStarted && RequestFailedError →
			// res.status(upstream).end() — the controller's CT/CD headers
			// stay, body EMPTY, no Content-Length (node-pinned).
			_, _ = io.Copy(io.Discard, io.LimitReader(upr.Body, 1<<20))
			res.W.WriteHeader(upr.StatusCode)
			return
		}
		// Node forwards exactly Content-Length + Content-Type when present.
		if v := upr.Header.Get("Content-Length"); v != "" {
			res.W.Header().Set("Content-Length", v)
		}
		if v := upr.Header.Get("Content-Type"); v != "" {
			res.W.Header().Set("Content-Type", v)
		}
		res.W.WriteHeader(upr.StatusCode)
		_, _ = io.Copy(res.W, upr.Body)
	}
}

var pdfProxyClient = &http.Client{}

// --- Route B: GET /project/:pid/output/cached/output.overleaf.json -------

var cachedJSONPattern = regexp.MustCompile(`^/project/([^/]+)/output/cached/output\.overleaf\.json$`)

func cachedBuildJSONHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		m := cachedJSONPattern.FindStringSubmatch(cxt.Req.URL.Path)
		if m == nil {
			res.JSON(404, []byte(malformed404))
			return
		}
		if _, ok := preflight(cxt, res, a, strings.ToLower(m[1])); !ok {
			return
		}
		// ClsiCacheManager.getLatestCompileResult with zero clsi-cache
		// instances → NotFoundError → res.sendStatus(404) (node-pinned:
		// text/plain "Not Found").
		res.SendStatus(404)
	}
}

// --- Route C: GET /download/project/:pid/build/:editorBuildId/output/cached/:filename

var cachedFilePattern = regexp.MustCompile(`^/download/project/([^/]+)/build/([^/]+)/output/cached/([^/]+)$`)

func cachedFileHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		m := cachedFilePattern.FindStringSubmatch(cxt.Req.URL.Path)
		if m == nil {
			res.JSON(404, []byte(malformed404))
			return
		}
		pid, editorBuildID, filename := strings.ToLower(m[1]), m[2], m[3]
		if _, ok := preflight(cxt, res, a, pid); !ok {
			return
		}
		// downloadFromCache param validation (schema order: Project_id,
		// editorBuildId, filename) — all 404s (node-pinned):
		if !editorBuildIDRe.MatchString(editorBuildID) {
			res.JSON(404, []byte(`{"error":"Validation error: Invalid editorId-buildId at \"params.editorBuildId\"","statusCode":404}`))
			return
		}
		if !clsiCacheAllowed(filename) {
			res.JSON(404, []byte(`{"error":"Validation error: Path is not allowed at \"params.filename\"","statusCode":404}`))
			return
		}
		// ClsiCacheHandler.getOutputFile with zero instances →
		// NotFoundError → res.status(404).end() (node-pinned: 404, NO
		// Content-Type, empty body).
		res.W.WriteHeader(404)
	}
}
