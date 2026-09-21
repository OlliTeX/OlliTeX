// reengine.go — closed-dialect evaluator for the exact regex sources that
// minimatch 10.2.6 compiles for its path-portion cells (see segast.go).
//
// Go's regexp (RE2) cannot express the negative lookaheads minimatch emits
// (eg (?!(?:^|/))), so this file evaluates byte-for-byte the dialect
// minimatch 10.2.6 actually produces. Scope verified against the full oracle
// (testoracle.tsv: 35400 rows, every compiled source x every test file, V8
// RegExp.test ground truth). Dialect (flags ” or 'u' — flag-independent for
// the ASCII byte corpus):
//
//	(?:...)        non-capturing group (transparent: inlined into parent)
//	(?!(?:...))   negative lookahead wrapping a (?: group
//	( ... )       plain parentheses (bare alternation, eg ($|/))
//	[...]          single chars, ranges a-b, ^ negation, escapes \t \r \n \v
//	                  \f \- \] and \p{L Ll Lu Nd Nl P Pc Z Zs Cc}
//	|              alternation (leading/trailing | = empty branch)
//	.              raw any-char (matches anything but \n \r)
//	*  +  *?  +?  {1,2}   quantifiers (lazy forms booleanly identical)
//	\X             identity escapes (\- \) \. \[ { } etc), incl \p{...}
//	^  $           anchors (zero width, absolute: ^ pos==0, $ pos==len(s))
//
// Anything outside this inventory aborts with dialectErr (loud, not silence).
//
// Evaluation = forward position simulation (NOT backtracking): each node maps
// a SET of input positions to positions reachable after it. Lazy vs greedy is
// booleanly irrelevant under RegExp.test() existence, so quantifiers resolve
// to the position-set closure. All oracle sources are ^...$-anchored, so the
// boolean test is "final position set is non-empty".
//
// Byte semantics: input paths are byte strings (CLSI paths are ASCII, never
// contain newlines); V8 code-unit positions == Go byte positions for those
// inputs. \p{X} applied per byte (byte as rune) — byte-approx matches V8 for
// single-byte input (multi-byte divergence documented).
package minimatch

import "unicode"

// ---------- node model ----------

type mmNode struct {
	typ      int
	children []mmNode // nSeq: elements; nAlt: branches
	inner    *mmNode  // nNegLook body
	r        rune     // nChar
	cl       *mmClass
	repLo    int // 0,0 = "exactly once"; * => 0,-1; + => 1,-1; ? => 0,1
	repHi    int
}

type mmClassPart struct {
	propName string // "" => byte part
	lo, hi   byte
}

type mmClass struct {
	neg   bool
	parts []mmClassPart
}

type reProg struct{ root mmNode }

const (
	nSeq = iota
	nAlt
	nChar
	nClass
	nAnyDot
	nAnchorStart
	nAnchorEnd
	nNegLook
)

// JS \p tables, restricted to the ten names the compile oracle uses.
var propTables = map[string]func(r rune) bool{
	"L":  func(r rune) bool { return unicode.Is(unicode.L, r) },
	"Ll": func(r rune) bool { return unicode.Is(unicode.Ll, r) },
	"Lu": func(r rune) bool { return unicode.Is(unicode.Lu, r) },
	"Nd": func(r rune) bool { return unicode.Is(unicode.Nd, r) },
	"Nl": func(r rune) bool { return r >= 0x1100 && r <= 0x115F },
	"P":  func(r rune) bool { return unicode.Is(unicode.P, r) },
	"Pc": func(r rune) bool { return unicode.Is(unicode.Pc, r) },
	"Z": func(r rune) bool {
		return unicode.Is(unicode.Zs, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r)
	},
	"Zs": func(r rune) bool { return unicode.Is(unicode.Zs, r) },
	"Cc": func(r rune) bool { return unicode.IsControl(r) },
}

type dialectErr struct{ msg string }

func (d dialectErr) Error() string { return "minimatch: dialect error: " + d.msg }

// CompileRe parses a closed-dialect regex source into a program.
func CompileRe(src string) (*reProg, error) {
	p := &reParser{s: src}
	n, err := p.sequence(0)
	if err != nil {
		return nil, err
	}
	if p.i != len(p.s) {
		end := p.i + 8
		if end > len(p.s) {
			end = len(p.s)
		}
		return nil, dialectErr{"unbalanced near " + p.s[p.i:end]}
	}
	return &reProg{root: n}, nil
}

type reParser struct {
	s string
	i int
}

