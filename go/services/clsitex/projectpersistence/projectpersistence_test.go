package projectpersistence

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	compilemanager "ollitex/go/services/clsitex/compilemanager"
	"ollitex/go/services/clsitex/config"
	"ollitex/go/services/clsitex/lastprojectaccess"
)

// --- call counters (seams record here; reset per test) ---------------------

var (
	testCmClearProjectCalls int
	testCmClearExpiredCalls int
	testCmClearExpiredMS    int64
	testUrlcacheClearCalls  []string
	testHistoryClearCalls   []string
)

func resetCounters() {
	testCmClearProjectCalls = 0
	testCmClearExpiredCalls = 0
	testCmClearExpiredMS = 0
	testUrlcacheClearCalls = nil
	testHistoryClearCalls = nil
}

func resetSingletonsForTest(t *testing.T, cfg *config.Config, cm *compilemanager.Manager) {
	t.Helper()
	ForTest(cfg, cm)
	resetCounters()
	preCMProj := cmClearProject
	preCMExp := cmClearExpiredProjects
	preUC := urlcacheClearProject
	preHC := historyClearCache
	cmClearProject = func(cm *compilemanager.Manager, projectId, userId string) error {
		testCmClearProjectCalls++
		lastprojectaccess.Delete(projectId)
		os.RemoveAll(filepath.Join(cfg.Path.CompilesDir, projectId))
		return nil
	}
	cmClearExpiredProjects = func(cm *compilemanager.Manager, ms int64) error {
		testCmClearExpiredCalls++
		testCmClearExpiredMS = ms
		return nil
	}
	urlcacheClearProject = func(projectID string) error {
		testUrlcacheClearCalls = append(testUrlcacheClearCalls, projectID)
		lastprojectaccess.Delete(projectID)
		return nil
	}
	historyClearCache = func(projectId, userId, cacheKey string) {
		testHistoryClearCalls = append(testHistoryClearCalls, cacheKey)
	}
	t.Cleanup(func() {
		cmClearProject = preCMProj
		cmClearExpiredProjects = preCMExp
		urlcacheClearProject = preUC
		historyClearCache = preHC
		lastprojectaccess.LastAccess = map[string]int64{}
	})
}

// resetSeamsDefault restores the default (real) seams while keeping the
// harness config (used inside a resetSingletonsForTest test after it has
// saved its own overrides).
func resetSeamsDefault() {
	ForTest(&config.Config{}, nil)
}

func testConfig(tmp string) *config.Config {
	cfg := &config.Config{ProjectCacheLengthMs: 1000 * 60 * 60 * 24}
	cfg.Path.CompilesDir = filepath.Join(tmp, "compiles")
	cfg.Path.OutputDir = filepath.Join(tmp, "output")
	cfg.Path.ClsiCacheDir = filepath.Join(tmp, "cache")
	os.MkdirAll(cfg.Path.CompilesDir, 0o755)
	os.MkdirAll(cfg.Path.OutputDir, 0o755)
	os.MkdirAll(cfg.Path.ClsiCacheDir, 0o755)
	return cfg
}

func newTestMinCM(compilesDir string, now int64) *compilemanager.Manager {
	return &compilemanager.Manager{
		Paths: compilemanager.Paths{CompilesDir: compilesDir},
		Now:   func() int64 { return now },
	}
}

type fakeStatfsMap map[string]struct {
	total, avail int64
	err          error
}

func installFakeStatfs(t *testing.T, m fakeStatfsMap) {
	t.Helper()
	prev := statFSSeams
	statFSSeams = func(path string) (int64, int64, error) {
		if e, ok := m[path]; ok {
			return e.total, e.avail, e.err
		}
		return 0, 0, os.ErrNotExist
	}
	t.Cleanup(func() { statFSSeams = prev })
}

