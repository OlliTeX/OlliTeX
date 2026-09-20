package util

import (
	"net"
	"net/http"
	"strings"
)

// ClientIp ports Java Util.getClientIp: first "X-Forwarded-For" value, else
// the client's remote address. Java exposes host-only (no port) via
// request.getRemoteAddr(), so the remote-address path strips the port.
func ClientIp(r *http.Request) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// Java uses the first element of the (comma-separated) list.
		if i := strings.Index(xff, ","); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr)); err == nil {
		return host
	}
	// remoteAddr is already host-only (rarely; e.g. unparseable values).
	return strings.TrimSpace(r.RemoteAddr)
}
