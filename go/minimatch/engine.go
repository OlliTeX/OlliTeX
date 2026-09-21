// engine.go - port of the Minimatch class core (src/index.ts):
// New (ctor), make, parsePortion, HasMagic, preprocess (opt levels 0/1/2),
// Match (public), MatchList, and the match machinery:
//
//	matchOne, matchOneInner, matchGlobstar, matchGlobStarBodySections.
//
// POSIX port (CLSI). Upstream isWindows/UNC/drive-letter branches and
// makeRe are NOT ported (see README "Divergences"). All else mirrors
// index.ts 1:1.
package minimatch

import (
	"errors"
	"strings"
)

// mCell: one compiled pattern portion (a string between slashes).
// kind: -1 plain string (exact match), 0 compiled ^...$-anchored RE.
// globstar: true = the "**" sentinel, matched structurally, never test()-ed.
type mCell struct {
	globstar bool
	kind     int32
	s        string
	re       *reProg
}

// bodySeg: one globstar-delimited body segment (cells + after bound).
type bodySeg struct {
	cells []mCell
	after int
}

func (c mCell) isStr() bool { return !c.globstar && c.kind == -1 }

func (c mCell) test(f string) (bool, error) {
	if c.kind == -1 {
		return f == c.s, nil
	}
	return c.re.Test(f)
}

// tri-state values for matchGlobStarBodySections (JS `boolean|null`):
//
//	triWalk = JS null   (walked off; caller keeps trying)
//	triHit  = JS true   (matched)
//	triFail = JS false  (no match; caller keeps its own position)
const (
	triWalk = iota
	triHit
	triFail
)

// Minimatch is a compiled minimatch pattern set (POSIX).
type Minimatch struct {
	pat      string
	negate   bool
	comment  bool
	empty    bool
	partial  bool
	o        *Options
	maxGSRec int
	set      [][]mCell
}

// New ports `new Minimatch(pattern, options)`. POSIX-only.
func New(pattern string, o *Options) (*Minimatch, error) {
	if err := assertValidPattern(pattern); err != nil {
		return nil, err
	}
	if o == nil {
		o = &Options{}
	}
	if o.Platform != "" && o.Platform != defaultPlatform {
		return nil, errors.New("minimatch: POSIX-only port (platform=" + o.Platform + ")")
	}
	if o.windowsEscape() {
		pattern = strings.ReplaceAll(pattern, "\\", "/")
	}
	maxgs := o.MaxGlobstarRecursion
	if maxgs <= 0 {
		maxgs = 200
	}
	m := &Minimatch{pat: pattern, partial: o.Partial, maxGSRec: maxgs, o: o}
	m.parseNegate()
	if err := m.make(); err != nil {
		return nil, err
	}
	return m, nil
}

// Match ports `minimatch(pattern, options).match(f)` (line 1409).
func (m *Minimatch) Match(f string) bool {
	if m.comment {
		return false
	}
	if m.empty {
		return f == ""
	}
	if f == "/" && m.partial {
		return true
	}
	ff := m.slashSplit(f)
	filename := ""
	for i := len(ff) - 1; i >= 0; i-- {
		if ff[i] != "" {
			filename = ff[i]
			break
		}
	}
	for _, pattern := range m.set {
		file := ff
		if m.o.MatchBase && len(pattern) == 1 {
			file = []string{filename}
		}
		hit, err := m.matchOne(file, pattern, m.partial)
		if err != nil {
			return false
		}
		if hit {
			if m.o.FlipNegate {
				return true
			}
			return !m.negate
		}
	}
	if m.o.FlipNegate {
		return false
	}
	return m.negate
}

// MatchList ports module-level `match(list, pattern, options)`.
func (m *Minimatch) MatchList(list []string) []string {
	out := make([]string, 0, len(list))
	for _, f := range list {
		if m.Match(f) {
			out = append(out, f)
		}
	}
	if m.o.Nonull && len(out) == 0 {
		out = append(out, m.pat)
	}
	return out
}

