package otc

import (
	"context"
	"strings"
	"testing"
)

// TestHistory_FindBlobHashes pins the Node history.test.js oracle: findBlobHashes
// collects the blob + ranges hashes from the snapshot AND the changes.
func TestHistory_FindBlobHashes(t *testing.T) {
	h := NewHistory(NewSnapshot(nil, nil, nil, nil), nil)

	hashes := map[string]bool{}
	h.FindBlobHashes(hashes)
	if len(hashes) != 0 {
		t.Fatalf("empty history should find 0 hashes; got %d", len(hashes))
	}

	// Add a file with a hash to the snapshot.
	f1, err := FileFromHash(FileEmptyHash, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.GetSnapshot().AddFile("foo", f1); err != nil {
		t.Fatal(err)
	}
	h.FindBlobHashes(hashes)
	if len(hashes) != 1 || !hashes[FileEmptyHash] {
		t.Fatalf("want exactly {EMPTY_FILE_HASH}; got %v", hashes)
	}

	// Add a file with a hash AND a ranges hash to the snapshot.
	snapshotRangesHash := strings.Repeat("0", 40)
	f2, err := FileFromHash(FileEmptyHash, &snapshotRangesHash, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.GetSnapshot().AddFile("foo-with-ranges", f2); err != nil {
		t.Fatal(err)
	}
	h.FindBlobHashes(hashes)
	if len(hashes) != 2 || !hashes[FileEmptyHash] || !hashes[snapshotRangesHash] {
		t.Fatalf("want {EMPTY_FILE_HASH, snapshotRangesHash}; got %v", hashes)
	}

	// Add a file with a hash to a change.
	testHash := strings.Repeat("a", 40)
	c1, err := ChangeFromRaw(map[string]any{
		"operations": []any{},
		"timestamp":  "2015-03-05T12:03:53.035Z",
		"authors":    []any{nil},
	})
	if err != nil {
		t.Fatal(err)
	}
	fa, err := FileFromHash(testHash, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	c1.PushOperation(mustAddOp(t, "bar", fa))

	// Add a file with a hash AND a ranges hash to another change.
	fileHash := strings.Repeat("b", 40)
	rangesHash := strings.Repeat("c", 40)
	c2, err := ChangeFromRaw(map[string]any{
		"operations": []any{},
		"timestamp":  "2015-03-05T12:04:53.035Z",
		"authors":    []any{nil},
	})
	if err != nil {
		t.Fatal(err)
	}
	fb, err := FileFromHash(fileHash, &rangesHash, nil)
	if err != nil {
		t.Fatal(err)
	}
	c2.PushOperation(mustAddOp(t, "bar", fb))

	h.PushChanges([]*Change{c1, c2})
	h.FindBlobHashes(hashes)

	want := map[string]bool{
		FileEmptyHash:      true,
		snapshotRangesHash: true,
		testHash:           true,
		fileHash:           true,
		rangesHash:         true,
	}
	if len(hashes) != len(want) {
		t.Fatalf("want %d hashes %v; got %d %v", len(want), want, len(hashes), hashes)
	}
	for k := range want {
		if !hashes[k] {
			t.Fatalf("missing expected hash %q", k)
		}
	}
}

func mustAddOp(t *testing.T, pathname string, file *File) Operation {
	t.Helper()
	op, err := NewAddFileOperation(pathname, file)
	if err != nil {
		t.Fatal(err)
	}
	return op
}

func TestHistory_Accessors(t *testing.T) {
	snap := NewSnapshot(nil, nil, nil, nil)
	c := NewChange(nil, dummyNow(), nil, nil, nil, nil, nil)
	h := NewHistory(snap, nil)

	if h.GetSnapshot() != snap {
		t.Fatal("GetSnapshot should return the snapshot")
	}
	if h.CountChanges() != 0 {
		t.Fatalf("CountChanges = %d; want 0", h.CountChanges())
	}
	if len(h.GetChanges()) != 0 {
		t.Fatalf("len(GetChanges) = %d; want 0", len(h.GetChanges()))
	}

	h.PushChanges([]*Change{c, c})
	if h.CountChanges() != 2 {
		t.Fatalf("CountChanges = %d; want 2", h.CountChanges())
	}
}

func TestHistory_RoundTrip(t *testing.T) {
	snap := NewSnapshot(nil, nil, nil, nil)
	c := NewChange(nil, dummyNow(), []any{"alice"}, nil, nil, nil, nil)
	h := NewHistory(snap, []*Change{c})

	got, err := HistoryFromRaw(h.ToRaw())
	if err != nil {
		t.Fatal(err)
	}
	if got.CountChanges() != 1 {
		t.Fatalf("round-trip CountChanges = %d; want 1", got.CountChanges())
	}
	if got.GetSnapshot() == nil {
		t.Fatal("round-trip should restore the snapshot")
	}
}

func TestHistory_FromRawBad(t *testing.T) {
	if _, err := HistoryFromRaw(map[string]any{}); err == nil {
		t.Fatal("missing snapshot must error")
	}
	if _, err := HistoryFromRaw(map[string]any{"snapshot": "nope"}); err == nil {
		t.Fatal("non-object snapshot must error")
	}
}

func TestHistory_Store(t *testing.T) {
	snap := NewSnapshot(nil, nil, nil, nil)
	f, err := FileFromHash(FileEmptyHash, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := snap.AddFile("foo", f); err != nil {
		t.Fatal(err)
	}
	c := NewChange(nil, dummyNow(), []any{nil}, nil, nil, nil, nil)
	h := NewHistory(snap, []*Change{c})

	raw, err := h.Store(context.Background(), newFakeBlobStore(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["snapshot"]; !ok {
		t.Fatal("store: missing snapshot key")
	}
	chs, ok := raw["changes"].([]any)
	if !ok || len(chs) != 1 {
		t.Fatalf("store: changes = %v; want 1 element", raw["changes"])
	}
}

func TestHistory_LoadFiles_NoFileOps(t *testing.T) {
	// No add-file operations -> a clean no-op (covers History.LoadFiles +
	// Chunk.LoadFiles delegation without fetching any blob content).
	snap := NewSnapshot(nil, nil, nil, nil)
	c := NewChange(nil, dummyNow(), []any{nil}, nil, nil, nil, nil)
	h := NewHistory(snap, []*Change{c})
	if err := h.LoadFiles(context.Background(), "default", newFakeBlobStore()); err != nil {
		t.Fatalf("History.LoadFiles: %v", err)
	}
	ch := NewChunk(h, 1)
	if err := ch.LoadFiles(context.Background(), "default", newFakeBlobStore()); err != nil {
		t.Fatalf("Chunk.LoadFiles: %v", err)
	}
}
