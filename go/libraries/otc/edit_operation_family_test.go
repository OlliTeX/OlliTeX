package otc

import (
	"strings"
	"testing"
)

// tText builds a TextOperation EditOperation from a Phase A builder closure.
func tText(t *testing.T, b func(*TextOperation)) EditOperation {
	t.Helper()
	o := NewTextOperation()
	b(o)
	return NewTextEdit(o)
}

func mustAdd(t *testing.T, id string, ranges []Range, resolved bool) *AddCommentOp {
	t.Helper()
	op, err := NewAddCommentOp(id, ranges, resolved)
	if err != nil {
		t.Fatalf("NewAddCommentOp(%q): %v", id, err)
	}
	return op
}

// --- EditOperationBuilder --------------------------------------------------

func TestEditOpBuilderFromJSON(t *testing.T) {
	// text
	raw := map[string]any{"textOperation": []any{1, "foo", 3}}
	op, err := FromJSONEditOperation(raw)
	if err != nil {
		t.Fatalf("fromJSON(text): %v", err)
	}
	if _, ok := asTextOp(op); !ok {
		t.Fatal("fromJSON(text) should be a TextOperation")
	}
	sameRaw(t, "text roundtrip", op.ToJSON(), raw)

	// add comment
	rawAdd := map[string]any{
		"commentId": "comm1",
		"ranges":    []any{map[string]any{"pos": 0, "length": 1}},
	}
	opAdd, err := FromJSONEditOperation(rawAdd)
	if err != nil {
		t.Fatalf("fromJSON(add): %v", err)
	}
	if _, ok := opAdd.(*AddCommentOp); !ok {
		t.Fatalf("fromJSON(add) = %T", opAdd)
	}
	sameRaw(t, "add roundtrip", opAdd.ToJSON(), rawAdd)

	// delete comment
	rawDel := map[string]any{"deleteComment": "comm1"}
	opDel, err := FromJSONEditOperation(rawDel)
	if err != nil {
		t.Fatalf("fromJSON(delete): %v", err)
	}
	if _, ok := opDel.(*DeleteCommentOp); !ok {
		t.Fatalf("fromJSON(delete) = %T", opDel)
	}
	sameRaw(t, "delete roundtrip", opDel.ToJSON(), rawDel)

	// set comment state
	rawSet := map[string]any{"commentId": "comm1", "resolved": true}
	opSet, err := FromJSONEditOperation(rawSet)
	if err != nil {
		t.Fatalf("fromJSON(set): %v", err)
	}
	if _, ok := opSet.(*SetCommentStateOp); !ok {
		t.Fatalf("fromJSON(set) = %T", opSet)
	}
	sameRaw(t, "set roundtrip", opSet.ToJSON(), rawSet)

	// no-op
	rawNo := map[string]any{"noOp": true}
	opNo, err := FromJSONEditOperation(rawNo)
	if err != nil {
		t.Fatalf("fromJSON(noop): %v", err)
	}
	if _, ok := opNo.(*EditNoOp); !ok {
		t.Fatalf("fromJSON(noop) = %T", opNo)
	}
	sameRaw(t, "noop roundtrip", opNo.ToJSON(), rawNo)

	// unsupported
	if _, err := FromJSONEditOperation(map[string]any{"unsupportedOperation": map[string]any{}}); err == nil {
		t.Fatal("fromJSON(unsupported) should error")
	}

	// isValid
	if !IsValidEditOperationRaw(raw) || !IsValidEditOperationRaw(rawAdd) ||
		!IsValidEditOperationRaw(rawDel) || !IsValidEditOperationRaw(rawSet) ||
		!IsValidEditOperationRaw(rawNo) {
		t.Fatal("isValid should be true for all known shapes")
	}
	if IsValidEditOperationRaw(map[string]any{"unsupportedOperation": map[string]any{}}) {
		t.Fatal("isValid(unsupported) should be false")
	}
}

// --- AddCommentOperation ---------------------------------------------------

