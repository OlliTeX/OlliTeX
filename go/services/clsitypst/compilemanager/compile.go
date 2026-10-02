package compilemanager

import (
	"os"
	"path/filepath"

	"ollitex/go/services/clsitypst/commandrunner"
	clsl "ollitex/go/services/clsitypst/logger"
	off "ollitex/go/services/clsitypst/outputfilefinder"
	"ollitex/go/services/clsitypst/resourcewriter"

	"ollitex/go/services/clsitypst/typstrunner"
)

// --- DoCompileWithLock (Node: doCompileWithLock) ------------------------------

// DoCompileWithLock mirrors:
//
//	compileDir = getCompileDir(project_id, user_id)
//	request.isInitialCompile =
//	  (await fsPromises.mkdir(compileDir, {recursive:true})) === compileDir
//	await _prepareCompileDir(compileDir)
//	const lock = LockManager.acquire(compileDir)
//	try { return await doCompile(request, stats, timings) } finally { lock.release() }
//
// Node's finally releases on success AND error (defer covers both); a
// concurrent compile surfaces clsi/errors.AlreadyCompilingError (423 in T8).
func (m *Manager) DoCompileWithLock(req *Request, stats, timings map[string]any) (Result, error) {
	compileDir := compileDirOf(m.Paths.CompilesDir, req.ProjectID, req.UserID)
	created, err := m.MkdirAll(compileDir)
	if err != nil {
		return Result{}, err
	}
	// Node: isInitialCompile is true exactly when the mkdir CREATED the dir.
	req.IsInitialCompile = created
	m.PrepareCompileDir(compileDir)

	lock, aerr := m.Acquire(compileDir)
	if aerr != nil {
		return Result{}, aerr
	}
	defer lock.Release()

	return m.doCompile(req, stats, timings)
}

// --- doCompile (Node: doCompile) ------------------------------------------------

