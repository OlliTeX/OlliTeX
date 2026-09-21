package rediswrapper

// locker_test.go — the Node RedisLocker unit suite (test/unit/src/test.js),
// 1:1 mirrored, plus the port-policy additions (boundary values,
// overlong-SET auto-release, checkLock) that Node never pinned under unit
// test.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"
)

func TestRedisLockerTTLValidation(t *testing.T) {
	t.Parallel()
	// Node:
	//   'should error if lock TTL is wrong type' — { lockTTLSeconds: '60' }
	//     (unrepresentable in the Go int-typed API — documented)
	//   'should error if lockTTLSeconds is small' — { lockTTLSeconds: 1 }
	//   'should error if lockTTLSeconds is huge' — { lockTTLSeconds: 30000 }
	for _, ttl := range []int{1, 29, 1000, 30000} {
		_, err := NewRedisLocker(RedisLockerConfig{
			RClient: &fakeDriver{},
			GetKey:  func(id string) string { return "lock:" + id },
			WrapTimeoutError: func(err error, id string) error {
				return fmt.Errorf("wrapped(%s)", err.Error())
			},
			MetricsPrefix:  "test.lock",
			LockTTLSeconds: ttl,
		})
		if err == nil {
			t.Fatalf("TTL %d: expected validation error, got nil", ttl)
		}
		const want = "redis lock TTL must be at least 30s and below 1000s"
		if err.Error() != want {
			t.Fatalf("TTL %d:\n got: %q\nwant: %q", ttl, err.Error(), want)
		}
	}
	// boundaries 30 and 999 must be accepted (Node: 30 <= ttl < 1000).
	for _, ttl := range []int{30, 999} {
		_, err := NewRedisLocker(RedisLockerConfig{
			RClient:          &fakeDriver{},
			GetKey:           func(id string) string { return "lock:" + id },
			WrapTimeoutError: func(err error, id string) error { return err },
			MetricsPrefix:    "test.lock",
			LockTTLSeconds:   ttl,
		})
		if err != nil {
			t.Fatalf("TTL %d: expected acceptance, got %v", ttl, err)
		}
	}
}

func newTestLocker(d *fakeDriver, metrics *recordingMetrics) *RedisLocker {
	l, err := NewRedisLocker(RedisLockerConfig{
		RClient:          d,
		GetKey:           func(id string) string { return "lock:" + id },
		WrapTimeoutError: func(err error, id string) error { return err },
		MetricsPrefix:    "test.lock",
		LockTTLSeconds:   30,
		Metrics:          metrics,
	})
	if err != nil {
		panic(err)
	}
	return l
}

func TestRedisLockerExtendLockSuccess(t *testing.T) {
	t.Parallel()
	// Node 'should extend the lock': unlock script result 1 → success;
	// EVAL called with (extendScript, 1, 'lock:id-1', 'lock-value', 30).
	metrics := &recordingMetrics{}
	d := &fakeDriver{evalResults: []any{int64(1)}}
	locker := newTestLocker(d, metrics)

	if err := locker.ExtendLock(context.Background(), "id-1", "lock-value"); err != nil {
		t.Fatalf("extend should succeed, got %v", err)
	}
	call, ok := d.evalCall(0)
	if !ok {
		t.Fatal("EVAL not called")
	}
	if call.script != extendScript {
		t.Fatalf("script:\n got: %s\nwant: %s", call.script, extendScript)
	}
	if !reflectEqual(call.keys, []string{"lock:id-1"}) {
		t.Fatalf("keys: got %v want [lock:id-1]", call.keys)
	}
	if !reflectEqual(call.args, []any{"lock-value", 30}) {
		t.Fatalf("args: got %v want [lock-value 30]", call.args)
	}
	names := metrics.incNames()
	if !contains(names, "test.lock-extend-success") {
		t.Fatalf("metrics incs: want test.lock-extend-success in %v", names)
	}
}

