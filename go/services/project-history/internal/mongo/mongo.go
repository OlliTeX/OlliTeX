// Package mongo is the C1 port of app/js/mongodb.js (33 L) — the named
// project-history database seam. The vendor exports a MongoClient + a
// `db` object of five typed collections plus cleanupTestDatabase; this port
// defines the Collection interface every manager seam needs and the DB
// handle that names them (faithful: identical collection names).
package mongo

import "context"

// Collection is the mongodb Collection surface used across the managers
// (subset that the vendored services actually call; implementations may be
// real mongo drivers or test fakes).
type Collection interface {
	FindOne(ctx context.Context, filter map[string]any, opts FindOneOpts) (map[string]any, error)
	Find(ctx context.Context, filter map[string]any, opts FindOpts) (Iterator, error)
	InsertOne(ctx context.Context, doc map[string]any, opts InsertOneOpts) error
	InsertMany(ctx context.Context, docs []map[string]any) error
	UpdateOne(ctx context.Context, filter map[string]any, update Update, opts UpdateOpts) (UpdateResult, error)
	UpdateMany(ctx context.Context, filter map[string]any, update Update, opts UpdateOpts) (UpdateResult, error)
	DeleteOne(ctx context.Context, filter map[string]any) (int64, error)
	FindOneAndUpdate(ctx context.Context, filter map[string]any, update Update, opts FindOneAndUpdateOpts) (map[string]any, error)
	Distinct(ctx context.Context, field string, filter map[string]any) ([]any, error)
	CountDocuments(ctx context.Context, filter map[string]any) (int64, error)
}

// Update is a MongoDB update document ($set/$inc/$push/... operators as the
// top-level keys, mirroring the JS call sites).
type Update = map[string]any

type FindOneOpts struct {
	Projection  map[string]any
	Sort        map[string]any
	ContextName string
}
type FindOpts struct {
	Projection map[string]any
	Sort       map[string]any
	Limit      int64
}
type InsertOneOpts struct {
	SkipValidation bool
}
type UpdateOpts struct {
	Upsert       bool
	ReturnAfter  bool // returnDocument:'after'
	Projection   map[string]any
	Collation    map[string]any
	ArrayFilters []map[string]any
}
type FindOneAndUpdateOpts struct {
	ReturnAfter  bool
	Projection   map[string]any
	Sort         map[string]any
	Upsert       bool
	Collation    map[string]any
	ArrayFilters []map[string]any
}

type UpdateResult struct {
	MatchedCount  int64
	ModifiedCount int64
	UpsertedCount int64
	UpsertedID    any
	Raw           map[string]any // includeResultMetadata-style raw result when present
}

// Iterator streams Find results (vendor cursor contract: hasNext + next,
// but the services use .map().toArray() — the port consumes via All).
type Iterator interface {
	All(ctx context.Context) ([]map[string]any, error)
	Close(ctx context.Context) error
}

// DB is the vendor `db` object (mongodb.js L26-33): five named collections
// + the test cleanup hook.
type DB struct {
	DeletedProjects         Collection
	Projects                Collection
	ProjectHistoryFailures  Collection
	ProjectHistoryLabels    Collection
	ProjectHistorySyncState Collection
	// CleanupTestDatabase — vendor MongoUtils.cleanupTestDatabase.
	CleanupTestDatabase func(ctx context.Context) error
}

// NewDB builds the handle from per-collection handles (the production
// implementation is a real driver; tests supply fakes). Nil collections are
// left nil — manager seams check for nil before use (vendor: direct
// collection access).
func NewDB(c map[string]Collection, cleanup func(ctx context.Context) error) *DB {
	d := &DB{CleanupTestDatabase: cleanup}
	if c == nil {
		return d
	}
	g := func(name string) Collection {
		if col, ok := c[name]; ok {
			return col
		}
		return nil
	}
	d.DeletedProjects = g("deletedProjects")
	d.Projects = g("projects")
	d.ProjectHistoryFailures = g("projectHistoryFailures")
	d.ProjectHistoryLabels = g("projectHistoryLabels")
	d.ProjectHistorySyncState = g("projectHistorySyncState")
	return d
}

// Get returns the collection by its vendor name (nil-safe).
func (d *DB) Get(name string) Collection {
	if d == nil {
		return nil
	}
	switch name {
	case "deletedProjects":
		return d.DeletedProjects
	case "projects":
		return d.Projects
	case "projectHistoryFailures":
		return d.ProjectHistoryFailures
	case "projectHistoryLabels":
		return d.ProjectHistoryLabels
	case "projectHistorySyncState":
		return d.ProjectHistorySyncState
	}
	return nil
}
