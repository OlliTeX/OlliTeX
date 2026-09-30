package realtime

import (
	"net/http/httptest"
	"testing"
)

// TestOriginAllowed_H3 — audit H3: same-origin + allowlist + no-origin are
// admitted; arbitrary cross-origin is rejected.
func TestOriginAllowed_H3(t *testing.T) {
	cases := []struct {
		name     string
		origin   string
		host     string
		allowEnv string
		want     bool
	}{
		{"no-origin (internal ws client)", "", "127.0.0.1:3002", "", true},
		{"same-origin", "http://127.0.0.1:7420", "127.0.0.1:7420", "", true},
		{"cross-origin rejected", "http://evil.example", "127.0.0.1:7420", "", false},
		{"allowlisted", "http://app.example", "web.example", "http://app.example,http://other.example", true},
		{"not-allowlisted", "http://attacker.example", "web.example", "http://app.example", false},
		{"origin-path ignored", "http://127.0.0.1:7420/editor", "127.0.0.1:7420", "", true},
		{"different port rejected", "http://127.0.0.1:8080", "127.0.0.1:7420", "", false},
		// H3 proxy regression: nginx forwards `Host: $host` (port stripped)
		// to /socket.io — the browser Origin always carries its port. Both
		// directions of the one-sided port must be accepted.
		{"proxy-stripped host (origin has port)", "http://127.0.0.1:4000", "127.0.0.1", "", true},
		{"proxy-kept host (both have port)", "http://127.0.0.1:4000", "127.0.0.1:4000", "", true},
		{"cross-host different port rejected", "http://evil.example:4000", "127.0.0.1", "", false},
		{"ipv6 origin + stripped host", "http://[::1]:4000", "::1", "", true},
	}
	for _, c := range cases {
		t.Setenv("OVERLEAF_REALTIME_ALLOWED_ORIGINS", c.allowEnv)
		r := httptest.NewRequest("GET", "http://x/p", nil)
		r.Host = c.host
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if got := originAllowed(r); got != c.want {
			t.Errorf("%s: originAllowed = %v, want %v", c.name, got, c.want)
		}
	}
}
