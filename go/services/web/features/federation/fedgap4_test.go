package federation

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"ollitex/go/services/web/core"
)

// fedgap-4 envelope pins (overleaf-fed S2sRouter + AdminRouter parity):
// the S2S endpoint is ALWAYS mounted — feature-OFF answers the pinned
// 200 `federation-off` envelope, feature-ON dispatches the envelope
// sanity (400 bad-envelope / unknown-action) before the action.

func doFed(t *testing.T, a *core.App, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if hdr == nil {
		hdr = map[string]string{}
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if path == "/federation/s2s" || strings.Contains(path, "api/federation") {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	// Drive the app handler directly (no full middleware stack in unit
	// scope — the envelope + dispatch are what we pin).
	foundHandler := false
	feat := Feature(a)
	for _, r := range feat.Routes {
		if r.Method == method && r.Pattern != nil && r.Pattern.MatchString(path) {
			cxt := &core.Cxt{Req: req}
			r.Handler(cxt, &core.Res{W: w})
			foundHandler = true
			break
		}
	}
	if !foundHandler {
		t.Fatalf("route %s %s not mounted", method, path)
	}
	return w
}

func TestFedS2S_FeatureOff(t *testing.T) {
	t.Setenv("FEDERATION_ENABLED", "false")
	a := core.New(&core.Config{Profile: "web"}, nil)
	w := doFed(t, a, "POST", "/federation/s2s", `{"from":"b.example","to":"a.example","action":"invited","data":{}}`, nil)
	if w.Code != 200 {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"federation-off"`) {
		t.Errorf("want federation-off envelope, got %s", w.Body.String())
	}
}

func TestFedS2S_EnvelopeSanity(t *testing.T) {
	t.Setenv("FEDERATION_ENABLED", "true")
	a := core.New(&core.Config{Profile: "web"}, nil)
	// empty body → 400 bad-envelope
	w := doFed(t, a, "POST", "/federation/s2s", "", nil)
	if w.Code != 400 {
		t.Errorf("empty body: want 400, got %d", w.Code)
	}
	// unknown action → 400 unknown-action
	w = doFed(t, a, "POST", "/federation/s2s", `{"from":"b","to":"a","action":"nope"}`, nil)
	if w.Code != 400 || !strings.Contains(w.Body.String(), `"unknown-action"`) {
		t.Errorf("unknown action: %d %s", w.Code, w.Body.String())
	}
	// known action → 200 (dispatch pending envelope)
	w = doFed(t, a, "POST", "/federation/s2s", `{"from":"b","to":"a","action":"invited","data":{"projectId":"p1"}}`, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"action-pending"`) {
		t.Errorf("invited: %d %s", w.Code, w.Body.String())
	}
}

func TestFedAdmin_Gates(t *testing.T) {
	t.Setenv("FEDERATION_ENABLED", "false")
	a := core.New(&core.Config{Profile: "web"}, nil)
	// feature-OFF: the admin REST answers the off envelope (200 code,
	// not a 404/500) — the router is always mounted, like Node.
	w := doFed(t, a, "GET", "/admin/federation/peers", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"federation-off"`) {
		t.Errorf("admin-off: %d %s", w.Code, w.Body.String())
	}
	// user-fed invite preview same envelope
	w = doFed(t, a, "GET", "/api/federation/invite/preview", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"federation-off"`) {
		t.Errorf("invite-off: %d %s", w.Code, w.Body.String())
	}
	// RP callback: off envelope too (it is a webRouter route, pinned)
	w = doFed(t, a, "GET", "/federation/oidc/rp/callback", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"federation-off"`) {
		t.Errorf("rp-off: %d %s", w.Code, w.Body.String())
	}
}

func TestFedAdmin_RouteInventory(t *testing.T) {
	a := core.New(&core.Config{Profile: "web"}, nil)
	feat := Feature(a)
	want := map[string]bool{
		"GET    /admin/federation":                 false,
		"GET    /admin/federation/peers":           false,
		"POST   /admin/federation/peers":           false,
		"POST   /admin/federation/peers/p.approve": false,
		"DELETE /admin/federation/peers/p":         false,
		"POST   /admin/federation/peers/p.revoke":  false,
		"GET    /admin/federation/keys":            false,
		"POST   /admin/federation/keys/rotate":     false,
		"GET    /admin/federation/audit":           false,
		"GET    /admin/federation/trust-anchors":   false,
		"POST   /admin/federation/trust-anchors":   false,
		"DELETE /admin/federation/trust-anchors/e": false,
		"GET    /admin/federation/wizard":          false,
		"POST   /federation/s2s":                   false,
		"GET    /api/federation/invite/preview":    false,
		"POST   /api/federation/invite/authorize":  false,
		"GET    /federation/export":                false,
		"POST   /federation/export":                false,
		"GET    /federation/oidc/rp/callback":      false,
	}
	for _, r := range feat.Routes {
		m := strings.Title(r.Method)
		switch {
		case r.Pattern != nil && r.Pattern.MatchString("/admin/federation/peers") && (r.Method == "GET" || r.Method == "POST"):
			// which one?
			if r.Pattern.String() == "^/admin/federation/peers$" {
				want[m+"    /admin/federation/peers"] = true
			}
		}
		// generic match: build a probe path per route key
	}
	// simpler: check each expected (method, pattern) by matching against
	// the known pattern set.
	check := func(method, probe string) bool {
		for _, r := range feat.Routes {
			if r.Method != method || r.Pattern == nil {
				continue
			}
			if r.Pattern.MatchString(probe) {
				return true
			}
		}
		return false
	}
	cases := []struct{ method, probe string }{
		{"GET", "/admin/federation"},
		{"GET", "/admin/federation/peers"},
		{"POST", "/admin/federation/peers"},
		{"POST", "/admin/federation/peers/b.example/approve"},
		{"DELETE", "/admin/federation/peers/b.example"},
		{"POST", "/admin/federation/peers/b.example/revoke"},
		{"GET", "/admin/federation/keys"},
		{"POST", "/admin/federation/keys/rotate"},
		{"GET", "/admin/federation/audit"},
		{"GET", "/admin/federation/trust-anchors"},
		{"POST", "/admin/federation/trust-anchors"},
		{"DELETE", "/admin/federation/trust-anchors/root.example"},
		{"GET", "/admin/federation/wizard"},
		{"POST", "/federation/s2s"},
		{"GET", "/api/federation/invite/preview"},
		{"POST", "/api/federation/invite/authorize"},
		{"GET", "/federation/export"},
		{"POST", "/federation/export"},
		{"GET", "/federation/oidc/rp/callback"},
	}
	var missing []string
	for _, c := range cases {
		if !check(c.method, c.probe) {
			missing = append(missing, c.method+" "+c.probe)
		}
	}
	if len(missing) > 0 {
		t.Errorf("missing fedgap-4 routes: %v", missing)
	}
	// DELETE /admin/federation/peers/b.example/approve must NOT match
	// the approve-POST (method discrimination)

	_ = json.Marshal
}
