// Package chunkstore ports storage/lib/chunk_store/index.js + the mongo
// backend (mongo.js) state machine against hermetic in-memory fakes.
//
// Backend model (mongo.js, one record per chunk):
//
//	{_id, projectId, startVersion, endVersion, endTimestamp, state}
//
// state lifecycle: "pending" (inserted before upload) -> "active"
// (confirmCreate/confirmUpdate) ; "closed" keeps the record readable
// (confirmCreate over an old chunk) ; "deleted" (deleteActiveChunk /
// deleteChunk / deleteProjectChunks).
//
// Query semantics (locked against mongo.js):
//
//	getLatestChunk         sort startVersion desc, first state in {active,closed}
//	getChunkForVersion     state in {active,closed}, startVersion<=v AND
//	                       endVersion>=v, sort startVersion (preferNewer ? desc : asc)
//	getChunkForTimestamp   state in {active,closed}, endTimestamp>=ts,
//	                       sort startVersion asc (timestamps go up with version)
//
// The raw chunk histories are NOT held in this package: Upload stores them
// through the injected *historystore.HistoryStore exactly like Node
// `uploadChunk` (history.history.store(blobStore) -> backend.insertPendingChunk
// -> historyStore.storeRaw) and loaders fetch them through LoadRaw. Chunk ids
// are incrementing 24-hex Mongo-style strings ("1","2",...) so they flow
// through projectkey.Pad ("000000001") and the object-persistor key shape
// identically to production.
package chunkstore

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"history-v1/internal/core"
	"history-v1/service/historystore"
)

// --- Errors (mirror Node OError classes; HTTP mapping lives in the API layer) ---

// AlreadyInitialized — Node `new AlreadyInitialized(projectId)`.
type AlreadyInitialized struct{ ProjectID string }

func (e *AlreadyInitialized) Error() string {
	return fmt.Sprintf("Project is already initialized: %s", e.ProjectID)
}

// ChunkVersionConflictError — Node `new ChunkVersionConflictError(msg, info)`.
// info = { projectId, expected: chunk, actual: chunk } (the offending chunk
// is carried in `Actual` when it is a chunk-id conflict).
type ChunkVersionConflictError struct {
	Msg       string
	ProjectID string
	Expected  int
	Actual    string // the offending chunk id
}

func (e *ChunkVersionConflictError) Error() string { return e.Msg }

// VersionOutOfBoundsError — Node chunk_store errors (getChangesSinceVersion).
type VersionOutOfBoundsError struct{ Msg string }

func (e *VersionOutOfBoundsError) Error() string { return e.Msg }

// BackendError — generic OError fallback ("target project is not initialized
// yet", "pending chunk not found", ...).
type BackendError struct{ Msg string }

func (e *BackendError) Error() string { return e.Msg }

// --- Metadata record (Node chunkFromRecord) ---

// ChunkMetadata — what every reader returns for a record.
type ChunkMetadata struct {
	ID           string
	StartVersion int
	EndVersion   int
	EndTimestamp time.Time
}

// --- Store ---

type chunkRec struct {
	id           string
	startVersion int
	endVersion   int
	endTimestamp time.Time
	state        string // pending | active | closed | deleted
	updatedAt    time.Time
}

func (r chunkRec) meta() ChunkMetadata {
	return ChunkMetadata{ID: r.id, StartVersion: r.startVersion, EndVersion: r.endVersion, EndTimestamp: r.endTimestamp}
}

// Store — in-memory chunk store sharing its raw-history HistoryStore.
type Store struct {
	mu sync.RWMutex
	hs *historystore.HistoryStore
	// blobs resolves the per-project blob store (Node: every chunk-store
	// operation builds `new BlobStore(projectId)` — the blob namespace is
	// keyed by project, so uploads must land in the same namespace the
	// persist/set-content paths read from).
	blobs func(projectID string) core.BlobStoreI
	ids   map[string][]string // projectId -> chunk ids (insertion order)
	rec   map[string]map[string]*chunkRec
	seq   int
}

// New creates the chunk store on top of the raw-history store and the
// content-addressed blob store, resolving blobs per project like Node.
func New(hs *historystore.HistoryStore, blobs func(projectID string) core.BlobStoreI) *Store {
	return &Store{
		hs:    hs,
		blobs: blobs,
		ids:   map[string][]string{},
		rec:   map[string]map[string]*chunkRec{},
		seq:   0,
	}
}

// nextID — incrementing Mongo-style ids: "1","2",... (24 chars, zero padded).
func (s *Store) nextID() string {
	s.seq++
	return fmt.Sprintf("%024d", s.seq)
}

