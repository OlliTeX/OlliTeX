package compilemanager

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ollitex/go/services/clsitypst/commandrunner"
	clsErr "ollitex/go/services/clsitypst/errors"
	"ollitex/go/services/clsitypst/lockmanager"
	"ollitex/go/services/clsitypst/resourcewriter"
)

// --- StopCompile (Node: stopCompile) --------------------------------------------

func TestStopCompileIdle(t *testing.T) {
	m := newTestManager(t, nil)
	killed := false
	m.KillTypst = func(name string, cb func(err error)) { killed = true; cb(nil) }
	// no lock, not running -> early return, no kill.
	if err := m.StopCompile("p1", "u1"); err != nil {
		t.Fatalf("idle: %v", err)
	}
	if killed {
		t.Fatal("must not kill when idle")
	}
}

func TestStopCompileRunningNoLock(t *testing.T) {
	m := newTestManager(t, nil)
	m.IsRunning = func(name string) bool { return true }
	killed := false
	m.KillTypst = func(name string, cb func(err error)) {
		if name != "p1-u1" {
			t.Fatalf("wrong name: %q", name)
		}
		killed = true
		cb(nil)
	}
	if err := m.StopCompile("p1", "u1"); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if !killed {
		t.Fatal("must kill a running compile without lock")
	}
}

func TestStopCompileKillError(t *testing.T) {
	m := newTestManager(t, nil)
	m.IsRunning = func(name string) bool { return true }
	m.KillTypst = func(name string, cb func(err error)) { cb(errors.New("kill boom")) }
	if err := m.StopCompile("p1", "u1"); err == nil {
		t.Fatal("expected kill error")
	}
}

func TestStopCompileWaitForLock(t *testing.T) {
	m := newTestManager(t, nil)
	lk := &lockmanager.Lock{Key: "typst-p1-u1"}
	m.GetLock = func(key string) *lockmanager.Lock { return lk }
	m.IsRunning = func(name string) bool { return false }
	go func() {
		time.Sleep(20 * time.Millisecond)
		lk.Release()
	}()
	if err := m.StopCompile("p1", "u1"); err != nil {
		t.Fatalf("wait: %v", err)
	}
}

// --- ClearProject (Node: clearProject) --------------------------------------------

