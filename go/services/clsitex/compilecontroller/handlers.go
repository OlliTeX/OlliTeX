// handlers.go ports the stopCompile / clearCache / syncFromCode / syncFromPdf
// / wordcount / wordcountWithSync / status express handlers + the module
// export timeSinceLastSuccessfulCompile (consumed by the load agent) and the
// production constructor.
//
// Node: each handler is `parseReq -> CompileManager.<method> -> res`.
// Errors go to next(error) (Go: return (0, error)); the express error
// middleware (app.js) then maps NotFound 404 / InvalidParameter 400 /
// EPIPE 503 / else 500 — that mapping lives in the server layer here, so
// the controller returns the unhandled error for it.
package compilecontroller

import (
	"errors"
	"net/http"
	"time"

	clsicachehandler "ollitex/go/services/clsitex/clsicachehandler"
	"ollitex/go/services/clsitex/lastprojectaccess"
	"ollitex/go/services/clsitex/requestparser"

	compilemanager "ollitex/go/services/clsitex/compilemanager"
	clserrors "ollitex/go/services/clsitex/errors"
	clsl "ollitex/go/services/clsitex/logger"
)

// StopCompile ports stopCompile: stopCompile(projectId, userId) -> 204.
func (c *Controller) StopCompile(res http.ResponseWriter, params ProjectUser) (int, error) {
	if err := c.Manager.StopCompile(params.ProjectID, params.UserID); err != nil {
		return 0, err
	}
	_, err := sendPlain(res, http.StatusNoContent, "")
	return http.StatusNoContent, err
}

// ClearCache ports clearCache: stopCompile -> clearProject -> 204 (OError.tag
// 'stop compile' / 'clear project' on error — Go: cerrors.Tag mirrors it).
func (c *Controller) ClearCache(res http.ResponseWriter, params ProjectUser) (int, error) {
	if err := c.Manager.StopCompile(params.ProjectID, params.UserID); err != nil {
		return 0, tagErr(err, "stop compile")
	}
	// Node: ProjectPersistenceManager.clearProject (the full chain). The
	// Go seam is nil in tests (falls back to the Manager-local clear);
	// the apps layer wires projectpersistence.ClearProject.
	if c.ClearProject == nil {
		err := c.Manager.ClearProject(params.ProjectID, params.UserID)
		if err != nil {
			clsl.Error(map[string]any{"err": err, "projectId": params.ProjectID,
				"userId": params.UserID}, "clear project failed")
			return 0, tagErr(err, "clear project")
		}
	} else if err := c.ClearProject(params.ProjectID, params.UserID); err != nil {
		// log the CAUSE too: OError.Tag renders Message-only, so without
		// this the real failure stays invisible (owner clear-cache 500,
		// 2026-10-06).
		clsl.Error(map[string]any{"err": err, "projectId": params.ProjectID,
			"userId": params.UserID}, "clear project failed")
		return 0, tagErr(err, "clear project")
	}
	_, err := sendPlain(res, http.StatusNoContent, "")
	return http.StatusNoContent, err
}

// SyncFromCode ports syncFromCode: manager.SyncFromCode -> {pdf, downloadedFromCache}.
func (c *Controller) SyncFromCode(res http.ResponseWriter,
	params ProjectUser, q SyncQuery) (int, error) {
	result, err := c.Manager.SyncFromCode(params.ProjectID, params.UserID, q.File,
		q.Line, q.Column, c.syncOpts(params, q))
	if err != nil {
		return 0, err
	}
	return writeJSON(res, http.StatusOK, map[string]any{
		"pdf":                 result.CodePositions,
		"downloadedFromCache": result.DownloadedFromCache,
	})
}

// SyncFromPdf ports syncFromPdf: manager.SyncFromPdf -> {code, downloadedFromCache}.
func (c *Controller) SyncFromPdf(res http.ResponseWriter,
	params ProjectUser, q SyncQuery) (int, error) {
	result, err := c.Manager.SyncFromPdf(params.ProjectID, params.UserID, q.Page,
		q.H, q.V, c.syncOpts(params, q))
	if err != nil {
		return 0, err
	}
	return writeJSON(res, http.StatusOK, map[string]any{
		"code":                result.PdfPositions,
		"downloadedFromCache": result.DownloadedFromCache,
	})
}

// BuildFile is GET .../build/{build_id}/output/{file} (both mounts) — the
// shared-cls file-serving surface (Node clsi: the absorbed output URLs the
// compile response advertises; apps/server.go routes). Delegated to the
// ported manager method (compilemanager/outputfile.go).
func (c *Controller) BuildFile(res http.ResponseWriter, r *http.Request,
	params ProjectUser, buildID, fn string) (int, error) {
	return c.Manager.ServeBuildFile(res, r, params.ProjectID, params.UserID, buildID, fn)
}