func (s *Store) orderedActiveOrClosed(projectID string) []*chunkRec {
	var out []*chunkRec
	for _, id := range s.ids[projectID] {
		r := s.rec[projectID][id]
		if r == nil {
			continue
		}
		if r.state == "active" || r.state == "closed" {
			out = append(out, r)
		}
	}
	return out
}

// getLatestChunkLocked — Node backend.getLatestChunk (sort startVersion desc).
func (s *Store) getLatestChunkLocked(projectID string) (*chunkRec, error) {
	ordered := s.orderedActiveOrClosed(projectID)
	if len(ordered) == 0 {
		return nil, &core.ChunkNotFoundError{ProjectID: projectID}
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].startVersion > ordered[j].startVersion })
	return ordered[0], nil
}

// getChunkForVersionLocked — Node backend.getChunkForVersion:
//
//	state in {active,closed}, startVersion <= version AND endVersion >=
//	version, sort startVersion (preferNewer ? desc : asc).
func (s *Store) getChunkForVersionLocked(projectID string, version int, preferNewer bool) (*chunkRec, error) {
	var candidates []*chunkRec
	for _, id := range s.ids[projectID] {
		r := s.rec[projectID][id]
		if r == nil || (r.state != "active" && r.state != "closed") {
			continue
		}
		if r.startVersion <= version && r.endVersion >= version {
			candidates = append(candidates, r)
		}
	}
	if len(candidates) == 0 {
		return nil, &core.ChunkVersionNotFoundError{ProjectID: projectID, Version: version}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if preferNewer {
			return candidates[i].startVersion > candidates[j].startVersion
		}
		return candidates[i].startVersion < candidates[j].startVersion
	})
	if preferNewer {
		return candidates[len(candidates)-1], nil
	}
	return candidates[0], nil
}

// --- Initialize (Node initializeProject) ---

// Initialize — Node initializeProject(pid, snapshot):
//
//	latest = backend.getLatestChunk(pid); throw AlreadyInitialized when present
//	history = new History(snapshot ?? empty, []); chunk = new Chunk(history, 0)
//	create(pid, chunk)
//
// Returns the project's end version (0 for a fresh project).
func (s *Store) Initialize(projectID string, snapshot *core.Snapshot) (endVersion int, err error) {
	if snapshot == nil {
		// Node initializeProject: `new Snapshot()` — no timestamp (and no
		// projectVersion / v2DocVersions). The wire raw omits those keys.
		snapshot = core.NewSnapshot(core.NewFileMap(), "", nil, time.Time{})
	}
	// Node initializeProject: throw AlreadyInitialized when a chunk exists.
	s.mu.Lock()
	_, latestErr := s.getLatestChunkLocked(projectID)
	s.mu.Unlock()
	if latestErr == nil {
		return 0, &AlreadyInitialized{ProjectID: projectID}
	}
	// create (startVersion 0) — upload pending + activate.
	if _, err := s.Create(projectID, core.NewChunk(core.NewHistory(snapshot, nil), 0), time.Time{}); err != nil {
		return 0, err
	}
	return 0, nil
}

// uploadLocked — Node uploadChunk, with the record kept pending (confirm is
// handled by Create/Update):
//
//	raw = chunk.getHistory().store(blobStore)
//	chunkId = backend.insertPendingChunk(pid, chunk)   // state "pending"
//	historyStore.storeRaw(pid, chunkId, raw)
func (s *Store) uploadLocked(projectID string, chunk *core.Chunk, oldChunkID string) (string, error) {
	raw, err := chunk.GetHistory().Store(s.blobs(projectID))
	if err != nil {
		return "", err
	}
	id := s.nextID()
	rec := &chunkRec{
		id:           id,
		startVersion: chunk.GetStartVersion(),
		endVersion:   chunk.GetEndVersion(),
		endTimestamp: chunk.GetEndTimestamp(),
		state:        "pending",
		updatedAt:    time.Now(),
	}
	m := s.rec[projectID]
	if m == nil {
		m = map[string]*chunkRec{}
		s.rec[projectID] = m
	}
	m[id] = rec
	s.ids[projectID] = append(s.ids[projectID], id)
	if err := s.hs.StoreRaw(projectID, id, raw); err != nil {
		return "", err
	}
	_ = oldChunkID
	return id, nil
}

// --- Create (Node chunk_store.create) ---

