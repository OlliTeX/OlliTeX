// Package projectpersistence ports services/clsi_typst/app/js/
// ProjectPersistenceManager.js — the REDUCED clsi PPM (clsi's PPM drags in
// clsi CompileManager -> LatexRunner -> DockerRunner, the module graph we
// want to avoid).
//
// Node parity notes (typst baseline = the Node file above):
//
//   - EXPIRY_TIMEOUT = Settings.project_cache_length_ms || oneDay * 2.5
//     (Node typst default project_cache_length_ms is 24h; the `||` fallback
//     = clsi-tex 2.5d convention, only reached when the config is unset).
//   - Node's PPM.init() runs NO seed scan and NO refreshExpiryTimeout
//     (unlike clsi tex). D10 (LEAN-Y: cheap and environment-correct)
//     re-adds both, additive over the Node baseline and never changing its
//     observable behavior: the seed scan over lastprojectaccess (clsi
//     5-10min jitter for non-matching names, dir mtime for project ids)
//     and the refresh (lower EXPIRY toward 90% on a disk-low path).
//   - init(callback) => Go Init(cfg, cm) starts the 10-minute interval
//     (collectDiskStats + clearExpiredProjects) and one immediate
//     collectDiskStats pass (Node: both on app start).
//   - LAST_ACCESS lives in clsi/lastprojectaccess (the dependency-free
//     package of the READ-ONLY clsi.go module, D22 import, no edit) exactly
//     as node/lastprojectaccess.js lives outside this file.
//   - Node emit Metrics.gauge('disk_available_percent', ..., {path}); the
//     Go port uses the clsi metrics.Gauge surface (no prom layer yet —
//     same documented divergence as clsi.go ppm).
//   - statfs: Node fs.promises.statfs, Go syscall.Statfs (Linux-only port).
package projectpersistence

import (
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"syscall"
	"time"

	compilemanager "ollitex/go/services/clsitypst/compilemanager"
	cltypstcfg "ollitex/go/services/clsitypst/config"
	"ollitex/go/services/clsitypst/lastprojectaccess"
	clsl "ollitex/go/services/clsitypst/logger"
	"ollitex/go/services/clsitypst/metrics"
)

// Node-constant durations (mirrors app/js/ProjectPersistenceManager.js).
const (
	expiryInterval      = 10 * time.Minute
	diskStatsInterval   = 15 * time.Second
	lowDiskPercent      = 10
	criticalDiskPercent = 3
)

// oneDayMs mirrors Node's oneDay = 24 * 60 * 60 * 1000 (ms).
const oneDayMs = int64(24 * 60 * 60 * 1000)

// projectAndUserRe mirrors /^[a-f0-9]{24}(-[a-f0-9]{24})?$/ (clsi seed-scan
// name, D10 LEAN-Y — not in the Node typst file).
var projectAndUserRe = regexp.MustCompile(`^[a-f0-9]{24}(-[a-f0-9]{24})?$`)

// Now mirrors Date.now() for tests.
var Now = func() int64 { return time.Now().UnixMilli() }

// randFloat is the jitter seam (Node: Math.random()).
var randFloat = rand.Float64

// StatFSSeams — tests inject a fake statfs (syscall.Statfs is not mockable).
var statFSSeams = func(path string) (total, available int64, err error) {
	var st syscall.Statfs_t
	if serr := syscall.Statfs(path, &st); serr != nil {
		return 0, 0, serr
	}
	return int64(st.Blocks) * int64(st.Bsize), int64(st.Bavail) * int64(st.Bsize), nil
}

// --- disk gauges (Node: Metrics.gauge('disk_available_percent', ...)) ------

// diskAvailablePercentGauges carries the per-path disk_available_percent
// gauges (Node: one per {path} label, set per collectDiskStats pass).
var (
	gaugeMu                    sync.Mutex
	diskAvailablePercentGauges map[string]*metrics.Gauge
)

func gaugeForPath(path string) *metrics.Gauge {
	gaugeMu.Lock()
	defer gaugeMu.Unlock()
	if diskAvailablePercentGauges == nil {
		diskAvailablePercentGauges = map[string]*metrics.Gauge{}
	}
	g, ok := diskAvailablePercentGauges[path]
	if !ok {
		g = &metrics.Gauge{Name: "disk_available_percent: " + path}
		diskAvailablePercentGauges[path] = g
	}
	return g
}

