package linkedurlproxy

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	pbhttp "ollitex/go/pbhttp"
)

// loopbackResolver resolves every hostname to 127.0.0.1 (so the pinned dialer
// can reach the local httptest upstreams).
var loopbackResolver = func(hostname string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
}

// testConfig builds a config pointed at local httptest servers: no blocked
// networks, fast timeout, and the loopback resolver.
func lupTestConfig() *LinkedURLProxyConfig {
	cfg := DefaultLinkedURLProxyConfig()
	cfg.ResolveHost = loopbackResolver
	cfg.FetchTimeoutMs = 5000
	return cfg
}

// lupAllowLoopback opts a config in to allowing the local (loopback) httptest
// upstreams via an allow-list entry.
func lupAllowLoopback(c *LinkedURLProxyConfig) *LinkedURLProxyConfig {
	c.AllowedResources = regexp.MustCompile(`127\.0\.0\.1`)
	return c
}

func mustStatus(t *testing.T, err error, want int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with status %d, got nil", want)
	}
	got := pbhttp.StatusOf(err)
	if got != want {
		t.Fatalf("expected status %d, got %d (err=%v)", want, got, err)
	}
}

func TestLinkedURL_SplitCIDRList(t *testing.T) {
	got := splitCIDRList("10.0.0.0/8, 192.168.0.0/16\t172.16.0.0/12")
	want := []string{"10.0.0.0/8", "192.168.0.0/16", "172.16.0.0/12"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("splitCIDRList = %v, want %v", got, want)
	}
	if got := splitCIDRList("   "); len(got) != 0 {
		t.Fatalf("expected empty list for whitespace, got %v", got)
	}
}

func TestLinkedURL_ConfigFromEnv(t *testing.T) {
	env := map[string]string{
		"OVERLEAF_LINKED_URL_BLOCKED_NETWORKS":  "10.0.0.0/8,192.168.1.0/24",
		"OVERLEAF_LINKED_URL_ALLOWED_RESOURCES": `^https://(a|b)\.example\.com/`,
		"MAX_UPLOAD_SIZE":                       "5",
		"LINKED_URL_PROXY_HOST":                 "0.0.0.0",
	}
	cfg := NewLinkedURLProxyConfigFromEnv(func(k string) string { return env[k] })
	if len(cfg.BlockedNetworks) != 2 {
		t.Fatalf("expected 2 blocked networks, got %v", cfg.BlockedNetworks)
	}
	if cfg.AllowedResources == nil || !cfg.AllowedResources.MatchString("https://a.example.com/x") {
		t.Fatalf("allowed resources regex not applied: %+v", cfg.AllowedResources)
	}
	if cfg.MaxUploadSize != 5*1024*1024 {
		t.Fatalf("MaxUploadSize = %d, want %d", cfg.MaxUploadSize, 5*1024*1024)
	}
	if cfg.Host != "0.0.0.0" {
		t.Fatalf("Host = %q", cfg.Host)
	}
	cfg2 := NewLinkedURLProxyConfigFromEnv(func(k string) string { return "" })
	if cfg2.MaxRedirects != 5 || cfg2.Port != 3066 || cfg2.Host != "127.0.0.1" || cfg2.MaxUploadSize != 50*1024*1024 {
		t.Fatalf("defaults changed: %+v", cfg2)
	}
}

func TestLinkedURL_Sanitize(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantErr  bool
		wantMsg  string
		wantHost string
		wantPath string
	}{
		{name: "ok http", in: "http://example.com/a", wantHost: "example.com", wantPath: "/a"},
		{name: "path preserved as-is (node does not rewrite)", in: "http://example.com/a//b/..//c", wantHost: "example.com", wantPath: "/a//b/..//c"},
		{name: "empty path -> /", in: "http://example.com", wantHost: "example.com", wantPath: "/"},
		{name: "bad scheme -> 500", in: "ftp://example.com/a", wantErr: true, wantMsg: "Invalid url to pass to open(): ftp://example.com/a"},
		{name: "data scheme -> 500", in: "data:text/html,x", wantErr: true, wantMsg: "Invalid url to pass to open(): data:text/html,x"},
		{name: "javascript scheme -> 500", in: "javascript:alert(1)", wantErr: true, wantMsg: "Invalid url to pass to open(): javascript:alert(1)"},
		{name: "relative -> 500 (WHATWG new URL throws)", in: "example.com", wantErr: true, wantMsg: "Invalid url to pass to open(): example.com"},
		{name: "garbage -> 500", in: "not a url", wantErr: true, wantMsg: "Invalid url to pass to open(): not a url"},
		{name: "no host -> 500", in: "http:///a", wantErr: true, wantMsg: "Invalid url to pass to open(): http:///a"},
		{name: "host with space -> 500 (WHATWG rejects)", in: "http://exa mple.com/a", wantErr: true, wantMsg: "Invalid url to pass to open(): http://exa mple.com/a"},
		{name: "path > 2000 chars -> 400", in: "http://example.com/" + strings.Repeat("a", 2006), wantErr: true, wantMsg: "Invalid or unsafe URL path: /" + strings.Repeat("a", 2006)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := sanitizeLinkedURL(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.in)
				}
				if tc.wantMsg != "" && pbhttp.MessageOf(err) != tc.wantMsg {
					t.Fatalf("message = %q, want %q", pbhttp.MessageOf(err), tc.wantMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if u.Hostname() != tc.wantHost {
				t.Fatalf("host = %q, want %q", u.Hostname(), tc.wantHost)
			}
			if u.Path != tc.wantPath {
				t.Fatalf("path = %q, want %q", u.Path, tc.wantPath)
			}
		})
	}
}

