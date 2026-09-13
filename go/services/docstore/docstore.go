// Package docstore is the 1:1 Go conversion of services/docstore: the
// Overleaf text-document store (routes under /project/:id, archive
// unarchive, /health_check, /status). It reads and writes the shared `docs`
// collection with exactly the same queries, response shapes and error
// bodies as the Node original, and implements the FS persistor archive path
// 1:1 (flat `<bucket>/<projectId>_<docId>` files with the md5 round-trip
// check).
//
// Contract locked from the Node source (ext-6.3.0-port) plus live probes of
// the running Node service:
//
//   - validation runs in REQ_VALIDATION_MODE=enforce-log (production value)
//     ⇒ every schema failure throws: any issue rooted at params → 404
//     {"error":"Validation error: …","statusCode":404}, otherwise 400; all
//     issues in the request appear in schema order joined by "; ".
//   - express.json overflow (12MB updateDoc / 100kb patchDoc) and malformed
//     JSON bodies both fall through to the service's generic error handler:
//     500 "Oops, something went wrong" (text/html; charset=utf-8).
//   - res.sendStatus bodies are Express status texts (text/plain;
//     charset=utf-8): "Not Found" (404), "Conflict" (409), "OK" (200);
//     204 sends no body. res.status(n).send(string) bodies are text/html.
//   - unmatched route or wrong method → Express fallback 404 HTML page
//     "Cannot <METHOD> <path>" (verified byte counts 176/146).
//   - unknown-path bodies, ETag headers aside, otherwise 1:1.

package docstore

import (
	"context"
	"errors"
	"time"
)

// Node-side sentinel errors (services/docstore/app/js/Errors.js). The Go
// service maps them to the same HTTP outcomes as app.js's handlers.
var (
	ErrNotFound     = errors.New("Not found")
	ErrDocModified  = errors.New("doc rev has changed")
	ErrDocRevValue  = errors.New("doc rev mismatch")
	ErrVersionDown  = errors.New("rejecting stale update")
	ErrNoLines      = errors.New("doc has no lines")
	ErrNullByte     = errors.New("null bytes detected")
	ErrMd5Mismatch  = errors.New("md5 mismatch")
	ErrNotImpl      = errors.New("not implemented")
	ErrArchiveNoCfg = errors.New("found archived doc, but archiving backend is not configured")
	ErrArchiveFmt   = errors.New("I don't understand the doc format in s3")
)

// Config mirrors services/docstore/config/settings.defaults.cjs (env names
// and defaults kept 1:1).
type Config struct {
	// Mongo
	MongoURI string // MONGO_CONNECTION_STRING || mongodb://(MONGO_HOST||127.0.0.1)/sharelatex
	Database string // database segment of MongoURI (default sharelatex)

	// HTTP
	Host string // LISTEN_ADDRESS || 127.0.0.1
	Port int    // internal.docstore.port (3016)

	// Limits
	MaxDocLength        int64         // MAX_DOC_LENGTH || 2*1024*1024
	MaxJSONRequestSize  int64         // MAX_JSON_REQUEST_SIZE || 12*1024*1024
	MaxDeletedDocs      int           // MAX_DELETED_DOCS || 2000
	ArchiveBatchSize    int           // UN_ARCHIVE_BATCH_SIZE || 50
	ParallelArchiveJobs int           // PARALLEL_ARCHIVE_JOBS || 5
	ArchivingLockMS     time.Duration // ARCHIVING_LOCK_DURATION_MS || 60000

	// Archive (object-persistor fs backend)
	ArchiveOnSoftDelete         bool   // ARCHIVE_ON_SOFT_DELETE === 'true'
	KeepSoftDeletedDocsArchived bool   // KEEP_SOFT_DELETED_DOCS_ARCHIVED === 'true'
	Backend                     string // BACKEND ('' | 'fs' | …)
	Bucket                      string // BUCKET_NAME || AWS_BUCKET || 'bucket'

	// Health check
	HealthCheckProjectID string // HEALTH_CHECK_PROJECT_ID (empty ⇒ /health_check 500s)

	HasSecondaries bool // MONGO_HAS_SECONDARIES === 'true'
	Log            func(format string, args ...any)
}

// DefaultCfg applies the Node settings.defaults.cjs values to empty fields.
func (c *Config) Defaults() {
	if c.MaxDocLength == 0 {
		c.MaxDocLength = 2 * 1024 * 1024
	}
	if c.MaxJSONRequestSize == 0 {
		c.MaxJSONRequestSize = 12 * 1024 * 1024
	}
	if c.MaxDeletedDocs == 0 {
		c.MaxDeletedDocs = 2000
	}
	if c.ArchiveBatchSize == 0 {
		c.ArchiveBatchSize = 50
	}
	if c.ParallelArchiveJobs == 0 {
		c.ParallelArchiveJobs = 5
	}
	if c.ArchivingLockMS == 0 {
		c.ArchivingLockMS = 60 * time.Second
	}
	if c.Host == "" {
		c.Host = "127.0.0.1"
	}
	if c.Port == 0 {
		c.Port = 3016
	}
	if c.Bucket == "" {
		c.Bucket = "bucket"
	}
}

