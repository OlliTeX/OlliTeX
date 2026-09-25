package consent

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ollitex/go/services/web/core"
)

// ---- route table -----------------------------------------------------------

func TestRouteTable(t *testing.T) {
	f := Feature(nil)
	seen := 0
	for _, r := range f.Routes {
		seen++
		key := r.Method + " " + r.Path
		if r.Pattern != nil {
			key = r.Method + " " + r.Pattern.String()
		}
		switch key {
		case "GET /cookie-consent", "POST /cookie-consent":
			// must be NoLogin (anonymous consent on marketing pages);
			// CSRF is applied by core for every POST regardless.
			if !r.NoLogin {
				t.Fatalf("%s %s: must be NoLogin (consent works pre-account)", r.Method, r.Path)
			}
			if r.Handler == nil {
				t.Fatalf("%s %s: no handler", r.Method, r.Path)
			}
		case "GET " + legalPageRe.String():
			// the banner's privacy/cookie link target (legal.go)
			if !r.NoLogin {
				t.Fatalf("legal page must be NoLogin (public)")
			}
			if r.Pattern == nil {
				t.Fatalf("legal page must use a Pattern (Express case/slash variants)")
			}
			if r.Handler == nil {
				t.Fatalf("legal page: no handler")
			}
		default:
			t.Fatalf("unexpected route %s %s", r.Method, r.Path)
		}
	}
	if seen != 3 {
		t.Fatalf("route count %d, want 3 (2 consent + 1 legal)", seen)
	}
}

// ---------- /legal page ----------