// Match is the module-level convenience: `minimatch(f, pattern, options)`.
func Match(f, pattern string, o *Options) (bool, error) {
	m, err := New(pattern, o)
	if err != nil {
		return false, err
	}
	return m.Match(f), nil
}

// parseNegate ports Minimatch#parseNegate.
func (m *Minimatch) parseNegate() {
	if m.o.Nonegate {
		return
	}
	p := m.pat
	negate, off := false, 0
	for off < len(p) && p[off] == '!' {
		negate = !negate
		off++
	}
	if off > 0 {
		m.pat = p[off:]
	}
	m.negate = negate
}

// slashSplit ports Minimatch#slashSplit: JS `p.split(/\/+/)` for POSIX.
func (m *Minimatch) slashSplit(p string) []string {
	if m.o.PreserveMultipleSlashes {
		return strings.Split(p, "/")
	}
	out := []string{}
	last := 0
	for i := 0; i < len(p); {
		if p[i] != '/' {
			i++
			continue
		}
		j := i
		for j < len(p) && p[j] == '/' {
			j++
		}
		out = append(out, p[last:i])
		last = j
		i = j
	}
	out = append(out, p[last:])
	return out
}

// make ports Minimatch#make (comment/empty flags or set construction).
func (m *Minimatch) make() error {
	o := m.o
	if !o.Nocomment && len(m.pat) > 0 && m.pat[0] == '#' {
		m.comment = true
		return nil
	}
	if m.pat == "" {
		m.empty = true
		return nil
	}
	gset, err := m.braceExpand()
	if err != nil {
		return err
	}
	seen := map[string]struct{}{}
	gsets := make([]string, 0, len(gset))
	for _, s := range gset {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			gsets = append(gsets, s)
		}
	}
	raw := make([][]string, len(gsets))
	for i, s := range gsets {
		raw[i] = m.slashSplit(s)
	}
	globParts := m.preprocess(raw)
	set := make([][]mCell, 0, len(globParts))
	for _, parts := range globParts {
		cells := make([]mCell, len(parts))
		ok := true
		for i, ss := range parts {
			if err := m.parsePortion(ss, &cells[i]); err != nil {
				ok = false
				break
			}
		}
		if ok {
			set = append(set, cells)
		}
	}
	m.set = set
	return nil
}

func (m *Minimatch) braceExpand() ([]string, error) {
	if m.o.Nobrace || !beHasBracePattern(m.pat) {
		return []string{m.pat}, nil
	}
	return Expand(m.pat, *m.o)
}

// parsePortion ports Minimatch#parse for one portion.
func (m *Minimatch) parsePortion(portion string, out *mCell) error {
	if portion == "**" {
		*out = mCell{globstar: true}
		return nil
	}
	if portion == "" {
		*out = mCell{kind: -1, s: ""}
		return nil
	}
	pc, err := mmParsePortion(portion, m.o)
	if err != nil {
		return err
	}
	if pc.typ == -1 {
		*out = mCell{kind: -1, s: pc.s}
	} else {
		*out = mCell{kind: 0, re: pc.re}
	}
	return nil
}

// HasMagic ports Minimatch#hasMagic.
func (m *Minimatch) HasMagic() bool {
	if m.o.MagicalBraces && len(m.set) > 1 {
		return true
	}
	for _, pattern := range m.set {
		for _, part := range pattern {
			if !part.isStr() {
				return true
			}
		}
	}
	return false
}

// =================================================================
// preprocess (optimization levels 0/1/2)
// =================================================================

func (m *Minimatch) preprocess(globParts [][]string) [][]string {
	o := m.o
	if o.Noglobstar {
		for i := range globParts {
			for j := 0; j < len(globParts[i]); j++ {
				if globParts[i][j] == "**" {
					globParts[i][j] = "*"
				}
			}
		}
	}
	level := o.optimizationLevel()
	if level >= 2 {
		globParts = m.firstPhasePreProcess(globParts)
		globParts = m.secondPhasePreProcess(globParts)
	} else if level >= 1 {
		globParts = m.levelOneOptimize(globParts)
	} else {
		globParts = m.adjacentGlobstarOptimize(globParts)
	}
	return globParts
}

