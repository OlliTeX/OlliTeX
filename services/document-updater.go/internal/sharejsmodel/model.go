// Package sharejsmodel is a Go port of the vendored ShareJS server model
// (app/js/sharejs/server/model.js, ~895 LOC): the "model of all the ops". It
// manages database interaction, keeps an in-memory cache of live documents,
// and drives the op transform/apply pipeline through a per-document
// syncqueue. It is the Go drop-in the Phase-6 ShareJsDB + ShareJsUpdateManager
// port will consume.
//
// Wire semantics. The vendor model is type-agnostic: it passes WIRE ops
// ([]any component maps, or the absent op) through type.apply / type.transform
// and stores the WIRE op it receives. The Go port drives a wire-native Type
// face (below) instead of the typed sharejstypes adapters. The "text" wire
// adapter (TextWireType) wraps the oracle-verified sharejstext port.
//
// Vendored divergence (shared EventEmitter). model.js sets
//
//	Model.prototype = new EventEmitter()
//
// so EVERY model instance shares one emitter (a known vendored bug). The
// oracle harness resets that shared emitter per scenario (removeAllListeners).
// The Go port records model-level events per-instance (Model.EmitFn) and
// per-doc "op" events per doc (doc.listeners); per-scenario observable
// behavior (the golden log) is identical.
//
// Async divergence (documented). The vendor wraps DB callbacks in
// process.nextTick and drives ops through a syncqueue processor invoked via
// process.nextTick. The Go port is synchronous: syncqueue still FIFO
// serialises per-doc applyOp (the vendored "ops are applied in order"
// contract), and the DB is driven by value-returning methods. Observable
// behavior (ordering, versioning, caching, side-effect counters) is preserved
// and oracle-pinned by golden_test.go.
package sharejsmodel

import (
	"errors"
	"fmt"

	"document-updater/internal/syncqueue"
)

// --- vendored option defaults ------------------------------------------------

const (
	defaultReapTime        = 3000
	defaultNumCachedOps    = 10
	defaultOpsBeforeCommit = 20
	defaultMaximumAge      = 40
)

// --- wire-native type face ----------------------------------------------------

// Type is the wire-level type face the model drives (create/apply/transform
// over wire snapshots and wire ops), mirroring the vendor
// type.{create,apply,transform} used inside makeOpQueue.
type Type interface {
	Name() string
	Create() any
	Apply(snapshot any, op any) (any, error)
	Transform(op1 any, op2 any, side string) (any, error)
}

// --- options -------------------------------------------------------------------

// Options mirrors the vendored model options. A zero field means "use the
// vendored default". MaxDocLength == 0 disables the length check (vendor:
// options.maxDocLength != null). ReapTime / ForceReaping are retained for wire
// parity but are not timer-modelled (see divergence notes above).
type Options struct {
	ReapTime        int  // vendored 3000 (retained, no timer)
	NumCachedOps    int  // vendored 10
	ForceReaping    bool // vendored false
	OpsBeforeCommit int  // vendored 20
	MaximumAge      int  // vendored 40
	MaxDocLength    int  // 0 = disabled
}

func (o Options) numCachedOps() int {
	if o.NumCachedOps == 0 {
		return defaultNumCachedOps
	}
	return o.NumCachedOps
}

func (o Options) opsBeforeCommit() int {
	if o.OpsBeforeCommit == 0 {
		return defaultOpsBeforeCommit
	}
	return o.OpsBeforeCommit
}

func (o Options) maximumAge() int {
	if o.MaximumAge == 0 {
		return defaultMaximumAge
	}
	return o.MaximumAge
}

// --- wire op / snapshot values ------------------------------------------------

// StoredOp is a wire op as cached / stored in the DB (vendor {op, v, meta}).
type StoredOp struct {
	Op   any
	V    int
	Meta map[string]any
}

// OpData is the op submitted to ApplyOp (vendor opData = {op, v, meta,
// dupIfSource}). V is nil when the "Version missing" check should fire.
type OpData struct {
	Op    any            // wire []any, or nil (undefined op -> "not iterable")
	V     *int           // nil => "Version missing"
	Meta  map[string]any // wire meta (may be nil; the model stamps "ts")
	DupIf []string       // dupIfSource (optional)
}

// DocSnapshot is what the model hands to / reads from the DB (the vendor
// getSnapshot "data" + type name).
type DocSnapshot struct {
	Snapshot any
	V        int
	Type     string
	Meta     map[string]any
}

