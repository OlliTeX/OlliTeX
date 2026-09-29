package historyv1

// ChunkStores 1:1 with storage/lib/chunk_store/{index,mongo,postgres}.js:
// chunk metadata by project-id shape (24-hex → Mongo `chunks`; numeric →
// Postgres `chunks`) + the public chunk-store API (initialize/load/update/
// changes/delete/clone) on top of the S3 HistoryStore.
//
// The redis change buffer (storage/lib/chunk_store/redis.js) is the DU-era
// write seam; in this stack nothing writes to the V1 buffer (DU retired, see
// D41), so the Extender returns 'not_found' — the exact wire behavior of an
// empty buffer (redisBackend.getChangesSinceVersion → status not_found
// falls through to the chunk approach).

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"ollitex/go/libraries/otc"
)

// ChunkMeta — Node chunkFromRecord.
type ChunkMeta struct {
	ID           string
	StartVersion int
	EndVersion   int
	EndTimestamp *time.Time
}

// ChunkVersionConflictError — chunk_store/errors.js (OError).
type ChunkVersionConflictError struct {
	Message string
	Info    map[string]any
}

func (e *ChunkVersionConflictError) Error() string { return "ChunkVersionConflictError: " + e.Message }

func newChunkConflict(message string, info map[string]any) error {
	return &ChunkVersionConflictError{Message: message, Info: info}
}

// ChunkBackend — the per-store interface (Node mongo.js / postgres.js exports).
type ChunkBackend interface {
	GetLatestChunk(ctx context.Context, projectID string, readOnly bool) (*ChunkMeta, error)
	GetChunkForVersion(ctx context.Context, projectID string, version int, preferNewer bool) (*ChunkMeta, error)
	GetChunkForTimestamp(ctx context.Context, projectID string, ts time.Time) (*ChunkMeta, error)
	GetProjectChunkIDs(ctx context.Context, projectID string) ([]string, error)
	GetProjectChunks(ctx context.Context, projectID string) ([]ChunkMeta, error)
	Clone(ctx context.Context, srcID, dstID string) (map[string]string, error)
	InsertPendingChunk(ctx context.Context, projectID string, c *otc.Chunk) (string, error)
	ConfirmCreate(ctx context.Context, projectID string, c *otc.Chunk, chunkID string, oldChunkID *string, earliest *time.Time) error
	ConfirmUpdate(ctx context.Context, projectID, oldChunkID string, newChunk *otc.Chunk, newChunkID string, earliest *time.Time) error
	DeleteChunk(ctx context.Context, projectID, chunkID string) error
	DeleteProjectChunks(ctx context.Context, projectID string) error
}

// Extender — the redis buffer seam (Node redisBackend.get*Changes).
type Extender interface {
	// GetNonPersistedChanges — (projectId, baseVersion) → changes, or a
	// VersionOutOfBoundsError-like signal (empty slice when out of bounds).
	GetNonPersistedChanges(ctx context.Context, projectID string, baseVersion int) ([]*otc.Change, error)
	// GetChangesSinceVersion — status 'ok' with changes, 'not_found', or
	// 'out_of_bounds'.
	GetChangesSinceVersion(ctx context.Context, projectID string, version int) (status string, changes []*otc.Change)
}

// VersionOutOfBoundsError — chunk_store/errors.js.
type VersionOutOfBoundsError struct {
	Message string
	Info    map[string]any
}

func (e *VersionOutOfBoundsError) Error() string { return "VersionOutOfBoundsError: " + e.Message }

var _ = new(VersionOutOfBoundsError)

// NullExtender — empty-buffer behavior (see package doc): Node redis lookups
// miss → 'not_found' → chunk fallthrough; GetNonPersistedChanges → [].
type NullExtender struct{}

func NewMongoBuffer() *NullExtender { return &NullExtender{} }

func (NullExtender) GetNonPersistedChanges(ctx context.Context, projectID string, baseVersion int) ([]*otc.Change, error) {
	return nil, nil
}

func (NullExtender) GetChangesSinceVersion(ctx context.Context, projectID string, version int) (string, []*otc.Change) {
	return "not_found", nil
}