func (m *Manager) doCompile(req *Request, stats, timings map[string]any) (Result, error) {
	compileDir := compileDirOf(m.Paths.CompilesDir, req.ProjectID, req.UserID)
	e2eCompileStart := m.Now()

	if req.IsInitialCompile {
		stats["isInitialCompile"] = 1
		req.MetricsOpts.Compile = "initial"
		if req.CompileFromClsiCache {
			// D6 (clsi-parity, user 2026-09-28): bootstrap from the clsi
			// cache (Node typst v1 OFF; clsi.go ported it). Errors: warn +
			// continue (Node: catch { logger.warn(...) }).
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

	rr, rerr := m.ResourceSync(&resourcewriter.Request{
		ProjectID:   req.ProjectID,
		UserID:      req.UserID,
		SyncType:    req.SyncType,
		SyncState:   req.SyncState,
		Resources:   req.RWResources,
		MetricsPath: req.MetricsOpts.Path,
	}, compileDir)
	if rerr != nil {
		clsl.Error(map[string]any{
			"err": rerr.Error(), "projectId": req.ProjectID, "userId": req.UserID,
		}, "error writing resources to disk")
		return Result{}, rerr
	}
	// Node: `if (resourceList.length === 0) throw new Errors.NotFoundError
	// ('no resources provided to compile')`.
	if len(rr) == 0 {
		return Result{}, newNotFoundError("no resources provided to compile")
	}
	resourceList := rwResourcesToOff(rr)
	timings["sync"] = m.Now() - syncStart
	clsl.Debug(map[string]any{
		"projectId": req.ProjectID, "userId": req.UserID, "timeTaken": timings["sync"],
	}, "written files to disk")

	return m.runCompile(req, stats, timings, compileDir,
		compileName(req.ProjectID, req.UserID), resourceList, e2eCompileStart)
}

// rwResourcesToOff projects ResourceWriter results `{ path: r.path }` arrays.
func rwResourcesToOff(rs []resourcewriter.Resource) []off.Resource {
	out := make([]off.Resource, len(rs))
	for i, r := range rs {
		out[i] = off.Resource{Path: r.Path}
	}
	return out
}

// --- runCompile (Node: doCompile L126-177, the compile phase) -------------------

// runCompile: env = { OVERLEAF_PROJECT_ID, ...TYPST_DOCKER_ENV }; runTypst;
// catch: classify + _saveOutputFiles (error.outputFiles/buildId attach; save
// errors logged, original rethrown); success: _saveOutputFiles + timings.
func (m *Manager) runCompile(req *Request, stats, timings map[string]any,
	compileDir, name string, resourceList []off.Resource,
	e2eCompileStart int64) (Result, error) {
	// Node: env = { OVERLEAF_PROJECT_ID, ...TYPST_DOCKER_ENV } (the copied
	// dockerrunner merges Config.DockerEnv over this — caller wins).
	env := compileEnv(req.ProjectID)

	compileStart := m.Now()

	// Node: await TypstRunner.promises.runTypst(compileName, { directory,
	// mainFile, image, timeout, environment, compileGroup, stats, timings }).
	compErr := runTypstOut(m, name, compileDir, req, env, stats, timings)
	if compErr != nil {
		ce := classifyRunError(compErr)
		// Node catch: save the output files (in particular output.log) so
		// the user can read the compile errors even when the compile timed
		// out / was terminated. Save errors logged; original error throws.
		outFiles, _, bID, serr := m.saveOutputFiles(req, compileDir,
			resourceList, stats, timings)
		if serr != nil {
			clsl.Error(map[string]any{
				"err": serr.Error(), "projectId": req.ProjectID,
			}, "error saving output files on compile failure")
		} else {
			ce.OutputFiles = outFiles // return output files so user can check logs
			ce.BuildID = strPtr(bID)
		}
		timings["compile"] = m.Now() - compileStart
		clsl.Error(map[string]any{
			"err": compErr.Error(), "projectId": req.ProjectID, "userId": req.UserID,
		}, "compile failed")
		return Result{}, ce
	}

	// --- success tail (Node: L165-177)
	timings["compile"] = m.Now() - compileStart
	clsl.Debug(map[string]any{
		"projectId": req.ProjectID, "userId": req.UserID,
		"timeTaken": timings["compile"], "stats": stats,
	}, "done compile")

	outFiles, _, bID, serr := m.saveOutputFiles(req, compileDir,
		resourceList, stats, timings)
	if serr != nil {
		return Result{}, serr
	}
	timings["compileE2E"] = m.Now() - e2eCompileStart

	return Result{
		OutputFiles: outFiles,
		BuildID:     bID,
		// D9: Node returns request.baseHistoryVersion verbatim.
		BaseHistoryVersion: req.BaseHistoryVersion,
	}, nil
}

// compileEnv ports the Node env object of doCompile:
// { OVERLEAF_PROJECT_ID, ...TYPST_DOCKER_ENV } (Config.DockerEnv — the
// Settings.merge — is applied by the dockerrunner).
func compileEnv(projectID string) map[string]string {
	env := map[string]string{"OVERLEAF_PROJECT_ID": projectID}
	for k, v := range TYPST_DOCKER_ENV {
		env[k] = v
	}
	return env
}

// runTypstOut blocks on the RunTypst callback (mirrors promisify(runTypst)).
func runTypstOut(m *Manager, name, compileDir string, req *Request, env map[string]string, stats, timings map[string]any) error {
	done := make(chan error, 1)
	m.RunTypst(name, typstOpts(req, compileDir, env, stats, timings), func(err error, _ *commandrunner.RunOutput) {
		done <- err
	})
	return <-done
}

// typstOpts maps the (Node) options object onto typstrunner.Options.
func typstOpts(req *Request, compileDir string, env map[string]string, stats, timings map[string]any) typstrunner.Options {
	return typstrunner.Options{
		Directory:    compileDir,
		MainFile:     req.RootResourcePath,
		Timeout:      int64(req.Timeout),
		Image:        req.ImageName,
		Environment:  env,
		CompileGroup: req.CompileGroup,
		// Node: { ..., stats, timings } — the typst runner writes
		// stats["typst-errors"] (+ timings counters) in place.
		Stats:   stats,
		Timings: timings,
	}
}

// --- saveOutputFiles (Node: _saveOutputFiles) -----------------------------------

// metricsOptsMap re-serializes the typed MetricsOpts to the plain shape the
// OCM seam consumes.
func metricsOptsMap(req *Request) map[string]any {
	return map[string]any{
		"path":    req.MetricsOpts.Path,
		"method":  req.MetricsOpts.Method,
		"compile": req.MetricsOpts.Compile,
	}
}

// saveOutputFiles ports _saveOutputFiles: outputDir = getOutputDir(...);
// findOutputFiles(resourceList, compileDir) → {outputFiles, allEntries};
// saveOutputFiles({request, stats, timings}, raw, compileDir, outputDir) →
// {buildId, outputFiles}; timings.output = now - start.
func (m *Manager) saveOutputFiles(req *Request, compileDir string,
	resourceList []off.Resource, stats, timings map[string]any,
) (outFiles []off.OutputFile, allEntries []string, buildID string, err error) {
	start := m.Now()
	outputDir := filepath.Join(m.Paths.OutputDir, req.name())

	raw, ferr := m.FindOutputFiles(resourceList, compileDir)
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
		saveReq, raw.OutputFiles, compileDir, outputDir, statsF, timingsF)
	if serr != nil {
		return nil, nil, "", serr
	}
	mergeFloatBack(stats, statsF)
	mergeFloatBack(timings, timingsF)

	timings["output"] = m.Now() - start
	return sFiles, raw.AllEntries, bID, nil
}

// --- stats/timings bridging (clsi.go convention) --------------------------------

// anyToFloatMap projects the numeric keys of a flow map into a float64 map
// (the frozen SaveOutputFiles seam consumes the OCM's float64 surface).
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
		}
	}
	return out
}

// mergeFloatBack copies float64 values for keys absent from the flow map
// (the OCM only writes NEW numeric keys — pdf-size, ...).
func mergeFloatBack(into map[string]any, src map[string]float64) {
	for k, v := range src {
		if _, exists := into[k]; !exists {
			into[k] = v
		}
	}
}

func strPtr(s string) *string { return &s }

// --- fs seams (os-backed) -------------------------------------------------------

// mkdirAllWithIdentity mirrors fsPromises.mkdir(dir, {recursive:true}): the
// promise resolves to the PATH exactly when the directory is CREATED (Node:
// isInitialCompile === (await mkdir) === compileDir); an existing directory
// resolves without the path; mkdir-ing a path that exists as a FILE rejects
// (EEXIST) — that is the parity of the Node error surface here.
func mkdirAllWithIdentity(dir string) (bool, error) {
	if fi, err := os.Stat(dir); err == nil {
		if fi.IsDir() {
			return false, nil
		}
		return false, os.ErrExist
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	return true, nil
}
