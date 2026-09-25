// Package updatemanager — 1:1 port of `app/js/UpdateManager.js`
// (overleaf/document-updater): 456-LOC vendor + the 990-LOC oracle.
//
// The UpdateManager owns the per-project update-drain orchestration: take
// the project lock, drain the shared per-project queue (routing each update
// to the history-OT path or the ShareJS path by its wire-op shape), and
// keep draining as long as updates are queued. It also applies a single
// update end-to-end (sanitize -> getDoc -> ShareJS apply -> range update ->
// persist -> history queue -> rejected-track-changes notification ->
// collapse snapshot) and locks a caller method into the drain window
// (LockUpdatesAndDo).
//
// Every collaborator is an injected seam (mirroring the vendor unit test's
// sandboxed-module stubs):
//
//	GetLock / Extend / Release            — ProjectLockManager
//
//	GetUpdates / GetProjectUpdatesLength / SendData — RealTimeRedisManager
//
//	GetDoc          — DocumentManager
//
//	ShareJsApply    — ShareJsUpdateManager.applyUpdate
//
//	IsHistoryOT / HistoryOTApply          — HistoryOTUpdateManager
//
//	ApplyRanges     — RangesManager.applyUpdate
//
//	UpdateDocument  — RedisManager.updateDocument
//
//	QueueOps        — ProjectHistoryRedisManager.queueOps
//
//	RecordAndFlushHistoryOps              — HistoryManager
//
//	RecordProjectNotificationTimestamp    — RedisManager
//
//	BuildPreviews   — TrackedChangePreview.buildSparseChangePreviews
//
//	Notify          — WebApiManager.notifyTrackChangesRejected
//
//	RecordSnapshot  — SnapshotManager.recordSnapshot
//
//	Inc / Now       — Metrics / Date.now
//
// A nil seam is nil-tolerant (the vendor guard equivalents): fire-and-forget
// seams (SendData, Notify, RecordAndFlushHistoryOps, RecordSnapshot, Inc)
// no-op, and the lookup seams degrade to their zero values (no updates,
// not-found doc, ...). Production wiring supplies the concrete
// collaborators; the oracle supplies recorders.
//
// Go divergences (documented):
//   - the vendor Promises are Go sync value/err returns;
//   - the vendor Profiler (a no-op timing wrapper) is not modelled — the
//     metric side effects are observable only through the injected Inc
//     seam;
//   - vendor `error.message` (the catch broadcast) is modelled by
//     errString: a typed errorsx error broadcasts its BARE .Message (Go's
//     rendered string is "Type: message"), any other error its .Error();
//   - the vendor surrogate-sanitize regex op.i.replace(/[\uD800-\uDFFF]/g,
//     '\uFFFD') acts on 16-bit units; Go strings can only hold PAIRED
//     surrogates (a pair is one scalar rune \U00010000–\U00010FFFF), so the
//     wire op.i cannot carry a standalone surrogate and the substitution is
//     a documented no-op (the oracle's '\uD835\uDC00' input is not
//     expressible in Go source; the pair round-trip '\U00014B5' is pinned to
//     pass through unchanged);
//   - the raw ShareJS appliedOps (wire opData maps) go verbatim to the
//     UpdateDocument seam (oracle pins the raw passthrough) and are bridged
//     to the committed rangesmanager.Update shape via the exported
//     FromOpData for the ApplyRanges seam;
//   - vendor `RecordSnapshot`/`Notify`/`Continue` fire-and-forget (`.catch`
//     in the vendor) run synchronously here: Notify swallows its error,
//     Continue's error is metriced and swallowed, RecordSnapshot propagates
//     (vendor awaits it inside the try).
package updatemanager

import (
	"encoding/json"
	"time"
	"unicode/utf16"

	"ollitex/go/libraries/rangestracker"

	"document-updater/internal/errorsx"
	"document-updater/internal/historyotupdate"
	"document-updater/internal/preview"
	"document-updater/internal/rangesmanager"
	"document-updater/internal/utils"
)

// --- wire shapes (raw JSON-decable) ----------------------------------------

