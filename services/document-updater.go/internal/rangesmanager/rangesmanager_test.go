package rangesmanager

import (
	"errors"
	"reflect"
	"testing"

	"ollitex/go/libraries/rangestracker"
)

// ---------------------------------------------------------------------------
// Mirrors Node test/unit/js/RangesManager/RangersManagerTests.js 1:1.
// Every expected op/meta is an oracle value taken from that file.
// ---------------------------------------------------------------------------

const (
	testUserID    = "user-id-123"
	testDocID     = "doc-id-123"
	testProjectID = "project-id-123"
)

func s(v string) *string { return &v }

func b(v bool) *bool { return &v }

func i(v int) *int { return &v }

// metricsRecorder records histogram invocations (Node: this.Metrics stub).
type metricsRecorder struct {
	called  bool
	name    string
	value   int
	buckets []int
	opts    map[string]string
}

func (m *metricsRecorder) Histogram(name string, value int, buckets []int, opts map[string]string) {
	m.called = true
	m.name = name
	m.value = value
	m.buckets = buckets
	m.opts = opts
}

func newTestManager(t *testing.T) (*RangesManager, *metricsRecorder) {
	rec := &metricsRecorder{}
	return &RangesManager{MaxComments: 500, MaxChanges: 2000, Metrics: rec, Now: func() int64 { return 12345 }}, rec
}

func defaultRanges() Ranges {
	return Ranges{
		Changes: []rangestracker.Change{
			{ID: "1", Op: rangestracker.Op{I: s("five"), P: 15}},
		},
		Comments: []rangestracker.CommentItem{
			{ID: "1", Op: rangestracker.Op{C: s("three "), P: 4}},
		},
	}
}

func defaultUpdates() []Update {
	return []Update{{
		Doc:  testDocID,
		Op:   []rangestracker.Op{{I: s("two "), P: 4}},
		Meta: map[string]any{"user_id": testUserID},
	}}
}

func defaultLines() []string { return []string{"one two three four five"} }

func checkHistoryOps(t *testing.T, updates []HistoryUpdate, want [][]HistoryOp) {
	t.Helper()
	if len(updates) != len(want) {
		t.Fatalf("want %d history updates, got %d: %+v", len(want), len(updates), updates)
	}
	for k := range updates {
		got := updates[k]
		if !reflect.DeepEqual(got.Doc, testDocID) {
			t.Errorf("update[%d].doc = %q, want %q", k, got.Doc, testDocID)
		}
		if !reflect.DeepEqual(got.Op, want[k]) {
			t.Errorf("update[%d].op mismatch:\n got: %+v\nwant: %+v", k, got.Op, want[k])
		}
	}
}