// adjacentGlobstarOptimize: <pre>/**/**/<rest> -> <pre>/**/<rest> (level 0).
func (m *Minimatch) adjacentGlobstarOptimize(globe [][]string) [][]string {
	out := make([][]string, len(globe))
	for k, parts := range globe {
		res := []string{}
		prevDouble := false
		for _, p := range parts {
			if p == "**" && prevDouble {
				continue
			}
			res = append(res, p)
			prevDouble = (p == "**")
		}
		out[k] = res
	}
	return out
}

// levelOneOptimize: drop adjacent ** and resolve .. <pre>/<p>/../ -> <pre>/
// (the default level — CLSI and oracle run here).
func (m *Minimatch) levelOneOptimize(globe [][]string) [][]string {
	out := make([][]string, len(globe))
	for k, parts := range globe {
		set := []string{}
		for _, part := range parts {
			if len(set) > 0 {
				prev := set[len(set)-1]
				if part == "**" && prev == "**" {
					continue
				}
				if part == ".." {
					if prev != "" && prev != ".." && prev != "." && prev != "**" {
						set = set[:len(set)-1]
						continue
					}
				}
			}
			set = append(set, part)
		}
		if len(set) == 0 {
			set = []string{""}
		}
		out[k] = set
	}
	return out
}

// indexOfStr: first index of needle in hay at or after start, else -1.
func indexOfStr(hay []string, needle string, start int) int {
	for i := start; i >= 0 && i < len(hay); i++ {
		if hay[i] == needle {
			return i
		}
	}
	// forward scan (start < 0 means from 0)
	for i := max(0, start); i < len(hay); i++ {
		if hay[i] == needle {
			return i
		}
	}
	return -1
}

func spliceRemove(parts []string, start, n int) []string {
	out := make([]string, 0, max(0, len(parts)-n))
	out = append(out, parts[:start]...)
	out = append(out, parts[start+n:]...)
	return out
}

func spliceReplace(parts []string, s, n int, v string) []string {
	out := make([]string, 0, max(0, len(parts)-n)+1)
	out = append(out, parts[:s]...)
	out = append(out, v)
	out = append(out, parts[s+n:]...)
	return out
}

func hasDotPrefix(s string) bool {
	return len(s) > 0 && s[0] == '.'
}

