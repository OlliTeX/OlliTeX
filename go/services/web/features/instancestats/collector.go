// collector.go — POST /internal/collect-instance-stats (cron-collected
// instance statistics). Re-implements the retired Node module collector
// (services/web/modules/instance-stats/app/src/collectInstanceStats.mjs,
// recovered from git c131867093 — P7 step-4 removed the Node tree): same
// stat keys, same {statKey, day, values, generatedAt} upsert shape the
// Go series endpoint reads, same best-effort semantics (every subsystem
// that fails is reported as zero/best-available while the run still
// succeeds for the rest), same 365-day retention pruning.
//
// Live audit issue 002 (2026-09-30): the hub's site.general.stats leaf
// showed empty series because (a) the collector route never existed in
// the Go stack (Node retired, Go route never ported) and (b) the cron
// daemon did not run in the container. Both halves fixed: this route +
// the container cron service with the instance-stats crontab.
package instancestats

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"ollitex/go/libraries/ometrics"
	"ollitex/go/services/web/core"
)

const collectDataDir = "/var/lib/overleaf"
const retentionDays = 365 // Node `Settings.instanceStats?.retentionDays ?? 365`

// collectForDay — Node collectForDay({day}), same metric semantics:
//
//	active_projects   projects.lastUpdated >= now-24h
//	active_users      users.lastActive >= now-24h
//	new_users         users.signUpDate >= now-24h
//	shared_projects   projects.'tokens.readAndWrite' exists
//	user_count        users total
//	project_count     projects total
//	file_count        docs total
//	mongodb_storage   dbStats.totalSize (bytes)
//	overleaf_storage  du -b0s <data dir>
//	redis_storage     [aof_current_size, used_memory]
//	disk_usage        [availableBytes, totalBytes] (df -B1)
//	cpu_load          [loadavg1] (/proc/loadavg)
//	ram_usage         [freeBytes, usedBytes] (/proc/meminfo)
//
// Returns the computed scalar stats (statKey → latest value) so the caller
// can mirror them as Prometheus gauges (AG dashboards read the same numbers
// the hub series read).
func collectForDay(ctx context.Context, a *core.App, day time.Time) (scalars map[string]float64, errs []string) {
	scalars = map[string]float64{}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, []string{"mongo: " + err.Error()}
	}
	now := time.Now()
	oneDayAgo := now.Add(-24 * time.Hour)

	counts := func(coll string, filter interface{}) int64 {
		n, cerr := db.Collection(coll).CountDocuments(ctx, filter)
		if cerr != nil {
			errs = append(errs, "count."+coll+": "+cerr.Error())
		}
		return n
	}

	activeProjects := counts("projects", bson.D{{Key: "lastUpdated", Value: bson.D{{Key: "$gte", Value: oneDayAgo}}}})
	projectCount := counts("projects", bson.D{})
	activeUsers := counts("users", bson.D{{Key: "lastActive", Value: bson.D{{Key: "$gte", Value: oneDayAgo}}}})
	totalUsers := counts("users", bson.D{})
	newUserCount := counts("users", bson.D{{Key: "signUpDate", Value: bson.D{{Key: "$gte", Value: oneDayAgo}}}})
	fileCount := counts("docs", bson.D{})
	sharedProjectCount := counts("projects", bson.D{{Key: "tokens.readAndWrite", Value: bson.D{{Key: "$exists", Value: true}}}})

	// Mongo storage = dbStats.totalSize (lower than du: journaling files are
	// not measurable from the driver — same note as the Node collector).
	mongodbStorage := int64(0)
	var statsOut bson.M
	if serr := db.RunCommand(ctx, bson.D{{Key: "dbStats", Value: 1}, {Key: "scale", Value: 1}}).Decode(&statsOut); serr != nil {
		errs = append(errs, "dbStats: "+serr.Error())
	}
	if v, ok := statsOut["size"]; ok {
		if f, ok := toFloat(v); ok {
			mongodbStorage = int64(f)
		}
	}

	duBytes := duBytes(ctx, collectDataDir)
	diskAvail, diskTotal, diskOK := diskUsage(ctx, collectDataDir)
	if !diskOK {
		errs = append(errs, "df /var/lib/overleaf failed")
	}
	cpuLoad := getCpuLoad()
	redisDisk, redisRam := getRedisMemory(a)
	ramFree, ramUsed := getRamUsage()

	// AG: the scalar mirror the Grafana dashboards read (Prometheus
	// gauges on the web service's own scrape surface — no new datasource,
	// no static token; values = exactly what the hub series store).
	scalars = map[string]float64{
		"active_projects":  float64(activeProjects),
		"active_users":     float64(activeUsers),
		"new_users":        float64(newUserCount),
		"shared_projects":  float64(sharedProjectCount),
		"user_count":       float64(totalUsers),
		"project_count":    float64(projectCount),
		"file_count":       float64(fileCount),
		"mongodb_storage":  float64(mongodbStorage),
		"overleaf_storage": float64(duBytes),
		"redis_storage":    float64(redisRam),
		"disk_usage_avail": float64(diskAvail),
		"disk_usage_total": float64(diskTotal),
		"cpu_load":         cpuLoad,
		"ram_usage_free":   float64(ramFree),
		"ram_usage_used":   float64(ramUsed),
	}

	generatedAt := now
	entries := []map[string]any{
		{"statKey": "active_projects", "values": []int64{activeProjects}},
		{"statKey": "active_users", "values": []int64{activeUsers}},
		{"statKey": "new_users", "values": []int64{newUserCount}},
		{"statKey": "shared_projects", "values": []int64{sharedProjectCount}},
		{"statKey": "user_count", "values": []int64{totalUsers}},
		{"statKey": "project_count", "values": []int64{projectCount}},
		{"statKey": "file_count", "values": []int64{fileCount}},
		{"statKey": "mongodb_storage", "values": []int64{mongodbStorage}},
		{"statKey": "overleaf_storage", "values": []int64{duBytes}},
		{"statKey": "redis_storage", "values": []int64{redisDisk, redisRam}},
		{"statKey": "disk_usage", "values": []int64{diskAvail, diskTotal}},
		{"statKey": "cpu_load", "values": []float64{cpuLoad}},
		{"statKey": "ram_usage", "values": []int64{ramFree, ramUsed}},
	}
	upsert := options.Replace().SetUpsert(true)
	for _, e := range entries {
		statKey, _ := e["statKey"].(string)
		_, uerr := db.Collection("instanceStats").ReplaceOne(
			ctx,
			bson.D{{Key: "statKey", Value: statKey}, {Key: "day", Value: day}},
			bson.D{
				{Key: "statKey", Value: statKey},
				{Key: "day", Value: day},
				{Key: "values", Value: e["values"]},
				{Key: "generatedAt", Value: generatedAt},
			},
			upsert,
		)
		if uerr != nil {
			errs = append(errs, "upsert."+statKey+": "+uerr.Error())
		}
	}

	// pruneExpired — retention 365d, best-effort (non-fatal like Node).
	pruneCutoff := toUtcMidnight(time.Now().Add(-time.Duration(retentionDays+1) * 24 * time.Hour))
	if _, perr := db.Collection("instanceStats").DeleteMany(ctx, bson.D{{Key: "day", Value: bson.D{{Key: "$lt", Value: pruneCutoff}}}}); perr != nil {
		errs = append(errs, "prune: "+perr.Error())
	}
	return scalars, errs
}

