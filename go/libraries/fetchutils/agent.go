package fetchutils

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"
)

// agent.go — Node CustomHttpAgent / CustomHttpsAgent / withTimeout /
// tryToCreateConnection: a pluggable connection creator, each attempt bounded
// by connectTimeout, up to 3 attempts (Node `attempt(3)`), retry delay
// connectRetryInterval (default 100ms), the LAST error wins. A connect
// timeout surfaces ConnectTimeoutError (Node: socket destroyed with new
// ConnectTimeoutError(options)).

const (
	// ConnectAttempts — Node: attempt(3).
	ConnectAttempts = 3
	// DefaultConnectRetryInterval — Node: options.connectRetryInterval ?? 100
	DefaultConnectRetryInterval = 100 * time.Millisecond
	// ConnectTimeoutDefault — Node MAX_CONNECT_TIME (1000ms).
	ConnectTimeoutDefault = time.Second
)

// AgentOptions mirrors the Node agent options subset
// ({connectTimeout, connectRetryInterval, ca}).
type AgentOptions struct {
	ConnectTimeout       time.Duration
	ConnectRetryInterval time.Duration
	CA                   []byte // PEM root certs (Node: ca)
}

func (o *AgentOptions) retryInterval() time.Duration {
	if o.ConnectRetryInterval > 0 {
		return o.ConnectRetryInterval
	}
	return DefaultConnectRetryInterval
}

// NewCustomHttpAgent mirrors `new CustomHttpAgent({connectTimeout, ...})`:
// errors unless connectTimeout > 0 (Node: "CustomHttpAgent must be called
// with positive connectTimeout").
func NewCustomHttpAgent(opts AgentOptions) (*http.Client, error) {
	if !(opts.ConnectTimeout > 0) {
		return nil, errors.New("CustomHttpAgent must be called with positive connectTimeout")
	}
	var d net.Dialer
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DisableKeepAlives:     false,
			DialContext:           dialContextWithPolicy(opts, d.DialContext),
			IdleConnTimeout:       90 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}, nil
}

// NewCustomHttpsAgent mirrors `new CustomHttpsAgent({connectTimeout, ca, ...})`
// (Node: tls.connect through the same withTimeout machinery).
//
// The TLS dialer is installed as Transport.DialTLSContext: Go's transport
// performs its own handshake for DialContext conns, which would double-
// handshake (the server then sees the second ClientHello as HTTP).
func NewCustomHttpsAgent(opts AgentOptions) (*http.Client, error) {
	if !(opts.ConnectTimeout > 0) {
		return nil, errors.New("CustomHttpsAgent must be called with positive connectTimeout")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if len(opts.CA) > 0 {
		pool := x509.NewCertPool()
		if ok := pool.AppendCertsFromPEM(opts.CA); !ok {
			return nil, errors.New("CustomHttpsAgent: no valid certs found in ca")
		}
		tlsConfig.RootCAs = pool
	}
	var d net.Dialer // fallback for plain http URLs on the same client
	tl := tls.Dialer{
		NetDialer: &d,
		Config:    tlsConfig,
	}
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DisableKeepAlives:     false,
			DialContext:           dialContextWithPolicy(opts, d.DialContext),
			DialTLSContext:        dialContextWithPolicy(opts, tl.DialContext),
			IdleConnTimeout:       90 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}, nil
}

// connectWithPolicy implements Node withTimeout(createConnection, options,
// callback): up to ConnectAttempts attempts, each bounded by ConnectTimeout;
// between failed attempts, sleep ConnectRetryInterval (or honor ctx cancel);
// the LAST error is surfaced; a timeout surfaces ConnectTimeoutError.
func connectWithPolicy(ctx context.Context, create func(ctx context.Context, network, addr string) (net.Conn, error), network, addr string, opts AgentOptions) (net.Conn, error) { //nolint:gocognit // policy loop
	var lastErr error
	for attempt := 0; attempt < ConnectAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(opts.retryInterval()):
			}
		}
		attemptCtx, cancel := context.WithTimeout(ctx, opts.ConnectTimeout)
		conn, err := create(attemptCtx, network, addr)
		cancel()
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			lastErr = newConnectTimeoutError(map[string]any{
				"connectTimeoutMs": strconv.FormatInt(int64(opts.ConnectTimeout/time.Millisecond), 10),
			})
			continue
		}
		lastErr = err
	}
	return nil, lastErr
}

// dialContextWithPolicy is the Transport.DialContext closure form (Node:
// createConnection through withTimeout).
func dialContextWithPolicy(opts AgentOptions, create func(ctx context.Context, network, addr string) (net.Conn, error)) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return connectWithPolicy(ctx, create, network, addr, opts)
	}
}