// Create — Node create(pid, chunk, earliestChangeTimestamp?):
//
//	if chunkStart > 0:
//		oldChunk = getChunkForVersion(pid, chunkStart)
//		ChunkVersionConflictError("unexpected end version on chunk to be updated")
//		if oldChunk.endVersion != chunkStart
//	uploadChunk;  confirmCreate: closeChunk(old, active->closed)
//	+ activateChunk(new, pending->active)
//
// earliestChangeTs is accepted and ignored (Node writes the project's backup
// record; the fake has no backup store: `config.has('backupStore')` is false).
func (s *Store) Create(projectID string, chunk *core.Chunk, earliestChangeTs time.Time) (string, error) {
	start := chunk.GetStartVersion()
	var oldID string
	if start > 0 {
		s.mu.Lock()
		old, err := s.getChunkForVersionLocked(projectID, start, false)
		if err != nil {
			s.mu.Unlock()
			return "", err
		}
		if old.endVersion != chunk.GetStartVersion() {
			s.mu.Unlock()
			return "", &ChunkVersionConflictError{
				Msg:       "unexpected end version on chunk to be updated",
				ProjectID: projectID,
				Expected:  chunk.GetStartVersion(),
				Actual:    old.id,
			}
		}
		oldID = old.id
		s.mu.Unlock()
	}

	s.mu.Lock()
	id, err := s.uploadLocked(projectID, chunk, oldID)
	if err != nil {
		s.mu.Unlock()
		return "", err
	}
	// confirmCreate.
	var actErr error
	if oldID != "" {
		// closeChunk: active -> closed; unmatched -> ChunkVersionConflict.
		oldRec := s.rec[projectID][oldID]
		if oldRec == nil || oldRec.state != "active" {
			actErr = &ChunkVersionConflictError{Msg: "unable to close chunk", ProjectID: projectID, Actual: oldID}
		} else {
			oldRec.state = "closed"
		}
	}
	newRec := s.rec[projectID][id]
	if newRec == nil || newRec.state != "pending" {
		if actErr == nil {
			actErr = &BackendError{Msg: "pending chunk not found"}
		}
	} else {
		newRec.state = "active"
		newRec.updatedAt = time.Now()
	}
	s.mu.Unlock()
	_ = earliestChangeTs
	if actErr != nil {
		return "", actErr
	}
	return id, nil
}

// --- Update (Node chunk_store.update) ---

// Update — Node update(pid, newChunk, earliestChangeTimestamp?):
//
//	oldChunk = getChunkForVersion(pid, newChunk.startVersion, preferNewer: true)
//	ChunkVersionConflictError("unexpected start version on chunk to be updated")
//	  if oldChunk.startVersion != newChunk.startVersion
//	ChunkVersionConflictError("chunk update would decrease chunk version")
//	  if oldChunk.endVersion > newChunk.endVersion
//
//	uploadChunk;  confirmUpdate: deleteActiveChunk(old, active->deleted)
//	+ activateChunk(new, pending->active)
func (s *Store) Update(projectID string, newChunk *core.Chunk, earliestChangeTs time.Time) (string, error) {
	start := newChunk.GetStartVersion()
	s.mu.Lock()
	old, err := s.getChunkForVersionLocked(projectID, start, true)
	if err == nil {
		if old.startVersion != start {
			err = &ChunkVersionConflictError{
				Msg:       "unexpected start version on chunk to be updated",
				ProjectID: projectID,
				Expected:  start,
				Actual:    old.id,
			}
		} else if old.endVersion > newChunk.GetEndVersion() {
			err = &ChunkVersionConflictError{
				Msg:       "chunk update would decrease chunk version",
				ProjectID: projectID,
				Expected:  old.endVersion,
				Actual:    old.id,
			}
		}
	}
	s.mu.Unlock()
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	id, upErr := s.uploadLocked(projectID, newChunk, old.id)
	var delErr, actErr error
	if upErr == nil {
		// confirmUpdate.
		oldRec := s.rec[projectID][old.id]
		if oldRec == nil || oldRec.state != "active" {
			delErr = &ChunkVersionConflictError{Msg: "unable to delete active chunk", ProjectID: projectID, Actual: old.id}
		} else {
			oldRec.state = "deleted"
			oldRec.updatedAt = time.Now()
		}
		newRec := s.rec[projectID][id]
		if newRec == nil || newRec.state != "pending" {
			actErr = &BackendError{Msg: "pending chunk not found"}
		} else {
			newRec.state = "active"
			newRec.updatedAt = time.Now()
		}
	}
	s.mu.Unlock()
	_ = earliestChangeTs
	if upErr != nil {
		return "", upErr
	}
	if delErr != nil {
		return "", delErr
	}
	if actErr != nil {
		return "", actErr
	}
	return id, nil
}

