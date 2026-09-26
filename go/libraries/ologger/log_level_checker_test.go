package ologger

import (
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"
)

const (
	nowMS  = int64(10000)
	pastMS = nowMS - 1000
	futMS  = nowMS + 1000
)

// sharedState is mutable oracle state the injected read/fetch closures read.
type sharedState struct {
	mu         sync.Mutex
	val        string
	err        error
	lastURI    string
	lastHdrs   map[string]string
	fetchCalls int
}

func (s *sharedState) set(val string, err error) {
	s.mu.Lock()
	s.val, s.err = val, err
	s.mu.Unlock()
}

func (s *sharedState) get() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.val, s.err
}

func newFileChecker(t *testing.T, st *sharedState) Checker {
	t.Helper()
	logger := newStubLogger("myapp")
	c := NewFileLogLevelChecker(logger, "warn", func() int64 { return nowMS },
		func(path string) (string, error) {
			if path != TRACING_END_TIME_FILE {
				t.Fatalf("expected read of %q, got %q", TRACING_END_TIME_FILE, path)
			}
			return st.get()
		}, time.Hour)
	return c
}

func TestFileChecker_Empty(t *testing.T) {
	st := &sharedState{val: ""}
	c := newFileChecker(t, st)
	if err := c.CheckLogLevel(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// level called to the default
	logger := lastStubLogger(c)
	if len(logger.levelCalls) != 1 || logger.levelCalls[0] != "warn" {
		t.Fatalf("expected level set to default warn, got %v", logger.levelCalls)
	}
}

func TestFileChecker_ReadError(t *testing.T) {
	st := &sharedState{err: errors.New("Read error!")}
	c := newFileChecker(t, st)
	if err := c.CheckLogLevel(); err == nil {
		t.Fatalf("expected the read error to surface")
	}
	logger := lastStubLogger(c)
	if len(logger.levelCalls) != 1 || logger.levelCalls[0] != "warn" {
		t.Fatalf("expected level set to default on error, got %v", logger.levelCalls)
	}
}

func TestFileChecker_Future(t *testing.T) {
	st := &sharedState{val: itoa(futMS)}
	c := newFileChecker(t, st)
	if err := c.CheckLogLevel(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	logger := lastStubLogger(c)
	if len(logger.levelCalls) != 1 || logger.levelCalls[0] != "trace" {
		t.Fatalf("expected level trace for future end time, got %v", logger.levelCalls)
	}
}

func TestFileChecker_Past(t *testing.T) {
	st := &sharedState{val: itoa(pastMS)}
	c := newFileChecker(t, st)
	if err := c.CheckLogLevel(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	logger := lastStubLogger(c)
	if len(logger.levelCalls) != 1 || logger.levelCalls[0] != "warn" {
		t.Fatalf("expected level default for past end time, got %v", logger.levelCalls)
	}
}

func TestFileChecker_IntervalRechecks(t *testing.T) {
	st := &sharedState{val: ""}
	logger := newStubLogger("myapp")
	c := NewFileLogLevelChecker(logger, "warn", func() int64 { return nowMS },
		func(string) (string, error) { return st.get() }, 10*time.Millisecond)

	st.set("", nil)
	c.Start()
	defer c.Stop()

	// start() checks immediately -> default (empty).
	waitFor(t, func() bool { return lastLevel(logger) == "warn" }, 3000*time.Millisecond)

	// Flip to a future end time; a periodic check must raise to trace.
	st.set(itoa(nowMS+90000), nil)
	waitFor(t, func() bool { return lastLevel(logger) == "trace" }, 3000*time.Millisecond)

	// Flip back to the past; a periodic check must return to the default.
	st.set(itoa(pastMS), nil)
	waitFor(t, func() bool { return lastLevel(logger) == "warn" }, 3000*time.Millisecond)

	if logger.levelCalls[0] != "warn" {
		t.Fatalf("expected first interval check to set the default level")
	}
}

func TestGCEChecker_URIAndHeaders(t *testing.T) {
	st := &sharedState{val: ""}
	logger := newStubLogger("myapp")
	c := NewGCEMetadataLogLevelChecker(logger, "warn", func() int64 { return nowMS },
		func(uri string, hdrs map[string]string) (string, error) {
			st.mu.Lock()
			st.lastURI = uri
			st.lastHdrs = hdrs
			st.fetchCalls++
			st.mu.Unlock()
			return st.get()
		}, time.Hour)
	c.CheckLogLevel()
	st.mu.Lock()
	wantURI := "http://metadata.google.internal/computeMetadata/v1/project/attributes/myapp-setLogLevelEndTime"
	if st.lastURI != wantURI {
		t.Fatalf("expected GCE uri %q, got %q", wantURI, st.lastURI)
	}
	if st.lastHdrs["Metadata-Flavor"] != "Google" {
		t.Fatalf("expected Metadata-Flavor: Google header, got %#v", st.lastHdrs)
	}
	st.mu.Unlock()
}

func TestGCEChecker_Empty(t *testing.T) {
	st := &sharedState{val: ""}
	logger := newStubLogger("myapp")
	c := NewGCEMetadataLogLevelChecker(logger, "warn", func() int64 { return nowMS },
		func(string, map[string]string) (string, error) { return st.get() }, time.Hour)
	if err := c.CheckLogLevel(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lastLevel(logger) != "warn" {
		t.Fatalf("expected default level for empty GCE response, got %v", logger.levelCalls)
	}
}

func TestGCEChecker_Error(t *testing.T) {
	st := &sharedState{err: errors.New("Read error!")}
	logger := newStubLogger("myapp")
	c := NewGCEMetadataLogLevelChecker(logger, "warn", func() int64 { return nowMS },
		func(string, map[string]string) (string, error) { return st.get() }, time.Hour)
	if err := c.CheckLogLevel(); err == nil {
		t.Fatalf("expected fetch error")
	}
	if lastLevel(logger) != "warn" {
		t.Fatalf("expected default level on error, got %v", logger.levelCalls)
	}
}

func TestGCEChecker_Future(t *testing.T) {
	st := &sharedState{val: itoa(futMS)}
	logger := newStubLogger("myapp")
	c := NewGCEMetadataLogLevelChecker(logger, "warn", func() int64 { return nowMS },
		func(string, map[string]string) (string, error) { return st.get() }, time.Hour)
	if err := c.CheckLogLevel(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lastLevel(logger) != "trace" {
		t.Fatalf("expected trace for future end time, got %v", logger.levelCalls)
	}
}

func TestGCEChecker_Past(t *testing.T) {
	st := &sharedState{val: itoa(pastMS)}
	logger := newStubLogger("myapp")
	c := NewGCEMetadataLogLevelChecker(logger, "warn", func() int64 { return nowMS },
		func(string, map[string]string) (string, error) { return st.get() }, time.Hour)
	if err := c.CheckLogLevel(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lastLevel(logger) != "warn" {
		t.Fatalf("expected default for past end time, got %v", logger.levelCalls)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// --- test helpers -------------------------------------------------------------

// lastStubLogger pulls the stub logger out of a Checker via the embedded base.
func lastStubLogger(c Checker) *stubLogger {
	switch v := c.(type) {
	case *fileLogLevelChecker:
		return v.logger.(*stubLogger)
	case *gceLevelChecker:
		return v.logger.(*stubLogger)
	}
	return nil
}

func lastLevel(l *stubLogger) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.levelCalls) == 0 {
		return ""
	}
	return l.levelCalls[len(l.levelCalls)-1]
}

func waitFor(t *testing.T, cond func() bool, deadline time.Duration) {
	t.Helper()
	deadlineDur := deadline
	deadlineTime := time.Now().Add(deadlineDur)
	for time.Now().Before(deadlineTime) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", deadline)
}
