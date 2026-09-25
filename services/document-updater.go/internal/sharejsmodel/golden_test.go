package sharejsmodel

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

// mockDb mirrors the /tmp/modgen oracle mock: honest op commit on writeOp,
// no-op delete (the vendor mock only records delete, it does not remove the
// stored snapshot/ops -- a later load catch-up replays the committed ops).
type mockDb struct {
	snaps   map[string]DocSnapshot
	ops     map[string][]StoredOp
	create  []string
	deleted []string
	getSnap int
	gops    int
	gop     []map[string]any
	writeOp []int
	writeSn []int
}

func mkMock() *mockDb {
	return &mockDb{
		snaps: map[string]DocSnapshot{}, ops: map[string][]StoredOp{},
		create: []string{}, deleted: []string{}, gop: []map[string]any{},
		writeOp: []int{}, writeSn: []int{},
	}
}

func (m *mockDb) seed(key string, v int, snap string, seed []any) {
	m.snaps[key] = DocSnapshot{Snapshot: snap, V: v, Type: "text"}
	for i, o := range seed {
		m.ops[key] = append(m.ops[key], StoredOp{Op: o, V: i})
	}
}

func (m *mockDb) GetSnapshot(name string) (DocSnapshot, any, error) {
	m.getSnap++
	if d, ok := m.snaps[name]; ok {
		return d, undefinedMeta{}, nil
	}
	return DocSnapshot{}, nil, errors.New("not-in-db")
}

func (m *mockDb) GetOps(name string, start, end int, endNull bool) ([]StoredOp, error) {
	m.gops++
	if endNull {
		m.gop = append(m.gop, map[string]any{"key": name, "start": start, "end": nil})
	} else {
		m.gop = append(m.gop, map[string]any{"key": name, "start": start, "end": end})
	}
	ops := m.ops[name]
	e := end
	if endNull {
		e = len(ops)
	}
	out := []StoredOp{}
	for _, o := range ops {
		if o.V >= start && o.V < e {
			out = append(out, o)
		}
	}
	return out, nil
}

func (m *mockDb) Create(name string, data DocSnapshot) error {
	m.create = append(m.create, name)
	m.snaps[name] = data
	m.ops[name] = []StoredOp{}
	return nil
}

func (m *mockDb) WriteOp(name string, op StoredOp) error {
	m.writeOp = append(m.writeOp, op.V)
	m.ops[name] = append(m.ops[name], StoredOp{Op: op.Op, V: op.V, Meta: metaDup(op.Meta)})
	return nil
}

func (m *mockDb) WriteSnapshot(name string, d DocSnapshot) error {
	m.writeSn = append(m.writeSn, d.V)
	m.snaps[name] = d
	return nil
}

func (m *mockDb) Delete(name string) error {
	m.deleted = append(m.deleted, name)
	return nil
}

func (m *mockDb) Close() {}

type undefinedMeta struct{}

func comp(p int, k, s string) any {
	m := map[string]any{"p": p, k: s}
	return []any{m}
}

func vp(n int) *int { return &n }

// harness records the log exactly as /tmp/modgen/gen.js. Model-level emits
// (via EmitFn) and doc-op emits (via the doc listener) append immediately
// when they fire; each step's c:... callback row appends at the callback
// moment (after the method's emits), so the row order matches the oracle.
type harness struct {
	m       *Model
	db      *mockDb
	out     []map[string]any
	refs    map[int]Listener
	next    int
	lastRef Listener
}

func newHarness(db *mockDb, opts Options) *harness {
	var d Db
	if db != nil {
		d = db
	}
	m := New(d, map[string]Type{"text": TextWireType{}}, opts)
	h := &harness{m: m, db: db, refs: map[int]Listener{}, next: 1}
	m.EmitFn(func(e Event) {
		switch e.Kind {
		case "load":
			h.out = append(h.out, map[string]any{"ev": "model-load", "name": e.DocName, "v": e.DocV, "snap": e.Snapshot})
		case "add":
			h.out = append(h.out, map[string]any{"ev": "model-add", "name": e.DocName, "v": e.DocV, "snap": e.Snapshot})
		case "create":
			h.out = append(h.out, map[string]any{"ev": "model-create", "name": e.DocName, "v": e.DocV, "snap": e.Snapshot, "type": e.TypeName})
		case "delete":
			h.out = append(h.out, map[string]any{"ev": "model-delete", "name": e.DocName})
		case "applyOp":
			if e.MetaSrc != "" {
				h.out = append(h.out, map[string]any{"ev": "model-applyOp", "name": e.DocName, "opV": e.OpV, "snap": e.Snap, "old": e.OldSnap, "metaSrc": e.MetaSrc})
			} else {
				h.out = append(h.out, map[string]any{"ev": "model-applyOp", "name": e.DocName, "opV": e.OpV, "snap": e.Snap, "old": e.OldSnap})
			}
		case "applyMetaOp":
			h.out = append(h.out, map[string]any{"ev": "model-applyMetaOp", "name": e.DocName, "path": e.Path, "value": e.Value})
		}
	})
	return h
}

