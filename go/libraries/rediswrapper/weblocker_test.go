package rediswrapper

// weblocker_test.go — the Node RedisWebLocker runWithLock contract
// (runWithLock → _getLock queue → polling → watchdog → runner → release →
// slow-execution check). The Node unit suite does not cover this class
// (no live redis), so per the port policy these pins ARE the acceptance
// spec, mirroring the Node source line-by-line.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// newWebTestLocker builds a RedisWebLocker over a fake driver with the
// short-circuit test knobs (ms units like the Node options object).
func newWebTestLocker(d *fakeDriver, metrics *recordingMetrics, logger *recordingLogger) *RedisWebLocker {
	return NewRedisWebLocker(map[string]any{
		"lock_test_interval":       10,
		"max_test_interval":        50,
		"max_lock_wait_time":       300,
		"redis_lock_expiry":        1,
		"slow_execution_threshold": 100,
	}, func(namespace, id string) string {
		return "lock:" + namespace + ":" + id
	}, d, metrics, logger)
}

func TestWebLockerRunWithLockSuccess(t *testing.T) {
	t.Parallel()
	metrics := &recordingMetrics{}
	logger := &recordingLogger{}
	d := &fakeDriver{} // SET → "OK"; EVAL → 1
	w := newWebTestLocker(d, metrics, logger)

	values, err := w.RunWithLock(context.Background(), "proj", "op-1", func(ctx context.Context) ([]any, error) {
		return []any{"result-1", "result-2"}, nil
	})
	if err != nil {
		t.Fatalf("RunWithLock: %v", err)
	}
	if !reflectEqual(values, []any{"result-1", "result-2"}) {
		t.Fatalf("runner values: got %v", values)
	}
	// ordering: SET (acquire) → runner → EVAL (release)
	kinds := d.seqKinds()
	if !reflectEqual(kinds, []string{"s", "e"}) {
		t.Fatalf("driver call order: got %v want [set eval]", kinds)
	}
	// the SET must carry the Node-pinned EX REDIS_LOCK_EXPIRY*1000 quirk
	// (redis_lock_expiry=1 in test opts → EX 1000).
	setCall, _ := d.setCall(0)
	if setCall.key != "lock:proj:op-1" {
		t.Fatalf("SET key: got %q", setCall.key)
	}
	if setCall.ttlSecs != 1000 {
		t.Fatalf("SET EX: got %d want 1000 (Node EX REDIS_LOCK_EXPIRY*1000)", setCall.ttlSecs)
	}
	if !setCall.nx {
		t.Fatal("SET must be NX")
	}
	if evalCall, ok := d.evalCall(0); !ok || evalCall.script != unlockScript {
		t.Fatalf("release eval: %+v", d)
	}
	if len(metrics.timers) != 1 || metrics.timers[0] != "lock.proj" {
		t.Fatalf("timer name: got %v want [lock.proj]", metrics.timers)
	}
}

func TestWebLockerRunWithLockRunnerError(t *testing.T) {
	t.Parallel()
	d := &fakeDriver{} // release still succeeds (eval 1)
	w := newWebTestLocker(d, &recordingMetrics{}, &recordingLogger{})
	_, err := w.RunWithLock(context.Background(), "proj", "op-2", func(ctx context.Context) ([]any, error) {
		return nil, errors.New("runner exploded")
	})
	if err == nil || err.Error() != "runner exploded" {
		t.Fatalf("runner error must propagate: %v", err)
	}
	// release must still have run
	if _, ok := d.evalCall(0); !ok {
		t.Fatal("release EVAL must run even when the runner errors")
	}
}

func TestWebLockerReleaseFailureMasksNothingButWins(t *testing.T) {
	t.Parallel()
	d := &fakeDriver{evalResults: []any{int64(0)}} // unlock failed
	logger := &recordingLogger{}
	w := newWebTestLocker(d, &recordingMetrics{}, logger)
	_, err := w.RunWithLock(context.Background(), "proj", "op-3", func(ctx context.Context) ([]any, error) {
		return []any{"ok"}, nil
	})
	if err == nil {
		t.Fatal("expected the release failure")
	}
	const want = "tried to release timed out lock"
	if err.Error() != want {
		t.Fatalf("\n got: %q\nwant: %q", err.Error(), want)
	}
	if !containsMsg(logger.messages(), "warn unlock lock error") {
		t.Fatalf("logger: %v", logger.messages())
	}
}

