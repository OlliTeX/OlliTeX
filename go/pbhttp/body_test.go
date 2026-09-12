package pbhttp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLimitBody_OversizedGets413(t *testing.T) {
	h := LimitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(string(b)))
	}), 10)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(`1234567890ABCDE`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body = %d (want 413)", resp.StatusCode)
	}
}

func TestLimitBody_UnderLimitPasses(t *testing.T) {
	h := LimitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if string(b) != `{"ok":1}` {
			t.Fatalf("body = %q", b)
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte("read"))
	}), 100)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(`{"ok":1}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("under-limit body = %d (want 200)", resp.StatusCode)
	}
}

func TestLimitBody_ContentLengthFastPath(t *testing.T) {
	h := LimitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }), 5)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(`1234567890`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("declared-large body = %d (want 413)", resp.StatusCode)
	}
}

// TestLimitBodyWith_CustomOverflow verifies the configurable overflow path
// (e.g. Node's notifications service, whose global error handler forces 500
// rather than the 413 Express default).
func TestLimitBodyWith_CustomOverflow(t *testing.T) {
	h := LimitBodyWith(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}), 10, http.StatusInternalServerError, "Internal Server Error")
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(`1234567890ABC`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("overflow status = %d (want 500)", resp.StatusCode)
	}
	if strings.TrimSpace(string(body)) != "Internal Server Error" {
		t.Fatalf("overflow body = %q (want \"Internal Server Error\")", body)
	}
}
