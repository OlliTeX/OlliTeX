package rediswrapper

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// RedisWebLocker mirrors libraries/redis-wrapper/RedisWebLocker.js 1:1.
//
// Unlike RedisLocker (which the caller drives acquire/extend/release), the
// web locker exposes ONE call — runWithLock({namespace, id}, runner) — and
// handles the whole lifecycle:
//
//  1. _getLock: acquire the lock through a per-key FIFO queue (only ONE
//     lock-attempt per key runs concurrently in-process — Node
//     async.queue(concurrency 1)); the caller's callback is the queue's
//     next() continuation, so the second waiter starts polling before the
//     first runner is done (Node: queue.next() is called in the lock
//     attempt's callback, BEFORE that callback).
//  2. the lock-attempt itself polls _tryLock every lock_test_interval (fixed
//     50ms here — no backoff, unlike RedisLocker) for up to
//     max_lock_wait_time.
//  3. runner runs under a lock-expiry watchdog (REDIS_LOCK_EXPIRY seconds →
//     countIfExceededLockTimeout) and a slow-execution check
//     (SLOW_EXECUTION_THRESHOLD).
//  4. release happens after the runner settles (release errors do not mask
//     runner successes: Node error1||error2).
//
// LOCK_QUEUES is module-scope in Node (shared by every RedisWebLocker
// instance) — mirrored here as package state (lockQueues).

// WebLockerOptions mirrors the Node constructor options object (ms units
// unless noted; defaults are the Node module constants).
type WebLockerOptions struct {
	LockTestInterval       int // ms — Node LOCK_TEST_INTERVAL=50
	MaxTestInterval        int // ms — Node MAX_TEST_INTERVAL=1000
	MaxLockWaitTime        int // ms — Node MAX_LOCK_WAIT_TIME=10000
	RedisLockExpiry        int // s  — Node REDIS_LOCK_EXPIRY=30
	SlowExecutionThreshold int // ms — Node SLOW_EXECUTION_THRESHOLD=5000
}

func (o WebLockerOptions) withDefaults() WebLockerOptions {
	if o.LockTestInterval == 0 {
		o.LockTestInterval = 50
	}
	if o.MaxTestInterval == 0 {
		o.MaxTestInterval = 1000
	}
	if o.MaxLockWaitTime == 0 {
		o.MaxLockWaitTime = 10000
	}
	if o.RedisLockExpiry == 0 {
		o.RedisLockExpiry = 30
	}
	if o.SlowExecutionThreshold == 0 {
		o.SlowExecutionThreshold = 5000
	}
	return o
}

// slowExecutionError mirrors the Node `new Error('slow execution during
// lock')` sentinel (created on every runWithLock for potential logging).
var slowExecutionError = errors.New("slow execution during lock")

// RedisWebLocker mirrors class RedisWebLocker.
type RedisWebLocker struct {
	Driver       Driver
	GetKey       func(namespace, id string) string
	Options      WebLockerOptions
	Metrics      Metrics
	Logger       Logger
	UnlockScript string
}

// NewRedisWebLocker mirrors the Node constructor (option defaults + the
// shared LOCK_QUEUES reference).
func NewRedisWebLocker(opts map[string]any, getKey func(namespace, id string) string, driver Driver, metrics Metrics, logger Logger) *RedisWebLocker {
	if metrics == nil {
		metrics = NullMetrics{}
	}
	if logger == nil {
		logger = NullLogger{}
	}
	o := WebLockerOptions{
		LockTestInterval:       intOpt(opts, "lock_test_interval"),
		MaxTestInterval:        intOpt(opts, "max_test_interval"),
		MaxLockWaitTime:        intOpt(opts, "max_lock_wait_time"),
		RedisLockExpiry:        intOpt(opts, "redis_lock_expiry"),
		SlowExecutionThreshold: intOpt(opts, "slow_execution_threshold"),
	}.withDefaults()
	return &RedisWebLocker{
		Driver:       driver,
		GetKey:       getKey,
		Options:      o,
		Metrics:      metrics,
		Logger:       logger,
		UnlockScript: unlockScript, // Node: this.unlockScript = unlockScript
	}
}

func intOpt(opts map[string]any, key string) int {
	if opts == nil {
		return 0
	}
	switch v := opts[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	}
	return 0
}

// --- module-scope lock queue state (Node LOCK_QUEUES) ---------------------

var (
	lockQueuesMu sync.Mutex
	lockQueues   = map[string]*webLockQueue{}
)

// webLockQueue is the Node `async.queue(handler, {concurrency: 1})` per-key
// FIFO, modeled on async.queue's internals: a task list + one running flag;
// `next()` (called from inside each task's completion, before that task
// resolves — the Node pin) advances to the next task; when the list empties
// the queue drains and is removed from the registry (Node
// `queue.drain(() => delete LOCK_QUEUES[key])`).
type webLockQueue struct {
	mu       sync.Mutex
	tasks    []webLockTask
	running  bool
	ownerKey string
}

