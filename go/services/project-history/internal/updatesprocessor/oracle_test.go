package updatesprocessor

import (
	"context"
	"errors"
	"strings"
	"testing"

	pherrs "ollitex/go/services/project-history/internal/errors"
)

// --- fakes --------------------------------------------------------------------

type syncFake struct {
	state    *SyncState
	setCalls []*SyncState

	// overridable hooks (nil → default behavior)
	skipFn      func(ctx context.Context, projectID string, updates []map[string]any) ([]map[string]any, *SyncState, error)
	expandFn    func(ctx context.Context, projectID, historyID string, chunk map[string]any, updates []map[string]any) ([]map[string]any, error)
	getStateFn  func(ctx context.Context, projectID string) (*SyncState, error)
	startFn     func(ctx context.Context, projectID string, opts map[string]any) error
	hardStartFn func(ctx context.Context, projectID string, opts map[string]any) error
}

func (sf *syncFake) SkipUpdatesDuringSync(ctx context.Context, projectID string, updates []map[string]any) ([]map[string]any, *SyncState, error) {
	if sf.skipFn != nil {
		return sf.skipFn(ctx, projectID, updates)
	}
	if sf.state == nil || !sf.state.Ongoing {
		return updates, nil, nil
	}
	return updates, sf.state, nil
}

func (sf *syncFake) SetResyncState(ctx context.Context, projectID string, state *SyncState) error {
	sf.setCalls = append(sf.setCalls, state)
	return nil
}

func (sf *syncFake) ExpandSyncUpdates(ctx context.Context, projectID, historyID string, mostRecentChunk map[string]any, updates []map[string]any) ([]map[string]any, error) {
	if sf.expandFn != nil {
		return sf.expandFn(ctx, projectID, historyID, mostRecentChunk, updates)
	}
	return updates, nil
}

func (sf *syncFake) GetResyncState(ctx context.Context, projectID string) (*SyncState, error) {
	if sf.getStateFn != nil {
		return sf.getStateFn(ctx, projectID)
	}
	if sf.state == nil {
		return &SyncState{Ongoing: false}, nil
	}
	return sf.state, nil
}

func (sf *syncFake) StartResyncWithoutLock(ctx context.Context, projectID string, opts map[string]any) error {
	if sf.startFn != nil {
		return sf.startFn(ctx, projectID, opts)
	}
	return nil
}

func (sf *syncFake) StartHardResync(ctx context.Context, projectID string, opts map[string]any) error {
	if sf.hardStartFn != nil {
		return sf.hardStartFn(ctx, projectID, opts)
	}
	return nil
}

func baseC17(t *testing.T) (*Deps, *syncFake) {
	t.Helper()
	sf := &syncFake{}
	d := &Deps{
		Sync:                    sf,
		CountUnprocessedUpdates: func(ctx context.Context, pid string) (int, error) { return 2, nil },
		ParseDocUpdates:         func(jsonUpdates []string) ([]map[string]any, error) { return nil, nil },
		GetUpdatesInBatches: func(ctx context.Context, pid string, batchSize int, runner func([]map[string]any) error) error {
			return runner([]map[string]any{
				{"doc": "d1", "op": []any{map[string]any{"r": 1}}, "v": 5, "meta": map[string]any{"pathname": "a.tex", "doc_length": 3}},
			})
		},
		ClearDanglingFirstOpTimestamp: func(ctx context.Context, pid string) error { return nil },
		RunWithLock: func(ctx context.Context, key string, runner func(extend func() error) error) error {
			return runner(func() error { return nil })
		},
		Record: func(ctx context.Context, pid string, queueSize int, err error) (map[string]any, error) {
			return map[string]any{"attempts": 2}, nil
		},
		ClearError:       func(ctx context.Context, pid string) error { return nil },
		GetFailureRecord: func(ctx context.Context, pid string) (map[string]any, error) { return nil, nil },
		GetHistoryID:     func(ctx context.Context, pid string) (string, error) { return "hid", nil },
		GetMostRecentChunk: func(ctx context.Context, pid, hid string) (map[string]any, error) {
			return map[string]any{"startVersion": 1}, nil
		},
		GetMostRecentVersion: func(ctx context.Context, pid, hid string) (int, map[string]any, map[string]any, map[string]any, error) {
			return 10, map[string]any{"project": nil, "docs": map[string]any{}}, map[string]any{}, map[string]any{"startVersion": 1}, nil
		},
		SendChanges: func(ctx context.Context, pid, hid string, changes []any, baseVersion int) (bool, error) {
			return false, nil
		},
		CreateBlobsForUpdates: func(ctx context.Context, pid, hid string, updates []map[string]any) ([]map[string]any, error) {
			return updates, nil
		},
		CompressRawUpdates: func(rawUpdates []map[string]any) ([]map[string]any, error) { return rawUpdates, nil },
		ConvertToChanges: func(pid string, updates []map[string]any) ([]map[string]any, error) {
			return []map[string]any{{"operations": []any{map[string]any{"r": 1}}}}, nil
		},
		Now: func() int64 { return 1000 },
	}
	return d, sf
}

