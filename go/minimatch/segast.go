// Port of src/ast.ts (minimatch 10.2.6, isaacs/minimatch, BlueOak-1.0.0).
//
// Faithful to the upstream source: same adoption/usurp/flatten tables,
// same fillNegs tail copy-in, same guard start/end injection. The
// generated RE sources feed the oracle-verified reengine (testdata
// testoracle.tsv: 35,400/35,400 rows vs V8), so match semantics inherit
// that verification. The upstream fast-test shortcuts (starRE etc.) are
// deliberately NOT ported: they only shortcut .test() and the pure RE
// route is oracle-verified to be equivalent.

package minimatch

// adoption: anything but ! can adopt a matching-type child; * and +
// adopt a wider set.
var adoptionGo = map[int8][]int8{
	'!': {'@'},
	'?': {'?', '@'},
	'@': {'@'},
	'*': {'*', '+', '?', '@'},
	'+': {'+', '@'},
}

// nested extglobs adopted in WITH an extra ” blank arm appended.
var adoptionWithSpaceGo = map[int8][]int8{
	'!': {'?'},
	'@': {'?'},
	'+': {'?', '*'},
}

// union of the previous two (recursion-depth escape hatch).
var adoptionAnyGo = map[int8][]int8{
	'!': {'?', '@'},
	'?': {'?', '@'},
	'@': {'?', '@'},
	'*': {'*', '+', '?', '@'},
	'+': {'+', '@', '?', '*'},
}

// nested extglobs taking over a 1-arm parent: parent type, child type ->
// resulting parent type. '@' parents are a special case (always usurped).
var usurpGo = map[int8]map[int8]int8{
	'!': {'!': '@'},
	'?': {'*': '*', '+': '*'},
	'@': {'!': '!', '?': '?', '@': '@', '*': '*', '+': '+'},
	'+': {'?': '*', '*': '*'},
}

const (
	startNoTraversal = "(?!(?:^|/)\\.\\.?(?:$|/))"
	startNoDot       = "(?!\\.)"
)

var addPatternStartGo = map[int8]bool{'[': true, '.': true}
var justDotsGo = map[string]bool{"..": true, ".": true}

var reSpecialsGo = func() map[int8]bool {
	m := make(map[int8]bool, 15)
	const s = "().*{}+?[]^$\\!"
	for i := 0; i < len(s); i++ {
		m[int8(s[i])] = true
	}
	return m
}()

// qmark / star (byte-for-byte JS constants).
const mmQmark = "[^/]"
const mmStar = mmQmark + "*?"
const mmStarNoEmpty = mmQmark + "+?"

func isExtglobTypeGo(c byte) bool {
	switch c {
	case '!', '?', '+', '*', '@':
		return true
	}
	return false
}

// mmAST mirrors the AST class. typ: -1 flat, else the extglob type.
type mmAST struct {
	typ         int8
	parts       []mmPart
	parent      *mmAST
	parentIndex int
	opts        *Options // the root's options
	root        *mmAST
	negs        []*mmAST // root-registered '!' extglobs
	filledNegs  bool
	hasMagic    *bool
	uflag       bool
	emptyExt    bool
	toStr       string
	hasToStr    bool
}

// mmPart is the (string | AST) union.
type mmPart struct {
	isAst bool
	s     string
	a     *mmAST
}

func mmStrPart(s string) mmPart { return mmPart{s: s} }
func mmAstPart(a *mmAST) mmPart { return mmPart{isAst: true, a: a} }

func newMMAST(typ int8, parent *mmAST, opts *Options) *mmAST {
	a := &mmAST{typ: typ}
	if typ >= 0 {
		t := true
		a.hasMagic = &t
	}
	if parent == nil {
		a.root, a.opts = a, opts
		return a
	}
	a.parent = parent
	a.parentIndex = len(parent.parts)
	a.root = parent.root
	a.opts = a.root.opts
	if typ == '!' && !a.root.filledNegs {
		a.root.negs = append(a.root.negs, a)
	}
	return a
}

