// Package updatecompressor ports vendor app/js/UpdateCompressor.js (B7).
//
// Vendor updates are raw JSON maps: {op: ...|[ops], meta: {...}, v, doc,
// pathname, ...}. The port keeps them as map[string]any (matching how
// updates flow through JSON decoding and the vendor tests), mirroring the
// vendor function-for-function.
//
// Known port deviations (HANDOFF): vendor ConsistencyError -> plain error;
// the Metrics sampling wrapper is NOT ported (infrastructure) — the pure
// pipeline is CompressRawUpdates.
package updatecompressor

import (
	"errors"
	"ollitex/go/libraries/dmp"
	"ollitex/go/services/project-history/internal/historyot"
)

const (
	// MaxTimeBetweenUpdates — vendor `MAX_TIME_BETWEEN_UPDATES = 60 * 1000`.
	MaxTimeBetweenUpdates = int64(60 * 1000)
	// MaxUpdateSize — vendor `MAX_UPDATE_SIZE = 2 * 1024 * 1024` (2 MB).
	MaxUpdateSize = 2 * 1024 * 1024
)

// diffDmp mirrors vendor module-level `const dmp = new DMP();
// dmp.Diff_Timeout = 0.1`.
var diffDmp = func() *dmp.DMP {
	d := dmp.New()
	d.DiffTimeout = 0.1
	return d
}()

// jsTruthy mirrors JS `!= null` truthiness used across the vendor checks.
func jsTruthy(v any) bool { return v != nil }

func asNum(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	}
	return 0, false
}

// utf16Len — vendor `s.length` (JS UTF-16 code-unit count).
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func toU16(s string) []uint16 {
	out := make([]uint16, 0, utf16Len(s))
	for _, r := range s {
		if r > 0xFFFF {
			r -= 0x10000
			out = append(out, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
		} else {
			out = append(out, uint16(r))
		}
	}
	return out
}

func fromU16(u []uint16) string {
	var b []rune
	for i := 0; i < len(u); i++ {
		c := u[i]
		if c >= 0xD800 && c <= 0xDBFF && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] <= 0xDFFF {
			b = append(b, rune(c-0xD800)<<10+rune(u[i+1])-0xDC00+0x10000)
			i++
		} else {
			b = append(b, rune(c))
		}
	}
	return string(b)
}

// strUnitSlice — vendor `s.slice(start, start+length)` in UTF-16 units.
func strUnitSlice(s string, start, length int) string {
	u := toU16(s)
	if start > len(u) {
		start = len(u)
	}
	end := start + length
	if end > len(u) {
		end = len(u)
	}
	if start > end {
		start = end
	}
	return fromU16(u[start:end])
}

// strInject — vendor `s1.slice(0, pos) + s2 + s1.slice(pos)` (UTF-16 pos).
func strInject(s1 string, pos int, s2 string) string {
	u := toU16(s1)
	if pos < 0 {
		pos = 0
	}
	if pos > len(u) {
		pos = len(u)
	}
	out := make([]uint16, 0, len(u)+utf16Len(s2))
	out = append(out, u[:pos]...)
	out = append(out, toU16(s2)...)
	out = append(out, u[pos:]...)
	return fromU16(out)
}

// strRemove — vendor `s1.slice(0, pos) + s1.slice(pos + length)`.
func strRemove(s1 string, pos, length int) string {
	u := toU16(s1)
	if pos < 0 {
		pos = 0
	}
	if pos > len(u) {
		pos = len(u)
	}
	end := pos + length
	if end > len(u) {
		end = len(u)
	}
	out := make([]uint16, 0, len(u)-length)
	out = append(out, u[:pos]...)
	out = append(out, u[end:]...)
	return fromU16(out)
}

// isHistoryOT — vendor `EditOperationBuilder.isValid(op)`: the
// discriminator between a historyOT edit op and a sharejs text op
// ({p, i} / {p, d}).
func isHistoryOT(op any) bool {
	m, ok := op.(map[string]any)
	if !ok {
		return false
	}
	return historyot.Builder.IsValid(m)
}

func adjustLengthByOp(length int, op map[string]any, tracked bool) (int, error) {
	if iVal, hasI := op["i"]; hasI && iVal != nil {
		if jsTruthy(op["trackedDeleteRejection"]) {
			return length, nil
		}
		s, _ := iVal.(string)
		return length + utf16Len(s), nil
	}
	if dVal, hasD := op["d"]; hasD && dVal != nil {
		if tracked {
			if changes, okk := op["trackedChanges"].([]any); okk {
				for _, c := range changes {
					cm, ok := c.(map[string]any)
					if !ok {
						continue
					}
					if cm["type"] == "insert" {
						if cl, ok := asNum(cm["length"]); ok {
							length -= int(cl)
						}
					}
				}
			}
			return length, nil
		}
		s, _ := dVal.(string)
		return length - utf16Len(s), nil
	}
	if rVal, hasR := op["r"]; hasR && rVal != nil {
		return length, nil
	}
	if cVal, hasC := op["c"]; hasC && cVal != nil {
		return length, nil
	}
	return 0, errors.New("unexpected op type")
}
