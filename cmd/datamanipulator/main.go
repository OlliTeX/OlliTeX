// Command datamanipulator is the Go 1:1 rewrite of the Node datamanipulator
// service (services/datamanipulator/app/src/server.mjs).
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
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
	cfg := services.DMConfig{
		ProjectsRoot: firstNonEmpty(os.Getenv("DATAMANIPULATOR_PROJECTS_ROOT"), "/projects"),
		ServiceToken: os.Getenv("SHARED_SERVICE_TOKEN"),
	}
	listen := firstNonEmpty(os.Getenv("DATAMANIPULATOR_PORT"), "4001")
	srv := &http.Server{
		Addr:         ":" + listen,
		Handler:      services.NewDMHandlers(cfg).Mux(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() {
		log.Printf("datamanipulator (go) listening on %s", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		log.Print("datamanipulator: shutting down")
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
