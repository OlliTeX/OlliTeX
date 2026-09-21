package otc

import (
	"strings"
	"testing"
)

func TestStringFileDataMethods(t *testing.T) {
	f := newFileData(t,
		"the quick brown fox",
		[]map[string]any{{"id": "c1", "ranges": []any{map[string]any{"pos": 4, "length": 5}}, "resolved": false}},
		[]map[string]any{{"range": map[string]any{"pos": 4, "length": 5},
			"tracking": map[string]any{"ts": "2023-01-01T00:00:00.000Z", "type": "delete", "userId": "u"}}})
	if fe := f.IsEditable(); fe == nil || !*fe {
		t.Fatal("should be editable")
	}
	if fb := f.GetByteLength(); fb == nil || *fb != int64(len("the quick brown fox")) {
		t.Fatalf("byte length = %v", fb)
	}
	if fs := f.GetStringLength(); fs == nil || *fs != 19 {
		t.Fatalf("string length = %v", fs)
	}
	if gp := f.GetContent(false); gp == nil || *gp != "the quick brown fox" {
		t.Fatalf("GetContent(false) = %v", gp)
	}
	// filterTrackedDeletes removes the tracked-deleted "quick" (pos 4 length 5)
	if gp := f.GetContent(true); gp == nil || *gp != "the  brown fox" {
		t.Fatalf("GetContent(true) = %v", gp)
	}
	lines := f.GetLines()
	if len(lines) != 1 || lines[0] != "the  brown fox" {
		t.Fatalf("GetLines = %v", lines)
	}
	stats := f.ToStats()
	if stats["nContent"] != 1 || stats["nComments"] != 1 || stats["nTrackedChanges"] != 1 {
		t.Fatalf("ToStats = %v", stats)
	}
	if stats["commentsSize"].(int) <= 0 || stats["trackedChangesSize"].(int) <= 0 {
		t.Fatalf("ToStats sizes = %v", stats)
	}
}

func TestCommentMethods(t *testing.T) {
	c := NewComment("c1", []Range{NewRange(0, 3), NewRange(3, 2)}, true)
	if c.Len() != 1 { // 0-3 and 3-5 merge into one range
		t.Fatalf("Len = %d, want 1 (%v)", c.Len(), c.Ranges)
	}
	if c.IsEmpty() {
		t.Fatal("c1 should not be empty")
	}
	raw := c.ToRaw()
	if raw["resolved"] != true {
		t.Fatalf("resolved should be %v", raw["resolved"])
	}
	empty := NewComment("c2", nil, false)
	if !empty.IsEmpty() {
		t.Fatal("should be empty")
	}

	// ApplyTextOperation
	op := NewTextOperation()
	mustOps(t, op.Retain(2, RetainBuilderOpts{}))
	mustOps(t, op.Insert("XY", InsertBuilderOpts{CommentIds: []string{"c1"}}))
	mustOps(t, op.Retain(3, RetainBuilderOpts{}))
	file := newFileData(t, "abc",
		[]map[string]any{{"id": "c1", "ranges": []any{map[string]any{"pos": 0, "length": 5}}}}, nil)
	applied := file.Comments.GetComment("c1").ApplyTextOperation(op, "c1")
	// comment [0,5] with an insert of 2 at position 2 → extend
	if got := applied.Ranges[0].Length; got != 7 {
		t.Fatalf("ApplyTextOperation range length = %d, want 7 (%v)", got, applied.Ranges)
	}
}

