package orcidpicker

import (
	"context"
	"net"
	"strings"
)

// isPrivateAddress — exact port of the Node isPrivateAddress() decision
// tree (OrcidService.mjs): IPv4 special ranges, IPv4-mapped IPv6, and the
// enumerated IPv6 families.
func isPrivateAddress(ip string) bool {
	s := strings.TrimSpace(ip)
	// IPv4-mapped / IPv4-compatible IPv6: [?]::[ffff:]a.b.c.d[?]
	if inner, ok := mappedIPv4InIPv6(s); ok {
		return isPrivateAddress(inner)
	}
	dot := strings.Count(s, ".")
	if strings.Contains(s, ".") && dot == 3 {
		parts := strings.Split(s, ".")
		ok := true
		var vals [4]uint
		for i, p := range parts {
			// Node: Number.parseInt(p, 10) must be an integer 0..255;
			// '' / non-numeric → NaN → fail. Leading zeros parse fine.
			if p == "" {
				ok = false
				break
			}
			v := 0
			for _, c := range p {
				if c < '0' || c > '9' {
					ok = false
					break
				}
				v = v*10 + int(c-'0')
			}
			if !ok {
				break
			}
			if v > 255 {
				ok = false
				break
			}
			vals[i] = uint(v)
		}
		if !ok {
			return false
		}
		a, b := int(vals[0]), int(vals[1])
		return a == 0 || // 0.0.0.0/8
			a == 10 || // 10.0.0.0/8
			a == 127 || // 127.0.0.0/8
			(a == 100 && b >= 64 && b <= 127) || // 100.64.0.0/10 (CGNAT)
			(a == 169 && b == 254) || // 169.254.0.0/16 (link-local)
			(a == 172 && b >= 16 && b <= 31) || // 172.16.0.0/12
			(a == 192 && b == 168) || // 192.168.0.0/16
			(a == 198 && (b == 18 || b == 19)) || // 198.18.0.0/15
			a >= 224 // 224.0.0.0/3 + reserved
	}
	if strings.Contains(s, ":") {
		lower := strings.ToLower(s)
		if lower == "::" {
			return true
		}
		if lower == "::1" {
			return true
		}
		if strings.HasPrefix(lower, "fe8") || strings.HasPrefix(lower, "fe9") || strings.HasPrefix(lower, "fea") || strings.HasPrefix(lower, "feb") {
			return true // fe80::/10 link-local
		}
		if len(lower) > 4 && lower[0] == 'f' && (lower[1] == 'c' || lower[1] == 'd') &&
			hexDigit(lower[2]) && hexDigit(lower[3]) && lower[4] == ':' {
			return true // fc00::/7 ULA   (Node: /^f[cd][0-9a-f]{2}:/)
		}
		if strings.HasPrefix(lower, "fec0:") {
			return true // deprecated site-local
		}
		if strings.HasPrefix(lower, "ff") {
			return true // multicast
		}
	}
	return false
}

func hexDigit(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f'
}

// [?]::(?:ffff:)?a.b.c.d[?] (Node's leading regex, case-insensitive).
func mappedIPv4InIPv6(s string) (string, bool) {
	lower := strings.ToLower(s)
	rest := lower
	if strings.HasPrefix(rest, "[") {
		rest = rest[1:]
		if !strings.HasSuffix(rest, "]") {
			return "", false
		}
		rest = strings.TrimSuffix(rest, "]")
	}
	if !strings.HasPrefix(rest, "::") {
		return "", false
	}
	rest = rest[2:]
	if strings.HasPrefix(rest, "ffff:") {
		rest = rest[5:]
	}
	if strings.Count(rest, ".") != 3 {
		return "", false
	}
	return rest, true
}

// checkHostNotPrivate — Node: dns.promises.lookup(hostname, { all: true })
// then reject if ANY record is non-public (mitigates DNS-rebind at request
// time; safeFetch re-checks per redirect hop).
//
// Go: net.Resolver.LookupHost with a nil (system) resolver — the same
// resolver the container uses, so the decision agrees with Node's.
func checkHostNotPrivate(ctx context.Context, host string) error {
	if err := ctx.Err(); err != nil {
		return &orcidErr{msg: transportText(ctx, err)}
	}
	ips, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return &orcidErr{msg: dnsErrText(err, host)}
	}
	for _, ip := range ips {
		if isPrivateAddress(ip) {
			return &orcidErr{msg: errBlocked}
		}
	}
	return nil
}

// dnsErrText — Node's dns-promises rejection message for getaddrinfo
// failures: "getaddrinfo <CODE> <host>" (CODE ∈ ENOTFOUND / EAI_AGAIN /
// ETIMEDOUT / ...). The pinned gate paths never hit this (the sandbox
// resolves ORCID + doi.org to public IPs); faithful for parity anyway.
func dnsErrText(err error, host string) string {
	msg := err.Error()
	if strings.Contains(msg, "no such host") || strings.Contains(msg, "not found") {
		return "getaddrinfo ENOTFOUND " + host
	}
	if strings.Contains(msg, "i/o timeout") {
		return "getaddrinfo ETIMEDOUT " + host
	}
	if strings.Contains(msg, "temporary failure") || strings.Contains(msg, "no answer from DNS server") {
		return "getaddrinfo EAI_AGAIN " + host
	}
	return "getaddrinfo EAI_FAIL " + host
}

// transportText — Node undici's outer error for a request that never gets a
// response ("fetch failed") or was aborted by the timeout controller
// ("The operation was aborted").
func transportText(ctx context.Context, err error) string {
	if ctx.Err() != nil {
		return "The operation was aborted"
	}
	msg := err.Error()
	if strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "context canceled") {
		return "The operation was aborted"
	}
	return "fetch failed"
}
