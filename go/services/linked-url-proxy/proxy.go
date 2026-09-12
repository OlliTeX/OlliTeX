package linkedurlproxy

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	pbhttp "ollitex/go/pbhttp"
)

// linkedURLResult is the outcome of a successful upstream fetch.
type linkedURLResult struct {
	status int
	body   interface {
		Read([]byte) (int, error)
		Close() error
	}
	headers http.Header
}

// Handler returns the HTTP handler implementing the Node.js `proxy` route
// (`app.get('/', proxy)`), 1:1.
func (c *LinkedURLProxyConfig) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			pbhttp.WritePlainText(w, http.StatusMethodNotAllowed, "Method Not Allowed")
			return
		}
		target := r.URL.Query().Get("url")
		if target == "" {
			pbhttp.WritePlainText(w, http.StatusBadRequest, "Missing ?url parameter")
			return
		}
		res, err := c.ValidateAndFetch(r.Context(), target, 0)
		if err != nil {
			code := pbhttp.StatusOf(err)
			if code == 0 {
				code = http.StatusInternalServerError
			}
			pbhttp.WriteStatus(w, code, pbhttp.MessageOf(err))
			return
		}
		defer res.body.Close() // caller owns the stream on the success path
		w.Header().Set("Content-Type", headerValue(res.headers, "Content-Type", "application/octet-stream"))
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(res.status)
		safeCopy(w, res.body, r.Context().Done())
	}
}

// ValidateAndFetch is the 1:1 Go port of Node `validateAndFetch(rawUrl,
// redirectCount)`: sanitize URL, allow-list/blocked-IP checks via DNS, fetch
// pinned to the validated IP (anti rebinding), size limit, manual redirect
// handling, timeout mapping.
func (c *LinkedURLProxyConfig) ValidateAndFetch(ctx context.Context, rawURL string, redirectCount int) (*linkedURLResult, error) {
	if redirectCount > c.MaxRedirects {
		return nil, pbhttp.HTTPStatus(http.StatusMisdirectedRequest, "Too many redirects")
	}

	u, err := sanitizeLinkedURL(rawURL)
	if err != nil {
		return nil, err
	}

	validated, derr := c.CheckURLAccess(ctx, u.Hostname(), u.String())
	if derr != nil {
		return nil, derr
	}

	client := c.newPinnedClient(u, validated)
	req, rerr := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if rerr != nil {
		return nil, pbhttp.HTTPStatusErr(http.StatusUnprocessableEntity, "Could not build upstream request", rerr)
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	resp, fetchErr := client.Do(req)
	if fetchErr != nil {
		return c.mapFetchError(ctx, fetchErr)
	}
	// NOTE: resp.Body is NOT closed on the success path — the result owns the
	// stream and the caller reads then closes it (1:1 with the Node handoff).
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		// 1:1 content-length guard -> 413.
		if cl := resp.Header.Get("Content-Length"); cl != "" {
			if n, perr := strconv.ParseInt(cl, 10, 64); perr == nil && n > c.MaxUploadSize {
				_ = resp.Body.Close()
				return nil, pbhttp.HTTPStatus(http.StatusRequestEntityTooLarge, "file too large")
			}
		}
		return &linkedURLResult{status: resp.StatusCode, body: resp.Body, headers: resp.Header}, nil
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		loc := resp.Header.Get("Location")
		_ = resp.Body.Close()
		if loc == "" {
			return nil, pbhttp.HTTPStatus(http.StatusMisdirectedRequest, "Redirect response missing Location header")
		}
		next := url.URL{}
		if ref, uerr := url.Parse(loc); uerr == nil {
			next = *u.ResolveReference(ref)
		} else {
			next = *u
		}
		return c.ValidateAndFetch(ctx, next.String(), redirectCount+1)
	default:
		_ = resp.Body.Close()
		return nil, &pbhttp.HTTPStatusError{Status: resp.StatusCode, Msg: resp.Status}
	}
}

// newPinnedClient builds an http.Client that dials the validated IP (anti
// DNS-rebinding, 1:1 with the Node undici `lookup` override) while keeping the
// original host for SNI/certificate verification on TLS.
func (c *LinkedURLProxyConfig) newPinnedClient(u *url.URL, validated netip.Addr) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		ForceAttemptHTTP2: false,
		TLSClientConfig:   &tls.Config{ServerName: u.Hostname()},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			_, port, serr := net.SplitHostPort(addr)
			if serr != nil || port == "" {
				port = "80"
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(validated.String(), port))
		},
	}
	timeout := time.Duration(c.FetchTimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse // handle redirects manually
		},
	}
}

// mapFetchError maps a transport-level error to the Node.js semantics:
// timeout -> 408, otherwise 422 (Node: `err.type === "request-timeout" ? 408 : 422`).
func (c *LinkedURLProxyConfig) mapFetchError(ctx context.Context, err error) (*linkedURLResult, error) {
	if ctx.Err() == context.DeadlineExceeded || isTimeoutError(err) {
		return nil, pbhttp.HTTPStatusErr(http.StatusRequestTimeout, "upstream request timed out", err)
	}
	return nil, pbhttp.HTTPStatusErr(http.StatusUnprocessableEntity, "upstream request failed", err)
}

// isTimeoutError reports whether err represents a network/HTTP timeout.
func isTimeoutError(err error) bool {
	switch err.(type) {
	case net.Error:
		nerr := err.(net.Error)
		return nerr.Timeout()
	default:
		if strings.Contains(err.Error(), "context deadline exceeded") {
			return true
		}
	}
	return false
}

// sanitizeLinkedURL is the 1:1 port of the Node URL sanitising + protocol +
// path-normalisation checks (strict-url-sanitise + als-normalize-urlpath).
func sanitizeLinkedURL(rawURL string) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, pbhttp.HTTPStatus(http.StatusBadRequest, "Invalid or unsafe URL: "+rawURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, pbhttp.HTTPStatus(http.StatusBadRequest, u.Scheme+" protocol is not allowed")
	}
	if u.Hostname() == "" {
		return nil, pbhttp.HTTPStatus(http.StatusBadRequest, "Invalid or unsafe URL: "+rawURL)
	}
	p := u.Path
	if p == "" {
		p = "/"
	}
	cleaned := strings.Trim(strings.Trim(p, "/"), "")
	if cleaned == "" {
		u.Path = "/"
	} else {
		u.Path = "/" + pathClean(cleaned)
	}
	return u, nil
}

// pathClean collapses redundant slashes/dots in a path (a lightweight stand-in
// for als-normalize-urlpath). It is pure and deterministic.
func pathClean(p string) string {
	parts := strings.Split(p, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", ".":
			// skip duplicate slashes and single dots
		case "..":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
		default:
			out = append(out, part)
		}
	}
	return strings.Join(out, "/")
}

// headerValue returns the first value for the header key (case-insensitive) or
// def if absent.
func headerValue(h http.Header, key, def string) string {
	if v := h.Get(key); v != "" {
		return v
	}
	return def
}

// safeCopy streams src to dst, aborting early if the client goes away (1:1
// with destroying the upstream stream on error/abort).
func safeCopy(dst http.ResponseWriter, src interface{ Read([]byte) (int, error) }, done <-chan struct{}) {
	buf := make([]byte, 32*1024)
	for {
		select {
		case <-done:
			return
		default:
		}
		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if rerr != nil {
			return
		}
	}
}
