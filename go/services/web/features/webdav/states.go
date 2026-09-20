// State-doc surfaces + ordered JSON serialization (P6.9 webdav).

package webdav

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
)

// wdStateRec — a webdavSyncProjectStates doc (projection used by the
// status route).
type wdStateRec struct {
	ProjectID     string      `bson:"projectId"`
	LastSyncAt    interface{} `bson:"lastSyncAt"`
	LastSyncError interface{} `bson:"lastSyncError"`
	LastConflict  interface{} `bson:"lastConflict"`
}

// wdFirstStateForUser — Node status route:
//
//	projects = WebdavSyncProjectStates.find(
//	  { $or: [ { ownerId: userId }, { username: credentials.username } ] },
//	  { projectId, lastSyncAt, lastSyncError, lastConflict })
//	projects.sort((a,b) => (b.lastSyncAt?.getTime()||0) - (a.lastSyncAt?.getTime()||0))
//	projects[0] — (or null)
//
// Returns ok=false when no state docs match.
func wdFirstStateForUser(ctx context.Context, a *core.App, uid string, username *string, hasUser bool) (*wdStateRec, bool) {
	if a.Mongo == nil || uid == "" {
		return nil, false
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, false
	}
	or := []bson.E{{Key: "ownerId", Value: uid}}
	if hasUser && username != nil {
		or = append(or, bson.E{Key: "username", Value: *username})
	}
	filter := bson.D{{Key: "$or", Value: or}}
	var recs []wdStateRec
	cur, ferr := db.Collection(wdStatesColl).Find(ctx, filter)
	if ferr != nil {
		return nil, false
	}
	defer cur.Close(ctx)
	if err := cur.All(ctx, &recs); err != nil {
		return nil, false
	}
	// Node sorts by lastSyncAt DESC (null sorts last via (x||0)).
	sort.SliceStable(recs, func(i, j int) bool {
		return stateTime(recs[i].LastSyncAt) > stateTime(recs[j].LastSyncAt)
	})
	if len(recs) == 0 {
		return nil, false
	}
	return &recs[0], true
}

func stateTime(v interface{}) int64 {
	switch t := v.(type) {
	case primitive.DateTime:
		return t.Time().UnixMilli()
	case int64:
		return t
	}
	return 0
}

// wdStateSyncFields — (lastSyncAt ISO, lastSyncError string, lastConflict raw
// JSON or "") from a state doc.
func wdStateSyncFields(r *wdStateRec) (*string, *string, string) {
	var lastSyncAt, lastSyncError *string
	if t, ok := r.LastSyncAt.(primitive.DateTime); ok {
		s := t.Time().UTC().Format("2006-01-02T15:04:05.000Z")
		lastSyncAt = &s
	}
	if s, ok := r.LastSyncError.(string); ok {
		lastSyncError = &s
	}
	lastConflict := ""
	if r.LastConflict != nil {
		if raw, err := wdOrderedRaw(r.LastConflict); err == nil {
			lastConflict = raw
		}
	}
	return lastSyncAt, lastSyncError, lastConflict
}

// wdOrderedJSON — serialize a bson.D in document order (JSON.stringify of a
// mongoose-lean doc keeps field order).
func wdOrderedJSON(d bson.D) string {
	b, err := wdOrderedRaw(d)
	if err != nil {
		return `{}`
	}
	return b
}

// wdOrderedRaw — ordered JSON for arbitrary decoded values (strings,
// numbers, bools, null, ObjectIDs, Dates, arrays, objects keep document
// order — the full set a credentials/state doc can hold).
func wdOrderedRaw(v interface{}) (string, error) {
	switch t := v.(type) {
	case nil:
		return "null", nil
	case bool:
		if t {
			return "true", nil
		}
		return "false", nil
	case string:
		q, _ := json.Marshal(t)
		return string(q), nil
	case int32:
		return itoa64(int64(t)), nil
	case int64:
		return itoa64(t), nil
	case float64:
		return jsNumberFinite(t), nil
	case primitive.ObjectID:
		q, _ := json.Marshal(t.Hex())
		return string(q), nil
	case primitive.DateTime:
		q, _ := json.Marshal(t.Time().UTC().Format("2006-01-02T15:04:05.000Z"))
		return string(q), nil
	case time.Time:
		q, _ := json.Marshal(t.UTC().Format("2006-01-02T15:04:05.000Z"))
		return string(q), nil
	case []interface{}:
		var b []string
		for _, it := range t {
			s, err := wdOrderedRaw(it)
			if err != nil {
				return "", err
			}
			b = append(b, s)
		}
		return "[" + joinStr(b) + "]", nil
	case bson.A:
		var b []string
		for _, it := range t {
			s, err := wdOrderedRaw(it)
			if err != nil {
				return "", err
			}
			b = append(b, s)
		}
		return "[" + joinStr(b) + "]", nil
	case bson.D:
		var b []string
		for _, e := range t {
			kq, _ := json.Marshal(e.Key)
			s, err := wdOrderedRaw(e.Value)
			if err != nil {
				return "", err
			}
			b = append(b, string(kq)+":"+s)
		}
		return "{" + joinStr(b) + "}", nil
	case map[string]interface{}:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b []string
		for _, k := range keys {
			kq, _ := json.Marshal(k)
			s, err := wdOrderedRaw(t[k])
			if err != nil {
				return "", err
			}
			b = append(b, string(kq)+":"+s)
		}
		return "{" + joinStr(b) + "}", nil
	case json.RawMessage:
		return string(t), nil
	default:
		q, err := json.Marshal(v)
		if err != nil {
			return "null", nil
		}
		return string(q), nil
	}
}

func joinStr(b []string) string {
	out := ""
	for i, s := range b {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}

// jsNumberFinite — JSON.stringify number for finite values (Node).
func jsNumberFinite(f float64) string {
	if f != f || f == math.Inf(1) || f == math.Inf(-1) {
		return "null"
	}
	if f == math.Trunc(f) && f > -1e21 && f < 1e21 {
		return itoa64(int64(f))
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// wdActiveConflictPath — ConflictResolver.resolve: a conflict "exists" for
// the given file when the project STATE's lastConflict.path === path OR the
// user's CREDENTIALS lastConflict.path === path.
func wdActiveConflictPath(ctx context.Context, a *core.App, uid, projectID, path string) (string, bool) {
	if d, ok := wdGetState(ctx, a, projectID); ok {
		if lc, ok := wdDocVal(d, "lastConflict"); ok {
			if p, ok := wdConflictPath(lc); ok && p == path {
				return p, true
			}
		}
	}
	if plain, ok := wdGetCreds(ctx, a, uid); ok {
		var m map[string]json.RawMessage
		if json.Unmarshal([]byte(plain), &m) == nil {
			if raw, ok := m["lastConflict"]; ok {
				var lc map[string]json.RawMessage
				if json.Unmarshal(raw, &lc) == nil {
					if p, ok := lc["path"]; ok {
						var s string
						if json.Unmarshal(p, &s) == nil && s == path {
							return s, true
						}
					}
				}
			}
		}
	}
	return "", false
}

func wdConflictPath(v interface{}) (string, bool) {
	dm, ok := v.(bson.D)
	if !ok {
		return "", false
	}
	for _, e := range dm {
		if e.Key == "path" {
			if s, ok := e.Value.(string); ok {
				return s, true
			}
		}
	}
	return "", false
}
