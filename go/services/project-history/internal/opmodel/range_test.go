package opmodel

import (
	"testing"
)

// Mirrors vendor test/unit/range.test.js (overleaf-editor-core).

func TestRange_Construct(t *testing.T) {
	rng, err := NewRange(5, 10)
	if err != nil {
		t.Fatalf("NewRange(5,10): %v", err)
	}
	assertEqualT(t, rng.Start(), 5, "start")
	assertEqualT(t, rng.End(), 15, "end")
}

func TestRange_FromRaw(t *testing.T) {
	r, err := FromRawRange(map[string]int{"pos": 5, "length": 10})
	if err != nil {
		t.Fatalf("fromRaw: %v", err)
	}
	assertEqualT(t, r.Start(), 5, "fromRaw start")
	assertEqualT(t, r.End(), 15, "fromRaw end")
}

func TestRange_ToRaw(t *testing.T) {
	rng, _ := NewRange(5, 10)
	assertMapT(t, rng.ToRaw(), map[string]int{"pos": 5, "length": 10}, "toRaw")
}

func TestRange_IsEmpty(t *testing.T) {
	r1, _ := NewRange(5, 10)
	assertBoolT(t, r1.IsEmpty(), false, "non-empty isEmpty")
	r0, _ := NewRange(5, 0)
	assertBoolT(t, r0.IsEmpty(), true, "zero-length isEmpty")
}

func TestRange_NegativeArgs(t *testing.T) {
	if _, err := NewRange(-1, 10); err == nil {
		t.Fatal("negative pos did not error")
	}
	if _, err := NewRange(0, -2); err == nil {
		t.Fatal("negative length did not error")
	}
}

func TestRange_Overlaps(t *testing.T) {
	r13, _ := NewRange(1, 3)
	r10, _ := NewRange(10, 3)
	r4, _ := NewRange(4, 3)
	r2, _ := NewRange(2, 3)

	assertBoolT(t, r13.Overlaps(r13), true, "same ranges overlap")
	assertBoolT(t, r10.Overlaps(r13), false, "far ranges do not overlap")
	assertBoolT(t, r4.Overlaps(r13), false, "touching ranges do not overlap")
	assertBoolT(t, r13.Overlaps(r2), true, "overlapping")
	assertBoolT(t, r2.Overlaps(r13), true, "overlapping (sym)")
}

func TestRange_Touches(t *testing.T) {
	r13, _ := NewRange(1, 3)
	r4, _ := NewRange(4, 2)
	r5, _ := NewRange(5, 2)
	r30, _ := NewRange(3, 2)

	assertBoolT(t, r13.Touches(r13), false, "same does not touch")
	assertBoolT(t, r13.Touches(r4), true, "touch at one point")
	assertBoolT(t, r4.Touches(r13), true, "touch (sym)")
	assertBoolT(t, r13.Touches(r5), false, "apart does not touch")
	assertBoolT(t, r13.Touches(r30), false, "overlap does not touch")
}

func TestRange_Contains(t *testing.T) {
	f13, _ := NewRange(4, 10)  // [4..14]
	f14, _ := NewRange(4, 11)  // [4..15]
	f15, _ := NewRange(4, 12)  // [4..16]
	f513, _ := NewRange(5, 9)  // [5..14]
	f514, _ := NewRange(5, 10) // [5..15]
	f515, _ := NewRange(5, 11) // [5..16]
	big, _ := NewRange(0, 100)
	small, _ := NewRange(0, 3) // [0..3]

	assertBoolT(t, small.Contains(small), true, "self")
	assertBoolT(t, small.Contains(f13), false, "out of range")
	assertBoolT(t, small.Contains(big), false, "bigger")
	assertBoolT(t, f13.Contains(small), false, "small outside")
	assertBoolT(t, f13.Contains(f13), true, "self2")
	assertBoolT(t, f13.Contains(f513), true, "inner")
	assertBoolT(t, f13.Contains(f514), false, "inner end outside")
	assertBoolT(t, f14.Contains(f13), true, "covers")
	assertBoolT(t, f14.Contains(f14), true, "self3")
	assertBoolT(t, f14.Contains(f515), false, "inner end outside3")
	assertBoolT(t, f15.Contains(f13), true, "covers2")
	assertBoolT(t, f15.Contains(f14), true, "covers2b")
	assertBoolT(t, f15.Contains(f15), true, "self4")
	assertBoolT(t, f513.Contains(f513), true, "self5")
	assertBoolT(t, f513.Contains(f514), false, "end outside")
	assertBoolT(t, f514.Contains(f515), false, "end outside2")
	assertBoolT(t, f515.Contains(f513), true, "covers3")
	assertBoolT(t, f15.Contains(f515), true, "covers4 (from4to15 covers from5to15 per vendor)")
	assertBoolT(t, f515.Contains(big), false, "bigger not contained")
	assertBoolT(t, big.Contains(small), true, "big contains small")
	assertBoolT(t, big.Contains(f13), true, "big contains f13")
	assertBoolT(t, big.Contains(f515), true, "big contains f515")
	assertBoolT(t, big.Contains(big), true, "big self")
}