// AlreadyInitialized — chunk_store/index.js (OError 'Project is already initialized').
type AlreadyInitialized struct{ ProjectID string }

func (e *AlreadyInitialized) Error() string { return "OError: Project is already initialized" }

// ChunkStores — the routed chunk store (Node chunk_store/index.js surface).
type ChunkStores struct {
	Cfg   any
	Mongo *mongo.Database
	Hist  *HistoryStore
	Blob  *BlobStores
	Ext   Extender
	PG    ChunkBackend // numeric-id backend (PG18 seam) — nil until the PG slice
}

func NewChunkStores(cfg any, mongo *mongo.Database, hist *HistoryStore, ext Extender) *ChunkStores {
	return &ChunkStores{Cfg: cfg, Mongo: mongo, Hist: hist, Blob: &BlobStores{db: mongo}, Ext: ext}
}

// getBackend — Node getBackend (numeric → postgres; 24-hex → mongo).
func (cs *ChunkStores) getBackend(projectID string) (ChunkBackend, error) {
	if numericID(projectID) {
		if cs.PG == nil {
			return nil, errors.New("numeric project id " + projectID + " requires the Postgres chunk backend (not attached)")
		}
		return cs.PG, nil
	}
	if !isMongoID(projectID) {
		return nil, errors.New("bad projectId: " + projectID)
	}
	return &mongoChunkBackend{db: cs.Mongo}, nil
}

// Initialize 1:1 (index.js initializeProject).
func (cs *ChunkStores) Initialize(ctx context.Context, projectID string) (string, error) {
	if projectID == "" {
		oid := bson.NewObjectID().Hex()
		projectID = oid
	}
	// Node: assert.projectId — both shapes accepted (mongo id asserted inside
	// the mongo backend; numeric in the PG backend).
	bs := cs.Blob.ForProject(projectID)
	if err := bs.Initialize(ctx); err != nil {
		return "", err
	}
	backend, err := cs.getBackend(projectID)
	if err != nil {
		return "", err
	}
	if rec, err := backend.GetLatestChunk(ctx, projectID, false); err != nil {
		return "", err
	} else if rec != nil {
		return "", &AlreadyInitialized{ProjectID: projectID}
	}
	snapshot := otc.NewSnapshot(nil, nil, nil, nil)
	history := otc.NewHistory(snapshot, nil)
	chunk := otc.NewChunk(history, 0)
	if err := cs.create(ctx, projectID, chunk, nil); err != nil {
		return "", err
	}
	return projectID, nil
}

// LoadLatest 1:1 (index.js loadLatest).
func (cs *ChunkStores) LoadLatest(ctx context.Context, projectID string, persistedOnly bool) (*otc.Chunk, error) {
	meta, err := cs.getLatestChunkMetadata(ctx, projectID)
	if err != nil {
		return nil, err
	}
	raw, err := cs.Hist.LoadRaw(ctx, projectID, meta.ID)
	if err != nil {
		return nil, err
	}
	history, err := otc.HistoryFromRaw(raw)
	if err != nil {
		return nil, err
	}
	if !persistedOnly {
		changes, err := cs.Ext.GetNonPersistedChanges(ctx, projectID, meta.EndVersion)
		if err != nil {
			var vob *VersionOutOfBoundsError
			if !errors.As(err, &vob) {
				return nil, err
			}
		} else if len(changes) > 0 {
			history.PushChanges(changes)
		}
	}
	bs := cs.Blob.ForProject(projectID)
	if err := history.LoadFiles(ctx, "lazy", otcBridge{bs: bs}); err != nil {
		return nil, err
	}
	return otc.NewChunk(history, meta.StartVersion), nil
}

// otcBridge adapts BlobStore to the otc.BlobStore interface (lazy file load).
type otcBridge struct{ bs *BlobStore }

func (o otcBridge) GetBlob(ctx context.Context, hash string) (*otc.Blob, error) {
	if b, err := o.bs.FindBlob(ctx, hash); err != nil {
		return nil, err
	} else if b != nil {
		return b, nil
	}
	return nil, otc.NewChunkNotPersistedError(o.bs.projectID)
}

