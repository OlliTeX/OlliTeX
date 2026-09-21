package ologger

import (
	"fmt"
	"time"
)

// ExitDelay is Node's EXIT_DELAY - wait before exiting so final logs flush.
const ExitDelay = 500 * time.Millisecond

// LoggingManager is the Go port of the `LoggingManager` object in
// logging-manager.js (a stateful singleton; the host wires `Default`).
type LoggingManager struct {
	opts Options

	customSerializers map[string]Serializer

	isProduction bool
	isTest       bool
	defaultLevel string
	loggerName   string
	logger       Logger

	ringBuffer     *RingBuffer
	ringBufferSize int

	logLevelChecker Checker

	// warningHandler is the `process.on('warning')` callback (Node installs
	// one at import; RemoveWarningHandler detaches it).
	warningHandler        func(any)
	warningHandlerRemoved bool
}

// New builds a LoggingManager with the given seams. Node's module calls
// `LoggingManager.initialize('default')` at import; in Go the host wires the
// real fetchutils hooks + os.Exit, mirroring that bootstrap.
func New(opts Options) *LoggingManager {
	opts.withDefaults()
	return &LoggingManager{opts: opts, customSerializers: map[string]Serializer{}}
}

// Initialize mirrors `initialize(name, options)`. Returns the manager
// (Node: `return this`).
func (m *LoggingManager) Initialize(name string, options ...LoggerOption) *LoggingManager {
	env := m.opts.Env
	m.isProduction = env("NODE_ENV") == "production"
	m.isTest = env("NODE_ENV") == "test"

	level := env("LOG_LEVEL")
	if level == "" {
		switch {
		case m.isProduction:
			level = "info"
		case m.isTest:
			level = "fatal"
		default:
			level = "debug"
		}
	}
	m.defaultLevel = level
	m.loggerName = name

	serializers := map[string]Serializer{
		"err":   ErrSerializer,
		"error": ErrSerializer,
		"req":   ReqSerializer,
		"res":   ResSerializer,
	}
	for k, v := range m.customSerializers {
		serializers[k] = v
	}

	streams := []StreamConfig{m.getOutputStreamConfig()}
	for _, o := range options {
		if s := o.streams; s != nil {
			streams = *s
		}
	}

	m.logger = m.opts.Bunyan.CreateLogger(&LoggerConfig{
		Name:        name,
		Serializers: serializers,
		Streams:     streams,
	})

	m.setupRingBuffer()
	m.setupLogLevelChecker()

	// Node: setLogger(this) on @overleaf/fetch-utils and
	// setValidationToolsLogger(this).
	warn := func(info map[string]any, message string) { m.Warn(info, message) }
	m.opts.FetchUtilsSetLogger(warn)
	m.opts.ValidationToolsSetLogger(warn)

	return m
}

// AddSerializer mirrors addSerializer: registers on the current logger AND
// keeps it in customSerializers so a later Initialize re-applies it.
func (m *LoggingManager) AddSerializer(name string, serializer Serializer) {
	m.customSerializers[name] = serializer
	if m.logger != nil {
		m.logger.Serializers()[name] = serializer
	}
}

// --- the bunyan levels (Node: debug/info/error/err/warn/fatal) ----------------

func (m *LoggingManager) Debug(attributes any, message any, args ...any) {
	m.logger.Debug(attributes, message, args...)
}

func (m *LoggingManager) Info(attributes any, message any, args ...any) {
	m.logger.Info(attributes, message, args...)
}

func (m *LoggingManager) Warn(attributes any, message any, args ...any) {
	m.logger.Warn(attributes, message, args...)
}

// Error mirrors error(): when the ring buffer holds records, injects the
// records (excluding already-error entries, level !== 50) into
// `attributes.logBuffer` before delegating.
func (m *LoggingManager) Error(attributes any, message any, args ...any) {
	if m.ringBuffer != nil && m.ringBuffer.Records != nil {
		if attrs, ok := attributes.(map[string]any); ok {
			records := make([]Entry, 0, len(m.ringBuffer.Records))
			for _, r := range m.ringBuffer.Records {
				level, _ := r["level"].(int)
				if level != LevelError {
					records = append(records, r)
				}
			}
			attrs["logBuffer"] = records
			attributes = attrs
		}
	}
	m.logger.Error(attributes, message, args...)
}

