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

// Pin the BAD_CHAR_RX branches: '/', '*', C0 controls, DEL (0x7F), C1 controls
// (0x80-0x9F), and non-BMP (surrogate-pair) characters all become '_'.
func TestCleanPartInvalidChars(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"a/b", "a_b"},          // slash
		{"a*b", "a_b"},          // star
		{"a\x01b", "a_b"},       // C0 control
		{"a\x7Fb", "a_b"},       // DEL
		{"a\u0080b", "a_b"},     // C1 control (U+0080)
		{"a\u009Fb", "a_b"},     // C1 control (U+009F)
		{"a\U0001F600b", "a_b"}, // non-BMP char
		{"abc", "abc"},          // untouched
		{"a b", "a b"},          // space is allowed (BAD_CHAR leaves it)
	}
	for _, c := range cases {
		if got := cleanPart(c.in); got != c.want {
			t.Errorf("cleanPart(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCleanPartWhitespace(t *testing.T) {
	if got := cleanPart(" a "); got != "_a_" {
		t.Fatalf("cleanPart(' a ') = %q, want '_a_'", got)
	}
	if got := cleanPart("  foo"); got != "__foo" {
		t.Fatalf("cleanPart('  foo') = %q, want '__foo'", got)
	}
	if got := cleanPart("foo  "); got != "foo__" {
		t.Fatalf("cleanPart('foo  ') = %q, want 'foo__'", got)
	}
}
