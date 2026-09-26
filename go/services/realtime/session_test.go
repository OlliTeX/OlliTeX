package realtime

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestSessionCookiePins — the exact cookie the Go web signs, decoded the
// exact way the bus must (the collab service pins the same contract).
func TestSessionCookiePins(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	sid := "66c9f2a41a7c1e0f1c0b9a8d"
	val := signCookie(sid, secret)
	if !strings.Contains(val, "."+sid[:0]) && !strings.HasPrefix(val, sid+".") {
		t.Fatalf("signCookie shape: %q", val)
	}
	if got := unsignCookie(val, []string{secret}); got != sid {
		t.Fatalf("unsignCookie = %q, want %q", got, sid)
	}
	if got := unsignCookie(val, []string{"wrong"}); got != "" {
		t.Fatalf("unsign with wrong secret must fail, got %q", got)
	}
	// web pipeline: value = percent-encode("s:" + sign(sid))
	full := "s:" + val
	if got := sessionSid(url.PathEscape(full), []string{secret}); got != sid {
		t.Fatalf("sessionSid = %q, want %q", got, sid)
	}
	// no-secret chain → reject
	if got := sessionSid(full, []string{}); got != "" {
		t.Fatalf("empty secret chain must reject, got %q", got)
	}
}

func TestSessionResolverRequest(t *testing.T) {
	src := &fakeSessionSource{docs: map[string]fakeSessionDoc{
		"sidA": {
			"passport":        map[string]any{"user": map[string]any{"_id": "u1", "first_name": "E", "last_name": "A", "email": "e@a"}},
			"anonTokenAccess": map[string]any{pid: "tok-123"},
		},
	}}
	r := &SessionResolver{Source: src, Secrets: []string{testSecret}}

	req, _ := http.NewRequest("GET", "http://t/x", nil)
	req.AddCookie(&http.Cookie{Name: "overleaf.sid", Value: "s:" + signCookie("sidA", testSecret)})
	u, ok := r.User(req.Context(), req)
	if !ok || u.ID != "u1" || u.Email != "e@a" {
		t.Fatalf("User = %+v ok=%v", u, ok)
	}
	tok, ok := r.AnonToken(req.Context(), req, pid)
	if !ok || tok != "tok-123" {
		t.Fatalf("AnonToken = %q ok=%v", tok, ok)
	}

	// bad signature → no user
	req2, _ := http.NewRequest("GET", "http://t/x", nil)
	req2.AddCookie(&http.Cookie{Name: "overleaf.sid", Value: "s:" + signCookie("sidA", "other")})
	if _, ok := r.User(req2.Context(), req2); ok {
		t.Fatal("bad signature must not resolve")
	}
	// no cookie → no user
	req3, _ := http.NewRequest("GET", "http://t/x", nil)
	if _, ok := r.User(req3.Context(), req3); ok {
		t.Fatal("missing cookie must not resolve")
	}
}
