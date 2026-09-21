package fetchutils

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// agent_test.go — port of the describe('CustomHttpAgent' / 'CustomHttpsAgent')
// oracle blocks.

func certPEM(cert *x509.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
}

func TestCustomHttpAgent(t *testing.T) {
	ts := newTestServer(t)

	t.Run("makes an http request successfully", func(t *testing.T) {
		agent, err := NewCustomHttpAgent(AgentOptions{ConnectTimeout: 100 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		body, err := FetchString(context.Background(), ts.url("/hello"), &Options{Client: agent})
		if err != nil {
			t.Fatal(err)
		}
		if body != "hello" {
			t.Fatalf("body = %q", body)
		}
	})

	t.Run("times out when accessing a non-routable address", func(t *testing.T) {
		agent, err := NewCustomHttpAgent(AgentOptions{ConnectTimeout: 10 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		_, err = FetchString(context.Background(), "http://10.255.255.255/", &Options{Client: agent})
		var fe *FetchError
		if !errors.As(err, &fe) {
			t.Fatalf("got %v, want *FetchError", err)
		}
		want := "request to http://10.255.255.255/ failed, reason: connect timeout"
		if fe.Message != want {
			t.Fatalf("message = %q, want %q", fe.Message, want)
		}
	})

	t.Run("retries after a delay", func(t *testing.T) {
		// t      0ms: first connect fails with ECONNREFUSED
		// t    500ms: listen on port
		// t   1000ms: retry to connect again, works
		// t   1500ms: respond to request
		agent, err := NewCustomHttpAgent(AgentOptions{
			ConnectTimeout:       100 * time.Millisecond,
			ConnectRetryInterval: 1 * time.Second,
		})
		if err != nil {
			t.Fatal(err)
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(500 * time.Millisecond)
			_, _ = w.Write([]byte("hello"))
		})
		srv := &http.Server{Handler: mux}
		t.Cleanup(func() { srv.Close() })

		// Grab a dynamic port, then close again so that the first connect
		// attempt is refused before the server starts listening for real.
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()

		go func() {
			time.Sleep(500 * time.Millisecond)
			l2, err := net.Listen("tcp", "127.0.0.1:"+itoa(port))
			if err != nil {
				t.Errorf("re-listen: %v", err)
				return
			}
			_ = srv.Serve(l2)
		}()

		t0 := time.Now()
		body, err := FetchString(context.Background(), "http://127.0.0.1:"+itoa(port), &Options{Client: agent})
		if err != nil {
			t.Fatal(err)
		}
		if body != "hello" {
			t.Fatalf("body = %q", body)
		}
		if got := time.Since(t0); got < 1500*time.Millisecond {
			t.Fatalf("finished in %v, want >= 1500ms (retry delay)", got)
		}
		srv.Close()
	})

	t.Run("does not open a stray connection when the socket errors after connect", func(t *testing.T) {
		agent, err := NewCustomHttpAgent(AgentOptions{
			ConnectTimeout:       100 * time.Millisecond,
			ConnectRetryInterval: 10 * time.Millisecond,
		})
		if err != nil {
			t.Fatal(err)
		}
		connections := &connCounter{}

		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				connections.add()
				// Reset the established connection before responding. The
				// request handler guarantees the client finished the connect
				// phase, so the RST arrives after 'connect' has fired.
				go resetConn(c)
			}
		}()
		defer ln.Close()

		_, err = FetchString(context.Background(), "http://127.0.0.1:"+itoa(ln.Addr().(*net.TCPAddr).Port)+"/", &Options{Client: agent})
		if err == nil {
			t.Fatal("expected the request to fail after the reset")
		}
		// Leave time for a buggy connect retry to open a stray connection.
		time.Sleep(200 * time.Millisecond)
		if got := connections.count(); got != 1 {
			t.Fatalf("connections = %d, want exactly 1 (no stray reconnect)", got)
		}
	})

	t.Run("errors when connectTimeout is not positive", func(t *testing.T) {
		if _, err := NewCustomHttpAgent(AgentOptions{}); err == nil {
			t.Fatal("expected an error for non-positive connectTimeout")
		}
	})
}

func TestCustomHttpsAgent(t *testing.T) {
	ts := newTestServer(t)

	// httptest.NewTLSServer serves a self-signed cert; extract it for the ca
	// option.
	client := ts.httpsSrv.Client()
	transport := client.Transport.(*http.Transport)
	// Recover the server cert by dialing once.
	conn, err := tls.Dial("tcp", strings.TrimPrefix(ts.httpsSrv.URL, "https://"), &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // test: capture cert
	if err != nil {
		t.Fatal(err)
	}
	certs := conn.ConnectionState().PeerCertificates
	_ = conn.Close()
	_ = transport
	t.Logf("URLS http=%s https=%s", ts.httpSrv.URL, ts.httpsSrv.URL)

	t.Run("makes an https request successfully", func(t *testing.T) {
		agent, err := NewCustomHttpsAgent(AgentOptions{
			ConnectTimeout: 100 * time.Millisecond,
			CA:             certPEM(certs[0]),
		})
		if err != nil {
			t.Fatal(err)
		}
		body, err := FetchString(context.Background(), ts.httpsURL("/hello"), &Options{Client: agent})
		if err != nil {
			t.Fatal(err)
		}
		if body != "hello" {
			t.Fatalf("body = %q", body)
		}
	})

	t.Run("rejects an untrusted server", func(t *testing.T) {
		agent, err := NewCustomHttpsAgent(AgentOptions{ConnectTimeout: 100 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		_, err = FetchString(context.Background(), ts.httpsURL("/hello"), &Options{Client: agent})
		var fe *FetchError
		if !errors.As(err, &fe) {
			t.Fatalf("got %v, want *FetchError", err)
		}
		if fe.Code != "DEPTH_ZERO_SELF_SIGNED_CERT" {
			t.Fatalf("code = %q, want DEPTH_ZERO_SELF_SIGNED_CERT", fe.Code)
		}
	})

	t.Run("times out when accessing a non-routable address", func(t *testing.T) {
		agent, err := NewCustomHttpsAgent(AgentOptions{ConnectTimeout: 10 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		_, err = FetchString(context.Background(), "https://10.255.255.255/", &Options{Client: agent})
		var fe *FetchError
		if !errors.As(err, &fe) {
			t.Fatalf("got %v, want *FetchError", err)
		}
		want := "request to https://10.255.255.255/ failed, reason: connect timeout"
		if fe.Message != want {
			t.Fatalf("message = %q, want %q", fe.Message, want)
		}
	})

	t.Run("errors when connectTimeout is not positive", func(t *testing.T) {
		if _, err := NewCustomHttpsAgent(AgentOptions{}); err == nil {
			t.Fatal("expected an error for non-positive connectTimeout")
		}
	})

	t.Run("rejects invalid ca PEM", func(t *testing.T) {
		if _, err := NewCustomHttpsAgent(AgentOptions{ConnectTimeout: 10 * time.Millisecond, CA: []byte("not a cert")}); err == nil {
			t.Fatal("expected an error for invalid ca PEM")
		}
	})
}

// --- watchdog (Node: the 120s over-timeout warn) ----------------------------

func TestOverTimeoutWarn(t *testing.T) {
	ts := newTestServer(t)

	old := RequestWarnTimeout
	RequestWarnTimeout = 80 * time.Millisecond
	t.Cleanup(func() { RequestWarnTimeout = old })

	type warnCall struct {
		info    map[string]any
		message string
	}
	calls := make(chan warnCall, 4)
	SetLogger(func(info map[string]any, message string) {
		calls <- warnCall{info: info, message: message}
	})
	t.Cleanup(func() { SetLogger(nil) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := FetchString(ctx, ts.url("/hang"))
		done <- err
	}()

	// The request is in flight; the watchdog should fire before the request
	// settles (never, until we cancel).
	select {
	case c := <-calls:
		if c.message != "Fetch request did not complete within 120 seconds" {
			t.Fatalf("message = %q", c.message)
		}
		for _, k := range []string{"url", "method", "overTimeoutMs", "stack"} {
			if _, ok := c.info[k]; !ok {
				t.Fatalf("info missing key %q: %v", k, c.info)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("over-timeout warn did not fire")
	}
	cancel()
	<-done
}

// connCounter — safe connection counter.
type connCounter struct {
	mu  sync.Mutex
	cnt int
}

func (c *connCounter) add()       { c.mu.Lock(); c.cnt++; c.mu.Unlock() }
func (c *connCounter) count() int { c.mu.Lock(); defer c.mu.Unlock(); return c.cnt }

func itoa(i int) string { return strconv.Itoa(i) }

// resetConn closes a connection with an RST (Node: req.socket.resetAndDestroy).
func resetConn(c net.Conn) {
	tcp, ok := c.(*net.TCPConn)
	if !ok {
		_ = c.Close()
		return
	}
	_ = tcp.SetLinger(0)
	_ = c.Close()
}
