package resourcewriter

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"clsi/config"
	clserrors "clsi/errors"
	"clsi/metrics"
	"clsi/outputfilefinder"
	"clsi/resourcestatemanager"
)

// setupTestEnv pins CLSI paths to temp dirs and rebuilds the config singleton
// (mirrors the urlcache test harness). Seams are cleared afterwards.
func setupTestEnv(t *testing.T) {
	t.Helper()
	cacheDir := t.TempDir()
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", "/tmp/c")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_CACHE", "/tmp/nc")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_OUTPUT", "/tmp/o")
	t.Setenv("CLSI_CACHE_PATH", cacheDir)
	config.ForTest()
	InitPreciousFileMatcher("") // CLSI default: '' matches nothing except ""
	t.Cleanup(ClearSeams)
	t.Cleanup(func() { InitPreciousFileMatcher("") })
}

// --- checkPath (Node test: 'checkPath', 3 cases) ----------------------------

func TestCheckPathValid(t *testing.T) {
	got, err := CheckPath("foo", "bar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "foo/bar"; got != want {
		t.Errorf("CheckPath = %q, want %q", got, want)
	}
}

func TestCheckPathOutside(t *testing.T) {
	_, err := CheckPath("foo", "baz/../../bar")
	if err == nil {
		t.Fatalf("want error, got nil")
	}
	if want := "resource path is outside root directory"; err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestCheckPathPrefixAttack(t *testing.T) {
	// The classic prefix-attack: path.join('foo','../foobar/baz') ->
	// 'foobar/baz', which does NOT start with 'foo'+'/' so must be rejected.
	if _, err := CheckPath("foo", "../foobar/baz"); err == nil {
		t.Fatalf("want prefix-attack error, got nil")
	}
	// ...while a legitimately nested path passes.
	if _, err := CheckPath("foo", "bar/baz"); err != nil {
		t.Fatalf("nested path rejected: %v", err)
	}
}

// --- isExtraneousFile (Node: full decision table + precious override) -------

func TestIsExtraneousFileDefault(t *testing.T) {
	t.Cleanup(func() { InitPreciousFileMatcher("") })
	InitPreciousFileMatcher("") // CLSI default
	cases := []struct {
		path string
		want bool
	}{
		{"output.pdf", true},
		{"extra/file.tex", true},
		{"extra.aux", false},
		{"cache/_chunk1", false},
		{"figures/image-eps-converted-to.pdf", false},
		{"foo/main-figure0.md5", false},
		{"foo/main-figure0.dpth", false},
		{"foo/main-figure0.pdf", false},
		{"_minted-main/default-pyg-prefix.pygstyle", false},
		{"_minted-main/default.pygstyle", false},
		{"_minted-main/35E248B60965545BD232AE9F0FE9750D504A7AF0CD3BAA7542030FC560DFCC45.pygtex", false},
		{"_markdown_main/30893013dec5d69a415610079774c2f.md.tex", false},
		{"_minted-pkg_1/some-hash.pygstyle", false},
		{"output.stdout", true},
		{"output.stderr", true},
		{"output.tex", true},
		{"output-497f61b6a2", false},
		{"output.xdv", true},
		{"keepdir/file", true}, // no precious pattern -> extraneous
	}
	for _, c := range cases {
		if got := IsExtraneousFile(c.path); got != c.want {
			t.Errorf("IsExtraneousFile(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestIsExtraneousFilePrecious(t *testing.T) {
	t.Cleanup(func() { InitPreciousFileMatcher("") })
	InitPreciousFileMatcher("{keepdir/**,.other/keepdir/**}")
	if IsExtraneousFile("keepdir/file") {
		t.Errorf("keepdir/file should be kept (precious glob {keepdir/**})")
	}
	if IsExtraneousFile(".other/keepdir/.subdir/file") {
		t.Errorf("dot:true pattern must keep .other/keepdir/.subdir/file")
	}
	cases := []struct {
		path string
		want bool
	}{
		{"keepdirfile", true}, // no glob match
		{"output.pdf", true},  // forced-extraneous beats precious
		{"extra/file.tex", true},
		{"_minted-main/x.pygtex", false},
	}
	for _, c := range cases {
		if got := IsExtraneousFile(c.path); got != c.want {
			t.Errorf("IsExtraneousFile(%q) = %v, want %v (with precious pattern)", c.path, got, c.want)
		}
	}
}

// --- _removeExtraneousFiles (real FS, faked findOutputFiles listing) --------

func TestRemoveExtraneousFiles(t *testing.T) {
	setupTestEnv(t)
	InitPreciousFileMatcher("{keepdir/**,.other/keepdir/**}")
	base := t.TempDir()
	mkFile := func(rel string) {
		t.Helper()
		full := filepath.Join(base, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	extraneous := []string{"output.pdf", "extra/file.tex", "output.stdout", "output.stderr"}
	kept := []string{
		"extra.aux", "cache/_chunk1",
		"figures/image-eps-converted-to.pdf",
		"foo/main-figure0.md5", "foo/main-figure0.dpth", "foo/main-figure0.pdf",
		"_minted-main/default-pyg-prefix.pygstyle",
		"_markdown_main/30893013dec5d69a415610079774c2f.md.tex",
		"keepdir/file", ".other/keepdir/.subdir/file",
	}
	for _, rel := range append(append([]string{}, extraneous...), kept...) {
		mkFile(rel)
	}
	// Fake the findOutputFiles listing: every file listed above.
	FindOutputFiles = func(resources []outputfilefinder.Resource, directory string) (outputfilefinder.FindResult, error) {
		res := outputfilefinder.FindResult{}
		for _, rel := range append(append([]string{}, extraneous...), kept...) {
			res.OutputFiles = append(res.OutputFiles, outputfilefinder.OutputFile{Path: rel})
		}
		return res, nil
	}
	req := &Request{ProjectID: "p1", UserID: "u1", SyncType: "incremental", SyncState: "s1"}
	if _, err := RemoveExtraneousFiles(req, nil, base); err != nil {
		t.Fatalf("RemoveExtraneousFiles: %v", err)
	}
	for _, rel := range extraneous {
		if _, err := os.Stat(filepath.Join(base, filepath.FromSlash(rel))); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("want %s deleted (err=%v)", rel, err)
		}
	}
	for _, rel := range kept {
		if _, err := os.Lstat(filepath.Join(base, filepath.FromSlash(rel))); err != nil {
			t.Errorf("want %s kept: %v", rel, err)
		}
	}
}

// --- _writeResourceToDisk (content + URL, swallow-on-error) ------------------

func TestWriteResourceContent(t *testing.T) {
	setupTestEnv(t)
	base := t.TempDir()
	if err := SaveIncrementalResourcesToDisk("p1", []Resource{{Path: "sub/main.tex", Content: []byte("Hello world")}}, base); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(base, "sub", "main.tex"))
	if err != nil {
		t.Fatalf("content file missing: %v", err)
	}
	if want := "Hello world"; string(data) != want {
		t.Errorf("content = %q, want %q", data, want)
	}
}

func TestWriteResourceURLSwallowsError(t *testing.T) {
	setupTestEnv(t)
	base := t.TempDir()
	DownloadFile = func(projectID, url, fallbackURL, destPath string, _ *time.Time) error {
		return errors.New("fake download failure")
	}
	before := metrics.DownloadFailed.Get()
	if err := SaveIncrementalResourcesToDisk("p1", []Resource{{Path: "main.tex", URL: "http://example.com/a", FallbackURL: "http://example.com/b"}}, base); err != nil {
		t.Fatalf("download error must be swallowed, got %v", err)
	}
	if got := metrics.DownloadFailed.Get(); got != before+1 {
		t.Errorf("download-failed counter = %d, want %d", got, before+1)
	}
	if _, err := os.Stat(filepath.Join(base, "main.tex")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file should not exist after failed download (err=%v)", err)
	}
}

func TestWriteResourceURLSuccess(t *testing.T) {
	setupTestEnv(t)
	base := t.TempDir()
	DownloadFile = func(projectID, url, fallbackURL, destPath string, _ *time.Time) error {
		return os.WriteFile(destPath, []byte("downloaded"), 0o644)
	}
	if err := SaveIncrementalResourcesToDisk("p1", []Resource{{Path: "img.png", URL: "http://example.com/img", FallbackURL: "http://example.com/img-fb"}}, base); err != nil {
		t.Fatalf("%v", err)
	}
	data, err := os.ReadFile(filepath.Join(base, "img.png"))
	if err != nil || string(data) != "downloaded" {
		t.Fatalf("downloaded file = %q err=%v", data, err)
	}
}

func TestWriteResourceURLResolvedToFallback(t *testing.T) {
	setupTestEnv(t)
	base := t.TempDir()
	var gotURL string
	DownloadFile = func(projectID, url, fallbackURL, destPath string, _ *time.Time) error {
		gotURL = url
		return os.WriteFile(destPath, []byte("fb"), 0o644)
	}
	// node: const url = resource.fallbackURL ?? resource.url
	if err := SaveIncrementalResourcesToDisk("p1", []Resource{{Path: "i.png", URL: "http://x/i", FallbackURL: "http://x/i-fb"}}, base); err != nil {
		t.Fatalf("%v", err)
	}
	if want := "http://x/i-fb"; gotURL != want {
		t.Errorf("download called with url %q, want %q (fallback resolution)", gotURL, want)
	}
}

func TestWriteResourceTraversal(t *testing.T) {
	setupTestEnv(t)
	base := t.TempDir()
	if err := SaveIncrementalResourcesToDisk("p1", []Resource{{Path: "../../main.tex", Content: []byte("x")}}, base); err == nil {
		t.Fatalf("want traversal error")
	}
	if _, serr := os.Stat(filepath.Join(base, "..", "..", "main.tex")); serr == nil {
		t.Errorf("file written outside base dir")
	}
}

// --- syncResourcesToDisk (full / incremental) --------------------------------

func TestSyncFull(t *testing.T) {
	setupTestEnv(t)
	base := t.TempDir()
	// a stale extraneous file that saveAll must clean up: 'stale.tmp' matches
	// no keep-list and is not forced => isExtraneousFile('stale.tmp')=true
	if err := os.WriteFile(filepath.Join(base, "stale.tmp"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := &Request{
		ProjectID: "project-id-123",
		UserID:    "u1",
		SyncState: "0123456789abcdef",
		Resources: []Resource{
			{Path: "main.tex", Content: []byte("M")},
			{Path: "sub/nested.tex", Content: []byte("N")},
		},
	}
	list, err := SyncResourcesToDisk(req, base)
	if err != nil {
		t.Fatalf("full sync: %v", err)
	}
	if got, want := len(list), 2; got != want {
		t.Fatalf("full returns request.resources (len %d, want %d)", got, want)
	}
	if _, err := os.Stat(filepath.Join(base, "stale.tmp")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stale.tmp must be removed on full sync (err=%v)", err)
	}
	for rel := range map[string]struct{}{"main.tex": {}, "sub/nested.tex": {}} {
		if _, err := os.Stat(filepath.Join(base, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s must exist: %v", rel, err)
		}
	}
	stateFile, err := os.ReadFile(filepath.Join(base, ".project-sync-state"))
	if err != nil {
		t.Fatalf("state file: %v", err)
	}
	if want := "main.tex\nsub/nested.tex\nstateHash:0123456789abcdef"; string(stateFile) != want {
		t.Errorf("state file = %q, want %q", stateFile, want)
	}
}

func TestSyncIncremental(t *testing.T) {
	setupTestEnv(t)
	base := t.TempDir()
	mk := func(rel, content string) {
		t.Helper()
		full := filepath.Join(base, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// prior resources on disk, established as the prior sync-state
	mk("main.tex", "OLD")
	mk("input/a.tex", "A")
	seedState := "inc-state-abc"
	if err := resourcestatemanager.SaveProjectState(&seedState, []resourcestatemanager.Resource{{Path: "main.tex"}, {Path: "input/a.tex"}}, base); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	// a stale extraneous output the remove step must clear (output.stderr is
	// forced-extraneous, not in the prior resources)
	if err := os.WriteFile(filepath.Join(base, "output.stderr"), []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := &Request{
		ProjectID: "p1",
		UserID:    "u",
		SyncType:  "incremental",
		SyncState: "inc-state-abc",
		Resources: []Resource{{Path: "main.tex", Content: []byte("NEW")}},
	}
	list, err := SyncResourcesToDisk(req, base)
	if err != nil {
		t.Fatalf("incremental sync: %v", err)
	}

	if got, want := len(list), 2; got != want {
		t.Fatalf("incremental returns prior resourceList (len %d, want %d)", got, want)
	}
	// output.stderr cleared
	if _, err := os.Stat(filepath.Join(base, "output.stderr")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("output.stderr must be removed (err=%v)", err)
	}
	// delta written
	data, err := os.ReadFile(filepath.Join(base, "main.tex"))
	if err != nil || string(data) != "NEW" {
		t.Fatalf("main.tex = %q err=%v, want \"NEW\"", data, err)
	}
	// prior untouched
	data2, err := os.ReadFile(filepath.Join(base, "input", "a.tex"))
	if err != nil || string(data2) != "A" {
		t.Fatalf("input/a.tex = %q err=%v", data2, err)
	}
	// incremental must NOT rewrite the state file (only full does)
	if _, err := os.ReadFile(filepath.Join(base, ".project-sync-state")); err != nil {
		t.Errorf("state file must persist across incremental: %v", err)
	}
}

// --- error branches ----------------------------------------------------------

func TestDeleteStatErrorENOTDIR(t *testing.T) {
	// os.Stat on a path whose first component is a FILE -> ENOTDIR (not
	// ENOENT) so DeleteFileIfNotDirectory logs + returns the error.
	base := t.TempDir()
	file := filepath.Join(base, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// "f.txt/nope" -> ENOTDIR (stat fails because f.txt is not a directory).
	if err := DeleteFileIfNotDirectory(filepath.Join(file, "nope")); err == nil {
		t.Fatalf("want stat ENOTDIR error")
	}
}

func TestRemoveExtraneousFilesFindError(t *testing.T) {
	setupTestEnv(t)
	InitPreciousFileMatcher("")
	FindOutputFiles = func(resources []outputfilefinder.Resource, directory string) (outputfilefinder.FindResult, error) {
		return outputfilefinder.FindResult{}, errors.New("readdir boom")
	}
	if _, err := RemoveExtraneousFiles(&Request{ProjectID: "p", UserID: "u"}, nil, t.TempDir()); err == nil {
		t.Fatalf("want findOutputFiles error")
	}
}

func TestRemoveExtraneousFilesDeleteError(t *testing.T) {
	setupTestEnv(t)
	InitPreciousFileMatcher("")
	base := t.TempDir()
	// an extraneous relative path whose parent component is a FILE -> delete
	// -> os.Stat ENOTDIR (not ENOENT) -> error propagates.
	if err := os.WriteFile(filepath.Join(base, "a"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	FindOutputFiles = func(resources []outputfilefinder.Resource, directory string) (outputfilefinder.FindResult, error) {
		// "a/main.tex" IS extraneous (no keep-regex matches); deleting
		// <base>/a/main.tex stats a path whose first component "a" is a
		// FILE -> ENOTDIR (not ENOENT) -> DeleteFileIfNotDirectory logs +
		// returns the error, which propagates out of RemoveExtraneousFiles.
		return outputfilefinder.FindResult{OutputFiles: []outputfilefinder.OutputFile{{Path: "a/main.tex"}}}, nil
	}
	if _, err := RemoveExtraneousFiles(&Request{ProjectID: "p", UserID: "u"}, nil, base); err == nil {
		t.Fatalf("want delete ENOTDIR error propagated")
	}
}

func TestWriteResourceMkdirAllError(t *testing.T) {
	setupTestEnv(t)
	base := t.TempDir()
	// path "f/x.txt" where "f" is a FILE -> MkdirAll("f") fails (ENOTDIR).
	if err := os.WriteFile(filepath.Join(base, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveIncrementalResourcesToDisk("p1", []Resource{{Path: "f/x.txt", Content: []byte("x")}}, base); err == nil {
		t.Fatalf("want MkdirAll error")
	}
}

func TestCreateDirectoryError(t *testing.T) {
	// CreateDirectory under a FILE path -> os.Mkdir fails (not EEXIST).
	base := t.TempDir()
	file := filepath.Join(base, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CreateDirectory(filepath.Join(file, "sub")); err == nil {
		t.Fatalf("want mkdir error (base is a file)")
	}
}

func TestSyncIncrementalInvalidState(t *testing.T) {
	setupTestEnv(t)
	InitPreciousFileMatcher("")
	base := t.TempDir()
	seed := "stateA"
	if err := resourcestatemanager.SaveProjectState(&seed, []resourcestatemanager.Resource{{Path: "main.tex"}}, base); err != nil {
		t.Fatalf("seed: %v", err)
	}
	req := &Request{
		ProjectID: "p", UserID: "u", SyncType: "incremental", SyncState: "WRONG_HASH",
		Resources: []Resource{{Path: "main.tex", Content: []byte("x")}},
	}
	_, err := SyncResourcesToDisk(req, base)
	if err == nil {
		t.Fatalf("want FilesOutOfSyncError")
	}
	if _, ok := err.(*clserrors.FilesOutOfSyncError); !ok {
		t.Fatalf("want FilesOutOfSyncError, got %v (type %T)", err, err)
	}
}

func TestSyncFullCreateError(t *testing.T) {
	setupTestEnv(t)
	InitPreciousFileMatcher("")
	// Make urlcache.CreateProjectDir (MkdirAll) fail: set CLSI_CACHE_PATH to
	// a path whose parent component is a FILE, so MkdirAll("<file>/child")
	// errors. GetProjectCacheDir = CLSI_CACHE_PATH + "/" + projectID.
	file := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(file, []byte("f"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLSI_CACHE_PATH", file)
	config.ForTest()
	req := &Request{ProjectID: "p1", UserID: "u", Resources: nil, SyncType: ""}
	if _, err := SyncResourcesToDisk(req, t.TempDir()); err == nil {
		t.Fatalf("want createProjectDir (MkdirAll over file) error")
	}
}

func TestSaveAllRemoveExtraneousError(t *testing.T) {
	setupTestEnv(t)
	InitPreciousFileMatcher("")
	FindOutputFiles = func(resources []outputfilefinder.Resource, directory string) (outputfilefinder.FindResult, error) {
		return outputfilefinder.FindResult{}, errors.New("find boom")
	}
	if _, err := SyncResourcesToDisk(&Request{ProjectID: "p", UserID: "u", Resources: nil, SyncType: ""}, t.TempDir()); err == nil {
		t.Fatalf("want saveAll removeExtraneous error via full sync")
	}
}

func TestSaveIncrementalWriteErrorSeam(t *testing.T) {
	setupTestEnv(t)
	InitPreciousFileMatcher("")
	WriteFile = func(filePath string, content []byte) error { return errors.New("write boom") }
	if err := SaveIncrementalResourcesToDisk("p", []Resource{{Path: "x.tex", Content: []byte("x")}}, t.TempDir()); err == nil {
		t.Fatalf("want WriteFile seam error")
	}
}

func TestSaveAllWriteErrorSeam(t *testing.T) {
	setupTestEnv(t)
	InitPreciousFileMatcher("")
	FindOutputFiles = func(resources []outputfilefinder.Resource, directory string) (outputfilefinder.FindResult, error) {
		return outputfilefinder.FindResult{}, nil
	}
	WriteFile = func(filePath string, content []byte) error { return errors.New("write boom") }
	if _, err := SyncResourcesToDisk(&Request{ProjectID: "p", UserID: "u", Resources: []Resource{{Path: "x.tex", Content: []byte("x")}}, SyncType: ""}, t.TempDir()); err == nil {
		t.Fatalf("want saveAll write error via full sync")
	}
}

func TestDeleteNoSuchFileOk(t *testing.T) {
	// ENOENT delete path: the file is gone -> DeleteFileIfNotDirectory returns
	// nil (mirrors Node `if (err.code === 'ENOENT') callback()`).
	base := t.TempDir()
	if err := DeleteFileIfNotDirectory(filepath.Join(base, "ghost/output.log")); err != nil {
		t.Fatalf("ENOENT delete must be nil, got %v", err)
	}
}

func TestDeleteRemoveError(t *testing.T) {
	// os.Remove of a regular file inside a READ-ONLY dir -> EACCES (uid!=0),
	// so DeleteFileIfNotDirectory logs + returns the error (not ENOENT).
	base := t.TempDir()
	d := filepath.Join(base, "ro")
	if err := os.Mkdir(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "output.log"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(d, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(d, 0o755)
		_ = os.RemoveAll(d)
	})
	if err := DeleteFileIfNotDirectory(filepath.Join(d, "output.log")); err == nil {
		t.Fatalf("want os.Remove (EACCES) error propagated")
	}
}

func TestDownloadDefaultNoHTTP(t *testing.T) {
	setupTestEnv(t)
	InitPreciousFileMatcher("")
	base := t.TempDir()
	// Seed the urlcache cache for <CLSI_CACHE_DIR>/p1/-a-0 so
	// DownloadUrlToFile copies it (NO HTTP), exercising the default download
	// closure that the resourcewriter seams to urlcache with suffix "".
	cf := config.Get().Path.ClsiCacheDir
	seedDir := filepath.Join(cf, "p1")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "-a-0"), []byte("SEED"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := Resource{Path: "img.png", URL: "http://example.com/a"}
	if err := SaveIncrementalResourcesToDisk("p1", []Resource{res}, base); err != nil {
		t.Fatalf("default download: %v", err)
	}
	data, rerr := os.ReadFile(filepath.Join(base, "img.png"))
	if rerr != nil || string(data) != "SEED" {
		t.Fatalf("default download should copy seeded cache: %q err=%v", data, rerr)
	}
}
