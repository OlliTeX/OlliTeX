package wakatime

// security.go — audit 034-N1 (problems_30092026C §5.N1): guard the
// user-configurable WakaTime/Wakapi endpoint.
//
// Design note (why this is NOT a copy of the orcidpicker B3 guard):
// the WakaTime API URL is USER-CONFIGURABLE BY DESIGN — self-hosted
// Wakapi instances (audit 035 runs one in the default stack on the
// docker network) live at PRIVATE addresses. A blanket
// scheme-must-https + private-IP rejection would break the legitimate
// self-host case the platform is shipping. The hardening that matches
// the threat model is therefore layered and instance-policy driven:
//
//  1. strict URL validation at every trust boundary (link / verify /
//     heartbeat / client dial): parseable, http(s) only, no embedded
//     userinfo (credentials travel in the Authorization header),
//     sane host/length caps — kills malformed schemes, file://, and
//     URL-embedded credentials (which would leak into logs/echoes).
//  2. WAKATIME_ALLOWED_API_HOSTS — instance admin lockdown: a
//     comma-separated list of exact hosts or `*.suffix` patterns a
//     user's api URL host must match when SET. Unset = any http(s)
//     host (self-hosted-Wakapi default). This is the owner's lever
//     to pin the integration to `*.wakatime.com` on public instances.
//  3. WAKATIME_REJECT_PRIVATE_IPS=1 — resolve the host and reject any
//     private/loopback/link-local address (orcidpicker B3 decision
//     tree). Off by default so docker-network/self-hosted Wakapi keeps
//     working; on for public instances where user-chosen endpoints
//     must stay on the public internet.
//  4. echo hygiene: the API URL in responses is the URL the user
//     themselves submitted (no foreign data), and userinfo is
//     structurally impossible (rejected at validation).

import (
	"context"
	"net"
	"net/url"
	"os"
	"strings"
)

const maxAPIURLLen = 2048 // generous; WakaTime/Wakapi URLs are short

// allowedAPIHosts — WAKATIME_ALLOWED_API_HOSTS (comma list; entry =
// exact host or `*.suffix`). Empty = no host lockdown (default).
func allowedAPIHosts() []string {
	v := os.Getenv("WAKATIME_ALLOWED_API_HOSTS")
	if v == "" {
		return nil
	}
	var out []string
	for _, e := range strings.Split(v, ",") {
		e = strings.ToLower(strings.TrimSpace(e))
		if e != "" {
			out = append(out, e)
		}
	}
	return out
}

// rejectPrivateIPs — WAKATIME_REJECT_PRIVATE_IPS (1/true/yes). Off by
// default (self-hosted Wakapi on the docker network is first-class).
func rejectPrivateIPs() bool {
	switch strings.ToLower(os.Getenv("WAKATIME_REJECT_PRIVATE_IPS")) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// validateAPIURL — boundary validation (audit N1-1). Returns the
// normalized base URL (trailing '/' stripped) or a 400-shaped error.
func validateAPIURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errInvalidURL
	}
	if len(s) > maxAPIURLLen {
		return "", errInvalidURL
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errInvalidURL
	}
	switch strings.ToLower(u.Scheme) {
	case "https", "http":
	default:
		return "", errInvalidURL
	}
	if u.User != nil {
		// no credentials in the URL — the apiKey IS the credential and
		// travels in the Authorization header; embedded userinfo would
		// leak into logs/echoes and bypass the encrypted store.
		return "", errInvalidURL
	}
	if host, _, herr := net.SplitHostPort(u.Host); herr == nil {
		u.Host = host // keep it; reassembly uses u.Host verbatim
		_ = host
	}
	return strings.TrimRight(s, "/"), nil
}

// hostAllowed — WAKATIME_ALLOWED_API_HOSTS match (exact or *.suffix).
func hostAllowed(host string, allow []string) bool {
	if len(allow) == 0 {
		return true
	}
	h := strings.ToLower(strings.TrimSpace(host))
	for _, a := range allow {
		if a == h {
			return true
		}
		if strings.HasPrefix(a, "*.") && (strings.HasSuffix(h, a[1:]) || h == a[2:]) {
			// *.wakatime.com matches wakatime.com and a.b.wakatime.com
			return true
		}
	}
	return false
}

