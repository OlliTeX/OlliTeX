package texfmt

// bibtex.go — byte-parity reimplementation of bibtex-tidy@1.15.1 tidy()
// with the EXACT option set of the tex-autoformatter controller:
//
//	curly: true, numeric: true, align: 14, blankLines: true,
//	sortFields: true, stripEnclosingBraces: true, trailingCommas: true,
//	removeEmptyFields: true, removeDuplicateFields: false,
//	encodeUrls: false, tidyComments: false
//
// Behavior is pinned against a live corpus captured from the Node
// library (75 inputs, gotidy_corpus.json — texfmt_test.go): every input
// either renders byte-identically or throws (any throw -> the
// controller's 500 {"error":"Formatting failed"}).
//
// Corpus-anchored rules (the marked ones were subtle):
//   - CRLF and lone CR normalize to \n first.
//   - Top level = blocks: entry, @string, @preamble, @comment{...}
//     (raw even entry-shaped), %-comment lines (raw incl. trailing \n).
//   - Joins: "\n\n" before a block whose PREVIOUS block is not
//     comment-like; NOTHING after a comment-like block; a %-comment
//     line carries its own trailing \n. [trap: @comment→entry = zero]
//   - Leading whitespace is preserved only before a leading comment
//     block, dropped before everything else. [trap]
//   - Entry: @<lowercase>{key,fields} — type lowercased, names
//     lowercased on render; key with whitespace or a non-`,` first
//     token after the key is a parse error.
//   - Field order: DEFAULT_FIELD_SORT by lowercase name; unknowns keep
//     input order AFTER all knowns (stable).
//   - A value = # segments; each segment is braced, ONE quoted token,
//     or ONE bare token (no whitespace). Any other mix is a parse
//     error ("Syntax Error in concat"); an empty value (v = ,) is a
//     parse error; a bare % at value start starts a comment. [trap]
//   - preferCurly: bare/quoted -> braced, except month == jan..dec
//     (exact 3-letter lowercase) which stays bare.
//   - preferNumeric: braced inner matching ^[1-9][0-9]*$ renders bare.
//   - stripEnclosingBraces: inner is a single-curly chain (every level
//     one curly child) -> strip exactly one outer layer; branching
//     content untouched. 2->1, 3->2, 4->3 braces; {a}b stays 2. [trap]
//   - removeEmptyFields: all parts empty after trim -> field dropped
//     (entries may end with zero fields).
//   - Braced render: {latex} unless the latex is exactly two nested
//     curlys (then bare) — 4-brace plain renders 3 braces. [trap]
//   - Latex stringify: chars run through escapeChar() except inside
//     math ($...$ raw); newline -> space; value ends trimmed; internal
//     whitespace preserved; commands with braced args verbatim; stray
//     $ -> \$; " inside braces -> \" . [trap]
//   - Layout: name padded to max(14-len,1) spaces before "= "; indent
//     2; every field (incl. last) gets a trailing comma.
//   - Empty/whitespace-only input -> "\n"; final output trimEnd + \n.

import (
	"regexp"
	"sort"
	"strings"
)

