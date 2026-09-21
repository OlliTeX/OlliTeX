package mongoutils

// testutils_test.go — the Node test-utils.js contract (byte-pinned guard) +
// the cleanup/drop lifecycle, live-verified against a real mongod when one
// is reachable (skipped on clean CI boxes without mongo).

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const liveMongoURI = "mongodb://127.0.0.1:27017"

// liveMongo dials the local test mongod; skips the test when absent.
func liveMongo(t *testing.T) *mongo.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(liveMongoURI))
	if err != nil {
		t.Skipf("live mongo not available: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		t.Skipf("live mongo not reachable: %v", err)
	}
	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })
	return client
}

func TestEnsureTestDatabaseRefusals(t *testing.T) {
	// NOT parallel: mutates NODE_ENV, shared with sibling tests.
	t.Setenv("NODE_ENV", "test")
	err := EnsureTestDatabase("overleaf_prod")
	if err == nil {
		t.Fatal("expected refusal")
	}
	const want = "Refusing to clear database 'overleaf_prod' in environment 'test'"
	if err.Error() != want {
		t.Fatalf("\n got: %q\nwant: %q", err.Error(), want)
	}

	t.Setenv("NODE_ENV", "production")
	err = EnsureTestDatabase("test-overleaf")
	if err == nil {
		t.Fatal("expected refusal")
	}
	const want2 = "Refusing to clear database 'test-overleaf' in environment 'production'"
	if err.Error() != want2 {
		t.Fatalf("\n got: %q\nwant: %q", err.Error(), want2)
	}
}

func TestEnsureTestDatabaseAllowsTest(t *testing.T) {
	// NOT parallel: mutates NODE_ENV.
	t.Setenv("NODE_ENV", "test")
	if err := EnsureTestDatabase("test-overleaf"); err != nil {
		t.Fatalf("test-overleaf under NODE_ENV=test must be allowed: %v", err)
	}
}

func TestCleanupTestDatabaseLive(t *testing.T) {
	client := liveMongo(t)
	ctx := context.Background()
	t.Setenv("NODE_ENV", "test")

	const dbName = "test-overleaf"
	db := client.Database(dbName)
	work := db.Collection("work")
	migrations := db.Collection("migrations")

	defer func() {
		if err := client.Database(dbName).Drop(context.Background()); err != nil {
			t.Logf("final drop (best effort): %v", err)
		}
	}()

	// seed: unique index + data in `work`; data in `migrations` (preserved)
	_, _ = work.Indexes().DropAll(ctx)
	if _, err := work.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "token", Value: 1}},
		Options: &options.IndexOptions{Name: ptr("uniq_token"), Unique: ptrBool(true)},
	}); err != nil {
		t.Fatal(err)
	}
	_, err := work.InsertMany(ctx, []any{
		bson.M{"token": "a", "n": 1},
		bson.M{"token": "b", "n": 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrations.InsertOne(ctx, bson.M{"name": "0001"}); err != nil {
		t.Fatal(err)
	}

	if err := CleanupTestDatabase(ctx, client, dbName); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	// work emptied...
	var worked int64
	if n, e := work.CountDocuments(ctx, bson.M{}); e != nil {
		t.Fatal(e)
	} else {
		worked = n
	}
	if worked != 0 {
		t.Fatalf("work should be empty, has %d docs", worked)
	}
	// ...migrations preserved (the Node filter)
	if n, e := migrations.CountDocuments(ctx, bson.M{}); e != nil {
		t.Fatal(e)
	} else if n != 1 {
		t.Fatalf("migrations must be preserved, has %d docs", n)
	}
	// ...and the index survived (indexes are preserved — deleteMany, not drop)
	res, err := work.Indexes().List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var indexDocs []bson.M
	if err := res.All(ctx, &indexDocs); err != nil {
		t.Fatal(err)
	}
	foundUnique := false
	for _, idx := range indexDocs {
		if idx["name"] == "uniq_token" {
			foundUnique = true
		}
	}
	if !foundUnique {
		t.Fatal("the unique index must survive cleanup (Node: deleteMany keeps indexes)")
	}
	// the index still ENFORCES: two docs with the same token (fresh, since
	// cleanup emptied the collection) — the second must violate it
	nNow, _ := work.CountDocuments(ctx, bson.M{})
	idxNow, _ := work.Indexes().List(ctx)
	t.Logf("state before dup#1: count=%d indexes=%v", nNow, idxNow)
	if _, e := work.InsertOne(ctx, bson.M{"token": "dup", "n": 9}); e != nil {
		nafter, _ := work.CountDocuments(ctx, bson.M{})
		docs, _ := work.Find(ctx, bson.M{})
		var al []bson.M
		_ = docs.All(ctx, &al)
		t.Fatalf("first dup insert: %v (count-after=%d docs=%v indexes=%v)", e, nafter, al, idxNow)
	}
	if _, e := work.InsertOne(ctx, bson.M{"token": "dup", "n": 9}); e == nil {
		t.Fatal("a duplicate token must violate the preserved unique index")
	} else if !strings.Contains(e.Error(), "E11000") && !strings.Contains(e.Error(), "duplicate key") {
		t.Fatalf("expected a duplicate-key error, got: %v", e)
	}
}

func TestDropTestDatabaseLive(t *testing.T) {
	client := liveMongo(t)
	ctx := context.Background()
	t.Setenv("NODE_ENV", "test")

	const dbName = "test-overleaf"
	db := client.Database(dbName)
	if _, err := db.Collection("work").InsertOne(ctx, bson.M{"x": 1}); err != nil {
		t.Fatal(err)
	}

	if err := DropTestDatabase(ctx, client, dbName); err != nil {
		t.Fatalf("drop: %v", err)
	}

	names, err := client.ListDatabaseNames(ctx, bson.M{})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if n == dbName {
			t.Fatal("the dropped database must not be listed")
		}
	}
}

func TestGuardFiresBeforeAnyDriverCall(t *testing.T) {
	// Node order: ensureTestDatabase() FIRST — so a nil client must never be
	// dereferenced; the refusal must carry the exact message either way.
	t.Setenv("NODE_ENV", "production")
	err := CleanupTestDatabase(context.Background(), nil, "test-overleaf")
	if err == nil {
		t.Fatal("expected the guard refusal")
	}
	const want = "Refusing to clear database 'test-overleaf' in environment 'production'"
	if err.Error() != want {
		t.Fatalf("\n got: %q\nwant: %q", err.Error(), want)
	}

	t.Setenv("NODE_ENV", "test")
	err = DropTestDatabase(context.Background(), nil, "overleaf_prod")
	if err == nil {
		t.Fatal("expected the guard refusal")
	}
	const want2 = "Refusing to clear database 'overleaf_prod' in environment 'test'"
	if err.Error() != want2 {
		t.Fatalf("\n got: %q\nwant: %q", err.Error(), want2)
	}
}

func ptr(s string) *string { return &s }
func ptrBool(b bool) *bool { return &b }