func TestCommentListMethods(t *testing.T) {
	c1 := NewComment("a", []Range{NewRange(0, 2)}, false)
	c2 := NewComment("b", []Range{NewRange(5, 2)}, true)
	list := NewCommentList([]*Comment{c1, c2})
	if list.Len() != 2 {
		t.Fatalf("Len = %d", list.Len())
	}
	if got := list.GetComment("b"); got == nil || got.Resolved != true {
		t.Fatalf("GetComment(b) = %v", got)
	}
	if !list.Delete("a") {
		t.Fatal("Delete(a) should be true")
	}
	if list.Delete("a") {
		t.Fatal("Delete(a) again should be false")
	}
	if list.Len() != 1 {
		t.Fatalf("Len after delete = %d", list.Len())
	}
	ids := list.IDs()
	if len(ids) != 1 || ids[0] != "b" {
		t.Fatalf("IDs = %v", ids)
	}

	// FromRawCommentListAny
	anyRaw := []any{
		map[string]any{"id": "x", "ranges": []any{map[string]any{"pos": 1, "length": 2}}},
		map[string]any{"id": "y", "ranges": []any{}},
	}
	l2, err := FromRawCommentListAny(anyRaw)
	if err != nil {
		t.Fatalf("FromRawCommentListAny: %v", err)
	}
	if l2.Len() != 2 {
		t.Fatalf("l2.Len = %d", l2.Len())
	}

	// IDsCoveringRange
	list2 := NewCommentList([]*Comment{NewComment("cov", []Range{NewRange(0, 10)}, false)})
	cover := list2.IDsCoveringRange(NewRange(3, 2))
	if len(cover) != 1 || cover[0] != "cov" {
		t.Fatalf("IDsCoveringRange = %v", cover)
	}
	none := list2.IDsCoveringRange(NewRange(20, 2))
	if len(none) != 0 {
		t.Fatalf("IDsCoveringRange(out) = %v", none)
	}
}

func TestTrackedChangeListMethods(t *testing.T) {
	tp := NewTrackingProps("insert", "u", ts(t, "2023-01-01T00:00:00.000Z"))
	t1 := NewTrackedChange(NewRange(0, 3), tp)
	t2 := NewTrackedChange(NewRange(5, 2), tp)
	list := NewTrackedChangeList([]TrackedChange{t1, t2})
	if list.Len() != 2 {
		t.Fatalf("Len = %d", list.Len())
	}
	if len(list.Changes()) != 2 {
		t.Fatalf("Changes = %d", len(list.Changes()))
	}
	if got := list.InRange(NewRange(0, 10)); len(got) != 2 {
		t.Fatalf("InRange = %d", len(got))
	}
	// PropsAtRange
	props := list.PropsAtRange(NewRange(1, 1))
	if props.Type != "insert" || props.UserID != "u" {
		t.Fatalf("PropsAtRange = %v", props)
	}
	if list.PropsAtRange(NewRange(3, 1)).Type != "" {
		t.Fatal("PropsAtRange(gap) should be empty")
	}
	// IntersectRange
	inter := list.IntersectRange(NewRange(1, 2))
	if len(inter) != 1 {
		t.Fatalf("IntersectRange = %d", len(inter))
	}
	// RemoveInRange
	list.RemoveInRange(NewRange(0, 3))
	if list.Len() != 1 {
		t.Fatalf("after RemoveInRange Len = %d", list.Len())
	}
	// Add
	t3 := NewTrackedChange(NewRange(10, 2), tp)
	if err := list.Add(t3); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if list.Len() != 2 {
		t.Fatalf("after Add Len = %d", list.Len())
	}

	// FromRawTrackedChangeListAny
	anyRaw := []any{
		map[string]any{"range": map[string]any{"pos": 0, "length": 1},
			"tracking": map[string]any{"type": "insert", "userId": "u", "ts": "2023-01-01T00:00:00.000Z"}},
	}
	l2, err := FromRawTrackedChangeListAny(anyRaw)
	if err != nil {
		t.Fatalf("FromRawTrackedChangeListAny: %v", err)
	}
	if l2.Len() != 1 {
		t.Fatalf("l2.Len = %d", l2.Len())
	}

	// ApplyInsert / ApplyDelete / ApplyRetain (public wrappers)
	l3 := NewTrackedChangeList(nil)
	if err := l3.ApplyInsert(0, "abc", InsertOpts{tracking: tp}); err != nil {
		t.Fatalf("ApplyInsert: %v", err)
	}
	if l3.Len() != 1 {
		t.Fatalf("after ApplyInsert Len = %d", l3.Len())
	}
	if err := l3.ApplyDelete(0, 1); err != nil {
		t.Fatalf("ApplyDelete: %v", err)
	}
	l4 := NewTrackedChangeList(nil)
	if err := l4.ApplyRetain(0, 3, RetainOpts{tracking: tp}); err != nil {
		t.Fatalf("ApplyRetain: %v", err)
	}
	if err := l4.ApplyRetain(0, 3, RetainOpts{tracking: ClearTrackingProps{}}); err != nil {
		t.Fatalf("ApplyRetain(clear): %v", err)
	}
}

