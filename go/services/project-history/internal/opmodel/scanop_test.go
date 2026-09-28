package opmodel

import "testing"

// Mirrors vendor test/unit/scan_op.test.js (overleaf-editor-core).
//
// Merge semantics: vendor mutates op1 via a.mergeWith(b); the Go port uses
// value receivers (MergeWith is documented no-op) and mergeScanOps(a, b)
// returns the merged value. Tests assert on mergeScanOps results.

func mustMerge(t *testing.T, a, b ScanOp) ScanOp {
	t.Helper()
	if !a.CanMergeWith(b) {
		t.Fatalf("CanMergeWith: %v ? %v = false", a, b)
	}
	return mergeScanOps(a, b)
}

func assertNotMergeable(t *testing.T, a, b ScanOp, msg string) {
	t.Helper()
	assertBoolT(t, !a.CanMergeWith(b) && !b.CanMergeWith(a), true, msg)
}

func ts1() *Tracking  { return NewTracking("insert", "user1", unixMs("2024-01-01T00:00:00Z")) }
func ts1b() *Tracking { return NewTracking("insert", "user1", unixMs("2024-01-01T00:00:01Z")) }
func ts2() *Tracking  { return NewTracking("insert", "user2", unixMs("2024-01-01T00:00:00Z")) }

func unixMs(s string) int64 {
	// Deterministic fixture constants (vendor tests pin ms values).
	switch s {
	case "2024-01-01T00:00:00Z":
		return 1704067200000
	case "2024-01-01T00:00:01Z":
		return 1704067201000
	}
	panic("unknown fixture ts " + s)
}

func TestScanOp_FromJSON_Dispatch(t *testing.T) {
	// Vendor: "constructs a RetainOp from object".
	op, err := ScanOpFromJSON(map[string]any{"r": float64(1)})
	if err != nil {
		t.Fatalf("retain obj: %v", err)
	}
	if ro, ok := op.(RetainOp); !ok || ro.Length != 1 {
		t.Fatalf("retain obj: got %v", op)
	}

	// Vendor: "constructs a RetainOp from number".
	op, err = ScanOpFromJSON(float64(2))
	if err != nil {
		t.Fatalf("retain num: %v", err)
	}
	if ro, ok := op.(RetainOp); !ok || ro.Length != 2 {
		t.Fatalf("retain num: got %v", op)
	}

	// Vendor: "constructs an InsertOp from string".
	op, err = ScanOpFromJSON("abc")
	if err != nil {
		t.Fatalf("insert str: %v", err)
	}
	if io, ok := op.(InsertOp); !ok || io.Insertion != "abc" {
		t.Fatalf("insert str: got %v", op)
	}

	// Vendor: "constructs an InsertOp from object".
	op, err = ScanOpFromJSON(map[string]any{"i": "abc"})
	if err != nil {
		t.Fatalf("insert obj: %v", err)
	}
	if io, ok := op.(InsertOp); !ok || io.Insertion != "abc" {
		t.Fatalf("insert obj: got %v", op)
	}

	// Vendor: "constructs a RemoveOp from number" (wire negative).
	op, err = ScanOpFromJSON(float64(-2))
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if ro, ok := op.(RemoveOp); !ok || ro.Length != 2 {
		t.Fatalf("remove: got %v", op)
	}

	// Vendor: "throws an error for invalid input".
	if _, err := ScanOpFromJSON(map[string]any{}); err == nil {
		t.Fatalf("empty obj: expected error")
	}

	// Vendor: "throws an error for zero".
	if _, err := ScanOpFromJSON(float64(0)); err == nil {
		t.Fatalf("zero: expected error")
	}
}

func TestRetainOp_Equals(t *testing.T) {
	a, b := NewRetainOp(1, nil), NewRetainOp(1, nil)
	assertBoolT(t, a.Equals(b), true, "same length equal")
	a2 := NewRetainOp(1, nil)
	b2 := NewRetainOp(2, nil)
	assertBoolT(t, a2.Equals(b2), false, "diff length not equal")

	aT := NewRetainOp(4, ts1())
	aN := NewRetainOp(4, nil)
	assertBoolT(t, aT.Equals(aN), false, "tracking vs none not equal (a)")
	assertBoolT(t, aN.Equals(aT), false, "none vs tracking not equal (b)")

	aT2 := NewRetainOp(4, ts1())
	bT := NewRetainOp(4, ts2())
	assertBoolT(t, aT2.Equals(bT), false, "diff user not equal")

	assertBoolT(t, NewRetainOp(1, nil).Equals(InsertOp{Insertion: "a"}), false, "retain != insert")
	assertBoolT(t, NewRetainOp(1, nil).Equals(RemoveOp{Length: 1}), false, "retain != remove")
}