func incTracker() *map[string]int {
	m := map[string]int{}
	return &m
}

// --- GetRawUpdates (U2) ---------------------------------------------------------

func TestGetRawUpdatesSuccess(t *testing.T) {
	d, _ := baseC17(t)
	d.GetRawUpdatesBatch = func(ctx context.Context, pid string, batchSize int) ([]string, error) {
		return []string{"j1", "j2"}, nil
	}
	d.ParseDocUpdates = func(jsonUpdates []string) ([]map[string]any, error) {
		if len(jsonUpdates) != 2 {
			t.Fatalf("want batch passthrough, got %v", jsonUpdates)
		}
		return []map[string]any{{"doc": "d"}}, nil
	}
	out, err := d.GetRawUpdates(context.Background(), "p", 500)
	if err != nil {
		t.Fatal(err)
	}
	if out["project_id"] != "p" || out["chunk"] == nil || out["updates"] == nil {
		t.Fatalf("want {project_id, chunk, updates}, got %v", out)
	}
}

func TestGetRawUpdatesErrors(t *testing.T) {
	d, _ := baseC17(t)
	want := errors.New("redis down")
	d.GetRawUpdatesBatch = func(ctx context.Context, pid string, batchSize int) ([]string, error) {
		return nil, want
	}
	if _, err := d.GetRawUpdates(context.Background(), "p", 1); err != want {
		t.Fatalf("want batch error, got %v", err)
	}
	d.GetRawUpdatesBatch = func(ctx context.Context, pid string, batchSize int) ([]string, error) { return []string{}, nil }
	d.ParseDocUpdates = func(jsonUpdates []string) ([]map[string]any, error) { return nil, errors.New("bad json") }
	if _, err := d.GetRawUpdates(context.Background(), "p", 1); err == nil {
		t.Fatalf("want parse error")
	}
	d.ParseDocUpdates = func(jsonUpdates []string) ([]map[string]any, error) { return []map[string]any{}, nil }
	d.GetHistoryID = func(ctx context.Context, pid string) (string, error) { return "", errors.New("web 500") }
	if _, err := d.GetRawUpdates(context.Background(), "p", 1); err == nil {
		t.Fatalf("want web error")
	}
	d.GetHistoryID = func(ctx context.Context, pid string) (string, error) { return "hid", nil }
	d.GetMostRecentChunk = func(ctx context.Context, pid, hid string) (map[string]any, error) {
		return nil, errors.New("store down")
	}
	if _, err := d.GetRawUpdates(context.Background(), "p", 1); err == nil {
		t.Fatalf("want chunk error")
	}
}

// --- getHistoryID (U3: six branches, exact metrics) -----------------------------

