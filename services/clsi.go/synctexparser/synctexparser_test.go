package synctexparser

import (
	"math"
	"testing"
)

// The fixture below is the verbatim `synctex view` sample from the
// services/clsi test/unit/js/SynctexOutputParser.test.js contract — a fixed,
// synthetic command output that does not depend on host state.
const viewFixture = `This is SyncTeX command line utility, version 1.5
SyncTeX result begin
Output:/compile/output.pdf
Page:1
x:136.537964
y:661.437561
h:133.768356
v:663.928223
W:343.711060
H:9.962640
before:
offset:-1
middle:
after:
Output:/compile/output.pdf
Page:2
x:178.769592
y:649.482361
h:134.768356
v:651.973022
W:342.711060
H:19.962640
before:
offset:-1
middle:
after:
SyncTeX result end
`

func TestParseViewOutputRecords(t *testing.T) {
	records := ParseViewOutput(viewFixture)
	if len(records) != 2 {
		t.Fatalf("2 records expected, got %d", len(records))
	}
	// record 1
	if got := intOf(t, records[0]["page"]); got != 1 {
		t.Errorf("page[0] = %v, want 1", records[0]["page"])
	}
	if got := floatOf(t, records[0]["h"]); math.Abs(got-133.768356) > 1e-9 {
		t.Errorf("h[0] = %v, want 133.768356", got)
	}
	if got := floatOf(t, records[0]["v"]); math.Abs(got-663.928223) > 1e-9 {
		t.Errorf("v[0] = %v, want 663.928223", got)
	}
	if got := floatOf(t, records[0]["width"]); math.Abs(got-343.711060) > 1e-9 {
		t.Errorf("width[0] = %v, want 343.711060", got)
	}
	if got := floatOf(t, records[0]["height"]); math.Abs(got-9.962640) > 1e-9 {
		t.Errorf("height[0] = %v, want 9.962640", got)
	}
	// y and x are intentionally NOT mapped (view only maps Page/h/v/W/H).
	if _, has := records[0]["y"]; has {
		t.Errorf("view records must NOT include y; got %#v", records[0])
	}
	if _, has := records[0]["x"]; has {
		t.Errorf("view records must NOT include x; got %#v", records[0])
	}
	// record 2
	if got := intOf(t, records[1]["page"]); got != 2 {
		t.Errorf("page[1] = %v, want 2", records[1]["page"])
	}
	// empty-value lines (before:/middle:/after:) must NOT set anything (no key set: before/offset/middle/after aren't in the switch).
	for _, k := range []string{"before", "middle", "after", "offset"} {
		if _, has := records[0][k]; has {
			t.Errorf("unexpected key %q in record", k)
		}
	}
}

func TestParseViewOutputGarbage(t *testing.T) {
	if got := ParseViewOutput("This computer is on strike!"); len(got) != 0 {
		t.Errorf("garbage => %d records, want 0", len(got))
	}
}

func TestParseViewOutputBeforeFirstOutputDropped(t *testing.T) {
	// A Page: line before any Output: line is in no record => ignored.
	out := "Page:99\nOutput:x\nPage:5\n"
	rec := ParseViewOutput(out)
	if len(rec) != 1 {
		t.Fatalf("1 record expected, got %d", len(rec))
	}
	if got := intOf(t, rec[0]["page"]); got != 5 {
		t.Errorf("page = %v, want 5", rec[0]["page"])
	}
}

func TestParseEditOutput(t *testing.T) {
	out := "Output:/x\nInput:/home/u/proj/main.tex\nLine:10\nColumn:5\n"
	rec := ParseEditOutput(out, "/home/u/proj")
	if len(rec) != 1 {
		t.Fatalf("1 record expected, got %d", len(rec))
	}
	if got := strOf(t, rec[0]["file"]); got != "main.tex" {
		t.Errorf("file = %q, want %q (relative from baseDir)", got, "main.tex")
	}
	if got := intOf(t, rec[0]["line"]); got != 10 {
		t.Errorf("line = %v, want 10", rec[0]["line"])
	}
	if got := intOf(t, rec[0]["column"]); got != 5 {
		t.Errorf("column = %v, want 5", rec[0]["column"])
	}
}

func TestParseEditOutputRelativeInputKept(t *testing.T) {
	out := "Output:/x\nInput:rel/main.tex\n"
	rec := ParseEditOutput(out, "/base")
	if got := strOf(t, rec[0]["file"]); got != "rel/main.tex" {
		t.Errorf("relative Input must be verbatim, got %q", got)
	}
}

func TestPosixRelEqualIsEmpty(t *testing.T) {
	if got := posixRel("/a", "/a"); got != "" {
		t.Errorf("posixRel equal => %q, want empty", got)
	}
	if got := posixRel("", "/x/y"); got != "x/y" {
		t.Errorf("posixRel base=/ target=/x/y => %q, want x/y", got)
	}
}

func TestPosixRelDown(t *testing.T) {
	if got := posixRel("/a/b", "/a/b/c"); got != "c" {
		t.Errorf("down: %q, want c", got)
	}
	if got := posixRel("/a/b", "/a/other"); got != "../other" {
		t.Errorf("sibling escape: %q, want ../other", got)
	}
	if got := posixRel("/a/b/c", "/a/d/e"); got != "../../d/e" {
		t.Errorf("deep escape: %q, want ../../d/e", got)
	}
	// node: path.relative('/a', '/') === '..'  (one up, not empty).
	if got := posixRel("/a", "/"); got != ".." {
		t.Errorf("target=/ => %q, want ..", got)
	}
}

func intOf(t *testing.T, v any) int {
	n, ok := v.(int) // parseIntJS returns Go int
	if !ok {
		t.Fatalf("value not int: %#v (%T)", v, v)
	}
	return n
}

func floatOf(t *testing.T, v any) float64 {
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("value not float64: %#v (%T)", v, v)
	}
	return f
}

func strOf(t *testing.T, v any) string {
	s, ok := v.(string)
	if !ok {
		t.Fatalf("value not string: %#v (%T)", v, v)
	}
	return s
}
