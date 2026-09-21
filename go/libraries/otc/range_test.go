package otc

import "testing"

func mustRange(t *testing.T, pos, length int) Range {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic creating range(%d,%d): %v", pos, length, r)
		}
	}()
	return NewRange(pos, length)
}

func expectPanic(t *testing.T, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic, but none happened")
		}
	}()
	f()
}

func TestRangeBasics(t *testing.T) {
	r := mustRange(t, 5, 10)
	if r.Start() != 5 || r.End() != 15 {
		t.Fatalf("start/end = %d/%d, want 5/15", r.Start(), r.End())
	}
	fr := FromRawRange(map[string]int{"pos": 5, "length": 10})
	if fr.Start() != 5 || fr.End() != 15 {
		t.Fatalf("fromRaw start/end = %d/%d, want 5/15", fr.Start(), fr.End())
	}
	sameRaw(t, "toRaw", mustRange(t, 5, 10).ToRaw(), map[string]int{"pos": 5, "length": 10})
	if mustRange(t, 5, 10).IsEmpty() {
		t.Fatal("range(5,10).isEmpty should be false")
	}
	if !mustRange(t, 5, 0).IsEmpty() {
		t.Fatal("range(5,0).isEmpty should be true")
	}
	expectPanic(t, func() { NewRange(-1, 10) })
	expectPanic(t, func() { NewRange(0, -2) })
}

func TestRangeOverlaps(t *testing.T) {
	r1 := mustRange(t, 1, 3)
	r2 := mustRange(t, 1, 3)
	if !r1.Overlaps(r2) {
		t.Fatal("same ranges should overlap")
	}
	a := mustRange(t, 1, 3)
	b := mustRange(t, 10, 3)
	if a.Overlaps(b) || b.Overlaps(a) {
		t.Fatal("non-touching ranges should not overlap")
	}
	c := mustRange(t, 4, 3)
	if a.Overlaps(c) || c.Overlaps(a) {
		t.Fatal("touching ranges should not overlap")
	}
	d := mustRange(t, 2, 3)
	if !a.Overlaps(d) || !d.Overlaps(a) {
		t.Fatal("overlapping ranges should overlap")
	}
}

func TestRangeTouches(t *testing.T) {
	r1 := mustRange(t, 1, 3)
	if r1.Touches(r1) {
		t.Fatal("same range should not touch")
	}
	a := mustRange(t, 1, 3)
	b := mustRange(t, 4, 2)
	if !a.Touches(b) || !b.Touches(a) {
		t.Fatal("touching ranges should touch")
	}
	c := mustRange(t, 5, 2)
	if a.Touches(c) || c.Touches(a) {
		t.Fatal("non-touching ranges should not touch")
	}
	d := mustRange(t, 3, 2)
	if a.Touches(d) || d.Touches(a) {
		t.Fatal("overlapping ranges should not touch")
	}
}

func TestRangeContains(t *testing.T) {
	from0to2 := mustRange(t, 0, 3)
	from4to13 := mustRange(t, 4, 10)
	from4to14 := mustRange(t, 4, 11)
	from4to15 := mustRange(t, 4, 12)
	from5to13 := mustRange(t, 5, 9)
	from5to14 := mustRange(t, 5, 10)
	from5to15 := mustRange(t, 5, 11)
	from0to99 := mustRange(t, 0, 100)

	check := func(got bool, want bool, label string) {
		if got != want {
			t.Fatalf("%s: got %v, want %v", label, got, want)
		}
	}
	check(from0to2.Contains(from0to2), true, "0to2 contains 0to2")
	check(from0to2.Contains(from4to13), false, "0to2 contains 4to13")
	check(from0to2.Contains(from0to99), false, "0to2 contains 0to99")

	check(from4to13.Contains(from0to2), false, "4to13 contains 0to2")
	check(from4to13.Contains(from4to13), true, "4to13 contains 4to13")
	check(from4to13.Contains(from5to13), true, "4to13 contains 5to13")
	check(from4to13.Contains(from5to14), false, "4to13 contains 5to14")

	check(from4to14.Contains(from4to13), true, "4to14 contains 4to13")
	check(from4to14.Contains(from4to14), true, "4to14 contains 4to14")
	check(from4to14.Contains(from5to14), true, "4to14 contains 5to14")
	check(from4to14.Contains(from5to15), false, "4to14 contains 5to15")

	check(from4to15.Contains(from5to15), true, "4to15 contains 5to15")

	check(from5to14.Contains(from5to13), true, "5to14 contains 5to13")
	check(from5to14.Contains(from5to15), false, "5to14 contains 5to15")

	check(from5to15.Contains(from5to13), true, "5to15 contains 5to13")
	check(from5to15.Contains(from5to14), true, "5to15 contains 5to14")
	check(from5to15.Contains(from5to15), true, "5to15 contains 5to15")

	check(from0to99.Contains(from0to2), true, "0to99 contains 0to2")
	check(from0to99.Contains(from5to15), true, "0to99 contains 5to15")
	check(from0to99.Contains(from0to99), true, "0to99 contains 0to99")
}

