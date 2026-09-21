package rediswrapper

// health_test.go — the Node healthCheck contract (index.js). Not covered by
// the Node unit suite (manual live scripts only) but pin-tested here per the
// port policy: exact key/value token shapes, the write/read/delete round
// trip, and the distinct RedisHealthCheck{TimedOut, WriteError,
// VerifyError} error classes with the Node stage/context fields.

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"ollitex/go/libraries/oerror"
)

// the Node token format, byte-for-byte:
// host=...:pid=...:random=<8hex>:time=<ms>:count=N
var tokenRE = regexp.MustCompile(`^host=[^:]+:pid=\d+:random=[0-9a-f]{8}:time=\d+:count=\d+$`)

// echoSetDriver wraps a fakeDriver and makes Exec's GET row return exactly
// what the healthCheck SET wrote (a successful round trip, without needing
// live redis) — mirroring what a healthy redis instance would return.
type echoSetDriver struct {
	inner *fakeDriver
	ops   []Op // last Exec ops (for assertions — the inner fake never sees them)
}

func (e *echoSetDriver) Configure(opts map[string]any, cluster any) error {
	return e.inner.Configure(opts, cluster)
}

func (e *echoSetDriver) SetEx(ctx context.Context, key, value string, ttl int, nx bool) (string, error) {
	return e.inner.SetEx(ctx, key, value, ttl, nx)
}
func (e *echoSetDriver) Exists(ctx context.Context, key string) (int64, error) {
	return e.inner.Exists(ctx, key)
}
func (e *echoSetDriver) Eval(ctx context.Context, script string, keys []string, args []any) (any, error) {
	return e.inner.Eval(ctx, script, keys, args)
}

func (e *echoSetDriver) Exec(ctx context.Context, ops []Op) ([][]any, error) {
	e.ops = ops
	setCall, _ := e.inner.setCall(0)
	// scripted rows win (e.g. a scripted DEL ack), but the GET value always
	// echoes what the healthCheck wrote — the "healthy redis" behaviour.
	if e.inner.execRows == nil {
		return [][]any{{nil, setCall.value}, {nil, int64(1)}}, nil
	}
	rows := e.inner.execRows[0]
	if setCall.value != "" && len(rows) > 0 {
		rows[0] = []any{nil, setCall.value}
	}
	return rows, nil
}

func (e *echoSetDriver) FlushAll(ctx context.Context) error { return e.inner.FlushAll(ctx) }
func (e *echoSetDriver) Host() string                       { return e.inner.Host() }

func healthErr(t *testing.T, err error, wantType, wantMessage string) *oerror.OError {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	var oe *oerror.OError
	if !errors.As(err, &oe) {
		t.Fatalf("error is not an OError: %T %v", err, err)
	}
	if oe.Type != wantType {
		t.Fatalf("error type: got %q want %q", oe.Type, wantType)
	}
	if oe.Message != wantMessage {
		t.Fatalf("error message: got %q want %q", oe.Message, wantMessage)
	}
	return oe
}

func tokenOf(key string) string {
	const prefix = "_redis-wrapper:healthCheckKey:{"
	if len(key) < len(prefix) {
		return key
	}
	return key[len(prefix) : len(key)-1]
}

func TestHealthCheckSuccess(t *testing.T) {
	t.Parallel()
	inner := &fakeDriver{}
	client := &Client{Driver: &echoSetDriver{inner: inner}}

	if err := client.HealthCheck(context.Background()); err != nil {
		t.Fatalf("healthy round trip must succeed, got: %v", err)
	}

	setCall, ok := inner.setCall(0)
	if !ok {
		t.Fatal("exactly one SET expected, saw none")
	}
	token := tokenOf(setCall.key)
	if !tokenRE.MatchString(token) {
		t.Fatalf("token shape: %q does not match %v", token, tokenRE)
	}
	wantValue := "_redis-wrapper:healthCheckValue:{" + token + "}"
	if setCall.value != wantValue {
		t.Fatalf("\nvalue got : %q\nvalue want: %q", setCall.value, wantValue)
	}
	if setCall.ttlSecs != 60 {
		t.Fatalf("SET EX: got %d want 60", setCall.ttlSecs)
	}
	if setCall.nx {
		t.Fatal("healthCheck SET must not be NX")
	}

	ops := client.Driver.(*echoSetDriver).ops
	if !tokenRE.MatchString(tokenOf(setCall.key)) {
		t.Fatalf("token shape: %q", tokenOf(setCall.key))
	}
	if ops == nil || len(ops) != 2 {
		t.Fatalf("one exec of two ops expected, got %+v", ops)
	}
	if ops[0].Cmd != "GET" || ops[0].Key != setCall.key {
		t.Fatalf("first op: got %+v want GET on the healthCheck key", ops[0])
	}
	if ops[1].Cmd != "DEL" || ops[1].Key != setCall.key {
		t.Fatalf("second op: got %+v want DEL on the healthCheck key", ops[1])
	}
}

