// historyresourcewriter_cov2_test.go — final coverage scenarios: the
// defaultPngConvert bridge (stats-on-success / timings-always semantics), the
// incremental (non-full-sync) branch of SyncResourcesToDisk, and the residual
// error paths of the snapshot cache.
package historyresourcewriter

import (
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"clsi/config"
	"clsi/errors"
	"clsi/png2pdf"
)

// TestDefaultPngConvertBridge: defaultPngConvert records the png2pdf timing
// on every completion (the Node timer.done() runs before the throw) and the
// stats count only on success.
func TestDefaultPngConvertBridge(t *testing.T) {
	setup(t)
	t.Cleanup(func() { png2pdf.RunFunc = nil })

	// Disabled (the env default under setup()): ConvertPngFilesInCacheDir
	// returns nil immediately; stats + timings both written, no error.
	stats, timings := map[string]any{}, map[string]any{}
	if err := defaultPngConvert("p1", "cachedir", []string{"a.png"}, stats, timings); err != nil {
		t.Fatalf("disabled bridge: %v", err)
	}
	if stats["png2pdf"] != 0 {
		t.Fatalf("disabled stats png2pdf = %v, want 0", stats["png2pdf"])
	}
	if _, ok := timings["png2pdf"]; !ok {
		t.Fatalf("timings png2pdf not recorded")
	}

	// Enable the env knob and re-read the config singleton.
	t.Setenv("ENABLE_PNG2PDF_CONVERSIONS", "true")
	config.ForTest()
	if !png2pdf.IsEnabled() {
		t.Skipf("png2pdf unexpectedly disabled in test env")
	}

	// Runner error: timings written, stats NOT written, error returned.
	png2pdf.RunFunc = func(projectID string, command []string, directory, image string,
		timeout int64, environment map[string]string, compileGroup, cwd string,
	) (png2pdf.Output, error) {
		return png2pdf.Output{}, fmt.Errorf("docker boom")
	}
	st, tm := map[string]any{}, map[string]any{}
	if err := defaultPngConvert("p1", "cachedir", []string{"a.png"}, st, tm); err == nil {
		t.Fatalf("want conversion error from runner error")
	}
	if st["png2pdf"] != nil {
		t.Fatalf("stats must NOT be written on error: %v", st["png2pdf"])
	}
	if _, ok := tm["png2pdf"]; !ok {
		t.Fatalf("timings must be written on error")
	}

	// No runner configured: the "not configured" path.
	png2pdf.RunFunc = nil
	st, tm = map[string]any{}, map[string]any{}
	if err := defaultPngConvert("p1", "cachedir", []string{"a.png"}, st, tm); err == nil {
		t.Fatalf("want 'runner not configured' error")
	}

	// Success: the converted PNGs counted from the "Converted X to PDF" lines.
	png2pdf.RunFunc = func(projectID string, command []string, directory, image string,
		timeout int64, environment map[string]string, compileGroup, cwd string,
	) (png2pdf.Output, error) {
		return png2pdf.Output{Stdout: "Converted a.png to PDF\nConverted b.png to PDF\n"}, nil
	}
	st, tm = map[string]any{}, map[string]any{}
	if err := defaultPngConvert("p1", "cachedir", []string{"a.png", "b.png"}, st, tm); err != nil {
		t.Fatalf("success bridge: %v", err)
	}
	if st["png2pdf"] != 2 {
		t.Fatalf("stats png2pdf = %v, want 2", st["png2pdf"])
	}
	if _, ok := tm["png2pdf"]; !ok {
		t.Fatalf("timings png2pdf missing on success")
	}
}

// seedLocalSnapshot gzips the given JSON body to history.json.gz (index 0,
// so a load of the local snapshot is fullSync=false).
func seedLocalSnapshot(t *testing.T, cacheKey, body string) {
	t.Helper()
	writeGZ(t, SnapshotPathNames(cacheKey).path, body)
}