func (o otcBridge) GetString(ctx context.Context, hash string) (string, error) {
	return o.bs.GetString(ctx, hash)
}
func (o otcBridge) GetObject(ctx context.Context, hash string) (map[string]any, error) {
	return o.bs.GetObject(ctx, hash)
}
func (o otcBridge) PutString(ctx context.Context, content string) (*otc.Blob, error) {
	return o.bs.PutString(ctx, content)
}
func (o otcBridge) PutObject(ctx context.Context, obj map[string]any) (*otc.Blob, error) {
	j, err := jsonMarshalBytes(obj)
	if err != nil {
		return nil, err
	}
	return o.bs.PutString(ctx, string(j))
}

func (cs *ChunkStores) getLatestChunkMetadata(ctx context.Context, projectID string) (*ChunkMeta, error) {
	backend, err := cs.getBackend(projectID)
	if err != nil {
		return nil, err
	}
	meta, err := backend.GetLatestChunk(ctx, projectID, false)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, &otc.ChunkNotFoundError{ProjectID: projectID}
	}
	return meta, nil
}

// LoadAtVersion 1:1 (index.js loadAtVersion, incl. the non-persisted check).
func (cs *ChunkStores) LoadAtVersion(ctx context.Context, projectID string, version int, persistedOnly, preferNewer bool) (*otc.Chunk, error) {
	backend, err := cs.getBackend(projectID)
	if err != nil {
		return nil, err
	}
	latest, err := cs.getLatestChunkMetadata(ctx, projectID)
	if err != nil {
		return nil, err
	}
	target := version
	if !persistedOnly && latest.EndVersion < version {
		target = latest.EndVersion
	}
	rec, err := backend.GetChunkForVersion(ctx, projectID, target, preferNewer)
	if err != nil {
		return nil, err
	}
	raw, err := cs.Hist.LoadRaw(ctx, projectID, rec.ID)
	if err != nil {
		return nil, err
	}
	history, err := otc.HistoryFromRaw(raw)
	if err != nil {
		return nil, err
	}
	if !persistedOnly {
		changes, err := cs.Ext.GetNonPersistedChanges(ctx, projectID, rec.EndVersion)
		if err != nil {
			var vob *VersionOutOfBoundsError
			if !errors.As(err, &vob) {
				return nil, err
			}
		} else if len(changes) > 0 {
			history.PushChanges(changes)
		}
	}
	if persistedOnly || version <= rec.EndVersion {
		// version inside the chunk (or persistedOnly)
	} else {
		return nil, otc.NewChunkVersionNotFoundError(projectID, itoaVersion(version))
	}
	if err := lazyLoadHistoryFiles(ctx, history, cs.Blob.ForProject(projectID)); err != nil {
		return nil, err
	}
	return otc.NewChunk(history, rec.StartVersion), nil
}

func lazyLoadHistoryFiles(ctx context.Context, history *otc.History, bs *BlobStore) error {
	return history.LoadFiles(ctx, "lazy", otcBridge{bs: bs})
}

// LoadAtTimestamp 1:1 (index.js loadAtTimestamp).
func (cs *ChunkStores) LoadAtTimestamp(ctx context.Context, projectID string, ts time.Time, persistedOnly bool) (*otc.Chunk, error) {
	backend, err := cs.getBackend(projectID)
	if err != nil {
		return nil, err
	}
	rec, err := backend.GetChunkForTimestamp(ctx, projectID, ts)
	if err != nil {
		return nil, err
	}
	raw, err := cs.Hist.LoadRaw(ctx, projectID, rec.ID)
	if err != nil {
		return nil, err
	}
	history, err := otc.HistoryFromRaw(raw)
	if err != nil {
		return nil, err
	}
	startVersion := rec.EndVersion - history.CountChanges()
	if !persistedOnly {
		changes, err := cs.Ext.GetNonPersistedChanges(ctx, projectID, rec.EndVersion)
		if err != nil {
			var vob *VersionOutOfBoundsError
			if !errors.As(err, &vob) {
				return nil, err
			}
		} else if len(changes) > 0 {
			history.PushChanges(changes)
		}
	}
	if err := lazyLoadHistoryFiles(ctx, history, cs.Blob.ForProject(projectID)); err != nil {
		return nil, err
	}
	return otc.NewChunk(history, startVersion), nil
}

