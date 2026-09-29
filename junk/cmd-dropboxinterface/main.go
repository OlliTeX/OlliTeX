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

	dropboxinterface "ollitex/go/services/dropboxinterface"
)

func main() {
	cfg := dropboxinterface.DropboxConfig{
		ServiceToken: os.Getenv("SHARED_SERVICE_TOKEN"),
		APIBase:      firstNonEmpty(os.Getenv("DROPBOX_API_BASE"), "https://api.dropboxapi.com/2"), // dropbox SDK default (Node: new Dropbox({...}))
		ContentBase:  firstNonEmpty(os.Getenv("DROPBOX_CONTENT_BASE"), "https://content.dropboxapi.com/2"),
	}
	listen := firstNonEmpty(os.Getenv("DROPBOXINTERFACE_PORT"), "4003") // Node: process.env.DROPBOXINTERFACE_PORT || 4003
	handlers := &dropboxinterface.DropboxHandlers{Cfg: cfg}
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
