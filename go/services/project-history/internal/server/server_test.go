package server_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pherrs "ollitex/go/services/project-history/internal/errors"
	"ollitex/go/services/project-history/internal/server"
)

func TestStatusBody(t *testing.T) {
	s := &server.Server{}
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest("GET", "/status", nil))
	if rec.Code != 200 {
		t.Fatalf("status=%d want 200", rec.Code)
	}
	body := rec.Body.String()
	if body != "project-history is up" {
		t.Fatalf("body=%q want %q", body, "project-history is up")
	}
	if ct := rec.Result().Header.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("CT=%q", ct)
	}
}

func TestUnmatched404(t *testing.T) {
	s := &server.Server{}
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest("GET", "/nope", nil))
	if rec.Code != 404 {
		t.Fatalf("status=%d want 404", rec.Code)
	}
	if want := "Not Found"; !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("body=%q want %q", rec.Body.String(), want)
	}
}

// Wire dispatch branches (server.js L48-71, verified 2026-09-19).
func dispatchCase(t *testing.T, name string, err error, wantCode int, wantBodyContains string, wantRetryAfter bool) {
	rec := httptest.NewRecorder()
	server.Dispatch(rec, nil, err, func() http.HandlerFunc { return func(http.ResponseWriter, *http.Request) {} })
	if rec.Code != wantCode {
		t.Errorf("%s: code=%d want %d", name, rec.Code, wantCode)
	}
	if wantBodyContains != "" && !strings.Contains(rec.Body.String(), wantBodyContains) {
		t.Errorf("%s: body=%q want contains %q", name, rec.Body.String(), wantBodyContains)
	}
	if (wantRetryAfter && rec.Header().Get("Retry-After") != "300") || (rec.Header().Get("Retry-After") != "" && !wantRetryAfter) {
		t.Errorf("%s: Retry-After=%q want %v", name, rec.Header().Get("Retry-After"), wantRetryAfter)
	}
}

func TestDispatchNotFound(t *testing.T) {
	dispatchCase(t, "notfound", pherrs.NotFound("missing"), 404, "Not Found", false)
}
func TestDispatchBadRequest(t *testing.T) {
	dispatchCase(t, "badreq", pherrs.BadRequest("no"), 400, "Bad Request", false)
}
func TestDispatchInconsistent(t *testing.T) {
	dispatchCase(t, "incomplete", pherrs.InconsistentChunk("no"), 422, "Not Accepted", false)
} // "422 Not Accepted"
func TestDispatchSyncOngoing(t *testing.T) {
	dispatchCase(t, "syncongoing", pherrs.SyncOngoing("ongoing"), 409, "Conflict", false)
}
func TestDispatchTooMany(t *testing.T) {
	dispatchCase(t, "tmany", pherrs.TooManyRequests("slow down"), 429, "", true)
}
func TestDispatchLockTimeout(t *testing.T) {
	dispatchCase(t, "locktimeout", pherrs.LockTimeout("ph:lock:1"), 423, "redis lock is taken", false)
}
func TestDispatchGenericO(t *testing.T) {
	dispatchCase(t, "generico", pherrs.Sync("boom"), 500, "an internal error occurred", false)
}
func TestDispatchGenericErr(t *testing.T) {
	dispatchCase(t, "generic", errors.New("boom"), 500, "an internal error occurred", false)
}
