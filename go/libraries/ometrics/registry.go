// Package ometrics is the 1:1 Go port of `libraries/metrics` (the Overleaf
// prometheus-flavoured metrics module). Node source → Go:
//
//	prom_wrapper.js  → registry.go  (MetricWrapper + metric() cache + sweep)
//	(index.js core)   → metrics.go   (inc/summary/timing/histogram/gauge/Timer/destructors)
//	(index.js)        → index.go     (buildPromKey/sanitizeValue/Set/injectMetricsRoute)
//	initialize.js     → initialize.go
//	http.js           → http.go      (monitor middleware + RequestLogger + getRoutePath)
//	event_loop.js     → event_loop.go
//	mongodb.js        → mongodb.go   (pool gauges + command handlers)
//	open_sockets.js   → open_sockets.go
//	memory.js         → memory.go
//	leaked_sockets.js → leaked_sockets.go
//	uv_threadpool_size.js → uv_threadpool_size.go
//
// The Node oracle (37 tests: unit http/event_loop/mongodb + the acceptance
// metrics_tests.js) sandboxes `./index` for the collectors and drives the REAL
// prom-client registry for the metrics-API tests. The Go port therefore ships a
// real prometheus-style registry (Counter/Gauge/Summary/Histogram with default
// labels) so the metric-value oracle is genuine, and exposes a narrow
// `Recorder` seam http/event_loop call (stubbable, like the Node sandbox).
//
// Platform specifics (Node globalAgent sockets, process.memoryUsage,
// diagnostics_channel + /proc/net/tcp, prom default process metrics) are behind
// injectable seams (SocketsTracker, MemoryStats, DefaultMetrics) with the same
// metric names/labels, so the logic is 1:1 and the platform data source is a
// test seam - the documented Go divergence for this library.
package ometrics

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MetricKind is one of the prom-client metric types.
type MetricKind string

const (
	KindCounter   MetricKind = "counter"
	KindGauge     MetricKind = "gauge"
	KindSummary   MetricKind = "summary"
	KindHistogram MetricKind = "histogram"
)

// defaultQuantiles mirrors the prom-client Summary defaults.
var defaultQuantiles = []float64{0.01, 0.05, 0.5, 0.9, 0.95, 0.99, 0.999}

// series is one label-set's recorded values for a metric.
type series struct {
	labels      map[string]any
	count       int64
	sum         float64
	value       float64 // gauge: latest; counter: cumulative total
	samples     []float64
	histBuckets []int64 // cumulative per-le counts, aligned to the metric's buckets
	histTotal   int64   // +Inf count
}

// Metric is the Go port of prom_wrapper.MetricWrapper wrapping a
// Counter/Gauge/Summary/Histogram. It records per-label-set values and can
// carry a `collect` callback (Node prom-client behaviour + the mongodb poolSize
// collect hook that resets + re-populates all pool gauges).
type Metric struct {
	kind       MetricKind
	name       string
	help       string
	labelNames []string
	buckets    []float64
	quantiles  []float64

	mu         sync.Mutex
	series     map[string]*series // labelsKey → series
	lastAccess time.Time
	collect    func()
	registry   *Registry
}

func newMetric(kind MetricKind, name string, labelNames []string, buckets []float64, r *Registry) *Metric {
	q := defaultQuantiles
	return &Metric{
		kind:       kind,
		name:       name,
		help:       name,
		labelNames: labelNames,
		buckets:    buckets,
		quantiles:  q,
		series:     map[string]*series{},
		lastAccess: time.Now(),
		registry:   r,
	}
}

func (m *Metric) newSeries(labels map[string]any) *series {
	s := &series{labels: copyLabels(labels)}
	if m.kind == KindHistogram {
		s.histBuckets = make([]int64, len(m.buckets))
	}
	return s
}

// --- the prom-client surface -------------------------------------------------

// Inc mirrors `metric.inc(labels, value)` (Counter). value defaults to 1.
func (m *Metric) Inc(labels map[string]any, value ...float64) {
	v := 1.0
	if len(value) > 0 {
		v = value[0]
	}
	m.withSeries(labels, func(s *series) {
		s.count++
		s.value += v
		s.sum += v
	})
}

