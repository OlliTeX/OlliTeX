package ometrics

import (
	"os"
	"reflect"
	"testing"
)

// TestMetricsAPI mirrors test/acceptance/metrics_tests.js (the Node oracle's
// metrics-API + open_sockets sections). The registry is cleared once, then
// Initialise mirrors the Node `before(() => require('../initialize'))`, and the
// sub-assertions run in the same order / with the same accumulation as Node.
func TestMetricsAPI(t *testing.T) {
	DefaultRegistry.Clear()
	if err := os.Setenv("METRICS_APP_NAME", "test-app"); err != nil {
		t.Fatal(err)
	}
	defer os.Unsetenv("METRICS_APP_NAME")
	Initialize()

	t.Run("at startup", func(t *testing.T) {
		expectMetricValue(t, "process_startup", 1)
		if getMetricSingle("process_cpu_user_seconds_total") == nil {
			t.Fatal("process_cpu_user_seconds_total should exist")
		}
	})

	t.Run("inc()", func(t *testing.T) {
		Inc("duck_count")
		expectMetricValue(t, "duck_count", 1)
		Inc("duck_count")
		Inc("duck_count")
		expectMetricValue(t, "duck_count", 3)
	})

	t.Run("inc() escapes special characters", func(t *testing.T) {
		Inc("show.me the $!!")
		expectMetricValue(t, "show_me_the____", 1)
	})

	t.Run("count()", func(t *testing.T) {
		Count("rabbit_count", 5)
		expectMetricValue(t, "rabbit_count", 5)
		Count("rabbit_count", 6)
		Count("rabbit_count", 7)
		expectMetricValue(t, "rabbit_count", 18)
	})

	t.Run("summary()", func(t *testing.T) {
		Summary("oven_temp", 200)
		Summary("oven_temp", 300)
		Summary("oven_temp", 450)
		if got := getSummarySum("oven_temp"); got != 950 {
			t.Fatalf("oven_temp_sum = %v, want 950", got)
		}
	})

	t.Run("timing()", func(t *testing.T) {
		Timing("sprint_100m", 10)
		Timing("sprint_100m", 20)
		Timing("sprint_100m", 30)
		if got := getSummarySum("timer_sprint_100m"); got != 60 {
			t.Fatalf("timer_sprint_100m_sum = %v, want 60", got)
		}
	})

	t.Run("histogram()", func(t *testing.T) {
		buckets := []float64{10, 100, 1000}
		for _, v := range []float64{10, 20, 100, 200, 1000, 2000} {
			Histogram("distance", v, buckets)
		}
		if got := getSummarySum("histogram_distance"); got != 3330 {
			t.Fatalf("histogram_distance_sum = %v, want 3330", got)
		}
		checkHistogramValues(t, "histogram_distance", map[string]float64{
			"10": 1, "100": 3, "1000": 5, "+Inf": 6,
		})
	})

	t.Run("Timer with buckets", func(t *testing.T) {
		// First beforeEach: 9 durations (matches Node's first collect).
		collectTimer(1)
		checkHistogramValues(t, "histogram_height", map[string]float64{
			"10": 3, "100": 6, "1000": 9, "+Inf": 9,
		})
		m := getMetricSingle("histogram_height")
		if m == nil {
			t.Fatal("histogram_height should exist")
		}
		if !reflect.DeepEqual(m.LabelNames(), []string{"label_1"}) {
			t.Fatalf("histogram_height labelNames = %v, want [label_1]", m.LabelNames())
		}
	})

	t.Run("Timer without buckets", func(t *testing.T) {
		// Second beforeEach: 9 more durations (Node accumulates 18; the
		// quantile oracle is scale-invariant, so the expected values match).
		collectTimer(1)
		checkSummaryValues(t, "timer_depth", map[string]float64{
			"0.01": 1, "0.05": 1, "0.5": 15,
			"0.9": 105, "0.95": 105, "0.99": 105, "0.999": 105,
		})
		m := getMetricSingle("timer_depth")
		if m == nil {
			t.Fatal("timer_depth should exist")
		}
		if !reflect.DeepEqual(m.LabelNames(), []string{"label_2", "label_3"}) {
			t.Fatalf("timer_depth labelNames = %v, want [label_2 label_3]", m.LabelNames())
		}
	})

	t.Run("gauge()", func(t *testing.T) {
		Gauge("water_level", 1.5)
		expectMetricValue(t, "water_level", 1.5)
		Gauge("water_level", 4.2)
		expectMetricValue(t, "water_level", 4.2)
	})

	t.Run("globalGauge()", func(t *testing.T) {
		GlobalGauge("tire_pressure", 99.99)
		value, labels := getMetricValue("tire_pressure")
		if value != 99.99 {
			t.Fatalf("tire_pressure = %v, want 99.99", value)
		}
		if labels["host"] != "global" {
			t.Fatalf("tire_pressure host = %v, want global", labels["host"])
		}
		if labels["app"] != "test-app" {
			t.Fatalf("tire_pressure app = %v, want test-app", labels["app"])
		}
	})

	t.Run("open_sockets.gaugeOpenSockets", func(t *testing.T) {
		runOpenSocketsOracle(t)
	})
}