func TestGetHistoryIDBranches(t *testing.T) {
	t.Run("inconsistent updates id", func(t *testing.T) {
		d, _ := baseC17(t)
		incd := incTracker()
		d.Inc = func(name string) { (*incd)[name]++ }
		updates := []map[string]any{{"projectHistoryId": "a"}, {"projectHistoryId": "b"}}
		_, err := d.getHistoryID(context.Background(), "p", updates)
		if err == nil || err.Error() != "inconsistent project history id between updates" {
			t.Fatalf("want inconsistent updates error, got %v", err)
		}
		if (*incd)["updates.batches.project-history-id.inconsistent-update"] != 1 {
			t.Fatalf("metric, got %v", *incd)
		}
	})
	t.Run("web error + updates id → from-updates", func(t *testing.T) {
		d, _ := baseC17(t)
		incd := incTracker()
		d.Inc = func(name string) { (*incd)[name]++ }
		d.GetHistoryID = func(ctx context.Context, pid string) (string, error) { return "", errors.New("web 500") }
		hid, err := d.getHistoryID(context.Background(), "p", []map[string]any{{"projectHistoryId": "ua"}})
		if err != nil || hid != "ua" {
			t.Fatalf("want ua, got %v %v", hid, err)
		}
		if (*incd)["updates.batches.project-history-id.from-updates"] != 1 {
			t.Fatalf("metric, got %v", *incd)
		}
	})
	t.Run("web error, no updates id → tag", func(t *testing.T) {
		d, _ := baseC17(t)
		d.GetHistoryID = func(ctx context.Context, pid string) (string, error) { return "", errors.New("web 404") }
		_, err := d.getHistoryID(context.Background(), "p", []map[string]any{})
		if err == nil || !strings.Contains(err.Error(), "web 404") {
			t.Fatalf("want web error, got %v", err)
		}
	})
	t.Run("neither → empty", func(t *testing.T) {
		d, _ := baseC17(t)
		d.GetHistoryID = func(ctx context.Context, pid string) (string, error) { return "", nil }
		hid, err := d.getHistoryID(context.Background(), "p", []map[string]any{})
		if err != nil || hid != "" {
			t.Fatalf("want empty, got %v %v", hid, err)
		}
	})
	t.Run("web only → from-web", func(t *testing.T) {
		d, _ := baseC17(t)
		incd := incTracker()
		d.Inc = func(name string) { (*incd)[name]++ }
		hid, err := d.getHistoryID(context.Background(), "p", []map[string]any{})
		if err != nil || hid != "hid" {
			t.Fatalf("want hid, got %v %v", hid, err)
		}
		if (*incd)["updates.batches.project-history-id.from-web"] != 1 {
			t.Fatalf("metric, got %v", *incd)
		}
	})
	t.Run("updates only → from-updates", func(t *testing.T) {
		d, _ := baseC17(t)
		incd := incTracker()
		d.Inc = func(name string) { (*incd)[name]++ }
		d.GetHistoryID = func(ctx context.Context, pid string) (string, error) { return "", nil }
		hid, err := d.getHistoryID(context.Background(), "p", []map[string]any{{"projectHistoryId": "u1"}})
		if err != nil || hid != "u1" {
			t.Fatalf("want u1, got %v %v", hid, err)
		}
		if (*incd)["updates.batches.project-history-id.from-updates"] != 1 {
			t.Fatalf("metric, got %v", *incd)
		}
	})
	t.Run("mismatch → inconsistent-with-web", func(t *testing.T) {
		d, _ := baseC17(t)
		incd := incTracker()
		d.Inc = func(name string) { (*incd)[name]++ }
		updates := []map[string]any{{"projectHistoryId": "uX"}}
		_, err := d.getHistoryID(context.Background(), "p", updates)
		if err == nil || err.Error() != "inconsistent project history id between updates and web" {
			t.Fatalf("want mismatch error, got %v", err)
		}
		if (*incd)["updates.batches.project-history-id.inconsistent-with-web"] != 1 {
			t.Fatalf("metric, got %v", *incd)
		}
	})
	t.Run("same → from-updates (returns web id)", func(t *testing.T) {
		d, _ := baseC17(t)
		incd := incTracker()
		d.Inc = func(name string) { (*incd)[name]++ }
		hid, err := d.getHistoryID(context.Background(), "p", []map[string]any{{"projectHistoryId": "hid"}})
		if err != nil || hid != "hid" {
			t.Fatalf("want hid, got %v %v", hid, err)
		}
		if (*incd)["updates.batches.project-history-id.from-updates"] != 1 {
			t.Fatalf("metric, got %v", *incd)
		}
	})
}