// --- DB interface (wire; mirrors the vendor db wrapper) -----------------------

// Db mirrors the vendor sharejs db the model talks to. endNull in GetOps means
// "all ops from start" (the vendor end=null); end is ignored when endNull.
type Db interface {
	GetSnapshot(name string) (DocSnapshot, any, error)
	GetOps(name string, start, end int, endNull bool) ([]StoredOp, error)
	Create(name string, data DocSnapshot) error
	WriteOp(name string, op StoredOp) error
	WriteSnapshot(name string, data DocSnapshot) error
	Delete(name string) error
	Close()
}

// --- events -------------------------------------------------------------------

// Event is a model-level emission (create/add/load/delete/applyOp/applyMetaOp).
// Fields are meaningful per Kind; the harness picks the relevant ones.
type Event struct {
	Kind     string // create|add|load|delete|applyOp|applyMetaOp
	DocName  string
	DocV     int
	Snapshot any
	TypeName string
	OpV      int
	Snap     any
	OldSnap  any
	MetaSrc  string
	// applyOp events also carry the applied wire op + (meta after the .ts
	// stamp) so consumers (ShareJsUpdateManager._listenForOps) can forward
	// the vendored opData ({op, v, meta}) to RealTimeRedisManager.
	Op    any
	Meta  map[string]any
	Path  []any
	Value any
}

// --- listeners -----------------------------------------------------------------

// Listener is the opaque handle returned by Listen; pass it to
// RemoveListener. (Go functions are not comparable by identity except to
// nil, so the vendor's func-identity listener list is modelled as a handle
// list; per-scenario observable behavior is identical.)
type Listener struct{ seq int }

// --- Model ----------------------------------------------------------------------

// Model is the Go analogue of one vendored Model instance.
type Model struct {
	db       Db
	registry map[string]Type
	opts     Options
	docs     map[string]*doc
	emit     func(Event)
	lsSeq    int
}

// doc is the cache entry for one document (mirror of the vendor cache shape).
type doc struct {
	snapshot   any
	v          int
	typeName   string
	meta       map[string]any
	ops        []StoredOp
	listeners  []listenerEnt
	committedV int
	snapLock   bool
	dbMeta     any
	opQueue    *syncqueue.Queue
}

type listenerEnt struct {
	ref Listener
	fn  func(OpData)
}

// New builds a model over db (nil = memory-only, "Document does not exist")
// and the given wire-type registry (e.g. map[string]Type{"text": TextWireType{}}).
func New(db Db, registry map[string]Type, opts Options) *Model {
	return &Model{
		db:       db,
		registry: registry,
		opts:     opts,
		docs:     map[string]*doc{},
	}
}

// EmitFn sets the model-level event sink (mirrors model.on(...) in the
// oracle harness). It must be set before any activity.
func (m *Model) EmitFn(fn func(Event)) { m.emit = fn }

func (m *Model) emitEvent(e Event) {
	if m.emit != nil {
		m.emit(e)
	}
}

// --- load / add (vendored) -------------------------------------------------------

// load mirrors the vendor recursive load: a synchronous single-flight fetch
// into the cache. Because Go is synchronous there is no process.nextTick
// coalescing; the observable behavior (cache hit/miss, the catch-up apply) is
// preserved.
func (m *Model) load(name string) (*doc, error) {
	if d := m.docs[name]; d != nil {
		return d, nil
	}
	if m.db == nil {
		return nil, errors.New("Document does not exist")
	}
	data, dbMeta, err := m.db.GetSnapshot(name)
	if err != nil {
		return nil, err
	}
	t, ok := m.registry[data.Type]
	if !ok {
		return nil, errors.New("Type not found")
	}
	committedV := data.V
	ops, err := m.getOpsInternal(name, data.V, 0, true)
	if err != nil {
		return nil, err
	}
	snap := data.Snapshot
	v := data.V
	for _, op := range ops {
		ns, aerr := t.Apply(snap, op.Op)
		if aerr != nil {
			return nil, errors.New("Op data invalid")
		}
		snap = ns
		v++
	}
	m.emitEvent(Event{Kind: "load", DocName: name, DocV: v, Snapshot: snap, TypeName: data.Type})
	return m.add(name, data, v, ops, snap, committedV, dbMeta)
}

