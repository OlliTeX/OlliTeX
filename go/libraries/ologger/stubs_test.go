package ologger

import (
	"reflect"
	"sync"
)

// call records one (attributes, message, args...) call on a stub level.
type call struct {
	attributes any
	message    any
	args       []any
}

// stubLogger implements Logger, capturing calls per level for assertions.
type stubLogger struct {
	mu          sync.Mutex
	name        string
	serializers map[string]Serializer
	streams     []StreamConfig
	levelCalls  []string

	debugCalls []call
	infoCalls  []call
	errorCalls []call
	warnCalls  []call
	fatalCalls []call
}

func newStubLogger(name string) *stubLogger {
	return &stubLogger{name: name, serializers: map[string]Serializer{}}
}

func (l *stubLogger) Debug(a any, m any, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.debugCalls = append(l.debugCalls, call{a, m, args})
}
func (l *stubLogger) Info(a any, m any, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.infoCalls = append(l.infoCalls, call{a, m, args})
}
func (l *stubLogger) Error(a any, m any, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errorCalls = append(l.errorCalls, call{a, m, args})
}
func (l *stubLogger) Warn(a any, m any, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warnCalls = append(l.warnCalls, call{a, m, args})
}
func (l *stubLogger) Fatal(a any, m any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fatalCalls = append(l.fatalCalls, call{a, m, nil})
}
func (l *stubLogger) Level(level string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.levelCalls = append(l.levelCalls, level)
}
func (l *stubLogger) AddStream(s StreamConfig) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.streams = append(l.streams, s)
}
func (l *stubLogger) Name() string { return l.name }
func (l *stubLogger) Serializers() map[string]Serializer {
	if l.serializers == nil {
		l.serializers = map[string]Serializer{}
	}
	return l.serializers
}

type callList []call

func (c callList) last() (call, bool) {
	if len(c) == 0 {
		return call{}, false
	}
	return c[len(c)-1], true
}

func (c callList) len() int { return len(c) }

// stubBunyan implements Bunyan, capturing the LoggerConfig passed to
// CreateLogger (the Node oracle reads `createLogger.firstCall.args[0]`).
type stubBunyan struct {
	logger          *stubLogger
	configs         []*LoggerConfig
	ringBufferCalls []int
	ringBuffers     map[int]*RingBuffer
}

func newStubBunyan(logger *stubLogger) *stubBunyan {
	return &stubBunyan{
		logger:      logger,
		ringBuffers: map[int]*RingBuffer{},
	}
}

func (b *stubBunyan) CreateLogger(cfg *LoggerConfig) Logger {
	b.configs = append(b.configs, cfg)
	return b.logger
}

func (b *stubBunyan) RingBuffer(limit int) *RingBuffer {
	b.ringBufferCalls = append(b.ringBufferCalls, limit)
	rb := &RingBuffer{Limit: limit}
	b.ringBuffers[limit] = rb
	return rb
}

// firstConfig mirrors `createLogger.firstCall.args[0]`.
func (b *stubBunyan) firstConfig() (*LoggerConfig, bool) {
	if len(b.configs) == 0 {
		return nil, false
	}
	return b.configs[0], true
}

func (b *stubBunyan) lastConfig() (*LoggerConfig, bool) {
	if len(b.configs) == 0 {
		return nil, false
	}
	return b.configs[len(b.configs)-1], true
}

func (b *stubBunyan) resetCreateLogger() { b.configs = b.configs[:0] }

func deepEqualAny(a, b any) bool { return reflect.DeepEqual(a, b) }
