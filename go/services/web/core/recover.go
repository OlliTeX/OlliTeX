package core

import (
	"log"
	"net/http"
	"runtime/debug"
)

// RecoverPanic — Part B (audit): defer at the top of App.Handler. A handler
// panic becomes a logged clean 500 (when the response has not started)
// instead of the default connection reset (the B9 otc-decode path), and
// never a lost worker. Node models the same with express error middleware.
// (net/http also recovers per-connection and keeps the process alive —
// this makes the client-visible behavior clean.)
func RecoverPanic(w *recWriter) {
	r := recover()
	if r == nil {
		return
	}
	log.Printf("web-core: handler panic (recovered): %v\n%s", r, debug.Stack())
	if !w.written {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
	// w.written: response already started — nothing more can be written
	// cleanly; net/http closes the client connection.
}

// GoroutineGuard — defer inside a fire-and-forget goroutine (Part B B5/B7:
// a plain `go func` panic is process-lethal, unlike handler panics which
// net/http recovers per-connection). Usage:
//
//	go func() {
//		defer GoroutineGuard("site-label")
//		...
//	}()
func GoroutineGuard(site string) {
	r := recover()
	if r == nil {
		return
	}
	log.Printf("web-core: goroutine panic (recovered) %s: %v\n%s", site, r, debug.Stack())
}
