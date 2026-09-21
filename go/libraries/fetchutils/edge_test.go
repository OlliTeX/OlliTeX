package fetchutils

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"

	"ollitex/go/libraries/oerror"
)

// ecdsaKey is a P-256 signing key helper.
func ecdsaKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
}

// edge_test.go — coverage for the narrow error/edge branches.

func TestFetchErrorSurface(t *testing.T) {
	// Invalid JSON response body → FetchError (fetchJson).
	ts := newTestServer(t)
	_, err := FetchJson(context.Background(), ts.url("/badjson"))
	var fe *FetchError
	if !errors.As(err, &fe) {
		t.Fatalf("got %v, want *FetchError", err)
	}
	if !errors.Is(err, fe.Cause) && fe.Cause != nil {
		t.Fatalf("Unwrap must reach the cause")
	}
	if fe.Error() == "" {
		t.Fatal("Error() must not be empty")
	}
}

func TestNonStringHeaderValues(t *testing.T) {
	ts := newTestServer(t)
	_, _ = FetchJson(context.Background(), ts.url("/json/hello"), &Options{
		Headers: map[string]any{
			"X-Bool":  true,
			"X-Int":   42,
			"X-Float": 1.5,
			"X-Str":   "text",
		},
	})
	req := ts.lastRequest()
	if got := req.Header.Get("X-Bool"); got != "true" {
		t.Fatalf("x-bool = %q", got)
	}
	if got := req.Header.Get("X-Int"); got != "42" {
		t.Fatalf("x-int = %q", got)
	}
	if got := req.Header.Get("X-Float"); got != "1.5" {
		t.Fatalf("x-float = %q", got)
	}
	if got := req.Header.Get("X-Str"); got != "text" {
		t.Fatalf("x-str = %q", got)
	}
}

func TestJsonBodyMarshalError(t *testing.T) {
	_, err := FetchJson(context.Background(), "http://example.invalid/", &Options{
		Method: http.MethodPost,
		JSON:   func() {}, // unmarshalable
	})
	if err == nil {
		t.Fatal("expected a serialization error")
	}
}

func TestPerformRequestInvalidMethod(t *testing.T) {
	_, err := performRequest(context.Background(), "http://example.invalid/", "NOT A METHOD", http.Header{}, nil, defaultClient())
	if err == nil {
		t.Fatal("expected an error for an invalid HTTP method")
	}
	var oe *oerror.OError
	if !errors.As(err, &oe) {
		t.Fatalf("got %v, want *oerror.OError", err)
	}
}

func TestExpiredCertServer(t *testing.T) {
	// Serve on a self-signed EXPIRED cert; the client (no CA) must get a
	// FetchError with code ERR_CERT_EXPIRED.
	key, err := ecdsaKey()
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:             time.Now().Add(-2 * time.Hour),
		NotAfter:              time.Now().Add(-1 * time.Hour), // already expired
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(cryptorand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	cert := &tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  key,
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tlsLn := tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{*cert}})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello"))
	})}
	t.Cleanup(func() { srv.Close() })
	go func() { _ = srv.Serve(tlsLn) }()

	agent, err := NewCustomHttpsAgent(AgentOptions{ConnectTimeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	_, err = FetchString(context.Background(), "https://"+ln.Addr().String()+"/", &Options{Client: agent})
	var fe *FetchError
	if !errors.As(err, &fe) {
		t.Fatalf("got %v, want *FetchError", err)
	}
	if fe.Code != "ERR_CERT_EXPIRED" {
		t.Fatalf("code = %q, want ERR_CERT_EXPIRED", fe.Code)
	}
}
