package authpages

import "testing"

// TestValidLogoutRedirect_C2 — audit C2 (open-redirect): the post-logout
// Location must be a same-origin root-relative path.
func TestValidLogoutRedirect_C2(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"/login", true},
		{"/", true},
		{"/user/settings", true},
		{"//evil.com", false},          // protocol-relative
		{"https://evil.com", false},    // absolute
		{"http:evil.com", false},       // scheme
		{"javascript:alert(1)", false}, // script scheme
		{"/\\evil.com", false},         // backslash trick (browser normalizes /\/ → //)
		{"", false},                    // empty (caller defaults)
		{"relative", false},            // not root-relative
		{"/login\ncrash", false},       // control char injection
		{"/a/b?x=1", true},             // legit query
		{"/p/#frag", true},             // legit fragment
	}
	for _, c := range cases {
		if got := validLogoutRedirect(c.in); got != c.want {
			t.Errorf("validLogoutRedirect(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
