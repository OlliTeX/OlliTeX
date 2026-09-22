package history

import (
	"context"
	"regexp"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"ollitex/go/services/web/core"
)

var hexPat24 = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

func strOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// keysOf collects map keys into a slice.
func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func hexUID(s string) (primitive.ObjectID, error) {
	return primitive.ObjectIDFromHex(s)
}

// userView fields (Node _userView / projection {first_name,last_name,email}
// + overleaf.id for v1-lookups).
type userView struct {
	FirstName string
	LastName  string
	Email     string
	ID        string // overleaf id (hex)
	Name      string // user.name (absent in CE projection)
}

// loadUsersByIDs: Node UserGetter.promises.getUsers(ids, projection) ->
// users keyed by _id hex.
func loadUsersByIDs(a *core.App, cxt *core.Cxt, ids []string) map[string]userView {
	out := map[string]userView{}
	if a == nil || a.Mongo == nil || len(ids) == 0 {
		return out
	}
	oids := make([]any, 0, len(ids))
	for _, s := range ids {
		if o, err := primitive.ObjectIDFromHex(s); err == nil {
			oids = append(oids, o)
		}
	}
	if len(oids) == 0 {
		return out
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 10*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return out
	}
	cur, err := db.Collection("users").Find(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: oids}}}},
		options.Find().SetProjection(bson.D{
			{Key: "first_name", Value: 1},
			{Key: "last_name", Value: 1},
			{Key: "email", Value: 1},
		}))
	if err != nil {
		return out
	}
	for cur.Next(ctx) {
		var d primitive.D
		if cur.Decode(&d) != nil {
			continue
		}
		o := &d
		out[oidHex(o.Map()["_id"])] = userViewFromDoc(o)
	}
	return out
}

// loadUsersByV1IDs: Node getUsersByV1Ids — query {overleaf.id: {$in: ids}}
// keyed by the overleaf.id value.
func loadUsersByV1IDs(a *core.App, cxt *core.Cxt, ids []int64) map[int64]userView {
	out := map[int64]userView{}
	if a == nil || a.Mongo == nil || len(ids) == 0 {
		return out
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 10*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return out
	}
	cur, err := db.Collection("users").Find(ctx, bson.D{{Key: "overleaf.id", Value: bson.D{{Key: "$in", Value: ids}}}},
		options.Find().SetProjection(bson.D{
			{Key: "first_name", Value: 1},
			{Key: "last_name", Value: 1},
			{Key: "email", Value: 1},
			{Key: "overleaf", Value: 1},
		}))
	if err != nil {
		return out
	}
	for cur.Next(ctx) {
		var d primitive.D
		if cur.Decode(&d) != nil {
			continue
		}
		o := &d
		u := userViewFromDoc(o)
		key := int64(0)
		if ov, ok := o.Map()["overleaf"].(primitive.D); ok {
			if id, ok := ov.Map()["id"].(int64); ok {
				key = id
			} else if id, ok := ov.Map()["id"].(primitive.ObjectID); ok {
				u.ID = id.Hex()
			}
		}
		out[key] = u
	}
	return out
}

func userViewFromDoc(d *primitive.D) userView {
	m := d.Map()
	u := userView{
		FirstName: strOf(m["first_name"]),
		LastName:  strOf(m["last_name"]),
		Email:     strOf(m["email"]),
		ID:        oidHex(m["_id"]),
	}
	return u
}

var _ = strconv.Itoa
