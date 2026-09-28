// versionlog.go — d5dd23dd S1: per-version metadata (author + origin) for
// the Yjs version stream.
//
// ygo's persistence.VersionMeta carries only {Version, UpdatedAt}; the
// Node-parity /updates contract (frontend Update.meta: users[], start_ts,
// end_ts, origin, source) needs author + origin per version. This file adds
// a thin side log over the versioned store:
//
//   - MemVersionLog / NewMongoVersionLog implement Log;
//   - ActorLog wraps a persistence.VersionedPersistence and records a
//     VersionMeta whenever an AppendUpdate succeeds, taking (uid, origin)
//     from the request context (WithActor / WithOrigin);
//   - fail-soft: a log write NEVER fails the CRDT append (the version still
//     exists; /updates later renders that row's users as [null]).
//
// Actor availability (2026-09-28, verified against ygo v1.50.0 internals):
//   - server-side writes (seed, restore, review, test ClientEdit): the
//     caller enriches its ctx with the session user -> attributed;
//   - browser WS edits: the ygo persistence worker calls
//     StoreUpdate(Context) with its own context (no request identity —
//     provider/websocket/persistence.go), so the recorded uid is "" until
//     the awareness-based attribution hook lands (design hazard #5).
//     Wire-valid: the frontend contract is users: Nullable<User>[].
package collab

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/reearth/ygo/persistence"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// VersionMeta — one stored version's metadata side record.
//
// Origin — flat Node-parity origin tag (shared.ts origin kinds):
// "file-restore", "project-restore", "upload", "git-bridge", "github",
// "dropbox", "history-resync", "history-migration", "" (plain edit/seed).
// The /updates composition layer (S1.2) maps it onto the exact Node object
// shape.
type VersionMeta struct {
	Room   string    `bson:"room"`
	V      uint64    `bson:"v"`
	UID    string    `bson:"uid,omitempty"`
	Origin string    `bson:"origin,omitempty"`
	Source string    `bson:"source,omitempty"`
	At     time.Time `bson:"at"`
}

// Log — the version-metadata store interface.
type Log interface {
	// Upsert records (or overwrites) the metadata for one version.
	Upsert(ctx context.Context, m VersionMeta) error
	// Range returns the records for room's [from..to] (inclusive), ascending.
	Range(ctx context.Context, room string, from, to uint64) ([]VersionMeta, error)
	// Get returns the record for (room, v); ok=false when absent.
	Get(ctx context.Context, room string, v uint64) (VersionMeta, bool, error)
}

// ---------- context actor/origin ----------

type ctxKey int

const (
	actorCtxKey ctxKey = iota
	originCtxKey
	sourceCtxKey
)

// WithActor — bind the acting user id for every version created in this
// call tree (server-side write sites: restore, review, seed, tests).
func WithActor(ctx context.Context, uid string) context.Context {
	if uid == "" {
		return ctx
	}
	return context.WithValue(ctx, actorCtxKey, uid)
}

// WithOrigin — bind the Node-parity origin tag ("" clears).
func WithOrigin(ctx context.Context, origin string) context.Context {
	if origin == "" {
		return ctx
	}
	return context.WithValue(ctx, originCtxKey, origin)
}

// WithSource — bind the origin source (e.g. "git-bridge"), orthogonal to
// origin kind.
func WithSource(ctx context.Context, source string) context.Context {
	if source == "" {
		return ctx
	}
	return context.WithValue(ctx, sourceCtxKey, source)
}

func actorOf(ctx context.Context) string {
	if s, ok := ctx.Value(actorCtxKey).(string); ok {
		return s
	}
	return ""
}

func originOf(ctx context.Context) string {
	if s, ok := ctx.Value(originCtxKey).(string); ok {
		return s
	}
	return ""
}

func sourceOf(ctx context.Context) string {
	if s, ok := ctx.Value(sourceCtxKey).(string); ok {
		return s
	}
	return ""
}