// Update mirrors the shared per-project queue's wire update:
//
//	{doc, op: Component[], v, meta?, hash?, dupIfSource?, dup?}
//
// Op holds the RAW wire component maps — both the history-OT
// `{"textOperation": [...]}` shape and the ShareJS text `{i|d|c, p, ...}`
// shape decode into []map[string]any — so the manager can route the update
// before decoding. `Dup` is present (true) only when the ShareJS path has
// flipped it.
type Update struct {
	Doc         string
	Op          []map[string]any
	V           int
	Meta        map[string]any
	Hash        *string
	DupIfSource []string
	Dup         bool
}

// DocInfo mirrors the vendored DocumentManager.getDoc return shape:
// {lines, version, ranges, pathname, projectHistoryId,
// historyRangesSupport, type}. Lines == nil is the not-found signal
// (mirrors the vendor's `lines == null || version == null`); Ranges == nil
// means the doc carries no comments/changes.
type DocInfo struct {
	Lines                []string
	Version              int
	Ranges               *rangesmanager.Ranges
	Pathname             string
	ProjectHistoryID     *string
	HistoryRangesSupport bool
	Type                 string
}

// --- the manager (all collaborators injected) --------------------------------

// Manager is the UpdateManager. Every collaborator is a func seam (see the
// package doc for the vendor mapping); nil seams are nil-tolerant.
type Manager struct {
	// ProjectLockManager {getLock, extendLock, releaseLock}
	GetLock func(projectID string) (any, error)
	Extend  func(projectID string, token any) error
	Release func(projectID string, token any) error

	// RealTimeRedisManager {getPendingProjectUpdates, getProjectUpdatesLength,
	// sendData}
	GetUpdates              func(projectID string) ([]*Update, error)
	GetProjectUpdatesLength func(projectID string) (int, error)
	SendData                func(payload map[string]any)

	// DocumentManager.getDoc / ShareJsUpdateManager.applyUpdate /
	// RangesManager.applyUpdate / RedisManager.updateDocument
	GetDoc         func(projectID, docID string) (DocInfo, error)
	ShareJsApply   func(projectID, docID string, u *Update, lines []string, version int) ([]string, int, []any, error)
	ApplyRanges    func(projectID, docID string, rng *rangesmanager.Ranges, updates []rangesmanager.Update, newDocLines []string, historyRangesSupport bool) (rangesmanager.ApplyUpdateResult, error)
	UpdateDocument func(projectID, docID string, lines []string, version int, appliedOps []any, rng *rangesmanager.Ranges, meta map[string]any) error

	// HistoryOTUpdateManager {isHistoryOTEditOperationUpdate, applyUpdate}
	IsHistoryOT    func(u *Update) bool
	HistoryOTApply func(projectID, docID string, u *Update) error

	// ProjectHistoryRedisManager.queueOps / HistoryManager /
	// RedisManager.recordProjectNotificationTimestamp
	QueueOps                           func(projectID string, ops []string) (int, error)
	RecordAndFlushHistoryOps           func(projectID string, updates []rangesmanager.HistoryUpdate, projectOpsLength int)
	RecordProjectNotificationTimestamp func(projectID string, ts, userID any) error

	// TrackedChangePreview / WebApiManager.notifyTrackChangesRejected /
	// SnapshotManager
	BuildPreviews  func(changes []preview.Change, lines []string) []preview.SparseChangePreview
	Notify         func(projectID, docID string, authorIDs []string, userID any, previews []preview.SparseChangePreview) error
	RecordSnapshot func(projectID, docID string, previousVersion int, pathname string, lines []string, rng *rangesmanager.Ranges) error

	// Metrics.inc / Date.now
	Inc func(string)
	Now func() int64
}

// New returns a Manager with the vendor default wiring: Inc is a no-op
// seam, Now is the wall clock (ms), BuildPreviews is the committed preview
// builder and IsHistoryOT is the committed history-OT guard over the
// raw-op projection. Every other seam is nil (nil-tolerant) until
// production wiring fills it in.
func New() *Manager {
	return &Manager{
		Inc:           func(string) {},
		Now:           func() int64 { return time.Now().UnixMilli() },
		BuildPreviews: preview.BuildSparseChangePreviews,
		IsHistoryOT: func(u *Update) bool {
			return historyotupdate.IsHistoryOTEditOperationUpdate(toHistoryOTUpdate(u))
		},
	}
}

func (m *Manager) getLock(projectID string) (any, error) {
	if m.GetLock == nil {
		return nil, nil
	}
	return m.GetLock(projectID)
}

