package collab

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	gredis "github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// mongoClient — the production Mongo backing the Mongo interface.
type mongoClient struct {
	db *mongo.Database
}

// NewMongo connects (MONGO_CONNECTION_STRING || OVERLEAF_MONGO_URL ||
// mongodb://host/sharelatex, mirroring core config precedence) and returns
// the Mongo implementation.
func NewMongo(ctx context.Context, uri, dbName string) (Mongo, error) {
	c, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	if err := c.Ping(ctx, nil); err != nil {
		return nil, err
	}
	return &mongoClient{db: c.Database(dbName)}, nil
}

func objID(id string) (primitive.ObjectID, bool) {
	if !primitive.IsValidObjectID(id) {
		return primitive.ObjectID{}, false
	}
	oid, _ := primitive.ObjectIDFromHex(id)
	return oid, true
}

func (m *mongoClient) UserByID(ctx context.Context, id string) (bson.D, error) {
	oid, ok := objID(id)
	if !ok {
		return nil, errors.New("collab: malformed user id")
	}
	var doc bson.D
	err := m.db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return doc, nil
}

func (m *mongoClient) ProjectByID(ctx context.Context, id string) (bson.D, error) {
	oid, ok := objID(id)
	if !ok {
		return nil, errors.New("collab: malformed project id")
	}
	var doc bson.D
	err := m.db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return doc, nil
}

// RedisSessionDoc — production SessionDoc: the shared connect-redis session
// store (same cookies/keys the Go web and node web wrote). The session doc
// JSON shape is the express-session document (see core/session.go).
func RedisSessionDoc(rdb *gredis.Client, cookieName string) func(ctx context.Context, r *http.Request) (map[string]any, error) {
	name := cookieName
	if name == "" {
		name = defaultCookieName
	}
	return func(ctx context.Context, r *http.Request) (map[string]any, error) {
		c, err := r.Cookie(name)
		if err != nil {
			return nil, nil
		}
		sid := strings.TrimPrefix(c.Value, "s:")
		raw, err := rdb.Get(ctx, "sess:"+sid).Bytes()
		if err != nil {
			return nil, nil
		}
		var doc map[string]any
		if json.Unmarshal(raw, &doc) != nil {
			return nil, nil
		}
		return doc, nil
	}
}
