package historyconversions

import (
	"testing"

	"document-updater/internal/utils"
)

// Helper constructors (mirrors Node `makeComment`, `makeTrackedInsert/Delete`,
// `makeMetadata`).

func makeMetadata() *utils.Metadata {
	return &utils.Metadata{UserID: "user-id", TS: "ts"}
}

func makeComment(id string, pos, length int) Comment {
	c := makeC(length)
	return Comment{ID: id, Op: CommentOp{P: pos, C: c, T: id}}
}

func makeC(length int) string {
	out := make([]byte, length)
	for i := range out {
		out[i] = 'c'
	}
	return string(out)
}

func makeTrackedInsert(id string, pos, length int) utils.TrackedChange {
	i := makeI(length)
	return utils.TrackedChange{ID: &id, Op: utils.Op{P: pos, I: &i}, Metadata: makeMetadata()}
}

func makeI(length int) string {
	out := make([]byte, length)
	for i := range out {
		out[i] = 'i'
	}
	return string(out)
}

func makeTrackedDelete(id string, pos, length int) utils.TrackedChange {
	d := makeD(length)
	return utils.TrackedChange{ID: &id, Op: utils.Op{P: pos, D: &d}, Metadata: makeMetadata()}
}

func makeD(length int) string {
	out := make([]byte, length)
	for i := range out {
		out[i] = 'd'
	}
	return string(out)
}

func intP(v int) *int { return &v }
func intPtr(v *int) int {
	if v == nil {
		return -1
	}
	return *v
}

// ===== toHistoryRanges mirrors Node tests =====

func TestToHistoryRangesEmpty(t *testing.T) {
	got := ToHistoryRanges(Ranges{})
	if len(got.Changes) != 0 || len(got.Comments) != 0 {
		t.Fatalf("empty: want empty, got %v %v", got.Changes, got.Comments)
	}
}

func TestToHistoryRangesNoTrackedChanges(t *testing.T) {
	comments := []Comment{makeComment("comment1", 5, 12)}
	ranges := Ranges{Comments: comments}
	got := ToHistoryRanges(ranges)
	if len(got.Comments) != 1 {
		t.Fatalf("want 1 comment, got %d", len(got.Comments))
	}
	if got.Comments[0].Op.P != 5 || got.Comments[0].Op.C != makeC(12) || got.Comments[0].Op.T != "comment1" {
		t.Fatalf("comment mismatch: %+v", got.Comments[0].Op)
	}
	if got.Comments[0].Op.Hpos != nil || got.Comments[0].Op.Hlen != nil {
		t.Fatalf("no offset, no hpos/hlen: hpos=%v hlen=%v", got.Comments[0].Op.Hpos, got.Comments[0].Op.Hlen)
	}
}

