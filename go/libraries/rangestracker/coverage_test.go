package rangestracker

import (
	"reflect"
	"testing"
	"time"
)

// coverage_test.go — closes the uncovered branches beyond the oracle +
// supplement suites (absent/null user_id arms, sort tie-breaks, error
// surfaces, remaining comment/delete branches).

func TestChangeLookupAndRemoveById(t *testing.T) {
	base := []Change{
		{ID: "solo", Op: Op{I: ptrTo("x"), P: 1}},
		{ID: "dup", Op: Op{I: ptrTo("y"), P: 2}},
		{ID: "dup", Op: Op{D: ptrTo("z"), P: 3}},
	}
	rt := New(base, nil)
	if got := rt.GetChange("solo"); got == nil || got.Op.P != 1 {
		t.Fatalf("GetChange(solo) = %v", got)
	}
	if got := rt.GetChange("nope"); got != nil {
		t.Fatalf("GetChange(nope) = %v", got)
	}
	rt.RemoveChangeId("dup") // singular wrapper removes BOTH (Node: [id] set)
	if len(rt.Changes) != 1 || rt.Changes[0].ID != "solo" {
		t.Fatalf("after remove = %v", changesOps(rt.Changes))
	}
	rt.RemoveChangeIds(nil) // early return no-op
	if len(rt.Changes) != 1 {
		t.Fatalf("nil ids changed changes")
	}
}

func TestValidateOutOfRangeInsert(t *testing.T) {
	rt := New([]Change{{ID: "c", Op: Op{I: ptrTo("world"), P: 9}}}, nil)
	err := rt.Validate("hello world")
	if err == nil || err.Error() != "insertion does not match text in document" {
		t.Fatalf("got %v", err)
	}
}

func TestValidateCommentNilContent(t *testing.T) {
	// Node throws TypeError on nil c; Go treats it as "" (documented).
	rt := New(nil, []CommentItem{{ID: "t", Op: Op{C: nil, P: 3}, Metadata: Metadata{}}})
	if err := rt.Validate("hello world"); err != nil {
		t.Fatalf("nil c should validate as empty: %v", err)
	}
	if commentText(rt.Comments[0]) != "" {
		t.Fatalf("commentText = %q", *rt.Comments[0].Op.C)
	}
}

func TestApplyOpCommentViaDispatch(t *testing.T) {
	rt := New(nil, nil)
	meta := Metadata{"user_id": "u"}
	if err := rt.ApplyOp(CommentOp("hi", 0, "thr-1"), meta); err != nil {
		t.Fatal(err)
	}
	if len(rt.Comments) != 1 || rt.Comments[0].ID != "thr-1" {
		t.Fatalf("comments = %v", rt.Comments)
	}
	if _, ok := meta["ts"].(time.Time); !ok {
		t.Fatalf("ts not defaulted: %v", meta)
	}
}

func TestApplyOpsErrorPropagation(t *testing.T) {
	rt := New(nil, nil)
	rt.TrackChanges = true
	ops := []*Op{InsertOp("a", 0, nil), {}} // second is the unknown-op
	err := rt.ApplyOps(ops, metaUser("u"))
	if err != ErrUnknownOp {
		t.Fatalf("got %v, want ErrUnknownOp", err)
	}
	// first op still applied (tracked insert recorded)
	if len(rt.Changes) != 1 || deref(rt.Changes[0].Op.I) != "a" {
		t.Fatalf("first op not applied: %v", changesOps(rt.Changes))
	}
}

func TestDeleteCommentRemainingAfter(t *testing.T) {
	// comment "hello" at 0; delete "el" at 1 (ends 3, inside the comment)
	rt := New(nil, []CommentItem{{ID: "t", Op: Op{C: ptrTo("hello"), P: 0}, Metadata: Metadata{}}})
	if err := rt.ApplyDeleteToComments(DeleteOp("el", 1, nil)); err != nil {
		t.Fatal(err)
	}
	c := rt.GetComment("t")
	if c.Op.P != 0 || *c.Op.C != "hlo" {
		t.Fatalf("got p=%d c=%q, want p=0 c=hlo", c.Op.P, *c.Op.C)
	}
}

func TestDeleteCommentBeforeCommentShiftsMin(t *testing.T) {
	// doc "hello"; comment "llo" at 2; delete "hel" at 0 (ends 3, overlaps)
	rt := New(nil, []CommentItem{{ID: "t", Op: Op{C: ptrTo("llo"), P: 2}, Metadata: Metadata{}}})
	if err := rt.ApplyDeleteToComments(DeleteOp("hel", 0, nil)); err != nil {
		t.Fatal(err)
	}
	c := rt.GetComment("t")
	if c.Op.P != 0 || *c.Op.C != "lo" {
		t.Fatalf("got p=%d c=%q, want p=0 c=lo", c.Op.P, *c.Op.C)
	}
}

