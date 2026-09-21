package fetchutils

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

var errBodyDestroyed = errors.New("request body destroyed")

// rawEarlyResponder mirrors the express /json/ignore-request behavior: it
// sends a complete response WITHOUT reading the request body. The net/http
// test server cannot do this (it drains the body before flushing early
// responses, which blocks on unbounded bodies — a Node/Go server-stack
// diverge; see HANDOFF).
func rawEarlyResponder(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	resp := "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n" +
		"Content-Length: 15\r\nConnection: close\r\n\r\n" + `{"msg":"hello"}`
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				buf := make([]byte, 1024)
				head := ""
				for !strings.Contains(head, "\r\n\r\n") {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					head += string(buf[:n])
					if len(head) > 64*1024 {
						return
					}
				}
				_, _ = c.Write([]byte(resp))
				// keep reading (discarding) so the client can still upload
				// while observing the early response
				for {
					if _, err := c.Read(buf); err != nil {
						return
					}
				}
			}(c)
		}
	}()
	return "http://" + l.Addr().String()
}

// testServer mirrors test/unit/helpers/TestServer.js (same endpoints).

type testServer struct {
	mu            sync.Mutex
	lastReq       *http.Request
	received      chan struct{}
	hangCancelled chan struct{} // closed when a /hang client went away
	largePayload  string

	httpSrv  *httptest.Server
	httpsSrv *httptest.Server
}

func newTestServer(t *testing.T) *testServer {
	ts := &testServer{
		received:      make(chan struct{}, 32),
		hangCancelled: make(chan struct{}),
		largePayload:  strings.Repeat("x", 16*1024*1024),
	}
	mux := ts.mux()
	ts.httpSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r)
	}))
	ts.httpsSrv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		// Tear down without blocking: /hang handlers may hold a live server
		// conn (client abandoned mid-request); Close() would wait for it.
		// Leaked test-server conns are harmless at binary exit.
		ts.httpSrv.CloseClientConnections()
		ts.httpsSrv.CloseClientConnections()
		go ts.httpSrv.Close()
		go ts.httpsSrv.Close()
	})
	return ts
}

func (ts *testServer) note(r *http.Request) {
	ts.mu.Lock()
	ts.lastReq = r
	ts.mu.Unlock()
	select {
	case ts.received <- struct{}{}:
	default:
	}
}

func (ts *testServer) lastRequest() *http.Request {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.lastReq
}

// waitUntilRequest polls until a request for path has been observed on this
// server (robust against buffered tokens from earlier subtests).
func (ts *testServer) waitUntilRequest(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if r := ts.lastRequest(); r != nil && strings.HasSuffix(r.URL.Path, path) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("server did not receive the %s request", path)
}

func (ts *testServer) expectHangCancelled(t *testing.T) {
	t.Helper()
	select {
	case <-ts.hangCancelled:
	case <-time.After(5 * time.Second):
		t.Fatalf("server did not observe the /hang request cancellation (Node expectRequestAborted)")
	}
}

