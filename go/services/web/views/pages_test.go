package views

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func render(name string) string {
	d := PageData{CSRFToken: "tokval123456", Nonce: "nonceval1234", Origin: "http://127.0.0.1:7420"}
	switch name {
	case "login":
		w := httptest.NewRecorder()
		LoginPage(w, d)
		return w.Body.String()
	case "register":
		w := httptest.NewRecorder()
		RegisterPage(w, d)
		return w.Body.String()
	case "logout":
		w := httptest.NewRecorder()
		LogoutPage(w, d)
		return w.Body.String()
	case "restricted":
		w := httptest.NewRecorder()
		RestrictedPage(w, d)
		return w.Body.String()
	case "404":
		w := httptest.NewRecorder()
		NotFoundPage(w, d)
		return w.Body.String()
	}
	panic("unknown page " + name)
}

// Slot mechanics: no sentinel may survive a render; slots must be filled
// exactly (token/nonce appear once per skeleton occurrence).
func TestSlotsFullyFilled(t *testing.T) {
	for _, name := range []string{"login", "register", "logout", "restricted", "404"} {
		out := render(name)
		if strings.Contains(out, "\x01") || strings.Contains(out, "\x02") {
			t.Fatalf("%s: sentinel leaked", name)
		}
		if !strings.Contains(out, "tokval123456") {
			t.Fatalf("%s: csrf slot not filled", name)
		}
		if !strings.Contains(out, "nonceval1234") {
			t.Fatalf("%s: nonce slot not filled", name)
		}
		if !strings.HasPrefix(out, "<!DOCTYPE html>") {
			t.Fatalf("%s: bad start", name)
		}
		if !strings.HasSuffix(out, "</html>") {
			t.Fatalf("%s: bad end", name)
		}
		t.Logf("%s: OK (%d bytes)", name, len(out))
	}
}

func TestPageStatusAndHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	NotFoundPage(w, PageData{CSRFToken: "t", Nonce: "n", Origin: "http://127.0.0.1:7420"})
	if w.Code != 404 {
		t.Fatalf("404 page status = %d", w.Code)
	}
	w2 := httptest.NewRecorder()
	LoginPage(w2, PageData{CSRFToken: "t", Nonce: "n", Origin: "http://127.0.0.1:7420"})
	if w2.Code != 200 {
		t.Fatalf("login status = %d", w2.Code)
	}
	if w2.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("ct = %q", w2.Header().Get("Content-Type"))
	}
	if w2.Header().Get("X-Powered-By") != "Express" {
		t.Fatalf("xpb = %q", w2.Header().Get("X-Powered-By"))
	}
}
