// Package compilemanager ports services/clsi_typst/app/js/CompileManager.js
// (427L) — the typst-only compile core — onto the clsi (READ-ONLY, D22)
// generic seams plus this module's local typst runner:
//
//   - clsi/commandrunner        (generic Runner surface; wordcount runs)
//   - clsi/clsicachehandler     (generic, D6 bootstrap)
//   - clsi/config               (D23: shared CLSI_* env at clsi defaults)
//   - clsi/errors               (NotFoundError on empty resources)
//   - clsi/logger               (Debug / Warn / Error)
//   - clsi/lockmanager          (generic; AlreadyCompiling on re-acquire)
//   - clsi/outputcachemanager   (generic, D6 OCM reuse)
//   - clsi/outputfilefinder     (generic)
//   - clsi/resourcewriter       (generic)
//   - clsi_typst/config         (local, T4: CLSI_TYPST_* env surface)
//   - clsi_typst/dockerrunner   (local: typed terminated/timedout/exited)
//   - clsi_typst/typstrunner    (local, T5: ports TypstRunner.js)
//
// NO (reduced per Node clsi_typst vs clsi tex): HRW (D9: isCompileFromHistory
// is parsed but IGNORED — no history sync in v1 typst), no draft/tikz
// (TeX-only), no synctex (D3), no fdb/latexmetrics (TeX-only, D11 SKIP), no
// chktex env (replaced by TYPST_DOCKER_ENV), no image variants (TeX-only).
//
// ADDED per D6 (clsi-parity, user 2026-09-28): compileFromClsiCache bootstrap
// in doCompile (downloadLatestCompileCache on initial compile) — clsi.go
// ported it, Node typst v1 has it OFF.
//
// D16 success gate: typst compiles are `exit 0` ALWAYS (errors are rendered
// into output.log and surfaced via stats['typst-errors']); the controller's
// criterion is output.pdf size>0. timeout/terminated still surface via
// *CompileRunError (clsi classifyRunError parity).
package compilemanager

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	clsicachehandler "ollitex/go/services/clsitypst/clsicachehandler"
	"ollitex/go/services/clsitypst/commandrunner"
	clsErr "ollitex/go/services/clsitypst/errors"
	"ollitex/go/services/clsitypst/lockmanager"
	ocm "ollitex/go/services/clsitypst/outputcachemanager"
	off "ollitex/go/services/clsitypst/outputfilefinder"
	"ollitex/go/services/clsitypst/resourcewriter"

	cltypstcfg "ollitex/go/services/clsitypst/config"
	"ollitex/go/services/clsitypst/dockerrunner"
	"ollitex/go/services/clsitypst/typstrunner"
	wc "ollitex/go/services/clsitypst/wordcount"
)

// --- paths (Node: getCompileName/getCompileDir/getOutputDir) --------------------

// compileName mirrors getCompileName(): `<project>-<user>` or `<project>`.
func compileName(projectID, userID string) string {
	if userID != "" {
		return projectID + "-" + userID
	}
	return projectID
}

// compileDirOf mirrors getCompileDir(): compiles root + name.
func compileDirOf(root, projectID, userID string) string {
	return filepath.Join(root, compileName(projectID, userID))
}

// Paths mirrors the Settings.path.* reads CompileManager performs.
type Paths struct {
	CompilesDir string
	OutputDir   string
}

// --- env (Node: TYPST_DOCKER_ENV const, plan §3.4) ------------------------------

// TYPST_DOCKER_ENV (plan §3.4): HOME=/tmp because the typst image user
// (uid 33) has no home dir; the package cache lives in /tmp so the uid-33
// container can write it.
var TYPST_DOCKER_ENV = map[string]string{
	"HOME":                     "/tmp",
	"TYPST_PACKAGE_CACHE_PATH": "/tmp/.cache/typst",
}

// --- Request (Node: parsed compile request + project_id/user_id) -----------------

// MetricsOpts mirrors request.metricsOpts {path, method, compile}. doCompile
// rewrites Compile ("initial" / "recompile" / "from-clsi-cache").
type MetricsOpts struct {
	Path    string
	Method  string
	Compile string
}

// Request mirrors the Node compile request relevant to the compile path.
type Request struct {
	ProjectID string
	UserID    string

	// D9: parsed (schema shared) but IGNORED by the compile path (v1 typst
	// has no HRW); the value echoes back in Result.BaseHistoryVersion.
	IsCompileFromHistory bool
	// set by DoCompileWithLock (true when the mkdir CREATED the dir).
	IsInitialCompile bool

	MetricsOpts      MetricsOpts
	RWResources      []resourcewriter.Resource
	SyncType         string
	SyncState        string
	Compiler         string
	ImageName        string
	Timeout          int // ms; 0 => runner default 60000
	CompileGroup     string
	RootResourcePath string
	// D9: parsed (schema shared) but IGNORED by the compile path (v1
	// typst has no HRW); carried for the controller's dead-tail branch
	// (Node: request.stopOnFirstError) parity.
	StopOnFirstError bool
	// D6 (clsi-parity add over Node typst v1): bootstrap from the clsi cache.
	CompileFromClsiCache bool
	EnablePdfCaching     bool
	PdfCachingMinChunk   int64
	BuildID              *string
	// D9 echo: the controller mirrors Node's return
	// `request.baseHistoryVersion` into the success envelope.
	BaseHistoryVersion interface{}
}