// FormatBib runs the tidy() parity pipeline; an error means the Node
// library threw (500 Formatting failed upstream).
func FormatBib(src string) (string, error) {
	c := convertCRLF(src)
	doc, err := parseBib(c)
	if err != nil {
		return "", err
	}
	for i := range doc.blocks {
		b := &doc.blocks[i]
		if b.kind != btEntry {
			continue
		}
		for fi := range b.fields {
			f := &b.fields[fi]
			for pi := range f.parts {
				p := &f.parts[pi]
				// preferCurly (literal/quoted -> braced; month skip).
				if (p.kind == partLiteral || p.kind == partQuoted) &&
					!(f.name == "month" && monthAlias[p.text]) {
					p.kind = partBraced
				}
			}
			for pi := range f.parts {
				p := &f.parts[pi]
				// preferNumeric (stringified value ^[1-9][0-9]*$).
				s := stringifyLatex(parseLatex(p.text))
				if p.kind == partBraced && numericRe.MatchString(s) {
					p.kind = partLiteral
					p.text = s
				}
			}
			for pi := range f.parts {
				p := &f.parts[pi]
				// stripEnclosingBraces: exactly one un-nested brace layer
				// (stringified content is {X} with X brace-free).
				if p.kind == partBraced {
					if m := innerXRe.FindStringSubmatch(stringifyLatex(parseLatex(p.text))); m != nil {
						p.text = m[1]
					}
				}
			}
		}
		kept := b.fields[:0]
		for _, f := range b.fields {
			empty := true
			for _, p := range f.parts {
				if strings.TrimSpace(p.text) != "" {
					empty = false
					break
				}
			}
			if !empty {
				kept = append(kept, f)
			}
		}
		b.fields = kept
		// sortFields (stable; unknown last)
		sort.SliceStable(b.fields, func(x, y int) bool {
			sx := fieldOrderIndex(strings.ToLower(b.fields[x].name))
			sy := fieldOrderIndex(strings.ToLower(b.fields[y].name))
			if sx == -1 && sy == -1 {
				return false
			}
			if sx == -1 {
				return false
			}
			if sy == -1 {
				return true
			}
			return sx < sy
		})
	}
	return renderDoc(doc), nil
}

// ---------------- data model ----------------

// top-level node kinds (bibtex-tidy 1.15.1: blocks + raw text nodes).
const (
	btEntry    = iota // parsed entry (@name{...} / @name(...))
	btString          // @string{...}: raw pass-through
	btPreamble        // @preamble{...}: raw pass-through
	btComment         // @comment{...}: raw pass-through
	btRawEntry        // reserved (bracket/paren variants render as text in the real lib)
	btText            // top-level raw text (includes %-lines, @name-without-value,
	//             stray tokens) — verbatim pass-through [trap]
)

const (
	partBraced = iota
	partQuoted
	partLiteral
)

type btPart struct {
	kind int
	text string
}

type btField struct {
	name  string
	parts []btPart
}

type btBlock struct {
	kind   int
	typ    string
	key    string
	fields []btField
	raw    string
	// btText: text = raw run; prefix/root-ws captured below.
	text   string
	prefix string
}

type btDoc struct {
	blocks   []btBlock
	hasLead  bool
	leadText string
}

// ---------------- helpers ----------------

