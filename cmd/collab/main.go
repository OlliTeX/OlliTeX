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
//	                         N updates, oldest folded into one record).
//	                         The /hub config-DB value wins over this env
//	                         (configres: DB → env → default; binds on start).
//	COLLAB_COMPACT_EVERY     how often the server compacts a room
//	                         (0 = on room unload only — default; N = also after
//	                         every N persistence flushes); same config-DB
//	                         precedence as COLLAB_KEEP_VERSIONS.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	gredis "github.com/redis/go-redis/v9"

	"ollitex/go/libraries/configres"
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
	// ONE Mongo client for the whole service: auth gate, seed source, and
	// the versioned room store all share it (no per-component connections).
	mcl, mdb, err := collab.NewMongoClient(ctx, mongoURI, env("OLLITEX_DB_NAME", "sharelatex"))
	if err != nil {
		log.Fatalf("mongo: %v", err)
	}
	m := collab.NewMongoFrom(mdb)
	// Versioned room persistence = MongoStore (D19: Mongo is the system of
	// record — the filesystem fallback is dev/test-only; the prod container
	// has no writable DATA_DIR for it).
	store, err := collab.NewMongoStore(ctx, mdb)
	if err != nil {
		log.Fatalf("mongo store: %v", err)
	}
	defer func() { _ = mcl }() // lifetime = process; released on exit

	// Session cookie signatures: the SAME secret chain as the web (core/config.go):
	// OVERLEAF_SESSION_SECRET || CRYPTO_RANDOM (+ upcoming/fallback).
	secretChain := []string{os.Getenv("OVERLEAF_SESSION_SECRET")}
	if secretChain[0] == "" {
		secretChain[0] = os.Getenv("CRYPTO_RANDOM")
	}
	secretChain = append(secretChain,
		os.Getenv("SESSION_SECRET_UPCOMING"), os.Getenv("SESSION_SECRET_FALLBACK"))

	auth := &collab.SessionAuth{
		SessionDoc: collab.RedisSessionDoc(rdb, env("COOKIE_NAME", ""), secretChain...),
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
	// Config-DB (D6) precedence for the retention knobs: /hub-admin value →
	// env → default, via the shared configres contract (Open never creates
	// the file — a pre-config-DB deployment is bit-identical).
	cfgStore := configres.Open()
	if cfgStore != nil {
		defer cfgStore.Close()
	}
	keepVersions := configres.Int(cfgStore, "COLLAB_KEEP_VERSIONS", "COLLAB_KEEP_VERSIONS", 0)
	compactEvery := configres.Int(cfgStore, "COLLAB_COMPACT_EVERY", "COLLAB_COMPACT_EVERY", 0)

	// Seed source (S4 contract): a room adopts the project's CURRENT
	// main-file content read from the docstore — the SAME document the web's
	// editor surface renders (projectlist/docapi.go → Go docstore service):
	// projects doc → rootDoc_id → GET {WEB_DOCSTORE_URL}/project/{pid}/doc/{docID}
	// → {lines:[...]} → join("\n"). See seedsource.go. The projects reader
	// is the same Mongo client as the auth gate (one connection).
	seed := collab.NewSeedSource(m,
		env("WEB_DOCSTORE_URL", "http://127.0.0.1:3016"),
		env("V1_HISTORY_USER", ""), // basic-auth is optional (docstore is internal)
		env("V1_HISTORY_PASSWORD", ""),
		&http.Client{Timeout: 15 * time.Second})

	var sl *slog.Logger
	if os.Getenv("COLLAB_LOG_LEVEL") == "debug" {
		sl = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	lifecycle := func(name string) func(room string) {
		return func(room string) { log.Printf("collab: %s room=%s", name, room) }
	}
	svc, err := collab.New(collab.Options{
		Auth:             auth,
		Store:            store,
		Logger:           sl,
		OnFirstPeer:      lifecycle("first-peer"),
		OnLastPeer:       lifecycle("last-peer"),
		OnUnloadDocument: lifecycle("unload-doc"),
		KeepVersions:     keepVersions,
		CompactEvery:     compactEvery,
		AllowedOrigins:   origins,
		MaxConnections:   maxConn,
		MaxPeersPerRoom:  maxPeers,
		SeedFn:           seed.SeedText, // S4: rooms seed from live project content
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

	log.Printf("listening on %s (rooms under /collab/{projectId}, persistence=mongo/%s)", ln.Addr(), env("OLLITEX_DB_NAME", "sharelatex"))
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
