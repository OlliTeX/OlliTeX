package sso

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// TestProvidersFieldShapes — regression for the "Add provider wipes earlier
// providers" bug (live, 2026-10-09): the stored list decodes as []bson.M
// (mongo-driver v2), the old []any assertion failed and returned nil. The
// helper must extract the full list from every shape the field can take.
func TestProvidersFieldShapes(t *testing.T) {
	p1 := bson.M{"id": "a", "type": "saml", "enabled": true}
	p2 := bson.M{"id": "b", "type": "oidc"}

	// shape 0: THE REAL live shape — bson.A of bson.D (captured from
	// ollitex-mongo 2026-10-09). This is the one that broke everything.
	d1 := bson.D{{Key: "id", Value: "a"}, {Key: "type", Value: "saml"}}
	d2 := bson.D{{Key: "id", Value: "b"}}
	m0 := bson.M{"providers": bson.A{d1, d2}}
	if got := providersField(m0); len(got) != 2 || got[0]["id"] != "a" || got[1]["id"] != "b" {
		t.Fatalf("bson.A{bson.D} live shape: %+v", got)
	}

	// shape 1: decoded Mongo doc — bson.A of bson.M
	m1 := bson.M{"providers": bson.A{p1, p2}}
	got1 := providersField(m1)
	if len(got1) != 2 {
		t.Fatalf("bson.A shape: got %d providers, want 2", len(got1))
	}
	if got1[0]["id"] != "a" || got1[1]["id"] != "b" {
		t.Fatalf("order/identity broken: %+v", got1)
	}

	// shape 2: []bson.M directly
	m2 := bson.M{"providers": []bson.M{p1, p2}}
	got2 := providersField(m2)
	if len(got2) != 2 || got2[0]["id"] != "a" {
		t.Fatalf("[]bson.M shape: %+v", got2)
	}

	// shape 3: plain []any of maps (JSON body shape)
	m3 := bson.M{"providers": []any{p1, p2}}
	if got3 := providersField(m3); len(got3) != 2 {
		t.Fatalf("[]any shape: %+v", got3)
	}

	// shape 4: absent / empty
	if got := providersField(bson.M{}); len(got) != 0 {
		t.Fatalf("absent: got %v", got)
	}
	if got := providersField(bson.M{"providers": []bson.M{}}); len(got) != 0 {
		t.Fatalf("empty: got %v", got)
	}

	// shape 5: single map (tolerant)
	if got := providersField(bson.M{"providers": p1}); len(got) != 1 || got[0]["id"] != "a" {
		t.Fatalf("single map: %+v", got)
	}

	// setProviders round trip must keep the list intact
	cfg := bson.M{"_id": "x"}
	setProviders(cfg, nil)
	if got := providersField(cfg); len(got) != 0 {
		t.Fatalf("after setProviders(nil): %v", got)
	}
	setProviders(cfg, []bson.M{p1, p2, bson.M{"id": "c"}})
	if got := providersField(cfg); len(got) != 3 || got[2]["id"] != "c" {
		t.Fatalf("round trip: %v", got)
	}

	// attrFilter rows
	af := bson.A{bson.M{"attribute": "x", "role": "local", "match": "equals"}}
	rows, ok := toAttrFilterRows(af)
	if !ok || len(rows) != 1 || rows[0]["attribute"] != "x" {
		t.Fatalf("attrFilter bson.A: ok=%v rows=%v", ok, rows)
	}
	if rows, ok := toAttrFilterRows(nil); ok {
		t.Fatalf("attrFilter nil should be ok=false: %v", rows)
	}
}