// ---------- ActorLog wrapper ----------

// ActorLog — persistence.VersionedPersistence decorator that mirrors every
// successful AppendUpdate into a Log (see package comment for actor
// availability).
type ActorLog struct {
	Inner persistence.VersionedPersistence
	Log   Log
	// Now — injectable clock (tests).
	Now func() time.Time
	// ErrLog — fail-soft sink for log-write errors (optional).
	ErrLog func(err error)
}

var _ persistence.VersionedPersistence = (*ActorLog)(nil)

// AppendUpdate — the only non-delegating method.
func (a *ActorLog) AppendUpdate(ctx context.Context, room string, update []byte) (persistence.Version, error) {
	v, err := a.Inner.AppendUpdate(ctx, room, update)
	if err != nil || a.Log == nil {
		return v, err
	}
	now := time.Now().UTC()
	if a.Now != nil {
		now = a.Now().UTC()
	}
	m := VersionMeta{Room: room, V: uint64(v), UID: actorOf(ctx), Origin: originOf(ctx), Source: sourceOf(ctx), At: now}
	if lerr := a.Log.Upsert(ctx, m); lerr != nil {
		// Fail-soft: the version is live; metadata is best-effort.
		if a.ErrLog != nil {
			a.ErrLog(fmt.Errorf("collab.ActorLog: room %s v%d: %w", room, v, lerr))
		}
	}
	return v, nil
}

// --- delegation ---

func (a *ActorLog) Load(ctx context.Context, room string) (persistence.LoadResult, error) {
	return a.Inner.Load(ctx, room)
}

func (a *ActorLog) ListVersions(ctx context.Context, room string) ([]persistence.VersionMeta, error) {
	return a.Inner.ListVersions(ctx, room)
}

func (a *ActorLog) GetUpdate(ctx context.Context, room string, v persistence.Version) ([]byte, persistence.VersionMeta, bool, error) {
	return a.Inner.GetUpdate(ctx, room, v)
}

func (a *ActorLog) MaterializeAt(ctx context.Context, room string, v persistence.Version) ([]byte, error) {
	return a.Inner.MaterializeAt(ctx, room, v)
}

func (a *ActorLog) CaptureSnapshot(ctx context.Context, room, name string, state []byte) (persistence.Version, error) {
	return a.Inner.CaptureSnapshot(ctx, room, name, state)
}

func (a *ActorLog) RestoreSnapshot(ctx context.Context, room, name string) ([]byte, persistence.Version, bool, error) {
	return a.Inner.RestoreSnapshot(ctx, room, name)
}

func (a *ActorLog) PruneAfter(ctx context.Context, room string, target persistence.Version, rolledBack []byte) error {
	return a.Inner.PruneAfter(ctx, room, target, rolledBack)
}

func (a *ActorLog) Compact(ctx context.Context, room string, keep int) (int, error) {
	return a.Inner.Compact(ctx, room, keep)
}

func (a *ActorLog) Delete(ctx context.Context, room string) error {
	return a.Inner.Delete(ctx, room)
}

// Wrap — idempotent ActorLog wrap (returns the store as-is when it already
// is one or when log is nil).
func Wrap(Inner persistence.VersionedPersistence, log Log) persistence.VersionedPersistence {
	if log == nil {
		return Inner
	}
	if al, ok := Inner.(*ActorLog); ok {
		al.Log = log
		return al
	}
	return &ActorLog{Inner: Inner, Log: log}
}

// ---------- in-memory log (hermetic tests + non-Mongo stacks) ----------

// MemVersionLog — thread-safe in-memory Log.
type MemVersionLog struct {
	mu     sync.Mutex
	byRoom map[string]map[uint64]VersionMeta
}

// NewMemVersionLog — empty in-memory log.
func NewMemVersionLog() *MemVersionLog {
	return &MemVersionLog{byRoom: map[string]map[uint64]VersionMeta{}}
}