func TestLinkedURL_IsAllowedResource(t *testing.T) {
	cfg := DefaultLinkedURLProxyConfig()
	if cfg.isAllowedResource("https://a.example.com/") {
		t.Fatal("nil allowed-resource must be false")
	}
	cfg.AllowedResources = regexp.MustCompile(`^https://(a|b)\.example\.com/`)
	if !cfg.isAllowedResource("https://a.example.com/x") {
		t.Fatal("should be allowed")
	}
	if cfg.isAllowedResource("https://c.example.com/x") {
		t.Fatal("should not be allowed")
	}
}

func TestLinkedURL_IsBlockedIP(t *testing.T) {
	cases := []struct {
		name      string
		ip        string
		blocked   []string
		wantBlock bool
		wantErr   bool
	}{
		{name: "loopback blocked", ip: "127.0.0.1", wantBlock: true},
		{name: "link-local blocked", ip: "169.254.1.1", wantBlock: true},
		{name: "multicast blocked", ip: "224.0.0.1", wantBlock: true},
		{name: "private (10/8) is non-unicast -> blocked", ip: "10.0.0.5", wantBlock: true},
		{name: "private (172.16/12) blocked", ip: "172.20.1.1", wantBlock: true},
		{name: "private (192.168/16) is non-unicast -> blocked", ip: "192.168.7.9", wantBlock: true},
		{name: "unspecified blocked", ip: "0.0.0.0", wantBlock: true},
		{name: "broadcast blocked", ip: "255.255.255.255", wantBlock: true},
		{name: "reserved (240/4) blocked", ip: "240.0.0.1", wantBlock: true},
		{name: "public unicast not blocked", ip: "8.8.8.8", wantBlock: false},
		{name: "CGNAT (100.64/10) allowed (ipaddr: unicast)", ip: "100.64.3.4", wantBlock: false},
		{name: "0.0.0.1 allowed (ipaddr: unicast)", ip: "0.0.0.1", wantBlock: false},
		{name: "global IPv6 unicast not blocked", ip: "2001:4860:4860::8888", wantBlock: false},
		{name: "ULA (fc00::/7) blocked", ip: "fd12:3456:789a::1", wantBlock: true},
		{name: "v6 loopback blocked", ip: "::1", wantBlock: true},
		{name: "blocked CIDR match", ip: "10.1.2.3", blocked: []string{"10.0.0.0/8"}, wantBlock: true},
		{name: "blocked CIDR no-match", ip: "11.1.2.3", blocked: []string{"10.0.0.0/8"}, wantBlock: false},
		{name: "ipv4-mapped blocked", ip: "::ffff:127.0.0.1", wantBlock: true},
		{name: "invalid CIDR -> 500", ip: "8.8.8.8", blocked: []string{"not-a-cidr"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &LinkedURLProxyConfig{BlockedNetworks: tc.blocked}
			block, err := c.isBlockedIP(netip.MustParseAddr(tc.ip))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				if pbhttp.StatusOf(err) != 500 {
					t.Fatalf("expected 500, got statusOf=%d (%v)", pbhttp.StatusOf(err), err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if block != tc.wantBlock {
				t.Fatalf("isBlockedIP(%s) = %v, want %v", tc.ip, block, tc.wantBlock)
			}
		})
	}
}

func TestLinkedURL_CheckURLAccess(t *testing.T) {
	t.Run("no records -> 421", func(t *testing.T) {
		cfg := &LinkedURLProxyConfig{ResolveHost: func(string) ([]netip.Addr, error) { return nil, nil }}
		_, err := cfg.CheckURLAccess(context.Background(), "ghost.example", "http://ghost.example/")
		mustStatus(t, err, 421)
	})
	t.Run("resolver error -> 421", func(t *testing.T) {
		cfg := &LinkedURLProxyConfig{ResolveHost: func(string) ([]netip.Addr, error) { return nil, &net.DNSError{Err: "no such host"} }}
		_, err := cfg.CheckURLAccess(context.Background(), "ghost.example", "http://ghost.example/")
		mustStatus(t, err, 421)
	})
	t.Run("blocked ip -> 403", func(t *testing.T) {
		cfg := &LinkedURLProxyConfig{ResolveHost: loopbackResolver, BlockedNetworks: []string{"127.0.0.0/8"}}
		_, err := cfg.CheckURLAccess(context.Background(), "127.0.0.1", "http://127.0.0.1/")
		mustStatus(t, err, 403)
	})
	t.Run("allowed resource skips block", func(t *testing.T) {
		cfg := &LinkedURLProxyConfig{
			ResolveHost:      loopbackResolver,
			BlockedNetworks:  []string{"127.0.0.0/8"},
			AllowedResources: regexp.MustCompile(`127\.0\.0\.1`),
		}
		ip, err := cfg.CheckURLAccess(context.Background(), "127.0.0.1", "http://127.0.0.1/ok")
		if err != nil {
			t.Fatalf("expected allow, got %v", err)
		}
		if !ip.IsLoopback() {
			t.Fatalf("expected loopback ip, got %v", ip)
		}
	})
	t.Run("returns first record", func(t *testing.T) {
		cfg := &LinkedURLProxyConfig{ResolveHost: func(string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("1.2.3.4"), netip.MustParseAddr("5.6.7.8")}, nil
		}}
		ip, err := cfg.CheckURLAccess(context.Background(), "h", "http://h/")
		if err != nil {
			t.Fatalf("unexpected err %v", err)
		}
		if ip.String() != "1.2.3.4" {
			t.Fatalf("expected first record 1.2.3.4, got %v", ip)
		}
	})
}

func setUpstream(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func TestLinkedURL_FetchOK(t *testing.T) {
	srv := setUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("hello-upstream"))
	})
	cfg := lupAllowLoopback(lupTestConfig())
	res, err := cfg.ValidateAndFetch(context.Background(), srv.URL+"/data?q=1", 0)
	if err != nil {
		t.Fatalf("fetch error: %v", err)
	}
	if res.status != 200 {
		t.Fatalf("status = %d", res.status)
	}
	b, _ := io.ReadAll(res.body)
	if string(b) != "hello-upstream" {
		t.Fatalf("body = %q", b)
	}
	if got := headerValue(res.headers, "Content-Type", ""); !strings.Contains(got, "text/plain") {
		t.Fatalf("content-type = %q", got)
	}
}

