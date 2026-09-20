// Package wglog ports util/Log.java: static log helpers honouring LOG_LEVEL
// env var (default INFO; "WARNING" maps to WARN to match logback levels).
package wglog

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
)

var (
	mu     sync.Mutex
	logger = slog.Default()
)

// parseLevel maps LOG_LEVEL names to slog levels (logback semantics).
func parseLevel(name string) slog.Level {
	switch strings.ToUpper(name) {
	case "TRACE":
		return slog.Level(-4)
	case "DEBUG":
		return slog.LevelDebug
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func init() {
	setLevel(parseLevel(os.Getenv("LOG_LEVEL")))
}

func setLevel(l slog.Level) {
	mu.Lock()
	defer mu.Unlock()
	logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l}))
}

// SetLevel overrides the level at runtime (e.g. by GceMetadataLogLevelChecker).
func SetLevel(l slog.Level) { setLevel(l) }

func Debug(format string, args ...any) { logger.Debug(fmt.Sprintf(format, args...)) }
func Info(format string, args ...any)  { logger.Info(fmt.Sprintf(format, args...)) }
func Warn(format string, args ...any)  { logger.Warn(fmt.Sprintf(format, args...)) }
func Error(format string, args ...any) { logger.Error(fmt.Sprintf(format, args...)) }
