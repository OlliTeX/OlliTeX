// Command chat is the 1:1 Go stand-in for services/chat.
//
// Node config (fixed for drop-in behavior):
//
//	host  LISTEN_ADDRESS || 127.0.0.1
//	port  3010
//	mongo MONGO_CONNECTION_STRING || mongodb://${MONGO_HOST || 127.0.0.1}/sharelatex
//
// The service shares the project database with the rest of Overleaf: rooms,
// messages, users, projects, notifications, notificationsPreferences and
// emailNotifications are all in that one DB (default name 'sharelatex').
package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"ollitex/go/mongoh"
	"ollitex/go/services/chat"
)

func main() {
	cfg := chat.Config{}
	cfg.WithDefaults()
	// PORT = shadow-run escape hatch (cutover plan A4); default stays 3010.
	if p := os.Getenv("PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			cfg.Port = n
		}
	}

	client, err := mongoh.Connect(context.Background(), mongoh.Options{
		URI:         cfg.MongoURI,
		DB:          cfg.DB,
		PingTimeout: 10 * time.Second,
	})
	if err != nil {
		log.Fatalf("chat: mongo: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := client.Disconnect(ctx); err != nil {
			log.Printf("chat: disconnect: %v", err)
		}
	}()
	log.Printf("chat: connected to mongo, db=%q", cfg.DB)

	store := chat.NewMongoStore(client, cfg.DB)
	srv := chat.NewServer(store, cfg)

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("chat: listen: %v", err)
	}
	log.Printf("chat: listening on http://%s", listener.Addr())

	httpSrv := &http.Server{
		Handler:           srv.Router(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := httpSrv.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("chat: serve: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Printf("chat: shutdown: %v", err)
	}
}
