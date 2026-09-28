// Package historyv1 is the Go 1:1 port of the Node history-v1 service
// (services/history-v1: app.js + api/* + storage/lib/*): the OT v1 version
// plane — chunk metadata (Mongo for 24-hex project ids, Postgres for numeric
// ids), chunk histories (S3 persistor, gzipped RawHistory keyed
// <projectKey>/<chunkKey>), and project blobs (Mongo blobs.<shard>
// collections).
//
// Wire contract 1:1 with the Node service:
//
//	basic auth on /api (security.js security.basicHttpAuth — user/pass from
//	    config; anonymous → 401 plain)
//	GET  /                     → ''
//	POST /api/projects         initializeProject → 200 {projectId} / 409
//	POST /api/projects/:id/clone            cloneProject (IncrementalResponse)
//	POST /api/projects/:id                  persistChanges (DU era)
//	GET  /api/projects/:id                  deleteProject? no — DELETE → 204
//	GET  /api/projects/:id/latest/content   snapshot raw (eager files)
//	GET  /api/projects/:id/latest/hashed_content
//	GET  /api/projects/:id/latest/history   ChunkResponse raw / 404
//	GET  /api/projects/:id/latest/history/raw
//	GET  /api/projects/:id/latest/persistedHistory
//	GET  /api/projects/:id/versions/:v/history | /content
//	GET  /api/projects/:id/timestamp/:ts/history
//	GET  /api/projects/:id/changes?since=N  {changes:[...],hasMore} / 400
//	GET  /api/projects/:id/latest/zip | version/:v/zip
//	POST /api/projects/:id/version/:v/zip   createZip {zipUrl}
//	blobs: GET/HEAD/PUT /api/projects/:id/blobs/:hash (+copy ?copyFrom)
//	GET  /api/projects/blob-stats | :id/blob-stats
//
// Errors (render.js): 400/404/409/413 → {"message": "<Status>" [, message]};
// OError → 500 {"message":"<Name>: <message>"} (oerror rendering).
package historyv1

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.mongodb.org/mongo-driver/mongo"

	persistors "ollitex/go/libraries/persistors"
)

// Config mirrors services/history-v1/config/default.json + the server-ce
// custom-environment-variables mapping (env names kept 1:1).
type Config struct {
	// Mongo (storage/lib/mongodb.js)
	MongoURI string

	// Postgres (storage/lib/knex.js)
	MongoURIReadOnly string // HISTORY_FOLLOWER — informational (parity)
	PGDSN            string // DATABASE_URL || HISTORY_CONNECTION_STRING

	// Persistor (S3; G2 STOR-1 — the fs backend is retired)
	PersistorEndpoint  string // AWS_S3_ENDPOINT || OVERLEAF_HISTORY_S3_ENDPOINT
	PersistorKey       string // AWS_ACCESS_KEY_ID
	PersistorSecret    string // AWS_SECRET_ACCESS_KEY
	PersistorPathStyle bool   // AWS_S3_PATH_STYLE === 'true'
	ChunkBucket        string // OVERLEAF_HISTORY_CHUNKS_BUCKET || 'chunks'
	ZipBucket          string // OVERLEAF_HISTORY_ZIPS_BUCKET || 'zips'

	// Security (api/middleware/security.js: security.basicHttpAuth)
	SecurityUser     string // SECURITY_BASIC_AUTH_USER     || (OT_SECURITY_BASIC_HTTP_AUTH_USER default 'staging')
	SecurityUserPass string // || basicHttpAuth.password ('password')
	// Empty user/pass → auth disabled (dev mode), like the Node middleware.

	// Service
	Host string // LISTEN_ADDRESS || 0.0.0.0 (Node: host not pinned ⇒ 0.0.0.0)
	Port int    // internal.history_v1.port (3100)
	Log  func(format string, args ...any)
}

// DefaultCfg applies the Node defaults.
func (c *Config) Defaults() {
	if c.ChunkBucket == "" {
		c.ChunkBucket = "chunks"
	}
	if c.ZipBucket == "" {
		c.ZipBucket = "zips"
	}
	if c.Port == 0 {
		c.Port = 3100
	}
	if c.Log == nil {
		c.Log = log.Printf
	}
}

