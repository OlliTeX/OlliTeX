package settings

import (
	"errors"
	"reflect"
	"testing"
)

func mustMerge(t *testing.T, overrides, defaults map[string]any) map[string]any {
	t.Helper()
	out, err := Merge(overrides, defaults)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	return out
}

// mergedDeepEqual compares recursively, distinct from reflect.DeepEqual
// because it treats map identity (returned == same defaults map) separately.
func mergedEqual(t *testing.T, got, want map[string]any) {
	t.Helper()
	if !same(got, want) {
		t.Fatalf("merged = %v, want %v", got, want)
	}
}

func same(a, b any) bool {
	am, aok := a.(map[string]any)
	bm, bok := b.(map[string]any)
	if aok || bok {
		if !aok || !bok || len(am) != len(bm) {
			return false
		}
		for k, av := range am {
			bv, ok := bm[k]
			if !ok {
				return false
			}
			if !same(av, bv) {
				return false
			}
		}
		return true
	}
	if al, aok := a.([]any); aok {
		bl, bok := b.([]any)
		if !bok || len(al) != len(bl) {
			return false
		}
		for i := range al {
			if !same(al[i], bl[i]) {
				return false
			}
		}
		return true
	}
	return a == b
}

// mergeOracleCases: (label, override, defaults) -> want merged defaults / wantErr.
// Pin: the live `node merge.js` matrix captured against libraries/settings/merge.js
// (JS null == Go nil as override value; JS undefined == key absent).
func mergeOracleCases(t *testing.T) {
	mk := func(m map[string]any) map[string]any { return m }
	cases := []struct {
		label   string
		ov, def map[string]any
		wantOut map[string]any
		wantErr error
	}{
		{"empty-ov", map[string]any{}, mk(map[string]any{"a": 1}), map[string]any{"a": 1}, nil},
		{"empty-def", map[string]any{"a": 1}, map[string]any{}, map[string]any{"a": 1}, nil},
		{"prim-swap", map[string]any{"a": 1}, mk(map[string]any{"a": 0}), map[string]any{"a": 1}, nil},
		{"str-swap", map[string]any{"a": "x"}, map[string]any{"a": "y"}, map[string]any{"a": "x"}, nil},
		{"nested-ov", map[string]any{"a": map[string]any{"b": 1.0}},
			map[string]any{"a": map[string]any{"b": 0.0, "c": 2.0}},
			map[string]any{"a": map[string]any{"b": 1.0, "c": 2.0}}, nil},
		{"deep", map[string]any{"a": map[string]any{"b": map[string]any{"c": 5.0}}}, map[string]any{"a": map[string]any{"b": map[string]any{"c": 1.0}}}, map[string]any{"a": map[string]any{"b": map[string]any{"c": 5.0}}}, nil},
		{"falsy-slot-0", map[string]any{"a": map[string]any{"b": 1.0}},
			map[string]any{"a": 0.0}, map[string]any{"a": map[string]any{"b": 1.0}}, nil},
		{"falsy-slot-str", map[string]any{"a": map[string]any{"b": 1.0}},
			map[string]any{"a": ""}, map[string]any{"a": map[string]any{"b": 1.0}}, nil},
		{"falsy-slot-false", map[string]any{"a": map[string]any{"b": 1.0}},
			map[string]any{"a": false}, map[string]any{"a": map[string]any{"b": 1.0}}, nil},
		{"falsy-slot-null", map[string]any{"a": map[string]any{"b": 1.0}},
			map[string]any{"a": nil}, map[string]any{"a": map[string]any{"b": 1.0}}, nil},
		{"absent-slot", map[string]any{"a": map[string]any{"b": 1.0}},
			map[string]any{"b": 0.0}, map[string]any{"b": 0.0, "a": map[string]any{"b": 1.0}}, nil},
		{"truthy-prim-slot", map[string]any{"a": map[string]any{"b": 1.0}},
			map[string]any{"a": 5.0}, map[string]any{"a": 5.0}, nil},
		{"truthy-nonzero-str-slot", map[string]any{"a": map[string]any{"b": 1.0}},
			map[string]any{"a": "s"}, map[string]any{"a": "s"}, nil},
		{"array-replace", map[string]any{"a": []any{1.0, 2.0}},
			map[string]any{"a": []any{9.0}}, map[string]any{"a": []any{1.0, 2.0}}, nil},
		{"obj-to-arr", map[string]any{"a": []any{7.0}},
			map[string]any{"a": map[string]any{"x": 1.0}}, map[string]any{"a": []any{7.0}}, nil},
		{"num-swap", map[string]any{"a": 0.5}, map[string]any{"a": 2.0}, map[string]any{"a": 0.5}, nil},
		{"bool-swap", map[string]any{"a": true}, map[string]any{"a": false}, map[string]any{"a": true}, nil},
		{"deep3", map[string]any{"a": map[string]any{"b": map[string]any{"c": map[string]any{"d": 42.0}}}},
			map[string]any{"a": map[string]any{"b": map[string]any{"c": map[string]any{"d": 1.0, "k": 1.0}}}},
			map[string]any{"a": map[string]any{"b": map[string]any{"c": map[string]any{"d": 42.0, "k": 1.0}}}}, nil},
	}
	for _, c := range cases {
		if c.wantErr != nil {
			continue
		}
		got := mustMerge(t, c.ov, c.def)
		if !same(got, c.wantOut) {
			t.Errorf("%s: got %v, want %v", c.label, got, c.wantOut)
		}
	}
}

