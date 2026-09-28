package opmodel

import "testing"

// Mirrors vendor test/unit/comment.test.js (overleaf-editor-core).

func mustRange(t *testing.T, pos, length int) Range {
	r, err := NewRange(pos, length)
	if err != nil {
		t.Fatalf("NewRange(%d,%d): %v", pos, length, err)
	}
	return r
}

// rangeIs asserts a range has the given {pos, length}.
func rangeIs(t *testing.T, r Range, pos, length int, msg string) {
	t.Helper()
	assertEqualT(t, r.Pos, pos, msg+" pos")
	assertEqualT(t, r.Length, length, msg+" length")
}

func TestComment_ApplyInsert_MovesRight(t *testing.T) {
	comment, _ := NewComment("c1", []Range{mustRange(t, 5, 10)}, false)
	res := comment.ApplyInsert(3, 5, false)
	assertEqualT(t, len(res.Ranges), 1, "one range")
	rangeIs(t, res.Ranges[0], 10, 10, "moved")
}

func TestComment_ApplyInsert_Before(t *testing.T) {
	comment, _ := NewComment("c1", []Range{mustRange(t, 5, 10)}, false)
	expect := comment.ApplyInsert(4, 1, false)
	rangeIs(t, expect.Ranges[0], 6, 10, "insert before")
}

func TestComment_ApplyInsert_AtEdge_NoExtend(t *testing.T) {
	comment, _ := NewComment("c1", []Range{mustRange(t, 5, 10)}, false)
	expect := comment.ApplyInsert(5, 1, false)
	rangeIs(t, expect.Ranges[0], 6, 10, "edge no-extend")
}

func TestComment_ApplyInsert_AtEdge_Extend(t *testing.T) {
	comment, _ := NewComment("c1", []Range{mustRange(t, 5, 10)}, false)
	expect := comment.ApplyInsert(5, 1, true)
	rangeIs(t, expect.Ranges[0], 5, 11, "edge extend")
}

func TestComment_ApplyInsert_InsideExtend(t *testing.T) {
	comment, _ := NewComment("c1", []Range{mustRange(t, 5, 10)}, false)
	expect := comment.ApplyInsert(6, 1, true)
	rangeIs(t, expect.Ranges[0], 5, 11, "inside extend")
}

func TestComment_ApplyInsert_Split(t *testing.T) {
	comment, _ := NewComment("c1", []Range{mustRange(t, 5, 10)}, false)
	expect := comment.ApplyInsert(6, 10, false)
	assertEqualT(t, len(expect.Ranges), 2, "two ranges")
	rangeIs(t, expect.Ranges[0], 5, 1, "left")
	rangeIs(t, expect.Ranges[1], 16, 9, "right")
}

func TestComment_ApplyInsert_FarRightNoExtend(t *testing.T) {
	comment, _ := NewComment("c1", []Range{mustRange(t, 5, 10)}, false)
	expect := comment.ApplyInsert(14, 10, false)
	// The vendor oracle expects the ORIGINAL range back here: the insert at
	// 14 falls between the 5-15 range and its end (14 < 15), the Go port
	// matches vendor's branch order (startIsAfter(14) false, no range
	// covering cursor 14... actually containsCursor(14) is true for 5-15,
	// so it splits 5-14/24-15. Vendor expects [{5,9},{24,1}]? See comment:
	// vendor expectation [{5,9 },{24,1}] is NOT the same — vendor test says
	// "should insert the range if expandComment is false" expects
	// [[5,9],[24,1]]. The Go port (vendor-ported) produces that.
	assertEqualT(t, len(expect.Ranges), 2, "two ranges")
	rangeIs(t, expect.Ranges[0], 5, 9, "left shrunk")
	rangeIs(t, expect.Ranges[1], 24, 1, "right shifted")
}

func TestComment_ApplyInsert_AtStartNoExtend(t *testing.T) {
	comment, _ := NewComment("c1", []Range{mustRange(t, 5, 10)}, false)
	expect := comment.ApplyInsert(5, 10, false)
	rangeIs(t, expect.Ranges[0], 15, 10, "moved by 10")
}

func TestComment_ApplyInsert_AtEndNoExtendKeeps(t *testing.T) {
	comment, _ := NewComment("c1", []Range{mustRange(t, 5, 10)}, false)
	expect := comment.ApplyInsert(15, 10, false)
	rangeIs(t, expect.Ranges[0], 5, 10, "unchanged")
}

func TestComment_ApplyInsert_AtEndExtend(t *testing.T) {
	comment, _ := NewComment("c1", []Range{mustRange(t, 5, 10)}, false)
	expect := comment.ApplyInsert(15, 10, true)
	rangeIs(t, expect.Ranges[0], 5, 20, "extended to end")
}

func TestComment_ApplyDelete_Before(t *testing.T) {
	comment, _ := NewComment("c1", []Range{mustRange(t, 5, 10)}, false)
	expect := comment.ApplyDelete(mustRange(t, 3, 5))
	rangeIs(t, expect.Ranges[0], 3, 7, "shifted and shrunk... (vendor: [{3,7}])")
}

