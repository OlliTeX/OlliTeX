package library

import (
	"regexp"
	"strings"
)

// ---------- LibrarySearch.mjs (SaaS machine-extracted) ----------
//
// fold map  = {æ→ae, œ→oe, ø→o, ß→ss, ł→l, đ→d, ð→d, þ→th, ŋ→ng}
// normalize = toLowerCase → NFD → strip \p{Mn} → fold map
// tokenize  = split normalized text on runs of punctuation/whitespace
//             (\p{P}\s), de-duplicate, drop empties
//
// The per-rune NFD/Mn/fold table (nfdbase.go) was generated from the Node
// oracle pipeline itself (this module cache has no x/text/normalize); the
// Unicode property \p{Mn} test below is only needed for completeness on
// runes the table misses (it is an exact subset check).

// nodeToLower + NFD strip + fold — EXACTLY the Node pipeline, per rune.
func normalizeSearchText(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if repl, ok := nfdBase[r]; ok {
			b.WriteString(repl)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// JS `\p{P}` = Unicode property Punctuation (all 11 P* categories);
// plus \s = whitespace. Split on runs of [P\s], drop empties, de-dup
// (order-preserved).
var punctSplitRe = regexp.MustCompile(`[\p{P}\s]+`)

func tokenizeSearchQuery(text string) []string {
	norm := normalizeSearchText(text)
	parts := punctSplitRe.Split(norm, -1)
	seen := map[string]bool{}
	out := []string{}
	for _, p := range parts {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// entrySearchBlob — key + type + field names + field values (shape-agnostic
// {name,value} pairs), joined by spaces, normalized.
func entrySearchBlob(key, typ string, fields []fieldOut) string {
	parts := []string{}
	if key != "" {
		parts = append(parts, key)
	}
	if typ != "" {
		parts = append(parts, typ)
	}
	for _, f := range fields {
		if f.Name != "" {
			parts = append(parts, f.Name)
		}
		if f.Value != "" {
			parts = append(parts, f.Value)
		}
	}
	return normalizeSearchText(strings.Join(parts, " "))
}

// escapeRegex — Node /.../g replace of [.*+?^${}()|[\]\\] with \X (order-
// insensitive: every listed char is escaped exactly once).
func escapeRegex(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '.', '*', '+', '?', '^', '$', '{', '}', '(', ')', '|', '[', ']', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			// Multi-byte UTF-8 (non-ASCII never matched by the JS class)
			// passes through byte-exact.
			b.WriteByte(c)
		}
	}
	return b.String()
}
