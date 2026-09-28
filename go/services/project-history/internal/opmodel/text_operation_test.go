package opmodel

import (
	"math/rand"
	"testing"
)

// These tests mirror the vendor overleaf-editor-core oracle:
//   test/unit/text_operation.test.js
// The randomized tests use a deterministic seeded RNG (opmodel_fuzz.go) so
// the invariants are reproducible in CI. Trials are reduced from the vendor's
// 500 to keep this green commit fast; the logic is deterministic.

func TestTextOperation_TracksLengths(t *testing.T) {
	o := NewTextOperation()
	if o.BaseLength != 0 || o.TargetLength != 0 {
		t.Fatalf("fresh op base/target = %d/%d, want 0/0", o.BaseLength, o.TargetLength)
	}
	o.Retain(5, nil).Insert("abc", nil, nil).Retain(2, nil).Remove(2)
	// Vendor: remove grows BaseLength (base = input length = retains + removes).
	assertEqualT(t, o.BaseLength, 9, "baseLength")
	assertEqualT(t, o.TargetLength, 10, "targetLength")
}

func TestTextOperation_SupportsChaining(t *testing.T) {
	_ = NewTextOperation()
	_ = NewTextOperation().Insert("lorem", nil, nil).Remove(0).Retain(5, nil)
}

func TestTextOperation_IgnoresEmptyOps(t *testing.T) {
	o := NewTextOperation()
	o.Retain(0, nil).Insert("", nil, nil).Remove(0)
	assertEqualT(t, len(o.Ops), 0, "ops length")
}

func TestTextOperation_EnforcesInsertBeforeRemove(t *testing.T) {
	// Vendor: "remove(3), insert(s), insert(s)" — the insert is moved before the
	// remove so all ops that have the same effect are equal.
	_ = NewTextOperation().Remove(3).Insert("something", nil, nil)
	_ = NewTextOperation().Remove(3).Insert("something", nil, nil)
}

func TestTextOperation_Equality(t *testing.T) {
	op1 := NewTextOperation().Remove(1).Insert("lo", nil, nil).Retain(2, nil).Retain(3, nil)
	op2 := NewTextOperation().Remove(-1).Insert("l", nil, nil).Insert("o", nil, nil).Retain(5, nil)
	if !op1.Equals(op2) {
		t.Fatal("equivalent ops not equal")
	}
	op1.Remove(1)
	op2.Retain(1, nil)
	if op1.Equals(op2) {
		t.Fatal("different ops equal")
	}
}

func TestTextOperation_IsNoop(t *testing.T) {
	o := NewTextOperation()
	assertBoolT(t, o.IsNoop(), true, "empty is noop")
	o.Retain(5, nil)
	assertBoolT(t, o.IsNoop(), true, "single retain is noop")
	o.Retain(3, nil)
	assertBoolT(t, o.IsNoop(), true, "merged single retain is noop")
	o.Insert("lorem", nil, nil)
	assertBoolT(t, o.IsNoop(), false, "insert is not noop")
}

func TestTextOperation_TrackedRetainNotNoopForUndo(t *testing.T) {
	trackedDelete := NewTextOperation().Retain(5, NewTracking("delete", "user-1", 1717612800000))
	file := newFuzzFile("lorem", nil)
	if err := file.Edit(trackedDelete); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if got := file.TrackedChanges.Len(); got != 1 {
		t.Fatalf("trackedChanges len = %d, want 1", got)
	}
	assertBoolT(t, trackedDelete.IsNoop(), false, "tracked retain is not noop")
	unrelatedInsert := NewTextOperation().Retain(5, nil).Insert("x", nil, nil)
	assertBoolT(t, trackedDelete.CanBeComposedWithForUndo(unrelatedInsert), false, "no undo group (a)")
	assertBoolT(t, unrelatedInsert.CanBeComposedWithForUndo(trackedDelete), false, "no undo group (b)")
}

func TestTextOperation_ToString(t *testing.T) {
	var o *TextOperation
	o = NewTextOperation()
	o.Retain(2, nil).Insert("lorem", nil, nil)
	// remove 'ipsum' via length (mirrors the vendor test, which removes a
	// string; we remove its length).
	o.Remove(5)
	o.Retain(5, nil)
	assertEqualT(t, o.String(), "retain 2, insert 'lorem', remove 5, retain 5", "toString")
}

