// Command githubinterface is the Go 1:1 rewrite of the Node githubinterface
// service (services/githubinterface/app/src/server.mjs).
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"ollitex/services"
)

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func main() {
	cfg := services.GHIConfig{
		WorkRoot:     firstNonEmpty(os.Getenv("GITHUBINTERFACE_WORKDIR_ROOT"), filepath.Join(os.TempDir(), "ghif")),
		ServiceToken: os.Getenv("SHARED_SERVICE_TOKEN"),
	}
	if ops := os.Getenv("GITHUBINTERFACE_MAX_OPS"); ops != "" {
		if n, err := strconv.Atoi(ops); err == nil {
			cfg.MaxOps = n
		}
	}
	listen := firstNonEmpty(os.Getenv("GITHUBINTERFACE_PORT"), "4013")
	mux := services.NewGHIHandlerMux(cfg)
	srv := &http.Server{
		Addr:         ":" + listen,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Minute,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() {
		log.Printf("githubinterface (go) listening on %s", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		log.Print("githubinterface: shutting down")
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}
}
