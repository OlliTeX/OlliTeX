// Package synctexparser ports services/clsi/app/js/SynctexOutputParser.js.
//
// Records are flat maps of parsed synctex fields (the JS object becomes a Go
// map[string]any).
//
//   - parseViewOutput: Page->page, h->h, v->v, W->width, H->height
//   - parseEditOutput: Input->file (Path-relative if the Input is absolute),
//     Line->line, Column->column
//
// A line whose (trimmed) label is exactly "Output" opens a new record; field
// lines before the first "Output:" line are dropped. A line without a colon
// is (label="", value=line), so it never becomes a known field. Values that
// don't parse to int/float leave the key unset (JS !isNaN(...)).
package synctexparser

import (
	"strconv"
	"strings"
)

// Record is one synctex record: field name -> parsed value (int or float64,
// or string for "file"), matching the JS flat object.
type Record = map[string]any

// ParseViewOutput ports parseViewOutput (records for `synctex view`).
func ParseViewOutput(output string) []Record {
	return parseOutput(output, func(r Record, label, value string) {
		switch label {
		case "Page":
			if v, ok := parseIntJS(value); ok {
				r["page"] = v
			}
		case "h":
			if v, ok := parseFloatJS(value); ok {
				r["h"] = v
			}
		case "v":
			if v, ok := parseFloatJS(value); ok {
				r["v"] = v
			}
		case "W":
			if v, ok := parseFloatJS(value); ok {
				r["width"] = v
			}
		case "H":
			if v, ok := parseFloatJS(value); ok {
				r["height"] = v
			}
		}
	})
}

// ParseEditOutput ports parseEditOutput (records for `synctex edit`).
func ParseEditOutput(output, baseDir string) []Record {
	return parseOutput(output, func(r Record, label, value string) {
		switch label {
		case "Input":
			if strings.HasPrefix(value, "/") {
				// node Path.relative(baseDir, value) (posix) == posixRel.
				r["file"] = posixRel(baseDir, value)
			} else {
				r["file"] = value
			}
		case "Line":
			if v, ok := parseIntJS(value); ok {
				r["line"] = v
			}
		case "Column":
			if v, ok := parseIntJS(value); ok {
				r["column"] = v
			}
		}
	})
}

// parseOutput mirrors JS _parseOutput.
func parseOutput(output string, proc func(r Record, label, value string)) []Record {
	var records []Record
	current := Record(nil)
	for _, line := range strings.Split(output, "\n") {
		label, value := splitLine(line)
		if label == "Output" {
			rec := make(Record)
			records = append(records, rec)
			current = rec
			continue
		}
		if current == nil {
			continue
		}
		proc(current, label, value)
	}
	return records
}

// splitLine mirrors JS _splitLine: split at the FIRST colon, trimming both
// sides. No colon => ("", line).
func splitLine(line string) (string, string) {
	idx := strings.IndexAny(line, ":")
	if idx < 0 {
		return "", strings.TrimSpace(line)
	}
	return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:])
}

// posixRel approximates node Path.relative(base, target) (posix build):
// walk the common segment prefix, emit one ".." per remaining base segment,
// then the remaining target segments. base=="" (i.e. base was "/") => target
// minus its leading slash (the leading path of the target).
func posixRel(base, target string) string {
	if base == "" {
		return strings.TrimPrefix(target, "/")
	}
	bs := splitPath(strings.TrimPrefix(base, "/"))
	ts := splitPath(strings.TrimPrefix(target, "/"))
	n := 0
	for n < len(bs) && n < len(ts) && bs[n] == ts[n] {
		n++
	}
	common := n // the for-loop that appends ".." mutates n; keep common aside.
	rel := make([]string, 0, len(bs)-common+len(ts)-common)
	for i := common; i < len(bs); i++ {
		rel = append(rel, "..")
	}
	if common < len(ts) {
		rel = append(rel, ts[common:]...)
	}
	if len(rel) == 0 {
		return "" // node: path.relative('/a', '/a') === ''
	}
	return strings.Join(rel, "/")
}

func splitPath(p string) []string {
	if p == "" {
		return nil
	}
	out := make([]string, 0, strings.Count(p, "/")+1)
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseIntJS approximates JS parseInt(value, 10): optional sign, then the
// leading run of decimal digits; stops at the first other character. No
// digits => ok=false (NaN upstream). synctex emits plain integers, so this
// is exact for its input.
func parseIntJS(s string) (int, bool) {
	t := strings.TrimSpace(s)
	i := 0
	if i < len(t) && (t[i] == '+' || t[i] == '-') {
		i++
	}
	n := 0
	d := 0
	for i < len(t) && t[i] >= '0' && t[i] <= '9' {
		n = n*10 + int(t[i]-'0')
		d++
		i++
	}
	if d == 0 {
		return 0, false
	}
	if len(t) > 0 && t[0] == '-' {
		return -n, true
	}
	return n, true
}

// parseFloatJS approximates JS parseFloat (digits, decimal point, exponent,
// optional leading sign). synctex emits fixed-point decimals.
func parseFloatJS(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
