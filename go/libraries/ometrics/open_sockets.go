package ometrics

import (
	"strings"
	"sync"
)

// SocketsTracker is the platform seam that supplies live socket counts (Node:
// `http/https globalAgent.sockets` / `freeSockets`, keyed `host:port`). Tests
// inject a fake tracker; the metric logic is otherwise 1:1 with open_sockets.js.
type SocketsTracker interface {
	// Connections returns hostKey ("host:port") → count of connections in the
	// given state: scheme 'http'|'https', free=false counts active sockets,
	// free=true counts the idle/free socket pool.
	Connections(scheme string, free bool) map[string]int
}

// NoopSockets reports no sockets (default platform source in tests).
type NoopSockets struct{}

// Connections always returns an empty map.
func (NoopSockets) Connections(string, bool) map[string]int { return map[string]int{} }

var (
	// Sockets is the live-socket source (Node: the four globalAgent maps).
	Sockets SocketsTracker = NoopSockets{}

	openMu        sync.Mutex
	seenOpenHTTP  = map[string]bool{}
	seenOpenHTTPS = map[string]bool{}
	seenFreeHTTP  = map[string]bool{}
	seenFreeHTTPS = map[string]bool{}
)

// ResetOpenSockets clears the seen-host state (test helper; mirrors a fresh
// module load).
func ResetOpenSockets() {
	openMu.Lock()
	seenOpenHTTP = map[string]bool{}
	seenOpenHTTPS = map[string]bool{}
	seenFreeHTTP = map[string]bool{}
	seenFreeHTTPS = map[string]bool{}
	openMu.Unlock()
}

// collectConnectionsCount mirrors open_sockets.collectConnectionsCount.
func collectConnectionsCount(sockets map[string]int, seen map[string]bool, status, scheme string, emitLegacy bool) {
	for host := range sockets {
		seen[host] = true
	}
	// Iterate a snapshot so deletion during iteration is safe.
	keys := make([]string, 0, len(seen))
	for host := range seen {
		keys = append(keys, host)
	}
	for _, host := range keys {
		hostname := host
		if i := strings.IndexByte(host, ':'); i >= 0 {
			hostname = host[:i]
		}
		open := sockets[host]
		if open == 0 {
			delete(seen, host)
		}
		Gauge("sockets", float64(open), map[string]any{
			"path":   hostname,
			"method": scheme,
			"status": status,
		})
		if status == "open" && emitLegacy {
			Gauge(status+"_connections."+scheme+"."+hostname, float64(open), nil)
		}
	}
}

// OpenSocketsMonitor mirrors the open_sockets module export.
type OpenSocketsMonitor struct{}

// GaugeOpenSockets mirrors open_sockets.gaugeOpenSockets.
func (OpenSocketsMonitor) GaugeOpenSockets(emitLegacy bool) {
	openMu.Lock()
	defer openMu.Unlock()
	collectConnectionsCount(Sockets.Connections("http", false), seenOpenHTTP, "open", "http", emitLegacy)
	collectConnectionsCount(Sockets.Connections("https", false), seenOpenHTTPS, "open", "https", emitLegacy)
	collectConnectionsCount(Sockets.Connections("http", true), seenFreeHTTP, "free", "http", false)
	collectConnectionsCount(Sockets.Connections("https", true), seenFreeHTTPS, "free", "https", false)
}

// Monitor mirrors open_sockets.monitor (interval + destructor).
func (OpenSocketsMonitor) Monitor(emitLegacy bool) {
	RegisterDestructor(func() {
		// Node: clearInterval(interval). Go: no interval to clear in the seam;
		// the hook exists for 1:1 API parity.
	})
}
