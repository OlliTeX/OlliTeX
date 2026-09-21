package mongowrapper

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func TestDbFromURI(t *testing.T) {
	cases := []struct {
		uri  string
		want string
	}{
		{"mongodb://127.0.0.1:27017", ""},
		{"mongodb://127.0.0.1:27017/", ""},
		{"mongodb://host1:27017,host2:27017/proj?replicaSet=rs0", "proj"},
		{"mongodb://u:pw@host1:27017,host2:27017/proj?authSource=admin", "proj"},
		{"mongodb+srv://cluster.example.com/dbname?retryWrites=true", "dbname"},
		{"mongodb://host:27017", ""},
		{"not-a-uri", ""},
		{"mongodb://h:27017/a/b?x=1", "a/b"},
	}
	for _, tc := range cases {
		if got := dbFromURI(tc.uri); got != tc.want {
			t.Errorf("dbFromURI(%q) = %q, want %q", tc.uri, got, tc.want)
		}
	}
}

func TestSchemaAddIndexAutoName(t *testing.T) {
	s := NewSchema(map[string]any{"a": "string"})
	s.AddIndex(bson.D{{Key: "a", Value: int32(1)}}, "", options.IndexOptions{})
	s.AddIndex(bson.D{{Key: "b", Value: int32(-1)}}, "", options.IndexOptions{})
	if s.Indexes[0].Name != "a_1" {
		t.Errorf("auto name 0 = %q, want a_1", s.Indexes[0].Name)
	}
	if s.Indexes[1].Name != "b_-1" {
		t.Errorf("auto name 1 = %q, want b_-1", s.Indexes[1].Name)
	}
	if got := s.Indexes[0].Opts; got.Unique != nil || got.Name != nil {
		t.Errorf("empty opts should stay empty, got %+v", got)
	}
}

func TestSchemaAddIndexExplicitName(t *testing.T) {
	s := NewSchema(nil)
	s.AddIndex(bson.D{{Key: "a", Value: int32(1)}}, "my_name", options.IndexOptions{})
	if s.Indexes[0].Name != "my_name" {
		t.Errorf("explicit name = %q", s.Indexes[0].Name)
	}
}

// Connect with a no-path URI falls back to the driver default db "test"
// (Node: no path → the server's default).
func TestConnectNoPathDefaultDB(t *testing.T) {
	ctx := context.Background()
	c, err := Connect(ctx, "mongodb://127.0.0.1:27017")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close(ctx)
	if c.Database.Name() != "test" {
		t.Errorf("no-path default db = %q, want test", c.Database.Name())
	}
}

// Connect to an unreachable port must fail (Node: mongoose.connect rejects).
func TestConnectUnreachable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := Connect(ctx, "mongodb://127.0.0.1:2/nope"); err == nil {
		t.Fatal("expected a connection failure")
	}
}

// Apply creates the declared indexes on the collection (the observable half
// of a mongoose schema); an index-less schema is a no-op.
func TestSchemaApplyLive(t *testing.T) {
	ctx := context.Background()
	c, err := Connect(ctx, "mongodb://127.0.0.1:27017/overleaf_test_w")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close(ctx)
	col := c.Model("schema_probe")
	defer func() { _ = col.Drop(ctx) }()

	s := NewSchema(map[string]any{"token": "string"})
	s.AddIndex(bson.D{{Key: "token", Value: int32(1)}}, "", options.IndexOptions{Unique: boolPtr(true)})
	if err := s.Apply(ctx, col); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	cur, err := col.Indexes().List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var docs []bson.M
	if err := cur.All(ctx, &docs); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range docs {
		if d["name"] == "token_1" {
			found = true
		}
	}
	if !found {
		t.Errorf("index token_1 not applied; indexes: %v", docs)
	}
	if err := NewSchema(nil).Apply(ctx, col); err != nil {
		t.Errorf("schema with no indexes must be a no-op, got %v", err)
	}
}

func boolPtr(b bool) *bool { return &b }

func TestConnectionModel(t *testing.T) {
	ctx := context.Background()
	c, err := Connect(ctx, "mongodb://127.0.0.1:27017/overleaf_test_w")
	if err != nil {
		t.Fatalf("Connect to live mongo: %v", err)
	}
	defer c.Close(ctx)

	if c.Database.Name() != "overleaf_test_w" {
		t.Errorf("default db = %q, want overleaf_test_w (URI path)", c.Database.Name())
	}
	col := c.Model("projects")
	if col.Database().Name() != "overleaf_test_w" || col.Name() != "projects" {
		t.Errorf("Model() = %s.%s", col.Database().Name(), col.Name())
	}
}
