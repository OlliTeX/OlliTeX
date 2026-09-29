package metrics

import (
	"sync"
)

// LabeledCounter is an increasing counter keyed by a label tuple.
// 1:1 of a prom.Counter with labelNames: each `.inc({labels})` bump records
// under the exact label values.
type LabeledCounter struct {
	Name   string
	Labels []string // prom labelNames (documentation of the tuple)
	mu     sync.Mutex
	val    map[string]int64
}

// NewLabeledCounter creates a counter with the given prom-style label names.
func NewLabeledCounter(name string, labels ...string) *LabeledCounter {
	return &LabeledCounter{Name: name, Labels: labels, val: map[string]int64{}}
}

// labelKey stringifies the sorted label/value pairs for map storage.
func labelKey(values map[string]string) string {
	n := len(values)
	// deterministic: sort by value key (label names are fixed per metric)
	keys := make([]string, 0, n)
	for k := range values {
		keys = append(keys, k)
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	out := ""
	for i, k := range keys {
		if i > 0 {
			out += "\u0000"
		}
		out += k + "=" + values[k]
	}
	return out
}

// Inc records an increment of 1 for the label values.
func (c *LabeledCounter) Inc(values map[string]string) { c.Add(values, 1) }

// Add records an increment of n for the label values.
func (c *LabeledCounter) Add(values map[string]string, n int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	k := labelKey(values)
	c.val[k] += n
}

// Value returns the count recorded for the given label values.
func (c *LabeledCounter) Value(values map[string]string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.val[labelKey(values)]
}

// LabeledGauge is a settable gauge keyed by a label tuple
// (1:1 of a prom.Gauge).
type LabeledGauge struct {
	Name   string
	Labels []string
	mu     sync.Mutex
	val    map[string]float64
}

// NewLabeledGauge creates a gauge with the given prom-style label names.
func NewLabeledGauge(name string, labels ...string) *LabeledGauge {
	return &LabeledGauge{Name: name, Labels: labels, val: map[string]float64{}}
}

// Set records a value for the label values.
func (g *LabeledGauge) Set(values map[string]string, v float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.val[labelKey(values)] = v
}

// Value returns the value recorded for the given label values.
func (g *LabeledGauge) Value(values map[string]string) float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.val[labelKey(values)]
}

// LabeledHistogram is an accumulating histogram keyed by a label tuple
// (1:1 of a prom.Histogram; buckets not modeled, same as Histogram above).
type LabeledHistogram struct {
	Name   string
	Labels []string
	mu     sync.Mutex
	count  map[string]int64
	sum    map[string]float64
}

// NewLabeledHistogram creates a histogram with the given prom-style label
// names.
func NewLabeledHistogram(name string, labels ...string) *LabeledHistogram {
	return &LabeledHistogram{
		Name: name, Labels: labels,
		count: map[string]int64{}, sum: map[string]float64{},
	}
}

// Observe records `seconds` for the label tuple.
func (h *LabeledHistogram) Observe(values map[string]string, seconds float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	k := labelKey(values)
	h.count[k]++
	h.sum[k] += seconds
}

// Observed returns (count, sumSeconds) for the label tuple.
func (h *LabeledHistogram) Observed(values map[string]string) (int64, float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	k := labelKey(values)
	return h.count[k], h.sum[k]
}
