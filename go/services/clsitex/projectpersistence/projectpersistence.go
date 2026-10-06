// Package projectpersistence ports services/clsi/app/js/ProjectPersistenceManager.js.
//
// Node parity notes:
//
//   - EXPIRY_TIMEOUT is a mutable package var mirroring the Node module
//     constant (Settings.project_cache_length_ms || oneDay * 2.5). Init/ForTest
//     set it from config; refreshExpiryTimeout lowers it toward 90% on a low
//     disk (same float math as Node, computed in integers).
//   - The LAST_ACCESS map is NOT re-declared: it lives in lastprojectaccess
//     so the compilecontroller's markProjectAsJustAccessed and this package
//     share state, exactly as in Node (LAST_ACCESS owned by PPM but read by
//     DockerRunner via LastProjectAccess).
//   - init() = one-time compiles-dir seed scan (readdir; per-dir stat;
//     submission names get now-expiry+jitter last access via SetIfNewer,
//     project ids get the dir mtime or 0 on stat error) + a 10-minute
//     interval (refreshExpiryTimeout -> clearExpiredProjects) + a 15-second
//     disk-stats interval.
//   - clearProject chain: CompileManager.ClearProject ->
//     HistoryResourceWriter.ClearCache -> UrlCache.ClearProject +
//     LAST_ACCESS.delete. (Node: clearProjectFromCache +
//     _clearProjectFromDatabase.)
//   - Node emits prom gauges (disk_available_percent{path},
//     project_persistence_expiry_timeout); the Go port has no prom layer yet
//     (documented divergence: gauges omitted).
//   - statfs: Node fs.promises.statfs, Go syscall.Statfs (Linux; the port is
//     Linux-only).
//   - async.eachLimit(dirs, 10) scan runs serially here: each name is
//     independent and order-independent (SetIfNewer is max-ordered), so the
//     10-way bound changes no outcome (documented divergence).
package projectpersistence

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"syscall"
	"time"

	compilemanager "ollitex/go/services/clsitex/compilemanager"
	"ollitex/go/services/clsitex/config"
	"ollitex/go/services/clsitex/historyresourcewriter"
	"ollitex/go/services/clsitex/lastprojectaccess"
	"ollitex/go/services/clsitex/logger"
	"ollitex/go/services/clsitex/urlcache"
)

// Node-constant durations (mirrors app/js/ProjectPersistenceManager.js).
const (
	expiryInterval      = 10 * time.Minute
	diskStatsInterval   = 15 * time.Second
	lowDiskPercent      = 10
	criticalDiskPercent = 3
)

// oneDayMs mirrors Node's oneDay = 24 * 60 * 60 * 1000 (ms).
const oneDayMs = 24 * 60 * 60 * 1000

// projectAndUserRe mirrors /^[a-f0-9]{24}(-[a-f0-9]{24})?$/.
var projectAndUserRe = regexp.MustCompile(`^[a-f0-9]{24}(-[a-f0-9]{24})?$`)

// Now mirrors Date.now() for tests.
var Now = func() int64 { return time.Now().UnixMilli() }

// statFSSeams — tests inject a fake statfs (syscall.Statfs is not mockable).
var statFSSeams = defaultStatfs

// defaultStatfs is the production syscall.Statfs impl (tests replace
// statFSSeams with installFakeStatfs).
func defaultStatfs(path string) (total, available int64, err error) {
	var st syscall.Statfs_t
	if serr := syscall.Statfs(path, &st); serr != nil {
		return 0, 0, serr
	}
	return int64(st.Blocks) * int64(st.Bsize), int64(st.Bavail) * int64(st.Bsize), nil
}

// randFloat is the jitter seam (Node: Math.random()).
var randFloat = rand.Float64

// cmClearExpiredProjects mirrors the CompileManager.clearExpiredProjects
// call at the end of Node's clearExpiredProjects (seam for tests).
var cmClearExpiredProjects = func(cm *compilemanager.Manager, ms int64) error {
	return cm.ClearExpiredProjects(ms)
}

// urlcacheClearProject mirrors UrlCache.clearProject (Go: the urlcache
// package func; seam so tests avoid the config singleton).
var urlcacheClearProject = func(projectID string) error {
	return urlcache.ClearProject(projectID)
}

// historyClearCache mirrors HistoryResourceWriter.clearCache (the Go package
// func; seam so tests avoid snapshot-dir removal side effects).
var historyClearCache = func(projectID, userID, cacheKey string) {
	historyresourcewriter.ClearCache(projectID, userID, cacheKey)
}

// --- EXPIRY_TIMEOUT (Node module-level mutable) --------------------------

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

// --- disk state (Node ANY_DISK_LOW / ANY_DISK_CRITICAL_LOW) ---------------

var (
	diskMu      sync.Mutex
	anyDiskLow  bool
	anyDiskCrit bool
)

// IsAnyDiskLow ports isAnyDiskLow().
func IsAnyDiskLow() bool {
	diskMu.Lock()
	defer diskMu.Unlock()
	return anyDiskLow
}