// Set mirrors `metric.set(labels, value)` (Gauge).
func (m *Metric) Set(labels map[string]any, value float64) {
	m.withSeries(labels, func(s *series) { s.value = value })
}

// Observe mirrors `metric.observe(labels, value)` (Summary / Histogram).
func (m *Metric) Observe(labels map[string]any, value float64) {
	m.withSeries(labels, func(s *series) {
		s.count++
		s.sum += value
		if m.kind == KindSummary {
			s.samples = append(s.samples, value)
		} else if m.kind == KindHistogram {
			for i, le := range m.buckets {
				if value <= le {
					s.histBuckets[i]++
				}
			}
			s.histTotal++
		}
	})
}

func (m *Metric) withSeries(labels map[string]any, apply func(*series)) {
	key := labelsKey(labels)
	m.mu.Lock()
	s, ok := m.series[key]
	if !ok {
		s = m.newSeries(labels)
		m.series[key] = s
	}
	m.lastAccess = time.Now()
	m.mu.Unlock()
	apply(s)
}

// Reset clears all recorded label-sets (prom-client `reset()`).
func (m *Metric) Reset() {
	m.mu.Lock()
	m.series = map[string]*series{}
	m.mu.Unlock()
}

// SetCollect installs a pre-serialization hook (mongodb pool gauges).
func (m *Metric) SetCollect(fn func()) { m.collect = fn }

// Name / Help / Kind accessors.
func (m *Metric) Name() string     { return m.name }
func (m *Metric) Kind() MetricKind { return m.kind }

// LabelNames returns the sorted union of the metric's own label keys (declared
// + those actually observed), excluding registry default labels — matching
// prom-client `metric.labelNames`.
func (m *Metric) LabelNames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	set := map[string]bool{}
	for k := range m.series {
		if s := m.series[k]; s != nil {
			for kk := range s.labels {
				set[kk] = true
			}
		}
	}
	for _, k := range m.labelNames {
		set[k] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ValueEntry is one serialized value (node prom-client `values[]`).
type ValueEntry struct {
	MetricName string         `json:"metricName"`
	Value      float64        `json:"value"`
	Labels     map[string]any `json:"labels"`
}

// Get mirrors prom-client `metric.get()` → `{ values: [{metricName, value, labels}] }`.
func (m *Metric) Get() []ValueEntry {
	if m.collect != nil {
		m.collect()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	def := m.registry.defaultLabels()

	keys := sortedSeriesKeys(m.series)
	out := []ValueEntry{}
	for _, k := range keys {
		s := m.series[k]
		merged := mergeLabels(def, s.labels)
		switch m.kind {
		case KindCounter, KindGauge:
			out = append(out, ValueEntry{m.name, s.value, merged})
		case KindSummary:
			out = append(out, ValueEntry{m.name + "_count", float64(s.count), merged})
			out = append(out, ValueEntry{m.name + "_sum", s.sum, merged})
			for _, q := range m.quantiles {
				qv := quantile(s.samples, q)
				lb := copyLabels(merged)
				lb["quantile"] = fmtQuantile(q)
				out = append(out, ValueEntry{m.name, qv, lb})
			}
		case KindHistogram:
			for i, le := range m.buckets {
				lb := copyLabels(merged)
				lb["le"] = strconv.FormatFloat(le, 'g', -1, 64)
				out = append(out, ValueEntry{m.name + "_bucket", float64(s.histBuckets[i]), lb})
			}
			lb := copyLabels(merged)
			lb["le"] = "+Inf"
			out = append(out, ValueEntry{m.name + "_bucket", float64(s.histTotal), lb})
			out = append(out, ValueEntry{m.name + "_sum", s.sum, merged})
			out = append(out, ValueEntry{m.name + "_count", float64(s.count), merged})
		}
	}
	return out
}

// --- label helpers (prom_wrapper labelsKey) ----------------------------------

func labelsKey(labels map[string]any) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s:%v", k, labels[k]))
	}
	return strings.Join(parts, ",")
}

