package otc

import "testing"

// Oracle: libraries/overleaf-editor-core/test/unit/safe_pathname.test.js
//
// expectClean mirrors the Node helper: it checks the cleaned output, the exact
// reason string, and then idempotency (cleaning the cleaned input is a no-op)
// and isClean of the cleaned input.
func expectClean(t *testing.T, input, output, reason string) {
	t.Helper()
	cleaned, gotReason := CleanDebug(input)
	if cleaned != output {
		t.Fatalf("cleanDebug(%q) = %q, want %q", input, cleaned, output)
	}
	if gotReason != reason {
		t.Fatalf("cleanDebug(%q) reason = %q, want %q", input, gotReason, reason)
	}
	if got := Clean(cleaned); got != cleaned {
		t.Fatalf("clean is not idempotent: clean(%q) = %q (input %q)", cleaned, got, input)
	}
	if !IsClean(cleaned) {
		t.Fatalf("IsClean(%q) = false after cleaning (input %q)", cleaned, input)
	}
}

func TestSafePathnameCleansPathnames(t *testing.T) {
	// preserve valid pathnames
	expectClean(t, "llama.jpg", "llama.jpg", "")
	expectClean(t, "DSC4056.JPG", "DSC4056.JPG", "")

	// detects unclean pathnames
	if IsClean("rm -rf /") {
		t.Error("expected 'rm -rf /' to be unclean")
	}

	// replace invalid characters with underscores
	expectClean(t, "test-s*\u0001\u0002m\u0007st\u0008.jpg", "test-s___m_st_.jpg", "cleanPart")

	// keep slashes, normalize paths, replace ..
	expectClean(t, "./foo", "foo", "normalize")
	expectClean(t, "../foo", "__/foo", "cleanPart")
	expectClean(t, "foo/./bar", "foo/bar", "normalize")
	expectClean(t, "foo/../bar", "bar", "normalize")
	expectClean(t, "../../tricky/foo.bar", "__/__/tricky/foo.bar", "cleanPart")
	expectClean(t, "foo/../../tricky/foo.bar", "__/tricky/foo.bar", "normalize,cleanPart")
	expectClean(t, "foo/bar/../../tricky/foo.bar", "tricky/foo.bar", "normalize")
	expectClean(t, "foo/bar/baz/../../tricky/foo.bar", "foo/tricky/foo.bar", "normalize")

	// remove illegal chars even when there is no extension
	expectClean(t, "**foo", "__foo", "cleanPart")

	// remove windows file paths
	expectClean(t, `c:\temp\foo.txt`, "c:/temp/foo.txt", "workaround for IE")

	// no leading slash
	expectClean(t, "/foo", "_/foo", "no leading /")
	expectClean(t, "//foo", "_/foo", "normalize,no leading /")

	// no trailing slash
	expectClean(t, "/", "_", "no leading /,no trailing /")
	expectClean(t, "foo/", "foo", "no trailing /")
	expectClean(t, "foo.tex/", "foo.tex", "no trailing /")

	// multiple leading/trailing slashes
	expectClean(t, "//", "_", "normalize,no leading /,no trailing /")
	expectClean(t, "///", "_", "normalize,no leading /,no trailing /")
	expectClean(t, "foo//", "foo", "normalize,no trailing /")

	// file and folder names that consist of . and .. are not OK
	expectClean(t, ".", "_", "cleanPart")
	expectClean(t, "..", "__", "cleanPart")
	// but more dots are allowed
	expectClean(t, "...", "...", "")
	expectClean(t, "....", "....", "")
	expectClean(t, "foo/...", "foo/...", "")
	expectClean(t, "foo/....", "foo/....", "")
	expectClean(t, "foo/.../bar", "foo/.../bar", "")
	expectClean(t, "foo/..../bar", "foo/..../bar", "")

	// leading dots are OK
	expectClean(t, "._", "._", "")
	expectClean(t, ".gitignore", ".gitignore", "")

	// trailing dots are not OK on Windows but we allow them
	expectClean(t, "_.", "_.", "")
	expectClean(t, "foo/_.", "foo/_.", "")
	expectClean(t, "foo/_./bar", "foo/_./bar", "")
	expectClean(t, "foo/_../bar", "foo/_../bar", "")

	// spaces are allowed
	expectClean(t, "a b.png", "a b.png", "")

	// leading and trailing spaces are not OK
	expectClean(t, " foo", "foo", "no leading spaces")
	expectClean(t, "  foo", "foo", "no leading spaces")
	expectClean(t, "foo ", "foo", "no trailing spaces")
	expectClean(t, "foo  ", "foo", "no trailing spaces")

	// reserved Windows names are allowed (legacy)
	expectClean(t, "AUX", "AUX", "")
	expectClean(t, "foo/AUX", "foo/AUX", "")
	expectClean(t, "AUX/foo", "AUX/foo", "")

	// multiple dots are OK
	expectClean(t, "a.b.png", "a.b.png", "")
	expectClean(t, "a.code.tex", "a.code.tex", "")

	// multiple slashes
	expectClean(t, "foo//bar.png", "foo/bar.png", "normalize")
	expectClean(t, "foo///bar.png", "foo/bar.png", "normalize")

	// javascript property-name handling
	expectClean(t, "foo/prototype", "foo/prototype", "")
	expectClean(t, "prototype/test.txt", "prototype/test.txt", "")
	expectClean(t, "prototype", "@prototype", "BLOCKED_FILE_RX")
	expectClean(t, "hasOwnProperty", "@hasOwnProperty", "BLOCKED_FILE_RX")
	expectClean(t, "**proto**", "@__proto__", "cleanPart,BLOCKED_FILE_RX")
}

