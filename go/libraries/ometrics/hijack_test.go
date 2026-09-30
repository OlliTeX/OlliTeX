package ometrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

// TestHTTPMiddleware_WebSocketHijack — B-defect #2: the middleware's writer
// must forward http.Hijacker (and Flusher); gorilla's Upgrader asserts
// w.(http.Hijacker) and returns a bare 500 when the assertion fails —
// without this the editor's socket.io WS (and every collab Y-WS behind this
// middleware) fails the handshake with exactly that 500.
func TestHTTPMiddleware_WebSocketHijack(t *testing.T) {
	up := websocket.Upgrader{}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, msg, _ := conn.ReadMessage()
		_ = conn.WriteMessage(websocket.TextMessage, []byte("echo:"+string(msg)))
		_ = conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	})
	srv := httptest.NewServer(HTTPMiddleware(next))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/"
	dialer := websocket.Dialer{}
	conn, resp, err := dialer.Dial(wsURL, http.Header{})
	if err != nil {
		code, status := 0, "no response"
		if resp != nil {
			code, status = resp.StatusCode, resp.Status
		}
		t.Fatalf("handshake failed: %d %s (%v) — B-defect #2: Hijacker not forwarded by the middleware", code, status, err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("want 101, got %d", resp.StatusCode)
	}
	defer conn.Close()
	if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if string(msg) != "echo:ping" {
		t.Fatalf("round trip: got %q", msg)
	}
}
