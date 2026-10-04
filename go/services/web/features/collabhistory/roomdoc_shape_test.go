// zz_live_shape_test.go — hermetic check of the room resolver's tree walk
// against the EXACT live document shape (e2e stack, 2026-10-04), decoded the
// way fetchProjectDoc decodes (extended JSON → ObjectIDs as primitive.ObjectID).
package collabhistory

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestZZLiveShapeResolver(t *testing.T) {
	ext := `{
  "_id": {"$oid": "6ac2cf693e6524c3755b34f2"},
  "rootDoc_id": {"$oid": "6ac2cf693e6524c3755b34f3"},
  "rootFolder": [{
    "name": "rootFolder",
    "_id": {"$oid": "6ac2cf693e6524c3755b34f5"},
    "docs": [
      {"name": "main.tex", "_id": {"$oid": "6ac2cf693e6524c3755b34f3"}},
      {"name": "sample.bib", "_id": {"$oid": "6ac2cf693e6524c3755b34f4"}}
    ],
    "fileRefs": [{"name": "frog.jpg", "_id": {"$oid": "6ac2cf693e6524c3755b34f6"}}]
  }]
}`
	var d bson.D
	if err := bson.UnmarshalExtJSON([]byte(ext), false, &d); err != nil {
		t.Fatal(err)
	}

	root := docIDHexOf(fldOrZero(d, "rootDoc_id"))
	t.Logf("projectRootDoc -> %q (type %T)", root, fldOrZero(d, "rootDoc_id"))
	if root != "6ac2cf693e6524c3755b34f3" {
		t.Errorf("rootDoc mismatch: %q", root)
	}

	if !docInValues(rfAny(d), "6ac2cf693e6524c3755b34f4") {
		t.Error("resolver walk MISSED sample.bib (f4) in the live doc shape")
	}
	if !docInValues(rfAny(d), "6ac2cf693e6524c3755b34f3") {
		t.Error("resolver walk missed main.tex (f3) too")
	}
	// the full handler predicate:
	if !docInRootFolder(d, "6ac2cf693e6524c3755b34f4") {
		t.Error("docInRootFolder(bib) false on the live shape")
	}
}

func rfAny(d bson.D) any {
	v, _ := fld(d, "rootFolder")
	return v
}
