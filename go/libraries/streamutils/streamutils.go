// Package streamutils is the 1:1 Go port of `libraries/stream-utils`
// (npm `@overleaf/stream-utils`).
//
// Node spec (faithfully reproduced; divergences pinned in
// go/libraries/HANDOFF.md §Divergences and exercised by the tests):
//
//	class WritableBuffer extends Writable         // buffers chunks; contents()
//	class ReadableString extends Readable          // one data event for the string
//	class LimitedStream extends Transform          // SizeExceededError past maxSize
//	class TimeoutStream extends PassThrough        // AbortError after `timeout` ms
//	class LoggerStream extends Transform           // log once at limit + re-log at flush
//	class MeteredStream extends Transform          // metrics.count(len, 1, labels) per chunk
//	class IncrementalResponse                      // res + timeout + label + info logger
//
// Go shape (Node `Transform`/`PassThrough` are bidirectional; Go request-body
// pipelines read, so each stream ports to an `io.Reader` wrapper over its
// source):
//
//   - LimitedStream: reads until the cumulative bytes exceed `maxSize`; the
//     read that crosses the cap returns `*SizeExceededError` with the exact
//     Node message `exceeded stream size limit of <maxSize>: <size>`.
//   - TimeoutStream: passes `src` through until a timer fires; afterwards
//     every read returns `*AbortError` with the exact Node message
//     `stream timed out`. `Stop` cancels the pending timer
//     (Node: `_final`'s `clearTimeout`).
//   - LoggerStream: at the first overflow it logs `(size, isFlush=false)`
//     ONCE; `Close` (Node `_flush`) logs `(size, isFlush=true)` a second
//     time iff overflow happened — the double call the Node suite pins.
//   - MeteredStream: each read chunk is counted via the narrow `Meter`
//     interface (the ometrics port, LIB-14, implements it).
//   - IncrementalResponse: Node's `AbortController` → `context.Context`
//     (Go idiom, documented); Node's `res` → the narrow `ProgressResponse`
//     interface.
//
// WritableBuffer / ReadableString are direct ports (no wrapper needed).
package streamutils

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

// --- WritableBuffer (Node `class WritableBuffer extends Writable`) ---

// WritableBuffer accumulates written bytes (Node: `contents()` / `size()`).
type WritableBuffer struct {
	mu     sync.Mutex
	chunks [][]byte
	size   int
	closed bool
}

// NewWritableBuffer: default `@overleaf/stream-utils` constructor.
func NewWritableBuffer() *WritableBuffer { return &WritableBuffer{} }

func (w *WritableBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, fmt.Errorf("stream ended")
	}
	w.chunks = append(w.chunks, append([]byte(nil), p...))
	w.size += len(p)
	return len(p), nil
}

// Close ports Node `end()`: after end, further writes error.
func (w *WritableBuffer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return nil
}

// Size mirrors Node `size()`.
func (w *WritableBuffer) Size() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.size
}

// GetContents mirrors Node `getContents()` (an alias of `contents()` in the
// Node API; both are exported there).
func (w *WritableBuffer) GetContents() []byte { return w.Contents() }

// Contents mirrors Node `contents()` — the concatenated buffer.
func (w *WritableBuffer) Contents() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]byte, 0, w.size)
	for _, c := range w.chunks {
		out = append(out, c...)
	}
	return out
}

// --- ReadableString (Node `class ReadableString extends Readable`) ---

// ReadableString yields exactly its string's bytes, then EOF (the Node
// one-`data`-event pin: the full string, then end).
type ReadableString struct {
	mu  sync.Mutex
	src []byte
	pos int
}

// NewReadableString: `new ReadableString('hello world')`.
func NewReadableString(s string) *ReadableString { return &ReadableString{src: []byte(s)} }

func (r *ReadableString) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pos >= len(r.src) {
		return 0, io.EOF
	}
	n := copy(p, r.src[r.pos:])
	r.pos += n
	return n, nil
}

// --- LimitedStream (Node) → LimitedReader ---

// SizeExceededError ports `class SizeExceededError extends Error {}`. The
// message is pinned byte-for-byte by the Node test:
//
//	"exceeded stream size limit of <maxSize>: <size-just-exceeded>"
type SizeExceededError struct{ msg string }

func (e *SizeExceededError) Error() string { return e.msg }

// NewLimitedReader ports `LimitedStream` (Node `Transform`): passes `src`'s
// bytes until the cumulative bytes exceed `maxSize`; the read that crosses
// the cap (and all subsequent reads) return `*SizeExceededError`.
func NewLimitedReader(src io.Reader, maxSize int) *LimitedReader {
	return &LimitedReader{src: src, maxSize: maxSize}
}