// firstPhasePreProcess (level 2). CLSI/oracle use level 1 (dead code here).
func (m *Minimatch) firstPhasePreProcess(globe0 [][]string) [][]string {
	globe := globe0
	for {
		didSomething := false
		for k := range globe {
			parts := globe[k]
			gs := -1
			for {
				gs = indexOfStr(parts, "**", gs+1)
				if gs < 0 {
					break
				}
				gss := gs
				for gss+1 < len(parts) && parts[gss+1] == "**" {
					gss++
				}
				if gss > gs {
					parts = spliceRemove(parts, gs+1, gss-gs)
					didSomething = true
				}
				if gs+1 >= len(parts) {
					break
				}
				next := parts[gs+1]
				if next != ".." {
					break
				}
				p := ""
				p2 := ""
				if gs+2 < len(parts) {
					p = parts[gs+2]
				}
				if gs+3 < len(parts) {
					p2 = parts[gs+3]
				}
				if p == "" || p == "." || p == ".." || p2 == "" || p2 == "." || p2 == ".." {
					break
				}
				didSomething = true
				parts = spliceRemove(parts, gs, 1)
				globe[k] = parts
				other := make([]string, len(parts)+1)
				copy(other, parts[:gs])
				other[gs] = "**"
				copy(other[gs+1:], parts[gs:])
				globe = append(globe, other)
				break
			}
			globe[k] = parts
			if !m.o.PreserveMultipleSlashes {
				for i := 1; i < len(parts)-1; i++ {
					p := parts[i]
					if i == 1 && p == "" && parts[0] == "" {
						continue
					}
					if p == "." || p == "" {
						didSomething = true
						parts = spliceRemove(parts, i, 1)
						i--
					}
				}
				globe[k] = parts
				if parts[0] == "." && len(parts) == 2 && (parts[1] == "." || parts[1] == "") {
					didSomething = true
					parts = parts[:len(parts)-1]
					globe[k] = parts
				}
			}
			dd := 0
			for {
				dd = indexOfStr(parts, "..", dd+1)
				if dd < 0 {
					break
				}
				p := ""
				if dd-1 >= 0 {
					p = parts[dd-1]
				}
				if p != "" && p != "." && p != ".." && p != "**" {
					didSomething = true
					needDot := dd == 1 && dd+1 < len(parts) && parts[dd+1] == "**"
					if needDot {
						parts = spliceReplace(parts, dd-1, 2, ".")
					} else {
						parts = spliceRemove(parts, dd-1, 2)
					}
					if len(parts) == 0 {
						parts = append(parts, "")
					}
					globe[k] = parts
					dd -= 2
				}
			}
			globe[k] = parts
		}
		if !didSomething {
			break
		}
	}
	return globe
}

// secondPhasePreProcess: pattern-set dedupes (level 2).
func (m *Minimatch) secondPhasePreProcess(globe [][]string) [][]string {
	for i := 0; i < len(globe)-1; i++ {
		for j := i + 1; j < len(globe); j++ {
			if matched := m.partsMatch(globe[i], globe[j], !m.o.PreserveMultipleSlashes); matched != nil {
				globe[i] = nil
				globe[j] = matched
				break
			}
		}
	}
	out := [][]string{}
	for _, g := range globe {
		if len(g) > 0 {
			out = append(out, g)
		}
	}
	return out
}

// partsMatch ports Minimatch#partsMatch (level 2).
func (m *Minimatch) partsMatch(a, b []string, emptyGSMatch bool) []string {
	ai, bi := 0, 0
	result := []string{}
	which := ""
	for ai < len(a) && bi < len(b) {
		if a[ai] == b[bi] {
			if which == "b" {
				result = append(result, b[bi])
			} else {
				result = append(result, a[ai])
			}
			ai++
			bi++
		} else if emptyGSMatch && a[ai] == "**" && ai+1 < len(a) && b[bi] == a[ai+1] {
			result = append(result, a[ai])
			ai++
		} else if emptyGSMatch && b[bi] == "**" && bi+1 < len(b) && a[ai] == b[bi+1] {
			result = append(result, b[bi])
			bi++
		} else if a[ai] == "*" && b[bi] != "" && (m.o.Dot || !hasDotPrefix(b[bi])) && b[bi] != "**" {
			if which == "b" {
				return nil
			}
			which = "a"
			result = append(result, a[ai])
			ai++
			bi++
		} else if b[bi] == "*" && a[ai] != "" && (m.o.Dot || !hasDotPrefix(a[ai])) && a[ai] != "**" {
			if which == "a" {
				return nil
			}
			which = "b"
			result = append(result, b[bi])
			ai++
			bi++
		} else {
			return nil
		}
	}
	if len(a) == len(b) {
		return result
	}
	return nil
}

// =================================================================
// match machinery
// =================================================================

// firstGS: index of first globstar cell at or after `from`, else -1.
func firstGS(pattern []mCell, from int) int {
	for i := from; i < len(pattern); i++ {
		if pattern[i].globstar {
			return i
		}
	}
	return -1
}

// lastGS: index of LAST globstar cell, else -1.
func lastGS(pattern []mCell) int {
	for i := len(pattern) - 1; i >= 0; i-- {
		if pattern[i].globstar {
			return i
		}
	}
	return -1
}

