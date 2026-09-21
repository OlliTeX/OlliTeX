package validtools

import (
	"fmt"
	"strings"
	"testing"
)

func TestWire(t *testing.T) {
	if got := Wire(nil); got != "Validation error" {
		t.Fatalf("Wire(nil) = %q, want %q", got, "Validation error")
	}
	if got := Wire([]Issue{NewIssue("custom", "invalid Mongo ObjectId").At(StrSeg("id"))}); got != `Validation error: Invalid Mongo ObjectId at "id"` {
		t.Fatalf("Wire one = %q", got)
	}
	iss := []Issue{
		NewIssue("invalid_type", "Invalid input: expected string, received number"),
		NewIssue("too_small", "Too small: expected number to be >=0"),
	}
	want := "Validation error: Invalid input: expected string, received number; Too small: expected number to be >=0"
	if got := Wire(iss); got != want {
		t.Fatalf("Wire two = %q, want %q", got, want)
	}
}

func TestMapIssueFormats(t *testing.T) {
	cases := []struct {
		iss  Issue
		want string
	}{
		{NewIssue("invalid_type", "Invalid input: expected number, received string"), "Invalid input: expected number, received string"},
		{NewIssue("invalid_format", "Invalid ISO datetime").At(StrSeg("dt")), `Invalid ISO datetime at "dt"`},
		{NewIssue("invalid_format", "no path").At(IndexSeg(0)), "No path at index 0"},
		{NewIssue("custom", "path is empty").At(StrSeg("path")), `Path is empty at "path"`},
	}
	for i, c := range cases {
		if got := mapIssue(c.iss); got != c.want {
			t.Fatalf("case %d: mapIssue = %q, want %q", i, got, c.want)
		}
	}
}

func TestUnrecognizedKeysWire(t *testing.T) {
	iss := NewUnrecognizedKeysIssue([]string{"extra"})
	if got := mapIssue(iss); got != `Unrecognized key: "extra"` {
		t.Fatalf("root: %q", got)
	}
	deep := NewUnrecognizedKeysIssue([]string{"a", "b"}).At(StrSeg("body"))
	if got := mapIssue(deep); got != `Unrecognized keys: "a", "b" at "body"` {
		t.Fatalf("deep: %q", got)
	}
}

func TestJoinPath(t *testing.T) {
	cases := []struct {
		path []PathSeg
		want string
	}{
		{nil, ""},
		{[]PathSeg{StrSeg("name")}, "name"},
		{[]PathSeg{StrSeg("")}, `""`},
		// Single segment renders raw, identifier or not.
		{[]PathSeg{StrSeg(`a"b`)}, `a"b`},
		{[]PathSeg{StrSeg("body"), StrSeg("id")}, "body.id"},
		{[]PathSeg{StrSeg("a"), IndexSeg(3)}, "a[3]"},
		{[]PathSeg{StrSeg(`a"b`), StrSeg("c")}, `["a\"b"].c`},
		{[]PathSeg{StrSeg("a-b"), StrSeg("c")}, "a-b.c"}, // contains ID_Start chars
		{[]PathSeg{StrSeg("0"), StrSeg("c")}, `["0"].c`}, // none at all
		{[]PathSeg{StrSeg("_x"), StrSeg("c")}, "_x.c"},
		{[]PathSeg{StrSeg("αβ"), StrSeg("c")}, "αβ.c"}, // Greek letters are ID_Start
	}
	for i, c := range cases {
		if got := joinPath(c.path); got != c.want {
			t.Fatalf("joinPath case %d (%v) = %q, want %q", i, c.path, got, c.want)
		}
	}
}

func TestMapIssueUnion(t *testing.T) {
	// The union issue carries the object-key path; member sub-issue paths
	// are empty (probed: zod emits union issues above empty sub-issue paths).
	two := NewUnionIssue([][]Issue{
		{NewIssue("invalid_type", "Invalid input: expected date, received null")},
		{NewIssue("invalid_type", "Invalid input: expected string, received null")},
	}, StrSeg("dt"))
	want := `Invalid input: expected date, received null at "dt" or Invalid input: expected string, received null at "dt"`
	if got := mapIssue(two); got != want {
		t.Fatalf("union two: %q, want %q", got, want)
	}

	four := NewUnionIssue([][]Issue{
		{NewIssue("invalid_type", "Invalid input: expected date, received undefined")},
		{NewIssue("invalid_type", "Invalid input: expected string, received undefined")},
		{NewIssue("invalid_type", "Invalid input: expected null, received undefined")},
		{NewIssue("invalid_type", "Invalid input: expected undefined, received undefined")},
	}, StrSeg("dt"))
	want = `Invalid input: expected date, received undefined at "dt" or ` +
		`Invalid input: expected string, received undefined at "dt" or ` +
		`Invalid input: expected null, received undefined at "dt" or ` +
		`Invalid input: expected undefined, received undefined at "dt"`
	if got := mapIssue(four); got != want {
		t.Fatalf("union four: %q, want %q", got, want)
	}

	// Dedupe: identical member texts render once.
	dup := NewUnionIssue([][]Issue{
		{NewIssue("invalid_type", "Invalid input: expected string, received null")},
		{NewIssue("invalid_type", "Invalid input: expected string, received null")},
	}, StrSeg("dt"))
	if got := mapIssue(dup); got != `Invalid input: expected string, received null at "dt"` {
		t.Fatalf("union dedup: %q", got)
	}
}

func TestTitleCase(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"a", "A"},
		{"invalid Mongo ObjectId", "Invalid Mongo ObjectId"},
		{"Invalid input", "Invalid input"},
		{"too small: expected number", "Too small: expected number"},
		{"Unrecognized key", "Unrecognized key"},
	}
	for _, c := range cases {
		if got := titleCase(c.in); got != c.want {
			t.Fatalf("titleCase(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWireMaxIssues(t *testing.T) {
	issues := make([]Issue, 101)
	for i := range issues {
		issues[i] = NewIssue("custom", fmt.Sprintf("issue %d", i))
	}
	parts := make([]string, 99)
	for i := 0; i < 99; i++ {
		parts[i] = fmt.Sprintf("Issue %d", i)
	}
	want := "Validation error: " + strings.Join(parts, "; ")
	if got := Wire(issues); got != want {
		t.Fatalf("Wire 101 issues = %q\nwant  %q", got, want)
	}
	if got := Wire(issues[:99]); got != want {
		t.Fatalf("Wire 99 issues mismatch")
	}
}

func TestZodErrorError(t *testing.T) {
	z := NewZodError([]Issue{NewIssue("custom", "invalid Mongo ObjectId").At(StrSeg("id"))})
	if got := z.Error(); got != `Validation error: Invalid Mongo ObjectId at "id"` {
		t.Fatalf("ZodError.Error() = %q", got)
	}
}
