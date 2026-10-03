package webdav

import (
	"context"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"ollitex/go/services/web/core"
)

// TestWdProjectTreeGoShape — push-walker contract (WDV-C 2026-10-03): the Go
// build keeps project entries in top-level docs[]/files[] arrays (no
// rootFolder); wdProjectTree must export them or push silently PUTs zero
// files while answering 200 "Push completed".
func TestWdProjectTreeGoShape(t *testing.T) {
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		uri = "mongodb://127.0.0.1:27017"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m := core.NewMongoLazy(uri)
	if _, err := m.Client(ctx); err != nil {
		t.Skipf("mongo unavailable: %v", err)
	}
	var p struct {
		ID   bson.ObjectID `bson:"_id"`
		Name string        `bson:"name"`
	}
	db, err := m.DB(ctx)
	if err != nil {
		t.Skipf("db: %v", err)
	}
	cur, err := db.Collection("projects").Find(ctx, map[string]interface{}{
		"docs.name": "chapter.tex",
	})
	if err != nil {
		t.Skipf("query: %v", err)
	}
	found := false
	for cur.Next(ctx) {
		var row struct {
			ID   bson.ObjectID `bson:"_id"`
			Name string        `bson:"name"`
		}
		if derr := cur.Decode(&row); derr != nil {
			continue
		}
		p = row
		found = true
		break
	}
	if !found {
		t.Skip("no docs[]-bearing project present (run a webdav import first)")
	}
	a := &core.App{Mongo: m}
	entries, _, werr := wdProjectTree(a, ctx, p.ID.Hex())
	if werr != nil {
		t.Fatalf("wdProjectTree: %v", werr)
	}
	got := map[string]bool{}
	for _, e := range entries {
		got[e.path] = e.isDoc
	}
	t.Logf("push tree for %q: %v", p.Name, got)
	if _, ok := got["chapter.tex"]; !ok {
		t.Fatalf("chapter.tex missing from push tree — top-level docs[] not walked (entries=%v)", entries)
	}
}