// ---------- parser ----------

// sequence parses one alternation level: returns the level node (nSeq for one
// branch, nAlt for many). groupDepth==0 is the program root; >0 means "a )
// terminates this level" (transparent-group recursion).
func (p *reParser) sequence(groupDepth int) (mmNode, error) {
	branches, err := p.sequence2(groupDepth)
	if err != nil {
		return mmNode{}, err
	}
	if len(branches) == 1 {
		return mmNode{typ: nSeq, children: branches[0]}, nil
	}
	alts := make([]mmNode, len(branches))
	for i, b := range branches {
		alts[i] = mmNode{typ: nSeq, children: b}
	}
	return mmNode{typ: nAlt, children: alts}, nil
}

// sequence2 returns one flat atom list per |-separated branch.
func (p *reParser) sequence2(groupDepth int) ([][]mmNode, error) {
	var branches [][]mmNode
	cur := []mmNode{}
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '|' {
			branches = append(branches, cur)
			cur = []mmNode{}
			p.i++
			continue
		}
		if c == ')' {
			if groupDepth > 0 {
				break // the group caller consumes the ')'
			}
			return nil, dialectErr{"unbalanced )"}
		}
		at, err := p.atom(groupDepth)
		if err != nil {
			return nil, err
		}
		cur = append(cur, at)
	}
	branches = append(branches, cur)
	return branches, nil
}

// groupBody parses the INSIDE of a group (the opening prefix already
// consumed); consumes the closing ')'. Returns the group node: single child
// inlined, "exactly once" (repLo=repHi=0).
func (p *reParser) groupBody(groupDepth int) (mmNode, error) {
	branches, err := p.sequence2(groupDepth)
	if err != nil {
		return mmNode{}, err
	}
	if p.i >= len(p.s) || p.s[p.i] != ')' {
		return mmNode{}, dialectErr{"unterminated group"}
	}
	p.i++ // consume ')'
	if len(branches) == 1 {
		b := branches[0]
		if len(b) == 0 {
			return mmNode{typ: nSeq, children: []mmNode{}}, nil // empty group = empty sequence
		}
		if len(b) == 1 {
			return b[0], nil
		}
		return mmNode{typ: nSeq, children: b}, nil
	}
	alts := make([]mmNode, len(branches))
	for i, b := range branches {
		alts[i] = mmNode{typ: nSeq, children: b}
	}
	return mmNode{typ: nAlt, children: alts}, nil
}

// atom = bare unit + optional quantifier (lazy tail ? consumed).
func (p *reParser) atom(groupDepth int) (mmNode, error) {
	n, err := p.bare(groupDepth)
	if err != nil {
		return mmNode{}, err
	}
	if p.eof() {
		return n, nil
	}
	switch p.s[p.i] {
	case '*':
		p.i++
		n.repLo, n.repHi = 0, -1
	case '+':
		p.i++
		n.repLo, n.repHi = 1, -1
	case '?':
		p.i++
		n.repLo, n.repHi = 0, 1
	case '{':
		end := p.i + 1
		for end < len(p.s) && p.s[end] != '}' {
			end++
		}
		if end >= len(p.s) {
			return mmNode{}, dialectErr{"unterminated {…} quantifier"}
		}
		lo, hi, err := parseRepRange(p.s[p.i+1 : end])
		if err != nil {
			return mmNode{}, err
		}
		n.repLo, n.repHi = lo, hi
		p.i = end + 1
	}
	if !p.eof() && p.s[p.i] == '?' {
		p.i++
	}
	return n, nil
}

func parseRepRange(s string) (int, int, error) {
	comma := -1
	for i, c := range s {
		if c == ',' {
			comma = i
			break
		}
	}
	if comma < 0 {
		n, err := reDigits(s, "n")
		if err != nil {
			return 0, 0, err
		}
		if n > 100 {
			return 0, 0, dialectErr{"bound out of dialect: " + s}
		}
		return n, n, nil
	}
	lo, err := reDigits(s[:comma], "lo")
	if err != nil {
		return 0, 0, err
	}
	hiStr := s[comma+1:]
	if hiStr == "" {
		if lo > 100 {
			return 0, 0, dialectErr{"bound out of dialect: " + s}
		}
		return lo, -1, nil
	}
	hi, err := reDigits(hiStr, "hi")
	if err != nil {
		return 0, 0, err
	}
	if hi < lo || hi > 100 {
		return 0, 0, dialectErr{"bad bound: " + s}
	}
	return lo, hi, nil
}