// add inserts a fresh doc into the cache and emits "add".
func (m *Model) add(name string, data DocSnapshot, v int, ops []StoredOp, snapshot any, committedV int, dbMeta any) (*doc, error) {
	d := &doc{
		snapshot:   snapshot,
		v:          v,
		typeName:   data.Type,
		meta:       data.Meta,
		ops:        ops,
		committedV: committedV,
		dbMeta:     dbMeta,
	}
	d.opQueue = syncqueue.New(m.makeOpQueueProcess(name, d))
	m.docs[name] = d
	m.emitEvent(Event{Kind: "add", DocName: name, DocV: v, Snapshot: snapshot})
	return d, nil
}

// getOpsInternal mirrors the vendor helper: db.getOps + sequential v stamping.
func (m *Model) getOpsInternal(name string, start, end int, endNull bool) ([]StoredOp, error) {
	if m.db == nil {
		return nil, errors.New("Document does not exist")
	}
	ops, err := m.db.GetOps(name, start, end, endNull)
	if err != nil {
		return nil, err
	}
	v := start
	for i := range ops {
		ops[i].V = v
		v++
	}
	return ops, nil
}

// --- getOps (vendored) --------------------------------------------------------

// getOpsRaw returns ops in [start,end). endNull means "start..latest".
// start < 0 -> "start must be 0+". When the doc is cached it uses the cache
// when the range is covered (else the DB); the vendored `end=null` resolution
// happens before the DB call (and end then refers to the resolved version).
func (m *Model) getOpsRaw(name string, start, end int, endNull bool) ([]StoredOp, error) {
	if start < 0 {
		return nil, errors.New("start must be 0+")
	}
	if d, ok := m.docs[name]; ok {
		end2 := end
		if endNull {
			end2 = d.v
		}
		start = min(start, end2)
		if start == end2 {
			return []StoredOp{}, nil
		}
		base := d.v - len(d.ops)
		if start >= base || m.db == nil {
			lo, hi := start-base, end2-base
			out := []StoredOp{}
			for i, op := range d.ops {
				if i >= lo && i < hi {
					out = append(out, op)
				}
			}
			return out, nil
		}
		return m.getOpsInternal(name, start, end2, false)
	}
	// Not cached: the vendor passes the original end/endNull straight to the DB.
	return m.getOpsInternal(name, start, end, endNull)
}

// --- tryWriteSnapshot (vendored) ----------------------------------------------

func (m *Model) tryWriteSnapshot(name string) {
	if m.db == nil {
		return
	}
	if d := m.docs[name]; d != nil && d.committedV != d.v && !d.snapLock {
		d.snapLock = true
		m.db.WriteSnapshot(name, DocSnapshot{Snapshot: d.snapshot, V: d.v, Type: d.typeName, Meta: d.meta})
		d.snapLock = false
		d.committedV = d.v
	}
}

// --- makeOpQueue process (vendored) ---------------------------------------------