// removeAll mirrors fs.rm for tests (the force recursive remove).
var removeAll = func(path string) error { return os.RemoveAll(path) }

// --- EXPIRY_TIMEOUT (Node module-level mutable) ----------------------------

// expiryMS mirrors Node's EXPIRY_TIMEOUT module const (Settings.
// project_cache_length_ms || oneDay * 2.5); RefreshExpiry lowers it toward
// 90% (clsi parity, D10 LEAN-Y re-add).
var (
	expMu    sync.RWMutex
	expiryMS int64 = (oneDayMs * 25) / 10
)

func getExpiryMS() int64 {
	expMu.RLock()
	defer expMu.RUnlock()
	return expiryMS
}

func setExpiryMS(v int64) {
	expMu.Lock()
	expiryMS = v
	expMu.Unlock()
}

// ExpiryMS exposes EXPIRY_TIMEOUT (ms).
func ExpiryMS() int64 { return getExpiryMS() }

// --- disk state (Node ANY_DISK_LOW / ANY_DISK_CRITICAL_LOW) ------------------

var (
	diskMu      sync.RWMutex
	anyDiskLow  bool
	anyDiskCrit bool
)

// IsAnyDiskLow ports isAnyDiskLow().
func IsAnyDiskLow() bool {
	diskMu.RLock()
	defer diskMu.RUnlock()
	return anyDiskLow
}

// IsAnyDiskCriticalLow ports isAnyDiskCriticalLow().
func IsAnyDiskCriticalLow() bool {
	diskMu.RLock()
	defer diskMu.RUnlock()
	return anyDiskCrit
}

func setDiskState(low, critical bool) {
	diskMu.Lock()
	anyDiskLow = low
	anyDiskCrit = critical
	diskMu.Unlock()
}

// --- singleton wiring (Node app.js: ProjectPersistenceManager.init()) --------

var (
	singletonMu sync.RWMutex
	singletonCf *cltypstcfg.Config
	singletonCM *compilemanager.Manager

	cleanupStop chan struct{}
)

func setSingletons(cfg *cltypstcfg.Config, cm *compilemanager.Manager) {
	singletonMu.Lock()
	singletonCf = cfg
	singletonCM = cm
	singletonMu.Unlock()
}

func getSingleton() (cfg *cltypstcfg.Config, cm *compilemanager.Manager) {
	singletonMu.Lock()
	defer singletonMu.Unlock()
	return singletonCf, singletonCM
}

// SetExpiryFromConfig mirrors Node's module-const derivation:
// Settings.project_cache_length_ms || oneDay * 2.5 (Node default 24h; the
// clsi-tex 2.5d fallback is the env-UNSET guard only).
func SetExpiryFromConfig(cfg *cltypstcfg.Config) {
	if cfg.ProjectCacheLengthMs > 0 {
		setExpiryMS(int64(cfg.ProjectCacheLengthMs))
	} else {
		setExpiryMS((oneDayMs * 25) / 10)
	}
}

// ForTest sets singletons + expiry WITHOUT starting intervals (tests call
// the *Once functions directly).
func ForTest(cfg *cltypstcfg.Config, cm *compilemanager.Manager) {
	setSingletons(cfg, cm)
	SetExpiryFromConfig(cfg)
}

// runTick is the interval driver: runs fn on each tick until stop closes
// (clsi.go convention).
func runTick(period time.Duration, stop chan struct{}, fn func()) {
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			fn()
		case <-stop:
			return
		}
	}
}

// Init ports PPM.init(): sets EXPIRY_TIMEOUT, seeds LAST_ACCESS (D10
// LEAN-Y seed scan), starts the 10-minute interval (refreshExpiry +
// collectDiskStats + clearExpiredProjects), and one immediate
// collectDiskStats pass (Node: collectDiskStats + the interval body). cm
// must be non-nil (the expiry interval dereferences it).
func Init(cfg *cltypstcfg.Config, cm *compilemanager.Manager) {
	SetExpiryFromConfig(cfg)
	setSingletons(cfg, cm)
	RunDiskStatsOnce(cfg)
	stop := make(chan struct{})
	cleanupStop = stop
	go seedScan(cfg) // D10 LEAN-Y (Node typst init has no seed scan).
	go runTick(expiryInterval, stop, func() { expiryTickOnce(cm) })
	go runTick(diskStatsInterval, stop, func() { RunDiskStatsOnce(cfg) })
}

