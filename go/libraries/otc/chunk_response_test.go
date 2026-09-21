package otc

import (
	"testing"
)

func TestChunkResponse_RoundTrip(t *testing.T) {
	snap := NewSnapshot(nil, nil, nil, nil)
	h := NewHistory(snap, nil)
	c := NewChunk(h, 3)
	r := NewChunkResponse(c)

	if r.GetChunk() != c {
		t.Fatal("GetChunk should return the chunk")
	}

	raw := r.ToRaw()
	if _, ok := raw["chunk"]; !ok {
		t.Fatal("ToRaw must carry the chunk")
	}

	got, err := ChunkResponseFromRaw(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.GetChunk().GetStartVersion() != 3 {
		t.Fatalf("round-trip startVersion = %d; want 3", got.GetChunk().GetStartVersion())
	}
}

func TestChunkResponse_FromRawNull(t *testing.T) {
	got, err := ChunkResponseFromRaw(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatal("nil raw should return a nil *ChunkResponse (Node: null)")
	}
}

func TestChunkResponse_FromRawBad(t *testing.T) {
	if _, err := ChunkResponseFromRaw(map[string]any{}); err == nil {
		t.Fatal("missing chunk must error")
	}
}