// --- processForProjectWithLock flows (U5) ---------------------------------------

func TestProcessUpdatesForProjectSuccess(t *testing.T) {
	d, sf := baseC17(t)
	observed := []string{}
	d.Observe = func(name string, value float64) { observed = append(observed, name) }
	clearedDangling := 0
	d.ClearDanglingFirstOpTimestamp = func(ctx context.Context, pid string) error {
		clearedDangling++
		return nil
	}
	if err := d.ProcessUpdatesForProject(context.Background(), "p"); err != nil {
		t.Fatalf("unexpected %v", err)
	}
	if clearedDangling != 1 {
		t.Fatalf("want clearDanglingFirstOpTimestamp")
	}
	if len(observed) != 2 || observed[0] != "historyFlushDurationSeconds" || observed[1] != "historyFlushQueueSize" {
		t.Fatalf("want both observes, got %v", observed)
	}
	// the pipeline must have set the resync state (via _processUpdates)
	if len(sf.setCalls) == 0 {
		t.Fatalf("want setResyncState")
	}
}

func TestProcessUpdatesForProjectRecordError(t *testing.T) {
	d, _ := baseC17(t)
	batchErr := errors.New("batch failed")
	d.GetUpdatesInBatches = func(ctx context.Context, pid string, batchSize int, runner func([]map[string]any) error) error {
		return batchErr
	}
	recordErr := errors.New("record failed")
	recorded := 0
	d.Record = func(ctx context.Context, pid string, queueSize int, err error) (map[string]any, error) {
		recorded++
		if queueSize != 2 {
			t.Fatalf("want queueSize 2, got %d", queueSize)
		}
		return nil, recordErr
	}
	err := d.ProcessUpdatesForProject(context.Background(), "p")
	if recorded != 1 {
		t.Fatalf("want record call")
	}
	if err != recordErr {
		t.Fatalf("want recordError surfaced, got %v", err)
	}
}

func TestProcessUpdatesForProjectFirstHardFailureResyncs(t *testing.T) {
	d, sf := baseC17(t)
	batchErr := errors.New("Error: history store a non-success status code: 422")
	d.GetUpdatesInBatches = func(ctx context.Context, pid string, batchSize int, runner func([]map[string]any) error) error {
		return batchErr
	}
	recorded := 0
	d.Record = func(ctx context.Context, pid string, queueSize int, err error) (map[string]any, error) {
		recorded++
		return map[string]any{
			"attempts": 1,
			"error":    "Error: history store a non-success status code: 422",
		}, nil
	}
	hardResynced := false
	sf.hardStartFn = func(ctx context.Context, pid string, opts map[string]any) error {
		hardResynced = true
		return nil
	}
	warned := 0
	d.LogWarn = func(info map[string]any, msg string) {
		if msg == "Flush failed, attempting resync" {
			warned++
		}
	}
	err := d.ProcessUpdatesForProject(context.Background(), "p")
	if warned != 1 {
		t.Fatalf("want flush-failed warn, got %d", warned)
	}
	if !hardResynced {
		t.Fatalf("want hard resync path")
	}
	// the resync's own flush re-fails (same batch error) and is recorded;
	// the outer callback receives that tagged error (vendor behavior).
	if err == nil {
		t.Fatalf("want the re-failed flush error")
	}
}