func (m *Manager) extend(projectID string, token any) error {
	if m.Extend == nil {
		return nil
	}
	return m.Extend(projectID, token)
}

func (m *Manager) release(projectID string, token any) error {
	if m.Release == nil {
		return nil
	}
	return m.Release(projectID, token)
}

func (m *Manager) getUpdates(projectID string) ([]*Update, error) {
	if m.GetUpdates == nil {
		return nil, nil
	}
	return m.GetUpdates(projectID)
}

func (m *Manager) getProjectUpdatesLength(projectID string) (int, error) {
	if m.GetProjectUpdatesLength == nil {
		return 0, nil
	}
	return m.GetProjectUpdatesLength(projectID)
}

func (m *Manager) getDoc(projectID, docID string) (DocInfo, error) {
	if m.GetDoc == nil {
		return DocInfo{}, nil
	}
	return m.GetDoc(projectID, docID)
}

func (m *Manager) shareJsApply(projectID, docID string, u *Update, lines []string, version int) ([]string, int, []any, error) {
	if m.ShareJsApply == nil {
		return nil, 0, nil, nil
	}
	return m.ShareJsApply(projectID, docID, u, lines, version)
}

func (m *Manager) historyOTApply(projectID, docID string, u *Update) error {
	if m.HistoryOTApply == nil {
		return nil
	}
	return m.HistoryOTApply(projectID, docID, u)
}

func (m *Manager) isHistoryOT(u *Update) bool {
	if m.IsHistoryOT != nil {
		return m.IsHistoryOT(u)
	}
	return historyotupdate.IsHistoryOTEditOperationUpdate(toHistoryOTUpdate(u))
}

func (m *Manager) applyRanges(projectID, docID string, rng *rangesmanager.Ranges, updates []rangesmanager.Update, newDocLines []string, historyRangesSupport bool) (rangesmanager.ApplyUpdateResult, error) {
	if m.ApplyRanges == nil {
		return rangesmanager.ApplyUpdateResult{}, nil
	}
	return m.ApplyRanges(projectID, docID, rng, updates, newDocLines, historyRangesSupport)
}

func (m *Manager) updateDocument(projectID, docID string, lines []string, version int, appliedOps []any, rng *rangesmanager.Ranges, meta map[string]any) error {
	if m.UpdateDocument == nil {
		return nil
	}
	return m.UpdateDocument(projectID, docID, lines, version, appliedOps, rng, meta)
}

func (m *Manager) queueOps(projectID string, ops []string) (int, error) {
	if m.QueueOps == nil {
		return 0, nil
	}
	return m.QueueOps(projectID, ops)
}

func (m *Manager) recordAndFlush(projectID string, updates []rangesmanager.HistoryUpdate, projectOpsLength int) {
	if m.RecordAndFlushHistoryOps == nil {
		return
	}
	m.RecordAndFlushHistoryOps(projectID, updates, projectOpsLength)
}

func (m *Manager) recordTS(projectID string, ts, userID any) error {
	if m.RecordProjectNotificationTimestamp == nil {
		return nil
	}
	return m.RecordProjectNotificationTimestamp(projectID, ts, userID)
}

func (m *Manager) buildPreviews(changes []preview.Change, lines []string) []preview.SparseChangePreview {
	if m.BuildPreviews == nil {
		return nil
	}
	return m.BuildPreviews(changes, lines)
}

func (m *Manager) notify(projectID, docID string, authorIDs []string, userID any, previews []preview.SparseChangePreview) error {
	if m.Notify == nil {
		return nil
	}
	return m.Notify(projectID, docID, authorIDs, userID, previews)
}

func (m *Manager) recordSnapshot(projectID, docID string, previousVersion int, pathname string, lines []string, rng *rangesmanager.Ranges) error {
	if m.RecordSnapshot == nil {
		return nil
	}
	return m.RecordSnapshot(projectID, docID, previousVersion, pathname, lines, rng)
}

func (m *Manager) inc(name string) {
	if m.Inc == nil {
		return
	}
	m.Inc(name)
}

func (m *Manager) now() int64 {
	if m.Now == nil {
		return 0
	}
	return m.Now()
}

