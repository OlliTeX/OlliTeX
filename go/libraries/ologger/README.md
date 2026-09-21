# `go/libraries/ologger` — Overleaf logger (bunyan-compatible)

Go 1:1 port of **`libraries/logger`** (npm `@overleaf/logger`): the
**bunyan**-backed `LoggingManager` — request/response serializers, the
serializable-req lockdown, the GCP log-level checkers (GCE metadata +
ring-buffer GKE), the GCP log-entry converter, and a JSON-line sink. This is
the services' structured-logging layer, so it is built around **narrow seams**
so the Node `bunyan` sandbox in the oracle is mirrored by Go test stubs.

## The API
| Symbol | Purpose |
| --- | --- |
| `type LoggingManager` + `func New(…)` / `…WithStreams(…)` | the manager — `initialize`, `debug`/`info`/`warn`/`error`/`fatal`/`exit`, `addSerializer`, ring-buffer + level-checker wiring |
| `type Serializer` / `ReqSerializer` / `ResSerializer` / `ErrSerializer` | the bunyan serializers; `Req` ports `getRawReqInput` + `getRemoteIp` (the **lockdown** req model) |
| `type Req` | a request with `LockdownInstalled` + `RawParams` (the *safe* source) vs `Params` (poison — must not be read) |
| `type Bunyan` / `type Logger` / `type RingBuffer` / `type StreamConfig` | the bunyan seam surface (`createLogger` + ring buffer) |
| `type Checker` / `type CheckerFactories` + `func NewFileLogLevelChecker(…)` / `func NewGCEMetadataLogLevelChecker(…)` | the runtime log-level checkers (file + GCE metadata), base interval loop |
| `func ConvertLogEntry(entry) …` | the GCP log-entry converter (`gcp-manager.js`: `omit`, label conversion) |
| `func NewJSONBunyan(…)` | the real standalone default — a JSON-line sink with level filtering (a Go addition the Node suite never exercises, tested independently) |
| `type LoggerConfig` / `type Options` / `type LoggerOption` / `type Entry` | config/options/entry types |

## Node → Go seam strategy
The Node 31-test oracle is **fully bunyan-sandboxed** (`sandboxed-module` stubs
`bunyan`, `@overleaf/fetch-utils`, `@overleaf/validation-tools`,
`./log-level-checker`). So the Go port injects the narrow `Bunyan` / `Logger` /
`CheckerFactories` seams and the Go tests drive a **STUB bunyan** exactly like
the Node sandbox. The GKE `LoggingBunyan` / `@google-cloud/logging-bunyan`
runtime adapters are seams (flagged `StreamConfig.GKE/GCE`), not imported.

## Conventions / gotchas (pinned)
1. **`attributes`/`message` are `any`** — the Node "bunyan logging" oracle
   passes a single *array* as the sole arg with `message` undefined and expects
   it forwarded verbatim; `error()` only injects `logBuffer` when
   `attributes` is a `map[string]any`.
2. **Req-serializer lockdown.** Node's `getRawReqInput` reads the
   patched-express symbol-keyed raw accessors instead of the throwing
   `req.params` getter — Go models this as `Req.LockdownInstalled` +
   `Req.RawParams` (safe) vs `Req.Params` (poison, MUST NOT be read). The
   oracle test pins ids reading from `RawParams` while `Params` holds a wrong
   value.
3. **Level filter is bunyan semantics** — *suppress when `level <
   configuredLevel`* (i.e. `fatal` always emits, `debug` is suppressed at `info`
   level). An inverted guard would wrongly drop `fatal`.
4. **GCE checker** fetches the exact URI
   `http://metadata.google.internal/computeMetadata/v1/project/attributes/<name>-setLogLevelEndTime`
   with header `Metadata-Flavor: Google` (name from `Logger.Name()`).
5. **`parseInt(s)||0`** semantics for the ring buffer + tracing end-time.
6. **`setLogger(this)`** on `@overleaf/fetch-utils` /
   `@overleaf/validation-tools` is an injectable `Warn`-callback seam (default
   no-op; the host wires `fetchutils.SetLogger`).

## Testing & coverage
`go test ./go/libraries/ologger/ -count=1 -cover` — oracle-pinned to the Node
`logger` 31-test suite (all 31 Node oracle tests have a green Go counterpart).
**Coverage: 94.2%** (above the 85% gate).

## Dependencies
Standard library + `ollitex/go/libraries/oerror` (for `GetFullStack` in the err
serializer).
