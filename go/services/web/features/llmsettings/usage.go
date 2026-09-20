package llmsettings

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"time"

	"ollitex/go/services/web/core"
)

// usageSummary — Node getUsageSummary result shape.
type usageSummary struct {
	days         int
	calls        int64
	inputTokens  int64
	outputTokens int64
	totalTokens  int64
	byDay        []any
	byAction     []any
	byModel      []any
}

// getUsageSummary — Node LLMUsage.getUsageSummary({userId, days}).
//
//	userId "" = site scope (no userId filter, as in the Node null case).
//
// limit = min(max(Number(days)||30, 1), 365); from = local midnight of
// (today - (limit-1)); byDay is the CONTIGUOUS range from..today (zeros
// included); byAction top-12 by totalTokens desc; byModel top-8.
func getUsageSummary(ctx context.Context, a *core.App, userId string, days int) (*usageSummary, error) {
	limit := days
	if limit == 0 {
		limit = 30
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 365 {
		limit = 365
	}
	loc := time.Local
	now := time.Now()
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(limit - 1))

	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, err
	}
	filter := bson.M{"createdAt": bson.M{"$gte": from}}
	if userId != "" {
		filter["userId"] = userId
	}
	coll := db.Collection("llmusages")

	var calls, inputTokens, outputTokens, totalTokens int64
	cur, err := coll.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: filter}},
		{{Key: "$group", Value: bson.M{
			"_id":          nil,
			"calls":        bson.M{"$sum": 1},
			"inputTokens":  bson.M{"$sum": "$inputTokens"},
			"outputTokens": bson.M{"$sum": "$outputTokens"},
			"totalTokens":  bson.M{"$sum": "$totalTokens"},
		}}},
	})
	if err != nil {
		return nil, err
	}
	var totDocs []map[string]any
	if err := cur.All(ctx, &totDocs); err != nil {
		return nil, err
	}
	if len(totDocs) > 0 {
		d := totDocs[0]
		calls = toInt64(d["calls"])
		inputTokens = toInt64(d["inputTokens"])
		outputTokens = toInt64(d["outputTokens"])
		totalTokens = toInt64(d["totalTokens"])
	}

	byDayEntries := map[string]map[string]any{}
	cur2, err := coll.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: filter}},
		{{Key: "$group", Value: bson.M{"_id": "$day", "calls": bson.M{"$sum": 1}, "totalTokens": bson.M{"$sum": "$totalTokens"}}}},
		{{Key: "$sort", Value: bson.M{"_id": 1}}},
	})
	if err != nil {
		return nil, err
	}
	var dayDocs []map[string]any
	if err := cur2.All(ctx, &dayDocs); err != nil {
		return nil, err
	}
	for _, d := range dayDocs {
		id, _ := d["_id"].(string)
		byDayEntries[id] = d
	}
	byDay := make([]any, 0, limit)
	cursor := from
	for !cursor.After(now) {
		dayStr := cursor.Format("2006-01-02")
		entry := jobj("day", dayStr, "calls", int64(0), "totalTokens", int64(0))
		if d, ok := byDayEntries[dayStr]; ok {
			entry = entry.set("calls", toInt64(d["calls"]))
			entry = entry.set("totalTokens", toInt64(d["totalTokens"]))
		}
		byDay = append(byDay, entry)
		cursor = cursor.AddDate(0, 0, 1)
	}

	byAction, byModel, err := topGroups(ctx, coll, filter, "action", "other", 12, "model", "unknown", 8)
	if err != nil {
		return nil, err
	}

	return &usageSummary{
		days: limit, calls: calls, inputTokens: inputTokens, outputTokens: outputTokens, totalTokens: totalTokens,
		byDay: byDay, byAction: byAction, byModel: byModel,
	}, nil
}

// topGroups — byAction (label key "action", top 12) + byModel (label key
// "model", top 8), sorted totalTokens desc (Node Mongo pipeline).
func topGroups(ctx context.Context, coll *mongo.Collection, filter bson.M,
	actionKey, actionDflt string, actionLimit int,
	modelKey, modelDflt string, modelLimit int) ([]any, []any, error) {

	aggre := func(idExpr, label string, dflt string, limit int) ([]any, error) {
		cur, err := coll.Aggregate(ctx, mongo.Pipeline{
			{{Key: "$match", Value: filter}},
			{{Key: "$group", Value: bson.M{"_id": "$" + idExpr, "calls": bson.M{"$sum": 1}, "totalTokens": bson.M{"$sum": "$totalTokens"}}}},
			{{Key: "$sort", Value: bson.D{{Key: "totalTokens", Value: -1}}}},
			{{Key: "$limit", Value: limit}},
		})
		if err != nil {
			return nil, err
		}
		var docs []map[string]any
		if err := cur.All(ctx, &docs); err != nil {
			return nil, err
		}
		out := make([]any, 0, len(docs))
		for _, d := range docs {
			id, _ := d["_id"].(string)
			if id == "" {
				id = dflt
			}
			out = append(out, jobj(label, id, "calls", toInt64(d["calls"]), "totalTokens", toInt64(d["totalTokens"])))
		}
		return out, nil
	}
	byAction, err := aggre(actionKey, actionKey, actionDflt, actionLimit)
	if err != nil {
		return nil, nil, err
	}
	byModel, err := aggre(modelKey, modelKey, modelDflt, modelLimit)
	if err != nil {
		return nil, nil, err
	}
	return byAction, byModel, nil
}

func toInt64(v any) int64 {
	switch t := v.(type) {
	case int32:
		return int64(t)
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case float32:
		return int64(t)
	}
	return 0
}

var _ = options.FindOne
