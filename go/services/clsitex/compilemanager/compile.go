package compilemanager

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"strconv"

	commandrunner "ollitex/go/services/clsitex/commandrunner"
	ccmet "ollitex/go/services/clsitex/contentcachemetrics"
	dockerrunner "ollitex/go/services/clsitex/dockerrunner"
	cerrors "ollitex/go/services/clsitex/errors"
	histwriter "ollitex/go/services/clsitex/historyresourcewriter"
	"ollitex/go/services/clsitex/latexmetrics"
	"ollitex/go/services/clsitex/latexrunner"
	clsl "ollitex/go/services/clsitex/logger"
	"ollitex/go/services/clsitex/metrics"
	off "ollitex/go/services/clsitex/outputfilefinder"
	"ollitex/go/services/clsitex/resourcewriter"
	"ollitex/go/services/clsitex/safereader"
)

// --- Node error classification ------------------------------------------------
//
// Node decorates callback errors with flags (err.terminated / error.code /
// error.timedout / error.validate) and the controller inspects them. The Go
// dockerrunner port surfaces the SAME information as typed errors; classify
// maps them onto the shared CompileRunError the controller inspects.

func (m *Manager) classifyRunError(err error) *CompileRunError {
	e := &CompileRunError{Cause: err, Message: err.Error()}
	var (
		terminated *dockerrunner.TerminatedError
		exited     *dockerrunner.ExitedError
		timedOut   *dockerrunner.TimedOutError
	)
	switch {
	case errors.As(err, &terminated):
		e.Terminated = true // exit 137 (kill -9): Node err.terminated = true
	case errors.As(err, &exited):
		e.Code = strPtr(strconv.Itoa(exited.Code)) // exit 1: Node error.code = exitCode
	case errors.As(err, &timedOut):
		e.TimedOut = true
	}
	return e
}

func strPtr(s string) *string { return &s }

// --- sync wrappers over the callback seams --------------------------------------

// runLatexOut blocks on the RunLatex callback and returns (err, output).
// Mirrors Promises.runLatex (the promisified form the Node flow awaits).
func runLatexOut(m *Manager, compileName string, opts latexrunner.Options) (err error, out *commandrunner.RunOutput) {
	outCh := make(chan *commandrunner.RunOutput, 1)
	errCh := make(chan error, 1)
	m.RunLatex(compileName, opts, func(err error, o *commandrunner.RunOutput) {
		if err != nil {
			errCh <- err
			return
		}
		outCh <- o
	})
	select {
	case o := <-outCh:
		return nil, o
	case err := <-errCh:
		return err, nil
	}
}

// --- stats/timings bridging (Go-only) -------------------------------------------
//
// The compile flow passes a single map[string]any stats/timings through
// RunLatex (any) and the emit/fdb paths (any), but the frozen SaveOutputFiles
// seam consumes map[string]float64 (the OCM port's typed surface). The flow
// therefore carries the "any" maps and converts at the save boundary, merging
// the OCM's numeric writes back so doCompile can read pdf-size / compute-
// pdf-caching afterwards (Node: same object, always numeric-merge).

// anyToFloatMap projects the numeric keys of a flow map into a float64 map.
func anyToFloatMap(m map[string]any) map[string]float64 {
	out := map[string]float64{}
	for k, v := range m {
		switch n := v.(type) {
		case int:
			out[k] = float64(n)
		case int64:
			out[k] = float64(n)
		case float64:
			out[k] = n
		case float32:
			out[k] = float64(n)
		}
	}
	return out
}

// mergeFloatBack copies float64 values for keys absent from the flow map.
// The OCM only writes NEW numeric keys (pdf-size, compute-pdf-caching,
// pdf-caching-*), so merging absent keys restores them without clobbering
// the flow's int-typed entries (isInitialCompile, latexmk-errors, ...).
func mergeFloatBack(into map[string]any, src map[string]float64) {
	for k, v := range src {
		if _, exists := into[k]; !exists {
			into[k] = v
		}
	}
}