func TestLegalPage(t *testing.T) {
	w := httptest.NewRecorder()
	cxt := &core.Cxt{Req: httptest.NewRequest("GET", "/legal", nil)}
	res := &core.Res{W: w}
	legalPage(cxt, res)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
	body := w.Body.String()
	for _, want := range []string{`<section id="cookies"`, `<section id="privacy"`, "oa=1", "oa=0", "SameSite=Lax"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
	// CSP safety: the page must stay clean under the app default CSP
	// (default-src 'none') — no scripts, inline styles, or external refs.
	for _, bad := range []string{"<script", "<style", "<link", "<iframe", "http://", "https://", "src=", "href="} {
		if strings.Contains(body, bad) {
			t.Errorf("body contains CSP-hostile %q", bad)
		}
	}
	if w.Header().Get("ETag") == "" {
		t.Errorf("want weak ETag (static page)")
	}
}

func TestLegalPatternVariants(t *testing.T) {
	for _, p := range []string{"/legal", "/legal/", "/Legal", "/LEGAL"} {
		if !legalPageRe.MatchString(p) {
			t.Errorf("legalPageRe must match %q (Express case/slash tolerance)", p)
		}
	}
	for _, p := range []string{"/legalfoo", "/legalx", "/legal/"} {
		// note: /legal/ IS a valid variant
		if p == "/legal/" {
			continue
		}
		if legalPageRe.MatchString(p) {
			t.Errorf("legalPageRe wrongly matches %q", p)
		}
	}
}

// ---- value allowlist -------------------------------------------------------

func TestConsentValuesAllowlist(t *testing.T) {
	for v, want := range map[string]string{
		"all":       ValueAll,
		"essential": ValueEssential,
	} {
		if got, ok := consentValues[v]; !ok || got != want {
			t.Fatalf("consentValues[%q] = %q/ok=%v, want %q", v, got, ok, want)
		}
	}
	for _, bad := range []string{"", "All", "ALL", "yes", "1", "0", "tracker", "true"} {
		if _, ok := consentValues[bad]; ok {
			t.Fatalf("consentValues must NOT accept %q", bad)
		}
	}
}

func TestParseConsentValue(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{ValueAll, "all"},
		{ValueEssential, "essential"},
		{"", ""},
		{"2", ""},
		{"true", ""},
		{" 1", ""},
		{"11", ""},
	}
	for _, c := range cases {
		if got := ParseConsentValue(c.in); got != c.want {
			t.Errorf("ParseConsentValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ---- cookie attributes -----------------------------------------------------

func TestConsentCookieAttributes(t *testing.T) {
	for _, v := range []string{ValueAll, ValueEssential} {
		ck := consentCookie(v)
		if ck.Name != "oa" {
			t.Fatalf("name %q, want oa (compat contract — tracking-loader gates on it)", ck.Name)
		}
		if ck.Path != "/" {
			t.Fatalf("path %q, want /", ck.Path)
		}
		if ck.MaxAge != 365*24*3600 {
			t.Fatalf("maxage %d, want one year (parity with the legacy JS writer)", ck.MaxAge)
		}
		if !ck.Secure {
			t.Fatal("Secure must be set (browsers enforce it on HTTPS, drop it over plain http)")
		}
		if ck.HttpOnly {
			t.Fatal("HttpOnly must be OFF: first-party JS reads the cookie (gate + banner state)")
		}
		if ck.SameSite != http.SameSiteLaxMode {
			t.Fatalf("SameSite %v, want Lax", ck.SameSite)
		}
	}
}

// ---- POST handler ------------------------------------------------------------

func doSet(t *testing.T, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/cookie-consent", strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rw := httptest.NewRecorder()
	hSetConsent(&core.Cxt{Req: req}, &core.Res{W: rw})
	return rw
}

func TestSetConsentAll(t *testing.T) {
	w := doSet(t, `{"consent":"all"}`, map[string]string{"Content-Type": "application/json"})
	if w.Code != 200 {
		t.Fatalf("status %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	if b := w.Body.String(); b != `{"ok":true,"consent":"all"}` {
		t.Fatalf("body %q", b)
	}
	sc := w.Header().Get("Set-Cookie")
	for _, wantSub := range []string{"oa=1", "Path=/", "Max-Age=" + "31536000", "Secure", "SameSite=Lax"} {
		if !strings.Contains(sc, wantSub) {
			t.Fatalf("Set-Cookie %q missing %q", sc, wantSub)
		}
	}
	if strings.Contains(sc, "HttpOnly") {
		t.Fatalf("Set-Cookie must NOT carry HttpOnly: %q", sc)
	}
}

func TestSetConsentEssential(t *testing.T) {
	w := doSet(t, `{"consent":"essential"}`, map[string]string{"Content-Type": "application/json"})
	if w.Code != 200 {
		t.Fatalf("status %d, want 200", w.Code)
	}
	if b := w.Body.String(); b != `{"ok":true,"consent":"essential"}` {
		t.Fatalf("body %q", b)
	}
	if sc := w.Header().Get("Set-Cookie"); !strings.Contains(sc, "oa=0") {
		t.Fatalf("Set-Cookie %q, want oa=0", sc)
	}
}

func TestSetConsentInvalid(t *testing.T) {
	bad := []string{
		``,
		`{}`,
		`{"consent":"tracker"}`,
		`{"consent":"All"}`,
		`{"consent":"x",}`, // broken JSON (trailing key with no value)
		`{"consent":1}`,    // wrong type
		`[1,2]`,            // array root (core body-parser 400s it; handler must too)
	}
	for _, body := range bad {
		w := doSet(t, body, map[string]string{"Content-Type": "application/json"})
		if w.Code != 400 {
			t.Errorf("body %q: status %d, want 400", body, w.Code)
		}
		if b := w.Body.String(); b != `{"ok":false,"error":"invalid_consent"}` {
			t.Errorf("body %q: response %q", body, b)
		}
		if sc := w.Header().Get("Set-Cookie"); sc != "" {
			t.Errorf("body %q: must not set the cookie (got %q)", body, sc)
		}
	}
}

// ---- GET handler -------------------------------------------------------------

func doGet(t *testing.T, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/cookie-consent", nil)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	rw := httptest.NewRecorder()
	hGetConsent(&core.Cxt{Req: req}, &core.Res{W: rw})
	return rw
}

func TestGetConsent(t *testing.T) {
	cases := []struct {
		cookie, want string
	}{
		{"oa=1", `{"consent":"all"}`},
		{"oa=0", `{"consent":"essential"}`},
		{"", `{"consent":null}`},
		{"oa=2", `{"consent":null}`},
		{"oa=garbage", `{"consent":null}`},
		{"other=1", `{"consent":null}`},
	}
	for _, c := range cases {
		w := doGet(t, c.cookie)
		if w.Code != 200 {
			t.Errorf("cookie %q: status %d, want 200", c.cookie, w.Code)
		}
		if b := w.Body.String(); b != c.want {
			t.Errorf("cookie %q: body %q, want %q", c.cookie, b, c.want)
		}
	}
}

// ---- server-side injection predicate -----------------------------------------

func TestConsentAllowsAnalytics(t *testing.T) {
	cases := []struct {
		cookie string
		want   bool
	}{
		{"oa=1", true},
		{"oa=0", false},
		{"", false},
		{"oa=2", false},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", "/", nil)
		if c.cookie != "" {
			req.AddCookie(&http.Cookie{Name: "oa", Value: c.cookie[3:]})
		}
		if got := ConsentAllowsAnalytics(req); got != c.want {
			t.Errorf("ConsentAllowsAnalytics(%q) = %v, want %v", c.cookie, got, c.want)
		}
	}
}

// body is drained exactly once by the handler (no double-read panic).
func TestSetConsentBodyStream(t *testing.T) {
	req := httptest.NewRequest("POST", "/cookie-consent", strings.NewReader(`{"consent":"all"}`))
	rw := httptest.NewRecorder()
	hSetConsent(&core.Cxt{Req: req}, &core.Res{W: rw})
	if n, err := io.Copy(io.Discard, req.Body); n != 0 || err != nil {
		t.Fatalf("body left unread (%d bytes, err=%v) — handler must drain it", n, err)
	}
}
