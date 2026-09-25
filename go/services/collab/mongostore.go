package collab

import (
	"context"
	"time"

	"github.com/reearth/ygo/crdt"
	"github.com/reearth/ygo/persistence"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoStore — a VersionedPersistence implementation over a single Mongo
// collection (default "ydoc"): one document per room holding the versioned
// update log + named snapshots. This is the production store for the collab
// service (S2): the WS server gets it via LegacyAdapter, exactly like S1's
// FilePersistence, but with Mongo as the system of record (no filesystem
// volume for CRDT state).
//
// Semantics mirror ygo's MemoryPersistence (the conformance reference),
// which in a single-document store needs none of the file/multi-step crash
// choreography: every mutation is one atomic Mongo write, so the crash-safety
// subtests of the conformance suite (CrashInjector) do not apply and are
// skipped by contract.
//
// Room document shape:
//
//	{
//	  "_id":  "<room>",
//	  "head": 42,
//	  "upds": [ { "v": 1, "at": ISODate, "u": BinData }, ... ], // ascending v
//	  "snaps": { "<name>": { "v": 7, "at": ISODate, "state": BinData } }
//	}

const defaultYDocCollection = "ydoc"

type mongoStore struct {
	coll *mongo.Collection
}

type mongUpd struct {
	V  uint64           `bson:"v"`
	At time.Time        `bson:"at"`
	U  primitive.Binary `bson:"u"`
}

type mongSnap struct {
	V     uint64           `bson:"v"`
	At    time.Time        `bson:"at"`
	State primitive.Binary `bson:"state"`
}

type mongRoom struct {
	ID    string              `bson:"_id"`
	Head  uint64              `bson:"head"`
	Upds  []mongUpd           `bson:"upds"`
	Snaps map[string]mongSnap `bson:"snaps"`
}

var _ persistence.VersionedPersistence = (*mongoStore)(nil)

// NewMongoStore opens the store on db (collection defaultYDocCollection).
func NewMongoStore(ctx context.Context, db *mongo.Database) (*mongoStore, error) {
	s := &mongoStore{coll: db.Collection(defaultYDocCollection)}
	if _, err := s.coll.Distinct(ctx, "_id", bson.D{}); err != nil {
		return nil, err
	}
	return s, nil
}

// ClearAll removes every room (test/housekeeping utility).
func (s *mongoStore) ClearAll(ctx context.Context) error {
	if _, err := s.coll.DeleteMany(ctx, bson.D{}); err != nil {
		return err
	}
	return nil
}

func (s *mongoStore) fetch(ctx context.Context, room string) (*mongRoom, error) {
	var d mongRoom
	err := s.coll.FindOne(ctx, bson.D{{Key: "_id", Value: room}}).Decode(&d)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if d.Snaps == nil {
		d.Snaps = map[string]mongSnap{}
	}
	return &d, nil
}

func mergeBlobs(bs [][]byte) ([]byte, error) {
	return crdt.MergeUpdatesV1(bs...)
}

func (s *mongoStore) Load(ctx context.Context, room string) (persistence.LoadResult, error) {
	if err := ctx.Err(); err != nil {
		return persistence.LoadResult{}, err
	}
	d, err := s.fetch(ctx, room)
	if d == nil {
		return persistence.LoadResult{}, nil // unknown room: zero result (contract)
	}
	if err != nil {
		return persistence.LoadResult{}, err
	}
	if len(d.Upds) == 0 {
		return persistence.LoadResult{Version: Version(d.Head)}, nil
	}
	bs := make([][]byte, 0, len(d.Upds))
	for _, u := range d.Upds {
		bs = append(bs, u.U.Data)
	}
	merged, err := mergeBlobs(bs)
	if err != nil {
		return persistence.LoadResult{}, err
	}
	return persistence.LoadResult{Update: merged, Version: Version(d.Head)}, nil
}

func (s *mongoStore) AppendUpdate(ctx context.Context, room string, update []byte) (persistence.Version, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	// Reject invalid updates WITHOUT advancing the version (contract).
	if err := crdt.ApplyUpdateV1(crdt.New(), update, nil); err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	// Atomic: version bump + log append are ONE server-side pipeline write.
	// ($push is NOT a supported stage in update pipelines on this server, so
	// the append is expressed with the $concatArrays EXPRESSION — and each
	// field computes NEWHEAD = $ifNull($head,0)+1 independently via $let,
	// which is deterministic and identical across both assignments.)
	newHeadExpr := bson.D{{Key: "$let", Value: bson.D{
		{Key: "vars", Value: bson.D{{Key: "h", Value: bson.D{{Key: "$add", Value: bson.A{
			bson.D{{Key: "$ifNull", Value: bson.A{"$head", bson.D{{Key: "$literal", Value: 0}}}}},
			bson.D{{Key: "$literal", Value: 1}},
		}}}}}},
		{Key: "in", Value: "$$h"},
	}}}
	// Stage 1 assigns the new head; stage 2 appends the record (v references
	// the ALREADY-UPDATED head). $concatArrays (an aggregation EXPRESSION,
	// unlike the $push STAGE, which MongoDB does not support in update
	// pipelines) does the append.
	setHead := bson.D{{Key: "$set", Value: bson.D{{Key: "head", Value: newHeadExpr}}}}
	appendRec := bson.D{
		{Key: "v", Value: "$head"},
		{Key: "at", Value: now},
		{Key: "u", Value: primitive.Binary{Subtype: 0, Data: update}},
	}
	appendLog := bson.D{{Key: "$set", Value: bson.D{{Key: "upds", Value: bson.D{{Key: "$concatArrays", Value: bson.A{
		bson.D{{Key: "$ifNull", Value: bson.A{"$upds", bson.D{{Key: "$literal", Value: []any{}}}}}},
		bson.A{appendRec},
	}}}}}}}
	pipe := mongo.Pipeline{setHead, appendLog}
	sr := s.coll.FindOneAndUpdate(ctx,
		bson.D{{Key: "_id", Value: room}},
		pipe,
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	)
	var out struct {
		Head uint64 `bson:"head"`
	}
	if err := sr.Decode(&out); err != nil {
		return 0, err
	}
	return Version(out.Head), nil
}

func (s *mongoStore) ListVersions(ctx context.Context, room string) ([]persistence.VersionMeta, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d, err := s.fetch(ctx, room)
	if err != nil {
		return nil, err
	}
	metas := make([]persistence.VersionMeta, 0)
	if d == nil {
		return metas, nil
	}
	metas = make([]persistence.VersionMeta, 0, len(d.Upds))
	for i := len(d.Upds) - 1; i >= 0; i-- {
		metas = append(metas, persistence.VersionMeta{Version: Version(d.Upds[i].V), UpdatedAt: d.Upds[i].At})
	}
	return metas, nil
}

func (s *mongoStore) GetUpdate(ctx context.Context, room string, v persistence.Version) ([]byte, persistence.VersionMeta, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, persistence.VersionMeta{}, false, err
	}
	d, err := s.fetch(ctx, room)
	if err != nil {
		return nil, persistence.VersionMeta{}, false, err
	}
	if d != nil {
		for _, u := range d.Upds {
			if u.V == uint64(v) {
				meta := persistence.VersionMeta{Version: v, UpdatedAt: u.At}
				return u.U.Data, meta, true, nil
			}
		}
	}
	return nil, persistence.VersionMeta{}, false, nil
}