// collectTimer mirrors the Node Timer beforeEach run `reps` time(s): each run
// is the 9-duration loop that observes one `height` (buckets) + one `depth`
// (no buckets) sample per duration.
func collectTimer(reps int) {
	buckets := []float64{10, 100, 1000}
	durations := []float64{1, 1, 1, 15, 15, 15, 105, 105, 105}
	old := NowMS
	defer func() { NowMS = old }()
	var now int64
	for i := 0; i < reps; i++ {
		for _, d := range durations {
			start := now
			NowMS = func() int64 { return start }
			height := NewTimer("height", 1, map[string]any{"label_1": "a"}, buckets)
			depth := NewTimer("depth", 1, map[string]any{"label_2": "b"})
			now = start + int64(d)
			NowMS = func() int64 { return now }
			height.Done(nil)
			depth.Done(map[string]any{"label_3": "c"})
		}
	}
}

// fakeTracker supplies fixed live-socket counts for the open_sockets oracle.
type fakeTracker struct{ conns map[string]int }

func (f fakeTracker) Connections(string, bool) map[string]int {
	if f.conns == nil {
		return map[string]int{}
	}
	return f.conns
}

func runOpenSocketsOracle(t *testing.T) {
	t.Helper()
	ResetOpenSockets()
	old := Sockets
	defer func() { Sockets = old }()

	const (
		s1 = "127.42.42.1:5000"
		s2 = "127.42.42.2:6000"
		k1 = "open_connections_http_127_42_42_1"
		k2 = "open_connections_http_127_42_42_2"
	)
	set := func(conns map[string]int) { Sockets = fakeTracker{conns} }
	gauge := func() { OpenSocketsMonitor{}.GaugeOpenSockets(true) }

	t.Run("without pending connections", func(t *testing.T) {
		set(nil)
		gauge()
		expectNoMetricValue(t, k1)
		expectNoMetricValue(t, k2)
	})
	t.Run("with pending connections for server1", func(t *testing.T) {
		set(map[string]int{s1: 2})
		gauge()
		expectMetricValue(t, k1, 2)
		expectNoMetricValue(t, k2)
	})
	t.Run("with pending connections for server1 and server2", func(t *testing.T) {
		set(map[string]int{s1: 2, s2: 2})
		gauge()
		expectMetricValue(t, k1, 2)
		expectMetricValue(t, k2, 2)
	})
	t.Run("when requests finish for server1", func(t *testing.T) {
		set(map[string]int{s1: 1, s2: 2})
		gauge()
		expectMetricValue(t, k1, 1)
		expectMetricValue(t, k2, 2)
	})
	t.Run("when all requests complete", func(t *testing.T) {
		set(nil)
		gauge()
		expectNoMetricValue(t, k1)
		expectNoMetricValue(t, k2)
	})
}