func (a *mmAST) mmPushStr(s string) {
	if s == "" {
		return
	}
	a.parts = append(a.parts, mmStrPart(s))
}

func (a *mmAST) mmPushAst(c *mmAST) {
	if c.parent != a {
		panic("minimatch: invalid part: " + c.toString())
	}
	a.parts = append(a.parts, mmAstPart(c))
}

// toString mirrors JS toString().
func (a *mmAST) toString() string {
	if a.hasToStr {
		return a.toStr
	}
	var out string
	if a.typ < 0 {
		for _, p := range a.parts {
			if p.isAst {
				out += p.a.toString()
			} else {
				out += p.s
			}
		}
	} else {
		out = string(rune(a.typ)) + "("
		for i, p := range a.parts {
			if i > 0 {
				out += "|"
			}
			if p.isAst {
				out += p.a.toString()
			} else {
				out += p.s
			}
		}
		out += ")"
	}
	a.toStr, a.hasToStr = out, true
	return a.toStr
}

func (a *mmAST) invalidateToStr() {
	a.toStr, a.hasToStr = "", false
}

// ToString: public wrapper (original pattern form).
func (a *mmAST) ToString() string { return a.toString() }

func (a *mmAST) isStart() bool {
	if a.root == a {
		return true
	}
	if a.parent == nil || !a.parent.isStart() {
		return false
	}
	if a.parentIndex == 0 {
		return true
	}
	p := a.parent
	for i := 0; i < a.parentIndex; i++ {
		pp := p.parts[i]
		if !pp.isAst || pp.a.typ != '!' {
			return false
		}
	}
	return true
}

func (a *mmAST) isEnd() bool {
	if a.root == a {
		return true
	}
	if a.parent.typ == '!' {
		return true
	}
	if !a.parent.isEnd() {
		return false
	}
	if a.typ < 0 {
		return a.parent.isEnd()
	}
	pl := len(a.parent.parts)
	return a.parentIndex == pl-1
}

func (a *mmAST) cloneAST(parent *mmAST) *mmAST {
	c := newMMAST(a.typ, parent, nil)
	for _, p := range a.parts {
		a.mmCopyIn(c, p)
	}
	return c
}

// mmCopyIn into dst (JS copyIn(part): push string or clone).
func (a *mmAST) mmCopyIn(dst *mmAST, p mmPart) {
	if p.isAst {
		dst.mmPushAst(p.a.cloneAST(dst))
	} else {
		dst.mmPushStr(p.s)
	}
}

func (a *mmAST) canAdoptType(c int8, m map[int8][]int8) bool {
	if a.typ < 0 {
		return false
	}
	for _, t := range m[a.typ] {
		if t == c {
			return true
		}
	}
	return false
}

func (a *mmAST) canAdoptGeneric(child mmPart, m map[int8][]int8) bool {
	if !child.isAst || a.typ < 0 {
		return false
	}
	c := child.a
	if c.typ != -1 || len(c.parts) != 1 {
		return false
	}
	gc := c.parts[0]
	if !gc.isAst || gc.a.typ < 0 {
		return false
	}
	return a.canAdoptType(gc.a.typ, m)
}

func (a *mmAST) canUsurpGeneric(child mmPart) bool {
	if !child.isAst || a.typ < 0 || len(a.parts) != 1 {
		return false
	}
	c := child.a
	if c.typ != -1 || len(c.parts) != 1 {
		return false
	}
	gc := c.parts[0]
	if !gc.isAst || gc.a.typ < 0 {
		return false
	}
	return a.canUsurpType(gc.a.typ)
}

func (a *mmAST) canUsurpType(c int8) bool {
	if m, ok := usurpGo[a.typ]; ok {
		_, ok = m[c]
		return ok
	}
	return false
}

