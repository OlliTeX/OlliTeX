package rediswrapper

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"
)

// --- test doubles ----------------------------------------------------------

// recordingMetrics / recordingLogger implement the Metrics/Logger seams with
// recorded call lists, mirroring the Node sinon-stubbed metrics/logger
// objects (the oracle asserts the inc() names).
type recordingMetrics struct {
	mu     sync.Mutex
	incs   []string
	gauges []struct {
		name  string
		value float64
	}
	timers []string
}

func (m *recordingMetrics) Inc(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inc(safeCopy(name))
}

func safeCopy(s string) string { return s }

func (m *recordingMetrics) inc(s string) { m.incs = append(m.incs, s) }

func (m *recordingMetrics) Gauge(name string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gauges = append(m.gauges, struct {
		name  string
		value float64
	}{name, value})
}

func (m *recordingMetrics) NewTimer(name string) Timer {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.timers = append(m.timers, name)
	return NullTimer{}
}

func (m *recordingMetrics) incNames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.incs))
	copy(out, m.incs)
	return out
}

type callRecord struct {
	Kind string // "set" | "exists" | "eval" | "exec" | "flushall"
	Info string
}

type recordingLogger struct {
	mu      sync.Mutex
	records []struct {
		level string
		info  map[string]any
		msg   string
	}
}

func (l *recordingLogger) add(level string, info map[string]any, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, struct {
		level string
		info  map[string]any
		msg   string
	}{level, info, msg})
}

func (l *recordingLogger) Debug(info map[string]any, msg string) { l.add("debug", info, msg) }
func (l *recordingLogger) Warn(info map[string]any, msg string)  { l.add("warn", info, msg) }
func (l *recordingLogger) Error(info map[string]any, msg string) { l.add("error", info, msg) }

func (l *recordingLogger) messages() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.records))
	for _, r := range l.records {
		out = append(out, r.level+" "+r.msg)
	}
	return out
}

// fakeDriver is the scripted ioredis-shaped fake. Per-command response lists
// are indexed by call count (default when exhausted as noted per command).
type fakeDriver struct {
	mu sync.Mutex

	// constructor (Node: new Redis(opts) / new Redis.Cluster(nodes, opts))
	configured       bool
	configureOpts    map[string]any
	configureCluster any

	host string // Node rclient.options.host

	// in-memory key/value store (real SET NX exclusivity, script-driven
	// unlock/extend) — used when no scripted response is set, so the fake
	// emulates a real redis for the contention/serialization paths.
	store map[string]string

	// ordered cross-call sequence log (for runner-vs-driver ordering asserts)
	seq []callRecord

	// SetEx
	setAcks   []string        // default "OK"
	setErrs   []error         // default nil
	setDelays []time.Duration // indexed by call
	setCalls  []fakeSetCall

	// Exists
	existsCounts []int64 // default 0
	existsErrs   []error

	// Eval
	evalResults []any   // default int64(1)
	evalErrs    []error // default nil
	evalCalls   []fakeEvalCall

	// Exec (raw ioredis rows) — one [][]any (row list) per scripted Exec call
	execRows  [][][]any // default [[nil, "health-value"], [nil, int64(1)]]
	execErr   error
	execCalls [][][]any

	// FlushAll
	flushErr error
	flushed  bool
}

type fakeSetCall struct {
	key     string
	value   string
	ttlSecs int
	nx      bool
}

type fakeEvalCall struct {
	script string
	keys   []string
	args   []any
}

func (f *fakeDriver) log(kind, info string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq = append(f.seq, callRecord{kind, info})
}

func (f *fakeDriver) seqKinds() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.seq))
	for _, s := range f.seq {
		out = append(out, s.Kind[:1]) // s/e/x/E/F
	}
	return out
}

func pick[T any](list []T, n int, def T) T {
	if n < len(list) {
		return list[n]
	}
	return def
}

// --- DriverConstructor ------------------------------------------------------

func (f *fakeDriver) Configure(opts map[string]any, clusterConfig any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.configured = true
	f.configureOpts = opts
	f.configureCluster = clusterConfig
	return nil
}

// --- Driver ------------------------------------------------------------------

func (f *fakeDriver) SetEx(ctx context.Context, key, value string, ttlSeconds int, nx bool) (string, error) {
	f.mu.Lock()
	i := len(f.setCalls)
	f.mu.Unlock()

	delay := pick(f.setDelays, i, 0)
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	f.mu.Lock()
	f.setCalls = append(f.setCalls, fakeSetCall{key, value, ttlSeconds, nx})
	f.mu.Unlock()

	f.log("set", key)
	if len(f.setErrs) > 0 {
		if err := f.setErrs[minIdx(i, len(f.setErrs))]; err != nil {
			return "", err
		}
	}
	if len(f.setAcks) > 0 {
		return f.setAcks[minIdx(i, len(f.setAcks))], nil
	}
	f.mu.Lock()
	st := f.ensureStore()
	if _, ok := st[key]; ok && nx {
		f.mu.Unlock()
		return "", nil // someone else holds it
	}
	st[key] = value
	f.mu.Unlock()
	return "OK", nil
}

