package projectlist

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"ollitex/go/services/web/core"
)

type buckets struct {
	owned             []docRef
	readWrite         []docRef
	review            []docRef
	readOnly          []docRef
	tokenReadAndWrite []docRef
	tokenReadOnly     []docRef
}

// proj mirrors ProjectGetter.findAllUsersProjects' `fields` argument plus the
// archived/trashed arrays the list filter needs.
var proj = bson.D{
	{Key: "name", Value: 1},
	{Key: "lastUpdated", Value: 1},
	{Key: "lastUpdatedBy", Value: 1},
	{Key: "publicAccesLevel", Value: 1},
	{Key: "archived", Value: 1},
	{Key: "trashed", Value: 1},
	{Key: "owner_ref", Value: 1},
}

func loadProjectBuckets(a *core.App, cxt *core.Cxt, uid string) (*buckets, error) {
	if a.Mongo == nil {
		return nil, fmt.Errorf("mongo not available")
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 10*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, err
	}
	uidOID, e := primitive.ObjectIDFromHex(uid)
	if e != nil {
		return nil, e
	}

	find := func(filter bson.D) ([]docRef, error) {
		opts := options.Find().SetProjection(proj)
		cur, err := db.Collection("projects").Find(ctx, filter, opts)
		if err != nil {
			return nil, err
		}
		defer cur.Close(ctx)
		var out []docRef
		for cur.Next(ctx) {
			var d primitive.D
			if cur.Decode(&d) != nil {
				continue
			}
			out = append(out, docRefFrom(d))
		}
		if cur.Err() != nil {
			return nil, cur.Err()
		}
		return out, nil
	}

	owned, err := find(bson.D{{Key: "owner_ref", Value: uidOID}})
	if err != nil {
		return nil, err
	}
	readWrite, err := find(bson.D{{Key: "collaberator_refs", Value: uidOID}})
	if err != nil {
		return nil, err
	}
	review, err := find(bson.D{{Key: "reviewer_refs", Value: uidOID}})
	if err != nil {
		return nil, err
	}
	readOnly, err := find(bson.D{{Key: "readOnly_refs", Value: uidOID}})
	if err != nil {
		return nil, err
	}
	tokenRW, err := find(bson.D{
		{Key: "tokenAccessReadAndWrite_refs", Value: uidOID},
		{Key: "publicAccesLevel", Value: "tokenBased"},
	})
	if err != nil {
		return nil, err
	}
	tokenRO, err := find(bson.D{
		{Key: "tokenAccessReadOnly_refs", Value: uidOID},
		{Key: "publicAccesLevel", Value: "tokenBased"},
	})
	if err != nil {
		return nil, err
	}

	return &buckets{
		owned:             owned,
		readWrite:         readWrite,
		review:            review,
		readOnly:          readOnly,
		tokenReadAndWrite: tokenRW,
		tokenReadOnly:     tokenRO,
	}, nil
}

// docRef is one project doc reduced to the fields the list needs (hex ids so
// membership comparison behaves like Node's `.equals(userId)`).
type docRef struct {
	id       string
	name     string
	archived []string
	trashed  []string
}

// ---------- primitive.D field readers ----------

func dget(d primitive.D, key string) any {
	for _, e := range d {
		if e.Key == key {
			return e.Value
		}
	}
	return nil
}

func asStr(v any) string {
	s, ok := v.(string)
	if ok {
		return s
	}
	return ""
}

func oidHex(v any) string {
	oid, ok := v.(primitive.ObjectID)
	if ok {
		return oid.Hex()
	}
	// Real projects store ids as plain hex strings in several places
	// (overleaf.history.id for Node/Go-created projects) — accept those.
	if hex, ok := v.(string); ok && len(hex) == 24 {
		for i := 0; i < 24; i++ {
			c := hex[i]
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return ""
			}
		}
		return hex
	}
	return ""
}

func asOIDList(v any) []string {
	var out []string
	a, ok := v.(primitive.A)
	if !ok {
		return out
	}
	for _, e := range a {
		if s := oidHex(e); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func docRefFrom(d primitive.D) docRef {
	return docRef{
		id:       oidHex(dget(d, "_id")),
		name:     asStr(dget(d, "name")),
		archived: asOIDList(dget(d, "archived")),
		trashed:  asOIDList(dget(d, "trashed")),
	}
}
