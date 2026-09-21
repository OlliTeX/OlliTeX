// Package fetchutils is the 1:1 drop-in Go port of `libraries/fetch-utils`
// (npm `@overleaf/fetch-utils`, node-fetch 2 wrapper):
//
//   - fetchJson / fetchJsonWithResponse
//   - fetchStream / fetchStreamWithResponse
//   - fetchNothing
//   - fetchRedirect / fetchRedirectWithResponse
//   - fetchString / fetchStringWithResponse
//   - RequestFailedError ("request failed", info {url, method, status, body?})
//   - ConnectTimeoutError ("connect timeout")
//   - CustomHttpAgent / CustomHttpsAgent (connect timeout + 3-attempt retry)
//   - setLogger (120s over-timeout warn)
//
// Go conventions (documented deltas):
//   - Node's AbortSignal → Go context.Context (ctx.Err drives cancellation).
//   - Node's stream destroy hooks are native in Go: closing the response body
//     (or the context) tears the request down; destroying an unconsumed
//     request reader closes it (pinned in tests).
//   - Node's agent/timeout plumbing → `*http.Client` with a retried dialer
//     (ConnectTimeout/ConnectRetryInterval, 3 attempts — node semantics).
//   - `setLogger({warn})` → SetLogger(WarnFunc); the 120s watchdog warns
//     {url, method, overTimeoutMs, stack} "Fetch request did not complete
//     within 120 seconds" (stack is Go's runtime Frames, Node's V8 stack).
package fetchutils

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"ollitex/go/libraries/oerror"
)

// BasicAuth mirrors {user, password}.
type BasicAuth struct {
	User     string
	Password string
}

// Options mirrors FetchRequestOptions (RequestInit + json/basicAuth).
type Options struct {
	Method    string // default GET
	Headers   any    // http.Header | map[string]any | []any of [name,value] pairs; nil values dropped (Node)
	JSON      any    // request JSON body → JSON string + Content-Type: application/json
	Body      io.Reader
	BasicAuth *BasicAuth
	Client    *http.Client // nil → DefaultClient (1s connect timeout, 3 attempts)
	// Redirect is node-fetch redirect mode; the fetch*Redirect variants set
	// "manual" themselves.
}

// WarnFunc is the logger seam (Node: setLogger({warn}) pino-style
// warn(object, message)).
type WarnFunc func(info map[string]any, message string)

var warn WarnFunc

// SetLogger mirrors setLogger({warn}).
func SetLogger(w WarnFunc) { warn = w }

// RequestWarnTimeout is the 120s over-timeout warn threshold (Node: 120000ms).
// A var (not const) so tests can shorten it.
var RequestWarnTimeout = 120 * time.Second

// FetchError mirrors node-fetch FetchError for non-HTTP/protocol failures
// (e.g. an invalid JSON response body), distinct from RequestFailedError.
type FetchError struct {
	Message string
	Code    string // Node: FetchError.code (e.g. DEPTH_ZERO_SELF_SIGNED_CERT)
	Cause   error
}

func (e *FetchError) Error() string { return e.Message }

func (e *FetchError) Unwrap() error { return e.Cause }

// RequestFailedError mirrors `class RequestFailedError extends OError`:
// message "request failed", info {url, method, status[, body]} where body is
// only included for 400/409/413/422 (Node spread-condition).
type RequestFailedError struct {
	*oerror.OError
	Url      string `json:"-"`
	Method   string
	Status   int
	Body     string
	Response *http.Response
}

func newRequestFailedError(url, method string, resp *http.Response, body string) *RequestFailedError {
	info := map[string]any{
		"url":    url,
		"method": method,
		"status": resp.StatusCode,
	}
	switch resp.StatusCode {
	case 400, 409, 413, 422:
		info["body"] = body
	}
	e := oerror.New("request failed", info).WithName("RequestFailedError")
	rfe := &RequestFailedError{
		OError:   e,
		Url:      url,
		Method:   method,
		Status:   resp.StatusCode,
		Response: resp,
	}
	if body != "" {
		rfe.Body = body
	}
	return rfe
}

// WithCause mirrors RequestFailedError.prototype chain (Node: .withCause).
func (r *RequestFailedError) WithCause(cause error) *RequestFailedError {
	r.OError = r.OError.WithCause(cause)
	return r
}

// ConnectTimeoutError mirrors `class ConnectTimeoutError extends OError`.
type ConnectTimeoutError struct{ *oerror.OError }

func newConnectTimeoutError(info map[string]any) *ConnectTimeoutError {
	return &ConnectTimeoutError{oerror.New("connect timeout", info).WithName("ConnectTimeoutError")}
}

// --- headers (Node: parseHeaders) ------------------------------------------

// parseHeaders normalises header input to http.Header. Node drops values that
// are null/undefined (they would otherwise stringify to "undefined") and
// stringifies the rest. Repeated keys append (Node: Headers.append).
func parseHeaders(h any) http.Header {
	out := http.Header{}
	switch v := h.(type) {
	case nil:
	case http.Header:
		for k, vs := range v {
			for _, x := range vs {
				out.Add(k, x)
			}
		}
	case map[string]any:
		for k, val := range v {
			if val == nil {
				continue // unset → dropped (Node)
			}
			out.Add(k, headerValue(val))
		}
	case []any:
		for _, entry := range v {
			pair, ok := entry.([2]any)
			if !ok {
				continue
			}
			if pair[1] == nil {
				continue
			}
			name, _ := pair[0].(string)
			out.Add(name, headerValue(pair[1]))
		}
	}
	return out
}