func (m *Manager) send(payload map[string]any) {
	if m.SendData == nil {
		return
	}
	m.SendData(payload)
}

// toHistoryOTUpdate projects the wire update onto the committed history-OT
// shape the guard consumes (the ShareJS `Hash` field has no history-OT
// analogue).
func toHistoryOTUpdate(u *Update) *historyotupdate.Update {
	if u == nil {
		return nil
	}
	return &historyotupdate.Update{
		Doc:         u.Doc,
		Op:          u.Op,
		V:           u.V,
		Meta:        u.Meta,
		DupIfSource: u.DupIfSource,
		Dup:         u.Dup,
	}
}

// --- drain orchestration -----------------------------------------------------

// Process drains the project's outstanding updates under the project lock,
// then keeps draining as long as more are queued
// (processOutstandingUpdatesWithLock + continueProcessingUpdatesWithLock).
//
// Vendor order: getLock -> (try fetchAndApply / finally release) ->
// continueProcessingUpdatesWithLock. Vendor error precedence: the release
// error wins over the fetch error; a fetch error stops the chain (no
// continue).
func (m *Manager) Process(projectID string) error {
	token, err := m.getLock(projectID)
	if err != nil {
		return err
	}
	fetchErr := m.FetchAndApply(projectID)
	releaseErr := m.release(projectID, token)
	if fetchErr != nil {
		if releaseErr != nil {
			return releaseErr
		}
		return fetchErr
	}
	if releaseErr != nil {
		return releaseErr
	}
	shouldContinue, lengthErr := m.continueProcessing(projectID)
	if lengthErr != nil {
		return lengthErr
	}
	if shouldContinue {
		return m.Process(projectID)
	}
	return nil
}

// FetchAndApply applies each pending update for the project in queue
// order, routing each to the history-OT path (wire op with a
// "textOperation" key) or the plain ShareJS path.
func (m *Manager) FetchAndApply(projectID string) error {
	updates, err := m.getUpdates(projectID)
	if err != nil {
		return err
	}
	for _, u := range updates {
		if u.Doc == "" {
			continue
		}
		if m.isHistoryOT(u) {
			if err := m.historyOTApply(projectID, u.Doc, u); err != nil {
				return err
			}
			continue
		}
		if err := m.ApplyUpdate(projectID, u.Doc, u); err != nil {
			return err
		}
	}
	return nil
}

// Continue keeps draining while updates are still queued
// (continueProcessingUpdatesWithLock). length > 0 re-enters Process (which
// re-takes the lock, mirroring the vendor's nested call); length == 0 is a
// no-op.
func (m *Manager) Continue(projectID string) error {
	length, err := m.getProjectUpdatesLength(projectID)
	if err != nil {
		return err
	}
	if length == 0 {
		return nil
	}
	return m.Process(projectID)
}

// Method is the caller method LockUpdatesAndDo runs while holding the
// project lock (the vendor's `method(projectId, docId, ...args)`).
type Method func(projectID, docID string, args ...any) (any, error)

// LockUpdatesAndDo drains the project's updates, runs method under the lock
// (extending the lock first), releases in all cases, and fire-and-forget
// continues draining. Vendor order: getLock -> fetchAndApply -> extendLock ->
// method -> (finally release) -> continue...catch(metric). The continue
// error is metriced and swallowed; the return value is the method's result.
func (m *Manager) LockUpdatesAndDo(method Method, projectID, docID string, args ...any) (any, error) {
	token, err := m.getLock(projectID)
	if err != nil {
		return nil, err
	}
	applyErr := m.FetchAndApply(projectID)
	var result any
	var methodErr error
	if applyErr == nil {
		extendErr := m.extend(projectID, token)
		if extendErr != nil {
			methodErr = extendErr
		} else if method != nil {
			result, methodErr = method(projectID, docID, args...)
		}
	}
	releaseErr := m.release(projectID, token)
	if methodErr != nil {
		if releaseErr != nil {
			return nil, releaseErr
		}
		return nil, methodErr
	}
	if applyErr != nil {
		if releaseErr != nil {
			return nil, releaseErr
		}
		return nil, applyErr
	}
	if releaseErr != nil {
		return nil, releaseErr
	}
	// Fire-and-forget: the vendor's .catch(err => Metrics.inc(...)).
	// A failed Continue metrics 'background-processing-updates-error' and
	// is swallowed.
	if err := m.Continue(projectID); err != nil {
		m.inc("background-processing-updates-error")
		return result, nil
	}
	return result, nil
}

