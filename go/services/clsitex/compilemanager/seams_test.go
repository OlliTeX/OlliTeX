package compilemanager

import (
	"context"
	"path/filepath"
	"testing"

	commandrunner "ollitex/go/services/clsitex/commandrunner"
	histwriter "ollitex/go/services/clsitex/historyresourcewriter"
	latexrunner "ollitex/go/services/clsitex/latexrunner"
	"ollitex/go/services/clsitex/lockmanager"
	off "ollitex/go/services/clsitex/outputfilefinder"
	"ollitex/go/services/clsitex/resourcewriter"
)

type fakeRunner struct {
	project string
	command []string
	dir     string
	image   string
	timeout int64
	env     map[string]string
	group   string
	err     error
	out     *commandrunner.RunOutput
	ran     bool
	killErr error
	killRan bool
	synctex bool
}

func (r *fakeRunner) Run(projectID string, command []string, directory string, image string,
	timeout int64, environment map[string]string, compileGroup string, cwd string,
	callback func(err error, out *commandrunner.RunOutput)) string {
	r.project, r.command, r.dir, r.image, r.timeout, r.env, r.group =
		projectID, command, directory, image, timeout, environment, compileGroup
	r.ran = true
	if r.err != nil {
		callback(r.err, nil)
	} else {
		callback(nil, r.out)
	}
	return "container-name"
}

func (r *fakeRunner) Kill(containerID string, callback func(err error)) {
	r.killRan = true
	callback(r.killErr)
}

func (r *fakeRunner) CanRunSyncTeXInOutputDir() bool { return r.synctex }

// nowVal is the flow's fixed wall clock (Node: Date.now() calls).
var nowVal int64 = 42

// makeReq builds a minimal compile request (RW path: no HRW, root main.tex).
func makeReq() *Request {
	return &Request{
		ProjectID: "p1", UserID: "u1",
		Compiler:         "pdflatex",
		RootResourcePath: "main.tex",
		SyncType:         "full",
		MetricsOpts:      MetricsOpts{Path: "/render", Method: "POST"},
	}
}

// makeHRWReq builds a history-path request (isCompileFromHistory + HRW).
func makeHRWReq() *Request {
	req := makeReq()
	req.IsCompileFromHistory = true
	req.HRW = &histwriter.Request{BaseHistoryVersion: 3, RootResourcePath: "main.tex"}
	req.RWResources = []resourcewriter.Resource{{Path: "main.tex"}}
	return req
}

// newTestManager builds a Manager with every module seam faked. Paths point
// at a temp tree; tests override individual seams for the branch under test.
func newTestManager(t *testing.T, tmp string) *Manager {
	t.Helper()
	m := &Manager{
		Paths: Paths{
			CompilesDir: filepath.Join(tmp, "compiles"),
			OutputDir:   filepath.Join(tmp, "output"),
			SynctexBase: "/compile",
		},
		DefaultImage:  "texlive/texlive:latest-full",
		AllowedImages: []string{"texlive/texlive:2024"},
		Runner:        &fakeRunner{out: &commandrunner.RunOutput{Stdout: "ok", ExitCode: 0}},
		Now:           func() int64 { return nowVal },
		LoadAvg:       func() [3]float64 { return [3]float64{1, 2, 3} },
		SaveSlowPngs:  func(cacheKey string, slowPngs []string) error { return nil },
		Png2pdfOn:     func() bool { return false },
		SampleRequest: func(userID, path string, pct int) *bool { return nil },
		SkipMetrics:   func(path string) bool { return false },
		MkdirAll:      func(dir string) (bool, error) { return false, nil },
		Acquire: func(key string) (*lockmanager.Lock, error) {
			return &lockmanager.Lock{}, nil
		},
		GetLock:   func(key string) *lockmanager.Lock { return nil },
		IsRunning: func(name string) bool { return false },
		KillLatex: func(name string, cb func(err error)) { cb(nil) },
		FindOutputFiles: func(resources []off.Resource, directory string) (off.FindResult, error) {
			sz := int64(13)
			return off.FindResult{
				OutputFiles: []off.OutputFile{{Path: "output.pdf", Type: "pdf", Size: &sz, Build: "b1"}},
				AllEntries:  []string{"output.pdf"},
			}, nil
		},
		SaveOutputFiles: func(req SaveOutputReq, rawFiles []off.OutputFile, compileDir, outputDir string,
			stats map[string]float64, timings map[string]float64) (string, []off.OutputFile, error) {
			stats["pdf-size"] = 13
			timings["output"] = 1
			return "b1", rawFiles, nil
		},
	}
	m.ResourceSync = func(req *resourcewriter.Request, basePath string) ([]resourcewriter.Resource, error) {
		return []resourcewriter.Resource{{Path: "main.tex"}}, nil
	}
	m.HistorySync = func(ctx context.Context, projectID, userID string,
		req *histwriter.Request, compileDir string, timings, stats map[string]any) (*histwriter.Result, error) {
		return &histwriter.Result{
			BaseHistoryVersion: 9,
			ResourceList:       []histwriter.Resource{{Path: "main.tex"}},
		}, nil
	}
	m.RunLatex = func(name string, opts latexrunner.Options, cb func(err error,
		out *commandrunner.RunOutput)) {
		cb(nil, &commandrunner.RunOutput{Stdout: "compile ok"})
	}
	m.DownloadLatestCompileCache = func(pid, uid, dir string) (bool, error) { return false, nil }
	m.QueueOnOutputDir = func(dir string, fn func() (any, error)) (any, error) { return fn() }
	m.DownloadOutputDotSynctex = func(pid, uid, editor, build, dir string) (bool, error) {
		return false, nil
	}
	m.DraftInject = func(filename string) error { return nil }
	m.TikzCheck = func(compileDir, mainFile string, resources []resourcewriter.Resource) (bool, error) {
		return false, nil
	}
	m.TikzInject = func(compileDir, mainFile string) error { return nil }
	return m
}
