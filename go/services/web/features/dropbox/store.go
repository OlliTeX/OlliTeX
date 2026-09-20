package dropbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"ollitex/go/services/web/core"
)

const (
	dbxCredsColl  = "dropboxusercredentials"  // live-verified lowercase plural
	dbxStatesColl = "dropboxsyncprojectstates"
)

const defaultDbxPath = "/"
const legacyDbxPath = "Overleaf Dev"

// dbxCred — the user credentials document (userId is the query key).
type dbxCred struct {
	Token string
	Path  string
}

// dbxGetCreds — findOne {userId:<string>}; absent/err → (nil,false).
func dbxGetCreds(ctx context.Context, a *core.App, uid string) (*dbxCred, bool) {
	if a.Mongo == nil || uid == "" {
		return nil, false
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, false
	}
	var d bson.D
	if err := db.Collection(dbxCredsColl).FindOne(ctx, bson.D{{Key: "userId", Value: uid}}).Decode(&d); err != nil {
		return nil, false
	}
	var cred dbxCred
	for _, e := range d {
		switch e.Key {
		case "accessToken":
			if s, ok := e.Value.(string); ok {
				cred.Token = s
			}
		case "path":
			if s, ok := e.Value.(string); ok {
				cred.Path = s
			}
		}
	}
	return &cred, true
}

// dbxEncryptAndSave — encrypt + upsert (Node findOneAndUpdate upsert, no
// explicit _id — Mongo auto-generates it on insert).
func dbxEncryptAndSave(ctx context.Context, a *core.App, uid, plaintextToken string) error {
	token, err := dbxEncryptToken(plaintextToken)
	if err != nil {
		return err
	}
	update := bson.D{
		{Key: "$set", Value: bson.D{
			{Key: "userId", Value: uid},
			{Key: "accessToken", Value: token},
			{Key: "path", Value: defaultDbxPath},
		}},
	}
	return dbxRun(ctx, a, func(db *mongo.Database) error {
		_, err := db.Collection(dbxCredsColl).UpdateOne(ctx, bson.D{{Key: "userId", Value: uid}}, update,
			options.Update().SetUpsert(true))
		return err
	})
}

func dbxRemoveCreds(ctx context.Context, a *core.App, uid string) {
	dbxRun(ctx, a, func(db *mongo.Database) error {
		_, err := db.Collection(dbxCredsColl).DeleteOne(ctx, bson.D{{Key: "userId", Value: uid}})
		return err
	})
}

func dbxRun(ctx context.Context, a *core.App, f func(*mongo.Database) error) error {
	if a.Mongo == nil {
		return nil
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return err
	}
	return f(db)
}

// dbxGetState — findOne {projectId:<string>} with the Node projection.
func dbxGetState(ctx context.Context, a *core.App, projectID string) (map[string]interface{}, bool) {
	if a.Mongo == nil || projectID == "" {
		return nil, false
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, false
	}
	var d bson.D
	if err := db.Collection(dbxStatesColl).
		FindOne(ctx, bson.D{{Key: "projectId", Value: projectID}},
			options.FindOne().SetProjection(bson.D{
				{Key: "connected", Value: 1},
				{Key: "path", Value: 1},
				{Key: "mergeStatus", Value: 1},
				{Key: "lastSyncAt", Value: 1},
				{Key: "lastSyncError", Value: 1},
				{Key: "remoteFiles", Value: 1},
				{Key: "lastSyncRev", Value: 1},
				{Key: "lastSyncVersion", Value: 1},
				{Key: "conflicts", Value: 1},
			})).Decode(&d); err != nil {
		return nil, false
	}
	m := map[string]interface{}{}
	for _, e := range d {
		if e.Key == "_id" {
			continue
		}
		m[e.Key] = e.Value
	}
	return m, true
}

func dbxRemoveState(ctx context.Context, a *core.App, projectID string) {
	dbxRun(ctx, a, func(db *mongo.Database) error {
		_, err := db.Collection(dbxStatesColl).DeleteOne(ctx, bson.D{{Key: "projectId", Value: projectID}})
		return err
	})
}

// dbxRemoveStatesByOwner — disconnect's unlink scope {path, ownerId}.
func dbxRemoveStatesByOwner(ctx context.Context, a *core.App, path, uid string) {
	dbxRun(ctx, a, func(db *mongo.Database) error {
		_, err := db.Collection(dbxStatesColl).DeleteMany(ctx, bson.D{
			{Key: "path", Value: path},
			{Key: "ownerId", Value: uid},
		})
		return err
	})
}

// dbxLinkedStates — status's project list {path, connected:true} (Node
// orders by MongoDB natural order — no sort pinned).
func dbxLinkedStates(ctx context.Context, a *core.App, path string) []map[string]interface{} {
	if a.Mongo == nil {
		return nil
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil
	}
	cur, err := db.Collection(dbxStatesColl).
		Find(ctx, bson.D{{Key: "path", Value: path}, {Key: "connected", Value: true}},
			options.Find().SetLimit(300).SetProjection(bson.D{
					{Key: "projectId", Value: 1},
					{Key: "path", Value: 1},
					{Key: "projectName", Value: 1},
					{Key: "projectPath", Value: 1},
					{Key: "lastSyncAt", Value: 1},
					{Key: "lastSyncError", Value: 1},
				}))
	if err != nil {
		return nil
	}
	defer cur.Close(ctx)
	var out []map[string]interface{}
	for cur.Next(ctx) {
		var d bson.D
		if cur.Decode(&d) != nil {
			continue
		}
		m := map[string]interface{}{}
		for _, e := range d {
			if e.Key != "_id" {
				m[e.Key] = e.Value
			}
		}
		out = append(out, m)
	}
	return out
}

// ---- shared small helpers ---------------------------------------------------

func jsonStr(s string) string {
	b, _ := jsonQuoteString(s)
	return b
}

// jsonQuoteString — marshal a JSON string (kept local so the package
// self-documents; the body assemblers concatenate raw JSON fragments).
func jsonQuoteString(s string) (string, error) {
	buf := strings.Builder{}
	buf.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&buf, "\\u%04x", r)
			} else {
				buf.WriteRune(r)
			}
		}
	}
	buf.WriteByte('"')
	return buf.String(), nil
}

// isoTime — a stored date → Node's toISOString form for JSON serialization.
func isoTime(v interface{}) (string, bool) {
	if dt, ok := v.(primitive.DateTime); ok {
		return dt.Time().UTC().Format("2006-01-02T15:04:05.000Z"), true
	}
	if t, ok := v.(time.Time); ok {
		return t.UTC().Format("2006-01-02T15:04:05.000Z"), true
	}
	return "", false
}

// normalizeDropboxPath — Node's normalizeDropboxPath byte-for-byte.
func normalizeDropboxPath(p string) string {
	if p == "/Overleaf/Dropbox" || p == "" {
		return defaultDbxPath
	}
	if dec, ok := urlPathDecode(p); ok {
		p = dec
	}
	if p == legacyDbxPath {
		return defaultDbxPath
	}
	return p
}
