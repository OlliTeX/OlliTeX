package passwordreset

import (
	"strings"
	"testing"
)

// validatePassword parity probes (CE order + texts).
func TestValidatePasswordParity(t *testing.T) {
	cases := []struct {
		pw    string
		email string
		code  int
		key   string // expected response key
		text  string
	}{
		{"abc", "e2e-user@e2e.test", 1, "password-too-short", "Password too short, minimum 8."},
		{strings.Repeat("a", 73), "x@y.test", 2, "password-too-long", "Password too long, maximum 72."},
		{"abcdefg ", "x@y.test", 3, "password-invalid-character", "Password contains an invalid character."},
		{"passwörx", "x@y.test", 3, "password-invalid-character", "Password contains an invalid character."},
		{"abcde12345", "abcde@x.test", 4, "password-contains-email", "Password cannot contain parts of email address."},
		{"abcdefg1", "x@y.test", 0, "", ""},
		{"£abcdef1", "x@y.test", 0, "", ""},
		{"Aa1@abcd", "x@y.test", 0, "", ""},
	}
	for _, c := range cases {
		code, text := validatePassword(c.pw, c.email)
		if code != c.code {
			t.Fatalf("pw=%q: code=%d want %d", c.pw, code, c.code)
		}
		if code != 0 && text != c.text {
			t.Fatalf("pw=%q: text=%q want %q", c.pw, text, c.text)
		}
	}
}

// TestParseSetBodyFormAndJSON — regression (2026-10-04): the e2e
// global-setup posts /user/password/set as a URL-ENCODED form (Node req.body
// parity); a JSON-only parse dropped every field → spurious
// invalid-password 400. Both encodings must decode.
func TestParseSetBodyFormAndJSON(t *testing.T) {
	e, p, tk := parseSetBody([]byte("email=e2e-admin%40e2e.test&password=Ol-Fixture-9x7K&passwordResetToken=TKN123"))
	if e != "e2e-admin@e2e.test" || p != "Ol-Fixture-9x7K" || tk != "TKN123" {
		t.Fatalf("form path: got %q %q %q", e, p, tk)
	}
	e, p, tk = parseSetBody([]byte(`{"email":"e2e-admin@e2e.test","password":"Ol-Fixture-9x7K","passwordResetToken":"TKN123"}`))
	if e != "e2e-admin@e2e.test" || p != "Ol-Fixture-9x7K" || tk != "TKN123" {
		t.Fatalf("json path: got %q %q %q", e, p, tk)
	}
	_, p, _ = parseSetBody([]byte("email=x@y.test&passwordResetToken=TKN"))
	if p != "" {
		t.Fatalf("missing password must stay empty, got %q", p)
	}
}