func TestProcessUpdatesForProjectNotFirst(t *testing.T) {
	d, _ := baseC17(t)
	batchErr := errors.New("plain failure")
	d.GetUpdatesInBatches = func(ctx context.Context, pid string, batchSize int, runner func([]map[string]any) error) error {
		return batchErr
	}
	d.Record = func(ctx context.Context, pid string, queueSize int, err error) (map[string]any, error) {
		return map[string]any{"attempts": 3}, nil // not first failure → no resync
	}
	err := d.ProcessUpdatesForProject(context.Background(), "p")
	if err != batchErr {
		t.Fatalf("want flushError callback, got %v", err)
	}
}

func TestFlushResyncUpdatesResyncNeeded(t *testing.T) {
	d, _ := baseC17(t)
	// checkResyncState=false → countAndProcess directly; sendChanges reports
	// resyncNeeded → resyncProject path.
	d.SendChanges = func(ctx context.Context, pid, hid string, changes []any, baseVersion int) (bool, error) {
		return true, nil
	}
	sf, sferr := syncFakeFromDeps(d)
	if sferr != nil {
		t.Fatal(sferr)
	}
	resyncCalled := false
	sf.hardStartFn = func(ctx context.Context, pid string, opts map[string]any) error {
		resyncCalled = true
		return nil
	}
	if err := d.FlushResyncUpdates(context.Background(), "p"); err != nil {
		t.Fatalf("unexpected %v", err)
	}
	if !resyncCalled {
		t.Fatalf("want resyncProject because resyncNeeded=true")
	}
}

// syncFakeFromDeps unwraps the Sync fake used in tests.
func syncFakeFromDeps(d *Deps) (*syncFake, error) {
	sf, ok := d.Sync.(*syncFake)
	if !ok {
		return nil, errors.New("not a sync fake")
	}
	return sf, nil
}

// --- resyncProject (U11) --------------------------------------------------------

func TestResyncProjectError(t *testing.T) {
	d, sf := baseC17(t)
	want := errors.New("hard resync failed")
	sf.hardStartFn = func(ctx context.Context, pid string, opts map[string]any) error { return want }
	err := d.ResyncProject(context.Background(), "p")
	if err == nil || err.Error() != want.Error() {
		t.Fatalf("want tagged rethrow, got %v", err)
	}
}

// --- bisect (U8) -----------------------------------------------------------------

func TestBisectRecurseOnError(t *testing.T) {
	d, _ := baseC17(t)
	// first call errors → bisects to batch 0 (2/2=1? vendor: floor(amount/2));
	// amount=1 → floor(1/2)=0; amount=0 with queueSize>0 → recurse again with 0...
	// vendor guards: amount==0 || queueSize==0 → terminal. With amount=2:
	//  err → recurse(1); err → recurse(0); amount 0 → terminal (record).
	counts := []int{}
	d.GetUpdatesInBatches = func(ctx context.Context, pid string, batchSize int, runner func([]map[string]any) error) error {
		counts = append(counts, batchSize)
		return errors.New("still failing")
	}
	recorded := 0
	d.Record = func(ctx context.Context, pid string, queueSize int, err error) (map[string]any, error) {
		recorded++
		return map[string]any{"attempts": 3}, nil
	}
	err := d.ProcessUpdatesForProjectUsingBisect(context.Background(), "p", 2)
	if err == nil {
		t.Fatalf("want error propagation")
	}
	if recorded != 1 {
		t.Fatalf("want terminal record, got %d", recorded)
	}
	if len(counts) == 0 {
		t.Fatalf("want at least one batch attempt")
	}
}

// --- single update (U9) -----------------------------------------------------------

func TestSingleUpdateClears(t *testing.T) {
	d, _ := baseC17(t)
	if err := d.ProcessSingleUpdateForProject(context.Background(), "p"); err != nil {
		t.Fatalf("unexpected %v", err)
	}
}