func TestHealthCheckWriteErrored(t *testing.T) {
	t.Parallel()
	d := &fakeDriver{setErrs: []error{errors.New("write: no such host")}}
	client := &Client{Driver: d}
	err := client.HealthCheck(context.Background())
	oe := healthErr(t, err, "RedisHealthCheckWriteError", "write errored")
	cause, _ := oe.Cause.(error)
	if cause == nil || cause.Error() != "write: no such host" {
		t.Fatalf("cause: got %v", oe.Cause)
	}
	info := oe.Info
	if info["stage"] != "write" {
		t.Fatalf("context stage: got %v want write", info["stage"])
	}
	if _, ok := info["uniqueToken"]; !ok {
		t.Fatal("context.uniqueToken missing")
	}
}

func TestHealthCheckWriteAckNotOK(t *testing.T) {
	t.Parallel()
	d := &fakeDriver{setAcks: []string{""}} // ioredis null ack
	client := &Client{Driver: d}
	err := client.HealthCheck(context.Background())
	oe := healthErr(t, err, "RedisHealthCheckWriteError", "write failed")
	if v, ok := oe.Info["writeAck"]; !ok || v != "" {
		t.Fatalf("writeAck context: got %v (present=%v)", v, ok)
	}
}

func TestHealthCheckReadMismatch(t *testing.T) {
	t.Parallel()
	d := &fakeDriver{execRows: [][][]any{{{nil, "someone-elses-value"}, {nil, int64(1)}}}}
	client := &Client{Driver: d}
	err := client.HealthCheck(context.Background())
	oe := healthErr(t, err, "RedisHealthCheckVerifyError", "read failed")
	if v := oe.Info["roundTrippedHealthCheckValue"]; v != "someone-elses-value" {
		t.Fatalf("roundTrippedHealthCheckValue: got %v", v)
	}
}

func TestHealthCheckDeleteAckNotOne(t *testing.T) {
	t.Parallel()
	inner := &fakeDriver{execRows: [][][]any{{{nil, "x"}, {nil, int64(0)}}}}
	d := &echoSetDriver{inner: inner}
	client := &Client{Driver: d}
	err := client.HealthCheck(context.Background())
	oe := healthErr(t, err, "RedisHealthCheckVerifyError", "delete failed")
	if v := oe.Info["deleteAck"]; v != int64(0) {
		t.Fatalf("deleteAck context: got %v", v)
	}
}

func TestHealthCheckExecErrored(t *testing.T) {
	t.Parallel()
	d := &fakeDriver{execErr: errors.New("exec: server went away")}
	client := &Client{Driver: d}
	err := client.HealthCheck(context.Background())
	oe := healthErr(t, err, "RedisHealthCheckVerifyError", "read/delete errored")
	cause, _ := oe.Cause.(error)
	if cause == nil || cause.Error() != "exec: server went away" {
		t.Fatalf("cause: got %v", oe.Cause)
	}
	if info := oe.Info; info != nil && info["stage"] != "verify" {
		t.Fatalf("context stage: got %v want verify", info["stage"])
	}
}

func TestHealthCheckTimedOut(t *testing.T) {
	t.Parallel()
	old := HealthCheckTimeout
	HealthCheckTimeout = 50 * time.Millisecond
	defer func() { HealthCheckTimeout = old }()

	d := &fakeDriver{setDelays: []time.Duration{5 * time.Second}} // block past the timeout
	client := &Client{Driver: d}
	err := client.HealthCheck(context.Background())
	oe := healthErr(t, err, "RedisHealthCheckTimedOut", "timeout")
	if v := oe.Info["timeout"]; v != int64(50) {
		t.Fatalf("timeout context: got %v want int64(50) ms", v)
	}
	if _, ok := oe.Info["uniqueToken"]; !ok {
		t.Fatal("context.uniqueToken missing")
	}
}

func TestHealthCheckTokenUniqueness(t *testing.T) {
	t.Parallel()
	d1 := &echoSetDriver{inner: &fakeDriver{}}
	d2 := &echoSetDriver{inner: &fakeDriver{}}
	if err := (&Client{Driver: d1}).HealthCheck(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := (&Client{Driver: d2}).HealthCheck(context.Background()); err != nil {
		t.Fatal(err)
	}
	k1, _ := d1.inner.setCall(0)
	k2, _ := d2.inner.setCall(0)
	t1, t2 := tokenOf(k1.key), tokenOf(k2.key)
	if t1 == t2 {
		t.Fatalf("healthCheck tokens must be unique per call:\n%s\n%s", t1, t2)
	}
	n1 := tokenCount(t1)
	n2 := tokenCount(t2)
	if n1 == n2 {
		t.Fatalf("token count must be monotonic: %d == %d", n1, n2)
	}
}

func tokenCount(token string) int64 {
	const marker = ":count="
	idx := strings.LastIndex(token, marker)
	if idx < 0 {
		return -1
	}
	v, _ := strconv.ParseInt(token[idx+len(marker):], 10, 64)
	return v
}

func lastColon(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			return i
		}
	}
	return -1
}