func (c *Controller) syncOpts(params ProjectUser, q SyncQuery) compilemanager.SyncOpts {
	return compilemanager.SyncOpts{
		ImageName:            q.ImageName,
		EditorID:             q.EditorID,
		BuildID:              q.BuildID,
		CompileFromClsiCache: q.CompileFromClsiCache,
	}
}

// Wordcount ports wordcount: manager.Wordcount(req=nil) -> {texcount}.
// texcount reads the compile dir as-is (no sync — Node passes null).
func (c *Controller) Wordcount(res http.ResponseWriter,
	params ProjectUser, file, image string) (int, error) {
	result, err := c.Manager.Wordcount(params.ProjectID, params.UserID, file, image, nil)
	if err != nil {
		return 0, err
	}
	return writeJSON(res, http.StatusOK, map[string]any{"texcount": result})
}

// WordcountWithSync ports wordcountWithSync: parse the compile body, mark
// the project accessed, then wordcount WITH the (HRW/RW) sync. Missing
// updates -> 409 {baseHistoryVersion}; AlreadyCompiling -> 423 plain
// 'compile in progress'; TooMany -> 503 plain 'too many concurrent requests';
// other errors -> next(error).
func (c *Controller) WordcountWithSync(res http.ResponseWriter,
	params ProjectUser, file, image string, body map[string]any) (int, error) {
	parsed, err := requestparser.Parse(body, c.ParserConfig())
	if err != nil {
		return 0, err
	}
	req := toRequest(parsed, params.ProjectID, params.UserID)
	if c.MarkProjectAccessed != nil {
		c.MarkProjectAccessed(req.ProjectID, nowMilli())
	}
	result, cerr := c.Manager.Wordcount(req.ProjectID, req.UserID, file, image, req)
	if cerr != nil {
		if isMissingUpdatesWithBHV(cerr) {
			return writeJSON(res, 409, map[string]any{
				"baseHistoryVersion": missingUpdatesBHV(cerr),
			})
		}
		if asAlreadyCompiling(cerr) != nil {
			// Http 423 Locked
			_, err = sendPlain(res, 423, "compile in progress")
			return 423, err
		}
		if asTooMany(cerr) != nil {
			_, err = sendPlain(res, 503, "too many concurrent requests")
			return 503, err
		}
		return 0, cerr
	}
	return writeJSON(res, http.StatusOK, map[string]any{"texcount": result})
}

// Status ports status: res.send('OK').
func (c *Controller) Status(res http.ResponseWriter) (int, error) {
	_, err := sendPlain(res, http.StatusOK, "OK")
	return http.StatusOK, err
}

// TimeSinceLastSuccessfulCompile ports the module export
// timeSinceLastSuccessfulCompile() (ms since the last successful compile;
// consumed by the load agent).
func TimeSinceLastSuccessfulCompile() int64 {
	return nowMilli() - lastSuccessful()
}

// New wires the production seams (Node: the module imports). cfg may
// carry the production ProjectPersistenceManager instance (Node: the
// module import; the Go apps layer injects it to avoid an import edge
// compilecontroller -> projectpersistence).
func New(m *compilemanager.Manager, cfg ControllerConfig) *Controller {
	c := &Controller{
		Manager:             m,
		Notify:              clsicachehandler.Notify,
		MarkProjectAccessed: lastprojectaccess.SetLastProjectAccessTime,
		Config:              cfg,
	}
	if cfg.ClearProject != nil {
		c.ClearProject = cfg.ClearProject
	}
	return c
}

// --- error extraction helpers (Node: error instanceof Errors.X) ----------------

func asAlreadyCompiling(err error) *clserrors.AlreadyCompilingError {
	var e *clserrors.AlreadyCompilingError
	if errors.As(err, &e) {
		return e
	}
	return nil
}

func asTooMany(err error) *clserrors.TooManyCompileRequestsError {
	var e *clserrors.TooManyCompileRequestsError
	if errors.As(err, &e) {
		return e
	}
	return nil
}

func asMissingUpdates(err error) *clserrors.MissingUpdatesError {
	var e *clserrors.MissingUpdatesError
	if errors.As(err, &e) {
		return e
	}
	return nil
}

func isMissingUpdatesWithBHV(err error) bool { return asMissingUpdates(err) != nil }

func missingUpdatesBHV(err error) int {
	e := asMissingUpdates(err)
	if e == nil || e.Info == nil {
		return 0
	}
	if v, ok := e.Info["baseHistoryVersion"].(int); ok {
		return v
	}
	return 0
}

// tagErr mirrors OError.tag(error, message) (Node: the tag is attached to the
// error carried into next).
func tagErr(err error, message string) error {
	return clserrors.Tag(err, message)
}

func nowMilli() int64 { return time.Now().UnixMilli() }

func lastSuccessful() int64 {
	lastSuccessfulMu.Lock()
	defer lastSuccessfulMu.Unlock()
	return lastSuccessfulCompileTimestamp
}
