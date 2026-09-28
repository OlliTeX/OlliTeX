package mongo

import (
	"context"
	"testing"
)

type fakeColl struct {
	name string
}

func (f *fakeColl) FindOne(ctx context.Context, filter map[string]any, opts FindOneOpts) (map[string]any, error) {
	return map[string]any{"_id": f.name}, nil
}
func (f *fakeColl) Find(ctx context.Context, filter map[string]any, opts FindOpts) (Iterator, error) {
	return fakeIter{}, nil
}
func (f *fakeColl) InsertOne(ctx context.Context, doc map[string]any, opts InsertOneOpts) error {
	return nil
}
func (f *fakeColl) InsertMany(ctx context.Context, docs []map[string]any) error { return nil }
func (f *fakeColl) UpdateOne(ctx context.Context, filter map[string]any, update Update, opts UpdateOpts) (UpdateResult, error) {
	return UpdateResult{MatchedCount: 1}, nil
}
func (f *fakeColl) UpdateMany(ctx context.Context, filter map[string]any, update Update, opts UpdateOpts) (UpdateResult, error) {
	return UpdateResult{MatchedCount: 1}, nil
}
func (f *fakeColl) DeleteOne(ctx context.Context, filter map[string]any) (int64, error) {
	return 1, nil
}
func (f *fakeColl) FindOneAndUpdate(ctx context.Context, filter map[string]any, update Update, opts FindOneAndUpdateOpts) (map[string]any, error) {
	return map[string]any{"_id": "x"}, nil
}
func (f *fakeColl) Distinct(ctx context.Context, field string, filter map[string]any) ([]any, error) {
	return []any{"a"}, nil
}
func (f *fakeColl) CountDocuments(ctx context.Context, filter map[string]any) (int64, error) {
	return 1, nil
}

type fakeIter struct{}

func (fakeIter) All(ctx context.Context) ([]map[string]any, error) {
	return []map[string]any{}, nil
}
func (fakeIter) Close(ctx context.Context) error { return nil }

func TestNewDBAndGet(t *testing.T) {
	cols := map[string]Collection{
		"deletedProjects":         &fakeColl{name: "dp"},
		"projects":                &fakeColl{name: "p"},
		"projectHistoryFailures":  &fakeColl{name: "f"},
		"projectHistoryLabels":    &fakeColl{name: "l"},
		"projectHistorySyncState": &fakeColl{name: "s"},
	}
	var cleaned bool
	d := NewDB(cols, func(ctx context.Context) error { cleaned = true; return nil })
	for k, want := range map[string]string{
		"deletedProjects":         "dp",
		"projects":                "p",
		"projectHistoryFailures":  "f",
		"projectHistoryLabels":    "l",
		"projectHistorySyncState": "s",
	} {
		got := d.Get(k)
		if got == nil {
			t.Fatalf("Get(%s) nil", k)
		}
		doc, err := got.FindOne(context.Background(), map[string]any{}, FindOneOpts{})
		if err != nil || doc["_id"] != want {
			t.Fatalf("Get(%s).FindOne = %v %v", k, doc, err)
		}
	}
	// unknown name → nil
	if d.Get("unknown") != nil {
		t.Fatal("unknown collection must be nil")
	}
	// nil DB → nil
	var nd *DB
	if nd.Get("projects") != nil {
		t.Fatal("nil DB must be nil-safe")
	}
	// nil collections map → all nil
	if d2 := NewDB(nil, nil); d2.Projects != nil {
		t.Fatal("nil map → nil collections")
	}
	// missing entries → nil
	if d3 := NewDB(map[string]Collection{"projects": &fakeColl{}}, nil); d3.DeletedProjects != nil {
		t.Fatal("missing entry must be nil")
	}
	// cleanup hook
	if err := d.CleanupTestDatabase(context.Background()); err != nil || !cleaned {
		t.Fatal("cleanup hook not invoked")
	}
}