func TestRedisLockerExtendLockAlreadyReleased(t *testing.T) {
	t.Parallel()
	// Node 'should extend an already timed out lock': result 0 → error with
	// exact message.
	metrics := &recordingMetrics{}
	d := &fakeDriver{evalResults: []any{int64(0)}}
	locker := newTestLocker(d, metrics)

	err := locker.ExtendLock(context.Background(), "id-1", "lock-value")
	if err == nil {
		t.Fatal("expected 'tried to extend a lock we no longer hold'")
	}
	const want = "tried to extend a lock we no longer hold"
	if err.Error() != want {
		t.Fatalf("\n got: %q\nwant: %q", err.Error(), want)
	}
	if names := metrics.incNames(); !contains(names, "test.lock-extend-error") {
		t.Fatalf("metrics incs: want test.lock-extend-error in %v", names)
	}
}

func TestRedisLockerExtendLockDriverError(t *testing.T) {
	t.Parallel()
	d := &fakeDriver{evalErrs: []error{errors.New("eval: boom")}}
	locker := newTestLocker(d, &recordingMetrics{})
	err := locker.ExtendLock(context.Background(), "id-1", "lock-value")
	if err == nil || err.Error() != "eval: boom" {
		t.Fatalf("driver error propagation: got %v", err)
	}
}

func TestRedisLockerTryLockSuccess(t *testing.T) {
	t.Parallel()
	metrics := &recordingMetrics{}
	d := &fakeDriver{} // default SET ack "OK"
	locker := newTestLocker(d, metrics)

	got, value, err := locker.TryLock(context.Background(), "id-1")
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("expected the lock to be acquired")
	}
	setCall, _ := d.setCall(0)
	if setCall.key != "lock:id-1" {
		t.Fatalf("SET key: got %q want lock:id-1", setCall.key)
	}
	if setCall.ttlSecs != 30 || !setCall.nx {
		t.Fatalf("SET EX/NX: got ttl=%d nx=%v want 30/true", setCall.ttlSecs, setCall.nx)
	}
	// the signed lock-value shape: locked:host=...:pid=...:random=...:time=...:count=N
	if !tokenLockRE.MatchString(value) {
		t.Fatalf("lock value shape: %q", value)
	}
	if value != setCall.value {
		t.Fatalf("returned lockValue must be the one written:\nreturned: %q\nwritten: %q", value, setCall.value)
	}
	if names := metrics.incNames(); !contains(names, "test.lock-not-blocking") {
		t.Fatalf("metrics: want test.lock-not-blocking in %v", names)
	}
}

func TestRedisLockerTryLockContended(t *testing.T) {
	t.Parallel()
	metrics := &recordingMetrics{}
	d := &fakeDriver{setAcks: []string{""}} // NX failed — someone holds it
	locker := newTestLocker(d, metrics)
	got, value, err := locker.TryLock(context.Background(), "id-1")
	if err != nil {
		t.Fatal(err)
	}
	if got || value != "" {
		t.Fatalf("contended TryLock: got (%v, %q) want (false, \"\")", got, value)
	}
	if names := metrics.incNames(); !contains(names, "test.lock-blocking") {
		t.Fatalf("metrics: want test.lock-blocking in %v", names)
	}
}

func TestRedisLockerTryLockOverlongAutoRelease(t *testing.T) {
	t.Parallel()
	old := MaxRedisRequestLength
	MaxRedisRequestLength = 10
	defer func() { MaxRedisRequestLength = old }()

	d := &fakeDriver{setDelays: []time.Duration{50 * time.Millisecond}} // > 10ms budget
	metrics := &recordingMetrics{}
	locker := newTestLocker(d, metrics)

	got, _, err := locker.TryLock(context.Background(), "id-1")
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("overlong SET must be auto-released and reported as not-acquired")
	}
	// the release must have happened via the unlock script
	call, ok := d.evalCall(0)
	if !ok {
		t.Fatal("auto-release EVAL missing")
	}
	if call.script != unlockScript {
		t.Fatalf("auto-release script: got %q", call.script)
	}
}