// Cleanup shuts down the Init intervals (test seam). Idempotent, safe when
// Init was never called.
func Cleanup() {
	if cleanupStop != nil {
		close(cleanupStop)
		cleanupStop = nil
	}
}

// expiryTickOnce is the 10-minute interval body: D10 refreshExpiry (LEAN-Y
// re-add) then Node's collectDiskStats + clearExpiredProjects(callback
// error log).
func expiryTickOnce(cm *compilemanager.Manager) {
	RefreshExpiry()
	if err := ClearExpiredProjectsOnce(cm); err != nil {
		clsl.Error(map[string]any{"err": err}, "clearing expired projects failed")
	}
}

// --- markProjectAsJustAccessed (Node) ---------------------------------------

// MarkProjectJustAccessed ports markProjectAsJustAccessed (Node:
// LAST_ACCESS.set(projectId, max(Date.now(), prev))).
func MarkProjectJustAccessed(projectID string) {
	if prev := lastprojectaccess.GetLastProjectAccessTime(projectID); prev < Now() {
		lastprojectaccess.SetLastProjectAccessTime(projectID, Now())
	}
}

// --- seed scan (D10 LEAN-Y re-add — clsi tex seed scan, NOT in Node typst) ----

// seedScan ports the clsi tex compiles-dir scan (Node typst init has none).
// Non-matching names (submissions etc) get now-expiry+jitter(5-10 min) last
// access; matching names (24-hex project id, optionally + user suffix) get
// the dir mtime (stat error -> 0 = schedule for immediate cleanup).
func seedScan(cfg *cltypstcfg.Config) {
	entries, err := os.ReadDir(cfg.Path.CompilesDir)
	if err != nil {
		clsl.Warn(map[string]any{"err": err}, "cannot get project listing")
		entries = nil
	}
	for _, e := range entries {
		name := e.Name()
		compileDir := filepath.Join(cfg.Path.CompilesDir, name)
		if !projectAndUserRe.MatchString(name) {
			// Node (verbatim): delay = (5 + 5 * Math.random()) * 60_000 (ms);
			// setLastAccessIfNewer(name, Date.now() - EXPIRY_TIMEOUT + delay).
			delayMS := int64((5 + 5*randFloat()) * 60_000)
			lastprojectaccess.SetIfNewer(name, Now()-getExpiryMS()+delayMS)
			continue
		}
		projectId := first24(name)
		fi, err := os.Stat(compileDir)
		if err != nil {
			lastprojectaccess.SetIfNewer(projectId, 0)
			continue
		}
		lastprojectaccess.SetIfNewer(projectId, fi.ModTime().UnixMilli())
	}
}

func first24(s string) string {
	if len(s) >= 24 {
		return s[:24]
	}
	return s
}

// --- disk stats (Node collectDiskStats) -------------------------------------

// diskStatResult is the per-path statfs result (Node: {total, available}).
type diskStatResult struct {
	Path    string
	Total   int64
	Avail   int64
	Percent float64
	Low     bool
}

// RunDiskStatsOnce ports collectDiskStats: stat each sandboxed dir;
// low = <10%, critical = <3%; per-path try/catch -> logger.error + skip (an
// erroring path does NOT clear the other paths' flags — Node try/catch
// semantics).
func RunDiskStatsOnce(cfg *cltypstcfg.Config) []diskStatResult {
	var results []diskStatResult
	var anyLow, anyCrit bool
	for _, path := range []string{
		cfg.Path.CompilesDir, cfg.Path.OutputDir, cfg.Path.ClsiCacheDir,
	} {
		total, avail, err := statFSSeams(path)
		if err != nil {
			clsl.Error(map[string]any{"err": err, "path": path}, "error getting disk usage")
			continue
		}
		pct := 0.0
		if total > 0 {
			pct = float64(avail) / float64(total) * 100
		}
		low, critical := pct < lowDiskPercent, pct < criticalDiskPercent
		anyLow = anyLow || low
		anyCrit = anyCrit || critical
		results = append(results, diskStatResult{
			Path: path, Total: total, Avail: avail, Percent: pct, Low: low,
		})
		gaugeForPath(path).Set(pct)
	}
	diskMu.Lock()
	anyDiskLow = anyLow
	anyDiskCrit = anyCrit
	diskMu.Unlock()
	return results
}