func isFilesOutOfSync(err error) bool {
	var o *cerrors.FilesOutOfSyncError
	if errors.As(err, &o) {
		return true
	}
	if oerr, ok := err.(*cerrors.OError); ok && oerr != nil {
		var fo *cerrors.FilesOutOfSyncError
		return errors.As(oerr.Cause, &fo)
	}
	return false
}

func oIsFilesOutOfSync(err error) bool {
	if oerr, ok := err.(*cerrors.OError); ok && oerr != nil {
		var fo *cerrors.FilesOutOfSyncError
		return errors.As(oerr.Cause, &fo)
	}
	var fo *cerrors.FilesOutOfSyncError
	return errors.As(err, &fo)
}

// --- isLaTeXFile (Node: request.rootResourcePath?.match(/\.tex$/i)) ----------

// texSuffixRe mirrors /\.tex$/i — the chktex env is only attached to real
// LaTeX roots (never .Rtex or friends).
var texSuffixRe = regexp.MustCompile(`(?i)\.tex$`)

func isLaTeXFile(rootResourcePath string) bool {
	return texSuffixRe.MatchString(rootResourcePath)
}

// --- doCompileWithLock (Node: doCompileWithLock) --------------------------------

// DoCompileWithLock mirrors doCompileWithLock(request, stats, timings):
//
//	const compileDir = getCompileDir(projectId, userId)
//	request.isInitialCompile =
//	  (await fsPromises.mkdir(compileDir, { recursive: true })) === compileDir
//	const lock = LockManager.acquire(compileDir)
//	try { return await doCompile(request, stats, timings) }
//	finally { lock.release() }
//
// Node's finally releases on success AND error paths; defer covers that. A
// concurrent compile surface Node's Errors.AlreadyCompilingError (423 in the
// controller) is propagated from the Acquire seam untouched.
func (m *Manager) DoCompileWithLock(req *Request, stats, timings map[string]any) (Result, error) {
	compileDir := compileDirOf(m.Paths.CompilesDir, req.ProjectID, req.UserID)
	created, err := m.MkdirAll(compileDir)
	if err != nil {
		return Result{}, err
	}
	// Node: isInitialCompile is true exactly when the mkdir CREATED the dir
	// (fs.mkdir's path-return identity check). The flow reads it for its
	// initial-compile branch (stats.isInitialCompile / metricsOpts.compile).
	req.IsInitialCompile = created

	lock, aerr := m.Acquire(compileDir)
	if aerr != nil {
		return Result{}, aerr
	}
	defer lock.Release()

	return m.doCompile(req, stats, timings)
}

// --- doCompile env (Node: 'set up environment variables for chktex') --------

// compileEnv builds the docker environment for the compile run (Node: the
// env object of doCompile): OVERLEAF_PROJECT_ID always, texlive overrides
// when set, and the CHKTEX_* block only for 'error'/'validate' checks on real
// .tex roots (never .Rtex / knitr).
func compileEnv(texliveOpenoutAny, texliveMaxPrintLine string, projectID string,
	check *string, rootResourcePath string) map[string]string {
	// Node: `let env = { OVERLEAF_PROJECT_ID: request.project_id }` first,
	// then the optional overrides, then the CHKTEX block (only when
	// request.check != null AND the root is a .tex file), then the
	// image-variant env (merged in doCompile).
	env := map[string]string{"OVERLEAF_PROJECT_ID": projectID}
	if texliveOpenoutAny != "" {
		env["openout_any"] = texliveOpenoutAny
	}
	if texliveMaxPrintLine != "" {
		env["max_print_line"] = texliveMaxPrintLine
	}
	if check != nil && isLaTeXFile(rootResourcePath) {
		// Node: CHKTEX_OPTIONS / CHKTEX_ULIMIT_OPTIONS are set for BOTH
		// 'error' and 'validate'; the *_1 flags discriminate.
		env["CHKTEX_OPTIONS"] = "-nall -e9 -e10 -w15 -w16"
		env["CHKTEX_ULIMIT_OPTIONS"] = "-t 5 -v 64000"
		if *check == "error" {
			env["CHKTEX_EXIT_ON_ERROR"] = "1"
		}
		if *check == "validate" {
			env["CHKTEX_VALIDATE"] = "1"
		}
	}
	return env
}

