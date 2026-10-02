package compilemanager

import (
	"os"
	"testing"

	"ollitex/go/services/clsitypst/commandrunner"
	off "ollitex/go/services/clsitypst/outputfilefinder"

	cltypstcfg "ollitex/go/services/clsitypst/config"
)

func TestNewWiredClosures(t *testing.T) {
	// env so config.New populates the clsi block (D23).
	t.Setenv("SANDBOXED_COMPILES", "true")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", "/tmp/c")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_CACHE", "/tmp/nc")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_OUTPUT", "/tmp/o")
	cfg := cltypstcfg.ForTest()

	cr := &fakeRun{out: &commandrunner.RunOutput{}}
	m := New(cfg, nil, cr)

	// MkdirAll closure: os-backed create identity.
	tmp := t.TempDir()
	dir := tmp + "/cd"
	created, err := m.MkdirAll(dir)
	if err != nil || !created {
		t.Fatalf("MkdirAll create: %v %v", created, err)
	}
	created, err = m.MkdirAll(dir)
	if err != nil || created {
		t.Fatalf("MkdirAll existing: %v %v", created, err)
	}

	// PrepareCompileDir closure (D8): chown/chmod best-effort — must NOT
	// panic; the dir survives.
	m.PrepareCompileDir(dir)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatal("prepareCompileDir must not destroy the dir")
	}

	// SaveOutputFiles closure over the REAL OutputCacheManager:
	// happy (file exists) + error (source missing).
	cd, od := tmp+"/src", tmp+"/dst"
	if err := os.MkdirAll(cd, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cd+"/output.pdf", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	sz := int64(1)
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
		cd, od, map[string]float64{}, map[string]float64{}); err == nil {
		t.Fatal("expected OCM error copying a missing file")
	}
}
