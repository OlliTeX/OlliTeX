// writeback.go — 024 Option B: the room→docstore write-through.
//
// Content semantics (the 024 root-cause cluster 007/017/014 fix): the
// collab room is the LIVE collaborative state for its document; the
// docstore is the DURABLE content of record that every read path
// (downloads, git-provider export, doc GET, the seed itself) serves.
// Without this bridge, an editor edit exists only in the room and the
// store of record silently diverges — exactly the audit-024 symptom
// family ("sample.bib contains main.tex content" / stale downloads).
//
// Mechanism: WriteBack schedules a debounced write after every successful
// AppendUpdate (via the WriteBackLog store decorator) and flushes
// immediately when the service asks (room unload — ygo's
// OnUnloadDocument — via FlushRoom; deterministic tests call FlushRoom too).
// The write is fail-soft by the same contract as the version log: a
// write-through failure NEVER breaks the CRDT plane (log + drop the
// pending timer); the NEXT edit or the unload flush re-writes the room
// head, which always carries the converged content, so the store of
// record converges.
//
// Wire (1:1 with the Node setDocument→docstore path — the web docapi
// setDocument body shape): POST {docstore}/project/{pid}/doc/{did}
//   {"lines": [...], "version": <room head version>, "ranges": <unchanged>}
//   → {"modified": bool, "rev": n}
//
// version = the room's HEAD version: monotone within a document's room,
// satisfying the docstore stale-reject guard (doc.version > sent →
// ErrVersionDown) across a room's whole debounced sequence.
//
// ranges are READ-BACK from the docstore and sent unchanged: the D40
// review records live in the room, but any OT-era comment ranges stored
// in the docstore must not be clobbered (docstore updateDoc rewrites
// ranges when they differ — see routes_docs.go tryUpdateDoc).

package collab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/reearth/ygo/persistence"
)

// DocFetcher — the docstore document surface WriteBack needs (GET current
// view + POST write). Production: the docstore HTTP API (same base/auth
// chain the seed source uses). Tests inject fakes.
type DocFetcher interface {
	// GetDoc — the document's current view; (ErrDocNotFound) when absent.
	GetDoc(ctx context.Context, projectID, docID string) (DocView, error)
	// PutDoc — write lines with the given version, preserving ranges.
	PutDoc(ctx context.Context, projectID, docID string, dv DocView) (DocView, error)
}

// DocView — the document's stored content fields (the docstore view subset).
type DocView struct {
	Lines   []string
	Version int
	Ranges  map[string]any
}

// ErrDocNotFound — the document is absent from the docstore (a PUT still
// creates it — the docstore write path treats a missing doc as an insert).
var ErrDocNotFound = errors.New("collab.writeback: docstore doc not found")

// RootDocFunc — project → its root-document id ("" when the project has
// none). Root rooms seed from the root doc, so the write must as well.
type RootDocFunc func(ctx context.Context, projectID string) (string, error)

// WriteBack — the room-head → docstore writer (one per service; all rooms
// share it; per-room debounce + in-flight serialization).
type WriteBack struct {
	Docs   DocFetcher
	Store  persistence.VersionedPersistence // the service's versioned room store
	RootDoc RootDocFunc                     // nil → root rooms are skipped (test mode)
	// Delay — debounce window after the last room update (default 1.5s;
	// 0 → immediate, for tests).
	Delay   time.Duration
	ErrSink func(err error)

	mu       sync.Mutex
	timers   map[string]*time.Timer
	inFlight map[string]bool
}

// NewWriteBack — construct (Delay default 1.5s).
func NewWriteBack(docs DocFetcher, store persistence.VersionedPersistence, root RootDocFunc) *WriteBack {
	return &WriteBack{
		Docs: docs, Store: store, RootDoc: root,
		Delay: 1500 * time.Millisecond,
		timers: map[string]*time.Timer{}, inFlight: map[string]bool{},
	}
}

// ---- production DocFetcher over the docstore HTTP API ----

type httpDocFetcher struct {
	Base string
	User string
	Pass string
	HTTP *http.Client
}