// --- readFdbFile (Node: _readFdbFile) ---------------------------------------

// maxFdbFileSize mirrors MAX_FDB_FILE_SIZE: the 1 MB guard on fdb-file reads
// (the file is only read for the perf metrics — never to serve it).
const maxFdbFileSize = 1024 * 1024

// readFdbFile ports _readFdbFile: SafeReader.readFile(fdbFile, MAX_FDB_FILE_SIZE,
// 'utf8') → result ?? ”. A missing fdb file is a safe no-op ("").
func (m *Manager) readFdbFile(compileDir string) (string, error) {
	content, _, err := safereader.ReadFile(filepath.Join(compileDir, "output.fdb_latexmk"), maxFdbFileSize)
	if err != nil {
		return "", err
	}
	return content, nil
}

// --- saveOutputFiles (Node: _saveOutputFiles) ---------------------------------

// metricsOptsMap re-serializes the typed MetricsOpts to the plain-object
// shape the OCM / ccmet seams consume (Node: request.metricsOpts {path,
// method, compile}).
func metricsOptsMap(req *Request) ccmet.MetricsOpts {
	return ccmet.MetricsOpts{
		"path":    req.MetricsOpts.Path,
		"method":  req.MetricsOpts.Method,
		"compile": req.MetricsOpts.Compile,
	}
}

// saveOutputFiles ports _saveOutputFiles: outputDir = getOutputDir(...);
// findOutputFiles(resourceList, compileDir) → {outputFiles, allEntries};
// OutputCacheManager.saveOutputFiles({request, stats, timings}, ...) →
// {buildId, outputFiles}; timings.output = Date.now() - start.
//
// The frozen SaveOutputFiles seam is the OCM's typed (float64-map) surface,
// so the flow's any maps are projected in with anyToFloatMap and the OCM's
// numeric writes merged back out (pdf-size, compute-pdf-caching, ...) —
// Node shares one object, Go bridges at the seam.
func (m *Manager) saveOutputFiles(req *Request, compileDir string,
	resourceList []off.Resource, stats, timings map[string]any,
) (outFiles []off.OutputFile, allEntries []string, buildID string, err error) {
	start := m.Now()
	outputDir := filepath.Join(m.Paths.OutputDir, req.name())

	result, ferr := m.FindOutputFiles(resourceList, compileDir)
	if ferr != nil {
		return nil, nil, "", ferr
	}

	saveReq := SaveOutputReq{
		EnablePdfCaching: req.EnablePdfCaching,
		MinChunkSize:     req.PdfCachingMinChunk,
		MetricsOpts:      metricsOptsMap(req),
	}
	if req.BuildID != nil {
		saveReq.BuildID = *req.BuildID
	}

	statsF := anyToFloatMap(stats)
	timingsF := anyToFloatMap(timings)
	bID, sFiles, serr := m.SaveOutputFiles(
		saveReq, result.OutputFiles, compileDir, outputDir, statsF, timingsF)
	if serr != nil {
		return nil, nil, "", serr
	}
	mergeFloatBack(stats, statsF)
	mergeFloatBack(timings, timingsF)

	timings["output"] = m.Now() - start
	return sFiles, result.AllEntries, bID, nil
}

// --- perf-log clsiRequest (Node: logger.clsiRequest serializer) -----------------