func TestWebLockerRunWithLockAcquireTimeout(t *testing.T) {
	t.Parallel()
	d := &fakeDriver{setAcks: []string{"", "", "", ""}} // always contended
	metrics := &recordingMetrics{}
	w := newWebTestLocker(d, metrics, &recordingLogger{})
	start := time.Now()
	_, err := w.RunWithLock(context.Background(), "proj", "op-4", func(ctx context.Context) ([]any, error) {
		t.Fatal("runner must not run when the lock is never acquired")
		return nil, nil
	})
	if err == nil || err.Error() != "Timeout" {
		t.Fatalf("acquire timeout: got %v", err)
	}
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Fatalf("should have polled until max_lock_wait_time: elapsed %v", elapsed)
	}
	if names := metrics.incNames(); !contains(names, "lock.proj.get.failed") {
		t.Fatalf("metrics: %v", names)
	}
}

func TestWebLockerSerializerizesPerKey(t *testing.T) {
	t.Parallel()
	// Two concurrent runWithLock calls on the SAME key must not both hold
	// the lock: the second one's runner starts only after the first has
	// released (the Node LOCK_QUEUES FIFO contract).
	d := &fakeDriver{} // every SET NX → "OK" (fake is permissive)
	metrics := &recordingMetrics{}
	logger := &recordingLogger{}
	w := newWebTestLocker(d, metrics, logger)

	var runSeq []string
	var seqMu sync.Mutex
	mkRunner := func(label string, hold time.Duration) func(context.Context) ([]any, error) {
		return func(ctx context.Context) ([]any, error) {
			seqMu.Lock()
			runSeq = append(runSeq, label+"-start")
			seqMu.Unlock()
			time.Sleep(hold) // hold the lock
			seqMu.Lock()
			runSeq = append(runSeq, label+"-end")
			seqMu.Unlock()
			return []any{label}, nil
		}
	}

	var wg sync.WaitGroup
	for i, label := range []string{"a", "b"} {
		wg.Add(1)
		go func(label string, hold time.Duration) {
			defer wg.Done()
			_, err := w.RunWithLock(context.Background(), "proj", "same-key", mkRunner(label, hold))
			if err != nil {
				t.Errorf("runner %s: %v", label, err)
			}
		}(label, time.Duration(60+30*i)*time.Millisecond)
	}
	wg.Wait()

	// serialization: neither runner may interleave with the other (FIFO
	// after both are enqueued; the enqueuing order itself is scheduler
	// dependent, like the Node event-loop push).
	valid := reflectEqual(runSeq, []string{"a-start", "a-end", "b-start", "b-end"}) ||
		reflectEqual(runSeq, []string{"b-start", "b-end", "a-start", "a-end"})
	if !valid {
		t.Fatalf("serialization order: got %v (want a and b runners non-overlapping)", runSeq)
	}
}

func TestWebLockerQueuesSizeDrainsToZero(t *testing.T) {
	t.Parallel()
	d := &fakeDriver{}
	w := newWebTestLocker(d, &recordingMetrics{}, &recordingLogger{})
	if _, err := w.RunWithLock(context.Background(), "proj", "solo", func(ctx context.Context) ([]any, error) {
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	// the queue must drain (Node: removeQueue on drain).
	deadline := time.Now().Add(2 * time.Second)
	for {
		if LockQueuesSize() == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("queues did not drain: size %d", LockQueuesSize())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestWebLockerExceedsLockTimeoutWatchdog(t *testing.T) {
	t.Parallel()
	d := &fakeDriver{}
	metrics := &recordingMetrics{}
	logger := &recordingLogger{}
	w := newWebTestLocker(d, metrics, logger)

	// redis_lock_expiry=1s (test opts) → the watchdog fires after 1s.
	_, err := w.RunWithLock(context.Background(), "proj", "slow", func(ctx context.Context) ([]any, error) {
		time.Sleep(1150 * time.Millisecond) // outlive the lock budget
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if names := metrics.incNames(); !contains(names, "lock.proj.exceeded_lock_timeout") {
		t.Fatalf("watchdog metric: got %v", names)
	}
	if !containsMsg(logger.messages(), "debug exceeded lock timeout") {
		t.Fatalf("logger: %v", logger.messages())
	}
}

func TestWebLockerSlowExecutionLogged(t *testing.T) {
	t.Parallel()
	d := &fakeDriver{}
	metrics := &recordingMetrics{}
	logger := &recordingLogger{}
	w := newWebTestLocker(d, metrics, logger) // slow_execution_threshold=100ms

	_, err := w.RunWithLock(context.Background(), "proj", "laggy", func(ctx context.Context) ([]any, error) {
		time.Sleep(150 * time.Millisecond)
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsMsg(logger.messages(), "debug slow execution during lock") {
		t.Fatalf("logger: %v", logger.messages())
	}
}

func containsMsg(messages []string, want string) bool {
	for _, m := range messages {
		if m == want {
			return true
		}
	}
	return false
}
