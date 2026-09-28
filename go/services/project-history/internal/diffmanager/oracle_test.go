// C6 oracle — DiffManager D1–D5.
package diffmanager

import (
	"context"
	"errors"
	"testing"
)

var cctx = context.Background()

func chunkAt(start int, changes ...any) map[string]any {
	return map[string]any{"chunk": map[string]any{
		"startVersion": start,
		"history":      map[string]any{"changes": changes, "snapshot": map[string]any{}},
	}}
}

func TestC6_GetDiffBinary(t *testing.T) {
	d := &Deps{
		ToDiffUpdates: func(ctx context.Context, p string, chunk map[string]any, path string, from, to int) (map[string]any, error) {
			return map[string]any{"binary": true, "initialContent": "", "updates": []any{}}, nil
		},
		GetHistoryId: func(ctx context.Context, p string) (string, error) { return "h", nil },
		GetChunkAtVersion: func(ctx context.Context, p, h string, v int) (map[string]any, error) {
			return chunkAt(0), nil
		},
		BuildDiff: func(s string, u []map[string]any) any { return "DIFFED" },
	}
	got, err := d.GetDiff(cctx, "p", "f", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := got.(map[string]any)
	if !ok || m["binary"] != true {
		t.Fatalf("binary diff: %#v", got)
	}
}

func TestC6_GetDiffTextual(t *testing.T) {
	d := &Deps{
		ToDiffUpdates: func(ctx context.Context, p string, chunk map[string]any, path string, from, to int) (map[string]any, error) {
			return map[string]any{
				"binary":         false,
				"initialContent": "abc",
				"updates":        []any{map[string]any{"i": "x"}},
			}, nil
		},
		GetHistoryId:      func(ctx context.Context, p string) (string, error) { return "h", nil },
		GetChunkAtVersion: func(ctx context.Context, p, h string, v int) (map[string]any, error) { return chunkAt(0), nil },
		BuildDiff: func(s string, u []map[string]any) any {
			if s != "abc" || len(u) != 1 {
				t.Fatalf("args: %q %d", s, len(u))
			}
			return "DIFF"
		},
	}
	got, err := d.GetDiff(cctx, "p", "f", 0, 1)
	if err != nil || got != "DIFF" {
		t.Fatalf("%v %v", got, err)
	}
}

func TestC6_GetFileTreeDiffInconsistentPassthrough(t *testing.T) {
	ice := &InconsistentChunkError{Msg: "inconsistent chunk here"}
	d := &Deps{
		GetHistoryId:      func(ctx context.Context, p string) (string, error) { return "h", nil },
		GetChunkAtVersion: func(ctx context.Context, p, h string, v int) (map[string]any, error) { return chunkAt(0), nil },
		BuildFileTreeDiff: func(chunk map[string]any, from, to int) (any, error) {
			return nil, ice
		},
	}
	_, err := d.GetFileTreeDiff(cctx, "p", 0, 1)
	if !errors.Is(err, ice) {
		t.Fatalf("InconsistentChunkError must pass through untagged: %v", err)
	}
}

func TestC6_GetFileTreeDiffOtherTagged(t *testing.T) {
	boom := errors.New("fold broke")
	d := &Deps{
		GetHistoryId:      func(ctx context.Context, p string) (string, error) { return "h", nil },
		GetChunkAtVersion: func(ctx context.Context, p, h string, v int) (map[string]any, error) { return chunkAt(0), nil },
		BuildFileTreeDiff: func(chunk map[string]any, from, to int) (any, error) {
			return nil, boom
		},
	}
	_, err := d.GetFileTreeDiff(cctx, "p", 0, 1)
	tg, ok := err.(*tagged)
	if !ok || tg.cause != boom {
		t.Fatalf("tag shape: %v (%T)", err, err)
	}
}

func TestC6_ChunkWalkBackward(t *testing.T) {
	seen := []int{}
	d := &Deps{
		GetHistoryId: func(ctx context.Context, p string) (string, error) { return "h", nil },
		GetChunkAtVersion: func(ctx context.Context, p, h string, v int) (map[string]any, error) {
			seen = append(seen, v)
			if v == 5 {
				return chunkAt(3, "op-5→4"), nil
			}
			if v == 3 {
				return chunkAt(1, "op-3→2"), nil
			}
			return chunkAt(0), nil
		},
	}
	chunk, err := d.GetChunksAsSingleChunk(cctx, "p", 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != 5 || seen[1] != 3 {
		t.Fatalf("walk: %v (vendor: downward from toVersion)", seen)
	}
	// D4: changes concatenated earliest-first
	c, _ := chunk["chunk"].(map[string]any)
	h, _ := c["history"].(map[string]any)
	changes, _ := h["changes"].([]any)
	if len(changes) != 2 || changes[0] != "op-3→2" || changes[1] != "op-5→4" {
		t.Fatalf("merged changes: %v (vendor: earliest-first)", changes)
	}
}

func TestC6_TooManyChunks(t *testing.T) {
	defer SetMaxChunkRequests(10)
	SetMaxChunkRequests(3)
	n := 0
	d := &Deps{
		GetHistoryId: func(ctx context.Context, p string) (string, error) { return "h", nil },
		GetChunkAtVersion: func(ctx context.Context, p, h string, v int) (map[string]any, error) {
			n++
			if v > 2 {
				return chunkAt(v - 1), nil
			}
			return chunkAt(0), nil
		},
	}
	_, err := d.GetChunksAsSingleChunk(cctx, "p", 0, 9)
	if err == nil {
		t.Fatal("want 'Diff spans too many chunks'")
	}
	if err.Error() != "Diff spans too many chunks" {
		t.Fatalf("msg: %q", err.Error())
	}
	if n != 3 {
		t.Fatalf("requests: %d (vendor stops at the cap)", n)
	}
}

func TestC6_ProcessUpdatesErrorPropagates(t *testing.T) {
	boom := errors.New("updates down")
	d := &Deps{
		ProcessUpdates: func(ctx context.Context, p string) error { return boom },
		GetHistoryId:   func(ctx context.Context, p string) (string, error) { return "h", nil },
		GetChunkAtVersion: func(ctx context.Context, p, h string, v int) (map[string]any, error) {
			return chunkAt(0), nil
		},
	}
	if _, err := d.GetDiff(cctx, "p", "f", 0, 1); err == nil {
		t.Fatal("want process error")
	}
	if _, err := d.GetFileTreeDiff(cctx, "p", 0, 1); err == nil {
		t.Fatal("want process error (tree)")
	}
}

func TestC6_ChunkFetchErrors(t *testing.T) {
	boom := errors.New("store down")
	d := &Deps{
		GetHistoryId: func(ctx context.Context, p string) (string, error) { return "", boom },
	}
	if _, err := d.GetChunksAsSingleChunk(cctx, "p", 0, 1); err == nil {
		t.Fatal("want hid error")
	}
	d2 := &Deps{
		GetHistoryId:      func(ctx context.Context, p string) (string, error) { return "h", nil },
		GetChunkAtVersion: func(ctx context.Context, p, h string, v int) (map[string]any, error) { return nil, boom },
	}
	if _, err := d2.GetChunksAsSingleChunk(cctx, "p", 0, 1); err == nil {
		t.Fatal("want fetch error")
	}
}

func TestC6_TreeDiffSuccessAndSeams(t *testing.T) {
	d := NewDepsWithB11(Deps{
		GetHistoryId:      func(ctx context.Context, p string) (string, error) { return "h", nil },
		GetChunkAtVersion: func(ctx context.Context, p, h string, v int) (map[string]any, error) { return chunkAt(0), nil },
		BuildFileTreeDiff: func(chunk map[string]any, from, to int) (any, error) { return map[string]any{"type": "diff"}, nil },
	})
	got, err := d.GetFileTreeDiff(cctx, "p", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.(map[string]any)["type"] != "diff" {
		t.Fatalf("tree diff: %#v", got)
	}
	// the B11 default kicks in when no seam is supplied
	d2 := NewDepsWithB11(Deps{})
	if d2.BuildFileTreeDiff == nil {
		t.Fatal("default seam must be installed")
	}
	if _, err := d2.BuildFileTreeDiff(map[string]any{}, 0, 1); err == nil {
		t.Fatal("unwired default must error (no panic)")
	}
}

func TestC6_ToDiffUpdatesErrorTagged(t *testing.T) {
	boom := errors.New("translate failed")
	d := &Deps{
		GetHistoryId:      func(ctx context.Context, p string) (string, error) { return "h", nil },
		GetChunkAtVersion: func(ctx context.Context, p, h string, v int) (map[string]any, error) { return chunkAt(0), nil },
		ToDiffUpdates: func(ctx context.Context, p string, chunk map[string]any, path string, from, to int) (map[string]any, error) {
			return nil, boom
		},
		BuildDiff: func(s string, u []map[string]any) any { return nil },
	}
	_, err := d.GetDiff(cctx, "p", "f", 0, 1)
	if err == nil {
		t.Fatal("want translate error")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("cause must be preserved: %v", err)
	}
}

func TestC6_TaggedErrorShapes(t *testing.T) {
	e := &tagged{}
	if e.Error() != "error" {
		t.Fatalf("empty tagged: %q", e.Error())
	}
	if len(e.Info()) != 0 {
		t.Fatalf("nil info: %#v", e.Info())
	}
	e2 := &tagged{msg: "m", cause: errors.New("c"), info: map[string]any{"k": 1}}
	if e2.Error() != "m" || e2.Info()["k"] != 1 {
		t.Fatalf("%#v", e2)
	}
}

func TestC6_MergeHelpersNilSafe(t *testing.T) {
	a, b := map[string]any{}, map[string]any{"chunk": map[string]any{}}
	mergeChanges(a, b)
	if intAnyOf(map[string]any{}) != 0 {
		t.Fatal("intAnyOf empty → 0")
	}
	if intAnyOf(map[string]any{"startVersion": 7}) != 7 {
		t.Fatal("intAnyOf flat")
	}
}
