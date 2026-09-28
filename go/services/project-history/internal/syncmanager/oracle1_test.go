package syncmanager

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

// --- fakes -------------------------------------------------------------------

type fakeStore struct {
	raw       map[string]any
	found     bool
	inserted  []map[string]any
	updates   []map[string]any
	deleted   []map[string]any
	projectUp []map[string]any
	fail      error
}

func (f *fakeStore) wire(d *Deps) {
	d.FindOneState = func(ctx context.Context, pid string) (map[string]any, bool, error) {
		if f.fail != nil {
			return nil, false, f.fail
		}
		return f.raw, f.found, nil
	}
	d.InsertOneState = func(ctx context.Context, doc map[string]any) error {
		f.inserted = append(f.inserted, doc)
		return nil
	}
	d.UpdateStateDoc = func(ctx context.Context, pid string, update map[string]any, upsert bool) error {
		f.updates = append(f.updates, update)
		return nil
	}
	d.DeleteStateDoc = func(ctx context.Context, pid string, match map[string]any) error {
		f.deleted = append(f.deleted, match)
		return nil
	}
	d.UpdateProjectsDoc = func(ctx context.Context, pid string, update map[string]any) error {
		f.projectUp = append(f.projectUp, update)
		return nil
	}
}

type fakeLock struct {
	runErr    error
	runnerErr error
}

func (fl *fakeLock) wire(d *Deps) {
	d.RunWithLock = func(key string, runner func(extend func() error, release func(error, ...any) error), done func(error, ...any)) {
		if fl.runErr != nil {
			done(fl.runErr)
			return
		}
		extend := func() error { return nil }
		release := func(err error, args ...any) error { done(err, args...); return nil }
		runner(extend, release)
	}
}

var testBase = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

func baseDeps() (*Deps, *fakeStore, *fakeLock) {
	d := &Deps{Now: func() time.Time { return testBase }}
	fs := &fakeStore{found: false}
	fl := &fakeLock{}
	fs.wire(d)
	fl.wire(d)
	d.RecordSyncStart = func(ctx context.Context, pid string) error { return nil }
	d.RecordError = func(ctx context.Context, pid string, queueSize int, err error) error { return nil }
	d.RequestResync = func(ctx context.Context, pid string, opts map[string]any) error { return nil }
	return d, fs, fl
}

func mustErrEq(t *testing.T, err error, msg string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want error %q, got nil", msg)
	}
	if err.Error() != msg {
		t.Fatalf("want %q, got %q", msg, err.Error())
	}
}

// --- FromRaw + ToRaw (vendor SyncState.fromRaw / toRaw) ---------------------

// vendor test: "getResyncState returns defaults when absent"
func TestFromRawDefaults(t *testing.T) {
	s := FromRaw("pid", nil)
	if s.ResyncProjectStructure || len(s.ResyncDocContents) != 0 || s.ResyncCount != 0 || s.StuckClearCount != 0 {
		t.Fatalf("want all defaults, got %+v", s)
	}
	if s.IsSyncOngoing() {
		t.Fatalf("want not ongoing")
	}
}

// vendor: toRaw has EXACTLY 5 keys.
func TestToRawExactKeys(t *testing.T) {
	s := &SyncState{
		ProjectID:              "p",
		ResyncProjectStructure: true,
		ResyncDocContents:      []string{"a.tex"},
		Origin:                 map[string]any{"kind": "history-resync"},
		HardResync:             true,
	}
	raw := s.ToRaw()
	if len(raw) != 5 {
		t.Fatalf("want 5 keys, got %d: %v", len(raw), raw)
	}
	for _, k := range []string{"resyncProjectStructure", "resyncDocContents", "origin", "hardResync", "recoverCorruptedFiles"} {
		if _, ok := raw[k]; !ok {
			t.Fatalf("missing key %s", k)
		}
	}
}

