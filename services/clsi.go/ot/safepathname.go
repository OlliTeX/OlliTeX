package ot

// Pathname sanitation, a faithful port of safe_pathname.js, which itself
// relies on path-browserify's posix normalize (a vendored copy of Node's
// path.posix.normalize).
//
// The JS source works in terms of UTF-16 code units (String.replace,
// .length, .lastIndexOf all index into the UTF-16 array), so the port
// operates on []uint16 throughout.

import "unicode/utf16"

// MAX_PATH is the maximum pathname length, in UTF-16 code units
// (mirrors safe_pathname.MAX_PATH = 1024).
const MAX_PATH = 1024

// blockedFiles mirrors BLOCKED_FILE_RX, an anchored whole-pathname match:
//
//	/^(prototype|constructor|toString|toLocaleString|valueOf|hasOwnProperty|
//	    isPrototypeOf|propertyIsEnumerable|__defineGetter__|__lookupGetter__|
//	    __defineSetter__|__lookupSetter__|__proto__)$/
var blockedFiles = map[string]struct{}{
	"prototype": {}, "constructor": {}, "toString": {}, "toLocaleString": {},
	"valueOf": {}, "hasOwnProperty": {}, "isPrototypeOf": {},
	"propertyIsEnumerable": {}, "__defineGetter__": {}, "__lookupGetter__": {},
	"__defineSetter__": {}, "__lookupSetter__": {}, "__proto__": {},
}

// Clean mirrors safe_pathname.clean.
func Clean(pathname string) string {
	return CleanDebug(pathname).Pathname
}

// CleanResult pairs the cleaned pathname with the reason string (a
// comma-separated list of the stages that changed the pathname).
type CleanResult struct {
	Pathname string
	Reason   string
}

