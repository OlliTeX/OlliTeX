// Command project-history is the production entrypoint of the project-history
// Go service (vendor services/project-history/app.js 1:1): connect mongo,
// wire the C/D manager bundles (appfactory.Build), and serve the HTTP surface
// on the vendor host/port (Settings.internal.history: 127.0.0.1:3054) with
// the vendor longerTimeout (6 min) so long history-v1 calls complete.
//
// Fatal-on-start errors exit non-zero (vendor: "Cannot connect to mongo.
// Exiting." — the container supervisor sees the crash).
package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"ollitex/go/libraries/ometrics"
	"os"
	"os/signal"
	"syscall"
	"time"

	ph "ollitex/go/services/project-history"
)

func main() {
	log.SetPrefix("project-history: ")
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	cfg := ph.Load()
	ctx := context.Background()

	app, err := ph.Build(ctx, cfg)
	if err != nil {
		log.Fatalf("cannot start: %v (vendor: fatal on connect failure)", err)
	}
	defer app.Close()

	// D22 A2: the metrics surface (internal port only).
	ometrics.Initialize()

	srv := &http.Server{
		Addr:              cfg.Bind(),
		Handler:           ometrics.HTTPMiddleware(ometrics.WithMetricsRoute(app.Handler)),
		ReadTimeout:       6 * time.Minute, // vendor Router.longerTimeout (all endpoints)
		WriteTimeout:      6 * time.Minute,
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		s := <-sig
		log.Printf("signal %s: shutting down", s)
		shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			log.Printf("shutdown: %v", err)
		}
	}()

	log.Printf("history starting up, listening on %s (vendor app.js parity)", srv.Addr)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve: %v", err)
	}
	log.Printf("history server stopped")
}
