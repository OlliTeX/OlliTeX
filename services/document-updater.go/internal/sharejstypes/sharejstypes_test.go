package sharejstypes

import (
	"document-updater/internal/sharejstext"
	"testing"
)

// simple — oracle values verified against Node `simple.js` via `node -e`.
func TestSimpleCreate(t *testing.T) {
	if got := (Simple{}).Create().(SimpleSnapshot); got.Str != "" {
		t.Fatalf("create must be empty, got %v", got)
	}
}

func TestSimpleApply(t *testing.T) {
	var s Simple
	v, err := s.Apply(SimpleSnapshot{Str: "abcdef"}, SimpleOp{Position: 2, Text: "XY"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := v.(SimpleSnapshot); got.Str != "abXYcdef" {
		t.Fatalf("want abXYcdef, got %q", got.Str)
	}
	if _, err := s.Apply(SimpleSnapshot{Str: "ab"}, SimpleOp{Position: 9, Text: "x"}); err == nil || err.Error() != "Invalid position" {
		t.Fatalf("want 'Invalid position' error, got %v", err)
	}
	if _, err := s.Apply(SimpleSnapshot{}, SimpleOp{Position: -1, Text: "x"}); err == nil ||
		err.Error() != "Invalid position" {
		t.Fatalf("negative position must fail, got %v", err)
	}
}

func TestSimpleTransform(t *testing.T) {
	var s Simple
	run := func(op1, op2 SimpleOp, side string) SimpleOp {
		v, err := s.Transform(op1, op2, side)
		if err != nil {
			t.Fatalf("transform: %v", err)
		}
		return v.(SimpleOp)
	}
	if r := run(SimpleOp{2, "a"}, SimpleOp{1, "bc"}, "left"); r.Position != 4 {
		t.Fatalf("left insert before: want 4, got %d", r.Position)
	}
	if r := run(SimpleOp{2, "a"}, SimpleOp{1, "bc"}, "right"); r.Position != 4 {
		t.Fatalf("right insert before: want 4, got %d", r.Position)
	}
	if r := run(SimpleOp{2, "a"}, SimpleOp{2, "bc"}, "left"); r.Position != 4 {
		t.Fatalf("left same position: want 4 (left wins the tie), got %d", r.Position)
	}
	if r := run(SimpleOp{2, "a"}, SimpleOp{2, "bc"}, "right"); r.Position != 2 {
		t.Fatalf("right same position: want 2, got %d", r.Position)
	}
	if r := run(SimpleOp{2, "a"}, SimpleOp{5, "bc"}, "left"); r.Position != 2 {
		t.Fatalf("insert after: want 2, got %d", r.Position)
	}
}

// count — oracle values verified against Node `count.js` via `node -e`.
func TestCountCreate(t *testing.T) {
	var c Count
	if got := c.Create(); got != 1 {
		t.Fatalf("create: want 1, got %v", got)
	}
}

func TestCountApply(t *testing.T) {
	var c Count
	out, err := c.Apply(1, []int{1, 2})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := out.(int); got != 3 {
		t.Fatalf("want 3, got %d", got)
	}
	if _, err := c.Apply(1, []int{9, 2}); err == nil ||
		err.Error() != "Op 9 != snapshot 1" {
		t.Fatalf("want 'Op 9 != snapshot 1', got %v", err)
	}
}

func TestCountTransform(t *testing.T) {
	var c Count
	out, err := c.Transform([]int{1, 2}, []int{1, 4}, "left")
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if got := out.([]int); got[0] != 5 || got[1] != 2 {
		t.Fatalf("want [5 2], got %v", got)
	}
	if _, err := c.Transform([]int{1, 2}, []int{3, 4}, "left"); err == nil ||
		err.Error() != "Op1 1 != op2 3" {
		t.Fatalf("want 'Op1 1 != op2 3', got %v", err)
	}
}

func TestCountCompose(t *testing.T) {
	var c Count
	out, err := c.Compose([]int{1, 2}, []int{3, 4})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if got := out.([]int); got[0] != 1 || got[1] != 6 {
		t.Fatalf("want [1 6], got %v", got)
	}
	if _, err := c.Compose([]int{1, 2}, []int{5, 9}); err == nil ||
		err.Error() != "Op1 [1,2] + 1 != op2 [5,9]" {
		t.Fatalf("want JS-style compose error, got %v", err)
	}
}

func TestCountGenerateRandomOp(t *testing.T) {
	var c Count
	op, next := c.GenerateRandomOp(5)
	if op[0] != 5 || op[1] != 1 || next != 6 {
		t.Fatalf("want ([[5,1]], 6), got %v %d", op, next)
	}
}

// text — Go-side wrapper over internal/sharejstext (oracle-verified in that
// package). These guards verify the interface plumbing only.
func TestTextTypePlumbing(t *testing.T) {
	var s textType
	if s.Name() != "text" {
		t.Fatalf("name: want text, got %s", s.Name())
	}
	out, err := s.Apply("world", []sharejstext.Component{{I: ptr("X"), P: 0}})
	if err != nil || out.(string) != "Xworld" {
		t.Fatalf("apply: got %v %v", out, err)
	}
	if t2 := s.Create().(string); t2 != "" {
		t.Fatalf("create: want empty string, got %v", t2)
	}
	if v := (Simple{}).Create().(SimpleSnapshot); v.Str != "" {
		t.Fatalf("simple create: want empty str, got %v", v)
	}
	if v := (Count{}).Create().(int); v != 1 {
		t.Fatalf("count create: want 1, got %v", v)
	}
}

func ptr(s string) *string { return &s }

func TestRegistryLookup(t *testing.T) {
	for _, name := range []string{"simple", "count", "text"} {
		if Lookup(name) == nil {
			t.Fatalf("Lookup(%q) must resolve", name)
		}
	}
	// (json will resolve once json.go lands; tracked in the json chunk test.)
	_ = "json-pending"
	if Lookup("nope") != nil {
		t.Fatal("Lookup of unknown name must be nil")
	}
}
