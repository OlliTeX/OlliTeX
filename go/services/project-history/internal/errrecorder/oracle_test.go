// B12 oracle — vendor ErrorRecorderTest.js cases + the faithful-quirk pins
// (Q1 pre-image getLastFailure, Q2 byType branch, Q3 category omission,
// Q4 no-value error, Q5 forceDebug coercion).
package errrecorder

import (
	"context"
	"testing"
	"time"
)

// fakeStore captures the update/filter payloads like the vendor sinon stubs.
type fakeStore struct {
	found    map[string]any // what FindOne/FindOneAndUpdate "returns"
	deleted  []map[string]any
	updated  []map[string]any // the `update` payloads of UpdateOne
	fouFound map[string]any   // FindOneAndUpdate result ("value")
	foFilter []map[string]any
	inserted []map[string]any
	all      []map[string]any
}

func (f *fakeStore) FindOneAndUpdate(ctx context.Context, filter, update map[string]any, retAfter bool, projection map[string]any) (map[string]any, error) {
	f.foFilter = append(f.foFilter, filter)
	f.updated = append(f.updated, update)
	return f.fouFound, nil
}
func (f *fakeStore) DeleteOne(ctx context.Context, filter map[string]any) (int64, error) {
	f.deleted = append(f.deleted, filter)
	return 1, nil
}
func (f *fakeStore) UpdateOne(ctx context.Context, filter, update map[string]any, upsert bool) error {
	f.foFilter = append(f.foFilter, filter)
	f.updated = append(f.updated, update)
	return nil
}
func (f *fakeStore) FindAll(ctx context.Context) ([]map[string]any, error) { return f.all, nil }
func (f *fakeStore) FindOne(ctx context.Context, filter map[string]any, projection map[string]any) (map[string]any, error) {
	return f.found, nil
}
func (f *fakeStore) InsertOne(ctx context.Context, doc map[string]any) error {
	f.inserted = append(f.inserted, doc)
	return nil
}

type fakeClock struct{ t time.Time }

func (fakeClock) Now() time.Time { return time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC) }

type gaugeSink struct {
	calls []string // name + status label (order-relevant)
	vals  map[string]float64
}

func (g *gaugeSink) GlobalGauge(name string, value float64, mtype int, labels map[string]string) {
	key := name + "/" + labels["status"]
	g.calls = append(g.calls, key)
	g.vals[key] = value
}

var b12ctx = context.Background()

// --- vendor test case 1: record ---------------------------------------------

func TestB12_RecordVendorOracle(t *testing.T) {
	fs := &fakeStore{fouFound: map[string]any{"_id": "x", "project_id": "project-id-123"}}
	d := &Deps{Store: fs, Clock: fakeClock{}}
	got, err := Record(b12ctx, d, "project-id-123", 445, "Error: something bad", "/tmp/stack/x")
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	// vendor assertions (calledWithMatch):
	u := fs.updated[0]
	set, _ := u["$set"].(map[string]any)
	if set["queueSize"] != 445 {
		t.Errorf("$set.queueSize: %#v", set["queueSize"])
	}
	if set["error"] != "Error: something bad" {
		t.Errorf("$set.error: %#v", set["error"])
	}
	if set["stack"] != "/tmp/stack/x" {
		t.Errorf("$set.stack: %#v", set["stack"])
	}
	inc, _ := u["$inc"].(map[string]any)
	if inc["attempts"] != 1 {
		t.Errorf("$inc.attempts: %#v", inc["attempts"])
	}
	push, _ := u["$push"].(map[string]any)
	hist, _ := push["history"].(map[string]any)
	if hist["$position"] != 0 || hist["$slice"] != 10 {
		t.Errorf("$push.history: %#v", hist)
	}
	each, _ := hist["$each"].([]any)
	if len(each) != 1 {
		t.Fatalf("$each len: %d", len(each))
	}
	// returned doc is normalized (no OError present here)
	if got["project_id"] != "project-id-123" {
		t.Errorf("got doc: %#v", got)
	}
}

// --- vendor test case 2: clearError ------------------------------------------