// FromEnv builds the config from the environment (1:1 env names).
func FromEnv() Config {
	c := Config{}
	c.MongoURI = os.Getenv("MONGO_CONNECTION_STRING")
	if c.MongoURI == "" {
		if host := os.Getenv("MONGO_HOST"); host != "" {
			c.MongoURI = "mongodb://" + host + "/sharelatex"
		} else {
			c.MongoURI = "mongodb://127.0.0.1/sharelatex"
		}
	}
	c.PGDSN = os.Getenv("HISTORY_CONNECTION_STRING")
	if c.PGDSN == "" {
		c.PGDSN = os.Getenv("DATABASE_URL")
	}
	penv := func(k string) string { return os.Getenv(k) }
	c.PersistorEndpoint = penv("AWS_S3_ENDPOINT")
	if c.PersistorEndpoint == "" {
		c.PersistorEndpoint = penv("OVERLEAF_HISTORY_S3_ENDPOINT")
	}
	c.PersistorKey = penv("AWS_ACCESS_KEY_ID")
	if c.PersistorKey == "" {
		c.PersistorKey = penv("OVERLEAF_HISTORY_S3_ACCESS_KEY_ID")
	}
	c.PersistorSecret = penv("AWS_SECRET_ACCESS_KEY")
	if c.PersistorSecret == "" {
		c.PersistorSecret = penv("OVERLEAF_HISTORY_S3_SECRET_ACCESS_KEY")
	}
	c.PersistorPathStyle = penv("AWS_S3_PATH_STYLE") == "true"
	if penv("OVERLEAF_HISTORY_S3_PATH_STYLE") == "true" {
		c.PersistorPathStyle = true
	}
	c.ChunkBucket = penv("OVERLEAF_HISTORY_CHUNKS_BUCKET")
	c.ZipBucket = penv("OVERLEAF_HISTORY_ZIPS_BUCKET")

	u := penv("SECURITY_BASIC_AUTH_USER")
	if u == "" {
		u = "staging"
	}
	c.SecurityUser = u
	c.SecurityUserPass = penv("V1_HISTORY_PASSWORD")
	if c.SecurityUserPass == "" {
		c.SecurityUserPass = "password"
	}

	c.Host = os.Getenv("LISTEN_ADDRESS")
	if c.Host == "" {
		c.Host = "0.0.0.0"
	}
	if port := os.Getenv("PORT"); port != "" {
		if n, err := strconv.Atoi(port); err == nil {
			c.Port = n
		}
	}
	c.Defaults()
	return c
}

// Service is the history-v1 service (Node: app.js + controllers + storage).
type Service struct {
	Cfg       Config
	Mongo     *mongo.Database // sharelatex db (chunks, blobs.*, projects, global blobs)
	Persistor persistors.Persistor
	History   *HistoryStore
	Blob      *BlobStores  // mongo blob store (blobs / blobs.<shard> collections)
	Chunks    *ChunkStores // backend-routed chunk store
	Ext       Extender     // redis buffer seam (Node chunk_store/redis.js)
}

// New assembles the service (all stores live behind small interfaces so unit
// tests can fake them).
func New(cfg Config, mongo *mongo.Database, persistor persistors.Persistor, pgPool *pgxpool.Pool) *Service {
	cfg.Defaults()
	hist := NewHistoryStore(persistor, cfg.ChunkBucket)
	cs := NewChunkStores(cfg, mongo, hist, NewMongoBuffer())
	if pgPool != nil {
		cs.PG = &pgChunkBackend{pg: pgPool}
	}
	return &Service{
		Cfg:       cfg,
		Mongo:     mongo,
		Persistor: persistor,
		History:   hist,
		Blob:      &BlobStores{db: mongo, pers: persistor, PG: pgPool},
		Chunks:    cs,
		Ext:       NewMongoBuffer(),
	}
}

// ---------- wire helpers ----------

var objectIdRe = regexp.MustCompile(`^[0-9a-f]{24}$`)

func isMongoID(id string) bool { return objectIdRe.MatchString(id) }

