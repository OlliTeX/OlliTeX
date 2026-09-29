package compilemanager

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	commandrunner "ollitex/go/services/clsitex/commandrunner"
	"ollitex/go/services/clsitex/config"
	cerrors "ollitex/go/services/clsitex/errors"
	histwriter "ollitex/go/services/clsitex/historyresourcewriter"
	"ollitex/go/services/clsitex/resourcewriter"
)

// --- runOut (package-level helper; both callback channels) --------------------

func TestRunOutSuccess(t *testing.T) {
	r := &fakeRunner{out: &commandrunner.RunOutput{Stdout: "hi"}}
	out, err := runOut(r, "p1", []string{"ls"}, "/d", "img", 60, nil, "cg")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Stdout != "hi" {
		t.Fatalf("stdout: %q", out.Stdout)
	}
	if r.group != "cg" || r.image != "img" {
		t.Fatalf("runner args: group=%q image=%q", r.group, r.image)
	}
}

func TestRunOutError(t *testing.T) {
	r := &fakeRunner{err: os.ErrClosed}
	if _, err := runOut(r, "p1", nil, "", "", 1, nil, ""); err == nil {
		t.Fatal("expected error")
	}
}

// --- New() constructor + defaultPaths/nowMS/loadAvg ---------------------------

func TestNewConstructorWiring(t *testing.T) {
	// env required by config.New (see lockmanager.lockEnv).
	t.Setenv("PREEMPTIBLE", "TRUE")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", "/tmp/c")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_CACHE", "/tmp/nc")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_OUTPUT", "/tmp/o")
	config.ForTest()

	m := New(&fakeRunner{}, nil)
	if m == nil {
		t.Fatal("nil manager")
	}
	if m.Paths.CompilesDir == "" || m.Paths.OutputDir == "" || m.Paths.SynctexBase == "" {
		t.Fatalf("paths not set: %+v", m.Paths)
	}
	if m.Now == nil || m.LoadAvg == nil || m.Acquire == nil || m.SaveSlowPngs == nil {
		t.Fatal("base seams not wired")
	}
	if m.FindOutputFiles == nil || m.ResourceSync == nil || m.HistorySync == nil || m.SaveOutputFiles == nil {
		t.Fatal("module seams not wired")
	}
}

func TestNowMSAndLoadAvg(t *testing.T) {
	if got := nowMS(); got <= 0 {
		t.Fatalf("nowMS: %d", got)
	}
	la := loadAvg()
	if la[0] < 0 || la[1] < 0 || la[2] < 0 {
		t.Fatal("loadavg negative")
	}
}

// --- synctex: runSynctex via real temp dirs + fake Runner --------------------

const viewSample = "SyncTeX result begin\nOutput:/c/output.pdf\nPage:1\nx:136.5\ny:661.4\nh:133.7\nv:663.9\nW:343.7\nH:9.9\nSyncTeX result end\n"

const editSample = "Output:/x\nInput:/home/u/p/main.tex\nLine:10\nColumn:5\n"

func TestSyncFromCodeSuccess(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	cd := compileDirOf(m.Paths.CompilesDir, "p1", "u1")
	if err := os.MkdirAll(cd, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(cd, "output.synctex.gz"))
	m.Runner = &fakeRunner{out: &commandrunner.RunOutput{Stdout: viewSample}}
	res, err := m.SyncFromCode("p1", "u1", "main.tex", 10, 5, SyncOpts{})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(res.CodePositions) == 0 {
		t.Fatal("expected code positions")
	}
}

