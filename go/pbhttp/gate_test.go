package pbhttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func gateOK(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("inner-ok"))
}

func doGate(t *testing.T, h http.Handler, target, token string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", target, nil)
	if token != "" {
		r.Header.Set("X-Service-Token", token)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	return rr
}

func TestAuthGate_TokenSet(t *testing.T) {
	h := AuthGate(http.HandlerFunc(gateOK), "sekret", func() {})
	if rr := doGate(t, h, "/x", "sekret"); rr.Code != 200 {
		t.Fatalf("valid token → 200, got %d", rr.Code)
	}
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("Authorization", "Bearer sekret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Fatalf("bearer token → 200, got %d", rr.Code)
	}
	if rr := doGate(t, h, "/x", "wrong"); rr.Code != 401 {
		t.Fatalf("wrong token → 401, got %d", rr.Code)
	}
	if rr := doGate(t, h, "/x", ""); rr.Code != 401 {
		t.Fatalf("missing token → 401, got %d", rr.Code)
	}
}

// TestAuthGate_DefaultDeny — audit C4: empty expected token (unconfigured)
// must DENY, not open wide; openPaths stay open.
func TestAuthGate_DefaultDeny(t *testing.T) {
	var warned int
	h := AuthGate(http.HandlerFunc(gateOK), "", func() { warned++ }, "/health")
	if rr := doGate(t, h, "/data", ""); rr.Code != 401 {
		t.Fatalf("default-deny: /data → 401, got %d", rr.Code)
	}
	if rr := doGate(t, h, "/data", "anything"); rr.Code != 401 {
		t.Fatalf("default-deny: any token rejected, got %d", rr.Code)
	}
	if rr := doGate(t, h, "/health", ""); rr.Code != 200 {
		t.Fatalf("openPath /health stays open, got %d", rr.Code)
	}
	if warned == 0 {
		t.Fatal("expected a warning on first default-deny hit")
	}
}

// TestAuthGate_BodyShape — Node parity of the 401 JSON body.
func TestAuthGate_BodyShape(t *testing.T) {
	h := AuthGate(http.HandlerFunc(gateOK), "", func() {})
	rr := doGate(t, h, "/x", "")
	body := rr.Body.String()
	if !strings.Contains(body, "Invalid or missing service token") {
		t.Fatalf("401 body mismatch: %q", body)
	}
}
