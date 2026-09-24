package otc

// safe_pathname.go mirrors libraries/overleaf-editor-core/lib/safe_pathname.js
// 1:1.
//
// Regular expressions for Overleaf v2, taken from web's
// Features/Project/SafePath.js. All pathnames in a Snapshot must be clean:
// unambiguous, no directory traversal, no leading/trailing space, no '*'.
//
// The JS source works in terms of UTF-16 code units (String.replace,
// .length, .lastIndexOf all index into the UTF-16 array), so the port
// operates on []uint16 throughout. V8 regex gotchas the port encodes:
//   - BAD_CHAR_RX matches per UTF-16 code unit (a supplementary char is two
//     surrogate units and becomes two underscores; a lone surrogate unit
//     matches on its own).
//   - V8 "." excludes exactly {U+000A, U+000D, U+2028, U+2029}; the four
//     anchored guard stages record no reason and make no change when the
//     string contains a line terminator where the engine would have to
//     cover it.
//   - cleanPart's BAD_FILE_RX whitespace is JS \s (superset note: this
//     includes U+FEFF, which Go's unicode.IsSpace does not).
//
// Oracle: safe_pathname_oracle_test.go (80,782 Node-generated rows, GREEN 2026-09-24).
// CLSI-side acceptance replica: services/clsi.go/safepathname_oracle.

// maxPath is the maximum path length, in UTF-16 code units (Node MAX_PATH).
const maxPath = 1024

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

// isBadChar mirrors BAD_CHAR_RX = /[/*\u0000-\u001F\u007F\u0080-\u009F
// \uD800-\uDFFF]/ operating per UTF-16 code unit: each surrogate unit (and
// any supplementary char as the pair of units it encodes) is its own match.
func isBadChar(u uint16) bool {
	return u == '/' || u == '*' ||
		u <= 0x1F ||
		u == 0x7F ||
		(u >= 0x80 && u <= 0x9F) ||
		(u >= 0xD800 && u <= 0xDFFF)
}