// Doc is a decoded `docs` collection document. Pointer fields distinguish
// absent from zero values exactly as the Node code distinguishes undefined
// from 0/false (views include a key only when the value != null).
type Doc struct {
	ID             string
	ProjectID      string
	Lines          *[]string
	Rev            *int64
	Version        *int64
	Ranges         any // neutral tree or nil (absent)
	Deleted        *bool
	InS3           *bool
	Name           *string
	DeletedAt      *time.Time
	ArchivingUntil *time.Time
}

// docProj selects the findDoc projection (1:1 flag sets from the Node
// callsites).
type docProj struct {
	Lines     bool
	Rev       bool
	Version   bool
	Ranges    bool
	Deleted   bool
	InS3      bool
	Name      bool
	DeletedAt bool
}

// WriteUpdates is the updateDoc $set set, node key order lines → ranges →
// version (rev is appended by the store when lines or ranges are present).
type WriteUpdates struct {
	Lines   *[]string
	Ranges  *any
	Version *int64
}

// ArchivedDoc is the persistor JSON payload shape.
type ArchivedDoc struct {
	Lines  []string
	Ranges any // neutral tree or nil
	Rev    *int64
}

// Store is the docstore data plane (Node DocManager + MongoManager folded
// into one behavioral interface so the in-memory test store and the real
// Mongo store are interchangeable). All multi-ID filters use explicit $in;
// single-ID filters use equality (both verified against mongod 8.3.7).
type Store interface {
	// FindDoc: findOne {_id, project_id} with the given projection;
	// version normalizes to 0 when projected and 0/absent (Node findDoc).
	FindDoc(ctx context.Context, projectID, docID string, p docProj, useSecondary bool) (*Doc, error)
	// ProjectDocs: find {project_id[, deleted={$ne:true}]}, projection per
	// flags, optional inS3 filter, optional limit.
	ProjectDocs(ctx context.Context, projectID string, opts ProjectDocOpts) ([]*Doc, error)
	// DeletedDocs: find {project_id, deleted:true} projection {_id,name,
	// deletedAt}, sort deletedAt desc, limit (Node getProjectsDeletedDocs).
	DeletedDocs(ctx context.Context, projectID string, limit int) ([]*Doc, error)
	// GetDocRev: findOne {_id} rev (ok=false when absent).
	GetDocRev(ctx context.Context, docID string) (int64, bool, error)

	// UpsertDoc: previousRev > 0 → updateOne pipeline with rev/previousRev
	// filter; else insertOne rev:1. Mismatch / duplicate ⇒ ErrDocRevValue.
	UpsertDoc(ctx context.Context, projectID, docID string, previousRev int64, u WriteUpdates) error
	// PatchDocMeta: updateOne $set {deletedAt, name, deleted:true}.
	PatchDocMeta(ctx context.Context, projectID, docID string, deletedAt time.Time, name string) error
	// GetDocForArchiving: findOneAndUpdate lock (archivingUntil), original
	// doc with {lines,ranges,rev} (nil when not found / locked).
	GetDocForArchiving(ctx context.Context, projectID, docID string, lockUntil time.Time) (*Doc, error)
	// MarkDocAsArchived: filter {_id, rev}; $set inS3, $unset lines/ranges/archivingUntil.
	MarkDocAsArchived(ctx context.Context, docID string, rev int64) error
	// RestoreArchivedDoc: filter {_id, project_id, rev}; $set lines/ranges,
	// $unset inS3 (pipeline form like Node).
	RestoreArchivedDoc(ctx context.Context, projectID, docID string, lines []string, ranges any, rev int64) error
	// DestroyProjectDocs: deleteMany {project_id}.
	DestroyProjectDocs(ctx context.Context, projectID string) error
	// DeleteDoc: deleteOne {_id, project_id} (health-check cleanup).
	DeleteDoc(ctx context.Context, projectID, docID string) error
}

// ProjectDocOpts mirrors the Node getProjectsDocs options/projections (and
// the archive id-list queries).
type ProjectDocOpts struct {
	IncludeDeleted  bool
	UseSecondary    bool
	WantLines       bool
	WantRev         bool
	WantRanges      bool
	WantVersion     bool
	NonArchivedOnly bool // inS3 {$ne: true} (nonArchivedDocsList)
	ArchivedOnly    bool // inS3: true (archived id lists)
	Limit           int
}

// Archiver is the object-persistor surface docstore uses (fs backend 1:1;
// s3/gcs are out of scope, exactly like the filestore port).
type Archiver interface {
	// Send: FSPersistor.sendStream (flat <bucket>/<key '/' → '_'> file);
	// sourceMD5 (non-s3) is computed over the bytes and must match.
	Send(ctx context.Context, bucket, key string, data []byte, sourceMD5 string) error
	// Get: file bytes + the live md5 of the stored file (FSPersistor
	// getObjectStream + getObjectMd5Hash).
	Get(ctx context.Context, bucket, key string) ([]byte, string, error)
	// DeleteDirectory: the flat `bucket/<key>_*` sweep (useSubdirectories is
	// false for docstore, verified via PersistorFactory).
	DeleteDirectory(ctx context.Context, bucket, key string) error
}
