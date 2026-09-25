// Package historyotupdate — 1:1 port of `app/js/HistoryOTUpdateManager.js`
// (172 LOC).
//
// The vendor module applies a history-OT (overleaf-editor-core) edit update
// to a document: it fetches the doc, rebases the update's ops onto any
// concurrent updates that advanced the doc since `update.v` (via
// overleaf-editor-core `EditOperationTransformer.transformMultiple`),
// applies them to a `StringFileData`, persists through Redis, queues the
// update into project history, records a project notification timestamp,
// and broadcasts the op to the realtime bus.
//
// All collaborators are injected seams (mirroring the vendor unit test's
// `SandboxedModule` stubs): DocumentManager.getDoc, RedisManager
// {getPreviousDocOps, updateDocument, recordProjectNotificationTimestamp},
// ProjectHistoryRedisManager.queueOps, HistoryManager.recordAndFlushHistoryOps,
// RealTimeRedisManager.sendData. The OT logic (op validation / fromJSON /
// transform / StringFileData) is consumed from the upstream `otc` package
// (overleaf-editor-core) — the same engine the vendor requires.
//
// Go divergences (documented):
//   - vendor `Metrics.inc` side effects are not modelled (not observable in
//     the oracle);
//   - vendor's Profiler is a no-op timing wrapper; one is still instantiated
//     and its .log/.end calls made for shape fidelity;
//   - vendor's async/promise is Go sync (value returns + error);
//   - upstream `otc.TransformEditOpsMultiple` swallows per-pair transform
//     errors (`continue`); the vendor Node throws. The oracle paths never hit
//     a transform error, so behavior is identical there (upstream is a fixed
//     contract — not changed);
//   - the vendor guard's `'v' in update` key-presence check has no Go
//     analogue for Update.V (an int is always "present"); every update flowing
//     through the services carries v (wire type invariant), so the guard is
//     `u != nil && u.Doc != "" && len(u.Op) > 0 && every op valid`;
//   - the `dup` key is only present in the serialised shapes when true
//     (vendor `{...update}` spread: the key exists only when assigned).
package historyotupdate

import (
	"encoding/json"

	"document-updater/internal/errorsx"
	"document-updater/internal/profiler"

	otc "ollitex/go/libraries/otc"
)

// Update mirrors the vendor Update object for history-OT edits:
// {doc, op (raw wire edit ops), v, meta, dupIfSource?, dup?}.
//
// Op holds the RAW wire ops (each a JSON object, e.g.
// {textOperation: [...]} / {commentId, ranges} / {noOp: true}) so both the
// overleaf-editor-core validation (EditOperationBuilder.isValid) and the
// re-serialisation (redis updateDocument / project-history queueOps) are
// direct.
type Update struct {
	Doc         string
	Op          []map[string]any
	V           int
	Meta        map[string]any
	DupIfSource []string
	Dup         bool
}

// ToRaw serialises the update to its wire shape. `dup` appears only when set
// (vendor `{...update}` spread semantics).
func (u *Update) ToRaw() map[string]any {
	raw := map[string]any{
		"doc": u.Doc,
		"op":  rawOpArray(u.Op),
		"v":   u.V,
		"meta": func() map[string]any {
			if u.Meta == nil {
				return map[string]any{}
			}
			return u.Meta
		}(),
	}
	if len(u.DupIfSource) > 0 {
		raw["dupIfSource"] = u.DupIfSource
	}
	if u.Dup {
		raw["dup"] = true
	}
	return raw
}

func rawOpArray(op []map[string]any) []any {
	out := make([]any, 0, len(op))
	for _, o := range op {
		out = append(out, o)
	}
	return out
}

// FromWire builds an Update from a raw wire object (e.g. a concurrent update
// read back from Redis — always `{doc, op, v, meta...}`).
func FromWire(raw map[string]any) *Update {
	u := &Update{Meta: map[string]any{}}
	if d, ok := raw["doc"].(string); ok {
		u.Doc = d
	}
	if v, ok := raw["v"].(int); ok {
		u.V = v
	} else if f, ok := raw["v"].(float64); ok {
		u.V = int(f)
	}
	if m, ok := raw["meta"].(map[string]any); ok {
		u.Meta = m
	}
	if s, ok := raw["dupIfSource"].([]any); ok {
		for _, e := range s {
			if ss, ok := e.(string); ok {
				u.DupIfSource = append(u.DupIfSource, ss)
			}
		}
	}
	if s, ok := raw["dupIfSource"].([]string); ok {
		u.DupIfSource = append(u.DupIfSource, s...)
	}
	if b, ok := raw["dup"].(bool); ok {
		u.Dup = b
	}
	switch op := raw["op"].(type) {
	case []any:
		for _, e := range op {
			if m, ok := e.(map[string]any); ok {
				u.Op = append(u.Op, m)
			}
		}
	case []map[string]any:
		for _, m := range op {
			u.Op = append(u.Op, m)
		}
	}
	return u
}