func TestB12_ClearErrorVendorOracle(t *testing.T) {
	fs := &fakeStore{}
	if _, err := ClearError(b12ctx, &Deps{Store: fs}, "project-id-123"); err != nil {
		t.Fatalf("ClearError: %v", err)
	}
	if len(fs.deleted) != 1 || fs.deleted[0]["project_id"] != "project-id-123" {
		t.Fatalf("deleted: %#v", fs.deleted)
	}
}

// --- quirk pins ---------------------------------------------------------------

func TestB12_RecordNoValueOError(t *testing.T) {
	fs := &fakeStore{fouFound: nil} // Q4
	_, err := Record(b12ctx, &Deps{Store: fs}, "p1", 3, "e", "s")
	if err == nil {
		t.Fatal("want no-value error")
	}
	if err.Error() != "no value returned when recording an error" {
		t.Fatalf("err: %q", err.Error())
	}
	et, ok := err.(interface{ Props() any })
	if !ok {
		t.Fatal("want OError props")
	}
	props := et.Props().(map[string]any)
	if props["projectId"] != "p1" {
		t.Errorf("props: %#v", props)
	}
}

func TestB12_NormalizeOError(t *testing.T) {
	doc := map[string]any{"error": "OError: something", "attempts": 2}
	out, err := GetFailureRecord(b12ctx, &Deps{Store: &fakeStore{found: doc}}, "p")
	if err != nil {
		t.Fatal(err)
	}
	if out["error"] != "Error: something" {
		t.Errorf("normalized: %#v", out)
	}
	// no 'error' field → untouched
	doc2 := map[string]any{"resyncStartedAt": 1}
	out2, _ := GetFailureRecord(b12ctx, &Deps{Store: &fakeStore{found: doc2}}, "p")
	if _, has := out2["error"]; has {
		t.Error("must not invent error key")
	}
	// none → nil
	out3, _ := GetFailureRecord(b12ctx, &Deps{Store: &fakeStore{found: nil}}, "p")
	if out3 != nil {
		t.Errorf("want nil, got %#v", out3)
	}
}

func TestB12_SetForceDebugCoercion(t *testing.T) {
	fs := &fakeStore{}
	if err := SetForceDebug(b12ctx, &Deps{Store: fs}, "p", nil); err != nil {
		t.Fatal(err)
	}
	set, _ := fs.updated[0]["$set"].(map[string]any)
	if set["forceDebug"] != true {
		t.Errorf("nil-state coercion: %#v", set["forceDebug"])
	}
	fs2 := &fakeStore{}
	f := false
	if err := SetForceDebug(b12ctx, &Deps{Store: fs2}, "p", &f); err != nil {
		t.Fatal(err)
	}
	set2, _ := fs2.updated[0]["$set"].(map[string]any)
	if set2["forceDebug"] != false {
		t.Errorf("explicit false must stay false: %#v", set2["forceDebug"])
	}
}

func TestB12_RecordSyncStartPayload(t *testing.T) {
	fs := &fakeStore{}
	if err := RecordSyncStart(b12ctx, &Deps{Store: fs, Clock: fakeClock{}}, "p"); err != nil {
		t.Fatal(err)
	}
	u := fs.updated[0]
	if _, has := u["$currentDate"]; !has {
		t.Error("want $currentDate")
	}
	inc, _ := u["$inc"].(map[string]any)
	if inc["resyncAttempts"] != 1 {
		t.Errorf("$inc: %#v", inc)
	}
	_ = time.Now()
}

func TestB12_CloneFailure(t *testing.T) {
	fs := &fakeStore{found: map[string]any{"error": "e", "_id": "x", "project_id": "src", "attempts": 5}}
	if err := CloneFailure(b12ctx, &Deps{Store: fs}, "src", "dst"); err != nil {
		t.Fatal(err)
	}
	if len(fs.inserted) != 1 || fs.inserted[0]["project_id"] != "dst" || fs.inserted[0]["attempts"] != 5 {
		t.Fatalf("inserted: %#v", fs.inserted)
	}
	// early-return when no source doc
	fs2 := &fakeStore{found: nil}
	if err := CloneFailure(b12ctx, &Deps{Store: fs2}, "src", "dst"); err != nil {
		t.Fatal(err)
	}
	if len(fs2.inserted) != 0 {
		t.Error("must not insert when source absent")
	}
}

