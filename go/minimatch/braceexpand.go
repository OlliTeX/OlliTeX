// Port of the brace-expansion v5 (MIT, Isaac Z. Schlueter) expand() function
// and balanced-match v1.0.2 (MIT) balanced()/range(). Sources cited in
// go/minimatch/README.md. Byte-exact oracle: testdata/expand.tsv (95 rows).
package minimatch

import (
	"strings"
)

// Cap defaults (JS: EXPANSION_MAX / EXPANSION_MAX_LENGTH / EXPANSION_MAX_DEPTH
// / EXPANSION_MAX_REWRITES).
const (
	beMaxCap     = 100_000
	beMaxLenCap  = 4_000_000
	beMaxDepth   = 1_000
	beMaxRewrite = 1_000
)

// Sentinel tokens for escape/unescape. JS uses random tags per process; these
// fixed tags are process-local and cannot occur in glob input. Byte-verified:
// "\\\\ " -> "\\"  "\\{" -> "{"  "\\}" -> "}"  "\\," -> ","  "\\." -> "."
const (
	tSL = "\x00SL\x00"
	tOP = "\x00OP\x00"
	tCL = "\x00CL\x00"
	tCM = "\x00CM\x00"
	tDT = "\x00DT\x00"
)

// beEscape mirrors JS escapeBraces (five backslash-pair sentinels).
func beEscape(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case '\\':
				out.WriteString(tSL)
				i++
				continue
			case '{':
				out.WriteString(tOP)
				i++
				continue
			case '}':
				out.WriteString(tCL)
				i++
				continue
			case ',':
				out.WriteString(tCM)
				i++
				continue
			case '.':
				out.WriteString(tDT)
				i++
				continue
			}
		}
		out.WriteByte(c)
	}
	return out.String()
}

// beUnescape mirrors JS unescapeBraces.
func beUnescape(s string) string {
	s = strings.ReplaceAll(s, tSL, "\\")
	s = strings.ReplaceAll(s, tOP, "{")
	s = strings.ReplaceAll(s, tCL, "}")
	s = strings.ReplaceAll(s, tCM, ",")
	s = strings.ReplaceAll(s, tDT, ".")
	return s
}

// beNumeric: JS isNaN(str) ? str.charCodeAt(0) : parseInt(str, 10).
// Bodies are regex-validated (digits [+sign]), so only that alphabet reaches.
func beNumeric(s string) int {
	if n, ok := beParseNum(s); ok {
		return n
	}
	if s == "" {
		return 0
	}
	return int(s[0])
}

func beParseNum(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	i := 0
	neg := false
	if s[0] == '-' {
		neg = true
		i++
	} else if s[0] == '+' {
		i++
	}
	v := 0
	digs := false
	for j := i; j < len(s); j++ {
		if s[j] < '0' || s[j] > '9' {
			break
		}
		v = v*10 + int(s[j]-'0')
		digs = true
	}
	if !digs {
		return 0, false
	}
	if neg {
		v = -v
	}
	return v, true
}

func beIsPadded(s string) bool {
	i := 0
	if len(s) > 0 && s[0] == '-' {
		i = 1
	}
	if len(s)-i < 2 {
		return false
	}
	return s[i] == '0' && s[i+1] >= '0' && s[i+1] <= '9'
}

// beHasBracePattern: /\{(?:(?!\{).)*\}/ port (negative lookahead = no '{'
// between the matched '{' and '}'). Sentinel-agnostic (sentinels carry no
// braces). Faithful scan: at each open, the run after it must contain no
// '{' and must end with '}'; a nested '{' breaks that candidate (but later
// opens are still checked, so \{a,{b,c}}\} matches at the inner open).
func beHasBracePattern(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		for j := i + 1; j < len(s); j++ {
			if s[j] == '{' {
				break // this open's run broke; try the next one
			}
			if s[j] == '}' {
				return true
			}
		}
	}
	return false
}

// beHasCommaThenClose: post.matches(/,(?!,).*\}/) port.
func beHasCommaThenClose(post string) bool {
	for i := 0; i < len(post); i++ {
		if post[i] == ',' {
			if i+1 < len(post) && post[i+1] == ',' {
				continue
			}
			if strings.IndexByte(post[i+1:], '}') >= 0 {
				return true
			}
		}
	}
	return false
}

