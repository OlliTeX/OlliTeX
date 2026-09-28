package mongodrv

import (
	"context"
	"reflect"
	"testing"

	phmongo "ollitex/go/services/project-history/internal/mongo"
)

// fakeDriver — a scripted DriverCollection for the adapter's seam semantics.
type fakeDriver struct {
	findOneDoc    map[string]any
	findOneErr    error
	findDocs      []map[string]any
	findErr       error
	deleted       int64
	deleteErr     error
	updateMatches int64
	updateErr     error
	lastFilter    map[string]any
	lastUpdate    map[string]any
	lastOpts      map[string]any
	fauDoc        map[string]any
	fauErr        error
	count         int64
	distinctVals  []any
	inserted      []map[string]any
}

func (f *fakeDriver) FindOne(ctx context.Context, filter map[string]any, opts map[string]any) (map[string]any, error) {
	f.lastFilter, f.lastOpts = filter, opts
	if f.findOneErr != nil {
		return nil, f.findOneErr
	}
	return f.findOneDoc, nil
}
func (f *fakeDriver) Find(ctx context.Context, filter map[string]any, opts map[string]any) ([]map[string]any, error) {
	f.lastFilter, f.lastOpts = filter, opts
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.findDocs, nil
}
func (f *fakeDriver) InsertOne(ctx context.Context, doc map[string]any) (any, error) {
	f.inserted = append(f.inserted, doc)
	return "ID1", nil
}
func (f *fakeDriver) InsertMany(ctx context.Context, docs []map[string]any) error {
	f.inserted = append(f.inserted, docs...)
	return nil
}
func (f *fakeDriver) UpdateOne(ctx context.Context, filter map[string]any, update map[string]any, opts map[string]any) (*phmongo.UpdateResult, error) {
	f.lastFilter, f.lastUpdate, f.lastOpts = filter, update, opts
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return &phmongo.UpdateResult{MatchedCount: f.updateMatches, ModifiedCount: f.updateMatches}, nil
}
func (f *fakeDriver) UpdateMany(ctx context.Context, filter map[string]any, update map[string]any, opts map[string]any) (*phmongo.UpdateResult, error) {
	return f.UpdateOne(ctx, filter, update, opts)
}
func (f *fakeDriver) DeleteOne(ctx context.Context, filter map[string]any) (int64, error) {
	if f.deleteErr != nil {
		return 0, f.deleteErr
	}
	return f.deleted, nil
}
func (f *fakeDriver) FindOneAndUpdate(ctx context.Context, filter map[string]any, update map[string]any, opts map[string]any) (map[string]any, error) {
	f.lastFilter, f.lastOpts = filter, opts
	if f.fauErr != nil {
		return nil, f.fauErr
	}
	return f.fauDoc, nil
}
func (f *fakeDriver) Distinct(ctx context.Context, field string, filter map[string]any) ([]any, error) {
	return f.distinctVals, nil
}
func (f *fakeDriver) CountDocuments(ctx context.Context, filter map[string]any) (int64, error) {
	return f.count, nil
}

func TestFindOneNoDocIsNilNil(t *testing.T) {
	d := &fakeDriver{findOneErr: ErrNoDocuments}
	doc, err := NewCollection(d).FindOne(context.Background(), map[string]any{"a": 1}, phmongo.FindOneOpts{})
	if err != nil || doc != nil {
		t.Fatalf("no-doc must be (nil, nil), got (%v, %v)", doc, err)
	}
	if !reflect.DeepEqual(d.lastOpts, map[string]any{}) {
		t.Fatalf("empty opts bag = %v", d.lastOpts)
	}
}

func TestFindOneOptsBag(t *testing.T) {
	d := &fakeDriver{findOneDoc: map[string]any{"x": 1}}
	proj := map[string]any{"_id": 0}
	sort := map[string]any{"v": -1}
	doc, err := NewCollection(d).FindOne(context.Background(), nil, phmongo.FindOneOpts{Projection: proj, Sort: sort})
	if err != nil || doc["x"] != 1 {
		t.Fatalf("doc=%v err=%v", doc, err)
	}
	if !reflect.DeepEqual(d.lastOpts["projection"], proj) || !reflect.DeepEqual(d.lastOpts["sort"], sort) {
		t.Fatalf("opts bag = %v", d.lastOpts)
	}
}

func TestFindIteratorSeam(t *testing.T) {
	d := &fakeDriver{findDocs: []map[string]any{{"n": 1}, {"n": 2}}}
	it, err := NewCollection(d).Find(context.Background(), nil, phmongo.FindOpts{Limit: 10, Sort: map[string]any{"n": 1}})
	if err != nil {
		t.Fatal(err)
	}
	all, err := it.All(context.Background())
	if err != nil || len(all) != 2 {
		t.Fatalf("all=%v err=%v", all, err)
	}
	if err := it.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d.lastOpts["limit"] != int64(10) {
		t.Fatalf("limit option = %v", d.lastOpts["limit"])
	}
}

