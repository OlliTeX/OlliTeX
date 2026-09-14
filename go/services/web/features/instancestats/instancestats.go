// Package instancestats ports the instance-stats module (P3.2).
//
// Node sources (oracle):
//
//	services/web/modules/instance-stats/app/src/InstanceStatsRouter.mjs
//	services/web/modules/instance-stats/app/src/InstanceStatsController.mjs
//	services/web/modules/instance-stats/app/src/instanceStatsConstants.mjs
//	services/web/modules/instance-stats/app/src/models/InstanceStatAlertConfig.mjs
//
// Routes (web profile, all require site-admin):
//
//	GET  /admin/instance-stats                 → 301 /hub#/site.general.stats (retired page)
//	GET  /admin/instance-stats/api/series      → {metric, window, points:[{day, values}]}
//	GET  /admin/instance-stats/api/alert-config
//	PUT  /admin/instance-stats/api/alert-config
//	POST /admin/instance-stats/api/send-test-alert-email
//
// The `privateApiRouter` route /internal/collect-instance-stats (cron
// collector on the api profile, port 3000) stays with the Node api process
// until Go owns the api profile — the web flip targets webRouter paths only.
//
// Collections (EXPLICIT names in the Node models — no mongoose
// auto-pluralization involved):
//
//	instanceStats             (model InstanceStat)
//	instanceStatAlertConfigs  (model InstanceStatAlertConfig; single doc,
//	                        string _id "instance-stats")
package instancestats

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"ollitex/go/services/web/core"
)

// ---------- constants (instanceStatsConstants.mjs, pinned) ----------

var STAT_KEYS = []string{
	"active_projects",
	"active_users",
	"new_users",
	"shared_projects",
	"user_count",
	"project_count",
	"file_count",
	"mongodb_storage",
	"overleaf_storage",
	"redis_storage",
	"disk_usage",
	"cpu_load",
	"ram_usage",
}

var WINDOWS = map[string]int{
	"day":   1,
	"week":  7,
	"month": 30,
	"6m":    180,
	"year":  365,
}

const alertConfigID = "instance-stats"

// toUtcMidnight — UTC midnight for a given instant (Node day bucket).
func toUtcMidnight(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// computeCutoff — Node computeCutoff(window, now, retentionDays=365):
// 'all' → retentionDays; WINDOWS[window] ?? 30; bucketed at UTC midnight.
func computeCutoff(window string, now time.Time, retentionDays int) time.Time {
	days := 30 // Node `?? 30` — unreachable for validated windows
	if window == "all" {
		days = retentionDays
	}
	if w, ok := WINDOWS[window]; ok {
		days = w
	}
	return toUtcMidnight(now.Add(-time.Duration(days) * 24 * time.Hour))
}

func validateWindow(window string) bool {
	if window == "all" {
		return true
	}
	_, ok := WINDOWS[window]
	return ok
}

func isValidMetric(metric string) bool {
	for _, k := range STAT_KEYS {
		if k == metric {
			return true
		}
	}
	return false
}

// ---------- regexes (JS \s ported exactly) ----------

// jsWSElems — the JavaScript \s class (incl. U+000B and the Unicode
// spaces) WITHOUT brackets, interpolated into the classes below.
const jsWSElems = `\t\n\v\f\r \x{00A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}`

var (
	// Node EMAIL_RE: /^[^\s]+@[^\s]+\.[^\s]+$/
	emailRe = regexp.MustCompile(`^[^` + jsWSElems + `]+@[^` + jsWSElems + `]+\.[^` + jsWSElems + `]+$`)
	// Node `s.split(/[\s,;]+/)`
	splitRe = regexp.MustCompile(`[` + jsWSElems + `,;]+`)
)

func validEmail(e string) bool { return emailRe.MatchString(e) }

// ---------- Feature ----------

func Feature(a *core.App) core.Feature {
	mail := core.NewMail() // same wiring as passwordreset (P2)
	return core.Feature{
		Name: "instancestats",
		Routes: []core.Route{
			{Method: "GET", Path: "/admin/instance-stats", Handler: page(a)},
			{Method: "GET", Path: "/admin/instance-stats/api/series", Handler: series(a)},
			{Method: "GET", Path: "/admin/instance-stats/api/alert-config", Handler: getAlertConfig(a)},
			{Method: "PUT", Path: "/admin/instance-stats/api/alert-config", Handler: saveAlertConfig(a)},
			{Method: "POST", Path: "/admin/instance-stats/api/send-test-alert-email", Handler: sendTestAlert(a, mail)},
		},
	}
}

// ---------- handlers ----------

// page — Node (Owner #23): retired page → 301 /hub#/site.general.stats.
// express res.redirect(301, url) — the Accept matrix is status-independent
// (pinned: html → `<p>Moved Permanently. Redirecting to …</p>`; text/*|*/*
// → plain; json → empty body, no CT; Vary: Accept).
func page(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		res.Redirect(cxt.Req, 301, "/hub#/site.general.stats")
	}
}