// TestSyncIncrementalApplyOps: the non-full-sync (incremental) branch —
// dirty-list dedupe, op-derived dedupe (add / move / remove / noOp),
// restored-deleted handling, and the post-apply snapshot re-save with the
// advanced base history version.
func TestSyncIncrementalApplyOps(t *testing.T) {
	projectID, userID, cacheKey, compileDir := setup(t)
	m := &fakeSeams{}
	installFakeSeams(t, m)

	// png2pdf same as the request (no mode change). fig.png is 2 MB (above
	// the 1 MB minimum) but is not on the slow list, so it is served raw and
	// NOT converted here.
	seedLocalSnapshot(t, cacheKey, `{
		"rawSnapshot": {"files": {
			"main.tex": {"content": "hello"},
			"fig.png": {"hash": "`+pngHash+`", "byteLength": 2097152}
		}},
		"localBaseVersion": 0,
		"dirty": ["main.tex"],
		"png2pdf": true
	}`)
	// fig.png already on disk (restore-deleted skips it; it is not re-written).
	if err := os.WriteFile(filepath.Join(compileDir, "fig.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	// baseHistoryVersion (request) 0, localBaseVersion 0 -> changeStart 0 (the
	// whole window). Ops: add added.tex, move main.tex->renamed.tex, remove
	// renamed.tex, and a no-op op.
	changes := [][]map[string]any{
		{{"pathname": "added.tex", "file": map[string]any{"content": "added"}}},
		{{"pathname": "main.tex", "newPathname": "renamed.tex"}},
		{{"pathname": "renamed.tex", "newPathname": ""}},
		{{}},
	}
	res, _, err := syncOnce(t, m, projectID, userID, compileDir, map[string]any{"changes": changes})
	if err != nil {
		t.Fatalf("incremental sync: %v", err)
	}
	// baseHistoryVersion = localBaseVersion + len(changes).
	if res.BaseHistoryVersion != 4 {
		t.Fatalf("result baseHistoryVersion = %d, want 4", res.BaseHistoryVersion)
	}
	// added.tex is written (an AddFileOperation made it dirty).
	if c, err := os.ReadFile(filepath.Join(compileDir, "added.tex")); err != nil || string(c) != "added" {
		t.Fatalf("added.tex = %q: %v, want 'added'", c, err)
	}
	// main.tex is moved + removed: neither it nor renamed.tex is written.
	if _, err := os.Stat(filepath.Join(compileDir, "renamed.tex")); !os.IsNotExist(err) {
		t.Fatalf("renamed.tex must not be written after the remove op")
	}
	// The incremental sync re-saved the snapshot (len(changes) > 0).
	saved := readSaved(t, cacheKey)
	if lv, _ := saved["localBaseVersion"].(float64); lv != 4 {
		t.Fatalf("saved localBaseVersion = %v, want 4", saved["localBaseVersion"])
	}
}

// TestSyncIncrementalModeChange: the non-full-sync branch with a png2pdf mode
// change (seeded false -> request true). The mode-change loop re-serves every
// PNG, the slow-list PNG is attempted (not opt-cached -> added to the changed
// set), downloaded through the conversion path and committed; a missing root
// resource is restored.
func TestSyncIncrementalModeChange(t *testing.T) {
	projectID, userID, cacheKey, compileDir := setup(t)
	m := &fakeSeams{}
	installFakeSeams(t, m)

	// main.tex is in the raw snapshot but NOT on disk -> restored by the
	// restored-deleted loop. fig.png is on the slow list (2 MB, converted) and
	// there is no .opt cache entry, so it is attempted.
	seedLocalSnapshot(t, cacheKey, `{
		"rawSnapshot": {"files": {
			"main.tex": {"content": "hello"},
			"fig.png": {"hash": "`+pngHash+`", "byteLength": 2097152}
		}},
		"localBaseVersion": 0,
		"dirty": [],
		"png2pdf": false
	}`)
	if err := os.WriteFile(filepath.Join(compileDir, "fig.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveSlowPngList(cacheKey, []string{"fig.png"}); err != nil {
		t.Fatal(err)
	}
	// Request png2pdf (default true) vs seeded false -> mode change.
	_, _, err := syncOnce(t, m, projectID, userID, compileDir, nil)
	if err != nil {
		t.Fatalf("incremental mode-change sync: %v", err)
	}
	// main.tex is restored (it was missing on disk).
	if c, rerr := os.ReadFile(filepath.Join(compileDir, "main.tex")); rerr != nil || string(c) != "hello" {
		t.Fatalf("main.tex = %q: %v, want hello (restored)", c, rerr)
	}
	// fig.png is opt-not-cached and in the slow list: it is downloaded through
	// the conversion path (cacheKey suffix) and committed once.
	converted := false
	for _, d := range m.downloads {
		if d["suffix"] != "" && d["destPath"] == filepath.Join(compileDir, "fig.png") {
			converted = true
		}
	}
	if !converted {
		t.Fatalf("fig.png must be downloaded through the conversion path (opt not cached): %v", m.downloads)
	}
	if m.committed != 1 {
		t.Fatalf("commits = %d, want 1", m.committed)
	}
	// mode change alone triggers the re-save even with no changes.
	if v, _ := readSaved(t, cacheKey)["png2pdf"].(bool); !v {
		t.Fatalf("saved png2pdf = %v, want true (re-saved on mode change)", v)
	}
}

// TestSaveSlowPngListNilAndErrors: nil slice persists as [] ; a tmp path that
// is a directory fails the write; a dir-at-path fails the read (non-ENOENT).
func TestSaveSlowPngListNilAndErrors(t *testing.T) {
	_, _, cacheKey, _ := setup(t)
	sp := SnapshotPathNames(cacheKey)

	if err := SaveSlowPngList(cacheKey, nil); err != nil {
		t.Fatalf("nil save: %v", err)
	}
	if data, err := os.ReadFile(sp.slowPngPath); err != nil || string(data) != "[]" {
		t.Fatalf("nil persisted = %q: %v, want []", data, err)
	}

	if err := os.Mkdir(sp.slowPngPath+"~", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveSlowPngList(cacheKey, []string{"x.png"}); err == nil {
		t.Fatalf("want WriteFile error when the tmp path is a directory")
	}
	if err := os.RemoveAll(sp.slowPngPath + "~"); err != nil {
		t.Fatal(err)
	}

	// A directory at the slow-list path: ReadFile is a non-ENOENT error ->
	// the warn path returns an empty list.
	os.RemoveAll(sp.slowPngPath) // the nil-save left the file here
	if err := os.Mkdir(sp.slowPngPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := loadSlowPngList(cacheKey); len(got) != 0 {
		t.Fatalf("dir-at-path list = %v, want empty", got)
	}
	if err := os.RemoveAll(sp.slowPngPath); err != nil {
		t.Fatal(err)
	}
}

// TestLoadSnapshotFromFileBadJSON: a gz body that is not JSON is a plain
// error (neither MissingUpdates nor ENOENT).
func TestLoadSnapshotFromFileBadJSON(t *testing.T) {
	_, _, cacheKey, _ := setup(t)
	sp := SnapshotPathNames(cacheKey)
	writeGZ(t, sp.resyncPath, `this is not json`)
	if _, err := loadSnapshotFromFile(sp.resyncPath, 0, true); err == nil || errors.IsMissingUpdates(err) {
		t.Fatalf("want plain unmarshal error, got MissingUpdates/nil: %v", err)
	}
}

// TestSaveSnapshotDirIsFile: sp.dir exists as a file -> MkdirAll fails.
func TestSaveSnapshotDirIsFile(t *testing.T) {
	_, _, cacheKey, _ := setup(t)
	sp := SnapshotPathNames(cacheKey)
	if err := os.WriteFile(sp.dir, []byte("i am a file"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sp.dir)
	if err := saveSnapshot(cacheKey, map[string]any{"files": map[string]any{}}, 0, nil, nil, false); err == nil {
		t.Fatalf("want MkdirAll error when the cache dir is a file")
	}
}

// TestSyncInvalidSnapshotAndChanges: an invalid raw snapshot and an invalid
// raw change operation both surface as errors.
func TestSyncInvalidSnapshotAndChanges(t *testing.T) {
	projectID, userID, _, compileDir := setup(t)
	m := &fakeSeams{}
	installFakeSeams(t, m)

	if _, _, err := syncOnce(t, m, projectID, userID, compileDir,
		map[string]any{"rawSnapshot": map[string]any{}}); err == nil {
		t.Fatalf("want invalid snapshot error")
	}
	changes := [][]map[string]any{{{"bogus": 1}}}
	if _, _, err := syncOnce(t, m, projectID, userID, compileDir, map[string]any{"changes": changes}); err == nil {
		t.Fatalf("want invalid change operation error")
	}
}

// TestGunzipBytesTruncated: a truncated gz stream is a ReadFrom error.
func TestGunzipBytesTruncated(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "gz")
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewWriterLevel(f, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gz.Write(make([]byte, 4096)); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	blob, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gunzipBytes(blob[:len(blob)/2]); err == nil {
		t.Fatalf("want ReadFrom error for truncated gz")
	}
}
