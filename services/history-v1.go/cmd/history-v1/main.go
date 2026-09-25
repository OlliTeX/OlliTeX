package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"history-v1/internal/config"
	"history-v1/internal/core"
	"history-v1/service/api"
	"history-v1/service/blobstore"
	"history-v1/service/chunkstore"
	"history-v1/service/historystore"
)

// limitedBody wraps the request body to apply the Node 12MB soft cap while
// still satisfying io.ReadCloser (handlers read r.Body as io.ReadCloser).
type limitedBody struct {
	rd io.Reader // capped reader
	cl io.Closer // original body (closed on Close)
}

func (l limitedBody) Read(p []byte) (int, error) { return l.rd.Read(p) }
func (l limitedBody) Close() error               { return l.cl.Close() }

func main() {
	cfg := config.FromEnv()

	// Storage (hermetic: in-memory fakes mirror the Node S3/persistor +
	// per-project BlobStore contracts).
	fp := historystore.NewFakePersister()
	hs := historystore.New(fp, "main")
	bs := blobstore.NewStore()
	cs := chunkstore.New(hs, func(p string) core.BlobStoreI { return bs.Project(p) })

	apiHandler := api.New(cs, bs, *cfg)

	jsonCap := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			// Node express.json({limit:'12MB'}); soft cap — the overflow 413
			// is exercised via createProjectBlob (currently 501 Not Ported).
			orig := r.Body
			r.Body = limitedBody{rd: io.LimitReader(orig, 12*1024*1024), cl: orig}
		}
		apiHandler.ServeHTTP(w, r)
	})

	// res.setTimeout(HTTPRequestTimeout ms, default 300000) -> Go server
	// read/idle timeouts.
	timeout := time.Duration(cfg.HTTPRequestTimeout) * time.Millisecond
	if timeout <= 0 {
		timeout = 300 * time.Second
	}

	port := cfg.Port
	if port == 0 {
		port = 3100
	}

	log.Printf("history-v1 (go) listening on :%d timeout=%v maxFileUploadSize=%d (hermetic)",
		port, timeout, cfg.MaxFileUploadSize)

	srv := &http.Server{
		Addr:        fmt.Sprintf(":%d", port),
		Handler:     jsonCap,
		ReadTimeout: timeout,
		IdleTimeout: timeout,
	}
	log.Fatal(srv.ListenAndServe())
}