func resetExpiry(t *testing.T, ms int64) {
	setExpiryMS(ms)
	t.Cleanup(func() { setExpiryMS((oneDayMs * 25) / 10) })
}

func resetDisk(t *testing.T) {
	setDiskState(false, false)
	t.Cleanup(func() { setDiskState(false, false) })
}

// --- seed scan --------------------------------------------------------------

func TestSeedScanMixedNames(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	cm := newTestMinCM(cfg.Path.CompilesDir, time.Now().UnixMilli())
	resetSingletonsForTest(t, cfg, cm)
	resetExpiry(t, oneDayMs*7)
	lastprojectaccess.LastAccess = map[string]int64{}

	projID := strings.Repeat("a", 24)
	projDir := filepath.Join(cfg.Path.CompilesDir, projID)
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	submissionName := "sub-abc123"
	if err := os.MkdirAll(filepath.Join(cfg.Path.CompilesDir, submissionName), 0o755); err != nil {
		t.Fatal(err)
	}
	combined := strings.Repeat("b", 24) + "-" + strings.Repeat("1", 24)
	_ = os.Mkdir(filepath.Join(cfg.Path.CompilesDir, combined), 0o755)

	seedScan(cfg)

	// Node: setLastAccessIfNewer(name, Date.now() - EXPIRY_TIMEOUT + delay),
	// delay = (5 + 5*rnd) minutes. So ts lives in [now-expiry, now-expiry+10m).
	subTS := lastprojectaccess.GetLastProjectAccessTime(submissionName)
	if subTS <= Now()-getExpiryMS() {
		t.Fatalf("submission ts %d must be >= now-expiry (jitter pulls toward now)", subTS)
	}
	if subTS > Now()-getExpiryMS()+10*60_000 {
		t.Fatalf("submission ts %d: jitter must be 5-10 min above now-expiry", subTS)
	}
	if ts := lastprojectaccess.GetLastProjectAccessTime(projID); ts == 0 {
		t.Fatal("project ts should be the dir mtime (non-zero)")
	}
	if ts := lastprojectaccess.GetLastProjectAccessTime(first24(combined)); ts == 0 {
		t.Fatal("combined name: projectId ts should be set")
	}
}

func TestSeedScanMissingProjectDir(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	cm := newTestMinCM(cfg.Path.CompilesDir, time.Now().UnixMilli())
	resetSingletonsForTest(t, cfg, cm)
	resetExpiry(t, oneDayMs*7)
	lastprojectaccess.LastAccess = map[string]int64{}

	projID := strings.Repeat("d", 24)
	seedScan(cfg)
	// name not present on disk -> os.Stat error -> SetIfNewer(pid, 0).
	// SetIfNewer over an absent key: 0 is not > 0, so the map stays empty =>
	// GetLastProjectAccessTime returns 0 (not "set" either).
	if ts := lastprojectaccess.GetLastProjectAccessTime(projID); ts != 0 {
		t.Fatalf("missing dir: ts should be 0, got %d", ts)
	}
}

func TestSeedScanReadDirFailure(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	cm := newTestMinCM(cfg.Path.CompilesDir, time.Now().UnixMilli())
	resetSingletonsForTest(t, cfg, cm)
	resetExpiry(t, oneDayMs*7)
	lastprojectaccess.LastAccess = map[string]int64{}
	// point the scan at a missing dir: use a cfg with an absent compiles dir
	badCfg := &config.Config{}
	badCfg.Path.CompilesDir = filepath.Join(tmp, "does-not-exist")
	seedScan(badCfg) // must not panic; warns + empty scan
}

// --- disk stats ---------------------------------------------------------------