// GetChangesSinceVersion 1:1 (redis first; chunk approach + hasMore 1:1).
func (cs *ChunkStores) GetChangesSinceVersion(ctx context.Context, projectID string, since int) (changes []*otc.Change, hasMore bool, err error) {
	if status, ch := cs.Ext.GetChangesSinceVersion(ctx, projectID, since); status == "ok" {
		return ch, false, nil
	}
	chunk, err := cs.LoadAtVersion(ctx, projectID, since, false, true)
	if err != nil {
		return nil, false, err
	}
	if since < chunk.GetStartVersion() {
		return nil, false, &VersionOutOfBoundsError{Message: "Chunk does not include since version", Info: map[string]any{"projectId": projectID, "since": since}}
	}
	all := chunk.GetHistory().GetChanges()
	off := since - chunk.GetStartVersion()
	if off < 0 {
		off = 0
	} else if off > len(all) {
		off = len(all)
	}
	latest, err := cs.getLatestChunkMetadata(ctx, projectID)
	if err != nil {
		return nil, false, err
	}
	return all[off:], latest.EndVersion > chunk.GetEndVersion(), nil
}

// Create 1:1 (index.js create — startVersion 0 in this stack; old-chunk
// bookkeeping preserved for parity).
func (cs *ChunkStores) create(ctx context.Context, projectID string, chunk *otc.Chunk, earliest *time.Time) error {
	backend, err := cs.getBackend(projectID)
	if err != nil {
		return err
	}
	chunkStart := chunk.GetStartVersion()
	var oldChunkID *string
	if chunkStart > 0 {
		old, err := backend.GetChunkForVersion(ctx, projectID, chunkStart, false)
		if err != nil {
			return err
		}
		if old.EndVersion != chunkStart {
			return newChunkConflict("unexpected end version on chunk to be updated", map[string]any{
				"projectId": projectID, "expectedVersion": chunkStart, "actualVersion": old.EndVersion,
			})
		}
		id := old.ID
		oldChunkID = &id
	}
	chunkID, err := cs.uploadChunk(ctx, projectID, chunk)
	if err != nil {
		return err
	}
	return backend.ConfirmCreate(ctx, projectID, chunk, chunkID, oldChunkID, earliest)
}

// uploadChunk 1:1 (index.js uploadChunk: history.store + insertPending +
// historyStore.storeRaw).
func (cs *ChunkStores) uploadChunk(ctx context.Context, projectID string, chunk *otc.Chunk) (string, error) {
	backend, err := cs.getBackend(projectID)
	if err != nil {
		return "", err
	}
	rawHistory, err := chunk.GetHistory().Store(ctx, otcBridge{bs: cs.Blob.ForProject(projectID)}, 4)
	if err != nil {
		return "", err
	}
	chunkID, err := backend.InsertPendingChunk(ctx, projectID, chunk)
	if err != nil {
		return "", err
	}
	if err := cs.Hist.StoreRaw(ctx, projectID, chunkID, rawHistory); err != nil {
		return "", err
	}
	return chunkID, nil
}

// DeleteProjectChunks 1:1.
func (cs *ChunkStores) DeleteProjectChunks(ctx context.Context, projectID string) error {
	backend, err := cs.getBackend(projectID)
	if err != nil {
		return err
	}
	return backend.DeleteProjectChunks(ctx, projectID)
}

// Clone 1:1 (index.js cloneProject, sans progress/abort signals).
func (cs *ChunkStores) Clone(ctx context.Context, srcID, dstID string) error {
	backend, err := cs.getBackend(dstID)
	if err != nil {
		return err
	}
	rec, err := backend.GetLatestChunk(ctx, dstID, false)
	if err != nil {
		return err
	}
	if rec == nil {
		return errors.New("OError: target project is not initialized yet")
	}
	if rec.EndVersion > 0 {
		return &AlreadyInitialized{ProjectID: dstID}
	}
	if err := backend.DeleteChunk(ctx, dstID, rec.ID); err != nil {
		return err
	}
	// blobs meta
	if _, err := cs.Blob.ForProject(dstID).Clone(ctx, srcID); err != nil {
		return err
	}
	// chunks meta + history objects
	chunkIDs, err := backend.Clone(ctx, srcID, dstID)
	if err != nil {
		return err
	}
	for src, dst := range chunkIDs {
		if err := cs.Hist.CloneChunk(ctx, srcID, src, dstID, dst); err != nil {
			return err
		}
	}
	return nil
}

