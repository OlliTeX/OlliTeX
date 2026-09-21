package ologger

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

var osStderr = os.Stderr

// jsonBunyan is the default Bunyan implementation - it builds a JSON-line
// Logger that appends each entry to a buffer on the provided writer. It exists
// so the package compiles standalone; the Go test suite injects a stub instead
// (exactly like the Node sandboxed-module tests).
type jsonBunyan struct{ out io.Writer }

// NewJSONBunyan builds the default Bunyan factory writing to `out` (nil =
// stdout).
func NewJSONBunyan(out io.Writer) *jsonBunyan {
	if out == nil {
		out = os.Stdout
	}
	return &jsonBunyan{out: out}
}

func (b *jsonBunyan) RingBuffer(limit int) *RingBuffer {
	return &RingBuffer{Limit: limit}
}

// jsonLogger is the minimal bunyan Logger: it serializes entries to JSON lines
// (applying serializers + hostname/time/name/level) and applies level filtering
// against the configured stream level.
type jsonLogger struct {
	name        string
	serializers map[string]Serializer
	streams     []StreamConfig
	out         io.Writer
	level       string
}

// newJSONLogger builds the logger.
func newJSONLogger(cfg *LoggerConfig, out io.Writer) *jsonLogger {
	l := &jsonLogger{name: cfg.Name, serializers: cfg.Serializers, out: out, level: "debug"}
	if len(cfg.Streams) > 0 && cfg.Streams[0].Level != "" {
		l.level = cfg.Streams[0].Level
	}
	return l
}

func (b *jsonBunyan) CreateLogger(cfg *LoggerConfig) Logger { return newJSONLogger(cfg, b.out) }

func (l *jsonLogger) Debug(a any, msg any, args ...any) { l.emit(LevelDebug, a, msg) }
func (l *jsonLogger) Info(a any, msg any, args ...any)  { l.emit(LevelInfo, a, msg) }
func (l *jsonLogger) Error(a any, msg any, args ...any) { l.emit(LevelError, a, msg) }
func (l *jsonLogger) Warn(a any, msg any, args ...any)  { l.emit(LevelWarn, a, msg) }
func (l *jsonLogger) Fatal(a any, msg any)              { l.emit(LevelFatal, a, msg) }
func (l *jsonLogger) Level(level string)                { l.level = level }
func (l *jsonLogger) AddStream(s StreamConfig)          { l.streams = append(l.streams, s) }
func (l *jsonLogger) Name() string                      { return l.name }
func (l *jsonLogger) Serializers() map[string]Serializer {
	if l.serializers == nil {
		l.serializers = map[string]Serializer{}
	}
	return l.serializers
}

func (l *jsonLogger) emit(level int, attrs any, msg any) {
	// bunyan: suppress records below the configured logger level
	// (`if (this.level() > rec.level) return`).
	if minLevel := levelFromName(l.level); level < minLevel {
		return
	}
	entry := Entry{
		"level":    level,
		"name":     l.name,
		"hostname": hostName(),
		"v":        1,
		"msg":      msgString(msg),
	}
	if m, ok := attrs.(map[string]any); ok {
		for k, v := range m {
			if s, ok := l.serializers[k]; ok {
				v = s(v)
			}
			entry[k] = v
		}
	}
	if b, err := json.Marshal(entry); err == nil {
		fmt.Fprintf(l.out, "%s\n", b)
	}
}

// msgString renders a log message that may be absent (nil).
func msgString(msg any) string {
	if msg == nil {
		return ""
	}
	s, ok := msg.(string)
	if ok {
		return s
	}
	return fmt.Sprint(msg)
}

// hostName caches os.Hostname (Node: os.hostname()).
func hostName() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "localhost"
	}
	return h
}
