package streamutils

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// Node suite parity (libraries/stream-utils/test/unit/*.js): every `it()` is
// re-expressed here with byte-for-byte pinned strings where Node pins them
// ("exceeded stream size limit of %d: %d", "stream timed out", logger call
// shape, "helloworld").

// --- WritableBuffer (Node WritableBufferTests.js) ---

func TestWritableBufferStoresAllData(t *testing.T) {
	// Node: write 'hello'; write 'world'; end(); contents() === 'helloworld'
	b := NewWritableBuffer()
	mustWrite(t, b, "hello")
	mustWrite(t, b, "world")
	b.Close()
	if got := string(b.Contents()); got != "helloworld" {
		t.Fatalf("contents %q want helloworld", got)
	}
}

func TestWritableBufferSize(t *testing.T) {
	// Node: same stream → size() === 10
	b := NewWritableBuffer()
	mustWrite(t, b, "hello")
	mustWrite(t, b, "world")
	b.Close()
	if b.Size() != 10 {
		t.Fatalf("size %d want 10", b.Size())
	}
}

func TestWritableBufferWriteAfterEnd(t *testing.T) {
	// Go pin: a write after Close errors (Node `end()` does not reject
	// subsequent .write() in Writable; documented divergence).
	b := NewWritableBuffer()
	b.Close()
	if _, err := b.Write([]byte("y")); err == nil {
		t.Fatalf("write after end must error")
	}
}

// --- ReadableString (Node ReadableStringTests.js) ---

func TestReadableStringEmitsTheString(t *testing.T) {
	// Node: data events accumulate to 'hello world', then end.
	if got := readAll(t, NewReadableString("hello world")); got != "hello world" {
		t.Fatalf("data %q want hello world", got)
	}
	// Go pin: a fresh empty stream reads io.EOF immediately (Node emits no
	// data events and then end).
	if _, err := NewReadableString("").Read(make([]byte, 4)); err != io.EOF {
		t.Fatalf("empty stream must EOF: %v", err)
	}
}

// --- LimitedStream (Node LimitedStreamTests.js) ---

func TestLimitedStreamErrorsPastLimit(t *testing.T) {
	// Node: maxSize = 10; write(Buffer.alloc(11)) → 'error' event,
	// err instanceof SizeExceededError. Message pinned:
	// "exceeded stream size limit of 10: 11".
	l := NewLimitedReader(newTestReader("ABCDEFGHIJK"), 10)
	_, err := l.Read(make([]byte, 4100))
	var sce *SizeExceededError
	if !errors.As(err, &sce) {
		t.Fatalf("want *SizeExceededError, got %v", err)
	}
	if got := sce.Error(); got != "exceeded stream size limit of 10: 11" {
		t.Fatalf("message %q", got)
	}
	// Re-reads keep erroring.
	if _, err2 := l.Read(make([]byte, 4)); err2 == nil {
		t.Fatalf("post-cap read must keep erroring")
	}
}

func TestLimitedStreamPassesThroughUnderLimit(t *testing.T) {
	// Node: maxSize = 15; write 'hello', ' world'; end → data 'hello world',
	// no error event.
	if got := readAll(t, NewLimitedReader(newTestReader("hello world"), 15)); got != "hello world" {
		t.Fatalf("data %q want hello world", got)
	}
	// Exactly at the limit: also passes (Node: `size > maxSize` — 10 is NOT
	// greater than 10).
	if got := readAll(t, NewLimitedReader(newTestReader("ABCDEFGHIJ"), 10)); got != "ABCDEFGHIJ" {
		t.Fatalf("at-limit data %q", got)
	}
}

// --- TimeoutStream (Node TimeoutStreamTests.js) ---

func TestTimeoutStreamAbortsAfterTimeout(t *testing.T) {
	// Node: 10ms timeout, nothing ends the stream → error event,
	// AbortError instance.
	tr := NewTimeoutReader(newTestReader(""), 10*time.Millisecond)
	defer tr.Stop()
	waitUntil(func() bool { return tr.abortedPolling() }, 150*time.Millisecond, t)
	_, err := tr.Read(make([]byte, 4))
	var ae *AbortError
	if !errors.As(err, &ae) {
		t.Fatalf("want *AbortError after timeout, got %v", err)
	}
	if got := ae.Error(); got != "stream timed out" {
		t.Fatalf("message %q want 'stream timed out'", got)
	}
}

func TestTimeoutStreamPassesThroughBeforeTimeout(t *testing.T) {
	// Node: 100ms timeout; end() after 1ms → no error event.
	src := newTestReader("quick")
	tr := NewTimeoutReader(src, 100*time.Millisecond)
	if got := readAll(t, tr); got != "quick" {
		t.Fatalf("data %q want quick", got)
	}
	tr.Stop() // Node: _final clearTimeout
	// A Stop'd timer must NOT fire even after its deadline would have
	// passed — sleep past it and verify the flag is still clear (the
	// Node "does not time out" pin).
	time.Sleep(110 * time.Millisecond)
	tr.mu.Lock()
	aborted := tr.aborted
	tr.mu.Unlock()
	if aborted {
		t.Fatalf("Stop() must cancel the timer (Node clearTimeout on end)")
	}
}