// --- refreshExpiryTimeout (D10 LEAN-Y re-add — clsi tex, NOT in Node typst) --

// RefreshExpiry ports clsi refreshExpiryTimeout: on the FIRST low path
// where projectCacheLengthMs/2 < expiry*0.9, lower EXPIRY_TIMEOUT to
// expiry*0.9 and break (Node float math, integer ms here).
func RefreshExpiry() {
	cfg, _ := getSingleton()
	if cfg == nil {
		return
	}
	results := RunDiskStatsOnce(cfg)
	lowerExpiry := getExpiryMS() * 90 / 100
	for _, s := range results {
		if s.Low && int64(cfg.ProjectCacheLengthMs/2) < lowerExpiry {
			clsl.Warn(map[string]any{
				"path":                   s.Path,
				"stats":                  map[string]any{"total": s.Total, "available": s.Avail},
				"newExpiryTimeoutInDays": float64(lowerExpiry) / float64(oneDayMs),
			}, "disk running low on space, modifying EXPIRY_TIMEOUT")
			setExpiryMS(lowerExpiry)
			break
		}
	}
}

// --- findExpiredProjectIDs (clsi parity, consumed by clearExpiredProjects) --

func findExpiredProjectIDs() []string {
	expiredFrom := Now() - getExpiryMS()
	return lastprojectaccess.EachExpired(expiredFrom)
}

// --- clearExpiredProjectsOnce (Node callback; clsi parity) -------------------

// ClearExpiredProjectsOnce ports PPM.clearExpiredProjects: find expired
// ids (lastprojectaccess.EachExpired), delete each from LAST_ACCESS and
// rm its compile dir (Node typst baseline loop is the same readdir/stat/age
// path clsi uses; clsi additionally clears urlcache/history LAST_ACCESS —
// D10 re-adds the LAST_ACCESS delete + per-id CompileManager.ClearProject,
// both additive and environment-correct), then
// CompileManager.clearExpiredProjects(EXPIRY_TIMEOUT) (rm dirs older than
// the timeout — Node typst baseline).
func ClearExpiredProjectsOnce(cm *compilemanager.Manager) error {
	expired := findExpiredProjectIDs()
	if len(expired) > 0 {
		clsl.Debug(map[string]any{"projectIds": expired}, "clearing expired projects")
	}
	for _, projectId := range expired {
		lastprojectaccess.Delete(projectId)
		if cmErr := cmClearProject(cm, projectId, ""); cmErr != nil {
			clsl.Error(map[string]any{"error": cmErr, "projectId": projectId},
				"error clearing project from cache")
		}
	}
	return cmClearExpiredProjects(cm, getExpiryMS())
}

// cmClearProject mirrors CompileManager.clearProject (seam for tests).
var cmClearProject = func(cm *compilemanager.Manager, projectId, userId string) error {
	return cm.ClearProject(projectId, userId)
}

// cmClearExpiredProjects mirrors CompileManager.clearExpiredProjects (seam
// for tests).
var cmClearExpiredProjects = func(cm *compilemanager.Manager, ms int64) error {
	return cm.ClearExpiredProjects(ms)
}

// --- clearProject (Node) -----------------------------------------------------

// ClearProject ports clearProject: LAST_ACCESS.delete(projectId +
// project-user) then rm compilesDir/project[-user] AND outputDir/project
// [-user] {force, recursive} over both uid variants (undefined + userId;
// fs rm force:true treats a missing dir as success).
func ClearProject(projectID, userID string) error {
	lastprojectaccess.Delete(projectID)
	if userID != "" {
		lastprojectaccess.Delete(projectID + "-" + userID)
	}
	cfg, _ := getSingleton()
	for _, uid := range []string{"", userID} {
		if userID == "" && uid != "" {
			continue
		}
		name := projectID
		if uid != "" {
			name = projectID + "-" + uid
		}
		if rerr := removeAll(filepath.Join(cfg.Path.CompilesDir, name)); rerr != nil {
			return rerr
		}
		if rerr := removeAll(filepath.Join(cfg.Path.OutputDir, name)); rerr != nil {
			return rerr
		}
	}
	return nil
}