func convertCRLF(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

var numericRe = regexp.MustCompile(`^[1-9][0-9]*$`)

var innerXRe = regexp.MustCompile(`^\{([^{}]*)\}$`)

var monthAlias = map[string]bool{
	"jan": true, "feb": true, "mar": true, "apr": true,
	"may": true, "jun": true, "jul": true, "aug": true,
	"sep": true, "oct": true, "nov": true, "dec": true,
}

var fieldOrder = []string{
	"title", "shorttitle", "author", "year", "month", "day",
	"journal", "booktitle", "location", "on", "publisher",
	"address", "series", "volume", "number", "pages", "doi",
	"isbn", "issn", "url", "urldate", "copyright", "category",
	"note", "metadata",
}

func fieldOrderIndex(lowerName string) int {
	for i, n := range fieldOrder {
		if n == lowerName {
			return i
		}
	}
	return -1
}

// curlyChainDepth counts nested single-curly levels of the braced inner
// text (0 = branching/no nested curly).
func curlyChainDepth(inner string) int {
	d := 0
	s := inner
	for {
		if !strings.HasPrefix(s, "{") {
			return d
		}
		depth, end := 0, -1
		for i := 0; i < len(s); i++ {
			switch s[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = i
				}
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 || end != len(s)-1 {
			return d // unbalanced or trailing content -> branching/invalid at this level
		}
		d++
		s = s[1:end]
	}
}

// partFinalText is the part's rendered content (without the outer braces
// of braced parts), for empty-check purposes.
func partFinalText(p *btPart) string {
	switch p.kind {
	case partBraced:
		return p.text
	case partQuoted:
		return p.text
	default:
		return p.text
	}
}

// ---------------- parsing ----------------

type bibError struct{ msg string }

func (e *bibError) Error() string { return "bibtex: " + e.msg }

func parseBib(src string) (*btDoc, error) {
	doc := &btDoc{}
	i, n := 0, len(src)
	// BOM at offset 0 is dropped by the real pipeline (bom corpus).
	if len(src) >= 3 && src[0] == 0xEF && src[1] == 0xBB && src[2] == 0xBF {
		src = src[3:]
		n -= 3
	}
	ws := "" // whitespace accumulated at root level (flushed to the next node)
	for i < n {
		if src[i] == '@' && validBlockStart(src, i) {
			p := ws
			ws = ""
			j := i + 1
			for j < n && isBibNameChar(src[j]) {
				j++
			}
			typ := src[i+1 : j]
			k := j
			for k < n && (src[k] == ' ' || src[k] == '\t') {
				k++
			}
			open := src[k] // '{' or '('
			// value end: first brace/paren-balanced close.
			end := -1
			depth := 0
			for m := k; m < n; m++ {
				switch src[m] {
				case '{', '(':
					depth++
				case '}', ')':
					depth--
					if depth == 0 {
						end = m
					}
				}
				if end >= 0 {
					break
				}
			}
			switch {
			case typ == "string":
				if end < 0 {
					doc.blocks = append(doc.blocks, btBlock{kind: btString, typ: typ, prefix: p, raw: src[i:]})
					return doc, nil
				}
				doc.blocks = append(doc.blocks, btBlock{kind: btString, typ: typ, prefix: p, raw: src[i : end+1]})
			case typ == "preamble":
				if end < 0 {
					doc.blocks = append(doc.blocks, btBlock{kind: btPreamble, typ: typ, prefix: p, raw: src[i:]})
					return doc, nil
				}
				doc.blocks = append(doc.blocks, btBlock{kind: btPreamble, typ: typ, prefix: p, raw: src[i : end+1]})
			case typ == "comment":
				if end < 0 {
					doc.blocks = append(doc.blocks, btBlock{kind: btComment, typ: typ, prefix: p, raw: src[i:]})
					return doc, nil
				}
				doc.blocks = append(doc.blocks, btBlock{kind: btComment, typ: typ, prefix: p, raw: src[i : end+1]})
			default:
				if end < 0 {
					end = n // implicit close (missing-close corpus)
				}
				inner := src[k+1 : end]
				var b btBlock
				b.kind = btEntry
				b.typ = typ
				b.prefix = p
				if err := parseEntryInner(inner, &b, open); err != nil {
					return nil, err
				}
				doc.blocks = append(doc.blocks, b)
			}
			i = end + 1
			continue
		}
		if isWSByte(src[i]) {
			ws += src[i : i+1]
			i++
			continue
		}
		// top-level text node: runs until a valid '@name{(...)' boundary whose
		// previous char is whitespace or '}' (real-lib text state).
		k := i
		for k < n {
			if src[k] == '@' && (k == 0 || blockSep(src[k-1])) && validBlockStart(src, k) {
				break
			}
			k++
		}
		if k == i {
			k = i + 1
		}
		doc.blocks = append(doc.blocks, btBlock{kind: btText, prefix: ws, text: src[i:k]})
		ws = ""
		i = k
	}
	return doc, nil
}

// validBlockStart: '@' + non-empty name + (spaces) + '{' or '('.
func validBlockStart(s string, i int) bool {
	n := len(s)
	j := i + 1
	if j >= n || !isBibNameChar(s[j]) {
		return false
	}
	for j < n && isBibNameChar(s[j]) {
		j++
	}
	for j < n && (s[j] == ' ' || s[j] == '\t') {
		j++
	}
	return j < n && (s[j] == '{' || s[j] == '(')
}

// blockSep: chars that make a following '@' a block boundary (real lib:
// /[\s\r\n}]/).
func blockSep(c byte) bool {
	return c == '}' || isWSByte(c) || c == 0xA0 // NBSP approximates JS \s extras
}

func isWSByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func indexByteFrom(s string, b byte, from int) int {
	for i := from; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func isPrelimType(typ string) bool {
	switch typ {
	case "string", "preamble", "comment":
		return true
	}
	return false
}

func btRawKind(typ string) int {
	switch typ {
	case "string":
		return btString
	case "preamble":
		return btPreamble
	default:
		return btComment
	}
}

func isBibNameChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// braceEnd returns the index of the closing brace matching src[start] (a
// '{'), or -1 when unbalanced before end.
func braceEnd(src string, start, end int) int {
	depth := 0
	for i := start; i < end; i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func parseEntryInner(inner string, block *btBlock, open byte) error {
	closeB := byte('}')
	if open == '(' {
		closeB = ')'
	}
	// key = up to first top-level ',' or the entry close char (corpus:
	// whitespace inside key is a parse error; empty key OK; specials
	// like :.-_ OK). '}' / ')' each track their own nesting.
	keyEnd := -1
	brace, paren := 0, 0
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '{':
			brace++
		case '(':
			paren++
		case '}':
			if brace > 0 {
				brace--
			} else if closeB == '}' && keyEnd < 0 {
				keyEnd = i
			}
		case ')':
			if paren > 0 {
				paren--
			} else if closeB == ')' && keyEnd < 0 {
				keyEnd = i
			}
		case ',':
			if brace == 0 && paren == 0 && keyEnd < 0 {
				keyEnd = i
			}
		}
		if keyEnd >= 0 {
			break
		}
	}
	// Key/field split: a top-level '=' inside the first segment (before any
	// top-level ',') means the segment is a field and the entry has no key
	// (r6-paren-comment / eq-key probes). A key with whitespace errors.
	fieldStart := len(inner)
	seg := inner
	if keyEnd >= 0 {
		seg = inner[:keyEnd]
	}
	if eqp := indexTopLevel(seg, '=', 0); eqp >= 0 {
		block.key = ""
		fieldStart = 0
	} else if keyEnd >= 0 {
		fieldStart = keyEnd + 1
		block.key = strings.TrimSpace(seg)
		if !isKeyToken(seg) {
			return &bibError{"Syntax Error in entry (The entry key cannot contain whitespace)"}
		}
	} else {
		// no top-level comma/close: the whole thing is a bare key-ish token
		// (or empty). Corpus: "@article{a title {T}...}" errors.
		if strings.TrimSpace(inner) != "" && !isKeyToken(inner) {
			return &bibError{"Syntax Error in entry (The entry key cannot contain whitespace)"}
		}
		block.key = strings.TrimSpace(inner)
		return nil
	}
	if fieldStart >= len(inner) {
		return nil
	}
	rest := inner[fieldStart:]
	if strings.TrimSpace(rest) == "" {
		return nil
	}
	// fields: name = up to '='; value up to top-level ','.
	for {
		rest = strings.TrimLeft(rest, " \t\n")
		if rest == "" {
			break
		}
		eq := indexTopLevel(rest, '=', 0)
		if eq < 0 {
			return &bibError{"Syntax Error in field (no '=')"}
		}
		name := strings.TrimSpace(rest[:eq])
		if name == "" {
			return &bibError{"Syntax Error in field (empty name)"}
		}
		vs := eq + 1
		vend := indexTopLevelValueEnd(rest, vs)
		if vend < 0 {
			vend = len(rest)
		}
		rawVal := rest[vs:vend]
		parts, err := parseValue(rawVal)
		if err != nil {
			return err
		}
		block.fields = append(block.fields, btField{name: name, parts: parts})
		rest = rest[vend:]
		if strings.TrimLeft(rest, " \t\n") != "" && !strings.HasPrefix(strings.TrimLeft(rest, " \t\n"), ",") {
			// entry end or stray content: stray content is a parse error.
			stripped := strings.TrimLeft(rest, " \t\n")
			if stripped == "" {
				break
			}
			if stripped[0] != ',' {
				return &bibError{"Syntax Error in entry (unexpected content)"}
			}
		}
		rest = strings.TrimLeft(rest, " \t\n")
		if !strings.HasPrefix(rest, ",") {
			break
		}
		rest = rest[1:]
	}
	return nil
}

func isKeyToken(s string) bool {
	s = strings.TrimSpace(s)
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' {
			return false
		}
	}
	return true
}

