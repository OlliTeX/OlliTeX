package validtools

import (
	"strings"
	"unicode"
)

// Wire renders issues exactly like `fromError(zodError).toString()` from
// zod-validation-error v4, with the default messageBuilder options: prefix
// "Validation error", separator ": ", maxIssuesInMessage 99, issueSeparator
// "; ", unionSeparator " or ", includePath, forceTitleCase.
//
// mapIssue (1:1):
//
//   - invalid_union: per member, sub-issues mapped at issue.path + sub.path,
//     joined by "; " within a member, member texts deduped (first occurrence),
//     members joined " or ";
//   - every other issue: titleCase(message); non-empty path adds
//     " at index N" (single numeric segment) or ` at "joinPath(path)"`.
//
// Golden-pinned against Node (wire_test.go).
func Wire(issues []Issue) string {
	const maxIssues = 99
	n := len(issues)
	if n > maxIssues {
		n = maxIssues
	}
	msgs := make([]string, n)
	for i := 0; i < n; i++ {
		msgs[i] = mapIssue(issues[i])
	}
	msg := strings.Join(msgs, "; ")
	if msg != "" {
		return "Validation error: " + msg
	}
	return "Validation error"
}

func mapIssue(issue Issue) string {
	if issue.Code == "invalid_union" {
		var members []string
		seen := map[string]bool{}
		for _, group := range issue.InvalidUnion {
			parts := make([]string, len(group))
			for i, sub := range group {
				sub.Path = concatPaths(issue.Path, sub.Path)
				parts[i] = mapIssue(sub)
			}
			text := strings.Join(parts, "; ")
			if !seen[text] {
				seen[text] = true
				members = append(members, text)
			}
		}
		return strings.Join(members, " or ")
	}
	buf := titleCase(issue.Message)
	if len(issue.Path) > 0 {
		if len(issue.Path) == 1 && issue.Path[0].IsIndex {
			buf += " at index " + issue.Path[0].Value
		} else {
			buf += ` at "` + joinPath(issue.Path) + `"`
		}
	}
	return buf
}

// titleCase mirrors zod-validation-error's titleCase: UTF-16 code-unit
// first-char toUpperCase, rest verbatim (first rune only — identical for all
// literal messages our schemas produce).
func titleCase(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = toUpperRune(r[0])
	return string(r)
}

// toUpperRune mirrors JS `ch.toUpperCase()` for the single chars that ever
// appear as first message characters (ASCII letters; identity otherwise).
func toUpperRune(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - ('a' - 'A')
	}
	return r
}

// joinPath mirrors zod-validation-error's joinPath (lib/utils/joinPath):
// single segment renders raw (or `""` when empty); many segments reduce as
// numbers → `[N]`, quote-containing → `["escaped"]`, non-identifiers →
// `["..."]`, identifiers dot-joined.
func joinPath(segs []PathSeg) string {
	if len(segs) == 1 {
		v := segs[0].Value
		if v == "" {
			return `""`
		}
		return v
	}
	var acc string
	for _, s := range segs {
		v := s.Value
		switch {
		case s.IsIndex:
			acc += "[" + v + "]"
		case strings.Contains(v, `"`):
			acc += `["` + strings.ReplaceAll(v, `"`, `\"`) + `"]`
		case !isJSIdentifier(v):
			acc += `["` + v + `"]`
		default:
			if acc != "" {
				acc += "."
			}
			acc += v
		}
	}
	return acc
}

// isJSIdentifier reports whether segment s is a JS identifier for wire
// rendering, mirroring the Node identifier regex
// /[$\p{ID_Start}][$\u200C\u200D\p{ID_Continue}]*/u used via `.test`
// (unanchored): it matches whenever ANY character of s is an ID_Start
// character (the continue part is optional, so a single start char suffices).
// Go RE2 has no \p{ID_Start}; this uses unicode.IsLetter (all ID_Start
// characters are letters or _/$) which is exact for every key our services
// see: "1abc" -> true, "0" -> false, "a-b" -> true, CJK keys -> true.
func isJSIdentifier(s string) bool {
	for _, r := range s {
		switch {
		case r == '$' || r == '_':
			return true
		case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z':
			return true
		default:
			if isJSIdStart(r) {
				return true
			}
		}
	}
	return false
}

// isJSIdStart is the non-ASCII half of JS \p{ID_Start}: letters, plus CJK
// ideographs and a few more scripts' letters, which unicode.IsLetter covers
// (ID_Start is a subset of letter + CJK).
func isJSIdStart(r rune) bool {
	if r < 0x80 {
		return false // handled ASCII above
	}
	if r < 1<<16 {
		return unicode.IsLetter(r)
	}
	return false
}

func concatPaths(a, b []PathSeg) []PathSeg {
	out := make([]PathSeg, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}