func (m *Manager) continueProcessing(projectID string) (bool, error) {
	length, err := m.getProjectUpdatesLength(projectID)
	if err != nil {
		return false, err
	}
	return length > 0, nil
}

// --- applyUpdate: the full single-update pipeline ------------------------------

// SanitizeUpdate mirrors `_sanitizeUpdate`: a standalone JS 16-bit surrogate
// (which can sit in a JS string but nowhere in a Go string) is replaced by
// the U+FFFD replacement unit in every insert op's i, per unit. In Go the
// string only holds scalar runes: a lone surrogate is impossible, a PAIRED
// surrogate is one rune above the BMP and is NOT a standalone surrogate, so
// no substitution applies (a documented no-op; see package doc). The wire
// op.i passes through unchanged.
func (m *Manager) SanitizeUpdate(u *Update) {
	for i := range u.Op {
		op := u.Op[i]
		val, ok := op["i"]
		if !ok {
			continue
		}
		str, ok := val.(string)
		if !ok {
			continue
		}
		// The vendor operates on 16-bit code units (JS strings are UTF-16)
		// and replaces every unit in U+D800-0xDFFF (the surrogate ranges —
		// INCLUDING both halves of valid surrogate pairs, which is the
		// oracle's pin) with U+FFFD. Model that: encode to UTF-16, remap the
		// units, decode back. BMP runes below U+D800 and the non-surrogate
		// range U+E000-0x10FFFF keep their single unit and pass through.
		units := utf16.Encode([]rune(str))
		if !hasSurrogateUnit(units) {
			continue
		}
		for j, unit := range units {
			if unit >= 0xD800 && unit <= 0xDFFF {
				units[j] = 0xFFFD
			}
		}
		u.Op[i]["i"] = string(utf16.Decode(units))
	}
}

func hasSurrogateUnit(units []uint16) bool {
	for _, unit := range units {
		if unit >= 0xD800 && unit <= 0xDFFF {
			return true
		}
	}
	return false
}

func (m *Manager) errString(err error) string {
	// Vendor: `error instanceof Error ? error.message : String(error)`.
	// errorsx typed errors render "Type: message" in Go but broadcast the
	// BARE message to match the vendor (pattern committed in
	// historyotupdate).
	switch e := err.(type) {
	case *errorsx.NotFoundError:
		return e.Message
	case *errorsx.OTTypeMismatchError:
		return e.Message
	case *errorsx.DeleteMismatchError:
		return e.Message
	case *errorsx.FileTooLargeError:
		return e.Message
	case *errorsx.WebApiServerError:
		return e.Message
	default:
		return err.Error()
	}
}

// --- raw -> typed bridge (the oracle pins raw passthrough end to end) -------

// FromOpData bridges the raw ShareJS applied wire opData ({op: [wire
// component...], v, meta?}) returned by ShareJsApply into the committed
// typed rangesmanager.Update shape the committed rangesmanager.ApplyUpdate
// consumes. It is the exported adapter production wiring uses for the
// ApplyRanges seam; the oracle's mock op values (non-map op lists) degrade
// to empty Op slices.
func FromOpData(docID string, appliedOps []any) []rangesmanager.Update {
	if appliedOps == nil {
		return nil
	}
	updates := make([]rangesmanager.Update, 0, len(appliedOps))
	for _, raw := range appliedOps {
		od, _ := raw.(map[string]any)
		updates = append(updates, rawOpDataToUpdate(docID, od))
	}
	return updates
}

func rawOpDataToUpdate(docID string, od map[string]any) rangesmanager.Update {
	u := rangesmanager.Update{Doc: docID}
	if rawV, ok := od["v"]; ok {
		v := int(toFloatAny(rawV))
		u.V = &v
	}
	if md, ok := od["meta"].(map[string]any); ok {
		meta := make(map[string]any, len(md))
		for k, v := range md {
			meta[k] = v
		}
		u.Meta = meta
	}
	if rawOp, ok := od["op"].([]any); ok {
		u.Op = make([]rangestracker.Op, 0, len(rawOp))
		for _, c := range rawOp {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			u.Op = append(u.Op, wireComponentToOp(cm))
		}
	}
	return u
}

