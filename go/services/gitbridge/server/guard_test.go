// guard_test.go — 2c (09 §3): the read-only export-PAT write guard.
//
// Node contract (GitBridgeAuthMiddleware + GitBridgePATManager): on WRITE
// (receive-pack advertisement + push) resolve the token WITH scope; scope
// starting with `federation:` → 403 — WITHOUT importing the federation
// module. Reads (upload-pack) keep working for the same token.

package server

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ollitex/go/services/gitbridge/config"
)

const (
	fedScope  = "federation:git_bridge"
	fedPAT    = "olp_guardtoken00000000000000000000000"
	guardProj = "650a00000000000000000000" // 24-hex, IsProjectID shape
)

func scopedFake(code int, errorCode, scope string) OAuthScopedClient {
	return func(token, ip string) (int, string, string) { return code, errorCode, scope }
}

func TestReadOnlyExportWritePredicate(t *testing.T) {
	cases := []struct {
		name string
		r    gitRoute
		sc   string
		want bool
	}{
		{"receive-push + federation", routeReceivePush, fedScope, true},
		{"receive-adv + federation", routeReceiveAdv, fedScope, true},
		{"receive-push + plain git_bridge", routeReceivePush, "git_bridge", false},
		{"receive-push + legacy empty scope", routeReceivePush, "", false},
		{"receive-push + other scope", routeReceivePush, "gh_sync", false},
		{"upload-push + federation (READ allowed)", routeUploadFetch, fedScope, false},
		{"upload-adv + federation (READ allowed)", routeUploadAdv, fedScope, false},
		{"grid + federation", routeGrid, fedScope, false},
	}
	for _, c := range cases {
		if got := readonlyExportWrite(c.r, c.sc); got != c.want {
			t.Errorf("%s: readonlyExportWrite(%v, %q) = %v, want %v", c.name, c.r, c.sc, got, c.want)
		}
	}
}

// doAuthed drives the full dispatch with a git Basic-auth credential.
func doAuthed(h http.Handler, method, path, ct, user, pass string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+pass)))
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func guardHandler(scoped OAuthScopedClient) http.Handler {
	cfg := &config.Config{Oauth2Server: "http://web.example"}
	return NewServerWithScopedSeams(cfg, nil, "git", "", "", nil, scoped)
}

// TestFederationPATWriteDeniedHandler — the full request flow for the guard:
// a federation-scoped PAT is accepted by the token check (200) but refused
// on WRITE with the 403 guard body (distinct from the resolve-failure grid).
// The br bridge is never reached — the guard fires first.
func TestFederationPATWriteDeniedHandler(t *testing.T) {
	h := guardHandler(scopedFake(200, "", fedScope))

	// POST push (routeReceivePush).
	w := doAuthed(h, http.MethodPost, "/"+guardProj+".git/git-receive-pack",
		"application/x-git-receive-pack-request", "git", fedPAT)
	if w.Code != http.StatusForbidden {
		t.Fatalf("push: want 403, got %d body=%q", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "read-only export token") {
		t.Errorf("push: want the 2c guard body, got %q", w.Body.String())
	}

	// GET receive advertisement (routeReceiveAdv).
	w = doAuthed(h, http.MethodGet, "/"+guardProj+".git/info/refs?service=git-receive-pack",
		"", "git", fedPAT)
	if w.Code != http.StatusForbidden {
		t.Fatalf("receive-adv: want 403, got %d body=%q", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "read-only export token") {
		t.Errorf("receive-adv: want the 2c guard body, got %q", w.Body.String())
	}
}

// TestFederationPATGuardRespectsTokenRejection — the 4xx branches of the
// scoped seam keep working (429 / 401 token_expired / 500), i.e. the guard
// never masks a rejected token.
func TestFederationPATGuardRespectsTokenRejection(t *testing.T) {
	cases := []struct {
		name      string
		code      int
		errorCode string
		want      int
		wantSub   string
	}{
		{"429", 429, "", http.StatusTooManyRequests, "Rate limit exceeded"},
		{"401-expired", 401, "token_expired", http.StatusUnauthorized, "token has expired"},
		{"500", 500, "", http.StatusInternalServerError, "Unexpected server error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := guardHandler(scopedFake(c.code, c.errorCode, fedScope))
			w := doAuthed(h, http.MethodPost, "/"+guardProj+".git/git-receive-pack",
				"application/x-git-receive-pack-request", "git", fedPAT)
			if w.Code != c.want {
				t.Fatalf("want %d, got %d body=%q", c.want, w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), c.wantSub) {
				t.Errorf("want %q in body, got %q", c.wantSub, w.Body.String())
			}
		})
	}
}

// TestScopedSeamFallbackToPlain — with NO scoped seam wired (legacy wiring),
// the plain OAuthClient path still drives the flow and the scope is "" (guard
// off) — i.e. the 2c change is opt-in per deployment.
func TestScopedSeamFallbackToPlain(t *testing.T) {
	cfg := &config.Config{Oauth2Server: "http://web.example"}
	plain := OAuthClient(func(token, ip string) (int, string) { return 429, "" })
	h := NewServerWithSeams(cfg, nil, "git", "", "", plain) // scoped seam nil
	w := doAuthed(h, http.MethodPost, "/"+guardProj+".git/git-receive-pack",
		"application/x-git-receive-pack-request", "git", fedPAT)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("plain fallback: want 429, got %d body=%q", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "read-only export token") {
		t.Errorf("guard must stay off without the scoped seam")
	}
}