func TestAddCommentOp(t *testing.T) {
	// empty range -> error
	if _, err := NewAddCommentOp("123", []Range{Range{Pos: 0, Length: 0}}, false); err == nil {
		t.Fatal("empty range should error")
	}
	// empty array ok
	op0 := mustAdd(t, "123", nil, true)
	if len(op0.Ranges) != 0 || !op0.Resolved {
		t.Fatalf("empty add op = %+v", op0)
	}

	// toJSON omits resolved when false
	op := mustAdd(t, "123", []Range{NewRange(0, 1)}, false)
	sameRaw(t, "add toJSON", op.ToJSON(),
		map[string]any{"commentId": "123", "ranges": []map[string]any{{"pos": 0, "length": 1}}})

	// toJSON includes resolved when true
	opR := mustAdd(t, "123", []Range{NewRange(0, 1)}, true)
	sameRaw(t, "add toJSON resolved", opR.ToJSON(),
		map[string]any{"commentId": "123", "ranges": []map[string]any{{"pos": 0, "length": 1}}, "resolved": true})

	// apply
	file, _ := NewStringFileData("abc", nil, nil)
	if err := op.Apply(file); err != nil {
		t.Fatalf("apply: %v", err)
	}
	sameRaw(t, "add apply comments", file.Comments.ToRaw(),
		[]map[string]any{{"id": "123", "ranges": []map[string]any{{"pos": 0, "length": 1}}}})

	// invert: comment absent -> DeleteCommentOp
	initial, _ := NewStringFileData("abc", nil, nil)
	inv, err := op.Invert(initial)
	if err != nil {
		t.Fatalf("invert: %v", err)
	}
	if d, ok := inv.(*DeleteCommentOp); !ok || d.CommentID != "123" {
		t.Fatalf("invert(absent) = %T %+v", inv, inv)
	}

	// invert: comment present -> AddCommentOp restoring prior ranges/resolved
	rawComments := []map[string]any{{"id": "123", "ranges": []any{map[string]any{"pos": 0, "length": 1}}}}
	initial2, _ := NewStringFileData("the quick brown fox jumps over the lazy dog", rawComments, nil)
	op2 := mustAdd(t, "123", []Range{NewRange(12, 7)}, true)
	inv2, err := op2.Invert(initial2)
	if err != nil {
		t.Fatalf("invert2: %v", err)
	}
	aa, ok := inv2.(*AddCommentOp)
	if !ok || aa.CommentID != "123" || len(aa.Ranges) != 1 ||
		aa.Ranges[0].Pos != 0 || aa.Ranges[0].Length != 1 || aa.Resolved {
		t.Fatalf("invert(present) = %+v", inv2)
	}

	// canBeComposedWith
	del := &DeleteCommentOp{CommentID: "123"}
	set := &SetCommentStateOp{CommentID: "123", Resolved: true}
	delOther := &DeleteCommentOp{CommentID: "other"}
	if !op.CanBeComposedWith(del) || !op.CanBeComposedWith(mustAdd(t, "123", nil, false)) ||
		!op.CanBeComposedWith(set) {
		t.Fatal("canBeComposedWith should be true for same-id comment ops")
	}
	if op.CanBeComposedWith(delOther) || op.CanBeComposedWith(tText(t, func(o *TextOperation) {
		mustOps(t, o.Insert("x", InsertBuilderOpts{}))
	})) {
		t.Fatal("canBeComposedWith should be false for different-id / text ops")
	}

	// compose
	if got, err := op.Compose(del); err != nil {
		t.Fatalf("compose(delete): %v", err)
	} else if _, ok := got.(*DeleteCommentOp); !ok {
		t.Fatalf("compose(delete) = %T", got)
	}
	if got, _ := op.Compose(set); !isAddResolved(got, "123", true) {
		t.Fatalf("compose(set) = %+v", got)
	}
	if _, err := op.Compose(delOther); err == nil {
		t.Fatal("compose(delete other-id) should error")
	}
}

