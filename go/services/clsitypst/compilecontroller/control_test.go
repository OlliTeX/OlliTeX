package compilecontroller

import (
	"bytes"
	"net/http"
	"testing"

	commandrunner "ollitex/go/services/clsitypst/commandrunner"
	"ollitex/go/services/clsitypst/lockmanager"
	off "ollitex/go/services/clsitypst/outputfilefinder"
	"ollitex/go/services/clsitypst/resourcewriter"

	compilemanager "ollitex/go/services/clsitypst/compilemanager"
	"ollitex/go/services/clsitypst/typstrunner"
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

// newTestManager builds a raw compilemanager.Manager with every typst seam
// faked; Paths point at temp trees (clsi.go newTestManager convention).
func newTestManager(t *testing.T, tmp string) *compilemanager.Manager {
	t.Helper()
	m := &compilemanager.Manager{
		Paths: compilemanager.Paths{
			CompilesDir: tmp + "/compiles",
			OutputDir:   tmp + "/output",
		},
		DockerEnv: map[string]string{"HOME": "/tmp"},
		Runner:    &ctrlFakeRunner{out: &commandrunner.RunOutput{Stdout: "ok", ExitCode: 0}},
		Now:       func() int64 { return 42 },
		MkdirAll:  func(dir string) (bool, error) { return false, nil },
		Acquire: func(key string) (*lockmanager.Lock, error) {
			return &lockmanager.Lock{Key: key}, nil
		},
		GetLock:           func(key string) *lockmanager.Lock { return nil },
		PrepareCompileDir: func(compileDir string) {},
		FindOutputFiles: func(resources []off.Resource, directory string) (off.FindResult, error) {
			sz := int64(13)
			return off.FindResult{
				OutputFiles: []off.OutputFile{{Path: "output.pdf", Type: "pdf", Size: &sz, Build: "b1"}},
				AllEntries:  []string{"output.pdf"},
			}, nil
		},
		SaveOutputFiles: func(req compilemanager.SaveOutputReq, rawFiles []off.OutputFile,
			compileDir, outputDir string, stats map[string]float64,
			timings map[string]float64) (string, []off.OutputFile, error) {
			return "b1", rawFiles, nil
		},
		ResourceSync: func(req *resourcewriter.Request, basePath string) ([]resourcewriter.Resource, error) {
			return []resourcewriter.Resource{{Path: "main.typ"}}, nil
		},
		DownloadLatestCompileCache: func(projectID, userID, compileDir string) (bool, error) {
			return false, nil
		},
		RunTypst: func(compileName string, opts typstrunner.Options, cb func(err error, out *commandrunner.RunOutput)) {
			cb(nil, &commandrunner.RunOutput{ExitCode: 0})
		},
		IsRunning:        func(name string) bool { return false },
		KillTypst:        func(name string, cb func(err error)) { cb(nil) },
		InjectWordometer: func(compileDir, rootResourcePath string) error { return nil },
		RemoveArtifacts:  func(compileDir string) error { return nil },
		ReadPdfMarker:    func(pdfPath string) (total, headingWords, numHeadings int, ok bool) { return 0, 0, 0, false },
	}
	return m
}

// newTestController builds a Controller over the faked Manager.
// MarkProjectAccessed is nil (the Compile handler tolerates nil, mirroring
// the Node module import being optional in this port); Config mirrors the
// production ControllerConfig for wire assertions.
func newTestController(t *testing.T, tmp string) *Controller {
	return &Controller{
		Manager: newTestManager(t, tmp),
		Config: ControllerConfig{
			InstanceType:            "test-instance",
			Zone:                    "zone-a",
			IsSpotInstance:          true,
			OutputURLPrefix:         "http://outputs.test",
			DownloadHost:            "http://download.test",
			AllowedImages:           []string{"pandoc/typst:latest-alpine"},
			AllowedCompileGroups:    []string{"standard"},
			AllowedCompileGroupsSet: true,
			PdfCachingMinChunk:      1024,
		},
	}
}
