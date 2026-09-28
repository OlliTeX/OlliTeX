package versions

// Port of services/project-history/app/js/Versions.js (1:1).
//
// Live caller: UpdatesProcessor.js (gte). Route params are always strings, so
// the Go numeric path stays as the string-only port. Node source (Versions.js
// L14-20) parses each dot-segment with `parseInt(x, 10)` — empty or
// non-numeric segments become NaN and compare equal against any slot while
// still counting in the padded array length; `null`-shift pads to 2 slots.
// Oracle: app/js/Versions.js + test/unit/js/Versions/VersionTest.js.

import (
	"math"
	"strconv"
	"strings"
)

// naSeg encodes a JS NaN slot.
type seg struct {
	n  int64
	ok bool
}

// parseIntPrefix mirrors JS parseInt(s, 10): leading whitespace + optional
// sign, then the leading digit run; anything else is NaN (ok=false).
func parseIntPrefix(s string) (seg, bool) {
	i := 0
	for i < len(s) && isJSSpace(s[i]) {
		i++
	}
	rest := s[i:]
	sign := int64(1)
	if rest != "" && (rest[0] == '+' || rest[0] == '-') {
		if rest[0] == '-' {
			sign = -1
		}
		rest = rest[1:]
	}
	if rest == "" || rest[0] < '0' || rest[0] > '9' {
		return seg{}, false
	}
	j := 0
	for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
		j++
	}
	// JS saturates overflow toward Infinity; keep finite saturation.
	if j > 15 {
		return seg{math.MaxInt64, true}, true
	}
	n, _ := strconv.ParseInt(rest[:j], 10, 64)
	return seg{n * sign, true}, true
}

func isJSSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\v', '\f', '\r', 0xA0: // practical subset of JS whitespace set
		return true
	}
	return false
}

// convertToArray mirrors Versions.js L14-20 (null-shifted parseInt array).
func convertToArray(v string) []seg {
	parts := strings.Split(v, ".")
	res := make([]seg, 0, len(parts)+1)
	if len(parts) < 2 {
		res = append(res, seg{0, true})
	}
	for _, p := range parts {
		if n, ok := parseIntPrefix(p); ok {
			res = append(res, n)
		} else {
			res = append(res, seg{}) // NaN slot
		}
	}
	return res
}

// compare mirrors Versions.js `compare` (L25-50 dead-code branches preserved:
// idx<0 dead, idx>=len dead-branches, trailing length compare).
func Compare(v1, v2 string) int {
	a, b := convertToArray(v1), convertToArray(v2)
	max := len(a)
	if len(b) > max {
		max = len(b)
	}
	for i := 0; i < max; i++ {
		var s1, s2 seg
		if i < len(a) {
			s1 = a[i]
		}
		if i < len(b) {
			s2 = b[i]
		}
		// JS: NaN slots make both > and < comparisons false.
		if s1.ok && s2.ok {
			if s1.n > s2.n {
				return 1
			}
			if s1.n < s2.n {
				return -1
			}
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}

func GT(v1, v2 string) bool  { return Compare(v2, v1) < 0 }
func GTE(v1, v2 string) bool { return Compare(v2, v1) <= 0 }
func LT(v1, v2 string) bool  { return Compare(v2, v1) > 0 }
func LTE(v1, v2 string) bool { return Compare(v2, v1) >= 0 }