func isAddResolved(got EditOperation, id string, resolved bool) bool {
	a, o := got.(*AddCommentOp)
	return o && a.CommentID == id && a.Resolved == resolved
}

// --- DeleteCommentOperation ------------------------------------------------

func TestDeleteCommentOp(t *testing.T) {
	op := &DeleteCommentOp{CommentID: "123"}
	sameRaw(t, "delete toJSON", op.ToJSON(), map[string]any{"deleteComment": "123"})

	// apply removes the comment
	rawComments := []map[string]any{{"id": "123", "ranges": []any{map[string]any{"pos": 0, "length": 1}}}}
	file, _ := NewStringFileData("abc", rawComments, nil)
	if err := op.Apply(file); err != nil {
		t.Fatalf("apply: %v", err)
	}
	sameRaw(t, "delete apply", file.Comments.ToRaw(), []map[string]any{})

	// invert present -> AddCommentOp
	inv, err := op.Invert(fileWithComment(t, "123", NewRange(0, 1), false))
	if err != nil {
		t.Fatalf("invert: %v", err)
	}
	if a, o := inv.(*AddCommentOp); !o || a.CommentID != "123" || len(a.Ranges) != 1 ||
		a.Ranges[0].Pos != 0 || a.Ranges[0].Length != 1 {
		t.Fatalf("invert(present) = %+v", inv)
	}
	// invert absent -> EditNoOp
	inv0, err := op.Invert(mustFile(t, "abc"))
	if err != nil {
		t.Fatalf("invert0: %v", err)
	}
	if _, o := inv0.(*EditNoOp); !o {
		t.Fatalf("invert(absent) = %T", inv0)
	}
	// canBeComposedWith false
	if op.CanBeComposedWith(mustAdd(t, "123", nil, false)) {
		t.Fatal("delete.canBeComposedWith should be false")
	}
}

// --- SetCommentStateOperation ----------------------------------------------

func TestSetCommentStateOp(t *testing.T) {
	op := &SetCommentStateOp{CommentID: "123", Resolved: true}
	sameRaw(t, "set toJSON", op.ToJSON(), map[string]any{"commentId": "123", "resolved": true})

	// apply resolves the existing comment
	file := fileWithComment(t, "123", NewRange(0, 1), false)
	if err := op.Apply(file); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if c := file.Comments.GetComment("123"); c == nil || !c.Resolved {
		t.Fatalf("apply should resolve comment; got %+v", c)
	}
	// apply with no comment: no error, no comment added
	empty := mustFile(t, "abc")
	if err := op.Apply(empty); err != nil {
		t.Fatalf("apply(absent): %v", err)
	}
	if empty.Comments.Len() != 0 {
		t.Fatal("apply(absent) should not add a comment")
	}

	// invert absent -> EditNoOp
	inv0, err := op.Invert(mustFile(t, "abc"))
	if err != nil {
		t.Fatalf("invert0: %v", err)
	}
	if _, o := inv0.(*EditNoOp); !o {
		t.Fatalf("invert(absent) = %T", inv0)
	}
	// invert present -> SetCommentStateOp of the old resolved
	fPrev := fileWithComment(t, "123", NewRange(0, 1), false)
	inv, err := (&SetCommentStateOp{CommentID: "123", Resolved: true}).Invert(fPrev)
	if err != nil {
		t.Fatalf("invert: %v", err)
	}
	if s, o := inv.(*SetCommentStateOp); !o || s.CommentID != "123" || s.Resolved {
		t.Fatalf("invert(present) = %+v", inv)
	}

	// canBeComposedWith: set same id, delete same id
	if !op.CanBeComposedWith(&SetCommentStateOp{CommentID: "123"}) ||
		!op.CanBeComposedWith(&DeleteCommentOp{CommentID: "123"}) {
		t.Fatal("set.canBeComposedWith should be true for same-id set/delete")
	}
	if op.CanBeComposedWith(&SetCommentStateOp{CommentID: "other"}) {
		t.Fatal("set.canBeComposedWith(other id) should be false")
	}

	// compose: same-id set -> other; same-id delete -> delete; add -> error
	gotSet, _ := op.Compose(&SetCommentStateOp{CommentID: "123", Resolved: false})
	if _, o := gotSet.(*SetCommentStateOp); !o {
		t.Fatalf("compose(set) = %T", gotSet)
	}
	del := &DeleteCommentOp{CommentID: "123"}
	if got, _ := op.Compose(del); got != EditOperation(del) {
		t.Fatalf("compose(delete) = %T", got)
	}
	if _, err := op.Compose(mustAdd(t, "123", nil, false)); err == nil {
		t.Fatal("compose(add) should error")
	}
}

