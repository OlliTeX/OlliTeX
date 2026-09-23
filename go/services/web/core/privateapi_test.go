package core

import (
	"encoding/base64"
	"testing"
)

// TestAPIBasicGateMatrix pins the private-API gate matrix in-process (the
// valid-cred success branch is untestable in e2e — the sandbox network guard
// drops any request whose Authorization carries the WEB_API password, so the
// app-level valid path never runs there). This exercises the handler's valid
// branch directly.
func TestAPIBasicGateMatrix(t *testing.T) {
	const u, p = "apitest", "secrettest123"
	t.Setenv("WEB_API_USER", u)
	t.Setenv("WEB_API_PASSWORD", p)
	app, _, _ := newTestApp(t, "web", "sec")
	app.RegisterFeature(Feature{
		Name: "privgate",
		Routes: []Route{
			{Method: "GET", Path: "/piv", NoSession: true, NoLogin: true, Handler: func(c *Cxt, r *Res) {
				if !c.A.APIBasicGate(c, r, c.Req) {
					return
				}
				r.PlainText(200, "OK")
			}},
		},
	})
	h := app.Handler()
	mkauth := func(user, pass string) string {
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
	}

	// unauth + Accept json → 401 + OverleafLogin + FULL helmet + no XPB
	w := doReq(t, h, "GET", "/piv", map[string]string{"Accept": "application/json"}, "")
	if w.Code != 401 {
		t.Fatalf("unauth-json: code=%d want 401", w.Code)
	}
	if v := w.Header().Get("WWW-Authenticate"); v != "OverleafLogin" {
		t.Fatalf("unauth-json WWW-Authenticate=%q", v)
	}
	if v := w.Header().Get("Cross-Origin-Opener-Policy"); v == "" {
		t.Fatalf("unauth-json missing helmet (COOP)")
	}
	if v := w.Header().Get("X-Powered-By"); v != "" {
		t.Fatalf("unauth-json must NOT carry X-Powered-By (got %q)", v)
	}
	if b := w.Body.String(); b != "Unauthorized" {
		t.Fatalf("unauth-json body=%q", b)
	}

	// unauth + no Accept → 302 /login + helmet + Vary: Accept + no XPB
	w = doReq(t, h, "GET", "/piv", nil, "")
	if w.Code != 302 {
		t.Fatalf("unauth-plain: code=%d want 302", w.Code)
	}
	if v := w.Header().Get("Location"); v != "/login" {
		t.Fatalf("unauth-plain Location=%q", v)
	}
	if v := w.Header().Get("Vary"); v != "Accept" {
		t.Fatalf("unauth-plain Vary=%q", v)
	}
	if v := w.Header().Get("Cross-Origin-Resource-Policy"); v == "" {
		t.Fatalf("unauth-plain missing helmet (CORP)")
	}
	if v := w.Header().Get("X-Powered-By"); v != "" {
		t.Fatalf("unauth-plain must NOT carry X-Powered-By (got %q)", v)
	}

	// wrong cred + html accept → 401 (unconditional, NOT 302)
	w = doReq(t, h, "GET", "/piv", map[string]string{"Authorization": mkauth(u, "nope"), "Accept": "text/html"}, "")
	if w.Code != 401 {
		t.Fatalf("wrong-cred: code=%d want 401", w.Code)
	}
	if v := w.Header().Get("WWW-Authenticate"); v != "OverleafLogin" {
		t.Fatalf("wrong-cred WWW-Authenticate=%q", v)
	}

	// VALID cred → the gate accepts and the handler runs (200). In e2e this
	// exact wire is interceptor-blocked; in-process it proves the accept
	// branch + downstream dispatch.
	w = doReq(t, h, "GET", "/piv", map[string]string{"Authorization": mkauth(u, p)}, "")
	if w.Code != 200 {
		t.Fatalf("valid-cred: code=%d want 200 (gate should accept)", w.Code)
	}
	if b := w.Body.String(); b != "OK" {
		t.Fatalf("valid-cred body=%q want OK", b)
	}
}
