package redismanager

// Oracle tests mirroring vendor test/unit/js/RedisManager/RedisManagerTests.js
// (overleaf project-history). Vendor's FakeRedis: lrange slice + injectable
// lrange error; lrem with count assertion; setList/getList. Vendor's
// test-updates: {v: i, ...extraFields}; raws are JSON.stringify of those.
//
// Vendor deviation covered here: `resyncDocContent: 123` (number, JS truthy)
// drives the doc-content batch limit — the Go port must use JS truthiness
// (jsTruthy), NOT a Go bool assertion.

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// rawOf builds raw updates mirroring vendor makeUpdates(n, extras...):
// {v: i} plus optional op/resyncDocContent/resyncProjectStructureOnly.
func rawOf(n int, extras ...map[string]any) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		m := map[string]any{"v": i}
		for _, ex := range extras {
			for k, v := range ex {
				m[k] = v
			}
		}
		out = append(out, marshalRaw(m))
	}
	return out
}

func rawOfOne(m map[string]any) string { return marshalRaw(m) }

func marshalRaw(v any) string {
	s, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("marshal: %v", err))
	}
	return string(s)
}

type runnerLog struct {
	calls [][]map[string]any
}

func (r *runnerLog) run(batch []map[string]any) error {
	r.calls = append(r.calls, batch)
	return nil
}

// TestGetRawUpdatesBatchSmallOneBatch: vendor "gets a small number of updates
// in one batch" (2 updates, batch 100).
func TestGetRawUpdatesBatchSmallOneBatch(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	f.seedList("ops:p1", rawOf(2)...)
	batch, err := rm.GetRawUpdatesBatch("p1", 100)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(batch.RAWUpdates) != 2 || batch.HasMore != false {
		t.Fatalf("want 2 raws hasMore=false, got %d hasMore=%v", len(batch.RAWUpdates), batch.HasMore)
	}
	for i, u := range batch.RAWUpdates {
		if u != rawOf(2)[i] {
			t.Fatalf("raw mismatch at %d: %q != %q", i, u, rawOf(2)[i])
		}
	}
}

// TestGetRawUpdatesBatchMultiplePages: vendor "gets a larger number of
// updates in several batches" (RAW_UPDATES_BATCH_SIZE*2 + 12, batch 5000).
func TestGetRawUpdatesBatchMultiplePages(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	n := RawUpdatesBatchSize*2 + 12
	f.seedList("ops:p1", rawOf(n)...)
	batch, err := rm.GetRawUpdatesBatch("p1", 5000)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := rawOf(n)
	if len(batch.RAWUpdates) != n || batch.HasMore {
		t.Fatalf("want %d raws hasMore=false, got %d hasMore=%v", n, len(batch.RAWUpdates), batch.HasMore)
	}
	if len(batch.RAWUpdates[0]) != len(want[0]) || batch.RAWUpdates[n-1] != want[n-1] {
		t.Fatalf("raws mismatch")
	}
}

// TestGetRawUpdatesBatchCap: vendor "doesn't return more than the number of
// updates requested" (100 updates, batch 75 → first 75, hasMore=true).
func TestGetRawUpdatesBatchCap(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	f.seedList("ops:p1", rawOf(100)...)
	batch, err := rm.GetRawUpdatesBatch("p1", 75)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(batch.RAWUpdates) != 75 || !batch.HasMore {
		t.Fatalf("want 75 raws hasMore=true, got %d hasMore=%v", len(batch.RAWUpdates), batch.HasMore)
	}
}

// TestGetUpdatesInBatches_singleBatch: vendor "single batch smaller than
// batch size" (2 updates, batch 3) — runner once, all updates applied, list
// emptied, first-op-timestamp deleted.
func TestGetUpdatesInBatches_singleBatch(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	f.seedList("ops:p1", rawOf(2)...)
	f.str["ts:p1"] = "1"
	var lg runnerLog
	if err := rm.GetUpdatesInBatches("p1", 3, lg.run); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(lg.calls) != 1 || len(lg.calls[0]) != 2 {
		t.Fatalf("want 1 call with 2 updates, got %d calls (first len=%d)", len(lg.calls), len(lg.calls[0]))
	}
	if got, _ := f.LRange("ops:p1", 0, -1); len(got) != 0 {
		t.Fatalf("want empty queue, got %d", len(got))
	}
	if _, ok := f.str["ts:p1"]; ok {
		t.Fatalf("want ts deleted")
	}
}