func wireComponentToOp(cm map[string]any) rangestracker.Op {
	var op rangestracker.Op
	if s, ok := cm["i"].(string); ok {
		op.I = &s
	}
	if s, ok := cm["d"].(string); ok {
		op.D = &s
	}
	if s, ok := cm["c"].(string); ok {
		op.C = &s
	}
	if s, ok := cm["t"].(string); ok {
		op.T = &s
	}
	if n, ok := cm["p"]; ok {
		op.P = int(toFloatAny(n))
	}
	if b, ok := cm["u"].(bool); ok {
		op.U = &b
	}
	if b, ok := cm["resolved"].(bool); ok {
		op.Resolved = &b
	}
	return op
}

func toFloatAny(v any) float64 {
	switch t := v.(type) {
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case float64:
		return t
	case float32:
		return float64(t)
	}
	return 0
}

// --- vendor _adjustHistoryUpdatesMetadata ----------------------------------

// AdjustHistoryUpdatesMetadata mirrors vendor `_adjustHistoryUpdatesMetadata`
// (in place on `updates`): stamps projectHistoryId, computes the running
// per-update meta {pathname, doc_length} (and meta.history_doc_length when
// history ranges are supported and it differs from doc_length), drops
// meta.tc when history ranges are NOT supported, and — when history ranges
// ARE supported — stamps meta.doc_hash on the last update (the hash of
// `newLines`). Vendor argument order: (updates, pathname, projectHistoryId,
// lines /* pre-update */, ranges /* pre-update */, newLines,
// historyRangesSupport).
func (m *Manager) AdjustHistoryUpdatesMetadata(updates []rangesmanager.HistoryUpdate, pathname string, projectHistoryID *string, lines []string, rng *rangesmanager.Ranges, newLines []string, historyRangesSupport bool) {
	docLength := utils.GetDocLength(lines)
	historyDocLength := docLength
	if rng != nil {
		for _, change := range rng.Changes {
			if change.Op.D != nil {
				historyDocLength += len(*change.Op.D)
			}
		}
	}

	for i := range updates {
		u := &updates[i]
		u.ProjectHistoryID = projectHistoryID
		if u.Meta == nil {
			u.Meta = map[string]any{}
		}
		u.Meta["pathname"] = pathname
		u.Meta["doc_length"] = docLength
		if historyRangesSupport && historyDocLength != docLength {
			u.Meta["history_doc_length"] = historyDocLength
		}

		for _, op := range u.Op {
			if op.I != nil {
				docLength += len(*op.I)
				if !op.TrackedDeleteRejection {
					// Tracked delete rejections retain characters rather
					// than inserting.
					historyDocLength += len(*op.I)
				}
			}
			if op.D != nil {
				docLength -= len(*op.D)
				if jsTruthy(u.Meta["tc"]) {
					// Tracked delete: retained in history except the
					// enclosed tracked inserts, which become regular deletes.
					for _, change := range op.TrackedChanges {
						if change.Type == "insert" {
							historyDocLength -= change.Length
						}
					}
				} else {
					// Regular delete.
					historyDocLength -= len(*op.D)
				}
			}
		}

		if !historyRangesSupport {
			// Prevent project-history from processing tracked changes.
			delete(u.Meta, "tc")
		}
	}

	if historyRangesSupport && len(updates) > 0 {
		last := &updates[len(updates)-1]
		if last.Meta == nil {
			last.Meta = map[string]any{}
		}
		last.Meta["doc_hash"] = utils.ComputeDocHash(newLines)
	}
}

// jsTruthy mirrors the vendor `||`/truthiness used for meta values (a nil,
// "" or 0 or false value is falsy).
func jsTruthy(v any) bool {
	if v == nil {
		return false
	}
	switch t := v.(type) {
	case string:
		return t != ""
	case float64:
		return t != 0
	case int:
		return t != 0
	case int64:
		return t != 0
	case bool:
		return t
	}
	return true
}

// --- vendor history-update wire serialization -------------------------------

// historyOpWire / historyUpdateWire are the wire shapes vendor
// `historyUpdates.map(op => JSON.stringify(op))` emits (the committed
// HistoryUpdate/HistoryOp have no JSON tags, so the tagged mirror is local).
type historyTrackedChangeWire struct {
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
}

