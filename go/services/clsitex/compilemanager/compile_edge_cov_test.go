package compilemanager

import (
	"errors"
	"path/filepath"
	"testing"

	"ollitex/go/services/clsitex/commandrunner"
	"ollitex/go/services/clsitex/config"
	latexrunner "ollitex/go/services/clsitex/latexrunner"
	"ollitex/go/services/clsitex/lockmanager"
	off "ollitex/go/services/clsitex/outputfilefinder"
	"ollitex/go/services/clsitex/resourcewriter"
)

// --- wordcount edges -----------------------------------------------------------

func TestWordcountEdges(t *testing.T) {
	// sync success (req != nil, RW path ok) covers the `return nil` tail.
	m := newTestManager(t, t.TempDir())
	m.Runner = &fakeRunner{out: &commandrunner.RunOutput{
		Stdout: "Encoding: latin1\nWords in text: 1234\n",
	}}
	res, err := m.Wordcount("p1", "u1", "main.tex", "", makeReq())
	if err != nil {
		t.Fatalf("sync success: %v", err)
	}
	if res.Encode != "latin1" || res.TextWords != 1234 {
		t.Fatalf("results: %+v", res)
	}

	// sync acquire error propagates raw.
	m2 := newTestManager(t, t.TempDir())
	m2.Acquire = func(key string) (*lockmanager.Lock, error) {
		return nil, errors.New("acquire failed")
	}
	if _, err := m2.Wordcount("p1", "u1", "main.tex", "", makeReq()); err == nil {
		t.Fatal("expected acquire error")
	}

	// sync tagged error (ResourceSync failure, not FOS/MissingUpdates).
	m3 := newTestManager(t, t.TempDir())
	m3.ResourceSync = func(req *resourcewriter.Request, basePath string) ([]resourcewriter.Resource, error) {
		return nil, errors.New("rw failed")
	}
	if _, err := m3.Wordcount("p1", "u1", "main.tex", "", makeReq()); err == nil {
		t.Fatal("expected synced error")
	}

	// MkdirAll error is tagged.
	m4 := newTestManager(t, t.TempDir())
	m4.MkdirAll = func(dir string) (bool, error) { return false, errors.New("mkdir failed") }
	if _, err := m4.Wordcount("p1", "u1", "main.tex", "", nil); err == nil {
		t.Fatal("expected mkdir error")
	}

	// created + cache download error warns + continues.
	m5 := newTestManager(t, t.TempDir())
	m5.MkdirAll = func(dir string) (bool, error) { return true, nil }
	m5.DownloadLatestCompileCache = func(pid, uid, dir string) (bool, error) {
		return false, errors.New("cache 500")
	}
	if _, err := m5.Wordcount("p1", "u1", "main.tex", "", makeReq()); err != nil {
		t.Fatalf("download err should continue: %v", err)
	}

	// invalid image rejected.
	m6 := newTestManager(t, t.TempDir())
	if _, err := m6.Wordcount("p1", "u1", "main.tex", "evil/image:1", nil); err == nil {
		t.Fatal("expected invalid image error")
	}

	// texcount runner error is tagged.
	m7 := newTestManager(t, t.TempDir())
	m7.Runner = &fakeRunner{err: errors.New("texcount blew up")}
	if _, err := m7.Wordcount("p1", "u1", "main.tex", "", nil); err == nil {
		t.Fatal("expected runner error")
	}
}

// --- New(): production closures wired from the real module seams --------------

func TestNewWiredClosures(t *testing.T) {
	// env required by config.New (see lockmanager bootstrap).
	t.Setenv("PREEMPTIBLE", "TRUE")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", "/tmp/c")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_CACHE", "/tmp/nc")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_OUTPUT", "/tmp/o")
	config.ForTest()

	lr := latexrunner.New(&fakeRunner{}, nil, nil, nil)
	m := New(&fakeRunner{}, lr)
	if m.RunLatex == nil || m.IsRunning == nil || m.KillLatex == nil {
		t.Fatal("latex != nil branch not wired")
	}
	// SampleRequest closure: user empty -> nil; 100% -> non-nil.
	if got := m.SampleRequest("", "", 100); got != nil {
		t.Fatal("empty userId should yield nil")
	}
	if got := m.SampleRequest("u1", "", 0); got != nil {
		t.Fatal("0% sampling should yield nil")
	}
	if got := m.SampleRequest("u1", "", 100); got == nil {
		t.Fatal("100% sampling should yield non-nil")
	}

	// SaveOutputFiles closure over the REAL OutputCacheManager:
	// happy (file exists) + error (source missing).
	tmp := t.TempDir()
	cd, od := filepath.Join(tmp, "cd"), filepath.Join(tmp, "od")
	mustMkdir(t, cd)
	mustWriteFile(t, filepath.Join(cd, "output.pdf"))
	sz := int64(13)
	b, files, err := m.SaveOutputFiles(SaveOutputReq{},
		[]off.OutputFile{{Path: "output.pdf", Type: "pdf", Size: &sz}},
		cd, od, map[string]float64{}, map[string]float64{})
	if err != nil {
		t.Fatalf("OCM save: %v", err)
	}
	if b == "" || len(files) == 0 {
		t.Fatalf("OCM result: %q %d", b, len(files))
	}
	if _, _, err := m.SaveOutputFiles(SaveOutputReq{BuildID: "b-err"},
		[]off.OutputFile{{Path: "nope.pdf", Type: "pdf"}},
		cd, filepath.Join(tmp, "od2"), map[string]float64{}, map[string]float64{}); err == nil {
		t.Fatal("expected OCM error copying a missing file")
	}

	// QueueOnOutputDir closure: real OCM per-dir queue pump.
	v, qerr := m.QueueOnOutputDir(od, func() (any, error) { return "v", nil })
	if qerr != nil || v != "v" {
		t.Fatalf("queue: %v %v", qerr, v)
	}
}
