package userpages

import (
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ---------- sortContacts (Node sortContacts: n DESC, ts DESC, stable) ----------

func TestSortContactsOrder(t *testing.T) {
	t1 := time.Date(2026, 9, 22, 13, 27, 50, 350e6, time.UTC) // admin
	t2 := time.Date(2026, 9, 22, 13, 27, 51, 129e6, time.UTC) // tpladmin (later)
	t3 := time.Date(2026, 9, 21, 23, 54, 40, 862e6, time.UTC) // tpladmin n=3 ts
	entries := []contactEntry{
		{id: "aaaa", n: 1, ts: t1, tsOK: true},
		{id: "bbbb", n: 3, ts: t3, tsOK: true},
		{id: "cccc", n: 1, ts: t2, tsOK: true},
		{id: "dddd", n: 1, tsOK: false}, // untimestamped ranks after
		{id: "eeee", n: 5, ts: t3, tsOK: true},
	}
	sortContacts(entries)
	want := []string{"eeee", "bbbb", "cccc", "aaaa", "dddd"}
	got := make([]string, len(entries))
	for i, e := range entries {
		got[i] = e.id
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestSortContactsStableOnFullTie(t *testing.T) {
	ts := time.Date(2026, 9, 1, 0, 0, 0, 123e6, time.UTC)
	entries := []contactEntry{
		{id: "x1", n: 2, ts: ts, tsOK: true},
		{id: "x2", n: 2, ts: ts, tsOK: true},
		{id: "x3", n: 2, ts: ts, tsOK: true},
	}
	sortContacts(entries)
	// Full tie: stable order preserved.
	if entries[0].id != "x1" || entries[1].id != "x2" || entries[2].id != "x3" {
		t.Fatalf("tie order = %v %v %v", entries[0].id, entries[1].id, entries[2].id)
	}
}

// ---------- parseContacts (BSON doc shape) ----------

func TestParseContactsBSON(t *testing.T) {
	tsA := primitive.NewDateTimeFromTime(time.Date(2026, 9, 22, 13, 27, 50, 329e6, time.UTC))
	tsB := primitive.NewDateTimeFromTime(time.Date(2026, 9, 22, 13, 27, 51, 105e6, time.UTC))
	cdoc := bson.D{{Key: "contacts", Value: bson.D{
		{Key: "6aa4b8b5...", Value: bson.D{
			{Key: "n", Value: int32(1)},
			{Key: "ts", Value: tsA},
		}},
		{Key: "6aa4b8c0...", Value: bson.D{
			{Key: "n", Value: int32(1)},
			{Key: "ts", Value: tsB},
		}},
	}}}
	entries := parseContacts(cdoc)
	if len(entries) != 2 {
		t.Fatalf("got %d entries", len(entries))
	}
	sortContacts(entries)
	if entries[0].id != "6aa4b8c0..." || entries[1].id != "6aa4b8b5..." {
		t.Fatalf("ts DESC order broken: %v then %v", entries[0].id, entries[1].id)
	}
	if entries[0].n != 1 {
		t.Fatalf("n = %d", entries[0].n)
	}
}

func TestParseContactsEmpty(t *testing.T) {
	if e := parseContacts(bson.D{}); e != nil {
		t.Fatalf("want nil, got %v", e)
	}
}

// ---------- contactsRowJSON (formatting + holding filter) ----------

func TestContactsRowJSON(t *testing.T) {
	u := bson.D{
		{Key: "_id", Value: mustObjectIDFromHex("6aa4b8c0ee67ff98732d4947")},
		{Key: "email", Value: "e2e-tpladmin@e2e.test"},
		{Key: "first_name", Value: "E2e"},
		{Key: "last_name", Value: "Tpladmin"},
	}
	row, ok := contactsRowJSON(contactEntry{id: "6aa4b8c0ee67ff98732d4947"}, u)
	if !ok {
		t.Fatal("expected row")
	}
	want := `{"id":"6aa4b8c0ee67ff98732d4947","email":"e2e-tpladmin@e2e.test","first_name":"E2e","last_name":"Tpladmin","type":"user"}`
	if row != want {
		t.Fatalf("row:\n got  %s\n want %s", row, want)
	}
	// absence → ""
	u2 := bson.D{{Key: "email", Value: "a@b.c"}}
	row2, _ := contactsRowJSON(contactEntry{id: "abcd"}, u2)
	if !strings.Contains(row2, `"first_name":""`) || !strings.Contains(row2, `"last_name":""`) {
		t.Fatalf("absent names must be empty strings: %s", row2)
	}
	// holding account → dropped
	u3 := bson.D{{Key: "email", Value: "hold@x.test"}, {Key: "holdingAccount", Value: true}}
	if _, ok3 := contactsRowJSON(contactEntry{id: "abcd"}, u3); ok3 {
		t.Fatal("holdingAccount user must be dropped")
	}
}

func mustObjectIDFromHex(h string) primitive.ObjectID {
	oid, err := primitive.ObjectIDFromHex(h)
	if err != nil {
		panic(err)
	}
	return oid
}

// ---------- emailJSONStruct (wire order + CE flags) ----------

func TestEmailJSONWire(t *testing.T) {
	created := primitive.NewDateTimeFromTime(time.Date(2026, 9, 12, 2, 27, 52, 936e6, time.UTC))
	ed := emailDocV{
		Email:            "e2e-admin@e2e.test",
		ReversedHostname: "tset.e2e",
		CreatedAt:        &created,
		ID:               mustObjectIDFromHex("6aa4b8a873ef0e5094f4cba4"),
	}
	got := emailJSONStruct(ed, "e2e-admin@e2e.test")
	want := `{"email":"e2e-admin@e2e.test","reversedHostname":"tset.e2e","createdAt":"2026-09-12T02:27:52.936Z","_id":"6aa4b8a873ef0e5094f4cba4","default":true,"emailHasInstitutionLicence":false,"lastConfirmedAt":null}`
	if got != want {
		t.Fatalf("email row:\n got  %s\n want %s", got, want)
	}
	// secondary: default false + confirmedAt -> lastConfirmedAt
	confirmed := primitive.NewDateTimeFromTime(time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC))
	ed2 := emailDocV{
		Email:            "second@x.test",
		ReversedHostname: "tse.x",
		CreatedAt:        &created,
		ID:               mustObjectIDFromHex("6aa4b8a873ef0e5094f4cba5"),
		ConfirmedAt:      &confirmed,
	}
	got2 := emailJSONStruct(ed2, "e2e-admin@e2e.test")
	if !strings.Contains(got2, `"default":false`) || !strings.Contains(got2, `"lastConfirmedAt":"2026-08-01T10:00:00.000Z"`) {
		t.Fatalf("secondary row: %s", got2)
	}
	// reconfirmedAt wins over confirmedAt
	ed3 := emailDocV{
		Email:         "third@x.test",
		ConfirmedAt:   &confirmed,
		ReconfirmedAt: &created,
	}
	got3 := emailJSONStruct(ed3, "e2e-admin@e2e.test")
	if !strings.Contains(got3, `"lastConfirmedAt":"2026-09-12T02:27:52.936Z"`) {
		t.Fatalf("reconfirmedAt precedence: %s", got3)
	}
	// absent createdAt/_id -> keys omitted entirely (Node JSON.stringify
	// drops undefined), rest of the wire intact (tpladmin fixture shape)
	ed4 := emailDocV{Email: "n@x.test", ReversedHostname: "tse.x"}
	got4 := emailJSONStruct(ed4, "n@x.test")
	if got4 != `{"email":"n@x.test","reversedHostname":"tse.x","default":true,"emailHasInstitutionLicence":false,"lastConfirmedAt":null}` {
		t.Fatalf("absent-key omission: %s", got4)
	}
}

// ---------- reversedHostname + jsonTime ----------

func TestReversedHostname(t *testing.T) {
	cases := map[string]string{
		"a@e2e.test":      "tset.e2e",
		"b@sub.dom.co.uk": "ku.oc.mod.bus",
		"no-at":           "",
		"trailing@":       "",
	}
	for in, want := range cases {
		if got := reversedHostname(in); got != want {
			t.Errorf("reversedHostname(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestJSONTimeMillis(t *testing.T) {
	tm := time.Date(2026, 9, 12, 2, 27, 52, 936e6, time.UTC)
	if got := jsonTime(tm); got != "2026-09-12T02:27:52.936Z" {
		t.Fatalf("jsonTime = %s", got)
	}
	tm2 := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if got := jsonTime(tm2); got != "2020-01-02T03:04:05.000Z" {
		t.Fatalf("jsonTime zero-ms = %s", got)
	}
}
