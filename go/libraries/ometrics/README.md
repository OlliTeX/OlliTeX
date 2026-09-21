# `go/libraries/ometrics` — metrics registry + monitors (Prometheus-format)

Go 1:1 port of **`libraries/metrics`** (npm `@overleaf/metrics`): a
**Prometheus-flavoured registry over the Go standard library** (no Prometheus
client dependency), the top-level metrics API (`inc`/`count`/`summary`/
`timing`/`histogram`/`gauge`/…), and the platform monitors. Every service
reports its metrics through this package, and the `/metrics` endpoint reads it.

## The registry
| Symbol | Purpose |
| --- | --- |
| `func NewRegistry() *Registry` / `type Registry` | the metric store (Counters/Gauges/Summaries/Histograms) |
| `type Counter` / `type Gauge` | the two basic metric families |
| `type Summary` | quantiles + `_sum` (the `summary` metric) |
| `type Histogram` | cumulative buckets with `le` labels + `+Inf` (the `histogram` metric) |
| `type Metric` / `type MetricJSON` | a metric value / its JSON wire shape |

## The top-level API (the `index` surface)
`Inc` / `Count` / `Summary` / `Timing` / `Histogram` / `Gauge` /
`GlobalGauge` / `NewTimer` / `Close` / `Initialize` / `CollectDefaultMetrics`
/ `MetricsHandler` — the call sites the services use (e.g.
`ometrics.Inc("requests", labels)`, `ometrics.Timing("db.query", ms, labels)`).

## Platform monitors
| Monitor | Collects |
| --- | --- |
| `HTTPMonitor` / `NewRequestLogger` / `FlattenHeaders` / `Redact` / `SanitizeValue` | per-request metrics + the request-logger with header redaction |
| `EventLoopMonitor` / `UvThreadpoolSize` / `ActiveHandles` | the Node event-loop / uv-threadpool liveness |
| `MongoMonitor` / `MongoReset` / `CommandEvent` / `CommandCursor` / `CommandReply` / `DefaultRecorder` | the mongoose command-level metrics |
| `MemoryMonitor` / `MemorySource` / `GoMemorySource` / `MemUsage` | process memory (Go `runtime` source behind the `MemorySource` seam) |
| open/leaked sockets | `ResetOpenSockets` / `SetLeakThreshold` / the socket-leak detector |
| `AppName` / `Hostname` / `BuildPromKey` | the default `host`/`app` labels + the `buildPromKey` label escaping |

## Node → Go conventions (narrow seams)
The Node metrics read platform data (event loop, libuv threadpool, mongoose
command events, open sockets, memory) through Node-specific sources. The Go
port puts each behind a **narrow seam** (`MemorySource`, `MemoryMonitor`, the
mongo `DefaultRecorder`, etc.) so the Go tests drive **fake** data sources
exactly like the Node mock-based oracle. The registry itself is a real,
dependency-free implementation (Counters/Gauges/Summaries/Histograms) — this is
a *port*, not a wrapper.

## Conventions / gotchas
- **Cumulative histograms** bucket by `le` and always carry `+Inf`; summaries
  carry quantiles + `_sum`. The `buildPromKey` **escaping** of label values is
  pinned (special chars are escaped the way Prometheus requires).
- **Default labels** (`host`, `app`) are added automatically; `SanitizeValue`
  / `Redact` / `FlattenHeaders` normalise + mask request data before it's
  counted (the `Redact` list is the Node one).
- **`GlobalGauge`** is the process-wide, label-free gauge (e.g. uptime),
  distinct from a per-label `Gauge`.

## Testing & coverage
`go test ./go/libraries/ometrics/ -count=1 -cover` — oracle-pinned to the Node
`metrics` suite: the Go suite **mirrors all 37 Node oracle tests** (the index
API + every monitor, over fake seams). **Coverage: 95.9%** (above the 85%
gate).

## Dependencies
Standard library only (`runtime`, `sync/atomic`, `net`, `time`).
