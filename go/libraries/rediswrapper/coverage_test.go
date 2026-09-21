package rediswrapper

// coverage_test.go — small focused pins the main suites leave uncovered
// (default no-op seams, constructor error paths, type coercions,
// cancellation).

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNullSeamsAreNoOps(t *testing.T) {
	t.Parallel()
	var m Metrics = NullMetrics{}
	m.Inc("x")
	m.Gauge("g", 1)
	m.NewTimer("t").Done()

	var l Logger = NullLogger{}
	l.Debug(nil, "d")
	l.Warn(nil, "w")
	l.Error(nil, "e")
}

func TestNewRedisLockerDefaultsToNullSeams(t *testing.T) {
	t.Parallel()
	l, err := NewRedisLocker(RedisLockerConfig{
		RClient:          &fakeDriver{},
		GetKey:           func(id string) string { return "lock:" + id },
		WrapTimeoutError: func(err error, id string) error { return err },
		MetricsPrefix:    "p",
		LockTTLSeconds:   60,
	})
	if err != nil {
		t.Fatal(err)
	}
	// driving a null seam must be panic-free
	if _, _, err := l.TryLock(context.Background(), "id"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.CheckLock(context.Background(), "id"); err != nil {
		t.Fatal(err)
	}
}

func TestCreateDriverConstructorError(t *testing.T) {
	t.Parallel()
	ctor := &erroringConstructor{}
	if _, err := CreateClient(nil, ctor); err == nil || err.Error() != "ctor: no dice" {
		t.Fatalf("constructor error: got %v", err)
	}
}

type erroringConstructor struct{ fakeDriver }

func (e *erroringConstructor) Configure(opts map[string]any, cluster any) error {
	return errors.New("ctor: no dice")
}

func TestClientExecMalformedRow(t *testing.T) {
	t.Parallel()
	d := &fakeDriver{}
	d.execRows = [][][]any{{{nil, "v", "extra"}}}
	client := &Client{Driver: d}
	_, err := client.Exec(context.Background(), []Op{{"GET", "a"}})
	if err == nil || err.Error() == "" {
		t.Fatal("malformed multi row should be an error")
	}
}

func TestWebLockerNilSeamsDefaultToNull(t *testing.T) {
	t.Parallel()
	w := NewRedisWebLocker(nil, func(namespace, id string) string { return "k" }, &fakeDriver{}, nil, nil)
	if w.Options.LockTestInterval != 50 || w.Options.RedisLockExpiry != 30 || w.Options.SlowExecutionThreshold != 5000 {
		t.Fatalf("default options: %+v", w.Options)
	}
	if _, err := w.RunWithLock(context.Background(), "ns", "id", func(ctx context.Context) ([]any, error) {
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestIntOptCoercions(t *testing.T) {
	t.Parallel()
	if got := intOpt(map[string]any{"x": int(7)}, "x"); got != 7 {
		t.Fatalf("int: %d", got)
	}
	if got := intOpt(map[string]any{"x": int64(9)}, "x"); got != 9 {
		t.Fatalf("int64: %d", got)
	}
	if got := intOpt(nil, "x"); got != 0 {
		t.Fatalf("nil opts: %d", got)
	}
	if got := intOpt(map[string]any{"x": "seven"}, "x"); got != 0 {
		t.Fatalf("string: %d", got)
	}
}

func TestResultAsIntCoercions(t *testing.T) {
	t.Parallel()
	if got := resultAsInt(int64(1)); got != 1 {
		t.Fatalf("int64: %d", got)
	}
	if got := resultAsInt(int(1)); got != 1 {
		t.Fatalf("int: %d", got)
	}
	if got := resultAsInt(int32(1)); got != 1 {
		t.Fatalf("int32: %d", got)
	}
	if got := resultAsInt(float64(1)); got != 1 {
		t.Fatalf("float64: %d", got)
	}
	if got := resultAsInt("weird"); got != -1 {
		t.Fatalf("non-numeric: %d", got)
	}
	if got := resultAsInt(nil); got != -1 {
		t.Fatalf("nil: %d", got)
	}
}

func TestGetLockCtxCancel(t *testing.T) {
	t.Parallel()
	d := &fakeDriver{setAcks: []string{""}} // always contended
	locker := newTestLocker(d, &recordingMetrics{})
	locker.MaxLockWaitTime = time.Hour
	locker.LockTestInterval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	_, err := locker.GetLock(ctx, "id")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ctx cancel: got %v", err)
	}
}

// TestLockerCountResets is a guard: the process-wide token counter only ever
// grows (Node's COUNT++ is module-monotonic).
func TestLockerCountResets(t *testing.T) {
	t.Parallel()
	d := &fakeDriver{}
	locker := newTestLocker(d, &recordingMetrics{})
	got, v, err := locker.TryLock(context.Background(), "a")
	if err != nil || !got || v == "" {
		t.Fatalf("acquire: got=%v v=%q err=%v", got, v, err)
	}
	c1 := v[len("locked:"):]
	// release so a fresh acquisition is possible on the same key
	if _, err := locker.ReleaseLock(context.Background(), "a", v); err != nil {
		t.Fatal(err)
	}
	got2, v2, err := locker.TryLock(context.Background(), "a")
	if err != nil || !got2 || v2 == "" {
		t.Fatalf("reacquire: got=%v v=%q err=%v", got2, v2, err)
	}
	if c1 == v2[len("locked:"):] {
		t.Fatal("token count must advance between acquisitions")
	}
}
