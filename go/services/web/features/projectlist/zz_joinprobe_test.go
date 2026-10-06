package projectlist

// zz_joinprobe_test.go — live-diagnostic: why does the owner's join 403?
import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestJoinProbe(t *testing.T) {
	if os.Getenv("OLLITEX_JOINPROBE") != "1" {
		t.Skip("set OLLITEX_JOINPROBE=1 to run the live join probe")
	}
	uri := os.Getenv("OLLITEX_JOINPROBE_MONGO")
	if uri == "" {
		uri = "mongodb://127.0.0.1:27017/ollitex"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatal(err)
	}
	db := client.Database("ollitex")

	uid := "671b6c9ef2e168f259294e9a" // davrot@uni-bremen.de
	pid := "671bd3cf6e2d5e98f0eacd9a"
	oid, _ := bson.ObjectIDFromHex(pid)
	var pd bson.D
	if err := db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&pd); err != nil {
		t.Fatalf("project load: %v", err)
	}
	for _, e := range pd {
		t.Logf("field %-28s = %T %v", e.Key, e.Value, summarizeVal(e.Value))
	}
	or := dget(pd, "owner_ref")
	t.Logf("owner_ref raw: %T %v", or, summarizeVal(or))
	t.Logf("oidHex(owner_ref) = %q", oidHex(or))
	t.Logf("uid = %q", uid)
	t.Logf("owner match: %v", ownerRefHex(pd) == uid)
	pal := asStr(dget(pd, "publicAccesLevel"))
	t.Logf("publicAccesLevel = %q", pal)
	for _, listKey := range []string{"collablator_refs", "collaborator_refs", "reviewer_refs", "readOnly_refs", "tokenAccessReadAndWrite_refs", "tokenAccessReadOnly_refs"} {
		v := dget(pd, listKey)
		t.Logf("list %-28s = %T n=%d contains-uid=%v", listKey, v, lenAs(v), inOIDList(v, uid))
	}
	t.Logf("=> level = %q", joinPrivilegeForUser(uid, pd, pal))

	uoid, _ := bson.ObjectIDFromHex(uid)
	var udoc bson.D
	if err := db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: uoid}}).Decode(&udoc); err != nil {
		t.Logf("user load: %v", err)
	}
	for _, e := range udoc {
		if strings.HasPrefix(strings.ToLower(e.Key), "email") {
			t.Logf("user field %s = %v", e.Key, summarizeVal(e.Value))
		}
	}
}

func summarizeVal(v any) any {
	switch x := v.(type) {
	case string:
		if len(x) > 40 {
			return x[:40] + "…"
		}
	case bson.A:
		return "array(n=" + itoa(int64(len(x))) + ")"
	case bson.D:
		return "doc(n=" + itoa(int64(len(x))) + ")"
	}
	return v
}

func lenAs(v any) int {
	switch x := v.(type) {
	case bson.A:
		return len(x)
	case []any:
		return len(x)
	}
	return 0
}
