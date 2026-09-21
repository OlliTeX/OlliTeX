package otc

import (
	"strings"
	"testing"
	"time"
)

// newFileData builds a StringFileData from content + optional raw collections.
func newFileData(t *testing.T, content string, comments []map[string]any, tracked []map[string]any) *StringFileData {
	t.Helper()
	f, err := NewStringFileData(content, comments, tracked)
	if err != nil {
		t.Fatalf("newFileData: %v", err)
	}
	return f
}

func ts(t *testing.T, s string) time.Time {
	t.Helper()
	x, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatalf("bad ts %q: %v", s, err)
	}
	return x
}

func TestTextOperationLengths(t *testing.T) {
	o := NewTextOperation()
	if o.BaseLength != 0 || o.TargetLength != 0 {
		t.Fatalf("initial lengths = %d/%d, want 0/0", o.BaseLength, o.TargetLength)
	}
	mustOps(t, o.Retain(5, RetainBuilderOpts{}))
	if o.BaseLength != 5 || o.TargetLength != 5 {
		t.Fatalf("after retain(5) = %d/%d, want 5/5", o.BaseLength, o.TargetLength)
	}
	mustOps(t, o.Insert("abc", InsertBuilderOpts{}))
	if o.BaseLength != 5 || o.TargetLength != 8 {
		t.Fatalf("after insert(abc) = %d/%d, want 5/8", o.BaseLength, o.TargetLength)
	}
	mustOps(t, o.Retain(2, RetainBuilderOpts{}))
	if o.BaseLength != 7 || o.TargetLength != 10 {
		t.Fatalf("after retain(2) = %d/%d, want 7/10", o.BaseLength, o.TargetLength)
	}
	mustOps(t, o.Remove(2))
	if o.BaseLength != 9 || o.TargetLength != 10 {
		t.Fatalf("after remove(2) = %d/%d, want 9/10", o.BaseLength, o.TargetLength)
	}
}

func mustOps(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("operation builder failed: %v", err)
	}
}

func TestTextOperationChaining(t *testing.T) {
	o := NewTextOperation()
	mustOps(t, o.Retain(5, RetainBuilderOpts{}))
	mustOps(t, o.Retain(0, RetainBuilderOpts{}))
	mustOps(t, o.Insert("lorem", InsertBuilderOpts{}))
	mustOps(t, o.Insert("", InsertBuilderOpts{}))
	mustOps(t, o.RemoveStr("abc"))
	mustOps(t, o.Remove(3))
	mustOps(t, o.Remove(0))
	mustOps(t, o.RemoveStr(""))
	if len(o.Ops) != 3 {
		t.Fatalf("ops length = %d, want 3 (%v)", len(o.Ops), o.Ops)
	}
}

func TestTextOperationIgnoresEmpty(t *testing.T) {
	o := NewTextOperation()
	mustOps(t, o.Retain(0, RetainBuilderOpts{}))
	mustOps(t, o.Insert("", InsertBuilderOpts{}))
	mustOps(t, o.RemoveStr(""))
	if len(o.Ops) != 0 {
		t.Fatalf("ops length = %d, want 0", len(o.Ops))
	}
}

func TestTextOperationEquality(t *testing.T) {
	op1 := NewTextOperation()
	mustOps(t, op1.Remove(1))
	mustOps(t, op1.Insert("lo", InsertBuilderOpts{}))
	mustOps(t, op1.Retain(2, RetainBuilderOpts{}))
	mustOps(t, op1.Retain(3, RetainBuilderOpts{}))

	op2 := NewTextOperation()
	mustOps(t, op2.Remove(-1))
	mustOps(t, op2.Insert("l", InsertBuilderOpts{}))
	mustOps(t, op2.Insert("o", InsertBuilderOpts{}))
	mustOps(t, op2.Retain(5, RetainBuilderOpts{}))

	if !op1.Equals(op2) {
		t.Fatal("op1 should equal op2")
	}
	mustOps(t, op1.Remove(1))
	mustOps(t, op2.Retain(1, RetainBuilderOpts{}))
	if op1.Equals(op2) {
		t.Fatal("op1 should not equal op2 after mutation")
	}
}

func TestTextOperationMerges(t *testing.T) {
	o := NewTextOperation()
	last := func() ScanOp { return o.Ops[len(o.Ops)-1] }
	if len(o.Ops) != 0 {
		t.Fatalf("initial ops = %d, want 0", len(o.Ops))
	}
	mustOps(t, o.Retain(2, RetainBuilderOpts{}))
	if len(o.Ops) != 1 || !last().Equals(RetainOp{Length: 2}) {
		t.Fatalf("after retain(2): %v", o.Ops)
	}
	mustOps(t, o.Retain(3, RetainBuilderOpts{}))
	if len(o.Ops) != 1 || !last().Equals(RetainOp{Length: 5}) {
		t.Fatalf("after retain(3): %v", o.Ops)
	}
	mustOps(t, o.Insert("abc", InsertBuilderOpts{}))
	if len(o.Ops) != 2 {
		t.Fatalf("after insert(abc): %v (len %d)", o.Ops, len(o.Ops))
	}
	insert1, ok := last().(InsertOp)
	if !ok || insert1.Insertion != "abc" {
		t.Fatalf("last op after insert(abc): %v", last())
	}
	mustOps(t, o.Insert("xyz", InsertBuilderOpts{}))
	if len(o.Ops) != 2 {
		t.Fatalf("after insert(xyz): len %d, want 2", len(o.Ops))
	}
	insert2, ok := last().(InsertOp)
	if !ok || insert2.Insertion != "abcxyz" {
		t.Fatalf("last op after insert(xyz): %v", last())
	}
	mustOps(t, o.RemoveStr("d"))
	if len(o.Ops) != 3 {
		t.Fatalf("after remove(d): len %d, want 3", len(o.Ops))
	}
	rm1, ok := last().(RemoveOp)
	if !ok || rm1.Length != 1 {
		t.Fatalf("last op after remove(d): %v", last())
	}
	mustOps(t, o.RemoveStr("d"))
	if len(o.Ops) != 3 {
		t.Fatalf("after remove(d): len %d, want 3", len(o.Ops))
	}
	rm2, ok := last().(RemoveOp)
	if !ok || rm2.Length != 2 {
		t.Fatalf("last op after remove(d): %v", last())
	}
}

