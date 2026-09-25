// Package sharejsdb — 1:1 port of `app/js/ShareJsDB.js` (147 LOC).
//
// The vendored ShareJsDB is a Redis-backed ShareJS db: one is opened per
// (project, doc) pair at a known base version, it serves the known current
// state from getSnapshot (no Redis on that path), slices ops-in-range from
// Redis in getOps (early exit when the range is empty, or when start already
// is the base version and no end is given), buffers writeOp calls into
// appliedOps (so the caller can persist them later — this DB never writes to
// Redis itself), and no-ops delete/close (the vendor handles its own
// Redis-side cleanup separately).
//
// The vendor's Metrics/logger side effects are not modelled (not observable
// in any oracle); the injected OpsFetcher mirrors RedisManager.getPreviousDocOps
// so tests are deterministic and hermetic.
//
// Go divergence (documented): the committed sharejsmodel model drives this
// db SYNCHRONOUSLY, so this port exposes the sync sharejsmodel.Db contract
// (value returns) instead of the vendored (err, val, cb) callbacks. The
// observable behavior (the early exits, the fetcher arguments — end-inclusive
// Redis lrange, the appliedOps buffer, the NotFound key check) is mirrored
// and oracle-pinned.
package sharejsdb

import (
	"errors"
	"fmt"

	"document-updater/internal/errorsx"
	"document-updater/internal/sharejsmodel"
	"document-updater/internal/updatekeys"
)

// OpsFetcher mirrors RedisManager.getPreviousDocOps(docID, start, end):
// an INCLUSIVE range over the doc's stored op list (Redis lrange is
// inclusive on both ends). end == -1 means "to the end of the list".
// Elements are raw wire op values (JSON objects in Node); error is
// the vendor's OpRangeNotAvailableError when the range is not loaded.
type OpsFetcher func(docID string, start, end int) ([]any, error)

// ShareJsDB mirrors the vendor ShareJsDB class and implements
// sharejsmodel.Db. It is bound to one (projectID, docID) pair:
//
//	projectID/docID  — identity (the docKey the model is addressed with)
//	lines            — the known base state (getSnapshot returns it joined)
//	version          — the base version the state holds
//
// AppliedOps is the vendor's @appliedOps buffer (docKey -> applied wire
// opData). WriteOp appends to it.
type ShareJsDB struct {
	ProjectID string
	DocID     string
	// Lines is the known base state (the lines getSnapshot serves).
	Lines []string
	// Version is the base version (parsed from the vendor's raw value).
	Version int

	// GetPreviousDocOps is the injected Redis round-trip (nil = fetcher not
	// configured; the model is only driven over the version range it is
	// opened at, so a nil fetcher never fires on the manager path).
	GetPreviousDocOps OpsFetcher

	// AppliedOps mirrors @appliedOps (vendor: {docKey: [opData...]}).
	AppliedOps map[string][]any
}

// New constructs the ShareJsDB for (projectID, docID) holding lines at the
// given base version.
func New(projectID, docID string, lines []string, version int) *ShareJsDB {
	return &ShareJsDB{
		ProjectID:  projectID,
		DocID:      docID,
		Lines:      lines,
		Version:    version,
		AppliedOps: map[string][]any{},
	}
}

func (s *ShareJsDB) docKey() string {
	return updatekeys.CombineProjectIdAndDocId(s.ProjectID, s.DocID)
}

// Get mirrors vendor getSnapshot: key matches -> (snapshot
// lines.join('\n'), v: version, type 'text'); key mismatch ->
// NotFoundError("unexpected doc_key X, expected Y").
func (s *ShareJsDB) GetSnapshot(name string) (sharejsmodel.DocSnapshot, any, error) {
	want := s.docKey()
	if name != want {
		return sharejsmodel.DocSnapshot{}, nil, errorsx.NotFoundMsg(
			fmt.Sprintf("unexpected doc_key %s, expected %s", name, want))
	}
	snap := joinLines(s.Lines)
	return sharejsmodel.DocSnapshot{Snapshot: snap, V: s.Version, Type: "text"}, nil, nil
}

// GetOps mirrors vendor getOps:
//
//	start == end  ->  ([] , nil) without any fetcher call
//	start == base version and endNull  ->  ([] , nil) ("is-up-to-date")
//	start > base version  ->  vendor's RedisManager.getPreviousDocOps
//	reaches here only after RedisManager's own range check already passed,
//	so this port delegates directly (the fetcher models the check + fetch).
//
// end semantics: endNull means "start..end-of-list" (the fetcher is called
// with end = -1); a numeric end decrements before the vendor's Redis
// lrange (inclusive) call.
func (s *ShareJsDB) GetOps(name string, start, end int, endNull bool) ([]sharejsmodel.StoredOp, error) {
	if start == end || (start == s.Version && endNull) {
		return []sharejsmodel.StoredOp{}, nil
	}
	fEnd := -1
	if !endNull {
		fEnd = end - 1
	}
	if s.GetPreviousDocOps == nil {
		return nil, errors.New("sharejsdb: no OpsFetcher configured")
	}
	raw, err := s.GetPreviousDocOps(s.DocID, start, fEnd)
	if err != nil {
		return nil, err
	}
	ops := make([]sharejsmodel.StoredOp, 0, len(raw))
	for _, r := range raw {
		ops = append(ops, rawOpToStored(r))
	}
	return ops, nil
}

// rawOpToStored converts a raw wire op value (as Redis returns JSON) into a
// StoredOp. Fields absent on the wire are zero-valued: nil Op (JSON null /
// absent), V = 0 stamped later by the model, Meta = nil (absent meta).
func rawOpToStored(r any) sharejsmodel.StoredOp {
	m, _ := r.(map[string]any)
	op, _ := m["op"]
	meta, _ := m["meta"].(map[string]any)
	v, _ := m["v"].(int)
	return sharejsmodel.StoredOp{Op: op, V: v, Meta: meta}
}

// WriteOp mirrors the vendor's bound _writeOp: appends opData to
// appliedOps[docKey] and returns (the vendor's (docKey, opData, cb) ->
// cb() callback is implicit).
func (s *ShareJsDB) WriteOp(name string, op sharejsmodel.StoredOp) error {
	s.AppliedOps[name] = append(s.AppliedOps[name], opWireData(op))
	return nil
}

// opWireData models the vendor opData object the writeOp callback receives:
// an object holding the wire op fields (op, meta, v) so appliedOps consumers
// (the manager returns model.db.appliedOps[docKey] straight out) see the
// vendored shape.
func opWireData(op sharejsmodel.StoredOp) any {
	m := map[string]any{"op": op.Op, "v": op.V}
	if op.Meta != nil {
		m["meta"] = op.Meta
	}
	return m
}

// Create / WriteSnapshot / Close are not used on the vendor path (the
// manager never creates via this db and close is a no-op); the sync model
// contract requires them, so they are no-ops.
func (s *ShareJsDB) Create(name string, data sharejsmodel.DocSnapshot) error { return nil }

func (s *ShareJsDB) WriteSnapshot(name string, data sharejsmodel.DocSnapshot) error {
	return nil
}

// Delete mirrors vendor delete: a no-op (the vendor removes from Redis
// itself, outside this class).
func (s *ShareJsDB) Delete(name string) error { return nil }

func (s *ShareJsDB) Close() {}

// joinLines mirrors the vendored lines.join('\n').
func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}