// vendor: fromRaw back-fill walk (history DESC, reversed; first ongoing ts
// wins; non-ongoing entry resets).
func TestFromRawPendingSinceBackfill(t *testing.T) {
	t0 := testBase
	t1 := testBase.Add(time.Hour)
	t2 := testBase.Add(2 * time.Hour)
	hist3 := []map[string]any{
		// DESC storage: newest first
		{"syncState": map[string]any{"resyncProjectStructure": false, "resyncDocContents": []any{}}, "timestamp": t2},
		{"syncState": map[string]any{"resyncProjectStructure": true, "resyncDocContents": []any{}}, "timestamp": t1},
		{"syncState": map[string]any{"resyncProjectStructure": true, "resyncDocContents": []any{}}, "timestamp": t0},
	}
	raw := map[string]any{
		"resyncProjectStructure": true,
		"resyncDocContents":      []any{},
		"history":                hist3,
	}
	s := FromRaw("p", raw)
	// reversed (ascending) walk: t0 ongoing → set t0; t1 ongoing → keep; t2
	// non-ongoing → reset. Newest entry non-ongoing ⇒ final nil.
	if s.ResyncPendingSince != nil {
		t.Fatalf("newest non-ongoing entry should reset pending since, got %v", *s.ResyncPendingSince)
	}
	raw["history"] = []map[string]any{hist3[1], hist3[2]}
	s = FromRaw("p", raw)
	if s.ResyncPendingSince == nil || !s.ResyncPendingSince.Equal(t0) {
		t.Fatalf("want first ongoing ts %v, got %v", t0, s.ResyncPendingSince)
	}
	raw["history"] = []map[string]any{
		{"syncState": map[string]any{"resyncProjectStructure": true, "resyncDocContents": []any{}}, "timestamp": t1},
		{"syncState": map[string]any{"resyncProjectStructure": false, "resyncDocContents": []any{}}, "timestamp": t0},
	}
	s = FromRaw("p", raw)
	if s.ResyncPendingSince == nil || !s.ResyncPendingSince.Equal(t1) {
		t.Fatalf("want t1 (only ongoing entry), got %v", s.ResyncPendingSince)
	}
}

// vendor: back-fill only when sync is ongoing.
func TestFromRawNoBackfillWhenNotOngoing(t *testing.T) {
	raw := map[string]any{
		"resyncProjectStructure": false,
		"resyncDocContents":      []any{"a.tex"},
		"history":                []map[string]any{{"timestamp": testBase}},
	}
	s := FromRaw("p", raw)
	// ongoing via doc contents, but raw has no resyncPendingSince key:
	if s.ResyncPendingSince != nil {
		t.Fatalf("want nil pending since, got %v", *s.ResyncPendingSince)
	}
}

// --- UpdateState (vendor three SyncError branches) ---------------------------

// vendor test: updateState throws when not syncing structure.
func TestUpdateStateStructureNotSyncing(t *testing.T) {
	s := &SyncState{ProjectID: "p"}
	err := s.UpdateState(map[string]any{"resyncProjectStructure": map[string]any{"docs": []any{}}})
	se, ok := err.(*SyncError)
	if !ok {
		t.Fatalf("want SyncError, got %v", err)
	}
	if se.Msg != "unexpected resyncProjectStructure update" {
		t.Fatalf("want structure msg, got %q", se.Msg)
	}
	if se.Info["projectId"] != "p" {
		t.Fatalf("want projectId info")
	}
}

// vendor: throws when doc contents are syncing while structure sync completes.
func TestUpdateStateStructureWithDocSync(t *testing.T) {
	s := &SyncState{ProjectID: "p", ResyncProjectStructure: true, ResyncDocContents: []string{"a.tex"}}
	err := s.UpdateState(map[string]any{"resyncProjectStructure": map[string]any{"docs": []any{}}})
	if err == nil || err.Error() != "unexpected resyncDocContents update" {
		t.Fatalf("want docContents msg, got %v", err)
	}
}

// vendor: structure completion starts doc syncs for docs and stops structure.
func TestUpdateStateStructureCompletion(t *testing.T) {
	s := &SyncState{ProjectID: "p", ResyncProjectStructure: true}
	err := s.UpdateState(map[string]any{"resyncProjectStructure": map[string]any{
		"docs": []any{map[string]any{"path": "a.tex"}, map[string]any{"path": "b.tex"}},
	}})
	if err != nil {
		t.Fatalf("unexpected err %v", err)
	}
	if s.IsProjectStructureSyncing() {
		t.Fatalf("structure sync should be stopped")
	}
	if !s.IsDocContentSyncing("a.tex") || !s.IsDocContentSyncing("b.tex") || len(s.ResyncDocContents) != 2 {
		t.Fatalf("want doc syncs started: %v", s.ResyncDocContents)
	}
}

// vendor: resyncProjectStructureOnly does not start doc syncs.
func TestUpdateStateStructureOnlyNoDocs(t *testing.T) {
	s := &SyncState{ProjectID: "p", ResyncProjectStructure: true}
	err := s.UpdateState(map[string]any{
		"resyncProjectStructure":     map[string]any{"docs": []any{map[string]any{"path": "a.tex"}}},
		"resyncProjectStructureOnly": true,
	})
	if err != nil {
		t.Fatalf("unexpected err %v", err)
	}
	if len(s.ResyncDocContents) != 0 {
		t.Fatalf("want no doc syncs, got %v", s.ResyncDocContents)
	}
}