// Update 1:1 (index.js update — DU-era write plane; provided for parity).
func (cs *ChunkStores) Update(ctx context.Context, projectID string, newChunk *otc.Chunk, earliest *time.Time) error {
	backend, err := cs.getBackend(projectID)
	if err != nil {
		return err
	}
	old, err := backend.GetChunkForVersion(ctx, projectID, newChunk.GetStartVersion(), true)
	if err != nil {
		return err
	}
	if old.StartVersion != newChunk.GetStartVersion() {
		return newChunkConflict("unexpected start version on chunk to be updated", map[string]any{
			"projectId": projectID, "expectedVersion": newChunk.GetStartVersion(), "actualVersion": old.StartVersion,
		})
	}
	if old.EndVersion > newChunk.GetEndVersion() {
		return newChunkConflict("chunk update would decrease chunk version", map[string]any{
			"projectId": projectID, "currentVersion": old.EndVersion, "newVersion": newChunk.GetEndVersion(),
		})
	}
	newID, err := cs.uploadChunk(ctx, projectID, newChunk)
	if err != nil {
		return err
	}
	return backend.ConfirmUpdate(ctx, projectID, old.ID, newChunk, newID, earliest)
}

// ---------- mongo backend (storage/lib/chunk_store/mongo.js 1:1) ----------

type mongoChunkBackend struct{ db *mongo.Database }

func (m *mongoChunkBackend) coll() *mongo.Collection { return m.db.Collection("chunks") }

func (rec mongoChunkRecord) toMeta() ChunkMeta {
	return ChunkMeta{
		ID:           rec.ID.Hex(),
		StartVersion: rec.StartVersion,
		EndVersion:   rec.EndVersion,
		EndTimestamp: rec.EndTimestamp,
	}
}

type mongoChunkRecord struct {
	ID           bson.ObjectID `bson:"_id"`
	ProjectID    bson.ObjectID `bson:"projectId"`
	StartVersion int           `bson:"startVersion"`
	EndVersion   int           `bson:"endVersion"`
	EndTimestamp *time.Time    `bson:"endTimestamp"`
	State        string        `bson:"state"`
	Updated      *time.Time    `bson:"updatedAt"`
}

func (m *mongoChunkBackend) GetLatestChunk(ctx context.Context, projectID string, readOnly bool) (*ChunkMeta, error) {
	oid, err := bson.ObjectIDFromHex(projectID)
	if err != nil {
		return nil, errors.New("bad projectId")
	}
	var rec mongoChunkRecord
	_ = readOnly // Node: secondaryPreferred read preference; single-node in this stack
	opts := options.FindOne().SetSort(bson.D{{Key: "startVersion", Value: -1}})
	if err := m.coll().FindOne(ctx, bson.D{
		{Key: "projectId", Value: oid},
		{Key: "state", Value: bson.D{{Key: "$in", Value: []string{"active", "closed"}}}},
	}, opts).Decode(&rec); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	meta := rec.toMeta()
	return &meta, nil
}

func (m *mongoChunkBackend) GetChunkForVersion(ctx context.Context, projectID string, version int, preferNewer bool) (*ChunkMeta, error) {
	oid, err := bson.ObjectIDFromHex(projectID)
	if err != nil {
		return nil, errors.New("bad projectId")
	}
	dir := 1
	if preferNewer {
		dir = -1
	}
	var rec mongoChunkRecord
	err = m.coll().FindOne(ctx, bson.D{
		{Key: "projectId", Value: oid},
		{Key: "state", Value: bson.D{{Key: "$in", Value: []string{"active", "closed"}}}},
		{Key: "startVersion", Value: bson.D{{Key: "$lte", Value: version}}},
		{Key: "endVersion", Value: bson.D{{Key: "$gte", Value: version}}},
	}, options.FindOne().SetSort(bson.D{{Key: "startVersion", Value: dir}})).Decode(&rec)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, otc.NewChunkVersionNotFoundError(projectID, itoaVersion(version))
		}
		return nil, err
	}
	meta := rec.toMeta()
	return &meta, nil
}