// --- LoggerStream (Node LoggerStreamTests.js) ---

func TestLoggerStreamLogsOnceAtOverflowAndAgainAtFlush(t *testing.T) {
	// Node: maxSize = 10; write 10 bytes, write 1 byte (size 11 > 10) →
	// logs [11, undefined]; end() → _flush re-logs [11, true].
	//
	// Go pin: the same two calls with (11, false) then (11, true).
	type call struct {
		size    int
		isFlush bool
	}
	var calls []call
	l := NewLoggerReader(newTestReader("ABCDEFGHIJK"), 10, func(size int, isFlush bool) {
		calls = append(calls, call{size, isFlush})
	})
	readAll(t, l)
	l.Close()
	want := []call{{11, false}, {11, true}}
	if len(calls) != len(want) {
		t.Fatalf("calls %v want %v", calls, want)
	}
	for i, c := range calls {
		if c != want[i] {
			t.Fatalf("call %d got %+v want %+v (Node pins [11, undefined], [11, true])", i, c, want[i])
		}
	}
}

func TestLoggerStreamDoesNotLogUnderLimit(t *testing.T) {
	// Node: writes exactly the limit (10 bytes at maxSize 10) → loggedSizes
	// is [] at finish (no overflow).
	var calls []int
	l := NewLoggerReader(newTestReader("ABCDEFGHIJ"), 10, func(size int, isFlush bool) {
		calls = append(calls, size)
	})
	readAll(t, l)
	l.Close()
	if len(calls) != 0 {
		t.Fatalf("logger called %d times, want 0", len(calls))
	}
	// Close is idempotent in call-count (Node _flush does not run again).
	l.Close()
	if len(calls) != 0 {
		t.Fatalf("re-Close must not re-log")
	}
}

// --- MeteredStream ---

func TestMeteredReaderCountsEachChunk(t *testing.T) {
	// Node: `metrics.count(metric, chunk.byteLength, 1, labels)` per chunk.
	var total int64
	var calls int
	m := NewMeteredReader(newTestReader("hello world"), fakeMeter{fn: func(metric string, v int64, c int, labels map[string]string) {
		total += v
		calls++
	}}, "stream.bytes", map[string]string{"service": "test"})
	if got := readAll(t, m); got != "hello world" {
		t.Fatalf("data %q", got)
	}
	if total != 11 {
		t.Fatalf("counted bytes %d want 11", total)
	}
	if calls == 0 {
		t.Fatalf("no count calls")
	}
}

// --- IncrementalResponse (no Node unit test for this class; documented:
// the class is exercised only via services. Go pins its observable surface.) ---

func TestIncrementalResponseTimeoutAborts(t *testing.T) {
	logs := newLogRecorder()
	res := newFakeRes()
	ir := NewIncrementalResponse(res, 30*time.Millisecond, "mylabel", map[string]any{"projectId": 42}, logs)
	defer ir.End()
	// Node order (stream-utils index.js: warn → update → abort signal):
	// the written update is the LAST observable of the three, so gating on
	// it also guarantees the warning (wait-for-warn is the racy direction).
	waitUntil(func() bool { return res.written() != "" }, 500*time.Millisecond, t)
	// Pin: the abort pushed the exact update line.
	if got := res.written(); got != "error: mylabel: aborting after 30ms\n" {
		t.Fatalf("abort update %q", got)
	}
	// Pin (Go std-dev): Signal() IS the abort context (Node: AbortSignal).
	// Pin: the warn log has {info…, timeout: <ms>}.
	var warn fieldsAndMsg
	for _, l := range logs.snapshot() {
		if l.msg == "mylabel: aborting" {
			warn = l
		}
	}
	if warn.msg == "" {
		t.Fatalf("missing abort warning: %+v", logs.snapshot())
	}
	if warn.fields["projectId"] != 42 || warn.fields["timeout"] != int64(30) {
		t.Fatalf("abort fields %v (want projectId:42 timeout:30)", warn.fields)
	}
}

func TestIncrementalResponseFailLogsAndEnds(t *testing.T) {
	logs := newLogRecorder()
	res := newFakeRes()
	ir := NewIncrementalResponse(res, time.Hour, "mylabel", map[string]any{"a": 1}, logs)
	ir.Fail(errors.New("boom"))
	// Pin: logger.err({err, …info}, "mylabel: error") + update "error: mylabel".
	if len(logs.errors) != 1 || logs.errors[0].msg != "mylabel: error" {
		t.Fatalf("errs %+v", logs.errors)
	}
	if got := res.written(); got != "error: mylabel\n" {
		t.Fatalf("fail update %q", got)
	}
	if !res.finished {
		t.Fatalf("response must be finished by Fail")
	}
}