func TestTextOperationIsNoop(t *testing.T) {
	o := NewTextOperation()
	if !o.IsNoop() {
		t.Fatal("empty op should be noop")
	}
	mustOps(t, o.Retain(5, RetainBuilderOpts{}))
	if !o.IsNoop() {
		t.Fatal("retain(5) should be noop")
	}
	mustOps(t, o.Retain(3, RetainBuilderOpts{}))
	if !o.IsNoop() {
		t.Fatal("retain(5,3) should be noop")
	}
	mustOps(t, o.Insert("lorem", InsertBuilderOpts{}))
	if o.IsNoop() {
		t.Fatal("retain+insert should not be noop")
	}
}

func TestTrackedRetainIsNotNoop(t *testing.T) {
	trackedDelete := NewTextOperation()
	mustOps(t, trackedDelete.Retain(5, RetainBuilderOpts{Tracking: NewTrackingProps("delete", "user-1", ts(t, "2026-07-10T00:00:00.000Z"))}))

	file := newFileData(t, "lorem", nil, nil)
	if err := trackedDelete.Apply(file); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := file.TrackedChanges.Len(); got != 1 {
		t.Fatalf("trackedChanges length = %d, want 1", got)
	}
	if trackedDelete.IsNoop() {
		t.Fatal("tracked retain should not be a noop")
	}
	unrelated := NewTextOperation()
	mustOps(t, unrelated.Retain(5, RetainBuilderOpts{}))
	mustOps(t, unrelated.Insert("x", InsertBuilderOpts{}))
	if trackedDelete.CanBeComposedWithForUndo(unrelated) {
		t.Fatal("trackedDelete should not be undo-composable with unrelatedInsert")
	}
	if unrelated.CanBeComposedWithForUndo(trackedDelete) {
		t.Fatal("unrelatedInsert should not be undo-composable with trackedDelete")
	}
}

func TestTextOperationToString(t *testing.T) {
	o := NewTextOperation()
	mustOps(t, o.Retain(2, RetainBuilderOpts{}))
	mustOps(t, o.Insert("lorem", InsertBuilderOpts{}))
	mustOps(t, o.RemoveStr("ipsum"))
	mustOps(t, o.Retain(5, RetainBuilderOpts{}))
	got := o.String()
	want := "retain 2, insert 'lorem', remove 5, retain 5"
	if got != want {
		t.Fatalf("toString = %q, want %q", got, want)
	}
}

func TestTextOperationFromJSON(t *testing.T) {
	ops := []any{2, -1, -1, "cde"}
	o, err := FromJSONTextOperation(map[string]any{"textOperation": ops})
	if err != nil {
		t.Fatalf("fromJSON: %v", err)
	}
	if len(o.Ops) != 3 {
		t.Fatalf("ops length = %d, want 3", len(o.Ops))
	}
	if o.BaseLength != 4 {
		t.Fatalf("baseLength = %d, want 4", o.BaseLength)
	}
	if o.TargetLength != 5 {
		t.Fatalf("targetLength = %d, want 5", o.TargetLength)
	}

	bad1 := []any{2, -1, -1, "cde", map[string]any{"insert": "x"}}
	if _, err := FromJSONTextOperation(map[string]any{"textOperation": bad1}); err == nil {
		t.Fatal("fromJSON with {insert} should fail")
	}
	bad2 := []any{2, -1, -1, "cde", nil}
	if _, err := FromJSONTextOperation(map[string]any{"textOperation": bad2}); err == nil {
		t.Fatal("fromJSON with null should fail")
	}
}

func TestTextOperationApplyErrors(t *testing.T) {
	o := NewTextOperation()
	mustOps(t, o.Retain(1, RetainBuilderOpts{}))
	file := newFileData(t, "", nil, nil)
	err := o.Apply(file)
	if err == nil {
		t.Fatal("apply with wrong base length should error")
	}
	assertErrType[*ApplyError](t, "apply", err)

	ok := newFileData(t, " ", nil, nil)
	if err := o.Apply(ok); err != nil {
		t.Fatal("apply with matching length should not error")
	}
}

func TestInvalidInsertion(t *testing.T) {
	o := NewTextOperation()
	err := o.Insert("𝌆\n", InsertBuilderOpts{})
	if err == nil {
		t.Fatal("insert non-BMP should error")
	}
	assertErrType[*InvalidInsertionError](t, "insert", err)
	if !strings.Contains(err.Error(), "inserted text contains non BMP characters") {
		t.Fatalf("wrong message: %v", err)
	}
	_, err2 := FromJSONTextOperation(map[string]any{"textOperation": []any{"𝌆\n"}})
	if err2 == nil {
		t.Fatal("fromJSON non-BMP should error")
	}
	if !strings.Contains(err2.Error(), "inserted text contains non BMP characters") {
		t.Fatalf("wrong message: %v", err2)
	}
}