func TestB12_ByTypeQuirks(t *testing.T) {
	// two docs of the same type + one resync (no 'error' field)
	fs := &fakeStore{all: []map[string]any{
		{"error": "Error: no project found", "attempts": 3, "requestCount": 2, "queueSize": 9},
		{"error": "Error: no project found", "attempts": 4, "requestCount": 1, "queueSize": 5},
		{"resyncStartedAt": "t", "queueSize": 2}, // resync, attempts default 1
	}}
	c, a, r, q, err := GetFailuresByType(b12ctx, &Deps{Store: fs})
	if err != nil {
		t.Fatal(err)
	}
	if c["Error: no project found"] != 2 || a["Error: no project found"] != 7 || r["Error: no project found"] != 3 || q["Error: no project found"] != 9 {
		t.Errorf("no-project: c=%v a=%v r=%v q=%v", c, a, r, q)
	}
	if c["resync"] != 1 || a["resync"] != 1 || r["resync"] != 0 || q["resync"] != 2 {
		t.Errorf("resync: c=%v a=%v r=%v q=%v", c["resync"], a["resync"], r["resync"], q["resync"])
	}
}

func TestB12_GetFailuresFullCategory(t *testing.T) {
	fs := &fakeStore{all: []map[string]any{
		{"error": "Error: no project found", "attempts": 1},
		{"error": "some totally unknown failure"}, // Q3: no category key
		{"resyncStartedAt": "x"},                  // no category key
	}}
	got, err := GetFailuresFull(b12ctx, &Deps{Store: fs})
	if err != nil {
		t.Fatal(err)
	}
	if got[0]["category"] != "no-project-found" {
		t.Errorf("known: %#v", got[0])
	}
	if _, has := got[1]["category"]; has {
		t.Errorf("unknown error must omit category: %#v", got[1])
	}
	if _, has := got[2]["category"]; has {
		t.Errorf("resync must omit category: %#v", got[2])
	}
}

func TestB12_GetFailuresZeroInitAndFallback(t *testing.T) {
	fs := &fakeStore{all: []map[string]any{
		{"error": "totally-unknown-type", "attempts": 2, "queueSize": 7},
		{"error": "Error: Timeout", "attempts": 1, "queueSize": 3},
	}}
	g := &gaugeSink{vals: map[string]float64{}}
	counts, attempts, requests, mq, err := GetFailures(b12ctx, &Deps{Store: fs, Metrics: g})
	if err != nil {
		t.Fatal(err)
	}
	// every known label zero-initialized (so gauges reset)
	if counts["ENOSPC"] != 0 || counts["text-op-error"] != 0 {
		t.Errorf("zero-init missing: %#v", counts)
	}
	// 'other' fallback accumulates the unknown type
	if counts["other"] != 1 {
		t.Errorf("other: %#v", counts)
	}
	// lock-overrun merges Error: Timeout
	if counts["lock-overrun"] != 1 || mq["lock-overrun"] != 3 {
		t.Errorf("lock-overrun: c=%v q=%v", counts["lock-overrun"], mq["lock-overrun"])
	}
	// attempts/requests of other fallthrough
	if attempts["other"] != 2 || requests["other"] != 0 {
		t.Errorf("other attempts=%v requests=%v", attempts["other"], requests["other"])
	}
	// gauge families emitted: 4 families × (distinct labels) calls, each label seen exactly once per family
	fam := map[string]map[string]bool{}
	for _, c := range g.calls {
		name, label := splitSlash(c)
		fam[name] = fam[name]
		if fam[name] == nil {
			fam[name] = map[string]bool{}
		}
		fam[name][label] = true
	}
	for _, wantName := range []string{"failed", "attempts", "requests", "max-queue-size"} {
		if fam[wantName]["other"] == false || fam[wantName]["lock-overrun"] == false {
			t.Errorf("gauge family %q missing labels: %#v", wantName, fam[wantName])
		}
	}
}

func splitSlash(s string) (name, label string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}