// indexTopLevel finds ch at top level (brace depth 0, ignoring quoted
// spans) scanning from 0.
func indexTopLevel(s string, ch byte, from int) int {
	depth := 0
	inQuote := false
	for i := from; i < len(s); i++ {
		c := s[i]
		if inQuote {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inQuote = false
			}
			continue
		}
		switch c {
		case '"':
			inQuote = true
		case '{':
			depth++
		case '}':
			depth--
		default:
			if c == ch && depth == 0 {
				return i
			}
		}
	}
	return -1
}

// indexTopLevelValueEnd returns the index of the next top-level ',' after
// pos (scanning over braces/quotes), or -1 when none (value runs to end).
// Quotes are only recognized at depth 0 (braces contain quotes, never the
// reverse at the bibtex value level).
func indexTopLevelValueEnd(s string, pos int) int {
	depth := 0
	inQuote := false
	for i := pos; i < len(s); i++ {
		c := s[i]
		if inQuote {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inQuote = false
			}
			continue
		}
		switch c {
		case '"':
			if depth == 0 {
				inQuote = true
			}
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func parseValue(raw string) ([]btPart, error) {
	v := strings.TrimLeft(raw, " \t\n")
	if v == "" {
		return nil, &bibError{"Syntax Error in concat (undefined)"} // v = ,
	}
	if v[0] == '%' {
		// % at value start: bibtex comment — the rest of the line is a
		// comment (corpus: parse error when the entry continues below).
		rest := raw[strings.IndexByte(raw, '\n')+1:]
		if strings.TrimSpace(rest) != "" {
			return nil, &bibError{"Syntax Error in concat (% comment in value)"}
		}
		return nil, &bibError{"Syntax Error in concat (% comment in value)"}
	}
	// Split on top-level '#' into segments.
	segs := splitHashSegments(raw)
	var parts []btPart
	for _, seg := range segs {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			return nil, &bibError{"Syntax Error in concat (undefined)"}
		}
		switch seg[0] {
		case '{':
			end := braceEnd(seg, 0, len(seg))
			if end < 0 || end != len(seg)-1 {
				return nil, &bibError{"Syntax Error in concat (undefined)"}
			}
			parts = append(parts, btPart{kind: partBraced, text: seg[1:end]})
		case '"':
			closeIdx := quotedEnd(seg)
			if closeIdx < 0 || strings.TrimSpace(seg[closeIdx+1:]) != "" {
				return nil, &bibError{"Syntax Error in concat (undefined)"}
			}
			parts = append(parts, btPart{kind: partQuoted, text: seg[1:closeIdx]})
		default:
			if !isBareToken(seg) {
				return nil, &bibError{"Syntax Error in concat (undefined)"}
			}
			parts = append(parts, btPart{kind: partLiteral, text: seg})
		}
	}
	if len(parts) == 0 {
		return nil, &bibError{"Syntax Error in concat (undefined)"}
	}
	return parts, nil
}

func splitHashSegments(raw string) []string {
	var segs []string
	depth := 0
	inQuote := false
	start := 0
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if inQuote {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inQuote = false
			}
			continue
		}
		switch c {
		case '"':
			inQuote = true
		case '{':
			depth++
		case '}':
			depth--
		case '#':
			if depth == 0 {
				segs = append(segs, raw[start:i])
				start = i + 1
			}
		}
	}
	segs = append(segs, raw[start:])
	return segs
}

func isBareToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '"' || r == '{' || r == '}' || r == ',' || r == '#' {
			return false
		}
	}
	return true
}

func quotedEnd(s string) int {
	for i := 1; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == '"' {
			return i
		}
	}
	return -1
}

// ---------------- rendering ----------------
// The real bibtex-tidy formats top level as: node.whitespacePrefix +
// node-content, joined, trimmed at end + final newline. Prefixes are
// rewritten by two transforms (in pipeline order):
//   reset-whitespace (keepCommentWhitespace=true when tidyComments=false):
//     a non-comment node gets a one-newline prefix unless the previous node is
//     comment-like (text or @comment); a text node followed by a block
//     that does not end in a newline gains one; comment-like nodes keep
//     their original prefix/content.
//   blank-lines (blankLines=true): every node whose previous sibling is
//     not comment-like gets prefix "\n\n" (overwriting reset).
// [trap] comment-like == top-level text node OR @comment block only
// (@string/@preamble are NOT comment-like and take the "\n\n" join).

func isCommentLikeKind(k int) bool {
	return k == btText || k == btComment
}

func renderDoc(doc *btDoc) string {
	blocks := doc.blocks
	// reset-whitespace pass (keepCommentWhitespace = true).
	var prev *btBlock
	for i := range blocks {
		bl := &blocks[i]
		if !isCommentLikeKind(bl.kind) {
			if prev != nil && prev.kind == btText && !strings.HasSuffix(prev.text, "\n") {
				prev.text = strings.TrimRight(prev.text, " \t\n") + "\n"
			}
			if prev != nil && !isCommentLikeKind(prev.kind) {
				bl.prefix = "\n"
			} else {
				bl.prefix = ""
			}
		}
		prev = bl
	}
	// blank-lines pass (blankLines = true).
	for i := 1; i < len(blocks); i++ {
		if !isCommentLikeKind(blocks[i-1].kind) {
			blocks[i].prefix = "\n\n"
		}
	}
	var b strings.Builder
	for i := range blocks {
		bl := &blocks[i]
		b.WriteString(bl.prefix)
		switch bl.kind {
		case btText:
			b.WriteString(bl.text)
		case btEntry:
			b.WriteString(renderEntry(bl))
		case btString, btPreamble, btComment, btRawEntry:
			b.WriteString(bl.raw)
		}
	}
	out := b.String()
	if strings.TrimSpace(out) == "" {
		return "\n"
	}
	out = strings.TrimRight(out, " \t\n")
	return out + "\n"
}