// --- Load (Node loadLatest / loadAtVersion) ---

// GetLatestChunkMetadata — Node getLatestChunkMetadata (Chunk.NotFoundError
// when absent).
func (s *Store) GetLatestChunkMetadata(projectID string) (ChunkMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, err := s.getLatestChunkLocked(projectID)
	if err != nil {
		return ChunkMetadata{}, err
	}
	return rec.meta(), nil
}

// LoadLatest — Node loadLatest: the latest active/closed chunk with its
// history (persistedOnly semantics only; this port has no non-persisted
// buffer). Returns the parsed chunk (record-driven startVersion).
func (s *Store) LoadLatest(projectID string) (*core.Chunk, error) {
	s.mu.RLock()
	rec, err := s.getLatestChunkLocked(projectID)
	s.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	raw, err := s.hs.LoadRaw(projectID, rec.id)
	if err != nil {
		return nil, err
	}
	return core.NewChunk(core.HistoryFromRaw(raw), rec.startVersion), nil
}

// LoadAtVersion — Node loadAtVersion (persistedOnly semantics only). On
// version not covered -> Chunk.VersionNotFoundError. When preferNewer the
// newer chunk in a boundary match wins (no buffer, so the requested version
// is clamped implicitly).
func (s *Store) LoadAtVersion(projectID string, version int, preferNewer bool) (*core.Chunk, error) {
	s.mu.RLock()
	rec, err := s.getChunkForVersionLocked(projectID, version, preferNewer)
	s.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	raw, err := s.hs.LoadRaw(projectID, rec.id)
	if err != nil {
		return nil, err
	}
	return core.NewChunk(core.HistoryFromRaw(raw), rec.startVersion), nil
}

// GetChunkForVersionMetadata — Node getChunkMetadataForVersion.
func (s *Store) GetChunkForVersionMetadata(projectID string, version int, preferNewer bool) (ChunkMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, err := s.getChunkForVersionLocked(projectID, version, preferNewer)
	if err != nil {
		return ChunkMetadata{}, err
	}
	return rec.meta(), nil
}

// --- Changes since version (Node getChangesSinceVersion, level 0) ---

// ChangesSince — Node chunk_store.getChangesSinceVersion(pid, since, preferNewer):
//
//	chunk = loadAtVersion(pid, since, preferNewer)  (preferNewer: true by default)
//	if since < chunk.startVersion -> VersionOutOfBoundsError
//	changes = chunk.changes[since - chunk.startVersion:]
//	hasMore = latest.endVersion > chunk.endVersion
func (s *Store) ChangesSince(projectID string, since int, preferNewer bool) (changes []*core.Change, hasMore bool, err error) {
	if since < 0 {
		return nil, false, &VersionOutOfBoundsError{Msg: "Chunk does not include since version"}
	}
	chunk, err := s.LoadAtVersion(projectID, since, preferNewer)
	if err != nil {
		return nil, false, err
	}
	if since < chunk.GetStartVersion() {
		return nil, false, &VersionOutOfBoundsError{Msg: "Chunk does not include since version"}
	}
	changes = chunk.GetChanges()[since-chunk.GetStartVersion():]
	latest, err := s.GetLatestChunkMetadata(projectID)
	if err != nil {
		return nil, false, err
	}
	hasMore = latest.EndVersion > chunk.GetEndVersion()
	return changes, hasMore, nil
}

// getChunkForTimestampLocked — Node backend.getChunkForTimestamp:
//
//	state in {active,closed}, endTimestamp >= timestamp,
//	sort { startVersion: 1 }, take the first; when none, fall back to the
//	latest chunk; when the project is empty,
//	*core.ChunkBeforeTimestampNotFoundError.
func (s *Store) getChunkForTimestampLocked(projectID string, ts time.Time) (*chunkRec, error) {
	for _, id := range s.ids[projectID] {
		r := s.rec[projectID][id]
		if r == nil || (r.state != "active" && r.state != "closed") {
			continue
		}
		if !r.endTimestamp.Before(ts) {
			return r, nil
		}
	}
	// No chunk has modifications after the given timestamp: fall back to
	// the latest (or error when the project has no chunks at all).
	return s.getLatestChunkLocked(projectID)
}

