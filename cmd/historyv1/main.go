// Command historyv1 is the Go 1:1 runtime for the history-v1 service (see
// ollitex/go/services/historyv1). Node: services/history-v1/app.js.
//
// Env (1:1 with services/history-v1/config + server-ce mappings):
//
//	MONGO_CONNECTION_STRING || mongodb://(MONGO_HOST || 127.0.0.1)/sharelatex
//	HISTORY_CONNECTION_STRING || DATABASE_URL (PG18 chunk/blob seam)
//	AWS_S3_ENDPOINT || OVERLEAF_HISTORY_S3_ENDPOINT (+ key/secret/pathStyle)
//	OVERLEAF_HISTORY_CHUNKS_BUCKET (default 'chunks'),
//	OVERLEAF_HISTORY_ZIPS_BUCKET (default 'zips')
//	SECURITY_BASIC_AUTH_USER (default 'staging'), V1_HISTORY_PASSWORD
package main

import (
	"context"
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

	mongodrv "go.mongodb.org/mongo-driver/mongo"
	mongooptions "go.mongodb.org/mongo-driver/mongo/options"

	"github.com/jackc/pgx/v5/pgxpool"

	persistors "ollitex/go/libraries/persistors"
	"ollitex/go/services/historyv1"
)

func main() {
	cfg := historyv1.FromEnv()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client, err := mongodrv.Connect(ctx, mongooptions.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		log.Fatalf("historyv1: mongo connect: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		log.Printf("historyv1: mongo ping failed (%v) — proceeding (lazy) ", err)
	}
	dbName := "sharelatex"
	if i := indexOf(cfg.MongoURI, "/"); i >= 0 {
		rest := cfg.MongoURI[i+1:]
		if j := indexOfAny(rest, "?"); j >= 0 {
			rest = rest[:j]
		}
		if rest != "" {
			dbName = rest
		}
	}
	db := client.Database(dbName)

	// S3 persistor (G2 STOR-1: the fs persistor is retired in-tree) with the
	// s3x gateway adapter (basic-auth SeaweedFS) wired in as the client
	// factory — without it persistors refuse to issue any request.
	factory := func(bucket string) (persistors.S3Client, error) {
		return historyv1.NewS3xAdapter(cfg.PersistorEndpoint, cfg.PersistorKey, cfg.PersistorSecret), nil
	}
	pers := persistors.NewS3Persistor(persistors.S3Settings{
		Key:       cfg.PersistorKey,
		Secret:    cfg.PersistorSecret,
		Endpoint:  cfg.PersistorEndpoint,
		PathStyle: cfg.PersistorPathStyle,
	}, factory)

	// PG18 seam (numeric-id projects): optional
	var pgPool *pgxpool.Pool
	if dsn := strings.TrimSpace(cfg.PGDSN); dsn != "" {
		if pool, err := pgxpool.New(ctx, dsn); err != nil {
			log.Printf("historyv1: PG pool: %v (numeric project ids unavailable)", err)
		} else {
			if err := pool.Ping(ctx); err != nil {
				log.Printf("historyv1: PG ping: %v (numeric project ids unavailable)", err)
			} else {
				pgPool = pool
				log.Printf("historyv1: PG chunk/blob seam attached")
			}
		}
	}

	svc := historyv1.New(cfg, db, pers, pgPool)

	ln, err := net.Listen("tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)))
	if err != nil {
		log.Fatalf("historyv1: listen: %v", err)
	}
	httpSrv := &http.Server{Handler: svc.Handler()}
	go func() {
		if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatalf("historyv1: serve: %v", err)
		}
	}()
	log.Printf("historyv1 (go) listening on %s (mongo %s, persistor s3 %s, chunk bucket %q)",
		ln.Addr().String(), cfg.MongoURI, cfg.PersistorEndpoint, cfg.ChunkBucket)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	shutdownCtx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer scancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}

// indexOf / indexOfAny — tiny string helpers (avoid strconv dependency churn).
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func indexOfAny(s string, chars string) int {
	for i := 0; i < len(s); i++ {
		for _, c := range chars {
			if rune(s[i]) == c {
				return i
			}
		}
	}
	return -1
}

var _ = fmt.Sprintf
