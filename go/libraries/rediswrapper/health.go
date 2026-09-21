package rediswrapper

import (
	"context"
	"fmt"
	"time"
)

// healthCheckTimeout mirrors the Node module constant HEARTBEAT_TIMEOUT
// (ms). A var (not const) so the port's own test-suite can shrink it —
// same as request-wait's RequestWarnTimeout.
var HealthCheckTimeout = 2000 * time.Millisecond

// HealthCheck mirrors the Node `async function healthCheck(client)` (the
// index.js export and `client.healthCheck`). The 2s heartbeat timeout and
// the distinct timeout/write/verify failure errors are the observable
// contract; the round-trip key/value shapes are pin-tested.
func (c *Client) HealthCheck(ctx context.Context) error {
	token := uniqueToken()
	key := healthCheckKey(token)
	value := healthCheckValue(token)

	// healthCheckContext mirrors the Node `context` object — mutated across
	// stages and attached to whatever error is raised.
	contextObject := map[string]any{
		"uniqueToken": token,
		"stage":       "add context for a timeout",
	}

	return runWithTimeout(ctx, contextObject, HealthCheckTimeout,
		func(ctx context.Context) error {
			return runCheck(ctx, c, key, value, contextObject)
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
func runCheck(ctx context.Context, c *Client, key, value string, contextObject map[string]any) error {
	contextObject["stage"] = "write"

	// client.set(key, value, 'EX', 60)
	ack, err := c.Driver.SetEx(ctx, key, value, 60, false)
	if err != nil {
		return NewRedisHealthCheckWriteError("write errored", contextObject, err)
	}
	if ack != "OK" {
		contextObject["writeAck"] = ack
		return NewRedisHealthCheckWriteError("write failed", contextObject, nil)
	}

	contextObject["stage"] = "verify"

	// client.multi().get(key).del(key).exec() → [value, delAck] (old-redis
	// format via the patched exec).
	values, err := c.Exec(ctx, []Op{{Cmd: "GET", Key: key}, {Cmd: "DEL", Key: key}})
	if err != nil {
		return NewRedisHealthCheckVerifyError("read/delete errored", contextObject, err)
	}
	roundTrippedValue, _ := values[0].(string)
	delAck, _ := values[1].(int64)
	if roundTrippedValue != value {
		contextObject["roundTrippedHealthCheckValue"] = roundTrippedValue
		return NewRedisHealthCheckVerifyError("read failed", contextObject, nil)
	}
	if delAck != 1 {
		contextObject["deleteAck"] = delAck
		return NewRedisHealthCheckVerifyError("delete failed", contextObject, nil)
	}
	return nil
}

// runWithTimeout mirrors the Node runWithTimeout(context, timeout, runFn):
// the runner is raced against setTimeout; on timeout the context gains
// `timeout: timeout` (ms) and RedisHealthCheckTimedOut('timeout') is
// raised — even if the runner later succeeds (Node Promise.race: the timer
// result wins the race; the loser keeps running, same leak preserved here).
func runWithTimeout(ctx context.Context, contextObject map[string]any, timeout time.Duration, runFn func(context.Context) error) error {
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
		contextObject["timeout"] = int64(timeout / time.Millisecond)
		return NewRedisHealthCheckTimedOut(contextObject, nil)
	}
}
