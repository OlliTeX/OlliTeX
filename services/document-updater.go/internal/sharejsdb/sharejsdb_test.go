package sharejsdb

import (
	"errors"
	"reflect"
	"testing"

	"document-updater/internal/errorsx"
	"document-updater/internal/sharejsmodel"
)

// fakeFetcher mirrors RedisManager.getPreviousDocOps: an INCLUSIVE [start,
// end] range over an in-memory op list, end == -1 means "to the end".
type fakeFetcher struct {
	errToRaise error
	inrange    []any
	calls      []fcall
}

type fcall struct {
	docID      string
	start, end int
}

func (f *fakeFetcher) fetch(docID string, start, end int) ([]any, error) {
	f.calls = append(f.calls, fcall{docID, start, end})
	if f.errToRaise != nil {
		return nil, f.errToRaise
	}
	n := len(f.inrange)
	e := end
	if e == -1 {
		e = n - 1
	}
	if start > e || n == 0 || start < 0 {
		return []any{}, nil
	}
	return f.inrange[start : e+1], nil
}

// opData mirrors a raw opData object in Redis ({op, meta, v}).
func rawOpData(i int, source string) any {
	return map[string]any{
		"op":   []any{map[string]any{"p": i, "i": "x"}},
		"v":    i,
		"meta": map[string]any{"source": source},
	}
}

// --- getSnapshot (vendor: ShareJsDBTests getSnapshot) -------------------------

func TestGetSnapshotSuccess(t *testing.T) {
	lines := []string{"one", "two", "three"}
	db := New("project-id", "document-id", lines, 42)
	want := "project-id:document-id"
	_ = want

	snap, _, err := db.GetSnapshot("project-id:document-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.Snapshot != "one\ntwo\nthree" {
		t.Fatalf("snapshot: %q", snap.Snapshot)
	}
	if snap.V != 42 {
		t.Fatalf("v: %d (want 42)", snap.V)
	}
	if snap.Type != "text" {
		t.Fatalf("type: %q (want \"text\")", snap.Type)
	}
}

func TestGetSnapshotKeyMismatch(t *testing.T) {
	db := New("project-id", "document-id", []string{"one"}, 42)
	_, _, err := db.GetSnapshot("bad:key")
	var nfe *errorsx.NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected *NotFoundError, got %v (type %T)", err, err)
	}
	if nfe.Message != "unexpected doc_key bad:key, expected project-id:document-id" {
		t.Fatalf("message: %q", nfe.Message)
	}
}

// --- getOps (vendor: ShareJsDBTests getOps) -----------------------------------

func TestGetOpsStartEqEnd(t *testing.T) {
	f := &fakeFetcher{}
	db := New("project-id", "document-id", nil, 42)
	db.GetPreviousDocOps = f.fetch
	ops, err := db.GetOps("project-id:document-id", 42, 42, false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(ops) != 0 {
		t.Fatalf("ops: %v", ops)
	}
	if len(f.calls) != 0 {
		t.Fatalf("fetcher called: %v", f.calls)
	}
}

func TestGetOpsStartVersionEndUnset(t *testing.T) {
	f := &fakeFetcher{}
	db := New("project-id", "document-id", nil, 42)
	db.GetPreviousDocOps = f.fetch
	ops, err := db.GetOps("project-id:document-id", 42, 0, true)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(ops) != 0 {
		t.Fatalf("ops: %v", ops)
	}
	if len(f.calls) != 0 {
		t.Fatalf("fetcher called: %v", f.calls)
	}
}

func TestGetOpsNonEmptyRange(t *testing.T) {
	f := &fakeFetcher{}
	for i := 0; i < 42; i++ {
		f.inrange = append(f.inrange, rawOpData(i, "src"))
	}
	db := New("project-id", "document-id", nil, 42)
	db.GetPreviousDocOps = f.fetch
	// Vendor: fetch(docId, 35, 41) (the end-- happens before the Redis call)
	ops, err := db.GetOps("project-id:document-id", 35, 42, false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(ops) != 7 {
		t.Fatalf("ops len: %d (want 7)", len(ops))
	}
	if len(f.calls) != 1 || f.calls[0] != (fcall{"document-id", 35, 41}) {
		t.Fatalf("fetched: %+v", f.calls)
	}
	// Wire opData round-trips into StoredOp fields.
	if ops[0].V != 35 || opts0Meta(ops[0]) != "src" {
		t.Fatalf("stored op: %+v", ops[0])
	}
}

func opts0Meta(op sharejsmodel.StoredOp) string {
	if op.Meta != nil {
		return op.Meta["source"].(string)
	}
	return ""
}

func TestGetOpsNoEnd(t *testing.T) {
	f := &fakeFetcher{}
	for i := 0; i < 5; i++ {
		f.inrange = append(f.inrange, rawOpData(i, "src"))
	}
	db := New("project-id", "document-id", nil, 42)
	db.GetPreviousDocOps = f.fetch
	// Vendor: "get until the end of the list" -> fetch(..., -1)
	ops, err := db.GetOps("project-id:document-id", 3, 0, true)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(ops) != 2 {
		t.Fatalf("ops: %v", ops)
	}
	if len(f.calls) != 1 || f.calls[0] != (fcall{"document-id", 3, -1}) {
		t.Fatalf("fetched: %+v", f.calls)
	}
}

func TestGetOpsFetcherErrorPropagates(t *testing.T) {
	f := &fakeFetcher{errToRaise: errorsx.OpRangeNotAvailable()}
	db := New("project-id", "document-id", nil, 42)
	db.GetPreviousDocOps = f.fetch
	_, err := db.GetOps("project-id:document-id", 0, 10, false)
	var orna *errorsx.OpRangeNotAvailableError
	if !errors.As(err, &orna) {
		t.Fatalf("want *OpRangeNotAvailableError, got %v", err)
	}
}

// --- writeOp (vendor: ShareJsDBTests writeOps) --------------------------------

func TestWriteOpBuffers(t *testing.T) {
	db := New("project-id", "document-id", nil, 42)
	op := sharejsmodel.StoredOp{
		Op:   []any{map[string]any{"p": 20, "t": "foo"}},
		V:    42,
		Meta: map[string]any{"source": "bar"},
	}
	if err := db.WriteOp("project-id:document-id", op); err != nil {
		t.Fatalf("err: %v", err)
	}
	got := db.AppliedOps["project-id:document-id"]
	if len(got) != 1 || !opDataEqual(got[0], op) {
		t.Fatalf("appliedOps: %v", got)
	}
	// Writing to a different docKey buffers under that key.
	if err := db.WriteOp("other:key", op); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(db.AppliedOps["other:key"]) != 1 {
		t.Fatalf("appliedOps: %v", db.AppliedOps["other:key"])
	}
	// Vendor: delete is a no-op.
	if err := db.Delete("project-id:document-id"); err != nil {
		t.Fatalf("delete must be no-op: %v", err)
	}
	db.Close()
}

func opDataEqual(got any, want sharejsmodel.StoredOp) bool {
	m, ok := got.(map[string]any)
	if !ok {
		return false
	}
	if !reflect.DeepEqual(m["op"], want.Op) || m["v"] != want.V {
		return false
	}
	if want.Meta != nil {
		if mm, ok := m["meta"].(map[string]any); !ok || len(mm) != 1 || mm["source"] != want.Meta["source"] {
			return false
		}
	}
	return true
}
