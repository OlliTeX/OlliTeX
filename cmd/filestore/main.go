// Command filestore is the Go 1:1 rewrite of the Node filestore service
// (services/filestore/app.js + app/js/*), on the local FS backend.
package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	filestore "ollitex/go/services/filestore"
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
	cfg := filestore.FSTConfig{
		// 1:1 with the Node config surface the CE image actually exports to the
		// filestore process (server-ce/config/settings.js ce-fs block +
		// services/filestore/config/settings.defaults.cjs). Unset values fall
		// back to the same Node defaults in FSTConfig.withDefaults.
		TemplateFiles:     os.Getenv("TEMPLATE_FILES_BUCKET_NAME"),
		ProjectBlobs:      os.Getenv("OVERLEAF_HISTORY_PROJECT_BLOBS_BUCKET"),
		GlobalBlobs:       os.Getenv("OVERLEAF_HISTORY_BLOBS_BUCKET"),
		EnableConversions: os.Getenv("ENABLE_CONVERSIONS") == "true",
		Converter:         os.Getenv("CONVERTER"),
	}
	// Node binds app.listen(port, host) with
	//   port = settings.internal.filestore.port || 3009   (fixed 3009)
	//   host = settings.internal.filestore.host || '0.0.0.0'
	// and the service config sets host = LISTEN_ADDRESS || '127.0.0.1', so the
	// effective default bind is 127.0.0.1:3009. Mirror that exactly.
	host := firstNonEmpty(os.Getenv("LISTEN_ADDRESS"), "127.0.0.1")
	listen := net.JoinHostPort(host, "3009")
	mux := filestore.NewFSTHandlers(cfg).Mux()
	srv := &http.Server{
		Addr:         listen,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() {
		log.Printf("filestore (go) listening on %s", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		log.Print("filestore: shutting down")
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(sctx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}
}
