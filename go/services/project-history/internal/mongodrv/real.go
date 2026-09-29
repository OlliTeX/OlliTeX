package mongodrv

// real.go — the concrete official-driver binding. mongodrv's fake-friendly
// core (driver.go) stays import-light; this file carries the
// go.mongodb.org/mongo-driver types.

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	phmongo "ollitex/go/services/project-history/internal/mongo"
)

// realColl — *mongo.Collection mapped onto DriverCollection.
type realColl struct{ c *mongo.Collection }

var _ DriverCollection = realColl{}

func (r realColl) FindOne(ctx context.Context, filter map[string]any, opts map[string]any) (map[string]any, error) {
	o := options.FindOne()
	if p, ok := optMap(opts["projection"]); ok {
		o = o.SetProjection(bson.M(p))
	}
	if s, ok := opts["sort"]; ok && s != nil {
		o = o.SetSort(s)
	}
	var out map[string]any
	err := r.c.FindOne(ctx, bson.M(filter), o).Decode(&out)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrNoDocuments
		}
		return nil, err
	}
	return out, nil
}

func (r realColl) Find(ctx context.Context, filter map[string]any, opts map[string]any) ([]map[string]any, error) {
	o := options.Find()
	if p, ok := optMap(opts["projection"]); ok {
		o = o.SetProjection(bson.M(p))
	}
	if s, ok := opts["sort"]; ok && s != nil {
		o = o.SetSort(s)
	}
	if l, ok := opts["limit"]; ok && l != nil {
		if n, ok := l.(int64); ok {
			o = o.SetLimit(n)
		}
	}
	cur, err := r.c.Find(ctx, bson.M(filter), o)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var all []map[string]any
	if err := cur.All(ctx, &all); err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(all))
	for _, d := range all {
		out = append(out, normalizeKeys(d))
	}
	return out, nil
}

func (r realColl) InsertOne(ctx context.Context, doc map[string]any) (any, error) {
	res, err := r.c.InsertOne(ctx, bson.M(doc))
	if err != nil {
		return nil, err
	}
	return res.InsertedID, nil
}

func (r realColl) InsertMany(ctx context.Context, docs []map[string]any) error {
	vals := make([]interface{}, 0, len(docs))
	for _, d := range docs {
		vals = append(vals, bson.M(d))
	}
	_, err := r.c.InsertMany(ctx, vals)
	return err
}

func (r realColl) UpdateOne(ctx context.Context, filter map[string]any, update map[string]any, opts map[string]any) (*phmongo.UpdateResult, error) {
	res, err := r.c.UpdateOne(ctx, bson.M(filter), bson.M(update), upsertOneOpts(opts))
	if err != nil {
		return nil, err
	}
	return &phmongo.UpdateResult{
		MatchedCount:  res.MatchedCount,
		ModifiedCount: res.ModifiedCount,
		UpsertedCount: res.UpsertedCount,
		UpsertedID:    res.UpsertedID,
	}, nil
}

func (r realColl) UpdateMany(ctx context.Context, filter map[string]any, update map[string]any, opts map[string]any) (*phmongo.UpdateResult, error) {
	res, err := r.c.UpdateMany(ctx, bson.M(filter), bson.M(update), upsertManyOpts(opts))
	if err != nil {
		return nil, err
	}
	return &phmongo.UpdateResult{
		MatchedCount:  res.MatchedCount,
		ModifiedCount: res.ModifiedCount,
		UpsertedCount: res.UpsertedCount,
		UpsertedID:    res.UpsertedID,
	}, nil
}

func (r realColl) DeleteOne(ctx context.Context, filter map[string]any) (int64, error) {
	res, err := r.c.DeleteOne(ctx, bson.M(filter))
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

func (r realColl) FindOneAndUpdate(ctx context.Context, filter map[string]any, update map[string]any, opts map[string]any) (map[string]any, error) {
	var out map[string]any
	err := r.c.FindOneAndUpdate(ctx, bson.M(filter), bson.M(update), fauOpts(opts)).Decode(&out)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrNoDocuments
		}
		return nil, err
	}
	return out, nil
}

func (r realColl) Distinct(ctx context.Context, field string, filter map[string]any) ([]any, error) {
	// v2 driver: Distinct returns a DistinctResult (decode into a slice).
	var vals []any
	if err := r.c.Distinct(ctx, field, bson.M(filter)).Decode(&vals); err != nil {
		return nil, err
	}
	out := make([]any, 0, len(vals))
	for _, v := range vals {
		out = append(out, v)
	}
	return out, nil
}

func (r realColl) CountDocuments(ctx context.Context, filter map[string]any) (int64, error) {
	return r.c.CountDocuments(ctx, bson.M(filter))
}

// realDB — *mongo.Database mapped onto DBDriver.
type realDB struct{ db *mongo.Database }

func (r realDB) Collection(name string) DriverCollection {
	return realColl{c: r.db.Collection(name)}
}

// DriverDB binds the official driver database. Call for `-what=serve`.
func DriverDB(db *mongo.Database) DBDriver { return realDB{db: db} }

func upsertOneOpts(opts map[string]any) *options.UpdateOneOptionsBuilder {
	o := options.UpdateOne()
	if b, ok := optBool(opts["upsert"]); ok {
		o = o.SetUpsert(b)
	}
	if c, ok := opts["collation"]; ok && c != nil {
		if cm, ok := c.(map[string]any); ok {
			o = o.SetCollation(&options.Collation{Locale: stringVal(cm["locale"])})
		}
	}
	return o
}
func upsertManyOpts(opts map[string]any) *options.UpdateManyOptionsBuilder {
	o := options.UpdateMany()
	if b, ok := optBool(opts["upsert"]); ok {
		o = o.SetUpsert(b)
	}
	if c, ok := opts["collation"]; ok && c != nil {
		if cm, ok := c.(map[string]any); ok {
			o = o.SetCollation(&options.Collation{Locale: stringVal(cm["locale"])})
		}
	}
	return o
}

func fauOpts(opts map[string]any) *options.FindOneAndUpdateOptionsBuilder {
	o := options.FindOneAndUpdate()
	if b, ok := optBool(opts["upsert"]); ok {
		o = o.SetUpsert(b)
	}
	if p, ok := optMap(opts["projection"]); ok {
		o = o.SetProjection(bson.M(p))
	}
	if s, ok := opts["sort"]; ok && s != nil {
		o = o.SetSort(s)
	}
	if a, ok := opts["arrayFilters"]; ok && a != nil {
		if arr, ok := a.([]map[string]any); ok {
			aff := make([]any, len(arr))
			for i, af := range arr {
				aff[i] = bson.M(af)
			}
			o = o.SetArrayFilters(aff)
		}
	}
	if c, ok := opts["collation"]; ok && c != nil {
		if cm, ok := c.(map[string]any); ok {
			o = o.SetCollation(&options.Collation{Locale: stringVal(cm["locale"])})
		}
	}
	return o
}

func optMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func optBool(v any) (bool, bool) {
	b, ok := v.(bool)
	return b, ok
}

func stringVal(v any) string {
	s, _ := v.(string)
	return s
}

func afterDoc(b bool) options.ReturnDocument {
	if b {
		return options.After
	}
	return options.Before
}

// normalizeKeys — the driver decodes into map[string]interface{} already.
func normalizeKeys(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	return m
}
