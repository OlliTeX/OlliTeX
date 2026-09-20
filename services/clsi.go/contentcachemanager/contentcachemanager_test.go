package contentcachemanager

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	clserrors "clsi/errors"
)

const (
	hashLarge = "d7cfc73ad2fba4578a437517923e3714927bbf35e63ea88bd93c7a8076cf1fcd"
	hashSmall = "896749b8343851b0dc385f71616916a7ba0434fcfb56d1fc7e27cd139eaa2f71"
)

var fixtureBase = filepath.Join("..", "..", "clsi", "test")

func readFixture(t *testing.T, name string) []byte {
	b, err := os.ReadFile(filepath.Join(fixtureBase, name))
	if err != nil {
		t.Skipf("fixture unavailable: %v", err)
	}
	return b
}

func writeFile(t *testing.T, path string, data []byte) {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func stateJSON(t *testing.T, contentDir string) string {
	blob, err := os.ReadFile(filepath.Join(contentDir, ".state.v0.json"))
	if err != nil {
		t.Fatalf("state file: %v", err)
	}
	return string(blob)
}

func TestMinimalCompileTwoRangesQualifying(t *testing.T) {
	var reclaimed int64

	contentDir := filepath.Join(t.TempDir(), "content", "1797a7f48f9-5abc1998509dea1f")
	pdfDir := filepath.Join(t.TempDir(), "generated-files", "1797a7f48ea-8ac6805139f43351")
	if err := os.MkdirAll(contentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pdfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pdfPath := filepath.Join(pdfDir, "output.pdf")
	pdf := readFixture(t, filepath.Join("acceptance", "fixtures", "minimal.pdf"))
	writeFile(t, pdfPath, pdf)
	writeFile(t, pdfPath+"xref", readFixture(t, filepath.Join("acceptance", "fixtures", "minimal.pdfxref")))

	chunkL := readFixture(t, filepath.Join("unit", "js", "snapshots", "minimalCompile", "chunks", hashLarge))
	chunkS := readFixture(t, filepath.Join("unit", "js", "snapshots", "minimalCompile", "chunks", hashSmall))
	startL := idxBytes(t, pdf, chunkL)
	endL := startL + int64(len(chunkL))
	startS := idxBytes(t, pdf, chunkS)
	endS := startS + int64(len(chunkS))

	run := func(minChunk int64) *UpdateResult {
		res, err := Update(UpdateArgs{
			ContentDir:             contentDir,
			FilePath:               pdfPath,
			PdfSize:                int64(len(pdf)),
			PdfCachingMinChunkSize: minChunk,
			CompileTime:            1337,
		})
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		reclaimed += res.ReclaimedSpace
		return res
	}

	wantState1 := fmt.Sprintf(
		`{"hashAge":[["%s",0],["%s",0]],"hashSize":[["%s",%d],["%s",%d]]}`,
		hashLarge, hashSmall, hashLarge, len(chunkL), hashSmall, len(chunkS))
	wantState2 := fmt.Sprintf(
		`{"hashAge":[["%s",0],["%s",1]],"hashSize":[["%s",%d],["%s",%d]]}`,
		hashLarge, hashSmall, hashLarge, len(chunkL), hashSmall, len(chunkS))
	wantState3 := fmt.Sprintf(
		`{"hashAge":[["%s",0]],"hashSize":[["%s",%d]]}`,
		hashLarge, hashLarge, len(chunkL))

	checkState := func(wantJSON string) {
		if got := stateJSON(t, contentDir); got != wantJSON {
			t.Fatalf("state mismatch:\n got: %s\nwant: %s", got, wantJSON)
		}
	}

	// Run 1: minChunk 500 — two ranges qualify.
	res := run(500)
	if got, want := len(res.ContentRanges), 2; got != want {
		t.Fatalf("run1: expected %d ranges, got %d (%+v)", want, got, res)
	}
	if got := res.ContentRanges[0]; got.ObjectID != "9 0 " || got.Start != startL ||
		got.End != endL || got.Hash != hashLarge {
		t.Errorf("run1: range0 mismatch: %+v (want start=%d end=%d)", got, startL, endL)
	}
	if got := res.ContentRanges[1]; got.ObjectID != "10 0 " || got.Start != startS ||
		got.End != endS || got.Hash != hashSmall {
		t.Errorf("run1: range1 mismatch: %+v (want start=%d end=%d)", got, startS, endS)
	}
	if got, want := len(res.NewContentRanges), 2; got != want {
		t.Fatalf("run1: all ranges should be new, got %d new (want %d)", got, want)
	}
	assertStoredChunk(t, contentDir, hashLarge, chunkL)
	assertStoredChunk(t, contentDir, hashSmall, chunkS)
	checkState(wantState1)
	if res.OverheadDeleteStaleHashes == nil {
		t.Error("run1: overheadDeleteStaleHashes must be set")
	}

	// Run 2: minChunk 1024 — one range too small.
	res = run(1024)
	if got, want := len(res.ContentRanges), 1; got != want {
		t.Fatalf("run2: expected %d range, got %d", want, got)
	}
	if got := res.ContentRanges[0]; got.ObjectID != "9 0 " || got.Start != startL ||
		got.End != endL || got.Hash != hashLarge {
		t.Errorf("run2: range mismatch: %+v", got)
	}
	if got, want := len(res.NewContentRanges), 0; got != want {
		t.Fatalf("run2: no new ranges expected, got %d", got)
	}
	// The 2nd range's age increments while it is no longer tracked to 0.
	checkState(wantState2)

	// Run 3-7: 5 more passes — the small range ages out and is deleted.
	for i := 0; i < 5; i++ {
		res = run(1024)
	}
	if got, want := int(len(chunkS)), int(reclaimed); got != want {
		t.Fatalf("reclaimed space: expected %d (= chunkS length), got %d", got, want)
	}
	if _, err := os.Stat(filepath.Join(contentDir, hashSmall)); err == nil {
		t.Error("small chunk file should be deleted after 5 idle generations")
	}
	checkState(wantState3)
}

func TestUpdateEarlyReturnWhenBelowMinChunk(t *testing.T) {
	res, err := Update(UpdateArgs{
		ContentDir:             filepath.Join(t.TempDir(), "content"),
		FilePath:               "/nonexistent/output.pdf",
		PdfSize:                10,
		PdfCachingMinChunkSize: 1024,
		CompileTime:            1000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.ContentRanges) != 0 || len(res.NewContentRanges) != 0 {
		t.Errorf("expected empty ranges, got %+v", res)
	}
	if res.ReclaimedSpace != 0 {
		t.Errorf("expected 0 reclaimed")
	}
	if res.OverheadDeleteStaleHashes != nil {
		t.Error("early return must not set overheadDeleteStaleHashes")
	}
	if res.StartXRefTable != nil {
		t.Error("early return must not set startXRefTable")
	}
	if res.TimedOutErr != nil {
		t.Error("early return must not set timedOutErr")
	}
}

func TestUpdateNoXrefTablePropagates(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "output.pdf"), []byte("some pdf bytes"))
	_, err := Update(UpdateArgs{
		ContentDir:             filepath.Join(dir, "content"),
		FilePath:               filepath.Join(dir, "output.pdf"),
		PdfSize:                100,
		PdfCachingMinChunkSize: 10,
		CompileTime:            1000,
	})
	if err == nil {
		t.Fatal("expected NoXrefTableError, got nil")
	}
	if _, ok := err.(*clserrors.NoXrefTableError); !ok {
		t.Fatalf("expected *NoXrefTableError, got %T: %v", err, err)
	}
}

func TestUpdateReadFullChunkError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "output.pdfxref"),
		[]byte("1/0: uncompressed; offset = 0\n2/0: uncompressed; offset = 210\n"))
	writeFile(t, filepath.Join(dir, "output.pdf"), make([]byte, 200))
	_, err := Update(UpdateArgs{
		ContentDir:             filepath.Join(dir, "content"),
		FilePath:               filepath.Join(dir, "output.pdf"),
		PdfSize:                200,
		PdfCachingMinChunkSize: 100,
		CompileTime:            1000,
	})
	if err == nil {
		t.Fatal("expected could-not-read-full-chunk error")
	}
	if got, want := err.Error(), "could not read full chunk"; got != want {
		t.Fatalf("unexpected error: %q, want %q", got, want)
	}
	if _, ok := err.(*clserrors.OError); !ok {
		t.Fatalf("expected *clserrors.OError, got %T", err)
	}
}