func TestIncrementalResponseFailWhileAbortedSkipsUpdate(t *testing.T) {
	// Node: fail() when #ac.signal.aborted → skip the log+update, just end().
	logs := newLogRecorder()
	res := newFakeRes()
	ir := NewIncrementalResponse(res, 20*time.Millisecond, "l", nil, logs)
	// Pin the timeout's own side effects first (Node: fail-after-abort is
	// tested AFTER the abort actually happened; the race is that the
	// timeout's "aborting" line is still in flight when Fail arrives).
	defer ir.End()
	waitUntil(func() bool {
		return res.written() == "error: l: aborting after 20ms\n"
	}, 300*time.Millisecond, t)
	ir.Fail(errors.New("late"))
	// No second "error:" update line (Node skips sendUpdate when aborted),
	// and Finish still ran (res.end()).
	if got := res.written(); got != "error: l: aborting after 20ms\n" {
		t.Fatalf("only the timeout line expected, got %q", got)
	}
	if !res.finished {
		t.Fatalf("Fail must end the response (Go: Done gate; Node: unconditional end)")
	}
}

func TestIncrementalResponseSendUpdateWriteFails(t *testing.T) {
	logs := newLogRecorder()
	res := newFakeRes()
	ir := NewIncrementalResponse(res, time.Hour, "l", map[string]any{"info": "x"}, logs)
	defer ir.End()
	res.failNext = true
	ir.SendUpdate("progress: 50%")
	// Pin: warn "l: failed to send progress update" with {err, …info}.
	if len(logs.warnings) != 1 || logs.warnings[0].msg != "l: failed to send progress update" {
		t.Fatalf("warns %+v", logs.warnings)
	}
	// Pin: the failed write aborted (Node: #ac.abort() on write error).
	if ir.ctx.Err() == nil {
		t.Fatalf("ctx must be aborted after a failed write")
	}
}

func TestHumanReadableTimeout(t *testing.T) {
	pins := []struct {
		in   time.Duration
		want string
	}{
		{123_450 * time.Millisecond, "2min3s450ms"},
		{90_000 * time.Millisecond, "1min30s"},
		{800 * time.Millisecond, "800ms"},
		{61_000 * time.Millisecond, "1min1s"},
	}
	for _, c := range pins {
		if got := humanReadableTimeout(c.in); got != c.want {
			t.Fatalf("human(%v) got %q want %q", c.in, got, c.want)
		}
	}
	// Zero: "" (Node: empty string with no units).
	if got := humanReadableTimeout(0); got != "" {
		t.Fatalf("zero %q want \"\"", got)
	}
}

// --- test fixtures ---

type testReader struct {
	pos int
	src []byte
}

func newTestReader(s string) io.Reader { return &testReader{src: []byte(s)} }

func (r *testReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.src) {
		return 0, io.EOF
	}
	n := copy(p, r.src[r.pos:])
	r.pos += n
	return n, nil
}

func mustWrite(t *testing.T, w io.Writer, s string) {
	t.Helper()
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatalf("write %q: %v", s, err)
	}
}

func readAll(t *testing.T, r io.Reader) string {
	t.Helper()
	var b bytes.Buffer
	if _, err := io.Copy(&b, r); err != nil {
		t.Fatalf("readall: %v", err)
	}
	return b.String()
}

func (tr *TimeoutReader) abortedPolling() bool {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.aborted
}

type fakeMeter struct {
	fn func(string, int64, int, map[string]string)
}

func (f fakeMeter) Count(metric string, v int64, c int, l map[string]string) {
	f.fn(metric, v, c, l)
}

type fieldsAndMsg struct {
	fields map[string]any
	msg    string
}

type logRecorder struct {
	mu       sync.Mutex
	warnings []fieldsAndMsg
	errors   []fieldsAndMsg
}

func newLogRecorder() *logRecorder { return &logRecorder{} }

func (l *logRecorder) Warn(fields map[string]any, msg string) {
	l.mu.Lock()
	l.warnings = append(l.warnings, fieldsAndMsg{fields, msg})
	l.mu.Unlock()
}

func (l *logRecorder) Err(fields map[string]any, msg string) {
	l.mu.Lock()
	l.errors = append(l.errors, fieldsAndMsg{fields, msg})
	l.mu.Unlock()
}

func (l *logRecorder) warningCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.warnings)
}

func (l *logRecorder) snapshot() []fieldsAndMsg {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]fieldsAndMsg, len(l.warnings)+len(l.errors))
	copy(out, l.warnings)
	copy(out[len(l.warnings):], l.errors)
	return out
}

type fakeRes struct {
	buf      bytes.Buffer
	finished bool
	failNext bool
}

func newFakeRes() *fakeRes { return &fakeRes{} }

func (r *fakeRes) Write(p []byte) (int, error) {
	if r.failNext {
		return 0, errors.New("write failed")
	}
	return r.buf.Write(p)
}

func (r *fakeRes) Finish() error {
	r.finished = true
	return nil
}

func (r *fakeRes) written() string { return r.buf.String() }

func waitUntil(cond func() bool, timeout time.Duration, t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

// Note: `ir.ctx` is an unexported field; tests live in the same package so
// direct access is permitted.
