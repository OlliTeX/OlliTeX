package history

// d5dd23dd S3a — tree-ops version log (D41-b1: the file tree lives in the
// project doc + filestore, NOT in the Y.Text room, so tree ops keep their
// OWN version log in the Go web API; the composition layer merges it with
// the room's versions into the single /updates sequence).
//
// Wire: Node project_ops (pinned from the Node `UpdateTranslator` oracle):
//
//	rename : {pathname, newPathname}
//	add    : {pathname, file: {hash, metadata?}}
//	remove : {pathname}                      (MoveFileOperation remove form)
// move   : {pathname, newPathname, metadata? — file move}
//
// The summarizer (vendor S5) stamps `atV` onto each op from the unified
// version index — already implemented in the S1.2 core.

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// YopKind — the recorded mutation kinds (the web API's file surface).
type YopKind string

const (
	YopAdd    YopKind = "add"
	YopRemove YopKind = "remove"
	YopRename YopKind = "rename"
	YopMove   YopKind = "move"
)

// YopMeta — one recorded tree op.
type YopMeta struct {
	Room     string    // project (lowercased pid) = the room name
	V        uint64    // own counter (1-based, per room)
	Kind     YopKind   // add|remove|rename|move
	Pathname string    // the path (project-internal, '/'-joined names)
	NewPath  string    // rename/move destination ("" for add/remove)
	UID      string    // actor ("" = unattributed → users:[] → null-resolved)
	At       time.Time // when it happened (ms precision enough)
}

// YopLog — the tree-op store.
type YopLog interface {
	Append(ctx context.Context, m YopMeta) (uint64, error) // returns the assigned v
	List(ctx context.Context, room string) ([]YopMeta, error)
}

// MemYopLog — in-memory (tests).
type MemYopLog struct {
	ops  map[string][]YopMeta
	next map[string]uint64
}

func NewMemYopLog() *MemYopLog {
	return &MemYopLog{ops: map[string][]YopMeta{}, next: map[string]uint64{}}
}

func (m *MemYopLog) Append(_ context.Context, me YopMeta) (uint64, error) {
	m.next[me.Room]++
	m.ops[me.Room] = append(m.ops[me.Room], me)
	return m.next[me.Room], nil
}

func (m *MemYopLog) List(_ context.Context, room string) ([]YopMeta, error) {
	out := []YopMeta{}
	out = append(out, m.ops[room]...)
	return out, nil
}

// projectOpsWire — the Node-shaped op object for a recorded tree op
// (UpdateTranslator oracle shapes; atV is added later by the summarizer).
func projectOpsWire(m YopMeta) map[string]any {
	switch m.Kind {
	case YopRename:
		return map[string]any{"pathname": m.Pathname, "newPathname": m.NewPath}
	case YopMove:
		return map[string]any{"pathname": m.Pathname, "newPathname": m.NewPath}
	case YopRemove:
		return map[string]any{"pathname": m.Pathname}
	case YopAdd:
		fallthrough
	default:
		// file.hash: Node is the overleaf file hash (sha256 hex); the
		// recording site fills it from the filestore content. "" stays
		// honest (an add op with no hash is wire-valid).
		return map[string]any{"pathname": m.Pathname, "file": map[string]any{"hash": ""}}
	}
}

// mergedFeedItem — one version in the unified (ts-ordered) stream, in the
// exact shape the S1.2 summarizer consumes ({v, meta, pathnames,
// project_ops}) — v is the unified index (1-based).
type mergedFeedItem struct {
	UnifiedV int // unified index (1-based), assigned after sort
	V        int
	Meta     map[string]any
	Path     []string
	Ops      []map[string]any
	Ts       int64   // feed order key (ms)
	Source   int     // 0 = room (text), 1 = yops (tree) — tie-break: room first
	Kind     YopKind // Source==1 only
	Pathname string
	NewPath  string
}

type mergedFeed []mergedFeedItem

// MongoYopLog — durable tree-op log (collection yopsVersionMeta, unique
// {room, v}) — the S3c recording sites write here.
type MongoYopLog struct {
	coll *mongo.Collection
}

func NewMongoYopLog(ctx context.Context, db *mongo.Database) (*MongoYopLog, error) {
	coll := db.Collection("yopsVersionMeta")
	if _, err := coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "room", Value: 1}, {Key: "v", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		return nil, err
	}
	return &MongoYopLog{coll: coll}, nil
}

func (m *MongoYopLog) Append(ctx context.Context, me YopMeta) (uint64, error) {
	v, err := m.coll.CountDocuments(ctx, bson.D{{Key: "room", Value: me.Room}})
	if err != nil {
		return 0, err
	}
	me.V = uint64(v) + 1
	doc := bson.D{
		{Key: "room", Value: me.Room},
		{Key: "v", Value: me.V},
		{Key: "kind", Value: string(me.Kind)},
		{Key: "path", Value: me.Pathname},
		{Key: "new_path", Value: me.NewPath},
		{Key: "uid", Value: me.UID},
		{Key: "at", Value: me.At},
	}
	if _, err := m.coll.ReplaceOne(ctx, bson.D{{Key: "room", Value: me.Room}, {Key: "v", Value: me.V}}, doc, options.Replace().SetUpsert(true)); err != nil {
		return 0, err
	}
	return me.V, nil
}

func (m *MongoYopLog) List(ctx context.Context, room string) ([]YopMeta, error) {
	cur, err := m.coll.Find(ctx, bson.D{{Key: "room", Value: room}}, options.Find().SetSort(bson.D{{Key: "v", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	type raw struct {
		Room string    `bson:"room"`
		V    uint64    `bson:"v"`
		Kind string    `bson:"kind"`
		Path string    `bson:"path"`
		NewP string    `bson:"new_path"`
		UID  string    `bson:"uid"`
		At   time.Time `bson:"at"`
	}
	var out []raw
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	ops := []YopMeta{}
	for _, r := range out {
		ops = append(ops, YopMeta{Room: r.Room, V: r.V, Kind: YopKind(r.Kind), Pathname: r.Path, NewPath: r.NewP, UID: r.UID, At: r.At})
	}
	return ops, nil
}
