// Package mongoh is the thin shared helper for the Go service ports that talk
// to the shared Overleaf MongoDB (notifications, chat, docstore, history-v1).
//
// It centralises the connection-string derivation and connect/close lifecycle
// that all four services share, mirroring exactly how the Node services build
// settings.mongo.url and call mongoClient.connect(). It deliberately does NOT
// wrap the driver's CRUD surface — each service issues its own 1:1 queries via
// go.mongodb.org/mongo-driver so the correct semantics stay explicit and easy
// to verify against the Node originals.
package mongoh

import (
	"context"
	"net/url"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Node fallbacks (1:1 with the services' config defaults):
//
//	host  127.0.0.1
//	db    sharelatex
const (
	DefaultHost = "127.0.0.1"
	DefaultDB   = "sharelatex"
)

// DBFromURI extracts the logical database name from a mongodb:// (or
// mongodb+srv://) connection URI — the same database Node's `mongoClient.db()`
// (no argument) resolves to. Non-Mongo URIs and URIs without a database path
// fall back to `fallback` (or DefaultDB).
func DBFromURI(uri, fallback string) string {
	def := fallback
	if def == "" {
		def = DefaultDB
	}
	u, err := url.Parse(uri)
	if err != nil {
		return def
	}
	if u.Scheme != "mongodb" && u.Scheme != "mongodb+srv" {
		return def
	}
	name := strings.TrimPrefix(u.EscapedPath(), "/")
	if name == "" {
		return def
	}
	if d, derr := url.PathUnescape(name); derr == nil && d != "" {
		return d
	}
	return name
}

// Options bundles the connect settings.
type Options struct {
	// URI is the full connection string (use resolveURI from env, or supply
	// one directly in tests).
	URI string
	// DB is the logical database name (see DBFromURI).
	DB string
	// PingTimeout bounds the single connectivity ping at startup (default 5s).
	// The Node services hard-exit when they cannot reach Mongo; we surface the
	// same failure as a returned error so `main` can log+exit(1).
	PingTimeout time.Duration
}

// WithDefaults fills unset Options fields with 1:1 defaults.
func (o *Options) WithDefaults() {
	if o.URI == "" {
		o.URI = DefaultURI()
	}
	if o.DB == "" {
		o.DB = DBFromURI(o.URI, DefaultDB)
	}
	if o.PingTimeout <= 0 {
		o.PingTimeout = 5 * time.Second
	}
}

// DefaultURI mirrors the Node `settings.mongo.url` resolution:
//
//	MONGO_CONNECTION_STRING || OVERLEAF_MONGO_URL || `mongodb://${MONGO_HOST || '127.0.0.1'}/sharelatex`
//
// (Node reads settings.js `mongo.url` = OVERLEAF_MONGO_URL || mongodb://dockerhost/sharelatex;
// MONGO_CONNECTION_STRING is kept first for deployments that set it explicitly —
// the e2e stack sets both to the same value.)
func DefaultURI() string {
	if u := os.Getenv("MONGO_CONNECTION_STRING"); u != "" {
		return u
	}
	if u := os.Getenv("OVERLEAF_MONGO_URL"); u != "" {
		return u
	}
	host := os.Getenv("MONGO_HOST")
	if host == "" {
		host = DefaultHost
	}
	return "mongodb://" + host + "/" + DefaultDB
}

// Connect returns a verified *mongo.Client (it executes one ping within
// Options.PingTimeout so an unreachable Mongo fails fast, matching the Node
// services' connect-then-exit behaviour). The caller is responsible for Close.
func Connect(ctx context.Context, o Options) (*mongo.Client, error) {
	o.WithDefaults()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(o.URI))
	if err != nil {
		return nil, err
	}
	pctx, cancel := context.WithTimeout(ctx, o.PingTimeout)
	defer cancel()
	if perr := client.Ping(pctx, nil); perr != nil {
		cctx, ccancel := context.WithTimeout(context.Background(), o.PingTimeout)
		defer ccancel()
		_ = client.Disconnect(cctx)
		return nil, perr
	}
	return client, nil
}
