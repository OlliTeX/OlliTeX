package opmodel

import (
	"reflect"
	"testing"
)

// Mirrors vendor test/unit/comments_list.test.js (overleaf-editor-core).

func wantComment(id string, posLen ...[2]int) map[string]any {
	ranges := make([]any, 0, len(posLen))
	for _, pl := range posLen {
		ranges = append(ranges, map[string]any{"pos": pl[0], "length": pl[1]})
	}
	return map[string]any{"id": id, "ranges": ranges}
}

func assertCLRaw(t *testing.T, cl *CommentList, want ...map[string]any) {
	t.Helper()
	got := cl.ToRaw()
	if len(got) != len(want) {
		t.Fatalf("list length: got %d (full: %v), want %d", len(got), got, len(want))
	}
	for i := range got {
		norm := normalizeCL(got[i])
		if !reflect.DeepEqual(norm, want[i]) {
			t.Errorf("comment[%d]: got %v, want %v", i, norm, want[i])
		}
	}
}

// normalizeCL coerces ToRaw's map[string]int range values into the
// wantComment's map[string]any shape so the DeepEqual compares values,
// not types (the wire JSON round-trips fine either way).
func normalizeCL(raw map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range raw {
		if rm, ok := v.(map[string]int); ok {
			out[k] = map[string]any{"pos": rm["pos"], "length": rm["length"]}
		} else {
			out[k] = v
		}
	}
	if rs, ok := raw["ranges"].([]any); ok {
		norm := make([]any, 0, len(rs))
		for _, r := range rs {
			if rm, ok := r.(map[string]int); ok {
				norm = append(norm, map[string]any{"pos": rm["pos"], "length": rm["length"]})
			} else {
				norm = append(norm, r)
			}
		}
		out["ranges"] = norm
	}
	return out
}

func clFromRangeRaw(t *testing.T, raw ...map[string]any) *CommentList {
	items := make([]any, len(raw))
	for i, r := range raw {
		items[i] = r
	}
	cl, err := CommentListFromRaw(items)
	if err != nil {
		t.Fatalf("CommentListFromRaw: %v", err)
	}
	return cl
}

func TestCommentList_ToRaw_Order(t *testing.T) {
	cl := clFromRangeRaw(t,
		wantComment("comm1", [2]int{5, 10}),
		wantComment("comm2", [2]int{20, 5}),
		wantComment("comm3", [2]int{30, 15}),
	)
	assertCLRaw(t, cl,
		wantComment("comm1", [2]int{5, 10}),
		wantComment("comm2", [2]int{20, 5}),
		wantComment("comm3", [2]int{30, 15}),
	)
}

func TestCommentList_GetCommentByID(t *testing.T) {
	cl := clFromRangeRaw(t,
		wantComment("comm1", [2]int{5, 10}),
		wantComment("comm3", [2]int{30, 15}),
		wantComment("comm2", [2]int{20, 5}),
	)
	c := cl.GetComment("comm2")
	assertBoolT(t, c != nil, true, "found")
	assertEqualT(t, c.ID, "comm2", "id")
	assertEqualT(t, len(c.Ranges), 1, "ranges")
	assertNilComment(t, cl.GetComment("nope"), "absent id nil")
}

func assertNilComment(t *testing.T, c *Comment, msg string) {
	t.Helper()
	assertBoolT(t, c == nil, true, msg)
}

func TestCommentList_AddNewKeepsOrder(t *testing.T) {
	cl := clFromRangeRaw(t,
		wantComment("comm1", [2]int{5, 10}),
		wantComment("comm2", [2]int{20, 5}),
		wantComment("comm3", [2]int{30, 15}),
	)
	c4, _ := NewComment("comm4", []Range{mustRange(t, 40, 10)}, false)
	cl.Add(c4)
	assertCLRaw(t, cl,
		wantComment("comm1", [2]int{5, 10}),
		wantComment("comm2", [2]int{20, 5}),
		wantComment("comm3", [2]int{30, 15}),
		wantComment("comm4", [2]int{40, 10}),
	)
}

func TestCommentList_OverwritePreservesOrder(t *testing.T) {
	cl := clFromRangeRaw(t,
		wantComment("comm1", [2]int{5, 10}),
		wantComment("comm2", [2]int{20, 5}),
		wantComment("comm3", [2]int{30, 15}),
	)
	// Overwrite comm1 (resolved) and comm2 (new range + resolved).
	c1, _ := NewComment("comm1", []Range{mustRange(t, 5, 10)}, true)
	cl.Add(c1)
	c2, _ := NewComment("comm2", []Range{mustRange(t, 40, 10)}, true)
	cl.Add(c2)

	want1 := wantComment("comm1", [2]int{5, 10})
	want1["resolved"] = true
	want2 := wantComment("comm2", [2]int{40, 10})
	want2["resolved"] = true
	assertCLRaw(t, cl, want1, want2, wantComment("comm3", [2]int{30, 15}))
}