func TestToHistoryRangesDeleteOffset(t *testing.T) {
	comments := []Comment{
		makeComment("comment0", 0, 1),
		makeComment("comment1", 10, 12),
		makeComment("comment2", 20, 10),
		makeComment("comment3", 15, 3),
	}
	changes := []utils.TrackedChange{
		makeTrackedDelete("change0", 2, 5),
		makeTrackedInsert("change1", 4, 5),
		makeTrackedDelete("change2", 10, 10),
		makeTrackedDelete("change3", 21, 6),
		makeTrackedDelete("change4", 50, 7),
	}
	ranges := Ranges{Comments: comments, Changes: changes}
	got := ToHistoryRanges(ranges)

	if len(got.Comments) != 4 {
		t.Fatalf("want 4 comments, got %d", len(got.Comments))
	}
	expected := map[string]struct{ hpos, hlen *int }{
		"comment0": {nil, nil},
		"c1":       {intP(25), intP(18)},
		"c2":       {intP(35), intP(16)},
		"c3":       {intP(30), nil},
	}
	_byT := map[string]HistoryComment{}
	for _, hc := range got.Comments {
		_byT[hc.Op.T] = hc
	}
	for _, tc := range []struct {
		commentID string
		want      HistoryTrackedChange
	}{
		{"comment0", HistoryTrackedChange{Op: HistoryOp{P: 0, Hpos: nil, Hlen: nil}}},
	} {
		_ = tc
	}
	// comment0: p=0, c length 1, hpos/hlen nil
	if hc := _byT["comment0"]; hc.Op.Hpos != nil || hc.Op.Hlen != nil {
		t.Fatalf("comment0 hpos/hlen: %v %v", hc.Op.Hpos, hc.Op.Hlen)
	}
	checkT := func(id string, wantP int, wantHpos, wantHlen *int) {
		t.Helper()
		hc, ok := _byT[id]
		if !ok {
			t.Fatalf("missing comment %s", id)
		}
		if hc.Op.P != wantP {
			t.Errorf("comment %s p: want %d got %d", id, wantP, hc.Op.P)
		}
		if !intEqPtr(hc.Op.Hpos, wantHpos) {
			t.Errorf("comment %s hpos: want %v got %v", id, wantHpos, hc.Op.Hpos)
		}
		if !intEqPtr(hc.Op.Hlen, wantHlen) {
			t.Errorf("comment %s hlen: want %v got %v", id, wantHlen, hc.Op.Hlen)
		}
	}
	checkT("comment1", 10, intP(25), intP(18))
	checkT("comment2", 20, intP(35), intP(16))
	checkT("comment3", 15, intP(30), nil)

	// check hpos/hlen values
	if hc := _byT["comment1"]; intPtr(hc.Op.Hpos) != 25 || intPtr(hc.Op.Hlen) != 18 {
		t.Fatalf("comment1 hpos=%d hlen=%d", intPtr(hc.Op.Hpos), intPtr(hc.Op.Hlen))
	}
	if hc := _byT["comment2"]; intPtr(hc.Op.Hpos) != 35 || intPtr(hc.Op.Hlen) != 16 {
		t.Fatalf("comment2 hpos=%d hlen=%d", intPtr(hc.Op.Hpos), intPtr(hc.Op.Hlen))
	}
	if hc := _byT["comment3"]; intPtr(hc.Op.Hpos) != 30 {
		t.Fatalf("comment3 hpos=%d", intPtr(hc.Op.Hpos))
	}
	_ = expected

	// Changes: hpos shifts (inserts skipped).
	if len(got.Changes) != 5 {
		t.Fatalf("want 5 changes, got %d", len(got.Changes))
	}
	wants := []*int{nil, intP(9), intP(15), intP(36), intP(71)}
	for i, wc := range wants {
		if hc := got.Changes[i]; !intEqPtr(hc.Op.Hpos, wc) {
			t.Errorf("change %d hpos: want %v got %v", i, wc, hc.Op.Hpos)
		}
	}
}