func headerValue(val any) string {
	switch v := val.(type) {
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.FormatInt(int64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	}
	return fmt.Sprint(val)
}

// --- body helpers (Node: setupJsonBody / setupBasicAuth) -------------------

func jsonBody(opts *Options) ([]byte, bool, error) {
	if opts == nil || opts.JSON == nil {
		return nil, false, nil
	}
	raw, err := json.Marshal(opts.JSON)
	if err != nil {
		return nil, false, fmt.Errorf("fetchutils: encoding json body: %w", err)
	}
	return raw, true, nil
}

func basicAuthHeader(auth *BasicAuth) string {
	sum := base64.StdEncoding.EncodeToString([]byte(auth.User + ":" + auth.Password))
	return "Basic " + sum
}

// --- watchdog (Node: the 120s over-timeout warn) ----------------------------

// watchdogStart arms the 120s over-timeout warn (Node: setTimeout 120000).
// Returns a detach function (Node: detachSignal) which must be called exactly
// once when the request settles (success or error).
func watchdogStart(url, method string) func() {
	start := time.Now()
	stack := captureStack()
	timer := time.AfterFunc(RequestWarnTimeout, func() {
		if warn != nil {
			warn(map[string]any{
				"url":           url,
				"method":        method,
				"overTimeoutMs": float64(time.Since(start).Microseconds()) / 1000,
				"stack":         stack,
			}, "Fetch request did not complete within 120 seconds")
		}
	})
	return func() { timer.Stop() }
}

func captureStack() []uintptr {
	pc := make([]uintptr, 32)
	n := runtime.Callers(3, pc)
	return pc[:n]
}

// --- request body pipe (Node: body stream destroy semantics) ----------------

// bodyPipe wraps a streaming request body so the transport side can be torn
// down and the source destroyed, mirroring Node's
// `requestBodyStream.destroy()` hooks (performRequest error path and the
// response-body-close hook in the stream variants).
type bodyPipe struct {
	src      io.Reader
	closer   io.Closer // set when src implements io.Closer
	pr       *io.PipeReader
	pw       *io.PipeWriter
	copyDone atomic.Bool
	aborted  atomic.Bool
}

func newBodyPipe(src io.Reader) *bodyPipe {
	pr, pw := io.Pipe()
	b := &bodyPipe{src: src, pr: pr, pw: pw}
	if c, ok := src.(io.Closer); ok {
		b.closer = c
	}
	go func() {
		_, err := io.Copy(pw, src)
		if err != nil {
			// The source errored (destroyed/aborted) → fail the request
			// (Node: destroy() → controller.abort()). A clean close here
			// would only signal end-of-body (EOF), not an abort.
			_ = pw.CloseWithError(err)
		} else if !b.aborted.Load() {
			_ = pw.Close() // normal end of the request body
		}
		b.copyDone.Store(true)
	}()
	return b
}

// Read implements io.Reader for the transport.
func (b *bodyPipe) Read(p []byte) (int, error) { return b.pr.Read(p) }

// Close implements io.Closer — the http transport may close the request body
// when it tears the connection down (Node: stream.destroy()).
func (b *bodyPipe) Close() error {
	b.abort()
	return nil
}

// abort stops the copy and destroys the source (Node: requestBodyStream.destroy()).
func (b *bodyPipe) abort() {
	if !b.aborted.CompareAndSwap(false, true) {
		return
	}
	_ = b.pw.Close()
	if b.closer != nil {
		_ = b.closer.Close()
	}
}

// settle aborts the body if, after the request settled, it was NOT fully
// consumed (Node: response.body.on('close', () => { if (!readableEnded)
// destroy })). Finite bodies finish within the grace window; infinite
// bodies are destroyed deterministically.
func (b *bodyPipe) settle(grace time.Duration) {
	deadline := time.Now().Add(grace)
	for !b.copyDone.Load() {
		if time.Now().After(deadline) {
			b.abort()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// --- core request (Node: performRequest) ------------------------------------

var (
	defaultClientOnce  sync.Once
	defaultClientValue *http.Client
)

// defaultClient mirrors setupDefaultAgent: 1s connect timeout (Node
// MAX_CONNECT_TIME) with the 3-attempt dialer, routing by protocol like Node's
// agent-per-protocol function (Go's dialer does that natively).
func defaultClient() *http.Client {
	defaultClientOnce.Do(func() {
		c, err := NewCustomHttpAgent(AgentOptions{ConnectTimeout: ConnectTimeoutDefault})
		if err != nil {
			// ConnectTimeoutDefault is positive; NewCustomHttpAgent only
			// fails for non-positive timeouts.
			panic(err.Error())
		}
		defaultClientValue = c
	})
	return defaultClientValue
}

// performRequest mirrors Node's performRequest: on transport error the error
// is Tagged (oerror) with {url, method}; on success the caller owns the
// response body (Node: detachSignal wired to body close).
func performRequest(ctx context.Context, url, method string, headers http.Header, body io.Reader, client *http.Client) (*http.Response, error) {
	if client == nil {
		client = defaultClient()
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, oerror.Tag(err, err.Error(), map[string]any{"url": url, "method": method})
	}
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		// Node: detachSignal(); destroy request body; OError.tag(err, msg, {url, method})
		if closer, ok := body.(io.Closer); ok {
			_ = closer.Close()
		}
		if mapped := mapRequestError(url, err); mapped != nil {
			return nil, mapped
		}
		return nil, oerror.Tag(err, err.Error(), map[string]any{"url": url, "method": method})
	}
	return resp, nil
}

// maybeGetResponseBody mirrors Node's maybeGetResponseBody (read body on 2xx
// failure surfaces; errors → empty string, matching Node's null).
func maybeGetResponseBody(resp *http.Response) string {
	if resp.Body == nil {
		return ""
	}
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return ""
	}
	return string(raw)
}

// isOK mirrors node-fetch response.ok (200-299).
func isOK(status int) bool { return status >= 200 && status < 300 }