type historyTrackingWire struct {
	Type string `json:"type"`
}

type historyOpWire struct {
	I        *string `json:"i,omitempty"`
	D        *string `json:"d,omitempty"`
	C        *string `json:"c,omitempty"`
	R        *string `json:"r,omitempty"`
	T        *string `json:"t,omitempty"`
	P        int     `json:"p"`
	U        *bool   `json:"u,omitempty"`
	Resolved bool    `json:"resolved,omitempty"`

	Hpos *int `json:"hpos,omitempty"`
	Hlen *int `json:"hlen,omitempty"`

	CommentIds             []string                   `json:"commentIds,omitempty"`
	TrackedDeleteRejection bool                       `json:"trackedDeleteRejection,omitempty"`
	TrackedChanges         []historyTrackedChangeWire `json:"trackedChanges,omitempty"`
	Tracking               *historyTrackingWire       `json:"tracking,omitempty"`
}

type historyUpdateWire struct {
	Doc              string          `json:"doc,omitempty"`
	V                *int            `json:"v,omitempty"`
	Op               []historyOpWire `json:"op"`
	Meta             map[string]any  `json:"meta,omitempty"`
	ProjectHistoryID *string         `json:"projectHistoryId,omitempty"`
}

// SerializeHistoryUpdate mirrors vendor `JSON.stringify(update)` on one
// history update (the element of `historyUpdates.map(op => JSON.stringify(op))`
// that the vendor queues).
func (m *Manager) SerializeHistoryUpdate(u rangesmanager.HistoryUpdate) string {
	wire := historyUpdateWire{
		Doc:              u.Doc,
		V:                u.V,
		Meta:             u.Meta,
		ProjectHistoryID: u.ProjectHistoryID,
		Op:               make([]historyOpWire, 0, len(u.Op)),
	}
	for _, op := range u.Op {
		var trackedChanges []historyTrackedChangeWire
		for _, c := range op.TrackedChanges {
			trackedChanges = append(trackedChanges, historyTrackedChangeWire{Type: c.Type, Offset: c.Offset, Length: c.Length})
		}
		var tracking *historyTrackingWire
		if op.Tracking != nil {
			typ := op.Tracking.Type
			tracking = &historyTrackingWire{Type: typ}
		}
		wire.Op = append(wire.Op, historyOpWire{
			I:                      op.I,
			D:                      op.D,
			C:                      op.C,
			R:                      op.R,
			T:                      op.T,
			P:                      op.P,
			U:                      op.U,
			Resolved:               op.Resolved,
			Hpos:                   op.Hpos,
			Hlen:                   op.Hlen,
			CommentIds:             op.CommentIds,
			TrackedDeleteRejection: op.TrackedDeleteRejection,
			TrackedChanges:         trackedChanges,
			Tracking:               tracking,
		})
	}
	data, _ := json.Marshal(wire)
	return string(data)
}

// --- applyUpdate: end-to-end single-update pipeline ------------------------

// ApplyUpdate mirrors vendor `applyUpdate(projectId, docId, update)`: the
// full single-update pipeline (sanitize -> getDoc -> ShareJsApply ->
// ApplyRanges -> UpdateDocument -> adjust history metadata -> queue + flush
// history -> record notification timestamp -> rejected-changes notify ->
// collapse snapshot), with every collaborator an injected seam. Any error
// in the pipeline is broadcast to the realtime bus (SendData) and then
// returned.
func (m *Manager) ApplyUpdate(projectID, docID string, u *Update) error {
	// Vendor: _sanitizeUpdate runs first, IN PLACE, before the try block.
	m.SanitizeUpdate(u)

	if err := m.tryApplyUpdate(projectID, docID, u); err != nil {
		m.send(map[string]any{
			"project_id": projectID,
			"doc_id":     docID,
			"error":      m.errString(err),
		})
		return err
	}
	return nil
}