func TestTrackingHelpers(t *testing.T) {
	clear := ClearTrackingProps{}
	if _, ok := AsClearTrackingProps(clear); !ok {
		t.Fatal("AsClearTrackingProps(clear) should be ok")
	}
	if _, ok := AsClearTrackingProps(NewTrackingProps("insert", "u", ts(t, "2023-01-01T00:00:00.000Z"))); ok {
		t.Fatal("AsClearTrackingProps(props) should be not ok")
	}
	if !IsClearTrackingProps(clear) {
		t.Fatal("IsClearTrackingProps(clear) should be true")
	}
	if IsClearTrackingProps(nil) || IsClearTrackingProps(NewTrackingProps("delete", "u", ts(t, "2023-01-01T00:00:00.000Z"))) {
		t.Fatal("IsClearTrackingProps(nil/props) should be false")
	}

	tp1 := NewTrackingProps("insert", "u", ts(t, "2023-01-01T00:00:00.000Z"))
	tp2 := NewTrackingProps("insert", "u", ts(t, "2024-01-01T00:00:00.000Z"))
	tp3 := NewTrackingProps("delete", "u", ts(t, "2023-01-01T00:00:00.000Z"))
	// equals compares type+userId+ts
	if !tp1.Equals(tp1) {
		t.Fatal("tp1.Equals(tp1) should be true")
	}
	if tp1.Equals(tp2) {
		t.Fatal("tp1.Equals(tp2) should be false (different ts)")
	}
	if tp1.CanMergeWith(tp3) {
		t.Fatal("tp1.CanMergeWith(tp3) should be false (different type)")
	}
	if tp1.CanMergeWith(clear) {
		t.Fatal("tp1.CanMergeWith(clear) should be false")
	}
	if _, err := tp1.MergeWith(tp3); err == nil {
		t.Fatal("MergeWith(incompatible) should error")
	}
	// MergeWith picks the lower timestamp
	merged, err := tp1.MergeWith(tp2)
	if err != nil {
		t.Fatalf("MergeWith: %v", err)
	}
	mp := merged.(TrackingProps)
	if isoDate(mp.TS) != "2023-01-01T00:00:00.000Z" {
		t.Fatalf("merged ts = %s, want 2023", isoDate(mp.TS))
	}
	// Equals with non-matching type
	if tp1.Equals(tp3) {
		t.Fatal("tp1.Equals(tp3) should be false")
	}
	if tp1.Equals(clear) {
		t.Fatal("tp1.Equals(clear) should be false")
	}
}

func TestApplyToLength(t *testing.T) {
	o := NewTextOperation()
	mustOps(t, o.Retain(5, RetainBuilderOpts{}))
	n, err := o.ApplyToLength(5)
	if err != nil || n != 5 {
		t.Fatalf("ApplyToLength(5) = %d, %v", n, err)
	}
	o2 := NewTextOperation()
	mustOps(t, o2.Retain(5, RetainBuilderOpts{}))
	mustOps(t, o2.Insert("abc", InsertBuilderOpts{}))
	n2, err := o2.ApplyToLength(5)
	if err != nil || n2 != 8 {
		t.Fatalf("ApplyToLength(5) = %d, %v", n2, err)
	}
	// mismatched base length
	_, matchErr := o.ApplyToLength(3)
	if matchErr == nil {
		t.Fatal("ApplyToLength(3) should error")
	}
	assertErrType[*ApplyError](t, "ApplyToLength", matchErr)
	// NewTooLongError message
	tl := NewTooLongError(nil, 5)
	if !strings.Contains(tl.Error(), "resulting string would be too long") {
		t.Fatalf("TooLongError message = %q", tl.Error())
	}
	te := NewUnprocessableError("x")
	_, isUnproc := errorToAny(te).(*UnprocessableError)
	if !isUnproc {
		t.Fatal("te should be *UnprocessableError")
	}
}

