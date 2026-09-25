package core

import (
	"encoding/json"
	"testing"
)

// Oracle: Node overleaf-editor-core/lib/operation/text_operation.js
//   o.retain(5).remove(3).insert("abcdef").retain(2)
//   baseLength = 10  (retain 5+2 + remove 3)  targetLength = 13 (retain 5+2 + insert 6)

func TestBaseLengthRetainRemove(t *testing.T) {
	o := NewTextOp(
		NewRetain(5, nil),
		NewRemove(3),
		NewInsert("abcdef", nil, nil),
		NewRetain(2, nil),
	)
	if got := o.BaseLength(); got != 10 {
		t.Fatalf("BaseLength() = %d, want 10 (retain+remove, oracle)", got)
	}
	if got, err := o.ApplyToLength(10); err != nil || got != 13 {
		t.Fatalf("ApplyToLength(10) = %d, %v; want 13, nil (oracle)", got, err)
	}
}

// Round-trip: raw wire -> typed -> Back length math must agree with the builder.
func TestBaseLengthFromRawMatchesBuilder(t *testing.T) {
	o := NewTextOp(
		NewRetain(5, nil),
		NewRemove(3),
		NewInsert("abcdef", nil, nil),
		NewRetain(2, nil),
	)
	raw := o.ToRaw()
	parsed, err := TextOpFromRaw(raw)
	if err != nil {
		t.Fatalf("TextOpFromRaw: %v", err)
	}
	if got := parsed.BaseLength(); got != 10 {
		t.Fatalf("parsed BaseLength() = %d, want 10", got)
	}
	if got, err := parsed.ApplyToLength(10); err != nil || got != 13 {
		t.Fatalf("parsed ApplyToLength(10) = %d, %v; want 13, nil", got, err)
	}
	// ensure no stray keys leaked (parse-equivalence: contentHash only when set)
	var m map[string]json.RawMessage
	_ = json.Unmarshal(raw, &m)
	if _, ok := m["contentHash"]; ok {
		t.Fatalf("ToRaw() should not set contentHash when unset")
	}
}

// baseLength guard: an op whose baseLength differs from the length given must fail.
func TestApplyToLengthMismatch(t *testing.T) {
	o := NewTextOp(NewRetain(5, nil))
	if _, err := o.ApplyToLength(4); err == nil {
		t.Fatalf("ApplyToLength(4) should error (baseLength must match)")
	}
	if _, err := o.ApplyToLength(5); err != nil {
		t.Fatalf("ApplyToLength(5) unexpected error: %v", err)
	}
}