// isPrivateAddress — the orcidpicker B3 IPv4 decision tree (compact
// re-implementation: the features packages are siblings and must not
// import each other) + the IPv6 families used there.
func isPrivateAddress(ip string) bool {
	s := strings.TrimSpace(ip)
	if v4, err := netipParse(s); err == nil {
		a, b := int(v4[0]), int(v4[1])
		return a == 0 || // 0.0.0.0/8
			a == 10 || // 10/8
			a == 127 || // 127/8 loopback
			(a == 100 && b >= 64 && b <= 127) || // CGNAT
			(a == 169 && b == 254) || // link-local
			(a == 172 && b >= 16 && b <= 31) || // 172.16/12
			(a == 192 && b == 168) || // 192.168/16
			(a == 198 && (b == 18 || b == 19)) || // benchmarking
			a >= 224 // multicast + reserved
	}
	p := net.ParseIP(s)
	if p == nil {
		return false
	}
	if v6 := p.To16(); v6 != nil {
		lower := strings.ToLower(p.String())
		if lower == "::" || lower == "::1" {
			return true
		}
		if strings.HasPrefix(lower, "fe8") || strings.HasPrefix(lower, "fe9") ||
			strings.HasPrefix(lower, "fea") || strings.HasPrefix(lower, "feb") {
			return true // fe80::/10
		}
		if len(lower) > 4 && lower[0] == 'f' && (lower[1] == 'c' || lower[1] == 'd') &&
			hexish(lower[2:4]) {
			return true // fc00::/7
		}
		if strings.HasPrefix(lower, "fec0:") || strings.HasPrefix(lower, "ff") {
			return true
		}
		if strings.HasPrefix(lower, "ffff:") || strings.HasPrefix(strings.TrimPrefix(lower, ":"), "ffff:") {
			if inner, ok := mappedIPv4(lower); ok {
				return isPrivateAddress(inner)
			}
		}
	}
	return false
}

func hexish(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return len(s) == 2
}

func netipParse(s string) ([4]byte, error) {
	p := net.ParseIP(s)
	if p == nil {
		return [4]byte{}, errInvalidURL
	}
	if v4 := p.To4(); v4 != nil {
		return [4]byte{v4[0], v4[1], v4[2], v4[3]}, nil
	}
	return [4]byte{}, errInvalidURL
}

func mappedIPv4(lower string) (string, bool) {
	const m = "ffff:"
	i := strings.Index(lower, m)
	if i < 0 {
		return "", false
	}
	rest := lower[i+len(m):]
	// also handle the ::ffff: leading form
	if strings.Contains(rest, "ffff:") {
		i2 := strings.LastIndex(lower, "ffff:")
		rest = lower[i2+len(m):]
	}
	if strings.Count(rest, ".") == 3 && !strings.Contains(rest, ":") {
		return rest, true
	}
	return "", false
}

// checkCredsPolicy — the full N1 policy for one credential set:
// URL shape + host lockdown (if configured) + private-IP rejection
// (if configured). Every route that dials the user endpoint goes
// through this, so a credential stored BEFORE a tightening of policy is
// still blocked at use time.
func checkCredsPolicy(ctx context.Context, cr wakaCreds) error {
	base, verr := validateAPIURL(cr.APIURL)
	if verr != nil {
		return verr
	}
	u, uerr := url.Parse(base)
	if uerr != nil {
		return errInvalidURL
	}
	host := u.Hostname()
	if !hostAllowed(host, allowedAPIHosts()) {
		return errHostBlocked
	}
	if rejectPrivateIPs() {
		if err := ctx.Err(); err != nil {
			return err
		}
		ips, lerr := net.DefaultResolver.LookupHost(ctx, host)
		if lerr != nil {
			return errHostBlocked // unresolvable from the server = not a usable endpoint
		}
		for _, ip := range ips {
			if isPrivateAddress(ip) {
				return errHostBlocked
			}
		}
	}
	return nil
}
