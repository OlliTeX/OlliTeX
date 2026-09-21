package ometrics

import (
	"regexp"
	"strconv"
	"sync"
	"time"
)

// Registry is the global prom-client-style registry (Node: `prom-client`
// `register` / `promWrapper.registry`). Tests clear/inspect it directly.
var DefaultRegistry = NewRegistry()

// Recorder is the narrow metrics surface http/event_loop drive (Node: they
// import `./index` and call `Metrics.timing`/`Metrics.summary`). Tests swap in
// a stub exactly as the Node `sandboxed-module` tests stub `./index`.
type Recorder interface {
	Timing(key string, value float64, labels map[string]any)
	Summary(key string, value float64, labels map[string]any)
}

// recorder is the live Recorder (default: the real registry-backed one).
var recorder Recorder = DefaultRecorder{}

// DefaultRecorder bridges the Recorder seam to the real registry (the
// no-stub production path).
type DefaultRecorder struct{}

func (DefaultRecorder) Timing(key string, value float64, labels map[string]any) {
	k := BuildPromKey("timer_" + key)
	m := DefaultRegistry.Metric(KindSummary, k, labelNames(labels), nil)
	m.Observe(labels, value)
}

func (DefaultRecorder) Summary(key string, value float64, labels map[string]any) {
	k := BuildPromKey(key)
	m := DefaultRegistry.Metric(KindSummary, k, labelNames(labels), nil)
	m.Observe(labels, value)
}

func labelNames(labels map[string]any) []string {
	if len(labels) == 0 {
		return nil
	}
	out := make([]string, 0, len(labels))
	for k := range labels {
		out = append(out, k)
	}
	return out
}

// --- destructors (index.js registerDestructor / close) ------------------------

var (
	destructorsMu sync.Mutex
	destructors   []func()
)

// RegisterDestructor records a function to run on Close (Node:
// `registerDestructor` + `close()`). A var so tests can capture calls.
var RegisterDestructor = func(f func()) {
	destructorsMu.Lock()
	destructors = append(destructors, f)
	destructorsMu.Unlock()
}

// Close runs all registered destructors (Node: `close()`).
func Close() {
	destructorsMu.Lock()
	d := append([]func(){}, destructors...)
	copy(d, destructors)
	destructors = nil
	destructorsMu.Unlock()
	for _, f := range d {
		f()
	}
}

// --- key / value helpers (index.js buildPromKey / sanitizeValue) --------------

var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]`)

// BuildPromKey mirrors `buildPromKey`: replaces every non-alphanumeric char
// with `_`.
func BuildPromKey(key string) string {
	return nonAlnum.ReplaceAllString(key, "_")
}

// SanitizeValue mirrors `sanitizeValue`: parseFloat (NaN → 0).
func SanitizeValue(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	}
	return 0
}

// --- the index.js API (inc/count/summary/timing/histogram/gauge/globalGauge) ---

// Inc mirrors `inc(key, sampleRate?, labels?)`. labels is optional.
func Inc(key string, labels ...map[string]any) {
	k := BuildPromKey(key)
	l := optLabels(labels)
	DefaultRegistry.Metric(KindCounter, k, labelNames(l), nil).Inc(l)
}

// Count mirrors `count(key, count, sampleRate?, labels?)`.
func Count(key string, count float64, labels ...map[string]any) {
	k := BuildPromKey(key)
	l := optLabels(labels)
	DefaultRegistry.Metric(KindCounter, k, labelNames(l), nil).Inc(l, count)
}

// Summary mirrors `summary(key, value, labels?)`.
func Summary(key string, value float64, labels ...map[string]any) {
	k := BuildPromKey(key)
	l := optLabels(labels)
	DefaultRegistry.Metric(KindSummary, k, labelNames(l), nil).Observe(l, value)
}

// Timing mirrors `timing(key, timeSpan, sampleRate?, labels?)` (observes the
// `timer_<key>` summary).
func Timing(key string, timeSpan float64, labels ...map[string]any) {
	k := BuildPromKey("timer_" + key)
	l := optLabels(labels)
	DefaultRegistry.Metric(KindSummary, k, labelNames(l), nil).Observe(l, timeSpan)
}

// Histogram mirrors `histogram(key, value, buckets, labels?)`.
func Histogram(key string, value float64, buckets []float64, labels ...map[string]any) {
	k := BuildPromKey("histogram_" + key)
	l := optLabels(labels)
	DefaultRegistry.Metric(KindHistogram, k, labelNames(l), buckets).Observe(l, value)
}

// Gauge mirrors `gauge(key, value, sampleRate?, labels?)`. labels is optional.
func Gauge(key string, value float64, labels ...map[string]any) {
	k := BuildPromKey(key)
	l := optLabels(labels)
	DefaultRegistry.Metric(KindGauge, k, labelNames(l), nil).Set(l, SanitizeValue(value))
}

// GlobalGauge mirrors `globalGauge(key, value, sampleRate?, labels?)` (adds the
// `host: 'global'` label).
func GlobalGauge(key string, value float64, labels ...map[string]any) {
	k := BuildPromKey(key)
	l := optLabels(labels)
	l["host"] = "global"
	DefaultRegistry.Metric(KindGauge, k, labelNames(l), nil).Set(l, SanitizeValue(value))
}

// optLabels picks the first provided label set (or an empty map), matching the
// Node optional `labels` argument.
func optLabels(v []map[string]any) map[string]any {
	if len(v) == 0 {
		return map[string]any{}
	}
	if v[0] == nil {
		return map[string]any{}
	}
	return v[0]
}

// Set mirrors `set(key, value, sampleRate?)` — "counts are not currently
// supported" (no-op, like Node).
func Set(key string, value float64) {
	// Node logs "counts are not currently supported" and records nothing.
	_ = key
	_ = value
}

// --- Timer (index.js class) ----------------------------------------------------

// NowMS is the injectable clock (Node: `Date.now()`). ms since epoch.
var NowMS = func() int64 { return time.Now().UnixMilli() }

// Timer mirrors the index.js Timer class.
type Timer struct {
	start      int64
	key        string
	sampleRate float64
	labels     map[string]any
	buckets    []float64
}

// NewTimer mirrors the Timer constructor. buckets may be nil.
func NewTimer(key string, sampleRate float64, labels map[string]any, buckets ...[]float64) *Timer {
	b := []float64(nil)
	if len(buckets) > 0 {
		b = buckets[0]
	}
	return &Timer{
		start:      NowMS(),
		key:        BuildPromKey(key),
		sampleRate: sampleRate,
		labels:     labels,
		buckets:    b,
	}
}

// Done mirrors Timer.done: records histogram (if buckets) else timing, merged
// with any done-labels; returns the elapsed ms.
func (t *Timer) Done(labels map[string]any) float64 {
	timeSpan := float64(NowMS() - t.start)
	merged := make(map[string]any, len(t.labels)+len(labels))
	for k, v := range t.labels {
		merged[k] = v
	}
	for k, v := range labels {
		merged[k] = v
	}
	if len(t.buckets) > 0 {
		timingKey(t.key, timeSpan, t.buckets, merged)
	} else {
		timingKey(t.key, timeSpan, nil, merged)
	}
	return timeSpan
}

// timingKey is the shared observation (histogram when buckets present).
func timingKey(key string, value float64, buckets []float64, labels map[string]any) {
	if len(buckets) > 0 {
		Histogram(key, value, buckets, labels)
	} else {
		Timing(key, value, labels)
	}
}