func TestSingleUpdateError(t *testing.T) {
	d, _ := baseC17(t)
	d.GetUpdatesInBatches = func(ctx context.Context, pid string, batchSize int, runner func([]map[string]any) error) error {
		return errors.New("x")
	}
	err := d.ProcessSingleUpdateForProject(context.Background(), "p")
	if err == nil {
		t.Fatalf("want error")
	}
}

// --- _processUpdates pipeline (U12) -------------------------------------------------

func TestProcessUpdatesSkipEmpty(t *testing.T) {
	d, sf := baseC17(t)
	sf.skipFn = func(ctx context.Context, projectID string, updates []map[string]any) ([]map[string]any, *SyncState, error) {
		return []map[string]any{}, &SyncState{Ongoing: true, StuckClearCount: 1}, nil
	}
	resp, err := d.processUpdates(context.Background(), "p", "hid", []map[string]any{{"version": 7}}, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if resp["resyncNeeded"] != false {
		t.Fatalf("want resyncNeeded false early-return, got %v", resp)
	}
	if len(sf.setCalls) != 1 {
		t.Fatalf("want early setResyncState, got %d", len(sf.setCalls))
	}
}

// withSkipAll swaps in a Sync fake that filters everything out.
func withSkipAll(d *Deps) *Deps {
	d2 := *d
	d2.Sync = skipAllSync{}
	return &d2
}

type skipAllSync struct{}

func (skipAllSync) SkipUpdatesDuringSync(ctx context.Context, projectID string, updates []map[string]any) ([]map[string]any, *SyncState, error) {
	return []map[string]any{}, &SyncState{Ongoing: true, StuckClearCount: 1}, nil
}
func (skipAllSync) SetResyncState(ctx context.Context, projectID string, state *SyncState) error {
	return nil
}
func (skipAllSync) ExpandSyncUpdates(ctx context.Context, projectID, historyID string, mostRecentChunk map[string]any, updates []map[string]any) ([]map[string]any, error) {
	return updates, nil
}
func (skipAllSync) GetResyncState(ctx context.Context, projectID string) (*SyncState, error) {
	return &SyncState{Ongoing: false}, nil
}
func (skipAllSync) StartResyncWithoutLock(ctx context.Context, projectID string, opts map[string]any) error {
	return nil
}
func (skipAllSync) StartHardResync(ctx context.Context, projectID string, opts map[string]any) error {
	return nil
}

func TestProcessUpdatesFullPipeline(t *testing.T) {
	d, _ := baseC17(t)
	incd := incTracker()
	d.Inc = func(name string) { (*incd)[name]++ }
	timed := []string{}
	d.Timing = func(name string, summary int, count int) { timed = append(timed, name) }
	sendCalled := false
	d.SendChanges = func(ctx context.Context, pid, hid string, changes []any, baseVersion int) (bool, error) {
		sendCalled = true
		if baseVersion != 10 {
			t.Fatalf("want base version 10, got %d", baseVersion)
		}
		if len(changes) != 1 {
			t.Fatalf("want 1 change, got %d", len(changes))
		}
		return false, nil
	}
	resp, err := d.processUpdates(context.Background(), "p", "hid", []map[string]any{
		{"doc": "d1", "op": []any{map[string]any{"r": 5}}, "v": 11, "meta": map[string]any{"pathname": "a.tex", "doc_length": 9}},
	}, func() error { return nil })
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}
	if resp["resyncNeeded"] != false {
		t.Fatalf("want resyncNeeded false, got %v", resp)
	}
	if !sendCalled {
		t.Fatalf("want sendChanges")
	}
	for _, want := range []string{"history-store.request.changes", "history-store.request.bytes", "history-store.request.operations"} {
		found := false
		for _, n := range timed {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("want timing %s, got %v", want, timed)
		}
	}
}

