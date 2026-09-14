package instancestats

import (
	"context"
	"encoding/json"
	"ollitex/go/services/web/core"
	"os"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ---------- series ----------

type seriesPoint struct {
	Day    int64 `json:"day"`
	Values []any `json:"values"`
}

func series(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		q := cxt.Req.URL.Query()
		// Node: req.query.metric — duplicate params yield an ARRAY, which
		// fails the `typeof metric !== 'string'` check (400). Mirror that.
		metricVals := q["metric"]
		windowVals := q["window"]
		if len(metricVals) > 1 || len(windowVals) > 1 {
			if len(metricVals) > 1 {
				res.JSON(400, []byte(`{"message":"Invalid metric"}`))
				return
			}
			res.JSON(400, []byte(`{"message":"Invalid window"}`))
			return
		}
		metric := ""
		if len(metricVals) == 1 {
			metric = metricVals[0]
		}
		window := ""
		if len(windowVals) == 1 {
			window = windowVals[0]
		}
		if window == "" {
			window = "month" // Node: req.query.window || 'month'
		}
		if !isValidMetric(metric) { // Node order: metric check first
			res.JSON(400, []byte(`{"message":"Invalid metric"}`))
			return
		}
		if !validateWindow(window) {
			res.JSON(400, []byte(`{"message":"Invalid window"}`))
			return
		}
		// Node: Settings.instanceStats.retentionDays =
		// intFromEnv('INSTANCE_STATS_RETENTION_DAYS', 365)
		retention := 365
		if s := os.Getenv("INSTANCE_STATS_RETENTION_DAYS"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				retention = n
			}
		}
		cutoffMS := computeCutoff(window, time.Now(), retention).UnixMilli()

		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 5*time.Second)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			res.SendStatus(500)
			return
		}
		cur, err := db.Collection("instanceStats").Find(ctx, bson.D{
			{Key: "statKey", Value: metric},
			// day is a BSON Date in the docs — query with a Date too (an
			// int64 would fall in the Number type-bracket and the $gte
			// ordering would match every doc regardless of the cutoff).
			{Key: "day", Value: bson.D{{Key: "$gte", Value: primitive.DateTime(cutoffMS)}}},
		}, options.Find().SetSort(bson.D{{Key: "day", Value: 1}}))
		if err != nil {
			res.SendStatus(500)
			return
		}
		defer cur.Close(ctx)

		points := make([]seriesPoint, 0)
		for cur.Next(ctx) {
			var bm bson.M
			if err := cur.Decode(&bm); err != nil {
				continue
			}
			var dayMS int64
			switch v := bm["day"].(type) {
			case primitive.DateTime: // driver v1 decodes BSON Date in a map
				dayMS = int64(v)
			case time.Time:
				dayMS = v.UnixMilli()
			case int64:
				dayMS = v
			case int32:
				dayMS = int64(v)
			}
			points = append(points, seriesPoint{Day: dayMS, Values: normalizeValues(bm["values"])})
		}
		if err := cur.Err(); err != nil {
			res.SendStatus(500)
			return
		}

		body, err := json.Marshal(struct {
			Metric string        `json:"metric"`
			Window string        `json:"window"`
			Points []seriesPoint `json:"points"`
		}{Metric: metric, Window: window, Points: points})
		if err != nil {
			res.SendStatus(500)
			return
		}
		res.JSON(200, body)
	}
}

// normalizeValues — Node `values: [Number]`: whole-number doubles must
// serialize without a decimal point (Go's encoding/json does that for
// integer float64s; the driver yields int64 for BSON ints).

// normalizeValues — Node `values: [Number]`: whole-number doubles must
// serialize without a decimal point (Go's encoding/json does that for
// integer float64s; the driver yields int64 for BSON ints).
func normalizeValues(v any) []any {
	arr, ok := v.([]any)
	if !ok {
		if pa, ok2 := v.(primitive.A); ok2 { // driver v1 BSON array type
			arr = pa
			ok = true
		}
	}
	if !ok {
		out := []any{}
		if v != nil {
			out = append(out, v)
		}
		return out
	}
	out := []any{}
	for _, e := range arr {
		switch n := e.(type) {
		case float64:
			if n == float64(int64(n)) {
				out = append(out, int64(n))
			} else {
				out = append(out, n)
			}
		case int32:
			out = append(out, int64(n))
		default:
			out = append(out, e)
		}
	}
	return out
}

// ---------- alert config ----------

// ---------- alert config ----------

func configEmails(doc map[string]any) []string {
	out := []string{}
	var arr []any
	switch v := doc["alertEmails"].(type) {
	case []any:
		arr = v
	case primitive.A:
		arr = v
	case []string:
		for _, s := range v {
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	for _, x := range arr {
		if s, ok := x.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		if s, ok := doc["alertEmail"].(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}