// --- Transform matrix ------------------------------------------------------

func TestTransformMatrix(t *testing.T) {
	textOp := tText(t, func(o *TextOperation) {
		mustOps(t, o.Retain(9, RetainBuilderOpts{}))
		mustOps(t, o.Insert(" world", InsertBuilderOpts{}))
	})
	del := &DeleteCommentOp{CommentID: "comm1"}
	set := &SetCommentStateOp{CommentID: "comm1", Resolved: true}
	add := mustAdd(t, "comm1", []Range{NewRange(0, 1)}, false)

	// text + delete no conflict
	a, b, err := TransformEditOps(textOp, del)
	if err != nil {
		t.Fatalf("transform(text,delete): %v", err)
	}
	sameRaw(t, "text+delete a", a.ToJSON(), textOp.ToJSON())
	sameRaw(t, "text+delete b", b.ToJSON(), del.ToJSON())

	// text + set no conflict
	a, b, err = TransformEditOps(textOp, set)
	if err != nil {
		t.Fatalf("transform(text,set): %v", err)
	}
	sameRaw(t, "text+set a", a.ToJSON(), textOp.ToJSON())
	sameRaw(t, "text+set b", b.ToJSON(), set.ToJSON())

	// set + add -> add takes set.resolved
	a, b, err = TransformEditOps(add, set)
	if err != nil {
		t.Fatalf("transform(add,set): %v", err)
	}
	sameRaw(t, "add+set a", a.ToJSON(),
		map[string]any{"commentId": "comm1", "ranges": []map[string]any{{"pos": 0, "length": 1}}, "resolved": true})
	sameRaw(t, "add+set b", b.ToJSON(), set.ToJSON())

	// delete + set -> [delete, noop]
	a, b, err = TransformEditOps(del, set)
	if err != nil {
		t.Fatalf("transform(delete,set): %v", err)
	}
	if _, o := a.(*DeleteCommentOp); !o {
		t.Fatalf("delete+set a = %T", a)
	}
	if _, o := b.(*EditNoOp); !o {
		t.Fatalf("delete+set b = %T", b)
	}

	// set + set (same id, different resolved) -> [a, noop]
	aSet := &SetCommentStateOp{CommentID: "comm1", Resolved: false}
	a, b, err = TransformEditOps(aSet, set)
	if err != nil {
		t.Fatalf("transform(set,set): %v", err)
	}
	sameRaw(t, "set+set a", a.ToJSON(), map[string]any{"commentId": "comm1", "resolved": false})
	if _, o := b.(*EditNoOp); !o {
		t.Fatalf("set+set b = %T", b)
	}

	// set + set different id -> [a, b]
	bSet := &SetCommentStateOp{CommentID: "comm2", Resolved: true}
	a, b, err = TransformEditOps(aSet, bSet)
	if err != nil {
		t.Fatalf("transform(set,set diff): %v", err)
	}
	sameRaw(t, "set+set diff a", a.ToJSON(), aSet.ToJSON())
	sameRaw(t, "set+set diff b", b.ToJSON(), bSet.ToJSON())

	// add + add same id -> [noop, b]
	a, b, err = TransformEditOps(add, mustAdd(t, "comm1", []Range{NewRange(2, 3)}, true))
	if err != nil {
		t.Fatalf("transform(add,add): %v", err)
	}
	if _, o := a.(*EditNoOp); !o {
		t.Fatalf("add+add a = %T", a)
	}
	// add + delete same id -> [noop, delete]
	if a, b, err = TransformEditOps(add, del); err != nil {
		t.Fatalf("transform(add,delete): %v", err)
	}
	if _, o := a.(*EditNoOp); !o {
		t.Fatalf("add+delete a = %T", a)
	}
	if _, o := b.(*DeleteCommentOp); !o {
		t.Fatalf("add+delete b = %T", b)
	}

	// noop + other -> [a, b]
	no := &EditNoOp{}
	a, b, err = TransformEditOps(no, textOp)
	if err != nil {
		t.Fatalf("transform(noop,text): %v", err)
	}
	if a != EditOperation(no) {
		t.Fatal("noop+text a should be noop")
	}
	sameRaw(t, "noop+text b", b.ToJSON(), textOp.ToJSON())

	// unimplemented (unknown op types) -> message with \uFFAE separator
	if _, _, err := TransformEditOps(fakeEditOp{}, fakeEditOp{}); err == nil {
		t.Fatal("transform(fake,fake) should error")
	} else if !strings.Contains(err.Error(), "\uFFAE") {
		t.Fatalf("transform error should contain separator: %q", err.Error())
	}
}

