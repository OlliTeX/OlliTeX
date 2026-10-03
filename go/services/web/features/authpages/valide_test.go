package authpages

// Oracle-parity tests for validEmail — the expected values below are the
// REAL Node oracle results (regex extracted verbatim from
// junk/services-web/app/src/Features/Helpers/EmailHelper.mjs line 9 and
// executed with node 2026-10-03 during the deck D39 live-verify), not
// hand-derived assumptions.

import (
	"strings"
	"testing"
)

func TestValidEmailOracleParity(t *testing.T) {
	cases := []struct {
		email string
		want  bool
	}{
		// deck D39 live-verify (the regression this guard exists for)
		{"testjoe@rotermund.at", true},
		// Node oracle ground truth (measured 2026-10-03)
		{"test@example.com", true},
		{"testexample.com", false}, // no @
		{"u@x.com", true},
		{`bad"quote@example.com`, false},
		{"user@exa mple.com", false},
		{"a@b.c", false},   // TLD must be >= 2 letters
		{"a@-b.com", true}, // oracle label class allows leading hyphen
		{"Group name:test1@example.com,test2@example.com;", false},
		{"x@y.test", true},
		{"a@sub.example.org", true},
		{"bo@cs.fau.de", true},
		{"user@[10.0.0.1]", true}, // IP-literal domain (oracle allows)
		{"A..b@example.com", false},
	}
	for _, c := range cases {
		if got := validEmail(c.email); got != c.want {
			t.Errorf("validEmail(%q) = %v, want %v", c.email, got, c.want)
		}
	}
	if validEmail("") {
		t.Error("validEmail(\"\") = true, want false")
	}
	// length guard (oracle: >254 chars -> null)
	long := "x@" + strings.Repeat("a", 260) + ".com"
	if validEmail(long) {
		t.Error("validEmail(>254 chars) = true, want false")
	}
}
