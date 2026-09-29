package ometrics

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestPromRegistry(t *testing.T) *Registry {
	t.Helper()
	r := NewRegistry()
	r.SetDefaultLabels(map[string]string{"app": "ollitex-test", "host": "h"})

	// counter with labels
	c := r.Metric(KindCounter, "requests", []string{"method", "path", "code"}, nil)
	c.Inc(map[string]any{"method": "GET", "path": "/launchpad", "code": 200})
	c.Inc(map[string]any{"method": "POST", "path": "/login", "code": 200}, 3)

	// gauge
	g := r.Metric(KindGauge, "process_heap_bytes", nil, nil)
	g.Set(map[string]any{}, 123456)

	// summary (quantiles)
	s := r.Metric(KindSummary, "request_time", nil, nil)
	s.Observe(map[string]any{}, 10)
	s.Observe(map[string]any{}, 20)
	s.Observe(map[string]any{}, 30)

	// histogram (le buckets)
	h := r.Metric(KindHistogram, "ws_roundtrip_ms", nil, []float64{1, 5, 25, 100})
	h.Observe(map[string]any{}, 0.5)
	h.Observe(map[string]any{}, 7)
	h.Observe(map[string]any{}, 250)
	return r
}

func TestPrometheusEncodeAllKinds(t *testing.T) {
	r := newTestPromRegistry(t)
	var buf bytes.Buffer
	if err := PrometheusEncode(&buf, r); err != nil {
		t.Fatalf("encode: %v", err)
	}
	out := buf.String()

	// HELP/TYPE per base metric
	for _, want := range []string{
		"# HELP requests requests",
		"# TYPE requests counter",
		"# HELP process_heap_bytes process_heap_bytes",
		"# TYPE process_heap_bytes gauge",
		"# TYPE request_time summary",
		"# TYPE ws_roundtrip_ms histogram",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\n%s", want, out)
		}
	}

	// counter sample lines (labels sorted: code, method, path; default labels merged last-into-line sorted)
	if !strings.Contains(out, `requests{app="ollitex-test",code="200",host="h",method="GET",path="/launchpad"} 1`) {
		t.Errorf("missing GET counter line\n%s", out)
	}
	if !strings.Contains(out, `requests{app="ollitex-test",code="200",host="h",method="POST",path="/login"} 3`) {
		t.Errorf("missing POST counter line\n%s", out)
	}

	// gauge
	if !strings.Contains(out, "process_heap_bytes{app=\"ollitex-test\",host=\"h\"} 123456") {
		t.Errorf("missing gauge line\n%s", out)
	}

	// summary: quantile series + _sum + _count
	if !strings.Contains(out, `request_time{app="ollitex-test",host="h",quantile="0.5"} 20`) {
		t.Errorf("missing summary p50 line\n%s", out)
	}
	if !strings.Contains(out, "request_time_sum{app=\"ollitex-test\",host=\"h\"} 60") {
		t.Errorf("missing summary _sum line\n%s", out)
	}
	if !strings.Contains(out, "request_time_count{app=\"ollitex-test\",host=\"h\"} 3") {
		t.Errorf("missing summary _count line\n%s", out)
	}

	// histogram: cumulative le buckets + +Inf + _sum/_count
	if !strings.Contains(out, `ws_roundtrip_ms_bucket{app="ollitex-test",host="h",le="1"} 1`) {
		t.Errorf("missing bucket le=1\n%s", out)
	}
	if !strings.Contains(out, `ws_roundtrip_ms_bucket{app="ollitex-test",host="h",le="5"} 1`) {
		t.Errorf("missing bucket le=5\n%s", out)
	}
	if !strings.Contains(out, `ws_roundtrip_ms_bucket{app="ollitex-test",host="h",le="25"} 2`) {
		t.Errorf("missing bucket le=25\n%s", out)
	}
	if !strings.Contains(out, `ws_roundtrip_ms_bucket{app="ollitex-test",host="h",le="100"} 2`) {
		t.Errorf("missing bucket le=100\n%s", out)
	}
	if !strings.Contains(out, `ws_roundtrip_ms_bucket{app="ollitex-test",host="h",le="+Inf"} 3`) {
		t.Errorf("missing bucket +Inf\n%s", out)
	}
	if !strings.Contains(out, "ws_roundtrip_ms_sum{app=\"ollitex-test\",host=\"h\"} 257.5") {
		t.Errorf("missing hist _sum\n%s", out)
	}
	if !strings.Contains(out, "ws_roundtrip_ms_count{app=\"ollitex-test\",host=\"h\"} 3") {
		t.Errorf("missing hist _count\n%s", out)
	}
}

func TestPrometheusEncodeEscaping(t *testing.T) {
	r := NewRegistry()
	c := r.Metric(KindCounter, "weird", []string{"note"}, nil)
	c.Inc(map[string]any{"note": `a "quoted" \ n`})
	var buf bytes.Buffer
	if err := PrometheusEncode(&buf, r); err != nil {
		t.Fatalf("encode: %v", err)
	}
	const want = `weird{note="a \"quoted\" \\ n"} 1`
	if !strings.Contains(buf.String(), want) {
		t.Errorf("escaped line mismatch:\n%s\nwant %s", buf.String(), want)
	}
}

func TestPrometheusEncodeDeterministic(t *testing.T) {
	r := newTestPromRegistry(t)
	one := func() string {
		var buf bytes.Buffer
		if err := PrometheusEncode(&buf, r); err != nil {
			t.Fatalf("encode: %v", err)
		}
		return buf.String()
	}
	if a, b := one(), one(); a != b {
		t.Errorf("non-deterministic output:\nA:\n%s\nB:\n%s", a, b)
	}
}

func TestPrometheusHandler(t *testing.T) {
	r := newTestPromRegistry(t)
	h := PrometheusHandler(r)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != PrometheusContentType {
		t.Errorf("content-type = %q, want %q", ct, PrometheusContentType)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "# TYPE requests counter") {
		t.Errorf("handler body missing counter type:\n%s", body)
	}
}

func TestRouteLabelCardinality(t *testing.T) {
	cases := map[string]string{
		"/project/64f1a2b3c4d5e6f7a8b9c0d1/doc":           "/project/{id}/doc",
		"/user/0123456789abcdef0123456789abcdef/settings": "/user/{id}/settings",
		"/a/f0f7c6fd-c0de-4c0d-b00d-000000000001/b":       "/a/{uuid}/b",
		"/launchpad": "/launchpad",
	}
	for in, want := range cases {
		if got := routeLabel(in); got != want {
			t.Errorf("routeLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHTTPMiddlewareRecords(t *testing.T) {
	r := NewRegistry()
	SaveDefault := DefaultRegistry
	DefaultRegistry = r
	defer func() { DefaultRegistry = SaveDefault }()

	h := HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	}))
	req := httptest.NewRequest(http.MethodGet, "/project/64f1a2b3c4d5e6f7a8b9c0d1/doc", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var buf bytes.Buffer
	if err := PrometheusEncode(&buf, r); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(buf.String(), `requests{code="201",method="GET",path="/project/{id}/doc"} 1`) {
		t.Errorf("missing middleware counter line\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "request_time_count") {
		t.Errorf("missing middleware summary\n%s", buf.String())
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
}
