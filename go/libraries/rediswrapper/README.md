# `go/libraries/rediswrapper` — redis client wrapper + lockers

Go 1:1 port of **`libraries/redis-wrapper`** (npm `@overleaf/redis-wrapper`,
an **ioredis** wrapper): the connect/create surface, the **distributed
lockers** (`RedisLocker` short-TTL, `RedisWebLocker` queue-based), the redis
**health check** (token write/read/verify), and the typed error family. Used
by the web service and the chat service for mutual exclusion and health.

## The API
| Symbol | Purpose |
| --- | --- |
| `type Driver` / `type DriverConstructor` / `type Op` | the ioredis seam — the narrow surface of the redis command surface the package uses, so tests inject a fake driver |
| `func CreateClient(…)` (`client.go`) | the Node `createClient` — mode/option validation (short-TTL, no sentinel, single/cluster driver choice) |
| `func NewRedisLocker(cfg RedisLockerConfig) (*RedisLocker, error)` | the TTL-based locker (Node `RedisLocker`) — `lock`/`extend`/`unlock`, `lockTTLSeconds` validation |
| `func NewRedisWebLocker(opts map[string]any, getKey func(namespace,id string) string, driver Driver, metrics Metrics, logger Logger) *RedisWebLocker` | the web locker (Node `RedisWebLocker`) — queue-based locking over the driver |
| `func LockQueuesSize() int` | the number of queued locks (diagnostics) |
| `func NewRedisError(message, info, cause) *oerror.OError` | the generic redis error |
| `func NewRedisHealthCheckFailed / …TimedOut / …WriteError / …VerifyError(…)` | the four health-check error variants |
| health token machinery (`client.go`) | the health-check **unique token** format, byte-for-byte to Node (`HOST:PID:RND:COUNT`, `RND` = 4 random hex bytes, `COUNT` a per-process monotonic counter) |
| `type Metrics` / `type Logger` / `type Timer` / `NullMetrics` / `NullLogger` / `NullTimer` | the narrow metrics/log/timer seams (null defaults) |

## Node → Go seam strategy
The Node package wraps **ioredis** and the oracle is **fully mock-based** (the
Node `test/unit` injects a fake driver), so **no live redis is needed** for the
unit tests. Go mirrors that: the `Driver` / `DriverConstructor` seams stand in
for ioredis, and the Go tests drive a **fake driver** exactly like the Node
oracle. The real ioredis/redis driver is a host-injected implementation of the
same seam.

## Conventions / gotchas
- **`lockTTLSeconds` is validated** — a too-small TTL is rejected (Node
  `should error if lockTTLSeconds is small`), and **redis-sentinel mode is
  disallowed** (Node `should throw if redis-sentinel is used`).
- **The health-check token is process-wide** in Node (module-scope
  `HOST/PID/RND/COUNT`); Go keeps the same per-process state so two calls
  produce distinct, ordered tokens (pinned).
- **Errors embed `oerror.OError`** (`NewRedisError` and the four health-check
  variants) with structured `info`, so `oerror.GetFullInfo` / `errors.As` work
  on the wrapped chain.
- **`NullLogger`/`NullMetrics`/`NullTimer`** are the no-op defaults — pass a
  real implementation (e.g. the `ologger`/`ometrics` ports) in production.

## Testing & coverage
`go test ./go/libraries/rediswrapper/ -count=1 -cover` — oracle-pinned to the
Node `redis-wrapper` unit suite (`test/unit/src/test.js`), over a fake driver.
**Coverage: 95.3%** (above the 85% gate; **no live redis required**).

## Dependencies
Standard library + `ollitex/go/libraries/oerror` (typed error shapes).
