package otc

import (
	"testing"
	"time"
)

func newChangeRequestOp(t *testing.T) Operation {
	t.Helper()
	file, err := FileFromHash(FileEmptyHash, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	op, err := NewAddFileOperation("main.tex", file)
	if err != nil {
		t.Fatal(err)
	}
	return op
}

func TestChangeRequest_Basics(t *testing.T) {
	op := newChangeRequestOp(t)
	// v1 authors are integers (or *Author); strings are rejected by assertV1.
	author := NewAuthor(1, "alice@example.com", "Alice")
	r := NewChangeRequest(3, []Operation{op}, true, []any{author})

	if r.GetBaseVersion() != 3 {
		t.Fatalf("GetBaseVersion = %d; want 3", r.GetBaseVersion())
	}
	if !r.IsUntransformable() {
		t.Fatal("IsUntransformable should be true")
	}
	if len(r.Operations) != 1 {
		t.Fatalf("len(Operations) = %d; want 1", len(r.Operations))
	}

	// makeChange produces a Change carrying the operations + authors.
	ch := r.MakeChange(time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC))
	if len(ch.GetOperations()) != 1 {
		t.Fatalf("makeChange: len(ops) = %d; want 1", len(ch.GetOperations()))
	}
	if len(ch.GetAuthors()) != 1 {
		t.Fatalf("makeChange: len(authors) = %d; want 1", len(ch.GetAuthors()))
	}
}

func TestChangeRequest_AuthorsDefault(t *testing.T) {
	// Node: `authors = authors || []` — nil authors default to an empty list.
	r := NewChangeRequest(0, nil, false, nil)
	if r == nil {
		t.Fatal("nil request")
	}
	if r.IsUntransformable() {
		t.Fatal("untransformable should default to false")
	}
	// nil ops default to an empty slice (not nil for ToRaw stability).
	if len(r.Operations) != 0 {
		t.Fatalf("len(Operations) = %d; want 0", len(r.Operations))
	}
}

func TestChangeRequest_RoundTrip(t *testing.T) {
	op := newChangeRequestOp(t)
	// Exercise the integer branch of AuthorList.assertV1 (first non-null is a number).
	r := NewChangeRequest(9, []Operation{op}, false, []any{int64(1), int64(2), nil})

	got, err := ChangeRequestFromRaw(r.ToRaw())
	if err != nil {
		t.Fatal(err)
	}
	if got.GetBaseVersion() != 9 {
		t.Fatalf("round-trip baseVersion = %d; want 9", got.GetBaseVersion())
	}
	if got.IsUntransformable() {
		t.Fatal("round-trip untransformable should be false")
	}
	if len(got.Operations) != 1 {
		t.Fatalf("round-trip len(ops) = %d; want 1", len(got.Operations))
	}
	if len(got.Authors) != 3 {
		t.Fatalf("round-trip len(authors) = %d; want 3", len(got.Authors))
	}
}

func TestChangeRequest_AuthorsMustBeV1(t *testing.T) {
	// assertV1 rejects plain strings (Node: expected a number or an Author).
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewChangeRequest with string authors should panic (assertV1)")
		}
	}()
	NewChangeRequest(1, nil, false, []any{"just-a-string"})
}

func TestChangeRequest_FromRawBadOps(t *testing.T) {
	if _, err := ChangeRequestFromRaw(map[string]any{}); err == nil {
		t.Fatal("missing operations must error")
	}
	if _, err := ChangeRequestFromRaw(map[string]any{"operations": "nope"}); err == nil {
		t.Fatal("non-array operations must error")
	}
}