// ---------- series ----------

type seriesPoint struct {
	Day    int64   `json:"day"`
	Values []any   `json:"values"`
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

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int64:
		return int(n), true
	case int32:
		return int(n), true
	case int:
		return n, true
	case float64:
		if n == float64(int64(n)) {
			return int(n), true
		}
	}
	return 0, false
}

// numberOr — Node `config?.diskWarningPercent ?? 90`: emit the stored
// number verbatim (float64 renders like JSON.stringify(Number)); fall back
// to the default only when the field is absent or non-numeric.
func numberOr(v any, d float64) float64 {
	if f, ok := toFloat(v); ok {
		return f
	}
	return d
}

func getAlertConfig(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 5*time.Second)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			res.SendStatus(500)
			return
		}
		doc := map[string]any{}
		if err := db.Collection("instanceStatAlertConfigs").
			FindOne(ctx, bson.D{{Key: "_id", Value: alertConfigID}}).Decode(&doc); err != nil {
			doc = map[string]any{} // no config doc → defaults (Node: config = null)
		}
		emails := configEmails(doc)
		first := ""
		if len(emails) > 0 {
			first = emails[0]
		}
		body, _ := json.Marshal(struct {
			AlertEmails        []string `json:"alertEmails"`
			AlertEmail         string   `json:"alertEmail"`
			DiskWarningPercent float64  `json:"diskWarningPercent"`
			RamWarningPercent  float64  `json:"ramWarningPercent"`
		}{emails, first, numberOr(doc["diskWarningPercent"], 90), numberOr(doc["ramWarningPercent"], 90)})
		res.JSON(200, body)
	}
}

// normalizeEmails — Node normalizeEmails(body).
func normalizeEmails(body map[string]any) ([]string, string) {
	var raw []string
	hasSa := false
	switch v := body["alertEmails"].(type) {
	case []any:
		hasSa = true
		for _, x := range v {
			if s, ok := x.(string); ok {
				raw = append(raw, s)
			}
		}
	case primitive.A:
		hasSa = true
		for _, x := range v {
			if s, ok := x.(string); ok {
				raw = append(raw, s)
			}
		}
	case []string:
		hasSa = true
		raw = v
	case string:
		hasSa = true
		raw = splitRe.Split(v, -1)
	}
	// Node: the legacy alertEmail string is used ONLY when alertEmails is
	// not an array and not a string.
	if !hasSa {
		if s, ok := body["alertEmail"].(string); ok {
			raw = splitRe.Split(s, -1)
		}
	}
	seen := map[string]bool{}
	emails := []string{}
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		if !validEmail(s) {
			return nil, fmt.Sprintf("Invalid email address: %s", s)
		}
		seen[s] = true
		emails = append(emails, s)
	}
	return emails, ""
}

// toFloat — JSON number → float; bool/string/nil → not-a-number
// (Node: typeof value !== 'number').
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case int:
		return float64(n), true
	}
	return 0, false
}