// numericID: the PG-plane project ids (Node blob_store/chunk_store: "Numeric
// ids use the Postgres backend").
func numericID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func jsonRes(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// render — api/controllers/render.js ({"message": status text or custom}).
var statusTexts = map[int]string{
	400: "Bad Request", 404: "Not Found", 409: "Conflict", 413: "Payload Too Large",
	422: "Unprocessable Entity", 429: "Too Many Requests", 500: "Internal Server Error",
}

func renderErr(w http.ResponseWriter, status int, message ...string) {
	msg := statusTexts[status]
	if len(message) > 0 && message[0] != "" {
		msg = message[0]
	}
	jsonRes(w, status, map[string]any{"message": msg})
}

// notPersisted maps chunk/blob not-found to the Node 404 render.
func notFound(w http.ResponseWriter) { renderErr(w, 404) }

func conflict(w http.ResponseWriter, msg ...string) { renderErr(w, 409, msg...) }

// ---------- routes (api/routes/projects.js 1:1) ----------

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(""))
	})

	auth := s.authMiddleware()
	api := func(pattern, name string, h http.HandlerFunc) {
		mux.HandleFunc(pattern, auth(h))
	}
	api("POST /api/projects", "initialize", s.initializeProject)
	api("POST /api/projects/{id}/clone", "clone", s.cloneProject)
	api("DELETE /api/projects/{id}", "delete", s.deleteProject)
	api("GET /api/projects/{id}/latest/content", "latestContent", s.getLatestContent)
	api("GET /api/projects/{id}/latest/hashed_content", "latestHashed", s.getLatestHashedContent)
	api("GET /api/projects/{id}/latest/history", "latestHistory", s.getLatestHistory)
	api("GET /api/projects/{id}/latest/history/raw", "latestHistoryRaw", s.getLatestHistoryRaw)
	api("GET /api/projects/{id}/latest/persistedHistory", "latestPersisted", s.getLatestHistory)
	api("GET /api/projects/{id}/versions/{version}/history", "historyAtVersion", s.getHistory)
	api("GET /api/projects/{id}/versions/{version}/content", "contentAtVersion", s.getContentAtVersion)
	api("GET /api/projects/{id}/timestamp/{timestamp}/history", "historyBefore", s.getHistoryBefore)
	api("GET /api/projects/{id}/changes", "changes", s.getChanges)
	api("GET /api/projects/{id}/latest/zip", "latestZip", s.getLatestZip)
	api("GET /api/projects/{id}/version/{version}/zip", "zipAtVersion", s.getZip)
	api("POST /api/projects/{id}/version/{version}/zip", "createZip", s.createZip)
	api("GET /api/projects/{id}/blobs/{hash}", "getBlob", s.getProjectBlob)
	api("HEAD /api/projects/{id}/blobs/{hash}", "headBlob", s.headProjectBlob)
	api("PUT /api/projects/{id}/blobs/{hash}", "createBlob", s.createProjectBlob)
	api("POST /api/projects/{id}/blobs/{hash}", "copyBlob", s.copyProjectBlob)
	api("POST /api/projects/blob-stats", "blobStats", s.getProjectBlobsStats)
	api("POST /api/projects/{id}/blob-stats", "projectBlobStats", s.getBlobStats)

	return withErrors(mux, s.Cfg.Log)
}

// authMiddleware — Node api/middleware/security.js setupBasicHttpAuthForSwaggerDocs:
// every /api route requires basic auth when user+pass are configured.
func (s *Service) authMiddleware() func(http.HandlerFunc) http.HandlerFunc {
	user, pass := s.Cfg.SecurityUser, s.Cfg.SecurityUserPass
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if user != "" && pass != "" && strings.HasPrefix(r.URL.Path, "/api/") {
				u, p, ok := r.BasicAuth()
				if !ok || u != user || p != pass {
					w.Header().Set("WWW-Authenticate", `Basic realm="api"`)
					jsonRes(w, 401, map[string]any{"message": "Authentication Required"})
					return
				}
			}
			next(w, r)
		}
	}
}

// withErrors — Node express error handler for uncaught errors (app.js + oerror
// rendering): OError → 500 {"message":"<Name>: <msg>"}, otherwise 500.
func withErrors(h http.Handler, logf func(string, ...any)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
	})
}

var _ = context.Background
var _ = time.Second
var _ = strings.TrimSpace
