// Command collab — the Yjs/Ygo collaboration service (ARC-9, D19).
//
// Replaces the node real-time service for editor collaboration: a
// Hocuspocus-compatible y-protocols WebSocket relay (ygo) with OlliTeX
// session + project-role authorization and a versioned per-project update
// log (the new history model; old OT history is not read — D19 hard cut).
//
// Rooms:  /collab/{projectId}   (room name = project id)
// Auth:   COOKIE_NAME cookie (default overleaf.sid) -> shared Redis session
//
//	store -> passport.user._id -> project role (owner/collab = rw,
//	readOnly_refs = read-only peer) -> user not frozen.
//
// Env (same precedence as the other services):
//
//	COLLAB_LISTEN          listen addr (default :3450)
//	MONGO_CONNECTION_STRING || OVERLEAF_MONGO_URL || mongodb://127.0.0.1/sharelatex
//	OLLITEX_DB_NAME        mongo db name (default: the URI's db, else "sharelatex")
//	OVERLEAF_REDIS_HOST || REDIS_HOST (127.0.0.1)
//	OVERLEAF_REDIS_PORT || REDIS_PORT (6379)
//	OVERLEAF_REDIS_PASS || REDIS_PASSWORD
//	COLLAB_DATA_DIR        persistence root (default /data/collab-docs;
//	                       override for dev: e.g. /tmp/collab-docs)
//	COLLAB_ALLOWED_ORIGINS comma-separated origins ("*" = any — see SECURITY
//	                       of ygo: cookie auth + "*" = CSWSH risk; prefer the
//	                       public origin)
//	COLLAB_MAX_CONNECTIONS / COLLAB_MAX_PEERS_PER_ROOM (0 = unlimited)
//	COLLAB_KEEP_VERSIONS     history retention for server auto-compaction
//	                         (0 = keep-all — default; N = retain most recent
//	                         N updates, oldest folded into one record)
//	COLLAB_COMPACT_EVERY     how often the server compacts a room
//	                         (0 = on room unload only — default; N = also after
//	                         every N persistence flushes)
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	gredis "github.com/redis/go-redis/v9"

	"ollitex/go/services/collab"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	log.SetPrefix("collab: ")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- stores ---
	host := env("OVERLEAF_REDIS_HOST", env("REDIS_HOST", "127.0.0.1"))
	port, _ := strconv.Atoi(env("OVERLEAF_REDIS_PORT", env("REDIS_PORT", "6379")))
	rdb := gredis.NewClient(&gredis.Options{
		Addr:     net.JoinHostPort(host, strconv.Itoa(port)),
		Password: env("OVERLEAF_REDIS_PASS", env("REDIS_PASSWORD", "")),
	})

	mongoURI := env("MONGO_CONNECTION_STRING", env("OVERLEAF_MONGO_URL", "mongodb://127.0.0.1:27017/sharelatex"))
	m, err := collab.NewMongo(ctx, mongoURI, env("OLLITEX_DB_NAME", "sharelatex"))
	if err != nil {
		log.Fatalf("mongo: %v", err)
	}

	auth := &collab.SessionAuth{
		SessionDoc: collab.RedisSessionDoc(rdb, env("COOKIE_NAME", "")),
		M:          m,
		Ctx:        ctx,
	}

	var origins []string
	if o := os.Getenv("COLLAB_ALLOWED_ORIGINS"); o != "" {
		for _, p := range strings.Split(o, ",") {
			if p = strings.TrimSpace(p); p != "" {
				origins = append(origins, p)
			}
		}
	}
	maxConn, _ := strconv.Atoi(env("COLLAB_MAX_CONNECTIONS", "0"))
	maxPeers, _ := strconv.Atoi(env("COLLAB_MAX_PEERS_PER_ROOM", "0"))
	keepVersions, _ := strconv.Atoi(env("COLLAB_KEEP_VERSIONS", "0"))
	compactEvery, _ := strconv.Atoi(env("COLLAB_COMPACT_EVERY", "0"))

	svc, err := collab.New(collab.Options{
		Auth:            auth,
		DataDir:         env("COLLAB_DATA_DIR", "/data/collab-docs"),
		KeepVersions:    keepVersions,
		CompactEvery:    compactEvery,
		AllowedOrigins:  origins,
		MaxConnections:  maxConn,
		MaxPeersPerRoom: maxPeers,
	})
	if err != nil {
		log.Fatalf("init: %v", err)
	}

	// --- HTTP: /healthz + room endpoint (/collab/{projectId} — room = base
	// path segment, exactly what the ygo server derives) ---
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok","service":"collab","engine":"ygo"}`)
	})
	mux.Handle("/collab/", svc)

	ln, err := net.Listen("tcp", env("COLLAB_LISTEN", ":3450"))
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	httpSrv := &http.Server{Handler: mux}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
	}()

	log.Printf("listening on %s (rooms under /collab/{projectId}, persistence=%s)", ln.Addr(), env("COLLAB_DATA_DIR", "/data/collab-docs"))
	errCh := make(chan error, 1)
	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	select {
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := svc.Shutdown(sctx); err != nil {
			log.Printf("shutdown: %v", err)
		}
		log.Print("bye")
	case err := <-errCh:
		log.Fatalf("serve: %v", err)
	}
}