// opQueue mirrors the vendored makeOpQueue processor: version checks, getOps,
// transform-by-cached-ops (with dup detection), apply, the maxDocLength check
// (vendored quirk: against the OLD snapshot), the version-match check,
// writeOp, cache update, emits, and the opsBeforeCommit snapshot commit.
func (m *Model) opQueue(name string, d *doc, od OpData, cb func(error, any)) {
	// "Version missing" (nil or < 0 version).
	if od.V == nil || *od.V < 0 {
		cb(errors.New("Version missing"), 0)
		return
	}
	if *od.V > d.v {
		cb(errors.New("Op at future version"), 0)
		return
	}
	// Punt the transforming work back to the client if the op is too old.
	if *od.V+m.opts.maximumAge() < d.v {
		cb(errors.New("Op too old"), 0)
		return
	}
	meta := metaDup(od.Meta)
	meta["ts"] = "ts"

	ops, err := m.getOpsRaw(name, *od.V, d.v, false)
	if err != nil {
		cb(err, 0)
		return
	}
	if d.v-*od.V != len(ops) {
		cb(errors.New("Internal error"), 0)
		return
	}
	t := m.registry[d.typeName]
	op := od.Op
	v := *od.V
	for i := range ops {
		// Dup detection (vendor: oldOp.meta.source in opData.dupIfSource).
		if od.DupIf != nil && ops[i].Meta["source"] != nil {
			if src, ok := ops[i].Meta["source"].(string); ok {
				for _, s := range od.DupIf {
					if s == src {
						cb(errors.New("Op already submitted"), 0)
						return
					}
				}
			}
		}
		tr, terr := t.Transform(op, ops[i].Op, "left")
		if terr != nil {
			cb(terr, 0)
			return
		}
		op = tr
		v++
	}
	nsnap, aerr := t.Apply(d.snapshot, op)
	if aerr != nil {
		cb(aerr, 0)
		return
	}
	// Vendored quirk: the check uses the OLD snapshot (pre-apply).
	if m.opts.MaxDocLength != 0 && lenOfString(d.snapshot) > m.opts.MaxDocLength {
		cb(errors.New("Update takes doc over max doc size"), 0)
		return
	}
	// Version match (should-never-happen sanity check).
	if v != d.v {
		cb(errors.New("Internal error"), 0)
		return
	}
	if m.db != nil {
		m.db.WriteOp(name, StoredOp{Op: op, V: v, Meta: metaDup(meta)})
	}
	oldSnap := d.snapshot
	d.v = v + 1
	d.snapshot = nsnap
	d.ops = append(d.ops, StoredOp{Op: op, V: v, Meta: metaDup(meta)})
	if m.db != nil && len(d.ops) > m.opts.numCachedOps() {
		d.ops = d.ops[1:]
	}
	m.emitEvent(Event{
		Kind:    "applyOp",
		DocName: name,
		OpV:     v,
		Snap:    nsnap,
		OldSnap: oldSnap,
		MetaSrc: metaSource(meta),
		Op:      op,
		Meta:    metaDup(meta),
	})
	for _, l := range d.listeners {
		l.fn(OpData{Op: op, V: ptr(v), Meta: metaDup(meta)})
	}
	// The callback is called with the version at which the op was applied.
	cb(nil, v)
	// Maybe commit the snapshot.
	if !d.snapLock && d.committedV+m.opts.opsBeforeCommit() <= d.v {
		m.tryWriteSnapshot(name)
	}
}

func (m *Model) makeOpQueueProcess(name string, d *doc) syncqueue.Processor {
	return func(data any, done syncqueue.Callback) {
		od, ok := data.(OpData)
		if !ok {
			done(fmt.Errorf("Invalid op data"), 0)
			return
		}
		m.opQueue(name, d, od, done)
	}
}

// --- public API (vendored interface methods) ------------------------------------

// Create mirrors the vendor create. typeName is a registry name; meta may be
// nil. Emits "add" (via add) then "create" (vendor ordering).
func (m *Model) Create(name, typeName string, meta map[string]any) error {
	if nameHasSlash(name) {
		return errors.New("Invalid document name")
	}
	if m.docs[name] != nil {
		return errors.New("Document already exists")
	}
	t, ok := m.registry[typeName]
	if !ok {
		return errors.New("Type not found")
	}
	if meta == nil {
		meta = map[string]any{}
	}
	data := DocSnapshot{Snapshot: t.Create(), V: 0, Type: typeName, Meta: meta}
	if m.db != nil {
		if err := m.db.Create(name, data); err != nil {
			return err
		}
	}
	if _, err := m.add(name, data, 0, []StoredOp{}, data.Snapshot, 0, nil); err != nil {
		return err
	}
	m.emitEvent(Event{Kind: "create", DocName: name, DocV: 0, Snapshot: data.Snapshot, TypeName: typeName})
	return nil
}

// Delete mirrors the vendor delete. Emits "delete" only on success.
func (m *Model) Delete(name string) error {
	wasCached := m.docs[name] != nil
	delete(m.docs, name)
	if m.db != nil {
		if err := m.db.Delete(name); err != nil {
			return err
		}
	} else if !wasCached {
		return errors.New("Document does not exist")
	}
	m.emitEvent(Event{Kind: "delete", DocName: name})
	return nil
}

// GetOps mirrors the vendor getOps. end nil means start..latest.
func (m *Model) GetOps(name string, start int, end *int) ([]StoredOp, error) {
	if end == nil {
		return m.getOpsRaw(name, start, 0, true)
	}
	return m.getOpsRaw(name, start, *end, false)
}

// GetVersion mirrors the vendor getVersion.
func (m *Model) GetVersion(name string) (int, error) {
	d, err := m.load(name)
	if err != nil {
		return 0, err
	}
	return d.v, nil
}

// GetSnapshot mirrors the vendor getSnapshot.
func (m *Model) GetSnapshot(name string) (DocSnapshot, error) {
	d, err := m.load(name)
	if err != nil {
		return DocSnapshot{}, err
	}
	return DocSnapshot{Snapshot: d.snapshot, V: d.v, Type: d.typeName, Meta: d.meta}, nil
}

