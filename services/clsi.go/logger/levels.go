package logger

import (
	"log/slog"
	"os"
	"strconv"
)

// Node bunyan logger levels used across the service: debug, info, warn, error.
// We honour LOG_LEVEL (env) exactly as settings.defaults.cjs does for bunyan:
// LOG_LEVEL unset -> info; "debug" | "trace" -> debug; numbers per bunyan map.

var Log *slog.Logger

func init() {
	Log = NewLogger(resolveLevel(os.Getenv("LOG_LEVEL")))
}

// resolveLevel maps a raw LOG_LEVEL var value to a slog.Level, honouring the
// bunyan conventions used by settings.defaults.cjs:
//   - ""            -> info
//   - "debug"|"trace" -> debug (finest)
//   - numeric string -> info for >=30, debug otherwise
//   - other/invalid -> info
func resolveLevel(lvl string) slog.Level {
	switch lvl {
	case "debug", "trace":
		return slog.Level(-4)
	case "":
		return slog.LevelInfo
	default:
		if n, err := strconv.Atoi(lvl); err == nil {
			if n >= 30 {
				return slog.LevelInfo
			}
			return slog.LevelDebug
		}
		return slog.LevelInfo
	}
}

// NewLogger builds a *slog.Logger writing JSON to stderr at the given level.
func NewLogger(l slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: l}))
}

// Debug mirrors bunyan debug(obj, msg).
func Debug(obj map[string]any, msg string) {
	Log.Debug(msg, "obj", obj)
}

// Info mirrors bunyan info(obj, msg).
func Info(obj map[string]any, msg string) {
	Log.Info(msg, "obj", obj)
}

// Warn mirrors bunyan warn(obj, msg).
func Warn(obj map[string]any, msg string) {
	Log.Warn(msg, "obj", obj)
}

// Error mirrors bunyan error(obj, msg).
func Error(obj map[string]any, msg string) {
	Log.Error(msg, "obj", obj)
}

// Err mirrors the node logger.err(...).
func Err(obj map[string]any, msg string) {
	Log.Error(msg, "obj", obj)
}
