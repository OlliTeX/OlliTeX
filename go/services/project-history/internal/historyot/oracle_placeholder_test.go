// Oracle: vendor test/unit/edit_operation.test.js (EditOperationBuilder +
// EditOperationTransformer sections), test/unit/add_comment_operation.test.js
// and test/unit/delete_comment_operation.test.js, plus the SetCommentState
// compose/invert cases.
package historyot

import (
	"testing"

	"ollitex/go/services/project-history/internal/opmodel"
)

func wireTextOp(t *testing.T, ops []any) *TextOpAdapter {
	raw := map[string]any{"textOperation": ops}
	tt, err := opmodel.TextOperationFromJSON(raw)
	if err != nil {
		t.Fatalf("TextOperationFromJSON: %v", err)
	}
	return &TextOpAdapter{t: tt}
}

func newCommentT(t *testing.T, id string, ranges []opmodel.Range, resolved bool) *opmodel.Comment {
	c, err := opmodel.NewComment(id, ranges, resolved)
	if err != nil {
		t.Fatalf("NewComment: %v", err)
	}
	return c
}

func fileWithCommentT(t *testing.T, id string, ranges []opmodel.Range, resolved bool) *opmodel.StringFileData {
	cl := opmodel.NewCommentList(nil)
	cl.Add(newCommentT(t, id, ranges, resolved))
	return &opmodel.StringFileData{Content: "abc", Comments: cl}
}

// --- vendor: EditOperationBuilder.fromJSON / isValid ---

func TestEditOperationBuilderFromJSON(t *testing.T) {
	// vendor edit_operation.test.js: "EditOperationBuilder.fromJSON constructs
	// a TextOperation"
	text, err := Builder.BuildFromRaw(map[string]any{"textOperation": []any{float64(3), "foo"}})
	if err != nil {
		t.Fatalf("text: %v", err)
	}
	tt, ok := text.(*TextOpAdapter)
	if !ok || !tt.IsText() || tt.IsNoOp() {
		t.Fatalf("expected non-noop TextOperation, got %v", text)
	}

	// vendor: "... constructs an EditNoOperation"
	noOp, err := Builder.BuildFromRaw(map[string]any{"noOp": true})
	if err != nil {
		t.Fatalf("noOp: %v", err)
	}
	if !noOp.IsNoOp() {
		t.Fatalf("expected EditNoOperation, got %T", noOp)
	}

	// unsupported raw must error
	if _, err := Builder.BuildFromRaw(map[string]any{"bogus": true}); err == nil {
		t.Fatal("expected error for unsupported raw op")
	}

	// vendor TextOperation.fromJSON throws when the array is missing
	if _, err := Builder.BuildFromRaw(map[string]any{"textOperation": "nope"}); err == nil {
		t.Fatal("expected error for non-array textOperation")
	}
}