func TestRunDiskStatsOnceLowOnly(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	cm := newTestMinCM(cfg.Path.CompilesDir, time.Now().UnixMilli())
	resetSingletonsForTest(t, cfg, cm)
	resetDisk(t)
	installFakeStatfs(t, fakeStatfsMap{
		cfg.Path.CompilesDir:  {total: 1000, avail: 500},
		cfg.Path.OutputDir:    {total: 1000, avail: 500},
		cfg.Path.ClsiCacheDir: {total: 1000, avail: 50}, // 5% -> low, not critical
	})
	res := runDiskStatsOnce(cfg)
	if len(res) != 3 {
		t.Fatalf("expected 3 results, got %d", len(res))
	}
	if !IsAnyDiskLow() || IsAnyDiskCriticalLow() {
		t.Fatalf("low=%v crit=%v (want low=true crit=false)",
			IsAnyDiskLow(), IsAnyDiskCriticalLow())
	}
}

func TestRunDiskStatsOnceCritical(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	cm := newTestMinCM(cfg.Path.CompilesDir, time.Now().UnixMilli())
	resetSingletonsForTest(t, cfg, cm)
	resetDisk(t)
	installFakeStatfs(t, fakeStatfsMap{
		cfg.Path.CompilesDir:  {total: 1000, avail: 900},
		cfg.Path.OutputDir:    {total: 1000, avail: 0}, // critical
		cfg.Path.ClsiCacheDir: {total: 1000, avail: 900},
	})
	runDiskStatsOnce(cfg)
	if !IsAnyDiskLow() || !IsAnyDiskCriticalLow() {
		t.Fatalf("low=%v crit=%v (want both true)",
			IsAnyDiskLow(), IsAnyDiskCriticalLow())
	}
}

func TestRunDiskStatsOnceHealthy(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	cm := newTestMinCM(cfg.Path.CompilesDir, time.Now().UnixMilli())
	resetSingletonsForTest(t, cfg, cm)
	resetDisk(t)
	installFakeStatfs(t, fakeStatfsMap{
		cfg.Path.CompilesDir:  {total: 1000, avail: 950},
		cfg.Path.OutputDir:    {total: 1000, avail: 950},
		cfg.Path.ClsiCacheDir: {total: 1000, avail: 950},
	})
	runDiskStatsOnce(cfg)
	if IsAnyDiskLow() || IsAnyDiskCriticalLow() {
		t.Fatalf("healthy: low=%v crit=%v", IsAnyDiskLow(), IsAnyDiskCriticalLow())
	}
}

func TestRunDiskStatsOnceErrSkipped(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	cm := newTestMinCM(cfg.Path.CompilesDir, time.Now().UnixMilli())
	resetSingletonsForTest(t, cfg, cm)
	resetDisk(t)
	installFakeStatfs(t, fakeStatfsMap{
		cfg.Path.CompilesDir:  {err: os.ErrNotExist},
		cfg.Path.OutputDir:    {total: 1000, avail: 900},
		cfg.Path.ClsiCacheDir: {err: os.ErrNotExist},
	})
	res := runDiskStatsOnce(cfg)
	if len(res) != 1 {
		t.Fatalf("expected 1 result (2 errors skipped), got %d", len(res))
	}
	if IsAnyDiskLow() || IsAnyDiskCriticalLow() {
		t.Fatalf("healthy: low=%v crit=%v", IsAnyDiskLow(), IsAnyDiskCriticalLow())
	}
}

func TestRunDiskStatsOnceZeroTotal(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	cm := newTestMinCM(cfg.Path.CompilesDir, time.Now().UnixMilli())
	resetSingletonsForTest(t, cfg, cm)
	resetDisk(t)
	installFakeStatfs(t, fakeStatfsMap{
		cfg.Path.CompilesDir:  {total: 0, avail: 0},
		cfg.Path.OutputDir:    {total: 0, avail: 0},
		cfg.Path.ClsiCacheDir: {total: 0, avail: 0},
	})
	res := runDiskStatsOnce(cfg)
	// total==0 -> pct stays 0 -> low AND critical
	if !res[0].Low || !IsAnyDiskCriticalLow() {
		t.Fatalf("zero total: should be low+critical: %+v", res)
	}
}

