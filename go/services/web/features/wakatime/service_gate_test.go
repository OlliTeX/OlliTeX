package wakatime

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// audit 035 — modern project docs carry `collab_refs` (Overleaf 6.x shape);
// the gate must accept those holders, not just the legacy `collaborator_refs`.
func TestCanReadProjectModernCollabRefs(t *testing.T) {
	p := bson.D{{Key: "owner_ref", Value: "someone-else"},
		{Key: "collab_refs", Value: bson.A{"6ab876738b368815f15d216b"}}}
	if !canReadProject("6ab876738b368815f15d216b", false, p) {
		t.Fatal("holder in collab_refs must be able to read (modern shape)")
	}
	if canReadProject("stranger", false, p) {
		t.Fatal("unrelated user must not read")
	}
	pl := bson.D{{Key: "owner_ref", Value: "someone-else"},
		{Key: "collaborator_refs", Value: bson.A{"6ab876738b368815f15d216b"}}}
	if !canReadProject("6ab876738b368815f15d216b", false, pl) {
		t.Fatal("legacy collaborator_refs holders must still read")
	}
}

// audit 035 — project docs store owner_ref / list refs as bson.ObjectID,
// not string (live shape); the gate must accept the ObjectID form.
func TestCanReadProjectObjectIDOwner(t *testing.T) {
	oid, _ := bson.ObjectIDFromHex("6ab876738b368815f15d216b")
	p := bson.D{{Key: "owner_ref", Value: oid},
		{Key: "collab_refs", Value: bson.A{oid}}}
	if !canReadProject("6ab876738b368815f15d216b", false, p) {
		t.Fatal("ObjectID owner/collab must match (live mongo shape)")
	}
	if canReadProject("stranger", false, p) {
		t.Fatal("unrelated user must not read")
	}
}
