package cronmail

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Doc — one `emailNotifications` record (the producer shapes in this stack:
// chat writes {recipient_id, project_id, emailType, opts, scheduledAt,
// updatedAt}; the legacy Node scheduler the same with emailType
// 'trackedChangesNotification').
type Doc struct {
	ID          bson.Raw
	EmailType   string
	Type        string         // legacy docs carry the type under `type` (node `notification.emailType || notification.type`)
	Raw         map[string]any // the selected opts bag (opts||options||data) — render inputs keep native types (isComment is a bool)
	RecipientID *primitive.ObjectID
	ProjectID   *primitive.ObjectID
	Attempts    int // prior failure count (node `notification.attempts || 0`)

	// claim/lifecycle (node claim-protocol doc fields)
	ScheduledAt         time.Time
	Processing          bool
	ProcessingSet       bool // was the `processing` key ever present? (distinct from Processing=false)
	ProcessingStartedAt *time.Time
	NextRetryAt         *time.Time
	Dead                bool
	Error               string // processingError
}

// Store — the claim/resolve surface over `emailNotifications`.
type Store interface {
	// ClaimNextDue — node `_claimNextDueNotification`: the atomic
	// findOneAndUpdate claim (due + one of: never-processed / retryable /
	// legacy-retryable / stale-in-progress; not dead), returns the doc after
	// the claim update. nil when the queue has nothing claimable.
	ClaimNextDue(ctx context.Context, now time.Time) (*Doc, error)
	// MarkRetry — node `_markFailed` sub-MAX_ATTEMPTS path.
	MarkRetry(ctx context.Context, id bson.Raw, attempts int, nextRetryAt time.Time, errMsg string) error
	// MarkDead — node `_markFailed` dead-letter path.
	MarkDead(ctx context.Context, id bson.Raw, attempts int, errMsg string) error
	// DeleteDoc — success path (node `deleteOne`).
	DeleteDoc(ctx context.Context, id bson.Raw) error
	// ReleaseMany — dry-run end-of-run release (node `_releaseForDryRun`).
	ReleaseMany(ctx context.Context, ids []bson.Raw) error
	// GetUserEmail — node `UserGetter.promises.getUser(id, { email: 1 })`
	// (users collection; empty string when the user or email is missing).
	GetUserEmail(ctx context.Context, id any) (string, error)
}

const staleProcessing = time.Hour // node STALE_PROCESSING_MS = 1h

// MongoStore — the real implementation (Node: db.emailNotifications).
type MongoStore struct {
	coll  *mongo.Collection
	users *mongo.Collection
}

func NewMongoStore(client *mongo.Client, db, users string) *MongoStore {
	d := client.Database(db)
	return &MongoStore{coll: d.Collection("emailNotifications"), users: d.Collection(users)}
}

// claimFilter — 1:1 with the node `_claimNextDueNotification` filter.
func claimFilter(nowDate time.Time, staleDate time.Time) bson.D {
	return bson.D{
		{Key: "scheduledAt", Value: bson.D{{Key: "$lte", Value: nowDate}}},
		{Key: "$or", Value: []bson.D{
			{{Key: "processing", Value: bson.D{{Key: "$exists", Value: false}}}},
			{
				{Key: "processing", Value: false},
				{Key: "nextRetryAt", Value: bson.D{{Key: "$lt", Value: nowDate}}},
			},
			{
				{Key: "processing", Value: false},
				{Key: "nextRetryAt", Value: bson.D{{Key: "$exists", Value: false}}},
			},
			{
				{Key: "processing", Value: true},
				{Key: "processingStartedAt", Value: bson.D{{Key: "$lt", Value: staleDate}}},
			},
		}},
		{Key: "$nor", Value: []bson.D{bson.D{{Key: "dead", Value: true}}}},
	}
}

func (s *MongoStore) ClaimNextDue(ctx context.Context, now time.Time) (*Doc, error) {
	res := s.coll.FindOneAndUpdate(ctx, claimFilter(now, now.Add(-staleProcessing)),
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "processing", Value: true},
			{Key: "processingStartedAt", Value: now},
		}}},
		options.FindOneAndUpdate().SetSort(bson.D{{Key: "scheduledAt", Value: 1}}).SetReturnDocument(options.After))
	var raw bson.M
	if err := res.Decode(&raw); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return decodeDoc(raw), nil
}