// vendor: resyncDocContent while structure syncing → error.
func TestUpdateStateDocWhileStructure(t *testing.T) {
	s := &SyncState{ProjectID: "p", ResyncProjectStructure: true}
	err := s.UpdateState(map[string]any{"resyncDocContent": map[string]any{}, "path": "a.tex"})
	if err == nil || err.Error() != "unexpected resyncDocContent update" {
		t.Fatalf("want doc update error, got %v", err)
	}
	info := err.(*SyncError).Info
	if info["resyncProjectStructure"] != true {
		t.Fatalf("want resyncProjectStructure info, got %v", info)
	}
}

// vendor: resyncDocContent for a path not syncing → error with path.
func TestUpdateStateDocNotSyncing(t *testing.T) {
	s := &SyncState{ProjectID: "p"}
	err := s.UpdateState(map[string]any{"resyncDocContent": map[string]any{}, "path": "a.tex"})
	se, ok := err.(*SyncError)
	if !ok || se.Msg != "unexpected resyncDocContent update" {
		t.Fatalf("want doc update error, got %v", err)
	}
	if se.Info["path"] != "a.tex" {
		t.Fatalf("want path info, got %v", se.Info)
	}
}

// vendor: completing a doc sync removes it from the set.
func TestUpdateStateDocCompletion(t *testing.T) {
	s := &SyncState{ProjectID: "p", ResyncDocContents: []string{"a.tex", "b.tex"}}
	err := s.UpdateState(map[string]any{"resyncDocContent": map[string]any{}, "path": "a.tex"})
	if err != nil {
		t.Fatalf("unexpected err %v", err)
	}
	if s.IsDocContentSyncing("a.tex") || !s.IsDocContentSyncing("b.tex") {
		t.Fatalf("want a.tex stopped, got %v", s.ResyncDocContents)
	}
}

// --- ShouldSkipUpdate (vendor) ----------------------------------------------

func TestShouldSkipUpdateMatrix(t *testing.T) {
	s := &SyncState{ProjectID: "p", ResyncProjectStructure: true}
	if s.ShouldSkipUpdate(map[string]any{"resyncProjectStructure": map[string]any{}}) {
		t.Fatalf("sync updates never skipped")
	}
	if s.ShouldSkipUpdate(map[string]any{"resyncDocContent": map[string]any{}, "path": "x"}) {
		t.Fatalf("sync updates never skipped")
	}
	if !s.ShouldSkipUpdate(map[string]any{"version": 3}) {
		t.Fatalf("non-sync updates skipped during structure sync")
	}
	text := map[string]any{
		"doc": "docid", "op": []any{},
		"meta": map[string]any{"pathname": "a.tex", "doc_length": 1},
	}
	s2 := &SyncState{ProjectID: "p", ResyncDocContents: []string{"a.tex"}}
	if !s2.ShouldSkipUpdate(text) {
		t.Fatalf("text update of syncing doc must be skipped")
	}
	text["meta"] = map[string]any{"pathname": "other.tex", "doc_length": 1}
	if s2.ShouldSkipUpdate(text) {
		t.Fatalf("text update of non-syncing doc kept")
	}
}

// --- SetResyncState (vendor ongoing vs non-ongoing shapes) -------------------

func TestSetResyncStateNil(t *testing.T) {
	d, fs, _ := baseDeps()
	if err := d.SetResyncState(context.Background(), "p", nil); err != nil {
		t.Fatalf("nil state must no-op, got %v", err)
	}
	if len(fs.updates) != 0 {
		t.Fatalf("no mongo update expected")
	}
}

func TestSetResyncStateOngoing(t *testing.T) {
	d, fs, _ := baseDeps()
	s := &SyncState{ProjectID: "p", ResyncProjectStructure: true, Origin: map[string]any{}}
	if err := d.SetResyncState(context.Background(), "p", s); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	if len(fs.updates) != 1 {
		t.Fatalf("want 1 update call, got %d", len(fs.updates))
	}
	up := fs.updates[0]
	inc, _ := up["$inc"].(map[string]any)
	if inc["resyncCount"] != 1 {
		t.Fatalf("want $inc resyncCount 1, got %v", up["$inc"])
	}
	unset, _ := up["$unset"].(map[string]any)
	if !reflect.DeepEqual(unset, map[string]any{"expiresAt": true}) {
		t.Fatalf("want $unset expiresAt, got %v", unset)
	}
	minv, _ := up["$min"].(map[string]any)
	if !minv["resyncPendingSince"].(time.Time).Equal(testBase) {
		t.Fatalf("want $min resyncPendingSince now")
	}
	push, _ := up["$push"].(map[string]any)
	hist, _ := push["history"].(map[string]any)
	if hist["$slice"] != 100 || hist["$position"] != 0 {
		t.Fatalf("want history push shape, got %v", hist)
	}
	if len(fs.projectUp) != 0 {
		t.Fatalf("no projects update while ongoing")
	}
}

