package federation

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

// TestS2SPeerURLOverride_EnvShape — the local-dev seam (config.go
// s2sPeerURLOverride): FEDERATION_S2S_PEER_URLS JSON object → the map the
// production S2SCall wiring injects (invite_preview.go / export_wizard.go);
// empty/bad env → nil (the production wire: https://<origin>/…). The
// identity rules are UNCHANGED — this redirects the dial only (03 §8:
// the assertion keeps its https iss/aud; the transport dials here).
func TestS2SPeerURLOverride_EnvShape(t *testing.T) {
	t.Run("empty = production (nil)", func(t *testing.T) {
		os.Unsetenv("FEDERATION_S2S_PEER_URLS")
		if m := s2sPeerURLOverride(); m != nil {
			t.Fatalf("want nil (production wire), got %v", m)
		}
	})
	t.Run("JSON map = local dev dial", func(t *testing.T) {
		t.Setenv("FEDERATION_S2S_PEER_URLS", `{"peerb.example":"http://127.0.0.1:4481/federation/s2s"}`)
		m := s2sPeerURLOverride()
		if len(m) != 1 || m["peerb.example"] != "http://127.0.0.1:4481/federation/s2s" {
			t.Fatalf("map=%v", m)
		}
	})
	t.Run("bad JSON = production (nil, honest fallback)", func(t *testing.T) {
		t.Setenv("FEDERATION_S2S_PEER_URLS", `{not-json`)
		if m := s2sPeerURLOverride(); m != nil {
			t.Fatalf("want nil on bad JSON, got %v", m)
		}
	})
}

// TestS2SCall_OOverrideDials — the override is honored on the wire: with
// S2SUROverride set, the A-side CALL dials the override target (an
// httptest peer), not https://<origin>/… — the hermetic loopback form
// the S2SUROverride seam documents.
func TestS2SCall_OOverrideDials(t *testing.T) {
	var hits int32
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"payload":{"approved":true}}`))
	}))
	defer peer.Close()

	// a store with the A-side key bootstrapped (the signer).
	st := NewMapStore()
	_ = (&KeyProvider{Store: st, Site: "https://peera.example"}).Bootstrap()

	c := &S2SCall{Store: st, Site: "https://peera.example", S2SUROverride: map[string]string{"peerb.example": peer.URL + "/federation/s2s"}}
	env, err := c.Call(t.Context(), "peerb.example", "invited", map[string]any{"invitee": map[string]any{"localName": "x", "origin": "peerb.example"}})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if env["ok"] != true {
		t.Fatalf("envelope=%v", env)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("override target not dialed (hits=%d)", hits)
	}
	// and WITHOUT the override the same call is a refusal (https://
	// peerb.example is unreachable in the hermetic env) — the override
	// is what made it work.
	c2 := &S2SCall{Store: st, Site: "https://peera.example"}
	if _, err := c2.Call(t.Context(), "peerb.example", "invited", map[string]any{"invitee": map[string]any{}}); err == nil {
		t.Fatalf("expected refusal without the override (hermetic env cannot reach https://peerb.example)")
	} else if !strings.Contains(err.Error(), "peerb.example") && !strings.Contains(strings.ToLower(err.Error()), "wire") {
		t.Logf("refusal detail: %v (shape check)", err)
	}
}
