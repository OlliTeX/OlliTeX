package docstore

import (
	"context"
	"testing"
)

// ag-71 (2026-10-15 live): the nameless UpsertDoc insert branch produced
// live docs rows WITHOUT `name`, breaking every read surface that treats the
// store name as canonical (file listings, LLM context, exports). Pinned:
// an insert carrying the caller-known Name keeps it, and an upsert over a
// nameless row heals the name.
func TestAg71UpsertDocNameParity(t *testing.T) {
	ctx := context.Background()
	store := NewMemStore()

	// insert branch: caller-known name is retained on the row
	if err := store.UpsertDoc(ctx, "p-ag71", "d-ins", 0, WriteUpdates{Lines: int64Slice([]string{"a"}), Name: "main.typ"}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	d, err := store.FindDoc(ctx, "p-ag71", "d-ins", docProj{Name: true, Lines: true}, false)
	if err != nil || d == nil {
		t.Fatalf("find after insert: %v / %+v", err, d)
	}
	if d.Name == nil || *d.Name != "main.typ" {
		t.Fatalf("insert lost name: Name=%v (want main.typ)", d.Name)
	}

	// upsert branch: heal a nameless row on next write
	if err := store.UpsertDoc(ctx, "p-ag71", "d-heal", 0, WriteUpdates{Lines: int64Slice([]string{"b"})}); err != nil {
		t.Fatalf("nameless insert: %v", err)
	}
	if err := store.UpsertDoc(ctx, "p-ag71", "d-heal", 1, WriteUpdates{Lines: int64Slice([]string{"b2"}), Name: "notes.tex"}); err != nil {
		t.Fatalf("heal upsert: %v", err)
	}
	dh, err := store.FindDoc(ctx, "p-ag71", "d-heal", docProj{Name: true}, false)
	if err != nil || dh == nil {
		t.Fatalf("find after heal: %v / %+v", err, dh)
	}
	if dh.Name == nil || *dh.Name != "notes.tex" {
		t.Fatalf("heal upsert did not set name: Name=%v (want notes.tex)", dh.Name)
	}
}