// IsHistoryOTEditOperationUpdate mirrors the vendor type-guard.
func IsHistoryOTEditOperationUpdate(u *Update) bool {
	if u == nil || u.Doc == "" || len(u.Op) == 0 {
		return false
	}
	for _, op := range u.Op {
		if !otc.IsValidEditOperationRaw(op) {
			return false
		}
	}
	return true
}

// Manager is the HistoryOTUpdateManager with all collaborators injected
// (mirroring the vendor unit test's SandboxedModule stubs). A nil seam is a
// no-op for calls the vendor module guards (`?`-optional meta.user_id etc.).
type Manager struct {
	GetDoc func(projectID, docID string) (lines any, version int, pathname, docType string, found bool, err error)

	GetPreviousDocOps func(docID string, start, end int) ([]map[string]any, error)

	UpdateDocument func(projectID, docID string, docLines map[string]any, version int, appliedOps []map[string]any, ranges map[string]any, updateMeta map[string]any) error

	RecordProjectNotificationTimestamp func(projectID string, timestamp, userID any) error

	QueueOps func(projectID string, ops []string) (int, error)

	RecordAndFlushHistoryOps func(projectID string, updates []map[string]any, projectOpsLength int)

	SendData func(payload map[string]any)

	// Now mirrors Date.now() (vendor `Date.now()` for meta.ts).
	Now func() int64
}

// New builds an empty Manager (all seams nil); production wiring supplies the
// concrete collaborators.
func New() *Manager { return &Manager{} }

// ApplyUpdate mirrors vendor applyUpdate: wrap tryApplyUpdate; on a throw,
// broadcast the error to the realtime bus and re-throw.
func (m *Manager) ApplyUpdate(projectID, docID string, u *Update) error {
	prof := profiler.New("applyUpdate")
	defer prof.End()
	err := m.tryApplyUpdate(projectID, docID, u)
	if err != nil {
		prof.Log("sendData", false)
		m.send(map[string]any{"project_id": projectID, "doc_id": docID, "error": errString(err)})
	}
	return err
}

// errString mirrors the vendor catch `error instanceof Error ? error.message
// : String(error)`: a typed errorsx error carries `message` separate from its
// rendered name (Go renders "Type: message"); the vendored otc errors already
// render message-only.
func errString(err error) string {
	switch e := err.(type) {
	case *errorsx.NotFoundError:
		return e.Message
	case *errorsx.OTTypeMismatchError:
		return e.Message
	default:
		return err.Error()
	}
}

func (m *Manager) tryApplyUpdate(projectID, docID string, u *Update) (retErr error) {
	prof := profiler.New("applyUpdate")
	defer prof.End()

	lines, version, pathname, docType, found, err := m.doc(projectID, docID)
	prof.Log("getDoc", false)
	if err != nil {
		return err
	}
	if !found || lines == nil {
		return errorsx.NotFoundMsg("document not found: " + docID)
	}
	if docType != "history-ot" {
		return errorsx.OTTypeMismatch(docType, "history-ot")
	}
	if u.Meta == nil {
		u.Meta = map[string]any{}
	}

	// Parse the update's raw ops into overleaf-editor-core edit ops.
	ops, perr := parseOps(u.Op)
	if perr != nil {
		return perr
	}

	// Rebase onto any concurrent updates that advanced the doc since u.V.
	if version != u.V {
		transformUpdates, terr := m.getPreviousDocOps(docID, u.V, version)
		prof.Log("getPreviousDocOps", false)
		if terr != nil {
			return terr
		}
		for _, tr := range transformUpdates {
			tu := FromWire(tr)
			if !IsHistoryOTEditOperationUpdate(tu) {
				return errorsx.OTTypeMismatch("sharejs-text-ot", "history-ot")
			}
			if tu.Meta != nil {
				if src, ok := tu.Meta["source"].(string); ok {
					if u.dupIfSourceIncludes(src) {
						u.Dup = true
						break
					}
				}
			}
			transformOps, xerr := parseOps(tu.Op)
			if xerr != nil {
				return xerr
			}
			otc.TransformEditOpsMultiple(ops, transformOps)
		}
		u.Op = opsAsRaw(ops)
	}

	if !u.Dup {
		file, ferr := otc.FromRawStringFileData(linesAsMap(lines))
		if ferr != nil {
			return ferr
		}
		for _, op := range ops {
			if aerr := file.Edit(op); aerr != nil {
				return aerr
			}
		}
		version += 1
		u.Meta["ts"] = m.now()

		if uerr := m.updateDocument(projectID, docID, file.ToRaw(), version, []map[string]any{u.ToRaw()}, map[string]any{}, u.Meta); uerr != nil {
			return uerr
		}
		// queueOps + recordAndFlushHistoryOps: a failure is nonfatal
		// (vendor catch: metrics.inc('history-queue-error') and the ack).
		_ = m.queueAndHistory(projectID, u, pathname)
		_ = m.recordNotification(projectID, u)
		prof.Log("recordAndFlushHistoryOps", false)
		prof.Log("recordProjectNotificationTimestamp", false)
	}

	// Broadcast the (possibly dup/transformed) op to the realtime bus.
	m.send(map[string]any{"project_id": projectID, "doc_id": docID, "op": u.ToRaw()})
	return nil
}