func TestSafePathnameMaxLength(t *testing.T) {
	// a clean but over-long pathname is not clean (MAX_PATH)
	name := ""
	for i := 0; len(name) < 1100; i++ {
		name += "x"
	}
	// clean is a no-op on a run of 'x's, so cleanDebug would be identity; only
	// the length gate makes it unclean.
	if ok, reason := IsCleanDebug(name); ok || reason != "MAX_PATH" {
		t.Fatalf("expected an over-1024 clean pathname to be unclean; got ok=%v reason=%q", ok, reason)
	}
}

func TestSafePathnameEmpty(t *testing.T) {
	if ok, reason := IsCleanDebug(""); ok || reason != "empty" {
		t.Fatalf("expected empty pathname to be unclean; got ok=%v reason=%q", ok, reason)
	}
}

// Pin the per-UTF-16-code-unit branches of cleanPart (cleanPartUnits).
// BAD_CHAR_RX = /[/*\u0000-\u001F\u007F\u0080-\u009F\uD800-\uDFFF]/g
// matches per unit, so a supplementary char (a surrogate pair) becomes two
// underscores, and a lone surrogate unit is its own match. BAD_FILE_RX's \s
// (JS \s, which includes U+FEFF that Go's unicode.IsSpace does NOT) is
// replaced per run at each part's edge. Inputs are []uint16 because Go
// strings cannot hold lone surrogate units (encoding/json replaces unpaired
// ones with U+FFFD) while per-unit matching is the crux of the port.
func TestCleanPartInvalidChars(t *testing.T) {
	cases := []struct {
		in   []uint16
		want string
	}{
		{[]uint16{'a', '/', 'b'}, "a_b"},             // slash
		{[]uint16{'a', '*', 'b'}, "a_b"},             // star
		{[]uint16{'a', 0x01, 'b'}, "a_b"},            // C0 control
		{[]uint16{'a', 0x7F, 'b'}, "a_b"},            // DEL
		{[]uint16{'a', 0x80, 'b'}, "a_b"},            // C1 control
		{[]uint16{'a', 0x9F, 'b'}, "a_b"},            // C1 control
		{[]uint16{'a', 0xD83D, 0xDE00, 'b'}, "a__b"}, // U+1F600 = surrogate pair = two units
		{[]uint16{'a', 0xD800, 'b'}, "a_b"},          // lone high surrogate unit
		{[]uint16{'a', 0xDC00, 'b'}, "a_b"},          // lone low surrogate unit
		{[]uint16{'a', 'b', 'c'}, "abc"},             // untouched
		{[]uint16{'a', ' ', 'b'}, "a b"},             // space survives BAD_CHAR
	}
	for _, c := range cases {
		if got := utf16DecodeString(cleanPartUnits(c.in)); got != c.want {
			t.Errorf("cleanPartUnits(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCleanPartWhitespace(t *testing.T) {
	cases := []struct {
		in   []uint16
		want string
	}{
		{[]uint16{' ', 'a', ' '}, "_a_"},
		{[]uint16{' ', ' ', 'f', 'o', 'o'}, "__foo"},
		{[]uint16{'f', 'o', 'o', ' ', ' '}, "foo__"},
		{[]uint16{0xFEFF}, "_"}, // FEFF is JS \s
		{[]uint16{0xFEFF, 'a', 0xFEFF}, "_a_"},
		{[]uint16{0x2000, 'a', 0x202F}, "_a_"},
		{[]uint16{0x3000, 'a', 0xA0}, "_a_"},
		{[]uint16{'.', '.'}, "__"},             // full ".." -> "__"
		{[]uint16{'.'}, "_"},                   // full "." -> "_"
		{[]uint16{'.', '.', '.', '.'}, "...."}, // "...." untouched
	}
	for _, c := range cases {
		if got := utf16DecodeString(cleanPartUnits(c.in)); got != c.want {
			t.Errorf("cleanPartUnits(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
