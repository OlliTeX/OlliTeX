package collab

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/reearth/ygo/crdt"
	"github.com/reearth/ygo/persistence"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// mongoTestStore — live-Mongo backing for the conformance suite. Skipped
// when no Mongo URL is configured (CI provides one; local dev sets
// TEST_MONGO_URL). Uses a DISCARDABLE database per run — never the app DB.
func TestMongoStoreConformance(t *testing.T) {
	uri := os.Getenv("TEST_MONGO_URL")
	if uri == "" {
		uri = os.Getenv("MONGO_CONNECTION_STRING")
	}
	if uri == "" {
		uri = os.Getenv("OVERLEAF_MONGO_URL")
	}
	if uri == "" {
		t.Skip("set TEST_MONGO_URL (or MONGO_CONNECTION_STRING/OVERLEAF_MONGO_URL) to run the Mongo conformance suite")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := c.Ping(ctx, nil); err != nil {
		t.Fatalf("ping: %v", err)
	}
	// isolated, disposable database (test DB naming convention)
	dbName := "test-collab-" + os.Getenv("USER")
	if dbName == "test-collab-" {
		dbName = "test-collab"
	}
	db := c.Database(dbName)
	defer func() { _ = db.Drop(ctx) }()

	st, err := NewMongoStore(ctx, db)
	if err != nil {
		t.Fatalf("NewMongoStore: %v", err)
	}
	// RunConformance calls factory() per subtest expecting a FRESH store
	// (MemoryPersistence semantics) — clear the room data each time.
	makeFresh := func() persistence.VersionedPersistence {
		if err := st.ClearAll(ctx); err != nil {
			t.Fatalf("clear: %v", err)
		}
		return st
	}
	persistence.RunConformance(t, makeFresh)
}

// TestMongoStoreAdapterBridge — the exact bridge the WS server uses
// (LegacyAdapter over the Mongo store): append via StoreUpdate, read back
// via LoadDoc, and confirm the materialized text.
func TestMongoStoreAdapterBridge(t *testing.T) {
	uri := os.Getenv("TEST_MONGO_URL")
	if uri == "" {
		uri = os.Getenv("MONGO_CONNECTION_STRING")
	}
	if uri == "" {
		uri = os.Getenv("OVERLEAF_MONGO_URL")
	}
	if uri == "" {
		t.Skip("no Mongo URL configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	db := c.Database("test-collab-bridge")
	defer func() { _ = db.Drop(ctx) }()

	st, err := NewMongoStore(ctx, db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	ad := persistence.NewLegacyAdapter(st)

	d := crdt.New()
	d.Transact(func(txn *crdt.Transaction) { txn.GetText("content").Insert(txn, 0, "bridge ", nil) })
	full := crdt.EncodeStateAsUpdateV1(d, nil)
	if err := ad.StoreUpdate("room-x", full); err != nil {
		t.Fatalf("StoreUpdate: %v", err)
	}
	d.Transact(func(txn *crdt.Transaction) { txn.GetText("content").Insert(txn, 7, "ok", nil) })
	full2 := crdt.EncodeStateAsUpdateV1(d, nil)
	if err := ad.StoreUpdate("room-x", full2); err != nil {
		t.Fatalf("StoreUpdate2: %v", err)
	}

	stored, err := ad.LoadDoc("room-x")
	if err != nil {
		t.Fatalf("LoadDoc: %v", err)
	}
	if len(stored) == 0 {
		t.Fatal("LoadDoc returned empty state")
	}
	rest := crdt.New()
	if err := crdt.ApplyUpdateV1(rest, stored, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := rest.GetText("content").ToString(); got != "bridge ok" {
		t.Fatalf("restored %q, want %q", got, "bridge ok")
	}

	// unknown room -> nil,nil (adapter contract)
	none, err := ad.LoadDoc("room-nosuch")
	if err != nil || none != nil {
		t.Fatalf("unknown room: state=%v err=%v, want nil,nil", none, err)
	}
}