type LimitedReader struct {
	mu      sync.Mutex
	src     io.Reader
	maxSize int
	size    int
	erred   *SizeExceededError
}

func (l *LimitedReader) Read(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.erred != nil {
		return 0, l.erred
	}
	n, err := l.src.Read(p)
	l.size += n
	if l.size > l.maxSize {
		l.erred = &SizeExceededError{
			msg: fmt.Sprintf("exceeded stream size limit of %d: %d", l.maxSize, l.size),
		}
		return 0, l.erred
	}
	return n, err
}

// --- TimeoutStream (Node) → TimeoutReader ---

// AbortError ports `class AbortError extends Error {}`; the timeout error
// carries the pinned message `stream timed out`.
type AbortError struct{ msg string }

func (e *AbortError) Error() string { return e.msg }

// TimeoutReader ports `TimeoutStream` (Node `PassThrough` + `setTimeout`):
// passes `src` through until its timer fires; afterwards every read returns
// `*AbortError` ("stream timed out").
type TimeoutReader struct {
	src     io.Reader
	timer   *time.Timer
	mu      sync.Mutex
	aborted bool
}

// NewTimeoutReader: timeout in the Node `setTimeout` sense (any
// time.Duration; the suites use small values).
func NewTimeoutReader(src io.Reader, timeout time.Duration) *TimeoutReader {
	t := &TimeoutReader{src: src}
	t.timer = time.AfterFunc(timeout, func() {
		t.mu.Lock()
		t.aborted = true
		t.mu.Unlock()
	})
	return t
}

func (t *TimeoutReader) Read(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.aborted {
		return 0, &AbortError{msg: "stream timed out"}
	}
	return t.src.Read(p)
}

// Stop cancels the pending timeout (Node: `_final`'s `clearTimeout`).
func (t *TimeoutReader) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.timer != nil {
		t.timer.Stop()
	}
}

// --- LoggerStream (Node) → LoggerReader ---

// LoggerReader ports `LoggerStream`: cumulative bytes over `src` are
// counted; at the first overflow past `maxSize` the logger is called exactly
// once with `(size, isFlush=false)`; `Close` (Node `_flush`) calls the logger
// a SECOND time with `(size, isFlush=true)` iff the stream size exceeds the
// limit — the double call the Node suite pins (`[11, undefined], [11, true]`
// in Node; Go's `isFlush` booleans pin the same call pattern).
type LoggerReader struct {
	src     io.Reader
	maxSize int
	logger  func(size int, isFlush bool)
	mu      sync.Mutex
	size    int
	logged  bool
	flushed bool
}

// NewLoggerReader: Node `new LoggerStream(maxSize, fn, options)`.
func NewLoggerReader(src io.Reader, maxSize int, logger func(size int, isFlush bool)) *LoggerReader {
	return &LoggerReader{src: src, maxSize: maxSize, logger: logger}
}

func (l *LoggerReader) Read(p []byte) (int, error) {
	n, err := l.src.Read(p)
	if n > 0 {
		l.mu.Lock()
		l.size += n
		if !l.logged && l.size > l.maxSize {
			l.logged = true
			l.mu.Unlock()
			l.logger(l.size, false)
			l.mu.Lock()
		}
		l.mu.Unlock()
	}
	return n, err
}

// Close ports Node `_flush`: re-log iff the stream exceeded the limit.
func (l *LoggerReader) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.flushed && l.size > l.maxSize {
		l.flushed = true
		l.mu.Unlock()
		l.logger(l.size, true)
		l.mu.Lock()
	}
	return nil
}

// --- MeteredStream (Node) → MeteredReader ---

// Meter narrows ometrics' `metrics.count(metricName, value, count, labels)`
// (the ometrics port, LIB-14, will implement it).
type Meter interface {
	Count(metricName string, value int64, count int, labels map[string]string)
}

// MeteredReader ports `MeteredStream` (Node `metrics.count(metric, chunk
// byteLength, 1, labels)` per chunk).
type MeteredReader struct {
	src    io.Reader
	meter  Meter
	metric string
	labels map[string]string
}

// NewMeteredReader: Node `new MeteredStream(Metrics, metric, labels)`.
func NewMeteredReader(src io.Reader, meter Meter, metric string, labels map[string]string) *MeteredReader {
	return &MeteredReader{src: src, meter: meter, metric: metric, labels: labels}
}

func (m *MeteredReader) Read(p []byte) (int, error) {
	n, err := m.src.Read(p)
	if n > 0 {
		m.meter.Count(m.metric, int64(n), 1, m.labels)
	}
	return n, err
}

// --- IncrementalResponse (Node) — Go context-based ---

