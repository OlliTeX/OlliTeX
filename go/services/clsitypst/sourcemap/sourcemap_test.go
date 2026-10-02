// sourcemap_test.go covers the T16 reader (the total reader per D17):
// sidecar JSON parsing + the offset <-> (line, column) math + nearest-match.
package sourcemap

import (
	"encoding/json"
	"testing"
)

// fixture mirrors a live fork output.sourcemap.json (writer geo).
const fixture = `{
  "typst": "0.15.1+clsi",
  "pages": 2,
  "pageSizes": [[150, 200], [150, 200]],
  "locations": [
    {"span": {"file": "main.typ", "byteOffset": 10}, "page": 0, "x": 10, "y": 60, "w": 40, "h": 12},
    {"span": {"file": "main.typ", "byteOffset": 20}, "page": 0, "x": 10, "y": 90, "w": 30, "h": 12},
    {"span": {"file": "lib.typ", "byteOffset": 5}, "page": 1, "x": 50, "y": 100, "w": 80, "h": 10}
  ]
}`

func TestParseFixture(t *testing.T) {
	s, err := Parse([]byte(fixture))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.Typst != "0.15.1+clsi" {
		t.Fatalf("typst = %q, want 0.15.1+clsi", s.Typst)
	}
	if s.Pages != 2 {
		t.Fatalf("pages = %d, want 2", s.Pages)
	}
	if len(s.Sizes) != 2 || s.Sizes[0][0] != 150 || s.Sizes[0][1] != 200 {
		t.Fatalf("sizes = %v", s.Sizes)
	}
	w, h, ok := s.PageSize(0)
	if !ok || w != 150 || h != 200 {
		t.Fatalf("PageSize(0) = %v %v %v", w, h, ok)
	}
	if _, _, ok := s.PageSize(5); ok {
		t.Fatal("PageSize(5) out of range should be !ok")
	}
	if len(s.Locations) != 3 {
		t.Fatalf("locations = %d, want 3", len(s.Locations))
	}
	if s.Locations[0].Span.File != "main.typ" || s.Locations[0].Span.ByteOffset != 10 {
		t.Fatalf("loc[0] span = %+v", s.Locations[0].Span)
	}
}

func TestParseArms(t *testing.T) {
	if _, err := Parse(nil); err == nil {
		t.Fatal("Parse(nil) must error")
	}
	if _, err := Parse([]byte(`{broken`)); err == nil {
		t.Fatal("Parse(bad json) must error")
	}
	// locations-less body -> Locations empty slice (not nil).
	s, err := Parse([]byte(`{"typst":"x","pages":0,"pageSizes":[]}`))
	if err != nil {
		t.Fatalf("parse locations-less: %v", err)
	}
	if s.Locations == nil || len(s.Locations) != 0 {
		t.Fatalf("Locations should be empty slice, got %v", s.Locations)
	}
}

const testContent = "alpha\nbeta\n"

func TestOffsetAt(t *testing.T) {
	content := testContent
	// line 1 col 1 -> 0
	if o := OffsetAt(content, 1, 1); o != 0 {
		t.Fatalf("L1C1 = %d, want 0", o)
	}
	// line 2 col 1 -> 6 (after "alpha\n")
	if o := OffsetAt(content, 2, 1); o != 6 {
		t.Fatalf("L2C1 = %d, want 6", o)
	}
	// line 2 col 4 -> 6+3 = 9
	if o := OffsetAt(content, 2, 4); o != 9 {
		t.Fatalf("L2C4 = %d, want 9", o)
	}
	// column beyond end saturates to 9 (end of beta, last col index = offset 9)
	if o := OffsetAt(content, 2, 99); o != 9 {
		t.Fatalf("L2C99 = %d, want line end 9", o)
	}
	// line beyond end -> end of content
	if o := OffsetAt(content, 99, 1); o != int64(len(content)) {
		t.Fatalf("L99C1 = %d, want len", o)
	}
	// degenerate 0s
	if o := OffsetAt(content, 0, 0); o != 0 {
		t.Fatalf("L0C0 = %d, want 0", o)
	}
}

func TestLineColAt(t *testing.T) {
	const content = "alpha\nbeta\n"
	if l, c := LineColAt(content, 0); l != 1 || c != 1 {
		t.Fatalf("offset 0 = %d,%d, want 1,1", l, c)
	}
	if l, c := LineColAt(content, 6); l != 2 || c != 1 {
		t.Fatalf("offset 6 = %d,%d, want 2,1", l, c)
	}
	if l, c := LineColAt(content, 9); l != 2 || c != 4 {
		t.Fatalf("offset 9 = %d,%d, want 2,4", l, c)
	}
	if l, c := LineColAt(content, int64(len(content))); l != 3 || c != 1 {
		t.Fatalf("offset end = %d,%d, want 3,1", l, c)
	}
	if l, c := LineColAt(content, -5); l != 1 || c != 1 {
		t.Fatalf("offset -5 = %d,%d, want 1,1 (saturate)", l, c)
	}
	if l, c := LineColAt(content, 99); l != 3 || c != 1 {
		t.Fatalf("offset 99 = %d,%d, want 3,1 (saturate)", l, c)
	}
}