func TestUpdateObjectIdTooLarge(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "output.pdfxref"),
		[]byte("1/0: uncompressed; offset = 0\n2/0: uncompressed; offset = 210\n"))
	buf := make([]byte, 210)
	for i := 0; i < 150; i++ {
		buf[i] = 'A'
	}
	copy(buf[150:], []byte("obj"))
	for i := 153; i < 210; i++ {
		buf[i] = 'B'
	}
	writeFile(t, filepath.Join(dir, "output.pdf"), buf)
	_, err := Update(UpdateArgs{
		ContentDir:             filepath.Join(dir, "content"),
		FilePath:               filepath.Join(dir, "output.pdf"),
		PdfSize:                210,
		PdfCachingMinChunkSize: 100,
		CompileTime:            1000,
	})
	if err == nil {
		t.Fatal("expected objectId-too-large error")
	}
	if got, want := err.Error(), "objectId is too large"; got != want {
		t.Fatalf("unexpected error: %q", got)
	}
	if _, ok := err.(*clserrors.OError); !ok {
		t.Fatalf("expected *clserrors.OError, got %T", err)
	}
}

func TestUpdateHardTimeoutBeforeLoop(t *testing.T) {
	origNow := Now
	origMax := MaxProcessingTimeMS
	t.Cleanup(func() { Now = origNow; MaxProcessingTimeMS = origMax })
	MaxProcessingTimeMS = 1

	base := time.Now()
	call := 0
	Now = func() time.Time {
		call++
		if call == 1 {
			return base
		}
		return base.Add(10 * time.Millisecond)
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "output.pdfxref"),
		[]byte("1/0: uncompressed; offset = 0\n2/0: uncompressed; offset = 210\n"))
	writeFile(t, filepath.Join(dir, "output.pdf"), make([]byte, 210))
	_, err := Update(UpdateArgs{
		ContentDir:             filepath.Join(dir, "content"),
		FilePath:               filepath.Join(dir, "output.pdf"),
		PdfSize:                210,
		PdfCachingMinChunkSize: 100,
		CompileTime:            1000,
	})
	if err == nil {
		t.Fatal("expected hard TimedOutError, got nil")
	}
	if _, ok := err.(*clserrors.TimedOutError); !ok {
		t.Fatalf("expected *TimedOutError, got %T: %v", err, err)
	}
}

