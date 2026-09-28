package redismanager

// Error-path tests: the vendor suite injects LRange failures (covered in
// oracle_test); the Go port additionally returns errors from each redis op,
// so each wrapper's error branch is pinned here.

import "testing"

func errFake(op string) *fakeClient {
	f := newFake()
	f.failOps = map[string]error{op: errSentinel}
	return f
}

var errSentinel = fakeErr("injected redis error")

type fakeErr string

func (e fakeErr) Error() string { return string(e) }

func TestErrorCountUnprocessedUpdates(t *testing.T) {
	rm := New(errFake("LLen"), testKeys{})
	if _, err := rm.CountUnprocessedUpdates("p1"); err == nil {
		t.Fatalf("want LLen error")
	}
	n, err := New(newFake(), testKeys{}).CountUnprocessedUpdates("p1")
	if err != nil || n != 0 {
		t.Fatalf("empty queue: want 0 ok, got %d %v", n, err)
	}
}

func TestErrorGetRawUpdatesBatch(t *testing.T) {
	f := newFake()
	f.failOps = map[string]error{"LRange": fakeErr("boom")}
	rm := New(f, testKeys{})
	f.seedList("ops:p1", "x")
	if _, err := rm.GetRawUpdatesBatch("p1", 10); err == nil {
		t.Fatalf("want LRange error")
	}
}

func TestErrorParseDocUpdates(t *testing.T) {
	if _, err := ParseDocUpdates([]string{"not-json"}); err == nil {
		t.Fatalf("want parse error")
	}
}

func TestErrorDeleteAppliedDocUpdates(t *testing.T) {
	if err := New(errFake("LRem"), testKeys{}).DeleteAppliedDocUpdates("p1", []string{"x"}); err == nil {
		t.Fatalf("want LRem error")
	}
	if err := New(errFake("Del"), testKeys{}).DeleteAppliedDocUpdates("p1", []string{"x"}); err == nil {
		t.Fatalf("want Del error")
	}
	// empty raws list skips LRem but no Del either (len 0) → nil error.
	if err := New(newFake(), testKeys{}).DeleteAppliedDocUpdates("p1", nil); err != nil {
		t.Fatalf("want nil err, got %v", err)
	}
}

func TestErrorDestroyDocUpdatesQueue(t *testing.T) {
	if err := New(errFake("Del"), testKeys{}).DestroyDocUpdatesQueue("p1"); err == nil {
		t.Fatalf("want Del error")
	}
}

func TestErrorGetProjectIDs(t *testing.T) {
	if _, err := New(errFake("Scan"), testKeys{}).GetProjectIDsWithHistoryOps(); err == nil {
		t.Fatalf("want Scan error")
	}
	if _, err := New(errFake("Scan"), testKeys{}).GetProjectIDsWithFirstOpTimestamps(); err == nil {
		t.Fatalf("want Scan error")
	}
}

func TestErrorSetFirstOpTimestamp(t *testing.T) {
	if err := New(errFake("SetNX"), testKeys{}).SetFirstOpTimestamp("p1"); err == nil {
		t.Fatalf("want SetNX error")
	}
}

func TestErrorGetFirstOpTimestamp(t *testing.T) {
	if _, _, err := New(errFake("Get"), testKeys{}).GetFirstOpTimestamp("p1"); err == nil {
		t.Fatalf("want Get error")
	}
}

func TestGetFirstOpTimestampInvalidValues(t *testing.T) {
	// garbage → ok=false (vendor: parseInt NaN → null); empty value → ok=false.
	f := newFake()
	f.str["ts:p1"] = "1704067200000"
	rm := New(f, testKeys{})
	if ts, ok, _ := rm.GetFirstOpTimestamp("p1"); !ok || !ts.After(unixMilli(1704067200000-1)) {
		t.Fatalf("valid ts not round-tripped: ok=%v", ok)
	}
	f.str["ts:p1"] = "not-a-number"
	if _, ok, _ := rm.GetFirstOpTimestamp("p1"); ok {
		t.Fatalf("garbage ts: want ok=false")
	}
	f.str["ts:p1"] = "0"
	if _, ok, _ := rm.GetFirstOpTimestamp("p1"); ok {
		t.Fatalf("zero ts: want ok=false (vendor parseInt 0 → falsy)")
	}
}