func TestTransformMultipleConverges(t *testing.T) {
	base := "AB"
	as := []EditOperation{
		mkOp(t, func(o *TextOperation) {
			mustOps(t, o.Insert("x", InsertBuilderOpts{}))
			mustOps(t, o.Retain(2, RetainBuilderOpts{}))
		}),
		mkOp(t, func(o *TextOperation) {
			mustOps(t, o.Insert("y", InsertBuilderOpts{}))
			mustOps(t, o.Retain(3, RetainBuilderOpts{}))
		}),
	}
	bs := []EditOperation{
		mkOp(t, func(o *TextOperation) {
			mustOps(t, o.Retain(2, RetainBuilderOpts{}))
			mustOps(t, o.Insert("1", InsertBuilderOpts{}))
		}),
		mkOp(t, func(o *TextOperation) {
			mustOps(t, o.Retain(3, RetainBuilderOpts{}))
			mustOps(t, o.Insert("2", InsertBuilderOpts{}))
		}),
	}
	asOrig := make([]EditOperation, len(as))
	bsOrig := make([]EditOperation, len(bs))
	for i, op := range as {
		raw := op.ToJSON()
		asOrig[i], _ = FromJSONEditOperation(raw)
	}
	for i, op := range bs {
		raw := op.ToJSON()
		bsOrig[i], _ = FromJSONEditOperation(raw)
	}

	TransformEditOpsMultiple(as, bs)

	applySeq := func(ops []EditOperation) string {
		file, _ := NewStringFileData(base, nil, nil)
		for _, op := range ops {
			if err := file.Edit(op); err != nil {
				t.Fatalf("edit: %v", err)
			}
		}
		return *file.GetContent(false)
	}
	if got := applySeq(append(asOrig, bs...)); got != "yxAB12" {
		t.Fatalf("asOrig+bs = %q, want yxAB12", got)
	}
	if got := applySeq(append(bsOrig, as...)); got != "yxAB12" {
		t.Fatalf("bsOrig+as = %q, want yxAB12", got)
	}
}

// helpers + fakes