type webLockTask func(next func())

func newWebLockQueue(key string) *webLockQueue {
	return &webLockQueue{ownerKey: key}
}

// push appends a task and starts processing if the queue is idle
// (async.queue.push).
func (q *webLockQueue) push(task webLockTask) {
	q.mu.Lock()
	q.tasks = append(q.tasks, task)
	shouldRun := !q.running
	q.running = true
	q.mu.Unlock()
	if shouldRun {
		q.runNext()
	}
}

// runNext pops the next task (async.queue processing one at a time).
func (q *webLockQueue) runNext() {
	q.mu.Lock()
	if len(q.tasks) == 0 {
		q.running = false
		q.mu.Unlock()
		q.signalDrain()
		return
	}
	task := q.tasks[0]
	q.tasks = q.tasks[1:]
	q.mu.Unlock()
	task(q.next)
}

// next releases the concurrency slot to the next task (Node task calls
// queue.next() from inside its completion, before resolving the caller —
// so the next waiter starts polling before the previous runner settles).
func (q *webLockQueue) next() { q.runNext() }

// signalDrain mirrors the Node drain callback that removes the queue from
// LOCK_QUEUES (only ever fires when the queue is fully idle, so a key never
// has two concurrent in-flight queues).
func (q *webLockQueue) signalDrain() {
	lockQueuesMu.Lock()
	if lockQueues[q.ownerKey] == q {
		delete(lockQueues, q.ownerKey)
	}
	lockQueuesMu.Unlock()
}

// pushQueueTask mirrors Node `queue(key, task)` in _getLock: reuse the key's
// queue if live, create it otherwise, then enqueue.
func pushQueueTask(key string, task webLockTask) {
	lockQueuesMu.Lock()
	q, ok := lockQueues[key]
	if !ok {
		q = newWebLockQueue(key)
		lockQueues[key] = q
	}
	lockQueuesMu.Unlock()
	q.push(task)
}

// LockQueuesSize mirrors Node `_lockQueuesSize()`.
func LockQueuesSize() int {
	lockQueuesMu.Lock()
	defer lockQueuesMu.Unlock()
	return len(lockQueues)
}