func renderEntry(bl *btBlock) string {
	var b strings.Builder
	b.WriteString("@")
	b.WriteString(strings.ToLower(bl.typ))
	b.WriteString("{")
	if bl.key != "" {
		b.WriteString(bl.key)
		b.WriteString(",")
	}
	for _, f := range bl.fields {
		name := strings.ToLower(f.name)
		b.WriteString("\n  ")
		b.WriteString(name)
		pad := 14 - len(name)
		if pad < 1 {
			pad = 1
		}
		b.WriteString(spaces(pad))
		b.WriteString("= ")
		b.WriteString(renderValueParts(f.parts))
		b.WriteString(",")
	}
	b.WriteString("\n}")
	return b.String()
}

func spaces(n int) string {
	return strings.Repeat(" ", n)
}

func renderValueParts(parts []btPart) string {
	outs := make([]string, 0, len(parts))
	for i := range parts {
		p := &parts[i]
		switch p.kind {
		case partLiteral:
			outs = append(outs, p.text)
		case partQuoted:
			outs = append(outs, `"`+p.text+`"`)
		case partBraced:
			outs = append(outs, renderBraced(p.text))
		}
	}
	return strings.TrimSpace(strings.Join(outs, " # "))
}

// renderBraced: {latex} unless the latex is exactly two nested curlys.
// trim inside-brace whitespace (multiline-value); double-enclose verbatim case trimmed too.
func renderBraced(inner string) string {
	children := parseLatex(inner)
	if isDoubleCurly(children) {
		return strings.TrimSpace(stringifyLatex(children))
	}
	return "{" + strings.TrimSpace(stringifyLatex(children)) + "}"
}

func isDoubleCurly(children []latexNode) bool {
	if len(children) != 1 || children[0].kind != nCurly {
		return false
	}
	inner := children[0].children
	if len(inner) != 1 || inner[0].kind != nCurly {
		return false
	}
	return true
}