func (m *Manager) tryApplyUpdate(projectID, docID string, u *Update) error {
	// getDoc
	doc, err := m.getDoc(projectID, docID)
	if err != nil {
		return err
	}
	if doc.Lines == nil {
		return errorsx.NotFoundMsg("document not found: " + docID)
	}
	if doc.Type != "sharejs-text-ot" {
		return errorsx.OTTypeMismatch(doc.Type, "sharejs-text-ot")
	}

	// Snapshot the PRE-update state for the collapse-snapshot (the vendor
	// records the previous version + pre-update lines/ranges on collapse).
	previousLines := doc.Lines
	previousRanges := doc.Ranges
	previousVersion := doc.Version

	// ShareJsApply: (updatedDocLines, version, appliedOps)
	updatedDocLines, version, appliedOps, err := m.shareJsApply(projectID, docID, u, doc.Lines, doc.Version)
	if err != nil {
		return err
	}

	// RangesManager.applyUpdate over the raw-appliedOps bridge.
	result, err := m.applyRanges(projectID, docID, doc.Ranges, FromOpData(docID, appliedOps), updatedDocLines, doc.HistoryRangesSupport)
	if err != nil {
		return err
	}

	// RedisManager.updateDocument (RAW wire appliedOps verbatim).
	if err := m.updateDocument(projectID, docID, updatedDocLines, version, appliedOps, &result.NewRanges, u.Meta); err != nil {
		return err
	}

	// Adjust the history-update metadata (doc_length / history_doc_length /
	// doc_hash) in place on result.HistoryUpdates. The vendor calls this
	// unconditionally (it no-ops on an empty list).
	m.AdjustHistoryUpdatesMetadata(result.HistoryUpdates, doc.Pathname, doc.ProjectHistoryID, previousLines, previousRanges, updatedDocLines, doc.HistoryRangesSupport)

	// Then queue + flush into project history. On queue error: metric and
	// skip flush — but STILL record the timestamp (the recordTS call is
	// outside the vendor try/catch that swallows queue errors).
	if len(result.HistoryUpdates) > 0 {
		m.inc("history-queue")
		ops := make([]string, 0, len(result.HistoryUpdates))
		for _, hu := range result.HistoryUpdates {
			ops = append(ops, m.SerializeHistoryUpdate(hu))
		}
		projectOpsLength, queueErr := m.queueOps(projectID, ops)
		if queueErr == nil {
			m.recordAndFlush(projectID, result.HistoryUpdates, projectOpsLength)
		} else {
			m.inc("history-queue-error")
		}
		// recordProjectNotificationTimestamp always runs (outside the
		// vendor try/catch that swallows queue errors).
		var ts any
		var userID any
		if u.Meta != nil {
			if v, ok := u.Meta["ts"]; ok {
				ts = v
			}
			userID = u.Meta["user_id"]
		}
		if !jsTruthy(ts) {
			ts = m.now()
		}
		if err := m.recordTS(projectID, ts, userID); err != nil {
			return err
		}
	}

	// Rejected track-changes notification: build previews from the
	// PRE-update lines and notify the affected authors (fire-and-forget,
	// errors swallowed).
	if len(result.RemovedChangeIDs) > 0 {
		removed := make(map[string]bool, len(result.RemovedChangeIDs))
		for _, id := range result.RemovedChangeIDs {
			removed[id] = true
		}
		var rejected []preview.Change
		var authorIDs []string
		if previousRanges != nil {
			for _, c := range previousRanges.Changes {
				if removed[c.ID] {
					metadata := &preview.Metadata{}
					if c.Metadata != nil {
						if v, ok := c.Metadata["user_id"].(string); ok {
							metadata.UserID = v
						}
					}
					rejected = append(rejected, preview.Change{
						ID:       &c.ID,
						Op:       preview.Op{I: c.Op.I, D: c.Op.D, P: c.Op.P},
						Metadata: metadata,
					})
					authorIDs = append(authorIDs, metadata.UserID)
				}
			}
		}
		previews := m.buildPreviews(rejected, previousLines)
		var userID any
		if u.Meta != nil {
			userID = u.Meta["user_id"]
		}
		// Fire-and-forget: the notification is not awaited in the vendor.
		_ = m.notify(projectID, docID, authorIDs, userID, previews)
	}

	// Collapse snapshot: record the PRE-update state when any ranges collapsed.
	if result.RangesWereCollapsed {
		m.inc("doc-snapshot")
		if err := m.recordSnapshot(projectID, docID, previousVersion, doc.Pathname, previousLines, previousRanges); err != nil {
			return err
		}
	}

	return nil
}