// TestGetUpdatesInBatches_atBatchSize: vendor "single batch at batch size"
// (123 updates, batch 123) — runner once, 123 updates, queue emptied.
func TestGetUpdatesInBatches_atBatchSize(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	f.seedList("ops:p1", rawOf(123)...)
	f.str["ts:p1"] = "1"
	var lg runnerLog
	if err := rm.GetUpdatesInBatches("p1", 123, lg.run); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(lg.calls) != 1 || len(lg.calls[0]) != 123 {
		t.Fatalf("want 1 call with 123 updates, got %d calls", len(lg.calls))
	}
	if got, _ := f.LRange("ops:p1", 0, -1); len(got) != 0 {
		t.Fatalf("want empty queue")
	}
}

// TestGetUpdatesInBatches_sizeLimitSplits: vendor "single batch exceeding size
// limit" — 2 updates each with a 4 MiB op string; total exceeds
// RAWUpdateSizeThreshold only after the first. Runner called twice (1 + 1);
// both updates applied.
func TestGetUpdatesInBatches_sizeLimitSplits(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	big := map[string]any{"v": 0, "op": stringsRepeat("x", RAWUpdateSizeThreshold)}
	f.seedList("ops:p1", rawOfOne(map[string]any{"v": 0}), rawOfOne(big))
	f.str["ts:p1"] = "1"
	var lg runnerLog
	if err := rm.GetUpdatesInBatches("p1", 123, lg.run); err != nil {
		t.Fatalf("err: %v", err)
	}
	// Vendor expectation: runner called twice — first update alone, then the
	// giant one alone. The size threshold is checked per raw update.
	if len(lg.calls) < 2 {
		t.Fatalf("want >=2 runner calls (size split), got %d", len(lg.calls))
	}
	if got, _ := f.LRange("ops:p1", 0, -1); len(got) != 0 {
		t.Fatalf("want empty queue after split")
	}
}

// TestGetUpdatesInBatches_halfSizeSplit: vendor "two batches with first below
// and second above the size limit" — first update has op of threshold/2,
// second is small; total crosses threshold after first two of them. Runner
// called twice per the vendor oracle (first update alone, rest alone).
func TestGetUpdatesInBatches_halfSizeSplit(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	half := stringsRepeat("x", RAWUpdateSizeThreshold/2)
	f.seedList("ops:p1",
		rawOfOne(map[string]any{"v": 0, "op": half}),
		rawOfOne(map[string]any{"v": 1, "op": half}),
	)
	f.str["ts:p1"] = "1"
	var lg runnerLog
	if err := rm.GetUpdatesInBatches("p1", 123, lg.run); err != nil {
		t.Fatalf("err: %v", err)
	}
	if got, _ := f.LRange("ops:p1", 0, -1); len(got) != 0 {
		t.Fatalf("want empty queue after split")
	}
}

// TestGetUpdatesInBatches_opCountLimit: vendor "single batch exceeding op
// count limit" (2 updates, ops = [MAX_UPDATE_OP_LENGTH+1] ops) — runner
// called twice (first update alone, then second).
func TestGetUpdatesInBatches_opCountLimit(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	ops := make([]any, MaxUpdateOpLength+1)
	for i := range ops {
		ops[i] = "op"
	}
	f.seedList("ops:p1",
		rawOfOne(map[string]any{"v": 0, "op": ops}),
		rawOfOne(map[string]any{"v": 1, "op": ops}),
	)
	f.str["ts:p1"] = "1"
	var lg runnerLog
	if err := rm.GetUpdatesInBatches("p1", 123, lg.run); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(lg.calls) < 2 {
		t.Fatalf("want >=2 calls (op-count split), got %d", len(lg.calls))
	}
	if got, _ := f.LRange("ops:p1", 0, -1); len(got) != 0 {
		t.Fatalf("want empty queue after split")
	}
}

