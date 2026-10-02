package compile

// c08 — synctex proxy (web->clsi synctex dispatch). Unit tests for the new
// PURE surface: route patterns must match the frontend's exact URLs, and the
// path/query validators must mirror the Node oracle guards.
//
// The HTTP plumbing (dispatch typst->clsiTypstBase :3014 / tex->clsiBase
// :3013, body passthrough, 404 mapping) is a 1-line conditional mirroring the
// proven compile/wordcount dispatch and is exercised by the owner-gated live
// E2E (c11). The crown-jewel service side (clsi_typst -> output.sourcemap.json)
// is separately proven (docs/clsi-typst-integration.md, c11).

import (
	"regexp"
	"testing"
)

func TestSyncRoutePatternsMatchFrontendURLs(t *testing.T) {
	// the frontend (use-synctex.ts) calls EXACTLY these:
	//   /project/<pid>/sync/code?file=...&line=...&column=...&buildId=...
	//   /project/<pid>/sync/pdf?page=...&h=...&v=...&buildId=...
	// The route patterns are path-only (query is free); verify the path form.
	cases := []struct {
		re   *regexp.Regexp
		path string
		ok   bool
	}{
		{syncCodePat, "/project/6abd00e37a29eb9a30056209/sync/code", true},
		{syncCodePat, "/Project/6abd00e37a29eb9a30056209/sync/code", true}, // express is case-insensitive
		{syncCodePat, "/project/6abd00e37a29eb9a30056209/sync/pdf", false},
		{syncCodePat, "/project/abc/sync/code/extra", false},
		{syncPdfPat, "/project/6abd00e37a29eb9a30056209/sync/pdf", true},
		{syncPdfPat, "/project/6abd00e37a29eb9a30056209/sync/code", false},
		{syncPdfPat, "/project//sync/pdf", false}, // empty id
	}
	for i, c := range cases {
		// anchored: use FindStringIndex + full-match check (pattern is ^...$ anchored).
		m := c.re.FindStringIndex(c.path)
		got := m != nil && m[0] == 0 && m[1] == len(c.path)
		if got != c.ok {
			t.Fatalf("case %d: pattern %q vs %q => match=%v want %v",
				i, c.re, c.path, got, c.ok)
		}
	}
}

func TestValidPathParam(t *testing.T) {
	good := []string{
		"main.typ",
		"main.tex",
		"folder/main.typ",
		"folder/./main.typ", // the Node oracle explicitly ALLOWS /./ (collapses)
		"a/b/c.tex",
	}
	for _, p := range good {
		if !validPathParam(p) {
			t.Fatalf("validPathParam(%q) = false, want true", p)
		}
	}
	bad := []string{
		"",            // required
		"/absolute",   // rooted (Node Path.resolve check)
		"a/../b",      // directory escape
		"../escape",   // escape
		"a/b/../../c", // escape
	}
	for _, p := range bad {
		if validPathParam(p) {
			t.Fatalf("validPathParam(%q) = true, want false", p)
		}
	}
}

func TestSyncQueryValidators(t *testing.T) {
	// digits (page/line/column)
	if !reDigits.MatchString("1") || !reDigits.MatchString("123") || reDigits.MatchString("1.5") ||
		reDigits.MatchString("") || reDigits.MatchString("-1") {
		t.Fatalf("reDigits validator wrong")
	}
	// sign+number (h/v coordinates)
	for _, s := range []string{"10", "10.5", "-10", "-0.5", "0"} {
		if !reNumSign.MatchString(s) {
			t.Fatalf("reNumSign should accept %q", s)
		}
	}
	for _, s := range []string{"", "abc", "1.2.3", "1e3"} {
		if reNumSign.MatchString(s) {
			t.Fatalf("reNumSign should reject %q", s)
		}
	}
}
