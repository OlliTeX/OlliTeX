package collab

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
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

// ---- session cookie contract (mirrors go/services/web/core) ----
//
// The sid cookie value on the wire is:
//   percent-encode("s:" + signCookie(rawSid, secret))
// where signCookie (cookie-signature@1.0.6) = "sid." +
// base64Std(HMAC-SHA256(secret, sid)) with trailing "=" trimmed. The web's
// read pipeline is decodeCookieValue → TrimPrefix "s:" → unsignCookie (last
// ".", verify candidate against the secret chain, timing-safe) → raw sid.
// This gate mirrors it exactly (live-probe-caught: missing BOTH the percent
// decode and the unsign made every real browser session 401).

func pctDecode(v string) string {
	if !strings.Contains(v, "%") {
		return v
	}
	dec := make([]byte, 0, len(v))
	for i := 0; i < len(v); i++ {
		if v[i] == '%' && i+2 < len(v) {
			d := func(b byte) int {
				switch {
				case b >= '0' && b <= '9':
					return int(b - '0')
				case b >= 'a' && b <= 'f':
					return int(b-'a') + 10
				case b >= 'A' && b <= 'F':
					return int(b-'A') + 10
				}
				return -1
			}
			a, b := d(v[i+1]), d(v[i+2])
			if a >= 0 && b >= 0 {
				dec = append(dec, byte(a*16+b))
				i += 2
				continue
			}
		}
		dec = append(dec, v[i])
	}
	return string(dec)
}

func signCookie(val, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(val))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return val + "." + strings.TrimRight(sig, "=")
}

func unsignCookie(val string, secrets []string) string {
	i := strings.LastIndex(val, ".")
	if i < 0 {
		return ""
	}
	candidate := val[:i]
	for _, s := range secrets {
		if s == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(signCookie(candidate, s)), []byte(val)) == 1 {
			return candidate
		}
	}
	return ""
}

// sessionSid — cookie value → raw sid ("" = reject/miss): the web's exact
// pipeline (pctDecode → "s:" → unsign against the secret chain).
func sessionSid(v string, secrets []string) string {
	v = pctDecode(v)
	v = strings.TrimPrefix(v, "s:")
	return unsignCookie(v, secrets)
}

// mongoClient — the production Mongo backing the Mongo interface.
type mongoClient struct {
	db *mongo.Database
}

// NewMongoClient connects (MONGO_CONNECTION_STRING || OVERLEAF_MONGO_URL ||
// mongodb://host/sharelatex, mirroring core config precedence) and returns
// the client + database for the given db name.
func NewMongoClient(ctx context.Context, uri, dbName string) (*mongo.Client, *mongo.Database, error) {
	c, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, nil, err
	}
	if err := c.Ping(ctx, nil); err != nil {
		return nil, nil, err
	}
	return c, c.Database(dbName), nil
}

// NewMongo connects and returns the Mongo implementation.
func NewMongo(ctx context.Context, uri, dbName string) (Mongo, error) {
	_, db, err := NewMongoClient(ctx, uri, dbName)
	if err != nil {
		return nil, err
	}
	return &mongoClient{db: db}, nil
}

// NewMongoFrom wraps an already-connected database (single-connection
// deployments: auth + seed + store share ONE client, see cmd/collab).
func NewMongoFrom(db *mongo.Database) Mongo { return &mongoClient{db: db} }

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
func RedisSessionDoc(rdb *gredis.Client, cookieName string, secrets ...string) func(ctx context.Context, r *http.Request) (map[string]any, error) {
	name := cookieName
	if name == "" {
		name = defaultCookieName
	}
	return func(ctx context.Context, r *http.Request) (map[string]any, error) {
		c, err := r.Cookie(name)
		if err != nil {
			return nil, nil
		}
		sid := sessionSid(c.Value, secrets)
		if sid == "" {
			return nil, nil
		}
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