// parseAlertConfigBody — Node parseAlertConfigBody(req.body) with the exact
// `{"message": ...}` error strings (pinned).
func parseAlertConfigBody(body map[string]any) (map[string]any, string) {
	emails, errStr := normalizeEmails(body)
	if errStr != "" {
		return nil, errStr
	}
	badErr := "must be a number between 1 and 100"
	disk, okD := toFloat(body["diskWarningPercent"])
	if !okD || disk < 1 || disk > 100 {
		return nil, "diskWarningPercent " + badErr
	}
	ram, okR := toFloat(body["ramWarningPercent"])
	if !okR || ram < 1 || ram > 100 {
		return nil, "ramWarningPercent " + badErr
	}
	first := ""
	if len(emails) > 0 {
		first = emails[0]
	}
	// Node passes the JS numbers through to $set: mongoose stores them as
	// BSON doubles (not int64). Non-integers (55.5) are legal and must
	// round-trip exactly.
	return map[string]any{
		"alertEmails":        emails,
		"alertEmail":         first,
		"diskWarningPercent": disk,
		"ramWarningPercent":  ram,
	}, ""
}

func parseJSONBody(r *http.Request) map[string]any {
	body := map[string]any{}
	if r.Body == nil {
		return body
	}
	var v any
	if err := json.NewDecoder(r.Body).Decode(&v); err == nil {
		if m, ok := v.(map[string]any); ok {
			body = m
		}
	}
	return body
}

func saveAlertConfig(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		body := parseJSONBody(cxt.Req)
		parsed, msg := parseAlertConfigBody(body)
		if msg != "" {
			b, _ := json.Marshal(map[string]string{"message": msg})
			res.JSON(400, b)
			return
		}
		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 5*time.Second)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			res.SendStatus(500)
			return
		}
		if _, err := db.Collection("instanceStatAlertConfigs").UpdateOne(ctx,
			bson.D{{Key: "_id", Value: alertConfigID}},
			bson.D{{Key: "$set", Value: parsed}},
			options.Update().SetUpsert(true),
		); err != nil {
			res.SendStatus(500)
			return
		}
		res.JSON(200, []byte(`{"ok":true}`))
	}
}

// sendTestAlert — Node sendTestAlertEmail: body {emails?: string|string[],
// email?: string}; one mail per normalized recipient; 400 shapes pinned.
func sendTestAlert(a *core.App, mail *core.Mail) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		body := parseJSONBody(cxt.Req)
		// Node reshapes req.body before normalizeEmails:
		//   { alertEmails: (typeof emails === 'string' ? emails.split(/[\s,;]+/)
		//                    : emails), alertEmail: email }
		nbody := map[string]any{}
		if v, ok := body["emails"]; ok {
			switch t := v.(type) {
			case string:
				// Keep the STRING — normalizeEmails splits it (Node: the split
				// result array is passed; observationally identical). The
				// split must NOT happen here: a pre-split []string is not a
				// case normalizeEmails handles and would silently drop all
				// recipients (P3.2 live diff: mail_legacy 500 / mail_priority
				// 400 family).
				nbody["alertEmails"] = t
			default:
				nbody["alertEmails"] = v
			}
		}
		if v, ok := body["email"]; ok {
			if s, ok := v.(string); ok {
				nbody["alertEmail"] = s
			}
		}
		emails, errStr := normalizeEmails(nbody)
		if errStr != "" {
			b, _ := json.Marshal(map[string]string{"message": errStr})
			res.JSON(400, b)
			return
		}
		if len(emails) == 0 {
			res.JSON(400, []byte(`{"message":"Invalid email address"}`))
			return
		}
		subject := "[Overleaf] Instance stats alert test"
		html := "<p>This is a test email from the Instance Statistics alert configuration.</p>"
		text := "This is a test email from the Instance Statistics alert configuration."
		if mail != nil {
			for _, to := range emails {
				if err := mail.Send(to, subject, text, html); err != nil {
					log.Printf("instancestats: mail send to %s failed: %v", to, err)
					res.SendStatus(500)
					return
				}
			}
		}
		b, _ := json.Marshal(map[string]any{"ok": true, "sentTo": emails})
		res.JSON(200, b)
	}
}
