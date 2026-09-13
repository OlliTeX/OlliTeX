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
