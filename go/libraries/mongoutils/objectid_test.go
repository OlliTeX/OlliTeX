package mongoutils

// objectid_test.go — pure pins for the ObjectId helpers (batchedUpdate.js),
// plus the batch-options refresh semantics (env precedence, defaults).

import (
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestObjectIdFromInputHex(t *testing.T) {
	t.Parallel()
	id, err := ObjectIdFromInput("5f3a1b2c3d4e5f6a7b8c9d0e")
	if err != nil {
		t.Fatal(err)
	}
	if got := id.Hex(); got != "5f3a1b2c3d4e5f6a7b8c9d0e" {
		t.Fatalf("hex round-trip: %s", got)
	}
}

func TestObjectIdFromInputDate(t *testing.T) {
	t.Parallel()
	// Node: `new Date('2023-05-17T12:00:00.000Z').getTime()` → ms
	const iso = "2023-05-17T12:00:00.000Z"
	id, err := ObjectIdFromInput(iso)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2023, 5, 17, 12, 0, 0, 0, time.UTC).UnixMilli()
	if got := getMsFromObjectId(id); got != want {
		t.Fatalf("timestamp: got %d want %d", got, want)
	}
}

func TestObjectIdFromInputInvalidDate(t *testing.T) {
	t.Parallel()
	_, err := ObjectIdFromInput("not-a-date-T-thing")
	if err == nil {
		t.Fatal("expected an error")
	}
	const want = "not-a-date-T-thing is not a valid date"
	if err.Error() != want {
		t.Fatalf("\n got: %q\nwant: %q", err.Error(), want)
	}
}

func TestObjectIdFromInputInvalidHex(t *testing.T) {
	t.Parallel()
	_, err := ObjectIdFromInput("zzz")
	if err == nil {
		t.Fatal("expected an error for a non-hex id")
	}
}

func TestRenderObjectId(t *testing.T) {
	t.Parallel()
	// Node: `${objectId} (${objectId.getTimestamp().toISOString()})`
	const iso = "2023-05-17T12:00:00.000Z"
	id, err := ObjectIdFromInput(iso)
	if err != nil {
		t.Fatal(err)
	}
	got := RenderObjectId(id)
	if !strings.HasPrefix(got, id.Hex()+" (") {
		t.Fatalf("render: %q", got)
	}
	if !strings.HasSuffix(got, "(2023-05-17T12:00:00.000Z)") {
		t.Fatalf("render suffix: %q", got)
	}
}

func TestObjectIDEdges(t *testing.T) {
	t.Parallel()
	// ID_EDGE_FUTURE is captured once, at package init (Node: module load):
	// within a generous window of the test's own clock it must read as
	// "now+1s-ish" (second-resolution, slightly future at init time).
	m := getMsFromObjectId(IEdgeFuture)
	now := time.Now().UnixMilli()
	if m > now+120*1000 || m < now-60*1000 {
		t.Fatalf("ID_EDGE_FUTURE should be ~init-time+1s, got %d (now %d)", m, now)
	}
	a := objectIdFromMs(1_000_000)
	b := objectIdFromMs(1_000_000 + 5000)
	if getMsFromObjectId(b)-getMsFromObjectId(a) != 5000 {
		t.Fatal("timestamp arithmetic broken")
	}
}

func TestGetNextEndSpans(t *testing.T) {
	t.Parallel()
	// epoch-scale ms throughout: ObjectId timestamps are 32-bit seconds, so
	// sub-second / negative values would truncate or wrap.
	const base = 1_700_000_000_000
	rangeEnd := objectIdFromMs(base + 3600_000)

	// ascending: end = start + span (clamped at rangeEnd)
	start := objectIdFromMs(base)
	if end := getNextEnd(start, 60_000, false, rangeEnd); getMsFromObjectId(end) != base+60_000 {
		t.Fatalf("asc end: %d", getMsFromObjectId(end))
	}
	startNearEnd := objectIdFromMs(getMsFromObjectId(rangeEnd) - 10_000)
	if end := getNextEnd(startNearEnd, 60_000, false, rangeEnd); end != rangeEnd {
		t.Fatalf("asc clamps to range end: %v", end)
	}

	// descending: end = start - span (clamped down to rangeEnd)
	rangeEndDesc := objectIdFromMs(base - 3600_000)
	if end := getNextEnd(objectIdFromMs(base+3600_000), 60_000, true, rangeEndDesc); getMsFromObjectId(end) != base+3600_000-60_000 {
		t.Fatalf("desc end: %d", getMsFromObjectId(end))
	}
	startNearEndDesc := objectIdFromMs(getMsFromObjectId(rangeEndDesc) + 10_000)
	if end := getNextEnd(startNearEndDesc, 60_000, true, rangeEndDesc); end != rangeEndDesc {
		t.Fatalf("desc clamps to range end: %v", end)
	}
}

