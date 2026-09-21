package otc

import (
	"testing"
)

func TestChunk_Versions(t *testing.T) {
	snap := NewSnapshot(nil, nil, nil, nil)
	h := NewHistory(snap, nil)
	c := NewChunk(h, 10)

	if c.GetStartVersion() != 10 {
		t.Fatalf("GetStartVersion = %d; want 10", c.GetStartVersion())
	}
	// No changes -> end version == start version.
	if c.GetEndVersion() != 10 {
		t.Fatalf("GetEndVersion (no changes) = %d; want 10", c.GetEndVersion())
	}
	// No changes -> end timestamp is nil (Node: null).
	if c.GetEndTimestamp() != nil {
		t.Fatalf("GetEndTimestamp (no changes) = %v; want nil", c.GetEndTimestamp())
	}

	// Add two changes -> end version advances by 2 and end timestamp is the last.
	c1 := NewChange(nil, dummyNow(), nil, nil, nil, nil, nil)
	c2 := NewChange(nil, dummyNow(), nil, nil, nil, nil, nil)
	c.PushChanges([]*Change{c1, c2})
	if c.GetEndVersion() != 12 {
		t.Fatalf("GetEndVersion (2 changes) = %d; want 12", c.GetEndVersion())
	}
	if ts := c.GetEndTimestamp(); ts == nil || !ts.Equal(dummyNow()) {
		t.Fatalf("GetEndTimestamp = %v; want the last change timestamp", ts)
	}
}

func TestChunk_Accessors(t *testing.T) {
	snap := NewSnapshot(nil, nil, nil, nil)
	h := NewHistory(snap, nil)
	c := NewChunk(h, 5)

	if c.GetHistory() != h {
		t.Fatal("GetHistory should return the history")
	}
	if c.GetSnapshot() != snap {
		t.Fatal("GetSnapshot should return the history's snapshot")
	}
	if len(c.GetChanges()) != 0 {
		t.Fatalf("len(GetChanges) = %d; want 0", len(c.GetChanges()))
	}
}

func TestChunk_RoundTrip(t *testing.T) {
	snap := NewSnapshot(nil, nil, nil, nil)
	h := NewHistory(snap, nil)
	c := NewChunk(h, 7)

	got, err := ChunkFromRaw(c.ToRaw())
	if err != nil {
		t.Fatal(err)
	}
	if got.GetStartVersion() != 7 {
		t.Fatalf("round-trip GetStartVersion = %d; want 7", got.GetStartVersion())
	}
	if got.GetHistory() == nil {
		t.Fatal("round-trip should restore the history")
	}
}

func TestChunk_FromRawBad(t *testing.T) {
	if _, err := ChunkFromRaw(map[string]any{}); err == nil {
		t.Fatal("missing history must error")
	}
	hRaw := map[string]any{"snapshot": map[string]any{}, "changes": []any{}}
	if _, err := ChunkFromRaw(map[string]any{"history": hRaw, "startVersion": "nope"}); err == nil {
		t.Fatal("non-int startVersion must error")
	}
}

func TestChunk_Errors(t *testing.T) {
	// ConflictingEndVersion (Node message, pin exact).
	e := NewChunkConflictingEndVersion(3, 5)
	if msg := e.Error(); msg != "client sent updates with end_version 3 but latest chunk has end_version 5" {
		t.Fatalf("ConflictingEndVersion: %q", msg)
	}

	// NotFoundError default message (Node: `no chunks for project ${projectId}`).
	nf := &ChunkNotFoundError{ProjectID: "proj1"}
	if msg := nf.Error(); msg != "no chunks for project proj1" {
		t.Fatalf("ChunkNotFoundError: %q", msg)
	}

	// VersionNotFoundError.
	ve := NewChunkVersionNotFoundError("proj2", "14")
	if msg := ve.Error(); msg != "chunk for proj2 v 14 not found" {
		t.Fatalf("VersionNotFoundError: %q", msg)
	}

	// BeforeTimestampNotFoundError.
	be := NewChunkBeforeTimestampNotFoundError("proj3", 1700000000000)
	if msg := be.Error(); msg != "chunk for proj3 timestamp 1700000000000 not found" {
		t.Fatalf("BeforeTimestampNotFoundError: %q", msg)
	}

	// NotPersistedError.
	pe := NewChunkNotPersistedError("proj4")
	if msg := pe.Error(); msg != "chunk for proj4 not persisted yet" {
		t.Fatalf("NotPersistedError: %q", msg)
	}
}
