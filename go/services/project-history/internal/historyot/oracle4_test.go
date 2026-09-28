package historyot

import (
	"testing"

	"ollitex/go/services/project-history/internal/opmodel"
)

func TestAddCommentComposeFamily(t *testing.T) {
	add, _ := AddCommentOperationFromRaw(map[string]any{"commentId": "c1", "ranges": []any{map[string]any{"pos": 0, "length": 1}}})
	del := NewDeleteCommentOperation("c1")
	otherAdd, _ := AddCommentOperationFromRaw(map[string]any{"commentId": "c1"})
	state, _ := SetCommentStateOperationFromRaw(map[string]any{"commentId": "c1", "resolved": true})
	// add + delete(same id) → delete wins
	out, err := add.Compose(del)
	if err != nil || out != EditOp(del) {
		t.Fatalf("delete wins: %v %v", out, err)
	}
	// add + add(same id) → the other add
	if out, err = add.Compose(otherAdd); err != nil || out != EditOp(otherAdd) {
		t.Fatalf("same-id add: %v %v", out, err)
	}
	// add + set(same id) → folded add with resolved
	if out, err = add.Compose(state); err != nil {
		t.Fatal(err)
	}
	folded, ok := out.(*AddCommentOperation)
	if !ok || !folded.resolved || folded.commentID != "c1" {
		t.Fatalf("want folded add, got %T %v", out, out)
	}
	// different id → compose error naming both classes.
	if _, err = add.Compose(NewDeleteCommentOperation("zz")); err == nil {
		t.Fatal("want compose error")
	}
	if err == nil {
		t.Fatal("want compose error")
	}
	// CanBeComposedWith matrix (same id true, text/non-matching false).
	if !add.CanBeComposedWith(del) || !add.CanBeComposedWith(otherAdd) || !add.CanBeComposedWith(state) {
		t.Fatal("same-id matrix must be true")
	}
	if add.CanBeComposedWith(NewTextOpAdapter(opmodel.NewTextOperation())) {
		t.Fatal("text must not compose with add")
	}
	if add.CanBeComposedWithForUndo(del) {
		t.Fatal("forUndo must be false")
	}
}

func TestFileSurface(t *testing.T) {
	h40 := "a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0"
	f, err := FileFromHash(h40, "f1e2d3c4b5a6f7e8d9c0b1a2f3e4d5c6b7a8c9d0", map[string]any{"m": 1})
	if err != nil {
		t.Fatal(err)
	}
	raw := f.ToRaw()
	if raw["hash"] != h40 || raw["rangesHash"] == "" {
		t.Fatalf("raw: %v", raw)
	}
	if raw["metadata"].(map[string]any)["m"] != 1 {
		t.Fatal("metadata")
	}
	f.SetMetadata(nil)
	if f.GetMetadata() != nil {
		t.Fatal("nil metadata")
	}
	f.SetMetadata(map[string]any{"k": 2})
	if f.GetMetadata()["k"] != 2 {
		t.Fatal("set metadata")
	}
	// bad ranges hash → error
	if _, err := FileFromHash(h40, "nothex", nil); err == nil {
		t.Fatal("want bad ranges hash error")
	}
	// isEager: content present
	fe := FileFromString("abc", nil)
	if !fe.IsEager() {
		t.Fatal("string file must be eager")
	}
	if f.IsEager() {
		t.Fatal("hash file must not be eager")
	}
	// invalid hex (39 chars) → error
	if _, err := FileFromHash("a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d", "", nil); err == nil {
		t.Fatal("want 39-char hash error")
	}
}

func TestAddFileOpToRawAndSurface(t *testing.T) {
	f, err := FileFromHash("a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	op := NewAddFileOperation("a.tex", f)
	raw := op.ToRaw()
	if raw["file"] == nil || raw["pathname"] != "a.tex" {
		t.Fatalf("raw: %v", raw)
	}
	if op.IsNoOp() {
		t.Fatal("add file is not a noop")
	}
	if op.CanBeComposedWith(op) || op.CanBeComposedWithForUndo(op) {
		t.Fatal("add file compose must be false")
	}
	if _, err := op.Compose(op); err == nil {
		t.Fatal("want not implemented")
	}
}

func TestTextOpAdapterApplyAndInvert(t *testing.T) {
	tt := opmodel.NewTextOperation()
	tt.Insert("hi", nil, nil)
	a := NewTextOpAdapter(tt)
	f := NewStringFileData("")
	if err := a.Apply(f); err != nil {
		t.Fatal(err)
	}
	if got := f.GetContent(); got != "hi" {
		t.Fatalf("want 'hi', got %q", got)
	}
	if n, err := a.ApplyToLength(0); err != nil || n != 2 {
		t.Fatalf("applyToLength: %d %v", n, err)
	}
	if a.ClassName() != "TextOperation" || !a.IsText() {
		t.Fatal("adapter identity")
	}
	// Invert returns an EditOp (the inverse insert is a delete op).
	inv := a.Invert(NewStringFileData(""))
	if inv == nil {
		t.Fatal("invert nil")
	}
	// compose text vs non-text → base-length error path.
	if _, err := a.Compose(NewEditNoOperation()); err == nil {
		t.Fatal("want base-length compose error")
	}
}
