package ologger

import (
	"io"
	"os"
	"time"
)

// Entry is a bunyan log entry (a loose object; Node: a plain object with
// level/name/hostname/v/pid/msg/time + custom fields).
type Entry = map[string]any

// Serializer narrows whatever was logged under a named field to the value
// that is JSON-encoded into the log line (Node: the bunyan serializers).
type Serializer func(value any) any

// Logger is the narrow bunyan-logger surface the manager drives (Node's stub
// `bunyanLogger` exposes exactly these). The real implementation serializes
// entries to JSON lines.
type Logger interface {
	Debug(attributes any, message any, args ...any)
	Info(attributes any, message any, args ...any)
	Error(attributes any, message any, args ...any)
	Warn(attributes any, message any, args ...any)
	Fatal(attributes any, message any)
	Level(level string)
	AddStream(s StreamConfig)
	// Name is the logger name (Node: `logger.name` / `logger.fields.name`).
	Name() string
	// Serializers is the live serializer map the manager mutates via
	// AddSerializer (Node: `bunyanLogger.serializers`).
	Serializers() map[string]Serializer
}

// StreamConfig mirrors a bunyan stream entry. GKE/GCE flag the special output
// paths Node builds in `_getOutputStreamConfig`.
type StreamConfig struct {
	Level  string
	Type   string // "raw" | "" (the default console)
	Stream io.Writer
	GKE    bool
	GCE    bool
	// RingBuf is set when this stream is the ring buffer (Node passes the
	// RingBuffer object as `stream`).
	RingBuf *RingBuffer
	// GCE-specific (the gce LoggingBunyan stream carries these):
	LogName        string
	ServiceContext string
}

// RingBuffer mirrors `bunyan.RingBuffer({limit})` — the ring with its
// observable `.records` array.
type RingBuffer struct {
	Limit   int
	Records []Entry
}

// LoggerConfig mirrors the config object passed to `bunyan.createLogger`
// ({name, serializers, streams}).
type LoggerConfig struct {
	Name        string
	Serializers map[string]Serializer
	Streams     []StreamConfig
}

// Bunyan is the injected factory seam (Node's `bunyan` module: createLogger +
// RingBuffer). Tests inject a stub; NewJSONBunyan builds the real one.
type Bunyan interface {
	CreateLogger(cfg *LoggerConfig) Logger
	RingBuffer(limit int) *RingBuffer
}

// Checker mirrors the log-level-checker module surface the manager drives.
type Checker interface {
	Start()
	Stop()
	CheckLogLevel() error
}

// CheckerFactories is the `./log-level-checker` seam (Node: the two classes).
type CheckerFactories struct {
	File        func(logger Logger, defaultLevel string) Checker
	GCERequired func(logger Logger, defaultLevel string) Checker
}

// Options customizes a LoggingManager for a deployment or a test.
type Options struct {
	// Bunyan is the logger factory (default: a JSON-line logger on stdout).
	Bunyan Bunyan
	// Checkers injects the level-checker classes (default: the real ones).
	Checkers CheckerFactories
	// Env overrides os.Getenv (tests set LOG_LEVEL / NODE_ENV / ...).
	Env func(key string) string
	// Now is the current time in ms (default: time.Now().UnixMilli()); the
	// level checkers and their periodic loop use it.
	Now func() int64
	// LevelCheckInterval is the re-check period (Node: 60s). Shorten for tests.
	LevelCheckInterval time.Duration
	// Exit is the finalizer Node's `process.exit` maps to (tests stub it).
	Exit func(code int)
	// RegisterWarning / UnregisterWarning model `process.on/off('warning')`.
	RegisterWarning   func(handler func(any))
	UnregisterWarning func(handler func(any))
	// FetchString is the GCE metadata fetch (Node: @overleaf/fetch-utils
	// fetchString). Injectable so the checker is testable offline.
	FetchString func(uri string, headers map[string]string) (string, error)
	// ReadFile is the file-read the FileLogLevelChecker uses (Node:
	// fs.promises.readFile). Injectable for tests.
	ReadFile func(path string) (string, error)
	// FetchUtilsSetLogger / ValidationToolsSetLogger are the two `setLogger`
	// registrations initialize() performs (Node: @overleaf/fetch-utils +
	// @overleaf/validation-tools). Default: no-op (the host wires fetchutils).
	FetchUtilsSetLogger      func(warn func(info map[string]any, message string))
	ValidationToolsSetLogger func(warn func(info map[string]any, message string))
}

func (o *Options) withDefaults() {
	if o.Bunyan == nil {
		o.Bunyan = NewJSONBunyan(os.Stdout)
	}
	if o.Env == nil {
		o.Env = os.Getenv
	}
	if o.Now == nil {
		o.Now = func() int64 { return time.Now().UnixMilli() }
	}
	if o.LevelCheckInterval == 0 {
		o.LevelCheckInterval = 60 * time.Second
	}
	if o.Exit == nil {
		o.Exit = func(int) {}
	}
	if o.RegisterWarning == nil {
		o.RegisterWarning = func(func(any)) {}
	}
	if o.UnregisterWarning == nil {
		o.UnregisterWarning = func(func(any)) {}
	}
	if o.FetchString == nil {
		o.FetchString = func(string, map[string]string) (string, error) { return "", nil }
	}
	if o.ReadFile == nil {
		o.ReadFile = func(path string) (string, error) {
			b, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			return string(b), nil
		}
	}
	if o.FetchUtilsSetLogger == nil {
		o.FetchUtilsSetLogger = func(warn func(info map[string]any, message string)) {}
	}
	if o.ValidationToolsSetLogger == nil {
		o.ValidationToolsSetLogger = func(warn func(info map[string]any, message string)) {}
	}
	if o.Checkers.File == nil {
		o.Checkers.File = func(logger Logger, def string) Checker {
			return NewFileLogLevelChecker(logger, def, o.Now, o.ReadFile, o.LevelCheckInterval)
		}
	}
	if o.Checkers.GCERequired == nil {
		o.Checkers.GCERequired = func(logger Logger, def string) Checker {
			return NewGCEMetadataLogLevelChecker(logger, def, o.Now, o.FetchString, o.LevelCheckInterval)
		}
	}
}
