package notifications

import (
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

// --- getGlobalJSON -----------------------------------------------------------

func TestGetGlobalEmptyDocAllDefaults(t *testing.T) {
	got := string(getGlobalJSON(bson.D{}))
	want := `{"muteAllNotifications":false,"notificationDelayMinutes":null,` +
		`"commentOnOwnProject":true,"commentOnInvitedProject":true,` +
		`"repliesOnAuthoredThread":true,"repliesOnParticipatingThread":true,` +
		`"commentResolvedOnAuthoredThread":true,"commentResolvedOnParticipatingThread":true,` +
		`"commentReopenedOnAuthoredThread":true,"commentReopenedOnParticipatingThread":true,` +
		`"trackedChangesOnOwnProject":true,"trackedChangesOnInvitedProject":true,` +
		`"trackChangesAcceptedOnAuthoredChange":true,"trackChangesRejectedOnAuthoredChange":true}`
	if got != want {
		t.Fatalf("all-defaults global json:\n got %s\nwant %s", got, want)
	}
}

func TestGetGlobalDocValuesAndDelayNormalization(t *testing.T) {
	doc := bson.D{
		{Key: "muteAllNotifications", Value: true},
		{Key: "notificationDelayMinutes", Value: 5},
		{Key: "commentOnOwnProject", Value: false},
		{Key: "commentInvitedTypo", Value: nil}, // unknown key — ignored
	}
	got := string(getGlobalJSON(doc))
	if !strings.HasPrefix(got, `{"muteAllNotifications":true,"notificationDelayMinutes":5,`) {
		t.Fatalf("prefix: %s", got)
	}
	if !strings.Contains(got, `"commentOnOwnProject":false`) {
		t.Fatalf("doc false not honored: %s", got)
	}
	if !strings.Contains(got, `"commentOnInvitedProject":true`) {
		t.Fatalf("default expected: %s", got)
	}
	// out-of-range stored delay → null (Node normalizeGlobalDelayMinutes)
	d2 := bson.D{{Key: "notificationDelayMinutes", Value: 99999}}
	if got2 := string(getGlobalJSON(d2)); !strings.Contains(got2, `"notificationDelayMinutes":null`) {
		t.Fatalf("out-of-range delay:\n%s", got2)
	}
	// wrong-type stored delay → null
	d3 := bson.D{{Key: "notificationDelayMinutes", Value: "5"}}
	if got3 := string(getGlobalJSON(d3)); !strings.Contains(got3, `"notificationDelayMinutes":null`) {
		t.Fatalf("string delay:\n%s", got3)
	}
	// boundary 10080
	d4 := bson.D{{Key: "notificationDelayMinutes", Value: 10080}}
	if got4 := string(getGlobalJSON(d4)); !strings.Contains(got4, `"notificationDelayMinutes":10080`) {
		t.Fatalf("max delay:\n%s", got4)
	}
}

// --- ntfBool / ntfDelay ------------------------------------------------------

func TestNtfBoolCoercion(t *testing.T) {
	if ntfBool(true) != true || ntfBool(false) != false {
		t.Fatal("bool identity")
	}
	for _, v := range []interface{}{nil, 1, "true", 1.0} {
		if ntfBool(v) {
			t.Fatalf("Boolean(%v) must be false", v)
		}
	}
}

func TestNtfDelayStored(t *testing.T) {
	cases := []struct {
		v    interface{}
		want int
		has  bool
	}{
		{nil, 0, false},
		{0, 0, false},
		{1, 1, true},
		{10080, 10080, true},
		{10081, 0, false},
		{"5", 0, false},
		{2.5, 0, false},
	}
	for _, c := range cases {
		got, has := ntfDelay(c.v)
		if got != c.want || has != c.has {
			t.Fatalf("ntfDelay(%v) = (%d, %v), want (%d, %v)", c.v, got, has, c.want, c.has)
		}
	}
}

// --- ntfBodyClass ------------------------------------------------------------

func TestNtfBodyClassMatrix(t *testing.T) {
	cases := []struct {
		raw  string
		cls  string
		keys int // only meaningful for object
	}{
		{"", "empty", 0},
		{"{bad", "garbage", 0},
		{`"hello"`, "garbage", 0},
		{"null", "garbage", 0},
		{"[1,2]", "array", 0},
		{"{}", "object", 0},
		{`{"muteAllNotifications":false}`, "object", 1},
	}
	for _, c := range cases {
		cls, obj := ntfBodyClass([]byte(c.raw))
		if cls != c.cls {
			t.Fatalf("class(%q) = %q, want %q", c.raw, cls, c.cls)
		}
		if cls == "object" && len(obj) != c.keys {
			t.Fatalf("object len(%s) = %d, want %d", c.raw, len(obj), c.keys)
		}
	}
}

// --- ntfHasAccess ------------------------------------------------------------

func TestNtfHasAccessSets(t *testing.T) {
	const uid = "aaaaaaaaaaaaaaaaaaaaaaaa"
	doc := bson.D{
		{Key: "owner_ref", Value: "bbbbbbbbbbbbbbbbbbbbbbbb"},
		{Key: "collaberator_refs", Value: []interface{}{uid}},
	}
	if !ntfHasAccess(uid, doc) {
		t.Fatal("collab ref must grant access")
	}
	doc2 := bson.D{
		{Key: "owner_ref", Value: uid},
	}
	if !ntfHasAccess(uid, doc2) {
		t.Fatal("owner_ref must grant access")
	}
	doc3 := bson.D{
		{Key: "reviewer_refs", Value: []interface{}{uid}}, // NOT in the Node 5-set
	}
	if ntfHasAccess(uid, doc3) {
		t.Fatal("reviewer_refs must NOT grant access (pinned Node set)")
	}
	doc4 := bson.D{
		{Key: "readOnly_refs", Value: []interface{}{"cccccccccccccccccccccccc"}},
	}
	if ntfHasAccess(uid, doc4) {
		t.Fatal("non-member must not match")
	}
}
