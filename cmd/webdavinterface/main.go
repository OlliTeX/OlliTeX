// Command webdavinterface is the Go conversion of the Node.js
// services/webdavinterface microservice (1:1 behaviour). It serves:
//
//	POST   /check   verify credentials against the WebDAV server
//	POST   /list    list a directory
//	POST   /mkdir   create a directory (idempotent)
//	GET    /file    download a file (x-server-url / x-username / Basic auth)
//	POST   /file    upload/create a file (etag via If-Match)
//	DELETE /file    delete a file (404 -> notFound)
//	POST   /move    move/rename a file
//
// Auth: SHARED_SERVICE_TOKEN (Bearer or X-Service-Token), timing-safe. When
// unset (legacy) it accepts unauthenticated calls and warns, 1:1 with Node.
// Mirrors services/webdavinterface/app/src/server.mjs (default port 4002).
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
	port := 4002
	if p := os.Getenv("WEBDAVINTERFACE_PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			port = n
		}
	}
	handlers := &services.WebDAVHandlers{Cfg: services.WebDAVConfig{ServiceToken: os.Getenv("SHARED_SERVICE_TOKEN")}}
	srv := &http.Server{
		Addr:         net.JoinHostPort("0.0.0.0", strconv.Itoa(port)),
		Handler:      handlers.Mux(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
	}
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		log.Fatalf("webdavinterface: listen %s: %v", srv.Addr, err)
	}
	log.Printf("webdavinterface service running on port %d", port)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-stop
		log.Printf("webdavinterface shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()
	if err := srv.Serve(ln); err != http.ErrServerClosed {
		log.Fatalf("webdavinterface: serve: %v", err)
	}
	log.Printf("webdavinterface closed")
}