// clsiRequestAttrs builds the plain clsiRequest object the perf log serializes
// through logger.SerializeClsiRequest (allowlist + imageName basename, with the
// post-variant imageName). The logger takes map[string]any.
func clsiRequestAttrs(req *Request) map[string]interface{} {
	out := map[string]interface{}{
		"compiler":               req.Compiler,
		"compileFromClsiCache":   req.CompileFromClsiCache,
		"enablePdfCatching":      req.EnablePdfCaching,
		"pdfCachingMinChunkSize": req.PdfCachingMinChunk,
		"timeout":                req.Timeout,
		"imageName":              req.ImageName,
		"draft":                  req.Draft,
		"stopOnFirstError":       req.StopOnFirstError,
	}
	if req.Check != nil {
		out["check"] = *req.Check
	}
	out["flags"] = req.Flags
	out["compileGroup"] = req.CompileGroup
	out["syncType"] = req.SyncType
	return out
}

// --- doCompile (Node: doCompile, CompileManager.js L78-352) -------------------
//
// Go-only: Node's single try/catch funnels every failure into a catch block
// that reclassifies the error, saves outputs, conditionally clears the
// project and emits metrics, then rethrows. The Go port splits that: sync
// failures return the ORIGINAL error after the tag-log is recorded
// (failSync: Go logs instead of Node discarding OError.tag's result), and the
// compile phase returns a classified *CompileRunError via runCompile's catch
// tail. The controller inspects *CompileRunError flags the same way Node
// inspects the decorated error (timedout/terminated/validate/code/
// outputFiles/buildId).

func (m *Manager) doCompile(req *Request, stats, timings map[string]any) (Result, error) {
	compileDir := compileDirOf(m.Paths.CompilesDir, req.ProjectID, req.UserID)
	e2eCompileStart := m.Now()

	if req.IsInitialCompile {
		stats["isInitialCompile"] = 1
		req.MetricsOpts.Compile = "initial"
		if req.CompileFromClsiCache {
			// Node: try { if (await downloadLatestCompileCache(...)) {...} }
			// catch (err) { logger.warn(...) }
			restored, derr := m.DownloadLatestCompileCache(req.ProjectID, req.UserID, compileDir)
			if restored {
				stats["restoredClsiCache"] = 1
				req.MetricsOpts.Compile = "from-clsi-cache"
			}
			if derr != nil {
				clsl.Warn(map[string]any{
					"err": derr.Error(), "projectId": req.ProjectID, "userId": req.UserID,
				}, "failed to populate compile dir from cache")
			}
		}
	} else {
		req.MetricsOpts.Compile = "recompile"
	}

	syncStart := m.Now()
	clsl.Debug(map[string]any{"projectId": req.ProjectID, "userId": req.UserID},
		"syncing resources to disk")

	// Node: `let resourceList, baseHistoryVersion` — both only set on the
	// HRW path (the destructure); undefined on the RW path.
	var resourceList []off.Resource
	var baseHistoryVersion *int
	if req.IsCompileFromHistory {
		hres, herr := m.HistorySync(context.Background(), req.ProjectID, req.UserID,
			req.HRW, compileDir, timings, stats)
		if herr != nil {
			return Result{}, m.failSync(req, herr)
		}
		resourceList = hrwResourcesToOff(hres.ResourceList)
		hv := hres.BaseHistoryVersion
		baseHistoryVersion = &hv
	} else {
		rr, rerr := m.ResourceSync(&resourcewriter.Request{
			ProjectID:   req.ProjectID,
			UserID:      req.UserID,
			SyncType:    req.SyncType,
			SyncState:   req.SyncState,
			Resources:   req.RWResources,
			MetricsPath: req.MetricsOpts.Path,
		}, compileDir)
		if rerr != nil {
			return Result{}, m.failSync(req, rerr)
		}
		resourceList = rwResourcesToOff(rr)
		// draft/tikz share Node's single catch (failSync) — any sync-phase
		// error is tagged+thrown before the compile starts.
		if req.Draft {
			if derr := m.DraftInject(filepath.Join(compileDir, req.RootResourcePath)); derr != nil {
				return Result{}, m.failSync(req, derr)
			}
		}
		needsMain, terr := m.TikzCheck(compileDir, req.RootResourcePath, rr)
		if terr != nil {
			return Result{}, m.failSync(req, terr)
		}
		if needsMain {
			if ierr := m.TikzInject(compileDir, req.RootResourcePath); ierr != nil {
				return Result{}, m.failSync(req, ierr)
			}
		}
	}

	timings["sync"] = m.Now() - syncStart
	clsl.Debug(map[string]any{
		"projectId": req.ProjectID, "userId": req.UserID, "timeTaken": timings["sync"],
	}, "written files to disk")

	return m.runCompile(req, stats, timings, compileDir,
		compileName(req.ProjectID, req.UserID), resourceList, baseHistoryVersion,
		e2eCompileStart)
}