func TestRange_ContainsCursor(t *testing.T) {
	r, _ := NewRange(5, 10)
	assertBoolT(t, r.ContainsCursor(4), false, "cursor before")
	assertBoolT(t, r.ContainsCursor(5), true, "cursor at start")
	assertBoolT(t, r.ContainsCursor(6), true, "cursor inside")
	assertBoolT(t, r.ContainsCursor(14), true, "cursor at last unit")
	assertBoolT(t, r.ContainsCursor(15), true, "cursor at end")
	assertBoolT(t, r.ContainsCursor(16), false, "cursor after")
}

func TestRange_Subtract(t *testing.T) {
	// no subtraction (no overlap)
	a, _ := NewRange(1, 6)
	b, _ := NewRange(0, 1)
	s := a.Subtract(b)
	assertEqualT(t, s.Start(), 1, "no-sub start")
	assertEqualT(t, s.Length, 6, "no-sub length")

	// subtract from the left
	big, _ := NewRange(5, 15)
	left, _ := NewRange(15, 10)
	s2 := left.Subtract(big)
	assertEqualT(t, s2.Start(), 5, "sub-left start")
	assertEqualT(t, s2.End(), 10, "sub-left end")

	// subtract from the right
	a10, _ := NewRange(10, 15)
	a5, _ := NewRange(5, 15)
	s3 := a5.Subtract(a10)
	assertEqualT(t, s3.Start(), 5, "sub-right start")
	assertEqualT(t, s3.End(), 10, "sub-right end")

	// subtract from the middle (contained)
	mid, _ := NewRange(10, 5)
	s4 := big.Subtract(mid)
	assertEqualT(t, s4.Start(), 5, "sub-mid start")
	assertEqualT(t, s4.End(), 15, "sub-mid end")

	// delete entire range (contained)
	huge, _ := NewRange(0, 100)
	s5 := big.Subtract(huge)
	assertEqualT(t, s5.Start(), 5, "delete-entire start")
	assertEqualT(t, s5.Length, 0, "delete-entire length")

	// no overlap: both directions unchanged
	f14, _ := NewRange(5, 10)
	f29, _ := NewRange(20, 10)
	s6 := f14.Subtract(f29)
	assertMapT(t, s6.ToRaw(), f14.ToRaw(), "no-overlap a")
	s7 := f29.Subtract(f14)
	assertMapT(t, s7.ToRaw(), f29.ToRaw(), "no-overlap b")
}

func TestRange_Merge(t *testing.T) {
	r5, _ := NewRange(5, 10)
	r10, _ := NewRange(10, 10)
	assertBoolT(t, r5.CanMerge(r10), true, "canMerge at end")
	m, err := r5.Merge(r10)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	assertEqualT(t, m.Start(), 5, "merge start")
	assertEqualT(t, m.End(), 20, "merge end")

	r0, _ := NewRange(0, 10)
	assertBoolT(t, r5.CanMerge(r0), true, "canMerge at start")
	m2, err := r5.Merge(r0)
	if err != nil {
		t.Fatalf("merge2: %v", err)
	}
	assertEqualT(t, m2.Start(), 0, "merge2 start")
	assertEqualT(t, m2.End(), 15, "merge2 end")

	r019, _ := NewRange(0, 20)
	assertBoolT(t, r5.CanMerge(r019), true, "canMerge covered")
	m3, err := r5.Merge(r019)
	if err != nil {
		t.Fatalf("merge3: %v", err)
	}
	assertMapT(t, m3.ToRaw(), r019.ToRaw(), "covered merge")

	assertBoolT(t, r019.CanMerge(r5), true, "canMerge covered2")
	m4, err := r019.Merge(r5)
	if err != nil {
		t.Fatalf("merge4: %v", err)
	}
	assertEqualT(t, m4.Start(), 0, "covered2 start")
	assertEqualT(t, m4.End(), 20, "covered2 end")

	r20, _ := NewRange(20, 10)
	assertBoolT(t, r5.CanMerge(r20), false, "no canMerge")
	if _, err := r5.Merge(r20); err == nil {
		t.Fatal("merge without overlap should error")
	}
}