// IsAnyDiskCriticalLow ports isAnyDiskCriticalLow().
func IsAnyDiskCriticalLow() bool {
	diskMu.Lock()
	defer diskMu.Unlock()
	return anyDiskCrit
}

func setDiskState(low, critical bool) {
	diskMu.Lock()
	anyDiskLow = low
	anyDiskCrit = critical
	diskMu.Unlock()
}

// --- singleton wiring (Node PPM.init() called once at app start) ----------

var (
	singletonMu sync.Mutex
	singletonCf *config.Config
	singletonCM *compilemanager.Manager

	cleanupStop chan struct{}
)

// runTick is the interval driver: runs fn on each tick until stop closes.
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

func setSingletons(cfg *config.Config, cm *compilemanager.Manager) {
	singletonMu.Lock()
	singletonCf = cfg
	singletonCM = cm
	singletonMu.Unlock()
}

func getSingleton() (cfg *config.Config, cm *compilemanager.Manager) {
	singletonMu.Lock()
	defer singletonMu.Unlock()
	return singletonCf, singletonCM
}

// Init ports PPM.init(): sets EXPIRY_TIMEOUT, seeds LAST_ACCESS (async compiles
// dir scan), and starts the two interval goroutines:
//
//	10 min: refreshExpiryTimeout -> clearExpiredProjects
//	15  s:  collectDiskStats
//
// cm must be non-nil (the expiry interval dereferences it, as in Node where
// CompileManager.clearExpiredProjects is always available after init).
func Init(cfg *config.Config, cm *compilemanager.Manager) {
	// Bind the singletons: production paths (ClearProject, expiry chain)
	// read them via getSingleton. ForTest performs the same bind without
	// the intervals; omitting it here left a nil Manager in production
	// (owner clear-cache panic/500, 2026-10-06).
	setSingletons(cfg, cm)
	SetExpiryFromConfig(cfg)
	stop := make(chan struct{})
	cleanupStop = stop
	go seedScan(cfg) // Node: fs.readdir + async.eachLimit — async
	go runTick(expiryInterval, stop, func() { expiryTickOnce(cm) })
	go runTick(diskStatsInterval, stop, func() { runDiskStatsOnce(cfg) })
}

// expiryTickOnce is the 10-minute interval body (Node's anonymous interval
// callback): refreshExpiryTimeout then clearExpiredProjects (errors logged).
func expiryTickOnce(cm *compilemanager.Manager) {
	refreshExpiryTimeoutOnce()
	if err := clearExpiredProjectsOnce(cm); err != nil {
		logger.Error(map[string]any{"err": err}, "clearing expired projects failed")
	}
}

// Cleanup shuts down the Init intervals (test seam). Idempotent, safe when
// Init was never called.
func Cleanup() {
	if cleanupStop != nil {
		close(cleanupStop)
		cleanupStop = nil
	}
}

// SetExpiryFromConfig mirrors Node's module-const derivation:
// Settings.project_cache_length_ms || oneDay * 2.5.
func SetExpiryFromConfig(cfg *config.Config) {
	if cfg.ProjectCacheLengthMs > 0 {
		setExpiryMS(int64(cfg.ProjectCacheLengthMs))
	} else {
		setExpiryMS((oneDayMs * 25) / 10)
	}
}

// ForTest sets singletons + expiry WITHOUT starting intervals (tests call
// the *Once functions directly).
func ForTest(cfg *config.Config, cm *compilemanager.Manager) {
	setSingletons(cfg, cm)
	SetExpiryFromConfig(cfg)
}

// MarkProjectJustAccessed ports markProjectAsJustAccessed (Node:
// LAST_ACCESS.set(pid, Date.now())).
func MarkProjectJustAccessed(projectID string) {
	lastprojectaccess.SetLastProjectAccessTime(projectID, Now())
}

// --- seed scan (Node init()'s fs.readdir + async.eachLimit block) ---------