func copyLabels(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func mergeLabels(overrides map[string]string, base map[string]any) map[string]any {
	merged := map[string]any{}
	for k, v := range overrides {
		merged[k] = v
	}
	for k, v := range base {
		merged[k] = v
	}
	return merged
}

func sortedSeriesKeys(m map[string]*series) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func quantile(values []float64, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	vals := make([]float64, len(values))
	copy(vals, values)
	sort.Float64s(vals)
	// nearest-rank (ceiling) quantile
	rank := int(math.Ceil(float64(len(vals)) * q))
	if rank < 1 {
		rank = 1
	}
	if rank > len(vals) {
		rank = len(vals)
	}
	return vals[rank-1]
}

func fmtQuantile(q float64) string {
	return strconv.FormatFloat(q, 'g', -1, 64)
}

// --- Registry (prom-client `register`) ----------------------------------------

// Registry is the Go port of the prom-client global registry.
type Registry struct {
	mu        sync.Mutex
	metrics   map[string]*Metric
	order     []string
	defaultLb map[string]string // setDefaultLabels({app, host})

	ContentType  string
	ttlInMinutes int
}

// NewRegistry builds an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		metrics:     map[string]*Metric{},
		ContentType: "text/plain; version=0.0.4; charset=utf-8",
	}
}

func (r *Registry) defaultLabels() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.defaultLb == nil {
		return nil
	}
	out := map[string]string{}
	for k, v := range r.defaultLb {
		out[k] = v
	}
	return out
}

// SetDefaultLabels mirrors prom-client `register.setDefaultLabels`.
func (r *Registry) SetDefaultLabels(labels map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defaultLb = labels
}

// Metric mirrors prom_wrapper.metric: cached-by-name or newly built.
func (r *Registry) Metric(kind MetricKind, name string, labelNames []string, buckets []float64) *Metric {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.metrics[name]; ok {
		return m
	}
	m := newMetric(kind, name, labelNames, buckets, r)
	r.metrics[name] = m
	r.order = append(r.order, name)
	return m
}

// GetSingleMetric mirrors prom-client `register.getSingleMetric(name)`.
func (r *Registry) GetSingleMetric(name string) *Metric {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.metrics[name]
}

// Clear removes all metrics (Node `prom.register.clear()`).
func (r *Registry) Clear() {
	r.mu.Lock()
	r.metrics = map[string]*Metric{}
	r.order = r.order[:0]
	r.defaultLb = nil
	r.mu.Unlock()
}

// RemoveSingleMetric drops one metric.
func (r *Registry) RemoveSingleMetric(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.metrics, name)
	for i, n := range r.order {
		if n == name {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
}

func (r *Registry) allMetrics() []*Metric {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Metric, 0, len(r.order))
	for _, n := range r.order {
		if m, ok := r.metrics[n]; ok {
			out = append(out, m)
		}
	}
	return out
}

// ValueJSON / MetricJSON mirror the `getMetricsAsJSON` shape the oracle reads.
type ValueJSON struct {
	Labels map[string]any `json:"labels"`
	Value  float64        `json:"value"`
}

type MetricJSON struct {
	Name   string      `json:"name"`
	Values []ValueJSON `json:"values"`
}

// GetMetricsAsJSON mirrors prom-client `register.getMetricsAsJSON()`. Each
// metric's collect hook runs first; values carry the merged default labels.
func (r *Registry) GetMetricsAsJSON() []MetricJSON {
	out := make([]MetricJSON, 0, len(r.order))
	for _, m := range r.allMetrics() {
		vals := m.Get()
		mj := MetricJSON{Name: m.name, Values: []ValueJSON{}}
		for _, v := range vals {
			mj.Values = append(mj.Values, ValueJSON{Labels: v.Labels, Value: v.Value})
		}
		out = append(out, mj)
	}
	return out
}

// TTL returns the sweep TTL in minutes (prom_wrapper.ttlInMinutes).
func (r *Registry) TTL() int { return r.ttlInMinutes }

// SetTTL sets the sweep TTL in minutes.
func (r *Registry) SetTTL(minutes int) { r.ttlInMinutes = minutes }

// Sweep mirrors MetricWrapper.sweep (drops stale label-sets / the metric).
func (m *Metric) Sweep() {
	if m.registry == nil || m.registry.ttlInMinutes == 0 {
		return
	}
	thresh := time.Now().Add(-time.Duration(m.registry.ttlInMinutes) * time.Minute)
	m.mu.Lock()
	stale := m.lastAccess.Before(thresh)
	m.mu.Unlock()
	if stale {
		m.registry.RemoveSingleMetric(m.name)
	}
}