func (h *harness) docListener(lid int) func(OpData) {
	return func(od OpData) {
		if od.V != nil {
			h.out = append(h.out, map[string]any{"ev": "doc-op", "l": lid, "v": *od.V})
		} else {
			h.out = append(h.out, map[string]any{"ev": "doc-op", "l": lid})
		}
	}
}

// --- step helpers: append c:... row AFTER the model call (callback moment) ----

func (h *harness) createStep(name, typ string) {
	if err := h.m.Create(name, typ, nil); err != nil {
		h.out = append(h.out, map[string]any{"c": "create", "err": err.Error()})
	} else {
		h.out = append(h.out, map[string]any{"c": "create"})
	}
}

func (h *harness) applyOpStep(name string, od OpData) {
	v, err := h.m.ApplyOp(name, od)
	if err != nil {
		h.out = append(h.out, map[string]any{"c": "applyOp", "err": err.Error()})
	} else {
		h.out = append(h.out, map[string]any{"c": "applyOp", "err": nil, "v": v})
	}
}

func (h *harness) getSnapshotStep(name string) {
	snap, err := h.m.GetSnapshot(name)
	if err != nil {
		h.out = append(h.out, map[string]any{"c": "getSnapshot", "err": err.Error()})
	} else {
		h.out = append(h.out, map[string]any{"c": "getSnapshot", "err": nil, "v": snap.V, "snap": snap.Snapshot})
	}
}

func (h *harness) getVersionStep(name string) {
	v, err := h.m.GetVersion(name)
	if err != nil {
		h.out = append(h.out, map[string]any{"c": "getVersion", "err": err.Error()})
	} else {
		h.out = append(h.out, map[string]any{"c": "getVersion", "err": nil, "v": v})
	}
}

func (h *harness) getOpsStep(name string, start int, end *int) {
	ops, err := h.m.GetOps(name, start, end)
	if err != nil {
		h.out = append(h.out, map[string]any{"c": "getOps", "err": err.Error()})
		return
	}
	rows := make([]any, 0, len(ops))
	for _, o := range ops {
		r := map[string]any{"op": o.Op, "v": o.V}
		if s, ok := o.Meta["source"].(string); ok {
			r["src"] = s
		}
		rows = append(rows, r)
	}
	h.out = append(h.out, map[string]any{"c": "getOps", "err": nil, "ops": rows})
}

func (h *harness) listenStep(name string, version *int) {
	h.lastRef, _ = h.m.Listen(name, version, h.docListener(0), func(err error, v int) {
		if err != nil {
			h.out = append(h.out, map[string]any{"c": "listen", "err": err.Error()})
		} else {
			h.out = append(h.out, map[string]any{"c": "listen", "err": nil})
		}
	})
}

func (h *harness) stopListenerStep(name string) {
	_ = h.m.RemoveListener(name, h.lastRef)
	h.out = append(h.out, map[string]any{"c": "removeListener", "doc": name})
}

func (h *harness) applyMetaOpStep(name string, meta map[string]any) {
	v, err := h.m.ApplyMetaOp(name, meta)
	if err != nil {
		h.out = append(h.out, map[string]any{"c": "applyMetaOp", "err": err.Error()})
	} else {
		h.out = append(h.out, map[string]any{"c": "applyMetaOp", "err": nil, "v": v})
	}
}

func (h *harness) deleteStep(name string) {
	if err := h.m.Delete(name); err != nil {
		h.out = append(h.out, map[string]any{"c": "delete", "err": err.Error()})
	} else {
		h.out = append(h.out, map[string]any{"c": "delete"})
	}
}

// finish mirrors the oracle trailing: db-final snapshot, then flush, closeDb.
func (h *harness) finish() {
	if h.db != nil {
		h.out = append(h.out, map[string]any{
			"t":           "db-final",
			"create":      h.db.create,
			"getSnap":     h.db.getSnap,
			"getOps":      h.db.gops,
			"getOpsCalls": h.db.gop,
			"writeOps":    h.db.writeOp,
			"writeSnaps":  h.db.writeSn,
			"delete":      h.db.deleted,
		})
	}
}

