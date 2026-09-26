// Command realtime — the Go real-time service (D28a, S4 FLIP):
// the socket.io 0.9 event bus the IDE boots against (:3026), replacing
// the Node services/real-time.
//
// Env (Node `services/real-time/config/settings.defaults.cjs` parity):
//
//	LISTEN_ADDRESS          (default 127.0.0.1) — port is fixed 3026 (nginx /socket.io)
//	OVERLEAF_SESSION_SECRET — session cookie secret chain (comma-separated)
//	COOKIE_NAME             (default overleaf.sid)
//	REAL_TIME_REDIS_HOST || REDIS_HOST || 127.0.0.1
//	REAL_TIME_REDIS_PORT || REDIS_PORT || 6379
//	REAL_TIME_REDIS_PASSWORD || REDIS_PASSWORD || ''
//	WEB_API_HOST || WEB_HOST || 127.0.0.1
//	WEB_API_PORT || WEB_PORT || 3000 — the Go web API-PROFILE listener
//	(exactly the Node real-time default: the private join/flush APIs live on
//	the api profile, NOT the web profile on :4000)
//	WEB_API_USER            (default overleaf)
//	WEB_API_PASSWORD        (default password)
//	DOCUMENT_UPDATER_HOST || DOCUPDATER_HOST || 127.0.0.1 (:3003)
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	rt "ollitex/go/services/realtime"
)

func env(names ...string) (string, bool) {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v, true
		}
	}
	return "", false
}

func main() {
	level := slog.LevelInfo
	if lv := os.Getenv("LOGLEVEL"); lv != "" {
		switch strings.ToLower(lv) {
		case "debug":
			level = slog.LevelDebug
		case "warn", "warning":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		}
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	// redis — the same instance the Go web / collab / Node stack share.
	redisHost, _ := env("REAL_TIME_REDIS_HOST", "REDIS_HOST")
	if redisHost == "" {
		redisHost = "127.0.0.1"
	}
	redisPort := 6379
	if v, ok := env("REAL_TIME_REDIS_PORT", "REDIS_PORT"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			redisPort = n
		}
	}
	redisPass, _ := env("REAL_TIME_REDIS_PASSWORD", "REDIS_PASSWORD")
	g, err := rt.NewGoRedis(redisHost, redisPort, redisPass)
	if err != nil {
		log.Error("redis connect failed", "host", redisHost, "port", redisPort, "err", err)
		os.Exit(1)
	}
	log.Info("realtime: redis connected", "host", redisHost, "port", redisPort)

	secrets := []string{}
	if sc := os.Getenv("OVERLEAF_SESSION_SECRET"); sc != "" {
		for _, part := range strings.Split(sc, ",") {
			if strings.TrimSpace(part) != "" {
				secrets = append(secrets, strings.TrimSpace(part))
			}
		}
	}
	cookieName := "overleaf.sid"
	if v, ok := env("COOKIE_NAME"); ok {
		cookieName = v
	}
	sessions := &rt.SessionResolver{
		Source:     rt.NewRedisSession(g),
		CookieName: cookieName,
		Secrets:    secrets,
	}

	webHost, _ := env("WEB_API_HOST", "WEB_HOST")
	if webHost == "" {
		webHost = "127.0.0.1"
	}
	webPort := 3000
	if v, ok := env("WEB_API_PORT", "WEB_PORT"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			webPort = n
		}
	}
	webUser := "overleaf"
	if v, ok := env("WEB_API_USER"); ok {
		webUser = v
	}
	webPass := "password"
	if v, ok := env("WEB_API_PASSWORD"); ok {
		webPass = v
	}
	web := &rt.WebAPI{
		BaseURL: fmt.Sprintf("http://%s:%d", webHost, webPort),
		User:    webUser,
		Pass:    webPass,
	}
	dupHost, _ := env("DOCUMENT_UPDATER_HOST", "DOCUPDATER_HOST")
	if dupHost == "" {
		dupHost = "127.0.0.1"
	}
	flush := &rt.FlushAPI{BaseURL: fmt.Sprintf("http://%s:3003", dupHost)}

	bus := rt.New(rt.Options{
		Sessions: sessions,
		Web:      web,
		Flush:    flush,
		Redis:    g,
	})
	srv := rt.NewServer(bus)

	addr := net.JoinHostPort("127.0.0.1", "3026")
	if v, ok := env("LISTEN_ADDRESS"); ok {
		addr = net.JoinHostPort(v, "3026")
	}
	log.Info("realtime starting up, listening", "addr", addr)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("listener stopped", "err", err)
			os.Exit(1)
		}
	}()

	// graceful shutdown on SIGTERM/SIGINT (runit sv down)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	log.Info("shutting down")
	shctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shctx)
}