func TestCommentList_DeleteExisting(t *testing.T) {
	cl := clFromRangeRaw(t,
		wantComment("comm1", [2]int{5, 10}),
		wantComment("comm2", [2]int{20, 5}),
		wantComment("comm3", [2]int{30, 15}),
	)
	assertBoolT(t, cl.Delete("comm3"), true, "delete returns true")
	assertCLRaw(t, cl,
		wantComment("comm1", [2]int{5, 10}),
		wantComment("comm2", [2]int{20, 5}),
	)
}

func TestCommentList_DeleteAbsentNoOp(t *testing.T) {
	cl := clFromRangeRaw(t,
		wantComment("comm1", [2]int{5, 10}),
		wantComment("comm2", [2]int{20, 5}),
		wantComment("comm3", [2]int{30, 15}),
	)
	assertBoolT(t, cl.Delete("comm5"), false, "absent returns false")
	assertCLRaw(t, cl,
		wantComment("comm1", [2]int{5, 10}),
		wantComment("comm2", [2]int{20, 5}),
		wantComment("comm3", [2]int{30, 15}),
	)
}

func TestCommentList_ApplyInsert_ExtendLeft(t *testing.T) {
	// Vendor: "should expand comment on the left".
	cl := clFromRangeRaw(t,
		wantComment("comm1", [2]int{5, 10}),
		wantComment("comm2", [2]int{15, 10}),
	)
	cl.ApplyInsert(mustRange(t, 15, 5), []string{"comm1"})
	assertCLRaw(t, cl,
		wantComment("comm1", [2]int{5, 15}),
		wantComment("comm2", [2]int{20, 10}),
	)
}

func TestCommentList_ApplyInsert_ExtendRight(t *testing.T) {
	// Vendor: "should expand comment on the right".
	cl := clFromRangeRaw(t,
		wantComment("comm1", [2]int{5, 10}),
		wantComment("comm2", [2]int{15, 10}),
	)
	cl.ApplyInsert(mustRange(t, 15, 5), []string{"comm2"})
	assertCLRaw(t, cl,
		wantComment("comm1", [2]int{5, 10}),
		wantComment("comm2", [2]int{15, 15}),
	)
}

func TestCommentList_ApplyDelete_SpansTwoComments(t *testing.T) {
	// Vendor: "should delete a text overlapping two comments". 5-14 and 15-24.
	cl := clFromRangeRaw(t,
		wantComment("comm1", [2]int{5, 10}),
		wantComment("comm2", [2]int{15, 10}),
	)
	cl.ApplyDelete(mustRange(t, 10, 10))
	assertCLRaw(t, cl,
		wantComment("comm1", [2]int{5, 5}),
		wantComment("comm2", [2]int{10, 5}),
	)
}

func TestCommentList_ApplyInsert_InsideExtend(t *testing.T) {
	// Vendor: "expands comments inside inserted text".
	cl := clFromRangeRaw(t,
		wantComment("comm1", [2]int{5, 10}),
		wantComment("comm2", [2]int{20, 5}),
		wantComment("comm3", [2]int{30, 15}),
	)
	cl.ApplyInsert(mustRange(t, 7, 5), []string{"comm1"})
	assertCLRaw(t, cl,
		wantComment("comm1", [2]int{5, 15}),
		wantComment("comm2", [2]int{25, 5}),
		wantComment("comm3", [2]int{35, 15}),
	)
}

func TestCommentList_ApplyInsert_OverlappingWithoutID(t *testing.T) {
	// Vendor: "should insert an overlapping comment without overlapped
	// comment id". comm2 is tagged, its own range moves AND the new range
	// (7,5) is added; comm1 splits.
	cl := clFromRangeRaw(t,
		wantComment("comm1", [2]int{5, 10}),
		wantComment("comm2", [2]int{20, 5}),
		wantComment("comm3", [2]int{30, 15}),
	)
	cl.ApplyInsert(mustRange(t, 7, 5), []string{"comm2"})
	assertCLRaw(t, cl,
		wantComment("comm1", [2]int{5, 2}, [2]int{12, 8}),
		wantComment("comm2", [2]int{7, 5}, [2]int{25, 5}),
		wantComment("comm3", [2]int{35, 15}),
	)
}

