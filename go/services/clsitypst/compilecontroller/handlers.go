// handlers.go ports the stopCompile / clearCache / wordcount / status
// express handlers (Node: CompileController.js L178-256).
//
// Node: each handler is `parseReq -> CompileManager.<method> -> res`.
// Errors go to next(error) (Go: return (0, error)); the express error
// middleware (app.js, T10) then maps NotFoundError 404 / InvalidParameter
// 400 / EPIPE 503 / else error.statusCode || 500. The wordcount handler is a
// PLAIN next(error) passthrough (Node clsi_typst has no wordcount-lock /
// typed 409-423 pre-dispatch, unlike the clsi tex WordcountWithSync).
package compilecontroller

import (
	"net/http"

	clserrors "ollitex/go/services/clsitypst/errors"
	"ollitex/go/services/clsitypst/requestparser"

	compilemanager "ollitex/go/services/clsitypst/compilemanager"
)

// tagErr mirrors OError.tag(error, message) (Node: the tag is attached to
// the error carried into next; the error type chain is preserved).
func tagErr(err error, message string) error {
	return clserrors.Tag(err, message)
}

// SyncQuery mirrors the clsi synctex query object (file/line/column and
// page/h/v + imageName/editorId/buildId/compileFromClsiCache; the last four
// are accepted-but-ignored beyond buildId routing, as in Node). The server
// layer (apps) parses the query string into it (coerceInt, clsi.go
// convention: ""/"NaN" -> 0).
type SyncQuery struct {
	File                     string
	Line, Column, Page, H, V int
	ImageName                string
	EditorID                 string
	BuildID                  string
	CompileFromClsiCache     bool
}

// StopCompile ports stopCompile: stopCompile(projectId, userId) -> 204.
func (c *Controller) StopCompile(res http.ResponseWriter, params ProjectUser) (int, error) {
	if err := c.Manager.StopCompile(params.ProjectID, params.UserID); err != nil {
		return 0, err
	}
	_, err := sendPlain(res, http.StatusNoContent, "")
	return http.StatusNoContent, err
}

// ClearCache ports clearCache: stopCompile -> PPM.clearProject -> 204
// (OError.tag 'stop compile' / 'clear project' on error — Go: cerrors.Tag).
func (c *Controller) ClearCache(res http.ResponseWriter, params ProjectUser) (int, error) {
	if err := c.Manager.StopCompile(params.ProjectID, params.UserID); err != nil {
		return 0, tagErr(err, "stop compile")
	}
	clear := c.ClearProject
	if clear == nil {
		clear = c.Manager.ClearProject
	}
	if err := clear(params.ProjectID, params.UserID); err != nil {
		return 0, tagErr(err, "clear project")
	}
	_, err := sendPlain(res, http.StatusNoContent, "")
	return http.StatusNoContent, err
}

// Wordcount ports wordcount (both GET and POST — the Go form is one method;
// the server layer routes both). file/image are the query params (Node:
// zz.filepath().default('main.typ') and optional string); body is the
// compile request body on POST (nil for GET: the plain count on the last
// synced sources). Returns (status, error): (0, error) is next(error).
func (c *Controller) Wordcount(res http.ResponseWriter,
	params ProjectUser, file, image string, body map[string]any) (int, error) {
	// Node: `if (req.body != null) { request = parse(req.body); ... }`.
	var req *compilemanager.Request
	if body != nil {
		parsed, err := requestparser.Parse(body, c.ParserConfig())
		if err != nil {
			return 0, err
		}
		req = toRequest(parsed, params.ProjectID, params.UserID)
	}
	result, err := c.Manager.Wordcount(
		params.ProjectID, params.UserID, file, image, req)
	if err != nil {
		// Node: catch (error) { next(error) } — plain passthrough.
		return 0, err
	}
	return writeJSON(res, http.StatusOK, map[string]any{"texcount": result})
}

// SyncFromCode (D21) ports the clsi syncFromCode wire: (file, line, column)
// -> view records {page, h, v, width, height}. The manager reads the T15
// sidecar and matches (downloadedFromCache always false — the v1 sidecar is
// local, no clsi-cache bootstrap).
func (c *Controller) SyncFromCode(res http.ResponseWriter, params ProjectUser, q SyncQuery) (int, error) {
	opts := compilemanager.SyncOpts{
		ImageName:            q.ImageName,
		EditorID:             q.EditorID,
		BuildID:              q.BuildID,
		CompileFromClsiCache: q.CompileFromClsiCache,
	}
	result, err := c.Manager.SyncFromCode(params.ProjectID, params.UserID, q.File,
		q.Line, q.Column, opts)
	if err != nil {
		return 0, err
	}
	return writeJSON(res, http.StatusOK, map[string]any{
		"pdf":                 result.CodePositions,
		"downloadedFromCache": result.DownloadedFromCache,
	})
}

// BuildFile (M1 parity 2026-10-05) streams one output artifact of a build
// (the shared-cls file-serving surface that had no Go counterpart until
// now — typst-t2 "PDF artifact not ready"); see compilemanager/outputfile.
func (c *Controller) BuildFile(res http.ResponseWriter, r *http.Request,
	params ProjectUser, buildID, filename string) (int, error) {
	return c.Manager.ServeBuildFile(res, r, params.ProjectID, params.UserID, buildID, filename)
}

// SyncFromPdf (D21) ports the clsi syncFromPdf wire: (page, h, v) -> edit
// record {file, line, column}.
func (c *Controller) SyncFromPdf(res http.ResponseWriter, params ProjectUser, q SyncQuery) (int, error) {
	opts := compilemanager.SyncOpts{
		ImageName:            q.ImageName,
		EditorID:             q.EditorID,
		BuildID:              q.BuildID,
		CompileFromClsiCache: q.CompileFromClsiCache,
	}
	result, err := c.Manager.SyncFromPdf(params.ProjectID, params.UserID, q.Page, q.H, q.V, opts)
	if err != nil {
		return 0, err
	}
	return writeJSON(res, http.StatusOK, map[string]any{
		"code":                result.PdfPositions,
		"downloadedFromCache": result.DownloadedFromCache,
	})
}

// Status ports status: res.send('OK').
func (c *Controller) Status(res http.ResponseWriter) (int, error) {
	_, err := sendPlain(res, http.StatusOK, "OK")
	return http.StatusOK, err
}