// failSync ports the sync catch:
//
//	if (error instanceof FilesOutOfSyncError) OError.tag(error, 'files out of sync, please retry', info)
//	else                                     OError.tag(error, 'error writing resources to disk', info)
//	throw error
//
// Go divergence (documented): OError.tag's tagged error is DISCARDED in Node
// (the original throws), so Go logs the tag message and returns the
// ORIGINAL error untouched. The controller re-maps FilesOutOfSyncError → 409
// 'conflict' and MissingUpdatesError → 409 + baseHistoryVersion.
func (m *Manager) failSync(req *Request, err error) error {
	info := map[string]any{"projectId": req.ProjectID, "userId": req.UserID}
	if isFilesOutOfSync(err) {
		clsl.Error(info, "files out of sync, please retry")
	} else {
		clsl.Error(info, "error writing resources to disk")
	}
	return err
}

// --- resource conversion (Node: `{ path: r.path }` arrays) --------------------

// hrwResourcesToOff projects HRW syncResourcesToDisk's result resourceList
// (paths only) onto the off.Resource shape saveOutputFiles consumes.
func hrwResourcesToOff(rs []histwriter.Resource) []off.Resource {
	out := make([]off.Resource, len(rs))
	for i, r := range rs {
		out[i] = off.Resource{Path: r.Path}
	}
	return out
}

// rwResourcesToOff projects ResourceWriter prom results the same way.
func rwResourcesToOff(rs []resourcewriter.Resource) []off.Resource {
	out := make([]off.Resource, len(rs))
	for i, r := range rs {
		out[i] = off.Resource{Path: r.Path}
	}
	return out
}

// --- runCompile (Node: doCompile L209-352, the compile phase) -----------------
//
// doCompile's tail (env, image variant, runLatex, catch, success) is split
// into runCompile so both the error classification and the controller's
// mapping stay in one place. Node's catch reclassifies then rethrows:
//
//	const compileStart = Date.now()
//	const compileName = getCompileName(...)
//	const recordPerformanceMetrics = StatsManager.sampleRequest(...)
//	enableLatexMkMetrics(stats)
//	try { await runLatex(...); if (request.check === 'validate') throw <validate-error> }
//	catch (originalError) { classify; _saveOutputFiles; clearProjectWithListing?
//	  _emitMetrics(status); throw error }
//	<success tail>

// recordPerfSample ports StatsManager.sampleRequest(request, sampling).
// The seam takes the request's (userId, metricsPath); nil => not sampled.
func (m *Manager) recordPerfSample(req *Request) bool {
	return m.SampleRequest(req.UserID, req.MetricsOpts.Path, m.SamplingPct) != nil
}