func (s *mongoStore) MaterializeAt(ctx context.Context, room string, v persistence.Version) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if v == 0 {
		return nil, nil
	}
	d, err := s.fetch(ctx, room)
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, persistence.ErrRoomNotFound
	}
	bs := make([][]byte, 0, len(d.Upds))
	for _, u := range d.Upds {
		if u.V <= uint64(v) {
			bs = append(bs, u.U.Data)
		}
	}
	if len(bs) == 0 {
		return nil, persistence.ErrRoomNotFound
	}
	return mergeBlobs(bs)
}

// snapField — the "snaps.<name>" dotted field path (names are server-assigned
// version labels or caller-provided; we sanitize path metacharacters by
// storing under a map rather than a dotted path, so no escaping is needed —
// Mongo map semantics keep any name intact).
func (s *mongoStore) CaptureSnapshot(ctx context.Context, room, name string, state []byte) (persistence.Version, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	d, err := s.fetch(ctx, room)
	if err != nil {
		return 0, err
	}
	if d == nil {
		return 0, persistence.ErrRoomNotFound
	}
	head := Version(d.Head)
	snap := mongSnap{
		V:     uint64(head),
		At:    time.Now().UTC(),
		State: primitive.Binary{Subtype: 0, Data: state},
	}
	nameMap := bson.D{{Key: name, Value: snap}}
	snapField := bson.E{Key: "snaps", Value: nameMap}
	setDoc := bson.D{{Key: "$set", Value: bson.D{snapField}}}
	if _, err := s.coll.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: room}},
		setDoc,
	); err != nil {
		return 0, err
	}
	return head, nil
}

