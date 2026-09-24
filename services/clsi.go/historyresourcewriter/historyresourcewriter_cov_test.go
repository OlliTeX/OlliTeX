// historyresourcewriter_cov_test.go — whitebox coverage scenarios for the
// residual gaps (HANDOFF §10.9): fetchString retry/404, clsi-cache populate,
// draft/tikz write branches, nested-dir discovery/removal, raw-op conversion,
// and the error paths of the snapshot cache. Same package: unexported
// symbols are reachable.
package historyresourcewriter

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"clsi/draftmodemanager"
	clsierrors "clsi/errors"
	"clsi/metrics"
	"clsi/urlcache"

	"ollitex/go/libraries/fetchutils"
	"ollitex/go/libraries/oerror"
	otc "ollitex/go/libraries/otc"
)

// writeGZ gzips body to file (parent dirs created). 1:1 with Node gzip level 1.
func writeGZ(t *testing.T, file, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewWriterLevel(f, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gz.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// readSaved gunzips + JSON-decodes the history.json.gz a sync just saved.
func readSaved(t *testing.T, cacheKey string) map[string]any {
	t.Helper()
	blob, err := os.ReadFile(SnapshotPathNames(cacheKey).path)
	if err != nil {
		t.Fatalf("read saved snapshot: %v", err)
	}
	out := map[string]any{}
	if err := gunzipBytesThenUnmarshal(blob, &out); err != nil {
		t.Fatalf("saved snapshot is not JSON: %v", err)
	}
	return out
}

// gunzipBytesThenUnmarshal: gunzip a blob and JSON-decode it into out.
func gunzipBytesThenUnmarshal(blob []byte, out any) error {
	blob, err := gunzipBytes(blob)
	if err != nil {
		return err
	}
	return json.Unmarshal(blob, out)
}

func TestClearCacheRemovesCacheDir(t *testing.T) {
	_, _, cacheKey, _ := setup(t)
	sp := SnapshotPathNames(cacheKey)
	if err := os.MkdirAll(filepath.Join(sp.dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sp.dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ClearCache("p1", "u1", cacheKey)
	if _, err := os.Stat(sp.dir); !os.IsNotExist(err) {
		t.Fatalf("cache dir still present after ClearCache: %v", err)
	}
	// ENOENT path: clearing an already-absent dir is a silent no-op.
	ClearCache("p1", "u1", cacheKey)
}

func TestDedupeGlobalBlobsDedupPreservesOrder(t *testing.T) {
	got := dedupeGlobalBlobs([]string{"a", "b", "a"}, []string{"b", "c"})
	if strings.Join(got, ",") != "a,b,c" {
		t.Fatalf("dedupe = %v, want [a b c]", got)
	}
	if got := dedupeGlobalBlobs(nil, nil); len(got) != 0 {
		t.Fatalf("nil dedupe = %v, want empty", got)
	}
}

func TestChangesFromRawChangeOperations(t *testing.T) {
	changes, err := changesFromRawChangeOperations([][]map[string]any{
		{{"pathname": "a.txt", "file": map[string]any{"content": "x"}}},
		{{"pathname": "a.txt", "newPathname": "b.txt"}},
		{{}},
	})
	if err != nil {
		t.Fatalf("valid ops: %v", err)
	}
	if len(changes) != 3 {
		t.Fatalf("changes = %d, want 3", len(changes))
	}
	if _, ok := changes[0].Operations[0].(*otc.AddFileOperation); !ok {
		t.Fatalf("op[0] = %T, want *AddFileOperation", changes[0].Operations[0])
	}
	if _, ok := changes[1].Operations[0].(*otc.MoveFileOperation); !ok {
		t.Fatalf("op[1] = %T, want *MoveFileOperation", changes[1].Operations[0])
	}
	if !changes[2].Operations[0].IsNoOp() {
		t.Fatalf("op[2] should be a no-op")
	}
	// An op that does not materialise is an error (mustFromRaw parity).
	if _, err := changesFromRawChangeOperations([][]map[string]any{
		{{"bogus": 1}},
	}); err == nil {
		t.Fatalf("want error for invalid raw op")
	}
	if changes, err := changesFromRawChangeOperations(nil); err != nil || len(changes) != 0 {
		t.Fatalf("nil input: %v %d, want nil 0", err, len(changes))
	}
}

func TestGetBlobURLBranches(t *testing.T) {
	setup(t)
	const h = "deadbeef00000000000000000000000000000000"
	u1 := newHRWBlobStore("hist1", "/blobs", "", []string{h}).getBlobURL(h)
	if !strings.HasSuffix(u1, "/blobs/"+h) {
		t.Fatalf("filestore-blob-prefix branch: %s", u1)
	}
	u2 := newHRWBlobStore("hist1", "", "variantX", []string{h}).getBlobURL(h)
	if !strings.Contains(u2, "/variant/variantX/hash/"+h) {
		t.Fatalf("variant branch: %s", u2)
	}
	u3 := newHRWBlobStore("hist1", "", "", []string{h}).getBlobURL(h)
	if !strings.HasSuffix(u3, "/history/global/hash/"+h) {
		t.Fatalf("global-hash branch: %s", u3)
	}
	u4 := newHRWBlobStore("hist1", "", "", []string{"other"}).getBlobURL(h)
	if !strings.HasSuffix(u4, "/history/project/hist1/hash/"+h) {
		t.Fatalf("project-hash branch: %s", u4)
	}
}

// rfeStatus builds a *fetchutils.RequestFailedError with a live OError core
// (the 500 path logs err.Error(), so the embedded OError must not be nil).
func rfeStatus(status int) error {
	oe := oerror.New("request failed", map[string]any{"status": status, "url": "x", "method": "GET"})
	return &fetchutils.RequestFailedError{OError: oe, Url: "x", Method: "GET", Status: status}
}

func TestFetchStringSuccess(t *testing.T) {
	setup(t)
	t.Cleanup(func() { ResetSeams() })
	calls := 0
	FetchStringFunc = func(ctx context.Context, url string, opts ...*fetchutils.Options) (string, error) {
		calls++
		return "blob", nil
	}
	bs := newHRWBlobStore("hist1", "", "", nil)
	s, err := bs.fetchString(context.Background(), "x")
	if err != nil || s != "blob" || calls != 1 {
		t.Fatalf("fetch = (%q, %v), %d calls — want (blob, nil), 1", s, err, calls)
	}
}

func TestFetchString404NoRetry(t *testing.T) {
	setup(t)
	t.Cleanup(func() { ResetSeams() })
	calls := 0
	FetchStringFunc = func(ctx context.Context, url string, opts ...*fetchutils.Options) (string, error) {
		calls++
		return "", rfeStatus(404)
	}
	bs := newHRWBlobStore("hist1", "", "", nil)
	_, err := bs.fetchString(context.Background(), "x")
	if calls != 1 {
		t.Fatalf("404 must not retry: %d calls", calls)
	}
	if !clsierrors.IsNotFoundError(err) {
		t.Fatalf("404 error = %v, want NotFoundError", err)
	}
}

func TestFetchStringRetryThenSuccess(t *testing.T) {
	setup(t)
	t.Cleanup(func() { ResetSeams() })
	calls := 0
	FetchStringFunc = func(ctx context.Context, url string, opts ...*fetchutils.Options) (string, error) {
		calls++
		if calls < 3 {
			return "", rfeStatus(500)
		}
		return "finally", nil
	}
	bs := newHRWBlobStore("hist1", "", "", nil)
	s, err := bs.fetchString(context.Background(), "x")
	if err != nil || s != "finally" || calls != 3 {
		t.Fatalf("retry: (%q, %v), %d calls — want success on 3rd", s, err, calls)
	}
}

func TestFetchStringExhausted(t *testing.T) {
	setup(t)
	t.Cleanup(func() { ResetSeams() })
	calls := 0
	FetchStringFunc = func(ctx context.Context, url string, opts ...*fetchutils.Options) (string, error) {
		calls++
		return "", rfeStatus(500)
	}
	bs := newHRWBlobStore("hist1", "", "", nil)
	_, err := bs.fetchString(context.Background(), "x")
	if calls != 3 {
		t.Fatalf("exhausted attempts = %d, want 3", calls)
	}
	if err == nil {
		t.Fatalf("exhausted fetch must return the last error")
	}
}

func TestFetchStringCtxCancelled(t *testing.T) {
	setup(t)
	t.Cleanup(func() { ResetSeams() })
	FetchStringFunc = func(ctx context.Context, url string, opts ...*fetchutils.Options) (string, error) {
		return "", rfeStatus(500)
	}
	bs := newHRWBlobStore("hist1", "", "", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := bs.fetchString(ctx, "x")
	if err != context.Canceled {
		t.Fatalf("cancelled ctx error = %v, want context.Canceled", err)
	}
}

func TestReservableFromOptCacheDirect(t *testing.T) {
	setup(t)
	t.Cleanup(func() { ResetSeams() })
	IsConversionCached = func(projectID, urlStr string, lastModified *time.Time) (bool, error) {
		return false, nil
	}
	// "nope.png" is not in the snapshot (file==nil); "hollow.png" is present
	// but has no hash — both must be unreservable.
	sn, err := otc.SnapshotFromRaw(map[string]any{"files": map[string]any{
		"hollow.png": map[string]any{"byteLength": float64(5)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	bs := newHRWBlobStore("hist1", "", "", nil)
	if reservableFromOptCache("p1", "nope.png", sn, bs) {
		t.Fatalf("missing file must not be reservable")
	}
	if reservableFromOptCache("p1", "hollow.png", sn, bs) {
		t.Fatalf("hashless png must not be reservable")
	}
}

func TestLoadSlowPngListVariants(t *testing.T) {
	_, _, cacheKey, _ := setup(t)
	sp := SnapshotPathNames(cacheKey)
	if err := os.MkdirAll(sp.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// no file (ENOENT: silent, empty list)
	if got := loadSlowPngList(cacheKey); len(got) != 0 {
		t.Fatalf("missing list = %v, want empty", got)
	}
	// invalid JSON (warn, empty list)
	if err := os.WriteFile(sp.slowPngPath, []byte("notjson"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadSlowPngList(cacheKey); len(got) != 0 {
		t.Fatalf("bad JSON list = %v, want empty", got)
	}
	// JSON null — unmarshals without error but to a nil list
	if err := os.WriteFile(sp.slowPngPath, []byte("null"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadSlowPngList(cacheKey); len(got) != 0 {
		t.Fatalf("null list = %v, want empty", got)
	}
}

func TestSaveSnapshotTmpCollision(t *testing.T) {
	_, _, cacheKey, _ := setup(t)
	sp := SnapshotPathNames(cacheKey)
	if err := os.MkdirAll(sp.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A stale tmp file: the O_CREATE|O_EXCL open must fail.
	if err := os.WriteFile(sp.path+"~", []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := saveSnapshot(cacheKey, map[string]any{"files": map[string]any{}}, 0, nil, nil, false); err == nil {
		t.Fatalf("stale tmp must fail the save (Node: writeFile flag 'wx')")
	}
}

func TestDeleteResyncSnapshotError(t *testing.T) {
	_, _, cacheKey, _ := setup(t)
	sp := SnapshotPathNames(cacheKey)
	// A directory at the resync path: os.Remove fails (ENOTEMPTY) -> warn path.
	if err := os.MkdirAll(filepath.Join(sp.resyncPath, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sp.resyncPath) })
	deleteResyncSnapshot("p1", "u1", cacheKey)
}

func TestDiscoverAndRemoveExtraneousDirect(t *testing.T) {
	_, _, _, compileDir := setup(t)
	// keep/kept.txt          resource (kept, folder kept in use)
	// orphan/deeper/         extraneous empty dirs (removed, entries removed)
	// trash.txt              extraneous file (removed)
	// promoted/inner.txt     dir where the snapshot has a FILE (promoted)
	// link                   symlink (blocked dirent removed at discovery)
	if err := os.MkdirAll(filepath.Join(compileDir, "keep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(compileDir, "keep/kept.txt"), []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(compileDir, "orphan/deeper"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(compileDir, "trash.txt"), []byte("t"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(compileDir, "promoted"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(compileDir, "promoted/inner.txt"), []byte("i"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("keep", filepath.Join(compileDir, "link")); err != nil {
		t.Fatal(err)
	}

	IsExtraneousFile = func(p string) bool { return true }
	t.Cleanup(func() { ResetSeams() })

	entries := &entryList{isDir: map[string]bool{}}
	if err := discoverExistingEntries(compileDir, ".", entries); err != nil {
		t.Fatalf("discover: %v", err)
	}
	resource := map[string]struct{}{"keep/kept.txt": {}, "promoted": {}}
	if err := removeExtraneousEntries(compileDir, func(p string) bool {
		_, ok := resource[p]
		return ok
	}, entries); err != nil {
		t.Fatalf("removeExtraneous: %v", err)
	}

	// symlink blocked at discovery
	if _, err := os.Lstat(filepath.Join(compileDir, "link")); !os.IsNotExist(err) {
		t.Fatalf("symlink not removed: %v", err)
	}
	// extraneous dirs + file gone
	for _, p := range []string{"orphan/deeper", "orphan", "trash.txt"} {
		if _, err := os.Lstat(filepath.Join(compileDir, p)); !os.IsNotExist(err) {
			t.Fatalf("extraneous %q not removed", p)
		}
	}
	// promoted dir + its child gone
	if _, err := os.Lstat(filepath.Join(compileDir, "promoted/inner.txt")); !os.IsNotExist(err) {
		t.Fatalf("promoted child not removed")
	}
	if _, err := os.Lstat(filepath.Join(compileDir, "promoted")); !os.IsNotExist(err) {
		t.Fatalf("promoted dir not removed")
	}
	// kept file + folder survive
	if _, err := os.Lstat(filepath.Join(compileDir, "keep/kept.txt")); err != nil {
		t.Fatalf("kept file missing: %v", err)
	}
	if st, err := os.Lstat(filepath.Join(compileDir, "keep")); err != nil || !st.IsDir() {
		t.Fatalf("keep folder missing")
	}
	// entry bookkeeping: isDirOf + removed entries no longer present
	if !entries.isDirOf("keep") {
		t.Fatalf("isDirOf(keep) = false")
	}
	if entries.isDirOf("orphan") || entries.has("orphan") {
		t.Fatalf("orphan not removed from entry bookkeeping")
	}
	if entries.isDirOf("promoted") {
		t.Fatalf("promoted not removed from entry bookkeeping")
	}
}

func TestDiscoverUnexpectedDirEntry(t *testing.T) {
	_, _, _, compileDir := setup(t)
	// A FIFO is neither a dir, a plain file, a symlink/device/socket: it hits
	// the "unexpected dir entry" error.
	if err := syscall.Mkfifo(filepath.Join(compileDir, "fifo"), 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	entries := &entryList{isDir: map[string]bool{}}
	err := discoverExistingEntries(compileDir, ".", entries)
	if err == nil || !strings.Contains(err.Error(), "unexpected dir entry") {
		t.Fatalf("want unexpected-dir-entry error, got: %v", err)
	}
}

func TestDiscoverReadDirError(t *testing.T) {
	_, _, _, compileDir := setup(t)
	entries := &entryList{isDir: map[string]bool{}}
	err := discoverExistingEntries(compileDir, "sub/dir/does/not/exist", entries)
	if err == nil {
		t.Fatalf("ReadDir error expected for missing subdir")
	}
}

func TestEnsureHasParentFolderDirect(t *testing.T) {
	_, _, _, compileDir := setup(t)
	e := &entryList{isDir: map[string]bool{".": true}}
	if err := ensureHasParentFolder(compileDir, "a/b/c.txt", e); err != nil {
		t.Fatalf("ensureHasParentFolder: %v", err)
	}
	if !e.has("a") || !e.has("a/b") || !e.isDirOf("a") || !e.isDirOf("a/b") {
		t.Fatalf("entry bookkeeping incomplete: %v", e.isDir)
	}
	st, err := os.Stat(filepath.Join(compileDir, "a/b"))
	if err != nil || !st.IsDir() {
		t.Fatalf("a/b dir not created: %v", err)
	}
	// Second call: every parent already in the map -> no-op success.
	if err := ensureHasParentFolder(compileDir, "a/b/c.txt", e); err != nil {
		t.Fatalf("re-ensure: %v", err)
	}
}

// --- sync-level scenario coverage (HANDOFF §10.9) ---------------------------

// seedHistoryGz pre-seeds the history.json.gz cache for a cacheKey with the
// given JSON body (gunzipped + JSON-parsed by loadSnapshotFromFile).
func seedHistoryGz(t *testing.T, cacheKey, body string) {
	t.Helper()
	writeGZ(t, SnapshotPathNames(cacheKey).path, body)
}

// TestSyncMissingUpdatesNoRawSeededRemote: local + resync snapshots fall
// behind remote -> MissingUpdates rethrow carrying the accumulated
// baseHistoryVersion (covers the loadSnapshot tracking loop + maxInt).
func TestSyncMissingUpdatesNoRawSeededRemote(t *testing.T) {
	projectID, userID, cacheKey, compileDir := setup(t)
	m := &fakeSeams{}
	installFakeSeams(t, m)

	seedHistoryGz(t, cacheKey, `{"rawSnapshot": {"files": {}}, "localBaseVersion": 5}`)
	writeGZ(t, SnapshotPathNames(cacheKey).resyncPath,
		`{"rawSnapshot": {"files": {}}, "localBaseVersion": 2}`)

	overrides := map[string]any{
		"noRawSnapshot":      struct{}{},
		"baseHistoryVersion": 10,
	}
	_, _, err := syncOnce(t, m, projectID, userID, compileDir, overrides)
	if err == nil {
		t.Fatalf("want MissingUpdates error")
	}
	mu, ok := err.(*clsierrors.MissingUpdatesError)
	if !ok {
		t.Fatalf("err = %v, want *MissingUpdatesError", err)
	}
	if v := mu.Info["baseHistoryVersion"]; v != 5 {
		t.Fatalf("info baseHistoryVersion = %v, want 5 (max of candidates)", v)
	}
}

// TestSyncPopulateClsiCacheEndToEnd: clsi-cache download populates the
// resync snapshot, which is loaded (fullSync) and then deleted.
func TestSyncPopulateClsiCacheEndToEnd(t *testing.T) {
	projectID, userID, cacheKey, compileDir := setup(t)
	m := &fakeSeams{}
	installFakeSeams(t, m)

	DownloadHistorySnapshot = func(projectID, userID, dir string) (bool, error) {
		writeGZ(t, filepath.Join(dir, "history-resync.json.gz"),
			`{"rawSnapshot": {"files": {"main.tex": {"content": "hello"}}}, "localBaseVersion": 0}`)
		return true, nil
	}

	_, _, err := syncOnce(t, m, projectID, userID, compileDir, map[string]any{
		"populate": true,
	})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if content, err := os.ReadFile(filepath.Join(compileDir, "main.tex")); err != nil || string(content) != "hello" {
		t.Fatalf("main.tex = %q: %v, want hello", content, err)
	}
	// The resync snapshot is deleted after a successful full sync.
	if _, err := os.Stat(SnapshotPathNames(cacheKey).resyncPath); !os.IsNotExist(err) {
		t.Fatalf("resync snapshot not deleted: %v", err)
	}
	// history.json.gz was saved from the full sync.
	saved := readSaved(t, cacheKey)
	lv, _ := saved["localBaseVersion"].(float64)
	if lv != 0 {
		t.Fatalf("saved localBaseVersion = %v, want 0", saved["localBaseVersion"])
	}
}

// TestSyncPopulateNoOkDownload: clsi-cache reports nothing downloaded
// (the 'needs full sync' MissingUpdates path), then the remote fallback.
func TestSyncPopulateNoOkDownload(t *testing.T) {
	projectID, userID, cacheKey, compileDir := setup(t)
	m := &fakeSeams{}
	installFakeSeams(t, m)
	DownloadHistorySnapshot = func(projectID, userID, dir string) (bool, error) {
		return false, nil
	}
	if _, _, err := syncOnce(t, m, projectID, userID, compileDir, map[string]any{
		"populate": true,
	}); err != nil {
		t.Fatalf("fallback sync: %v", err)
	}
	if _, err := os.Stat(SnapshotPathNames(cacheKey).path); err != nil {
		t.Fatalf("saved snapshot: %v", err)
	}
}

// TestSyncPopulateDownloadError: a clsi-cache failure other than
// MissingUpdates/ENOENT logs 'cannot download from clsi-cache' and the
// remote fallback is used.
func TestSyncPopulateDownloadError(t *testing.T) {
	projectID, userID, cacheKey, compileDir := setup(t)
	m := &fakeSeams{}
	installFakeSeams(t, m)
	DownloadHistorySnapshot = func(projectID, userID, dir string) (bool, error) {
		return true, fmt.Errorf("boom")
	}
	if _, _, err := syncOnce(t, m, projectID, userID, compileDir, map[string]any{
		"populate": true,
	}); err != nil {
		t.Fatalf("fallback sync: %v", err)
	}
	if _, err := os.Stat(SnapshotPathNames(cacheKey).path); err != nil {
		t.Fatalf("saved snapshot: %v", err)
	}
}

// TestSyncCorruptLocalHistory: a bad (non-gzip) local history.json.gz is a plain
// error (neither MissingUpdates nor ENOENT) -> the loadSnapshot warn path and
// the sync-level 'bad local history state' warn.
func TestSyncCorruptLocalHistory(t *testing.T) {
	projectID, userID, cacheKey, compileDir := setup(t)
	m := &fakeSeams{}
	installFakeSeams(t, m)
	if err := os.MkdirAll(SnapshotPathNames(cacheKey).dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(SnapshotPathNames(cacheKey).path, []byte("notgzip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := syncOnce(t, m, projectID, userID, compileDir, nil); err != nil {
		t.Fatalf("corrupt-local fallback: %v", err)
	}
	// Corrupt resync path too: second candidate plain error.
	if err := os.WriteFile(SnapshotPathNames(cacheKey).resyncPath, []byte("notgzip"), 0o644); err != nil {
		t.Fatal(err)
	}
	ClearCache(projectID, userID, cacheKey)
}

// TestSyncDraftPrefixAndDirty: draft mode writes the root resource with the
// draft-mode prefix and persists the dirty list in the snapshot cache.
func TestSyncDraftPrefixAndDirty(t *testing.T) {
	projectID, userID, cacheKey, compileDir := setup(t)
	m := &fakeSeams{}
	installFakeSeams(t, m)

	// A normal sync first (baseline).
	if _, _, err := syncResult(t, m, projectID, userID, compileDir, nil); err != nil {
		t.Fatalf("sync1 (normal): %v", err)
	}

	// draft: the root resource is written with the draft prefix and marked
	// dirty (persisted into the snapshot).
	overrides := map[string]any{
		"files": map[string]any{"main.tex": map[string]any{"content": "hello"}},
		"draft": true,
	}
	if _, _, err := syncResult(t, m, projectID, userID, compileDir, overrides); err != nil {
		t.Fatalf("sync2 (draft): %v", err)
	}
	content, err := os.ReadFile(filepath.Join(compileDir, "main.tex"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != draftmodemanager.PREFIX+"hello" {
		t.Fatalf("draft content = %q", string(content))
	}
	// The dirty list is persisted to the snapshot cache.
	blob, err := os.ReadFile(SnapshotPathNames(cacheKey).path)
	if err != nil {
		t.Fatalf("snapshot not saved: %v", err)
	}
	var savedRaw struct {
		Dirty []string `json:"dirty"`
	}
	if err := gunzipBytesThenUnmarshal(blob, &savedRaw); err != nil {
		t.Fatalf("saved snapshot: %v", err)
	}
	if len(savedRaw.Dirty) < 1 || savedRaw.Dirty[0] != "main.tex" {
		t.Fatalf("dirty = %v, want [main.tex ...]", savedRaw.Dirty)
	}
}

// TestTikzWriteOutputFileIfNeeded: output.tex present -> the tikz manager is
// invoked for the root resource (and the output.tex content is written to the
// compile dir); its rejection rides the same 'write failed' tag.
func TestTikzWriteOutputFileIfNeeded(t *testing.T) {
	projectID, userID, cacheKey, compileDir := setup(t)
	m := &fakeSeams{}
	installFakeSeams(t, m)

	var seenHasOutput bool
	var seenContent string
	WriteOutputFileIfNeeded = func(compileDir string, hasOutputTex bool, content string) error {
		seenHasOutput = hasOutputTex
		seenContent = content
		return nil
	}
	files := map[string]any{
		"main.tex":   map[string]any{"content": "hello"},
		"output.tex": map[string]any{"content": "tikz"},
	}
	if _, _, err := syncResult(t, m, projectID, userID, compileDir, map[string]any{"files": files}); err != nil {
		t.Fatalf("tikz sync: %v", err)
	}
	if !seenHasOutput {
		t.Fatalf("writeOutputFileIfNeeded not called with hasOutputTex=true")
	}
	if seenContent != "hello" {
		t.Fatalf("passthrough content = %q, want hello", seenContent)
	}
	// output.tex is written to the compile dir.
	if _, err := os.Stat(filepath.Join(compileDir, "output.tex")); err != nil {
		t.Fatalf("output.tex missing: %v", err)
	}

	// Tikz rejection -> "write failed" tag on the error. A fresh sync (the
	// cached snapshot was cleared -> fullSync, so the paths are re-written)
	// surfaces the tikz rejection.
	WriteOutputFileIfNeeded = func(compileDir string, hasOutputTex bool, content string) error {
		return fmt.Errorf("tikz failed")
	}
	ClearCache(projectID, userID, cacheKey)
	_, _, err := syncResult(t, m, projectID, userID, compileDir, map[string]any{"files": files})
	if !hasWriteFailedTag(err) || !containsInChain(err, "tikz failed") {
		t.Fatalf("want 'write failed' tag carrying 'tikz failed', got: %v", err)
	}
}

// hasWriteFailedTag: the top-level 'write failed' tagged OError.
func hasWriteFailedTag(err error) bool {
	return err != nil && strings.Contains(err.Error(), "write failed")
}

// containsInChain reports whether any message in the Unwrap chain matches.
func containsInChain(err error, needle string) bool {
	for e := err; e != nil; {
		if e.Error() == needle || strings.Contains(e.Error(), needle) {
			return true
		}
		if u, ok := e.(interface{ Unwrap() error }); ok {
			e = u.Unwrap()
		} else {
			return false
		}
	}
	return false
}

// TestNoContentNoHash: a file with neither inline content nor a hash is
// "unexpected file without content and hash".
func TestNoContentNoHash(t *testing.T) {
	projectID, userID, _, compileDir := setup(t)
	m := &fakeSeams{}
	installFakeSeams(t, m)
	files := map[string]any{
		"main.tex": map[string]any{"content": "hello"},
		"mystery":  map[string]any{"byteLength": float64(42)},
	}
	_, _, err := syncResult(t, m, projectID, userID, compileDir, map[string]any{"files": files})
	if !hasWriteFailedTag(err) || !containsInChain(err, "without content and hash") {
		t.Fatalf("want 'write failed' tag carrying 'unexpected file without content and hash', got: %v", err)
	}
}

// TestCreateProjectDirError: a lazily-created project cache dir error is
// surfaced as a "write failed" tag.
func TestCreateProjectDirError(t *testing.T) {
	projectID, userID, _, compileDir := setup(t)
	m := &fakeSeams{}
	installFakeSeams(t, m)
	CreateProjectDir = func(projectID string) error { return fmt.Errorf("no cache dir") }
	files := map[string]any{
		"img.png": map[string]any{"hash": pngHash, "byteLength": float64(42)},
	}
	_, _, err := syncResult(t, m, projectID, userID, compileDir, map[string]any{"files": files})
	if err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("want 'write failed' (createProjectDir), got: %v", err)
	}
}

// TestDownloadContinueOnError: a failed blob download logs + metrics, and the
// loop continues to the next resource.
func TestDownloadContinueOnError(t *testing.T) {
	projectID, userID, _, compileDir := setup(t)
	m := &fakeSeams{}
	installFakeSeams(t, m)
	before := metrics.DownloadFailed.Get()
	DownloadUrlToFile = func(projectID, urlStr, fallbackURL, destPath string,
		lastModified *time.Time, conversionSuffix string,
	) (*urlcache.ConversionHandle, error) {
		if strings.Contains(urlStr, pngHash) {
			return nil, fmt.Errorf("download failed")
		}
		return nil, os.WriteFile(destPath, []byte("bytes"), 0o644)
	}
	files := map[string]any{
		"a.png":    map[string]any{"hash": pngHash, "byteLength": float64(42)},
		"main.tex": map[string]any{"content": "hello"},
	}
	res, _, err := syncOnce(t, m, projectID, userID, compileDir, map[string]any{"files": files})
	if err != nil {
		t.Fatalf("loop must continue after a failed download: %v", err)
	}
	if got := metrics.DownloadFailed.Get(); got < before+1 {
		t.Fatalf("download-failed counter = %d, want >= %d", got, before+1)
	}
	if res.ResourceList == nil {
		t.Fatalf("resource list empty")
	}
	// main.tex is still written even though a.png's download failed.
	content, err := os.ReadFile(filepath.Join(compileDir, "main.tex"))
	if err != nil || string(content) != "hello" {
		t.Fatalf("main.tex = %q: %v", content, err)
	}
}

// TestPngConvertErrorAndCommit: convert + commit failures are swallowed (the
// original PNG is kept); commit failure increments download-failed.
func TestPngConvertErrorAndCommit(t *testing.T) {
	projectID, userID, cacheKey, compileDir := setup(t)
	m := &fakeSeams{}
	installFakeSeams(t, m)
	if err := SaveSlowPngList(cacheKey, []string{"fig.png"}); err != nil {
		t.Fatal(err)
	}
	PngConvert = func(projectID, cacheProjectDir string, relativePaths []string,
		stats, timings map[string]any,
	) error {
		return fmt.Errorf("convert failed")
	}
	CommitConversion = func(conversionPath, cachePath, destPath string) error {
		return fmt.Errorf("commit failed")
	}
	before := metrics.DownloadFailed.Get()
	_, _, err := syncOnce(t, m, projectID, userID, compileDir, map[string]any{
		"png2pdf": true,
	})
	if err != nil {
		t.Fatalf("png2pdf errors must be swallowed: %v", err)
	}
	if got := metrics.DownloadFailed.Get(); got < before+1 {
		t.Fatalf("commit failure must increment download-failed: %d vs %d", got, before)
	}
	// The original fig.png is served, but through the conversion suffix path.
	found := false
	for _, d := range m.downloads {
		s, ok := d["suffix"].(string)
		if ok && s != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("conversion-suffixed download expected")
	}
}

// TestGzipWriteFailure: the gzip writer error path in saveSnapshot cleans up
// the temp file.
func TestGzipWriteFailure(t *testing.T) {
	_, _, cacheKey, _ := setup(t)
	sp := SnapshotPathNames(cacheKey)
	if err := os.MkdirAll(sp.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Make the tmp path a directory: open succeeds, write fails.
	if err := os.Mkdir(sp.path+"~", 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sp.path + "~")
	err := saveSnapshot(cacheKey, map[string]any{"files": map[string]any{}}, 0, nil, nil, false)
	if err == nil {
		t.Fatalf("want error when the tmp path is a directory")
	}
}
