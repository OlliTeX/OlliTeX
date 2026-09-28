package syncmanager

import (
	"testing"
)

// --- Range (vendor editor-core Range) ---------------------------------------

func TestRangeMethods(t *testing.T) {
	r := Range{Pos: 2, Length: 4}
	if r.End() != 6 || r.Start() != 2 {
		t.Fatalf("end/start")
	}
	if !r.StartIsAfter(1) || r.StartIsAfter(2) {
		t.Fatalf("startIsAfter")
	}
	farRange := Range{Pos: 6, Length: 1}
	if !farRange.StartsAfter(r) {
		t.Fatalf("startsAfter")
	}
	emptyRange := Range{Pos: 1, Length: 0}
	if !emptyRange.IsEmpty() || r.IsEmpty() {
		t.Fatalf("isEmpty")
	}
	if !r.ContainsCursor(3) || !r.ContainsCursor(2) || !r.ContainsCursor(6) || r.ContainsCursor(7) {
		t.Fatalf("containsCursor")
	}
	if !r.Contains(Range{Pos: 3, Length: 1}) || r.Contains(Range{Pos: 1, Length: 2}) {
		t.Fatalf("contains")
	}
	if !r.Overlaps(Range{Pos: 5, Length: 2}) || r.Overlaps(Range{Pos: 6, Length: 1}) {
		t.Fatalf("overlaps")
	}
	if !r.Touches(Range{Pos: 6, Length: 2}) || !r.Touches(Range{Pos: 0, Length: 2}) || r.Touches(Range{Pos: 0, Length: 1}) {
		t.Fatalf("touches")
	}
	if got := r.MoveBy(3); got.Pos != 5 || got.Length != 4 {
		t.Fatalf("moveBy: %v", got)
	}
	if got := r.ExtendBy(2); got.Pos != 2 || got.Length != 6 {
		t.Fatalf("extendBy: %v", got)
	}
	// vendor r={2,4} insertAt(4,5): upTo={2,2}, inserted={4,5},
	// after={9, this.length - upTo.Length = 4-2 = 2} (vendor quirk —
	// faithful).
	up, ins, after := r.InsertAt(4, 5)
	if up.Pos != 2 || up.Length != 2 {
		t.Fatalf("insertAt up: %v", up)
	}
	if ins.Pos != 4 || ins.Length != 5 {
		t.Fatalf("insertAt ins: %v", ins)
	}
	if after.Pos != 9 || after.Length != 2 {
		t.Fatalf("insertAt after: %v", after)
	}
}

func TestRangeSubtract(t *testing.T) {
	r := Range{Pos: 0, Length: 10}
	// vendor quirk (preserve faithfully): this={0,10}, deleted={5,3} →
	// else-branch: intersected = this.end - range.start = 5 →
	// Range(this.pos, this.length - 5) = {0,5}
	if got := r.Subtract(Range{Pos: 5, Length: 3}); got.Pos != 0 || got.Length != 5 {
		t.Fatalf("right overlap (vendor quirk): %v", got)
	}
	// vendor quirk: deleted={-2,3} → range.start < this.start →
	// intersected = range.end - this.start = 1 → Range(range.pos, 10-1) = {-2,9}
	if got := r.Subtract(Range{Pos: -2, Length: 3}); got.Pos != -2 || got.Length != 9 {
		t.Fatalf("left overlap (vendor quirk): %v", got)
	}
	// no overlap
	if got := r.Subtract(Range{Pos: 50, Length: 1}); got.Pos != 0 || got.Length != 10 {
		t.Fatalf("no overlap: %v", got)
	}
}

// --- Comment (vendor) --------------------------------------------------------

