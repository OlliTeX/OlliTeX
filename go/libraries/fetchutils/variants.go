package fetchutils

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"ollitex/go/libraries/oerror"
)

// variants.go — Node fetchJson / fetchStream / fetchNothing / fetchRedirect /
// fetchString (+ the *WithResponse twins).

func prepared(opts *Options) *Options {
	if opts == nil {
		opts = &Options{}
	}
	if opts.Method == "" {
		opts.Method = http.MethodGet
	}
	return opts
}

// doRequest runs one HTTP request with the shared option normalisation
// (headers, JSON body, basic auth). Returns the response; the caller owns
// resp.Body.
func doRequest(ctx context.Context, url string, o *Options) (*http.Response, error) {
	o = prepared(o)
	headers := parseHeaders(o.Headers)
	var body io.Reader
	var pipe *bodyPipe

	jsonRaw, hasJSON, jerr := jsonBody(o)
	if jerr != nil {
		return nil, jerr
	}
	if hasJSON {
		body = bytes.NewReader(jsonRaw)
		headers.Set(http.CanonicalHeaderKey("Content-Type"), "application/json")
	} else if o.Body != nil {
		// Node: request body stream + destroy hooks → Go bodyPipe
		pipe = newBodyPipe(o.Body)
		body = pipe
	}
	if o.BasicAuth != nil {
		headers.Set(http.CanonicalHeaderKey("Authorization"), basicAuthHeader(o.BasicAuth))
	}

	client := o.Client
	if client == nil {
		client = defaultClient()
	}
	// Node: 120s over-timeout warn (armed in parseOpts, detached on settle)
	detach := watchdogStart(url, o.Method)
	defer detach()
	resp, err := performRequest(ctx, url, o.Method, headers, body, client)
	if pipe != nil {
		if err != nil {
			pipe.abort() // Node: fetchOpts.body.destroy() on request error
		} else {
			go pipe.settle(200 * time.Millisecond) // destroy if not fully sent
		}
	}
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// mapRequestError ports the node-fetch error wrapping: a connect timeout or a
// TLS trust failure surfaces as a FetchError (oracle: message
// "request to <url> failed, reason: connect timeout"; untrusted code
// DEPTH_ZERO_SELF_SIGNED_CERT). Other errors pass through unchanged.
func mapRequestError(url string, err error) error {
	var cte *ConnectTimeoutError
	if errors.As(err, &cte) {
		return &FetchError{
			Message: "request to " + url + " failed, reason: connect timeout",
			Cause:   err,
		}
	}
	if code := tlsErrorCauseCode(err); code != "" {
		return &FetchError{
			Message: "request to " + url + " failed, reason: " + code,
			Code:    code,
			Cause:   err,
		}
	}
	return nil
}

// tlsErrorCauseCode maps Go x509 errors to the Node/openssl codes the oracle
// asserts. In Go, an untrusted (including self-signed) root surfaces as
// x509.UnknownAuthorityError → Node's DEPTH_ZERO_SELF_SIGNED_CERT.
func tlsErrorCauseCode(err error) string {
	var invalid x509.CertificateInvalidError
	if errors.As(err, &invalid) {
		switch invalid.Reason {
		case x509.Expired:
			return "ERR_CERT_EXPIRED"
		default:
			return invalid.Error()
		}
	}
	var unknown x509.UnknownAuthorityError
	if errors.As(err, &unknown) {
		return "DEPTH_ZERO_SELF_SIGNED_CERT"
	}
	return ""
}

// failResponse builds a RequestFailedError from a non-2xx response, reading
// the body for the info (Node: maybeGetResponseBody before throw).
func failResponse(url, method string, resp *http.Response) *RequestFailedError {
	body := maybeGetResponseBody(resp)
	return newRequestFailedError(url, method, resp, body)
}

// --- fetchJson (Node: fetchJson / fetchJsonWithResponse) --------------------

// FetchJson mirrors fetchJson: GET request, Accept: application/json default,
// parsed JSON response.
func FetchJson(ctx context.Context, url string, opts ...*Options) (any, error) {
	value, _, err := fetchJsonWithResponse(ctx, url, take(opts))
	return value, err
}

func fetchJsonWithResponse(ctx context.Context, url string, o *Options) (any, *http.Response, error) {
	o = prepared(o)
	if !hasHeader(o.Headers, "Accept") {
		headers := parseHeaders(o.Headers)
		headers.Set(http.CanonicalHeaderKey("Accept"), "application/json")
		o.Headers = headers
	}
	resp, err := doRequest(ctx, url, o)
	if err != nil {
		return nil, nil, err
	}
	if !isOK(resp.StatusCode) {
		return nil, nil, failResponse(url, o.Method, resp)
	}
	defer resp.Body.Close()
	var value any
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&value); err != nil {
		// Node: node-fetch rejects with FetchError on invalid JSON body.
		return nil, nil, &FetchError{Message: "invalid json response body", Cause: err}
	}
	return value, resp, nil
}