func TestProcessUpdatesNoSendWhenEmpty(t *testing.T) {
	d, _ := baseC17(t)
	d.ConvertToChanges = func(pid string, updates []map[string]any) ([]map[string]any, error) {
		return nil, nil // zero changes → no send
	}
	sendCalled := false
	d.SendChanges = func(ctx context.Context, pid, hid string, changes []any, baseVersion int) (bool, error) {
		sendCalled = true
		return false, nil
	}
	resp, err := d.processUpdates(context.Background(), "p", "hid", []map[string]any{{"version": 3}}, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if resp["resyncNeeded"] != false {
		t.Fatalf("want false")
	}
	if sendCalled {
		t.Fatalf("must NOT send when 0 changes")
	}
}

// --- OpsOutOfOrder bypass (U12 step 2) ---------------------------------------------

func TestProcessUpdatesForceDebugBypass(t *testing.T) {
	ooterr := errorsOpsOutOfOrder()
	d, _ := baseC17(t)
	d.GetMostRecentVersion = func(ctx context.Context, pid, hid string) (int, map[string]any, map[string]any, map[string]any, error) {
		return 0, nil, nil, nil, ooterr
	}
	d.GetFailureRecord = func(ctx context.Context, pid string) (map[string]any, error) {
		return nil, nil // no forceDebug → propagate
	}
	d2 := *d
	_, err := d2.processUpdates(context.Background(), "p", "hid", []map[string]any{{"version": 3}}, func() error { return nil })
	if err != ooterr {
		t.Fatalf("want OpsOutOfOrder propagated, got %v", err)
	}
	// with forceDebug the port continues with a null-ish psdv:
	d3 := *d
	d3.GetFailureRecord = func(ctx context.Context, pid string) (map[string]any, error) {
		return map[string]any{"forceDebug": true}, nil
	}
	warned := 0
	d3.LogWarn = func(info map[string]any, msg string) {
		if msg == "ops out of order in chunk, forced continue" {
			warned++
		}
	}
	_, err = d3.processUpdates(context.Background(), "p", "hid", []map[string]any{{"doc": "d", "op": []any{}, "v": 1, "meta": map[string]any{"pathname": "x", "doc_length": 1}}}, func() error { return nil })
	if err != nil {
		t.Fatalf("forceDebug must continue, got %v", err)
	}
	if warned != 1 {
		t.Fatalf("want warn, got %d", warned)
	}
}

func errorsOpsOutOfOrder() error {
	return pherrs.OpsOutOfOrder("out of order in chunk")
}

// --- _skipAlreadyAppliedUpdates (U13/U14) -------------------------------------------

func TestSkipAlreadyAppliedOrderingErrors(t *testing.T) {
	d, _ := baseC17(t)
	_, err := d.skipAlreadyAppliedUpdates("p", []map[string]any{
		{"version": 5},
		{"version": 5}, // duplicate → out of order
	}, map[string]any{"project": nil, "docs": map[string]any{}})
	if err == nil || !strings.Contains(err.Error(), "project structure version out of order on incoming updates") {
		t.Fatalf("want project out-of-order error, got %v", err)
	}
	_, err = d.skipAlreadyAppliedUpdates("p", []map[string]any{
		{"doc": "d", "op": []any{}, "v": 9, "meta": map[string]any{"pathname": "a", "doc_length": 1}},
		{"doc": "d", "op": []any{}, "v": 9, "meta": map[string]any{"pathname": "a", "doc_length": 1}},
	}, map[string]any{"project": nil, "docs": map[string]any{}})
	if err == nil || !strings.Contains(err.Error(), "doc version out of order on incoming updates") {
		t.Fatalf("want doc out-of-order error, got %v", err)
	}
}

func TestSkipAlreadyAppliedDiscards(t *testing.T) {
	d, _ := baseC17(t)
	incd := incTracker()
	d.Inc = func(name string) { (*incd)[name]++ }
	psdv := map[string]any{
		"project": 5,
		"docs":    map[string]any{"d1": map[string]any{"v": 7}},
	}
	out, err := d.skipAlreadyAppliedUpdates("p", []map[string]any{
		{"version": 5}, // already applied (gte) → discard
		{"version": 6}, // new → keep
		{"doc": "d1", "op": []any{}, "v": 7, "meta": map[string]any{"pathname": "a", "doc_length": 1}}, // applied → discard
		{"doc": "d1", "op": []any{}, "v": 8, "meta": map[string]any{"pathname": "a", "doc_length": 1}}, // new → keep
	}, psdv)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 kept, got %v", out)
	}
	if (*incd)["updates.discarded_project_structure_version"] != 1 {
		t.Fatalf("metric, got %v", *incd)
	}
	if (*incd)["updates.discarded_doc_version"] != 1 {
		t.Fatalf("metric, got %v", *incd)
	}
}

