package collab

import (
	"context"
	"testing"

	"github.com/reearth/ygo/crdt"
	"github.com/reearth/ygo/persistence"
)

// TestKeepVersionsWiring — the retention knob I introduced: the ygo server's
// auto-compaction path is LegacyAdapter.Compact -> store.Compact(room,
// KeepVersions). This pins that the configured KeepVersions is actually the
// 'keep' the store folds to (the seam my New() wiring depends on).
func TestKeepVersionsWiring(t *testing.T) {
	ctx := context.Background()
	store, err := persistence.NewFilePersistence(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	d := crdt.New(crdt.WithClientID(3))
	for i := 0; i < 6; i++ {
		d.Transact(func(txn *crdt.Transaction) {
			txt := txn.GetText("t")
			txt.Insert(txn, 0, "x", nil)
		})
		upd := crdt.EncodeStateAsUpdateV1(d, nil) // each append = one log entry (cumulative deltas are valid updates)
		if _, err := store.AppendUpdate(ctx, "r", upd); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	// 6 log entries before compaction.
	if metas, _ := store.ListVersions(ctx, "r"); len(metas) != 6 {
		t.Fatalf("pre-compact versions = %d, want 6", len(metas))
	}

	keep := 2
	adapter := persistence.NewLegacyAdapter(store)
	adapter.KeepVersions = keep // the field New() wires from Options
	if err := adapter.Compact(ctx, "r"); err != nil {
		t.Fatalf("adapter.Compact: %v", err)
	}
	metas, err := store.ListVersions(ctx, "r")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(metas) != keep {
		t.Fatalf("post-compact versions = %d, want KeepVersions=%d (adapter did not forward the knob)", len(metas), keep)
	}
	// Head content must survive the fold.
	lr, err := store.Load(ctx, "r")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	rest := crdt.New()
	if err := crdt.ApplyUpdateV1(rest, lr.Update, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := rest.GetText("t").ToString(); len(got) != 6 {
		t.Fatalf("post-compact content len = %d, want 6 (fold lost content)", len(got))
	}
}