// --- batch options refresh (package-scope state: sequential tests) ---------

// snapGlobals / restoreGlobals snapshot the module-scope batch state (tests
// that mutate it must restore it — the state is shared process-wide, exactly
// like the Node module vars).
type globalsSnapshot struct {
	descending  bool
	size        int
	verbose     bool
	rangeStart  primitive.ObjectID
	rangeEnd    primitive.ObjectID
	maxSpan     int64
	running     bool
	ideEdgePast primitive.ObjectID
	hasEdgePast bool
}

func readGlobalsLocked() globalsSnapshot {
	return globalsSnapshot{
		descending:  BatchDescending,
		size:        BatchSize,
		verbose:     VerboseLogging,
		rangeStart:  BatchRangeStart,
		rangeEnd:    BatchRangeEnd,
		maxSpan:     BatchMaxTimeSpanMs,
		running:     BatchedUpdateRunning,
		ideEdgePast: ideEdgePast,
		hasEdgePast: hasIdeEdgePast,
	}
}

func restoreGlobals(g globalsSnapshot) {
	batchStateMu.Lock()
	defer batchStateMu.Unlock()
	BatchDescending = g.descending
	BatchSize = g.size
	VerboseLogging = g.verbose
	BatchRangeStart = g.rangeStart
	BatchRangeEnd = g.rangeEnd
	BatchMaxTimeSpanMs = g.maxSpan
	BatchedUpdateRunning = g.running
	ideEdgePast = g.ideEdgePast
	hasIdeEdgePast = g.hasEdgePast
}

func snapGlobals() globalsSnapshot {
	batchStateMu.Lock()
	defer batchStateMu.Unlock()
	return readGlobalsLocked()
}

func readGlobals() globalsSnapshot {
	batchStateMu.Lock()
	defer batchStateMu.Unlock()
	return readGlobalsLocked()
}

func TestRefreshOptionsExplicit(t *testing.T) {
	snap := snapGlobals()
	defer restoreGlobals(snap)
	refreshGlobalOptionsForBatchedUpdate(BatchedUpdateOptions{
		BatchDescending:    "true",
		BatchSize:          "250",
		BatchMaxTimeSpanMs: "600000",
		VerboseLogging:     "true",
	})
	g := readGlobals()
	if !g.descending || g.size != 250 || !g.verbose || g.maxSpan != 600000 {
		t.Fatalf("globals: desc=%v size=%d verbose=%v span=%d", g.descending, g.size, g.verbose, g.maxSpan)
	}
	// descending: BATCH_RANGE_START defaults to ID_EDGE_FUTURE
	if g.rangeStart != IEdgeFuture {
		t.Fatalf("desc rangeStart: %v want %v", g.rangeStart, IEdgeFuture)
	}
}

func TestRefreshOptionsEnvWins(t *testing.T) {
	snap := snapGlobals()
	defer restoreGlobals(snap)
	t.Setenv("BATCH_SIZE", "42")
	t.Setenv("BATCH_DESCENDING", "true")
	t.Setenv("VERBOSE_LOGGING", "true")
	refreshGlobalOptionsForBatchedUpdate(BatchedUpdateOptions{
		BatchSize:       "1000", // must lose to the env (Node Object.assign order)
		BatchDescending: "",     // env 'true' wins over an absent option
		VerboseLogging:  "",
	})
	g := readGlobals()
	if g.size != 42 {
		t.Fatalf("env BATCH_SIZE should win: got %d want 42", g.size)
	}
	if !g.descending || !g.verbose {
		t.Fatal("env booleans should win over options")
	}
}

func TestRefreshOptionsDefaults(t *testing.T) {
	snap := snapGlobals()
	defer restoreGlobals(snap)
	refreshGlobalOptionsForBatchedUpdate(BatchedUpdateOptions{})
	g := readGlobals()
	if g.size != 1000 {
		t.Fatalf("default BATCH_SIZE: got %d want 1000", g.size)
	}
	if g.maxSpan != ONE_MONTH_IN_MS {
		t.Fatalf("default span: got %d want %d", g.maxSpan, ONE_MONTH_IN_MS)
	}
	if g.descending {
		t.Fatal("default direction is ascending")
	}
	// ascending: range end defaults to the future edge
	if g.rangeEnd != IEdgeFuture {
		t.Fatalf("asc rangeEnd: got %v want %v", g.rangeEnd, IEdgeFuture)
	}
}
