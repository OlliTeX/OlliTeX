package sitesettings

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// bsonValToAny converts a mongo-decoded value into an ordered Go JSON value
// (string/bool/int64/int32/float64/nil/[]any/Obj), preserving key order
// (the driver yields nested docs as primitive.D and arrays as primitive.A
// when the target is primitive.D).
func bsonValToAny(v any) any {
	switch t := v.(type) {
	case primitive.D:
		return bsonDocToObj(t)
	case primitive.M:
		// unordered map — sort keys for determinism (should not occur when
		// decoding into primitive.D).
		out := Obj{}
		for k, val := range t {
			out = append(out, KV{k, bsonValToAny(val)})
		}
		return out
	case primitive.A:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = bsonValToAny(e)
		}
		return out
	case primitive.DateTime:
		return t.Time()
	case nil:
		return nil
	default:
		return v // string/bool/int32/int64/float64/double pass through
	}
}

func bsonDocToObj(d primitive.D) Obj {
	out := make(Obj, 0, len(d))
	for _, el := range d {
		out = append(out, KV{el.Key, bsonValToAny(el.Value)})
	}
	return out
}

// loadAllSections reads the single site_settings document once and returns
// an ordered section-name → Obj map (only sections present in the doc).
func loadAllSections(ctx context.Context, db *mongo.Database) map[string]Obj {
	sections := map[string]Obj{}
	var doc primitive.D
	err := db.Collection("site_settings").
		FindOne(ctx, bson.D{{Key: "_id", Value: "global"}}).Decode(&doc)
	if err != nil {
		return sections // no doc → all stored sections empty
	}
	for _, el := range doc {
		if el.Key == "_id" {
			continue
		}
		if od, ok := el.Value.(primitive.D); ok {
			sections[el.Key] = bsonDocToObj(od)
		}
	}
	return sections
}
