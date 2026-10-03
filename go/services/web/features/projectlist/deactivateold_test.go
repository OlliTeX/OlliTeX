package projectlist

// Pinned against the Node oracle
// (services/web/app/src/Features/InactiveData/InactiveProjectManager.mjs
// findInactiveProjects + controller), b63post-2 (TODO-deb4b4ba). Pure-function
// green-slice for the /internal/deactivateOldProjects bulk handler.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// TestDeactivateOldQueryShape — the exact mongo filter Node issues:
// { active: true, lastOpened: { $not: { $gt: now - daysOld·24h } } }.
// The {$not,$gt} (not $lt) is load-bearing: it also matches projects with
// lastOpened unset (newDate() coercion → null) per the Node comment "catch
// non-opened projects where lastOpened is null".
func TestDeactivateOldQueryShape(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 123*1e6, time.UTC)
	q := deactivateOldQuery(360, now)
	if len(q) != 2 {
		t.Fatalf("filter len = %d want 2: %+v", len(q), q)
	}
	if q[0].Key != "active" || q[0].Value != true {
		t.Fatalf("active clause = %+v want {active:true}", q[0])
	}
	if q[1].Key != "lastOpened" {
		t.Fatalf("lastOpened clause key = %q", q[1].Key)
	}
	notD, ok := q[1].Value.(bson.D)
	if !ok || len(notD) != 1 || notD[0].Key != "$not" {
		t.Fatalf("$not shape = %+v", q[1].Value)
	}
	arr, ok := notD[0].Value.(bson.A)
	if !ok || len(arr) != 1 {
		t.Fatalf("$not array = %T", notD[0].Value)
	}
	gtD, ok := arr[0].(bson.D)
	if !ok || len(gtD) != 1 || gtD[0].Key != "$gt" {
		t.Fatalf("$gt shape = %v", arr[0])
	}
	got, ok := gtD[0].Value.(time.Time)
	if !ok {
		t.Fatalf("cutoff type = %T", gtD[0].Value)
	}
	want := now.Add(-360 * 24 * time.Hour)
	if !got.Equal(want) {
		t.Fatalf("cutoff = %v want %v", got, want)
	}
}

// TestDeactivateOldDefaults — Node: numberOfProjectsToArchive default 10,
// ageOfProjects default 360.
func TestDeactivateOldDefaults(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(""), []byte(`{}`), []byte(`{"unrelated":1}`)} {
		limit, days := deactivateOldParams(body)
		if limit != 10 || days != 360 {
			t.Fatalf("body %s: limit=%d days=%v want 10/360", body, limit, days)
		}
	}
}

// TestDeactivateOldCoerce — z.coerce.number() parity: JSON numbers and numeric
// STRINGS both coerce; non-numeric values are absent (parseReq logOnly:true →
// the controller NEVER 400s on this body).
func TestDeactivateOldCoerce(t *testing.T) {
	cases := []struct {
		body  string
		limit int
		days  float64
	}{
		{`{"numberOfProjectsToArchive":5}`, 5, 360},
		{`{"ageOfProjects":120}`, 10, 120},
		{`{"numberOfProjectsToArchive":"7","ageOfProjects":"120.5"}`, 7, 120.5},
		{`{"numberOfProjectsToArchive":{"x":1}}`, 10, 360}, // object → absent
		{`{"ageOfProjects":true}`, 10, 360},                // bool → absent
		{`{"ageOfProjects":"soon"}`, 10, 360},              // non-numeric → absent
		{`garbage`, 10, 360},                               // malformed → defaults
	}
	for _, c := range cases {
		limit, days := deactivateOldParams([]byte(c.body))
		if limit != c.limit || days != c.days {
			t.Fatalf("body %s: got %d/%v want %d/%v", c.body, limit, days, c.limit, c.days)
		}
	}
}

// TestDeactivateOldItemJSON — entry shape (select projection _id + lastOpened,
// _id first per mongoose doc order; null when the doc has none; ISO with ms).
func TestDeactivateOldItemJSON(t *testing.T) {
	got := string(deactivateOldItem("6aa4b8a873ef0e5094f4cba3", nil))
	want := `{"_id":"6aa4b8a873ef0e5094f4cba3","lastOpened":null}`
	if got != want {
		t.Fatalf("null item = %s want %s", got, want)
	}

	tm := time.Date(2026, 9, 30, 12, 20, 0, 123*1e6, time.UTC)
	got = string(deactivateOldItem("abc123abc123abc123abc123", &tm))
	want = `{"_id":"abc123abc123abc123abc123","lastOpened":"2026-09-30T12:20:00.123Z"}`
	if got != want {
		t.Fatalf("dated item = %s want %s", got, want)
	}

	// must be valid JSON both alone and in the array the handler emits.
	var m map[string]any
	if json.Unmarshal([]byte(got), &m) != nil {
		t.Fatal("item is not valid JSON")
	}
	arr := "[" + strings.Join([]string{got, got}, ",") + "]"
	var slice []map[string]any
	if json.Unmarshal([]byte(arr), &slice) != nil || len(slice) != 2 {
		t.Fatalf("array parse failed: %q", arr)
	}
}

// TestDeactivateOldArrayJoin — empty batch → `[]` (Node res.json([])).
func TestDeactivateOldArrayJoin(t *testing.T) {
	if s := "[" + strings.Join(nil, ",") + "]"; s != "[]" {
		t.Fatalf("empty array = %q", s)
	}
}