// beBalanceMPair: balanced-match v1.0.2 balanced()/range() port (b=='}' a=='{').
// Returns (pre, body, post, ok).
func beBalanceMPair(s string) (string, string, string, bool) {
	ai := strings.IndexByte(s, '{')
	if ai < 0 {
		return "", "", "", false
	}
	bi := -1
	if r := strings.IndexByte(s[ai+1:], '}'); r >= 0 {
		bi = r + ai + 1
	}
	if bi <= 0 {
		return "", "", "", false
	}
	var begs []int
	left := len(s)
	right := 0
	rightSet := false
	i := ai
	for i >= 0 {
		if i == ai {
			begs = append(begs, i)
			if r2 := strings.IndexByte(s[ai+1:], '{'); r2 >= 0 {
				ai = r2 + ai + 1
			} else {
				ai = -1
			}
		} else if len(begs) == 1 {
			r := begs[0]
			begs = begs[:0]
			return s[:r], s[r+1 : bi], s[bi+1:], true
		} else {
			if len(begs) > 0 {
				beg := begs[len(begs)-1]
				begs = begs[:len(begs)-1]
				if beg < left {
					left = beg
					right = bi
					rightSet = true
				}
			}
			if r2 := strings.IndexByte(s[bi+1:], '}'); r2 >= 0 {
				bi = r2 + bi + 1
			} else {
				bi = -1
			}
		}
		if ai < bi && ai >= 0 {
			i = ai
		} else {
			i = bi
		}
	}
	if len(begs) > 0 && rightSet {
		if left+1 >= right && right > 0 {
			return s[:left], "", s[right+1:], true
		}
		if left+1 < right {
			return s[:left], s[left+1 : right], s[right+1:], true
		}
	}
	return "", "", "", false
}

// beParseCommaParts: "Basically a str.split(','), except it handles nested
// brace groups, treating them as a single member."
// beParseCommaParts: "Basically a str.split(","), except it treats
// nested brace groups as single members."
func beParseCommaParts(str string) (res []string) {
	parts := []string{}
	carry := ""
	for {
		pr, bd, po, ok := beBalanceMPair(str)
		if !ok {
			tail := beSplitComma(str)
			tail[0] = carry + tail[0]
			return append(parts, tail...)
		}
		pp := beSplitComma(pr)
		pp[0] = carry + pp[0]
		pp[len(pp)-1] += "{" + bd + "}"
		if po == "" {
			return append(parts, pp...)
		}
		carry = pp[len(pp)-1]
		// pop last
		pp2 := pp[:len(pp)-1]
		parts = append(parts, pp2...)
		str = po
	}
}

func beSplitComma(s string) []string {
	parts := []string{}
	last := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			parts = append(parts, s[last:i])
			last = i + 1
		}
	}
	parts = append(parts, s[last:])
	return parts
}

// beExpandSequence port (JS expandSequence).
func beExpandSequence(body string, isAlpha bool, mx, ml int) []string {
	parts := []string{}
	last := 0
	for i := 0; i+1 < len(body); i++ {
		if body[i] == '.' && body[i+1] == '.' {
			parts = append(parts, body[last:i])
			i++
			last = i + 1
		}
	}
	parts = append(parts, body[last:])
	if len(parts) < 2 {
		return []string{}
	}
	x := beNumeric(parts[0])
	y := beNumeric(parts[1])
	width := len(parts[0])
	if len(parts[1]) > width {
		width = len(parts[1])
	}
	incr := 1
	if len(parts) == 3 && parts[2] != "" {
		v := beNumeric(parts[2])
		if v < 0 {
			v = -v
		}
		if v == 0 {
			v = 1
		}
		incr = v
	}
	reverse := y < x
	if reverse {
		incr = -incr
	}
	pad := beIsPadded(parts[0]) || beIsPadded(parts[1])
	length := 0
	N := []string{}
	i := x
	for {
		if reverse && i < y {
			break
		}
		if !reverse && i > y {
			break
		}
		if len(N) >= mx {
			break
		}
		var c string
		if isAlpha {
			c = string(rune(i))
			if c == "\\" {
				c = ""
			}
		} else {
			c = itoa(i)
			if pad {
				need := width - len(c)
				if need > 0 {
					z := ""
					for k := 0; k < need; k++ {
						z += "0"
					}
					if i < 0 {
						c = "-" + z + c[1:]
					} else {
						c = z + c
					}
				}
			}
		}
		if length+len(c) > ml {
			break
		}
		N = append(N, c)
		length += len(c)
		i += incr
	}
	return N
}