func intEqPtr(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// ===== toHistoryOT mirrors Node tests =====

func TestToHistoryOTEmpty(t *testing.T) {
	got := ToHistoryOT([]string{"one two"}, Ranges{}, nil)
	if got["content"] != "one two" {
		t.Fatalf("content: %q", got["content"])
	}
	if len(got) != 1 {
		kinds := []string{}
		for k := range got {
			kinds = append(kinds, k)
		}
		t.Fatalf("want only content key, got %v", kinds)
	}
}

func TestToHistoryOTConversions(t *testing.T) {
	metadata := makeMetadata()
	ranges := Ranges{
		Changes: []utils.TrackedChange{
			{ID: ptrStr("change0"), Op: utils.Op{P: 4, D: ptrStr("two ")}, Metadata: metadata},
			{ID: ptrStr("change1"), Op: utils.Op{P: 4, I: ptrStr("three")}, Metadata: metadata},
		},
		Comments: []Comment{
			{ID: "comment0", Op: CommentOp{P: 4, C: "three", T: "comment0"}},
			{ID: "comment1", Op: CommentOp{P: 0, C: "", T: "comment1"}},
		},
	}
	got := ToHistoryOT([]string{"one three"}, ranges, []string{"comment0"})

	if got["content"] != "one two three" {
		t.Fatalf("content: %q", got["content"])
	}

	comments, ok := got["comments"].([]map[string]any)
	if !ok {
		t.Fatalf("comments missing/type")
	}
	if len(comments) != 2 {
		t.Fatalf("want 2 comments, got %d", len(comments))
	}
	// comment1 first (detached, p=0)
	if comments[0]["id"] != "comment1" {
		t.Fatalf("first comment id: %v", comments[0]["id"])
	}
	ranges0, _ := comments[0]["ranges"].(map[string]any)
	if len(ranges0) != 0 {
		t.Fatalf("comment1 ranges: want detached (empty), got %v", ranges0)
	}
	if comments[0]["resolved"] != nil {
		t.Fatalf("comment1 should not be resolved")
	}
	// comment0 second (p=4 > 0)
	if comments[1]["id"] != "comment0" {
		t.Fatalf("second comment id: %v", comments[1]["id"])
	}
	ranges1, _ := comments[1]["ranges"].([]any)
	if len(ranges1) != 1 {
		t.Fatalf("comment0 ranges count")
	}
	if comments[1]["resolved"] != true {
		t.Fatalf("comment0 should be resolved")
	}

	tracked, ok := got["trackedChanges"].([]map[string]any)
	if !ok {
		t.Fatalf("trackedChanges missing/type")
	}
	if len(tracked) != 2 {
		t.Fatalf("want 2 tracked changes, got %d", len(tracked))
	}
}

func ptrStr(s string) *string { return &s }

// ===== fromHistoryOT mirrors Node tests =====

func TestFromHistoryOTNoRanges(t *testing.T) {
	got, err := FromHistoryOT(map[string]any{"content": "one two"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got.Lines) != 1 || got.Lines[0] != "one two" {
		t.Fatalf("lines: %v", got.Lines)
	}
	if len(got.Changes) != 0 || len(got.Comments) != 0 {
		t.Fatalf("want no ranges, got %d changes %d comments", len(got.Changes), len(got.Comments))
	}
}

func TestFromHistoryOTConversions(t *testing.T) {
	raw := map[string]any{
		"content": "one two three",
		"trackedChanges": []any{
			map[string]any{
				"range":    map[string]any{"pos": 4, "length": 4},
				"tracking": map[string]any{"type": "delete", "userId": "user-id", "ts": "2024-01-01T00:00:00.000Z"},
			},
			map[string]any{
				"range":    map[string]any{"pos": 8, "length": 5},
				"tracking": map[string]any{"type": "insert", "userId": "user-id", "ts": "2024-01-01T00:00:00.000Z"},
			},
		},
		"comments": []any{
			map[string]any{"id": "comment0", "ranges": []any{map[string]any{"pos": 8, "length": 5}}, "resolved": true},
			map[string]any{"id": "comment1", "ranges": []any{}},
		},
	}
	got, err := FromHistoryOT(raw)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got.Lines) != 1 || got.Lines[0] != "one three" {
		t.Fatalf("lines: %v", got.Lines)
	}
	if len(got.Changes) != 2 {
		t.Fatalf("want 2 changes, got %d", len(got.Changes))
	}
	for i, c := range got.Changes {
		if len(c.ID) == 0 {
			t.Fatalf("change %d id empty", i)
		}
	}
	if got.Changes[0].P != 4 || *got.Changes[0].Delete != "two " {
		t.Fatalf("change0: op %+v", got.Changes[0])
	}
	if got.Changes[1].P != 4 || *got.Changes[1].Insert != "three" {
		t.Fatalf("change1: op %+v", got.Changes[1])
	}
	for i, c := range got.Changes {
		if c.UserID != "user-id" || c.TS != "2024-01-01T00:00:00.000Z" {
			t.Fatalf("change %d metadata: %+v", i, c)
		}
	}
	if len(got.Comments) != 2 {
		t.Fatalf("want 2 comments, got %d", len(got.Comments))
	}
	if got.Comments[0].ID != "comment0" || got.Comments[0].P != 4 || got.Comments[0].C != "three" || got.Comments[0].T != "comment0" || !got.Comments[0].Resolved {
		t.Fatalf("comment0: %+v", got.Comments[0])
	}
	if got.Comments[1].ID != "comment1" || got.Comments[1].C != "" || got.Comments[1].Resolved {
		t.Fatalf("comment1: %+v", got.Comments[1])
	}
}
