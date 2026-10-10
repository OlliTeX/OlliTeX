package editorpages

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Q (2026-10-10, two-cooperator E2E): the v2 driver decodes BSON arrays
// into bson.A (named type) when decoding into map[string]any — a bare
// `v.([]any)` assertion fails on it, which 403'd EVERY refs-authorized
// collaborator on the editor page (owner path was unaffected).
func TestOidInList_BsonA(t *testing.T) {
	oid := bson.ObjectID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c}
	cases := []struct {
		name string
		v    any
		want bool
	}{
		{"bson.A of ObjectID", bson.A{oid}, true},
		{"[]any of ObjectID", []any{oid}, true},
		{"bson.A of hex string", bson.A{oid.Hex()}, true},
		{"[]any of hex string", []any{oid.Hex()}, true},
		{"[]string hex", []string{oid.Hex()}, true},
		{"[]ObjectID", []bson.ObjectID{oid}, true},
		{"foreign oid", bson.A{bson.ObjectID{0xff}}, false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		got := oidInList(c.v, oid.Hex())
		if got != c.want {
			t.Fatalf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestProjectCanRead_CollabRef(t *testing.T) {
	oid := bson.ObjectID{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c}
	owner := bson.ObjectID{0x99, 0x99, 0x99, 0x99, 0x99, 0x99, 0x99, 0x99, 0x99, 0x99, 0x99, 0x99}
	d := map[string]any{
		"owner_ref":         owner,
		"collaberator_refs": bson.A{oid},
	}
	if !projectCanRead(oid.Hex(), false, d) {
		t.Fatal("collab (bson.A) must be canRead")
	}
	if !projectCanRead(owner.Hex(), false, d) {
		t.Fatal("owner must be canRead")
	}
	if projectCanRead(bson.ObjectID{0xee}.Hex(), false, d) {
		t.Fatal("stranger must NOT be canRead")
	}
}
