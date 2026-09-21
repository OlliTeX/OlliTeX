package minimatch

// 1:1 port of src/brace-expressions.ts (parseClass), minimatch 10.2.6,
// isaacs/minimatch, BlueOak-1.0.0. Ported byte-for-byte from
// dist/commonjs/brace-expressions.js (the authoritative dist for v10.2.6).

// { <posix class>: [<translation>, u flag required, negated] }
// Faithful to the dist table. Note: upstream table value is a 3-tuple
// (unip, u, neg); only [:graph:] is negated.
type posixEntry struct {
	unip string
	u    bool
	neg  bool
}

var posixClasses = map[string]posixEntry{
	"[:alnum:]":  {"\\p{L}\\p{Nl}\\p{Nd}", true, false},
	"[:alpha:]":  {"\\p{L}\\p{Nl}", true, false},
	"[:ascii:]":  {"\\x" + "00-" + "\\x" + "7f", false, false},
	"[:blank:]":  {"\\p{Zs}\t", true, false},
	"[:cntrl:]":  {"\\p{Cc}", true, false},
	"[:digit:]":  {"\\p{Nd}", true, false},
	"[:graph:]":  {"\\p{Z}\\p{C}", true, true},
	"[:lower:]":  {"\\p{Ll}", true, false},
	"[:print:]":  {"\\p{C}", true, false},
	"[:punct:]":  {"\\p{P}", true, false},
	"[:space:]":  {"\\p{Z}\t\r\n\v\f", true, false},
	"[:upper:]":  {"\\p{Lu}", true, false},
	"[:word:]":   {"\\p{L}\\p{Nl}\\p{Nd}\\p{Pc}", true, false},
	"[:xdigit:]": {"A-Fa-f0-9", false, false},
}

// Insertion order of the dist table (Object.entries order — JS keeps
// string-keyed object property order for non-integer keys).
var posixClassOrder = []string{
	"[:alnum:]", "[:alpha:]", "[:ascii:]", "[:blank:]", "[:cntrl:]",
	"[:digit:]", "[:graph:]", "[:lower:]", "[:print:]", "[:punct:]",
	"[:space:]", "[:upper:]", "[:word:]", "[:xdigit:]",
}

// rangeEscapes: s.replace(/[[\]\\-]/g, '\\$&')
func rangeEscapes(s string) string {
	out := ""
	for _, r := range s {
		if r == '[' || r == ']' || r == '\\' || r == '-' {
			out += "\\" + string(r)
		} else {
			out += string(r)
		}
	}
	return out
}

// regexpEscape: s.replace(/[-[\]{}()*+?.,\\^$|#\s]/g, '\\$&')
func regexpEscapeGlob(s string) string {
	out := ""
	for _, r := range s {
		if r == '-' || r == '[' || r == ']' || r == '{' || r == '}' ||
			r == '(' || r == ')' || r == '*' || r == '+' || r == '?' ||
			r == '.' || r == ',' || r == '\\' || r == '^' || r == '$' ||
			r == '|' || r == '#' || r == ' ' {
			out += "\\" + string(r)
		} else {
			out += string(r)
		}
	}
	return out
}

// isSingleEscaped mirrors JS /^\\?.$/.test(r): either one char, or "\x".
func isSingleEscaped(r string) bool {
	rs := []rune(r)
	if len(rs) == 1 {
		return true
	}
	return len(rs) == 2 && r[0] == '\\'
}

// parseClass at a glob string's '[' and returns (src, uFlag, consumed, magic).
//
// Convention:
//   - consumed==0 → "not a class" (caller leaves '[' as plain text).
//   - magic==false, consumed>0 → a single literal char (escaped off).
//   - src=="$." → poisoned (never matches); magic=true.
func parseClass(glob string, pos int) (src string, uflag bool, consumed int, magic bool) {
	if pos >= len(glob) || glob[pos] != '[' {
		return "", false, 0, false
	}

	var ranges, negs []string
	i := pos + 1
	seenStart := false
	uFlag := false
	escaping := false
	negate := false
	endPos := pos
	rangeStart := ""

	for i < len(glob) {
		c := glob[i]
		if (c == '!' || c == '^') && i == pos+1 {
			negate = true
			i++
			continue
		}
		if c == ']' && seenStart && !escaping {
			endPos = i + 1
			break
		}
		seenStart = true
		if c == '\\' {
			if !escaping {
				escaping = true
				i++
				continue
			}
			// escaped \ char, fall through and treat like normal char
		}
		if c == '[' && !escaping {
			matched := false
			for _, cls := range posixClassOrder {
				if i+len(cls) <= len(glob) && glob[i:i+len(cls)] == cls {
					// invalid: [a-[] is fine, but not [a-[:alpha]]
					if rangeStart != "" {
						return "$.", false, len(glob) - pos, true
					}
					i += len(cls)
					e := posixClasses[cls]
					if e.neg {
						negs = append(negs, e.unip)
					} else {
						ranges = append(ranges, e.unip)
					}
					uFlag = uFlag || e.u
					matched = true
					break
					// `continue WHILE`
				}
			}
			if matched {
				// `continue WHILE`
				continue
			}
			// fall through: bare '[' treated as a normal char
		}
		// now it's just a normal character, effectively
		escaping = false
		if rangeStart != "" {
			// throw this range away if it's not valid, but others
			// can still match.
			if c > rangeStart[0] {
				ranges = append(ranges, rangeEscapes(rangeStart)+"-"+rangeEscapes(string(c)))
			} else if c == rangeStart[0] {
				ranges = append(ranges, rangeEscapes(string(c)))
			}
			rangeStart = ""
			i++
			continue
		}
		// now might be the start of a range.
		if i+2 < len(glob) && glob[i+1] == '-' && glob[i+2] == ']' {
			ranges = append(ranges, rangeEscapes(string(c)+"-"))
			i += 2
			continue
		}
		if i+1 < len(glob) && glob[i+1] == '-' {
			rangeStart = string(c)
			i += 2
			continue
		}
		ranges = append(ranges, rangeEscapes(string(c)))
		i++
	}

	if endPos < i {
		// didn't see the end of the class, not a valid class,
		// but might still be valid as a literal match.
		return "", false, 0, false
	}
	// if we got no ranges and no negates, then we have a range that
	// cannot possibly match anything, and that poisons the whole glob
	if len(ranges) == 0 && len(negs) == 0 {
		return "$.", false, len(glob) - pos, true
	}
	// if we got one positive range, and it's a single char, then that's
	// not actually a magic pattern, it's just that one literal character.
	if len(negs) == 0 && len(ranges) == 1 && isSingleEscaped(ranges[0]) && !negate {
		r := ranges[0]
		if len(ranges[0]) == 2 {
			r = ranges[0][1:]
		}
		return regexpEscapeGlob(r), false, endPos - pos, false
	}

	sranges := "[" + (map[bool]string{true: "^", false: ""}[negate]) + joinRanges(ranges) + "]"
	snegs := "[" + (map[bool]string{true: "", false: "^"}[negate]) + joinRanges(negs) + "]"
	var comb string
	if len(ranges) != 0 && len(negs) != 0 {
		comb = "(" + sranges + "|" + snegs + ")"
	} else if len(ranges) != 0 {
		comb = sranges
	} else {
		comb = snegs
	}
	return comb, uFlag, endPos - pos, true
}

// joinRanges: ranges.join(”) — everything has already been escaped.
func joinRanges(ranges []string) string {
	out := ""
	for _, r := range ranges {
		out += r
	}
	return out
}