func (r *Request) name() string { return compileName(r.ProjectID, r.UserID) }

// --- Result + error --------------------------------------------------------------

// Result mirrors the doCompile success value {outputFiles, buildId,
// baseHistoryVersion}.
type Result struct {
	OutputFiles []off.OutputFile
	BuildID     string
	// D9: Node returns request.baseHistoryVersion verbatim (undefined-safe
	// omission handled by the controller).
	BaseHistoryVersion interface{}
}

// CompileRunError is the Go form of the Node decorated compile error: the
// flags the controller discriminates (terminated/timedout) plus the payload
// Node attaches (error.outputFiles / error.buildId). It wraps Cause so
// errors.As still reaches the dockerrunner typed errors / clsi causes.
type CompileRunError struct {
	Cause       error
	Message     string
	Terminated  bool
	TimedOut    bool
	OutputFiles []off.OutputFile
	BuildID     *string
}

func (e *CompileRunError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return "compile error"
}

func (e *CompileRunError) Unwrap() error { return e.Cause }

// --- Save-request seam ------------------------------------------------------------

// SaveOutputReq is the (Go-trimmed) Node SaveRequest consumed by
// saveOutputFiles.
type SaveOutputReq struct {
	BuildID          string
	EnablePdfCaching bool
	MinChunkSize     int64
	MetricsOpts      map[string]any
}

// --- The Manager: one seam per Node module import ----------------------------------

// Manager is the stateless ported CompileManager. New() wires production
// (Node module import) values; tests override individual fields (clsi.go
// convention).
type Manager struct {
	// Settings (port of the Settings.* reads).
	Paths Paths

	// Settings.clsi.docker.env — merged over the per-run env (caller wins).
	DockerEnv map[string]string

	// clsi/commandrunner (generic Runner surface; wordcount runs).
	Runner commandrunner.Runner

	// clsi/lockmanager.
	Acquire func(key string) (*lockmanager.Lock, error)
	GetLock func(key string) *lockmanager.Lock

	// clsi/outputfilefinder + outputcachemanager (generic).
	FindOutputFiles func(resources []off.Resource, directory string) (off.FindResult, error)
	SaveOutputFiles func(req SaveOutputReq, rawFiles []off.OutputFile, compileDir, outputDir string,
		stats map[string]float64, timings map[string]float64) (buildID string, outFiles []off.OutputFile, err error)

	// clsi/resourcewriter (generic).
	ResourceSync func(req *resourcewriter.Request, basePath string) ([]resourcewriter.Resource, error)

	// clsi/clsicachehandler (D6 bootstrap, generic).
	DownloadLatestCompileCache func(projectID, userID, compileDir string) (bool, error)

	// clsi_typst/typstrunner (local, T5).
	RunTypst  func(compileName string, opts typstrunner.Options, cb func(err error, out *commandrunner.RunOutput))
	IsRunning func(compileName string) bool
	KillTypst func(compileName string, cb func(err error))

	// clsi_typst/wordcount (T7): the injected driver + vendored wordometer
	// (wired in New; D5 fixture-picked gopdf-line engine at module path
	// github.com/ledongthuc/pdf, see wordcount/pdfmarker.go). At-runtime
	// ok=false degrades to the wc -w fallback (plan §9).
	InjectWordometer func(compileDir, rootResourcePath string) error
	RemoveArtifacts  func(compileDir string) error
	// ports ClsiWordText.pdfLastPagesMarkerText: the 3 captured numbers
	// (TOTAL_WORDS/HEADING_WORDS/NUM_HEADINGS) or nil when absent.
	ReadPdfMarker func(pdfPath string) (total, headingWords, numHeadings int, ok bool)
	// ReadSidecar (T16, D21) locates + reads the T15 sourcemap sidecar (the
	// fork writes output.sourcemap.json next to output.pdf; productionFind
	// lands it in the generated-files build dir too). Nil seam (tests) = no
	// sidecar -> 404 arm. Production: os-backed first-existing read of
	// <compileDir>/output.sourcemap.json else
	// <outputDir>/<name>/generated-files/<buildID>/output.sourcemap.json
	// (wired in New; see readSidecarPath).
	ReadSidecar func(compileDir, name, buildID string) (sidecar string, found bool)

	// logger + fs seams (tests fake; production os-backed in New).
	Now func() int64
	// mirrors fsPromises.mkdir(compileDir, {recursive:true}): true when the
	// directory IS created (Node: promise resolves to the path only then).
	MkdirAll func(dir string) (bool, error)
	// typst-only (D8): chown(33,33) best-effort + chmod 0o777 fallback.
	PrepareCompileDir func(compileDir string)
}

