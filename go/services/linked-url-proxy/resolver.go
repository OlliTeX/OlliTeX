package linkedurlproxy

import (
	"context"
	"net/http"
	"net/netip"
	"strings"

	pbhttp "ollitex/go/pbhttp"
)

// CheckURLAccess is the 1:1 Go port of Node `checkUrlAccess(hostname,
// targetUrl)`: resolve all addresses, fail 421 on no records, skip the
// blocked-IP check for allow-listed resources, else 403 on any blocked IP.
func (c *LinkedURLProxyConfig) CheckURLAccess(ctx context.Context, hostname, targetURL string) (netip.Addr, error) {
	resolve := c.ResolveHost
	if resolve == nil {
		resolve = pbhttp.ResolveHostDNS
	}
	addrs, err := resolve(hostname)
	if err != nil || len(addrs) == 0 {
		return netip.Addr{}, pbhttp.HTTPStatus(http.StatusMisdirectedRequest, "DNS lookup failed for "+hostname)
	}
	if c.isAllowedResource(targetURL) {
		return addrs[0], nil
	}
	for _, a := range addrs {
		if block, perr := c.isBlockedIP(a); perr != nil {
			return netip.Addr{}, perr
		} else if block {
			return netip.Addr{}, pbhttp.HTTPStatus(http.StatusForbidden, "Blocked IP address: "+a.String())
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
			return false, &pbhttp.HTTPStatusError{Status: http.StatusInternalServerError, Msg: "Invalid blockedNetworks entry: " + raw}
		}
		if pfx.Contains(ip) {
			return true, nil
		}
	}
	return false, nil
}

// isUnicast mirrors ipaddr.js `addr.range() === 'unicast'` (allowed list).
// Blocked (non-unicast): unspecified 0.0.0.0/32 + ::, loopback 127.0.0.0/8
// + ::1, private 10.0.0.0/8 + 172.16.0.0/12 + 192.168.0.0/16 + fc00::/7,
// link-local 169.254.0.0/16 + fe80::/10, multicast 224.0.0.0/4 + ff00::/8,
// broadcast 255.255.255.255/32, reserved 240.0.0.0/4. ipaddr.js does NOT
// classify 100.64.0.0/10 (CGNAT) or the rest of 0.0.0.0/8 (e.g. 0.0.0.1) as
// non-unicast, so those two ranges stay allowed.
var v4BlockedPrefixes = []netip.Prefix{
	netip.PrefixFrom(netip.AddrFrom4([4]byte{0, 0, 0, 0}), 32),         // 0.0.0.0
	netip.PrefixFrom(netip.AddrFrom4([4]byte{127, 0, 0, 0}), 8),        // loopback
	netip.PrefixFrom(netip.AddrFrom4([4]byte{10, 0, 0, 0}), 8),         // private
	netip.PrefixFrom(netip.AddrFrom4([4]byte{172, 16, 0, 0}), 12),      // private
	netip.PrefixFrom(netip.AddrFrom4([4]byte{192, 168, 0, 0}), 16),     // private
	netip.PrefixFrom(netip.AddrFrom4([4]byte{169, 254, 0, 0}), 16),     // link-local
	netip.PrefixFrom(netip.AddrFrom4([4]byte{224, 0, 0, 0}), 4),        // multicast
	netip.PrefixFrom(netip.AddrFrom4([4]byte{240, 0, 0, 0}), 4),        // reserved
	netip.PrefixFrom(netip.AddrFrom4([4]byte{255, 255, 255, 255}), 32), // broadcast
}

var v6BlockedPrefixes = []netip.Prefix{
	netip.PrefixFrom(netip.MustParseAddr("::"), 128),
	netip.PrefixFrom(netip.MustParseAddr("::1"), 128),
	netip.PrefixFrom(netip.MustParseAddr("fc00::"), 7),
	netip.PrefixFrom(netip.MustParseAddr("fe80::"), 10),
	netip.PrefixFrom(netip.MustParseAddr("ff00::"), 8),
}

func isUnicast(ip netip.Addr) bool {
	ip = ip.Unmap()
	if ip.Is4() {
		for _, p := range v4BlockedPrefixes {
			if p.Contains(ip) {
				return false
			}
		}
		return true
	}
	for _, p := range v6BlockedPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}
