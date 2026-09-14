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
	"encoding/json"
	"net/http"
	"ollitex/go/services/web/core"
	"regexp"
	"time"
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

// toUtcMidnight — UTC midnight for a given instant (Node day bucket).
func toUtcMidnight(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// computeCutoff — Node computeCutoff(window, now, retentionDays=365):
// 'all' → retentionDays; WINDOWS[window] ?? 30; bucketed at UTC midnight.

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

// numberOr — Node `config?.diskWarningPercent ?? 90`: emit the stored
// number verbatim (float64 renders like JSON.stringify(Number)); fall back
// to the default only when the field is absent or non-numeric.
func numberOr(v any, d float64) float64 {
	if f, ok := toFloat(v); ok {
		return f
	}
	return d
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
