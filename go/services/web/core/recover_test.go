package core

import (
	"net/http/httptest"
	"testing"
)

// TestRecoverPanic_Clean500 — Part B: a handler panic before any write
// becomes a clean 500 (B9 asks for exactly this instead of a connection
// reset), and the panic does not escape.
func TestRecoverPanic_Clean500(t *testing.T) {
	raw := httptest.NewRecorder()
	rw := &recWriter{ResponseWriter: raw}
	func() {
		defer RecoverPanic(rw)
		panic("deliberate handler panic")
	}()
	if raw.Code != 500 {
		t.Fatalf("expected clean 500, got %d (body %q)", raw.Code, raw.Body.String())
	}
	if raw.Body.String() != "Internal Server Error\n" {
		t.Fatalf("expected plain-text 500 body, got %q", raw.Body.String())
	}
}

// TestRecoverPanic_AlreadyWritten — a panic after the response started is
// absorbed without a corrupting write attempt.
func TestRecoverPanic_AlreadyWritten(t *testing.T) {
	raw := httptest.NewRecorder()
	rw := &recWriter{ResponseWriter: raw, written: true}
	func() {
		defer RecoverPanic(rw)
		panic("deliberate late panic")
	}()
}

// TestGoroutineGuard_Survives — Part B B5/B7: a panic inside a plain `go
// func` with the guard must complete without killing the process.
func TestGoroutineGuard_Survives(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer GoroutineGuard("test-site")
		panic("deliberate goroutine panic")
	}()
	<-done // process still alive; guarded goroutine completed
}

// TestRecoverPanic_NilPassThrough — the deferred guard on the happy path
// (no panic) is a no-op.
func TestRecoverPanic_NilPassThrough(t *testing.T) {
	rw := &recWriter{ResponseWriter: httptest.NewRecorder()}
	func() {
		defer RecoverPanic(rw)
	}()
}
