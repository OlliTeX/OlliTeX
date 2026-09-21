// Package ologger is the 1:1 Go port of `libraries/logger` (the Overleaf
// logging-manager: a bunyan-backed singleton manager with custom serializers,
// a ring buffer, log-level checkers, and a GCP log-entry converter).
//
// Node source → Go files:
//
//	logging-manager.js   → ologger.go    (LoggingManager)
//	serializers.js       → serializers.go
//	log-level-checker.js → log_level_checker.go
//	gcp-manager.js       → gcp_manager.go
//	index.js             → this file (package doc / entry)
//
// The Node suite (31 tests) is the acceptance oracle. The manager consumes a
// narrow `Bunyan` seam (Node's `bunyan.createLogger`) so the Go tests inject a
// stub exactly like the Node sandboxed-module tests; a minimal real JSON-line
// logger is provided for standalone use.
package ologger

// bunyan log levels (Node: bunyan constants). The ring-buffer filter and the
// GCP severity mapping both key off these numeric values.
const (
	LevelTrace = 10
	LevelDebug = 20
	LevelInfo  = 30
	LevelWarn  = 40
	LevelError = 50
	LevelFatal = 60
)

// nameFromLevel mirrors `bunyan.nameFromLevel`.
func nameFromLevel(level int) string {
	switch level {
	case LevelTrace:
		return "trace"
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	case LevelFatal:
		return "fatal"
	}
	return ""
}

// levelFromName is the inverse (Node: bunyan.levelFromName).
func levelFromName(name string) int {
	switch name {
	case "trace":
		return LevelTrace
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn":
		return LevelWarn
	case "error":
		return LevelError
	case "fatal":
		return LevelFatal
	}
	return LevelInfo
}
