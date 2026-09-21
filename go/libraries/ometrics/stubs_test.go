package ometrics

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func toStringAny(x any) string {
	return strings.TrimSpace(fmt.Sprint(x))
}

// --- oracle test helpers (mirror the Node acceptance helpers) -----------------

func getMetricSingle(key string) *Metric {
	return DefaultRegistry.GetSingleMetric(key)
}

func getMetricValue(key string) (float64, map[string]any) {
	for _, m := range DefaultRegistry.GetMetricsAsJSON() {
		if m.Name == key && len(m.Values) > 0 {
			return m.Values[0].Value, m.Values[0].Labels
		}
	}
	return 0, nil
}

func expectMetricValue(t *testing.T, key string, expected float64) {
	t.Helper()
	value, labels := getMetricValue(key)
	if labels == nil {
		t.Fatalf("metric %q not found", key)
	}
	if value != expected {
		t.Fatalf("%s: value = %v, want %v", key, value, expected)
	}
	if labels["host"] != Hostname() {
		t.Fatalf("%s: labels.host = %v, want %v", key, labels["host"], Hostname())
	}
	if labels["app"] != "test-app" {
		t.Fatalf("%s: labels.app = %v, want test-app", key, labels["app"])
	}
}

func expectNoMetricValue(t *testing.T, key string) {
	t.Helper()
	if getMetricSingle(key) == nil {
		return
	}
	expectMetricValue(t, key, 0)
}

func getSummarySum(key string) float64 {
	m := getMetricSingle(key)
	if m == nil {
		return 0
	}
	for _, v := range m.Get() {
		if v.MetricName == key+"_sum" {
			return v.Value
		}
	}
	return 0
}

func checkHistogramValues(t *testing.T, key string, expected map[string]float64) {
	t.Helper()
	m := getMetricSingle(key)
	if m == nil {
		t.Fatalf("metric %q not found", key)
	}
	found := map[string]float64{}
	for _, v := range m.Get() {
		if le, ok := v.Labels["le"]; ok {
			found[toStringKey(le)] = v.Value
		}
	}
	if !reflect.DeepEqual(found, expected) {
		t.Fatalf("%s buckets = %v, want %v", key, found, expected)
	}
}

func checkSummaryValues(t *testing.T, key string, expected map[string]float64) {
	t.Helper()
	m := getMetricSingle(key)
	if m == nil {
		t.Fatalf("metric %q not found", key)
	}
	found := map[string]float64{}
	for _, v := range m.Get() {
		if q, ok := v.Labels["quantile"]; ok {
			found[toStringKey(q)] = v.Value
		}
	}
	for q, want := range expected {
		got, ok := found[q]
		if !ok {
			t.Fatalf("quantile %s missing (found %v)", q, found)
		}
		if got < want-5 || got > want+15 {
			t.Fatalf("quantile %s = %v, want within [%v, %v]", q, got, want-5, want+15)
		}
	}
}

func toStringKey(v any) string {
	switch x := v.(type) {
	case string:
		return x
	default:
		return toStringAny(x)
	}
}

// stubRecorder is a test double for the `./index` seam (Node sandbox-stubbed).
type stubRecorder struct {
	timings []recCall
	sums    []recCall
}

type recCall struct {
	key    string
	value  float64
	labels map[string]any
}

func (r *stubRecorder) Timing(key string, timeSpan float64, labels map[string]any) {
	r.timings = append(r.timings, recCall{key, timeSpan, labels})
}

func (r *stubRecorder) Summary(key string, value float64, labels map[string]any) {
	r.sums = append(r.sums, recCall{key, value, labels})
}

func (r *stubRecorder) reset() {
	r.timings = nil
	r.sums = nil
}