func TestRoundTrip(t *testing.T) {
	content := testContent
	for _, lc := range [][2]int{{1, 1}, {1, 5}, {2, 1}, {2, 4}} {
		line, col := lc[0], lc[1]
		o := OffsetAt(content, line, col)
		if o < 0 || o > int64(len(content)) {
			t.Fatalf("round-trip L%dC%d out of range: %d", line, col, o)
		}
		// offset -> line should agree on the line, column == col-or-saturated
		l, c := LineColAt(content, o)
		if l != line || c != col {
			t.Fatalf("round-trip L%dC%d -> offset %d -> L%dC%d mismatch", line, col, o, l, c)
		}
	}
}

func TestNearestByOffset(t *testing.T) {
	s, err := Parse([]byte(fixture))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// offset 10 -> loc[0] (byteOffset 10, file main.typ)
	if loc := NearestByOffset(s.Locations, "main.typ", 10); loc == nil || loc.Span.ByteOffset != 10 {
		t.Fatalf("NearestByOffset(10) = %+v, want byteOffset 10", loc)
	}
	// offset 15 -> between 10 and 20 -> nearer to 10
	if loc := NearestByOffset(s.Locations, "main.typ", 15); loc == nil || loc.Span.ByteOffset != 10 {
		t.Fatalf("NearestByOffset(15) = %+v, want byteOffset 10", loc)
	}
	// offset 16 -> nearer to 20
	if loc := NearestByOffset(s.Locations, "main.typ", 16); loc == nil || loc.Span.ByteOffset != 20 {
		t.Fatalf("NearestByOffset(16) = %+v, want byteOffset 20", loc)
	}
	// offset in a different file -> loc for lib.typ
	if loc := NearestByOffset(s.Locations, "lib.typ", 4); loc == nil || loc.Span.File != "lib.typ" {
		t.Fatalf("NearestByOffset(lib.typ,4) = %+v, want lib.typ", loc)
	}
	// file not in locations -> nil
	if loc := NearestByOffset(s.Locations, "nope.typ", 1); loc != nil {
		t.Fatalf("NearestByOffset(nope.typ) = %+v, want nil", loc)
	}
	// empty locations -> nil
	if loc := NearestByOffset([]Location{}, "main.typ", 1); loc != nil {
		t.Fatalf("NearestByOffset empty = %+v, want nil", loc)
	}
}

func TestNearestByPoint(t *testing.T) {
	s, err := Parse([]byte(fixture))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// page 0 (0-based), point (10, 15) -> loc[0] (box at y 60) vs loc[1] (y 90)
	//   loc[0] center: (30, 66); loc[1] center: (25, 96)
	//   distance from (10,15): loc[0] -> (20)^2+(51)^2, loc[1] -> (15)^2+(81)^2
	//   loc[0] nearer
	if loc := NearestByPoint(s.Locations, 0, 10, 15); loc == nil || loc.Y != 60 {
		t.Fatalf("NearestByPoint(p0, 10, 15) = %+v, want y 60", loc)
	}
	// page 0, a point inside box1 (y 90, center y 96) -> loc[1]
	if loc := NearestByPoint(s.Locations, 0, 20, 95); loc == nil || loc.Y != 90 {
		t.Fatalf("NearestByPoint -> y 90, got %+v", loc)
	}
	// page 1 -> loc[2]
	if loc := NearestByPoint(s.Locations, 1, 55, 105); loc == nil || loc.Page != 1 {
		t.Fatalf("NearestByPoint(p1) = %+v, want page 1", loc)
	}
	// page with no locations -> nil
	if loc := NearestByPoint(s.Locations, 5, 1, 1); loc != nil {
		t.Fatalf("NearestByPoint no-locations = %+v, want nil", loc)
	}
	// empty -> nil
	if loc := NearestByPoint([]Location{}, 0, 1, 1); loc != nil {
		t.Fatalf("NearestByPoint empty = %+v, want nil", loc)
	}
}

func TestErrorString(t *testing.T) {
	e := &Error{Message: "boom"}
	if e.Error() != "boom" {
		t.Fatalf("Error = %q", e.Error())
	}
}

func TestFixtureJSONMatchesLiveSchema(t *testing.T) {
	// A real sidecar (writer emitted by the fork) has these exact top-level
	// keys; make sure the fixture is structurally valid (guards against a
	// future drift where the live writer and the reader disagree).
	var live map[string]any
	if err := json.Unmarshal([]byte(fixture), &live); err != nil {
		t.Fatalf("fixture json: %v", err)
	}
	for _, k := range []string{"typst", "pages", "pageSizes", "locations"} {
		if _, ok := live[k]; !ok {
			t.Fatalf("fixture missing key %q", k)
		}
	}
}