// isLineTerminator reports whether u is one of the line terminators (LF, CR,
// LS, PS). V8's "." character and the (non-multiline) "$" anchor exclude
// them, so any scan-based stage using ".+" or "$" fails to match when one of
// them is present where the engine would have to cover it.
func isLineTerminator(u uint16) bool {
	return u == '\n' || u == '\r' || u == 0x2028 || u == 0x2029
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

// isBadFileSpace mirrors BAD_FILE_RX's \s as it can occur after the BAD_CHAR
// pass (BAD_CHAR already replaced 0x00-0x1F, 0x7F, 0x80-0x9F). The superset
// including those units is harmless since they can never reach this check.
// JS \s = {0009-000D, 0020, 00A0, 1680, 2000-200A, 2028, 2029, 202F, 205F,
// 3000, FEFF}; note U+FEFF is included (Go's unicode.IsSpace is NOT this set).
func isBadFileSpace(u uint16) bool {
	switch {
	case u >= 0x09 && u <= 0x0D,
		u == 0x20,
		u == 0xA0,
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

// Clean mirrors safe_pathname.clean(pathname): the cleaned pathname.
func Clean(pathname string) string {
	cleaned, _ := CleanDebug(pathname)
	return cleaned
}

// CleanDebug mirrors safe_pathname.cleanDebug(pathname): returns the cleaned
// pathname and a comma-joined reason string naming each stage that changed it:
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
func CleanDebug(pathname string) (string, string) {
	prev := pathname
	reason := ""
	rec := func(label string) {
		if pathname == prev {
			return
		}
		if reason != "" {
			reason += ","
		}
		reason += label
		prev = pathname
	}

	// path.normalize(pathname) — path-browserify's posix normalize.
	pathname = normalizePosix(pathname)
	rec("normalize")

	// Workaround for IE: replace every '\' with '/'.
	pathname = replaceUnitAll(pathname, '\\', '/')
	rec("workaround for IE")

	// No multiple slashes: pathname.replace(/\/+/g, '/').
	pathname = collapseSlashes(pathname)
	rec("no multiple slashes")

	// No leading /: pathname.replace(/^(\/.*)$/, '_$1'). The "." in (\/.*)$
	// cannot cover a line terminator that sits after the leading slash, so
	// the stage is skipped (no change, no reason) in that case.
	{
		nu := utf16EncodeStr(pathname)
		if len(nu) >= 1 && nu[0] == '/' && !containsLineTerinator(nu[1:]) {
			pathname = "_" + pathname
		}
	}
	rec("no leading /")

	// No trailing /: pathname.replace(/^(.+)\/$/, '$1'). A single "/"" is
	// left unchanged, and V8's "." (".+") cannot cover a line terminator in
	// the prefix before the trailing '/'.
	{
		nu := utf16EncodeStr(pathname)
		if len(nu) > 1 && nu[len(nu)-1] == '/' && !containsLineTerinator(nu[:len(nu)-1]) {
			pathname = utf16DecodeString(nu[:len(nu)-1])
		}
	}
	rec("no trailing /")

	// No leading spaces: pathname.replace(/^ *(.*)$/, '$1') (U+0020 only).
	// An all-space string becomes empty (then "empty" replaces it with '_'),
	// matching the oracle. No line-terminator guard is needed for this stage
	// (the "." of ".*" only has to cover units AFTER the leading run, and
	// V8's leftmost-longest scan of the leading spaces tolerates terminators
	// in the remainder — verified against the oracle).
	{
		nu := utf16EncodeStr(pathname)
		i := 0
		for i < len(nu) && nu[i] == ' ' {
			i++
		}
		if i > 0 && !containsLineTerinator(nu[i:]) {
			pathname = utf16DecodeString(nu[i:])
		}
	}
	rec("no leading spaces")

	// No trailing spaces: pathname.replace(/^(.*[^ ]) *$/, '$1'). The group
	// (.*[^ ]) is greedily the longest prefix ending in a non-space unit, so
	// the stage trims the trailing space run iff one exists and the engine's
	// ".*" can cover the prefix: no line terminator before the last non-
	// space unit (the [^ ] unit itself may be a line terminator — negated
	// classes are not dot-excluded).
	nu := utf16EncodeStr(pathname)
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

	// Empty: if (pathname.length === 0) pathname = '_'.
	if utf16Len(pathname) == 0 {
		pathname = "_"
	}
	rec("empty")

	// split('/').map(cleanPart).join('/').
	pathname = cleanParts(pathname)
	rec("cleanPart")

	// BLOCKED_FILE_RX: pathname.replace(BLOCKED_FILE_RX, '@$1').
	if _, blocked := blockedFiles[pathname]; blocked {
		pathname = "@" + pathname
	}
	rec("BLOCKED_FILE_RX")

	return pathname, reason
}

// IsClean mirrors safe_pathname.isClean: clean leaves pathname unchanged
// and it is short enough.
func IsClean(pathname string) bool {
	ok, _ := IsCleanDebug(pathname)
	return ok
}

// IsCleanDebug mirrors safe_pathname.isCleanDebug: the pathname must not
// exceed MAX_PATH (UTF-16 code units, mirroring JS str.length), be non-empty
// and survive cleanDebug unchanged.
func IsCleanDebug(pathname string) (bool, string) {
	if utf16Len(pathname) > maxPath {
		return false, "MAX_PATH"
	}
	if utf16Len(pathname) == 0 {
		return false, "empty"
	}
	cleaned, reason := CleanDebug(pathname)
	if cleaned != pathname {
		return false, reason
	}
	return true, ""
}

// cleanParts mirrors pathname.split('/').map(cleanPart).join('/').
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

// cleanPartUnits cleans one path part (no '/' in it) with the two passes:
//
//	filename = filename.replace(BAD_CHAR_RX, '_')
//	filename = filename.replace(BAD_FILE_RX, (m) => '...'.repeat(m.length))
//
// with BAD_CHAR_RX = /[/*\u0000-\u001F\u007F\u0080-\u009F\uD800-\uDFFF]/g and
// BAD_FILE_RX = /(^\.$)|(^\.\.$)|(^\s+)|(\s+$)/g. After the BAD_CHAR pass
// (which replaces \t \n \v \f \r and every control range), BAD_FILE's \s can
// only matter for {0020, 00A0, 1680, 2000-200A, 2028, 2029, 202F, 205F, 3000,
// FEFF}; the leading alternatives (^\.$ / ^\.\.$ / ^\s+) match at position 0,
// the trailing (\s+)$ at the end — a single pass covers both sides.
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
	// Pass 2: BAD_FILE_RX.
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

func isAllSpaces(units []uint16) bool {
	for _, u := range units {
		if u != ' ' {
			return false
		}
	}
	return true
}

// --- UTF-16 code-unit helpers ----------------------------------------------
//
// Node operates on UTF-16 code units. Go strings hold UTF-8; the helpers
// below convert explicitly. Lone surrogate units (U+D800-U+DFFF without a
// pair) round-trip through the 0xFFFD replacement char and are preserved
// unit-for-unit, which matters because BAD_CHAR_RX matches them and CLSI
// snapshots carry them from JSON fuzz input (encoding/json emits lone
// surrogates as \uXXXX escapes).

// utf16Len returns the length of s in UTF-16 code units (JS str.length).
func utf16Len(s string) int {
	if s == "" {
		return 0
	}
	return len(utf16EncodeStr(s))
}

// utf16EncodeStr encodes a Go string to UTF-16 code units (manual: Go's
// utf16.Encode maps invalid runes to U+FFFD and does NOT accept lone
// surrogates).
func utf16EncodeStr(s string) []uint16 {
	var out []uint16
	for _, r := range s {
		if r > 0xFFFF {
			s := uint32(r - 0x10000)
			out = append(out, uint16(s>>10+0xD800), uint16(s&0x3FF+0xDC00))
		} else {
			out = append(out, uint16(r))
		}
	}
	return out
}

// utf16DecodeString decodes UTF-16 code units to a Go string (manual: Go's
// utf16.Decode maps invalid units (lone surrogates, U+FFFE/U+FFFF) to U+FFFD
// and would destroy exactly the units this module must preserve).
func utf16DecodeString(units []uint16) string {
	var b []rune
	for i := 0; i < len(units); i++ {
		u := units[i]
		if u >= 0xD800 && u <= 0xDBFF && i+1 < len(units) {
			l := units[i+1]
			if l >= 0xDC00 && l <= 0xDFFF {
				// Correct surrogate pair -> supplementary code point.
				// U+10000 + (high-0xD800)*0x400 + (low-0xDC00).
				code := rune(0x10000 + (uint32(u-0xD800) << 10) + uint32(l-0xDC00))
				b = append(b, code)
				i++ // skip the low surrogate
				continue
			}
		}
		b = append(b, rune(u))
	}
	return string(b)
}

// replaceUnitAll mirrors str.replace(/\\g/...) for the single-char case
// (used for the IE workaround: every '\' -> '/').
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

// collapseSlashes mirrors pathname.replace(/\/+/g, '/').
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

// normalizePosix mirrors path-browserify's posix.normalize (a vendored copy
// of Node's path.posix.normalize, verified line-for-line), operating on
// UTF-16 code units.
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