func reDigits(s, what string) (int, error) {
	if len(s) == 0 {
		return 0, dialectErr{"empty {…} bound (" + what + ")"}
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, dialectErr{"non-digit in {…} bound (" + what + ")"}
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

func (p *reParser) eof() bool { return p.i >= len(p.s) }

// bare parses a leaf or a group (no quantifier yet).
func (p *reParser) bare(groupDepth int) (mmNode, error) {
	c := p.s[p.i]
	switch {
	case c == '^':
		p.i++
		return mmNode{typ: nAnchorStart}, nil
	case c == '$':
		p.i++
		return mmNode{typ: nAnchorEnd}, nil
	case c == '(' && p.i+3 <= len(p.s) && p.s[p.i+1] == '?' && p.s[p.i+2] == '!':
		// (?!(?:...)) negative lookahead
		p.i += 3
		inner, err := p.groupBody(1)
		if err != nil {
			return mmNode{}, err
		}
		innerPtr := inner
		return mmNode{typ: nNegLook, inner: &innerPtr}, nil
	case c == '(' && p.i+3 <= len(p.s) && p.s[p.i+1] == '?' && p.s[p.i+2] == ':':
		// (?: transparent group
		p.i += 3
		return p.groupBody(1)
	case c == '(':
		// plain group (transparent)
		p.i++
		return p.groupBody(1)
	case c == '[':
		cl, bodyLen, ok := parseClassBody(p.s[p.i:])
		if !ok {
			return mmNode{}, dialectErr{"invalid class (dialect)"}
		}
		p.i += bodyLen
		return mmNode{typ: nClass, cl: cl}, nil
	case c == '.' && p.i+1 < len(p.s):
		p.i++
		return mmNode{typ: nAnyDot}, nil
	case c == '\\':
		return p.mmEscaped()
	case c == '|' || c == '*' || c == '+' || c == '?':
		// quantifier without a unit, or stray pipe handled by sequence2
		return mmNode{}, dialectErr{"stray " + string(c)}
	}
	if c == ')' && groupDepth > 0 {
		// impossible: sequence2 breaks before atom()
		return mmNode{}, dialectErr{"stray )"}
	}
	// plain char
	p.i++
	return mmNode{typ: nChar, r: rune(c)}, nil
}

// mmEscaped parses \X (X one byte: n r t v f p or an identity-escape char).
func (p *reParser) mmEscaped() (mmNode, error) {
	if p.i+1 >= len(p.s) {
		return mmNode{}, dialectErr{"dangling backslash"}
	}
	esc := p.s[p.i+1]
	switch esc {
	case 'n', 'r', 't', 'v', 'f':
		p.i += 2
		var r rune
		switch esc {
		case 'n':
			r = '\n'
		case 'r':
			r = '\r'
		case 't':
			r = '\t'
		case 'v':
			r = '\v'
		case 'f':
			r = '\f'
		}
		return mmNode{typ: nChar, r: r}, nil
	case 'p':
		if p.i+3 >= len(p.s) || p.s[p.i+2] != '{' {
			return mmNode{}, dialectErr{"\\p without {…}"}
		}
		end := p.i + 3
		for end < len(p.s) && p.s[end] != '}' {
			end++
		}
		if end >= len(p.s) {
			return mmNode{}, dialectErr{"unterminated \\p{…}"}
		}
		name := p.s[p.i+3 : end]
		if _, ok := propTables[name]; !ok {
			return mmNode{}, dialectErr{"unsupported \\p{" + name + "}"}
		}
		p.i = end + 1
		return mmNode{typ: nClass, cl: &mmClass{parts: []mmClassPart{{propName: name}}}}, nil
	default:
		if !idEscapeOK(esc) {
			return mmNode{}, dialectErr{"unsupported escape \\" + string(esc)}
		}
		// identity escape \X (covered: \- \) \. \[ { } ^ $ | * + ? / etc)
		p.i += 2
		return mmNode{typ: nChar, r: rune(p.s[p.i-1])}, nil
	}
}

// parseClassBody parses a bracket expression starting at s[0]=='[' and
// returns (class, bytesConsumedInclClosingBracket, ok). Dialect (oracle
// inventory of 24 unique spans): single chars, ranges a-b, ^ negation,
// escapes \t \r \n \v \f \- \] \/ and \p{L Ll Lu Nd Nl P Pc Z Zs Cc}.
// Anything outside the dialect returns ok=false (loud failure).
func parseClassBody(s string) (cl *mmClass, consumed int, ok bool) {
	// s[0]=='['. Find the closing unescaped ']'. V8 rule: an unescaped ']' at
	// body position 0 (right after [ or [^) is a LITERAL ']; every other
	// unescaped ']' closes the class. pos tracks body offset for that check;
	// escape pairs \X consume 2 body positions, \p{NAME} is handled as the
	// 3-byte escape plus {NAME}.
	n := len(s)
	i := 1
	pos := 0

	for i < n {
		c := s[i]
		if c == '\\' {
			if i+1 >= n {
				return nil, 0, false
			}
			if s[i+1] == 'p' {
				if i+2 >= n || s[i+2] != '{' {
					return nil, 0, false // dialect: \p{...} only
				}
				k := i + 3
				for k < n && s[k] != '}' {
					k++
				}
				if k >= n {
					return nil, 0, false
				}
				// escape consumed bytes i..k-1 of body text: (k-i) body positions
				pos += k - i
				i = k + 1
				continue
			}
			pos += 2
			i += 2
			continue
		}
		if c == ']' && pos > 0 {
			break
		}
		i++
		pos++
	}
	if i >= n {
		return nil, 0, false // unterminated class
	}
	body := s[1:i]
	cl, err := parseClassInner(body)
	if err != nil {
		return nil, 0, false
	}
	return cl, i + 1, true
}

func parseClassInner(body string) (*mmClass, error) {
	cl := &mmClass{}
	i := 0
	if i < len(body) && (body[i] == '^' || body[i] == '-') {
		if body[i] == '^' {
			cl.neg = true
		}
		i++
	}
	for i < len(body) {
		c := body[i]
		if c == '\\' {
			if i+1 >= len(body) {
				return nil, dialectErr{"dangling backslash in class"}
			}
			e := body[i+1]
			if e == 'p' {
				if i+2 >= len(body) || body[i+2] != '{' {
					return nil, dialectErr{"\\p without {…} in class"}
				}
				k := i + 3
				for k < len(body) && body[k] != '}' {
					k++
				}
				if k >= len(body) {
					return nil, dialectErr{"unterminated \\p{…}"}
				}
				name := body[i+3 : k]
				if _, j := propTables[name]; !j {
					return nil, dialectErr{"unsupported \\p{" + name + "}"}
				}
				cl.parts = append(cl.parts, mmClassPart{propName: name})
				i = k + 1
				continue
			}
			var val byte
			switch e {
			case 'n':
				val = '\n'
			case 'r':
				val = '\r'
			case 't':
				val = '\t'
			case 'v':
				val = '\v'
			case 'f':
				val = '\f'
			default:
				if !idEscapeOK(e) {
					return nil, dialectErr{"unsupported class escape " + string(e)}
				}
				val = e // identity escape (\- \] \/ etc)
			}
			cl.parts = append(cl.parts, mmClassPart{lo: val, hi: val})
			i += 2
			continue
		}
		// plain char; range a-b when both ends are plain chars
		if i+2 < len(body) && body[i+1] == '-' && body[i+2] != '\\' {
			cl.parts = append(cl.parts, mmClassPart{lo: c, hi: body[i+2]})
			i += 3
			continue
		}
		cl.parts = append(cl.parts, mmClassPart{lo: c, hi: c})
		i++
	}
	return cl, nil
}

func (c *mmClass) containsByte(b byte, _ string) (bool, error) {
	for _, part := range c.parts {
		if part.propName != "" {
			if propTables[part.propName](rune(b)) {
				return !c.neg, nil
			}
			continue
		}
		if part.lo == part.hi {
			if b == part.lo {
				return !c.neg, nil
			}
			continue
		}
		if b >= part.lo && b <= part.hi {
			return !c.neg, nil
		}
	}
	// no part matched
	if c.neg {
		return true, nil // negated classes match everything else
	}
	return false, nil
}

// ---------- forward-set evaluator (NOT backtracking) ----------
//
// evaluate(n, s, starts) returns the set of positions (byte offsets in s)
// reachable after matching node n starting anywhere in starts. For the
// oracle's ^...$-anchored sources, RegExp.test(s) ⇔ len(s) ∈ evaluate(root,
// s, {0}). Lazy vs greedy quantifiers is booleanly irrelevant under
// existence, so both resolve to the position-set closure (worklist fixpoint).

func evaluate(n mmNode, s string, starts map[int]bool) (map[int]bool, error) {
	if n.repLo != 0 || n.repHi != 0 {
		return closedEval(n, s, starts)
	}
	var out map[int]bool
	switch n.typ {
	case nChar:
		out = map[int]bool{}
		for p := range starts {
			if p < len(s) && rune(s[p]) == n.r {
				out[p+1] = true
			}
		}
	case nAnyDot:
		out = map[int]bool{}
		for p := range starts {
			if p < len(s) && s[p] != '\n' && s[p] != '\r' {
				out[p+1] = true
			}
		}
	case nClass:
		out = map[int]bool{}
		for p := range starts {
			if p >= len(s) {
				continue
			}
			m, err := n.cl.containsByte(s[p], s)
			if err != nil {
				return nil, err
			}
			if m {
				out[p+1] = true
			}
		}
	case nAnchorStart:
		out = map[int]bool{}
		for p := range starts {
			if p == 0 {
				out[p] = true
			}
		}
	case nAnchorEnd:
		out = map[int]bool{}
		for p := range starts {
			if p == len(s) {
				out[p] = true
			}
		}
	case nNegLook:
		// negative lookahead = zero width: passes p through iff the inner
		// matches nothing at p (existence over all inner paths).
		out = map[int]bool{}
		for p := range starts {
			reach, err := evaluate(*n.inner, s, map[int]bool{p: true})
			if err != nil {
				return nil, err
			}
			if len(reach) == 0 {
				out[p] = true
			}
		}
	case nSeq:
		cur := starts
		for _, c := range n.children {
			var err error
			if cur, err = evaluate(c, s, cur); err != nil {
				return nil, err
			}
			if len(cur) == 0 {
				return cur, nil
			}
		}
		return cur, nil
	case nAlt:
		out = map[int]bool{}
		for _, b := range n.children {
			reach, err := evaluate(b, s, starts)
			if err != nil {
				return nil, err
			}
			for p := range reach {
				out[p] = true
			}
		}
	default:
		return nil, dialectErr{"unknown node type"}
	}
	return out, nil
}

// closedEval evaluates a quantified node: exactly [repLo, repHi] iterations
// (hi=-1 = unbounded: position-set closure, bounded hi = exact k-iteration).
func closedEval(n mmNode, s string, starts map[int]bool) (map[int]bool, error) {
	body := n
	body.repLo, body.repHi = 0, 0
	if n.repHi == n.repLo {
		// exactly n.repLo iterations (the (0,0) default never reaches here)
		cur := starts
		for k := 0; k < n.repLo; k++ {
			var err error
			if cur, err = evaluate(body, s, cur); err != nil {
				return nil, err
			}
			if len(cur) == 0 {
				return cur, nil
			}
		}
		return cur, nil
	}
	// forced minimum iterations
	cur := starts
	for k := 0; k < n.repLo; k++ {
		var err error
		if cur, err = evaluate(body, s, cur); err != nil {
			return nil, err
		}
		if len(cur) == 0 && n.repHi > 0 {
			return cur, nil
		}
	}
	if n.repHi < 0 {
		// unbounded: closure over reachable positions (fixpoint)
		for {
			next, err := evaluate(body, s, cur)
			if err != nil {
				return nil, err
			}
			grew := false
			for p, v := range next {
				if !cur[p] {
					cur[p] = v
					grew = true
				}
			}
			if !grew {
				break
			}
		}
		return cur, nil
	}
	// bounded: union of stages k in [repLo, repHi]
	out := map[int]bool{}
	stage := starts
	for k := 0; k <= n.repHi; k++ {
		if k > 0 {
			var err error
			if stage, err = evaluate(body, s, stage); err != nil {
				return nil, err
			}
		}
		if k >= n.repLo {
			for p := range stage {
				out[p] = true
			}
		}
		if k > 0 && len(stage) == 0 {
			break
		}
	}
	return out, nil
}

// Test reports whether the program matches the whole string s
// (the oracle sources are always ^...$-anchored).
func (p *reProg) Test(s string) (bool, error) {
	reach, err := evaluate(p.root, s, map[int]bool{0: true})
	if err != nil {
		return false, err
	}
	return reach[len(s)], nil
}

// idEscapeOK: characters minimatch 10.2.6 may identity-escape in compiled
// sources (its regExprEscape class), plus '!' and ' ' from guard patterns.
func idEscapeOK(c byte) bool {
	switch c {
	case '(', ')', '[', ']', '{', '}', '*', '+', '?', '.', ',', '-', '\\',
		'^', '$', '|', '#', '/', ' ', '!', '~', '"':
		return true
	}
	return false
}
