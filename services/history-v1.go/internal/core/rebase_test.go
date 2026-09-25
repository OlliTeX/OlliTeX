package core

import (
	"encoding/json"
	"testing"
	"time"
)

// RebaseChanges tests — a port of the Node oracle
// test/unit/rebase.test.js (rebaseChanges describe block).

func rebTimestamp() time.Time {
	ts, _ := time.Parse(time.RFC3339, "2025-01-02T03:04:05.678Z")
	return ts
}

// rchange builds a Change the same way the Node oracle helper does:
// new Change(operations, timestamp, authors, origin, v2Authors).
func rchange(operations []*Operation, overrides ...func(*Change)) *Change {
	c := NewChange(operations, rebTimestamp(), []any{}, nil, []any{}, "", nil)
	for _, o := range overrides {
		o(c)
	}
	return c
}

func rebaseOpPaths(t *testing.T, rebased []*Change) []string {
	t.Helper()
	paths := make([]string, 0, len(rebased))
	for _, c := range rebased {
		if len(c.Operations) == 0 {
			t.Fatalf("rebased change has no operations")
		}
		paths = append(paths, c.Operations[0].Pathname)
	}
	return paths
}

func TestRebaseChangesLeavesNonConflictAlone(t *testing.T) {
	ours := []*Change{rchange([]*Operation{AddFile("ours.tex", &File{Kind: "string", Content: "content"})})}
	rebased := RebaseChanges(ours, []*Change{rchange([]*Operation{AddFile("theirs.tex", &File{Kind: "string", Content: "content"})})})
	if len(rebased) != 1 {
		t.Fatalf("rebased length = %d, want 1", len(rebased))
	}
	if got := rebased[0].Operations[0].Pathname; got != "ours.tex" {
		t.Errorf("pathname = %q, want ours.tex", got)
	}
}

func TestRebaseChangesDropsSingleNoopChange(t *testing.T) {
	// Two clients add the same pathname. transformAddFileAddFile resolves this
	// in favour of the change that landed first, so ours becomes a no-op.
	ours := []*Change{rchange([]*Operation{AddFile("main.tex", &File{Kind: "string", Content: "ours"})})}
	rebased := RebaseChanges(ours, []*Change{rchange([]*Operation{AddFile("main.tex", &File{Kind: "string", Content: "theirs"})})})
	if len(rebased) != 0 {
		t.Fatalf("rebalanced length = %d, want 0", len(rebased))
	}
}

func TestRebaseChangesPrunesNoopOpsKeepsChange(t *testing.T) {
	ours := []*Change{rchange([]*Operation{
		AddFile("main.tex", &File{Kind: "string", Content: "content"}),
		AddFile("other.tex", &File{Kind: "string", Content: "content"}),
	})}
	rebased := RebaseChanges(ours, []*Change{rchange([]*Operation{AddFile("main.tex", &File{Kind: "string", Content: "content"})})})
	if len(rebased) != 1 {
		t.Fatalf("rebalanced length = %d, want 1", len(rebased))
	}
	if got := len(rebased[0].Operations); got != 1 {
		t.Fatalf("ops length = %d, want 1", got)
	}
	if got := rebased[0].Operations[0].Pathname; got != "other.tex" {
		t.Errorf("pathname = %q, want other.tex", got)
	}
}