func TestCommentList_ApplyInsert_OverlappingWithID(t *testing.T) {
	// Vendor: "should insert an overlapping comment with overlapped
	// comment id". comm1 (5-20) contains the insert -> extends to 5-25;
	// comm2 tagged -> its range moves to 25 AND new range at 7 is added.
	cl := clFromRangeRaw(t,
		wantComment("comm1", [2]int{5, 15}),
		wantComment("comm2", [2]int{20, 5}),
		wantComment("comm3", [2]int{30, 15}),
	)
	cl.ApplyInsert(mustRange(t, 7, 5), []string{"comm1", "comm2"})
	assertCLRaw(t, cl,
		wantComment("comm1", [2]int{5, 20}),
		wantComment("comm2", [2]int{7, 5}, [2]int{25, 5}),
		wantComment("comm3", [2]int{35, 15}),
	)
}

func TestCommentList_ApplyInsert_MovesComments(t *testing.T) {
	// Vendor: "moves comments after inserted text".
	cl := clFromRangeRaw(t,
		wantComment("comm1", [2]int{5, 10}),
		wantComment("comm2", [2]int{20, 5}),
		wantComment("comm3", [2]int{30, 15}),
	)
	cl.ApplyInsert(mustRange(t, 16, 5), nil)
	assertCLRaw(t, cl,
		wantComment("comm1", [2]int{5, 10}),
		wantComment("comm2", [2]int{25, 5}),
		wantComment("comm3", [2]int{35, 15}),
	)
}

func TestCommentList_ApplyInsert_OutsideUntouched(t *testing.T) {
	// Vendor: "does not affect comments outside of inserted text".
	cl := clFromRangeRaw(t,
		wantComment("comm1", [2]int{5, 10}),
		wantComment("comm2", [2]int{20, 5}),
		wantComment("comm3", [2]int{30, 15}),
	)
	cl.ApplyInsert(mustRange(t, 50, 5), nil)
	assertCLRaw(t, cl,
		wantComment("comm1", [2]int{5, 10}),
		wantComment("comm2", [2]int{20, 5}),
		wantComment("comm3", [2]int{30, 15}),
	)
}

func TestCommentList_ApplyDelete_MovesComments(t *testing.T) {
	// Vendor: "should move comments if delete happened before it".
	cl := clFromRangeRaw(t,
		wantComment("comm1", [2]int{5, 10}),
		wantComment("comm2", [2]int{20, 5}),
		wantComment("comm3", [2]int{30, 15}),
	)
	cl.ApplyDelete(mustRange(t, 0, 4))
	assertCLRaw(t, cl,
		wantComment("comm1", [2]int{1, 10}),
		wantComment("comm2", [2]int{16, 5}),
		wantComment("comm3", [2]int{26, 15}),
	)
}

func TestCommentList_ApplyDelete_IntersectionLeft(t *testing.T) {
	// Vendor: "should delete intersection from the left".
	// Vendor subtract re-anchors at the deleted range's pos (quirk,
	// mirrored): 5-14 minus 0-6 -> [{0,9}] per vendor oracle.
	cl := clFromRangeRaw(t, wantComment("comm1", [2]int{5, 10}))
	cl.ApplyDelete(mustRange(t, 0, 6))
	assertCLRaw(t, cl, wantComment("comm1", [2]int{0, 9}))
}

func TestCommentList_ApplyDelete_IntersectionRight(t *testing.T) {
	// Vendor: "should delete intersection from the right".
	cl := clFromRangeRaw(t, wantComment("comm1", [2]int{5, 10}))
	cl.ApplyDelete(mustRange(t, 7, 10))
	assertCLRaw(t, cl, wantComment("comm1", [2]int{5, 2}))
}

func TestCommentList_ApplyDelete_IntersectionMiddle(t *testing.T) {
	// Vendor: "should delete intersection in the middle".
	cl := clFromRangeRaw(t, wantComment("comm1", [2]int{5, 10}))
	cl.ApplyDelete(mustRange(t, 6, 2))
	assertCLRaw(t, cl, wantComment("comm1", [2]int{5, 8}))
}

func TestCommentList_ApplyDelete_EmptyRanges(t *testing.T) {
	// Vendor: "should leave comment without ranges". comm2 is fully covered
	// by the delete and survives with no ranges.
	cl := clFromRangeRaw(t,
		wantComment("comm1", [2]int{5, 10}),
		wantComment("comm2", [2]int{20, 5}),
		wantComment("comm3", [2]int{30, 15}),
	)
	cl.ApplyDelete(mustRange(t, 19, 10))
	assertCLRaw(t, cl,
		wantComment("comm1", [2]int{5, 10}),
		wantComment("comm2"),
		wantComment("comm3", [2]int{20, 15}),
	)
}
