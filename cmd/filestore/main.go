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
		// filestore process (server-ce/config/settings.js `switch
		// (OVERLEAF_FILESTORE_BACKEND)` block + the s3 fallback env names
		// from services/filestore/config/settings.defaults.cjs). In s3 mode
		// the three bucket values are BUCKET NAMES, not directories.
		TemplateFiles:     firstNonEmpty(os.Getenv("OVERLEAF_FILESTORE_TEMPLATE_FILES_BUCKET_NAME"), os.Getenv("TEMPLATE_FILES_BUCKET_NAME")),
		ProjectBlobs:      firstNonEmpty(os.Getenv("OVERLEAF_HISTORY_PROJECT_BLOBS_BUCKET"), os.Getenv("OVERLEAF_EDITOR_PROJECT_BLOBS_BUCKET")),
		GlobalBlobs:       firstNonEmpty(os.Getenv("OVERLEAF_HISTORY_BLOBS_BUCKET"), os.Getenv("OVERLEAF_EDITOR_BLOBS_BUCKET")),
		EnableConversions: os.Getenv("ENABLE_CONVERSIONS") == "true",
		Converter:         os.Getenv("CONVERTER"),
		Backend:           firstNonEmpty(os.Getenv("OVERLEAF_FILESTORE_BACKEND"), os.Getenv("FILESTORE_BACKEND"), os.Getenv("BACKEND")),
		S3Endpoint:        firstNonEmpty(os.Getenv("OVERLEAF_FILESTORE_S3_ENDPOINT"), os.Getenv("AWS_S3_ENDPOINT"), "http://127.0.0.1:8333"),
		S3Key:             firstNonEmpty(os.Getenv("OVERLEAF_FILESTORE_S3_ACCESS_KEY_ID"), os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_KEY")),
		S3Secret:          firstNonEmpty(os.Getenv("OVERLEAF_FILESTORE_S3_SECRET_ACCESS_KEY"), os.Getenv("AWS_SECRET_ACCESS_KEY"), os.Getenv("AWS_SECRET")),
	}
	host := firstNonEmpty(os.Getenv("LISTEN_ADDRESS"), "127.0.0.1")
	port := firstNonEmpty(os.Getenv("PORT"), "3009") // Node: fixed 3009; PORT is a shadow-run escape hatch
	listen := net.JoinHostPort(host, port)
	h, herr := filestore.NewFSTHandlers(cfg)
	if herr != nil {
		log.Fatalf("filestore: %v", herr)
	}
	mux := h.Mux()
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
