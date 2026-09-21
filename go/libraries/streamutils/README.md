# `go/libraries/streamutils` — streaming helpers

Go 1:1 port of **`libraries/stream-utils`** (npm `@overleaf/stream-utils`):
the readable/writable stream primitives the CE services use to move request
and response bodies (size limits, timeouts, metering, logging, and
incremental/aborted responses).

## Node → Go shape
Node's `Transform` / `PassThrough` streams are bidirectional; Go request-body
pipelines only **read**, so each one ports to an `io.Reader` wrapper over its
source. `WritableBuffer` / `ReadableString` are direct ports (no wrapper).

## The API
| Symbol | Purpose |
| --- | --- |
| `type WritableBuffer` / `func NewWritableBuffer()` | buffers the bytes written to it; `contents()` returns them (Node `Writable`) |
| `type ReadableString` / `func NewReadableString(s)` | a reader that yields `s` (Node `Readable`) |
| `func NewLimitedReader(src io.Reader, maxSize int) *LimitedReader` | reads until cumulative bytes exceed `maxSize`; the crossing read returns `*SizeExceededError` (`exceeded stream size limit of <maxSize>: <size>`) |
| `type SizeExceededError` | the over-limit error (exact Node message) |
| `func NewTimeoutReader(src io.Reader, timeout time.Duration) *TimeoutReader` | passes `src` through until the timer fires; afterwards every read returns `*AbortError` (`stream timed out`); `Stop()` cancels the pending timer (Node `_final`'s `clearTimeout`) |
| `type AbortError` | the timeout / abort error (exact Node message) |
| `func NewLoggerReader(src io.Reader, maxSize int, logger func(size int, isFlush bool)) *LoggerReader` | on first overflow logs `(size, isFlush=false)` **once**; `Close()` logs `(size, isFlush=true)` a second time iff overflow happened — the double-call the Node suite pins |
| `type Meter` (iface) | `count(value, count, labels)` — the narrow surface of the `ometics` port (LIB-14) |
| `func NewMeteredReader(src io.Reader, meter Meter, metric string, labels map[string]string) *MeteredReader` | counts every read chunk via the `Meter` |
| `func NewIncrementalResponse(res ProgressResponse, timeout time.Duration, label string, info map[string]any, logger Logger) *IncrementalResponse` | Node's `AbortController` → `context.Context`; Node's `res` → the narrow `ProgressResponse` interface |
| `type ProgressResponse`, `type Logger` | the narrow `res` / logger surfaces |

## Conventions / gotchas
- **Timeout uses `context.Context`.** Node's `AbortController` has no Go
  equivalent, so cancellation is a context (documented divergence); the
  `TimeoutReader` still models the "read-after-fire → `AbortError`" behaviour
  that the oracles pin.
- **`LoggerReader` double-logs** (overflow, then flush) — a real behaviour
  difference from a naive "log once" reader, pinned by the Node test.
- **`MeteredReader`/`IncrementalResponse` depend on narrow interfaces**
  (`Meter`, `Logger`, `ProgressResponse`) rather than the concrete ometrics /
  services types — so this package stays dependency-light and injectable.

## Testing & coverage
`go test ./go/libraries/streamutils/ -count=1 -cover` — oracle-pinned to the
Node `stream-utils` suite (every error message string, the double log call,
timeout-after-read, size-limit crossing). **Coverage: 98.6%** (above the 85%
gate).

## Dependencies
Standard library only (`io`, `context`, `time`, `sync`).
