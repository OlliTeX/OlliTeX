package compilecontroller

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	commandrunner "ollitex/go/services/clsitex/commandrunner"
	histwriter "ollitex/go/services/clsitex/historyresourcewriter"
	latexrunner "ollitex/go/services/clsitex/latexrunner"
	"ollitex/go/services/clsitex/lockmanager"
	off "ollitex/go/services/clsitex/outputfilefinder"
	"ollitex/go/services/clsitex/resourcewriter"

	compilemanager "ollitex/go/services/clsitex/compilemanager"
)

// ctrlTestRecorder is a minimal http.ResponseWriter for the wire assertions.
type ctrlTestRecorder struct {
	header http.Header
	status int
	body   *bytes.Buffer
}

func (w *ctrlTestRecorder) Header() http.Header { return w.header }
func (w *ctrlTestRecorder) WriteHeader(c int)   { w.status = c }
func (w *ctrlTestRecorder) Write(b []byte) (int, error) {
	return w.body.Write(b)
}

func newCtrlRecorder() *ctrlTestRecorder {
	return &ctrlTestRecorder{header: http.Header{}, body: &bytes.Buffer{}}
}

// ctrlFakeRunner implements commandrunner.Runner with fixed (err, out).
type ctrlFakeRunner struct {
	err error
	out *commandrunner.RunOutput
}

func (r *ctrlFakeRunner) Run(projectID string, command []string, directory, image string,
	timeout int64, environment map[string]string, compileGroup string, cwd string,
	callback func(err error, out *commandrunner.RunOutput)) string {
	if r.err != nil {
		callback(r.err, nil)
	} else {
		callback(nil, r.out)
	}
	return ""
}
func (r *ctrlFakeRunner) Kill(containerID string, callback func(err error)) {}
func (r *ctrlFakeRunner) CanRunSyncTeXInOutputDir() bool                    { return false }

// newTestManager builds a raw Manager with every module seam faked; Paths
// point at temp trees. (Mirrors the compilemanager seams_test helper.)
func newTestManager(t *testing.T, tmp string) *compilemanager.Manager {
	t.Helper()
	m := &compilemanager.Manager{
		Paths: compilemanager.Paths{
			CompilesDir: tmp + "/compiles",
			OutputDir:   tmp + "/output",
			SynctexBase: "/compile",
		},
		DefaultImage:  "texlive/texlive:latest-full",
		AllowedImages: []string{"texlive/texlive:2024"},
		Runner:        &ctrlFakeRunner{out: &commandrunner.RunOutput{Stdout: "ok", ExitCode: 0}},
		Now:           func() int64 { return 42 },
		LoadAvg:       func() [3]float64 { return [3]float64{1, 2, 3} },
		SaveSlowPngs:  func(cacheKey string, slowPngs []string) error { return nil },
		Png2pdfOn:     func() bool { return false },
		SampleRequest: func(userID, path string, pct int) *bool { return nil },
		SkipMetrics:   func(path string) bool { return false },
		MkdirAll:      func(dir string) (bool, error) { return false, nil },
		Acquire: func(key string) (*lockmanager.Lock, error) {
			return &lockmanager.Lock{}, nil
		},
		GetLock:     func(key string) *lockmanager.Lock { return nil },
		IsRunning:   func(name string) bool { return false },
		KillLatex:   func(name string, cb func(err error)) { cb(nil) },
		DraftInject: func(filename string) error { return nil },
		TikzCheck: func(compileDir, mainFile string, resources []resourcewriter.Resource) (bool, error) {
			return false, nil
		},
		TikzInject: func(compileDir, mainFile string) error { return nil },
		FindOutputFiles: func(resources []off.Resource, directory string) (off.FindResult, error) {
			sz := int64(13)
			return off.FindResult{
				OutputFiles: []off.OutputFile{{Path: "output.pdf", Type: "pdf", Size: &sz, Build: "b1"}},
				AllEntries:  []string{"output.pdf"},
			}, nil
		},
		SaveOutputFiles: func(req compilemanager.SaveOutputReq, rawFiles []off.OutputFile, compileDir, outputDir string,
			stats map[string]float64, timings map[string]float64) (string, []off.OutputFile, error) {
			return "b1", rawFiles, nil
		},
		QueueOnOutputDir: func(dir string, fn func() (any, error)) (any, error) { return fn() },
		DownloadLatestCompileCache: func(projectID, userID, compileDir string) (bool, error) {
			return false, nil
		},
		DownloadOutputDotSynctex: func(projectID, userID, editorID, buildID, outputDir string) (bool, error) {
			return false, nil
		},
	}
	m.ResourceSync = func(req *resourcewriter.Request, basePath string) ([]resourcewriter.Resource, error) {
		return []resourcewriter.Resource{{Path: "main.tex"}}, nil
	}
	m.HistorySync = func(ctx context.Context, projectID, userID string, req *histwriter.Request,
		compileDir string, timings, stats map[string]any) (*histwriter.Result, error) {
		if req == nil {
			return nil, &compilemanager.CompileRunError{Message: "no hrw"}
		}
		return &histwriter.Result{BaseHistoryVersion: 9, ResourceList: []histwriter.Resource{{Path: "main.tex"}}}, nil
	}
	m.RunLatex = func(name string, opts latexrunner.Options, cb func(err error,
		out *commandrunner.RunOutput)) {
		cb(nil, &commandrunner.RunOutput{Stdout: "compile ok"})
	}
	return m
}

// newTestController builds a Controller over the faked Manager. Notify /
// MarkProjectAccessed are nil by default (handlers tolerate nil).
func newTestController(t *testing.T, tmp string) *Controller {
	return &Controller{
		Manager: newTestManager(t, tmp),
		Config: ControllerConfig{
			InstanceType:    "test-instance",
			Zone:            "zone-a",
			IsSpotInstance:  true,
			OutputURLPrefix: "http://outputs.test",
			DownloadHost:    "http://download.test",
			AllowedImages:   []string{"texlive/texlive:2024"},
		},
	}
}