func TestRange_StartsAfter(t *testing.T) {
	r0, _ := NewRange(0, 5)
	r1, _ := NewRange(1, 5)
	r5, _ := NewRange(5, 5)
	r6, _ := NewRange(6, 5)
	r10, _ := NewRange(10, 5)

	assertBoolT(t, r0.StartsAfter(r0), false, "self0")
	assertBoolT(t, r0.StartsAfter(r1), false, "0 after1")
	assertBoolT(t, r5.StartsAfter(r0), true, "5 after0")
	assertBoolT(t, r5.StartsAfter(r1), false, "5 after1")
	assertBoolT(t, r5.StartsAfter(r5), false, "self5")
	assertBoolT(t, r6.StartsAfter(r0), true, "6 after0")
	assertBoolT(t, r6.StartsAfter(r1), true, "6 after1")
	assertBoolT(t, r6.StartsAfter(r5), false, "6 after5")
	assertBoolT(t, r10.StartsAfter(r0), true, "10 after0")
	assertBoolT(t, r10.StartsAfter(r6), false, "10 after6")
	assertBoolT(t, r10.StartsAfter(r10), false, "self10")
}

func TestRange_StartIsAfter(t *testing.T) {
	r, _ := NewRange(5, 10)
	assertBoolT(t, r.StartIsAfter(3), true, "3")
	assertBoolT(t, r.StartIsAfter(4), true, "4")
	assertBoolT(t, r.StartIsAfter(5), false, "at start not after")
	assertBoolT(t, r.StartIsAfter(15), false, "at end not after")
	assertBoolT(t, r.StartIsAfter(16), false, "after end not after")
}

func TestRange_ExtendBy(t *testing.T) {
	r, _ := NewRange(5, 10)
	e := r.ExtendBy(3)
	assertEqualT(t, e.Length, 13, "extend length")
	assertEqualT(t, e.Start(), 5, "extend start")
	assertEqualT(t, e.End(), 18, "extend end")
}

func TestRange_ShrinkBy(t *testing.T) {
	r, _ := NewRange(5, 10)
	s, err := r.ShrinkBy(3)
	if err != nil {
		t.Fatalf("shrink: %v", err)
	}
	assertEqualT(t, s.Length, 7, "shrink length")
	assertEqualT(t, s.Start(), 5, "shrink start")
	assertEqualT(t, s.End(), 12, "shrink end")
	if _, err := r.ShrinkBy(11); err == nil {
		t.Fatal("shrink beyond length did not error")
	}
}

func TestRange_MoveBy(t *testing.T) {
	r, _ := NewRange(5, 10)
	m := r.MoveBy(3)
	assertEqualT(t, m.Length, 10, "move length")
	assertEqualT(t, m.Start(), 8, "move start")
	assertEqualT(t, m.End(), 18, "move end")
}

func TestRange_SplitAt(t *testing.T) {
	r, _ := NewRange(5, 10)

	left, right, err := r.SplitAt(5)
	if err != nil {
		t.Fatalf("splitAt start: %v", err)
	}
	assertBoolT(t, left.IsEmpty(), true, "left empty at start")
	assertEqualT(t, right.Start(), 5, "right start")
	assertEqualT(t, right.End(), 15, "right end")

	if _, _, err := r.SplitAt(4); err == nil {
		t.Fatal("splitAt before start did not error")
	}

	left2, right2, err := r.SplitAt(14)
	if err != nil {
		t.Fatalf("splitAt 14: %v", err)
	}
	assertEqualT(t, left2.Start(), 5, "14 left start")
	assertEqualT(t, left2.End(), 14, "14 left end")
	assertEqualT(t, right2.Start(), 14, "14 right start")
	assertEqualT(t, right2.End(), 15, "14 right end")

	if _, _, err := r.SplitAt(16); err == nil {
		t.Fatal("splitAt after end did not error")
	}

	left3, right3, err := r.SplitAt(15)
	if err != nil {
		t.Fatalf("splitAt end: %v", err)
	}
	assertEqualT(t, left3.Start(), 5, "end left start")
	assertEqualT(t, left3.End(), 15, "end left end")
	assertEqualT(t, right3.Start(), 15, "end right start")
	assertEqualT(t, right3.End(), 15, "end right end")

	left4, right4, err := r.SplitAt(10)
	if err != nil {
		t.Fatalf("splitAt mid: %v", err)
	}
	assertEqualT(t, left4.Start(), 5, "mid left start")
	assertEqualT(t, left4.End(), 10, "mid left end")
	assertEqualT(t, right4.Start(), 10, "mid right start")
	assertEqualT(t, right4.End(), 15, "mid right end")
}