func TestSetResyncStateNonOngoing(t *testing.T) {
	d, fs, _ := baseDeps()
	s := &SyncState{ProjectID: "p", Origin: map[string]any{}}
	if err := d.SetResyncState(context.Background(), "p", s); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	up := fs.updates[0]
	setv, _ := up["$set"].(map[string]any)
	expAt, ok := setv["expiresAt"].(time.Time)
	if !ok {
		t.Fatalf("want expiresAt set, got %v", setv["expiresAt"])
	}
	want := testBase.Add(90 * 24 * time.Hour)
	if !expAt.Equal(want) {
		t.Fatalf("want %+v, got %+v", want, expAt)
	}
	unset, _ := up["$unset"].(map[string]any)
	for _, k := range []string{"resyncPendingSince", "stuckClearCount", "lastStuckClearAt", "lastStuckDocPaths"} {
		if _, ok := unset[k]; !ok {
			t.Fatalf("want unset %s, got %v", k, unset)
		}
	}
	if len(fs.projectUp) != 1 {
		t.Fatalf("want projects lastResyncedAt max")
	}
}

// --- ClearIfAllAfter (vendor branches) ---------------------------------------

func TestClearIfAllAfterBranches(t *testing.T) {
	t.Run("absent no-op", func(t *testing.T) {
		d, fs, _ := baseDeps()
		if err := d.ClearResyncStateIfAllAfter(context.Background(), "p", testBase); err != nil {
			t.Fatal(err)
		}
		if len(fs.deleted) != 0 {
			t.Fatalf("want no delete")
		}
	})
	t.Run("ongoing no-op", func(t *testing.T) {
		d, fs, _ := baseDeps()
		fs.raw = map[string]any{"resyncProjectStructure": true}
		fs.found = true
		d.ClearResyncStateIfAllAfter(context.Background(), "p", testBase)
		if len(fs.deleted) != 0 {
			t.Fatalf("want no delete while ongoing")
		}
	})
	t.Run("old history preserved", func(t *testing.T) {
		d, fs, _ := baseDeps()
		fs.raw = map[string]any{
			"resyncProjectStructure": false,
			"resyncDocContents":      []any{},
			"history": []map[string]any{
				{"timestamp": testBase.Add(-time.Hour)},
			},
		}
		fs.found = true
		d.ClearResyncStateIfAllAfter(context.Background(), "p", testBase)
		if len(fs.deleted) != 0 {
			t.Fatalf("want no delete for old history")
		}
	})
	t.Run("conditional delete with expiresAt", func(t *testing.T) {
		d, fs, _ := baseDeps()
		exp := testBase.Add(time.Hour)
		fs.raw = map[string]any{
			"resyncProjectStructure": false,
			"resyncDocContents":      []any{},
			"history":                []map[string]any{{"timestamp": testBase.Add(2 * time.Hour)}},
			"expiresAt":              exp,
		}
		fs.found = true
		d.ClearResyncStateIfAllAfter(context.Background(), "p", testBase)
		if len(fs.deleted) != 1 {
			t.Fatalf("want 1 delete")
		}
		if !fs.deleted[0]["expiresAt"].(time.Time).Equal(exp) {
			t.Fatalf("want expiresAt in match, got %v", fs.deleted[0])
		}
	})
}

// --- Clone (vendor) -----------------------------------------------------------

