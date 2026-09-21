// escape.go - ports minimatch v10.2.6 escape() and unescape() (1:1 drop-in).
//
// Sources (minimatch v10.2.6, BlueOak-1.0-0):
//   escape:    https://github.com/isaacs/minimatch/blob/master/src/escape.ts
//   unescape:  https://github.com/isaacs/minimatch/blob/master/src/unescape.ts
//
// Upstream escape:
//   magicalBraces ? (wpne ? s.replace(/[?*()[\]{}]/g,'[$&]')
//                       : s.replace(/[?*()[\]\\{}]/g,'\\$&'))
//                 : (wpne ? s.replace(/[?*()[\]]/g,'[$&]')
//                         : s.replace(/[?*()[\]\\]/g,'\\$&'))
// Upstream unescape:
//   magicalBraces ? (wpne ? s.replace(/\[([^/\\])\]/g,'$1')
//                          : s.replace(/((?!\\).|^)\[([^/\\])\]/g,'$1$2')
//                              .replace(/\\([^/])/g,'$1'))
//                 : (wpne ? s.replace(/\[([^/\\{}])\]/g,'$1')
//                          : s.replace(/((?!\\).|^)\[([^/\\{}])\]/g,'$1$2')
//                              .replace(/\\([^/{}])/g,'$1'))
//
// NOTE the upstream asymmetry: escape() default magicalBraces=false,
// unescape() default magicalBraces=true. This port preserves that.

package minimatch

import (
	"errors"
	"strconv"
)

// PatternLimit is the maximum pattern length in bytes (mirrors the upstream
// v10 guard that rejects patterns above 64KiB). Patterns at or below this
// limit pass; longer ones are rejected so a malformed pattern cannot
// wedge regex compilation.
const PatternLimit = 65536

// assertValidPattern rejects patterns longer than PatternLimit. This is the
// Go analogue of the upstream "invalid pattern" fast-path (the upstream
// regex is compiled lazily; here we guard the length to mirror the
// documented 64KiB ceiling).
func assertValidPattern(pattern string) error {
	if len(pattern) <= PatternLimit {
		return nil
	}
	return errors.New("minimatch: pattern too long (limit " + strconv.Itoa(PatternLimit) + ")")
}

// DefaultOptions mirrors the minimatch()-level constructor defaults (src/index.ts).
// magicalBraces=true at the minimatch() level, though escape()/unescape()
// standalone functions default it to false/true respectively.
func DefaultOptions() *Options {
	return &Options{MagicalBraces: true}
}

// Escape ports escape() (src/escape.ts). nil options default to
// magicalBraces=false (upstream escape() default; differ from unescape's
// default of true).
func Escape(s string, o *Options) string {
	if o == nil {
		// upstream escape() default: magicalBraces=false.
		o = &Options{}
	}
	if o.WindowsPathsNoEscape {
		// Wrap each magic char in "[c]". Braces only when magicalBraces.
		var b []byte
		for ri := 0; ri < len(s); ri++ {
			c := s[ri]
			special := c == '*' || c == '?' || c == '(' || c == ')' || c == '[' || c == ']'
			if o.MagicalBraces && (c == '{' || c == '}') {
				special = true
			}
			if special {
				b = append(b, '[', c, ']')
			} else {
				b = append(b, c)
			}
		}
		return string(b)
	}
	// Prepend backslash to each magic char (and to backslash itself).
	var b []byte
	for ri := 0; ri < len(s); ri++ {
		c := s[ri]
		special := c == '*' || c == '?' || c == '(' || c == ')' || c == '[' || c == ']' || c == '\\'
		if o.MagicalBraces && (c == '{' || c == '}') {
			special = true
		}
		if special {
			b = append(b, '\\', c)
		} else {
			b = append(b, c)
		}
	}
	return string(b)
}

func unescapeBraceExcluded(c byte) bool {
	return c == '{' || c == '}'
}

// Unescape ports unescape() (src/unescape.ts).
func Unescape(s string, o *Options) string {
	if o == nil {
		o = DefaultOptions()
	}
	// upWpneUnescape when !wpne: pass1 (lookaround) + pass2 (\X strip);
	// wpne: single "[X]" unwrap.
	if o.WindowsPathsNoEscape {
		// s.replace(/\[(?:X)\]/g, '$1') where X = [^/\\] or [^/\\{}]
		var b []byte
		i := 0
		for i < len(s) {
			if s[i] == '[' && i+2 < len(s) && s[i+2] == ']' {
				inner := s[i+1]
				ok := inner != '/' && inner != '\\'
				if !o.MagicalBraces && unescapeBraceExcluded(inner) {
					ok = false
				}
				if ok {
					b = append(b, inner)
					i += 3
					continue
				}
			}
			b = append(b, s[i])
			i++
		}
		return string(b)
	}
	// pass1: /((?!\\).|^)\[([X])\]/g -> '$1$2'
	t := pass1NonWpne(s, o.MagicalBraces)
	// pass2: /\\([^/])/g or /\\([^/{}])/g -> '$1' (manual scan, no loop
	// post-increment to avoid double-advance).
	var b []byte
	i := 0
	for i < len(t) {
		if t[i] == '\\' && i+1 < len(t) {
			c := t[i+1]
			ok := c != '/'
			if !o.MagicalBraces && (c == '{' || c == '}') {
				ok = false
			}
			if ok {
				b = append(b, c)
				i += 2
				continue
			}
		}
		b = append(b, t[i])
		i++
	}
	return string(b)
}

// pass1NonWpne ports the lookaround regex
//
//	(magicalBraces ? /((?!\\).|^)\[([^/\\])\]/g
//	                : /((?!\\).|^)\[([^/\\{}])\]/g) -> '$1$2'
//
// as a manual leftmost-first scanner that mirrors JS alternation/lookahead
// semantics exactly.
func pass1NonWpne(s string, magicalBraces bool) string {
	var b []byte
	n := len(s)
	i := 0
	for i < n {
		innerOk := func(c byte) bool {
			if c == '/' || c == '\\' {
				return false
			}
			if !magicalBraces && (c == '{' || c == '}') {
				return false
			}
			return true
		}
		// Try match starting at i (leftmost-first semantics).
		// Alternate 1 (tried first in JS): 1-char group1 = s[i] (must not be '\').
		didMatch := false
		if i < n && s[i] != '\\' {
			// group1 = s[i], then needs "[X]" at i+1..i+3
			j := i + 1
			if j+2 < n && s[j] == '[' && innerOk(s[j+1]) && s[j+2] == ']' {
				b = append(b, s[i], s[j+1])
				i += 3
				didMatch = true
			}
		}
		if !didMatch {
			// Alternate 2: group1 = ^ (only at position 0), then "[X]" at 0..2
			if i == 0 && n >= 3 && s[0] == '[' && innerOk(s[1]) && s[2] == ']' {
				b = append(b, s[1])
				i += 3
			} else {
				b = append(b, s[i])
				i++
			}
		}
	}
	return string(b)
}