func TestTransformTextAdd(t *testing.T) {
	textIns := tText(t, func(o *TextOperation) {
		mustOps(t, o.Retain(9, RetainBuilderOpts{}))
		mustOps(t, o.Insert(" world", InsertBuilderOpts{}))
	})
	add := mustAdd(t, "comm1", []Range{NewRange(10, 3)}, false)

	// (text, add): comment ranges shift by the inserted length (pos 10 -> 16 after 6-insert)
	a, b, err := TransformEditOps(textIns, add)
	if err != nil {
		t.Fatalf("transform(text,add): %v", err)
	}
	sameRaw(t, "t+a a", a.ToJSON(), textIns.ToJSON())
	sameRaw(t, "t+a b", b.ToJSON(),
		map[string]any{"commentId": "comm1", "ranges": []map[string]any{{"pos": 16, "length": 3}}})

	// (add, text): symmetric branch
	a, b, err = TransformEditOps(add, textIns)
	if err != nil {
		t.Fatalf("transform(add,text): %v", err)
	}
	sameRaw(t, "a+t a", a.ToJSON(),
		map[string]any{"commentId": "comm1", "ranges": []map[string]any{{"pos": 16, "length": 3}}})
	sameRaw(t, "a+t b", b.ToJSON(), textIns.ToJSON())

	// partial delete over the comment -> shrunk range
	textDel := tText(t, func(o *TextOperation) {
		mustOps(t, o.Retain(8, RetainBuilderOpts{}))
		mustOps(t, o.Remove(4))
	})
	add3 := mustAdd(t, "comm1", []Range{NewRange(10, 3)}, false)
	a, b, err = TransformEditOps(textDel, add3)
	if err != nil {
		t.Fatalf("transform(textDel,add): %v", err)
	}
	sameRaw(t, "tDel+a a", a.ToJSON(), textDel.ToJSON())
	sameRaw(t, "tDel+a b", b.ToJSON(),
		map[string]any{"commentId": "comm1", "ranges": []map[string]any{{"pos": 8, "length": 1}}})

	// detached: delete covers the comment entirely -> empty ranges
	textDelAll := tText(t, func(o *TextOperation) {
		mustOps(t, o.Remove(13))
	})
	add4 := mustAdd(t, "comm1", []Range{NewRange(5, 5)}, false)
	if a, b, err = TransformEditOps(textDelAll, add4); err != nil {
		t.Fatalf("transform(textDelAll,add): %v", err)
	} else {
		sameRaw(t, "delAll+a a", a.ToJSON(), textDelAll.ToJSON())
		sameRaw(t, "delAll+a b", b.ToJSON(),
			map[string]any{"commentId": "comm1", "ranges": []map[string]any{}})
	}
}

func mkOp(t *testing.T, b func(*TextOperation)) EditOperation {
	t.Helper()
	o := NewTextOperation()
	b(o)
	return NewTextEdit(o)
}

func mustFile(t *testing.T, content string) *StringFileData {
	t.Helper()
	f, err := NewStringFileData(content, nil, nil)
	if err != nil {
		t.Fatalf("NewStringFileData: %v", err)
	}
	return f
}

func fileWithComment(t *testing.T, id string, r Range, resolved bool) *StringFileData {
	t.Helper()
	raw := []map[string]any{{"id": id, "ranges": []any{map[string]any{"pos": r.Pos, "length": r.Length}}, "resolved": resolved}}
	f, err := NewStringFileData("abcdef", raw, nil)
	if err != nil {
		t.Fatalf("fileWithComment: %v", err)
	}
	return f
}

// fakeEditOp is an unrelated EditOperation used to exercise the transform
// fallback ("Transform not implemented") branch.
type fakeEditOp struct{}

func (fakeEditOp) ToJSON() map[string]any           { return map[string]any{} }
func (fakeEditOp) Apply(*StringFileData) error      { return nil }
func (fakeEditOp) ApplyToLength(n int) (int, error) { return n, nil }
func (fakeEditOp) Invert(*StringFileData) (EditOperation, error) {
	return nil, nil
}
func (fakeEditOp) CanBeComposedWith(EditOperation) bool { return false }
func (fakeEditOp) CanBeComposedWithForUndo(EditOperation) bool {
	return false
}
func (fakeEditOp) Compose(EditOperation) (EditOperation, error) { return nil, nil }