func TestErrorGetFirstOpTimestamps(t *testing.T) {
	if _, err := New(errFake("MGet"), testKeys{}).GetFirstOpTimestamps([]string{"p1"}); err == nil {
		t.Fatalf("want MGet error")
	}
	// empty values round trip as zero time.
	f := newFake()
	f.str["ts:p1"] = "1704067200000"
	tss, err := New(f, testKeys{}).GetFirstOpTimestamps([]string{"p1", "p2"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if tss[0].IsZero() || !tss[1].IsZero() {
		t.Fatalf("want first set and second zero, got %v %v", tss[0], tss[1])
	}
}

func TestErrorClearFirstOpTimestamp(t *testing.T) {
	if err := New(errFake("Del"), testKeys{}).ClearFirstOpTimestamp("p1"); err == nil {
		t.Fatalf("want Del error")
	}
}

func TestErrorClearDanglingFirstOpTimestamp(t *testing.T) {
	if _, err := New(errFake("Exists"), testKeys{}).ClearDanglingFirstOpTimestamp("p1"); err == nil {
		t.Fatalf("want Exists error")
	}
	// one dangling: ts only → Del of ts; Del error propagates with both
	// keys present? No: Del only called when n==1. Seed ts-only then fail Del.
	f := newFake()
	f.str["ts:p1"] = "1"
	f.failOps = map[string]error{"Del": fakeErr("boom")}
	if _, err := New(f, testKeys{}).ClearDanglingFirstOpTimestamp("p1"); err == nil {
		t.Fatalf("want Del error when clearing dangling ts")
	}
}

func TestErrorCachedHistoryID(t *testing.T) {
	if _, _, err := New(errFake("Get"), testKeys{}).GetCachedHistoryID("p1"); err == nil {
		t.Fatalf("want Get error")
	}
	if err := New(errFake("Set"), testKeys{}).SetCachedHistoryID("p1", "h"); err == nil {
		t.Fatalf("want Set error")
	}
	if err := New(errFake("Del"), testKeys{}).ClearCachedHistoryID("p1"); err == nil {
		t.Fatalf("want Del error")
	}
}

// batchSize==1 single-stepping: vendor breaks after one batch even when the
// queue has more. Pin the Go port.
func TestGetUpdatesInBatches_batchSizeOneSingleStep(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	f.seedList("ops:p1", rawOf(5)...)
	var lg runnerLog
	if err := rm.GetUpdatesInBatches("p1", 1, lg.run); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(lg.calls) != 1 || len(lg.calls[0]) != 1 {
		t.Fatalf("want one batch of one, got %d calls (first=%d)", len(lg.calls), len(lg.calls[0]))
	}
	if got, _ := f.LRange("ops:p1", 0, -1); len(got) != 4 {
		t.Fatalf("want 4 preserved (single-step), got %d", len(got))
	}
}

// empty queue: runner not called, ts key NOT deleted (no updates applied).
func TestGetUpdatesInBatches_emptyQueueNoOp(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	f.str["ts:p1"] = "1"
	var lg runnerLog
	if err := rm.GetUpdatesInBatches("p1", 10, lg.run); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(lg.calls) != 0 {
		t.Fatalf("want runner not called for empty queue, got %d", len(lg.calls))
	}
	if _, ok := f.str["ts:p1"]; !ok {
		t.Fatalf("want ts preserved when nothing applied")
	}
}

// resyncProjectStructureOnly raw back-patching (vendor `update._raw = raw`).
func TestGetUpdatesInBatches_resyncProjectStructureOnlyRaw(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	f.seedList("ops:p1", rawOfOne(map[string]any{"v": 0, "resyncProjectStructureOnly": true}))
	var lg runnerLog
	if err := rm.GetUpdatesInBatches("p1", 10, lg.run); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(lg.calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(lg.calls))
	}
	got, _ := lg.calls[0][0]["_raw"].(string)
	if got == "" || got != rawOfOne(map[string]any{"v": 0, "resyncProjectStructureOnly": true}) {
		t.Fatalf("want _raw back-patched to the raw, got %q", got)
	}
}

func TestGetUpdatesInBatches_runnerErrorStopsLoop(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	f.seedList("ops:p1", rawOf(4)...)
	errs := []error{fakeErr("runner boom")}
	runner := func(batch []map[string]any) error {
		err := errs[0]
		errs[0] = nil
		return err
	}
	if err := rm.GetUpdatesInBatches("p1", 10, runner); err == nil {
		t.Fatalf("want runner error propagated")
	}
}

func TestCountUnprocessedUpdatesAfterSeeds(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	f.seedList("ops:p1", "a", "b", "c")
	n, err := rm.CountUnprocessedUpdates("p1")
	if err != nil || n != 3 {
		t.Fatalf("want 3, got %d %v", n, err)
	}
}
