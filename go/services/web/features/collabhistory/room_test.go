package collabhistory

import (
	"testing"

	"ollitex/go/services/collab"
	"ollitex/go/services/web/core"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// 024 Option B — the doc → room resolver contract (GET /project/:id/collab/room).

const (
	bdocPID = "66a00000000000000000a111"
	bdoc    = "66a00000000000000000b222"
	bdoc2   = "66a00000000000000000c333"
)

func roomProjectDoc(rootDocID string, docIDs ...string) bson.D {
	d := bson.D{{Key: "_id", Value: bson.ObjectID{0xa1, 0x11}}, {Key: "name", Value: "room-probe"}}
	if rootDocID != "" && rootDocID == "root" {
		d = append(d, bson.E{Key: "rootDoc_id", Value: docIDs[0]})
	}
	docs := make([]any, 0, len(docIDs))
	names := []string{"main.tex", "sample.bib", "extra.bib"}
	for i, id := range docIDs {
		nm := "doc"
		if i < len(names) {
			nm = names[i]
		}
		docs = append(docs, bson.D{{Key: "name", Value: nm}, {Key: "_id", Value: id}})
	}
	d = append(d, bson.E{Key: "rootFolder", Value: []any{
		bson.D{{Key: "name", Value: "rootFolder"}, {Key: "docs", Value: docs}},
	}})
	return d
}

func TestRoomResolverRootRoom(t *testing.T) {
	st, h := setupStore(t)
	_ = st
	h.DocReader = func(c *core.Cxt, pid string) (bson.D, error) {
		return roomProjectDoc("root", bdoc, bdoc2), nil
	}
	// no doc param → root room (the D19 room name — backward compatible)
	got := serve(t, h, cxt("GET", "/project/"+testPID+"/collab/room", "owner", nil), h.room)
	if got.code != 200 || got.body != `{"room":"`+testPID+`","root":true}` {
		t.Fatalf("root room = %d %s, want 200 the bare project room", got.code, got.body)
	}
	// doc == the project's rootDoc → ALSO the root room (contract: root keeps
	// the D19 room so its history/review/IndexedDB identity is unchanged)
	if got := serve(t, h, cxt("GET", "/project/"+testPID+"/collab/room?doc="+bdoc, "owner", nil), h.room); got.code != 200 || got.body != `{"room":"`+testPID+`","root":true}` {
		t.Fatalf("root doc = %d %s, want the bare project room", got.code, got.body)
	}
}

func TestRoomResolverPerDocRoom(t *testing.T) {
	st, h := setupStore(t)
	_ = st
	h.DocReader = func(c *core.Cxt, pid string) (bson.D, error) {
		return roomProjectDoc("root", bdoc, bdoc2), nil
	}
	// a NON-root doc → the per-doc room {pid}-{doc}
	got := serve(t, h, cxt("GET", "/project/"+testPID+"/collab/room?doc="+bdoc2, "owner", nil), h.room)
	want := `{"room":"` + testPID + "-" + bdoc2 + `","root":false}`
	if got.code != 200 || got.body != want {
		t.Fatalf("per-doc = %d %s, want %s", got.code, got.body, want)
	}
	if !collab.ValidRoomName(testPID + "-" + bdoc2) {
		t.Fatal("the resolved per-doc room must be a valid collab room name")
	}
}

func TestRoomResolverForeignDoc404(t *testing.T) {
	st, h := setupStore(t)
	_ = st
	h.DocReader = func(c *core.Cxt, pid string) (bson.D, error) {
		return roomProjectDoc("root", bdoc, bdoc2), nil
	}
	// a doc id NOT in the project tree → 404 (no leak, no bogus room)
	got := serve(t, h, cxt("GET", "/project/"+testPID+"/collab/room?doc=66a00000000000000000ffff", "owner", nil), h.room)
	if got.code != 404 {
		t.Fatalf("foreign doc = %d %s, want 404", got.code, got.body)
	}
	// invalid shape → 400
	if got := serve(t, h, cxt("GET", "/project/"+testPID+"/collab/room?doc=zzz", "owner", nil), h.room); got.code != 400 {
		t.Fatalf("malformed doc = %d %s, want 400", got.code, got.body)
	}
}

func TestRoomResolverRoleGate(t *testing.T) {
	st, h := setupStore(t)
	_ = st
	h.DocReader = func(c *core.Cxt, pid string) (bson.D, error) {
		return roomProjectDoc("root", bdoc, bdoc2), nil
	}
	if got := serve(t, h, cxt("GET", "/project/"+testPID+"/collab/room", "stranger", nil), h.room); got.code != 404 {
		t.Fatalf("stranger = %d %s, want 404 (role gate)", got.code, got.body)
	}
}
