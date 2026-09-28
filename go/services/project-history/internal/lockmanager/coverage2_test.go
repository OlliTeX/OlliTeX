package lockmanager

import (
	"errors"
	"testing"
	"time"
)

// fakeRedis — the redisx.Client surface for the lock paths.
type fakeRedis struct {
	keys        map[string]string
	setErr      error
	existN      int
	existErr    error
	expired     bool
	getErr      error
	ExtendCalls int
}

func newFakeRedis() *fakeRedis {
	return &fakeRedis{keys: map[string]string{}}
}

func (f *fakeRedis) Get(k string) (string, bool, error) {
	if f.getErr != nil {
		return "", false, f.getErr
	}
	v, ok := f.keys[k]
	return v, ok, nil
}

func (f *fakeRedis) Set(k, v string, ttl ...int) error {
	f.keys[k] = v
	return nil
}

func (f *fakeRedis) SetNX(k, v string) (bool, error) {
	if _, ok := f.keys[k]; ok {
		return false, nil
	}
	f.keys[k] = v
	return true, nil
}

func (f *fakeRedis) SetNXWithTTL(k, v string, ttl int) (bool, error) {
	if f.setErr != nil {
		return false, f.setErr
	}
	if _, ok := f.keys[k]; ok {
		return false, nil
	}
	f.keys[k] = v
	return true, nil
}

func (f *fakeRedis) Del(keys ...string) (int64, error) {
	n := int64(0)
	for _, k := range keys {
		if _, ok := f.keys[k]; ok {
			delete(f.keys, k)
			n++
		}
	}
	return n, nil
}

func (f *fakeRedis) Exists(keys ...string) (int, error) {
	if f.existErr != nil {
		return 0, f.existErr
	}
	return f.existN, nil
}

func (f *fakeRedis) Expire(k string, ttl int) error {
	f.ExtendCalls++
	return nil
}
func (f *fakeRedis) LRange(k string, a, b int) ([]string, error) {
	return nil, nil
}
func (f *fakeRedis) LRem(k string, n int, v string) (int64, error)  { return 0, nil }
func (f *fakeRedis) LLen(k string) (int64, error)                   { return 0, nil }
func (f *fakeRedis) Scan(match string, limit int) ([]string, error) { return nil, nil }
func (f *fakeRedis) MGet(keys ...string) ([]string, error)          { return nil, nil }
func (f *fakeRedis) Ping() error                                    { return nil }
func (f *fakeRedis) Close() error                                   { return nil }

func TestTryLockPaths(t *testing.T) {
	fr := newFakeRedis()
	lm := New(fr)
	// free lock → value + ok
	v, ok, err := lm.TryLock("k")
	if err != nil || !ok || v == "" {
		t.Fatalf("acquire: ok=%v v=%q err=%v", ok, v, err)
	}
	// held lock → not acquired
	_, ok2, err := lm.TryLock("k")
	if err != nil || ok2 {
		t.Fatalf("second acquire: ok=%v", ok2)
	}
	// redis error path
	fr.setErr = errors.New("down")
	if _, _, err := lm.TryLock("x"); err == nil {
		t.Fatal("want redis error")
	}
}

func TestCheckLockAndExtendRelease(t *testing.T) {
	fr := newFakeRedis()
	lm := New(fr)
	fr.keys["held"] = "v"
	fr.existN = 1
	free, err := lm.CheckLock("held")
	if err != nil || free {
		t.Fatalf("held must be locked: free=%v err=%v", free, err)
	}
	// Exists error propagation
	fr.existErr = errors.New("boom")
	if _, err := lm.CheckLock("held"); err == nil {
		t.Fatal("want propagation")
	}
	fr.existErr = nil
	fr.keys["free"] = "v"
	// GetLock on a fresh key → value.
	val, err := lm.GetLock("acquired")
	if err != nil || val == "" {
		t.Fatalf("GetLock: %v %q", err, val)
	}
	// GetLock on a HELD key with a short wait → timeout (vendor
	// MAX_LOCK_WAIT_TIME exhaustion).
	lm.maxWait = 200 * time.Microsecond
	if _, err := lm.GetLock("held"); err == nil {
		t.Fatal("want GetLock timeout on held key")
	}
	lm.maxWait = MaxLockWaitTime
	// RunWithLock: acquire → runner(extend) → release.
	done := false
	if err := lm.RunWithLock("withlock", func(ext Extend) error {
		done = true
		// fresh lock: extension within MinLockExtensionInterval is a no-op.
		return ext()
	}); err != nil {
		t.Fatal(err)
	}
	if !done {
		t.Fatal("runner not invoked")
	}
	if _, ok := fr.keys["withlock"]; ok {
		t.Fatal("lock not released after RunWithLock")
	}
	// HealthCheck: acquire + release a synthetic key
	if err := lm.HealthCheck(); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
}

func TestGetLockTimeoutAndError(t *testing.T) {
	fr := newFakeRedis()
	lm := New(fr)
	fr.keys["busy"] = "other"
	lm.maxWait = 500 * time.Microsecond
	_, err := lm.GetLock("busy")
	if err == nil {
		t.Fatal("want timeout error")
	}
	// redis down
	fr.setErr = errors.New("down")
	if _, err := lm.GetLock("whatever"); err == nil {
		t.Fatal("want redis error")
	}
}

func TestLockReleaseErrorAndValueMismatch(t *testing.T) {
	fr := newFakeRedis()
	lm := New(fr)
	lm.RunWithLock("a", func(ext Extend) error { return errors.New("runner fail") })
	// a wrong-value release must NOT delete the lock
	fr.keys["b"] = "someone-else"
	l := lm.lock("b", "my-value")
	_ = l.Release()
	if _, ok := fr.keys["b"]; !ok {
		t.Fatal("wrong-value release must not delete the held lock")
	}
}

func TestErrorAndReleaseExtendEdges(t *testing.T) {
	// LockTimeoutError message
	e := &LockTimeoutError{Key: "k"}
	if e.Error() != "Timeout: k" {
		t.Fatalf("want 'Timeout: k', got %q", e.Error())
	}
	fr := newFakeRedis()
	lm := New(fr)
	// Release: Get-error propagation.
	fr.getErr = errors.New("boom")
	if err := lm.Release("x", "v"); err == nil {
		t.Fatal("want Get error propagation")
	}
	fr.getErr = nil
	fr.keys["m"] = "holder"
	// Extend mismatch → vendor 'failed to extend lock'.
	if err := lm.Extend("m", "someone-else"); err == nil || err.Error() != "failed to extend lock" {
		t.Fatalf("want failed-to-extend, got %v", err)
	}
	// Extend happy path: value matches → TTL re-set.
	fr.ExtendCalls = 0
	if err := lm.Extend("m", "holder"); err != nil {
		t.Fatalf("extend mismatch: %v", err)
	}
	if fr.ExtendCalls != 1 {
		t.Fatalf("want one Expire call, got %d", fr.ExtendCalls)
	}
}
