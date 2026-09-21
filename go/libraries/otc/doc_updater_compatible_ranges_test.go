package otc

// doc_updater_compatible_ranges_test.go — 1:1 mirror of
// test/unit/doc_updater_compatible_ranges.test.js (the Node oracle).

import (
	"testing"
)

const duContent = "the quick brown fox jumps over the lazy dog"

func duTrackedTC() []map[string]any {
	return []map[string]any{
		{"range": map[string]any{"pos": 4, "length": 6}, "tracking": map[string]any{"type": "delete", "userId": "31", "ts": "2023-01-01T00:00:00.000Z"}},
		{"range": map[string]any{"pos": 16, "length": 4}, "tracking": map[string]any{"type": "delete", "userId": "31", "ts": "2023-01-01T00:00:00.000Z"}},
		{"range": map[string]any{"pos": 35, "length": 5}, "tracking": map[string]any{"type": "insert", "userId": "31", "ts": "2023-01-01T00:00:00.000Z"}},
		{"range": map[string]any{"pos": 40, "length": 3}, "tracking": map[string]any{"type": "delete", "userId": "31", "ts": "2023-01-01T00:00:00.000Z"}},
	}
}

func duFile(t *testing.T, comments, tracked []map[string]any) *File {
	t.Helper()
	fd, err := NewStringFileData(duContent, comments, tracked)
	if err != nil {
		t.Fatalf("NewStringFileData: %v", err)
	}
	return NewFile(fd, nil)
}

func duRun(t *testing.T, comments, tracked []map[string]any) DocUpdaterCompatibleRanges {
	t.Helper()
	r, err := GetDocUpdaterCompatibleRanges(duFile(t, comments, tracked))
	if err != nil {
		t.Fatalf("GetDocUpdaterCompatibleRanges: %v", err)
	}
	return r
}

func TestDU_TrackedDeletionsShiftFollowing(t *testing.T) {
	r := duRun(t, nil, duTrackedTC())
	want := []int{4, 16 - 6, 35 - 6 - 4, 40 - 6 - 4}
	if len(r.Changes) != 4 {
		t.Fatalf("got %d changes, want 4", len(r.Changes))
	}
	for i, w := range want {
		if r.Changes[i].Op.P != w {
			t.Errorf("changes[%d].op.p = %d, want %d", i, r.Changes[i].Op.P, w)
		}
	}
	// the delete/insert kinds are recorded too
	if r.Changes[0].Op.Delete == nil || *r.Changes[0].Op.Delete != "quick " {
		t.Errorf("changes[0] should be a delete of 'quick '")
	}
	if r.Changes[2].Op.Insert == nil || *r.Changes[2].Op.Insert != "lazy " {
		t.Errorf("changes[2] should be an insert of 'lazy '")
	}
}

var duCommentTC = []map[string]any{
	{"range": map[string]any{"pos": 2, "length": 5}, "tracking": map[string]any{"type": "delete", "userId": "31", "ts": "2023-01-01T00:00:00.000Z"}},
	{"range": map[string]any{"pos": 11, "length": 1}, "tracking": map[string]any{"type": "delete", "userId": "31", "ts": "2023-01-01T00:00:00.000Z"}},
	{"range": map[string]any{"pos": 28, "length": 9}, "tracking": map[string]any{"type": "delete", "userId": "31", "ts": "2023-01-01T00:00:00.000Z"}},
}

func duComments() []map[string]any {
	return []map[string]any{
		{"id": "comment-1", "ranges": []any{
			map[string]any{"pos": 4, "length": 5},
			map[string]any{"pos": 10, "length": 5},
			map[string]any{"pos": 26, "length": 4},
			map[string]any{"pos": 35, "length": 4},
		}, "resolved": false},
		{"id": "comment-2", "ranges": []any{}, "resolved": true},
		{"id": "comment-3", "ranges": []any{map[string]any{"pos": 4, "length": 1}}, "resolved": true},
	}
}

func TestDU_CommentsTruncateTrackedDeletes(t *testing.T) {
	r := duRun(t, duComments(), duCommentTC)
	if len(r.Comments) != 3 {
		t.Fatalf("got %d comments, want 3", len(r.Comments))
	}
	if r.Comments[0].Op.P != 2 {
		t.Errorf("comments[0].op.p = %d, want 2", r.Comments[0].Op.P)
	}
	if want := "ck bown fox jumps ovzy"; r.Comments[0].Op.C != want {
		t.Errorf("comments[0].op.c = %q, want %q", r.Comments[0].Op.C, want)
	}
}

func TestDU_CommentResolvedStatus(t *testing.T) {
	r := duRun(t, duComments(), duCommentTC)
	if r.Comments[0].Op.Resolved != false {
		t.Error("comments[0].op.resolved should be false")
	}
	if r.Comments[1].Op.Resolved != true {
		t.Error("comments[1].op.resolved should be true")
	}
	if r.Comments[2].Op.Resolved != true {
		t.Error("comments[2].op.resolved should be true")
	}
}

func TestDU_CommentThreadID(t *testing.T) {
	r := duRun(t, duComments(), duCommentTC)
	want := []string{"comment-1", "comment-2", "comment-3"}
	for i, w := range want {
		if r.Comments[i].Op.T != w {
			t.Errorf("comments[%d].op.t = %q, want %q", i, r.Comments[i].Op.T, w)
		}
	}
}

func TestDU_DetachedCommentZeroLength(t *testing.T) {
	r := duRun(t, duComments(), duCommentTC)
	if r.Comments[1].Op.P != 0 {
		t.Errorf("detached comment op.p = %d, want 0", r.Comments[1].Op.P)
	}
	if r.Comments[1].Op.C != "" {
		t.Errorf("detached comment op.c = %q, want empty", r.Comments[1].Op.C)
	}
}

func TestDU_CommentEntirelyInTrackedDelete(t *testing.T) {
	// comment-3 ranges [{pos:4,length:1}] is 'q', entirely inside the tracked
	// delete at pos2..7 — it must be positioned at the start of that delete.
	r := duRun(t, duComments(), duCommentTC)
	// Node 'should position a comment entirely in a tracked delete next to the
	// tracked delete' (p == 2, c == '').
	if r.Comments[2].Op.P != 2 {
		t.Errorf("comments[2].op.p = %d, want 2", r.Comments[2].Op.P)
	}
	if r.Comments[2].Op.C != "" {
		t.Errorf("comments[2].op.c = %q, want empty", r.Comments[2].Op.C)
	}
}

// A binary (uneditable) file yields the empty shape.
func TestDU_BinaryFileIsEmpty(t *testing.T) {
	bfd, err := newBinaryFileData(EmptyHash, 3)
	if err != nil {
		t.Fatalf("newBinaryFileData: %v", err)
	}
	file := NewFile(bfd, nil)
	r, err := GetDocUpdaterCompatibleRanges(file)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.Changes) != 0 || len(r.Comments) != 0 {
		t.Errorf("binary file must yield empty ranges, got %d changes / %d comments", len(r.Changes), len(r.Comments))
	}
}