func TestClearProject(t *testing.T) {
	m := newTestManager(t, nil)
	dir := compileDirOf(m.Paths.CompilesDir, "p1", "u1")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.typ"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// present dir is removed
	if err := m.ClearProject("p1", "u1"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if exists(m.Paths.CompilesDir+"/p1-u1", "p1-u1") {
		t.Fatal("compile dir must be removed")
	}
	// missing dir: fs.rm({force}) parity -> nil.
	if err := m.ClearProject("p1", "u1"); err != nil {
		t.Fatalf("missing: %v", err)
	}
}

// exists is a small helper (kept unexported, test-only).
func exists(p, _ string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// --- ClearExpiredProjects (Node: clearExpiredProjects) ------------------------------

func TestClearExpiredProjects(t *testing.T) {
	m := newTestManager(t, nil)
	// age both dirs: one OLD, one FRESH (via mtime).
	oldDir := filepath.Join(m.Paths.CompilesDir, "old-p")
	freshDir := filepath.Join(m.Paths.CompilesDir, "fresh-p")
	filesDir := filepath.Join(m.Paths.CompilesDir, "afile")
	for _, d := range []string{oldDir, freshDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filesDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-24 * time.Hour)
	// the entries to be listed
	_ = old

	// mark old + fresh via mtime
	if err := os.Chtimes(oldDir, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(freshDir, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	// age relative to m.Now() (real wall clock): 1s budget.
	if err := m.ClearExpiredProjects(1000); err != nil {
		t.Fatalf("clear: %v", err)
	}
	// old removed, fresh kept; a FILE entry is stat-ed (IsDir false) and
	// skipped by age-check (the Go walk lists files too; Node fs.readdir on
	// a POSIX dir yields both — the age check on a file still uses mtime,
	// so a file older than the budget would be RemoveAll'd. Here the file
	// is fresh, so it stays).
	if exists(oldDir, "old-p") {
		t.Fatal("old dir must be removed")
	}
	if !exists(freshDir, "fresh-p") {
		t.Fatal("fresh dir must be kept")
	}
	// missing compiles root: readdir errors -> [] (Node .catch(() => [])).
	m2 := newTestManager(t, nil)
	m2.Paths.CompilesDir = m2.Paths.CompilesDir + "/missing"
	if err := m2.ClearExpiredProjects(1000); err != nil {
		t.Fatalf("missing root: %v", err)
	}
}

// --- Wordcount (Node: wordcount) ----------------------------------------------------

func writeRoot(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestWordcountRootMissing(t *testing.T) {
	m := newTestManager(t, nil)
	_, err := m.Wordcount("p1", "u1", "main.typ", "img", nil)
	var nf *clsErr.NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("expected NotFound, got %v", err)
	}
	if want := "no compiled state to word count: main.typ not synced"; nf.Message != want {
		t.Fatalf("message: %q want %q", nf.Message, want)
	}
}

func TestWordcountHappy(t *testing.T) {
	m := newTestManager(t, nil)
	cd := compileDirOf(m.Paths.CompilesDir, "p1", "u1")
	writeRoot(t, cd, "main.typ")

	var injected, removed bool
	m.InjectWordometer = func(dir, root string) error {
		injected = true
		if root != "main.typ" {
			t.Fatalf("root: %q", root)
		}
		return nil
	}
	m.RemoveArtifacts = func(dir string) error { removed = true; return nil }
	m.ReadPdfMarker = func(pdf string) (int, int, int, bool) {
		if pdf != filepath.Join(cd, WCOutPDF) {
			t.Fatalf("pdf: %q", pdf)
		}
		return 120, 10, 3, true
	}
	res, err := m.Wordcount("p1", "u1", "main.typ", "img", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.TextWords != 120 || res.HeadWords != 10 || res.Headers != 3 {
		t.Fatalf("results: %+v", res)
	}
	// finally: artifacts removed even on success.
	if !injected || !removed {
		t.Fatal("wordometer artifacts must be injected + removed")
	}
}

func TestWordcountMarkerAbsentFallsBack(t *testing.T) {
	m := newTestManager(t, nil)
	cd := compileDirOf(m.Paths.CompilesDir, "p1", "u1")
	writeRoot(t, cd, "main.typ")
	m.InjectWordometer = func(dir, root string) error { return nil }
	m.RemoveArtifacts = func(dir string) error { return nil }
	m.ReadPdfMarker = func(pdf string) (int, int, int, bool) { return 0, 0, 0, false }
	m.Runner = &fakeRun{out: &commandrunner.RunOutput{Stdout: "256 /compile/main.typ", ExitCode: 0}}
	res, err := m.Wordcount("p1", "u1", "main.typ", "img", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.TextWords != 256 {
		t.Fatalf("fallback: %+v", res)
	}
}

func TestWordcountCompileFailsFallsBack(t *testing.T) {
	m := newTestManager(t, nil)
	cd := compileDirOf(m.Paths.CompilesDir, "p1", "u1")
	writeRoot(t, cd, "main.typ")
	m.InjectWordometer = func(dir, root string) error { return nil }
	m.RemoveArtifacts = func(dir string) error { return nil }
	m.ReadPdfMarker = func(dir string) (int, int, int, bool) { return 0, 0, 0, false }
	m.Runner = &fakeRun{out: &commandrunner.RunOutput{Stdout: "999 /compile/main.typ"}}
	res, err := m.Wordcount("p1", "u1", "main.typ", "img", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.TextWords != 999 {
		t.Fatalf("fallback: %+v", res)
	}
}

func TestWordcountInjectErrorFallsBack(t *testing.T) {
	m := newTestManager(t, nil)
	cd := compileDirOf(m.Paths.CompilesDir, "p1", "u1")
	writeRoot(t, cd, "main.typ")
	m.InjectWordometer = func(dir, root string) error { return errors.New("inject boom") }
	m.RemoveArtifacts = func(dir string) error { return nil }
	m.ReadPdfMarker = func(pdf string) (int, int, int, bool) { return 0, 0, 0, true }
	m.Runner = &fakeRun{out: &commandrunner.RunOutput{Stdout: "55 /compile/main.typ"}}
	res, err := m.Wordcount("p1", "u1", "main.typ", "img", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.TextWords != 55 {
		t.Fatalf("fallback after inject error: %+v", res)
	}
}

func TestWordcountRunnerError(t *testing.T) {
	m := newTestManager(t, nil)
	cd := compileDirOf(m.Paths.CompilesDir, "p1", "u1")
	writeRoot(t, cd, "main.typ")
	m.InjectWordometer = func(dir, root string) error { return errors.New("x") }
	m.RemoveArtifacts = func(dir string) error { return nil }
	m.ReadPdfMarker = func(pdf string) (int, int, int, bool) { return 0, 0, 0, true }
	m.Runner = &fakeRun{err: errors.New("docker boom"), out: &commandrunner.RunOutput{}}
	if _, err := m.Wordcount("p1", "u1", "main.typ", "img", nil); err == nil {
		t.Fatal("expected runner error")
	}
}

func TestWordcountSyncsResources(t *testing.T) {
	// a POST-style request with resources triggers the sync (Node: POST
	// /wordcount carries the compile request body, GET does not).
	m := newTestManager(t, nil)
	cd := compileDirOf(m.Paths.CompilesDir, "p1", "u1")
	synced := false
	m.ResourceSync = func(req *resourcewriter.Request, basePath string) ([]resourcewriter.Resource, error) {
		synced = true
		return req.Resources, nil
	}
	// root must exist on disk for the happy path
	writeRoot(t, cd, "main.typ")
	m.InjectWordometer = func(dir, root string) error { return nil }
	m.RemoveArtifacts = func(dir string) error { return nil }
	m.ReadPdfMarker = func(pdf string) (int, int, int, bool) { return 7, 2, 1, true }

	req := makeReq()
	res, err := m.Wordcount("p1", "u1", "", "img", req)
	if err != nil {
		t.Fatal(err)
	}
	if !synced {
		t.Fatal("resources must be synced for the POST-style path")
	}
	if res.TextWords != 7 {
		t.Fatalf("results: %+v", res)
	}
}

// --- concurrency: held lock -> AlreadyCompiling (T8 maps to 423) ----------------------

func TestCompileLockHeld(t *testing.T) {
	m := newTestManager(t, nil)
	m.Acquire = func(key string) (*lockmanager.Lock, error) {
		return nil, clsErr.NewAlreadyCompilingError("compile in progress")
	}
	if _, err := m.DoCompileWithLock(makeReq(), map[string]any{}, map[string]any{}); err == nil {
		t.Fatal("expected AlreadyCompiling error")
	}
}

func TestWordcountCompileFailsMarkerAbsent(t *testing.T) {
	m := newTestManager(t, nil)
	cd := compileDirOf(m.Paths.CompilesDir, "p1", "u1")
	writeRoot(t, cd, "main.typ")
	m.InjectWordometer = func(dir, root string) error { return nil }
	m.RemoveArtifacts = func(dir string) error { return nil }
	// the wordometer PDF never rendered -> marker absent -> wc fallback.
	m.ReadPdfMarker = func(pdf string) (int, int, int, bool) { return 0, 0, 0, false }
	m.Runner = &fakeRun{out: &commandrunner.RunOutput{Stdout: "314 /compile/main.typ"}}
	res, err := m.Wordcount("p1", "u1", "main.typ", "img", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.TextWords != 314 || res.Encode != "utf-8" {
		t.Fatalf("results: %+v", res)
	}
}
