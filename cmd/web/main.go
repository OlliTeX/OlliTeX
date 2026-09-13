// Command web is the Go replacement of services/web (WEB_GO_PLAN.md).
//
// One binary, two profiles — selected by ENABLED_SERVICES exactly like
// the Node app (server-ce/runit/web-overleaf/run sets web,
// web-api-overleaf/run sets api). P0 runs as the SHADOW service
// (web-go-overleaf) on a distinct port; the nginx vhost-extras flip
// table routes individual prefixes here. See WEB_GO_PLAN.md §3.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/features/devcsrf"
	"ollitex/go/services/web/features/healthcheck"
	"ollitex/go/services/web/features/status"
)

func main() {
	cfg, err := core.LoadConfig()
	if err != nil {
		log.Fatalf("webgo: %v", err)
	}

	rdb, err := core.DialRedis(cfg.RedisAddr)
	if err != nil {
		// Node hard-exits on redis connect failure; for the SHADOW service
		// we stay up (health endpoints report 500 until redis is reachable)
		// and re-dial transparently per command.
		log.Printf("webgo: redis %s unreachable at boot: %v (re-dialing per command)", cfg.RedisAddr, err)
		rdb = &core.RedisClient{Addr: cfg.RedisAddr}
	}

	app := core.New(cfg, rdb)
	app.RegisterFeature(status.Feature)
	app.RegisterFeature(healthcheck.New(app))
	app.RegisterFeature(devcsrf.Feature)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// graceful shutdown (GracefulShutdown parity, minimal: stop
	// accepting, drain in-flight)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	go func() {
		<-ctx.Done()
		log.Printf("webgo: shutting down")
		shctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = srv.Shutdown(shctx)
	}()

	log.Printf("webgo: profile=%s listening on %s (shadow of services/web)", cfg.Profile, cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("webgo: %v", err)
	}
	// graceful shutdown handled above; reaching here means ListenAndServe
	// returned cleanly after Shutdown.
}

