package lockmanager

import (
	"errors"
	"testing"
	"time"

	"ollitex/go/services/project-history/internal/redisx"
)

type store struct{ str map[string]string }

func (s *store) Get(k string) (string, bool, error) { v, ok := s.str[k]; return v, ok, nil }
func (s *store) Set(k, v string, ttl ...int) error  { s.str[k] = v; return nil }
func (s *store) SetNX(k, v string) (bool, error) {
	if _, ok := s.str[k]; ok {
		return false, nil
	}
	s.str[k] = v
	return true, nil
}
func (s *store) SetNXWithTTL(k, v string, ttl int) (bool, error) {
	if _, ok := s.str[k]; ok {
		return false, nil
	}
	s.str[k] = v
	return true, nil
}
func (s *store) Del(keys ...string) (int64, error) {
	var n int64
	for _, k := range keys {
		if _, ok := s.str[k]; ok {
			delete(s.str, k)
			n++
		}
	}
	return n, nil
}
func (s *store) Exists(keys ...string) (int, error) {
	n := 0
	for _, k := range keys {
		if _, ok := s.str[k]; ok {
			n++
		}
	}
	return n, nil
}
func (s *store) Expire(k string, ttl int) error                     { return nil }
func (s *store) LRange(k string, start, stop int) ([]string, error) { return nil, nil }
func (s *store) LRem(k string, n int, v string) (int64, error)      { return 0, nil }
func (s *store) LLen(k string) (int64, error)                       { return 0, nil }
func (s *store) Scan(p string, limit int) ([]string, error)         { return nil, nil }
func (s *store) MGet(keys ...string) ([]string, error) {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = s.str[k]
	}
	return out, nil
}
func (s *store) Ping() error  { return nil }
func (s *store) Close() error { return nil }

var _ redisx.Client = (*store)(nil)

func TestTryLockAndRelease(t *testing.T) {
	s := &store{str: map[string]string{}}
	lm := New(s)
	value, got, err := lm.TryLock("lock:1")
	if err != nil || !got {
		t.Fatalf("want acquired, got %v err=%v", got, err)
	}
	if s.str["lock:1"] != value {
		t.Fatalf("want value stored")
	}
	// second attempt fails (same key)
	if _, got, err := lm.TryLock("lock:1"); err != nil || got {
		t.Fatalf("want not acquired on 2nd, got %v", got)
	}
	// release
	if err := lm.Release("lock:1", value); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, ok := s.str["lock:1"]; ok {
		t.Fatalf("want lock gone")
	}
}

func TestReleaseWrongValueNoop(t *testing.T) {
	s := &store{str: map[string]string{}}
	lm := New(s)
	v, _, err := lm.TryLock("k")
	if err != nil {
		t.Fatalf("try: %v", err)
	}
	_ = v
	// release with a different value should be a no-op
	if err := lm.Release("k", "not-my-value"); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, ok := s.str["k"]; !ok {
		t.Fatalf("lock should still be held — wrong-value release must not delete")
	}
}

func TestExtend(t *testing.T) {
	s := &store{str: map[string]string{}}
	lm := New(s)
	v, got, err := lm.TryLock("k")
	if err != nil || !got {
		t.Fatalf("want acquired err=%v", err)
	}
	if err := lm.Extend("k", v); err != nil {
		t.Fatalf("extend: %v", err)
	}
	// extend with wrong value fails
	if err := lm.Extend("k", "wrong"); err == nil {
		t.Fatalf("want extend error for wrong value")
	}
}

func TestRunWithRunner(t *testing.T) {
	s := &store{str: map[string]string{}}
	lm := New(s)
	gotExtend := false
	err := lm.RunWithLock("k", func(extend Extend) error {
		gotExtend = true
		return nil
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !gotExtend {
		t.Fatalf("extend not wired")
	}
	// after run, lock should be released
	if _, ok := s.str["k"]; ok {
		t.Fatalf("want lock released after run")
	}
}

func TestCheckLock(t *testing.T) {
	s := &store{str: map[string]string{}}
	lm := New(s)
	free, err := lm.CheckLock("k")
	if err != nil || !free {
		t.Fatalf("want free, got %v err=%v", free, err)
	}
	s.str["k"] = "held"
	free, err = lm.CheckLock("k")
	if err != nil || free {
		t.Fatalf("want held, got free=%v", free)
	}
}

// vendor: runWithLock(key, runner, callback) — the third-argument "done"
// callback receives (error, ...releaseArgs); lock acquisition failure passes
// the tagged error to done with no args; the release error is used only when
// the inner callback had none.
func TestRunWithLockDone(t *testing.T) {
	t.Run("success forwards release args", func(t *testing.T) {
		s := &store{str: map[string]string{}}
		lm := New(s)
		var doneErr error
		var doneArgs []any
		lm.RunWithLockDone("k", func(extend Extend, release func(error, ...any) error) {
			release(nil, "a", 1)
		}, func(err error, args ...any) {
			doneErr, doneArgs = err, args
		})
		if doneErr != nil {
			t.Fatalf("want nil, got %v", doneErr)
		}
		if len(doneArgs) != 2 || doneArgs[0] != "a" {
			t.Fatalf("want release args forwarded, got %v", doneArgs)
		}
		if _, ok := s.str["k"]; ok {
			t.Fatalf("lock should be released")
		}
	})
	t.Run("inner error preserved", func(t *testing.T) {
		s := &store{str: map[string]string{}}
		lm := New(s)
		inner := errors.New("flush failed")
		var doneErr error
		lm.RunWithLockDone("k", func(extend Extend, release func(error, ...any) error) {
			release(inner, map[string]int{"queueSize": 3})
		}, func(err error, args ...any) {
			doneErr = err
		})
		if doneErr != inner {
			t.Fatalf("want inner error, got %v", doneErr)
		}
	})
	t.Run("getlock failure -> done(err) no args", func(t *testing.T) {
		s := &store{str: map[string]string{}}
		lm := New(s)
		// hold the lock so GetLock times out (use a tiny maxWait)
		lm.maxWait = 1 * time.Millisecond
		s.str["k"] = "held-by-other"
		called := 0
		lm.RunWithLockDone("k", nil, func(err error, args ...any) {
			called++
			if err == nil {
				t.Fatalf("want error")
			}
		})
		if called != 1 {
			t.Fatalf("done should be called once, got %d", called)
		}
	})
}
