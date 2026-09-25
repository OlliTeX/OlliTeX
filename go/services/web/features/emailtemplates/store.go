package emailtemplates

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Collection — the Mongo collection holding admin overrides.
const Collection = "emailtemplateoverrides"

// Override — the admin's per-slot overrides (any field "" = default).
//
// Document shape (flat): {_id: "<slot>", subject?, text?, html?,
// updatedAt, updatedBy} — only the overridden fields are stored.
type Override struct {
	Subject   string `bson:"subject"`
	Text      string `bson:"text"`
	HTML      string `bson:"html"`
	UpdatedAt string `bson:"updatedAt"`
	UpdatedBy string `bson:"updatedBy"`
}

// overrideField — an override's field ("" = use the default).
func overrideField(ov Override, field string) string {
	switch field {
	case "subject":
		return ov.Subject
	case "text":
		return ov.Text
	case "html":
		return ov.HTML
	}
	return ""
}

// Store persists overrides. Production = MongoStore; tests inject the
// map.
type Store interface {
	GetOverride(ctx context.Context, slot string) (Override, error)
	SetOverride(ctx context.Context, slot string, ov Override) error
	ClearOverride(ctx context.Context, slot string) error
	LoadAll(ctx context.Context) (map[string]Override, error)
}

// ---------- map store (unit tests / no-db runs) ----------

type MapStore struct {
	m map[string]Override
}

var _ Store = (*MapStore)(nil)

// NewMapStore — an in-memory override store.
func NewMapStore() *MapStore {
	return &MapStore{m: map[string]Override{}}
}

func (s *MapStore) GetOverride(_ context.Context, slot string) (Override, error) {
	ov, _ := s.m[slot]
	return ov, nil
}

func (s *MapStore) SetOverride(_ context.Context, slot string, ov Override) error {
	s.m[slot] = ov
	return nil
}

func (s *MapStore) ClearOverride(_ context.Context, slot string) error {
	delete(s.m, slot)
	return nil
}

func (s *MapStore) LoadAll(_ context.Context) (map[string]Override, error) {
	out := make(map[string]Override, len(s.m))
	for k, v := range s.m {
		out[k] = v
	}
	return out, nil
}

// ---------- Mongo store ----------

type MongoStore struct {
	db   *mongo.Database
	coll string
}

var _ Store = (*MongoStore)(nil)

// NewMongoStore — overrides persisted in <db>.emailtemplateoverrides.
func NewMongoStore(db *mongo.Database, coll string) *MongoStore {
	if coll == "" {
		coll = Collection
	}
	return &MongoStore{db: db, coll: coll}
}

// decodeOverrideDoc — manual field extraction (robust against nested
// decode surprises; the document is flat by construction).
func decodeOverrideDoc(raw any) (string, Override, bool) {
	ov := Override{}
	id := ""
	switch m := raw.(type) {
	case bson.M:
		id, _ = m["_id"].(string)
		ov.Subject, _ = m["subject"].(string)
		ov.Text, _ = m["text"].(string)
		ov.HTML, _ = m["html"].(string)
		ov.UpdatedAt, _ = m["updatedAt"].(string)
		ov.UpdatedBy, _ = m["updatedBy"].(string)
	case bson.D:
		for _, e := range m {
			switch string(e.Key) {
			case "_id":
				id, _ = e.Value.(string)
			case "subject":
				ov.Subject, _ = e.Value.(string)
			case "text":
				ov.Text, _ = e.Value.(string)
			case "html":
				ov.HTML, _ = e.Value.(string)
			case "updatedAt":
				ov.UpdatedAt, _ = e.Value.(string)
			case "updatedBy":
				ov.UpdatedBy, _ = e.Value.(string)
			}
		}
	default:
		return "", Override{}, false
	}
	return id, ov, true
}

// LoadAll — every override ("" fields = defaults).
func (s *MongoStore) LoadAll(ctx context.Context) (map[string]Override, error) {
	if s.db == nil {
		return map[string]Override{}, nil
	}
	cur, err := s.db.Collection(s.coll).Find(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var docs []any
	if derr := cur.All(ctx, &docs); derr != nil {
		return nil, derr
	}
	out := make(map[string]Override, len(docs))
	for _, raw := range docs {
		id, ov, okDoc := decodeOverrideDoc(raw)
		if !okDoc || id == "" {
			continue
		}
		out[id] = ov
	}
	return out, nil
}

func (s *MongoStore) GetOverride(ctx context.Context, slot string) (Override, error) {
	all, err := s.LoadAll(ctx)
	if err != nil {
		return Override{}, err
	}
	ov, _ := all[slot]
	return ov, nil
}

func (s *MongoStore) SetOverride(ctx context.Context, slot string, ov Override) error {
	if s.db == nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	doc := bson.M{"subject": ov.Subject, "text": ov.Text, "html": ov.HTML, "updatedAt": now}
	if ov.UpdatedBy != "" {
		doc["updatedBy"] = ov.UpdatedBy
	}
	_, err := s.db.Collection(s.coll).UpdateOne(ctx,
		bson.M{"_id": slot}, bson.M{"$set": doc},
		options.Update().SetUpsert(true))
	return err
}

func (s *MongoStore) ClearOverride(ctx context.Context, slot string) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.Collection(s.coll).DeleteOne(ctx, bson.M{"_id": slot})
	return err
}