// --- refresh expiry ---------------------------------------------------------------

func TestRefreshExpiryLowersWhenLow(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	cfg.ProjectCacheLengthMs = 10
	cm := newTestMinCM(cfg.Path.CompilesDir, time.Now().UnixMilli())
	resetSingletonsForTest(t, cfg, cm)
	resetDisk(t)
	orig := int64(10_000)
	resetExpiry(t, orig)
	installFakeStatfs(t, fakeStatfsMap{
		cfg.Path.CompilesDir:  {total: 1000, avail: 50},
		cfg.Path.OutputDir:    {total: 1000, avail: 50},
		cfg.Path.ClsiCacheDir: {total: 1000, avail: 50},
	})
	refreshExpiryTimeoutOnce()
	if got := getExpiryMS(); got != orig*90/100 {
		t.Fatalf("expiry lowered to %d, want %d", got, orig*90/100)
	}
}

func TestRefreshExpiryNoChangeWhenHealthy(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	cm := newTestMinCM(cfg.Path.CompilesDir, time.Now().UnixMilli())
	resetSingletonsForTest(t, cfg, cm)
	resetDisk(t)
	orig := int64(10_000)
	resetExpiry(t, orig)
	installFakeStatfs(t, fakeStatfsMap{
		cfg.Path.CompilesDir:  {total: 1000, avail: 900},
		cfg.Path.OutputDir:    {total: 1000, avail: 900},
		cfg.Path.ClsiCacheDir: {total: 1000, avail: 900},
	})
	refreshExpiryTimeoutOnce()
	if got := getExpiryMS(); got != orig {
		t.Fatalf("expiry should be unchanged, got %d", got)
	}
}

func TestRefreshExpiryLowButNotLowerable(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	// projectCacheLength/2 (12h) must be >= expiry*0.9 (9s) -> no lower
	cm := newTestMinCM(cfg.Path.CompilesDir, time.Now().UnixMilli())
	resetSingletonsForTest(t, cfg, cm)
	resetDisk(t)
	orig := int64(9000)
	resetExpiry(t, orig)
	installFakeStatfs(t, fakeStatfsMap{
		cfg.Path.CompilesDir:  {total: 1000, avail: 50},
		cfg.Path.OutputDir:    {total: 1000, avail: 50},
		cfg.Path.ClsiCacheDir: {total: 1000, avail: 50},
	})
	refreshExpiryTimeoutOnce()
	if got := getExpiryMS(); got != orig {
		t.Fatalf("expiry should not be lowered (projectCache/2 >= lowerExpiry), got %d", got)
	}
}

// --- mark / find / clear expired --------------------------------------------------

func TestMarkProjectJustAccessed(t *testing.T) {
	resetSingletonsForTest(t, &config.Config{}, nil)
	now := Now()
	MarkProjectJustAccessed("p1")
	if got := lastprojectaccess.GetLastProjectAccessTime("p1"); got != now {
		t.Fatalf("expected ts=%d, got %d", now, got)
	}
}

func TestFindExpiredProjectIDs(t *testing.T) {
	resetSingletonsForTest(t, &config.Config{}, nil)
	resetExpiry(t, 1000)
	lastprojectaccess.LastAccess = map[string]int64{
		"a": 1,
		"b": Now(),
	}
	exp := findExpiredProjectIDs()
	found := map[string]bool{}
	for _, e := range exp {
		found[e] = true
	}
	if !found["a"] || found["b"] {
		t.Fatalf("expired = %v (want a only)", exp)
	}
}

