// Package sharejsupdatemanager — 1:1 port of `app/js/ShareJsUpdateManager.js`
// (154 LOC).
//
// The vendor builds a FRESH ShareJsModel per applied update (a ShareJsDB
// bound to the known base state + version, options
// `maximumAge: MAX_AGE_OF_OP = 80` and `maxDocLength: Settings.max_doc_length`),
// applies the wire update to it, and then:
//
//   - maps the vendored model callback strings:
//     'Op already submitted'  ->  sets `update.dup = true`, re-emits the
//     update on the realtime bus (RealTimeRedisManager.sendData) and —
//     vendored quirk, that branch has no `return` — falls through to the
//     post-apply snapshot read;
//     /^Delete component/     ->  DeleteMismatchError("Delete component does
//     not match");
//     any other error         ->  passed through verbatim;
//   - reads the post-apply snapshot (model.getSnapshot, cached after the
//     apply): documents the new size against Settings.max_doc_length
//     ("Update takes doc over max doc size") and, when the update carried a
//     hash and no other update has been applied since (`incomingVersion ==
//     version`), verifies the SHA-1 blob hash ("Invalid hash");
//   - splits the snapshot on /\r\n|\n|\r/ and returns (docLines, v,
//     appliedOps), where appliedOps is the ops buffered on the db instance
//     (model.db.appliedOps[docKey] || []) so the caller can persist them;
//   - forwards every model 'applyOp' emission to
//     RealTimeRedisManager.sendData({project_id, doc_id, op})
//     (_listenForOps + _sendOp).
//
// Go divergence: the model is built through the overridable `NewModel`
// factory mirroring the vendor's (unit-test) stubbable
// `getNewShareJsModel`; the realtime bus is the injected `SendOps` func
// (a vendored module dependency); the vendor's `promisifyAll` promise
// wrapper is not modelled.
package sharejsupdatemanager

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"

	"document-updater/internal/errorsx"
	"document-updater/internal/sharejsdb"
	"document-updater/internal/sharejsmodel"
	"document-updater/internal/updatekeys"
)

// MaxAgeOfOp mirrors vendored MAX_AGE_OF_OP (ops whose version is more than
// this many behind the doc are rejected as 'Op too old').
const MaxAgeOfOp = 80

// DefaultMaxDocLength mirrors `Settings.max_doc_length` from
// config/settings.defaults.js (2 * 1024 * 1024).
const DefaultMaxDocLength = 2 * 1024 * 1024

// Update mirrors the vendored wire update object (the opData the model
// applies): {op, v, meta{source?...}, hash?, dupIfSource?}.
type Update struct {
	Op   []any // wire op (component maps); nil = "undefined op"
	V    int
	Meta map[string]any
	// Hash is present (non-nil) when the update carries a hash; the hash
	// check runs when it is present AND no other update has been applied in
	// the meantime (the incoming version equals the base version).
	Hash        *string
	DupIfSource []string
	Dup         bool // flipped true by ApplyUpdate on 'Op already submitted'
}

// Model is the model surface the manager drives. *sharejsmodel.Model
// satisfies it directly (production); unit tests inject a fake, mirroring
// the vendor unit test's sinon-stubbed model.
type Model interface {
	ApplyOp(docKey string, od sharejsmodel.OpData) (int, error)
	GetSnapshot(docKey string) (sharejsmodel.DocSnapshot, error)
	EmitFn(fn func(ev sharejsmodel.Event))
}

// Handle pairs the built model with its ShareJsDB (vendor: `model.db =
// db`). AppliedOps live on the db (vendor: `model.db.appliedOps[docKey]`).
type Handle struct {
	Model Model
	DB    *sharejsdb.ShareJsDB
}

// NewModel builds a Handle for (projectID, docID) bound to the known base
// state (lines, version). It mirrors the vendored `getNewShareJsModel`
// (a method on the module object, hence stubbable in unit tests).
type NewModel func(projectID, docID string, lines []string, version int) Handle

// ShareJsUpdateManager is the Go analogue of the vendored module object.
type ShareJsUpdateManager struct {
	// MaxDocLength mirrors `Settings.max_doc_length`; it is both the model
	// option and the manager's post-apply size check (vendored
	// `docSizeAfter > Settings.max_doc_length`). 0 disables the manager
	// check (vendor: settings is always set; 0 is a test-seam).
	MaxDocLength int
	// OpsFetcher is wired into the ShareJsDB the default NewModel builds
	// (nil => the db serves ops only at the base version, where the
	// vendored db early-exits without Redis).
	OpsFetcher sharejsdb.OpsFetcher
	// SendOps mirrors RealTimeRedisManager.sendData({project_id, doc_id,
	// op}). nil = no-op.
	SendOps func(projectID, docID string, opData any)
	// NewModel is the model factory (default: production ShareJsDB +
	// sharejsmodel.Model over the "text" wire type).
	NewModel NewModel
}

// New builds a production manager: real ShareJsDB + sharejsmodel.Model
// (text wire type, MaxAgeOfOp, MaxDocLength), no realtime sink, no ops
// fetcher. Production wiring (later HTTP/manager layers) sets SendOps and
// OpsFetcher.
func New() *ShareJsUpdateManager {
	m := &ShareJsUpdateManager{MaxDocLength: DefaultMaxDocLength}
	m.NewModel = func(projectID, docID string, lines []string, version int) Handle {
		return m.buildProduction(projectID, docID, lines, version)
	}
	return m
}