func TestUpdateResultMapping(t *testing.T) {
	d := &fakeDriver{updateMatches: 3}
	out, err := NewCollection(d).UpdateOne(context.Background(), map[string]any{"p": "x"}, phmongo.Update{"$set": map[string]any{"a": 1}}, phmongo.UpdateOpts{Upsert: true, ReturnAfter: true})
	if err != nil || out.MatchedCount != 3 || out.ModifiedCount != 3 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if d.lastOpts["upsert"] != true || d.lastOpts["returnAfter"] != true {
		t.Fatalf("opts = %v", d.lastOpts)
	}
}

func TestDeleteOneNoDocZero(t *testing.T) {
	d := &fakeDriver{deleteErr: ErrNoDocuments}
	n, err := NewCollection(d).DeleteOne(context.Background(), map[string]any{})
	if err != nil || n != 0 {
		t.Fatalf("no-doc delete must be (0, nil), got (%d, %v)", n, err)
	}
	d2 := &fakeDriver{deleted: 1}
	n, err = NewCollection(d2).DeleteOne(context.Background(), map[string]any{})
	if err != nil || n != 1 {
		t.Fatalf("deleted=%d err=%v", n, err)
	}
}

func TestFindOneAndUpdateNoDocIsNilNil(t *testing.T) {
	d := &fakeDriver{fauErr: ErrNoDocuments}
	doc, err := NewCollection(d).FindOneAndUpdate(context.Background(), map[string]any{}, phmongo.Update{}, phmongo.FindOneAndUpdateOpts{ReturnAfter: true})
	if err != nil || doc != nil {
		t.Fatalf("no-doc FAU must be (nil, nil), got (%v, %v)", doc, err)
	}
	if d.lastOpts["returnAfter"] != true {
		t.Fatalf("returnAfter missing: %v", d.lastOpts)
	}
}

func TestInsertPassThrough(t *testing.T) {
	d := &fakeDriver{}
	if err := NewCollection(d).InsertOne(context.Background(), map[string]any{"a": 1}, phmongo.InsertOneOpts{SkipValidation: true}); err != nil {
		t.Fatal(err)
	}
	if err := NewCollection(d).InsertMany(context.Background(), []map[string]any{{"a": 2}}); err != nil {
		t.Fatal(err)
	}
	if len(d.inserted) != 2 {
		t.Fatalf("inserted=%v", d.inserted)
	}
}

func TestDistinctCount(t *testing.T) {
	d := &fakeDriver{distinctVals: []any{"a", "b"}, count: 7}
	vals, err := NewCollection(d).Distinct(context.Background(), "k", nil)
	if err != nil || len(vals) != 2 {
		t.Fatalf("vals=%v err=%v", vals, err)
	}
	n, err := NewCollection(d).CountDocuments(context.Background(), nil)
	if err != nil || n != 7 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

// NewDB — the five named collections land on the seam DB object.
type fakeDB struct{ cols map[string]DriverCollection }

func (f fakeDB) Collection(name string) DriverCollection { return f.cols[name] }

func TestNewDBNames(t *testing.T) {
	names := []string{"deletedProjects", "projects", "projectHistoryFailures", "projectHistoryLabels", "projectHistorySyncState"}
	cols := map[string]DriverCollection{}
	for _, n := range names {
		cols[n] = &fakeDriver{}
	}
	db := NewDB(fakeDB{cols: cols})
	if db.Projects == nil || db.ProjectHistoryLabels == nil || db.ProjectHistoryFailures == nil || db.ProjectHistorySyncState == nil || db.DeletedProjects == nil {
		t.Fatalf("named collections not wired: %+v", db)
	}
}

func TestUpdateManyCopyNil(t *testing.T) {
	d := &fakeDriver{updateErr: context.Canceled}
	_, err := NewCollection(d).UpdateMany(context.Background(), nil, phmongo.Update{}, phmongo.UpdateOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
	// copyResult(nil) — the zero-value path exercised via the err branch above
	// plus a direct success mapping:
	d2 := &fakeDriver{updateMatches: 1}
	out, err := NewCollection(d2).UpdateMany(context.Background(), map[string]any{
		"k": "v",
	}, phmongo.Update{"$set": map[string]any{"n": 2}}, phmongo.UpdateOpts{Collation: map[string]any{"locale": "en"}})
	if err != nil || out.MatchedCount != 1 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if d2.lastOpts["collation"] == nil {
		t.Fatalf("collation option missing: %v", d2.lastOpts)
	}
}

func TestFAUSuccessAndOpts(t *testing.T) {
	d := &fakeDriver{fauDoc: map[string]any{"after": true}}
	doc, err := NewCollection(d).FindOneAndUpdate(context.Background(), map[string]any{"p": 1}, phmongo.Update{"$inc": map[string]any{"v": 1}}, phmongo.FindOneAndUpdateOpts{
		ReturnAfter:  true,
		Upsert:       true,
		Projection:   map[string]any{"_id": 0},
		Sort:         map[string]any{"v": -1},
		Collation:    map[string]any{"locale": "en"},
		ArrayFilters: []map[string]any{{"e.v": 1}},
	})
	if err != nil || doc["after"] != true {
		t.Fatalf("doc=%v err=%v", doc, err)
	}
	for _, k := range []string{"returnAfter", "upsert", "projection", "sort", "collation", "arrayFilters"} {
		if d.lastOpts[k] == nil {
			t.Fatalf("FAU opt %s missing: %v", k, d.lastOpts)
		}
	}
}