// NewHTTPDocFetcher wires the docstore fetcher (base = WEB_DOCSTORE_URL).
func NewHTTPDocFetcher(base, user, pass string, c *http.Client) DocFetcher {
	if c == nil {
		c = http.DefaultClient
	}
	return &httpDocFetcher{Base: strings.TrimSuffix(base, "/"), User: user, Pass: pass, HTTP: c}
}

func (f *httpDocFetcher) request(ctx context.Context, method, url string, body any) ([]byte, int, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		rdr = strings.NewReader(string(b))
	}
	up, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		up.Header.Set("Content-Type", "application/json")
	}
	if f.User != "" {
		up.SetBasicAuth(f.User, f.Pass)
	}
	resp, err := f.HTTP.Do(up)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, 0, err
	}
	return b, resp.StatusCode, nil
}

func (f *httpDocFetcher) GetDoc(ctx context.Context, pid, did string) (DocView, error) {
	b, code, err := f.request(ctx, http.MethodGet, f.Base+"/project/"+pid+"/doc/"+did, nil)
	if err != nil {
		return DocView{}, err
	}
	if code == http.StatusNotFound {
		return DocView{}, ErrDocNotFound
	}
	if code != http.StatusOK {
		return DocView{}, fmt.Errorf("collab.writeback: docstore GET %d", code)
	}
	var v struct {
		Lines   []string       `json:"lines"`
		Version *int           `json:"version"`
		Ranges  map[string]any `json:"ranges"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return DocView{}, err
	}
	dv := DocView{Lines: v.Lines, Ranges: v.Ranges}
	if v.Version != nil {
		dv.Version = *v.Version
	}
	return dv, nil
}

func (f *httpDocFetcher) PutDoc(ctx context.Context, pid, did string, dv DocView) (DocView, error) {
	ranges := dv.Ranges
	if ranges == nil {
		ranges = map[string]any{}
	}
	body := map[string]any{"lines": dv.Lines, "version": dv.Version, "ranges": ranges}
	_, code, err := f.request(ctx, http.MethodPost, f.Base+"/project/"+pid+"/doc/"+did, body)
	if err != nil {
		return dv, err
	}
	if code != http.StatusOK {
		return dv, fmt.Errorf("collab.writeback: docstore POST %d", code)
	}
	return dv, nil
}

// ---- scheduling ----

// Schedule — debounce a write for the room (replaces any pending timer).
// Must be called with a LIVE context-independent path: the timer callback
// runs on context.Background() (room unloads outlive request contexts).
func (w *WriteBack) Schedule(ctx context.Context, room string) {
	if w == nil || w.Docs == nil {
		return
	}
	w.mu.Lock()
	if w.inFlight[room] {
		w.mu.Unlock()
		return
	}
	if prev := w.timers[room]; prev != nil {
		prev.Stop()
	}
	delay := w.Delay
	w.timers[room] = time.AfterFunc(delay, func() {
		w.mu.Lock()
		delete(w.timers, room)
		w.mu.Unlock()
		w.writeNow(context.Background(), room)
	})
	w.mu.Unlock()
}

// FlushRoom — write the room head NOW (room unload / deterministic tests).
func (w *WriteBack) FlushRoom(ctx context.Context, room string) {
	if w == nil || w.Docs == nil {
		return
	}
	w.mu.Lock()
	if prev := w.timers[room]; prev != nil {
		prev.Stop()
		delete(w.timers, room)
	}
	w.mu.Unlock()
	w.writeNow(ctx, room)
}

// FlushAll — flush every pending room (service shutdown; best-effort).
func (w *WriteBack) FlushAll(ctx context.Context, rooms []string) {
	for _, room := range rooms {
		w.FlushRoom(ctx, room)
	}
}

func (w *WriteBack) writeNow(ctx context.Context, room string) {
	w.mu.Lock()
	if w.inFlight[room] {
		w.mu.Unlock()
		return
	}
	w.inFlight[room] = true
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		delete(w.inFlight, room)
		w.mu.Unlock()
	}()

	if err := w.write(ctx, room); err != nil && w.ErrSink != nil {
		w.ErrSink(err)
	}
}

func (w *WriteBack) write(ctx context.Context, room string) error {
	pid := RoomProject(room)
	did := RoomDoc(room)
	if pid == "" {
		return nil // not a service room name — nothing to do
	}
	if did == "" {
		// Root room → the project's root document ("": honest skip).
		if w.RootDoc == nil {
			return nil
		}
		var rerr error
		did, rerr = w.RootDoc(ctx, pid)
		if rerr != nil || did == "" {
			return nil // no root doc → nothing to persist
		}
	}
	text, version, err := HeadText(ctx, w.Store, room)
	if err != nil {
		return fmt.Errorf("collab.writeback: %w", err)
	}
	if version == 0 {
		return nil // no versions yet (seed not persisted) — nothing to write
	}
	dv := DocView{Lines: strings.Split(text, "\n"), Version: int(version), Ranges: map[string]any{}}
	got, gerr := w.Docs.GetDoc(ctx, pid, did)
	if gerr != nil && !errors.Is(gerr, ErrDocNotFound) {
		return fmt.Errorf("collab.writeback: docstore read: %w", gerr)
	}
	if gerr == nil && got.Ranges != nil {
		dv.Ranges = got.Ranges // preserve stored ranges (never clobber)
	}
	if _, perr := w.Docs.PutDoc(ctx, pid, did, dv); perr != nil {
		return fmt.Errorf("collab.writeback: docstore write: %w", perr)
	}
	return nil
}

// WriteBackLog — the VersionedPersistence decorator: every successful
// AppendUpdate schedules a debounced write-through (fail-soft; the CRDT
// append is NEVER blocked by write-back).
type WriteBackLog struct {
	Inner   persistence.VersionedPersistence
	WriteBack *WriteBack
}

var _ persistence.VersionedPersistence = (*WriteBackLog)(nil)

func (a *WriteBackLog) AppendUpdate(ctx context.Context, room string, update []byte) (persistence.Version, error) {
	v, err := a.Inner.AppendUpdate(ctx, room, update)
	if err == nil && a.WriteBack != nil {
		a.WriteBack.Schedule(ctx, room) // fail-soft inside (ErrSink)
	}
	return v, err
}

func (a *WriteBackLog) Load(ctx context.Context, room string) (persistence.LoadResult, error) {
	return a.Inner.Load(ctx, room)
}
func (a *WriteBackLog) ListVersions(ctx context.Context, room string) ([]persistence.VersionMeta, error) {
	return a.Inner.ListVersions(ctx, room)
}
func (a *WriteBackLog) GetUpdate(ctx context.Context, room string, v persistence.Version) ([]byte, persistence.VersionMeta, bool, error) {
	return a.Inner.GetUpdate(ctx, room, v)
}
func (a *WriteBackLog) MaterializeAt(ctx context.Context, room string, v persistence.Version) ([]byte, error) {
	return a.Inner.MaterializeAt(ctx, room, v)
}
func (a *WriteBackLog) CaptureSnapshot(ctx context.Context, room, name string, state []byte) (persistence.Version, error) {
	return a.Inner.CaptureSnapshot(ctx, room, name, state)
}
func (a *WriteBackLog) RestoreSnapshot(ctx context.Context, room, name string) ([]byte, persistence.Version, bool, error) {
	return a.Inner.RestoreSnapshot(ctx, room, name)
}
func (a *WriteBackLog) PruneAfter(ctx context.Context, room string, target persistence.Version, rolledBack []byte) error {
	return a.Inner.PruneAfter(ctx, room, target, rolledBack)
}
func (a *WriteBackLog) Compact(ctx context.Context, room string, keep int) (int, error) {
	return a.Inner.Compact(ctx, room, keep)
}
func (a *WriteBackLog) Delete(ctx context.Context, room string) error {
	return a.Inner.Delete(ctx, room)
}

// WrapWriteBack — idempotent WriteBackLog wrap (returns the store as-is
// when the write-back is nil).
func WrapWriteBack(Inner persistence.VersionedPersistence, wb *WriteBack) persistence.VersionedPersistence {
	if wb == nil {
		return Inner
	}
	if wl, ok := Inner.(*WriteBackLog); ok {
		wl.WriteBack = wb
		return wl
	}
	return &WriteBackLog{Inner: Inner, WriteBack: wb}
}