func (m *ShareJsUpdateManager) buildProduction(projectID, docID string, lines []string, version int) Handle {
	db := sharejsdb.New(projectID, docID, lines, version)
	db.GetPreviousDocOps = m.OpsFetcher
	opts := sharejsmodel.Options{
		MaximumAge:   MaxAgeOfOp,
		MaxDocLength: m.MaxDocLength,
	}
	model := sharejsmodel.New(
		db,
		map[string]sharejsmodel.Type{"text": sharejsmodel.TextWireType{}}, opts)
	return Handle{Model: model, DB: db}
}

// ApplyUpdate mirrors vendored applyUpdate(projectId, docId, update, lines,
// version) and resolves to the vendored promisifyAll
// [updatedDocLines, version, appliedOps] triple.
func (m *ShareJsUpdateManager) ApplyUpdate(projectID, docID string, update *Update, lines []string, version int) ([]string, int, []any, error) {
	// Vendored: "record the update version before it is modified" (the
	// model mutates opData.v during transform).
	incomingUpdateVersion := update.V
	h := m.NewModel(projectID, docID, lines, version)
	m.ListenOps(h.Model)

	docKey := updatekeys.CombineProjectIdAndDocId(projectID, docID)
	od := sharejsmodel.OpData{
		Op:    update.Op,
		V:     &update.V,
		Meta:  update.Meta,
		DupIf: update.DupIfSource,
	}
	if _, err := h.Model.ApplyOp(docKey, od); err != nil {
		switch {
		case err.Error() == "Op already submitted":
			// Vendor: no `return` here — flag the op as duplicate, re-emit
			// it on the realtime bus, and fall through to the post-apply
			// snapshot read (the op was rejected, so the read is the
			// PRE-APPLY state).
			update.Dup = true
			payload := map[string]any{"op": update.Op, "v": update.V, "meta": update.Meta, "dup": true}
			if update.Hash != nil {
				payload["hash"] = *update.Hash
			}
			if update.DupIfSource != nil {
				payload["dupIfSource"] = update.DupIfSource
			}
			m.sendOp(projectID, docID, payload)
		case strings.HasPrefix(err.Error(), "Delete component"):
			return nil, 0, nil, errorsx.DeleteMismatchMsg("Delete component does not match")
		default:
			return nil, 0, nil, err
		}
	}

	snap, err := h.Model.GetSnapshot(docKey)
	if err != nil {
		return nil, 0, nil, err
	}
	snapshot, _ := snap.Snapshot.(string)

	if m.MaxDocLength > 0 && len(snapshot) > m.MaxDocLength {
		// Vendor logs {err: "blocking persistence of ShareJs update: doc
		// size exceeds limits"} and surfaces the bare public string; the
		// Go port surfaces it typed.
		return nil, 0, nil, errorsx.FileTooLargeMsg("Update takes doc over max doc size")
	}

	// Vendor: "only check hash when present and no other updates have
	// been applied" (the base version the caller handed us still equals
	// the incoming version).
	if update.Hash != nil && incomingUpdateVersion == version {
		if computeHash(snapshot) != *update.Hash {
			return nil, 0, nil, errors.New("Invalid hash")
		}
	}

	appliedOps := h.DB.AppliedOps[docKey]
	if appliedOps == nil {
		appliedOps = []any{}
	}
	return splitDoc(snapshot), snap.V, appliedOps, nil
}

// ListenOps installs the vendored `_listenForOps`: every model-level
// 'applyOp' emission forwards {op, v, meta} to SendOps for the emitting
// document's (project, doc) pair. Emissions of other kinds are ignored.
func (m *ShareJsUpdateManager) ListenOps(model Model) {
	model.EmitFn(func(ev sharejsmodel.Event) {
		if ev.Kind != "applyOp" {
			return
		}
		projectID, docID, ok := updatekeys.SplitProjectIdAndDocId(ev.DocName)
		if !ok {
			return
		}
		// Vendor opData (post-apply in the model): {op, v, meta}.
		m.sendOp(projectID, docID, map[string]any{"op": ev.Op, "v": ev.OpV, "meta": ev.Meta})
	})
}

func (m *ShareJsUpdateManager) sendOp(projectID, docID string, opData any) {
	if m.SendOps != nil {
		m.SendOps(projectID, docID, opData)
	}
}

// computeHash mirrors vendored `_computeHash(content)`:
//
//	crypto.createHash('sha1').update('blob ' + content.length + '\x00')
//	     .update(content, 'utf8').digest('hex')
//
// `content.length` is a JavaScript length (UTF-16 code units); the Go
// port uses the UTF-16 unit count.
func computeHash(content string) string {
	utf16Len := len(utf16.Encode([]rune(content)))
	header := fmt.Sprintf("blob %d", utf16Len)
	h := sha1.New()
	h.Write([]byte(header))
	h.Write([]byte{0}) // the vendored '\x00'
	h.Write([]byte(content))
	return hex.EncodeToString(h.Sum(nil))
}

// splitDoc mirrors vendored `String.split(/\r\n|\n|\r/)`: a run "\r\n" is
// ONE separator, "\n" and "\r" each are separators, and a trailing
// separator yields a trailing empty element (Go's strings.Split is not
// equivalent for the "\r\n" case without manual handling).
func splitDoc(s string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\r':
			out = append(out, s[start:i])
			start = i + 1
			if i+1 < len(s) && s[i+1] == '\n' {
				start = i + 2 // \r\n is a single separator; skip both
				i++
			}
		case '\n':
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