func minIdx(i, n int) int {
	if i >= n {
		return n - 1
	}
	return i
}

func (f *fakeDriver) Exists(ctx context.Context, key string) (int64, error) {
	f.log("exists", key)
	if len(f.existsErrs) > 0 {
		if err := f.existsErrs[minIdx(0, len(f.existsErrs))]; err != nil {
			return 0, err
		}
	}
	if len(f.existsCounts) > 0 {
		return f.existsCounts[minIdx(0, len(f.existsCounts))], nil
	}
	f.mu.Lock()
	_, ok := f.ensureStore()[key]
	f.mu.Unlock()
	if ok {
		return 1, nil
	}
	return 0, nil
}

func (f *fakeDriver) Eval(ctx context.Context, script string, keys []string, args []any) (any, error) {
	f.mu.Lock()
	i := len(f.evalCalls)
	f.evalCalls = append(f.evalCalls, fakeEvalCall{script, keys, args})
	f.mu.Unlock()
	f.log("eval", script[:8])
	if len(f.evalErrs) > 0 {
		if err := f.evalErrs[minIdx(i, len(f.evalErrs))]; err != nil {
			return nil, err
		}
	}
	if len(f.evalResults) > 0 {
		return f.evalResults[minIdx(i, len(f.evalResults))], nil
	}
	// emulate the two lua scripts over the store (unlock/extend compare
	// the held value — real redis CAS semantics)
	f.mu.Lock()
	st := f.ensureStore()
	key := ""
	if len(keys) > 0 {
		key = keys[0]
	}
	held, ok := st[key]
	var result int64
	switch {
	case script == unlockScript:
		if ok && held == valueArg(args) {
			delete(st, key)
			result = 1
		}
	case script == extendScript:
		if ok && held == valueArg(args) {
			result = 1
		}
	default:
		result = 1
	}
	f.mu.Unlock()
	return result, nil
}

// ensureStore lazily initialises the store map (fakes built with struct
// literals start with a nil map — Go writes to nil maps panic).
func (f *fakeDriver) ensureStore() map[string]string {
	if f.store == nil {
		f.store = map[string]string{}
	}
	return f.store
}

// valueArg is the script's ARGV[1] (the lock value).
func valueArg(args []any) string {
	s, _ := args[0].(string)
	return s
}

func (f *fakeDriver) Exec(ctx context.Context, ops []Op) ([][]any, error) {
	f.mu.Lock()
	i := len(f.execCalls)
	f.execCalls = append(f.execCalls, rawOps(ops))
	f.mu.Unlock()
	f.log("exec", fmt.Sprint(len(ops)))
	if f.execErr != nil {
		return nil, f.execErr
	}
	if f.execRows == nil {
		return [][]any{{nil, "health-value"}, {nil, int64(1)}}, nil
	}
	idx := i
	if idx >= len(f.execRows) {
		idx = len(f.execRows) - 1
	}
	return f.execRows[idx], nil
}

func (f *fakeDriver) FlushAll(ctx context.Context) error {
	f.mu.Lock()
	f.flushed = true
	f.mu.Unlock()
	f.log("flushall", "")
	return f.flushErr
}

func (f *fakeDriver) Host() string { return f.host }

func (f *fakeDriver) execOp() ([]Op, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.execCalls) == 0 {
		return nil, false
	}
	ops := make([]Op, 0, len(f.execCalls[0]))
	for _, r := range f.execCalls[0] {
		cmd, _ := r[0].(string)
		key, _ := r[1].(string)
		ops = append(ops, Op{Cmd: cmd, Key: key})
	}
	return ops, true
}

func (f *fakeDriver) setCall(n int) (fakeSetCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if n < len(f.setCalls) {
		return f.setCalls[n], true
	}
	return fakeSetCall{}, false
}

func (f *fakeDriver) evalCall(n int) (fakeEvalCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if n < len(f.evalCalls) {
		return f.evalCalls[n], true
	}
	return fakeEvalCall{}, false
}

// reflectEqual is a small deep-equality helper for the assertions.
func reflectEqual(a, b any) bool { return reflect.DeepEqual(a, b) }

func rawOps(ops []Op) [][]any {
	out := make([][]any, len(ops))
	for i, op := range ops {
		out[i] = []any{op.Cmd, op.Key}
	}
	return out
}