func TestNewCommentMergeAndOverlap(t *testing.T) {
	c, err := NewComment("id1", []Range{{Pos: 0, Length: 2}, {Pos: 2, Length: 3}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Ranges) != 1 || c.Ranges[0].Pos != 0 || c.Ranges[0].Length != 5 {
		t.Fatalf("adjacent ranges must merge, got %v", c.Ranges)
	}
	if _, err := NewComment("id2", []Range{{Pos: 0, Length: 5}, {Pos: 3, Length: 2}}, false); err == nil {
		t.Fatalf("want overlap error")
	}
	c, _ = NewComment("id3", []Range{{Pos: 0, Length: 0}, {Pos: 1, Length: 2}}, true)
	if len(c.Ranges) != 1 {
		t.Fatalf("empty range must be dropped")
	}
	raw := c.ToRaw()
	if raw["resolved"] != true {
		t.Fatalf("resolved must be present when true, got %v", raw)
	}
	c, _ = NewComment("id4", nil, false)
	if _, ok := c.ToRaw()["resolved"]; ok {
		t.Fatalf("resolved key must be absent when false")
	}
}

func TestCommentApplyInsert(t *testing.T) {
	newC := func() *Comment {
		c, _ := NewComment("c", []Range{{Pos: 2, Length: 4}}, false)
		return c
	}
	// insert right after the comment (default extendComment=false)
	if c := newC().ApplyInsert(6, 3, false); len(c.Ranges) != 1 && (c.Ranges[0].Pos != 2 && c.Ranges[0].Pos != 9) {
		t.Fatalf("after: %v", c.Ranges)
	}
	// insert at the start
	if c := newC().ApplyInsert(2, 3, false); c.Ranges[0].Pos != 5 {
		t.Fatalf("at start: %v", c.Ranges)
	}
	// insert before the comment
	if c := newC().ApplyInsert(1, 3, false); c.Ranges[0].Pos != 5 {
		t.Fatalf("before: %v", c.Ranges)
	}
	// insert inside (split)
	c := newC().ApplyInsert(4, 2, false)
	if len(c.Ranges) != 2 {
		t.Fatalf("inside must split into 2 ranges, got %v", c.Ranges)
	}
	// extendComment: inside → extend
	if c := newC().ApplyInsert(4, 2, true); c.Ranges[0].Length != 6 {
		t.Fatalf("extend inside: %v", c.Ranges)
	}
	// extendComment outside → new range added
	if c := newC().ApplyInsert(10, 3, true); len(c.Ranges) != 2 {
		t.Fatalf("extend outside: %v", c.Ranges)
	}
}

func TestCommentApplyDelete(t *testing.T) {
	newC := func() *Comment {
		c, _ := NewComment("c", []Range{{Pos: 2, Length: 8}}, false)
		return c
	}
	// deletion overlapping the start → subtract
	c := newC().ApplyDelete(Range{Pos: 1, Length: 3})
	if len(c.Ranges) == 0 {
		t.Fatalf("want nonempty comment after partial delete, got %v", c.Ranges)
	}
	// deletion after the comment → no move
	c = newC().ApplyDelete(Range{Pos: 20, Length: 5})
	if c.Ranges[0].Pos != 2 {
		t.Fatalf("after: no move expected, got %v", c.Ranges)
	}
	// deletion before → move
	c = newC().ApplyDelete(Range{Pos: 0, Length: 2})
	if c.Ranges[0].Pos != 0 {
		t.Fatalf("before: move expected, got %v", c.Ranges)
	}
}

// --- TrackedChange list ops (vendor) -----------------------------------------

func TestTrackedInsertVariants(t *testing.T) {
	tc := TrackedChange{Range: Range{Pos: 2, Length: 4}, Tracking: Tracking{Type: "insert", UserID: "u", TS: "t"}}
	// cursor before → move
	out := applyTrackedInsert([]TrackedChange{tc}, 1, "ab")
	if out[0].Range.Pos != 4 {
		t.Fatalf("before: %v", out[0].Range)
	}
	// cursor at end → keep
	out = applyTrackedInsert([]TrackedChange{tc}, 6, "ab")
	if out[0].Range.Pos != 2 || out[0].Range.Length != 4 {
		t.Fatalf("at end: %v", out[0].Range)
	}
	// cursor inside → split (middle dropped)
	out = applyTrackedInsert([]TrackedChange{tc}, 4, "ab")
	if len(out) != 2 {
		t.Fatalf("inside must split, got %d parts", len(out))
	}
}

func TestTrackedDeleteVariants(t *testing.T) {
	tc := TrackedChange{Range: Range{Pos: 2, Length: 6}, Tracking: Tracking{Type: "delete"}}
	// fully contained → dropped
	out := applyTrackedDelete([]TrackedChange{tc}, 0, 10)
	if len(out) != 0 {
		t.Fatalf("contained must drop, got %v", out)
	}
	// partial overlap → subtract
	out = applyTrackedDelete([]TrackedChange{tc}, 1, 2)
	if len(out) != 1 {
		t.Fatalf("overlap must keep subset, got %v", out)
	}
	// after → move
	out = applyTrackedDelete([]TrackedChange{tc}, 0, 1)
	if len(out) == 1 && out[0].Range.Pos != 1 {
		t.Fatalf("after must move, got %v", out)
	}
}

// --- commentRangesAreInSync* (vendor) ----------------------------------------

func TestCommentRangesInSyncHistoryOT(t *testing.T) {
	p := map[string]any{"ranges": []any{map[string]any{"pos": 1, "length": 2}}}
	e := map[string]any{"ranges": []any{map[string]any{"pos": 1, "length": 2}}}
	if !commentRangesAreInSyncHistoryOT(p, e) {
		t.Fatalf("identical must be in sync")
	}
	e = map[string]any{"ranges": []any{map[string]any{"pos": 1, "length": 3}}}
	if commentRangesAreInSyncHistoryOT(p, e) {
		t.Fatalf("different length must be out of sync")
	}
	e = map[string]any{"ranges": []any{}}
	if commentRangesAreInSyncHistoryOT(p, e) {
		t.Fatalf("different count must be out of sync")
	}
}

func TestCommentRangesInSync(t *testing.T) {
	p := map[string]any{"ranges": []any{map[string]any{"pos": 1, "length": 4}}}
	e := map[string]any{"op": map[string]any{"p": 1, "c": "abcd"}}
	if !commentRangesAreInSync(p, e) {
		t.Fatalf("single range vs op must sync")
	}
	p = map[string]any{"ranges": []any{}}
	e = map[string]any{"op": map[string]any{"p": 1, "c": ""}}
	if !commentRangesAreInSync(p, e) {
		t.Fatalf("zero-length must map to detached (empty ranges)")
	}
	p = map[string]any{"ranges": []any{map[string]any{"pos": 1, "length": 4}, map[string]any{"pos": 9, "length": 1}}}
	if commentRangesAreInSync(p, e) {
		t.Fatalf("multi-range persisted must not sync")
	}
	p = map[string]any{"ranges": []any{map[string]any{"pos": 5, "length": 4}}}
	e = map[string]any{"op": map[string]any{"hpos": 5, "hlen": 4}}
	if !commentRangesAreInSync(p, e) {
		t.Fatalf("hpos/hlen must be honored")
	}
}

// --- queueUpdatesForOutOfSyncComments* (vendor) ------------------------------

func TestQueueCommentsHistoryOT(t *testing.T) {
	e := &expander{projectID: "p", origin: map[string]any{"kind": "k"}, expandedUpdates: []map[string]any{}}
	update := map[string]any{
		"doc":  "docA",
		"meta": map[string]any{"ts": "ts1"},
		"resyncDocContent": map[string]any{
			"historyOTRanges": map[string]any{
				"comments": []any{
					// expected: c2 only; c1 (persisted) must be deleted
					map[string]any{"id": "c2", "ranges": []any{map[string]any{"pos": 0, "length": 1}}, "resolved": true},
				},
			},
		},
	}
	persisted := []any{
		map[string]any{"id": "c1", "ranges": []any{}},
		map[string]any{"id": "c2", "ranges": []any{map[string]any{"pos": 9, "length": 9}}},
	}
	e.queueUpdatesForOutOfSyncCommentsHistoryOT(update, "a.tex", persisted)
	var deleted, newComment, resolvedOnly int
	for _, op := range e.expandedUpdates {
		ops, _ := op["op"].([]any)
		if len(ops) == 1 {
			o, _ := ops[0].(map[string]any)
			if _, ok := o["deleteComment"]; ok {
				deleted++
			}
			if _, ok := o["ranges"]; ok {
				newComment++
			}
			if _, ok := o["commentId"]; ok && o["ranges"] == nil {
				resolvedOnly++
			}
		}
	}
	if deleted != 1 {
		t.Fatalf("want deleteComment for c1, got %v", e.expandedUpdates)
	}
	if newComment != 1 {
		t.Fatalf("want new/ranges-diff update for c2, got %v", e.expandedUpdates)
	}
}

func TestQueueCommentsNonHistoryOT(t *testing.T) {
	e := &expander{projectID: "p", expandedUpdates: []map[string]any{}}
	update := map[string]any{
		"doc":  "docA",
		"meta": map[string]any{"ts": "ts1"},
		"resyncDocContent": map[string]any{
			"content":            "expected",
			"ranges":             map[string]any{"comments": []any{map[string]any{"id": "c9", "op": map[string]any{"p": 1, "i": "x"}}}},
			"resolvedCommentIds": []any{"c9"},
		},
	}
	persisted := []any{}
	e.queueUpdatesForOutOfSyncComments(update, "a.tex", persisted)
	if len(e.expandedUpdates) != 1 {
		t.Fatalf("want 1 new-comment update, got %v", e.expandedUpdates)
	}
	op := e.expandedUpdates[0]
	if op["commentId"] != nil && op["deleteComment"] != nil {
		t.Fatalf("shape mismatch")
	}
	ops, _ := op["op"].([]any)
	if len(ops) != 1 {
		t.Fatalf("want op array, got %v", op)
	}
	o, _ := ops[0].(map[string]any)
	if o["resolved"] != true {
		t.Fatalf("resolved from resolvedCommentIds must be true, got %v", o)
	}
	if o["p"] != 1 {
		t.Fatalf("op spread must include p, got %v", o)
	}
}

func TestQueueCommentsResolvedOnlyDiff(t *testing.T) {
	e := &expander{projectID: "p", expandedUpdates: []map[string]any{}}
	update := map[string]any{
		"doc":  "docA",
		"meta": map[string]any{"ts": "ts1"},
		"resyncDocContent": map[string]any{
			"content": "c",
			"ranges":  map[string]any{"comments": []any{map[string]any{"id": "c1", "op": map[string]any{"p": 0, "c": ""}}}},
		},
	}
	persisted := []any{
		map[string]any{"id": "c1", "ranges": []any{}, "resolved": true},
	}
	e.queueUpdatesForOutOfSyncComments(update, "a.tex", persisted)
	if len(e.expandedUpdates) != 1 {
		t.Fatalf("want resolved-only diff update, got %v", e.expandedUpdates)
	}
	op := e.expandedUpdates[0]
	if op["resolved"] == nil || op["commentId"] == nil {
		t.Fatalf("want resolved update shape, got %v", op)
	}
}

// --- tracked-change transitions (vendor) --------------------------------------

func TestTrackingDirectivesEqual(t *testing.T) {
	a := Tracking{Type: "none"}
	b := Tracking{Type: "none", UserID: "x"}
	if !trackingDirectivesEqual(a, b) {
		t.Fatalf("none == none regardless of extra fields")
	}
	a = Tracking{Type: "insert", UserID: "u", TS: "t"}
	b = Tracking{Type: "insert", UserID: "u", TS: "t"}
	if !trackingDirectivesEqual(a, b) {
		t.Fatalf("equal directives")
	}
	b.TS = "other"
	if trackingDirectivesEqual(a, b) {
		t.Fatalf("different ts must differ")
	}
}

func TestQueueTrackedChanges(t *testing.T) {
	e := &expander{projectID: "p", expandedUpdates: []map[string]any{}}
	update := map[string]any{
		"doc":  "docA",
		"meta": map[string]any{"ts": "ts1"},
		"resyncDocContent": map[string]any{
			"content": "0123456789",
			"ranges": map[string]any{
				"changes": []any{
					// expected insertion at pos 2 of length 3
					map[string]any{
						"op":       map[string]any{"p": 2, "i": "XYZ"},
						"metadata": map[string]any{"user_id": "u9", "ts": "ts9"},
					},
				},
			},
		},
	}
	// no persisted changes → the expected transition at pos2 differs from none →
	// a retain op with tracking must be emitted.
	e.queueUpdatesForOutOfSyncTrackedChanges(update, "a.tex", nil)
	found := false
	for _, op := range e.expandedUpdates {
		ops, _ := op["op"].([]any)
		for _, xo := range ops {
			m, _ := xo.(map[string]any)
			if tr, ok := m["tracking"].(map[string]any); ok && tr["type"] == "insert" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("want a retain op carrying the expected insert tracking, got %v", e.expandedUpdates)
	}
}

// --- error types ----------------------------------------------------------------

func TestErrorTypes(t *testing.T) {
	if (&UnprocessableError{Msg: "x"}).Error() != "x" {
		t.Fatalf("unprocessable msg")
	}
	if (&FileContentEmptyError{Msg: "y"}).Error() != "y" {
		t.Fatalf("empty msg")
	}
	if (&TooLongError{Msg: "z"}).Error() != "z" {
		t.Fatalf("toolong msg")
	}
	if (&NeedFullProjectStructureResyncError{Msg: "w"}).Error() != "w" {
		t.Fatalf("needfull msg")
	}
	if (&SyncOngoingError{Msg: "v"}).Error() != "v" {
		t.Fatalf("syncongoing msg")
	}
}

// --- isDataCorruptionError ------------------------------------------------------

func TestDataCorruptionDetection(t *testing.T) {
	d := &Deps{}
	d.withDefaults()
	if !d.isDataCorruptionError(&UnprocessableError{Msg: "x"}) {
		t.Fatalf("unprocessable is corruption")
	}
	if !d.isDataCorruptionError(&FileContentEmptyError{Msg: "x"}) {
		t.Fatalf("empty is corruption")
	}
	if !d.isDataCorruptionError(&TooLongError{Msg: "x"}) {
		t.Fatalf("toolong is corruption")
	}
	if d.isDataCorruptionError(errForTest("transient")) {
		t.Fatalf("transient is not corruption")
	}
}

func errForTest(msg string) error { return &errTest{msg} }

type errTest struct{ msg string }

func (e *errTest) Error() string { return e.msg }

// --- File helpers -----------------------------------------------------------------

func TestFileHelpers(t *testing.T) {
	f := NewStringFile("a.tex", "hello")
	if !f.Editable || f.Content != "hello" {
		t.Fatalf("fromString shape")
	}
	f.Comments = []*Comment{{ID: "c", Ranges: []Range{{Pos: 3, Length: 2}}}}
	f.CommentsApplyInsert(1, 3)
	// insert before the comment → ranges move by +3
	if f.Comments[0].Ranges[0].Pos != 6 {
		t.Fatalf("comment insert shift, got %v", f.Comments[0].Ranges)
	}
	f2 := NewStringFile("b.tex", "abcdef")
	f2.TrackedChanges = []TrackedChange{{Range: Range{Pos: 2, Length: 2}, Tracking: Tracking{Type: "insert"}}}
	f2.TrackedChangesApplyInsert(0, "XY")
	if f2.TrackedChanges[0].Range.Pos != 4 {
		t.Fatalf("tracked insert shift, got %v", f2.TrackedChanges[0].Range)
	}
	f3 := NewStringFile("c.tex", "abcdef")
	f3.Comments = []*Comment{{ID: "c", Ranges: []Range{{Pos: 2, Length: 4}}}}
	f3.CommentsApplyDelete(0, 2)
	if f3.Comments[0].Ranges[0].Pos != 0 {
		t.Fatalf("comment delete shift, got %v", f3.Comments[0].Ranges)
	}
	f4 := NewStringFile("d.tex", "ab")
	f4.TrackedChanges = []TrackedChange{{Range: Range{Pos: 0, Length: 1}, Tracking: Tracking{Type: "delete"}}}
	f4.TrackedChangesApplyDelete(0, 1)
	if len(f4.TrackedChanges) != 0 {
		t.Fatalf("fully deleted change must drop, got %v", f4.TrackedChanges)
	}
	raw := f4.CommentsToRaw()
	if len(raw) != 0 {
		t.Fatalf("empty comments")
	}
	sorted := f2.TrackedChangesAsSorted()
	if len(sorted) != 1 {
		t.Fatalf("sorted")
	}
}
