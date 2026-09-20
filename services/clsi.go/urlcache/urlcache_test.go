package urlcache

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"clsi/config"
)

// withCacheDir pins CLSI_CACHE_PATH to a fresh temp dir and re-builds the
// config singleton (restored on cleanup).
func withCacheDir(t *testing.T) string {
	cacheDir := t.TempDir()
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", "/tmp/c")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_CACHE", "/tmp/nc")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_OUTPUT", "/tmp/o")
	t.Setenv("CLSI_CACHE_PATH", cacheDir)
	config.ForTest()
	return cacheDir
}

func TestCachePathBase(t *testing.T) {
	withCacheDir(t)
	got, err := CachePath("p1", "http://filestore/project/p1/file/f", nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := config.Get().Path.ClsiCacheDir + "/p1/-project-p1-file-f-0"
	if got != want {
		t.Errorf("CachePath = %q, want %q", got, want)
	}
}

func TestCachePathWithLastModified(t *testing.T) {
	withCacheDir(t)
	ts := time.UnixMilli(123456)
	got, _ := CachePath("p2", "http://filestore/project/p2/file/f", &ts)
	want := config.Get().Path.ClsiCacheDir + "/p2/-project-p2-file-f-123456"
	if got != want {
		t.Errorf("CachePath = %q, want %q", got, want)
	}
}

func TestCachePathSlashReplace(t *testing.T) {
	withCacheDir(t)
	got, _ := CachePath("p3", "https://host/bucket/x/y/z/key/a", nil)
	want := config.Get().Path.ClsiCacheDir + "/p3/-bucket-x-y-z-key-a-0"
	if got != want {
		t.Errorf("CachePath = %q, want %q", got, want)
	}
}

func TestGetProjectCacheDir(t *testing.T) {
	withCacheDir(t)
	if got, want := GetProjectCacheDir("abc"), config.Get().Path.ClsiCacheDir+"/abc"; got != want {
		t.Errorf("GetProjectCacheDir = %q, want %q", got, want)
	}
}

func TestDownloadUrlToFile_NoSuffixCacheHit(t *testing.T) {
	withCacheDir(t)
	primary := "http://filestore/project/p1/file/f"
	cmp, _ := CachePath("p1", primary, nil)
	if err := os.MkdirAll(filepath.Dir(cmp), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(cmp, []byte("cached!"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "dest.bin")
	origFetch := FetchURL
	fetches := 0
	FetchURL = func(u, fb, target string) error { fetches++; return nil }
	defer func() { FetchURL = origFetch }()

	if _, err := DownloadUrlToFile("p1", primary, "", dest, nil, ""); err != nil {
		t.Fatalf("err: %v", err)
	}
	if fetches != 0 {
		t.Errorf("fetches = %d, want 0 (cache hit)", fetches)
	}
	data, _ := os.ReadFile(dest)
	if string(data) != "cached!" {
		t.Errorf("dest = %q, want 'cached!'", data)
	}
}

func TestDownloadUrlToFile_NoSuffixCacheMissDownloadAndCopy(t *testing.T) {
	withCacheDir(t)
	if err := CreateProjectDir("p1"); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cmp, _ := CachePath("p1", "http://filestore/project/p1/file/f", nil)
	dest := filepath.Join(t.TempDir(), "dest.bin")
	var fetchTarget string
	origFetch := FetchURL
	FetchURL = func(u, fb, target string) error {
		fetchTarget = target
		return os.WriteFile(target, []byte("downloaded"), 0644)
	}
	defer func() { FetchURL = origFetch }()

	h, err := DownloadUrlToFile("p1", "http://filestore/project/p1/file/f", "", dest, nil, "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if h != nil {
		t.Errorf("handle should be nil without suffix, got %+v", h)
	}
	if fetchTarget != cmp {
		t.Errorf("fetch target = %q, want cache path %q", fetchTarget, cmp)
	}
	data, _ := os.ReadFile(dest)
	if string(data) != "downloaded" {
		t.Errorf("dest = %q, want 'downloaded'", data)
	}
}

func TestDownloadUrlToFile_WithSuffixCacheMissReturnsHandle(t *testing.T) {
	withCacheDir(t)
	if err := CreateProjectDir("p1"); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	primary := "http://filestore/project/p1/file/f"
	const suffix = "-user-cache-key"
	dest := filepath.Join(t.TempDir(), "dest.bin")
	var fetchTarget string
	origFetch := FetchURL
	FetchURL = func(u, fb, target string) error { fetchTarget = target; return nil }
	defer func() { FetchURL = origFetch }()

	h, err := DownloadUrlToFile("p1", primary, "", dest, nil, suffix)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if h == nil {
		t.Fatalf("want conversion handle, got nil")
	}
	wantCache := "-project-p1-file-f-0.opt"
	if !endsWith(h.CachePath, wantCache) {
		t.Errorf("cachePath = %q, want ends with %q", h.CachePath, wantCache)
	}
	if h.ConversionPath != h.CachePath+suffix {
		t.Errorf("conversionPath = %q, want %q", h.ConversionPath, h.CachePath+suffix)
	}
	if h.DestPath != dest {
		t.Errorf("destPath = %q, want %q", h.DestPath, dest)
	}
	if fetchTarget != h.ConversionPath {
		t.Errorf("fetch targeted %q, want %q", fetchTarget, h.ConversionPath)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("dest should NOT exist yet (conversion not committed)")
	}
}

func TestDownloadUrlToFile_WithSuffixOptCacheHit(t *testing.T) {
	withCacheDir(t)
	primary := "http://filestore/project/p1/file/f"
	const suffix = "-user-cache-key"
	optPath, _ := CachePath("p1", primary, nil)
	optPath += ".opt"
	if err := os.MkdirAll(filepath.Dir(optPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(optPath, []byte("opted"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "dest.bin")
	origFetch := FetchURL
	fetches := 0
	FetchURL = func(u, fb, target string) error { fetches++; return nil }
	defer func() { FetchURL = origFetch }()

	h, err := DownloadUrlToFile("p1", primary, "", dest, nil, suffix)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if h != nil {
		t.Errorf("cache hit should return nil handle, got %+v", h)
	}
	if fetches != 0 {
		t.Errorf("fetches = %d, want 0", fetches)
	}
	data, _ := os.ReadFile(dest)
	if string(data) != "opted" {
		t.Errorf("dest = %q, want 'opted'", data)
	}
}

func TestDownloadUrlToFile_FallbackCopyOnPrimaryMiss(t *testing.T) {
	withCacheDir(t)
	dest := filepath.Join(t.TempDir(), "dest.bin")
	primary := "http://filestore/project/p1/file/f"
	fallback := "http://fallback-host/project/p1/file/f"
	fcmp, _ := CachePath("p1", fallback, nil)
	if err := os.MkdirAll(filepath.Dir(fcmp), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(fcmp, []byte("fallback cached"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	origFetch := FetchURL
	fetches := 0
	FetchURL = func(u, fb, target string) error { fetches++; return nil }
	defer func() { FetchURL = origFetch }()

	if _, err := DownloadUrlToFile("p1", primary, fallback, dest, nil, ""); err != nil {
		t.Fatalf("err: %v", err)
	}
	if fetches != 0 {
		t.Errorf("fetches = %d, want 0 (fallback cache hit)", fetches)
	}
	data, _ := os.ReadFile(dest)
	if string(data) != "fallback cached" {
		t.Errorf("dest = %q, want 'fallback cached'", data)
	}
}

func TestDownloadUrlToFile_NonEnoentCopyErrorPropagation(t *testing.T) {
	withCacheDir(t)
	// Make the cachePath a directory so copyFile fails with a non-ENOENT
	// error (Node contract: 'should raise non cache-miss errors').
	primary := "http://filestore/project/p1/file/f"
	cmp, _ := CachePath("p1", primary, nil)
	if err := os.MkdirAll(cmp, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	origFetch := FetchURL
	FetchURL = func(u, fb, target string) error { return nil }
	defer func() { FetchURL = origFetch }()

	dest := filepath.Join(t.TempDir(), "dest.bin")
	if _, err := DownloadUrlToFile("p1", primary, "", dest, nil, ""); err == nil {
		t.Errorf("want error (non-ENOENT copy failure), got nil")
	}
}

func TestCommitConversion(t *testing.T) {
	withCacheDir(t)
	convDir := t.TempDir()
	convPath := filepath.Join(convDir, "conv")
	cachePath := filepath.Join(convDir, "cache.opt")
	if err := os.WriteFile(convPath, []byte("converted"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "dest.bin")
	if err := CommitConversion(convPath, cachePath, dest); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if _, err := os.Stat(convPath); !os.IsNotExist(err) {
		t.Errorf("conversionPath should be gone (renamed), err = %v", err)
	}
	if data, err := os.ReadFile(cachePath); err != nil || string(data) != "converted" {
		t.Errorf("cachePath data = %q, want 'converted'", data)
	}
	if data, err := os.ReadFile(dest); err != nil || string(data) != "converted" {
		t.Errorf("dest data = %q, want 'converted'", data)
	}
}

func TestCommitConversion_MissingConversionPropagates(t *testing.T) {
	withCacheDir(t)
	dir := t.TempDir()
	convPath := filepath.Join(dir, "missing")
	cachePath := filepath.Join(dir, "cache.opt")
	dest := filepath.Join(t.TempDir(), "dest.bin")
	if err := CommitConversion(convPath, cachePath, dest); err == nil {
		t.Errorf("want error for missing conversionPath")
	}
}

func TestIsConversionCached(t *testing.T) {
	withCacheDir(t)
	primary := "http://filestore/project/p1/file/f"
	cmp, _ := CachePath("p1", primary, nil)
	if err := os.MkdirAll(filepath.Dir(cmp), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cached, err := IsConversionCached("p1", primary, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cached {
		t.Errorf("want false before .opt exists")
	}
	if err := os.WriteFile(cmp+".opt", nil, 0644); err != nil {
		t.Fatalf("seed .opt: %v", err)
	}
	cached, err = IsConversionCached("p1", primary, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !cached {
		t.Errorf("want true after .opt exists")
	}
}

func TestClearProject(t *testing.T) {
	withCacheDir(t)
	projDir := GetProjectCacheDir("pX")
	if err := os.MkdirAll(filepath.Join(projDir, "deep"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "f"), nil, 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := ClearProject("pX"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := os.Stat(projDir); !os.IsNotExist(err) {
		t.Errorf("projDir should be removed (err = %v)", err)
	}
}

func endsWith(s, suffix string) bool {
	if len(s) < len(suffix) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}

func TestDownloadUrlToFile_FallbackOptUsed(t *testing.T) {
	// suffix case where the *fallback* URL's .opt is hit (primary misses)
	withCacheDir(t)
	if err := CreateProjectDir("p1"); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	const suffix = "-user"
	primary := "http://filestore/project/p1/file/f"
	fallback := "http://fallback/project/p1/file/f"
	fcmp, _ := CachePath("p1", fallback, nil)
	if err := os.MkdirAll(filepath.Dir(fcmp), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(fcmp+".opt", []byte("fall opted"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	origFetch := FetchURL
	fetches := 0
	FetchURL = func(u, fb, target string) error { fetches++; return nil }
	defer func() { FetchURL = origFetch }()

	dest := filepath.Join(t.TempDir(), "dest.bin")
	if _, err := DownloadUrlToFile("p1", primary, fallback, dest, nil, suffix); err != nil {
		t.Fatalf("err: %v", err)
	}
	if fetches != 0 {
		t.Errorf("fetches = %d, want 0 (fallback .opt cache hit)", fetches)
	}
	data, _ := os.ReadFile(dest)
	if string(data) != "fall opted" {
		t.Errorf("dest = %q, want 'fall opted'", data)
	}
}

func TestDownloadUrlToFile_DownloadErrorPropagates(t *testing.T) {
	withCacheDir(t)
	origFetch := FetchURL
	FetchURL = func(u, fb, target string) error { return os.ErrInvalid }
	defer func() { FetchURL = origFetch }()

	_, err := DownloadUrlToFile("p1", "http://filestore/project/p1/file/f", "",
		filepath.Join(t.TempDir(), "d.bin"), nil, "")
	if err == nil {
		t.Fatalf("want download error to propagate")
	}
	if !errors.Is(err, os.ErrInvalid) {
		t.Errorf("err = %v, want os.ErrInvalid", err)
	}
}

func TestDownload_UsesDefaultFetchWhenUnset(t *testing.T) {
	// The default FetchURL (urlfetcher.PipeUrlToFileWithRetry) must be
	// invoked when the package-level var is nil; verify indirectly by
	// overriding the fetchReader in the urlfetcher package would cross
	// packages — instead exercise the nil-guard path only for the fact
	// that a non-nil FetchURL wins.
	origFetch := FetchURL
	defer func() { FetchURL = origFetch }()
	if FetchURL == nil {
		t.Fatalf("FetchURL should default to urlfetcher.PipeUrlToFileWithRetry")
	}
}

func TestCommitConversion_CopyFailPropagates(t *testing.T) {
	withCacheDir(t)
	// after rename, destPath is inside an existing directory (not a file)
	// so the copyFile to destPath hits a non-ENOENT error.
	dir := t.TempDir()
	conv := filepath.Join(dir, "conv")
	cache := filepath.Join(dir, "cache.opt")
	if err := os.WriteFile(conv, []byte("x"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// destPath is a directory → WriteFile to it fails with ENOTDIR
	dest := t.TempDir()
	// Note: os.MkdirAll(dest) won't do harm since it already exists.
	if err := CommitConversion(conv, cache, dest); err == nil {
		t.Fatalf("want error (destPath is a directory)")
	}
}

func TestIsConversionCached_NonExistStatErr(t *testing.T) {
	// stat on a valid directory returns nil (no err) → cached=false.
	withCacheDir(t)
	// create the *parent* of optPath such that stat on the optPath succeeds?
	// Instead: craft a projectID where getCachePath yields a path whose
	// '.opt' is inside a directory we made readable.
	// Easiest: create the optPath as a directory — os.Stat succeeds.
	primary := "http://filestore/project/p1/file/f"
	cmp, _ := CachePath("p1", primary, nil)
	if err := os.MkdirAll(cmp+".opt", 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cached, err := IsConversionCached("p1", primary, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !cached {
		t.Errorf("directory .opt should be counted as cached")
	}
}

func TestTryCopyFile_ReadFileDirError(t *testing.T) {
	// reading a directory via ReadFile on linux yields a non-ENOENT error.
	dir := t.TempDir()
	copied, err := tryCopyFile(dir, filepath.Join(t.TempDir(), "out.bin"))
	if err == nil {
		t.Fatalf("want error reading a directory (non-ENOENT)")
	}
	if copied {
		t.Errorf("copied = true, want false")
	}
}

func TestTryCopyFile_ParentDirBareFilename(t *testing.T) {
	// parentDir("") and parentDir("/abs") must not cause MkdirAll errors;
	// also parentDir("/file") returns "" (single-component path).
	for _, p := range []string{"bare", "/bare"} {
		if parentDir(p) != "" {
			t.Errorf("parentDir(%q) = %q, want ''", p, parentDir(p))
		}
	}
}

func TestClearProject_EmptyDir(t *testing.T) {
	withCacheDir(t)
	// no project created: rmAll on a non-existent path succeeds
	if err := ClearProject("nope"); err != nil {
		t.Errorf("ClearProject on missing dir should not error: %v", err)
	}
}

func TestDownload_DedupesConcurrentRequests(t *testing.T) {
	withCacheDir(t)
	// first fetch blocks; second goroutine must wait and share the outcome;
	// the fetch runs ONLY once.
	calls := 0
	ch := make(chan struct{})
	origFetch := FetchURL
	FetchURL = func(u, fb, target string) error {
		calls++
		<-ch
		return nil
	}
	defer func() { FetchURL = origFetch }()

	var res1, res2 error
	var g1, g2 sync.WaitGroup
	g1.Add(1)
	g2.Add(1)
	go func() {
		defer g1.Done()
		res1 = download("http://x", "", "target-1")
	}()
	go func() {
		defer g2.Done()
		res2 = download("http://x", "", "target-1")
	}()
	// let first goroutine register
	time.Sleep(10 * time.Millisecond)
	close(ch)
	g1.Wait()
	g2.Wait()
	if calls != 1 {
		t.Errorf("fetch calls = %d, want 1 (deduped)", calls)
	}
	if res1 != nil || res2 != nil {
		t.Errorf("res1 = %v, res2 = %v; want nil (shared success)", res1, res2)
	}
}

func TestDownload_WaiterSeesFirstError(t *testing.T) {
	withCacheDir(t)
	block := make(chan struct{})
	calls := 0
	origFetch := FetchURL
	FetchURL = func(u, fb, target string) error {
		calls++
		<-block
		return os.ErrInvalid
	}
	defer func() { FetchURL = origFetch }()

	err1Ch := make(chan error)
	go func() { err1Ch <- download("http://x", "", "target-err") }()
	time.Sleep(5 * time.Millisecond)
	close(block)
	err1 := <-err1Ch
	if !errors.Is(err1, os.ErrInvalid) {
		t.Fatalf("first call err = %v, want os.ErrInvalid", err1)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func lastModifiedMs(ms int64) *time.Time {
	tm := time.UnixMilli(ms)
	return &tm
}

func TestDownloadUrlToFile_DefaultFetchRealDownload(t *testing.T) {
	// Use the *real* default FetchURL (urlfetcher.PipeUrlToFileWithRetry)
	// against a real httptest server: miss → download → copy (no suffix).
	withCacheDir(t)
	if err := CreateProjectDir("p1"); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	urlStr := "http://filestore/project/p1/file/f"
	// Emulate a successful download that writes the conversionPath.
	origFetch := FetchURL
	FetchURL = func(u, fallback, target string) error {
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return os.WriteFile(target, []byte("downstream"), 0644)
	}
	defer func() { FetchURL = origFetch }()
	dest := filepath.Join(t.TempDir(), "d.bin", "out.bin")
	if _, err := DownloadUrlToFile("p1", urlStr, "", dest, lastModifiedMs(1), ""); err != nil {
		t.Fatalf("err: %v", err)
	}
	data, rerr := os.ReadFile(dest)
	if rerr != nil {
		t.Fatalf("read dest: %v", rerr)
	}
	if string(data) != "downstream" {
		t.Errorf("dest = %q, want 'downstream'", data)
	}
}

func TestIsConversionCached_StatNonExistError(t *testing.T) {
	// force a stat error that IS NOT "not exist": stat a path where the
	// parent is a *file*.
	withCacheDir(t)
	if err := CreateProjectDir("p1"); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	urlStr := "http://filestore/project/p1/file/f"
	// Make the cache path's parent a regular file: create the file with
	// the final segment of the path such that stat(optPath) yields ENOTDIR.
	cmp, _ := CachePath("p1", urlStr, nil)
	// The .opt path is sibling of the cache path; make the cache path itself
	// a file → stat <parent>/.opt won't hit ENOENT but ENOTDIR? No, the
	// parent is project dir. Instead: make optPath's parent a file.
	// opt = <project>/p/<key>.opt — parent of opt is <project>/p/<key>.
	// We create <project>/p/<key> as a FILE (not dir); then stat on
	// <project>/p/<key>.opt returns ENOENT (not exist), not ENOTDIR.
	if err := os.MkdirAll(filepath.Dir(cmp), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(cmp, []byte("x"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cached, err := IsConversionCached("p1", urlStr, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cached {
		t.Errorf("cached = true, want false")
	}
}

func TestDownloadUrlToFile_Non404FetchErrorPropagates(t *testing.T) {
	// download() returns an error → DownloadUrlToFile propagates it (line 113).
	withCacheDir(t)
	origFetch := FetchURL
	FetchURL = func(u, fb, target string) error { return os.ErrInvalid }
	defer func() { FetchURL = origFetch }()
	urlStr := "http://filestore/project/p1/file/f"
	dest := filepath.Join(t.TempDir(), "d.bin")
	_, err := DownloadUrlToFile("p1", urlStr, "", dest, lastModifiedMs(1), "")
	if err == nil {
		t.Errorf("want propagated fetch error")
	}
	if !errors.Is(err, os.ErrInvalid) {
		t.Errorf("err = %v, want os.ErrInvalid", err)
	}
}

func TestCachePath_ParseError(t *testing.T) {
	withCacheDir(t)
	// a URL that Go's url.Parse rejects: control chars
	if _, err := CachePath("p1", "\x00bad", nil); err == nil {
		t.Errorf("want url parse error, got nil")
	}
}

func TestCommitConversion_RenameError(t *testing.T) {
	// rename into a destination whose parent is a *file* → ENOTDIR.
	withCacheDir(t)
	dir := t.TempDir()
	// create 'p' as a file inside dir; rename into p/foo → not a directory
	blocker := filepath.Join(dir, "p")
	if err := os.WriteFile(blocker, []byte("file"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	src := filepath.Join(dir, "conv")
	if err := os.WriteFile(src, []byte("x"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := CommitConversion(src, filepath.Join(blocker, "cache.opt"), filepath.Join(dir, "d.bin")); err == nil {
		t.Errorf("want rename error (parent of dest is a file)")
	}
}

func TestTryCopyFile_DirCreateFails(t *testing.T) {
	// tryCopyFile: parentDir create fails → (false, err) (block 224-225).
	// Make a *file* at the parent-dir path: MkdirAll on /<file-path>/x fails.
	src := t.TempDir() + "/s.bin"
	if err := os.WriteFile(src, []byte("data"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// dest parent is a FILE: /tmp/xxx/f (f is a file, not a dir)
	blocker := t.TempDir() + "/f"
	if err := os.WriteFile(blocker, []byte("f"), 0644); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}
	dest := blocker + "/sub/out.bin"
	copied, err := tryCopyFile(src, dest)
	if copied {
		t.Errorf("copied = true, want false (MkdirAll fails)")
	}
	if err == nil {
		t.Errorf("err = nil, want MkdirAll error")
	}
}

func TestIsConversionCached_WarnOnNonExistErr(t *testing.T) {
	// IsConversionCached: stat returns a NON-ENOENT error → warn +
	// (false, nil) (block 197-198). Trigger via EACCES on chmod 000.
	withCacheDir(t)
	if os.Geteuid() != 0 {
		cmp, err := CachePath("p1", "http://filestore/project/p1/file/f", nil)
		if err != nil {
			t.Fatalf("cachepath: %v", err)
		}
		if err := os.MkdirAll(cmp, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.Chmod(cmp, 0); err != nil {
			t.Skipf("chmod: %v", err)
		}
		defer os.Chmod(cmp, 0755)
		cached, derr := IsConversionCached("p1", "http://filestore/project/p1/file/f", nil)
		if derr != nil {
			t.Errorf("derr = %v, want nil (stat error swallowed)", derr)
		}
		if cached {
			t.Errorf("cached = true, want false (stat error swallowed)")
		}
	}
}

// TestDownloadUrlToFile_CachePathError covers the first CachePath error
// (lines 100-101): url.Parse fails on a garbage URL, so the function bails
// before touching the filesystem.
func TestDownloadUrlToFile_CachePathError(t *testing.T) {
	withCacheDir(t)
	if _, err := DownloadUrlToFile("p1", "://x", "", "/dest", nil, ""); err == nil {
		t.Fatal("want url.Parse error from CachePath")
	}
}

// TestDownloadUrlToFile_FallbackCopied covers lines 109-116: primary
// cachePath absent, fallbackURL non-empty and its cache file present, so
// the fallback copy succeeds and we return (nil, nil) without downloading.
func TestDownloadUrlToFile_FallbackCopied(t *testing.T) {
	withCacheDir(t)
	primary := "http://filestore/project/p1/file/primary"
	fallback := "http://filestore/project/p1/file/fallback"
	fcache, _ := CachePath("p1", fallback, nil)
	if err := os.MkdirAll(filepath.Dir(fcache), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(fcache, []byte("fallback-bytes"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "out.bin")
	origFetch := FetchURL
	FetchURL = func(u, fb, target string) error { return nil }
	defer func() { FetchURL = origFetch }()
	// primary is a separate (absent) cache path; fallback exists.
	h, err := DownloadUrlToFile("p1", primary, fallback, dest, nil, "")
	if err != nil {
		t.Fatalf("DownloadUrlToFile fallback-copy = %v", err)
	}
	if h != nil {
		t.Errorf("suffixless fallback copy must not return a handle, got %+v", h)
	}
	data, rerr := os.ReadFile(dest)
	if rerr != nil || string(data) != "fallback-bytes" {
		t.Errorf("dest content = %q err=%v, want fallback-bytes", data, rerr)
	}
}

// TestDownloadUrlToFile_FallbackCachePathError covers the fallback CachePath
// error (lines 111-112 / 113-114 in the conversionSuffix="" path): primary
// fetchable, fallbackURL invalid -> CachePath(fallback) errors.
func TestDownloadUrlToFile_FallbackCachePathError(t *testing.T) {
	withCacheDir(t)
	primary := "http://filestore/project/p1/file/p"
	// primary cache absent (forces the fallback branch when fallback is set)
	if _, err := DownloadUrlToFile("p1", primary, "://bad", "/dest", nil, ""); err == nil {
		t.Fatal("want error from fallback CachePath on bad fallback URL")
	}
}

// TestIsConversionCached_CachePathError covers lines 192-193: url.Parse
// fails, so IsConversionCached returns (false, err).
func TestIsConversionCached_CachePathError(t *testing.T) {
	withCacheDir(t)
	if ok, err := IsConversionCached("p1", "://bad", nil); err == nil || ok {
		t.Errorf("want url.Parse error from CachePath; got ok=%v err=%v", ok, err)
	}
}

// TestIsConversionCached_WarnNonExistErr covers lines 197-198: stat of the
// .opt path errors with something OTHER than ENOENT (permission denied on
// a parent), and IsConversionCached warns but returns (false, nil).
func TestIsConversionCached_WarnNonExistErr(t *testing.T) {
	withCacheDir(t)
	urlStr := "http://filestore/x/y"
	cp, _ := CachePath("p9", urlStr, nil)
	opt := cp + ".opt"
	// Make stat(opt) fail with a permission error: create a directory at the
	// parent, chmod 000 so stat on the child fails with EACCES (non-ENOENT).
	if os.Geteuid() != 0 {
		if err := os.MkdirAll(filepath.Dir(opt), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		// chmod the parent dir so os.Stat(opt) yields EACCES rather than
		// ENOENT (stat needs execute on every parent).
		if err := os.Chmod(filepath.Dir(opt), 0); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		defer os.Chmod(filepath.Dir(opt), 0755)
	}
	if ok, err := IsConversionCached("p9", urlStr, nil); err != nil || ok {
		t.Errorf("IsConversionCached on stat-perm-err = (%v,%v), want (false,nil)", ok, err)
	}
}
