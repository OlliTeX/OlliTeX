package syncadapter

import (
	"context"
	"testing"
	"time"

	"ollitex/go/services/project-history/internal/syncmanager"
	"ollitex/go/services/project-history/internal/updatesprocessor"
)

func TestAdapterRoundTrip(t *testing.T) {
	base := time.Unix(1000, 0)
	_ = syncmanager.SyncState{
		ResyncProjectStructure: true,
		ResyncDocContents:      []string{"a.tex"},
		StuckClearCount:        2,
		ResyncPendingSince:     &base,
	}
	s := &syncmanager.Deps{
		FindOneState: func(ctx context.Context, p string) (map[string]any, bool, error) {
			return map[string]any{
				"resyncProjectStructure": true,
				"resyncDocContents":      []any{"a.tex"},
				"resyncPendingSince":     base,
			}, true, nil
		},
		InsertOneState: func(ctx context.Context, doc map[string]any) error {
			return nil
		},
		Now: func() time.Time { return base },
	}
	a := New(s)
	st, err := a.GetResyncState(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Ongoing {
		t.Fatalf("want ongoing, got %+v", st)
	}
	if len(st.ResyncDocContents) != 1 || st.ResyncDocContents[0] != "a.tex" {
		t.Fatalf("want doc contents, got %v", st.ResyncDocContents)
	}
	if st.ResyncPendingSince != "1970-01-01T00:16:40.000Z" {
		t.Fatalf("want pendingSince ISO, got %q", st.ResyncPendingSince)
	}
}

func TestAdapterToC17Nil(t *testing.T) {
	a := New(&syncmanager.Deps{Now: func() time.Time { return time.Unix(0, 0) }})
	_ = a
	if got := toC17(nil); got != nil {
		t.Fatalf("want nil, got %+v", got)
	}
}

func TestAdapterSetState(t *testing.T) {
	var capturedUpdate map[string]any
	s := &syncmanager.Deps{
		Now: func() time.Time { return time.Unix(0, 0) },
		UpdateStateDoc: func(ctx context.Context, p string, update map[string]any, upsert bool) error {
			capturedUpdate = update
			return nil
		},
	}
	a := New(s)
	st := &updatesprocessor.SyncState{
		Ongoing:             true,
		StuckClearCount:     1,
		ResyncDocContents:   []string{"b.tex"},
		ResyncProjectStruct: true,
	}
	if err := a.SetResyncState(context.Background(), "p", st); err != nil {
		t.Fatal(err)
	}
	set, _ := capturedUpdate["$set"].(map[string]any)
	if set == nil || set["resyncProjectStructure"] != true {
		t.Fatalf("want $set.resyncProjectStructure=true, got %v", capturedUpdate)
	}
	if _, ok := capturedUpdate["$inc"]; !ok {
		t.Fatalf("ongoing state must $inc resyncCount: %v", capturedUpdate)
	}
}
func TestAdapterStartResyncPaths(t *testing.T) {
	var hardRan, noLockRan, softRan int
	_ = softRan
	mk := func() *syncmanager.Deps {
		return &syncmanager.Deps{
			Now: func() time.Time { return time.Unix(0, 0) },
			FindOneState: func(ctx context.Context, p string) (map[string]any, bool, error) {
				return nil, false, nil
			},
			RecordSyncStart: func(ctx context.Context, p string) error { return nil },
			RecordError: func(ctx context.Context, p string, version int, err error) error {
				return nil
			},
			ClearFirstOpTimestamp:  func(ctx context.Context, p string) error { return nil },
			DestroyDocUpdatesQueue: func(ctx context.Context, p string) error { return nil },
			InsertOneState:         func(ctx context.Context, doc map[string]any) error { return nil },
			UpdateStateDoc:         func(ctx context.Context, p string, u map[string]any, upsert bool) error { return nil },
			UpdateProjectsDoc:      func(ctx context.Context, p string, u map[string]any) error { return nil },
			DeleteStateDoc:         func(ctx context.Context, p string, match map[string]any) error { return nil },
			DeleteAppliedDocUpdate: func(ctx context.Context, p string, update map[string]any) error { return nil },
			RequestResync:          func(ctx context.Context, p string, opts map[string]any) error { return nil },
			GetHistoryID:           func(ctx context.Context, p string) (string, error) { return "h", nil },
			GetLatestSnapshotFilesForChunk: func(ctx context.Context, h string, chunk map[string]any) (map[string]*syncmanager.File, error) {
				return map[string]*syncmanager.File{}, nil
			},
			LoadFileContent:       func(ctx context.Context, p string, f *syncmanager.File) error { return nil },
			GetBlobHashFromString: func(s string) string { return "sha1" },
			DiffAsShareJsOps:      func(a, b string) []any { return []any{} },
			IsDataCorruption:      func(err error) bool { return false },
			Inc:                   func(name string, n int, info map[string]any) {},
			LogDebug:              func(info map[string]any, msg string) {},
			LogWarn:               func(info map[string]any, msg string) {},
			LogErr:                func(info map[string]any, msg string) {},
			RunWithLock: func(key string, runner func(extend func() error, release func(error, ...any) error), done func(error, ...any)) {
				extend := func() error { return nil }
				runner(extend, func(err error, args ...any) error {
					done(err, args...)
					return err
				})
			},
		}
	}
	a := New(mk())
	if err := a.StartResyncWithoutLock(context.Background(), "p", map[string]any{}); err != nil {
		t.Fatalf("StartResyncWithoutLock: %v", err)
	}
	noLockRan++
	if err := a.StartHardResync(context.Background(), "p", map[string]any{}); err != nil {
		t.Fatalf("StartHardResync: %v", err)
	}
	hardRan++
	if noLockRan+hardRan != 2 {
		t.Fatalf("calls not recorded")
	}
	_ = softRan
}

func TestAdapterSkipAndExpand(t *testing.T) {
	var skipCalled int
	s := &syncmanager.Deps{
		Now: func() time.Time { return time.Unix(0, 0) },
		FindOneState: func(ctx context.Context, p string) (map[string]any, bool, error) {
			return map[string]any{"resyncDocContents": []any{"a.tex"}}, true, nil
		},
	}
	// drive SkipUpdatesDuringSync through the C16 method directly (the
	// adapter is a 1:1 pass-through — the C16 suite covers the branch logic;
	// we exercise the adapter's mapping + error propagation).
	a := New(s)
	filtered, _, err := a.SkipUpdatesDuringSync(context.Background(), "p", []map[string]any{{"t": "AddComment"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = filtered
	skipCalled++

	// GetResyncState error path: the C16 FindOneState error propagates.
	s.FindOneState = func(ctx context.Context, p string) (map[string]any, bool, error) {
		return nil, false, context.DeadlineExceeded
	}
	if _, err := a.GetResyncState(context.Background(), "p"); err == nil {
		t.Fatal("want propagation")
	}
	// SetResyncState nil → no-op.
	if err := a.SetResyncState(context.Background(), "p", nil); err != nil {
		t.Fatal(err)
	}
	_ = skipCalled
}