func TestRetainOp_Merge(t *testing.T) {
	// Vendor: "can merge with another RetainOp".
	merged := mustMerge(t, NewRetainOp(1, nil), NewRetainOp(2, nil))
	assertBoolT(t, merged.Equals(NewRetainOp(3, nil)), true, "1+2=3")

	// Vendor: "cannot merge ... different tracking user".
	assertNotMergeable(t, NewRetainOp(4, ts1()), NewRetainOp(4, ts2()), "diff user")

	// Vendor: "can merge ... same tracking user".
	mergedT := mustMerge(t, NewRetainOp(4, ts1()), NewRetainOp(4, ts1b()))
	wantT := NewRetainOp(8, ts1())
	assertBoolT(t, mergedT.Equals(wantT), true, "merged tracking keeps min ts")

	// Vendor: "cannot merge with InsertOp / RemoveOp".
	assertNotMergeable(t, NewRetainOp(1, nil), InsertOp{Insertion: "a"}, "retain+insert")
	assertNotMergeable(t, NewRetainOp(1, nil), RemoveOp{Length: 1}, "retain+remove")
}

func TestRetainOp_ToWire(t *testing.T) {
	assertWireT(t, NewRetainOp(3, nil).ToWire(), 3, "plain number")
	assertWireT(t, NewRetainOp(3, ts1()).ToWire(), map[string]any{
		"r":        3,
		"tracking": map[string]any{"type": "insert", "userId": "user1", "ts": "2024-01-01T00:00:00.000Z"},
	}, "with tracking")
}

func TestRetainOp_ApplyToLength(t *testing.T) {
	// Vendor: "adds to the length and cursor when applied to length".
	ctx := &LengthApplyContext{Length: 10, InputCursor: 10, InputLength: 30}
	if err := NewRetainOp(3, nil).ApplyToLength(ctx); err != nil {
		t.Fatalf("apply: %v", err)
	}
	assertEqualT(t, ctx.Length, 13, "length")
	assertEqualT(t, ctx.InputCursor, 13, "cursor")

	// Vendor: "cannot apply to length if the input cursor is at the end".
	ctx2 := &LengthApplyContext{Length: 10, InputCursor: 10, InputLength: 10}
	if err := NewRetainOp(10, nil).ApplyToLength(ctx2); err == nil {
		t.Fatalf("expected ApplyError for over-retain")
	}
}

func TestInsertOp_Equals(t *testing.T) {
	i1, _ := NewInsertOp("a", nil, nil)
	i1b, _ := NewInsertOp("a", nil, nil)
	assertBoolT(t, i1.Equals(i1b), true, "same insertion equal")
	i2, _ := NewInsertOp("b", nil, nil)
	assertBoolT(t, i1.Equals(i2), false, "diff insertion not equal")

	it, _ := NewInsertOp("a", ts1(), nil)
	in, _ := NewInsertOp("a", nil, nil)
	assertBoolT(t, it.Equals(in), false, "tracking vs none (a)")
	assertBoolT(t, in.Equals(it), false, "none vs tracking (b)")

	itU1, _ := NewInsertOp("a", ts1(), nil)
	itU2, _ := NewInsertOp("a", ts2(), nil)
	assertBoolT(t, itU1.Equals(itU2), false, "diff user not equal")

	ic1, _ := NewInsertOp("a", nil, []string{"1"})
	assertBoolT(t, ic1.Equals(in), false, "ids vs none (a)")
	assertBoolT(t, in.Equals(ic1), false, "none vs ids (b)")

	ic1b, _ := NewInsertOp("a", nil, []string{"1"})
	ic2, _ := NewInsertOp("a", nil, []string{"2"})
	assertBoolT(t, ic1b.Equals(ic2), false, "diff ids not equal")

	ic12, _ := NewInsertOp("a", nil, []string{"1", "2"})
	ic21, _ := NewInsertOp("a", nil, []string{"2", "1"})
	assertBoolT(t, ic12.Equals(ic21), true, "overlapping id sets equal (vendor: same set)")

	assertBoolT(t, i1.Equals(NewRetainOp(1, nil)), false, "insert != retain")
	assertBoolT(t, i1.Equals(RemoveOp{Length: 1}), false, "insert != remove")
}

func TestInsertOp_Merge(t *testing.T) {
	// Vendor: "can merge with another InsertOp".
	merged := mustMerge(t, InsertOp{Insertion: "a"}, InsertOp{Insertion: "b"})
	assertBoolT(t, merged.Equals(InsertOp{Insertion: "ab"}), true, "ab")

	// Vendor: "cannot merge ... comment id info is different".
	a1, _ := NewInsertOp("a", nil, []string{"1"})
	b12, _ := NewInsertOp("b", nil, []string{"1", "2"})
	assertNotMergeable(t, a1, b12, "ids 1 vs 1,2")

	// Vendor: "tracking info matches ... comment id different".
	aT12, _ := NewInsertOp("a", ts1(), []string{"1", "2"})
	bT3, _ := NewInsertOp("b", ts1(), []string{"3"})
	assertNotMergeable(t, aT12, bT3, "tracking same, ids diff")

	// Vendor: "comment id is present in other and tracking info matches".
	aT, _ := NewInsertOp("a", ts1(), nil)
	bT1, _ := NewInsertOp("b", ts1(), []string{"1"})
	assertNotMergeable(t, aT, bT1, "no ids vs ids with same tracking")

	// Vendor: "tracking user is different".
	aTU1, _ := NewInsertOp("a", ts1(), nil)
	bTU2, _ := NewInsertOp("b", ts2(), nil)
	assertNotMergeable(t, aTU1, bTU2, "diff user")

	// Vendor: "can merge ... tracking user and comment info the same".
	bT12b, _ := NewInsertOp("b", ts1b(), []string{"1", "2"})
	mergedT := mustMerge(t, aT12, bT12b)
	assertBoolT(t, mergedT.Equals(InsertOp{Insertion: "ab", Tracking: ts1(), CommentIDs: []string{"1", "2"}}), true, "merged insert tracking")

	// Vendor: "cannot merge with RetainOp / RemoveOp".
	assertNotMergeable(t, InsertOp{Insertion: "a"}, NewRetainOp(1, nil), "insert+retain")
	assertNotMergeable(t, InsertOp{Insertion: "a"}, RemoveOp{Length: 1}, "insert+remove")
}

