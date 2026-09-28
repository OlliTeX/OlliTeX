package historyot

import (
	"testing"

	"ollitex/go/services/project-history/internal/opmodel"
)

func TestDeleteCommentOpSurface(t *testing.T) {
	o := NewDeleteCommentOperation("c9")
	if o.IsNoOp() || o.IsText() {
		t.Fatal("flags")
	}
	if o.ClassName() != "DeleteCommentOperation" {
		t.Fatal("classname")
	}
	raw := o.ToRaw()
	if raw["deleteComment"] != "c9" {
		t.Fatalf("raw: %v", raw)
	}
	if n, err := o.ApplyToLength(5); err != nil || n != 0 {
		t.Fatal("applyToLength")
	}
	// Apply deletes the comment (and is a no-op when absent).
	f := NewStringFileData("hello world")
	c1, _ := opmodel.NewComment("c9", []opmodel.Range{{Pos: 0, Length: 3}}, false)
	f.Comments.Add(c1)
	if err := o.Apply(f); err != nil {
		t.Fatal(err)
	}
	if f.Comments.GetComment("c9") != nil {
		t.Fatal("comment must be gone")
	}
	absent := NewDeleteCommentOperation("missing")
	if err := absent.Apply(f); err != nil {
		t.Fatal("absent delete must be a no-op")
	}
	// Invert with + without the previous comment.
	prev := NewStringFileData("hello")
	pc, _ := opmodel.NewComment("c9", []opmodel.Range{{Pos: 1, Length: 2}}, true)
	prev.Comments.Add(pc)
	if inv := o.Invert(prev); inv == nil || inv.(*AddCommentOperation).CommentID() != "c9" {
		t.Fatalf("want AddComment inversion, got %T %v", inv, inv)
	}
	if inv := o.Invert(f); inv == nil || !inv.IsNoOp() {
		t.Fatal("want EditNoOperation when comment absent")
	}
	// compose family is vendor-unimplemented.
	if o.CanBeComposedWith(o) || o.CanBeComposedWithForUndo(o) {
		t.Fatal("compose must be false")
	}
	if _, err := o.Compose(o); err == nil {
		t.Fatal("want not implemented")
	}
}

func TestSetCommentStateOpSurface(t *testing.T) {
	o, err := SetCommentStateOperationFromRaw(map[string]any{"commentId": "c1", "resolved": true})
	if err != nil {
		t.Fatal(err)
	}
	if o.IsNoOp() || o.IsText() {
		t.Fatal("flags")
	}
	if o.ClassName() != "SetCommentStateOperation" {
		t.Fatal("classname")
	}
	if n, err := o.ApplyToLength(0); err != nil || n != 0 {
		t.Fatal("applyToLength")
	}
	// apply on a file without the comment → no-op, no panic.
	f := NewStringFileData("text")
	if err := o.Apply(f); err != nil {
		t.Fatal(err)
	}
	// apply with the comment present → resolved flips to the op's value.
	f2 := NewStringFileData("text")
	c1, _ := opmodel.NewComment("c1", []opmodel.Range{{Pos: 0, Length: 1}}, false)
	f2.Comments.Add(c1)
	if err := o.Apply(f2); err != nil {
		t.Fatal(err)
	}
	if got := f2.Comments.GetComment("c1"); got == nil || !got.Resolved {
		t.Fatalf("want resolved, got %+v", got)
	}
	// CanBeComposedWithForUndo is false across the board (vendor).
	if o.CanBeComposedWithForUndo(o) {
		t.Fatal("forUndo must be false")
	}
	// Compose with a different comment id → not implemented.
	other, _ := SetCommentStateOperationFromRaw(map[string]any{"commentId": "zz", "resolved": false})
	if o.CanBeComposedWith(other) {
		t.Fatal("different ids must not compose")
	}
}

func TestEditNoOperationSurface(t *testing.T) {
	o := NewEditNoOperation()
	if !o.IsNoOp() || o.IsText() {
		t.Fatal("flags")
	}
	if o.ClassName() != "EditNoOperation" {
		t.Fatal("classname")
	}
	raw := o.ToRaw()
	if raw["noOp"] != true {
		t.Fatalf("raw: %v", raw)
	}
	if n, err := o.ApplyToLength(3); err != nil || n != 3 {
		t.Fatalf("applyToLength must return the length, got %d %v", n, err)
	}
	f := NewStringFileData("x")
	if err := o.Apply(f); err != nil {
		t.Fatal("apply")
	}
	if inv := o.Invert(f); inv == nil || !inv.IsNoOp() {
		t.Fatal("invert")
	}
	if !o.CanBeComposedWith(o) == false {
		_ = o
	}
	_ = o.CanBeComposedWith(o)
	if _, err := o.Compose(o); err == nil {
		t.Fatal("want not implemented on compose")
	}
}

func TestAddCommentRemainingSurface(t *testing.T) {
	o, err := AddCommentOperationFromRaw(map[string]any{"commentId": "ca", "ranges": []any{map[string]any{"pos": 1, "length": 2}}})
	if err != nil {
		t.Fatal(err)
	}
	if o.IsNoOp() || o.IsText() {
		t.Fatal("flags")
	}
	if o.ClassName() != "AddCommentOperation" {
		t.Fatal("classname")
	}
	if n, err := o.ApplyToLength(10); err != nil || n != 0 {
		t.Fatal("applyToLength")
	}
	// Apply adds the comment to the file.
	f := NewStringFileData("hello")
	if err := o.Apply(f); err != nil {
		t.Fatal(err)
	}
	if f.Comments.GetComment("ca") == nil {
		t.Fatal("comment must exist after Apply")
	}
}

func TestJsTruthyAndAsFloat(t *testing.T) {
	if jsTruthyAny(nil) {
		t.Fatal("nil falsy")
	}
	if !jsTruthyAny(1.0) {
		t.Fatal("nonzero truthy")
	}
	if jsTruthyAny(0.0) {
		t.Fatal("zero falsy")
	}
	if jsTruthyAny("") {
		t.Fatal("empty string falsy")
	}
	if n, ok := asFloat64any(3.0); !ok || n != 3.0 {
		t.Fatalf("float: %v %v", n, ok)
	}
	if n, ok := asFloat64any("x"); ok || n != 0 {
		t.Fatalf("non-float: %v %v", n, ok)
	}
}