func TestRedisLockerGetLockPollsUntilAcquired(t *testing.T) {
	t.Parallel()
	d := &fakeDriver{setAcks: []string{"", "OK"}} // contended once, then acquired
	metrics := &recordingMetrics{}
	locker := newTestLocker(d, metrics)

	start := time.Now()
	value, err := locker.GetLock(context.Background(), "id-1")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("GetLock: %v", err)
	}
	if !tokenLockRE.MatchString(value) {
		t.Fatalf("lock value shape: %q", value)
	}
	if n := len(d.setCallsLocked()); n != 2 {
		t.Fatalf("SET attempts: got %d want 2", n)
	}
	if elapsed < 50*time.Millisecond {
		t.Fatalf("poll interval not respected: elapsed %v < 50ms", elapsed)
	}
}

func TestRedisLockerGetLockTimeout(t *testing.T) {
	t.Parallel()
	d := &fakeDriver{setAcks: []string{"", "", ""}} // never acquired
	metrics := &recordingMetrics{}
	locker := newTestLocker(d, metrics)
	locker.MaxLockWaitTime = 120 * time.Millisecond
	locker.LockTestInterval = 20 * time.Millisecond

	var seenID string
	locker.WrapTimeoutError = func(err error, id string) error {
		seenID = id
		return fmt.Errorf("wrapped(%v, %s)", err, id)
	}
	_, err := locker.GetLock(context.Background(), "id-7")
	if err == nil {
		t.Fatal("expected the wrapped timeout error")
	}
	if seenID != "id-7" {
		t.Fatalf("wrapTimeoutError id: got %q", seenID)
	}
}

func TestRedisLockerCheckLock(t *testing.T) {
	t.Parallel()
	metrics := &recordingMetrics{}
	d := &fakeDriver{existsCounts: []int64{1}} // key exists → locked
	locker := newTestLocker(d, metrics)
	free, err := locker.CheckLock(context.Background(), "id-1")
	if err != nil || free {
		t.Fatalf("CheckLock locked case: got (%v, %v)", free, err)
	}
	if names := metrics.incNames(); !contains(names, "test.lock-blocking") {
		t.Fatalf("metrics: %v", names)
	}

	d2 := &fakeDriver{} // exists 0 → free
	locker2 := newTestLocker(d2, &recordingMetrics{})
	free, err = locker2.CheckLock(context.Background(), "id-1")
	if err != nil || !free {
		t.Fatalf("CheckLock free case: got (%v, %v)", free, err)
	}
}

func TestRedisLockerReleaseLock(t *testing.T) {
	t.Parallel()
	d := &fakeDriver{evalResults: []any{int64(1)}}
	locker := newTestLocker(d, &recordingMetrics{})
	result, err := locker.ReleaseLock(context.Background(), "id-1", "lock-value")
	if err != nil || result != 1 {
		t.Fatalf("release: got (%d, %v)", result, err)
	}
	call, _ := d.evalCall(0)
	if call.script != unlockScript {
		t.Fatalf("release script: %q", call.script)
	}
	if !reflectEqual(call.keys, []string{"lock:id-1"}) || !reflectEqual(call.args, []any{"lock-value"}) {
		t.Fatalf("release eval args: keys=%v args=%v", call.keys, call.args)
	}

	// expired lock: eval 0 → exact error
	d2 := &fakeDriver{evalResults: []any{int64(0)}}
	locker2 := newTestLocker(d2, &recordingMetrics{})
	_, err = locker2.ReleaseLock(context.Background(), "id-1", "lock-value")
	if err == nil {
		t.Fatal("expected 'tried to release timed out lock'")
	}
	const want = "tried to release timed out lock"
	if err.Error() != want {
		t.Fatalf("\n got: %q\nwant: %q", err.Error(), want)
	}
}

// helpers shared by the weblocker tests

var tokenLockRE = regexp.MustCompile(`^locked:host=[^:]+:pid=\d+:random=[0-9a-f]{8}:time=\d+:count=\d+$`)

// setCallsLocked / small metric helpers
func (f *fakeDriver) setCallsLocked() []fakeSetCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeSetCall, len(f.setCalls))
	copy(out, f.setCalls)
	return out
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