func TestClearExpiredProjectsOnce(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	cm := newTestMinCM(cfg.Path.CompilesDir, time.Now().UnixMilli())
	resetSingletonsForTest(t, cfg, cm)
	resetExpiry(t, 1000)
	lastprojectaccess.LastAccess = map[string]int64{
		"aa": 0,
		"bb": Now(),
	}
	if err := clearExpiredProjectsOnce(cm); err != nil {
		t.Fatalf("clear error: %v", err)
	}
	if testCmClearExpiredCalls != 1 {
		t.Fatalf("cm.clearExpiredProjects called %d, want 1", testCmClearExpiredCalls)
	}
	if testCmClearExpiredMS != 1000 {
		t.Fatalf("cm.clearExpiredProjects got expiry %d, want 1000", testCmClearExpiredMS)
	}
	// aa deleted, bb kept
	if _, ok := lastprojectaccess.LastAccess["aa"]; ok {
		t.Fatal("aa should be deleted from LAST_ACCESS")
	}
	if _, ok := lastprojectaccess.LastAccess["bb"]; !ok {
		t.Fatal("bb should remain in LAST_ACCESS")
	}
	if len(testUrlcacheClearCalls) != 1 || testUrlcacheClearCalls[0] != "aa" {
		t.Fatalf("urlcache cleared %v, want [aa]", testUrlcacheClearCalls)
	}
}

func TestClearExpiredProjectsOnceCmError(t *testing.T) {
	resetSingletonsForTest(t, &config.Config{}, nil)
	lastprojectaccess.LastAccess = map[string]int64{}
	pre := cmClearExpiredProjects
	cmClearExpiredProjects = func(cm *compilemanager.Manager, ms int64) error {
		return errors.New("boom")
	}
	t.Cleanup(func() { cmClearExpiredProjects = pre })
	if err := clearExpiredProjectsOnce(nil); err == nil {
		t.Fatal("expected cm error to propagate")
	}
}

// --- clearProject chain -------------------------------------------------------------

func TestClearProjectChainNoUser(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	cm := newTestMinCM(cfg.Path.CompilesDir, time.Now().UnixMilli())
	resetSingletonsForTest(t, cfg, cm)
	lastprojectaccess.LastAccess = map[string]int64{"p1": Now()}

	if err := ClearProject("p1", ""); err != nil {
		t.Fatalf("ClearProject error: %v", err)
	}
	if testCmClearProjectCalls != 1 {
		t.Fatalf("cm.clearProject called %d, want 1", testCmClearProjectCalls)
	}
	if len(testHistoryClearCalls) != 1 || testHistoryClearCalls[0] != "p1" {
		t.Fatalf("history cacheKey = %v, want [p1]", testHistoryClearCalls)
	}
	if len(testUrlcacheClearCalls) != 1 || testUrlcacheClearCalls[0] != "p1" {
		t.Fatalf("urlcache cleared %v, want [p1]", testUrlcacheClearCalls)
	}
	if _, ok := lastprojectaccess.LastAccess["p1"]; ok {
		t.Fatal("p1 should be deleted from LAST_ACCESS")
	}
}

func TestClearProjectChainUser(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	cm := newTestMinCM(cfg.Path.CompilesDir, time.Now().UnixMilli())
	resetSingletonsForTest(t, cfg, cm)
	lastprojectaccess.LastAccess = map[string]int64{"p1": Now()}

	if err := ClearProject("p1", "u1"); err != nil {
		t.Fatalf("ClearProject error: %v", err)
	}
	// Node: cacheKey = `${projectId}-${userId}`
	if len(testHistoryClearCalls) != 1 || testHistoryClearCalls[0] != "p1-u1" {
		t.Fatalf("history cacheKey = %v, want [p1-u1]", testHistoryClearCalls)
	}
}