// --- scenarios (mirror the 12 oracle scenarios) ----------------------------------

func testBasic(h *harness) {
	h.applyOpStep("a:1", OpData{Op: comp(0, "i", "X"), V: vp(0), Meta: map[string]any{"source": "s1"}})
	h.getSnapshotStep("a:1")
	h.getVersionStep("a:1")
	h.getOpsStep("a:1", 0, nil)
	h.getOpsStep("a:1", 0, vp(1))
	h.getOpsStep("a:1", 5, nil)
	h.finish()
}

func testTransform(h *harness) {
	h.applyOpStep("a:1", OpData{Op: comp(3, "i", "Y"), V: vp(1)})
	h.getSnapshotStep("a:1")
	h.applyOpStep("a:1", OpData{Op: comp(0, "d", "z"), V: vp(2)})
	h.finish()
}

func testErrors(h *harness) {
	h.applyOpStep("a:1", OpData{Op: comp(0, "i", "Z"), V: vp(5)})
	h.applyOpStep("a:1", OpData{V: vp(3)})
	h.applyOpStep("a:1", OpData{Op: comp(0, "i", "Z"), V: vp(0), DupIf: []string{"s1"}})
	h.applyOpStep("a:1", OpData{Op: comp(0, "i", "Z"), V: vp(0)})
	h.getVersionStep("a:1")
	h.finish()
}

func testMaxdoc(h *harness) {
	h.applyOpStep("a:1", OpData{Op: comp(0, "i", "defg"), V: vp(1)})
	h.getSnapshotStep("a:1")
	h.applyOpStep("a:1", OpData{Op: comp(0, "i", "Z"), V: vp(1)})
	h.getSnapshotStep("a:1")
	h.finish()
}

func testOpsCache(h *harness) {
	h.applyOpStep("a:1", OpData{Op: comp(0, "i", "c"), V: vp(1)})
	h.applyOpStep("a:1", OpData{Op: comp(0, "i", "d"), V: vp(2)})
	h.getOpsStep("a:1", 0, nil)
	h.getOpsStep("a:1", 1, nil)
	h.getOpsStep("a:1", 3, nil)
	h.finish()
}

func testListen(h *harness) {
	h.listenStep("a:1", vp(0))
	h.applyOpStep("a:1", OpData{Op: comp(0, "i", "e"), V: vp(2)})
	h.stopListenerStep("a:1")
	h.applyOpStep("a:1", OpData{Op: comp(0, "i", "f"), V: vp(3)})
	h.finish()
}

func testCoalesce(h *harness) {
	h.getVersionStep("a:1")
	h.getVersionStep("a:1")
	h.getVersionStep("a:1")
	h.getSnapshotStep("a:1")
	h.getSnapshotStep("b:2")
	h.finish()
}

func testSnaps(h *harness) {
	h.applyOpStep("a:1", OpData{Op: comp(0, "i", "b"), V: vp(0)})
	h.applyOpStep("a:1", OpData{Op: comp(0, "i", "c"), V: vp(1)})
	h.getSnapshotStep("a:1")
	h.applyOpStep("a:1", OpData{Op: comp(0, "i", "d"), V: vp(2)})
	h.getSnapshotStep("a:1")
	h.applyOpStep("a:1", OpData{Op: comp(0, "i", "e"), V: vp(3)})
	h.finish()
}

func testMetaop(h *harness) {
	h.listenStep("a:1", vp(0))
	h.applyMetaOpStep("a:1", map[string]any{"path": []any{"shout"}, "value": "v"})
	h.applyMetaOpStep("a:1", map[string]any{"path": "nope", "value": "v"})
	h.applyMetaOpStep("b:2", map[string]any{"path": []any{"shout"}, "value": "v"})
	h.finish()
}

func testCreateDelete(h *harness) {
	h.createStep("c:1", "text")
	h.getVersionStep("c:1")
	h.createStep("c/1", "text")
	h.createStep("c:1", "text")
	h.createStep("c:2", "no-such-type")
	h.applyOpStep("c:1", OpData{Op: comp(0, "i", "Q"), V: vp(0)})
	h.getSnapshotStep("c:1")
	h.deleteStep("c:1")
	h.getSnapshotStep("c:1")
	h.deleteStep("zz:0")
	h.finish()
}