func TestInsertShiftsFollowInsert(t *testing.T) {
	// insert "M" at 5; new insert "xy" at 2 shifts it to 7 (not tracking)
	rt := New([]Change{{ID: "m", Op: Op{I: ptrTo("M"), P: 5}}}, nil)
	mustApply(t, rt, InsertOp("xy", 2, nil), metaUser("u"))
	c := rt.GetChange("m")
	if c.Op.P != 7 {
		t.Fatalf("p = %d, want 7", c.Op.P)
	}
}

func TestInsertAfterInsertNoop(t *testing.T) {
	// insert at 5; delete at 0 (before it) shifts; delete at 9 (after) no-op
	rt := New([]Change{{ID: "m", Op: Op{I: ptrTo("M"), P: 5}}}, nil)
	mustApply(t, rt, DeleteOp("x", 9, nil), metaUser("u"))
	c := rt.GetChange("m")
	if c.Op.P != 5 {
		t.Fatalf("p = %d, want 5 (delete after insert is a no-op)", c.Op.P)
	}
}

func TestNonTrackingDeleteTouchingSnaps(t *testing.T) {
	// non-tracking: delete ending exactly at a tracked delete's start snaps it
	rt := New([]Change{{ID: "d", Op: Op{D: ptrTo("yy"), P: 4}}}, nil) // opEnd===changeStart
	mustApply(t, rt, DeleteOp("ab", 2, nil), metaUser("u"))
	c := rt.GetChange("d")
	if c.Op.P != 2 {
		t.Fatalf("p = %d, want 2 (snapped to op start)", c.Op.P)
	}
}

func TestNonTrackingSamePositionDeleteMerge(t *testing.T) {
	// non-tracking snapshot lands three tracked deletes at the same p;
	// scanAndMerge then joins them into the FIRST (previous not advanced —
	// the Node quirk): "xx"+"yy"+"zz" in order of removal.
	rt := New([]Change{
		{ID: "a", Op: Op{D: ptrTo("xx"), P: 6}},
		{ID: "b", Op: Op{D: ptrTo("yy"), P: 8}},
		{ID: "c", Op: Op{D: ptrTo("zz"), P: 8}},
	}, nil)
	mustApply(t, rt, DeleteOp("abcd", 4, nil), metaUser("u"))
	if len(rt.Changes) != 1 {
		t.Fatalf("len = %d (%v), want 1", len(rt.Changes), changesOps(rt.Changes))
	}
	c := rt.Changes[0]
	if c.Op.P != 4 || *c.Op.D != "xxyyzz" {
		t.Fatalf("got p=%d d=%q, want p=4 d=xxyyzz", c.Op.P, *c.Op.D)
	}
}

func TestUndoCancelShorterNextDelete(t *testing.T) {
	// willOpCancelNextDelete with a SHORTER next delete (equalPrefix false):
	// no merge into the insert, and the next delete is not rejected by prefix.
	rt := New([]Change{
		{ID: "i", Op: Op{I: ptrTo("a"), P: 0}, Metadata: metaUser("u")},
		{ID: "d", Op: Op{D: ptrTo("x"), P: 1}, Metadata: metaUser("u")},
	}, nil)
	rt.TrackChanges = true
	mustApply(t, rt, InsertOp("ab", 1, b(true)), metaUser("u"))
	// "ab" does NOT cancel the short delete "x" (equalPrefix false), so the
	// same-user merge into the earlier insert proceeds, and the delete
	// shifts after the insert.
	got := map[string]Op{}
	ops := changesOps(rt.Changes)
	for i, c := range rt.Changes {
		got[c.ID] = ops[i]
	}
	if len(rt.Changes) != 2 {
		t.Fatalf("len = %d: %v", len(rt.Changes), ops)
	}
	if d, ok := got["d"]; !ok || d.P != 3 || deref(d.D) != "x" {
		t.Fatalf("d = %v, want P=3 D=x", d)
	}
	if i, ok := got["i"]; !ok || i.P != 0 || deref(i.I) != "aab" {
		t.Fatalf("i = %v, want P=0 I=aab", i)
	}
}