func TestUpdateSoftTimeoutInsideLoop(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "output.pdfxref"),
		[]byte("1/0: uncompressed; offset = 0\n2/0: uncompressed; offset = 210\n"))
	buf := make([]byte, 210)
	for i := 0; i < 100; i++ {
		buf[i] = 'A'
	}
	copy(buf[100:], []byte("obj"))
	for i := 103; i < 210; i++ {
		buf[i] = 'B'
	}
	writeFile(t, filepath.Join(dir, "output.pdf"), buf)

	origNow := Now
	origMax := MaxProcessingTimeMS
	t.Cleanup(func() { Now = origNow; MaxProcessingTimeMS = origMax })
	MaxProcessingTimeMS = 1

	base := time.Now()
	call := 0
	// Breach at the "after read 0" deadline check: 1=ctor, 2=init check,
	// 3-4=deleteStale, 5=after-delete, 6=after-parsing, 7=after-finding,
	// 8=after-read0.
	Now = func() time.Time {
		call++
		if call >= 8 {
			return base.Add(10 * time.Millisecond)
		}
		return base
	}

	res, err := Update(UpdateArgs{
		ContentDir:             filepath.Join(dir, "content"),
		FilePath:               filepath.Join(dir, "output.pdf"),
		PdfSize:                210,
		PdfCachingMinChunkSize: 100,
		CompileTime:            1000,
	})
	if err != nil {
		t.Fatalf("soft timeout must not surface as a hard error: %v", err)
	}
	if res.TimedOutErr == nil {
		t.Fatal("expected soft TimedOutErr in result")
	}
	if got, want := len(res.ContentRanges), 0; got != want {
		t.Fatalf("expected %d ranges (breach before first hash), got %d", got, want)
	}
	if res.OverheadDeleteStaleHashes == nil {
		t.Error("overheadDeleteStaleHashes must be set on the soft path")
	}
}