func duBytes(ctx context.Context, path string) int64 {
	out, err := exec.CommandContext(ctx, "du", "-b0s", path).Output()
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	return n
}

func diskUsage(ctx context.Context, path string) (avail, total int64, ok bool) {
	out, err := exec.CommandContext(ctx, "df", "-B1", path).Output()
	if err != nil {
		return 0, 0, false
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return 0, 0, false
	}
	// df -B1 layout: Filesystem 1B-blocks Used Available Capacity Mounted-on
	cols := strings.Fields(lines[1])
	if len(cols) < 4 {
		return 0, 0, false
	}
	total, _ = strconv.ParseInt(cols[1], 10, 64)
	avail, _ = strconv.ParseInt(cols[3], 10, 64)
	return avail, total, true
}

func getCpuLoad() float64 {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	parts := strings.Fields(string(b))
	if len(parts) < 1 {
		return 0
	}
	v, _ := strconv.ParseFloat(parts[0], 64)
	return v
}

func parseRedisInfoInt(info, key string) int64 {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:(\d+)`)
	if m := re.FindStringSubmatch(info); m != nil {
		n, _ := strconv.ParseInt(m[1], 10, 64)
		return n
	}
	return 0
}

// getRedisMemory — Node getRedisMemory(): [aof_current_size, used_memory];
// zeros on failure so the collector still succeeds for the other metrics.
func getRedisMemory(a *core.App) (diskBytes, ramBytes int64) {
	if a == nil || a.Redis == nil {
		return 0, 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if info, err := a.Redis.RawDo(ctx, "INFO", "memory"); err == nil {
		if s, ok := info.(string); ok {
			ramBytes = parseRedisInfoInt(s, "used_memory")
		}
	}
	if info, err := a.Redis.RawDo(ctx, "INFO", "persistence"); err == nil {
		if s, ok := info.(string); ok {
			diskBytes = parseRedisInfoInt(s, "aof_current_size")
		}
	}
	return diskBytes, ramBytes
}

func getRamUsage() (freeBytes, usedBytes int64) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	reTotal := regexp.MustCompile(`(?m)^MemTotal:\s*(\d+)\s*kB`)
	reFree := regexp.MustCompile(`(?m)^MemFree:\s*(\d+)\s*kB`)
	var t, f int64
	if m := reTotal.FindSubmatch(b); m != nil {
		t, _ = strconv.ParseInt(string(m[1]), 10, 64)
	}
	if m := reFree.FindSubmatch(b); m != nil {
		f, _ = strconv.ParseInt(string(m[1]), 10, 64)
	}
	t, f = t*1024, f*1024
	used := t - f
	if used < 0 {
		used = 0
	}
	return f, used
}

// collectHandler — POST /internal/collect-instance-stats, private-API gated
// (WEB_API_USER/PASSWORD basic auth — the same private-API contract the
// other cron internals use). Response: {"ok":true,"day":...} plus a
// "warnings" array when any sub-metric degraded (the run still stored the
// best-available values, Node semantics).
func collectHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.APIBasicGate(cxt, res, cxt.Req) {
			return
		}
		ctx := cxt.Req.Context()
		ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		day := toUtcMidnight(time.Now())
		scalars, errs := collectForDay(ctx, a, day)
		// AG: mirror the run's scalar stats as Prometheus gauges on THIS
		// service's /metrics surface (Prometheus already scrapes ollitex-web
		// — no new datasource, no credential anywhere). The Grafana
		// "Instance statistics" dashboard reads overleaf_stat_* from here.
		for k, v := range scalars {
			ometrics.Gauge("stat."+k, v, map[string]any{"source": "collector"})
		}
		body := map[string]any{
			"ok":  len(errs) == 0,
			"day": day.Format(time.RFC3339),
		}
		if len(errs) > 0 {
			body["warnings"] = errs
		}
		raw, _ := json.Marshal(body)
		res.JSON(200, raw)
	}
}