func (m *MemVersionLog) record(room string, v uint64, meta VersionMeta) {
	if m.byRoom[room] == nil {
		m.byRoom[room] = map[uint64]VersionMeta{}
	}
	m.byRoom[room][v] = meta
}

// Append records a version (test helper mirroring an ActorLog write).
func (m *MemVersionLog) Append(room string, v uint64, meta VersionMeta) {
	m.mu.Lock()
	defer m.mu.Unlock()
	meta.Room = room
	meta.V = v
	m.record(room, v, meta)
}

func (m *MemVersionLog) Upsert(ctx context.Context, meta VersionMeta) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record(meta.Room, meta.V, meta)
	return nil
}

func (m *MemVersionLog) Range(ctx context.Context, room string, from, to uint64) ([]VersionMeta, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []VersionMeta{}
	for v, meta := range m.byRoom[room] {
		if v >= from && v <= to {
			out = append(out, meta)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].V < out[j].V })
	return out, nil
}

func (m *MemVersionLog) Get(ctx context.Context, room string, v uint64) (VersionMeta, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	meta, ok := m.byRoom[room][v]
	return meta, ok, nil
}

// All returns every record (test helper).
func (m *MemVersionLog) All() []VersionMeta {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []VersionMeta{}
	roomMetas := make([]string, 0, len(m.byRoom))
	for room := range m.byRoom {
		roomMetas = append(roomMetas, room)
	}
	sort.Strings(roomMetas)
	for _, room := range roomMetas {
		for _, meta := range m.byRoom[room] {
			out = append(out, meta)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Room != out[j].Room {
			return out[i].Room < out[j].Room
		}
		return out[i].V < out[j].V
	})
	return out
}

// ---------- mongo log ----------

// MongoVersionLog — Log over collection "ydocVersionMeta" (unique {room, v}).
type MongoVersionLog struct {
	coll *mongo.Collection
}

var _ Log = (*MongoVersionLog)(nil)

// NewMongoVersionLog — construct + ensure the unique index.
func NewMongoVersionLog(ctx context.Context, db *mongo.Database) (*MongoVersionLog, error) {
	coll := db.Collection("ydocVersionMeta")
	if _, err := coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "room", Value: 1}, {Key: "v", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		return nil, err
	}
	return &MongoVersionLog{coll: coll}, nil
}

func (m *MongoVersionLog) Upsert(ctx context.Context, meta VersionMeta) error {
	filter := bson.D{{Key: "room", Value: meta.Room}, {Key: "v", Value: meta.V}}
	update := bson.D{{Key: "$set", Value: bson.D{
		{Key: "uid", Value: meta.UID},
		{Key: "origin", Value: meta.Origin},
		{Key: "source", Value: meta.Source},
		{Key: "at", Value: meta.At},
	}}}
	_, err := m.coll.ReplaceOne(ctx, filter, update, options.Replace().SetUpsert(true))
	return err
}

func (m *MongoVersionLog) Range(ctx context.Context, room string, from, to uint64) ([]VersionMeta, error) {
	cur, err := m.coll.Find(ctx, bson.D{
		{Key: "room", Value: room},
		{Key: "v", Value: bson.D{{Key: "$gte", Value: from}, {Key: "$lte", Value: to}}},
	}, options.Find().SetSort(bson.D{{Key: "v", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := []VersionMeta{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (m *MongoVersionLog) Get(ctx context.Context, room string, v uint64) (VersionMeta, bool, error) {
	var meta VersionMeta
	err := m.coll.FindOne(ctx, bson.D{{Key: "room", Value: room}, {Key: "v", Value: v}}).Decode(&meta)
	if err == mongo.ErrNoDocuments {
		return VersionMeta{}, false, nil
	}
	if err != nil {
		return VersionMeta{}, false, err
	}
	return meta, true, nil
}