func TestSanitizeUpdateSurrogates(t *testing.T) {
	// raw UTF-16 surrogate bytes (the vendor JS strings carry these; in Go
	// they arrive as raw ED xx xx bytes that decode as U+FFFD).
	high := string([]byte{0xED, 0xA0, 0x80}) // unpaired high surrogate
	low := string([]byte{0xED, 0xB0, 0x80})  // unpaired low surrogate
	control := "\u0001"

	raw := map[string]any{
		"op":       []any{map[string]any{"i": "a" + high + "bad"}},
		"docLines": "line" + low + "x",
	}
	sanitizeUpdate(raw)
	i, _ := raw["op"].([]any)
	m, _ := i[0].(map[string]any)
	gotI, _ := m["i"].(string)
	if strings.Contains(gotI, high) {
		t.Fatalf("surrogate bytes must be normalized, got %q", gotI)
	}
	if !strings.Contains(gotI, "\uFFFD") {
		t.Fatalf("want replacement char, got %q", gotI)
	}
	gotLines, _ := raw["docLines"].(string)
	if strings.Contains(gotLines, low) {
		t.Fatalf("docLines surrogate must be normalized, got %q", gotLines)
	}
	// non-surrogates are preserved (vendor only touches D800-DFFF).
	plain := map[string]any{"op": []any{map[string]any{"i": "a" + control + "b"}}}
	sanitizeUpdate(plain)
	i2, _ := plain["op"].([]any)
	m2, _ := i2[0].(map[string]any)
	if m2["i"] != "a\u0001b" {
		t.Fatalf("control char must be preserved, got %v", m2["i"])
	}
}

// --- StartResyncAndProcessUnderLock (U10) -------------------------------------------

func TestStartResyncAndProcessUnderLock(t *testing.T) {
	d, sf := baseC17(t)
	calls := []string{}
	d.ClearDanglingFirstOpTimestamp = func(ctx context.Context, pid string) error {
		calls = append(calls, "clearDangling")
		return nil
	}
	startCalls := 0
	sf.startFn = func(ctx context.Context, pid string, opts map[string]any) error {
		startCalls++
		if opts["origin"] == nil {
			t.Fatalf("want opts passthrough, got %v", opts)
		}
		return nil
	}
	if err := d.StartResyncAndProcessUpdatesUnderLock(context.Background(), "p", map[string]any{"origin": map[string]any{"kind": "k"}}); err != nil {
		t.Fatalf("unexpected %v", err)
	}
	if startCalls != 1 {
		t.Fatalf("want startResyncWithoutLock under lock, got %d calls", startCalls)
	}
	if len(calls) != 1 || calls[0] != "clearDangling" {
		t.Fatalf("want clearDangling tail, got %v", calls)
	}
}

func TestStartResyncAndProcessUnderLockError(t *testing.T) {
	d, sf := baseC17(t)
	want := errors.New("resync refused")
	sf.startFn = func(ctx context.Context, projectID string, opts map[string]any) error { return want }
	recorded := 0
	d.Record = func(ctx context.Context, pid string, queueSize int, err error) (map[string]any, error) {
		recorded++
		return map[string]any{}, nil
	}
	err := d.StartResyncAndProcessUpdatesUnderLock(context.Background(), "p", nil)
	if err == nil || err != want {
		t.Fatalf("want flushError, got %v", err)
	}
	if recorded != 1 {
		t.Fatalf("want record, got %d", recorded)
	}
}