func TestSyncFromCodeFileMissing(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	mustMkdir(t, compileDirOf(m.Paths.CompilesDir, "p1", "u1"))
	// synctex file absent, no download opts -> raw NotFound propagates
	_, err := m.SyncFromCode("p1", "u1", "main.tex", 1, 1, SyncOpts{})
	if err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestSyncFromCodeInvalidImage(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	_, err := m.SyncFromCode("p1", "u1", "main.tex", 1, 1, SyncOpts{ImageName: "evil"})
	if err == nil {
		t.Fatal("expected invalid image error")
	}
}

func TestSyncFromPdfSuccess(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	cd := compileDirOf(m.Paths.CompilesDir, "p1", "u1")
	if err := os.MkdirAll(cd, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(cd, "output.synctex.gz"))
	m.Runner = &fakeRunner{out: &commandrunner.RunOutput{Stdout: editSample}}
	res, err := m.SyncFromPdf("p1", "u1", 2, 50, 30, SyncOpts{})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(res.PdfPositions) == 0 {
		t.Fatal("expected pdf positions")
	}
	if res.DownloadedFromCache {
		t.Fatal("should not have downloaded from cache")
	}
}

func TestSyncFromPdfDownloadAndFileMissing(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	mustMkdir(t, compileDirOf(m.Paths.CompilesDir, "p1", "u1"))
	m.DownloadOutputDotSynctex = func(pid, uid, editor, build, dir string) (bool, error) {
		return false, nil
	}
	// editor+build present, download "succeeds" but file still absent -> NotFound
	_, err := m.SyncFromPdf("p1", "u1", 1, 0, 0, SyncOpts{
		CompileFromClsiCache: true,
		EditorID:             "e1",
		BuildID:              "b1-b2",
	})
	if err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestCheckFileExistsDirect(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	cd := compileDirOf(m.Paths.CompilesDir, "p1", "u1")
	// nonexistent dir -> NotFound
	err := m.checkFileExists(cd, "x.gz")
	if !cerrors.IsNotFoundError(err) {
		t.Fatalf("expected NotFound, got: %v", err)
	}
	// dir exists, file missing -> NotFound
	mustMkdir(t, cd)
	err = m.checkFileExists(cd, "absent.gz")
	if !cerrors.IsNotFoundError(err) {
		t.Fatalf("expected file NotFound, got: %v", err)
	}
	// file present -> nil
	mustWriteFile(t, filepath.Join(cd, "f.txt"))
	if err := m.checkFileExists(cd, "f.txt"); err != nil {
		t.Fatalf("file check: %v", err)
	}
	// dir exists, a dir as the file -> 'not a file'
	sub := filepath.Join(cd, "sub")
	mustMkdir(t, sub)
	if err := m.checkFileExists(cd, "sub"); err == nil {
		t.Fatal("expected not-a-file error")
	}
}

// --- wordcount ---------------------------------------------------------------

func TestParseWordcount(t *testing.T) {
	// '!!! ' lines accumulate into Messages; Node's parser keys on
	// indexOf('outside') AND indexOf('of head') — a single
	// "Words outside of head" line sets both Outside and Headers to the
	// same value (texcount prints it as "Words outside of head: N").
	out := "Encoding: utf-8\n" +
		"Words in text: 12\n" +
		"Words in head: 3\n" +
		"Words outside of head: 4\n" + // -> outside=4 AND headers=4
		"Number of floats/tables/figures: 5\n" +
		"Number of math inlines: 6\n" +
		"Number of math displayed: 7\n" +
		"(errors:0)\n" +
		"!!! bad line !!!\n"
	r := parseWordcountFromOutput(out)
	if r.Encode != "utf-8" || r.TextWords != 12 || r.HeadWords != 3 ||
		r.Outside != 4 || r.Headers != 4 || r.Elements != 5 ||
		r.MathInline != 6 || r.MathDisplay != 7 || r.Errors != 0 {
		t.Fatalf("wordcount mismatch: %+v", r)
	}
	if r.Messages == "" {
		t.Fatal("expected messages")
	}
	// (errors:9) => errors 9 (wcInt parses the leading digits; mirrors
	// Node's parseInt leading-digit semantics).
	if got := parseWordcountFromOutput("(errors:9)").Errors; got != 9 {
		t.Fatalf("(errors:9) => %d, want 9", got)
	}
	if got := wcInt("nope"); got != 0 {
		t.Fatalf("wcInt garbage: %d", got)
	}
	if got := wcInt("12"); got != 12 {
		t.Fatalf("wcInt: %d", got)
	}
}

func TestWordcountSuccess(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	mustMkdir(t, compileDirOf(m.Paths.CompilesDir, "p1", "u1"))
	m.Runner = &fakeRunner{out: &commandrunner.RunOutput{Stdout: "Encoding: utf-8\n"}}
	if _, err := m.Wordcount("p1", "u1", "main.tex", "", nil); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestWordcountInvalidImage(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	_, err := m.Wordcount("p1", "u1", "main.tex", "not-allowed", nil)
	if err == nil {
		t.Fatal("expected invalid image error")
	}
}

func TestWordcountRunnerError(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	mustMkdir(t, compileDirOf(m.Paths.CompilesDir, "p1", "u1"))
	m.Runner = &fakeRunner{err: os.ErrClosed}
	_, err := m.Wordcount("p1", "u1", "main.tex", "", nil)
	if err == nil {
		t.Fatal("expected runner error")
	}
}

func TestWordcountSyncMissingUpdates(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	mustMkdir(t, compileDirOf(m.Paths.CompilesDir, "p1", "u1"))
	req := makeHRWReq()
	m.HistorySync = func(ctx context.Context, pid, uid string, req *histwriter.Request,
		cd string, ti, st map[string]any) (*histwriter.Result, error) {
		return nil, cerrors.NewMissingUpdatesError("mu", nil)
	}
	if _, err := m.Wordcount("p1", "u1", "main.tex", "", req); err == nil {
		t.Fatal("expected missing-updates error")
	}
}

func TestWordcountSyncTaggedError(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	mustMkdir(t, compileDirOf(m.Paths.CompilesDir, "p1", "u1"))
	req := makeReq()
	m.ResourceSync = func(r *resourcewriter.Request, base string) ([]resourcewriter.Resource, error) {
		return nil, os.ErrClosed
	}
	if _, err := m.Wordcount("p1", "u1", "main.tex", "", req); err == nil {
		t.Fatal("expected tagged error")
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

func mustWriteFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
