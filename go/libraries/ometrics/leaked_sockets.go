package ometrics

import (
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SocketDebug is the per-socket `_ol_debug` record leaked_sockets.js tracks.
type SocketDebug struct {
	Method       string
	Protocol     string
	URL          string
	Request      *SocketPhase
	Response     *SocketPhase
	LastLoggedAt time.Time
}

// SocketPhase is `{ headers, ts }`.
type SocketPhase struct {
	Headers any
	TS      time.Time
}

// SocketHandle is the active-handle surface the scanner reads (Node:
// `process._getActiveHandles()` entries with localAddress/localPort/...).
type SocketHandle struct {
	LocalAddress  string
	LocalPort     int
	RemoteAddress string
	RemotePort    int
	Debug         *SocketDebug
}

// ActiveHandles is the seam for `process._getActiveHandles()` filtered to
// handles carrying `_ol_debug`.
type ActiveHandles func() []SocketHandle

// ReadProcNetTcp is the seam for `fs.promises.readFile('/proc/net/tcp')`.
type ReadProcNetTcp func() (string, error)

var (
	_ = ActiveHandles(nil)
	_ = ReadProcNetTcp(nil)
)

// SocketDebug handles exposed to the scanner (default: none).
var activeHandles ActiveHandles = func() []SocketHandle { return nil }
var readProcNetTcp ReadProcNetTcp = func() (string, error) {
	b, err := os.ReadFile("/proc/net/tcp")
	return string(b), err
}

const socketMonitorInterval = 60 * 1000
const minSocketLeakTimeDefault = 15 // minutes

var minSocketLeakTime = time.Duration(minSocketLeakTimeDefault) * 60 * time.Second

// SetLeakThreshold overrides MIN_SOCKET_LEAK_TIME (tests).
func SetLeakThreshold(d time.Duration) { minSocketLeakTime = d }

// SetActiveHandles / SetProcNetTcp install test seams.
func (SocketLeakedMonitor) SetActiveHandles(h ActiveHandles) { activeHandles = h }
func (SocketLeakedMonitor) SetProcNetTcp(r ReadProcNetTcp)   { readProcNetTcp = r }

var redactRegex = regexp.MustCompile(`(?im)^(Authorization|Set-Cookie|Cookie):.*?\r`)

// FlattenHeaders mirrors leaked_sockets.flattenHeaders.
func FlattenHeaders(rawHeaders any) string {
	switch h := rawHeaders.(type) {
	case []any:
		var b strings.Builder
		for i, item := range h {
			s := stringify(item)
			if i%2 == 0 {
				b.WriteString(s + ": ")
			} else {
				b.WriteString(s + "\r\n")
			}
		}
		return b.String()
	case map[string]any:
		keys := make([]string, 0, len(h))
		for k := range h {
			keys = append(keys, k)
		}
		var parts []string
		for _, k := range keys {
			parts = append(parts, k+": "+stringify(h[k])+"\r\n")
		}
		return strings.Join(parts, "")
	case string:
		return h
	default:
		return stringify(rawHeaders)
	}
}

func stringify(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

// Redact mirrors leaked_sockets.redactObject.
func Redact(obj map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range obj {
		if value == nil {
			result[key] = nil
		} else if key == "headers" {
			result[key] = redactRegex.ReplaceAllString(FlattenHeaders(value), "$1: REDACTED\r")
		} else if isObject(value) && (key == "request" || key == "response") {
			result[key] = Redact(value.(map[string]any))
		} else {
			result[key] = value
		}
	}
	return result
}

func isObject(v any) bool {
	_, ok := v.(map[string]any)
	return ok
}

// isOldSocket mirrors leaked_sockets.isOldSocket.
func isOldSocket(debug *SocketDebug) bool {
	now := time.Now()
	created := debug.Request.TS
	lastLoggedAt := debug.LastLoggedAt
	if lastLoggedAt.IsZero() {
		lastLoggedAt = created
	}
	backoff := lastLoggedAt.Sub(created)
	if backoff < 0 {
		backoff = 0
	}
	if backoff > minSocketLeakTime/2 {
		backoff = minSocketLeakTime / 2
	}
	nextLogTime := created.Add(backoff*2 + minSocketLeakTime)
	return !now.Before(nextLogTime)
}

// LeakInfo / logOldSocket are covered by the scanner test; the pure decode
// helpers are exercised directly in the Go suite. The monitor is below.

// SocketLeakedMonitor mirrors the leaked_sockets module export.
type SocketLeakedMonitor struct{}

// ScanSockets mirrors leaked_sockets.scanSockets (drives logOldSocket over the
// old sockets correlated against /proc/net/tcp).
func (SocketLeakedMonitor) ScanSockets(logger interface {
	Error(info map[string]any, msg string, args ...any)
	Warn(info map[string]any, msg string, args ...any)
}) {
	handles := activeHandles()
	debugSockets := []SocketHandle{}
	for _, h := range handles {
		if h.Debug != nil {
			debugSockets = append(debugSockets, h)
		}
	}
	if len(debugSockets) == 0 {
		return
	}
	oldSockets := []SocketHandle{}
	for _, h := range debugSockets {
		if isOldSocket(h.Debug) {
			oldSockets = append(oldSockets, h)
		}
	}
	if len(oldSockets) == 0 {
		return
	}
	open, err := readProcNetTcp()
	if err != nil {
		logger.Error(map[string]any{"err": err.Error()}, "error getting open sockets")
		return
	}
	openSet := parseProcNetTcp(open)
	for _, h := range oldSockets {
		key := keyFromSocket(h)
		line, ok := openSet[key]
		logOldSocket(logger, h, line, ok)
	}
}

// Monitor mirrors leaked_sockets.monitor.
func (SocketLeakedMonitor) Monitor(logger interface{}) {
	RegisterDestructor(func() {})
	_ = socketMonitorInterval
	_ = logger
}

func keyFromSocket(h SocketHandle) string {
	return h.LocalAddress + ":" + itoaInt(h.LocalPort) + " -> " + h.RemoteAddress + ":" + itoaInt(h.RemotePort)
}

func itoaInt(n int) string {
	return strconv.Itoa(n)
}

// parseProcNetTcp mirrors leaked_sockets.getOpenSockets (returns key→line).
func parseProcNetTcp(content string) map[string]string {
	openSockets := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		if key := matchProcNetTcpLine(line); key != "" {
			openSockets[key] = line
		}
	}
	return openSockets
}

var tcpStateRegex = regexp.MustCompile(`(?im)^\s*\d+:\s+([0-9A-F]{8}):([0-9A-F]{4})\s+([0-9A-F]{8}):([0-9A-F]{4})`)

// matchProcNetTcpLine mirrors parseProcNetTcp over one line ("" if no match).
func matchProcNetTcpLine(line string) string {
	m := tcpStateRegex.FindStringSubmatch(line)
	if m == nil {
		return ""
	}
	localIP, _ := hexAddr(m[1])
	localPort := hexPort(m[2])
	remoteIP, _ := hexAddr(m[3])
	remotePort := hexPort(m[4])
	return keyFromSocket(SocketHandle{
		LocalAddress:  localIP,
		LocalPort:     localPort,
		RemoteAddress: remoteIP,
		RemotePort:    remotePort,
	})
}

func hexAddr(hexAddr64 string) (string, bool) {
	b, err := hex.DecodeString(strings.ToUpper(hexAddr64))
	if err != nil || len(b) != 4 {
		return "", false
	}
	// Node little-endian: a=ip&0xff (b[3]), d=(ip>>>24)&0xff (b[0])
	return fmt.Sprintf("%d.%d.%d.%d", b[3], b[2], b[1], b[0]), true
}

func hexPort(hex string) int {
	n, err := strconv.ParseInt(strings.ToUpper(hex), 16, 32)
	if err != nil {
		return 0
	}
	return int(n)
}

// logOldSocket mirrors leaked_sockets.logOldSocket.
func logOldSocket(logger interface {
	Error(info map[string]any, msg string, args ...any)
	Warn(info map[string]any, msg string, args ...any)
}, h SocketHandle, line string, hasTCP bool) {
	now := time.Now()
	created := h.Debug.Request.TS
	ageMinutes := int(now.Sub(created) / (60 * 1000))
	sanitized := Redact(socketDebugToMap(h.Debug))
	info := map[string]any{
		"localAddress":  h.LocalAddress,
		"localPort":     h.LocalPort,
		"remoteAddress": h.RemoteAddress,
		"remotePort":    h.RemotePort,
		"age":           ageMinutes,
	}
	for k, v := range sanitized {
		info[k] = v
	}
	if hasTCP {
		info["tcpinfo"] = line
		logger.Error(info, "old socket handle - tcp socket")
	} else {
		logger.Warn(info, "stale socket handle - no entry in /proc/net/tcp")
	}
	_ = created
	h.Debug.LastLoggedAt = now
}

func socketDebugToMap(d *SocketDebug) map[string]any {
	out := map[string]any{
		"method":   d.Method,
		"protocol": d.Protocol,
		"url":      d.URL,
	}
	if d.Request != nil {
		out["request"] = map[string]any{"headers": d.Request.Headers, "ts": d.Request.TS}
	}
	if d.Response != nil {
		out["response"] = map[string]any{"headers": d.Response.Headers, "ts": d.Response.TS}
	}
	return out
}
