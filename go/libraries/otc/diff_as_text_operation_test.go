package otc

// diff_as_text_operation_test.go — 1:1 mirror of
// test/unit/diff_as_text_operation.test.js (the Node oracle).

import (
	"strings"
	"testing"
	"time"

	diffmatchpatch "github.com/sergi/go-diff/diffmatchpatch"
)

const (
	dtaTSISO      = "2026-07-10T00:00:00.000Z"
	dtaOTHERTSISO = "2026-07-09T00:00:00.000Z"
)

var dtaTS = func() time.Time { t, _ := time.Parse(time.RFC3339, "2026-07-10T00:00:00Z"); return t }()

// dtaTracking is the Node TRACKING = { userId: 'user-1', ts: TS }.
var dtaTracking = DiffOpts{Tracking: &DiffTracking{UserID: "user-1", TS: dtaTS}}

// dtaTrackedRaw builds a tracked-change raw (input + expected) with the given
// type / ts / userId / range (Node `trackedChange(...)`).
func dtaTrackedRaw(trackingType, tsISO, userID string, pos, length int) map[string]any {
	return map[string]any{
		"range":    map[string]any{"pos": pos, "length": length},
		"tracking": map[string]any{"type": trackingType, "userId": userID, "ts": tsISO},
	}
}

func dtaInputTc(trackingType string, pos, length int) map[string]any {
	return dtaTrackedRaw(trackingType, dtaOTHERTSISO, "user-2", pos, length)
}

func dtaTrackedList(fd *StringFileData) []map[string]any {
	return fd.GetTrackedChanges().ToRaw()
}