func TestTrackerMissingStateYieldsFreshTracker(t *testing.T) {
	dir := t.TempDir()
	tr := trackerFrom(dir)
	if len(tr.ageOrder) != 0 || len(tr.sizeOrder) != 0 || len(tr.hashAge) != 0 {
		t.Fatal("missing state must yield fresh tracker")
	}
}

func TestTrackerCorruptStateYieldsFreshTracker(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".state.v0.json"), []byte("{not json"))
	tr := trackerFrom(dir)
	if len(tr.ageOrder) != 0 || len(tr.sizeOrder) != 0 {
		t.Fatal("corrupt state must yield fresh tracker")
	}
}

func TestTrackerFlushError(t *testing.T) {
	dir := t.TempDir()
	// Make the state path a directory so the atomic write fails.
	if err := os.Mkdir(filepath.Join(dir, ".state.v0.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	tr := &hashFileTracker{
		contentDir: dir,
		hashAge:    map[string]int{"h": 0},
		hashSize:   map[string]int64{"h": 10},
		ageOrder:   []string{"h"},
		sizeOrder:  []string{"h"},
	}
	if err := tr.flush(); err == nil {
		t.Fatal("expected flush error (state path is a directory)")
	}
}

func TestDeleteStaleHashesRemovesFiles(t *testing.T) {
	dir := t.TempDir()
	tr := &hashFileTracker{
		contentDir: dir,
		hashAge:    map[string]int{"old": 6, "cur": 0},
		hashSize:   map[string]int64{"old": 42, "cur": 10},
		ageOrder:   []string{"old", "cur"},
		sizeOrder:  []string{"old", "cur"},
	}
	writeFile(t, filepath.Join(dir, "old"), append([]byte("stale"), make([]byte, 35)...))
	reclaimed, _, err := tr.deleteStaleHashes(5)
	if err != nil {
		t.Fatalf("deleteStaleHashes: %v", err)
	}
	if reclaimed != 42 {
		t.Fatalf("expected 42 reclaimed, got %d", reclaimed)
	}
	if _, err := os.Stat(filepath.Join(dir, "old")); err == nil {
		t.Error("stale hash file should be deleted")
	}
	if !tr.has("cur") || tr.has("old") {
		t.Error("tracker state inconsistent after delete")
	}
}

func TestDeleteStaleHashesIgnoresMissingFile(t *testing.T) {
	dir := t.TempDir()
	tr := &hashFileTracker{
		contentDir: dir,
		hashAge:    map[string]int{"gone": 9},
		hashSize:   map[string]int64{"gone": 7},
		ageOrder:   []string{"gone"},
		sizeOrder:  []string{"gone"},
	}
	reclaimed, _, err := tr.deleteStaleHashes(5)
	if err != nil {
		t.Fatalf("ENOENT must be ignored: %v", err)
	}
	if reclaimed != 7 {
		t.Fatalf("expected 7 reclaimed, got %d", reclaimed)
	}
}

func TestWritePdfStreamAtomic(t *testing.T) {
	dir := t.TempDir()
	if err := writePdfStream(dir, "abc", []byte("payload")); err != nil {
		t.Fatalf("writePdfStream: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "abc"))
	if err != nil || !bytes.Equal(b, []byte("payload")) {
		t.Fatalf("expected payload, got %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "abc~")); err == nil {
		t.Error("temp file should be gone after rename")
	}
}

func TestPdfStreamHashDeterministic(t *testing.T) {
	h1 := pdfStreamHash([]byte("payload"))
	h2 := pdfStreamHash([]byte("payload"))
	if h1 != h2 {
		t.Fatal("hash must be deterministic")
	}
	if len(h1) != 64 {
		t.Fatalf("sha256 hex must be 64 chars, got %d", len(h1))
	}
}

func TestGetDeadlineCheckerTimeoutFormulas(t *testing.T) {
	base := time.Unix(0, 0)
	now := func() time.Time { return base }

	origMax := MaxProcessingTimeMS
	t.Cleanup(func() { MaxProcessingTimeMS = origMax })

	// compileTime 1000 -> max(250,1000) = 1000; cap 10010 keeps 1000.
	MaxProcessingTimeMS = 10010
	if d := getDeadlineChecker(1000, now); d.timeout != 1000 {
		t.Fatalf("timeout should be 1000, got %v", d.timeout)
	}
	// compileTime 40000 -> 10000 vs cap 10000 -> 10000.
	MaxProcessingTimeMS = 10000
	if d := getDeadlineChecker(40000, now); d.timeout != 10000 {
		t.Fatalf("timeout should be 10000, got %v", d.timeout)
	}
	// compileTime 10000 -> 2500 (above 1000) vs cap 1000 -> 1000.
	MaxProcessingTimeMS = 1000
	if d := getDeadlineChecker(10000, now); d.timeout != 1000 {
		t.Fatalf("timeout should be 1000, got %v", d.timeout)
	}
}

// idxBytes returns the byte offset of needle in haystack (must match).
func idxBytes(t *testing.T, haystack, needle []byte) int64 {
	i := bytes.Index(haystack, needle)
	if i < 0 {
		t.Fatalf("needle not found in haystack")
	}
	return int64(i)
}

func assertStoredChunk(t *testing.T, contentDir, hash string, want []byte) {
	b, err := os.ReadFile(filepath.Join(contentDir, hash))
	if err != nil {
		t.Fatalf("stored chunk %s: %v", hash, err)
	}
	if !bytes.Equal(b, want) {
		t.Fatalf("stored chunk %s mismatch: %d bytes != %d bytes", hash, len(b), len(want))
	}
}

func TestWritePdfStreamWriteError(t *testing.T) {
	dir := t.TempDir()
	// A directory at the temp path makes the underlying write fail.
	if err := os.Mkdir(filepath.Join(dir, "abc~"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writePdfStream(dir, "abc", []byte("payload")); err == nil {
		t.Fatal("expected writePdfStream error (temp path is a directory)")
	}
}

func TestWritePdfStreamRenameError(t *testing.T) {
	dir := t.TempDir()
	// A directory at the final path makes the rename fail, exercising the
	// temp-cleanup branch.
	if err := os.Mkdir(filepath.Join(dir, "xyz"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writePdfStream(dir, "xyz", []byte("data")); err == nil {
		t.Fatal("expected writePdfStream rename error (target is a directory)")
	}
	if _, err := os.Stat(filepath.Join(dir, "xyz~")); err == nil {
		t.Error("stray temp file left behind after rename error")
	}
	_ = os.RemoveAll(dir)
}

func TestTrackerFromStateWithMixedTypes(t *testing.T) {
	dir := t.TempDir()
	state := `[{"hashAge":null}]` // placeholder; replaced below
	_ = state
	st := `{
	  "hashAge":  [["a", 1], ["b", 2.5], ["c", "not-a-number"], ["d"], ["e", "3"], ["f", 4], ["g", 5]],
	  "hashSize": [["a", 10], ["b", 20.5], ["c", "30"], ["d", 1], ["e", "junk"], ["f"], ["g", 99], ["h", 88]]
	}`
	writeFile(t, filepath.Join(dir, ".state.v0.json"), []byte(st))
	tr := trackerFrom(dir)
	if !tr.has("a") || !tr.has("b") || !tr.has("f") || !tr.has("g") {
		t.Errorf("expected a/b/f/g tracked, got %v", tr.ageOrder)
	}
	if tr.has("c") || tr.has("d") || tr.has("e") {
		t.Errorf("unexpected tracked hashes: %v", tr.ageOrder)
	}
	if got, want := tr.hashAge["b"], 2; got != want {
		t.Errorf("age[b]: got %d, want %d", got, want)
	}
	if got, want := tr.hashSize["a"], int64(10); got != want {
		t.Errorf("size[a]: got %d, want %d", got, want)
	}
}

func TestAsInt64Cases(t *testing.T) {
	if v, ok := asInt64(int64(3)); !ok || v != 3 {
		t.Errorf("int64 case: %v", ok)
	}
	if v, ok := asInt64(int(3)); !ok || v != 3 {
		t.Errorf("int case: %v", ok)
	}
	if v, ok := asInt64(float64(7.9)); !ok || v != 7 {
		t.Errorf("float case: %v got %v", ok, v)
	}
	if v, ok := asInt64("nope"); ok || v != 0 {
		t.Errorf("string case must not convert, got %v", v)
	}
}

func TestDeleteStaleHashesUnlinkError(t *testing.T) {
	dir := t.TempDir()
	// A non-EMPTY directory at the hash path makes os.Remove fail with
	// ENOTDIR/EPERM (not ENOENT) — exercises the error-return branch.
	if err := os.Mkdir(filepath.Join(dir, "dh"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "dh", "inner"), []byte("x"))
	tr := &hashFileTracker{
		contentDir: dir,
		hashAge:    map[string]int{"dh": 9},
		hashSize:   map[string]int64{"dh": 1},
		ageOrder:   []string{"dh"},
		sizeOrder:  []string{"dh"},
	}
	_, _, err := tr.deleteStaleHashes(5)
	if err == nil {
		t.Fatal("expected unlink error for non-empty directory hash path")
	}
}

func TestUpdateNoObjInBuffer(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "content"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "output.pdfxref"),
		[]byte("1/0: uncompressed; offset = 0\n2/0: uncompressed; offset = 210\n"))
	buf := make([]byte, 210)
	for i := range buf {
		buf[i] = 'x' // no "obj" substring anywhere
	}
	writeFile(t, filepath.Join(dir, "output.pdf"), buf)
	res, err := Update(UpdateArgs{
		ContentDir:             filepath.Join(dir, "content"),
		FilePath:               filepath.Join(dir, "output.pdf"),
		PdfSize:                210,
		PdfCachingMinChunkSize: 100,
		CompileTime:            1000,
	})
	if err != nil {
		t.Fatalf("no-obj buffer must not error: %v", err)
	}
	if got, want := len(res.ContentRanges), 1; got != want {
		t.Fatalf("expected %d range, got %d", want, got)
	}
	// Degenerate case (no "obj" marker): Node subarray semantics —
	// objectIdRaw is all but the last byte, and the last byte is the
	// stream.
	if got, want := len(res.ContentRanges[0].ObjectID), 209; got != want {
		t.Errorf("objectIdRaw mismatch: %d bytes want %d", got, want)
	}
	if got, want := res.ContentRanges[0].Start, int64(209); got != want {
		t.Errorf("start mismatch: %d", got)
	}
	if got, want := res.ContentRanges[0].End, int64(210); got != want {
		t.Errorf("end mismatch: %d", got)
	}
	assertStoredChunk(t, filepath.Join(dir, "content"), res.ContentRanges[0].Hash, []byte{'x'})
	if got, want := len(res.NewContentRanges), 1; got != want {
		t.Errorf("expected 1 new range, got %d", got)
	}
}

func TestUpdateSoftTimeoutAfterHash(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "output.pdfxref"),
		[]byte("1/0: uncompressed; offset = 0\n2/0: uncompressed; offset = 210\n"))
	buf := make([]byte, 210)
	for i := 0; i < 100; i++ {
		buf[i] = 'A'
	}
	copy(buf[100:], []byte("obj"))
	for i := 103; i < 210; i++ {
		buf[i] = 'B'
	}
	writeFile(t, filepath.Join(dir, "output.pdf"), buf)

	origNow := Now
	origMax := MaxProcessingTimeMS
	t.Cleanup(func() { Now = origNow; MaxProcessingTimeMS = origMax })
	MaxProcessingTimeMS = 1

	base := time.Now()
	call := 0
	// Breach at the "after hash 0" check (call 9): 1=ctor, 2=init, 3-4=
	// deleteStale, 5=after-delete, 6=after-parsing, 7=after-finding,
	// 8=after-read0 (OK), 9=after-hash0 (BREACH).
	Now = func() time.Time {
		call++
		if call >= 9 {
			return base.Add(10 * time.Millisecond)
		}
		return base
	}

	res, err := Update(UpdateArgs{
		ContentDir:             filepath.Join(dir, "content"),
		FilePath:               filepath.Join(dir, "output.pdf"),
		PdfSize:                210,
		PdfCachingMinChunkSize: 100,
		CompileTime:            1000,
	})
	if err != nil {
		t.Fatalf("soft timeout must not surface as a hard error: %v", err)
	}
	if res.TimedOutErr == nil {
		t.Fatal("expected soft TimedOutErr in result")
	}
	if got, want := len(res.ContentRanges), 0; got != want {
		t.Fatalf("expected %d ranges (breach after hash, before store), got %d", got, want)
	}
}

func TestUpdateSoftTimeoutAfterWrite(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "output.pdfxref"),
		[]byte("1/0: uncompressed; offset = 0\n2/0: uncompressed; offset = 210\n"))
	buf := make([]byte, 210)
	for i := 0; i < 100; i++ {
		buf[i] = 'A'
	}
	copy(buf[100:], []byte("obj"))
	for i := 103; i < 210; i++ {
		buf[i] = 'B'
	}
	writeFile(t, filepath.Join(dir, "output.pdf"), buf)

	origNow := Now
	origMax := MaxProcessingTimeMS
	t.Cleanup(func() { Now = origNow; MaxProcessingTimeMS = origMax })
	MaxProcessingTimeMS = 1

	base := time.Now()
	call := 0
	// Breach at the "after write 0" check (call 10): 1=ctor, 2=init,
	// 3-4=deleteStale, 5=after-delete, 6=after-parsing, 7=after-finding,
	// 8=after-read0, 9=after-hash0, 10=after-hash0 (second breach, soft).
	Now = func() time.Time {
		call++
		if call >= 10 {
			return base.Add(10 * time.Millisecond)
		}
		return base
	}

	if err := os.MkdirAll(filepath.Join(dir, "content"), 0o755); err != nil {
		t.Fatal(err)
	}
	contentDir := filepath.Join(dir, "content")
	res, err := Update(UpdateArgs{
		ContentDir:             contentDir,
		FilePath:               filepath.Join(dir, "output.pdf"),
		PdfSize:                210,
		PdfCachingMinChunkSize: 100,
		CompileTime:            1000,
	})
	if err != nil {
		t.Fatalf("soft timeout after write must not surface as hard error: %v", err)
	}
	if res.TimedOutErr == nil {
		t.Fatal("expected soft TimedOutErr in result")
	}
	if got, want := len(res.NewContentRanges), 1; got != want {
		t.Fatalf("expected range stored before breach, got %d new", got)
	}
	if res.ReclaimedSpace != 0 {
		t.Errorf("unexpected reclaimed: %d", res.ReclaimedSpace)
	}
}

func TestUpdateReadAtBeyondEOF(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "output.pdfxref"),
		[]byte("1/0: uncompressed; offset = 100\n2/0: uncompressed; offset = 1210\n"))
	writeFile(t, filepath.Join(dir, "output.pdf"), make([]byte, 50))
	_, err := Update(UpdateArgs{
		ContentDir:             filepath.Join(dir, "content"),
		FilePath:               filepath.Join(dir, "output.pdf"),
		PdfSize:                100,
		PdfCachingMinChunkSize: 100,
		CompileTime:            1000,
	})
	if err == nil {
		t.Fatal("expected could-not-read-full-chunk error")
	}
	if got, want := err.Error(), "could not read full chunk"; got != want {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestFlushWriteError(t *testing.T) {
	dir := t.TempDir()
	// The temp state path (".state.v0.json~") is a directory, so the
	// underlying atomic write fails.
	if err := os.Mkdir(filepath.Join(dir, ".state.v0.json~"), 0o755); err != nil {
		t.Fatal(err)
	}
	tr := &hashFileTracker{
		contentDir: dir,
		hashAge:    map[string]int{"h": 0},
		hashSize:   map[string]int64{"h": 10},
		ageOrder:   []string{"h"},
		sizeOrder:  []string{"h"},
	}
	if err := tr.flush(); err == nil {
		t.Fatal("expected flush write error (temp path is a directory)")
	}
}
