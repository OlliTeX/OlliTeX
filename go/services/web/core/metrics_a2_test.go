package core

import (
	"net/http"
	"strings"
	"testing"

	"ollitex/go/libraries/ometrics"
)

// TestAppMetricsSurface — D22 A2 wiring contract for the Go web service:
// GET /metrics on the app handler serves the ometrics registry as
// Prometheus text format WITHOUT the core pipeline (no session/CSRF/static),
// and only when the surface is wired (nil-safe: unwired apps 404 it like
// any unknown route — the existing byte-pinned tests stay green).
func TestAppMetricsSurface(t *testing.T) {
	app, _, _ := newTestApp(t, "web", "test-secret-012345")

	// A dedicated registry (no global-registry pollution for other tests).
	reg := ometrics.NewRegistry()
	reg.SetDefaultLabels(map[string]string{"app": "ollitex-web", "host": "testhost"})
	c := reg.Metric(ometrics.KindCounter, "requests", []string{"code", "method", "path"}, nil)
	c.Inc(map[string]any{"code": 200, "method": "GET", "path": "/launchpad"})

	app.MetricsHTTP = ometrics.PrometheusHandler(reg)

	rec := doReq(t, app.Handler(), "GET", "/metrics", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != ometrics.PrometheusContentType {
		t.Errorf("content-type = %q, want %q", ct, ometrics.PrometheusContentType)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "# TYPE requests counter") {
		t.Errorf("missing TYPE line in: %s", body)
	}
	if !strings.Contains(body, `requests{app="ollitex-web",code="200",host="testhost",method="GET",path="/launchpad"} 1`) {
		t.Errorf("missing sample line in: %s", body)
	}
}

// TestAppMetricsSurfaceUnwired — an App without MetricsHTTP set (tests, and
// any build that doesn't wire D22) treats /metrics as an ordinary unknown
// route (web profile: the 404 view) — no crash, no accidental scrape surface.
func TestAppMetricsSurfaceUnwired(t *testing.T) {
	app, _, _ := newTestApp(t, "web", "test-secret-012345")

	rec := doReq(t, app.Handler(), "GET", "/metrics", nil, "")
	if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "# TYPE") {
		t.Fatalf("unwired app must not serve a metrics surface, got: %s", rec.Body.String())
	}
}
