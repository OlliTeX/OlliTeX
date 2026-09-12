// Command linked-url-proxy is the Go conversion of the Node.js
// services/linked-url-proxy microservice (1:1 behaviour). It serves:
//
//	GET /?url=<target>   fetch <target> (sanitised, allow/blocked-IP checked,
//	                     DNS-pinned, size/redirect/timeout guarded) and stream
//	                     the response body back.
//	GET /status          liveness check.
//
// It mirrors services/linked-url-proxy/app.mjs: binds host:port from the
// environment (LINKED_URL_PROXY_HOST, default 127.0.0.1:3066) and shuts down
// gracefully on SIGTERM/SIGINT.
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

	"ollitex/services"
)

func main() {
	cfg := services.NewLinkedURLProxyConfigFromEnv(os.Getenv)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"linked-url-proxy is up"}`))
		case "/":
			cfg.Handler()(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("linked-url-proxy: listen on %s: %v", addr, err)
	}
	log.Printf("linked-url-proxy listening at %s", addr)

	// Graceful shutdown on SIGTERM/SIGINT (1:1 with the Node SIGTERM handler).
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-stop
		log.Printf("linked-url-proxy shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	if err := srv.Serve(ln); err != http.ErrServerClosed {
		log.Fatalf("linked-url-proxy: serve: %v", err)
	}
	log.Printf("linked-url-proxy closed")
}