// levelTwoFileOptimize (level 2 only; CLSI uses level 1 so this is dead).
// POSIX: the isWindows branch is removed.
func (m *Minimatch) levelTwoFileOptimize(parts []string) []string {
	file := parts
	for {
		didSomething := false
		if !m.o.PreserveMultipleSlashes {
			for i := 1; i < len(file)-1; i++ {
				p := file[i]
				if i == 1 && p == "" && file[0] == "" {
					continue
				}
				if p == "." || p == "" {
					didSomething = true
					file = spliceRemove(file, i, 1)
					i--
				}
			}
		}
		dd := 0
		for {
			dd = indexOfStr(file, "..", dd+1)
			if dd < 0 {
				break
			}
			p := ""
			if dd-1 >= 0 {
				p = file[dd-1]
			}
			// POSIX: isWindows && /^[a-z]:$/i.test(p) is always false
			if p != "" && p != "." && p != ".." && p != "**" {
				didSomething = true
				file = spliceRemove(file, dd-1, 2)
				dd -= 2
			}
		}
		if !didSomething {
			break
		}
	}
	if len(file) == 0 {
		return []string{""}
	}
	return file
}

// matchOne ports Minimatch#matchOne (windows/UNC not ported).
func (m *Minimatch) matchOne(file []string, pattern []mCell, partial bool) (bool, error) {
	level := m.o.optimizationLevel()
	if level >= 2 {
		file = m.levelTwoFileOptimize(file)
	}
	if firstGS(pattern, 0) >= 0 {
		return m.matchGlobstar(file, pattern, partial, 0, 0)
	}
	return m.matchOneInner(file, pattern, partial, 0, 0)
}

// matchOneInner ports #matchOne (the per-cell walk).
func (m *Minimatch) matchOneInner(file []string, pattern []mCell, partial bool, fi, pi int) (bool, error) {
	fl, pl := len(file), len(pattern)
	for fi < fl && pi < pl {
		p := pattern[pi]
		f := file[fi]
		if p.globstar {
			return false, nil
		}
		hit, err := p.test(f)
		if err != nil {
			return false, err
		}
		if !hit {
			return false, nil
		}
		fi++
		pi++
	}
	if fi == fl && pi == pl {
		return true, nil
	}
	if fi == fl {
		return partial, nil
	}
	if pi == pl {
		return fi == fl-1 && file[fi] == "", nil
	}
	return false, nil
}

// badDot reports a 'bad dot' file segment for body-section walking.
func (m *Minimatch) badDot(f string) bool {
	return f == "." || f == ".." || (!m.o.Dot && len(f) > 0 && f[0] == '.')
}