func TestClearProjectCmError(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	cm := newTestMinCM(cfg.Path.CompilesDir, time.Now().UnixMilli())
	resetSingletonsForTest(t, cfg, cm)
	lastprojectaccess.LastAccess = map[string]int64{"p1": Now()}
	pre := cmClearProject
	cmClearProject = func(cm *compilemanager.Manager, projectId, userId string) error {
		return errors.New("cm boom")
	}
	t.Cleanup(func() { cmClearProject = pre })
	if err := ClearProject("p1", ""); err == nil {
		t.Fatal("expected error from cm.ClearProject to propagate")
	}
	// urlcache/history should NOT be touched when cm fails
	if len(testUrlcacheClearCalls) != 0 || len(testHistoryClearCalls) != 0 {
		t.Fatalf("downstream should not run on cm error: uc=%v hc=%v",
			testUrlcacheClearCalls, testHistoryClearCalls)
	}
}

func TestClearProjectFromCacheOnce(t *testing.T) {
	resetSingletonsForTest(t, &config.Config{}, nil)
	lastprojectaccess.LastAccess = map[string]int64{"zz": 5}
	if err := clearProjectFromCacheOnce("zz"); err != nil {
		t.Fatalf("clear error: %v", err)
	}
	if _, ok := lastprojectaccess.LastAccess["zz"]; ok {
		t.Fatal("zz should be deleted from LAST_ACCESS")
	}
}

func TestClearProjectFromCacheOnceError(t *testing.T) {
	resetSingletonsForTest(t, &config.Config{}, nil)
	pre := urlcacheClearProject
	urlcacheClearProject = func(projectID string) error {
		return errors.New("uc boom")
	}
	t.Cleanup(func() { urlcacheClearProject = pre })
	lastprojectaccess.LastAccess = map[string]int64{"yy": 5}
	if err := clearProjectFromCacheOnce("yy"); err == nil {
		t.Fatal("expected error to propagate")
	}
	// Node: lastprojectaccess.Delete runs only after urlcache success
	if _, ok := lastprojectaccess.LastAccess["yy"]; !ok {
		t.Fatal("yy should remain in LAST_ACCESS on urlcache error")
	}
}

// --- Init / lifecycle ----------------------------------------------------------------

func TestInitSetsExpiryAndRunsSeams(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	cm := newTestMinCM(cfg.Path.CompilesDir, time.Now().UnixMilli())
	preCMExp := cmClearExpiredProjects
	preUC := urlcacheClearProject
	preHC := historyClearCache
	preCMProj := cmClearProject
	cmClearProject = func(cm2 *compilemanager.Manager, projectId, userId string) error { return nil }
	cmClearExpiredProjects = func(cm2 *compilemanager.Manager, ms int64) error { return nil }
	urlcacheClearProject = func(projectID string) error { return nil }
	historyClearCache = func(projectId, userId, cacheKey string) {}
	installFakeStatfs(t, fakeStatfsMap{})
	Init(cfg, cm)
	if got := getExpiryMS(); got != int64(cfg.ProjectCacheLengthMs) {
		t.Fatalf("Init should set EXPIRY_TIMEOUT from config, got %d", got)
	}
	Cleanup()
	cmClearProject = preCMProj
	cmClearExpiredProjects = preCMExp
	urlcacheClearProject = preUC
	historyClearCache = preHC
}

func TestForTestExpiryFallback(t *testing.T) {
	resetSingletonsForTest(t, &config.Config{}, nil)
	// ProjectCacheLengthMs==0 -> fallback 2.5 days
	cfg := &config.Config{}
	ForTest(cfg, nil)
	if got := getExpiryMS(); got != (oneDayMs*25)/10 {
		t.Fatalf("fallback expiry = %d, want %d", got, (oneDayMs*25)/10)
	}
}

func TestSetupExpiryFromConfigExplicit(t *testing.T) {
	cfg := &config.Config{ProjectCacheLengthMs: 42}
	SetExpiryFromConfig(cfg)
	if got := getExpiryMS(); got != 42 {
		t.Fatalf("SetExpiryFromConfig = %d", got)
	}
	resetExpiry(t, (oneDayMs*25)/10)
}

func TestExpiryMSGetter(t *testing.T) {
	resetExpiry(t, 42)
	if got := ExpiryMS(); got != 42 {
		t.Fatalf("ExpiryMS = %d", got)
	}
}