// CleanDebug mirrors safe_pathname.cleanDebug. Each stage records its label
// in the reason string only if it changed the pathname:
//
//	pathname = path.normalize(pathname)                      "normalize"
//	pathname = pathname.replace(/\\/g, '/')                 "workaround for IE"
//	pathname = pathname.replace(/\/+/g, '/')               "no multiple slashes"
//	pathname = pathname.replace(/^(\/.*)$/, '_$1')         "no leading /"
//	pathname = pathname.replace(/^(.+)\/$/, '$1')          "no trailing /"
//	pathname = pathname.replace(/^ *(.*)$/, '$1')          "no leading spaces"
//	pathname = pathname.replace(/^(.*[^ ]) *$/, '$1')      "no trailing spaces"
//	if (pathname.length === 0) pathname = '_'               "empty"
//	pathname = pathname.split('/').map(cleanPart).join('/') "cleanPart"
//	pathname = pathname.replace(BLOCKED_FILE_RX, '@$1')    "BLOCKED_FILE_RX"
func CleanDebug(input string) CleanResult {
	pathname := input
	var reasons []string
	prev := input
	rec := func(label string) {
		if pathname == prev {
			return
		}
		reasons = append(reasons, label)
		prev = pathname
	}

	// path.normalize(pathname)
	pathname = normalizePosix(pathname)
	rec("normalize")

	// Workaround for IE: replace every '\' with '/'
	pathname = replaceUnitAll(pathname, '\\', '/')
	rec("workaround for IE")

	// No multiple slashes: pathname.replace(/\/+/g, '/')
	pathname = collapseSlashes(pathname)
	rec("no multiple slashes")

	// No leading /: pathname.replace(/^(\/.*)$/, '_$1'). The only
	// constraint is that (\\.*)$ requires at least one further unit
	// The V8 pattern /^(\/.*)$/ has ".*" that cannot cover line
	// terminators, so the substitution requires units[1:] to contain no
	// line terminator. Result is "_" + pathname.
	nu := utf16EncodeStr(pathname)
	if len(nu) >= 1 && nu[0] == '/' && !containsLineTerinator(nu[1:]) {
		pathname = "_" + pathname
	}
	rec("no leading /")

	// No trailing /: pathname.replace(/^(.+)\/$/, '$1'). A single "/" is
	// left unchanged, and V's ".+" cannot cover line terminators in the
	// prefix before the trailing '/'.
	nu = utf16EncodeStr(pathname)
	if len(nu) > 1 && nu[len(nu)-1] == '/' && !containsLineTerinator(nu[:len(nu)-1]) {
		pathname = utf16DecodeString(nu[:len(nu)-1])
	}
	rec("no trailing /")

	// No leading spaces: pathname.replace(/^ *(.*)$/, '$1'), U+0020 only.
	// An all-space string becomes empty, then "empty" replaces it with
	// '_', matching the oracle. No line-terminator guard is needed for
	// this stage.
	nu = utf16EncodeStr(pathname)
	i := 0
	for i < len(nu) && nu[i] == ' ' {
		i++
	}
	if i > 0 && !containsLineTerinator(nu[i:]) {
		pathname = utf16DecodeString(nu[i:])
	}
	rec("no leading spaces")

	// No trailing spaces: pathname.replace(/^(.*[^ ]) *$/, '$1'); no
	// match (hence no replacement) when the string is empty or all spaces.
	nu = utf16EncodeStr(pathname)
	if !isAllSpaces(nu) {
		j := len(nu)
		for j > 0 && nu[j-1] == ' ' {
			j--
		}
		if !containsLineTerinator(nu[:j-1]) {
			pathname = utf16DecodeString(nu[:j])
		}
	}
	rec("no trailing spaces")

	// Empty
	if utf16Len(pathname) == 0 {
		pathname = "_"
	}
	rec("empty")

	// split('/').map(cleanPart).join('/')
	if cleaned := cleanParts(pathname); cleaned != pathname {
		pathname = cleaned
	}
	rec("cleanPart")

	// BLOCKED_FILE_RX: pathname.replace(BLOCKED_FILE_RX, '@$1')
	if _, blocked := blockedFiles[pathname]; blocked {
		pathname = "@" + pathname
	}
	rec("BLOCKED_FILE_RX")

	return CleanResult{Pathname: pathname, Reason: joinReasons(reasons)}
}

// IsClean mirrors safe_pathname.isClean:
// cleanDebug(pathname) leaves pathname unchanged and it is short enough.
func IsClean(pathname string) (bool, string) {
	if utf16Len(pathname) > MAX_PATH {
		return false, "MAX_PATH"
	}
	if utf16Len(pathname) == 0 {
		return false, "empty"
	}
	cr := CleanDebug(pathname)
	if cr.Pathname != pathname {
		return false, cr.Reason
	}
	return true, ""
}

// cleanParts mirrors pathname.split('/').map(cleanPart).join('/'), where
// cleanPart mirrors the lib's
//
//	filename = filename.replace(BAD_CHAR_RX, '_')
//	filename = filename.replace(BAD_FILE_RX, (m) => '_'.repeat(m.length))
//
// with BAD_CHAR_RX = /[/*\u0000-\u001F\u007F\u0080-\u009F\uD800-\uDFFF]/g and
// BAD_FILE_RX = /(^\.$)|(^\.\.$)|(^\s+)|(\s+$)/g. After the BAD_CHAR pass,
// BAD_FILE's \s can match space U+0020 and the whitespace characters outside
// BAD_CHAR's ranges: U+00A0, U+1680, U+2000-U+200A, U+2028, U+2029, U+202F,
// U+205F, U+3000 and U+FEFF.
func cleanParts(pathname string) string {
	nu := utf16EncodeStr(pathname)
	var out []uint16
	start := 0
	for i, u := range nu {
		if u == '/' {
			if i > start {
				out = append(out, cleanPartUnits(nu[start:i])...)
			}
			out = append(out, '/')
			start = i + 1
		}
	}
	if len(nu) > start {
		out = append(out, cleanPartUnits(nu[start:])...)
	} else {
		out = append(out, cleanPartUnits(nil)...)
	}
	return utf16DecodeString(out)
}