// matchGlobstar ports Minimatch#matchGlobstar (windows/UNC not ported).
func (m *Minimatch) matchGlobstar(file []string, pattern []mCell, partial bool, fileIndex, patternIndex int) (bool, error) {
	firstgs := firstGS(pattern, patternIndex)
	lastgs := lastGS(pattern)

	var head, body, tail []mCell
	if partial {
		head = pattern[patternIndex:firstgs]
		body = pattern[firstgs+1:]
		tail = nil
	} else {
		head = pattern[patternIndex:firstgs]
		body = nil
		if lastgs > firstgs {
			body = pattern[firstgs+1 : lastgs]
		}
		tail = pattern[lastgs+1:]
	}

	// check the head
	if len(head) > 0 {
		if fileIndex+len(head) > len(file) {
			// JS: file.slice(fileIndex, fileIndex+head.length) too short to match
			return false, nil
		}
		fileHead := file[fileIndex : fileIndex+len(head)]
		ok, err := m.matchOneInner(fileHead, head, partial, 0, 0)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
		fileIndex += len(head)
	}

	// check the tail
	fileTailMatch := 0
	if len(tail) > 0 {
		if len(tail)+fileIndex > len(file) {
			return false, nil
		}
		tailStart := len(file) - len(tail)
		ok, err := m.matchOneInner(file, tail, partial, tailStart, 0)
		if err != nil {
			return false, err
		}
		if ok {
			fileTailMatch = len(tail)
		} else {
			// affordance: a/**/* matching a/b/
			if (len(file) > 0 && file[len(file)-1] != "") ||
				fileIndex+len(tail) == len(file) {
				return false, nil
			}
			tailStart--
			ok, err = m.matchOneInner(file, tail, partial, tailStart, 0)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, nil
			}
			fileTailMatch = len(tail) + 1
		}
	}

	// empty body: just check for bad dots
	if len(body) == 0 {
		sawSome := fileTailMatch != 0
		for i := fileIndex; i < len(file)-fileTailMatch; i++ {
			f := file[i]
			sawSome = true
			if f == "." || f == ".." || (!m.o.Dot && len(f) > 0 && f[0] == '.') {
				return false, nil
			}
		}
		if partial {
			return true, nil
		}
		return sawSome, nil
	}

	sections := []bodySeg{{cells: nil, after: 0}}
	nonGsParts := 0
	nonGsSums := []int{0}
	for _, b := range body {
		if b.globstar {
			nonGsSums = append(nonGsSums, nonGsParts) // cumulative (JS: never reset)
			sections = append(sections, bodySeg{after: 0})
		} else {
			last := &sections[len(sections)-1].cells
			*last = append(*last, b)
			nonGsParts++
		}
	}
	// compute "after" bounds (backwards indexing)
	fileLength := len(file) - fileTailMatch
	for i := len(sections) - 1; i >= 0; i-- {
		sections[i].after = fileLength - (nonGsSums[len(nonGsSums)-1-i] + len(sections[i].cells))
	}

	result, err := m.matchGlobStarBodySections(file, sections, fileIndex, 0, partial, 0, fileTailMatch != 0)
	if err != nil {
		return false, err
	}
	// JS: return !!sub  (null->false, true->true, false->false)
	return result == triHit, nil
}

// matchGlobStarBodySections ports #matchGlobStarBodySections (JS returns
// boolean|null; here tri-state: triWalk=null, triHit=true, triFail=false).
func (m *Minimatch) matchGlobStarBodySections(
	file []string,
	sections []bodySeg,
	fileIndex, bodyIndex int,
	partial bool,
	depth int,
	sawTail bool,
) (int, error) {
	if bodyIndex >= len(sections) {
		// past the last section: just check no bad dots
		for i := fileIndex; i < len(file); i++ {
			sawTail = true
			f := file[i]
			if m.badDot(f) {
				return triFail, nil
			}
		}
		if sawTail {
			return triHit, nil
		}
		return triWalk, nil
	}

	bs := sections[bodyIndex]
	body := bs.cells
	after := bs.after
	for fileIndex <= after {
		// JS: file.slice(0, fileIndex + body.length) — the tail condition
		// (fi==fl-1 && file[fi]=="" or fi==fl&&pi==pl) depends on file length,
		// so we MUST pass a view truncated to fileIndex+len(body), not the
		// full file.
		view := file[:fileIndex+len(body)]
		if fileIndex+len(body) > len(file) {
			view = file
		}
		ok, err := m.matchOneInner(view, body, partial, fileIndex, 0)
		if err != nil {
			return triFail, err
		}
		if ok && depth < m.maxGSRec {
			sub, err := m.matchGlobStarBodySections(
				file, sections, fileIndex+len(body), bodyIndex+1,
				partial, depth+1, sawTail)
			if err != nil {
				return triFail, err
			}
			if sub != triFail {
				return sub, nil
			}
		}
		// JS: give up the walk immediately on a bad-dot segment.
		if m.badDot(file[fileIndex]) {
			return triFail, nil
		}
		fileIndex++
	}
	// walked off: no point continuing (JS: return partial || null)
	if partial {
		return triHit, nil
	}
	return triWalk, nil
}
