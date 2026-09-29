// Package compilemanager ports services/clsi/app/js/CompileManager.js (1021L),
// split by concern (mirroring the Node one-module-per-file layout):
//
//	compilemanager.go  request/error/result types, Paths, Manager + seams, New
//	compile.go         DoCompileWithLock / DoCompile / saveOutputFiles / readFdb
//	wordcount.go       Wordcount / syncResourcesForWordcount / parseWordcount
//	synctex.go         SyncFromCode / SyncFromPdf / runSynctex
//	lifecycle.go       StopCompile / ClearProject / ClearExpiredProjects
//	metrics.go         EmitMetrics / getImageVariantSettings / rules
package compilemanager

import (
	"context"
	"path/filepath"
	"time"

	clsicachehandler "ollitex/go/services/clsitex/clsicachehandler"
	commandrunner "ollitex/go/services/clsitex/commandrunner"
	"ollitex/go/services/clsitex/config"
	"ollitex/go/services/clsitex/draftmodemanager"
	histwriter "ollitex/go/services/clsitex/historyresourcewriter"
	latexrunner "ollitex/go/services/clsitex/latexrunner"
	"ollitex/go/services/clsitex/lockmanager"
	"ollitex/go/services/clsitex/metrics"
	ocm "ollitex/go/services/clsitex/outputcachemanager"
	off "ollitex/go/services/clsitex/outputfilefinder"
	"ollitex/go/services/clsitex/png2pdf"
	"ollitex/go/services/clsitex/resourcewriter"
	"ollitex/go/services/clsitex/statsmanager"
	"ollitex/go/services/clsitex/tikzmanager"
)

// --- paths (Node: getCompileName/getCompileDir/getOutputDir) --------------------

// compileName mirrors getCompileName(): `<project>-<user>` or `<project>`.
func compileName(projectID, userID string) string {
	if userID != "" {
		return projectID + "-" + userID
	}
	return projectID
}

// compileDir mirrors getCompileDir(): settings.path.compilesDir + name.
func compileDirOf(root, projectID, userID string) string {
	return filepath.Join(root, compileName(projectID, userID))
}

// Paths mirrors the Settings.path.* reads CompileManager performs.
type Paths struct {
	CompilesDir string
	OutputDir   string
	SynctexBase string // sandboxed prefix: always '/compile'
	CacheDir    string
}

func defaultPaths() Paths {
	c := config.Get()
	return Paths{
		CompilesDir: c.Path.CompilesDir,
		OutputDir:   c.Path.OutputDir,
		SynctexBase: c.Path.SynctexBase,
		CacheDir:    c.Path.ClsiCacheDir,
	}
}

// --- Request (Node: parsed compile request + project_id/user_id) -----------------

// MetricsOpts mirrors request.metricsOpts {path, method, compile}. doCompile
// rewrites Compile ("initial" / "recompile" / "from-clsi-cache").
type MetricsOpts struct {
	Path    string
	Method  string
	Compile string
}

// Request mirrors the Node compile request object (RequestParser.parse
// output) plus project_id/user_id. HRW != nil marks isCompileFromHistory.
type Request struct {
	ProjectID            string
	UserID               string
	IsCompileFromHistory bool
	MetricsOpts          MetricsOpts
	// HRW path (isCompileFromHistory).
	HRW *histwriter.Request
	// RW path (otherwise).
	RWResources []resourcewriter.Resource

	Compiler         string
	ImageName        string
	Timeout          int // ms; 0 => LatexRunner default 60000
	Flags            []string
	StopOnFirstError bool
	Check            *string // "validate" | "error" | nil
	Draft            bool
	Png2pdf          bool
	CompileGroup     string
	SyncType         string
	SyncState        string
	RootResourcePath string

	EnableCheckpoint bool
	IsInitialCompile bool // set by DoCompileWithLock
	EnablePdfCaching bool

	EditID  *string
	BuildID *string

	CompileFromClsiCache bool
	PdfCachingMinChunk   int64
}

func (r *Request) name() string { return compileName(r.ProjectID, r.UserID) }

// --- Result + error --------------------------------------------------------------