// cleanPartUnits cleans one path part (no '/' in it) with the two
// BAD_CHAR/BAD_FILE passes.
func cleanPartUnits(part []uint16) []uint16 {
	// Pass 1: BAD_CHAR_RX, one replacement per matching unit.
	pass1 := make([]uint16, len(part))
	for i, u := range part {
		if isBadChar(u) {
			pass1[i] = '_'
		} else {
			pass1[i] = u
		}
	}
	// Pass 2: BAD_FILE_RX. The leading alternatives (^\.$ / ^\.\.$ / ^\s+)
	// match at position 0 only, and the trailing (\s+)$ matches the terminal
	// run; a single pass covers both ends.
	out := make([]uint16, len(pass1))
	copy(out, pass1)

	switch {
	case len(pass1) == 1 && pass1[0] == '.':
		out[0] = '_'
		return out
	case len(pass1) == 2 && pass1[0] == '.' && pass1[1] == '.':
		out[0] = '_'
		out[1] = '_'
		return out
	}
	k := 0
	for k < len(pass1) && isBadFileSpace(pass1[k]) {
		k++
	}
	for i := 0; i < k; i++ {
		out[i] = '_'
	}
	j := len(pass1)
	for j > 0 && isBadFileSpace(pass1[j-1]) {
		j--
	}
	for i := len(out) - 1; i >= j; i-- {
		out[i] = '_'
	}
	return out
}

// isLineTerminator reports whether u is one of the line terminators
// (LF, CR, LS, PS). V's "." character and the (non-multiline) "$"
// anchor do not match or cover them, so any stage using ".+" or "$"
// fails to match when one of them is present where the engine would
// have to cover it during the scan.
func isLineTerminator(u uint16) bool {
	return u == '\n' || u == '\r' || u == '\u2028' || u == '\u2029'
}

// containsLineTerinator reports whether any unit of units is a line
// terminator.
func containsLineTerinator(units []uint16) bool {
	for _, u := range units {
		if isLineTerminator(u) {
			return true
		}
	}
	return false
}

// isBadChar mirrors BAD_CHAR_RX: /[/*\u0000-\u001F\u007F\u0080-\u009F
// \uD800-\uDFFF]/
func isBadChar(u uint16) bool {
	return u == '/' || u == '*' ||
		u <= 0x1F ||
		(u >= 0x7F && u <= 0x9F) ||
		(u >= 0xD800 && u <= 0xDFFF)
}

// isBadFileSpace mirrors BAD_FILE_RX's \s as it can occur after the
// BAD_CHAR pass (the BAD_CHAR set already removed 0x09-0x0D, 0x7F-0x9F).
// The superset with them is harmless since those units can never reach this
// check.
func isBadFileSpace(u uint16) bool {
	switch {
	case u == 0x09 || u == 0x0A || u == 0x0B || u == 0x0C || u == 0x0D,
		u == 0x20,
		u == 0x85 || u == 0xA0,
		u == 0x1680,
		u >= 0x2000 && u <= 0x200A,
		u == 0x2028 || u == 0x2029 || u == 0x202F || u == 0x205F,
		u == 0x3000,
		u == 0xFEFF:
		return true
	default:
		return false
	}
}

func isAllSpaces(units []uint16) bool {
	for _, u := range units {
		if u != ' ' {
			return false
		}
	}
	return true
}

func replaceUnitAll(pathname string, from, to uint16) string {
	nu := utf16EncodeStr(pathname)
	changed := false
	out := make([]uint16, len(nu))
	for i, u := range nu {
		if u == from {
			out[i] = to
			changed = true
		} else {
			out[i] = u
		}
	}
	if !changed {
		return pathname
	}
	return utf16DecodeString(out)
}