func (ts *testServer) mux() *http.ServeMux {
	m := http.NewServeMux()

	m.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		ts.note(r)
		_, _ = w.Write([]byte("hello"))
	})
	m.HandleFunc("/large", func(w http.ResponseWriter, r *http.Request) {
		ts.note(r)
		_, _ = w.Write([]byte(ts.largePayload))
	})
	m.HandleFunc("/204", func(w http.ResponseWriter, r *http.Request) {
		ts.note(r)
		w.WriteHeader(http.StatusNoContent)
	})
	m.HandleFunc("/empty", func(w http.ResponseWriter, r *http.Request) {
		ts.note(r)
	})
	m.HandleFunc("/500", func(w http.ResponseWriter, r *http.Request) {
		ts.note(r)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Internal Server Error")) // Node helper: res.sendStatus(500)
	})
	m.HandleFunc("/400", func(w http.ResponseWriter, r *http.Request) {
		ts.note(r)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("boom-400"))
	})
	m.HandleFunc("/409", func(w http.ResponseWriter, r *http.Request) {
		ts.note(r)
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte("boom-409"))
	})
	m.HandleFunc("/413", func(w http.ResponseWriter, r *http.Request) {
		ts.note(r)
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		_, _ = w.Write([]byte("boom-413"))
	})
	m.HandleFunc("/422", func(w http.ResponseWriter, r *http.Request) {
		ts.note(r)
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte("boom-422"))
	})
	m.HandleFunc("/badjson", func(w http.ResponseWriter, r *http.Request) {
		ts.note(r)
		_, _ = w.Write([]byte("{not json"))
	})
	m.HandleFunc("/json/hello", func(w http.ResponseWriter, r *http.Request) {
		ts.note(r)
		_ = json.NewEncoder(w).Encode(map[string]any{"msg": "hello"})
	})
	m.HandleFunc("/json/add", func(w http.ResponseWriter, r *http.Request) {
		ts.note(r)
		var in struct{ A, B float64 }
		_ = json.NewDecoder(r.Body).Decode(&in)
		_ = json.NewEncoder(w).Encode(map[string]any{"sum": in.A + in.B})
	})
	m.HandleFunc("/json/500", func(w http.ResponseWriter, r *http.Request) {
		ts.note(r)
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "Internal server error"})
	})
	m.HandleFunc("/json/basic-auth", func(w http.ResponseWriter, r *http.Request) {
		ts.note(r)
		if r.Header.Get("Authorization") == "Basic dXNlcjpwYXNz" {
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "verysecret"})
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "unauthorized"})
	})
	m.HandleFunc("/json/ignore-request", func(w http.ResponseWriter, r *http.Request) {
		ts.note(r)
		_ = json.NewEncoder(w).Encode(map[string]any{"msg": "hello"})
	})
	m.HandleFunc("/sink", func(w http.ResponseWriter, r *http.Request) {
		ts.note(r)
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusNoContent)
	})
	// /hang never responds; cancellation is observable in Go via req.Context().
	// Self-terminates (30s) so no handler can outlive the test process.
	m.HandleFunc("/hang", func(w http.ResponseWriter, r *http.Request) {
		ts.note(r)
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
		select {
		case <-ts.hangCancelled:
		default:
			close(ts.hangCancelled)
		}
	})
	m.HandleFunc("/redirect/1", func(w http.ResponseWriter, r *http.Request) {
		ts.note(r)
		// Node's http server resolves relative Location headers to absolute
		// URLs; Go's does not — mirror the Node server behaviour.
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		w.Header().Set("Location", scheme+":"+"//"+r.Host+"/redirect/2")
		w.WriteHeader(http.StatusFound)
	})
	m.HandleFunc("/redirect/2", func(w http.ResponseWriter, r *http.Request) {
		ts.note(r)
		_, _ = w.Write([]byte("body after redirect"))
	})
	m.HandleFunc("/redirect/empty-location", func(w http.ResponseWriter, r *http.Request) {
		ts.note(r)
		w.WriteHeader(http.StatusFound) // 302, no Location
	})
	return m
}

func (ts *testServer) url(path string) string      { return ts.httpSrv.URL + path }
func (ts *testServer) httpsURL(path string) string { return ts.httpsSrv.URL + path }

// infiniteBody is a request body that never ends, recording Close
// (Node: stream.destroyed).
type infiniteBody struct{ closed chan struct{} }

func newInfiniteBody() *infiniteBody {
	return &infiniteBody{closed: make(chan struct{})}
}

func (b *infiniteBody) Read(p []byte) (int, error) {
	<-b.closed
	return 0, errBodyDestroyed
}

func (b *infiniteBody) Close() error {
	select {
	case <-b.closed:
	default:
		close(b.closed)
	}
	return nil
}

func (b *infiniteBody) Destroyed() bool {
	select {
	case <-b.closed:
		return true
	default:
		return false
	}
}
