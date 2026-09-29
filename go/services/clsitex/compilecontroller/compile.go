// compile.go ports the compile express handler (Node: compile()).
//
// Node verbatim chain (CompileController.js L44-196): parseReq ->
// Metrics.Timer('compile-request') -> RequestParser.parse -> project_id/
// user_id assignment -> markProjectAsJustAccessed -> doCompileWithLock ->
// error dispatch (AlreadyCompiling 423 'compile-in-progress' /
// FilesOutOfSync 409 'conflict' / MissingUpdates 409 'missing-updates' /
// EPIPE|TooMany 503 'unavailable' / terminated / validation-<v> / timedout /
// else 500 'error') -> error payload reassign (error.outputFiles || [],
// error.buildId) -> success gate (output.pdf size > 0 -> 'success' +
// timestamp; stopOnFirstError -> 'stopped-on-first-error'; else 'failure')
// + core-file error log -> clsi-cache notify (success + editorId +
// populateClsiCache only) -> timer.done() -> res.status(code || 200).send.
//
// Go bridges (documented divergences):
//
//   - Node decorates error objects (.terminated/.validate/.timedout/.
//     code/.outputFiles/.buildId); the compilemanager classifies typed
//     dockerrunner errors into *compilemanager.CompileRunError with the same
//     flags. asCompileRunError (errors.As) extracts them.
//   - Node's error.code === 'EPIPE' (docker stream close on shutdown) has no
//     Go typed error: isEPipe checks CompileRunError.Code first, then the
//     error string containing "EPIPE".
//   - markProjectAsJustAccessed: the Go lastprojectaccess write has no error
//     case (Node's LAST_ACCESS.set could reject); the seam is a func field.
package compilecontroller

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	clsicachehandler "ollitex/go/services/clsitex/clsicachehandler"
	clsl "ollitex/go/services/clsitex/logger"
	"ollitex/go/services/clsitex/metrics"
	off "ollitex/go/services/clsitex/outputfilefinder"
	"ollitex/go/services/clsitex/requestparser"

	compilemanager "ollitex/go/services/clsitex/compilemanager"
	clserrors "ollitex/go/services/clsitex/errors"
)

// CompileParams mirrors the parsed express params
// (project_id: objectId.or(submissionId), user_id: objectId optional).
type ProjectUser struct {
	ProjectID string
	UserID    string
}

// SyncQuery mirrors the shared synctex query fields (file/page/h/v/line/
// column + imageName/editorId/buildId/compileFromClsiCache; the routing
// query fields are ignored here exactly as in Node).
type SyncQuery struct {
	File                 string
	Line                 int
	Column               int
	Page                 int
	H                    int
	V                    int
	ImageName            string
	EditorID             string
	BuildID              string
	CompileFromClsiCache bool
}

// ParserConfig builds the requestparser.Config from this controller's
// config (Node: the Settings.clsi.docker.allowedImages / settings.allowed
// CompileGroups / settings.pdfCachingMinChunkSize reads).
func (c *Controller) ParserConfig() requestparser.Config {
	return requestparser.Config{
		PdfCachingMinChunkSize:  c.Config.PdfCachingMinChunk,
		AllowedImages:           c.Config.AllowedImages,
		AllowedCompileGroups:    c.Config.AllowedCompileGroups,
		AllowedCompileGroupsSet: c.Config.AllowedCompileGroups != nil,
	}
}

