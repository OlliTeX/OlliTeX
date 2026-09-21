package mongowrapper

import (
	"context"
	"strings"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// connectWithDBName dials the URI and recovers the default database name the
// URI names (Node `mongoose.connect(uri)` → `mongoose.connection.name`). The
// Go driver does not expose the URI's default db on the client, so it is
// read off the URI path directly.
func connectWithDBName(ctx context.Context, uri string) (string, *mongo.Client, error) {
	dbName := dbFromURI(uri)
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return "", nil, err
	}
	if dbName == "" {
		dbName = "test" // driver default, as in Node's server-side default
	}
	return dbName, client, nil
}

// dbFromURI returns the default database named by a mongodb:// URI ("" when
// the URI names none). Only the path is consulted, never the credentials.
func dbFromURI(uri string) string {
	rest := uri
	if idx := strings.Index(rest, "://"); idx >= 0 {
		rest = rest[idx+len("://"):]
	}
	slash := strings.IndexAny(rest, "/?")
	if slash < 0 {
		return ""
	}
	if rest[slash] == '?' {
		return ""
	}
	end := slash + 1
	for end < len(rest) && rest[end] != '?' {
		end++
	}
	return rest[slash+1 : end]
}
