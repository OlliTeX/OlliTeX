package rediswrapper

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

// HEARTBEAT_TIMEOUT mirrors the Node module constant (ms).
const HeartbeatTimeout = 2000 // ms

// cleanup test-redis module state + healthCheck token generator (Node module
// scope: HOST/PID/RND/COUNT are process-wide in the Node module too).
var (
	host, _ = os.Hostname()
	pid     = os.Getpid()
	// RND — random hex per process (Node: crypto.randomBytes(4).toString('hex')).
	rnd = func() string {
		b := make([]byte, 4)
		_, _ = rand.Read(b)
		return hex.EncodeToString(b)
	}()
	// Count — monotonic per-process counter (Node: let COUNT = 0; COUNT++).
	count atomic.Int64
)

// uniqueToken mirrors the Node healthCheck token format byte-for-byte:
//
//	host=${HOST}:pid=${PID}:random=${RND}:time=${Date.now()}:count=${COUNT++}
func uniqueToken() string {
	return fmt.Sprintf("host=%s:pid=%d:random=%s:time=%d:count=%d",
		host, pid, rnd, time.Now().UnixMilli(), count.Add(1))
}

// healthCheckKey/value mirror the Node `_redis-wrapper:healthCheck{Key,Value}
// :{token}` shapes.
func healthCheckKey(token string) string {
	return fmt.Sprintf("_redis-wrapper:healthCheckKey:{%s}", token)
}
func healthCheckValue(token string) string {
	return fmt.Sprintf("_redis-wrapper:healthCheckValue:{%s}", token)
}

// Client is the Node `client` object from createClient: the driver plus the
// attached healthCheck and the patched multi/exec.
type Client struct {
	Driver Driver
	// Options is the normalised driver options CreateClient passed to the
	// driver (Node: `client.options` on the ioredis instance).
	Options map[string]any
	// ClusterConfig is the cluster node config in cluster mode (Node:
	// `client.config` on the ioredis Cluster; nil in single-instance mode).
	ClusterConfig any
}

// CreateClient mirrors createClient(opts) in index.js:
//
//	const standardOpts = Object.assign({}, opts)
//	delete standardOpts.key_schema
//	standardOpts.retry_max_delay ??= 5000
//	if (standardOpts.endpoints) throw '@overleaf/redis-wrapper: redis-sentinel is no longer supported'
//	if (standardOpts.cluster) → cluster dispatch: new Redis.Cluster(opts.cluster, standardOpts)
//	else                      → single:       new Redis(standardOpts)
//
// the driver construction is a seam (DriverConstructor.Configure).
func CreateClient(opts map[string]any, ctor DriverConstructor) (*Client, error) {
	// Object.assign({}, opts) — nil/empty opts are legal (Node
	// `createClient()` with undefined opts).
	standardOpts := map[string]any{}
	for k, v := range opts {
		standardOpts[k] = v
	}
	delete(standardOpts, "key_schema")
	if standardOpts["retry_max_delay"] == nil {
		standardOpts["retry_max_delay"] = 5000
	}
	if endpoints, present := standardOpts["endpoints"]; present && endpoints != nil {
		return nil, errors.New("@overleaf/redis-wrapper: redis-sentinel is no longer supported")
	}

	var clusterConfig any
	if cluster, present := standardOpts["cluster"]; present {
		clusterConfig = cluster
		delete(standardOpts, "cluster")
	}

	if err := ctor.Configure(standardOpts, clusterConfig); err != nil {
		return nil, err
	}

	return &Client{Driver: ctor, Options: standardOpts, ClusterConfig: clusterConfig}, nil
}

// Exec mirrors the Node monkey-patched `client.multi().exec()`: run the
// queued ops through the driver and return the OLD-REDIS format — one value
// per command (or the first error). Node unwrapMultiResult:
//
//	res.forEach(([err, result]) => { if (err) return callback(err); resultArr.push(result) })
func (c *Client) Exec(ctx context.Context, ops []Op) ([]any, error) {
	rows, err := c.Driver.Exec(ctx, ops)
	if err != nil {
		return nil, err
	}
	values := make([]any, 0, len(rows))
	for _, row := range rows {
		if len(row) != 2 {
			return nil, fmt.Errorf("rediswrapper: malformed multi row %v", row)
		}
		if row[0] != nil {
			var rowErr error
			switch e := row[0].(type) {
			case error:
				rowErr = e
			default:
				rowErr = errors.New(fmt.Sprint(e))
			}
			return nil, rowErr
		}
		values = append(values, row[1])
	}
	return values, nil
}

// CleanupTestRedis mirrors cleanupTestRedis(rclient) — flushes the test
// instance only after the ensureTestRedis safety check.
func CleanupTestRedis(c *Client) error {
	if err := ensureTestRedis(c); err != nil {
		return err
	}
	return c.Driver.FlushAll(context.Background())
}

// ensureTestRedis mirrors the Node helper byte-for-byte:
//
//	const host = rclient.options.host
//	const env = process.env.NODE_ENV
//	if (host !== 'redis_test' || env !== 'test') {
//		throw new Error(`Refusing to clear Redis instance '${host}' in environment '${env}'`)
//	}
func ensureTestRedis(c *Client) error {
	hostOpt, _ := c.Options["host"].(string)
	env := os.Getenv("NODE_ENV")
	if hostOpt != "redis_test" || env != "test" {
		return fmt.Errorf("Refusing to clear Redis instance '%s' in environment '%s'", hostOpt, env)
	}
	return nil
}