func TestCloneResyncState(t *testing.T) {
	t.Run("absent no-op", func(t *testing.T) {
		d, fs, _ := baseDeps()
		d.CloneResyncState(context.Background(), "src", "dst")
		if len(fs.inserted) != 0 {
			t.Fatalf("want no insert")
		}
	})
	t.Run("strips _id and project_id", func(t *testing.T) {
		d, fs, _ := baseDeps()
		fs.raw = map[string]any{"_id": "x", "project_id": "src", "origin": map[string]any{"kind": "k"}, "resyncCount": 2}
		fs.found = true
		d.CloneResyncState(context.Background(), "src", "dst")
		if len(fs.inserted) != 1 {
			t.Fatalf("want 1 insert")
		}
		doc := fs.inserted[0]
		if doc["project_id"] != "dst" {
			t.Fatalf("want dst id")
		}
		if _, ok := doc["_id"]; ok {
			t.Fatalf("_id must be stripped")
		}
		if doc["resyncCount"] != 2 {
			t.Fatalf("other fields preserved")
		}
	})
}

// --- StartResync / StartHardResync (vendor lock + record) ---------------------

// vendor test: startResync records the error (queueSize -1) and rethrows.
func TestStartResyncRecordsAndRethrows(t *testing.T) {
	d, _, _ := baseDeps()
	recorded := 0
	d.RecordError = func(ctx context.Context, pid string, queueSize int, err error) error {
		recorded++
		if queueSize != -1 {
			t.Fatalf("want queueSize -1, got %d", queueSize)
		}
		return nil
	}
	// force StartResyncWithoutLock to fail: requestResync errors.
	want := errors.New("web down")
	d.RequestResync = func(ctx context.Context, pid string, opts map[string]any) error { return want }
	err := d.StartResync(context.Background(), "p", map[string]any{})
	if err == nil || err != want {
		t.Fatalf("want rethrow %v, got %v", want, err)
	}
	if recorded != 1 {
		t.Fatalf("want 1 record, got %d", recorded)
	}
}

// vendor: startResync success path (metric + lock + no record).
func TestStartResyncSuccess(t *testing.T) {
	d, _, _ := baseDeps()
	incd := []string{}
	d.Inc = func(name string, n int, info map[string]any) { incd = append(incd, name) }
	if err := d.StartResync(context.Background(), "p", map[string]any{}); err != nil {
		t.Fatalf("unexpected %v", err)
	}
	if len(incd) != 1 || incd[0] != "project_history_resync" {
		t.Fatalf("want project_history_resync metric, got %v", incd)
	}
}

// vendor: startHardResync ordering (clear state, clear first op ts, destroy
// queue, start without lock with hard:true) + record on error.
func TestStartHardResyncOrdering(t *testing.T) {
	d, fs, _ := baseDeps()
	var order []string
	d.ClearFirstOpTimestamp = func(ctx context.Context, pid string) error {
		order = append(order, "clearFirstOpTimestamp")
		return nil
	}
	d.DestroyDocUpdatesQueue = func(ctx context.Context, pid string) error {
		order = append(order, "destroyDocUpdatesQueue")
		return nil
	}
	// the clearResyncState delete + web call come in via the seams:
	d.DeleteStateDoc = func(ctx context.Context, pid string, match map[string]any) error {
		order = append(order, "clearResyncState")
		return nil
	}
	d.RequestResync = func(ctx context.Context, pid string, opts map[string]any) error {
		order = append(order, "requestResync")
		if len(opts) != 0 {
			t.Fatalf("web opts must be empty (hard goes into state, not web), got %v", opts)
		}
		return nil
	}
	if err := d.StartHardResync(context.Background(), "p", map[string]any{}); err != nil {
		t.Fatalf("unexpected %v", err)
	}
	want := []string{"clearResyncState", "clearFirstOpTimestamp", "destroyDocUpdatesQueue", "requestResync"}
	if len(order) != len(want) {
		t.Fatalf("order mismatch: %v", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order mismatch: %v", order)
		}
	}
	// hardResync must be true in the stored state (vendor {...options, hard: true})
	if len(fs.updates) == 0 {
		t.Fatalf("want setResyncState update")
	}
	setv, _ := fs.updates[0]["$set"].(map[string]any)
	if setv["hardResync"] != true {
		t.Fatalf("want hardResync true in state, got %v", setv)
	}
}

// vendor: startHardResync records the error (queueSize -1) and rethrows.
func TestStartHardResyncErrorRecorded(t *testing.T) {
	d, _, _ := baseDeps()
	recorded := 0
	d.RecordError = func(ctx context.Context, pid string, queueSize int, err error) error {
		recorded++
		return nil
	}
	d.RequestResync = func(ctx context.Context, pid string, opts map[string]any) error {
		return errors.New("web down")
	}
	err := d.StartHardResync(context.Background(), "p", map[string]any{})
	if err == nil {
		t.Fatalf("want error")
	}
	if recorded != 1 {
		t.Fatalf("want 1 record, got %d", recorded)
	}
}