func TestLinkedURL_FetchTooLarge(t *testing.T) {
	srv := setUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "1048576")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("small"))
	})
	cfg := lupAllowLoopback(lupTestConfig())
	cfg.MaxUploadSize = 100
	_, err := cfg.ValidateAndFetch(context.Background(), srv.URL+"/big", 0)
	mustStatus(t, err, 413)
}

func TestLinkedURL_FetchRedirectFollows(t *testing.T) {
	srv := setUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			w.Header().Set("Location", "/b")
			w.WriteHeader(302)
		case "/b":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("final"))
		default:
			w.WriteHeader(404)
		}
	})
	cfg := lupAllowLoopback(lupTestConfig())
	res, err := cfg.ValidateAndFetch(context.Background(), srv.URL+"/a", 0)
	if err != nil {
		t.Fatalf("redirect error: %v", err)
	}
	b, _ := io.ReadAll(res.body)
	if string(b) != "final" {
		t.Fatalf("body = %q, want final", b)
	}
}

func TestLinkedURL_FetchRedirectNoLocation(t *testing.T) {
	srv := setUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(301)
		_, _ = w.Write([]byte("no-location"))
	})
	cfg := lupAllowLoopback(lupTestConfig())
	_, err := cfg.ValidateAndFetch(context.Background(), srv.URL+"/x", 0)
	mustStatus(t, err, 421)
}

