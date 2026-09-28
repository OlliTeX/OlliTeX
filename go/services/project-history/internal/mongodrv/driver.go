// Package mongodrv — the production driver adapter: project-history's
// internal/mongo.Collection seam over the official go.mongodb.org/mongo-driver.
//
// The rest of the module is hermetic (interface seams + fakes); this package
// is the ONE place a real MongoDB client plugs in for `project_history
// -what=serve`. The adapter is written against a minimal DriverCollection
// interface (which *mongo.Collection satisfies) so its semantics —
// option mapping, no-document → (nil, nil), UpdateResult translation — stay
// unit-testable without a live mongod (a fake driver lives in the tests).
package mongodrv

import (
	"context"
	"fmt"

	phmongo "ollitex/go/services/project-history/internal/mongo"
)

// DriverCollection is the driver surface this adapter uses.
// *mongo.Collection (go.mongodb.org/mongo-driver) satisfies it via
// method-value promotion; unit tests use a fake.
type DriverCollection interface {
	FindOne(ctx context.Context, filter map[string]any, opts map[string]any) (map[string]any, error)
	Find(ctx context.Context, filter map[string]any, opts map[string]any) ([]map[string]any, error)
	InsertOne(ctx context.Context, doc map[string]any) (any, error)
	InsertMany(ctx context.Context, docs []map[string]any) error
	UpdateOne(ctx context.Context, filter map[string]any, update map[string]any, opts map[string]any) (*phmongo.UpdateResult, error)
	UpdateMany(ctx context.Context, filter map[string]any, update map[string]any, opts map[string]any) (*phmongo.UpdateResult, error)
	DeleteOne(ctx context.Context, filter map[string]any) (int64, error)
	FindOneAndUpdate(ctx context.Context, filter map[string]any, update map[string]any, opts map[string]any) (map[string]any, error)
	Distinct(ctx context.Context, field string, filter map[string]any) ([]any, error)
	CountDocuments(ctx context.Context, filter map[string]any) (int64, error)
}

// ErrNoDocuments marks "no matching document" — the adapter converts it to
// the seam's (nil, nil) / zero results (vendor findOne → undefined,
// delete/insert → zero counts).
var ErrNoDocuments = fmt.Errorf("mongodrv: no documents")

// Collection adapts one driver collection to the seam interface.
type Collection struct {
	D DriverCollection
}

// NewCollection wraps a driver collection (e.g. db.Collection("x")).
func NewCollection(d DriverCollection) phmongo.Collection {
	return Collection{D: d}
}

func (c Collection) FindOne(ctx context.Context, filter map[string]any, opts phmongo.FindOneOpts) (map[string]any, error) {
	do := map[string]any{}
	if opts.Projection != nil {
		do["projection"] = opts.Projection
	}
	if opts.Sort != nil {
		do["sort"] = opts.Sort
	}
	doc, err := c.D.FindOne(ctx, filter, do)
	if err != nil {
		if err == ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return doc, nil
}

func (c Collection) Find(ctx context.Context, filter map[string]any, opts phmongo.FindOpts) (phmongo.Iterator, error) {
	do := map[string]any{}
	if opts.Projection != nil {
		do["projection"] = opts.Projection
	}
	if opts.Sort != nil {
		do["sort"] = opts.Sort
	}
	if opts.Limit > 0 {
		do["limit"] = int64(opts.Limit)
	}
	docs, err := c.D.Find(ctx, filter, do)
	if err != nil {
		return nil, err
	}
	return &sliceIterator{docs: docs}, nil
}

type sliceIterator struct {
	docs   []map[string]any
	done   bool
	closed bool
}

func (s *sliceIterator) All(ctx context.Context) ([]map[string]any, error) { return s.docs, nil }
func (s *sliceIterator) Close(ctx context.Context) error                   { s.closed = true; return nil }

func (c Collection) InsertOne(ctx context.Context, doc map[string]any, _ phmongo.InsertOneOpts) error {
	_, err := c.D.InsertOne(ctx, doc)
	return err
}

func (c Collection) InsertMany(ctx context.Context, docs []map[string]any) error {
	return c.D.InsertMany(ctx, docs)
}

func (c Collection) UpdateOne(ctx context.Context, filter map[string]any, update phmongo.Update, opts phmongo.UpdateOpts) (phmongo.UpdateResult, error) {
	do := updateOpts(opts)
	result, err := c.D.UpdateOne(ctx, filter, map[string]any(update), do)
	if err != nil {
		return phmongo.UpdateResult{}, err
	}
	return copyResult(result), nil
}

func (c Collection) UpdateMany(ctx context.Context, filter map[string]any, update phmongo.Update, opts phmongo.UpdateOpts) (phmongo.UpdateResult, error) {
	do := updateOpts(opts)
	result, err := c.D.UpdateMany(ctx, filter, map[string]any(update), do)
	if err != nil {
		return phmongo.UpdateResult{}, err
	}
	return copyResult(result), nil
}

func (c Collection) DeleteOne(ctx context.Context, filter map[string]any) (int64, error) {
	n, err := c.D.DeleteOne(ctx, filter)
	if err != nil {
		if err == ErrNoDocuments {
			return 0, nil
		}
		return 0, err
	}
	return n, nil
}

func (c Collection) FindOneAndUpdate(ctx context.Context, filter map[string]any, update phmongo.Update, opts phmongo.FindOneAndUpdateOpts) (map[string]any, error) {
	do := updateOpts(phmongo.UpdateOpts{Upsert: opts.Upsert, Projection: opts.Projection, Collation: opts.Collation, ArrayFilters: opts.ArrayFilters})
	if opts.Sort != nil {
		do["sort"] = opts.Sort
	}
	if opts.ReturnAfter {
		do["returnAfter"] = true
	}
	doc, err := c.D.FindOneAndUpdate(ctx, filter, map[string]any(update), do)
	if err != nil {
		if err == ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return doc, nil
}

func (c Collection) Distinct(ctx context.Context, field string, filter map[string]any) ([]any, error) {
	return c.D.Distinct(ctx, field, filter)
}

func (c Collection) CountDocuments(ctx context.Context, filter map[string]any) (int64, error) {
	return c.D.CountDocuments(ctx, filter)
}

// updateOpts maps the seam options to the adapter's option bag.
func updateOpts(o phmongo.UpdateOpts) map[string]any {
	do := map[string]any{}
	if o.Upsert {
		do["upsert"] = true
	}
	if o.ReturnAfter {
		do["returnAfter"] = true
	}
	if o.Projection != nil {
		do["projection"] = o.Projection
	}
	if o.Collation != nil {
		do["collation"] = o.Collation
	}
	if o.ArrayFilters != nil {
		do["arrayFilters"] = o.ArrayFilters
	}
	return do
}

func copyResult(r *phmongo.UpdateResult) phmongo.UpdateResult {
	if r == nil {
		return phmongo.UpdateResult{}
	}
	return *r
}

// DBAdapter — the five named collections of a database.
type DBDriver interface {
	Collection(name string) DriverCollection
}

// NewDB builds the seam DB object from a DBDriver (see NewDriverDB for the
// concrete *mongo.Database binding — that binding lives with the driver
// import at the call site to keep this file fake-friendly).
func NewDB(db DBDriver, names ...string) *phmongo.DB {
	want := []string{"deletedProjects", "projects", "projectHistoryFailures", "projectHistoryLabels", "projectHistorySyncState"}
	c := map[string]phmongo.Collection{}
	for _, n := range want {
		c[n] = NewCollection(db.Collection(n))
	}
	return phmongo.NewDB(c, nil)
}