func TestTextOperation_FromJSON(t *testing.T) {
	ops := []any{float64(2), float64(-1), float64(-1), "cde"}
	o, err := TextOperationFromJSON(map[string]any{"textOperation": ops})
	if err != nil {
		t.Fatalf("fromJSON: %v", err)
	}
	assertEqualT(t, len(o.Ops), 3, "ops length")
	assertEqualT(t, o.BaseLength, 4, "baseLength")
	assertEqualT(t, o.TargetLength, 5, "targetLength")

	// Invalid tail ops must fail the round-trip (vendor assertIncorrectAfter).
	bad1 := []any{float64(2), float64(-1), map[string]any{"insert": "x"}}
	if _, err := TextOperationFromJSON(map[string]any{"textOperation": bad1}); err == nil {
		t.Fatal("expected error for {insert: x}")
	}
	bad2 := []any{nil}
	if _, err := TextOperationFromJSON(map[string]any{"textOperation": bad2}); err == nil {
		t.Fatal("expected error for null op")
	}
}

func TestTextOperation_FromJSON_InvalidInsertNonBMP(t *testing.T) {
	op := []any{"𝌆\n"}
	if _, err := TextOperationFromJSON(map[string]any{"textOperation": op}); err == nil {
		t.Fatal("expected non-BMP insert error")
	}
}

func TestTextOperation_Apply_InvalidBaseLength(t *testing.T) {
	op := NewTextOperation().Retain(1, nil)
	if err := op.Apply(newFuzzFile("", nil)); err == nil {
		t.Fatal("expected base length mismatch error on empty string")
	}
	if err := op.Apply(newFuzzFile(" ", nil)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTextOperation_Apply_ThrowsNonBMP(t *testing.T) {
	op := NewTextOperation()
	// Vendor Insert panics on non-BMP insertions (InvalidInsertionError).
	assertPanic(t, func() { op.Insert(string(rune(0x1D106)), nil, nil) })
}

func assertPanic(t *testing.T, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic, got none")
		}
	}()
	f()
}

func TestTextOperation_Apply_Random(t *testing.T) {
	for trial := 0; trial < 50; trial++ {
		str := fuzzContent()
		ids, comments := fuzzComments()
		o := randomOp(str, ids, false)
		assertEqualT(t, o.BaseLength, Units(str), "fuzz baseLength")
		file := newFuzzFile(str, comments)
		if err := o.Apply(file); err != nil {
			t.Fatalf("apply(%d): %v", trial, err)
		}
		if got := Units(file.GetContent()); got != o.TargetLength {
			t.Fatalf("apply(%d): result length %d != target %d", trial, got, o.TargetLength)
		}
	}
}

func TestTextOperation_JSON_RoundTrip(t *testing.T) {
	for trial := 0; trial < 50; trial++ {
		doc := fuzzContent()
		_, comments := fuzzComments()
		operation := randomOp(doc, nil, false)
		roundTrip, err := TextOperationFromJSON(operation.ToRaw())
		if err != nil {
			t.Fatalf("roundtrip fromJSON(%d): %v", trial, err)
		}
		if !operation.Equals(roundTrip) {
			t.Fatalf("roundtrip(%d) not equal", trial)
		}
		_ = comments
	}
}

func TestTextOperation_Invert_Random(t *testing.T) {
	for trial := 0; trial < 50; trial++ {
		// Per-trial seed so a failing trial is reproducible in CI.
		r := rand.New(rand.NewSource(int64(0xE5E5 + trial))) // #nosec G404 -- deterministic test
		old := randState
		randState = r
		defer func() {}() // (no-op; state restored per-trial below)

		str := fuzzContent()
		ids, comments := fuzzComments()
		o := randomOp(str, ids, false)
		original := newFuzzFile(str, comments)
		assertEqualT(t, o.BaseLength, Units(str), "fuzz invert baseLength")
		p := o.Invert(original)
		assertEqualT(t, o.BaseLength, p.(*TextOperation).TargetLength, "invert base/target swap (base)")
		assertEqualT(t, o.TargetLength, p.(*TextOperation).BaseLength, "invert base/target swap (target)")

		if err := o.Apply(original); err != nil {
			t.Fatalf("apply(%d): %v", trial, err)
		}
		if err := p.(EditOperation).Apply(original); err != nil {
			t.Fatalf("inverse apply(%d): %v", trial, err)
		}
		got := original.GetContent()
		if got != str {
			// First difference index (in units) for bisection.
			i := 0
			for i < Units(str) && i < Units(got) {
				if got[i:] != str[i:] {
					break
				}
				i++
			}
			t.Fatalf("invert(%d): content not restored; first differ at unit %d. len(got)=%d len(str)=%d\nops(o) = %s\nops(inv) = %s\n  got = %q\n str = %q",
				trial, i, Units(got), Units(str), o.String(), p.(*TextOperation).String(), got, str)
		}
		randState = old
	}
}