func TestApplyUpdate_Successful(t *testing.T) {
	m, rec := newTestManager(t)
	ranges := defaultRanges()
	result, err := m.ApplyUpdate(testProjectID, testDocID, &ranges, defaultUpdates(), defaultLines(), Opts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RangesWereCollapsed {
		t.Error("rangesWereCollapsed should be false")
	}
	// newRanges: comments [0] is {c: 'three ', p: 8}
	if len(result.NewRanges.Comments) != 1 {
		t.Fatalf("want 1 comment, got %d: %+v", len(result.NewRanges.Comments), result.NewRanges.Comments)
	}
	if got := result.NewRanges.Comments[0].Op; got.C == nil || *got.C != "three " || got.P != 8 {
		t.Errorf("comment op = %+v, want {c: 'three ', p: 8}", got)
	}
	// changes [0] is {i: 'five', p: 19}
	if len(result.NewRanges.Changes) != 1 {
		t.Fatalf("want 1 change, got %d: %+v", len(result.NewRanges.Changes), result.NewRanges.Changes)
	}
	if got := result.NewRanges.Changes[0].Op; got.I == nil || *got.I != "five" || got.P != 19 {
		t.Errorf("change op = %+v, want {i: 'five', p: 19}", got)
	}
	// history updates: unmodified op (no hpos when p == hpos)
	checkHistoryOps(t, result.HistoryUpdates, [][]HistoryOp{
		{{I: s("two "), P: 4}},
	})
	// update meta is a copy of the source meta
	if !reflect.DeepEqual(result.HistoryUpdates[0].Meta, map[string]any{"user_id": testUserID}) {
		t.Errorf("history meta = %v, want copy of {user_id}", result.HistoryUpdates[0].Meta)
	}
	if rec.called {
		t.Error("metrics.histogram should not be called when range count does not decrease")
	}
	// removedChangeIDs is empty
	if len(result.RemovedChangeIDs) != 0 {
		t.Errorf("removedChangeIDs = %v, want empty", result.RemovedChangeIDs)
	}
}

func TestApplyUpdate_EmptyComments(t *testing.T) {
	m, _ := newTestManager(t)
	ranges := defaultRanges()
	ranges.Comments = nil
	result, err := m.ApplyUpdate(testProjectID, testDocID, &ranges, defaultUpdates(), defaultLines(), Opts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty (absent) comments must not be in the response object
	if result.NewRanges.Comments != nil {
		t.Errorf("newRanges.comments should be nil, got %+v", result.NewRanges.Comments)
	}
	checkHistoryOps(t, result.HistoryUpdates, [][]HistoryOp{
		{{I: s("two "), P: 4}},
	})
}

func TestApplyUpdate_EmptyChanges(t *testing.T) {
	m, _ := newTestManager(t)
	ranges := defaultRanges()
	ranges.Changes = nil
	result, err := m.ApplyUpdate(testProjectID, testDocID, &ranges, defaultUpdates(), defaultLines(), Opts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NewRanges.Changes != nil {
		t.Errorf("newRanges.changes should be nil, got %+v", result.NewRanges.Changes)
	}
	checkHistoryOps(t, result.HistoryUpdates, [][]HistoryOp{
		{{I: s("two "), P: 4}},
	})
}

func TestApplyUpdate_TooManyComments(t *testing.T) {
	m, _ := newTestManager(t)
	m.MaxComments = 2
	updates := []Update{{
		Doc:  testDocID,
		Op:   []rangestracker.Op{{C: s("one"), P: 0, T: s("thread-id-1")}},
		Meta: map[string]any{"user_id": testUserID},
	}}
	ranges := Ranges{
		Comments: []rangestracker.CommentItem{
			{ID: "1", Op: rangestracker.Op{C: s("three "), P: 4, T: s("thread-id-2")}},
			{ID: "2", Op: rangestracker.Op{C: s("four "), P: 10, T: s("thread-id-3")}},
		},
	}
	_, err := m.ApplyUpdate(testProjectID, testDocID, &ranges, updates, defaultLines(), Opts{})
	if !errors.Is(err, ErrTooManyRanges) {
		t.Fatalf("want ErrTooManyRanges ('too many comments or tracked changes'), got %v", err)
	}
}

func TestApplyUpdate_TooManyChanges(t *testing.T) {
	m, _ := newTestManager(t)
	m.MaxChanges = 2
	updates := []Update{{
		Doc:  testDocID,
		Op:   []rangestracker.Op{{I: s("one "), P: 0}},
		Meta: map[string]any{"user_id": testUserID, "tc": "track-changes-id-yes"},
	}}
	ranges := Ranges{
		Changes: []rangestracker.Change{
			{ID: "1", Op: rangestracker.Op{I: s("three"), P: 4}},
			{ID: "2", Op: rangestracker.Op{I: s("four"), P: 10}},
		},
	}
	_, err := m.ApplyUpdate(testProjectID, testDocID, &ranges, updates, []string{"one two three four"}, Opts{})
	if !errors.Is(err, ErrTooManyRanges) {
		t.Fatalf("want ErrTooManyRanges, got %v", err)
	}
}

func TestApplyUpdate_InconsistentChanges(t *testing.T) {
	m, _ := newTestManager(t)
	updates := []Update{{
		Doc:  testDocID,
		Op:   []rangestracker.Op{{C: s("doesn't match"), P: 0}},
		Meta: map[string]any{"user_id": testUserID},
	}}
	ranges := defaultRanges()
	_, err := m.ApplyUpdate(testProjectID, testDocID, &ranges, updates, defaultLines(), Opts{})
	if err == nil || err.Error() != "insertion does not match text in document" {
		t.Fatalf("want 'insertion does not match text in document', got %v", err)
	}
}

func TestApplyUpdate_CollapsesRange(t *testing.T) {
	m, _ := newTestManager(t)
	updates := []Update{{
		Doc:  testDocID,
		Op:   []rangestracker.Op{{D: s("one"), P: 0, T: s("thread-id-1")}},
		Meta: map[string]any{"user_id": testUserID},
	}}
	ranges := Ranges{
		Comments: []rangestracker.CommentItem{
			{ID: "1", Op: rangestracker.Op{C: s("n"), P: 1, T: s("thread-id-2")}},
		},
	}
	result, err := m.ApplyUpdate(testProjectID, testDocID, &ranges, updates, defaultLines(), Opts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.RangesWereCollapsed {
		t.Error("rangesWereCollapsed should be true")
	}
}

func TestApplyUpdate_DeletesRanges(t *testing.T) {
	m, rec := newTestManager(t)
	updates := []Update{{
		Doc:  testDocID,
		Op:   []rangestracker.Op{{D: s("one two three four five"), P: 0}},
		Meta: map[string]any{"user_id": testUserID},
	}}
	ranges := Ranges{
		Comments: []rangestracker.CommentItem{
			{ID: "1", Op: rangestracker.Op{C: s("n"), P: 1, T: s("thread-id-2")}},
		},
		Changes: []rangestracker.Change{
			{ID: "1", Op: rangestracker.Op{I: s("hello"), P: 1, T: s("thread-id-2")}},
		},
	}
	result, err := m.ApplyUpdate(testProjectID, testDocID, &ranges, updates, defaultLines(), Opts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rec.called || rec.name != "range-delta" {
		t.Error("metrics.histogram('range-delta') should be called")
	}
	if !result.RangesWereCollapsed {
		t.Error("rangesWereCollapsed should be true")
	}
}

func TestApplyUpdate_CommentOpsNotSentToHistory(t *testing.T) {
	m, _ := newTestManager(t)
	updates := []Update{{
		Doc:  testDocID,
		Op:   []rangestracker.Op{{I: s("two "), P: 4}, {C: s("one"), P: 0}},
		Meta: map[string]any{"user_id": testUserID},
	}}
	result, err := m.ApplyUpdate(testProjectID, testDocID, &Ranges{}, updates, defaultLines(), Opts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkHistoryOps(t, result.HistoryUpdates, [][]HistoryOp{
		{{I: s("two "), P: 4}},
	})
}

func TestInsertedAmongTrackedDeletes(t *testing.T) {
	m, _ := newTestManager(t)
	// original text is "on[1]e[22] [333](three) fo[4444]ur five"
	ranges := Ranges{
		Changes: []rangestracker.Change{
			{ID: "1", Op: rangestracker.Op{D: s("1"), P: 2}},
			{ID: "2", Op: rangestracker.Op{D: s("22"), P: 3}},
			{ID: "3", Op: rangestracker.Op{D: s("333"), P: 4}},
			{ID: "4", Op: rangestracker.Op{I: s("three"), P: 4}},
			{ID: "5", Op: rangestracker.Op{D: s("4444"), P: 12}},
		},
	}
	updates := []Update{
		{Doc: testDocID, Op: []rangestracker.Op{{I: s("zero "), P: 0}}, Meta: map[string]any{"user_id": testUserID}},
		{Doc: testDocID, Op: []rangestracker.Op{{I: s("two "), P: 9, U: b(true)}}, Meta: map[string]any{"user_id": testUserID}},
	}
	result, err := m.ApplyUpdate(testProjectID, testDocID, &ranges, updates, []string{"zero one two three four five"}, Opts{HistoryRangesSupport: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkHistoryOps(t, result.HistoryUpdates, [][]HistoryOp{
		{{I: s("zero "), P: 0}},
		// 'two' is added just before the "333" tracked delete
		{{I: s("two "), P: 9, U: b(true), Hpos: i(12)}},
	})
}

func TestTrackedDeleteRejection_Single(t *testing.T) {
	m, _ := newTestManager(t)
	// original text is "one [two ]three four five"
	ranges := Ranges{
		Changes: []rangestracker.Change{
			{ID: "1", Op: rangestracker.Op{D: s("two "), P: 4}},
		},
	}
	updates := []Update{{
		Doc:  testDocID,
		Op:   []rangestracker.Op{{I: s("tw"), P: 4, U: b(true)}},
		Meta: map[string]any{"user_id": testUserID},
	}}
	result, err := m.ApplyUpdate(testProjectID, testDocID, &ranges, updates, []string{"one twthree four five"}, Opts{HistoryRangesSupport: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// no hpos since 4 == hpos
	checkHistoryOps(t, result.HistoryUpdates, [][]HistoryOp{
		{{I: s("tw"), P: 4, U: b(true), TrackedDeleteRejection: true}},
	})
}

func TestTrackedDeleteRejection_MultipleAtSamePosition(t *testing.T) {
	m, _ := newTestManager(t)
	// original text is "one [two ][three ][four ]five"
	ranges := Ranges{
		Changes: []rangestracker.Change{
			{ID: "1", Op: rangestracker.Op{D: s("two "), P: 4}},
			{ID: "2", Op: rangestracker.Op{D: s("three "), P: 4}},
			{ID: "3", Op: rangestracker.Op{D: s("four "), P: 4}},
		},
	}
	updates := []Update{{
		Doc:  testDocID,
		Op:   []rangestracker.Op{{I: s("three "), P: 4, U: b(true)}},
		Meta: map[string]any{"user_id": testUserID},
	}}
	result, err := m.ApplyUpdate(testProjectID, testDocID, &ranges, updates, []string{"one three five"}, Opts{HistoryRangesSupport: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkHistoryOps(t, result.HistoryUpdates, [][]HistoryOp{
		{{I: s("three "), P: 4, U: b(true), Hpos: i(8), TrackedDeleteRejection: true}},
	})
	// The tracker rejects the tracked delete that MATCHES the inserted text
	// ("three ", id 2); "two " (id 1) just shifts. The Node test does not
	// assert removedChangeIds here, but the Go tracker deterministically
	// reports the rejected one.
	if len(result.RemovedChangeIDs) != 1 || result.RemovedChangeIDs[0] != "2" {
		t.Errorf("removedChangeIDs = %v, want [2]", result.RemovedChangeIDs)
	}
}

func TestDeletesOverTrackedChanges(t *testing.T) {
	m, _ := newTestManager(t)
	// original text is "on[1]e [22](three) f[333]ou[4444]r [55555]five"
	ranges := Ranges{
		Changes: []rangestracker.Change{
			{ID: "1", Op: rangestracker.Op{D: s("1"), P: 2}},
			{ID: "2", Op: rangestracker.Op{D: s("22"), P: 4}},
			{ID: "3", Op: rangestracker.Op{I: s("three"), P: 4}},
			{ID: "4", Op: rangestracker.Op{D: s("333"), P: 11}},
			{ID: "5", Op: rangestracker.Op{D: s("4444"), P: 13}},
			{ID: "6", Op: rangestracker.Op{D: s("55555"), P: 15}},
		},
	}
	updates := []Update{
		{Doc: testDocID, Op: []rangestracker.Op{{D: s("four "), P: 10}}, Meta: map[string]any{"user_id": testUserID}},
		{Doc: testDocID, Op: []rangestracker.Op{{D: s("three "), P: 4}}, Meta: map[string]any{"user_id": testUserID}},
	}
	result, err := m.ApplyUpdate(testProjectID, testDocID, &ranges, updates, []string{"one five"}, Opts{HistoryRangesSupport: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkHistoryOps(t, result.HistoryUpdates, [][]HistoryOp{
		{
			{
				D: s("four "), P: 10, Hpos: i(13),
				TrackedChanges: []HistoryDeleteTrackedChange{
					{Type: "delete", Offset: 1, Length: 3},
					{Type: "delete", Offset: 3, Length: 4},
				},
			},
		},
		{
			{
				D: s("three "), P: 4, Hpos: i(7),
				TrackedChanges: []HistoryDeleteTrackedChange{
					{Type: "insert", Offset: 0, Length: 5},
				},
			},
		},
	})
}

func TestDeletesOverlappingTrackedInserts(t *testing.T) {
	m, _ := newTestManager(t)
	// original text is "(one) (three) (four) five"
	ranges := Ranges{
		Changes: []rangestracker.Change{
			{ID: "1", Op: rangestracker.Op{I: s("one"), P: 0}},
			{ID: "2", Op: rangestracker.Op{I: s("three"), P: 4}},
			{ID: "3", Op: rangestracker.Op{I: s("four"), P: 10}},
		},
	}
	updates := []Update{
		{Doc: testDocID, Op: []rangestracker.Op{{D: s("ne th"), P: 1}}, Meta: map[string]any{"user_id": testUserID, "tc": "tracked-change-id"}},
		{Doc: testDocID, Op: []rangestracker.Op{{D: s("ou"), P: 6}}, Meta: map[string]any{"user_id": testUserID, "tc": "tracked-change-id"}},
	}
	result, err := m.ApplyUpdate(testProjectID, testDocID, &ranges, updates, []string{"oree fr five"}, Opts{HistoryRangesSupport: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkHistoryOps(t, result.HistoryUpdates, [][]HistoryOp{
		{
			{
				D: s("ne th"), P: 1,
				TrackedChanges: []HistoryDeleteTrackedChange{
					{Type: "insert", Offset: 0, Length: 2},
					{Type: "insert", Offset: 3, Length: 2},
				},
			},
		},
		{
			{
				D: s("ou"), P: 6, Hpos: i(7),
				TrackedChanges: []HistoryDeleteTrackedChange{
					{Type: "insert", Offset: 0, Length: 2},
				},
			},
		},
	})
}

func TestCommentsAmongTrackedDeletes(t *testing.T) {
	m, _ := newTestManager(t)
	// original text is "on[1]e[22] [333](three) fo[4444]ur five"
	ranges := Ranges{
		Changes: []rangestracker.Change{
			{ID: "1", Op: rangestracker.Op{D: s("1"), P: 2}},
			{ID: "2", Op: rangestracker.Op{D: s("22"), P: 3}},
			{ID: "3", Op: rangestracker.Op{D: s("333"), P: 4}},
			{ID: "4", Op: rangestracker.Op{I: s("three"), P: 4}},
			{ID: "5", Op: rangestracker.Op{D: s("4444"), P: 12}},
		},
	}
	updates := []Update{
		{Doc: testDocID, Op: []rangestracker.Op{{C: s("three "), P: 4}}, Meta: map[string]any{"user_id": testUserID}},
		{Doc: testDocID, Op: []rangestracker.Op{{C: s("four "), P: 10}}, Meta: map[string]any{"user_id": testUserID}},
	}
	result, err := m.ApplyUpdate(testProjectID, testDocID, &ranges, updates, []string{"one three four five"}, Opts{HistoryRangesSupport: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkHistoryOps(t, result.HistoryUpdates, [][]HistoryOp{
		{{C: s("three "), P: 4, Hpos: i(10)}},
		{{C: s("four "), P: 10, Hpos: i(16), Hlen: i(9)}},
	})
}

func TestInsertedIntoComments(t *testing.T) {
	m, _ := newTestManager(t)
	// original text is "one three four five"
	ranges := Ranges{
		Comments: []rangestracker.CommentItem{
			{ID: "1", Op: rangestracker.Op{C: s("three"), P: 4, T: s("comment-id-1")}},
			{ID: "2", Op: rangestracker.Op{C: s("ree four"), P: 6, T: s("comment-id-2")}},
		},
	}
	updates := []Update{
		{Doc: testDocID, Op: []rangestracker.Op{{I: s("[before]"), P: 4}}, Meta: map[string]any{"user_id": testUserID}},
		{Doc: testDocID, Op: []rangestracker.Op{{I: s("[inside]"), P: 13}}, Meta: map[string]any{"user_id": testUserID}},
		{Doc: testDocID, Op: []rangestracker.Op{{I: s("[overlap]"), P: 23}}, Meta: map[string]any{"user_id": testUserID}},
		{Doc: testDocID, Op: []rangestracker.Op{{I: s("[after]"), P: 39}}, Meta: map[string]any{"user_id": testUserID}},
	}
	result, err := m.ApplyUpdate(testProjectID, testDocID, &ranges, updates,
		[]string{"one [before]t[inside]hr[overlap]ee four[after] five"}, Opts{HistoryRangesSupport: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkHistoryOps(t, result.HistoryUpdates, [][]HistoryOp{
		{{I: s("[before]"), P: 4}},
		{{I: s("[inside]"), P: 13, CommentIds: []string{"comment-id-1"}}},
		{{I: s("[overlap]"), P: 23, CommentIds: []string{"comment-id-1", "comment-id-2"}}},
		{{I: s("[after]"), P: 39}},
	})
}

func TestCroppedStartOfComment(t *testing.T) {
	m, _ := newTestManager(t)
	// original text is "one three four five"
	ranges := Ranges{
		Comments: []rangestracker.CommentItem{
			{ID: "1", Op: rangestracker.Op{C: s("three"), P: 4, T: s("comment-id-1")}},
		},
	}
	updates := []Update{{
		Doc:  testDocID,
		Op:   []rangestracker.Op{{D: s("ne thr"), P: 1}},
		Meta: map[string]any{"user_id": testUserID, "tc": "tracking-id"},
	}}
	result, err := m.ApplyUpdate(testProjectID, testDocID, &ranges, updates, []string{"oee four five"}, Opts{HistoryRangesSupport: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkHistoryOps(t, result.HistoryUpdates, [][]HistoryOp{
		{
			{D: s("ne thr"), P: 1},
			{C: s("ee"), P: 1, Hpos: i(7), T: s("comment-id-1")},
		},
	})
}

func TestCroppedFullComment(t *testing.T) {
	m, _ := newTestManager(t)
	ranges := Ranges{
		Comments: []rangestracker.CommentItem{
			{ID: "1", Op: rangestracker.Op{C: s("three"), P: 4, T: s("comment-id-1")}},
		},
	}
	updates := []Update{{
		Doc:  testDocID,
		Op:   []rangestracker.Op{{D: s("ne three f"), P: 1}},
		Meta: map[string]any{"user_id": testUserID, "tc": "tracking-id"},
	}}
	result, err := m.ApplyUpdate(testProjectID, testDocID, &ranges, updates, []string{"oour five"}, Opts{HistoryRangesSupport: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkHistoryOps(t, result.HistoryUpdates, [][]HistoryOp{
		{
			{D: s("ne three f"), P: 1},
			{C: s(""), P: 1, Hpos: i(11), T: s("comment-id-1")},
		},
	})
}

func TestCroppedEndOfComment(t *testing.T) {
	m, _ := newTestManager(t)
	ranges := Ranges{
		Comments: []rangestracker.CommentItem{
			{ID: "1", Op: rangestracker.Op{C: s("three"), P: 4, T: s("comment-id-1")}},
		},
	}
	updates := []Update{{
		Doc:  testDocID,
		Op:   []rangestracker.Op{{D: s("ee f"), P: 7}},
		Meta: map[string]any{"user_id": testUserID, "tc": "tracking-id"},
	}}
	result, err := m.ApplyUpdate(testProjectID, testDocID, &ranges, updates, []string{"one throur five"}, Opts{HistoryRangesSupport: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkHistoryOps(t, result.HistoryUpdates, [][]HistoryOp{
		{
			{D: s("ee f"), P: 7},
			{C: s("thr"), P: 4, T: s("comment-id-1")},
		},
	})
}

func TestUnCroppedComment(t *testing.T) {
	m, _ := newTestManager(t)
	ranges := Ranges{
		Comments: []rangestracker.CommentItem{
			{ID: "1", Op: rangestracker.Op{C: s("three"), P: 4, T: s("comment-id-1")}},
		},
	}
	updates := []Update{{
		Doc:  testDocID,
		Op:   []rangestracker.Op{{D: s("hre"), P: 5}},
		Meta: map[string]any{"user_id": testUserID, "tc": "tracking-id"},
	}}
	result, err := m.ApplyUpdate(testProjectID, testDocID, &ranges, updates, []string{"one te four five"}, Opts{HistoryRangesSupport: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkHistoryOps(t, result.HistoryUpdates, [][]HistoryOp{
		{{D: s("hre"), P: 5}},
	})
}

func TestRemovedChangeIDs(t *testing.T) {
	m, _ := newTestManager(t)
	// original text is "one [two ]three four five"
	ranges := Ranges{
		Changes: []rangestracker.Change{
			{ID: "1", Op: rangestracker.Op{D: s("two "), P: 4}},
		},
	}
	updates := []Update{{
		Doc:  testDocID,
		Op:   []rangestracker.Op{{I: s("two "), P: 4, U: b(true)}},
		Meta: map[string]any{"user_id": testUserID},
	}}
	result, err := m.ApplyUpdate(testProjectID, testDocID, &ranges, updates, defaultLines(), Opts{HistoryRangesSupport: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.RemovedChangeIDs) != 1 || result.RemovedChangeIDs[0] != "1" {
		t.Fatalf("removedChangeIDs = %v, want [1]", result.RemovedChangeIDs)
	}
}

func TestAcceptChanges_Single(t *testing.T) {
	m, _ := newTestManager(t)
	ranges := Ranges{
		Changes: []rangestracker.Change{
			{ID: "1", Op: rangestracker.Op{I: s("lorem"), P: 0}},
			{ID: "2", Op: rangestracker.Op{I: s("ipsum"), P: 10}},
			{ID: "3", Op: rangestracker.Op{I: s("dolor"), P: 20}},
			{ID: "4", Op: rangestracker.Op{I: s("sit"), P: 30}},
			{ID: "5", Op: rangestracker.Op{I: s("amet"), P: 40}},
		},
	}
	lines := []string{"lorem xxx", "ipsum yyy", "dolor zzz", "sit wwwww", "amet"}
	result := m.AcceptChanges(testProjectID, testDocID, []string{"2"}, &ranges, lines)
	if len(result.Changes) != 4 {
		t.Fatalf("want 4 changes after accept, got %d", len(result.Changes))
	}
	ids := map[string]bool{}
	for _, c := range result.Changes {
		ids[c.ID] = true
	}
	if ids["2"] {
		t.Error("change id 2 should be removed")
	}
	for _, want := range []string{"1", "3", "4", "5"} {
		if !ids[want] {
			t.Errorf("change id %s should be kept", want)
		}
	}
}

func TestAcceptChanges_Multiple(t *testing.T) {
	m, _ := newTestManager(t)
	ranges := Ranges{
		Changes: []rangestracker.Change{
			{ID: "1", Op: rangestracker.Op{I: s("lorem"), P: 0}},
			{ID: "2", Op: rangestracker.Op{I: s("ipsum"), P: 10}},
			{ID: "3", Op: rangestracker.Op{I: s("dolor"), P: 20}},
			{ID: "4", Op: rangestracker.Op{I: s("sit"), P: 30}},
			{ID: "5", Op: rangestracker.Op{I: s("amet"), P: 40}},
		},
	}
	lines := []string{"lorem xxx", "ipsum yyy", "dolor zzz", "sit wwwww", "amet"}
	result := m.AcceptChanges(testProjectID, testDocID, []string{"2", "4", "5"}, &ranges, lines)
	if len(result.Changes) != 2 {
		t.Fatalf("want 2 changes after accept, got %d", len(result.Changes))
	}
	for _, c := range result.Changes {
		if c.ID == "2" || c.ID == "4" || c.ID == "5" {
			t.Errorf("change %s should be removed", c.ID)
		}
	}
}

func TestDeleteComment(t *testing.T) {
	m, _ := newTestManager(t)
	ranges := Ranges{
		Comments: []rangestracker.CommentItem{
			{ID: "1", Op: rangestracker.Op{C: s("foo"), P: 0}},
			{ID: "2", Op: rangestracker.Op{C: s("bar"), P: 10}},
		},
	}
	result := m.DeleteComment("1", &ranges)
	if len(result.Comments) != 1 || result.Comments[0].ID != "2" {
		t.Fatalf("want only comment id 2, got %+v", result.Comments)
	}
}

func TestGetHistoryUpdatesForAcceptedChanges_TrackedInserts(t *testing.T) {
	m, _ := newTestManager(t)
	// 'one two three four five' <-- text before changes
	changes := []rangestracker.Change{
		{ID: "1", Op: rangestracker.Op{I: s("lorem"), P: 0}, Metadata: rangestracker.Metadata{"user_id": testUserID, "ts": "2024-01-01T00:00:00Z"}},
		{ID: "2", Op: rangestracker.Op{I: s("ipsum"), P: 15}, Metadata: rangestracker.Metadata{"user_id": testUserID, "ts": "2024-01-01T00:00:01Z"}},
	}
	lines := []string{"loremone two thipsumree four five"} // doc_length 33, no deletes
	result := m.GetHistoryUpdatesForAcceptedChanges(AcceptedChangesArgs{
		DocID:             testDocID,
		AcceptedChangeIDs: []string{"1", "2"},
		Changes:           changes,
		Pathname:          "",
		ProjectHistoryID:  "",
		Lines:             lines,
	})
	if len(result) != 2 {
		t.Fatalf("want 2 updates, got %d", len(result))
	}
	wantMeta := map[string]any{
		"user_id":    testUserID,
		"ts":         int64(12345),
		"doc_length": 33,
		"pathname":   "",
	}
	for k, hu := range result {
		if !reflect.DeepEqual(hu.Meta, wantMeta) {
			t.Errorf("update[%d] meta = %v, want %v", k, hu.Meta, wantMeta)
		}
		if _, ok := hu.Meta["history_doc_length"]; ok {
			t.Errorf("update[%d] should have no history_doc_length", k)
		}
	}
	checkHistoryOps(t, result, [][]HistoryOp{
		{{R: s("lorem"), P: 0, Tracking: &HistoryTracking{Type: "none"}}},
		{{R: s("ipsum"), P: 15, Tracking: &HistoryTracking{Type: "none"}}},
	})
}

func TestGetHistoryUpdatesForAcceptedChanges_TrackedDeletes(t *testing.T) {
	m, _ := newTestManager(t)
	// 'one two three four five' <-- text before changes
	changes := []rangestracker.Change{
		{ID: "1", Op: rangestracker.Op{D: s("two"), P: 4}, Metadata: rangestracker.Metadata{"user_id": testUserID, "ts": "2024-01-01T00:00:00Z"}},
		{ID: "2", Op: rangestracker.Op{D: s("three"), P: 5}, Metadata: rangestracker.Metadata{"user_id": testUserID, "ts": "2024-01-01T00:00:01Z"}},
	}
	lines := []string{"one   four five"} // doc_length 15
	result := m.GetHistoryUpdatesForAcceptedChanges(AcceptedChangesArgs{
		DocID:             testDocID,
		AcceptedChangeIDs: []string{"1", "2"},
		Changes:           changes,
		Pathname:          "",
		ProjectHistoryID:  "",
		Lines:             lines,
	})
	if len(result) != 2 {
		t.Fatalf("want 2 updates, got %d", len(result))
	}
	// Per-node oracle both updates carry history_doc_length (Node adds the
	// key on every accepted change when any delete is present, even though
	// the value equals doc_length after the running decrement).
	// update[0]: 15 + 3 + 5 = 23, then -= 3 → 20
	// update[1]: 20, then -= 5 → 15
	if result[0].Meta["history_doc_length"] != 23 {
		t.Errorf("update[0] history_doc_length = %v, want 23", result[0].Meta["history_doc_length"])
	}
	if result[1].Meta["history_doc_length"] != 20 {
		t.Errorf("update[1] history_doc_length = %v, want 20", result[1].Meta["history_doc_length"])
	}
	checkHistoryOps(t, result, [][]HistoryOp{
		{{D: s("two"), P: 4}},
		{{D: s("three"), P: 5}},
	})
}

func TestGetHistoryUpdatesForAcceptedChanges_UnacceptedDeletes(t *testing.T) {
	m, _ := newTestManager(t)
	// 'one two three four five' <-- text before changes
	changes := []rangestracker.Change{
		{ID: "1", Op: rangestracker.Op{D: s("two"), P: 4}, Metadata: rangestracker.Metadata{"user_id": testUserID, "ts": "2024-01-01T00:00:00Z"}},
		{ID: "2", Op: rangestracker.Op{D: s("three"), P: 5}, Metadata: rangestracker.Metadata{"user_id": testUserID, "ts": "2024-01-01T00:00:01Z"}},
	}
	lines := []string{"one   four five"} // doc_length 15
	result := m.GetHistoryUpdatesForAcceptedChanges(AcceptedChangesArgs{
		DocID:             testDocID,
		AcceptedChangeIDs: []string{"2"},
		Changes:           changes,
		Pathname:          "",
		ProjectHistoryID:  "",
		Lines:             lines,
	})
	if len(result) != 1 {
		t.Fatalf("want 1 update, got %d", len(result))
	}
	if result[0].Meta["history_doc_length"] != 23 {
		t.Errorf("history_doc_length = %v, want 23", result[0].Meta["history_doc_length"])
	}
	checkHistoryOps(t, result, [][]HistoryOp{
		{{D: s("three"), P: 5, Hpos: i(8)}},
	})
}

func TestGetHistoryUpdatesForAcceptedChanges_Mixed(t *testing.T) {
	m, _ := newTestManager(t)
	// 'one two three four five' <-- text before changes
	changes := []rangestracker.Change{
		{ID: "1", Op: rangestracker.Op{D: s("two"), P: 4}, Metadata: rangestracker.Metadata{"user_id": testUserID, "ts": "2024-01-01T00:00:00Z"}},
		{ID: "2", Op: rangestracker.Op{D: s("three"), P: 5}, Metadata: rangestracker.Metadata{"user_id": testUserID, "ts": "2024-01-01T00:00:01Z"}},
		{ID: "3", Op: rangestracker.Op{I: s("xxx "), P: 6}, Metadata: rangestracker.Metadata{"user_id": testUserID, "ts": "2024-01-01T00:00:02Z"}},
		{ID: "4", Op: rangestracker.Op{D: s("five"), P: 15}, Metadata: rangestracker.Metadata{"user_id": testUserID, "ts": "2024-01-01T00:00:03Z"}},
	}
	lines := []string{"one   xxx four "} // doc_length 15
	result := m.GetHistoryUpdatesForAcceptedChanges(AcceptedChangesArgs{
		DocID:             testDocID,
		AcceptedChangeIDs: []string{"1", "3", "4"},
		Changes:           changes,
		Pathname:          "",
		ProjectHistoryID:  "",
		Lines:             lines,
	})
	if len(result) != 3 {
		t.Fatalf("want 3 updates, got %d", len(result))
	}
	// history_doc_length: 15+3+5+4=27 → -=3 → 24 for inserts & the
	// second delete (key present at every accepted change).
	if result[0].Meta["history_doc_length"] != 27 {
		t.Errorf("update[0] history_doc_length = %v, want 27", result[0].Meta["history_doc_length"])
	}
	if result[1].Meta["history_doc_length"] != 24 {
		t.Errorf("update[1] history_doc_length = %v, want 24", result[1].Meta["history_doc_length"])
	}
	if result[2].Meta["history_doc_length"] != 24 {
		t.Errorf("update[2] history_doc_length = %v, want 24", result[2].Meta["history_doc_length"])
	}
	checkHistoryOps(t, result, [][]HistoryOp{
		{{D: s("two"), P: 4}},
		{{R: s("xxx "), P: 6, Hpos: i(11), Tracking: &HistoryTracking{Type: "none"}}},
		{{D: s("five"), P: 15, Hpos: i(20)}},
	})
}