// TestGetUpdatesInBatches_docContentCount: vendor "single batch exceeding doc
// content count" (MAX_NEW_DOC_CONTENT_COUNT + 3 updates with
// resyncDocContent: 123 — a number, JS truthy). Runner called twice: first
// batch = MAX_NEW_DOC_CONTENT_COUNT updates, second = the remainder.
func TestGetUpdatesInBatches_docContentCount(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	n := MaxNewDocContentCount + 3
	for i := 0; i < n; i++ {
		f.seedList("ops:p1", rawOfOne(map[string]any{"v": i, "resyncDocContent": 123}))
	}
	f.str["ts:p1"] = "1"
	var lg runnerLog
	if err := rm.GetUpdatesInBatches("p1", 123, lg.run); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(lg.calls) < 2 {
		t.Fatalf("want >=2 calls (doc-content split), got %d", len(lg.calls))
	}
	// The vendor asserts: first batch exactly MAX_NEW_DOC_CONTENT_COUNT,
	// second = remainder. The JS truthiness (123 → truthy) is what drives
	// this: the Go port must count a non-nil resyncDocContent.
	if len(lg.calls[0]) != MaxNewDocContentCount {
		t.Fatalf("want first batch of %d (doc-content limit), got %d", MaxNewDocContentCount, len(lg.calls[0]))
	}
	if got, _ := f.LRange("ops:p1", 0, -1); len(got) != 0 {
		t.Fatalf("want empty queue after all applied, got %d", len(got))
	}
}

// TestGetUpdatesInBatches_partialThenLimit: vendor "two batches, one partial"
// (15 updates, batch 10) → runner twice (10 + 5).
func TestGetUpdatesInBatches_partialThenLimit(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	f.seedList("ops:p1", rawOf(15)...)
	f.str["ts:p1"] = "1"
	var lg runnerLog
	if err := rm.GetUpdatesInBatches("p1", 10, lg.run); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(lg.calls) != 2 {
		t.Fatalf("want 2 calls (10+5 partial), got %d", len(lg.calls))
	}
	if len(lg.calls[0]) != 10 || len(lg.calls[1]) != 5 {
		t.Fatalf("want batches of 10 and 5, got %d and %d", len(lg.calls[0]), len(lg.calls[1]))
	}
	if got, _ := f.LRange("ops:p1", 0, -1); len(got) != 0 {
		t.Fatalf("want empty queue")
	}
}

// TestGetUpdatesInBatches_twoFullBatches: vendor "two full batches" (20
// updates, batch 10) → runner twice (10 + 10).
func TestGetUpdatesInBatches_twoFullBatches(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	f.seedList("ops:p1", rawOf(20)...)
	f.str["ts:p1"] = "1"
	var lg runnerLog
	if err := rm.GetUpdatesInBatches("p1", 10, lg.run); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(lg.calls) != 2 || len(lg.calls[0]) != 10 || len(lg.calls[1]) != 10 {
		t.Fatalf("want two 10-batches, got %d calls, lens %d %d", len(lg.calls), len(lg.calls[0]), len(lg.calls[1]))
	}
	if got, _ := f.LRange("ops:p1", 0, -1); len(got) != 0 {
		t.Fatalf("want empty queue")
	}
}

// TestGetUpdatesInBatches_threeFullBatchesOverReadSize: vendor "three full
// batches, bigger than the Redis read batch size" (batchSize =
// 2*RAW_UPDATES_BATCH_SIZE = 100; updates = batchSize*3 = 300) → runner
// three times, 100 each.
func TestGetUpdatesInBatches_threeFullBatchesOverReadSize(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	batchSize := RawUpdatesBatchSize * 2
	n := batchSize * 3
	f.seedList("ops:p1", rawOf(n)...)
	f.str["ts:p1"] = "1"
	var lg runnerLog
	if err := rm.GetUpdatesInBatches("p1", batchSize, lg.run); err != nil {
		t.Fatalf("err: %v", err)
	}
	// 300 updates, batchSize 100: each outer round reads 100 (two 50-page
	// reads) and applies it; 3 rounds → 3 runner calls.
	if len(lg.calls) != 3 {
		t.Fatalf("want 3 runner calls, got %d", len(lg.calls))
	}
	for i, c := range lg.calls {
		if len(c) != 100 {
			t.Fatalf("round %d: want 100 updates, got %d", i, len(c))
		}
	}
	if got, _ := f.LRange("ops:p1", 0, -1); len(got) != 0 {
		t.Fatalf("want empty queue")
	}
}