// decodeDoc — node docs decode as objects; `opts` values are strings in both
// producers. (bson.M keeps field order irrelevant.)
func decodeDoc(raw bson.M) *Doc {
	d := &Doc{}
	if id, ok := raw["_id"]; ok {
		if b, err := bson.Marshal(id); err == nil {
			d.ID = b
		}
	}
	if v, ok := stringVal(raw["emailType"]); ok {
		d.EmailType = v
	}
	if v, ok := stringVal(raw["type"]); ok {
		d.Type = v
	}
	// Selected opts bag: node `notification.opts || notification.options || notification.data || {}`.
	var rawBag map[string]any
	if m := mapVal(raw["opts"]); m != nil {
		rawBag = m
	} else if m := mapVal(raw["options"]); m != nil {
		rawBag = m
	} else if m := mapVal(raw["data"]); m != nil {
		rawBag = m
	} else {
		rawBag = map[string]any{}
	}
	d.Raw = rawBag
	if oid, ok := raw["recipient_id"].(primitive.ObjectID); ok && oid != primitive.NilObjectID {
		d.RecipientID = &oid
	}
	if oid, ok := raw["project_id"].(primitive.ObjectID); ok && oid != primitive.NilObjectID {
		d.ProjectID = &oid
	}
	if n, ok := intValue(raw["attempts"]); ok {
		d.Attempts = n
	}
	if t, ok := raw["scheduledAt"].(time.Time); ok {
		d.ScheduledAt = t
	}
	{
		var processing bool
		if v, ok := raw["processing"].(bool); ok {
			processing = v
			d.ProcessingSet = true
		}
		d.Processing = processing
	}
	if t, ok := raw["processingStartedAt"].(time.Time); ok {
		d.ProcessingStartedAt = &t
	}
	if t, ok := raw["nextRetryAt"].(time.Time); ok {
		d.NextRetryAt = &t
	}
	if v, ok := raw["dead"].(bool); ok {
		d.Dead = v
	}
	if s, ok := raw["processingError"].(string); ok {
		d.Error = s
	}
	return d
}

func stringVal(v any) (string, bool) {
	s, ok := v.(string)
	if ok && s != "" {
		return s, true
	}
	return "", false
}

func mapVal(v any) map[string]any {
	switch m := v.(type) {
	case nil:
		return nil
	case bson.M:
		return map[string]any(m)
	case map[string]any:
		return m
	}
	return nil
}

func intValue(v any) (int, bool) {
	switch n := v.(type) {
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case int:
		return n, true
	case float64:
		return int(n), true
	}
	return 0, false
}

func (s *MongoStore) MarkRetry(ctx context.Context, id bson.Raw, attempts int, nextRetryAt time.Time, errMsg string) error {
	_, err := s.coll.UpdateOne(ctx, bson.D{{Key: "_id", Value: id}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "processing", Value: false},
			{Key: "attempts", Value: attempts},
			{Key: "nextRetryAt", Value: nextRetryAt},
			{Key: "processingError", Value: errMsg},
		}}})
	return err
}

func (s *MongoStore) MarkDead(ctx context.Context, id bson.Raw, attempts int, errMsg string) error {
	_, err := s.coll.UpdateOne(ctx, bson.D{{Key: "_id", Value: id}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "dead", Value: true},
			{Key: "processing", Value: false},
			{Key: "attempts", Value: attempts},
			{Key: "processingError", Value: errMsg},
		}}})
	return err
}

func (s *MongoStore) DeleteDoc(ctx context.Context, id bson.Raw) error {
	_, err := s.coll.DeleteOne(ctx, bson.D{{Key: "_id", Value: id}})
	return err
}

func (s *MongoStore) ReleaseMany(ctx context.Context, ids []bson.Raw) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := s.coll.UpdateMany(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: ids}}}},
		bson.D{
			{Key: "$set", Value: bson.D{{Key: "processing", Value: false}}},
			{Key: "$unset", Value: bson.D{
				{Key: "nextRetryAt", Value: ""},
				{Key: "processingStartedAt", Value: ""},
			}},
		})
	return err
}

func (s *MongoStore) GetUserEmail(ctx context.Context, id any) (string, error) {
	var doc struct {
		Email string `bson:"email"`
	}
	err := s.users.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return doc.Email, nil
}