// ApplyOp mirrors the vendor applyOp. Queued FIFO via the doc's syncqueue
// (serialised per doc). Returns the applied version (op.v after transform).
func (m *Model) ApplyOp(name string, od OpData) (int, error) {
	d, err := m.load(name)
	if err != nil {
		return 0, err
	}
	var outV int
	var aerr error
	d.opQueue.Enqueue(od, func(err error, res any) { aerr = err; outV = toInt(res) })
	return outV, aerr
}

// Listen mirrors the vendor listen. version nil => open at latest (no
// catch-up). The vendored model registers the listener, fires its callback
// (the harness logs c:listen), and THEN replays catch-up ops to the listener.
// The Go port preserves that ordering: register, cb, replay. listener is
// registered either way; the returned ref is for RemoveListener.
func (m *Model) Listen(name string, version *int, listener func(OpData), cb func(error, int)) (Listener, error) {
	d, lerr := m.load(name)
	if lerr != nil {
		cb(lerr, 0)
		return Listener{}, lerr
	}
	ref := Listener{seq: m.lsSeq}
	m.lsSeq++
	d.listeners = append(d.listeners, listenerEnt{ref: ref, fn: listener})
	if version != nil {
		ops, oerr := m.getOpsRaw(name, *version, 0, true)
		if oerr != nil {
			cb(oerr, 0)
			return ref, oerr
		}
		cb(nil, *version) // vendor: callback BEFORE the catch-up replay
		for i := range ops {
			op := ops[i]
			listener(OpData{Op: op.Op, V: ptr(op.V), Meta: metaDup(op.Meta)})
			// The listener may remove itself during catch-up (vendor breaks early).
			if !m.hasListener(d, ref) {
				break
			}
		}
	} else {
		cb(nil, d.v)
	}
	return ref, nil
}

func (m *Model) hasListener(d *doc, ref Listener) bool {
	for _, l := range d.listeners {
		if l.ref == ref {
			return true
		}
	}
	return false
}

// RemoveListener mirrors the vendor removeListener (vendor throws when the
// doc is not loaded; Go returns the same error).
func (m *Model) RemoveListener(name string, l Listener) error {
	d := m.docs[name]
	if d == nil {
		return errors.New("removeListener called but document not loaded")
	}
	out := make([]listenerEnt, 0, len(d.listeners))
	for _, e := range d.listeners {
		if e.ref != l {
			out = append(out, e)
		}
	}
	d.listeners = out
	return nil
}

// ApplyMetaOp mirrors the vendor applyMetaOp. meta = {path, value, ...}.
func (m *Model) ApplyMetaOp(name string, meta map[string]any) (int, error) {
	pathAny, _ := meta["path"]
	path, ok := pathAny.([]any)
	if !ok {
		return 0, errors.New("path should be an array")
	}
	d, lerr := m.load(name)
	if lerr != nil {
		return 0, lerr
	}
	value, _ := meta["value"]
	applied := false
	if len(path) > 0 {
		if p0, pok := path[0].(string); pok && p0 == "shout" {
			for _, l := range d.listeners {
				l.fn(OpData{Meta: meta})
			}
			applied = true
		}
	}
	if applied {
		m.emitEvent(Event{Kind: "applyMetaOp", DocName: name, Path: path, Value: value, DocV: d.v})
	}
	return d.v, nil
}

// Flush mirrors the vendor flush: write all pending snapshots to the DB.
func (m *Model) Flush() {
	if m.db == nil {
		return
	}
	for name, d := range m.docs {
		if d.committedV < d.v {
			m.tryWriteSnapshot(name)
		}
	}
}

// CloseDb mirrors the vendor closeDb (db.close(); db = nil).
func (m *Model) CloseDb() {
	if m.db != nil {
		m.db.Close()
	}
	m.db = nil
}

// --- helpers -------------------------------------------------------------------

func metaDup(m map[string]any) map[string]any {
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

func metaSource(m map[string]any) string {
	if s, ok := m["source"].(string); ok {
		return s
	}
	return ""
}

func lenOfString(x any) int {
	if s, ok := x.(string); ok {
		return len(s)
	}
	return 0
}

func toInt(x any) int {
	if v, ok := x.(int); ok {
		return v
	}
	return 0
}

func ptr(v int) *int { return &v }

func nameHasSlash(name string) bool {
	for i := 0; i < len(name); i++ {
		if name[i] == '/' {
			return true
		}
	}
	return false
}