// seedScan ports the one-time compiles-dir scan Node runs inside init:
// non-matching names (submissions etc) get now-expiry+jitter(5-10 min) last
// access; matching names (24-hex project id, optionally + user suffix) get
// the dir mtime (stat error -> 0 = cleanup immediately).
func seedScan(cfg *config.Config) {
	entries, err := os.ReadDir(cfg.Path.CompilesDir)
	if err != nil {
		logger.Warn(map[string]any{"err": err}, "cannot get project listing")
		entries = nil
	}
	for _, e := range entries {
		name := e.Name()
		compileDir := filepath.Join(cfg.Path.CompilesDir, name)
		if !projectAndUserRe.MatchString(name) {
			// Node (verbatim): delay = (5 + 5 * Math.random()) * 60_000 (ms);
			// setLastAccessIfNewer(name, Date.now() - EXPIRY_TIMEOUT + delay).
			// i.e. the entry is EXPIRY in the past, pulled toward now by the
			// 5-10 min jitter (so it expires just after a cold start).
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

// --- disk stats (Node collectDiskStats) -----------------------------------

// diskStatResult is the per-path statfs result (Node: {total, available}).
type diskStatResult struct {
	Path    string
	Total   int64
	Avail   int64
	Percent float64
	Low     bool
}

// runDiskStatsOnce ports collectDiskStats: stat each sandboxed dir; low =
// <10%, critical = <3%; per-path try/catch -> logger.err + skip.
func runDiskStatsOnce(cfg *config.Config) []diskStatResult {
	var results []diskStatResult
	var anyLow, anyCrit bool
	for _, path := range []string{
		cfg.Path.CompilesDir, cfg.Path.OutputDir, cfg.Path.ClsiCacheDir,
	} {
		total, avail, err := statFSSeams(path)
		if err != nil {
			logger.Error(map[string]any{"err": err, "path": path}, "error getting disk usage")
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
		_ = critical
	}
	diskMu.Lock()
	anyDiskLow = anyLow
	anyDiskCrit = anyCrit
	diskMu.Unlock()
	return results
}

// --- refreshExpiryTimeout (Node) ------------------------------------------

// refreshExpiryTimeoutOnce ports refreshExpiryTimeout: collect disk stats;
// on the FIRST low path where projectCacheLength/2 < expiry*0.9, lower
// EXPIRY_TIMEOUT to expiry*0.9 and break (Node float math, integer ms here).
func refreshExpiryTimeoutOnce() {
	cfg, _ := getSingleton()
	results := runDiskStatsOnce(cfg)
	lowerExpiry := getExpiryMS() * 90 / 100
	for _, s := range results {
		if s.Low && int64(cfg.ProjectCacheLengthMs/2) < lowerExpiry {
			logger.Warn(map[string]any{
				"path":                   s.Path,
				"stats":                  map[string]any{"total": s.Total, "available": s.Avail},
				"newExpiryTimeoutInDays": float64(lowerExpiry) / float64(oneDayMs),
			}, "disk running low on space, modifying EXPIRY_TIMEOUT")
			setExpiryMS(lowerExpiry)
			break
		}
	}
}

// --- findExpiredProjectIds (Node private) ----------------------------------

func findExpiredProjectIDs() []string {
	expiredFrom := Now() - getExpiryMS()
	return lastprojectaccess.EachExpired(expiredFrom)
}

// --- clearExpiredProjects (Node callback) ----------------------------------

// clearExpiredProjectsOnce ports clearExpiredProjects: find expired ids,
// clear each from urlcache + LAST_ACCESS via clearProjectFromCache, then
// CompileManager.clearExpiredProjects(EXPIRY_TIMEOUT) (rm on-dirs older than
// the timeout). Node runs the per-id jobs serially (async.series); Go does
// the same in a plain loop.
func clearExpiredProjectsOnce(cm *compilemanager.Manager) error {
	expired := findExpiredProjectIDs()
	if len(expired) > 0 {
		logger.Debug(map[string]any{"projectIds": expired}, "clearing expired projects")
	}
	for _, projectId := range expired {
		clearProjectFromCacheOnce(projectId)
	}
	return cmClearExpiredProjects(cm, getExpiryMS())
}

// --- clearProjectFromCache (Node private) ----------------------------------

func clearProjectFromCacheOnce(projectId string) error {
	if err := urlcacheClearProject(projectId); err != nil {
		logger.Error(map[string]any{"error": err, "projectId": projectId},
			"error clearing project from cache")
		return err
	}
	lastprojectaccess.Delete(projectId)
	return nil
}

// --- clearProject (Node callback) ------------------------------------------

// ClearProject ports clearProject: CompileManager.clearProject(pid, uid) ->
// HistoryResourceWriter.clearCache(pid, uid, cacheKey) ->
// clearProjectFromCache. cacheKey mirrors Node: userId ? pid-uid : pid.
func ClearProject(projectId, userId string) error {
	_, cm := getSingleton()
	logger.Debug(map[string]any{"projectId": projectId, "userId": userId},
		"clearing project for user")
	if cm == nil {
		// defensive: singleton unbound (apps bootstrap skipped
		// projectpersistence.Init). A clear error keeps the HTTP seam honest
		// instead of a nil-Manager panic (which used to drop the connection)
		// — owner live event 2026-10-06.
		return fmt.Errorf("projectpersistence: clear manager not bound")
	}
	if err := cmClearProject(cm, projectId, userId); err != nil {
		return err
	}
	cacheKey := projectId
	if userId != "" {
		cacheKey = projectId + "-" + userId
	}
	historyClearCache(projectId, userId, cacheKey)
	return clearProjectFromCacheOnce(projectId)
}

// cmClearProject mirrors CompileManager.clearProject (seam for tests).
var cmClearProject = func(cm *compilemanager.Manager, projectId, userId string) error {
	return cm.ClearProject(projectId, userId)
}