func TestLinkedURL_FetchRedirectLoop(t *testing.T) {
	up := setUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/loop")
		w.WriteHeader(302)
	})
	cfg := lupAllowLoopback(lupTestConfig())
	cfg.MaxRedirects = 3
	_, err := cfg.ValidateAndFetch(context.Background(), up.URL+"/loop", 0)
	mustStatus(t, err, 421)
}

func TestLinkedURL_FetchUpstreamErrorPassthrough(t *testing.T) {
	srv := setUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte("not found here"))
	})
	cfg := lupAllowLoopback(lupTestConfig())
	_, err := cfg.ValidateAndFetch(context.Background(), srv.URL+"/missing", 0)
	mustStatus(t, err, 404)
	// Node contract: RequestFailedError (OError message 'request failed') →
	// body is exactly `Error: request failed`.
	if got := pbhttp.MessageOf(err); got != "request failed" {
		t.Fatalf("message = %q, want %q", got, "request failed")
	}
}

func TestLinkedURL_FetchTimeout(t *testing.T) {
	srv := setUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		_, _ = w.Write([]byte("late"))
	})
	cfg := lupAllowLoopback(lupTestConfig())
	cfg.FetchTimeoutMs = 200
	_, err := cfg.ValidateAndFetch(context.Background(), srv.URL+"/slow", 0)
	mustStatus(t, err, 408)
}

func TestLinkedURL_HandlerMissingURL(t *testing.T) {
	cfg := lupAllowLoopback(lupTestConfig())
	srv := httptest.NewServer(cfg.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "Missing ?url parameter" {
		t.Fatalf("body = %q", b)
	}
}

func TestLinkedURL_HandlerRoutingParity(t *testing.T) {
	cfg := lupAllowLoopback(lupTestConfig())
	srv := httptest.NewServer(cfg.Handler())
	defer srv.Close()

	// Express: undefined path -> 404 `Cannot <METHOD> <path>` (text/html).
	if resp, err := http.Get(srv.URL + "/nope"); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("GET /nope status = %d, want 404", resp.StatusCode)
		}
		b, _ := io.ReadAll(resp.Body)
		want404 := "<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n<title>Error</title>\n</head>\n<body>\n<pre>Cannot GET /nope</pre>\n</body>\n</html>\n"
		if string(b) != want404 {
			t.Fatalf("body = %q, want %q", b, want404)
		}
	} else {
		t.Fatalf("get /nope: %v", err)
	}

	// Express: no route for unknown method on / -> 404 `Cannot POST /`.
	if resp, err := http.Post(srv.URL+"/", "text/plain", nil); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("POST / status = %d, want 404", resp.StatusCode)
		}
		b, _ := io.ReadAll(resp.Body)
		want404b := "<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n<title>Error</title>\n</head>\n<body>\n<pre>Cannot POST /</pre>\n</body>\n</html>\n"
		if string(b) != want404b {
			t.Fatalf("body = %q, want %q", b, want404b)
		}
	} else {
		t.Fatalf("post /: %v", err)
	}

	// HEAD mirrors GET (no body): HEAD / -> 400 like GET /.
	if req, err := http.NewRequest(http.MethodHead, srv.URL+"/", nil); err == nil {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("head: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Fatalf("HEAD / status = %d, want 400", resp.StatusCode)
		}
	}

	// Invalid URL through the handler -> 500 with the exact node message.
	resp, err := http.Get(srv.URL + "/?url=ftp%3A%2F%2Fexample.com%2Fx")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 500 {
		t.Fatalf("bad-url status = %d, want 500", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "Error: Invalid url to pass to open(): ftp://example.com/x" {
		t.Fatalf("body = %q", b)
	}
}

func TestLinkedURL_HandlerFullFlow(t *testing.T) {
	up := setUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	})
	cfg := lupAllowLoopback(lupTestConfig())
	proxy := httptest.NewServer(cfg.Handler())
	defer proxy.Close()
	resp, err := http.Get(proxy.URL + "/?url=" + urlEncode(up.URL+"/x"))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache-control = %q", got)
	}
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "ok" {
		t.Fatalf("body = %q", b)
	}
}

func TestLinkedURL_HandlerBlockedIP(t *testing.T) {
	up := setUpstream(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("x")) })
	cfg := lupAllowLoopback(lupTestConfig())
	cfg.BlockedNetworks = []string{"127.0.0.0/8"}
	cfg.AllowedResources = nil
	proxy := httptest.NewServer(cfg.Handler())
	defer proxy.Close()
	resp, err := http.Get(proxy.URL + "/?url=" + urlEncode(up.URL+"/x"))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func urlEncode(s string) string {
	return url.QueryEscape(s)
}