// Err is the Node alias for Error.
func (m *LoggingManager) Err(attributes any, message any, args ...any) {
	m.Error(attributes, message, args...)
}

func (m *LoggingManager) Fatal(attributes any, message any) {
	m.logger.Fatal(attributes, message)
}

// Exit mirrors `exit(code)`: delay to flush, then exit (Node:
// `await setTimeout(500); process.exit(code)`). Both are injectable.
func (m *LoggingManager) Exit(code int) {
	time.Sleep(ExitDelay)
	m.opts.Exit(code)
}

// --- warning handler (Node: process.on('warning')) ------------------------------

// RegisterWarning installs the `process.on('warning')` handler (Node: wired at
// import to `warn({err}, 'Warning details')`).
func (m *LoggingManager) RegisterWarning() {
	m.warningHandler = func(err any) {
		m.Warn(map[string]any{"err": err}, "Warning details")
	}
	m.warningHandlerRemoved = false
	m.opts.RegisterWarning(m.warningHandler)
}

// RemoveWarningHandler mirrors removeWarningHandler: detach the process
// warning handler.
func (m *LoggingManager) RemoveWarningHandler() {
	if m.warningHandler == nil {
		return
	}
	m.opts.UnregisterWarning(m.warningHandler)
	m.warningHandlerRemoved = true
}

// --- internals -------------------------------------------------------------------

func (m *LoggingManager) setupRingBuffer() {
	size := parseRingBufferSize(m.opts.Env("LOG_RING_BUFFER_SIZE"))
	m.ringBufferSize = size
	if size > 0 {
		rb := m.opts.Bunyan.RingBuffer(size)
		m.ringBuffer = rb
		m.logger.AddStream(StreamConfig{Level: "trace", Type: "raw", RingBuf: rb})
	} else {
		m.ringBuffer = nil
	}
}

func (m *LoggingManager) setupLogLevelChecker() {
	source := lowerString(m.opts.Env("LOG_LEVEL_SOURCE"))
	if source == "" {
		source = "file"
	}

	// Node stops any previous checker before building a new one.
	if m.logLevelChecker != nil {
		m.logLevelChecker.Stop()
		m.logLevelChecker = nil
	}

	if !m.isProduction {
		return
	}

	switch source {
	case "file":
		m.logLevelChecker = m.opts.Checkers.File(m.logger, m.defaultLevel)
	case "gce_metadata":
		m.logLevelChecker = m.opts.Checkers.GCERequired(m.logger, m.defaultLevel)
	case "none":
		return
	default:
		fmt.Fprintf(osStderr, "Unrecognised log level source: %s\n", source)
	}
	if m.logLevelChecker != nil {
		m.logLevelChecker.Start()
	}
}

// getOutputStreamConfig mirrors `_getOutputStreamConfig()` (LOGGING_FORMAT:
// gke | gce | default) - returns the single default stream config.
func (m *LoggingManager) getOutputStreamConfig() StreamConfig {
	switch m.opts.Env("LOGGING_FORMAT") {
	case "gke":
		return StreamConfig{Level: m.defaultLevel, Type: "raw", GKE: true}
	case "gce":
		return StreamConfig{
			Level:          m.defaultLevel,
			GCE:            true,
			LogName:        m.loggerName,
			ServiceContext: m.loggerName,
		}
	default:
		return StreamConfig{Level: m.defaultLevel}
	}
}

// parseRingBufferSize mirrors `parseInt(process.env.LOG_RING_BUFFER_SIZE) || 0`.
func parseRingBufferSize(s string) int {
	return int(parseInt10(s))
}

func lowerString(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		out[i] = b
	}
	return string(out)
}

// LoggerOption carries optional initialize() arguments (Node: the `options`
// object with an optional `streams` override).
type LoggerOption struct{ streams *[]StreamConfig }

// WithStreams overrides the output stream set (Node: `options.streams`).
func WithStreams(streams []StreamConfig) LoggerOption {
	return LoggerOption{streams: &streams}
}
