// Command docstore is the Go 1:1 rewrite of the Node docstore service
// (services/docstore/app.js + app/js/*): the Overleaf text-document store,
// including the archive/unarchive round-trip through the FS persistor.
//
// Config surface = services/docstore/config/settings.defaults.cjs (env names
// and defaults kept 1:1):
//
//	MONGO_CONNECTION_STRING || mongodb://(MONGO_HOST || 127.0.0.1)/sharelatex
//	MONGO_HAS_SECONDARIES === 'true'
//	ARCHIVE_ON_SOFT_DELETE / KEEP_SOFT_DELETED_DOCS_ARCHIVED === 'true'
//	BACKEND, HEALTH_CHECK_PROJECT_ID
//	BUCKET_NAME || AWS_BUCKET || 'bucket'
//	MAX_DELETED_DOCS (2000), MAX_DOC_LENGTH (2 MB),
//	MAX_JSON_REQUEST_SIZE (12 MB), UN_ARCHIVE_BATCH_SIZE (50),
//	PARALLEL_ARCHIVE_JOBS (5), ARCHIVING_LOCK_DURATION_MS (60000)
//	port 3016 (internal.docstore.port), host LISTEN_ADDRESS || 127.0.0.1
package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	docstore "ollitex/go/services/docstore"
)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envOrChain(keys []string, def string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return def
}

func envIntOr(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func envInt64Or(k string, def int64) int64 {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func main() {
	uri := envOr("MONGO_CONNECTION_STRING", "")
	// Node: MONGO_CONNECTION_STRING || mongodb://${MONGO_HOST}/sharelatex ||
	// mongodb://127.0.0.1/sharelatex
	if uri == "" {
		uri = "mongodb://" + envOr("MONGO_HOST", "127.0.0.1") + "/sharelatex"
	}
	// Node mongoClient.db() takes the database from the URI path
	database := "sharelatex"
	if u, err := url.Parse(uri); err == nil && u.Path != "" {
		database = strings.TrimPrefix(u.Path, "/")
	}

	cfg := docstore.Config{
		MongoURI:                    uri,
		Database:                    database,
		Host:                        envOr("LISTEN_ADDRESS", "127.0.0.1"),
		Port:                        envIntOr("PORT", 3016), // internal.docstore.port (3016; PORT = shadow-run escape hatch)
		Backend:                     os.Getenv("BACKEND"),
		Bucket:                      envOrChain([]string{"BUCKET_NAME", "AWS_BUCKET"}, "bucket"),
		ArchiveOnSoftDelete:         os.Getenv("ARCHIVE_ON_SOFT_DELETE") == "true",
		KeepSoftDeletedDocsArchived: os.Getenv("KEEP_SOFT_DELETED_DOCS_ARCHIVED") == "true",
		HealthCheckProjectID:        os.Getenv("HEALTH_CHECK_PROJECT_ID"),
		HasSecondaries:              os.Getenv("MONGO_HAS_SECONDARIES") == "true",
		MaxDeletedDocs:              envIntOr("MAX_DELETED_DOCS", 0),
		MaxDocLength:                envInt64Or("MAX_DOC_LENGTH", 0),
		MaxJSONRequestSize:          envInt64Or("MAX_JSON_REQUEST_SIZE", 0),
		ArchiveBatchSize:            envIntOr("UN_ARCHIVE_BATCH_SIZE", 0),
		ParallelArchiveJobs:         envIntOr("PARALLEL_ARCHIVE_JOBS", 0),
		ArchivingLockMS:             time.Duration(envIntOr("ARCHIVING_LOCK_DURATION_MS", 0)) * time.Millisecond,
	}
	cfg.Defaults()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := docstore.NewMongoStore(ctx, cfg.MongoURI, cfg.Database, cfg.HasSecondaries, cfg.Log)
	if err != nil {
		log.Fatalf("docstore: mongo: %v", err)
	}

	// Archive backend selection 1:1 with DocArchiveManager/PersistorManager:
	// BACKEND '' → unconfigured Node (AbstractPersistor no-op → archive
	// disabled); 'fs' → FSPersistor; 's3' → S3Persistor (here: the local
	// SeaweedFS S3 gateway, anonymous by default).
	var archiver docstore.Archiver = docstore.NewFSArchiver()
	if cfg.Backend == "s3" {
		sa := docstore.NewS3Archiver(
			envOrChain([]string{"AWS_S3_ENDPOINT", "OVERLEAF_FILESTORE_S3_ENDPOINT"}, "http://127.0.0.1:8333"),
			envOrChain([]string{"AWS_ACCESS_KEY_ID", "AWS_KEY"}, ""),
			envOrChain([]string{"AWS_SECRET_ACCESS_KEY", "AWS_SECRET"}, ""),
		)
		if ini, ok := sa.(docstore.BucketInitializer); ok {
			if err := ini.EnsureBucket(ctx, cfg.Bucket); err != nil {
				log.Fatalf("docstore: s3 bucket %q unusable: %v", cfg.Bucket, err)
			}
		}
		archiver = sa
	}
	srv := docstore.NewServer(cfg, store, archiver)
	httpSrv := &http.Server{
		Addr:    net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Handler: srv.Router(),
	}
	log.Printf("docstore (go) listening on %s (mongo %s/%s)", httpSrv.Addr, database, uri)
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ListenAndServe() }()
	select {
	case <-ctx.Done():
		log.Print("docstore: shutting down")
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(sctx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}
}