func TestFirst24(t *testing.T) {
	if got := first24("short"); got != "short" {
		t.Fatalf("first24 short = %q", got)
	}
	if got := first24(strings.Repeat("e", 30)); len(got) != 24 {
		t.Fatalf("first24 len = %d", len(got))
	}
}

func TestExpiryTickOnce(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	cm := newTestMinCM(cfg.Path.CompilesDir, time.Now().UnixMilli())
	resetSingletonsForTest(t, cfg, cm)
	installFakeStatfs(t, fakeStatfsMap{
		cfg.Path.CompilesDir:  {total: 1000, avail: 900},
		cfg.Path.OutputDir:    {total: 1000, avail: 900},
		cfg.Path.ClsiCacheDir: {total: 1000, avail: 900},
	})
	resetExpiry(t, 100)
	lastprojectaccess.LastAccess = map[string]int64{"stale": 0}
	// expiryTickOnce: refreshExpiry (healthy) + clearExpiredProjectsOnce(cm)
	expiryTickOnce(cm)
	if testCmClearExpiredCalls != 1 {
		t.Fatalf("cm.clearExpiredProjects should run once, got %d", testCmClearExpiredCalls)
	}
}

func TestRunTickStops(t *testing.T) {
	stop := make(chan struct{})
	close(stop) // closed before tick -> returns immediately
	var ran int
	runTick(10*time.Millisecond, stop, func() { ran++ })
	// give a short grace; a closed channel means the goroutine must not
	// keep ticking, and returns on select.
	time.Sleep(20 * time.Millisecond)
	if ran > 0 {
		t.Fatalf("tick should not run after stop closed, ran=%d", ran)
	}
}

func TestRunTickFires(t *testing.T) {
	stop := make(chan struct{})
	var ran int
	go runTick(5*time.Millisecond, stop, func() { ran++ })
	time.Sleep(30 * time.Millisecond)
	close(stop)
	time.Sleep(10 * time.Millisecond)
	if ran == 0 {
		t.Fatal("tick should fire before stop")
	}
}

func TestDiskStateAccessors(t *testing.T) {
	resetDisk(t)
	if IsAnyDiskCriticalLow() || IsAnyDiskLow() {
		t.Fatal("expected both false initially")
	}
	setDiskState(true, false)
	if !IsAnyDiskLow() || IsAnyDiskCriticalLow() {
		t.Fatalf("after set: low=%v crit=%v", IsAnyDiskLow(), IsAnyDiskCriticalLow())
	}
	setDiskState(false, true)
	if IsAnyDiskLow() || !IsAnyDiskCriticalLow() {
		t.Fatalf("after set: low=%v crit=%v", IsAnyDiskLow(), IsAnyDiskCriticalLow())
	}
}

// --- default statfs seam (production syscall.Statfs path) -------------------------

// TestStatFSSeamsDefaultOnTempDir exercises defaultStatfs (syscall.Statfs on a
// real temp dir) — the production path, which other tests replace via
// installFakeStatfs. Asserts a non-zero total/avail for an existing dir.
func TestStatFSSeamsDefaultOnTempDir(t *testing.T) {
	tmp := t.TempDir()
	total, avail, err := defaultStatfs(tmp)
	if err != nil {
		t.Fatalf("defaultStatfs(%q): %v", tmp, err)
	}
	if total <= 0 || avail <= 0 {
		t.Fatalf("defaultStatfs(%q): total=%d avail=%d (want >0)", tmp, total, avail)
	}

	sbTot, sbAvail, sbErr := defaultStatfs(filepath.Join(tmp, "does-not-exist"))
	if sbTot != 0 || sbAvail != 0 || sbErr == nil {
		t.Fatalf("defaultStatfs on missing dir: want err, got total=%d avail=%d", sbTot, sbAvail)
	}
}