func TestTextOperation_Compose_Random(t *testing.T) {
	for trial := 0; trial < 50; trial++ {
		str := fuzzContent()
		_, comments := fuzzComments()
		a := randomOp(str, nil, false)
		file := newFuzzFile(str, comments)
		if err := a.Apply(file); err != nil {
			t.Fatalf("apply a(%d): %v", trial, err)
		}
		afterA := file.GetContent()
		assertEqualT(t, Units(afterA), a.TargetLength, "a target")
		b := randomOp(afterA, nil, false)
		if err := b.Apply(file); err != nil {
			t.Fatalf("apply b(%d): %v", trial, err)
		}
		afterB := file.GetContent()
		composed, err := a.Compose(b)
		if err != nil {
			t.Fatalf("compose(%d): %v", trial, err)
		}
		assertEqualT(t, composed.(*TextOperation).TargetLength, b.TargetLength, "compose target")
		composedFile := newFuzzFile(str, comments)
		if err := composed.(EditOperation).Apply(composedFile); err != nil {
			t.Fatalf("apply composed(%d): %v", trial, err)
		}
		if composedFile.GetContent() != afterB {
			t.Fatalf("compose(%d): composed content != a-then-b content", trial)
		}
	}
}

func TestTextOperation_Compose_RejectsDifferentBase(t *testing.T) {
	_ = NewTextOperation().Retain(4, nil)
	a := NewTextOperation().Retain(4, nil)
	b := NewTextOperation().Retain(7, nil)
	if _, err := a.Compose(b); err == nil {
		t.Fatal("expected UnprocessableError composing mismatched lengths")
	}
}

func TestTextOperation_Compose_AssociativityTimestamp(t *testing.T) {
	// Vendor 'compose associativity does not handle timestamps'.
	str := "AB"
	a, err := TextOperationFromJSON(map[string]any{
		"textOperation": []any{
			map[string]any{"r": float64(2), "tracking": map[string]any{"type": "delete", "userId": "user1", "ts": "2023-01-01T00:00:00.000Z"}},
		},
	})
	if err != nil {
		t.Fatalf("a fromJSON: %v", err)
	}
	b, err := TextOperationFromJSON(map[string]any{
		"textOperation": []any{
			map[string]any{"r": float64(1), "tracking": map[string]any{"type": "delete", "userId": "user1", "ts": "2022-01-01T00:00:00.000Z"}},
			float64(1),
		},
	})
	if err != nil {
		t.Fatalf("b fromJSON: %v", err)
	}
	c, err := TextOperationFromJSON(map[string]any{
		"textOperation": []any{float64(1), "X", float64(1)},
	})
	if err != nil {
		t.Fatalf("c fromJSON: %v", err)
	}
	ab, err := a.Compose(b)
	if err != nil {
		t.Fatalf("ab: %v", err)
	}
	ab_c, err := ab.(*TextOperation).Compose(c)
	if err != nil {
		t.Fatalf("ab_c: %v", err)
	}
	bc, err := b.Compose(c)
	if err != nil {
		t.Fatalf("bc: %v", err)
	}
	a_bc, err := a.Compose(bc)
	if err != nil {
		t.Fatalf("a_bc: %v", err)
	}
	abFile := newFuzzFile(str, nil)
	if err := ab_c.(EditOperation).Apply(abFile); err != nil {
		t.Fatalf("apply ab_c: %v", err)
	}
	aBcFile := newFuzzFile(str, nil)
	if err := a_bc.(EditOperation).Apply(aBcFile); err != nil {
		t.Fatalf("apply a_bc: %v", err)
	}
	if abFile.GetContent() != aBcFile.GetContent() {
		t.Fatalf("associativity content mismatch: %q vs %q", abFile.GetContent(), aBcFile.GetContent())
	}
}

func TestTextOperation_Transform_Random(t *testing.T) {
	for trial := 0; trial < 50; trial++ {
		str := fuzzContent()
		_, comments := fuzzComments()
		a := randomOp(str, nil, false)
		b := randomOp(str, nil, false)
		aPrime, bPrime, err := Transform(a, b)
		if err != nil {
			t.Fatalf("transform(%d): %v", trial, err)
		}
		abPrime, err := a.Compose(bPrime)
		if err != nil {
			t.Fatalf("compose a b'(%d): %v", trial, err)
		}
		baPrime, err := b.Compose(aPrime)
		if err != nil {
			t.Fatalf("compose b a'(%d): %v", trial, err)
		}
		abFile := newFuzzFile(str, comments)
		if err := abPrime.(EditOperation).Apply(abFile); err != nil {
			t.Fatalf("apply ab'(%d): %v", trial, err)
		}
		baFile := newFuzzFile(str, comments)
		if err := baPrime.(EditOperation).Apply(baFile); err != nil {
			t.Fatalf("apply ba'(%d): %v", trial, err)
		}
		if abFile.GetContent() != baFile.GetContent() {
			t.Fatalf("transform(%d): ab' content %q != ba' content %q", trial, abFile.GetContent(), baFile.GetContent())
		}
	}
}