func (a *mmAST) adoptGeneric(child *mmAST, index int) {
	gc := child.parts[0].a
	head := make([]mmPart, 0, len(a.parts)-1+len(gc.parts))
	head = append(head, a.parts[:index]...)
	head = append(head, gc.parts...)
	head = append(head, a.parts[index+1:]...)
	a.parts = head
	for i := range gc.parts {
		if gc.parts[i].isAst {
			gc.parts[i].a.parent = a
		}
	}
	a.invalidateToStr()
}

func (a *mmAST) adoptWithSpaceGeneric(child *mmAST, index int) {
	gc := child.parts[0].a
	blank := newMMAST(-1, gc, nil)
	blank.parts = append(blank.parts, mmStrPart(""))
	gc.mmPushAst(blank)
	a.adoptGeneric(child, index)
}

func (a *mmAST) usurpGeneric(child *mmAST) {
	gc := child.parts[0].a
	nt, ok := usurpGo[a.typ][gc.typ]
	if !ok {
		return
	}
	a.parts = gc.parts
	for i := range a.parts {
		if a.parts[i].isAst {
			a.parts[i].a.parent = a
		}
	}
	a.typ = nt
	a.invalidateToStr()
	a.emptyExt = false
}

// flatten: up to 10 adopt/usurp passes on extglobs; recurses into
// children for flats.
func (a *mmAST) flatten() {
	if a.typ < 0 {
		for _, p := range a.parts {
			if p.isAst {
				p.a.flatten()
			}
		}
		return
	}
	var iter int
	done := false
	for !done && iter < 10 {
		done = true
		for i := 0; i < len(a.parts); i++ {
			c := a.parts[i]
			if !c.isAst {
				continue
			}
			c.a.flatten()
			if a.canAdoptGeneric(c, adoptionGo) {
				done = false
				a.adoptGeneric(c.a, i)
			} else if a.canAdoptGeneric(c, adoptionWithSpaceGo) {
				done = false
				a.adoptWithSpaceGeneric(c.a, i)
			} else if a.canUsurpGeneric(c) {
				done = false
				a.usurpGeneric(c.a)
			}
		}
		iter++
	}
	a.invalidateToStr()
}

// fillNegs copies tails into '!'-extglob arms. Root-only (JS throws).
func (a *mmAST) fillNegs() {
	if a.root != a {
		panic("minimatch: should only call on root")
	}
	if a.filledNegs {
		return
	}
	a.toString()
	a.filledNegs = true
	for len(a.negs) > 0 {
		n := a.negs[len(a.negs)-1]
		a.negs = a.negs[:len(a.negs)-1]
		if n.typ != '!' {
			continue
		}
		p := n
		pp := p.parent
		for pp != nil {
			for i := p.parentIndex + 1; pp.typ < 0 && i < len(pp.parts); i++ {
				for ci := range n.parts {
					pr := n.parts[ci]
					if !pr.isAst {
						panic("minimatch: string part in extglob AST??")
					}
					pr.a.mmCopyIn(pr.a, pp.parts[i])
				}
			}
			p = pp
			pp = p.parent
		}
	}
}

