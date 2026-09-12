package services

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// LinkedURLProxyConfig holds every runtime setting of the linked-url-proxy
// service. The defaults and environment wiring mirror
// services/linked-url-proxy/config/settings.defaults.cjs exactly.
type LinkedURLProxyConfig struct {
	// MaxRedirects is the maximum number of redirects followed (default 5).
	MaxRedirects int
	// FetchTimeoutMs is the upstream fetch timeout in milliseconds (default 30000).
	FetchTimeoutMs int
	// BlockedNetworks is the raw list of CIDR strings to block. They are parsed
	// and matched per request so that an invalid entry yields a 500 at request
	// time (1:1 with the Node.js implementation).
	BlockedNetworks []string
	// AllowedResources, when non-nil, exempts matching URLs from the blocked-IP
	// check (1:1 with OVERLEAF_LINKED_URL_ALLOWED_RESOURCES).
	AllowedResources *regexp.Regexp
	// UserAgent is the User-Agent header sent to upstream (1:1 default string).
	UserAgent string
	// MaxUploadSize is the maximum upstream body size in bytes (default 50 MiB).
	MaxUploadSize int64
	// Host and Port are the local bind address (default 127.0.0.1:3066).
	Host string
	Port int

	// ResolveHost is the DNS backend used to resolve the target host. It
	// defaults to the system resolver (resolveHostDNS) and can be overridden in
	// tests. 1:1 with `dns.lookup(hostname, { all: true })`.
	ResolveHost func(hostname string) ([]netip.Addr, error)
}

// DefaultLinkedURLProxyConfig returns a config with the same defaults as the
// Node.js settings.defaults.cjs (env-agnostic base values). Env overrides are
// applied by NewLinkedURLProxyConfigFromEnv.
func DefaultLinkedURLProxyConfig() *LinkedURLProxyConfig {
	return &LinkedURLProxyConfig{
		MaxRedirects:   5,
		FetchTimeoutMs: 30000,
		UserAgent:      "Overleaf Extended CE - LinkedURLProxy (https://github.com/yu-i-i/overleaf-cep)",
		MaxUploadSize:  50 * 1024 * 1024,
		Host:           "127.0.0.1",
		Port:           3066,
	}
}

// NewLinkedURLProxyConfigFromEnv builds a config from the environment,
// mirroring the exact parsing in config/settings.defaults.cjs.
func NewLinkedURLProxyConfigFromEnv(getEnv func(string) string) *LinkedURLProxyConfig {
	cfg := DefaultLinkedURLProxyConfig()

	if raw := getEnv("OVERLEAF_LINKED_URL_BLOCKED_NETWORKS"); raw != "" {
		cfg.BlockedNetworks = splitCIDRList(raw)
	}
	if raw := getEnv("OVERLEAF_LINKED_URL_ALLOWED_RESOURCES"); raw != "" {
		if re, err := regexp.Compile(raw); err == nil {
			cfg.AllowedResources = re
		}
	}
	if raw := getEnv("MAX_UPLOAD_SIZE"); raw != "" {
		if mb, err := strconv.Atoi(raw); err == nil {
			cfg.MaxUploadSize = int64(mb) * 1024 * 1024
		}
	}
	cfg.Host = getEnv("LINKED_URL_PROXY_HOST")
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}
	return cfg
}

// splitCIDRList splits a comma/whitespace-delimited CIDR list (1:1 with the
// Node `.split(/[,\s]+/).filter(Boolean).map(trim)` pipeline).
func splitCIDRList(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

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
			WritePlainText(w, http.StatusMethodNotAllowed, "Method Not Allowed")
			return
		}
		target := r.URL.Query().Get("url")
		if target == "" {
			WritePlainText(w, http.StatusBadRequest, "Missing ?url parameter")
			return
		}
		res, err := c.ValidateAndFetch(r.Context(), target, 0)
		if err != nil {
			code := StatusOf(err)
			if code == 0 {
				code = http.StatusInternalServerError
			}
			WriteStatus(w, code, MessageOf(err))
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
		return nil, HTTPStatus(http.StatusMisdirectedRequest, "Too many redirects")
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
		return nil, HTTPStatusErr(http.StatusUnprocessableEntity, "Could not build upstream request", rerr)
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	resp, fetchErr := client.Do(req)
	if fetchErr != nil {
		return c.mapFetchError(ctx, fetchErr)
	}
	// NOTE: resp.Body is NOT closed on the success path — the result owns the
	// stream and the caller reads then closes it (1:1 with the Node handoff, where
	// validateAndFetch returns the stream and the controller pipes it). The
	// non-success paths close it below.
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		// 1:1 content-length guard -> 413.
		if cl := resp.Header.Get("Content-Length"); cl != "" {
			if n, perr := strconv.ParseInt(cl, 10, 64); perr == nil && n > c.MaxUploadSize {
				_ = resp.Body.Close()
				return nil, HTTPStatus(http.StatusRequestEntityTooLarge, "file too large")
			}
		}
		return &linkedURLResult{status: resp.StatusCode, body: resp.Body, headers: resp.Header}, nil
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		loc := resp.Header.Get("Location")
		_ = resp.Body.Close()
		if loc == "" {
			return nil, HTTPStatus(http.StatusMisdirectedRequest, "Redirect response missing Location header")
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
		return nil, &HTTPStatusError{Status: resp.StatusCode, Msg: resp.Status}
	}
}