// TestGetUpdatesInBatches_lrangeErrorFirstCall: vendor "error when first
// reading updates" (LRange fails at call 0). getUpdatesInBatches must
// reject, and no updates should be deleted.
func TestGetUpdatesInBatches_lrangeErrorFirstCall(t *testing.T) {
	f := newFake()
	f.failLRangeAt = 0
	rm := New(f, testKeys{})
	f.seedList("ops:p1", rawOf(10)...)
	var lg runnerLog
	err := rm.GetUpdatesInBatches("p1", 2, lg.run)
	if err == nil {
		t.Fatalf("want error on first LRange, got nil")
	}
	if len(lg.calls) != 0 {
		t.Fatalf("want runner not called, got %d calls", len(lg.calls))
	}
	if got, _ := f.LRange("ops:p1", 0, -1); len(got) != 10 {
		t.Fatalf("want all 10 preserved, got %d", len(got))
	}
}

// TestGetUpdatesInBatches_lrangeErrorSecondCall: vendor "error when reading
// updates for a second batch" (LRange fails at call 1; batchSize =
// RAW_UPDATES_BATCH_SIZE - 1). Runner once with first batch, first batch
// deleted, rest preserved.
func TestGetUpdatesInBatches_lrangeErrorSecondCall(t *testing.T) {
	f := newFake()
	f.failLRangeAt = 1
	rm := New(f, testKeys{})
	batchSize := RawUpdatesBatchSize - 1
	n := RawUpdatesBatchSize * 2
	f.seedList("ops:p1", rawOf(n)...)
	f.str["ts:p1"] = "1"
	var lg runnerLog
	err := rm.GetUpdatesInBatches("p1", batchSize, lg.run)
	if err == nil {
		t.Fatalf("want error on second LRange, got nil")
	}
	if len(lg.calls) != 1 {
		t.Fatalf("want runner called once (first batch), got %d calls", len(lg.calls))
	}
	if len(lg.calls[0]) != batchSize {
		t.Fatalf("want first batch of %d, got %d", batchSize, len(lg.calls[0]))
	}
	if got, _ := f.LRange("ops:p1", 0, -1); len(got) != n-batchSize {
		t.Fatalf("want %d preserved (second half), got %d", n-batchSize, len(got))
	}
}

// TestGetProjectIDsWithHistoryOpsLimit: seed ids in the queue and assert
// SCAN + id-extraction + limit cap.
func TestGetProjectIDsWithHistoryOpsLimit(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	for i := 0; i < 3; i++ {
		f.seedList("ops:"+pidHex(i), "x")
	}
	// limit=2 → at most 2 returned.
	ids, err := rm.GetProjectIDsWithHistoryOps(2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(ids) > 2 {
		t.Fatalf("want <=2 ids, got %d: %v", len(ids), ids)
	}
	// no limit → all 3.
	ids, err = rm.GetProjectIDsWithHistoryOps()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("want 3 ids, got %d: %v", len(ids), ids)
	}
}

func TestGetProjectIDsWithFirstOpTimestampsLimit(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	for i := 0; i < 3; i++ {
		f.str["ts:"+pidHex(i)] = "1"
	}
	ids, _ := rm.GetProjectIDsWithFirstOpTimestamps()
	if len(ids) != 3 {
		t.Fatalf("want 3, got %d", len(ids))
	}
}

func TestExtractIDsRegex(t *testing.T) {
	// vendor: extract ids from keys like DocsWithHistoryOps:{<24-hex>}.
	keys := []string{
		"ProjectHistory:Ops:{57fd0b1f53a8396d22b2c24b}",
		"ProjectHistory:Ops:no-id",
		"ProjectHistory:FirstOpTimestamp:{aaaaaaaaaaaaaaaaaaaaaaaa}",
		"unrelated:key",
	}
	got := extractIDs(keys)
	want := []string{"57fd0b1f53a8396d22b2c24b", "aaaaaaaaaaaaaaaaaaaaaaaa"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("index %d: want %s got %s", i, w, got[i])
		}
	}
}

// --- helpers for the oracle suite ---

// pidHex(i) → "57fd0b1f53a8396d22b2c24" + digit, exactly 24 hex.
func pidHex(i int) string {
	return fmt.Sprintf("57fd0b1f53a8396d22b2c24%d", i)
}

func unixMilli(ms int64) time.Time { return time.UnixMilli(ms) }

func stringsRepeat(s string, n int) string {
	out := make([]byte, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, s[0])
	}
	return string(out)
}