// doc mirrors DocumentManager.promises.getDoc(projectId, docId) ->
// {lines, version, pathname, type}.
func (m *Manager) doc(projectID, docID string) (lines any, version int, pathname, docType string, found bool, err error) {
	if m.GetDoc == nil {
		return nil, 0, "", "", false, nil
	}
	return m.GetDoc(projectID, docID)
}

func (m *Manager) getPreviousDocOps(docID string, start, end int) ([]map[string]any, error) {
	if m.GetPreviousDocOps == nil {
		return nil, nil
	}
	return m.GetPreviousDocOps(docID, start, end)
}

func (m *Manager) updateDocument(projectID, docID string, docLines map[string]any, version int, appliedOps []map[string]any, ranges map[string]any, meta map[string]any) error {
	if m.UpdateDocument == nil {
		return nil
	}
	return m.UpdateDocument(projectID, docID, docLines, version, appliedOps, ranges, meta)
}

// queueAndHistory mirrors:
//
//	const projectOpsLength = await ProjectHistoryRedisManager.promises.queueOps(
//	  projectId, [JSON.stringify({...update, meta: {...update.meta, pathname}})])
//	HistoryManager.recordAndFlushHistoryOps(projectId, [update], projectOpsLength)
//
// wrapped in the vendor's nonfatal try/catch.
func (m *Manager) queueAndHistory(projectID string, u *Update, pathname string) error {
	if m.QueueOps == nil {
		return nil
	}
	serialized, serr := serializeForHistory(u, pathname)
	if serr != nil {
		return serr
	}
	n, qerr := m.QueueOps(projectID, []string{serialized})
	if qerr != nil {
		// vendor catch: recorded (metrics.inc('history-queue-error')), NOT
		// propagated; history is acked either way.
		return qerr
	}
	if m.RecordAndFlushHistoryOps != nil {
		m.RecordAndFlushHistoryOps(projectID, []map[string]any{u.ToRaw()}, n)
	}
	return nil
}

// recordNotification mirrors
// RedisManager.recordProjectNotificationTimestamp(projectId, update.meta.ts,
// update.meta?.user_id).
func (m *Manager) recordNotification(projectID string, u *Update) error {
	if m.RecordProjectNotificationTimestamp == nil {
		return nil
	}
	if u.Meta == nil {
		u.Meta = map[string]any{}
	}
	ts, _ := u.Meta["ts"]
	userID, _ := u.Meta["user_id"]
	return m.RecordProjectNotificationTimestamp(projectID, ts, userID)
}

// serializeForHistory mirrors
// JSON.stringify({...update, meta: {...update.meta, pathname}}):
// the full update (doc/op/v/meta, + dup/dupIfSource when present) with the
// meta augmented by the document pathname.
func serializeForHistory(u *Update, pathname string) (string, error) {
	raw := u.ToRaw()
	meta := map[string]any{}
	for k, v := range u.Meta {
		meta[k] = v
	}
	if pathname != "" {
		meta["pathname"] = pathname
	}
	raw["meta"] = meta
	b, err := json.Marshal(raw)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (m *Manager) send(payload map[string]any) {
	if m.SendData == nil {
		return
	}
	m.SendData(payload)
}

func (m *Manager) now() int64 {
	if m.Now == nil {
		return 0
	}
	return m.Now()
}

func (u *Update) dupIfSourceIncludes(source string) bool {
	for _, s := range u.DupIfSource {
		if s == source {
			return true
		}
	}
	return false
}

func parseOps(raw []map[string]any) ([]otc.EditOperation, error) {
	ops := make([]otc.EditOperation, 0, len(raw))
	for _, r := range raw {
		op, err := otc.FromJSONEditOperation(r)
		if err != nil {
			return nil, err
		}
		ops = append(ops, op)
	}
	return ops, nil
}

func opsAsRaw(ops []otc.EditOperation) []map[string]any {
	out := make([]map[string]any, 0, len(ops))
	for _, o := range ops {
		if noOp, ok := o.(*otc.EditNoOp); ok {
			out = append(out, noOp.ToJSON())
			continue
		}
		out = append(out, o.ToJSON())
	}
	return out
}

func linesAsMap(lines any) map[string]any {
	m, _ := lines.(map[string]any)
	return m
}