// --- fetchStream (Node: fetchStream / fetchStreamWithResponse) --------------

// FetchStream mirrors fetchStream: returns the response body stream.
func FetchStream(ctx context.Context, url string, opts ...*Options) (io.ReadCloser, error) {
	stream, _, err := fetchStreamWithResponse(ctx, url, take(opts))
	return stream, err
}

func fetchStreamWithResponse(ctx context.Context, url string, o *Options) (io.ReadCloser, *http.Response, error) {
	o = prepared(o)
	resp, err := doRequest(ctx, url, o)
	if err != nil {
		return nil, nil, err
	}
	if !isOK(resp.StatusCode) {
		return nil, nil, failResponse(url, o.Method, resp)
	}
	return resp.Body, resp, nil
}

// --- fetchNothing (Node) -----------------------------------------------------

// FetchNothing mirrors fetchNothing: discards the response body, returns the
// response (body drained and closed in Go — Node keeps a consumed stream).
func FetchNothing(ctx context.Context, url string, opts ...*Options) (*http.Response, error) {
	o := prepared(take(opts))
	resp, err := doRequest(ctx, url, o)
	if err != nil {
		return nil, err
	}
	if !isOK(resp.StatusCode) {
		return nil, failResponse(url, o.Method, resp)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp, nil
}

// --- fetchRedirect (Node) ----------------------------------------------------

// FetchRedirect mirrors fetchRedirect: manual redirect mode; returns the
// immediate Location.
func FetchRedirect(ctx context.Context, url string, opts ...*Options) (string, error) {
	location, _, err := fetchRedirectWithResponse(ctx, url, take(opts))
	return location, err
}

func fetchRedirectWithResponse(ctx context.Context, url string, o *Options) (string, *http.Response, error) {
	o = prepared(o)
	client := o.Client
	if client == nil {
		client = defaultClient()
	}
	// Node: fetchOpts.redirect = 'manual'
	manual := *client
	manual.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	o.Client = &manual

	resp, err := doRequest(ctx, url, o)
	if err != nil {
		return "", nil, err
	}
	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return "", nil, failResponse(url, o.Method, resp)
	}
	location := resp.Header.Get("Location")
	if location == "" {
		headers := map[string]any{}
		for k, vs := range resp.Header {
			headers[k] = vs
		}
		cause := oerror.New("missing Location response header on 3xx response", map[string]any{"headers": headers})
		cause = cause.WithName("OError")
		return "", nil, failResponse(url, o.Method, resp).WithCause(cause)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return location, resp, nil
}

// --- fetchString (Node) ------------------------------------------------------

// FetchString mirrors fetchString: returns the response body as a string.
func FetchString(ctx context.Context, url string, opts ...*Options) (string, error) {
	body, _, err := fetchStringWithResponse(ctx, url, take(opts))
	return body, err
}

func fetchStringWithResponse(ctx context.Context, url string, o *Options) (string, *http.Response, error) {
	o = prepared(o)
	resp, err := doRequest(ctx, url, o)
	if err != nil {
		return "", nil, err
	}
	if !isOK(resp.StatusCode) {
		return "", nil, failResponse(url, o.Method, resp)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, &FetchError{Message: "reading response body", Cause: err}
	}
	return string(raw), resp, nil
}

// --- helpers -----------------------------------------------------------------

func take(opts []*Options) *Options {
	if len(opts) == 0 {
		return nil
	}
	return opts[0]
}

func hasHeader(h any, name string) bool {
	switch v := h.(type) {
	case nil:
		return false
	case http.Header:
		return len(v.Values(name)) > 0
	case map[string]any:
		for k, val := range v {
			if http.CanonicalHeaderKey(k) == http.CanonicalHeaderKey(name) && val != nil {
				return true
			}
		}
		return false
	case []any:
		for _, entry := range v {
			pair, ok := entry.([2]any)
			if !ok {
				continue
			}
			if pair[0] == name && pair[1] != nil {
				return true
			}
		}
		return false
	}
	return false
}