func TestCanBeComposedWith(t *testing.T) {
	a := NewTextOperation()
	mustOps(t, a.Retain(3, RetainBuilderOpts{}))
	aTarget3 := NewTextOperation()
	mustOps(t, aTarget3.Insert("x", InsertBuilderOpts{})) // base 0 target 1
	if a.CanBeComposedWith(aTarget3) {                    // a.target=3, b.base=0
		t.Fatal("should not be composable")
	}
	b := NewTextOperation()
	mustOps(t, b.Retain(3, RetainBuilderOpts{})) // base 3
	if !a.CanBeComposedWith(b) {
		t.Fatal("should be composable")
	}
}

func TestScanOpErrorBranches(t *testing.T) {
	if _, err := NewRetainOp(-1, nil); err == nil {
		t.Fatal("NewRetainOp(-1) should error")
	}
	if _, err := NewRemoveOp(-1); err == nil {
		t.Fatal("NewRemoveOp(-1) should error")
	}
	if _, err := RemoveOpFromJSON(5); err == nil {
		t.Fatal("RemoveOpFromJSON(5) should error")
	}
	if _, err := RemoveOpFromJSON("abc"); err == nil {
		t.Fatal("RemoveOpFromJSON(abc) should error")
	}
	if _, err := RetainOpFromJSON(map[string]any{"r": "x"}); err == nil {
		t.Fatal("RetainOpFromJSON({r:x}) should error")
	}
	if _, err := InsertOpFromJSON(map[string]any{"i": 5}); err == nil {
		t.Fatal("InsertOpFromJSON({i:5}) should error")
	}
	_, err := ScanOpFromJSON(true)
	if err == nil {
		t.Fatal("ScanOpFromJSON(true) should error")
	}
	if !isUnprocessable(err) {
		t.Fatal("ScanOpFromJSON error should be Unprocessable")
	}
}

func TestRawConvBranches(t *testing.T) {
	// toAnySlice with []Range
	out := toAnySlice([]Range{NewRange(0, 3)})
	if len(out) != 1 {
		t.Fatalf("toAnySlice([]Range) = %d", len(out))
	}
	// toAnySlice with []string
	out2 := toAnySlice([]string{"a", "b"})
	if len(out2) != 2 {
		t.Fatalf("toAnySlice([]string) = %d", len(out2))
	}
	// asRawMap with map[string]int
	m := asRawMap(map[string]int{"pos": 1, "length": 2})
	if m["pos"] != 1 || m["length"] != 2 {
		t.Fatalf("asRawMap = %v", m)
	}
	// asRawMap with map[string]any
	m2 := asRawMap(map[string]any{"a": "b"})
	if m2["a"] != "b" {
		t.Fatalf("asRawMap = %v", m2)
	}
	// asRawObjects / asRawRanges
	objs := asRawObjects([]any{map[string]any{"id": "x"}})
	if len(objs) != 1 {
		t.Fatalf("asRawObjects = %d", len(objs))
	}
	ranges := asRawRanges([]any{map[string]any{"pos": 1, "length": 2}, map[string]int{"pos": 5, "length": 1}})
	if len(ranges) != 2 || ranges[0].Length != 2 || ranges[1].Pos != 5 {
		t.Fatalf("asRawRanges = %v", ranges)
	}
}