func TestMergeOracle(t *testing.T) { mergeOracleCases(t) }

// TestMergeNullOverride: a JS-null override value (Go nil) makes the *Node*
// merge throw a raw TypeError; the Go port returns ErrNullOverride once, at the
// first offending (canonical) key, with the dotted path.
func TestMergeNullOverride(t *testing.T) {
	_, err := Merge(map[string]any{"a": nil}, map[string]any{"a": 0.0})
	if !errors.Is(err, ErrNullOverride) {
		t.Fatalf("want ErrNullOverride, got %v", err)
	}
	if PathOf(err) != "a" {
		t.Fatalf("path = %q, want a", PathOf(err))
	}
	_, err = Merge(map[string]any{"a": map[string]any{"b": nil}}, map[string]any{"a": map[string]any{}})
	if !errors.Is(err, ErrNullOverride) || PathOf(err) != "a.b" {
		t.Fatalf("nested: %v path=%q", err, PathOf(err))
	}
	// multiple nulls: reported at the first in canonical order (numeric then
	// lexicographic). Pin, not V8 (V8 iterates keys in first-insertion order).
	_, err = Merge(map[string]any{"b": nil, "a": nil}, map[string]any{"a": 0.0, "b": 0.0})
	if PathOf(err) != "a" {
		t.Fatalf("multi-null path = %q, want a", PathOf(err))
	}
	var n *NullOverrideError
	if !errors.As(err, &n) || n.Path != "a" || n.Unwrap() != ErrNullOverride {
		t.Fatalf("typed error shape: %v", err)
	}
	if n.Error() != "cannot convert undefined or null to object" {
		t.Fatalf("message = %q (must match the Node TypeError verbatim)", n.Error())
	}
	// PathOf on a non-NullOverrideError
	if PathOf(errors.New("x")) != "" {
		t.Fatalf("PathOf(non-null) must be empty")
	}
}

func TestMergeNilArgs(t *testing.T) {
	if _, err := Merge(nil, map[string]any{}); !errors.Is(err, ErrNilOverrides) {
		t.Fatalf("nil overrides: %v", err)
	}
	if _, err := Merge(map[string]any{}, nil); !errors.Is(err, ErrNilDefaults) {
		t.Fatalf("nil defaults: %v", err)
	}
}

func TestMergeReturnsDefaultsIdentity(t *testing.T) {
	def := map[string]any{"a": 1.0}
	got, err := Merge(map[string]any{"b": 2.0}, def)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	// Node returns the SAME `defaults` object. In Go, prove same-reference
	// via reflect (map `!=` is illegal): the returned map must be the same
	// map data as `def`.
	if reflect.ValueOf(got).Pointer() != reflect.ValueOf(def).Pointer() {
		t.Fatalf("returned map is not the SAME defaults map (identity/aliasing contract)")
	}
}

func TestMergePartialApply(t *testing.T) {
	// V8 iterates keys in insertion order and throws mid-fold, leaving earlier
	// applied slots behind: {c:{v:1}} survives. Go applies in canonical
	// (numeric-then-lex) order, so "a" (first) throws before "b" (never
	// applied). The post-throw state is NOT pinned (documented in doc.go
	// adaptation 3); here the Go-observable behavior is pinned.
	def := map[string]any{"a": 0.0, "b": 0.0}
	if _, err := Merge(map[string]any{"a": nil, "b": map[string]any{"v": 1.0}}, def); err == nil {
		t.Fatalf("expected null error")
	}
	if def["b"] == nil {
		t.Fatalf("Go fold throws before 'b' (canonical order) — 'b' never applied")
	}
}

func TestMergeOrderedKeys(t *testing.T) {
	got := orderedKeys(map[string]any{"2": nil, "10": nil, "b": nil, "1": nil, "a": nil, "0": nil})
	want := []string{"0", "1", "2", "10", "a", "b"} // numeric then lexicographic
	if len(got) != len(want) {
		t.Fatalf("orderedKeys = %v, want %v", got, want)
	}
	for i, k := range got {
		if k != want[i] {
			t.Fatalf("orderedKeys = %v, want %v", got, want)
		}
	}
	// jsFalsy arms
	falsyTrue := []any{0, "", false, nil, 0.0}
	for _, v := range falsyTrue {
		if !jsFalsy(v) {
			t.Fatalf("jsFalsy(%v) must be true", v)
		}
	}
	falsyFalse := []any{7.0, "x", map[string]any{}, []any{}, "0"}
	for _, v := range falsyFalse {
		if jsFalsy(v) {
			t.Fatalf("jsFalsy(%v) must be false", v)
		}
	}
}
