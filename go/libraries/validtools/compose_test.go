package validtools

import "testing"

// compose_test.go — ArrayVal / UnionVal (added for the LIB-06 rangestracker
// schemas; framework values, not Node API — see HANDOFF §Decisions).

func TestArrayValArms(t *testing.T) {
	s := NewStrictObject(Field{Name: "items", Schema: ArrayVal{Item: StringVal{}}})

	okVal, iss := s.Validate(true, map[string]any{"items": []any{"a", "b"}})
	if len(iss) != 0 {
		t.Fatalf("issues = %v", iss)
	}
	got, _ := okVal.(map[string]any)["items"].([]any)
	if len(got) != 2 || got[0] != "a" {
		t.Fatalf("items = %v", got)
	}

	// absent / null / wrong type
	_, iss = s.Validate(true, map[string]any{})
	if len(iss) == 0 || !containsIssueCode(iss, "invalid_type") {
		t.Fatalf("absent: %v", iss)
	}
	_, iss = s.Validate(true, map[string]any{"items": nil})
	if len(iss) == 0 || !containsIssueCode(iss, "invalid_type") {
		t.Fatalf("null: %v", iss)
	}
	_, iss = s.Validate(true, map[string]any{"items": "nope"})
	if len(iss) == 0 || !containsIssueCode(iss, "invalid_type") {
		t.Fatalf("wrong type: %v", iss)
	}
}

func TestArrayValNestedElementIssues(t *testing.T) {
	inner := NewStrictObject(Field{Name: "q", Schema: StringVal{}})
	arr := ArrayVal{Item: inner}
	_, iss := arr.Validate(true, []any{map[string]any{"q": 1}})
	if len(iss) != 1 || iss[0].Code != "invalid_type" {
		t.Fatalf("issues = %v", iss)
	}
	if iss[0].Path[0].Value != "0" || iss[0].Path[1].Value != "q" {
		t.Fatalf("path = %v, want 0.q", iss[0].Path)
	}
}

func TestUnionValArms(t *testing.T) {
	a := NewStrictObject(
		Field{Name: "x", Schema: StringVal{}},
		Field{Name: "p", Schema: NumberIntNonnegative()},
	)
	b := NewStrictObject(
		Field{Name: "y", Schema: StringVal{}},
		Field{Name: "p", Schema: NumberIntNonnegative()},
	)
	u := NewUnion(a, b)

	// first arm clean
	val, iss := u.Validate(true, map[string]any{"x": "hi", "p": 1})
	if len(iss) != 0 {
		t.Fatalf("arm A: %v", iss)
	}
	if val.(map[string]any)["x"] != "hi" {
		t.Fatalf("val = %v", val)
	}

	// second arm clean
	_, iss = u.Validate(true, map[string]any{"y": "hi", "p": 2})
	if len(iss) != 0 {
		t.Fatalf("arm B: %v", iss)
	}

	// neither clean → single invalid_union issue with both groups
	_, iss = u.Validate(true, map[string]any{"z": 3, "p": 2})
	if len(iss) != 1 || iss[0].Code != "invalid_union" {
		t.Fatalf("union failure: %v", iss)
	}
	if len(iss[0].InvalidUnion) != 2 {
		t.Fatalf("groups = %d, want 2", len(iss[0].InvalidUnion))
	}
}

func TestUnionValSingleArmShortcut(t *testing.T) {
	one := NewStrictObject(Field{Name: "x", Schema: StringVal{}})
	u := NewUnion(one)
	if _, iss := u.Validate(true, map[string]any{"x": "ok"}); len(iss) != 0 {
		t.Fatalf("unexpected issues: %v", iss)
	}
	if _, iss := u.Validate(true, map[string]any{"x": 1}); len(iss) == 0 {
		t.Fatalf("expected issues")
	}
}

func containsIssueCode(iss []Issue, code string) bool {
	for _, i := range iss {
		if i.Code == code {
			return true
		}
	}
	return false
}
