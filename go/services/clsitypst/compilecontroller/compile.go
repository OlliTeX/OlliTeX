// compile.go ports the compile express handler + the error/success
// dispatch chain (Node: compile() L44-176) + the wire send.
//
// Node verbatim chain (CompileController.js L44-176): parseReq ->
// Metrics.Timer('compile-request') -> RequestParser.parse -> project_id/
// user_id assignment -> markProjectAsJustAccessed -> doCompileWithLock ->
// error dispatch (AlreadyCompiling 423 'compile-in-progress' /
// FilesOutOfSync 409 'conflict' / MissingUpdates 409 'missing-updates' /
// EPIPE|TooMany 503 'unavailable' / terminated / timedout / else 500
// 'error') -> error payload reassign (error.outputFiles || [],
// error.buildId) -> wire send res.status(code || 200).send(
// {compile: {...}}).
//
// D16 success-gate note (Node L125-129): the typst runner's catch saves
// the output files and RETHROWS, so a failed typst compile arrives as a
// non-nil error and never enters the `error == null` dispatch arm. The
// PDF-size gate (`output.pdf size > 0`) only runs with a nil error — and
// in that path output.pdf is always size>0 (typst writes one; a hard
// failure rethrew earlier)... except the Node dead tail:
// `stopped-on-first-error` / `failure` are unreachable (documented; D9).
// Node would render status undefined in that dead tail; Go mirrors the
// branch (documented divergence: Go renders 'stopped-on-first-error' /
// 'failure' — the concrete value Node's code INTENDS, unreachable either
// way).
//
// Go bridges (documented divergences):
//
//   - Node decorates error objects (.terminated/.timedout/.outputFiles/
//     .buildId); the compilemanager classifies typed dockerrunner errors
//     into *compilemanager.CompileRunError with the same flags.
//     asCompileRunError (errors.As) extracts them.
//   - Node's error.code === 'EPIPE' (docker stream close on shutdown) has
//     no Go typed error: isEPipe scans the error string (the dockerrunner
//     message carries the code).
//   - markProjectAsJustAccessed: the Go lastprojectaccess write has no
//     error case (Node's LAST_ACCESS.set could reject); the seam is a
//     func field.
package compilecontroller

import (
	"errors"
	"net/http"
	"strings"
	"time"

	clserrors "ollitex/go/services/clsitypst/errors"
	clsl "ollitex/go/services/clsitypst/logger"
	"ollitex/go/services/clsitypst/metrics"
	off "ollitex/go/services/clsitypst/outputfilefinder"
	requestparser "ollitex/go/services/clsitypst/requestparser"

	compilemanager "ollitex/go/services/clsitypst/compilemanager"
)

// CompileParams mirrors the parsed express params
// (project_id: objectId.or(submissionId), user_id: objectId optional).
type ProjectUser struct {
	ProjectID string
	UserID    string
}

// ParserConfig builds the requestparser.Config from this controller's
// config (Node: the Settings.clsi.docker.allowedImages / settings.allowed
// CompileGroups / settings.pdfCachingMinChunkSize reads).
func (c *Controller) ParserConfig() requestparser.Config {
	return requestparser.Config{
		PdfCachingMinChunkSize:  c.Config.PdfCachingMinChunk,
		AllowedImages:           c.Config.AllowedImages,
		AllowedCompileGroups:    c.Config.AllowedCompileGroups,
		AllowedCompileGroupsSet: c.Config.AllowedCompileGroupsSet,
	}
}

// Compile ports compile(req, res, next). body is the JSON-decoded compile
// request body (requestparser.Parse input). Returns the HTTP status for
// the server layer to record; (0, error) is Node's next(error) (unhandled
// error, the error middleware decides the final status).
func (c *Controller) Compile(res http.ResponseWriter, params ProjectUser,
	body map[string]any) (int, error) {
	timer := metrics.NewTimer("compile-request")

	parsed, err := requestparser.Parse(body, c.ParserConfig())
	if err != nil {
		return 0, err
	}
	req := toRequest(parsed, params.ProjectID, params.UserID)

	// Node: ProjectPersistenceManager.markProjectAsJustAccessed(project_id,
	// cb) — LAST_ACCESS.set; the Go form has no error case (documented).
	if c.MarkProjectAccessed != nil {
		c.MarkProjectAccessed(req.ProjectID, time.Now().UnixMilli())
	}

	stats := map[string]any{}
	timings := map[string]any{}
	result, cerr := c.Manager.DoCompileWithLock(req, stats, timings)
	timer.Done()
	return c.dispatchCompile(res, req, result, stats, timings, cerr)
}

// asCompileRunError extracts the classified compile error (Node: the
// decorated error object).
func asCompileRunError(err error) *compilemanager.CompileRunError {
	var ce *compilemanager.CompileRunError
	if err != nil && errors.As(err, &ce) {
		return ce
	}
	return nil
}

// isEPipe mirrors `error?.code === 'EPIPE'` (scan: the Go dockerrunner has
// no typed EPIPE error; its message carries the code).
func isEPipe(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "EPIPE")
}