// Logger is the minimal ologger surface this package needs; the o-logger
// port (LIB-13) provides the concrete implementation at the service
// boundary. Shapes mirror Node `logger.warn(fields, msg)` /
// `logger.err(fields, msg)`.
type Logger interface {
	Warn(fields map[string]any, msg string)
	Err(fields map[string]any, msg string)
}

// ProgressResponse is the narrow `res` surface the Node
// `IncrementalResponse#res` needs (an SSE response adapter in the Go
// services implements it).
type ProgressResponse interface {
	io.Writer
	Finish() error // Node `res.end()`
}

// IncrementalResponse ports the Node class: `#res → Resp`,
// `#timeout → Timeout`, `#label → Label`, `#info → Info`,
// `#logger → Logger`, `#ac` → Go context (divergence documented),
// `#timeout handle → timer`.
type IncrementalResponse struct {
	Resp    ProgressResponse
	Timeout time.Duration
	Label   string
	Info    map[string]any
	Logger  Logger

	ctx    context.Context
	cancel context.CancelFunc
	timer  *time.Timer
	mu     sync.Mutex
	done   bool
}

// NewIncrementalResponse arms the Node `setTimeout` timer.
// `End`/`Fail`/the timeout stops it.
func NewIncrementalResponse(res ProgressResponse, timeout time.Duration, label string, info map[string]any, logger Logger) *IncrementalResponse {
	ir := &IncrementalResponse{Resp: res, Timeout: timeout, Label: label, Info: info, Logger: logger}
	ir.ctx, ir.cancel = context.WithCancel(context.Background())
	ir.timer = time.AfterFunc(timeout, ir.onTimeout)
	return ir
}

func (ir *IncrementalResponse) onTimeout() {
	ir.mu.Lock()
	if ir.done {
		ir.mu.Unlock()
		return
	}
	ir.mu.Unlock()
	// Node order (IncrementalResponse's setTimeout body): warn first, then
	// sendUpdate, then abort — pinned here (the original port had it
	// reversed: cancel → write → warn).
	ir.Logger.Warn(ir.infoWith("timeout", ir.Timeout.Milliseconds()), ir.Label+": aborting")
	ir.SendUpdate(fmt.Sprintf("error: %s: aborting after %s", ir.Label, humanReadableTimeout(ir.Timeout)))
	ir.cancel()
}

func (ir *IncrementalResponse) infoWith(extra string, extraVal any) map[string]any {
	fields := map[string]any{}
	for k, v := range ir.Info {
		fields[k] = v
	}
	fields[extra] = extraVal
	return fields
}

// Signal: Node `signal()` — the Go abort context.
func (ir *IncrementalResponse) Signal() context.Context { return ir.ctx }

// End ports Node `end()`: abort, cancel the timer, end the response.
func (ir *IncrementalResponse) End() {
	ir.cancel()
	if ir.timer != nil {
		ir.timer.Stop()
	}
	ir.mu.Lock()
	defer ir.mu.Unlock()
	if ir.done {
		return
	}
	ir.done = true
	ir.Resp.Finish()
}

// SendUpdate ports Node `sendUpdate(msg)`: writes `msg + "\n"`; on write
// error aborts and warns "failed to send progress update".
func (ir *IncrementalResponse) SendUpdate(msg string) {
	if _, err := ir.Resp.Write([]byte(msg + "\n")); err != nil {
		ir.cancel()
		ir.Logger.Warn(ir.infoWith("err", err.Error()), ir.Label+": failed to send progress update")
	}
}

// Fail ports Node `fail(err)`: if not already aborted, log
// `"<label>: error"` and push the update; then end (Node's `#res.end()` is
// unconditional — the catch-swallow makes double-end safe there).
func (ir *IncrementalResponse) Fail(err error) {
	aborted := ir.ctx.Err() != nil
	ir.cancel()
	if !aborted {
		ir.Logger.Err(ir.infoWith("err", err.Error()), ir.Label+": error")
		ir.SendUpdate("error: " + ir.Label)
	}
	ir.End()
}

// humanReadableTimeout ports Node `#humanReadableTimeout`: Node operates in
// MILLISECONDS (its `timeout` arg is setTimeout's millisecond value);
// 123450 → "2min3s450ms", 90000 → "1min30s", 800 → "800ms".
func humanReadableTimeout(timeout time.Duration) string {
	ms := timeout.Milliseconds()
	minutes := ms / 60_000
	ms -= minutes * 60_000
	seconds := ms / 1_000
	ms -= seconds * 1_000
	var t string
	if minutes > 0 {
		t += fmt.Sprintf("%dmin", minutes)
	}
	if seconds > 0 {
		t += fmt.Sprintf("%ds", seconds)
	}
	if ms > 0 {
		t += fmt.Sprintf("%dms", ms)
	}
	return t
}