func TestTextOperation_Transform_RejectsDifferentBase(t *testing.T) {
	_ = NewTextOperation()
	a := NewTextOperation().Retain(4, nil)
	b := NewTextOperation().Retain(7, nil)
	if _, _, err := Transform(a, b); err == nil {
		t.Fatal("expected UnprocessableError transforming mismatched lengths")
	}
}

// TestTextOperation_Transform_ChosenTimestamp mirrors vendor
// 'chooses lower tracked change timestamp'.
func TestTextOperation_Transform_ChosenTimestamp(t *testing.T) {
	str := "abcde"
	a, err := TextOperationFromJSON(map[string]any{
		"textOperation": []any{
			map[string]any{"r": float64(2), "tracking": map[string]any{"type": "insert", "userId": "user1", "ts": "2024-01-01T01:00:00.000Z"}},
			float64(1),
			map[string]any{"r": float64(2), "tracking": map[string]any{"type": "insert", "userId": "user1", "ts": "2024-01-01T02:00:00.000Z"}},
		},
	})
	if err != nil {
		t.Fatalf("a: %v", err)
	}
	b, err := TextOperationFromJSON(map[string]any{"textOperation": []any{float64(1), float64(-3), float64(1)}})
	if err != nil {
		t.Fatalf("b: %v", err)
	}
	_, bPrime, err := Transform(a, b)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	abPrime, err := a.Compose(bPrime)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	file := newFuzzFile(str, nil)
	if err := abPrime.(EditOperation).Apply(file); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := file.TrackedChanges.Len(); got != 1 {
		t.Fatalf("transform ts: trackedChanges len %d != 1", got)
	}
}

func TestTextOperation_Compose_Comments(t *testing.T) {
	// Vendor 'composes two operations with comments': after op1 then c, the
	// file content is unchanged and the comment range is untouched.
	file, err := StringFileDataFromRaw(map[string]any{
		"content":  "foo baz",
		"comments": []any{map[string]any{"id": "comment1", "ranges": []any{}}},
	})
	if err != nil {
		t.Fatalf("fromRaw: %v", err)
	}
	op1 := NewTextOperation().Retain(4, nil).Insert("bar", NewTracking("insert", "user1", 1704067200000), []string{"comment1"}).Insert(" ", nil, nil).Retain(3, nil)
	assertEqualT(t, op1.BaseLength, 7, "op1 base length")
	assertEqualT(t, op1.TargetLength, 11, "op1 target length")
	c := NewTextOperation().Retain(4, nil).Remove(len("bar ")).Retain(3, nil)
	assertBoolT(t, op1.CanBeComposedWith(c), true, "op1 (target 11) composes with c (base 11)")
	if err := op1.Apply(file); err != nil {
		t.Fatalf("apply op1: %v", err)
	}
	if err := c.Apply(file); err != nil {
		t.Fatalf("apply c: %v", err)
	}
	assertEqualT(t, file.GetContent(), "foo baz", "content restored")
	raw := file.Comments.ToRaw()
	if len(raw) != 1 || raw[0]["id"] != "comment1" {
		t.Fatalf("comments not preserved: %v", raw)
	}
}

func TestTextOperation_Insert_Utf16Length(t *testing.T) {
	// Supplementary char is 2 UTF-16 units; inserting it is rejected by
	// Insert (non-BMP), but retaining over it must count 2 units.
	str := "a\U00010000b" // 4 UTF-16 units (a + surrogate pair + b)
	op := NewTextOperation()
	assertEqualT(t, Units(str), 4, "retain supplementary length")
	op.Retain(4, nil)
	file := newFuzzFile(str, nil)
	if err := op.Apply(file); err != nil {
		t.Fatalf("apply: %v", err)
	}
	assertEqualT(t, file.GetContent(), str, "retain supplementary content")
}

func TestTextOperation_ApplyToLength(t *testing.T) {
	op := NewTextOperation()
	op.Retain(4, nil).Remove(2).Insert("x", nil, nil)
	assertEqualT(t, op.BaseLength, 6, "applyToLength base is 6 (cursor over remove)")
	n, err := op.ApplyToLength(6)
	if err != nil {
		t.Fatalf("applyToLength: %v", err)
	}
	assertEqualT(t, n, op.TargetLength, "applyToLength == targetLength")
}