func TestComment_ApplyDelete_Merges(t *testing.T) {
	comment, _ := NewComment("c1", []Range{mustRange(t, 5, 10), mustRange(t, 20, 10)}, false)
	expect := comment.ApplyDelete(mustRange(t, 7, 18))
	assertEqualT(t, len(expect.Ranges), 1, "merged to one range")
	rangeIs(t, expect.Ranges[0], 5, 7, "merged range")
}

func TestComment_MergesOverlapping(t *testing.T) {
	comment, _ := NewComment("c1", []Range{mustRange(t, 5, 10), mustRange(t, 15, 20), mustRange(t, 50, 10)}, false)
	assertEqualT(t, len(comment.Ranges), 2, "two ranges")
	rangeIs(t, comment.Ranges[0], 5, 30, "merged")
	rangeIs(t, comment.Ranges[1], 50, 10, "far")
}

func TestComment_MergesUnsorted(t *testing.T) {
	comment, _ := NewComment("c1", []Range{mustRange(t, 15, 20), mustRange(t, 50, 10), mustRange(t, 5, 10)}, false)
	assertEqualT(t, len(comment.Ranges), 2, "two ranges sorted+merged")
	rangeIs(t, comment.Ranges[0], 5, 30, "merged")
	rangeIs(t, comment.Ranges[1], 50, 10, "far")
}

func TestComment_OverlappingRangesError(t *testing.T) {
	if _, err := NewComment("c1", []Range{mustRange(t, 5, 10), mustRange(t, 10, 5), mustRange(t, 50, 10)}, false); err == nil {
		t.Fatal("overlapping ranges did not error")
	}
}

func TestComment_JoinsTouchingRanges(t *testing.T) {
	comment, _ := NewComment("c1", []Range{mustRange(t, 5, 10), mustRange(t, 15, 5), mustRange(t, 50, 10)}, false)
	assertEqualT(t, len(comment.Ranges), 2, "touching merge")
	rangeIs(t, comment.Ranges[0], 5, 15, "joined")
	rangeIs(t, comment.Ranges[1], 50, 10, "far")
}

func TestComment_IsEmpty(t *testing.T) {
	c, _ := NewComment("c1", nil, false)
	assertBoolT(t, c.IsEmpty(), true, "no ranges empty")
	c2, _ := NewComment("c2", []Range{mustRange(t, 1, 2)}, false)
	assertBoolT(t, c2.IsEmpty(), false, "has range not empty")
}

func TestComment_FromRaw(t *testing.T) {
	raw := map[string]any{
		"id":     "c1",
		"ranges": []any{map[string]any{"pos": 5, "length": 10}},
	}
	c, err := CommentFromRaw(raw)
	if err != nil {
		t.Fatalf("fromRaw: %v", err)
	}
	assertEqualT(t, c.ID, "c1", "id")
	assertEqualT(t, len(c.Ranges), 1, "ranges")
	rangeIs(t, c.Ranges[0], 5, 10, "range raw")

	bad := map[string]any{"id": "c2", "ranges": []any{map[string]any{"pos": -1, "length": 1}}}
	if _, err := CommentFromRaw(bad); err == nil {
		t.Fatal("negative range did not error")
	}
}

func TestComment_ToRaw_Resolved(t *testing.T) {
	c, _ := NewComment("c1", []Range{mustRange(t, 5, 10)}, true)
	raw := c.ToRaw()
	assertEqualT(t, raw["id"], "c1", "id")
	assertEqualT(t, raw["resolved"], true, "resolved true")
	c2, _ := NewComment("c2", nil, false)
	if _, ok := c2.ToRaw()["resolved"]; ok {
		t.Fatal("resolved absent when false")
	}
}

func TestComment_ApplyTextOperation(t *testing.T) {
	// op: retain 4, insert 'bar' tagged comment1, remove 2, retain 2
	op := NewTextOperation().Retain(4, nil).Insert("bar", nil, []string{"c1"}).Remove(2).Retain(2, nil)
	c, _ := NewComment("c1", []Range{mustRange(t, 5, 5)}, false)
	expect := c.ApplyTextOperation(op, "c1")
	assertEqualT(t, len(expect.Ranges), 1, "one range")
	rangeIs(t, expect.Ranges[0], 4, 7, "insert tagged c1 extends, delete shrinks")

	op2 := NewTextOperation().Retain(2, nil).Insert("xy", nil, nil).Retain(2, nil)
	c2, _ := NewComment("c2", []Range{mustRange(t, 1, 5)}, false)
	expect2 := c2.ApplyTextOperation(op2, "c9")
	assertEqualT(t, len(expect2.Ranges), 2, "unrelated insert splits")
	rangeIs(t, expect2.Ranges[0], 1, 1, "left part")
	rangeIs(t, expect2.Ranges[1], 4, 4, "right part shifted")
}
