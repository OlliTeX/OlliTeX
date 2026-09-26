package collab_test

// Origin-gate regression pin (D36).
//
// The browser WebSocket handshake carries Origin; the collab service runs
// behind nginx, which MUST forward a Host that carries the same origin host
// (server-ce/nginx/overleaf.conf.template: `proxy_set_header Host $http_host`
// — NOT `$host`, which strips the port). ygo (v1.50.0) rejects an Origin
// whose host differs from the request Host (gorilla Upgrader → 403
// "Forbidden"), while non-browser clients omitting Origin are always
// permitted. This test pins that matrix at the ygo-handler level so a
// regression in either the library or the nginx header wiring fails here.
//
// (403 = origin gate. 500 = origin gate PASSED and the handshake failed for
// an unrelated reason — a bare NewServer() has no persistence adapter and the
// httptest response writer is not a Hijacker; either way the connection was
// NOT origin-rejected.)

import (
	"net/http"
	"net/http/httptest"
	"testing"

	ws "github.com/reearth/ygo/provider/websocket"
)

func wsOriginProbe(t *testing.T, srv *ws.Server, origin, host string) int {
	t.Helper()
	req, _ := http.NewRequest("GET", "http://x/room", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	req.Host = host
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w.Code
}

func TestOriginGateMatrix(t *testing.T) {
	srv := ws.NewServer()

	// Non-browser client: no Origin header — always permitted (D35/D36).
	code := wsOriginProbe(t, srv, "", "127.0.0.1:7420")
	if code == http.StatusForbidden {
		t.Fatalf("origin gate rejected an Origin-less (non-browser) handshake: %d", code)
	}

	// Browser same-origin: Origin host must equal the forwarded Host
	// ($http_host — port included). A 403 here is exactly the production
	// bug D36 fixed.
	code = wsOriginProbe(t, srv, "http://127.0.0.1:7420", "127.0.0.1:7420")
	if code == http.StatusForbidden {
		t.Fatalf("origin gate rejected same-origin handshake (Origin == Host): %d — check the nginx /collab block forwards Host WITH port ($http_host, not $host)", code)
	}

	// Cross-origin browser: must still be rejected by default (no
	// COLLAB_ALLOWED_ORIGINS configured).
	code = wsOriginProbe(t, srv, "http://evil.example", "127.0.0.1:7420")
	if code != http.StatusForbidden {
		t.Fatalf("cross-origin handshake was NOT rejected (got %d) — the default-deny origin gate is broken", code)
	}
}
