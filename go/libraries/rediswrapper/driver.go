// Package rediswrapper is the 1:1 Go port of `libraries/redis-wrapper`
// (npm `@overleaf/redis-wrapper`): a wrapper around a Redis driver that adds
//
//   - createClient option validation/normalisation (redis-sentinel rejected,
//     retry_max_delay default 5000, key_schema stripped, cluster dispatch),
//   - the monkey-patched client.multi().exec() result unwrap (old-redis
//     format: [42, "foo"] instead of ioredis's [[null, 42], [null, "foo"]]),
//   - a healthCheck that writes+reads+deletes a unique key under a 2s
//     timeout (distinct RedisHealthCheckTimedOut / WriteError / VerifyError),
//   - RedisLocker: SET NX EX distributed lock with signed lock values,
//     poll+exponential-backoff acquisition, TTL extension (EVAL),
//     release-via-Lua,
//   - RedisWebLocker: FIFO per-key lock acquisition queue + runWithLock
//     (slow-execution + lock-expiry logging),
//   - cleanupTestRedis with the host/environment safety check.
//
// Go seam (like the other library ports, e.g. fetchutils `Client`): the
// Node ioredis driver is driver-specific TCP plumbing; here the narrow
// `Driver` interface is the drop-in boundary and the consuming Go service
// supplies a real implementation (or a test fake — the Node oracle is
// itself fully driver-mocked, so this is 1:1 with the acceptance spec).
package rediswrapper

import "context"

// Driver is the narrow ioredis-shaped interface the wrapper drives. The
// result shapes mirror ioredis 4.x (the driver the Node wrapper targets),
// so the wrapper's observable contract stays byte-faithful.
type Driver interface {
	// SetEx runs `SET key value EX ttlSeconds [NX]` and resolves like
	// ioredis: "OK" when the key was set, "" (null) when it was not.
	SetEx(ctx context.Context, key, value string, ttlSeconds int, nx bool) (string, error)

	// Exists runs `EXISTS key` (ioredis resolves the integer count).
	Exists(ctx context.Context, key string) (int64, error)

	// Eval runs a lua script (Node: rclient.eval(script, numKeys, ...keys,
	// ...args)); ioredis resolves the script's return value.
	Eval(ctx context.Context, script string, keys []string, args []any) (any, error)

	// Exec runs a queued GET/DEL batch (Node: client.multi().get(k).del(k)
	// .exec()) and returns the RAW ioredis rows — one [err, value] pair per
	// queued command, in order (e.g. [[nil, "v"], [nil, 1]]). The Client
	// layer unwraps the rows into the old-redis format (the Node
	// monkey-patch, pin-tested 1:1).
	Exec(ctx context.Context, ops []Op) ([][]any, error)

	// FlushAll runs `FLUSHALL` (test-instance cleanup only).
	FlushAll(ctx context.Context) error

	// Host reports the configured host (Node: rclient.options.host — used by
	// the ensureTestRedis safety check).
	Host() string
}

// Op is one queued command for Driver.Exec (the healthCheck round-trip uses
// GET then DEL on the same key).
type Op struct {
	Cmd string // "GET" or "DEL"
	Key string
}

// Configure is the driver construction seam (Node: `new Redis(opts)` /
// `new Redis.Cluster(nodes, opts)`). CreateClient calls it with the
// normalised options and, in cluster mode, the cluster node config.
type DriverConstructor interface {
	Driver
	// Configure receives the normalised driver options (Node standardOpts)
	// and the cluster config (nil in single-instance mode), mirroring
	// `new Redis(opts)` / `new Redis.Cluster(nodes, opts)`.
	Configure(opts map[string]any, clusterConfig any) error
}