// GetChunkForTimestamp — public facade over getChunkForTimestampLocked
// (API getHistoryBefore uses loadAtTimestamp, which lazy-loads the
// snapshot from blobs; the record is returned so the API can recompute
// the chunk start version the way Node loadAtTimestamp does).
func (s *Store) GetChunkForTimestamp(projectID string, ts time.Time) (*chunkRec, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getChunkForTimestampLocked(projectID, ts)
}

// LoadAtTimestamp — Node chunk_store.loadAtTimestamp(pid, timestamp):
//
//	chunkRecord = backend.getChunkForTimestamp(pid, timestamp)
//	history = History.fromRaw(historyStore.loadRaw(pid, record.id))
//	startVersion = record.endVersion - history.countChanges()
//	(persistedOnly is irrelevant in the hermetic port: the redis
//	extension is empty)
//	chunk.loadFiles('lazy'); return new Chunk(history, startVersion)
//
// The redis getChunkExtension extension is empty by construction in the
// Go port, so the two opts variants behave identically.
func (s *Store) LoadAtTimestamp(projectID string, timestamp time.Time) (*core.Chunk, error) {
	s.mu.RLock()
	rec, err := s.getChunkForTimestampLocked(projectID, timestamp)
	if err != nil {
		s.mu.RUnlock()
		return nil, err
	}
	s.mu.RUnlock()

	history, _, err := s.LoadHistory(projectID, rec.id)
	if err != nil {
		return nil, err
	}
	startVersion := rec.endVersion - history.CountChanges()
	chunk := core.NewChunk(history, startVersion)
	if err := chunk.LoadFiles("lazy", s.blobs(projectID)); err != nil {
		return nil, &BackendError{Msg: "loadAtTimestamp: " + err.Error()}
	}
	return chunk, nil
}

// --- Clone / delete (Node cloneProject / destroy / deleteProjectChunks) ---

// Clone — Node cloneProject(sourcePid, targetPid):
//
//	latest(target); error "target project is not initialized yet" when absent
//	AlreadyInitialized when latest.endVersion > 0
//	deleteChunk(target, latest.id)
//	backend.clone: copy source active/closed records under new ids
//	historyStore.cloneChunk(sourcePid, targetPid, oldID, newID) per pair
func (s *Store) Clone(sourceID, targetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	latest, err := s.getLatestChunkLocked(targetID)
	if err != nil {
		return &BackendError{Msg: "target project is not initialized yet"}
	}
	if latest.endVersion > 0 {
		return &AlreadyInitialized{ProjectID: targetID}
	}
	// deleteChunk(target, latest.id) — any state, mark deleted.
	latest.state = "deleted"
	latest.updatedAt = time.Now()
	// backend.clone: copy source active/closed records with new ids.
	for _, id := range s.ids[sourceID] {
		rec := s.rec[sourceID][id]
		if rec == nil || (rec.state != "active" && rec.state != "closed") {
			continue
		}
		newID := s.nextID()
		s.rec[targetID][newID] = &chunkRec{
			id:           newID,
			startVersion: rec.startVersion,
			endVersion:   rec.endVersion,
			endTimestamp: rec.endTimestamp,
			state:        rec.state,
			updatedAt:    time.Now(),
		}
		s.ids[targetID] = append(s.ids[targetID], newID)
		// historyStore.cloneChunk(sourcePid, targetPid, oldID, newID)
		if err := s.hs.CloneChunk(sourceID, id, targetID, newID); err != nil {
			return err
		}
	}
	return nil
}

// LoadHistory — fetch + parse the raw history of (projectId, chunkId) with
// the record (for the record-driven startVersion).
func (s *Store) LoadHistory(projectID, chunkID string) (history *core.History, record ChunkMetadata, err error) {
	s.mu.RLock()
	rec, ok := s.rec[projectID][chunkID]
	s.mu.RUnlock()
	if !ok {
		return nil, ChunkMetadata{}, &BackendError{Msg: fmt.Sprintf("no chunk record %s/%s", projectID, chunkID)}
	}
	raw, err := s.hs.LoadRaw(projectID, chunkID)
	if err != nil {
		return nil, ChunkMetadata{}, err
	}
	return core.HistoryFromRaw(raw), rec.meta(), nil
}

// DeleteProjectChunks — Node backend.deleteProjectChunks: all project records
// with state in {active,closed} -> deleted.
func (s *Store) DeleteProjectChunks(projectID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range s.ids[projectID] {
		r := s.rec[projectID][id]
		if r == nil {
			continue
		}
		if r.state == "active" || r.state == "closed" {
			r.state = "deleted"
			r.updatedAt = time.Now()
		}
	}
	return nil
}
