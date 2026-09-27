package rediswrapper

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// healthCheckTimeout mirrors the Node module constant HEARTBEAT_TIMEOUT
// (ms). A var (not const) so the port's own test-suite can shrink it —
// same as request-wait's RequestWarnTimeout.
//
// D27b (race-isolation): the var is guarded by an RW lock because the
// shrink-restore test runs concurrently with other HealthCheck callers
// (t.Parallel in-package); unguarded read/write of the global was a
// data race in the full-suite -race run.
var (
	HealthCheckTimeoutMu sync.RWMutex
	HealthCheckTimeout   = 2000 * time.Millisecond
)

// get/setHealthCheckTimeout — the locked accessors (the public var is
// retained for config-style reads at init; runtime callers must go
// through these).
func getHealthCheckTimeout() time.Duration {
	HealthCheckTimeoutMu.RLock()
	defer HealthCheckTimeoutMu.RUnlock()
	return HealthCheckTimeout
}

func setHealthCheckTimeout(d time.Duration) time.Duration {
	HealthCheckTimeoutMu.Lock()
	defer HealthCheckTimeoutMu.Unlock()
	old := HealthCheckTimeout
	HealthCheckTimeout = d
	return old
}

// healthCheckContext — the Node `context` object of the healthCheck round
// trip.
//
// D27b (race): the timeout path and the (still-running) leaked runner
// goroutine both mutate this object — in Node single-threaded that is a
// harmless leak; in Go it is a data race on the map. The object is
// therefore internally locked, and every error CONSTRUCTION snapshots it
// under the lock so no oerror.Info ever references the live map.
type healthCheckContext struct {
	mu sync.RWMutex
	m  map[string]any
}

func newHealthCheckContext(init map[string]any) *healthCheckContext {
	m := make(map[string]any, len(init)+2)
	for k, v := range init {
		m[k] = v
	}
	return &healthCheckContext{m: m}
}

func (h *healthCheckContext) Set(k string, v any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.m[k] = v
}

func (h *healthCheckContext) snapshot() map[string]any {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make(map[string]any, len(h.m))
	for k, v := range h.m {
		out[k] = v
	}
	return out
}

// HealthCheck mirrors the Node `async function healthCheck(client)` (the
// index.js export and `client.healthCheck`). The 2s heartbeat timeout and
// the distinct timeout/write/verify failure errors are the observable
// contract; the round-trip key/value shapes are pin-tested.
func (c *Client) HealthCheck(ctx context.Context) error {
	token := uniqueToken()
	key := healthCheckKey(token)
	value := healthCheckValue(token)

	// healthCheckContext mirrors the Node `context` object — mutated across
	// stages and attached (as a snapshot) to whatever error is raised.
	ctxObj := newHealthCheckContext(map[string]any{
		"uniqueToken": token,
		"stage":       "add context for a timeout",
	})

	return runWithTimeout(ctx, ctxObj, getHealthCheckTimeout(),
		func(ctx context.Context) error {
			return runCheck(ctx, c, key, value, ctxObj)
		})
}

// runCheck mirrors the Node runCheck(client, key, value, context) —
// write/read/delete round-trip with the exact error mapping:
//
//	SET → err:                RedisHealthCheckWriteError('write errored', ctx, cause)
//	SET ack !== 'OK' →       RedisHealthCheckWriteError('write failed', ctx{writeAck})
//	multi exec err →         RedisHealthCheckVerifyError('read/delete errored', ctx, cause)
//	read mismatch →          RedisHealthCheckVerifyError('read failed', ctx{roundTrippedHealthCheckValue})
//	delete ack !== 1 →       RedisHealthCheckVerifyError('delete failed', ctx{deleteAck})
func runCheck(ctx context.Context, c *Client, key, value string, ctxObj *healthCheckContext) error {
	ctxObj.Set("stage", "write")

	// client.set(key, value, 'EX', 60)
	ack, err := c.Driver.SetEx(ctx, key, value, 60, false)
	if err != nil {
		return NewRedisHealthCheckWriteError("write errored", ctxObj.snapshot(), err)
	}
	if ack != "OK" {
		ctxObj.Set("writeAck", ack)
		return NewRedisHealthCheckWriteError("write failed", ctxObj.snapshot(), nil)
	}

	ctxObj.Set("stage", "verify")

	// client.multi().get(key).del(key).exec() → [value, delAck] (old-redis
	// format via the patched exec).
	values, err := c.Exec(ctx, []Op{{Cmd: "GET", Key: key}, {Cmd: "DEL", Key: key}})
	if err != nil {
		return NewRedisHealthCheckVerifyError("read/delete errored", ctxObj.snapshot(), err)
	}
	roundTrippedValue, _ := values[0].(string)
	delAck, _ := values[1].(int64)
	if roundTrippedValue != value {
		ctxObj.Set("roundTrippedHealthCheckValue", roundTrippedValue)
		return NewRedisHealthCheckVerifyError("read failed", ctxObj.snapshot(), nil)
	}
	if delAck != 1 {
		ctxObj.Set("deleteAck", delAck)
		return NewRedisHealthCheckVerifyError("delete failed", ctxObj.snapshot(), nil)
	}
	return nil
}

// runWithTimeout mirrors the Node runWithTimeout(context, timeout, runFn):
// the runner is raced against setTimeout; on timeout the context gains
// `timeout: timeout` (ms) and RedisHealthCheckTimedOut('timeout') is
// raised — even if the runner later succeeds (Node Promise.race: the timer
// result wins the race; the loser keeps running, same leak preserved here).
func runWithTimeout(ctx context.Context, ctxObj *healthCheckContext, timeout time.Duration, runFn func(context.Context) error) error {
	type result struct{ err error }
	ch := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				select {
				case ch <- result{err: fmt.Errorf("rediswrapper: runner panicked: %v", r)}:
				default:
				}
			}
		}()
		ch <- result{err: runFn(ctx)}
	}()

	t := time.NewTimer(timeout)
	defer t.Stop()

	select {
	case r := <-ch:
		return r.err
	case <-t.C:
		// Deterministic race resolution: if the runner's result is already
		// queued (it finished at/before the deadline), Node's race has the
		// earlier event win — so prefer the runner result.
		select {
		case r := <-ch:
			return r.err
		default:
		}
		ctxObj.Set("timeout", int64(timeout/time.Millisecond))
		return NewRedisHealthCheckTimedOut(ctxObj.snapshot(), nil)
	}
}
