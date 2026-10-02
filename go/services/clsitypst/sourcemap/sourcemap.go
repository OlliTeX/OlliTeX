// Package sourcemap reads the T15 fork `output.sourcemap.json` sidecar and
// does the coordinate math the clsi sync routes need (D21 part 2, T16).
//
// The sidecar is written by the fork during `typst compile` (see
// images/typst-amd64/patches/0001-clsi-sourcemap.patch) and lives NEXT TO
// the compiled PDF (writer geo). Schema (serde_json, one object):
//
//	{
//	  "typst":      "0.15.1+clsi",
//	  "pages":      <int>,                                  // page count
//	  "pageSizes":  [[w, h], ...],                          // per-page, user space
//	  "locations":  [ {"span": {"file": string,
//	                        "byteOffset": int64},
//	                  "page": int,                          // 0-based
//	                  "x","y","w","h": float } ]            // PDF user space
//	}
//
// Coordinates are PDF user space (points, origin at the page's bottom-left,
// y up) — the SAME basis the clsi synctex wire uses (the frontend
// highlights.ts flips v with pageHeight - v; it treats v as the bottom edge
// measured from the page bottom). NO flip is applied: the sidecar values map
// 1:1 onto the clsi view-record h/v (h = left, v = bottom, w, h).
//
// Page basis: the wire "page" is 1-based (the frontend sends
// `position.page + 1`); the sidecar "page" is 0-based. The reader exposes
// both; the caller (compilemanager) bridges them.
package sourcemap

import "encoding/json"

// Location is one captured text run placed on a page (sidecar `locations`
// element). Span is the source span; byteOffset is the byte offset of the
// run's START within its file (the reader resolves offset↔line:col against
// the file content). X/Y/W/H are the bounding box in PDF user space
// (bottom-left origin, y up, points).
type Location struct {
	Span struct {
		File       string `json:"file"`
		ByteOffset int64  `json:"byteOffset"`
	} `json:"span"`
	Page int     `json:"page"` // 0-based
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	W    float64 `json:"w"`
	H    float64 `json:"h"`
}

// Sidecar is the parsed `output.sourcemap.json`.
type Sidecar struct {
	Typst     string      `json:"typst"`
	Pages     int         `json:"pages"`
	Sizes     [][]float64 `json:"pageSizes"`
	Locations []Location  `json:"locations"`
}

// PageSize returns the [w, h] for a 0-based page, or nil if absent.
func (s *Sidecar) PageSize(i int) (float64, float64, bool) {
	if i < 0 || i >= len(s.Sizes) || len(s.Sizes[i]) < 2 {
		return 0, 0, false
	}
	return s.Sizes[i][0], s.Sizes[i][1], true
}

// Parse decodes the sidecar JSON. A nil / empty / malformed body is an error
// (the caller maps it: missing file -> not-compiled / 404 arms).
func Parse(data []byte) (*Sidecar, error) {
	var s Sidecar
	if len(data) == 0 {
		return nil, &Error{Message: "empty sourcemap"}
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, &Error{Message: "invalid sourcemap: " + err.Error()}
	}
	if s.Locations == nil {
		s.Locations = []Location{}
	}
	return &s, nil
}

// Error is a plain message error (the reader is total: parse + pure math).
type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

// OffsetAt maps a 1-based (line, column) position to a 0-based byte offset
// in content (column is 1-based; line 1 column 1 -> 0). Out-of-range
// positions saturate to the end of content (the reader is total; the caller
// has pre-validated the file exists).
func OffsetAt(content string, line, column int) int64 {
	if line < 1 {
		line = 1
	}
	// line starts: start of line 1 is 0; start of line L (L>=2) follows the
	// (L-2)th newline.
	starts := []int{}
	for i := 0; i < len(content) && len(starts) < line-1; i++ {
		if content[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	start := 0
	if line >= 2 {
		if line-2 < len(starts) {
			start = starts[line-2]
		} else {
			return int64(len(content)) // past the last line
		}
	}
	// end of the line: next newline after start, else end of content.
	end := len(content)
	for i := start; i < len(content); i++ {
		if content[i] == '\n' {
			end = i
			break
		}
	}
	if column < 1 {
		column = 1
	}
	if column > end-start {
		column = end - start
	}
	return int64(start) + int64(column-1)
}

// LineColAt maps a 0-based byte offset in content to a 1-based (line,
// column). Saturates: offset < 0 -> (1,1); offset >= len -> (lastLine, last).
func LineColAt(content string, offset int64) (line, column int) {
	if offset < 0 {
		offset = 0
	}
	if offset > int64(len(content)) {
		offset = int64(len(content))
	}
	line = 1
	column = 1
	for i := 0; i < int(offset) && i < len(content); i++ {
		if content[i] == '\n' {
			line++
			column = 1
		} else {
			column++
		}
	}
	return line, column
}

// NearestByOffset returns the location in locs (span.File == file) whose
// byteOffset is nearest to offset (ties: the earlier one). nil when there is
// no location for that file.
func NearestByOffset(locs []Location, file string, offset int64) *Location {
	best := -1
	for i := range locs {
		if locs[i].Span.File != file {
			continue
		}
		if best == -1 {
			best = i
			continue
		}
		if abs64(locs[i].Span.ByteOffset-offset) < abs64(locs[best].Span.ByteOffset-offset) {
			best = i
		}
	}
	if best == -1 {
		return nil
	}
	return &locs[best]
}

// NearestByPoint returns the location on a 0-based page whose center is
// nearest (x, y) in user space. nil when the page has no locations.
func NearestByPoint(locs []Location, page, x, y int) *Location {
	best := -1
	for i := range locs {
		if locs[i].Page != page {
			continue
		}
		if best == -1 {
			best = i
			continue
		}
		if centerDist(&locs[i], x, y) < centerDist(&locs[best], x, y) {
			best = i
		}
	}
	if best == -1 {
		return nil
	}
	return &locs[best]
}

func centerDist(loc *Location, x, y int) float64 {
	dx := float64(x) - (loc.X + loc.W/2)
	dy := float64(y) - (loc.Y + loc.H/2)
	return dx*dx + dy*dy
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