func collapseSlashes(pathname string) string {
	nu := utf16EncodeStr(pathname)
	changed := false
	out := make([]uint16, 0, len(nu))
	inRun := false
	for _, u := range nu {
		if u == '/' {
			if !inRun {
				out = append(out, '/')
			} else {
				changed = true
			}
			inRun = true
		} else {
			inRun = false
			out = append(out, u)
		}
	}
	if !changed {
		return pathname
	}
	return utf16DecodeString(out)
}

func joinReasons(reasons []string) string {
	out := ""
	for i, r := range reasons {
		if i > 0 {
			out += ","
		}
		out += r
	}
	return out
}

// normalizePosix mirrors path-browserify's posix.normalize (Node v8's
// path.posix.normalize), operating on UTF-16 code units.
func normalizePosix(pathname string) string {
	nu := utf16EncodeStr(pathname)
	if len(nu) == 0 {
		return "."
	}
	isAbsolute := nu[0] == '/'
	trailingSeparator := nu[len(nu)-1] == '/'

	res := normalizeStringPosix(nu, !isAbsolute)
	if len(res) == 0 && !isAbsolute {
		res = []uint16{'.'}
	}
	if len(res) > 0 && trailingSeparator {
		res = append(res, '/')
	}
	if isAbsolute {
		res = append([]uint16{'/'}, res...)
	}
	return utf16DecodeString(res)
}

// normalizeStringPosix mirrors the vendored path-browserify
// normalizeStringPosix, byte-for-byte equivalent on UTF-16 code units.
//
// NB: `code` is declared OUTSIDE the loop, matching the JS `var code` — the
// sentinel iteration (i == len(path)) observes the previous iteration's
// value and breaks when the input's last unit is a slash.
func normalizeStringPosix(path []uint16, allowAboveRoot bool) []uint16 {
	var res []uint16
	lastSegmentLength := 0
	lastSlash := -1
	dots := 0
	var code uint16
	for i := 0; i <= len(path); i++ {
		if i < len(path) {
			code = path[i]
		} else {
			if code == '/' {
				break
			}
			code = '/'
		}
		if code == '/' {
			if lastSlash == i-1 || dots == 1 {
				// NOOP
			} else if lastSlash != i-1 && dots == 2 {
				if len(res) < 2 || lastSegmentLength != 2 ||
					len(res) >= 2 && res[len(res)-1] != '.' ||
					len(res) >= 2 && res[len(res)-2] != '.' {
					if len(res) > 2 {
						idx := lastIndexUint16(res, '/')
						if idx != len(res)-1 {
							if idx == -1 {
								res = nil
								lastSegmentLength = 0
							} else {
								res = res[:idx]
								lastSegmentLength = len(res) - 1 - lastIndexUint16(res, '/')
							}
							lastSlash = i
							dots = 0
							continue
						}
					} else if len(res) == 2 || len(res) == 1 {
						res = nil
						lastSegmentLength = 0
						lastSlash = i
						dots = 0
						continue
					}
				}
				if allowAboveRoot {
					if len(res) > 0 {
						res = append(res, '/', '.', '.')
					} else {
						res = []uint16{'.', '.'}
					}
					lastSegmentLength = 2
				}
			} else {
				if len(res) > 0 {
					res = append(res, '/')
					res = append(res, path[lastSlash+1:i]...)
				} else {
					res = append([]uint16{}, path[lastSlash+1:i]...)
				}
				lastSegmentLength = i - lastSlash - 1
			}
			lastSlash = i
			dots = 0
		} else if code == '.' && dots != -1 {
			dots++
		} else {
			dots = -1
		}
	}
	return res
}

func lastIndexUint16(u []uint16, c uint16) int {
	for i := len(u) - 1; i >= 0; i-- {
		if u[i] == c {
			return i
		}
	}
	return -1
}

// utf16Encode is kept as a re-export for readers expecting the helper.
func utf16Encode(s string) []uint16 {
	return utf16.Encode([]rune(s))
}
