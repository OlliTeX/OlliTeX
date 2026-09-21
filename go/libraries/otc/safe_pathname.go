package otc

import (
	"regexp"
	"strings"
	"unicode"
)

// Mirrors libraries/overleaf-editor-core/lib/safe_pathname.js 1:1.
//
// Regular expressions for Overleaf v2, taken from web's
// Features/Project/SafePath.js. All pathnames in a Snapshot must be clean:
// unambiguous, no directory traversal, no leading/trailing space, no '*'.

// maxPath is the maximum path length in characters.
const maxPath = 1024

// blockedFileRx is the anchored match on whole pathnames that are JavaScript
// reserved property names (a temporary workaround so files can be used as map
// keys safely).
var blockedFileRx = regexp.MustCompile(`^(prototype|constructor|toString|toLocaleString|valueOf|hasOwnProperty|isPrototypeOf|propertyIsEnumerable|__defineGetter__|__lookupGetter__|__defineSetter__|__lookupSetter__|__proto__)$`)

// isBadChar mirrors BAD_CHAR_RX: '/', '*', control chars (0-0x1F), 0x7F,
// C1 control (0x80-0x9F), and (as surrogate pairs) non-BMP characters.
func isBadChar(r rune) bool {
	if r == '/' || r == '*' {
		return true
	}
	if r <= 0x1F {
		return true
	}
	if r == 0x7F {
		return true
	}
	if r >= 0x80 && r <= 0x9F {
		return true
	}
	if r > 0xFFFF {
		return true // non-BMP (surrogate-pair) char
	}
	return false
}

// cleanPart mirrors cleanPart: replace invalid characters and filename
// patterns ('.', '..', leading/trailing whitespace) with underscores.
func cleanPart(filename string) string {
	var b strings.Builder
	for _, r := range filename {
		if isBadChar(r) {
			b.WriteByte('_')
		} else {
			b.WriteRune(r)
		}
	}
	s := b.String()

	// BAD_FILE_RX: whole '.' -> '_', whole '..' -> '__'.
	if s == "." {
		return "_"
	}
	if s == ".." {
		return "__"
	}

	// leading / trailing whitespace runs -> equal-underline.
	rs := []rune(s)
	i := 0
	for i < len(rs) && unicode.IsSpace(rs[i]) {
		i++
	}
	j := len(rs)
	for j > i && unicode.IsSpace(rs[j-1]) {
		j--
	}
	if i == 0 && j == len(rs) {
		return s
	}
	out := make([]rune, 0, len(rs))
	for k := 0; k < i; k++ {
		out = append(out, '_')
	}
	out = append(out, rs[i:j]...)
	for k := j; k < len(rs); k++ {
		out = append(out, '_')
	}
	return string(out)
}

// posixNormalize mirrors the path-browserify / Node posix path.normalize.
//
// It is a 1:1 reproduction of the oracle behaviour (verified against Node's
// `path.posix.normalize` across the safe_pathname test inputs): '.' and empty
// segments are dropped, '..' pops a following real segment or (when there is no
// such segment and the path is relative) becomes an above-root '..', '..' at an
// absolute root is discarded, and a trailing slash is preserved when the result
// is non-empty.
func posixNormalize(p string) string {
	if p == "" {
		return "."
	}
	isAbs := p[0] == '/'
	trailing := len(p) > 1 && p[len(p)-1] == '/'

	var result []string
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "", ".":
			// skip
		case "..":
			if len(result) > 0 && result[len(result)-1] != ".." {
				result = result[:len(result)-1]
			} else if !isAbs {
				result = append(result, "..")
			}
		default:
			result = append(result, seg)
		}
	}

	var out string
	if len(result) == 0 {
		if isAbs {
			out = "/"
		} else {
			out = "."
		}
	} else {
		out = strings.Join(result, "/")
		if isAbs {
			out = "/" + out
		}
	}
	if trailing && !(isAbs && len(result) == 0) && out != "/" {
		out += "/"
	}
	return out
}

// Clean is the exported clean: the cleaned pathname.
func Clean(pathname string) string {
	cleaned, _ := CleanDebug(pathname)
	return cleaned
}

// CleanDebug mirrors cleanDebug: returns the cleaned pathname and a
// comma-joined reason string naming each step that changed it.
func CleanDebug(pathname string) (string, string) {
	prev := pathname
	reason := ""
	record := func(label string) {
		if pathname == prev {
			return
		}
		if reason != "" {
			reason += ","
		}
		reason += label
		prev = pathname
	}

	pathname = posixNormalize(pathname)
	record("normalize")

	pathname = strings.ReplaceAll(pathname, "\\", "/")
	record("workaround for IE")

	pathname = regexp.MustCompile(`/+`).ReplaceAllString(pathname, "/")
	record("no multiple slashes")

	if strings.HasPrefix(pathname, "/") {
		pathname = "_" + pathname
	}
	record("no leading /")

	if len(pathname) > 1 && strings.HasSuffix(pathname, "/") {
		pathname = pathname[:len(pathname)-1]
	}
	record("no trailing /")

	// no leading spaces (ASCII space only, Node `^ *(.*)$`)
	if t := strings.TrimLeft(pathname, " "); t != pathname {
		pathname = t
	}
	record("no leading spaces")

	// no trailing spaces (Node `^(.*[^ ]) *$`; only when non-space content precedes)
	if strings.HasSuffix(pathname, " ") && !allSpaces(pathname) {
		pathname = strings.TrimRight(pathname, " ")
	}
	record("no trailing spaces")

	if pathname == "" {
		pathname = "_"
	}
	record("empty")

	parts := strings.Split(pathname, "/")
	for i, p := range parts {
		parts[i] = cleanPart(p)
	}
	pathname = strings.Join(parts, "/")
	record("cleanPart")

	if m := blockedFileRx.FindString(pathname); m != "" {
		pathname = "@" + m
	}
	record("BLOCKED_FILE_RX")

	return pathname, reason
}

func allSpaces(p string) bool {
	if p == "" {
		return true
	}
	for _, r := range p {
		if r != ' ' {
			return false
		}
	}
	return true
}

// IsClean mirrors isClean.
func IsClean(pathname string) bool {
	ok, _ := IsCleanDebug(pathname)
	return ok
}

// IsCleanDebug mirrors isCleanDebug: the cleaned pathname is equal to the input
// (and within MAX_PATH / non-empty).
func IsCleanDebug(pathname string) (bool, string) {
	if len([]rune(pathname)) > maxPath {
		return false, "MAX_PATH"
	}
	if pathname == "" {
		return false, "empty"
	}
	cleaned, reason := CleanDebug(pathname)
	if cleaned != pathname {
		return false, reason
	}
	return true, ""
}