// beCombine: combine(acc, pre, values, max, maxLength, dropEmpties).
func beCombine(acc []string, pre string, values []string, mx, ml int, dropEmpties bool) []string {
	out := []string{}
	length := 0
	for a := range acc {
		for v := range values {
			if len(out) >= mx {
				return out
			}
			e := acc[a] + pre + values[v]
			if dropEmpties && e == "" {
				continue
			}
			if length+len(e) > ml {
				return out
			}
			out = append(out, e)
			length += len(e)
		}
	}
	return out
}

// beExpandCore: faithful v5 expand_ port (iterative, single accumulator,
// `$`-prefix, `{a},b}` rewrite, sequence/options paths).
func beExpandCore(str string, mx, ml, maxDepth, maxRewrite, depth int, isTop bool, accIn []string) []string {
	if depth > maxDepth {
		return []string{str}
	}
	acc := accIn
	rewrites := 0
	dropEmpties := false
	firstGroup := true
	for {
		pr, bd, po, ok := beBalanceMPair(str)
		if !ok {
			return beCombine(acc, str, []string{""}, mx, ml, dropEmpties)
		}
		if strings.HasSuffix(pr, "$") {
			acc = beCombine(acc, pr+"{"+bd+"}", []string{""}, mx, ml, dropEmpties && po == "")
			firstGroup = false
			if po == "" {
				break
			}
			str = po
			continue
		}
		isSeq := beIsNumericSequence(bd) || beIsAlphaSequence(bd)
		if !isSeq && !strings.ContainsAny(bd, ",") {
			if rewrites < maxRewrite && beHasCommaThenClose(po) {
				rewrites++
				str = pr + "{" + bd + tCL + po
				isTop = true
				continue
			}
			return beCombine(acc, pr+"{"+bd+"}"+po, []string{""}, mx, ml, dropEmpties)
		}
		if firstGroup {
			dropEmpties = isTop && !isSeq
			firstGroup = false
		}
		var values []string
		if beIsNumericSequence(bd) {
			values = beExpandSequence(bd, false, mx, ml)
		} else if beIsAlphaSequence(bd) {
			values = beExpandSequence(bd, true, mx, ml)
		} else {
			parts := beParseCommaParts(bd)
			if len(parts) == 1 {
				inner := beExpandCore(parts[0], mx, ml, maxDepth, maxRewrite, depth+1, false, []string{""})
				emb := make([]string, len(inner))
				for k, w := range inner {
					emb[k] = "{" + w + "}"
				}
				parts = emb
				if len(parts) == 1 {
					acc = beCombine(acc, pr+parts[0], []string{""}, mx, ml, dropEmpties && po == "")
					if po == "" {
						break
					}
					str = po
					continue
				}
			}
			dropsEmpties := dropEmpties && po == "" && pr == ""
			for d := 0; dropsEmpties && d < len(acc); d++ {
				if acc[d] != "" {
					dropsEmpties = false
					break
				}
			}
			values = []string{}
			valuesLen := 0
			outerDone := false
			for j := 0; !outerDone && j < len(parts); j++ {
				expanded := beExpandCore(parts[j], mx, ml, maxDepth, maxRewrite, depth+1, false, []string{""})
				for k, v := range expanded {
					_ = k
					if dropsEmpties && v == "" {
						continue
					}
					if len(values) >= mx || valuesLen+len(v) > ml {
						outerDone = true
						break
					}
					values = append(values, v)
					valuesLen += len(v)
				}
			}
		}
		acc = beCombine(acc, pr, values, mx, ml, dropEmpties && po == "")
		if po == "" {
			break
		}
		str = po
	}
	return acc
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	buf := []byte{}
	for v > 0 {
		buf = append(buf, byte('0'+v%10))
		v /= 10
	}
	if neg {
		buf = append(buf, '-')
	}
	// reverse
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}