func TestGetSimpleOpBranches(t *testing.T) {
	// case 3: [retain, insert, retain]
	o := NewTextOperation()
	mustOps(t, o.Retain(2, RetainBuilderOpts{}))
	mustOps(t, o.Insert("x", InsertBuilderOpts{}))
	mustOps(t, o.Retain(2, RetainBuilderOpts{}))
	if getSimpleOp(o) == nil {
		t.Fatal("getSimpleOp([retain,insert,retain]) should be non-nil")
	}
	// case 2: [insert, retain]
	o2 := NewTextOperation()
	mustOps(t, o2.Insert("x", InsertBuilderOpts{}))
	mustOps(t, o2.Retain(2, RetainBuilderOpts{}))
	if getSimpleOp(o2) == nil {
		t.Fatal("getSimpleOp([insert,retain]) should be non-nil")
	}
	// [retain, insert]
	o3 := NewTextOperation()
	mustOps(t, o3.Retain(2, RetainBuilderOpts{}))
	mustOps(t, o3.Insert("x", InsertBuilderOpts{}))
	if getSimpleOp(o3) == nil {
		t.Fatal("getSimpleOp([retain,insert]) should be non-nil")
	}
	// [insert, remove] → null
	o4 := NewTextOperation()
	mustOps(t, o4.Insert("x", InsertBuilderOpts{}))
	mustOps(t, o4.Remove(1))
	if getSimpleOp(o4) != nil {
		t.Fatal("getSimpleOp([insert,remove]) should be nil")
	}
	// 4 ops → null
	o5 := NewTextOperation()
	mustOps(t, o5.Retain(1, RetainBuilderOpts{}))
	mustOps(t, o5.Insert("a", InsertBuilderOpts{}))
	mustOps(t, o5.Remove(1))
	mustOps(t, o5.Retain(1, RetainBuilderOpts{}))
	if getSimpleOp(o5) != nil {
		t.Fatal("getSimpleOp(4 ops) should be nil")
	}
	// CanBeComposedWithForUndo with [retain,insert,retain]
	other := NewTextOperation()
	mustOps(t, other.Retain(0, RetainBuilderOpts{}))
	mustOps(t, other.Insert("y", InsertBuilderOpts{}))
	_ = o.CanBeComposedWithForUndo(other)
	_ = o.CanBeComposedWithForUndo(NewTextOperation())
}

func TestTrackedChangeCanMergeMerge(t *testing.T) {
	tp := NewTrackingProps("insert", "u", ts(t, "2023-01-01T00:00:00.000Z"))
	t1 := NewTrackedChange(NewRange(0, 3), tp)
	t2 := NewTrackedChange(NewRange(3, 2), tp)
	if !t1.CanMerge(t2) {
		t.Fatal("t1.CanMerge(t2) should be true")
	}
	merged, err := t1.Merge(t2)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if merged.Range.Length != 5 || merged.Range.Pos != 0 {
		t.Fatalf("merged range = %v", merged.Range)
	}
	// non-mergeable
	t3 := NewTrackedChange(NewRange(10, 2), tp)
	if t1.CanMerge(t3) {
		t.Fatal("t1.CanMerge(t3) should be false")
	}
	if _, err := t1.Merge(t3); err == nil {
		t.Fatal("Merge(non-mergeable) should error")
	}
	// IntersectRange nil
	if t1.IntersectRange(NewRange(10, 2)) != nil {
		t.Fatal("IntersectRange(non-overlap) should be nil")
	}
}

func TestNumberToIntBranches(t *testing.T) {
	if v, ok := numberToInt(int64(5)); !ok || v != 5 {
		t.Fatalf("numberToInt(int64) = %d %v", v, ok)
	}
	if v, ok := numberToInt(float64(5)); !ok || v != 5 {
		t.Fatalf("numberToInt(float64) = %d %v", v, ok)
	}
	if _, ok := numberToInt("x"); ok {
		t.Fatal("numberToInt(string) should be not ok")
	}
}

func TestIsRangeBranches(t *testing.T) {
	if _, ok := IsRange(Range{Pos: 1, Length: 2}); !ok {
		t.Fatal("IsRange(Range) should be ok")
	}
	r := Range{Pos: 1, Length: 2}
	if _, ok := IsRange(&r); !ok {
		t.Fatal("IsRange(*Range) should be ok")
	}
	if _, ok := IsRange(5); ok {
		t.Fatal("IsRange(5) should not be ok")
	}
}

// errorToAny helper to test type assertion.
func errorToAny(e error) any { return e }

func isUnprocessable(e error) bool {
	for e != nil {
		if _, ok := e.(*UnprocessableError); ok {
			return true
		}
		type u interface{ Unwrap() error }
		un, ok := e.(u)
		if !ok {
			return false
		}
		e = un.Unwrap()
	}
	return false
}