// pdfStatsSeam builds the ccmet seam bundle: prometheus bridges to the
// metrics package, sys load from the same provider nowMS/loadAvg wire.
func (m *Manager) pdfStatsSeam() ccmet.SeamMetrics {
	return ccmet.SeamMetrics{
		Summary: func(name string, value float64, opts ccmet.MetricsOpts) {
			metrics.GenericObserve(name, value)
		},
		Timing: func(name string, value float64, sampleRate float64, opts ccmet.MetricsOpts) {
			metrics.GenericObserve(name, value)
		},
		Inc: func(name string, value float64, opts ccmet.MetricsOpts) {
			metrics.GenericInc(name, int(value))
		},
		LogWarn: clsl.Warn,
		SysLoad: m.LoadAvg,
	}
}

func (m *Manager) runCompile(req *Request, stats, timings map[string]any,
	compileDir, compileName string, resourceList []off.Resource,
	baseHistoryVersion *int, e2eCompileStart int64) (Result, error) {
	// env = { OVERLEAF_PROJECT_ID, ...texlive overrides, ...chktex block }
	env := compileEnv(m.TexliveOpenoutAny, m.TexliveMaxPrintLine, req.ProjectID,
		req.Check, req.RootResourcePath)

	// the requested compile options may need a variant of the requested
	// image, which brings its own environment
	ivs := m.getImageVariantSettings(req)
	req.ImageName = ivs.ImageName
	for k, v := range ivs.Env {
		env[k] = v
	}

	compileStart := m.Now()
	recordPerformanceMetrics := m.recordPerfSample(req)
	// Define a `latexmk` property on the stats object to collect latexmk
	// -time stats.
	latexmetrics.EnableLatexMkMetrics(stats)

	// Node: await runLatex(...) then, for check 'validate', throw a fresh
	// validation error (.validate = 'pass') into the same catch. In Go the
	// classified error is synthesized after the call and the remaps below
	// mirror the catch's reclassification.
	err, _ := runLatexOut(m, compileName, latexrunner.Options{
		Directory:        compileDir,
		MainFile:         req.RootResourcePath,
		Compiler:         req.Compiler,
		Timeout:          int64(req.Timeout),
		Image:            ivs.ImageName,
		Flags:            req.Flags,
		Environment:      env,
		CompileGroup:     req.CompileGroup,
		StopOnFirstError: req.StopOnFirstError,
		Stats:            stats,
		Timings:          timings,
	})

	var ce *CompileRunError
	if err != nil {
		ce = m.classifyRunError(err)
		// request was for validation only: Node re-creates the error as
		// new Error('validation') with
		// error.validate = originalError.code ? 'fail' : 'pass'. (Go keeps
		// Cause so errors.As still reaches the dockerrunner typed error —
		// documented divergence from Node's fresh Error.)
		if req.Check != nil && *req.Check == "validate" && ce.Validate == nil {
			val := "pass"
			if ce.Code != nil {
				val = "fail" // chktex exit 1: failed-on-validation
			}
			ce = &CompileRunError{Cause: ce.Cause, Message: "validation", Validate: strPtr(val)}
		}
		// request was for compile, and failed on validation (chktex exit 1)
		if req.Check != nil && *req.Check == "error" && ce.Message == "exited" {
			ce = &CompileRunError{Cause: ce.Cause, Message: "compilation", Validate: strPtr("fail")}
		}
	} else if req.Check != nil && *req.Check == "validate" {
		// Node: the catch's validationError (.validate = 'pass').
		ce = &CompileRunError{Message: "validation", Validate: strPtr("pass")}
	}

	if ce != nil {
		// error/outputFiles/allEntries/buildId attach + conditional clear +
		// emit (Node catch L254-291). saveOutputFiles errors propagate
		// through the classified error (Node: the catch's own promise
		// rejection overrides).
		outFiles, allEntries, bID, serr := m.saveOutputFiles(req, compileDir,
			resourceList, stats, timings)
		if serr != nil {
			return Result{}, serr
		}
		ce.OutputFiles = outFiles // return output files so user can check logs
		ce.BuildID = strPtr(bID)
		// Clear project if this compile was abruptly terminated
		if ce.Terminated || ce.TimedOut {
			if cerr := m.ClearProjectWithListing(req.ProjectID, req.UserID, allEntries); cerr != nil {
				return Result{}, cerr
			}
		}
		if !m.SkipMetrics(req.MetricsOpts.Path) {
			status := "failure"
			switch {
			case ce.TimedOut:
				status = "timeout"
			case ce.Terminated:
				status = "terminated"
			}
			timings["compile"] = m.Now() - compileStart
			m.emitMetrics(req, status, stats, timings)
		}
		return Result{}, ce
	}

	// --- success tail (Node: L293-352)
	timings["compile"] = m.Now() - compileStart

	// Record the PNGs this compile flagged as "slow" so the next sync can
	// convert them to PDFs. (Node: gated isCompileFromHistory && Png2Pdf;
	// save errors warn + continue.)
	if req.IsCompileFromHistory && m.Png2pdfOn() {
		var slowPngs []string
		if lm, ok := stats["latexmk"].(map[string]any); ok {
			if sp, ok := lm["latexmk-png-slow"].([]string); ok {
				slowPngs = sp
			}
		}
		if serr := m.SaveSlowPngs(filepath.Base(compileDir), slowPngs); serr != nil {
			clsl.Warn(map[string]any{
				"err": serr.Error(), "projectId": req.ProjectID, "userId": req.UserID,
			}, "failed to save png2pdf slow-png list")
		}
	}

	clsl.Debug(map[string]any{
		"projectId": req.ProjectID, "userId": req.UserID,
		"timeTaken": timings["compile"], "stats": stats, "timings": timings,
	}, "done compile")

	outFiles, _, bID, serr := m.saveOutputFiles(req, compileDir, resourceList, stats, timings)
	if serr != nil {
		return Result{}, serr
	}
	timings["compileE2E"] = m.Now() - e2eCompileStart

	// Node: stats['latexmk-errors'] truthy => 'error' (the flag is int 0/1;
	// missing => 0 => 'success').
	status := "success"
	if ce2 := statsHasLatexmkErrors(stats); ce2 {
		status = "error"
	}
	m.emitMetrics(req, status, stats, timings)

	if f64(stats["pdf-size"]) != 0 && !m.SkipMetrics(req.MetricsOpts.Path) {
		ccmet.EmitPdfStats(stats, timings, m.pdfStatsSeam(), metricsOptsMap(req))
	}

	// Record compile performance for a subset of users
	if recordPerformanceMetrics {
		// Add fdb metrics if available (Node: error => warn + skip)
		if fdbContent, ferr := m.readFdbFile(compileDir); ferr != nil {
			clsl.Warn(map[string]any{
				"err": ferr.Error(), "projectId": req.ProjectID, "userId": req.UserID,
			}, "error reading fdb file for performance metrics")
		} else if fdbContent != "" {
			latexmetrics.AddLatexFdbMetrics(fdbContent, stats)
		}

		loadavg := m.LoadAvg()
		clsl.Info(map[string]any{
			"userId": req.UserID, "projectId": req.ProjectID,
			"timeTaken":   timings["compile"],
			"clsiRequest": clsl.SerializeClsiRequest(clsiRequestAttrs(req)),
			"stats":       stats, "timings": timings,
			// explicitly include latexmk stats to bypass the non-enumerable
			// property
			"latexmk":   stats["latexmk"],
			"loadavg1m": loadavg[0], "loadavg5m": loadavg[1], "loadavg15m": loadavg[2],
			"samplingPercentage": m.SamplingPct,
		}, "sampled performance log")
	}

	return Result{
		OutputFiles:        outFiles,
		BuildID:            bID,
		BaseHistoryVersion: baseHistoryVersion,
	}, nil
}

// statsHasLatexmkErrors mirrors the Node truthiness on stats['latexmk-errors']
// (latexrunner writes int 0/1; anything else is false).
func statsHasLatexmkErrors(stats map[string]any) bool {
	return f64(stats["latexmk-errors"]) != 0
}