// parseAST ports AST.#parseAST (flat and extglob branches). In extglob
// mode pos is the index of the '('; returns the next index.
func (a *mmAST) parseAST(str string, pos, extDepth int) int {
	maxDepth := 2
	noext := false
	if o := a.root.opts; o != nil {
		if o.MaxExtglobRecursion > 0 {
			maxDepth = o.MaxExtglobRecursion
		}
		noext = o.Noext || o.Noextglob
	}

	escaping := false
	inBrace := false
	braceStart := -1
	braceNeg := false

	if a.typ < 0 {
		i := pos
		acc := ""
		for i < len(str) {
			c := str[i]
			i++
			if escaping || c == '\\' {
				escaping = !escaping
				acc += string(c)
				continue
			}
			if inBrace {
				if i == braceStart+1 {
					if c == '^' || c == '!' {
						braceNeg = true
					}
				} else if c == ']' && !(i == braceStart+2 && braceNeg) {
					inBrace = false
				}
				acc += string(c)
				continue
			} else if c == '[' {
				inBrace = true
				braceStart = i
				braceNeg = false
				acc += string(c)
				continue
			}
			doRecurse := !noext && i < len(str) && isExtglobTypeGo(c) &&
				str[i] == '(' && extDepth <= maxDepth
			if doRecurse {
				a.mmPushStr(acc)
				acc = ""
				ext := newMMAST(int8(c), a, nil)
				i = ext.parseAST(str, i, extDepth+1)
				a.mmPushAst(ext)
				continue
			}
			acc += string(c)
		}
		a.mmPushStr(acc)
		return i
	}

	// extglob branch: pos is at the '('.
	i := pos + 1
	part := newMMAST(-1, a, nil)
	var parts []*mmAST
	acc := ""
	for i < len(str) {
		c := str[i]
		i++
		if escaping || c == '\\' {
			escaping = !escaping
			acc += string(c)
			continue
		}
		if inBrace {
			if i == braceStart+1 {
				if c == '^' || c == '!' {
					braceNeg = true
				}
			} else if c == ']' && !(i == braceStart+2 && braceNeg) {
				inBrace = false
			}
			acc += string(c)
			continue
		} else if c == '[' {
			inBrace = true
			braceStart = i
			braceNeg = false
			acc += string(c)
			continue
		}
		adoptable := a.canAdoptType(int8(c), adoptionAnyGo)
		doRecurse := !noext && i < len(str) && isExtglobTypeGo(c) &&
			str[i] == '(' && (extDepth <= maxDepth || adoptable)
		if doRecurse {
			depthAdd := 1
			if adoptable {
				depthAdd = 0
			}
			part.mmPushStr(acc)
			acc = ""
			ext := newMMAST(int8(c), part, nil)
			part.mmPushAst(ext)
			i = ext.parseAST(str, i, extDepth+depthAdd)
			continue
		}
		if c == '|' {
			part.mmPushStr(acc)
			acc = ""
			parts = append(parts, part)
			part = newMMAST(-1, a, nil)
			continue
		}
		if c == ')' {
			if acc == "" && len(a.parts) == 0 {
				a.emptyExt = true
			}
			part.mmPushStr(acc)
			acc = ""
			for _, p := range parts {
				a.mmPushAst(p)
			}
			a.mmPushAst(part)
			return i
		}
		acc += string(c)
	}

	// unfinished extglob: malformed; treat the text from c on as flat.
	a.typ = -1
	a.hasMagic = nil
	a.parts = []mmPart{mmStrPart(str[pos-1:])}
	a.invalidateToStr()
	return i
}

// mmASTFromGlob mirrors AST.fromGlob.
func mmASTFromGlob(pattern string, opts *Options) *mmAST {
	asts := newMMAST(-1, nil, opts)
	asts.parseAST(pattern, 0, 0)
	return asts
}

// mmUnescape mirrors upstream `unescape(body)` (default options).
func mmUnescape(s string) string { return Unescape(s, nil) }

// parseGlob ports AST.#parseGlob (flat glob string -> RE source chunk).
func parseMMGlob(glob string, initHasMagic *bool, noEmpty bool) (string, string, bool, bool) {
	hasMagic := false
	if initHasMagic != nil {
		hasMagic = *initHasMagic
	}
	uflag := false
	re := ""
	escaping := false
	inStar := false
	allStars := len(glob) > 0
	for k := 0; k < len(glob); k++ {
		if glob[k] != '*' {
			allStars = false
			break
		}
	}
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		if escaping {
			escaping = false
			if reSpecialsGo[int8(c)] {
				re += "\\"
			}
			re += string(c)
			continue
		}
		if c == '*' {
			if inStar {
				continue
			}
			inStar = true
			if noEmpty && allStars {
				re += mmStarNoEmpty
			} else {
				re += mmStar
			}
			hasMagic = true
			continue
		}
		inStar = false
		if c == '\\' {
			if i == len(glob)-1 {
				re += "\\\\"
			} else {
				escaping = true
			}
			continue
		}
		if c == '[' {
			src, uf, consumed, magic := parseClass(glob, i)
			if consumed > 0 {
				re += src
				uflag = uflag || uf
				hasMagic = hasMagic || magic
				i += consumed - 1
				continue
			}
		}
		if c == '?' {
			re += mmQmark
			hasMagic = true
			continue
		}
		re += regexpEscapeGlob(string(c))
	}
	return re, mmUnescape(glob), hasMagic, uflag
}