// New wires production values (the Node module imports). Tests may override
// individual fields after construction.
func New(cfg *cltypstcfg.Config, tr *typstrunner.TypstRunner, runner commandrunner.Runner) *Manager {
	m := &Manager{
		Paths: Paths{
			CompilesDir: cfg.Path.CompilesDir,
			OutputDir:   cfg.Path.OutputDir,
		},
		DockerEnv:                  cfg.ClSI.Docker.Env,
		Runner:                     runner,
		Acquire:                    lockmanager.Acquire,
		GetLock:                    lockmanager.GetExistingLock,
		FindOutputFiles:            off.FindOutputFiles,
		ResourceSync:               resourcewriter.SyncResourcesToDisk,
		DownloadLatestCompileCache: clsicachehandler.DownloadLatestCompileCache,
		// T7 (wordcount) seams: the injector is wired (D22: local package).
		// ReadPdfMarker = the D5 fixture-picked engine (see below).
		InjectWordometer: wc.Inject,
		RemoveArtifacts:  wc.Remove,
		// D5 (fixture-picked 2026-09-29): the gopdf line, at its post-rename
		// module path github.com/ledongthuc/pdf (the old .../gopdf repo path is
		// dead; repo renamed -> module path is .../pdf). rsc.io/pdf v0.1.1
		// FAILED the fixture (raw CID bytes on typst subset fonts), so this
		// engine is wired and ReadPdfMarker is no longer nil.
		ReadPdfMarker:     wc.ReadMarker,
		Now:               func() int64 { return time.Now().UnixMilli() },
		MkdirAll:          func(dir string) (bool, error) { return mkdirAllWithIdentity(dir) },
		PrepareCompileDir: prepareCompileDir,
	}

	ocmM := ocm.New()
	m.SaveOutputFiles = func(req SaveOutputReq, rawFiles []off.OutputFile, compileDir, outputDir string,
		stats map[string]float64, timings map[string]float64) (string, []off.OutputFile, error) {
		result, err := ocmM.SaveOutputFiles(ocm.SaveRequest{
			BuildID:                req.BuildID,
			EnablePdfCaching:       req.EnablePdfCaching,
			PdfCachingMinChunkSize: req.MinChunkSize,
			MetricsOpts:            req.MetricsOpts,
		}, rawFiles, compileDir, outputDir, stats, timings)
		if err != nil {
			return "", nil, err
		}
		return result.BuildID, result.Files, nil
	}

	if tr != nil {
		m.RunTypst = tr.RunTypst
		m.IsRunning = tr.IsRunning
		m.KillTypst = tr.KillTypst
	}
	return m
}

// newNotFoundError ports `new Errors.NotFoundError(msg)`.
func newNotFoundError(msg string) error { return clsErr.NewNotFoundError(msg) }

// --- _prepareCompileDir (D8, Node verbatim) ------------------------------------

// prepareCompileDir: clsi runs as www-data (uid 33) — the same uid its
// containers use. clsi_typst containers use the numeric uid 33 (pandoc/typst
// has no www-data user, plan §3.8); make the dir accessible to that uid:
// chown best-effort (privileged production), fall back to world-writable
// (unprivileged local run — the P0 spike convention).
//
//	Node try { fs.chown(compileDir, 33, 33) }
//	       catch { fs.chmod(compileDir, 0o777).catch(() => {}) }
func prepareCompileDir(compileDir string) {
	if err := os.Chown(compileDir, 33, 33); err != nil {
		// unprivileged: chown EPERM -> world-writable fallback (errors
		// swallowed, matching the Node .catch(() => {})).
		_ = os.Chmod(compileDir, 0o777)
	}
}

// --- classify (clsi compilemanager classifyRunError parity) ----------------------

// classifyRunError mirrors the Node flags the controller discriminates
// (error.terminated / error.timedout): the runner's typed dockerrunner
// errors map onto *CompileRunError so errors.As still reaches the cause.
func classifyRunError(err error) *CompileRunError {
	e := &CompileRunError{Cause: err}
	if err != nil {
		e.Message = err.Error()
	}
	var terminated *dockerrunner.TerminatedError
	var timedOut *dockerrunner.TimedOutError
	switch {
	case errors.As(err, &terminated):
		e.Terminated = true // exit 137 (kill -9): Node error.terminated = true
	case errors.As(err, &timedOut):
		e.TimedOut = true
	}
	// dockerrunner.ExitedError (non-zero exit): typst compiles always exit 0
	// (D16), so it is not raised on the compile path — left unclassified.
	return e
}