func (m *mongoChunkBackend) GetChunkForTimestamp(ctx context.Context, projectID string, ts time.Time) (*ChunkMeta, error) {
	oid, err := bson.ObjectIDFromHex(projectID)
	if err != nil {
		return nil, errors.New("bad projectId")
	}
	var rec mongoChunkRecord
	err = m.coll().FindOne(ctx, bson.D{
		{Key: "projectId", Value: oid},
		{Key: "state", Value: bson.D{{Key: "$in", Value: []string{"active", "closed"}}}},
		{Key: "endTimestamp", Value: bson.D{{Key: "$gte", Value: ts}}},
	}, options.FindOne().SetSort(bson.D{{Key: "startVersion", Value: 1}})).Decode(&rec)
	if err != nil {
		if !errors.Is(err, mongo.ErrNoDocuments) {
			return nil, err
		}
		// No chunk with modifications after the timestamp → latest (Node 1:1).
		latest, err := m.GetLatestChunk(ctx, projectID, false)
		if err != nil {
			return nil, err
		}
		if latest == nil {
			return nil, otc.NewChunkBeforeTimestampNotFoundError(projectID, ts)
		}
		return latest, nil
	}
	meta := rec.toMeta()
	return &meta, nil
}

func (m *mongoChunkBackend) GetProjectChunkIDs(ctx context.Context, projectID string) ([]string, error) {
	oid, err := bson.ObjectIDFromHex(projectID)
	if err != nil {
		return nil, errors.New("bad projectId")
	}
	cur, err := m.coll().Find(ctx, bson.D{
		{Key: "projectId", Value: oid},
		{Key: "state", Value: bson.D{{Key: "$in", Value: []string{"active", "closed"}}}},
	}, options.Find().SetProjection(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := []string{}
	var rec mongoChunkRecord
	for cur.Next(ctx) {
		if err := cur.Decode(&rec); err != nil {
			return nil, err
		}
		out = append(out, rec.ID.Hex())
	}
	return out, cur.Err()
}

func (m *mongoChunkBackend) GetProjectChunks(ctx context.Context, projectID string) ([]ChunkMeta, error) {
	oid, err := bson.ObjectIDFromHex(projectID)
	if err != nil {
		return nil, errors.New("bad projectId")
	}
	cur, err := m.coll().Find(ctx, bson.D{
		{Key: "projectId", Value: oid},
		{Key: "state", Value: bson.D{{Key: "$in", Value: []string{"active", "closed"}}}},
	}, options.Find().SetSort(bson.D{{Key: "startVersion", Value: 1}}).SetProjection(bson.D{{Key: "state", Value: 0}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := []ChunkMeta{}
	var rec mongoChunkRecord
	for cur.Next(ctx) {
		if err := cur.Decode(&rec); err != nil {
			return nil, err
		}
		out = append(out, rec.toMeta())
	}
	return out, cur.Err()
}

func (m *mongoChunkBackend) Clone(ctx context.Context, srcID, dstID string) (map[string]string, error) {
	oidSrc, err := bson.ObjectIDFromHex(srcID)
	if err != nil {
		return nil, errors.New("bad source projectId")
	}
	oidDst, err := bson.ObjectIDFromHex(dstID)
	if err != nil {
		return nil, errors.New("bad target projectId")
	}
	cur, err := m.coll().Find(ctx, bson.D{
		{Key: "projectId", Value: oidSrc},
		{Key: "state", Value: bson.D{{Key: "$in", Value: []string{"active", "closed"}}}},
	})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	chunkIDs := map[string]string{}
	batch := []any{}
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if _, err := m.coll().InsertMany(ctx, batch); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}
	for cur.Next(ctx) {
		var rec bson.M
		if err := cur.Decode(&rec); err != nil {
			return nil, err
		}
		delete(rec, "projectId")
		newID := bson.NewObjectID()
		old, _ := rec["_id"].(bson.ObjectID)
		chunkIDs[old.Hex()] = newID.Hex()
		rec["_id"] = newID
		rec["projectId"] = oidDst
		batch = append(batch, rec)
		if len(batch) > 100 {
			if err := flush(); err != nil {
				return nil, err
			}
		}
	}
	if err := cur.Err(); err != nil {
		return nil, err
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return chunkIDs, nil
}

func (m *mongoChunkBackend) InsertPendingChunk(ctx context.Context, projectID string, c *otc.Chunk) (string, error) {
	oid, err := bson.ObjectIDFromHex(projectID)
	if err != nil {
		return "", errors.New("bad projectId")
	}
	chunkID := bson.NewObjectID()
	_, err = m.coll().InsertOne(ctx, bson.D{
		{Key: "_id", Value: chunkID},
		{Key: "projectId", Value: oid},
		{Key: "startVersion", Value: c.GetStartVersion()},
		{Key: "endVersion", Value: c.GetEndVersion()},
		{Key: "endTimestamp", Value: c.GetEndTimestamp()},
		{Key: "state", Value: "pending"},
		{Key: "updatedAt", Value: time.Now()},
	})
	if err != nil {
		return "", err
	}
	return chunkID.Hex(), nil
}

func (m *mongoChunkBackend) activate(ctx context.Context, projectID, chunkID string) error {
	poid, err := bson.ObjectIDFromHex(projectID)
	if err != nil {
		return errors.New("bad projectId")
	}
	coid, err := bson.ObjectIDFromHex(chunkID)
	if err != nil {
		return errors.New("bad chunkId")
	}
	res, err := m.coll().UpdateOne(ctx, bson.D{
		{Key: "_id", Value: coid},
		{Key: "projectId", Value: poid},
		{Key: "state", Value: "pending"},
	}, bson.D{{Key: "$set", Value: bson.D{
		{Key: "state", Value: "active"},
		{Key: "updatedAt", Value: time.Now()},
	}}})
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return newChunkConflict("chunk start version is not unique", map[string]any{"projectId": projectID, "chunkId": chunkID})
		}
		return err
	}
	if res.MatchedCount == 0 {
		return errors.New("OError: pending chunk not found")
	}
	return nil
}

func (m *mongoChunkBackend) closeChunk(ctx context.Context, projectID, chunkID string) error {
	poid, err := bson.ObjectIDFromHex(projectID)
	if err != nil {
		return errors.New("bad projectId")
	}
	coid, err := bson.ObjectIDFromHex(chunkID)
	if err != nil {
		return errors.New("bad chunkId")
	}
	res, err := m.coll().UpdateOne(ctx, bson.D{
		{Key: "_id", Value: coid},
		{Key: "projectId", Value: poid},
		{Key: "state", Value: "active"},
	}, bson.D{{Key: "$set", Value: bson.D{{Key: "state", Value: "closed"}}}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return newChunkConflict("unable to close chunk", map[string]any{"projectId": projectID, "chunkId": chunkID})
	}
	return nil
}

func (m *mongoChunkBackend) deleteActiveChunk(ctx context.Context, projectID, chunkID string) error {
	poid, err := bson.ObjectIDFromHex(projectID)
	if err != nil {
		return errors.New("bad projectId")
	}
	coid, err := bson.ObjectIDFromHex(chunkID)
	if err != nil {
		return errors.New("bad chunkId")
	}
	res, err := m.coll().UpdateOne(ctx, bson.D{
		{Key: "_id", Value: coid},
		{Key: "projectId", Value: poid},
		{Key: "state", Value: "active"},
	}, bson.D{{Key: "$set", Value: bson.D{
		{Key: "state", Value: "deleted"},
		{Key: "updatedAt", Value: time.Now()},
	}}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return newChunkConflict("unable to delete active chunk", map[string]any{"projectId": projectID, "chunkId": chunkID})
	}
	return nil
}

func (m *mongoChunkBackend) ConfirmCreate(ctx context.Context, projectID string, c *otc.Chunk, chunkID string, oldChunkID *string, earliest *time.Time) error {
	sess, err := m.db.Client().StartSession()
	if err != nil {
		return err
	}
	defer sess.EndSession(ctx)
	_, err = sess.WithTransaction(ctx, func(tctx context.Context) (any, error) {
		if oldChunkID != nil {
			if err := m.closeChunk(tctx, projectID, *oldChunkID); err != nil {
				return nil, err
			}
		}
		if err := m.activate(tctx, projectID, chunkID); err != nil {
			return nil, err
		}
		if err := m.updateProjectRecord(tctx, projectID, c, earliest); err != nil {
			return nil, err
		}
		return nil, nil
	})
	return err
}

func (m *mongoChunkBackend) ConfirmUpdate(ctx context.Context, projectID, oldChunkID string, newChunk *otc.Chunk, newChunkID string, earliest *time.Time) error {
	sess, err := m.db.Client().StartSession()
	if err != nil {
		return err
	}
	defer sess.EndSession(ctx)
	_, err = sess.WithTransaction(ctx, func(tctx context.Context) (any, error) {
		if err := m.deleteActiveChunk(tctx, projectID, oldChunkID); err != nil {
			return nil, err
		}
		if err := m.activate(tctx, projectID, newChunkID); err != nil {
			return nil, err
		}
		if err := m.updateProjectRecord(tctx, projectID, newChunk, earliest); err != nil {
			return nil, err
		}
		return nil, nil
	})
	return err
}

func (m *mongoChunkBackend) DeleteChunk(ctx context.Context, projectID, chunkID string) error {
	poid, err := bson.ObjectIDFromHex(projectID)
	if err != nil {
		return errors.New("bad projectId")
	}
	coid, err := bson.ObjectIDFromHex(chunkID)
	if err != nil {
		return errors.New("bad chunkId")
	}
	_, err = m.coll().UpdateOne(ctx, bson.D{
		{Key: "_id", Value: coid},
		{Key: "projectId", Value: poid},
	}, bson.D{{Key: "$set", Value: bson.D{
		{Key: "state", Value: "deleted"},
		{Key: "updatedAt", Value: time.Now()},
	}}})
	return err
}

func (m *mongoChunkBackend) DeleteProjectChunks(ctx context.Context, projectID string) error {
	poid, err := bson.ObjectIDFromHex(projectID)
	if err != nil {
		return errors.New("bad projectId")
	}
	_, err = m.coll().UpdateMany(ctx, bson.D{
		{Key: "projectId", Value: poid},
		{Key: "state", Value: bson.D{{Key: "$in", Value: []string{"active", "closed"}}}},
	}, bson.D{{Key: "$set", Value: bson.D{
		{Key: "state", Value: "deleted"},
		{Key: "updatedAt", Value: time.Now()},
	}}})
	return err
}

// updateProjectRecord — Node skips it when config.backupStore is unset (as in
// this stack); kept as a documented no-op seam.
func (m *mongoChunkBackend) updateProjectRecord(ctx context.Context, projectID string, c *otc.Chunk, earliest *time.Time) error {
	return nil // config.has('backupStore') === false
}

func (m *mongoChunkBackend) lookupMongoProjectIDFromHistoryID(ctx context.Context, historyID any) (string, error) {
	var doc struct {
		ID bson.ObjectID `bson:"_id"`
	}
	if err := m.db.Collection("projects").FindOne(ctx, bson.D{
		{Key: "overleaf.history.id", Value: historyID},
	}, options.FindOne().SetProjection(bson.D{{Key: "_id", Value: 1}})).Decode(&doc); err != nil {
		return "", errors.New("OError: mongo project not found by history id")
	}
	return doc.ID.Hex(), nil
}

var _ ChunkBackend = (*mongoChunkBackend)(nil)

func jsonMarshalBytes(v any) ([]byte, error) { return json.Marshal(v) }

func itoaVersion(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [24]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	out := string(b[i:])
	if neg {
		out = "-" + out
	}
	return out
}