// _getLock mirrors Node _getLock(key, namespace, callback): enqueue the
// attempt for the key; the task, when polled-done (success OR failure),
// invokes its continuation (queue.next()) BEFORE resolving the caller — the
// pin-relevant ordering.
func (w *RedisWebLocker) getLock(ctx context.Context, key, namespace string) (string, error) {
	type lockResult struct {
		value string
		err   error
	}
	resultCh := make(chan lockResult, 1)

	task := func(next func()) {
		w.getLockByPolling(ctx, key, namespace, func(err error, value string) {
			next() // Node: queue.next() BEFORE the outer callback
			resultCh <- lockResult{value: value, err: err}
		})
	}
	pushQueueTask(key, task)

	select {
	case r := <-resultCh:
		return r.value, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// _getLockByPolling mirrors Node _getLockByPolling — fixed-interval polling
// (no backoff) until acquired or maxLockWaitTime elapses.
func (w *RedisWebLocker) getLockByPolling(ctx context.Context, key, namespace string, cb func(err error, value string)) {
	start := time.Now()
	attempts := 0

	var attempt func()
	attempt = func() {
		// Node checks the timeout BEFORE each attempt.
		if time.Since(start) > time.Duration(w.Options.MaxLockWaitTime)*time.Millisecond {
			w.Metrics.Inc("lock." + namespace + ".get.failed")
			cb(errors.New("Timeout"), "")
			return
		}
		attempts++

		got, value, err := w.tryLock(ctx, key, namespace)
		if err == nil && got {
			w.Metrics.Gauge("lock."+namespace+".get.success.tries", float64(attempts))
			cb(nil, value)
			return
		}
		if err != nil {
			w.Metrics.Inc("lock." + namespace + ".get.failed")
			cb(err, "")
			return
		}

		go func() {
			select {
			case <-time.After(time.Duration(w.Options.LockTestInterval) * time.Millisecond):
			case <-ctx.Done():
				return
			}
			attempt()
		}()
	}
	attempt()
}

// _tryLock mirrors Node _tryLock(key, namespace, callback):
// SET key lockValue EX redis_lock_expiry*... NX — success → 'OK'.
func (w *RedisWebLocker) tryLock(ctx context.Context, key, namespace string) (bool, string, error) {
	lockValue := randomLockToken()
	// Node quirk, pinned 1:1: the web locker sets `EX REDIS_LOCK_EXPIRY * 1000`
	// (EX is in seconds — so the redis-side TTL is REDIS_LOCK_EXPIRY*1000
	// SECONDS, i.e. the Node `* 1000` reads as ms but EX is not ms). The
	// client-side lock-exceeded watchdog fires after REDIS_LOCK_EXPIRY
	// SECONDS regardless (see the RunWithLock watchdog below).
	ttlSeconds := w.Options.RedisLockExpiry * 1000

	ack, err := w.Driver.SetEx(ctx, key, lockValue, ttlSeconds, true)
	if err != nil {
		return false, "", err
	}
	if ack == "OK" {
		w.Metrics.Inc("lock." + namespace + ".try.success")
		return true, lockValue, nil
	}
	w.Metrics.Inc("lock." + namespace + ".try.failed")
	w.Logger.Debug(map[string]any{"namespace": namespace, "key": key},
		"failed to acquire redis lock")
	return false, "", nil
}

// _releaseLock mirrors Node _releaseLock(key, value, callback): EVAL
// unlockScript; result !== 1 → the lock expired / was released already.
func (w *RedisWebLocker) releaseLock(ctx context.Context, key, lockValue string) (int64, error) {
	result, err := w.Driver.Eval(ctx, w.UnlockScript, []string{key}, []any{lockValue})
	if err != nil {
		return 0, err
	}
	if resultAsInt(result) != 1 {
		w.Logger.Warn(map[string]any{"key": key, "lockValue": lockValue},
			"unlock lock error")
		return 0, errors.New("tried to release timed out lock")
	}
	return resultAsInt(result), nil
}

// RunWithLock mirrors the Node promisified `runWithLock({namespace, id},
// runner, metrics)`:
//
//	timer = new metrics.Timer('lock.' + namespace)
//	key = getKey(namespace, id)
//	_getLock(key, namespace, (error, lockValue) => {
//	  if (error) return callback(error)
//	  const exceededLockTimeout = setTimeout(countIfExceededLockTimeout, REDIS_LOCK_EXPIRY * 1000)
//	  runner((error1, ...values) => {
//	    _releaseLock(key, lockValue, error2 => {
//	      clearTimeout(exceededLockTimeout)
//	      const timeTaken = Date.now() - timer.start
//	      if (timeTaken > SLOW_EXECUTION_THRESHOLD) logger.debug('slow execution during lock', {...})
//	      timer.done()
//	      const error = error1 || error2
//	      if (error) callback(error) else callback(null, ...values)
//	    })
//	  })
//	})
//
// Go mapping: runner is a blocking func returning (values, error)
// (Node callback(error, ...values) → ([]any, error)); runner errors do not
// block release, and release errors do not mask runner values (the first
// non-nil error wins, exactly like Node `error1 || error2`).
func (w *RedisWebLocker) RunWithLock(ctx context.Context, namespace, id string, runner func(context.Context) ([]any, error)) ([]any, error) {
	timer := w.Metrics.NewTimer("lock." + namespace)
	// Node: `new Date() - timer.start` — timer.start is at construction,
	// BEFORE _getLock (so lock-wait time counts toward slow execution).
	timeTakenStart := time.Now()
	key := w.GetKey(namespace, id)

	lockValue, err := w.getLock(ctx, key, namespace)
	if err != nil {
		return nil, err
	}

	// lock-expiry watchdog (Node countIfExceededLockTimeout).
	stopWatchdog := make(chan struct{})
	go func() {
		select {
		case <-time.After(time.Duration(w.Options.RedisLockExpiry) * time.Millisecond * 1000):
			w.Metrics.Inc("lock." + namespace + ".exceeded_lock_timeout")
			w.Logger.Debug(map[string]any{"namespace": namespace, "id": id},
				"exceeded lock timeout")
		case <-stopWatchdog:
			return
		}
	}()

	values, runnerErr := runner(ctx)
	if _, relErr := w.releaseLock(ctx, key, lockValue); relErr != nil {
		w.Logger.Warn(map[string]any{"key": key, "lockValue": lockValue},
			"unlock lock error")
		// Node: `const error = error1 || error2` — the runner's error wins;
		// the release error surfaces only when the runner succeeded.
		if runnerErr == nil {
			runnerErr = relErr
		}
	}
	close(stopWatchdog)

	timeTaken := time.Since(timeTakenStart)
	if timeTaken > time.Duration(w.Options.SlowExecutionThreshold)*time.Millisecond {
		w.Logger.Debug(map[string]any{
			"namespace":          namespace,
			"id":                 id,
			"slowExecutionError": slowExecutionError,
			"timeTaken":          int64(timeTaken / time.Millisecond),
		}, "slow execution during lock")
	}
	timer.Done()

	if runnerErr != nil {
		return nil, runnerErr
	}
	return values, nil
}

// fmt is used by the logger info helpers (kept as an import for the
// info-map formatting parity).
var _ = fmt.Sprintf