// bhvOf mirrors `error.info.baseHistoryVersion` (HRW stores the info value
// as a Go int).
func bhvOf(err *clserrors.MissingUpdatesError) *int {
	if err.Info == nil {
		return nil
	}
	switch v := err.Info["baseHistoryVersion"].(type) {
	case int:
		hv := v
		return &hv
	case int64:
		i := int(v)
		return &i
	case float64:
		f := int(v)
		return &f
	}
	return nil
}

// dispatchCompile mirrors the Node dispatch chain + wire send.
func (c *Controller) dispatchCompile(res http.ResponseWriter,
	req *compilemanager.Request, result compilemanager.Result,
	stats, timings map[string]any, cerr error) (int, error) {
	cre := asCompileRunError(cerr)

	var (
		status  string
		code    int
		bhv     *int = bhvFromInterface(result.BaseHistoryVersion)
		buildID *string
		files   = result.OutputFiles
	)
	switch {
	case cerr == nil:
		// success gate (D16): output.pdf size > 0 -> 'success' + stamp.
		if hasOutputPDF(files) {
			status = "success"
			setLastSuccessfulCompile(time.Now().UnixMilli())
		} else if req.StopOnFirstError {
			// Node dead tail (documented): unreachable (D9) — the typst
			// runner rethrows before nil-error no-PDF can happen.
			status = "stopped-on-first-error"
		} else {
			status = "failure"
			clsl.Warn(map[string]any{"projectId": req.ProjectID, "outputFiles": files},
				"project failed to compile successfully, no output.pdf generated")
		}
		// Node: buildId destructured from result (always present on the
		// success tail).
		if result.BuildID != "" {
			bID := result.BuildID
			buildID = &bID
		}
	default:
		var (
			already   *clserrors.AlreadyCompilingError
			outOfSync *clserrors.FilesOutOfSyncError
			missing   *clserrors.MissingUpdatesError
			tooMany   *clserrors.TooManyCompileRequestsError
		)
		switch {
		case errors.As(cerr, &already):
			code, status = 423, "compile-in-progress"
		case errors.As(cerr, &outOfSync):
			// Http 409 Conflict
			code, status = 409, "conflict"
			clsl.Warn(map[string]any{"projectId": req.ProjectID, "userId": req.UserID},
				"files out of sync, please retry")
		case errors.As(cerr, &missing):
			// Http 409 Conflict
			code, status = 409, "missing-updates"
			bhv = bhvOf(missing)
		case isEPipe(cerr), errors.As(cerr, &tooMany):
			// docker returns EPIPE when shutting down; lockmanager's
			// concurrency cap surfaces TooManyCompileRequestsError.
			code, status = 503, "unavailable"
		case cre != nil && cre.Terminated:
			status = "terminated"
		case cre != nil && cre.TimedOut:
			status = "timedout"
			clsl.Debug(map[string]any{"err": cerr.Error(), "projectId": req.ProjectID},
				"timeout running compile")
		default:
			status, code = "error", 500
			clsl.Error(map[string]any{"err": cerr.Error(), "projectId": req.ProjectID},
				"error running compile")
		}
		// Node `if (error)`: outputFiles = error.outputFiles || [];
		// buildId = error.buildId (the decorated error payload).
		files, buildID = nil, nil
		if ce := asCompileRunError(cerr); ce != nil {
			files, buildID = ce.OutputFiles, ce.BuildID
		}
	}
	if files == nil {
		files = []off.OutputFile{}
	}

	errorRender := ""
	if cerr != nil {
		// Node: error?.message || error — an empty message renders the
		// Error object itself (JSON.stringify -> {}).
		if cerr.Error() == "" {
			errorRender = "object"
		} else {
			errorRender = cerr.Error()
		}
	}

	out := compileWire{
		status:             status,
		code:               code,
		errorRender:        errorRender,
		baseHistoryVersion: bhv,
		buildID:            buildID,
		outputFiles:        files,
		projectID:          req.ProjectID,
		userID:             req.UserID,
		downloadHost:       c.Config.DownloadHost,
		instanceType:       c.Config.InstanceType,
		zone:               c.Config.Zone,
		isSpotInstance:     c.Config.IsSpotInstance,
		outputURLPrefix:    c.Config.OutputURLPrefix,
		stats:              stats,
		timings:            timings,
	}
	_ = cre

	// Node: clsiCacheShard is always undefined in the typst baseline
	// (D3: no notify; the D6 bootstrap is compile-side) — wire.go omits
	// the key.

	return sendCompileWire(res, out)
}

// hasOutputPDF mirrors `outputFiles.some(file => file.path ===
// 'output.pdf' && file.size > 0)` (the D16 success gate).
func hasOutputPDF(files []off.OutputFile) bool {
	for _, f := range files {
		if f.Path == "output.pdf" && f.Size != nil && *f.Size > 0 {
			return true
		}
	}
	return false
}

// bhvFromInterface mirrors the Node destructure of
// result.baseHistoryVersion (the parsed baseHistoryVersion echo, D9).
func bhvFromInterface(v interface{}) *int {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case int:
		hv := x
		return &hv
	case int64:
		i := int(x)
		return &i
	case float64:
		f := int(x)
		return &f
	}
	return nil
}
