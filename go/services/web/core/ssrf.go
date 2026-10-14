// core/ssrf.go — shared server-side-request guard (bug-hunt 2026-10-14,
// BH-1/BH-3, owner-gated product change).
//
// The web plane dials user-supplied URLs (LLM provider check/scan +
// assistant; WebDAV connect/sync). A user must never be able to make the
// server DIAL the container network (compose 172.16/12: mongo/filer/smtp
// sink/S3) or loopback — that is SSRF with an observable content/status
// oracle (proven live: sw-filer/smtpsink/sw-s3 bodies echoed to the caller).
//
// Two layers:
//   ① CheckSSRFHost — parse-time veto for literal-IP hosts (immediate 400,
//      no dial). Names pass here and are enforced at dial time.
//   ② SafeClient — every outbound dial (first hop, EVERY redirect hop, and
//      DNS-rebinding attempts) resolves the host and VETOS THE WHOLE address
//      set if ANY result is blocked (fail-closed), then dials the RESOLVED IP
//      directly (TOCTOU-safe: no name re-resolution at connect).
//
// Blocked: 0/8, 10/8, 100.64/10, 127/8, 169.254/16, 172.16/12, 192.168/16,
// 198.18/15, 224/3+240/4; IPv6 ::, ::1, fe80/10 (link-local), fc00::/7
// (ULA incl. fc::/8+fd::/8); IPv4-mapped (::ffff:a.b.c.d) judged by the
// embedded IPv4. Public IPs and public names (resolving only to public
// addresses) pass.
package core

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"time"
)

// testBypass — test-only escape hatch (unit tests dialing loopback
// httptest servers). Server-process env, unreachable from any web client;
// unset in production/e2e → the guard is always enforced there.
func testBypass() bool {
	e := os.Getenv("OL_SSRF_TEST_BYPASS")
	return e == "1" || e == "true"
}

// ErrSSRFBlocked — literal/resolved address is in a blocked range.
var ErrSSRFBlocked = errors.New("blocked host (private/loopback/link-local/internal not allowed)")

// ErrSSRFNoUsableAddress — no usable address at all.
var ErrSSRFNoUsableAddress = errors.New("no usable outbound address")

// SSRFBlocked reports whether `ip` falls in a blocked range.
func SSRFBlocked(ip netip.Addr) bool {
	if ip.Is6() && ip.Is4In6() {
		// IPv4-mapped/compatible ::ffff:a.b.c.d → judge the embedded IPv4.
		z := ip.As16()
		ip = netip.AddrFrom4([4]byte{z[12], z[13], z[14], z[15]})
	}
	if ip.Is4() {
		b := ip.As4()
		switch {
		case b[0] == 0, b[0] == 10, b[0] == 127:
			return true
		case b[0] == 172 && b[1] >= 16 && b[1] <= 31:
			return true // 172.16.0.0/12
		case b[0] == 192 && b[1] == 168:
			return true
		case b[0] == 169 && b[1] == 254:
			return true // link-local
		case b[0] == 100 && b[1]&0xFC == 64:
			return true // CGNAT 100.64.0.0/10
		case b[0] == 198 && (b[1] == 18 || b[1] == 19):
			return true // benchmarking 198.18/15
		case b[0] >= 224:
			return true // multicast + reserved
		}
		return false
	}
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return true
	}
	z := ip.As16()
	if (z[0] & 0xfe) == 0xfc {
		return true // fc00::/7 ULA (fc::/8 + fd::/8)
	}
	return false
}

// CheckSSRFHost vetoes a literal-IP host at parse time (names pass — the dial
// guard enforces ranges against the RESOLVED address set).
func CheckSSRFHost(host string) error {
	if testBypass() {
		return nil
	}
	host = strings.ToLower(strings.Trim(strings.TrimSpace(host), "."))
	if host == "" {
		return ErrSSRFNoUsableAddress
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if SSRFBlocked(ip) {
			return ErrSSRFBlocked
		}
	}
	return nil
}

// resolveSafe — resolve a host, fail closed if ANY result is blocked or no
// usable address exists, and return the first safe IP.
func resolveSafe(ctx context.Context, host string) (netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		if SSRFBlocked(ip) {
			return netip.Addr{}, ErrSSRFBlocked
		}
		return ip, nil
	}
	names, err := (&net.Resolver{PreferGo: true}).LookupHost(ctx, host)
	if err != nil {
		return netip.Addr{}, err
	}
	var first netip.Addr
	for _, nm := range names {
		ip, perr := netip.ParseAddr(nm)
		if perr != nil {
			continue
		}
		if SSRFBlocked(ip) {
			return netip.Addr{}, ErrSSRFBlocked // ANY private result vetoes
		}
		if !first.IsValid() {
			first = ip
		}
	}
	if !first.IsValid() {
		return netip.Addr{}, ErrSSRFNoUsableAddress
	}
	return first, nil
}

// SafeClient — http.Client with the dial-level SSRF guard. The dialer
// resolves each target and dials the resolved IP directly (rebinding-safe);
// CheckRedirect re-vetoes each Location (literal-IP fast + resolved-set for
// names). Use for every user-supplied outbound URL dial site.
func SafeClient(timeout time.Duration) *http.Client {
	if testBypass() {
		return &http.Client{Timeout: timeout}
	}
	dialer := net.Dialer{Timeout: 10 * time.Second}
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          96,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, serr := net.SplitHostPort(addr)
			if serr != nil {
				return nil, ErrSSRFNoUsableAddress
			}
			ip, rerr := resolveSafe(ctx, host)
			if rerr != nil {
				return nil, rerr
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		},
	}
	c := &http.Client{Timeout: timeout, Transport: tr}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		h := req.URL.Hostname()
		if h == "" {
			return http.ErrUseLastResponse
		}
		if err := CheckSSRFHost(h); err != nil {
			return http.ErrUseLastResponse
		}
		if _, err := resolveSafe(context.Background(), h); err != nil {
			return http.ErrUseLastResponse
		}
		return nil
	}
	return c
}