// Result mirrors the doCompile success value {outputFiles, buildId,
// baseHistoryVersion}.
type Result struct {
	OutputFiles        []off.OutputFile
	BuildID            string
	BaseHistoryVersion *int
}

// CompileRunError is the Go form of the Node decorated compile error: the
// flags the controller discriminates (error.timedout/error.terminated/
// error.validate/error.code) plus the payload Node attaches (error.
// outputFiles/error.buildId). It wraps Cause so errors.As still reaches
// dockerrunner's typed errors and the clsi/errors causes.
type CompileRunError struct {
	Cause       error
	Message     string
	Code        *string // "EPIPE" ... (Node error.code)
	Terminated  bool
	TimedOut    bool
	Validate    *string // "pass" | "fail"
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

// --- Runner (Node: CommandRunner.promises.run) -----------------------------------

// Runner is the runner surface (commandrunner.Runner).
type Runner = commandsRunner

type commandsRunner = commandrunner.Runner

// RunOutput is the runner output surface (commandrunner.RunOutput).
type RunOutput = commandrunner.RunOutput

// runOut mirrors CommandRunner.promises.run: blocks and returns the run's
// RunOutput / error.
func runOut(r Runner, projectID string, command []string, directory, image string,
	timeout int64, env map[string]string, group string) (*commandrunner.RunOutput, error) {
	outCh := make(chan *commandrunner.RunOutput, 1)
	errCh := make(chan error, 1)
	r.Run(projectID, command, directory, image, timeout, env, group, "", func(err error, o *commandrunner.RunOutput) {
		if err != nil {
			errCh <- err
			return
		}
		outCh <- o
	})
	select {
	case o := <-outCh:
		return o, nil
	case err := <-errCh:
		return nil, err
	}
}

// --- Save-request seam ------------------------------------------------------------

// SaveOutputReq is the (Go-trimmed) Node SaveRequest consumed by saveOutputFiles.
type SaveOutputReq struct {
	BuildID          string
	EnablePdfCaching bool
	MinChunkSize     int64
	MetricsOpts      map[string]any
}

// --- The Manager: one seam per Node module import ----------------------------------

// Manager is the stateless ported CompileManager. New() wires production
// (Node module import) values; tests override individual fields. All
// function seams may be replaced after construction.
type Manager struct {
	// Settings (port of the Settings.* reads CompileManager performs).
	Paths                  Paths
	TexliveOpenoutAny      string
	TexliveMaxPrintLine    string
	SamplingPct            int      // Settings.perfLogSamplingPct
	AllowedImages          []string // Settings.clsi.docker.allowedImages
	MinChunkSize           int64    // Settings.pdfCachingMinChunkSize
	ProcessLifespanLimitMs int64    // Settings.processLifespanLimitMs
	DefaultImage           string   // Settings.clsi.docker.image (wordcount/synctex)

	// Node: Settings.clsi.docker.instanceType/zone/isSpotInstance (controller).
	InstanceType func() string
	Zone         func() string
	IsSpot       func() bool

	// Node: CommandRunner.
	Runner commandrunner.Runner

	// Node: LockManager.
	Acquire func(key string) (*lockmanager.Lock, error)
	GetLock func(key string) *lockmanager.Lock

	// Node: HistoryResourceWriter (HRW).
	HistorySync func(ctx context.Context, projectID, userID string, req *histwriter.Request,
		compileDir string, timings, stats map[string]any) (*histwriter.Result, error)
	SaveSlowPngs func(cacheKey string, slowPngs []string) error
	Png2pdfOn    func() bool

	// Node: LatexRunner.
	RunLatex  func(compileName string, opts latexrunner.Options, cb func(err error, out *commandrunner.RunOutput))
	IsRunning func(compileName string) bool
	KillLatex func(compileName string, cb func(err error))

	// Node: ResourceWriter.promises.
	ResourceSync func(req *resourcewriter.Request, basePath string) ([]resourcewriter.Resource, error)
	// Node: DraftModeManager.promises.
	DraftInject func(filename string) error
	// Node: TikzManager.promises.
	TikzCheck  func(compileDir, mainFile string, resources []resourcewriter.Resource) (bool, error)
	TikzInject func(compileDir, mainFile string) error

	// Node: CLSICacheHandler.
	DownloadLatestCompileCache func(projectID, userID, compileDir string) (bool, error)

	// DownloadOutputDotSynctex mirrors
	// ClsiCacheHandler.promises.outputDotSynctexFromCompileCache
	// (syncFromCode only; false when not downloadable).
	DownloadOutputDotSynctex func(projectID, userID, editorID, buildID, outputDir string) (bool, error)

	// Node: OutputFileFinder + OutputCacheManager.
	FindOutputFiles func(resources []off.Resource, directory string) (off.FindResult, error)
	SaveOutputFiles func(req SaveOutputReq, rawFiles []off.OutputFile, compileDir, outputDir string,
		stats map[string]float64, timings map[string]float64) (buildID string, outFiles []off.OutputFile, err error)
	QueueOnOutputDir func(dir string, fn func() (any, error)) (any, error)

	// Node: StatsManager + ClsiMetrics (Metrics) + logger.
	SampleRequest func(userID string, metricsPath string, pct int) *bool
	SkipMetrics   func(metricsPath string) bool
	Now           func() int64
	LoadAvg       func() [3]float64 // os.loadavg()

	// fs: mirror the Node fsPromises calls the port performs (tests fake
	// these; production os-backed wiring is tests-only New wiring since the
	// production New is constructed by the server layer). Mirrors
	// fsPromises.mkdir(compileDir, {recursive: true}): returns true when the
	// directory IS created (Node: promise resolves to the path), false when
	// it already exists.
	MkdirAll func(dir string) (bool, error)
}

// New wires production values (the Node module imports). Tests may override
// individual fields after construction.
func New(runner commandrunner.Runner, latex *latexrunner.LatexRunner) *Manager {
	c := config.Get()
	m := &Manager{
		Paths:                  defaultPaths(),
		Runner:                 runner,
		Png2pdfOn:              png2pdf.IsEnabled,
		MinChunkSize:           int64(c.PdfCachingMinChunkSize),
		ProcessLifespanLimitMs: c.ProcessLifespanLimitMs,
		DefaultImage:           c.ClSI.Docker.Image,
		TexliveOpenoutAny:      c.TexliveOpenoutAny,
		TexliveMaxPrintLine:    c.TexliveMaxPrintLine,
		SamplingPct:            int(c.PerfLogSamplingPct),
		AllowedImages:          c.ClSI.Docker.AllowedImages,
		Acquire:                lockmanager.Acquire,
		GetLock:                lockmanager.GetExistingLock,
		HistorySync:            histwriter.SyncResourcesToDisk,
		SaveSlowPngs:           histwriter.SaveSlowPngList,
		SampleRequest: func(userID, path string, pct int) *bool {
			return statsmanager.SampleRequest(
				statsmanager.RequestSample{UserID: userID, MetricsPath: path}, pct)
		},
		SkipMetrics:                metrics.ShouldSkipMetrics,
		Now:                        nowMS,
		LoadAvg:                    loadAvg,
		FindOutputFiles:            off.FindOutputFiles,
		ResourceSync:               resourcewriter.SyncResourcesToDisk,
		DraftInject:                draftmodemanager.InjectDraftMode,
		TikzCheck:                  tikzmanager.CheckMainFile,
		TikzInject:                 tikzmanager.InjectOutputFile,
		DownloadLatestCompileCache: clsicachehandler.DownloadLatestCompileCache,
		InstanceType:               func() string { return c.APIs.Compile.InstanceType },
		Zone:                       func() string { return c.APIs.Compile.Zone },
		IsSpot:                     func() bool { return c.APIs.Compile.IsSpotInstance },
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
	m.QueueOnOutputDir = func(dir string, fn func() (any, error)) (any, error) {
		return ocmM.QueueDirOperation[any](dir, fn)
	}
	if latex != nil {
		m.RunLatex = latex.RunLatex
		m.IsRunning = latex.IsRunning
		m.KillLatex = latex.KillLatex
	}
	return m
}

// nowMS mirrors Date.now().
func nowMS() int64 { return time.Now().UnixMilli() }

// loadAvg mirrors os.loadavg() (1/5/15 min load averages).
func loadAvg() [3]float64 { return [3]float64{} }