func TestInsertOp_ToWire(t *testing.T) {
	i1, _ := NewInsertOp("a", nil, nil)
	assertWireT(t, i1.ToWire(), "a", "plain string")
	iT, _ := NewInsertOp("a", ts1(), nil)
	assertWireT(t, iT.ToWire(), map[string]any{
		"i":        "a",
		"tracking": map[string]any{"type": "insert", "userId": "user1", "ts": "2024-01-01T00:00:00.000Z"},
	}, "with tracking")
	iC, _ := NewInsertOp("a", nil, []string{"1"})
	assertWireT(t, iC.ToWire(), map[string]any{"i": "a", "commentIds": []string{"1"}}, "with commentIds")
}

func TestInsertOp_ApplyToLength(t *testing.T) {
	// Vendor: "adds to the length when applied to length".
	ctx := &LengthApplyContext{Length: 10, InputCursor: 20, InputLength: 40}
	if err := (InsertOp{Insertion: "abc"}).ApplyToLength(ctx); err != nil {
		t.Fatalf("apply: %v", err)
	}
	assertEqualT(t, ctx.Length, 13, "length +3")
	assertEqualT(t, ctx.InputCursor, 20, "cursor unchanged")
}

func TestInsertOp_NonBMPRejection(t *testing.T) {
	if _, err := NewInsertOp("a\U00010000b", nil, nil); err == nil {
		t.Fatalf("non-BMP insert accepted")
	}
	if _, err := ScanOpFromJSON("a\U00010000b"); err == nil {
		t.Fatalf("non-BMP insert wire accepted")
	}
}

func TestRemoveOp_Equals(t *testing.T) {
	a, b := RemoveOp{Length: 1}, RemoveOp{Length: 1}
	assertBoolT(t, a.Equals(b), true, "same length equal")
	assertBoolT(t, RemoveOp{Length: 1}.Equals(RemoveOp{Length: 2}), false, "diff length")
	assertBoolT(t, RemoveOp{Length: 1}.Equals(NewRetainOp(1, nil)), false, "remove != retain")
	assertBoolT(t, RemoveOp{Length: 1}.Equals(InsertOp{Insertion: "a"}), false, "remove != insert")
}

func TestRemoveOp_MergeAndWire(t *testing.T) {
	merged := mustMerge(t, RemoveOp{Length: 1}, RemoveOp{Length: 2})
	assertBoolT(t, merged.Equals(RemoveOp{Length: 3}), true, "1+2=3")
	assertNotMergeable(t, RemoveOp{Length: 1}, NewRetainOp(1, nil), "remove+retain")
	assertNotMergeable(t, RemoveOp{Length: 1}, InsertOp{Insertion: "a"}, "remove+insert")

	assertWireT(t, RemoveOp{Length: 3}.ToWire(), -3, "wire negative")
}

func TestRemoveOp_ApplyToLength(t *testing.T) {
	ctx := &LengthApplyContext{Length: 10, InputCursor: 10, InputLength: 30}
	rem := RemoveOp{Length: 3}
	if err := rem.ApplyToLength(ctx); err != nil {
		t.Fatalf("apply: %v", err)
	}
	assertEqualT(t, ctx.Length, 10, "length unchanged")
	assertEqualT(t, ctx.InputCursor, 13, "cursor +3")
}

func TestScanOp_WirePredicates(t *testing.T) {
	assertBoolT(t, IsRetainWire(float64(5)), true, "num>0 retain")
	assertBoolT(t, IsRetainWire(map[string]any{"r": float64(5)}), true, "obj r>0 retain")
	assertBoolT(t, IsInsertWire("abc"), true, "string insert")
	assertBoolT(t, IsInsertWire(map[string]any{"i": "abc"}), true, "obj i insert")
	assertBoolT(t, IsRemoveWire(float64(-2)), true, "num<0 remove")
	assertBoolT(t, IsRemoveWire(float64(0)), false, "zero not remove")
	assertBoolT(t, IsRemoveWire(map[string]any{"i": "a"}), false, "obj not remove")
}