func testNodeb(h *harness) {
	h.createStep("n:1", "text")
	h.getVersionStep("n:1")
	h.applyOpStep("n:1", OpData{Op: comp(0, "i", "Q"), V: vp(0)})
	h.getSnapshotStep("n:1")
	h.getOpsStep("no:1", 0, nil)
	h.getVersionStep("no:1")
	h.deleteStep("no:1")
	h.finish()
}

func testCloseDb(h *harness) {
	h.applyOpStep("a:1", OpData{Op: comp(0, "i", "b"), V: vp(0)})
	h.m.CloseDb()
	h.finish()
}

// --- oracle comparison --------------------------------------------------------------

// canonV converts ANY value to a canonical tree of map[string]any + []any +
// primitives (int/float64/string/bool/nil), so reflect.DeepEqual only depends
// on values, never on Go slice/element types (GOT uses []map[string]any,
// []int, []string, map keys order etc).
func canonV(v any) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		out := map[string]any{}
		iter := rv.MapRange()
		for iter.Next() {
			k := iter.Key().Interface()
			ks, _ := k.(string)
			out[ks] = canonV(canonVal(iter.Value().Interface()))
		}
		return out
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out[i] = canonV(canonVal(rv.Index(i).Interface()))
		}
		return out
	default:
		if f, ok := v.(float64); ok {
			return int(f)
		}
		return v
	}
}

func canonVal(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return canonV(t)
	default:
		return v
	}
}

func canonRows(rows []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, canonV(r).(map[string]any))
	}
	return out
}

func TestGoldenModel(t *testing.T) {
	var golden map[string][]map[string]any
	if err := json.Unmarshal([]byte(goldenJSON), &golden); err != nil {
		t.Fatalf("parse golden: %v", err)
	}

	run := func(name string, build func(*harness), db *mockDb, opts Options) {
		t.Helper()
		h := newHarness(db, opts)
		build(h)
		want, ok := golden[name]
		if !ok {
			t.Fatalf("no golden scenario %s", name)
		}
		got := canonRows(h.out)
		if !reflect.DeepEqual(got, canonRows(want)) {
			got, _ := json.MarshalIndent(h.out, "", "  ")
			wantJSON, _ := json.MarshalIndent(want, "", "  ")
			t.Errorf("scenario %s mismatch\nGOT:\n%s\nWANT:\n%s", name, got, wantJSON)
		}
	}

	run("basic", testBasic,
		func() *mockDb { m := mkMock(); m.seed("a:1", 0, "a", nil); return m }(), Options{})
	run("transform", testTransform,
		func() *mockDb { m := mkMock(); m.seed("a:1", 1, "a", []any{comp(0, "i", "X")}); return m }(), Options{})
	run("errors", testErrors,
		func() *mockDb {
			m := mkMock()
			m.seed("a:1", 3, "ab", []any{comp(0, "i", "c"), comp(0, "i", "d"), comp(0, "i", "e")})
			m.ops["a:1"][1].Meta = map[string]any{"source": "s1"}
			return m
		}(), Options{MaximumAge: 3})
	run("maxdoc", testMaxdoc,
		func() *mockDb { m := mkMock(); m.seed("a:1", 1, "abc", []any{comp(0, "i", "c")}); return m }(), Options{MaxDocLength: 4})
	run("opscache", testOpsCache,
		func() *mockDb { m := mkMock(); m.seed("a:1", 1, "b", []any{comp(0, "i", "b")}); return m }(), Options{NumCachedOps: 2})
	run("listen", testListen,
		func() *mockDb {
			m := mkMock()
			m.seed("a:1", 2, "cd", []any{comp(0, "i", "c"), comp(0, "i", "d")})
			return m
		}(), Options{})
	run("coalesce", testCoalesce,
		func() *mockDb { m := mkMock(); m.seed("a:1", 0, "a", nil); return m }(), Options{})
	run("snaps", testSnaps,
		func() *mockDb { m := mkMock(); m.seed("a:1", 0, "a", nil); return m }(), Options{OpsBeforeCommit: 2})
	run("metaop", testMetaop,
		func() *mockDb { m := mkMock(); m.seed("a:1", 0, "a", nil); return m }(), Options{})
	run("createdelete", testCreateDelete, mkMock(), Options{})
	run("nodeb", testNodeb, nil, Options{})
	run("cloisedb", testCloseDb,
		func() *mockDb { m := mkMock(); m.seed("a:1", 0, "a", nil); return m }(), Options{OpsBeforeCommit: 5})
}