// ---------------- latex AST (render model) ----------------

const (
	nText = iota
	nCurly
	nMath
	nCommand
)

type latexNode struct {
	kind     int
	text     string // nText
	children []latexNode
	command  string // nCommand (name incl. backslash)
	args     []latexNode
}

// parseLatex parses value content into the render AST.
func parseLatex(s string) []latexNode {
	var out []latexNode
	i := 0
	for i < len(s) {
		switch s[i] {
		case '{':
			end := braceEnd(s, i, len(s))
			if end < 0 {
				end = len(s) - 1
			}
			out = append(out, latexNode{kind: nCurly, children: parseLatex(s[i+1 : end])})
			i = end + 1
		case '$':
			j := i + 1
			for j < len(s) && s[j] != '$' {
				j++
			}
			if j < len(s) {
				out = append(out, latexNode{kind: nMath, text: s[i+1 : j]})
				i = j + 1
			} else {
				// stray $ -> escaped char
				out = append(out, latexNode{kind: nText, text: `\$`})
				i++
			}
		case '\\':
			j := i + 1
			if j < len(s) && s[j] == '\\' {
				out = append(out, latexNode{kind: nText, text: `\\`})
				i = j + 1
				break
			}
			// command: letters, or one non-alphabetic non-space char
			end := j
			for end < len(s) && isASCIIAlpha(s[end]) {
				end++
			}
			name := ""
			cmdEnd := end
			if end > j {
				name = s[j:end]
			} else if j < len(s) && s[j] != ' ' && s[j] != '\t' && s[j] != '\n' && s[j] != '{' {
				name = s[j : j+1]
				cmdEnd = j + 1
			}
			if name == "" {
				out = append(out, latexNode{kind: nText, text: `\`})
				i++
				break
			}
			node := latexNode{kind: nCommand, command: "\\" + name}
			// A braced arg binds only when directly adjacent (bibtex-tidy
			// command state: whitespace terminates the command, so "\& {x}"
			// renders "\& {x}" with the space intact).
			if cmdEnd < len(s) && s[cmdEnd] == '{' {
				end2 := braceEnd(s, cmdEnd, len(s))
				if end2 > 0 {
					node.args = []latexNode{latexNode{kind: nCurly, children: parseLatex(s[cmdEnd+1 : end2])}}
					out = append(out, node)
					i = end2 + 1
					break
				}
			}
			out = append(out, node)
			i = cmdEnd
		default:
			j := i
			// collect a run of plain text until the next structural char;
			// chars run through escapeText at render (" -> \", newline ->
			// space). A backslash-escaped char (e.g. \") is consumed by the
			// command branch as a verbatim unit.
			for j < len(s) && s[j] != '{' && s[j] != '}' && s[j] != '$' && s[j] != '\\' {
				j++
			}
			if j == i {
				j = i + 1
			}
			out = append(out, latexNode{kind: nText, text: s[i:j]})
			i = j
		}
	}
	return out
}

func isASCIIAlpha(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func stringifyLatex(nodes []latexNode) string {
	var b strings.Builder
	for i := range nodes {
		n := &nodes[i]
		switch n.kind {
		case nText:
			b.WriteString(escapeText(n.text))
		case nCurly:
			b.WriteString("{" + stringifyLatex(n.children) + "}")
		case nMath:
			b.WriteString("$" + n.text + "$")
		case nCommand:
			b.WriteString(n.command)
			for _, a := range n.args {
				b.WriteString(stringifyLatex([]latexNode{a}))
			}
		}
	}
	return b.String()
}

// escapeText: special-char escapes + newline -> space. Double quotes
// pass through verbatim (bibtex-tidy 1.15.1 has no 0022 map entry and
// its stringifyLaTeX is verbatim; only backslash-escaped units pass
// through as command nodes). Internal whitespace runs preserved.
func escapeText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\n' {
			b.WriteByte(' ')
			continue
		}
		if esc := escapeChar(r); esc != "" {
			b.WriteString(esc)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
