// Command dropboxinterface is the Go 1:1 rewrite of the Node dropboxinterface
// service (services/dropboxinterface/app/src/server.mjs).
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

func main() {
	cfg := services.DropboxConfig{
		ServiceToken: os.Getenv("SHARED_SERVICE_TOKEN"),
		APIBase:      firstNonEmpty(os.Getenv("DROPBOX_API_BASE"), "https://api.dropbox.com/2"),
		ContentBase:  firstNonEmpty(os.Getenv("DROPBOX_CONTENT_BASE"), "https://content.dropboxapi.com/2"),
	}
	listen := firstNonEmpty(os.Getenv("PORT"), "3071")
	handlers := &services.DropboxHandlers{Cfg: cfg}
	srv := &http.Server{
		Addr:         ":" + listen,
		Handler:      handlers.Mux(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("dropboxinterface (go) listening on %s", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		log.Print("dropboxinterface: shutting down")
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

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