// partsToRegExp ports AST#partsToRegExp (extglob arms joined with |).
func (a *mmAST) partsToRegExp(dot bool) string {
	out := make([]string, 0, len(a.parts))
	for _, p := range a.parts {
		if !p.isAst {
			panic("minimatch: string type in extglob ast??")
		}
		dd := dot
		r, _, _, uf := p.a.toRegExpSource(&dd)
		a.uflag = a.uflag || uf
		out = append(out, r)
	}
	joined := ""
	filtered := a.isStart() && a.isEnd()
	for i, r := range out {
		if filtered && r == "" {
			continue
		}
		if i > 0 {
			joined += "|"
		}
		joined += r
	}
	return joined
}

// mmGuardStart computes the guard start for a flat-portion root:
//
//	(dot || preEscapes) + aps(src[0]) ? startNoTraversal :
//	!dot && !allowDot + aps(src[0]) ? startNoDot : ""
//
// src[2]/src[4] for the \./ (and \(.) probes via JS charAt == "".
func mmGuardStart(dot bool, src string, allowDot *bool) string {
	c0 := int8(0)
	if len(src) > 0 {
		c0 = int8(src[0])
	}
	c2 := int8(0)
	if len(src) >= 3 {
		c2 = int8(src[2])
	}
	c4 := int8(0)
	if len(src) >= 5 {
		c4 = int8(src[4])
	}
	pre2 := len(src) >= 2 && src[0] == '\\' && src[1] == '.'
	pre4 := pre2 && len(src) >= 4 && src[2] == '\\' && src[3] == '.'
	if (dot && addPatternStartGo[c0]) || (pre2 && addPatternStartGo[c2]) || (pre4 && addPatternStartGo[c4]) {
		return startNoTraversal
	}
	if !dot && (allowDot == nil || !*allowDot) && addPatternStartGo[c0] {
		return startNoDot
	}
	return ""
}