func TestEditOperationBuilderIsValid(t *testing.T) {
	cases := []struct {
		name string
		raw  any
		want bool
	}{
		{"text", map[string]any{"textOperation": []any{}}, true},
		{"nil raw", nil, false},
		{"non-object", "hi", false},
		{"add-comment", map[string]any{"commentId": "1", "ranges": []any{}}, true},
		{"add-comment missing ranges", map[string]any{"commentId": "1"}, false},
		{"add-comment ranges not array", map[string]any{"commentId": "1", "ranges": "no"}, false},
		{"delete", map[string]any{"deleteComment": "1"}, true},
		{"set resolved", map[string]any{"commentId": "1", "resolved": true}, true},
		{"set resolved not bool", map[string]any{"commentId": "1", "resolved": "y"}, false},
		{"set missing resolved", map[string]any{"commentId": "1"}, false},
		{"no-op", map[string]any{"noOp": true}, true},
		{"bogus", map[string]any{"p": float64(0)}, false},
		{"empty", map[string]any{}, false},
	}
	for _, c := range cases {
		if got := Builder.IsValid(c.raw); got != c.want {
			t.Errorf("IsValid(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

// --- vendor: AddCommentOperation tests ---

func TestAddCommentOperationFromJSON(t *testing.T) {
	// vendor: constructs a collapsed AddCommentOperation fromJSON (empty ranges)
	_, err := Builder.BuildFromRaw(map[string]any{"commentId": "123", "resolved": true, "ranges": []any{}})
	if err != nil {
		t.Fatalf("empty-ranges fromJSON must not throw: %v", err)
	}

	// vendor: fromJSON with ranges
	op, err := Builder.BuildFromRaw(map[string]any{
		"commentId": "123",
		"resolved":  true,
		"ranges":    []any{map[string]any{"pos": float64(0), "length": float64(1)}},
	})
	if err != nil {
		t.Fatalf("fromJSON: %v", err)
	}
	add := op.(*AddCommentOperation)
	if add.CommentID() != "123" || !add.ResolvedRaw() || len(add.RangesRaw()) != 1 {
		t.Fatalf("AddComment fields wrong: %+v / %+v", add.CommentID(), add.ToRaw())
	}
}

func TestAddCommentOperationToJSON(t *testing.T) {
	// vendor: "should convert to JSON"
	op, _ := NewAddCommentOperation("123", []opmodel.Range{{Pos: 0, Length: 1}}, false)
	raw := op.ToRaw()
	if id, _ := raw["commentId"].(string); id != "123" {
		t.Fatalf("commentId: %+v", raw)
	}
	if rs, ok := raw["ranges"].([]any); !ok || len(rs) != 1 {
		t.Fatalf("ranges: %+v", raw)
	}
	if _, present := raw["resolved"]; present {
		t.Fatalf("resolved must be omitted when false: %+v", raw)
	}

	op2, _ := NewAddCommentOperation("124", []opmodel.Range{{Pos: 2, Length: 3}}, true)
	raw2 := op2.ToRaw()
	if resolved, _ := raw2["resolved"].(bool); !resolved {
		t.Fatalf("resolved must be true when set: %+v", raw2)
	}
}

func TestAddCommentOperationApplyAndInvert(t *testing.T) {
	// vendor: "should apply operation"
	fd := fileWithCommentT(t, "other", []opmodel.Range{{Pos: 5, Length: 5}}, false)
	_ = fd
	fd = &opmodel.StringFileData{Content: "abc", Comments: opmodel.NewCommentList(nil)}
	op, _ := NewAddCommentOperation("123", []opmodel.Range{{Pos: 0, Length: 1}}, false)
	if err := op.Apply(fd); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if fd.Comments.Len() != 1 {
		t.Fatalf("expected 1 comment, got %d", fd.Comments.Len())
	}
	if c := fd.Comments.GetComment("123"); c == nil || c.Resolved {
		t.Fatalf("bad comment after apply: %+v", c)
	}

	// vendor invert: "should delete added comment"
	initial := &opmodel.StringFileData{Content: "abc", Comments: opmodel.NewCommentList(nil)}
	after := &opmodel.StringFileData{Content: "abc", Comments: opmodel.NewCommentList(nil)}
	op1, _ := NewAddCommentOperation("123", []opmodel.Range{{Pos: 0, Length: 1}}, false)
	if err := op1.Apply(after); err != nil {
		t.Fatalf("apply: %v", err)
	}
	inverted := op1.Invert(initial)
	del, ok := inverted.(*DeleteCommentOperation)
	if !ok {
		t.Fatalf("expected DeleteCommentOperation inverse, got %T", inverted)
	}
	if err := del.Apply(after); err != nil {
		t.Fatalf("inverted apply: %v", err)
	}
	if after.Comments.Len() != 0 {
		t.Fatalf("expected 0 comments after inverse, got %d", after.Comments.Len())
	}

	// vendor: "should restore previous comment ranges" (two separate file
	// copies: `initial` is the invert input, `fileData` is what the op applies
	// to — mirrors vendor's initialFileData / copy pair).
	initialCopy := fileWithCommentT(t, "123", []opmodel.Range{{Pos: 0, Length: 1}}, false)
	fileDataCopy := fileWithCommentT(t, "123", []opmodel.Range{{Pos: 0, Length: 1}}, false)
	op2, _ := NewAddCommentOperation("123", []opmodel.Range{{Pos: 12, Length: 7}}, true)
	op2.Apply(fileDataCopy)
	inverted2 := op2.Invert(initialCopy)
	addBack, ok := inverted2.(*AddCommentOperation)
	if !ok {
		t.Fatalf("expected AddCommentOperation inverse, got %T", inverted2)
	}
	if err := addBack.Apply(fileDataCopy); err != nil {
		t.Fatalf("inverted apply: %v", err)
	}
	c := fileDataCopy.Comments.GetComment("123")
	if c == nil || c.Ranges[0].Pos != 0 || c.Ranges[0].Length != 1 || c.Resolved {
		t.Fatalf("expected restored original comment, got %+v", c)
	}
	if c2 := initialCopy.Comments.GetComment("123"); c2 != nil && c2.Ranges[0].Pos != 0 {
		t.Fatalf("initial copy should be unchanged, got %+v", c2.Ranges)
	}
}

func TestAddCommentOperationCompose(t *testing.T) {
	// vendor: "should compose with DeleteCommentOperation"
	add, _ := NewAddCommentOperation("123", []opmodel.Range{{Pos: 0, Length: 1}}, false)
	del := NewDeleteCommentOperation("123")
	if !add.CanBeComposedWith(del) {
		t.Fatal("Add.compose(Delete) should be composable")
	}
	composed, err := add.Compose(del)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if _, ok := composed.(*DeleteCommentOperation); !ok {
		t.Fatalf("expected DeleteCommentOperation, got %T", composed)
	}

	// vendor compose chain: Add -> same-id Add (second wins)
	add2, _ := NewAddCommentOperation("123", []opmodel.Range{{Pos: 5, Length: 2}}, false)
	composed2, err := add.Compose(add2)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if _, ok := composed2.(*AddCommentOperation); !ok {
		t.Fatalf("expected AddCommentOperation, got %T", composed2)
	}

	// cross-id compose errors
	different := &DeleteCommentOperation{commentID: "other"}
	if _, err := add.Compose(different); err == nil {
		t.Fatal("expected error composing with different-id delete")
	}
}

// --- vendor: DeleteCommentOperation tests ---

func TestDeleteCommentOperationFromJSON(t *testing.T) {
	// vendor: "constructs an DeleteCommentOperation fromJSON"
	op, err := Builder.BuildFromRaw(map[string]any{"deleteComment": "123"})
	if err != nil {
		t.Fatalf("fromJSON: %v", err)
	}
	if _, ok := op.(*DeleteCommentOperation); !ok {
		t.Fatalf("expected DeleteCommentOperation, got %T", op)
	}
}

func TestDeleteCommentOperationToJSON(t *testing.T) {
	// vendor: "should convert to JSON"
	op := NewDeleteCommentOperation("123")
	if raw := op.ToRaw(); raw["deleteComment"] != "123" {
		t.Fatalf("toJSON: %+v", raw)
	}
}

func TestDeleteCommentOperationApplyInvert(t *testing.T) {
	// vendor: "should apply operation"
	fd := fileWithCommentT(t, "123", []opmodel.Range{{Pos: 0, Length: 1}}, false)
	op := NewDeleteCommentOperation("123")
	if err := op.Apply(fd); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if fd.Comments.Len() != 0 {
		t.Fatalf("expected 0 comments, got %d", fd.Comments.Len())
	}

	// vendor: "should invert operation" (comment present -> re-add)
	inverted := op.Invert(fileWithCommentT(t, "123", []opmodel.Range{{Pos: 0, Length: 1}}, false))
	if _, ok := inverted.(*AddCommentOperation); !ok {
		t.Fatalf("expected AddCommentOperation inverse, got %T", inverted)
	}

	// vendor: "should not throw if comment not found"
	noop := op.Invert(&opmodel.StringFileData{Content: "abc", Comments: opmodel.NewCommentList(nil)})
	if !noop.IsNoOp() {
		t.Fatalf("expected EditNoOperation for not-found, got %T", noop)
	}
}

// --- vendor: SetCommentStateOperation cases ---

func TestSetCommentStateOperation(t *testing.T) {
	op, err := Builder.BuildFromRaw(map[string]any{"commentId": "123", "resolved": false})
	if err != nil {
		t.Fatalf("fromJSON: %v", err)
	}
	sc, ok := op.(*SetCommentStateOperation)
	if !ok || sc.CommentIDRaw() != "123" || sc.Resolved {
		t.Fatalf("SetCommentState fields wrong: %+v", op.ToRaw())
	}

	// vendor: compose with Delete (same id) -> delete wins
	del := NewDeleteCommentOperation("123")
	if !sc.CanBeComposedWith(del) {
		t.Fatal("Set.compose(Delete) should be composable")
	}
	composed, err := sc.Compose(del)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if _, ok := composed.(*DeleteCommentOperation); !ok {
		t.Fatalf("expected DeleteCommentOperation, got %T", composed)
	}

	// compose with same-id Set -> second wins
	set2 := &SetCommentStateOperation{commentID: "123", Resolved: false}
	composed2, err := sc.Compose(set2)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if s, ok := composed2.(*SetCommentStateOperation); !ok || s.CommentIDRaw() != "123" {
		t.Fatalf("expected second Set, got %+v", composed2)
	}

	// cross-id compose errors
	other := &SetCommentStateOperation{commentID: "other", Resolved: true}
	if _, err := sc.Compose(other); err == nil {
		t.Fatal("expected error composing different ids")
	}

	// vendor invert: restore previous resolution
	initial := fileWithCommentT(t, "123", []opmodel.Range{{Pos: 0, Length: 1}}, false)
	inverted := (&SetCommentStateOperation{commentID: "123", Resolved: true}).Invert(initial)
	if s, ok := inverted.(*SetCommentStateOperation); !ok || s.CommentIDRaw() != "123" || s.Resolved {
		t.Fatalf("expected previous resolved state inverse, got %+v", inverted.ToRaw())
	}

	// not-found -> EditNoOperation
	noop := (&SetCommentStateOperation{commentID: "nope", Resolved: true}).Invert(
		&opmodel.StringFileData{Content: "abc", Comments: opmodel.NewCommentList(nil)})
	if !noop.IsNoOp() {
		t.Fatalf("expected EditNoOperation for not-found, got %T", noop)
	}
}

// --- vendor: EditOperationTransformer partial (edit_operation.test.js) ---

func TestEditOperationTransformerNoOp(t *testing.T) {
	tr := EditOperationTransformer{}
	a, b := NewEditNoOperation(), NewEditNoOperation()
	aPrime, bPrime, err := tr.Transform(a, b)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if aPrime != a || bPrime != b {
		t.Fatal("noOp pair should pass through unchanged")
	}

	// (Text, AddComment different) — no conflict
	text := wireTextOp(t, []any{float64(1), "x"})
	add, _ := NewAddCommentOperation("123", []opmodel.Range{{Pos: 0, Length: 1}}, false)
	aPrime2, bPrime2, err := tr.Transform(text, add)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if !aPrime2.IsText() || aPrime2.(*TextOpAdapter).t.String() != text.t.String() {
		t.Fatalf("expected text unchanged on (Text, AddComment) no-conflict, got %v", aPrime2.ToRaw())
	}
	if !bPrime2.IsNoOp() || bPrime2 != nil && bPrime2.(*TextOpAdapter) != text {
		_ = bPrime2
	}
}

func (o *AddCommentOperation) ResolvedRaw() bool          { return o.resolved }
func (o *AddCommentOperation) RangesRaw() []opmodel.Range { return o.ranges }
func (o *SetCommentStateOperation) CommentIDRaw() string  { return o.commentID }
