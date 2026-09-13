// Command notifications is the Go 1:1 replacement for services/notifications
// (Node, port 3042). It is a standalone binary that speaks the same HTTP API
// and reads/writes the same shared `notifications` MongoDB collection, so it can
// substitute for the Node service as a drop-in.
//
// Boot parity with the Node app.ts:
//   - connect to Mongo first; on failure log fatal and exit(1)
//   - then bind host:port (host = LISTEN_ADDRESS||127.0.0.1, port 3042 fixed)
//   - log "notifications starting up, listening on <host>:<port>"
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"ollitex/go/mongoh"
	"ollitex/go/services/notifications"
)

func main() {
	cfg := notifications.Config{
		Host:       env("LISTEN_ADDRESS", "127.0.0.1"),
		Port:       envInt("PORT", 3042), // Node: fixed 3042; PORT = shadow-run escape hatch (A4)
		MongoURI:   mongoURI(),
		DB:         mongoh.DBFromURI(mongoURI(), "sharelatex"),
		Collection: "notifications",
	}
	log.Println("notifications: connecting to", cfg.MongoURI, "db:", cfg.DB)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := mongoh.Connect(ctx, mongoh.Options{URI: cfg.MongoURI, DB: cfg.DB})
	if err != nil {
		log.Fatalf("notifications: fatal: cannot connect to mongo: %v", err)
	}
	store := notifications.NewMongoStore(client, cfg.DB, cfg.Collection)
	server := notifications.NewServer(store, cfg)

	srv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:           server.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("notifications starting up, listening on %s:%d", cfg.Host, cfg.Port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("notifications listen: %v", err)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// mongoURI mirrors the Node settings.mongo.url resolution:
// MONGO_CONNECTION_STRING || OVERLEAF_MONGO_URL || mongodb://host/sharelatex.
func mongoURI() string {
	for _, k := range []string{"MONGO_CONNECTION_STRING", "OVERLEAF_MONGO_URL"} {
		if u := os.Getenv(k); u != "" {
			return u
		}
	}
	host := env("MONGO_HOST", "127.0.0.1")
	return "mongodb://" + host + "/sharelatex"
}