// beIsNumericSequence: /^-?\d+\.\.-?\d+(?:\.\.-?\d+)?$/ on a sentinel-free
// body (sentinels replaced digits with \x00-terminated tags — but bodies are
// only validated on raw digits because isNumericSequence is applied to the
// ESCAPED string where digits are literal; sentinels cannot match \d).
func beIsNumericSequence(b string) bool {
	i := 0
	if i < len(b) && b[i] == '-' {
		i++
	}
	if i >= len(b) || !beIsDigit(b[i]) {
		return false
	}
	for i < len(b) && beIsDigit(b[i]) {
		i++
	}
	if i+1 >= len(b) || b[i] != '.' || b[i+1] != '.' {
		return false
	}
	i += 2
	if i < len(b) && b[i] == '-' {
		i++
	}
	if i >= len(b) || !beIsDigit(b[i]) {
		return false
	}
	for i < len(b) && beIsDigit(b[i]) {
		i++
	}
	if i == len(b) {
		return true
	}
	if i+1 >= len(b) || b[i] != '.' || b[i+1] != '.' {
		return false
	}
	i += 2
	if i < len(b) && b[i] == '-' {
		i++
	}
	if i >= len(b) || !beIsDigit(b[i]) {
		return false
	}
	for i < len(b) && beIsDigit(b[i]) {
		i++
	}
	return i == len(b)
}

// beIsAlphaSequence: /^[a-zA-Z]\.\.[a-zA-Z](?:\.\.-?\d+)?$/.
func beIsAlphaSequence(b string) bool {
	if len(b) < 4 {
		return false
	}
	if !beIsAlpha(b[0]) || b[1] != '.' || b[2] != '.' || !beIsAlpha(b[3]) {
		return false
	}
	_ = b
	i := 4
	if i == len(b) {
		return true
	}
	if i+1 >= len(b) || b[i] != '.' || b[i+1] != '.' {
		return false
	}
	i += 2
	if i < len(b) && b[i] == '-' {
		i++
	}
	if i >= len(b) || !beIsDigit(b[i]) {
		return false
	}
	for i < len(b) && beIsDigit(b[i]) {
		i++
	}
	return i == len(b)
}

func beIsDigit(c byte) bool { return c >= '0' && c <= '9' }

func beIsAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// Expand: brace-expansion v5 expand(pattern, {max, maxLength, maxDepth,
// maxRewrites}) port. Empty pattern -> empty result. Leading "{}" is
// preserved by Bash at top level: the source prepends "\{\}" which (through
// escape/unescape) means a literal "{}".
func Expand(pattern string, opts Options) ([]string, error) {
	if pattern == "" {
		return []string{}, nil
	}
	mx := opts.BraceExpandMax
	if mx <= 0 {
		mx = beMaxCap
	}
	ml := beMaxLenCap
	// The "{,}" prefix quirk: JS rewrites str to "\\{\\}" + str[2:].
	s := pattern
	if len(s) >= 2 && s[0:2] == "{}" {
		s = "\\{" + "\\}" + s[2:]
	}
	esc := beEscape(s)
	out := beExpandCore(esc, mx, ml, beMaxDepth, beMaxRewrite, 0, true, []string{""})
	res := make([]string, len(out))
	for i, w := range out {
		res[i] = beUnescape(w)
	}
	return res, nil
}

// MMBraceExpand: minimatch's braceExpand(pattern, options) port — the
// hasBracePattern/nobrace shortcut over Expand (CLSI entry point).
func MMBraceExpand(pattern string, opts Options) ([]string, error) {
	if err := assertValidPattern(pattern); err != nil {
		return nil, err
	}
	if opts.Nobrace || !beHasBracePattern(pattern) {
		return []string{pattern}, nil
	}
	return Expand(pattern, opts)
}