func (s *mongoStore) RestoreSnapshot(ctx context.Context, room, name string) ([]byte, persistence.Version, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, false, err
	}
	d, err := s.fetch(ctx, room)
	if err != nil {
		return nil, 0, false, err
	}
	if d != nil {
		snap, ok := d.Snaps[name]
		if ok {
			return snap.State.Data, Version(snap.V), true, nil
		}
	}
	return nil, 0, false, nil
}

// PruneAfter — atomically rebuilds the room at `target`: everything above is
// dropped, the head becomes target, and the next append continues at
// target+1 (dense version reuse). The surviving log (versions <= target)
// fully reconstructs the target state, which is exactly what the caller
// passed as `rolledBack` (MaterializeAt(target)) — single-doc stores need no
// crash choreography.
func (s *mongoStore) PruneAfter(ctx context.Context, room string, target persistence.Version, rolledBack []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d, err := s.fetch(ctx, room)
	if err != nil {
		return err
	}
	if d == nil {
		return persistence.ErrRoomNotFound
	}
	// $filter is an aggregation EXPRESSION: it must run inside a pipeline
	// (array-form) update. A plain update document would store the
	// {$filter: ...} doc as a literal value, corrupting the log.
	filterExpr := bson.D{{Key: "$filter", Value: bson.D{
		{Key: "input", Value: "$upds"},
		{Key: "as", Value: "u"},
		{Key: "cond", Value: bson.D{{Key: "$lte", Value: bson.A{"$$u.v", uint64(target)}}}},
	}}}
	_, err = s.coll.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: room}},
		mongo.Pipeline{bson.D{{Key: "$set", Value: bson.D{
			{Key: "head", Value: uint64(target)},
			{Key: "upds", Value: filterExpr},
		}}}},
	)
	return err
}

// Compact folds the oldest (len-keep) updates into a single record carrying
// the version + timestamp of the oldest RETAINED record (conformance
// reference: ygo MemoryPersistence.Compact), keeping ListVersions and
// MaterializeAt monotonic.
func (s *mongoStore) Compact(ctx context.Context, room string, keep int) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if keep <= 0 {
		return 0, nil
	}
	d, err := s.fetch(ctx, room)
	if err != nil {
		return 0, err
	}
	if d == nil || len(d.Upds) <= keep {
		return 0, nil
	}
	trimEnd := len(d.Upds) - keep
	blobs := make([][]byte, 0, trimEnd+1)
	for i := 0; i <= trimEnd; i++ {
		blobs = append(blobs, d.Upds[i].U.Data)
	}
	merged, err := mergeBlobs(blobs)
	if err != nil {
		return 0, err
	}
	folded := mongUpd{
		V:  d.Upds[trimEnd].V,
		At: d.Upds[trimEnd].At,
		U:  primitive.Binary{Subtype: 0, Data: merged},
	}
	newLog := make([]mongUpd, 0, keep)
	newLog = append(newLog, folded)
	newLog = append(newLog, d.Upds[trimEnd+1:]...)

	if _, err := s.coll.ReplaceOne(ctx,
		bson.D{{Key: "_id", Value: room}},
		mongRoom{ID: room, Head: d.Head, Upds: newLog, Snaps: d.Snaps},
	); err != nil {
		return 0, err
	}
	return len(d.Upds) - keep, nil
}

func (s *mongoStore) Delete(ctx context.Context, room string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.coll.DeleteOne(ctx, bson.D{{Key: "_id", Value: room}})
	return err
}

// Version — local alias keeping callers inside this package tidy.
type Version = persistence.Version