// compile ports compile(req, res, next). body is the JSON-decoded compile
// request body (requestparser.Parse input). Returns the HTTP status for the
// server layer to record; (0, error) is Node's next(error) (unhandled error,
// the error middleware decides the final status).
func (c *Controller) Compile(res http.ResponseWriter, params ProjectUser,
	body map[string]any) (int, error) {
	timer := metrics.NewTimer("compile-request")

	parsed, err := requestparser.Parse(body, c.ParserConfig())
	if err != nil {
		return 0, err
	}
	userID := params.UserID
	req := toRequest(parsed, params.ProjectID, userID)

	// Node: ProjectPersistenceManager.markProjectAsJustAccessed(project_id,
	// cb) — LAST_ACCESS.set; the Go form has no error case (documented).
	if c.MarkProjectAccessed != nil {
		c.MarkProjectAccessed(req.ProjectID, time.Now().UnixMilli())
	}

	stats := map[string]any{}
	timings := map[string]any{}
	result, cerr := c.Manager.DoCompileWithLock(req, stats, timings)
	timer.Done()
	return c.dispatchCompile(res, req, parsed, result, stats, timings, cerr)
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

// isEPipe mirrors `error?.code === 'EPIPE'`.
func isEPipe(err error) bool {
	if cre := asCompileRunError(err); cre != nil && cre.Code != nil {
		return *cre.Code == "EPIPE"
	}
	if err != nil {
		return strings.Contains(err.Error(), "EPIPE")
	}
	return false
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

// dispatchCompile mirrors the Node error/success dispatch chain + wire send.
func (c *Controller) dispatchCompile(res http.ResponseWriter,
	req *compilemanager.Request, parsed requestparser.ParsedResponse,
	result compilemanager.Result, stats, timings map[string]any,
	cerr error) (int, error) {
	cre := asCompileRunError(cerr)

	var (
		status  string
		code    int
		bhv     *int = result.BaseHistoryVersion
		buildID *string
		files   = result.OutputFiles
	)
	switch {
	case cerr == nil:
		// success gate + core-file log below
		if hasOutputPDF(files) {
			status = "success"
			setLastSuccessfulCompile(time.Now().UnixMilli())
		} else if req.StopOnFirstError {
			status = "stopped-on-first-error"
		} else {
			status = "failure"
			clsl.Warn(map[string]any{"projectId": req.ProjectID, "outputFiles": files},
				"project failed to compile successfully, no output.pdf generated")
		}
		if hasCoreFile(files) {
			clsl.Error(map[string]any{"projectId": req.ProjectID, "outputFiles": files},
				"core file found in output")
		}
		// Node: buildId from the result destructure (always present on the
		// success tail).
		if result.BuildID != "" {
			bID := result.BuildID
			buildID = &bID
		}
		// Node: `if (error) { outputFiles = error.outputFiles || []; ... }`
		// runs AFTER the dispatch — only for non-nil errors.
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
			code = 409
			status = "conflict"
			clsl.Warn(map[string]any{"projectId": req.ProjectID, "userId": req.UserID},
				"files out of sync, please retry")
		case errors.As(cerr, &missing):
			// Http 409 Conflict
			code, status = 409, "missing-updates"
			bhv = bhvOf(missing)
		case isEPipe(cerr), errors.As(cerr, &tooMany):
			// docker returns EPIPE when shutting down; lockmanager's
			// concurrency cap surfaces Node's TooManyCompileRequestsError.
			code, status = 503, "unavailable"
		case cre != nil && cre.Terminated:
			status = "terminated"
		case cre != nil && cre.Validate != nil:
			status = "validation-" + *cre.Validate
		case cre != nil && cre.TimedOut:
			status = "timedout"
			clsl.Debug(map[string]any{"err": cerr.Error(), "projectId": req.ProjectID},
				"timeout running compile")
		default:
			status, code = "error", 500
			clsl.Error(map[string]any{"err": cerr.Error(), "projectId": req.ProjectID},
				"error running compile")
		}
		files, buildID = nil, nil
		if cre != nil {
			// Node: outputFiles = error.outputFiles || []; buildId =
			// error.buildId.
			if ce := asCompileRunError(cerr); ce != nil {
				files, buildID = ce.OutputFiles, ce.BuildID
			}
		}
	}
	if files == nil {
		files = []off.OutputFile{}
	}

	errorRender := ""
	if cerr != nil {
		// Node: error?.message || error — an empty message renders the Error
		// object itself (JSON.stringify -> {}).
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

	// Node: clsiCacheShard notify only when status === 'success' &&
	// request.editorId && request.populateClsiCache.
	if cerr == nil && status == "success" && parsed.EditorID != nil && parsed.PopulateClsiCache {
		opts := map[string]any{
			"compiler":         req.Compiler,
			"draft":            req.Draft,
			"png2pdf":          req.Png2pdf,
			"rootResourcePath": req.RootResourcePath,
			"stopOnFirstError": req.StopOnFirstError,
		}
		if req.ImageName != "" {
			// request.imageName is the (possibly variant-rewritten) image
			// name — doCompile mutates the shared Request exactly as Node
			// mutates request.imageName.
			opts["imageName"] = filepath.Base(req.ImageName)
		}
		shard := ""
		if c.Notify != nil {
			notifyOpts := clsicachehandler.NotifyOpts{
				ProjectID:    req.ProjectID,
				UserID:       req.UserID,
				BuildID:      files[0].Build,
				EditorID:     str(parsed.EditorID),
				OutputFiles:  files,
				CompileGroup: req.CompileGroup,
				Stats:        stats,
				Timings:      timings,
				Options:      opts,
				MetricsPath:  req.MetricsOpts.Path,
			}
			shard = c.Notify(notifyOpts)
		}
		out.clsiCacheShard = shard
		out.hasShard = true
	}

	return sendCompileWire(res, out)
}

// hasOutputPDF mirrors `outputFiles.some(file => file.path === 'output.pdf'
// && file.size > 0)`.
func hasOutputPDF(files []off.OutputFile) bool {
	for _, f := range files {
		if f.Path == "output.pdf" && f.Size != nil && *f.Size > 0 {
			return true
		}
	}
	return false
}

func hasCoreFile(files []off.OutputFile) bool {
	for _, f := range files {
		if f.Path == "core" {
			return true
		}
	}
	return false
}