func TestRange_InsertAt(t *testing.T) {
	r, _ := NewRange(5, 10)

	up, ins, after, err := r.InsertAt(5, 3)
	if err != nil {
		t.Fatalf("insertAt start: %v", err)
	}
	assertBoolT(t, up.IsEmpty(), true, "insert start up empty")
	assertEqualT(t, ins.Start(), 5, "ins start")
	assertEqualT(t, ins.End(), 8, "ins end")
	assertEqualT(t, after.Start(), 8, "after start")
	assertEqualT(t, after.End(), 18, "after end")

	up2, ins2, after2, err := r.InsertAt(15, 3)
	if err != nil {
		t.Fatalf("insertAt end: %v", err)
	}
	assertEqualT(t, up2.Start(), 5, "end up start")
	assertEqualT(t, up2.End(), 15, "end up end")
	assertEqualT(t, ins2.Start(), 15, "end ins start")
	assertEqualT(t, ins2.End(), 18, "end ins end")
	assertBoolT(t, after2.IsEmpty(), true, "end after empty")

	up3, ins3, after3, err := r.InsertAt(10, 3)
	if err != nil {
		t.Fatalf("insertAt mid: %v", err)
	}
	assertEqualT(t, up3.Start(), 5, "mid up start")
	assertEqualT(t, up3.End(), 10, "mid up end")
	assertEqualT(t, ins3.Start(), 10, "mid ins start")
	assertEqualT(t, ins3.End(), 13, "mid ins end")
	assertEqualT(t, after3.Start(), 13, "mid after start")
	assertEqualT(t, after3.End(), 18, "mid after end")

	if _, _, _, err := r.InsertAt(4, 3); err == nil {
		t.Fatal("insertAt before did not error")
	}
	if _, _, _, err := r.InsertAt(16, 3); err == nil {
		t.Fatal("insertAt after did not error")
	}
}

func TestRange_Intersect(t *testing.T) {
	r1, _ := NewRange(5, 10)
	r2, _ := NewRange(3, 6)
	i1p := r1.Intersect(r2)
	if i1p == nil {
		t.Fatal("overlapping should intersect")
	}
	assertEqualT(t, i1p.Pos, 5, "intersect pos")
	assertEqualT(t, i1p.Length, 4, "intersect length")
	i2 := r2.Intersect(r1)
	assertEqualT(t, i2.Pos, 5, "intersect2 pos")
	assertEqualT(t, i2.Length, 4, "intersect2 length")

	iself := r1.Intersect(r1)
	if iself == nil {
		t.Fatal("self intersect nil")
	}
	assertEqualT(t, iself.Pos, 5, "self pos")
	assertEqualT(t, iself.Length, 10, "self length")

	rn, _ := NewRange(7, 2)
	in := r1.Intersect(rn)
	if in == nil {
		t.Fatal("nested intersect nil")
	}
	assertEqualT(t, in.Pos, 7, "nested pos")
	assertEqualT(t, in.Length, 2, "nested length")
	in2 := rn.Intersect(r1)
	assertEqualT(t, in2.Pos, 7, "nested2 pos")
	assertEqualT(t, in2.Length, 2, "nested2 length")

	r30, _ := NewRange(20, 30)
	assertEqualT(t, r1.Intersect(r30), nil, "disconnected nil")
	assertEqualT(t, r30.Intersect(r1), nil, "disconnected nil2")
}

func TestRange_Equals(t *testing.T) {
	r1, _ := NewRange(5, 10)
	r2, _ := NewRange(5, 10)
	r3, _ := NewRange(5, 9)
	assertBoolT(t, r1.Equals(r2), true, "equal")
	assertBoolT(t, r1.Equals(r3), false, "different length not equal")
	r4, _ := NewRange(5, 5)
	assertBoolT(t, r1.Equals(r4), false, "diff len not equal")
	r5, _ := NewRange(6, 10)
	assertBoolT(t, r1.Equals(r5), false, "diff pos not equal")
}
