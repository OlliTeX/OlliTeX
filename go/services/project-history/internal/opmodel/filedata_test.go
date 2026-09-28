package opmodel

import (
	"strings"
	"testing"
)

// Mirrors the vendor StringFileData surface (vendor test files for the
// blob/snapshot layer are out of scope per HANDOFF).

func TestStringFileData_Getters(t *testing.T) {
	f, err := StringFileDataFromRaw(map[string]any{
		"content": "Hello, world!",
		"comments": []any{
			map[string]any{
				"id":     "c1",
				"ranges": []any{map[string]any{"pos": float64(0), "length": float64(5)}},
			},
		},
		"trackedChanges": []any{
			map[string]any{
				"range":    map[string]any{"pos": float64(0), "length": float64(5)},
				"tracking": map[string]any{"type": "insert", "userId": "u1", "ts": "2023-01-01T00:00:00.000Z"},
			},
		},
	})
	if err != nil {
		t.Fatalf("fromRaw: %v", err)
	}
	assertEqualT(t, f.GetContent(), "Hello, world!", "content")
	assertEqualT(t, f.GetComments().Len(), 1, "comments")
	assertEqualT(t, f.GetTrackedChanges().Len(), 1, "tracked changes")
	assertEqualT(t, f.GetComments().Array()[0].ID, "c1", "comment id")
}

func TestStringFileData_Edit(t *testing.T) {
	f, _ := StringFileDataFromRaw(map[string]any{"content": "abc"})
	op := NewTextOperation().Retain(1, nil).Insert("X", nil, nil).Retain(2, nil)
	if err := f.Edit(op); err != nil {
		t.Fatalf("edit: %v", err)
	}
	assertEqualT(t, f.GetContent(), "aXbc", "edited content")
}

func TestTextOperation_Apply_TooLongError(t *testing.T) {
	// Vendor: applying an operation whose result exceeds MAX_STRING_LENGTH
	// throws a TooLongError. Retain 2 MiB + insert (1 MiB + 10) = 3 MiB + 10.
	baseLen := 2 * 1024 * 1024
	insertLen := MaxStringLength - baseLen + 10
	f := &StringFileData{
		Content:        strings.Repeat("a", baseLen),
		Comments:       NewCommentList(nil),
		TrackedChanges: &TrackedChangeList{},
	}
	op := NewTextOperation()
	op.Insert(strings.Repeat("b", insertLen), nil, nil)
	op.Retain(baseLen, nil)
	err := f.Edit(op)
	if err == nil {
		t.Fatalf("expected error")
	}
	if _, ok := err.(*TooLongError); !ok {
		t.Fatalf("expected TooLongError, got %v", err)
	}
}

func TestTextOperation_ApplyToLength_TooLongError(t *testing.T) {
	op := NewTextOperation()
	op.BaseLength = MaxStringLength + 10
	op.TargetLength = MaxStringLength + 10
	op.Ops = []ScanOp{RetainOp{Length: MaxStringLength + 10}}
	_, err := op.ApplyToLength(MaxStringLength + 10)
	if err == nil {
		t.Fatalf("expected TooLongError from ApplyToLength")
	}
	if _, ok := err.(*TooLongError); !ok {
		t.Fatalf("not TooLongError: %v", err)
	}
}

func TestTextOperation_ErrorPaths(t *testing.T) {
	// Apply to a file with wrong base length.
	f, _ := StringFileDataFromRaw(map[string]any{"content": "ab"})
	op := NewTextOperation().Retain(1, nil)
	if err := op.Apply(f); err == nil {
		t.Fatalf("expected error for short apply")
	}

	// ApplyToLength base mismatch.
	op2 := NewTextOperation().Retain(1, nil)
	if _, err := op2.ApplyToLength(2); err == nil {
		t.Fatalf("expected apply-to-length base mismatch error")
	}
}

func TestUnprocessableError_Message(t *testing.T) {
	e := NewUnprocessableError("bad", nil)
	assertEqualT(t, e.Error(), "bad", "error message")
}