// toRegExpSource ports AST#toRegExpSource(allowDot).
func (a *mmAST) toRegExpSource(allowDot *bool) (string, string, bool, bool) {
	dot := a.opts != nil && a.opts.Dot
	if allowDot != nil {
		dot = *allowDot
	}
	if a.root == a {
		a.flatten()
		a.fillNegs()
	}

	if a.typ < 0 {
		anyAst := false
		for ci := range a.parts {
			if a.parts[ci].isAst {
				anyAst = true
				break
			}
		}
		noEmpty := a.isStart() && a.isEnd() && !anyAst

		src := ""
		for _, p := range a.parts {
			var r, body string
			var m, uf bool
			if p.isAst {
				r, _, m, uf = p.a.toRegExpSource(allowDot)
			} else {
				r, body, m, uf = parseMMGlob(p.s, a.hasMagic, noEmpty)
			}
			_ = body
			if a.hasMagic != nil {
				*a.hasMagic = *a.hasMagic || m
			} else {
				b := m
				a.hasMagic = &b
			}
			a.uflag = a.uflag || uf
			src += r
		}

		start := ""
		if a.isStart() && len(a.parts) > 0 && !a.parts[0].isAst {
			dotTravAllowed := len(a.parts) == 1 && justDotsGo[a.parts[0].s]
			if !dotTravAllowed {
				start = mmGuardStart(dot, src, allowDot)
			}
		}

		end := ""
		if a.isEnd() && a.root.filledNegs && a.parent != nil && a.parent.typ == '!' {
			end = "(?:$|\\/)"
		}
		norm := false
		if a.hasMagic != nil {
			norm = *a.hasMagic
		}
		b := norm
		a.hasMagic = &b
		return start + src + end, mmUnescape(src), norm, a.uflag
	}

	// extglob branch.
	repeated := a.typ == '*' || a.typ == '+'
	start := "(?:"
	if a.typ == '!' {
		start = "(?:(?!(?:"
	}
	body := a.partsToRegExp(dot)

	if a.isStart() && a.isEnd() && body == "" && a.typ != '!' {
		// invalid extglob: fall back to the literal text
		s := a.toString()
		a.typ = -1
		a.hasMagic = nil
		a.invalidateToStr()
		return s, mmUnescape(a.toString()), false, false
	}

	var bodyDotAllowed string
	if !repeated || (allowDot != nil && *allowDot) || dot {
		bodyDotAllowed = ""
	} else {
		bodyDotAllowed = a.partsToRegExp(true)
	}
	if bodyDotAllowed == body {
		bodyDotAllowed = ""
	}
	if bodyDotAllowed != "" {
		body = "(?:" + body + ")(?:" + bodyDotAllowed + ")*?"
	}

	final := ""
	switch {
	case a.typ == '!' && a.emptyExt:
		pre := ""
		if a.isStart() && !dot {
			pre = startNoDot
		}
		final = pre + mmStarNoEmpty
	default:
		closeStr := ""
		switch {
		// (compound conditions below)
		case a.typ == '!':
			guard := ""
			if a.isStart() && !dot && (allowDot == nil || !*allowDot) {
				guard = startNoDot
			}
			closeStr = "))" + guard + mmStar + ")"
		case a.typ == '@':
			closeStr = ")"
		case a.typ == '?':
			closeStr = ")?"
		case a.typ == '+' && bodyDotAllowed != "":
			closeStr = ")"
		case a.typ == '*' && bodyDotAllowed != "":
			closeStr = ")?"
		default:
			closeStr = ")" + string(rune(a.typ))
		}
		final = start + body + closeStr
		_ = bodyDotAllowed
	}
	norm := false
	if a.hasMagic != nil {
		norm = *a.hasMagic
	}
	bb := norm
	a.hasMagic = &bb
	return final, mmUnescape(body), norm, a.uflag
}

// mmPartCompile: a compiled glob portion (string or compiled RE).
type mmPartCompile struct {
	typ   int32 // -1 string, 0 compiled RE
	s     string
	flags string
	re    *reProg
}

// mmParsePortion ports Minimatch#parse's return: string or compiled
// ^...$-anchored RE source (the JS fast-test shortcuts are not ported;
// see package comment).
func mmParsePortion(pattern string, o *Options) (mmPartCompile, error) {
	if o == nil {
		o = &Options{}
	}
	astv := mmASTFromGlob(pattern, o)
	re, body, hasMagic, uf := astv.toRegExpSource(nil)
	_ = uf // (nocase/u flags: byte-approximation, see README)
	anyMagic := hasMagic
	if v := astv.hasMagic; v != nil {
		anyMagic = anyMagic || *v
	}
	cb := anyMagic || (o.Nocase && !o.NocaseMagicOnly &&
		asciiUpper(body) != asciiLower(body))
	flags := ""
	if o.Nocase {
		flags += "i"
	}
	if uf {
		flags += "u"
	}
	if !cb {
		return mmPartCompile{typ: -1, s: body, flags: flags}, nil
	}
	prog, err := CompileRe(re)
	if err != nil {
		return mmPartCompile{typ: 0, flags: flags}, err
	}
	return mmPartCompile{typ: 0, re: prog, flags: flags}, nil
}

func asciiUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}
