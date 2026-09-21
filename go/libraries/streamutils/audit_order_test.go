package streamutils

// audit_order_test.go — owner-audit regression pins.
//
// Node spec (libraries/stream-utils/index.js, IncrementalResponse):
//
//	this.#timeout = setTimeout(() => {
//	  this.#logger.warn({ ...this.#info, timeout }, `${this.#label}: aborting`)
//	  this.sendUpdate(`error: ${label}: aborting after ${...}`)
//	  this.#ac.abort()
//	}, timeout)
//
// i.e. WARN FIRST, then the progress WRITE, then abort. The pre-audit Go
// port had the observable order reversed (cancel → write → warn).

import (
	"sync"
	"testing"
	"time"
)

// sharedLog stamps every observable side effect (logger warn + response
// write) with a monotonic sequence so the RELATIVE ORDER across the two
// sinks is provable.
type sharedLog struct {
	mu     sync.Mutex
	events []string
}

func (s *sharedLog) stamp(kind string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, kind)
}

func (s *sharedLog) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.events))
	copy(out, s.events)
	return out
}

func (s *sharedLog) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

type orderLogger struct{ log *sharedLog }

func (o *orderLogger) Warn(fields map[string]any, msg string) { o.log.stamp("warn") }
func (o *orderLogger) Err(fields map[string]any, msg string)  { o.log.stamp("err") }

type orderRes struct {
	log  *sharedLog
	done bool
}

func (r *orderRes) Write(p []byte) (int, error) { r.log.stamp("write"); return len(p), nil }
func (r *orderRes) Finish() error               { r.done = true; return nil }

func TestAuditTimeoutOrderWarnBeforeWrite(t *testing.T) {
	log := &sharedLog{}
	ir := NewIncrementalResponse(&orderRes{log: log}, 25*time.Millisecond, "ord", map[string]any{"p": 1}, &orderLogger{log: log})
	defer ir.End()
	waitUntil(func() bool { return log.count() >= 1 }, 500*time.Millisecond, t)
	evs := log.snapshot()
	if len(evs) < 1 || evs[0] != "warn" {
		t.Fatalf("Node order: warn first, got %v (pre-audit Go was write → warn)", evs)
	}
	// The write follows the warn in the same callback.
	waitUntil(func() bool { return log.count() >= 2 }, 500*time.Millisecond, t)
	evs = log.snapshot()
	if evs[1] != "write" {
		t.Fatalf("Node order: warn → write, got %v", evs)
	}
	// The abort context is live (Node: #ac.abort() ran last).
	if ir.Signal().Err() == nil {
		t.Fatalf("timeout must abort the signal")
	}
}

// TestAuditGetContentsAlias — Node WritableBuffer exposes BOTH getContents()
// and contents(); the Go port must too (the pre-audit package lacked one).
func TestAuditGetContentsAlias(t *testing.T) {
	w := NewWritableBuffer()
	w.Write([]byte("he"))
	w.Write([]byte("llo"))
	if string(w.GetContents()) != string(w.Contents()) || string(w.Contents()) != "hello" {
		t.Fatalf("GetContents alias: %q / %q", w.GetContents(), w.Contents())
	}
}
