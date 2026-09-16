package compile

import (
	"strings"
	"testing"
)

// Node: name.replace(/[^\p{L}\p{Nd}]/gu, '_').
func TestSafeProjectName(t *testing.T) {
	cases := map[string]string{
		"P52b Oracle":  "P52b_Oracle",
		"hello world":  "hello_world",
		"a/b\\c":       "a_b_c",
		"ünïcødé 项目 2": "ünïcødé_项目_2", // Unicode letters (L) kept incl. CJK; spaces → _
		"café & naïve": "café___naïve", // non-letter symbols/spaces → _
		"":             "",
		"123":          "123",
		"_.-_":         "____",
		"\t\ttabs":     "__tabs",
	}
	for in, want := range cases {
		if got := safeProjectName(in); got != want {
			t.Errorf("safeProjectName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildIDRegexes(t *testing.T) {
	if !buildIDRe.MatchString("1a0a7b4d47a-32ed7a555de306bd") {
		t.Error("valid buildId rejected")
	}
	if buildIDRe.MatchString("deadbeefdeadbeefdeadbeef") {
		t.Error("dashed-required buildId accepted")
	}
	if editorBuildIDRe.MatchString("1a0a7b4d47a-32ed7a555de306bd") {
		t.Error("editorBuildId accepted a plain buildId")
	}
	uuid := strings.Repeat("0", 8) + "-" + strings.Repeat("0", 4) + "-" + strings.Repeat("0", 4) + "-" + strings.Repeat("0", 4) + "-" + strings.Repeat("0", 12)
	if !editorBuildIDRe.MatchString(uuid + "-1a0a7b4d47a-32ed7a555de306bd") {
		t.Error("valid editorBuildId rejected")
	}
	if clsiServerIDRe.MatchString("BAD-ID") {
		t.Error("clsiServerId with uppercase accepted")
	}
}

func TestClsiCacheAllowed(t *testing.T) {
	for _, ok := range []string{"output.blg", "output.log", "output.pdf", "output.synctex.gz", "output.overleaf.json", "output.tar.gz", "foo.blg"} {
		if !clsiCacheAllowed(ok) {
			t.Errorf("%s should be allowed", ok)
		}
	}
	for _, bad := range []string{"evil.txt", "output.exe", "..", "output.PDF"} {
		if clsiCacheAllowed(bad) {
			t.Errorf("%s should be rejected", bad)
		}
	}
}

func TestIsUUID(t *testing.T) {
	if !isUUID("123e4567-e89b-12d3-a456-426614174000") {
		t.Error("valid uuid rejected")
	}
	for _, bad := range []string{"nope", "123e4567e89b12d3a456426614174000", "123e4567-e89b-12d3-a456-42661417400g", ""} {
		if isUUID(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