func TestRebaseChangesDropsOnlyEmptiedChanges(t *testing.T) {
	ours := []*Change{
		rchange([]*Operation{AddFile("a.tex", &File{Kind: "string", Content: "content"})}),
		rchange([]*Operation{AddFile("collides.tex", &File{Kind: "string", Content: "content"})}),
		rchange([]*Operation{AddFile("b.tex", &File{Kind: "string", Content: "content"})}),
	}
	rebased := RebaseChanges(ours, []*Change{rchange([]*Operation{AddFile("collides.tex", &File{Kind: "string", Content: "content"})})})
	got := rebaseOpPaths(t, rebased)
	want := []string{"a.tex", "b.tex"}
	if len(got) != len(want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("path[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRebaseChangesAllTransformAway(t *testing.T) {
	ours := []*Change{
		rchange([]*Operation{AddFile("a.tex", &File{Kind: "string", Content: "content"})}),
		rchange([]*Operation{AddFile("b.tex", &File{Kind: "string", Content: "content"})}),
	}
	theirs := []*Change{
		rchange([]*Operation{AddFile("a.tex", &File{Kind: "string", Content: "content"})}),
		rchange([]*Operation{AddFile("b.tex", &File{Kind: "string", Content: "content"})}),
	}
	rebased := RebaseChanges(ours, theirs)
	if len(rebased) != 0 {
		t.Fatalf("rebalanced length = %d, want 0", len(rebased))
	}
}

func TestRebaseAgainstEachInterveningChange(t *testing.T) {
	// A rename chain: they moved a.tex -> b.tex, then b.tex -> c.tex. Our edit
	// of a.tex has to end up addressing c.tex, which only happens if the second
	// intervening change is applied to the already-transformed operation.
	ours := []*Change{rchange([]*Operation{
		EditFile("a.tex", NewTextEditOp(&TextOp{Ops: jsonRawScanOps(t, `"hello"`)})),
	})}
	rebased := RebaseChanges(ours, []*Change{
		rchange([]*Operation{MoveFile("a.tex", "b.tex")}),
		rchange([]*Operation{MoveFile("b.tex", "c.tex")}),
	})
	if len(rebased) != 1 {
		t.Fatalf("rebalanced length = %d, want 1", len(rebased))
	}
	if got := rebased[0].Operations[0].Pathname; got != "c.tex" {
		t.Errorf("pathname = %q, want c.tex", got)
	}
}

func TestRebaseSequenceAgainstOneTheirs(t *testing.T) {
	// Both of our changes address a.tex, which they renamed. Each of ours has
	// to be transformed, not just the first.
	ours := []*Change{
		rchange([]*Operation{
			EditFile("a.tex", NewTextEditOp(&TextOp{Ops: jsonRawScanOps(t, `"one"`)})),
		}),
		rchange([]*Operation{
			EditFile("a.tex", NewTextEditOp(&TextOp{Ops: jsonRawScanOps(t, `3`, `"two"`)})),
		}),
	}
	rebased := RebaseChanges(ours, []*Change{
		rchange([]*Operation{MoveFile("a.tex", "renamed.tex")}),
	})
	if len(rebased) != 2 {
		t.Fatalf("rebalanced length = %d, want 2", len(rebased))
	}
	got := rebaseOpPaths(t, rebased)
	want := []string{"renamed.tex", "renamed.tex"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("path[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRebasePreservesResendFields(t *testing.T) {
	// history-v1 identifies its own change read back from history by origin
	// kind, author and timestamp. A rebase must not disturb any of them, or
	// deduplicating a resend stops working.
	authorID := "65b9d7fb2a1b2c3d4e5f6a7b"
	ours := []*Change{
		rchange(
			[]*Operation{
				AddFile("main.tex", &File{Kind: "string", Content: "content"}),
				AddFile("survivor.tex", &File{Kind: "string", Content: "content"}),
			},
			func(c *Change) {
				c.Origin = NewEditorOrigin("")
				c.V2Authors = []any{authorID}
			},
		),
	}
	rebased := RebaseChanges(ours, []*Change{
		rchange([]*Operation{AddFile("main.tex", &File{Kind: "string", Content: "content"})}),
	})
	if len(rebased) != 1 {
		t.Fatalf("rebalanced length = %d, want 1", len(rebased))
	}
	raw := rebased[0].ToRaw()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal rebased raw: %v", err)
	}
	if got := string(fields["origin"]); got != `{"kind":"editor"}` {
		t.Errorf("origin = %s, want {\"kind\":\"editor\"}", got)
	}
	if got := string(fields["v2Authors"]); got != `["`+authorID+`"]` {
		t.Errorf("v2Authors = %s, want %q", got, `["`+authorID+`"]`)
	}
	if got := string(fields["timestamp"]); got != `"2025-01-02T03:04:05.678Z"` {
		t.Errorf("timestamp = %s, want \"2025-01-02T03:04:05.678Z\"", got)
	}
}

func TestRebaseLeavesOursWhenTheirsEmpty(t *testing.T) {
	ours := []*Change{rchange([]*Operation{AddFile("main.tex", &File{Kind: "string", Content: "content"})})}
	rebased := RebaseChanges(ours, nil)
	if len(rebased) != 1 {
		t.Fatalf("rebalanced length = %d, want 1", len(rebased))
	}
	if got := rebased[0].Operations[0].Pathname; got != "main.tex" {
		t.Errorf("pathname = %q, want main.tex", got)
	}
}

func TestRebaseEmptyOurs(t *testing.T) {
	rebased := RebaseChanges(nil, []*Change{rchange([]*Operation{AddFile("main.tex", &File{Kind: "string", Content: "content"})})})
	if len(rebased) != 0 {
		t.Fatalf("rebalanced length = %d, want 0", len(rebased))
	}
}
