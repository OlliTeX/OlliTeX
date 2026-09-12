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

// isUnicast mirrors ipaddr.js `addr.range() === 'unicast'`: an address is
// unicast only if it is not unspecified, loopback, multicast, private-use, or
// link-local. Node blocks everything in the non-unicast range before checking
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