func TestRangeContainsCursor(t *testing.T) {
	r := mustRange(t, 5, 10)
	// start=5 end=15
	if r.ContainsCursor(4) {
		t.Fatal("cursor 4 should not be contained")
	}
	for _, c := range []int{5, 6, 14, 15} {
		if !r.ContainsCursor(c) {
			t.Fatalf("cursor %d should be contained", c)
		}
	}
	if r.ContainsCursor(16) {
		t.Fatal("cursor 16 should not be contained")
	}
}

func TestRangeSubtract(t *testing.T) {
	check := func(got Range, wantPos, wantLen int) {
		if got.Start() != wantPos || got.Length != wantLen {
			t.Fatalf("subtract = (%d,%d), want (%d,%d)", got.Start(), got.Length, wantPos, wantLen)
		}
	}
	// no subtract
	check(mustRange(t, 1, 6).Subtract(mustRange(t, 0, 1)), 1, 6)
	// subtract from the left
	check(mustRange(t, 15, 10).Subtract(mustRange(t, 5, 15)), 5, 5)
	// subtract from the right
	check(mustRange(t, 5, 15).Subtract(mustRange(t, 10, 15)), 5, 5)
	// subtract from the middle
	check(mustRange(t, 5, 15).Subtract(mustRange(t, 10, 5)), 5, 10)
	// delete entire range
	check(mustRange(t, 5, 15).Subtract(mustRange(t, 0, 100)), 5, 0)
	// no overlap
	from5to14 := mustRange(t, 5, 10)
	from20to29 := mustRange(t, 20, 10)
	check(from5to14.Subtract(from20to29), 5, 10)
	check(from20to29.Subtract(from5to14), 20, 10)
}

func TestRangeMerge(t *testing.T) {
	check := func(r, other Range, wantPos, wantEnd int) {
		if !r.CanMerge(other) {
			t.Fatalf("canMerge(%v,%v) should be true", r, other)
		}
		res := r.Merge(other)
		if res.Start() != wantPos || res.End() != wantEnd {
			t.Fatalf("merge = (%d,%d), want (%d,%d)", res.Start(), res.End(), wantPos, wantEnd)
		}
	}
	check(mustRange(t, 5, 10), mustRange(t, 10, 10), 5, 20)
	check(mustRange(t, 5, 10), mustRange(t, 0, 10), 0, 15)
	check(mustRange(t, 5, 10), mustRange(t, 0, 20), 0, 20)
	check(mustRange(t, 0, 20), mustRange(t, 5, 10), 0, 20)

	noA := mustRange(t, 5, 10)
	noB := mustRange(t, 20, 10)
	if noA.CanMerge(noB) || noB.CanMerge(noA) {
		t.Fatal("non-touching ranges should not merge")
	}
	expectPanic(t, func() { noA.Merge(noB) })
}

func TestRangeStartsAfter(t *testing.T) {
	from0to4 := mustRange(t, 0, 5)
	from1to5 := mustRange(t, 1, 5)
	from5to9 := mustRange(t, 5, 5)
	from6to10 := mustRange(t, 6, 5)
	from10to14 := mustRange(t, 10, 5)

	cases := []struct {
		a, b Range
		want bool
	}{
		{from0to4, from0to4, false}, {from0to4, from1to5, false}, {from0to4, from5to9, false},
		{from0to4, from6to10, false}, {from0to4, from10to14, false},
		{from1to5, from0to4, false}, {from1to5, from5to9, false}, {from1to5, from10to14, false},
		{from5to9, from0to4, true}, {from5to9, from1to5, false}, {from5to9, from5to9, false},
		{from6to10, from0to4, true}, {from6to10, from1to5, true}, {from6to10, from5to9, false},
		{from10to14, from0to4, true}, {from10to14, from1to5, true}, {from10to14, from5to9, true},
		{from10to14, from6to10, false}, {from10to14, from10to14, false},
	}
	for i, c := range cases {
		if c.a.StartsAfter(c.b) != c.want {
			t.Fatalf("case %d: startsAfter got %v, want %v", i, c.a.StartsAfter(c.b), c.want)
		}
	}
}

func TestRangeStartIsAfter(t *testing.T) {
	r := mustRange(t, 5, 10)
	if !r.StartIsAfter(3) || !r.StartIsAfter(4) {
		t.Fatal("startIsAfter(3)/startIsAfter(4) should be true")
	}
	if r.StartIsAfter(5) || r.StartIsAfter(6) || r.StartIsAfter(15) || r.StartIsAfter(16) {
		t.Fatal("startIsAfter(5/6/15/16) should be false")
	}
}

func TestRangeExtendMoveShrink(t *testing.T) {
	r := mustRange(t, 5, 10)
	e := r.ExtendBy(3)
	if e.Length != 13 || e.Start() != 5 || e.End() != 18 {
		t.Fatalf("extendBy = (%d,%d,%d), want (5,13,18)", e.Start(), e.Length, e.End())
	}
	s := r.ShrinkBy(3)
	if s.Length != 7 || s.Start() != 5 || s.End() != 12 {
		t.Fatalf("shrinkBy = (%d,%d,%d), want (5,7,12)", s.Start(), s.Length, s.End())
	}
	expectPanic(t, func() { r.ShrinkBy(11) })
	m := r.MoveBy(3)
	if m.Length != 10 || m.Start() != 8 || m.End() != 18 {
		t.Fatalf("moveBy = (%d,%d,%d), want (8,10,18)", m.Start(), m.Length, m.End())
	}
}