func TestUndoRejectsMatchingNextDelete(t *testing.T) {
	// insert "a"@0, delete "ab"@1: undo-insert "ab"@1 cancels the delete.
	rt := New([]Change{
		{ID: "i", Op: Op{I: ptrTo("a"), P: 0}, Metadata: metaUser("u")},
		{ID: "d", Op: Op{D: ptrTo("ab"), P: 1}, Metadata: metaUser("u")},
	}, nil)
	rt.TrackChanges = true
	mustApply(t, rt, InsertOp("ab", 1, b(true)), metaUser("u"))
	if len(rt.Changes) != 1 {
		t.Fatalf("len = %d: %v", len(rt.Changes), changesOps(rt.Changes))
	}
	c := rt.Changes[0]
	if c.ID != "i" || deref(c.Op.I) != "a" || c.Op.P != 0 {
		t.Fatalf("got %v", c)
	}
}

func TestSortTieDeleteBeforeInsert(t *testing.T) {
	// whitebox: addOp sort must place deletes before inserts at equal p,
	// stable among same-kind.
	rt := New(nil, nil)
	a := &Change{ID: "ai", Op: Op{I: ptrTo("a"), P: 5}}
	rt.Changes = append(rt.Changes, a)
	b := &Change{ID: "bi", Op: Op{I: ptrTo("b"), P: 5}}
	rt.Changes = append(rt.Changes, b)
	sortChangesStable(rt.Changes)
	if rt.Changes[0].ID != "ai" || rt.Changes[1].ID != "bi" {
		t.Fatalf("same-kind stability broken: %v, %v", rt.Changes[0].ID, rt.Changes[1].ID)
	}
	d := &Change{ID: "cd", Op: Op{D: ptrTo("d"), P: 5}}
	rt.Changes = append(rt.Changes, d)
	sortChangesStable(rt.Changes)
	if rt.Changes[0].ID != "cd" {
		t.Fatalf("delete not first at equal p: %v", rt.Changes[0].ID)
	}
}

func TestSameUserEdges(t *testing.T) {
	if !sameUser(nil, nil) {
		t.Fatalf("nil,nil should be same user (undefined===undefined)")
	}
	if sameUser(Metadata{}, Metadata{"user_id": nil}) {
		t.Fatalf("absent vs null must be false (JS ===)")
	}
	if !sameUser(Metadata{"user_id": nil}, Metadata{"user_id": nil}) {
		t.Fatalf("null===null should be true")
	}
	if sameUser(Metadata{"user_id": "a"}, Metadata{"user_id": "b"}) {
		t.Fatalf("different ids must be false")
	}
	if sameUser(Metadata{"user_id": 1}, Metadata{"user_id": "1"}) {
		t.Fatalf("JS 1 === '1' is false; sameUser must be false")
	}
}

func TestPickTimestampNilAndPtr(t *testing.T) {
	ts := tsDate("2024-05-01T00:00:00Z")
	ptrTS := &ts
	// nil map → missing
	if got := pickTimestamp(nil, Metadata{"ts": ts}); got != ts {
		t.Fatalf("got %v", got)
	}
	// *time.Time arm (newer) vs older plain value → older wins
	oldTS := tsDate("2023-01-01T00:00:00Z")
	if got := pickTimestamp(Metadata{"ts": oldTS}, Metadata{"ts": ptrTS}); !reflect.DeepEqual(got, oldTS) {
		t.Fatalf("got %v, want %v", got, oldTS)
	}
	// *time.Time arm (newer plain value) vs older pointer → pointer value wins
	olderPtr := tsDate("2022-01-01T00:00:00Z")
	if got := pickTimestamp(Metadata{"ts": ts}, Metadata{"ts": &olderPtr}); !reflect.DeepEqual(got, &olderPtr) {
		t.Fatalf("got %v, want %v", got, &olderPtr)
	}
	// nil *time.Time → invalid (Node: new Date(null) = 1970, but Go
	// documents nil pointer as absent)
	if got := pickTimestamp(Metadata{"ts": (*time.Time)(nil)}, Metadata{"ts": ts}); got != ts {
		t.Fatalf("got %v", got)
	}
}

func TestOpModificationMismatchErrors(t *testing.T) {
	// Contrived: an absorbed tracked delete is inserted (i-mod) inside the
	// span a later delete-modification expects to delete verbatim → the
	// verbatim check must fail with the Node-pinned error.
	rt := New([]Change{
		{ID: "ins", Op: Op{I: ptrTo("XYZ"), P: 1}, Metadata: metaUser("u1")},
		{ID: "del", Op: Op{D: ptrTo("QQ"), P: 2}, Metadata: metaUser("u2")},
	}, nil)
	rt.TrackChanges = true
	err := rt.ApplyOp(DeleteOp("abcdef", 0, nil), metaUser("u3"))
	if err != ErrDeleteMismatch {
		t.Fatalf("got %v, want ErrDeleteMismatch", err)
	}
}