func dstrp(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// dtaD builds a keyed diff-match-patch.Diff (avoids unkeyed-literal vet).
func dtaD(op int, text string) diffmatchpatch.Diff {
	return diffmatchpatch.Diff{Type: diffmatchpatch.Operation(op), Text: text}
}

func dtaApplyDiff(t *testing.T, fd *StringFileData, after string, opts DiffOpts) *TextOperation {
	t.Helper()
	op, err := DiffAsTextOperation(fd, after, opts)
	if err != nil {
		t.Fatalf("DiffAsTextOperation: %v", err)
	}
	if err := fd.Edit(NewTextEdit(op)); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	return op
}

func dtaApplyDiffs(t *testing.T, fd *StringFileData, diffs []diffmatchpatch.Diff, opts DiffOpts) *TextOperation {
	t.Helper()
	op, err := DiffsToTextOperation(fd, diffs, opts)
	if err != nil {
		t.Fatalf("DiffsToTextOperation: %v", err)
	}
	if err := fd.Edit(NewTextEdit(op)); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	return op
}

// ---- diffAsTextOperation ----

func TestDTA_NoopUnchanged(t *testing.T) {
	fd, _ := NewStringFileData("hello world", nil, nil)
	op := dtaApplyDiff(t, fd, "hello world", DiffOpts{})
	if !op.IsNoop() {
		t.Error("unchanged content must give a noop operation")
	}
}

func TestDTA_MinimalEdit(t *testing.T) {
	fd, _ := NewStringFileData("hello world", nil, nil)
	op := dtaApplyDiff(t, fd, "hello brave world", DiffOpts{})
	if dstrp(fd.GetContent(false)) != "hello brave world" {
		t.Errorf("content = %q, want %q", dstrp(fd.GetContent(false)), "hello brave world")
	}
	sameRaw(t, "toJSON", op.ToJSON(), map[string]any{"textOperation": []any{6, "brave ", 5}})
}

func TestDTA_DiffsAgainstFilteredDeletes(t *testing.T) {
	fd, err := FromRawStringFileData(map[string]any{
		"content":        "hello cruel world",
		"trackedChanges": []any{dtaInputTc("delete", 6, 6)},
	})
	if err != nil {
		t.Fatalf("FromRaw: %v", err)
	}
	dtaApplyDiff(t, fd, "hello brave world", DiffOpts{})
	if dstrp(fd.GetContent(false)) != "hello brave cruel world" {
		t.Errorf("content = %q, want %q", dstrp(fd.GetContent(false)), "hello brave cruel world")
	}
	if dstrp(fd.GetContent(true)) != "hello brave world" {
		t.Errorf("filtered = %q, want %q", dstrp(fd.GetContent(true)), "hello brave world")
	}
	sameRaw(t, "trackedChanges", dtaTrackedList(fd), []any{dtaInputTc("delete", 12, 6)})
}

func TestDTA_TrackedInsert(t *testing.T) {
	fd, _ := NewStringFileData("hello world", nil, nil)
	dtaApplyDiff(t, fd, "hello brave world", dtaTracking)
	if dstrp(fd.GetContent(false)) != "hello brave world" {
		t.Errorf("content = %q", dstrp(fd.GetContent(false)))
	}
	sameRaw(t, "trackedChanges", dtaTrackedList(fd), []any{
		dtaTrackedRaw("insert", dtaTSISO, "user-1", 6, 6),
	})
}

func TestDTA_TrackedDelete(t *testing.T) {
	fd, _ := NewStringFileData("hello cruel world", nil, nil)
	dtaApplyDiff(t, fd, "hello world", dtaTracking)
	if dstrp(fd.GetContent(false)) != "hello cruel world" {
		t.Errorf("content = %q", dstrp(fd.GetContent(false)))
	}
	if dstrp(fd.GetContent(true)) != "hello world" {
		t.Errorf("filtered = %q", dstrp(fd.GetContent(true)))
	}
	sameRaw(t, "trackedChanges", dtaTrackedList(fd), []any{
		dtaTrackedRaw("delete", dtaTSISO, "user-1", 6, 6),
	})
}

// ---- diffsToTextOperation ----

func TestDTA_ConvertInsertion(t *testing.T) {
	fd, _ := NewStringFileData("hello world", nil, nil)
	dtaApplyDiffs(t, fd, []diffmatchpatch.Diff{dtaD(UNCHANGED, "hello "), dtaD(ADDED, "brave "), dtaD(UNCHANGED, "world")}, DiffOpts{})
	if dstrp(fd.GetContent(false)) != "hello brave world" {
		t.Errorf("content = %q", dstrp(fd.GetContent(false)))
	}
	sameRaw(t, "trackedChanges", dtaTrackedList(fd), []any{})
}

func TestDTA_ConvertRemoval(t *testing.T) {
	fd, _ := NewStringFileData("hello cruel world", nil, nil)
	dtaApplyDiffs(t, fd, []diffmatchpatch.Diff{dtaD(UNCHANGED, "hello "), dtaD(REMOVED, "cruel "), dtaD(UNCHANGED, "world")}, DiffOpts{})
	if dstrp(fd.GetContent(false)) != "hello world" {
		t.Errorf("content = %q", dstrp(fd.GetContent(false)))
	}
}

func TestDTA_NopIdentical(t *testing.T) {
	fd, _ := NewStringFileData("hello world", nil, nil)
	op, err := DiffsToTextOperation(fd, []diffmatchpatch.Diff{dtaD(UNCHANGED, "hello world")}, DiffOpts{})
	if err != nil {
		t.Fatalf("DiffsToTextOperation: %v", err)
	}
	if !op.IsNoop() {
		t.Error("identical content must give a noop")
	}
}

func TestDTA_UnknownTypeThrows(t *testing.T) {
	fd, _ := NewStringFileData("hello", nil, nil)
	_, err := DiffsToTextOperation(fd, []diffmatchpatch.Diff{dtaD(2, "hello")}, DiffOpts{})
	if err == nil || !strings.Contains(err.Error(), "Unknown type") {
		t.Fatalf("want 'Unknown type', got %v", err)
	}
}

func TestDTA_RetainDeleteInUnchanged(t *testing.T) {
	fd, _ := NewStringFileData("hello XXXworld", nil, []map[string]any{dtaInputTc("delete", 6, 3)})
	dtaApplyDiffs(t, fd, []diffmatchpatch.Diff{dtaD(UNCHANGED, "hello "), dtaD(ADDED, "brave "), dtaD(UNCHANGED, "world")}, DiffOpts{})
	if dstrp(fd.GetContent(false)) != "hello brave XXXworld" {
		t.Errorf("content = %q", dstrp(fd.GetContent(false)))
	}
	if dstrp(fd.GetContent(true)) != "hello brave world" {
		t.Errorf("filtered = %q", dstrp(fd.GetContent(true)))
	}
	sameRaw(t, "trackedChanges", dtaTrackedList(fd), []any{dtaInputTc("delete", 12, 3)})
}

func TestDTA_RetainDeleteInRemoved(t *testing.T) {
	fd, _ := NewStringFileData("abCCCcdef", nil, []map[string]any{dtaInputTc("delete", 2, 3)})
	dtaApplyDiffs(t, fd, []diffmatchpatch.Diff{dtaD(UNCHANGED, "a"), dtaD(REMOVED, "bc"), dtaD(UNCHANGED, "def")}, DiffOpts{})
	if dstrp(fd.GetContent(false)) != "aCCCdef" {
		t.Errorf("content = %q", dstrp(fd.GetContent(false)))
	}
	sameRaw(t, "trackedChanges", dtaTrackedList(fd), []any{dtaInputTc("delete", 1, 3)})
}

func TestDTA_RetainDeletesAfterEnd(t *testing.T) {
	fd, _ := NewStringFileData("abcDDD", nil, []map[string]any{dtaInputTc("delete", 3, 3)})
	op := dtaApplyDiffs(t, fd, []diffmatchpatch.Diff{dtaD(UNCHANGED, "abc")}, DiffOpts{})
	if op.BaseLength != 6 {
		t.Errorf("baseLength = %d, want 6", op.BaseLength)
	}
	if dstrp(fd.GetContent(false)) != "abcDDD" {
		t.Errorf("content = %q", dstrp(fd.GetContent(false)))
	}
	sameRaw(t, "trackedChanges", dtaTrackedList(fd), []any{dtaInputTc("delete", 3, 3)})
}

func TestDTA_OutOfSyncThrows(t *testing.T) {
	fd, _ := NewStringFileData("abcDE", nil, []map[string]any{dtaInputTc("delete", 4, 1)})
	_, err := DiffsToTextOperation(fd, []diffmatchpatch.Diff{dtaD(UNCHANGED, "abc")}, DiffOpts{})
	if err == nil || !strings.Contains(err.Error(), "StringFileData.trackedChanges out of sync") {
		t.Fatalf("want 'out of sync', got %v", err)
	}
}

func TestDTA_DiffsToOp_TrackedInsert(t *testing.T) {
	fd, _ := NewStringFileData("hello world", nil, nil)
	dtaApplyDiffs(t, fd, []diffmatchpatch.Diff{dtaD(UNCHANGED, "hello "), dtaD(ADDED, "brave "), dtaD(UNCHANGED, "world")}, dtaTracking)
	if dstrp(fd.GetContent(false)) != "hello brave world" {
		t.Errorf("content = %q", dstrp(fd.GetContent(false)))
	}
	sameRaw(t, "trackedChanges", dtaTrackedList(fd), []any{dtaTrackedRaw("insert", dtaTSISO, "user-1", 6, 6)})
}

func TestDTA_DiffsToOp_TrackedDelete(t *testing.T) {
	fd, _ := NewStringFileData("hello cruel world", nil, nil)
	dtaApplyDiffs(t, fd, []diffmatchpatch.Diff{dtaD(UNCHANGED, "hello "), dtaD(REMOVED, "cruel "), dtaD(UNCHANGED, "world")}, dtaTracking)
	if dstrp(fd.GetContent(false)) != "hello cruel world" {
		t.Errorf("content = %q", dstrp(fd.GetContent(false)))
	}
	if dstrp(fd.GetContent(true)) != "hello world" {
		t.Errorf("filtered = %q", dstrp(fd.GetContent(true)))
	}
	sameRaw(t, "trackedChanges", dtaTrackedList(fd), []any{dtaTrackedRaw("delete", dtaTSISO, "user-1", 6, 6)})
}

func TestDTA_DiffsToOp_WholeDocDelete(t *testing.T) {
	fd, _ := NewStringFileData("abc", nil, nil)
	op, err := DiffsToTextOperation(fd, []diffmatchpatch.Diff{dtaD(REMOVED, "abc")}, dtaTracking)
	if err != nil {
		t.Fatalf("DiffsToTextOperation: %v", err)
	}
	if err := fd.Edit(NewTextEdit(op)); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if dstrp(fd.GetContent(true)) != "" {
		t.Errorf("filtered = %q, want empty", dstrp(fd.GetContent(true)))
	}
	sameRaw(t, "trackedChanges", dtaTrackedList(fd), []any{dtaTrackedRaw("delete", dtaTSISO, "user-1", 0, 3)})
}

func TestDTA_LeavesExistingDeletes(t *testing.T) {
	fd, _ := NewStringFileData("hello CCCworld", nil, []map[string]any{dtaInputTc("delete", 6, 3)})
	dtaApplyDiffs(t, fd, []diffmatchpatch.Diff{dtaD(UNCHANGED, "hello "), dtaD(REMOVED, "wor"), dtaD(UNCHANGED, "ld")}, dtaTracking)
	if dstrp(fd.GetContent(false)) != "hello CCCworld" {
		t.Errorf("content = %q", dstrp(fd.GetContent(false)))
	}
	if dstrp(fd.GetContent(true)) != "hello ld" {
		t.Errorf("filtered = %q", dstrp(fd.GetContent(true)))
	}
	sameRaw(t, "trackedChanges", dtaTrackedList(fd), []any{
		dtaInputTc("delete", 6, 3),
		dtaTrackedRaw("delete", dtaTSISO, "user-1", 9, 3),
	})
}

func TestDTA_RemoveInsideTrackedInsert(t *testing.T) {
	fd, _ := NewStringFileData("hello INSworld", nil, []map[string]any{dtaInputTc("insert", 6, 3)})
	dtaApplyDiffs(t, fd, []diffmatchpatch.Diff{dtaD(UNCHANGED, "hello "), dtaD(REMOVED, "INS"), dtaD(UNCHANGED, "world")}, dtaTracking)
	if dstrp(fd.GetContent(false)) != "hello world" {
		t.Errorf("content = %q", dstrp(fd.GetContent(false)))
	}
	sameRaw(t, "trackedChanges", dtaTrackedList(fd), []any{})
}

func TestDTA_SplitRemovalOverTrackedInsert(t *testing.T) {
	fd, _ := NewStringFileData("abXYcd", nil, []map[string]any{dtaInputTc("insert", 2, 2)})
	dtaApplyDiffs(t, fd, []diffmatchpatch.Diff{dtaD(UNCHANGED, "a"), dtaD(REMOVED, "bXYc"), dtaD(UNCHANGED, "d")}, dtaTracking)
	if dstrp(fd.GetContent(false)) != "abcd" {
		t.Errorf("content = %q", dstrp(fd.GetContent(false)))
	}
	if dstrp(fd.GetContent(true)) != "ad" {
		t.Errorf("filtered = %q", dstrp(fd.GetContent(true)))
	}
	sameRaw(t, "trackedChanges", dtaTrackedList(fd), []any{dtaTrackedRaw("delete", dtaTSISO, "user-1", 1, 2)})
}

func TestDTA_StraddlingTrackedInsert(t *testing.T) {
	fd, _ := NewStringFileData("hello INSins", nil, []map[string]any{dtaInputTc("insert", 6, 6)})
	dtaApplyDiffs(t, fd, []diffmatchpatch.Diff{dtaD(UNCHANGED, "hello "), dtaD(REMOVED, "INS"), dtaD(UNCHANGED, "ins")}, dtaTracking)
	if dstrp(fd.GetContent(false)) != "hello ins" {
		t.Errorf("content = %q", dstrp(fd.GetContent(false)))
	}
	sameRaw(t, "trackedChanges", dtaTrackedList(fd), []any{dtaInputTc("insert", 6, 3)})
}

func TestDTA_KeepsIdenticalNoop(t *testing.T) {
	fd, _ := NewStringFileData("hello world", nil, nil)
	op := dtaApplyDiffs(t, fd, []diffmatchpatch.Diff{dtaD(UNCHANGED, "hello world")}, dtaTracking)
	if !op.IsNoop() {
		t.Error("identical content with tracking must still be a noop")
	}
	sameRaw(t, "trackedChanges", dtaTrackedList(fd), []any{})
}