func TestRangeSplitAt(t *testing.T) {
	r := mustRange(t, 5, 10)
	parts := r.SplitAt(5)
	if !parts[0].IsEmpty() {
		t.Fatal("splitAt(5) left should be empty")
	}
	if parts[1].Start() != 5 || parts[1].End() != 15 {
		t.Fatalf("splitAt(5) right = (%d,%d), want (5,15)", parts[1].Start(), parts[1].End())
	}
	expectPanic(t, func() { r.SplitAt(4) })

	p14 := r.SplitAt(14)
	if p14[0].Start() != 5 || p14[0].End() != 14 || p14[1].Start() != 14 || p14[1].End() != 15 {
		t.Fatalf("splitAt(14) = (%v %v), want (5,14)/(14,15)", p14[0], p14[1])
	}
	expectPanic(t, func() { r.SplitAt(16) })

	p15 := r.SplitAt(15)
	if p15[0].Start() != 5 || p15[0].End() != 15 || p15[1].Start() != 15 || p15[1].End() != 15 {
		t.Fatalf("splitAt(15) = (%v %v), want (5,15)/(15,15)", p15[0], p15[1])
	}

	p10 := r.SplitAt(10)
	if p10[0].Start() != 5 || p10[0].End() != 10 || p10[1].Start() != 10 || p10[1].End() != 15 {
		t.Fatalf("splitAt(10) = (%v %v), want (5,10)/(10,15)", p10[0], p10[1])
	}
}

func TestRangeInsertAt(t *testing.T) {
	r := mustRange(t, 5, 10)
	at5 := r.InsertAt(5, 3)
	if !at5[0].IsEmpty() {
		t.Fatal("insertAt(5) left should be empty")
	}
	if at5[1].Start() != 5 || at5[1].End() != 8 {
		t.Fatalf("insertAt(5) inserted = (%d,%d), want (5,8)", at5[1].Start(), at5[1].End())
	}
	if at5[2].Start() != 8 || at5[2].End() != 18 {
		t.Fatalf("insertAt(5) right = (%d,%d), want (8,18)", at5[2].Start(), at5[2].End())
	}
	at15 := r.InsertAt(15, 3)
	if at15[0].Start() != 5 || at15[0].End() != 15 {
		t.Fatalf("insertAt(15) left = (%d,%d), want (5,15)", at15[0].Start(), at15[0].End())
	}
	if at15[1].Start() != 15 || at15[1].End() != 18 {
		t.Fatalf("insertAt(15) inserted = (%d,%d), want (15,18)", at15[1].Start(), at15[1].End())
	}
	if !at15[2].IsEmpty() {
		t.Fatal("insertAt(15) right should be empty")
	}
	at10 := r.InsertAt(10, 3)
	if at10[0].Start() != 5 || at10[0].End() != 10 {
		t.Fatalf("insertAt(10) left = (%d,%d), want (5,10)", at10[0].Start(), at10[0].End())
	}
	if at10[1].Start() != 10 || at10[1].End() != 13 {
		t.Fatalf("insertAt(10) inserted = (%d,%d), want (10,13)", at10[1].Start(), at10[1].End())
	}
	if at10[2].Start() != 13 || at10[2].End() != 18 {
		t.Fatalf("insertAt(10) right = (%d,%d), want (13,18)", at10[2].Start(), at10[2].End())
	}
	expectPanic(t, func() { r.InsertAt(4, 3) })
	expectPanic(t, func() { r.InsertAt(16, 3) })
}

func TestRangeIntersect(t *testing.T) {
	r1 := mustRange(t, 5, 10)
	r2 := mustRange(t, 3, 6)
	if x := r1.Intersect(r2); x == nil || x.Pos != 5 || x.Length != 4 {
		t.Fatalf("intersect(5,10)-(3,6) = %v, want (5,4)", x)
	}
	if x := r2.Intersect(r1); x == nil || x.Pos != 5 || x.Length != 4 {
		t.Fatalf("intersect(3,6)-(5,10) = %v, want (5,4)", x)
	}
	self := r1.Intersect(r1)
	if self == nil || self.Pos != 5 || self.Length != 10 {
		t.Fatalf("self intersect = %v, want (5,10)", self)
	}
	r3 := mustRange(t, 7, 2)
	if x := r1.Intersect(r3); x == nil || x.Pos != 7 || x.Length != 2 {
		t.Fatalf("nested intersect = %v, want (7,2)", x)
	}
	r4 := mustRange(t, 20, 30)
	if x := r1.Intersect(r4); x != nil {
		t.Fatalf("disconnected intersect = %v, want nil", x)
	}
	if x := r4.Intersect(r1); x != nil {
		t.Fatalf("disconnected intersect = %v, want nil", x)
	}
}