// CheckURLAccess is the 1:1 Go port of Node `checkUrlAccess(hostname,
// targetUrl)`: resolve all addresses, fail 421 on no records, skip the
// blocked-IP check for allow-listed resources, else 403 on any blocked IP.
func (c *LinkedURLProxyConfig) CheckURLAccess(ctx context.Context, hostname, targetURL string) (netip.Addr, error) {
	resolve := c.ResolveHost
	if resolve == nil {
		resolve = resolveHostDNS
	}
	addrs, err := resolve(hostname)
	if err != nil || len(addrs) == 0 {
		return netip.Addr{}, HTTPStatus(http.StatusMisdirectedRequest, "DNS lookup failed for "+hostname)
	}
	if c.isAllowedResource(targetURL) {
		return addrs[0], nil
	}
	for _, a := range addrs {
		if block, perr := c.isBlockedIP(a); perr != nil {
			return netip.Addr{}, perr
		} else if block {
			return netip.Addr{}, HTTPStatus(http.StatusForbidden, "Blocked IP address: "+a.String())
		}
	}
	return addrs[0], nil
}

// isAllowedResource is the 1:1 port of Node `isAllowedResource(targetUrl)`.
func (c *LinkedURLProxyConfig) isAllowedResource(targetURL string) bool {
	if c.AllowedResources == nil {
		return false
	}
	return c.AllowedResources.MatchString(targetURL)
}

// isBlockedIP is the 1:1 port of Node `isBlockedIp(ipStr, targetUrl)`:
// unwrap IPv4-mapped addresses, block anything not unicast, and block
// addresses in the configured CIDR list (invalid CIDR -> 500).
func (c *LinkedURLProxyConfig) isBlockedIP(ip netip.Addr) (bool, error) {
	ip = ip.Unmap() // IPv4-mapped IPv6 -> IPv4 (1:1 with toIPv4Address)
	if !isUnicast(ip) {
		return true, nil
	}
	for _, raw := range c.BlockedNetworks {
		pfx, perr := netip.ParsePrefix(strings.TrimSpace(raw))
		if perr != nil {
			return false, &HTTPStatusError{Status: http.StatusInternalServerError, Msg: "Invalid blockedNetworks entry: " + raw}
		}
		if pfx.Contains(ip) {
			return true, nil
		}
	}
	return false, nil
}

// isUnicast mirrors ipaddr.js `addr.range() === 'unicast'`: an address is
// unicast only if it is not unspecified, loopback, multicast, private-use,
// or link-local. Node blocks everything in the non-unicast range (which
// includes the private 10/8, 172.16/12, 192.168/16 ranges) before checking
// the blockedNetworks list, so those are blocked here too.
func isUnicast(ip netip.Addr) bool {
	ip = ip.Unmap()
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() || ip.IsPrivate() {
		return false
	}
	if ip.Is4() {
		a := ip.As4()
		if a[0] == 169 && a[1] == 254 { // 169.254.0.0/16 link-local
			return false
		}
	} else if ip.Is6() {
		b := ip.As16()
		if b[0] == 0xfe && b[1]&0xc0 == 0x80 { // fe80::/10 link-local
			return false
		}
	}
	return true
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
		return nil, HTTPStatusErr(http.StatusRequestTimeout, "upstream request timed out", err)
	}
	return nil, HTTPStatusErr(http.StatusUnprocessableEntity, "upstream request failed", err)
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
		return nil, HTTPStatus(http.StatusBadRequest, "Invalid or unsafe URL: "+rawURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, HTTPStatus(http.StatusBadRequest, u.Scheme+" protocol is not allowed")
	}
	if u.Hostname() == "" {
		return nil, HTTPStatus(http.StatusBadRequest, "Invalid or unsafe URL: "+rawURL)
	}
	// Normalise the path (1:1 with normalizing the pathname).
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
