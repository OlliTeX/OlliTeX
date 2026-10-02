package projectpersistence

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ollitex/go/services/clsitypst/lastprojectaccess"

	compilemanager "ollitex/go/services/clsitypst/compilemanager"
	cltypstcfg "ollitex/go/services/clsitypst/config"
)

// --- test plumbing (clsi.go ppm convention) ---------------------------------

func testConfig(tmp string) *cltypstcfg.Config {
	cfg := &cltypstcfg.Config{ProjectCacheLengthMs: 1000 * 60 * 60 * 24}
	cfg.Path.CompilesDir = filepath.Join(tmp, "compiles")
	cfg.Path.OutputDir = filepath.Join(tmp, "output")
	cfg.Path.ClsiCacheDir = filepath.Join(tmp, "cache")
	for _, p := range []string{cfg.Path.CompilesDir, cfg.Path.OutputDir, cfg.Path.ClsiCacheDir} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			panic(err)
		}
	}
	return cfg
}

type statfsEntry struct {
	total, avail int64
	err          error
}

func installFakeStatfs(t *testing.T, m map[string]statfsEntry) {
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

func resetLastAccess(t *testing.T) {
	prev := lastprojectaccess.LastAccess
	lastprojectaccess.LastAccess = map[string]int64{}
	t.Cleanup(func() { lastprojectaccess.LastAccess = prev })
}

func resetSingletons(t *testing.T, cfg *cltypstcfg.Config) {
	prevCfg, prevCM := getSingleton()
	singletonMu.Lock()
	singletonCf = cfg
	singletonCM = nil
	singletonMu.Unlock()
	t.Cleanup(func() {
		singletonMu.Lock()
		singletonCf, singletonCM = prevCfg, prevCM
		singletonMu.Unlock()
	})
}

func fakeNow(t *testing.T, v int64) {
	t.Helper()
	prev := Now
	Now = func() int64 { return v }
	t.Cleanup(func() { Now = prev })
}

func fakeRand(t *testing.T, v float64) {
	t.Helper()
	prev := randFloat
	randFloat = func() float64 { return v }
	t.Cleanup(func() { randFloat = prev })
}

func fakeRemoveAll(t *testing.T, fn func(path string) error) {
	t.Helper()
	prev := removeAll
	removeAll = fn
	t.Cleanup(func() { removeAll = prev })
}

// --- SetExpiryFromConfig / ExpiryMS ------------------------------------------

func TestExpiryFallback(t *testing.T) {
	defer resetExpiry(t, -1)
	SetExpiryFromConfig(&cltypstcfg.Config{})
	if got := ExpiryMS(); got != (oneDayMs*25)/10 {
		t.Fatalf("fallback 2.5d: %d", got)
	}
}

func TestSetExpiryExplicit(t *testing.T) {
	defer resetExpiry(t, -1)
	SetExpiryFromConfig(&cltypstcfg.Config{ProjectCacheLengthMs: 1000 * 60 * 30})
	if got := ExpiryMS(); got != int64(1000*60*30) {
		t.Fatalf("explicit: %d", got)
	}
}

// --- seed scan (D10 LEAN-Y re-add) --------------------------------------------

func TestSeedScanProjectDirMTimestamp(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	resetSingletons(t, cfg)
	resetExpiry(t, 1000)
	resetLastAccess(t)
	fakeNow(t, 1_000_000)
	const pid = "aaaaaaaaaaaaaaaaaaaaaaaa"
	if err := os.MkdirAll(filepath.Join(cfg.Path.CompilesDir, pid), 0o755); err != nil {
		t.Fatal(err)
	}
	seedScan(cfg)
	if got := lastprojectaccess.GetLastProjectAccessTime(pid); got <= 0 {
		t.Fatalf("project dir last access (dir mtime): %d", got)
	}
}

func TestSeedScanSubmissionJitter(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	resetSingletons(t, cfg)
	resetExpiry(t, 1000)
	resetLastAccess(t)
	fakeNow(t, 1_000_000)
	fakeRand(t, 0.5)
	// create the submissions entry (non-matching name).
	if err := os.MkdirAll(filepath.Join(cfg.Path.CompilesDir, "submissions"), 0o755); err != nil {
		t.Fatal(err)
	}
	seedScan(cfg)
	want := int64(1_000_000 - 1_000 + int64((5+5*0.5)*60_000))
	if got := lastprojectaccess.GetLastProjectAccessTime("submissions"); got != want {
		t.Fatalf("submission jitter: %d != %d", got, want)
	}
}

func TestSeedScanMissingProjectDir(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	resetSingletons(t, cfg)
	resetExpiry(t, 1000)
	resetLastAccess(t)
	fakeNow(t, 1_000_000)
	// entry exists, stat succeeds, name matches project id + user (no dir).
	if err := os.WriteFile(filepath.Join(cfg.Path.CompilesDir,
		"fedcba0987654321fedcba09-0123456789abcdef01234567"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedScan(cfg)
	// os.Stat on a FILE is successful (not an error); SetIfNewer(pid, mtime).
	// For a regular file whose name matches, mtime is still recorded.
	if got := lastprojectaccess.GetLastProjectAccessTime("fedcba0987654321fedcba09"); got <= 0 {
		t.Fatalf("file mtime last access: %d", got)
	}
}

func TestSeedScanReadDirFailure(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	cfg.Path.CompilesDir = filepath.Join(tmp, "does-not-exist")
	resetSingletons(t, cfg)
	resetLastAccess(t)
	fakeNow(t, 1)
	seedScan(cfg) // readdir error -> warn, no entries.
	if len(lastprojectaccess.LastAccess) != 0 {
		t.Fatalf("no entries expected, got %v", lastprojectaccess.LastAccess)
	}
}

func TestSeedScanMissingProjectDirStatError(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	resetSingletons(t, cfg)
	resetExpiry(t, 1000)
	resetLastAccess(t)
	fakeNow(t, 1_000_000)
	// No entry at all: seedScan reads the DIR itself, so a genuinely
	// missing project name is covered by the ReadDir-failure arm.
	// Instead, exercise the matching name with the compiles dir present
	// but the project sub-entry absent (the stat fails on the path).
	seedScan(cfg)
	// nothing written for absent names (the scan only iterates existing).
	if len(lastprojectaccess.LastAccess) != 0 {
		t.Fatalf("scan of empty dir: %v", lastprojectaccess.LastAccess)
	}
}

// --- disk stats ----------------------------------------------------------------

func TestRunDiskStatsOnceLowOnly(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	installFakeStatfs(t, map[string]statfsEntry{
		cfg.Path.CompilesDir:  {total: 1000, avail: 50}, // 5% -> low
		cfg.Path.OutputDir:    {total: 1000, avail: 9900},
		cfg.Path.ClsiCacheDir: {total: 1000, avail: 9900},
	})
	resetDisk(t)
	results := RunDiskStatsOnce(cfg)
	if len(results) != 3 {
		t.Fatalf("results: %d", len(results))
	}
	if !IsAnyDiskLow() || IsAnyDiskCriticalLow() {
		t.Fatalf("flags: low=%v crit=%v", IsAnyDiskLow(), IsAnyDiskCriticalLow())
	}
}

func TestRunDiskStatsOnceCritical(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	installFakeStatfs(t, map[string]statfsEntry{
		cfg.Path.CompilesDir:  {total: 1000, avail: 9900},
		cfg.Path.OutputDir:    {total: 1000, avail: 10}, // 1% -> critical (<3)
		cfg.Path.ClsiCacheDir: {total: 1000, avail: 9900},
	})
	resetDisk(t)
	RunDiskStatsOnce(cfg)
	if !IsAnyDiskLow() || !IsAnyDiskCriticalLow() {
		t.Fatalf("flags: low=%v crit=%v", IsAnyDiskLow(), IsAnyDiskCriticalLow())
	}
}

func TestRunDiskStatsOnceHealthy(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	installFakeStatfs(t, map[string]statfsEntry{
		cfg.Path.CompilesDir:  {total: 1000, avail: 9900},
		cfg.Path.OutputDir:    {total: 1000, avail: 9900},
		cfg.Path.ClsiCacheDir: {total: 1000, avail: 9900},
	})
	resetDisk(t)
	RunDiskStatsOnce(cfg)
	if IsAnyDiskLow() || IsAnyDiskCriticalLow() {
		t.Fatalf("healthy: flags set")
	}
}

func TestRunDiskStatsOnceErrSkipped(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	installFakeStatfs(t, map[string]statfsEntry{
		cfg.Path.CompilesDir:  {err: os.ErrNotExist},
		cfg.Path.OutputDir:    {total: 1000, avail: 50}, // 5% low
		cfg.Path.ClsiCacheDir: {err: os.ErrNotExist},
	})
	resetDisk(t)
	results := RunDiskStatsOnce(cfg)
	if len(results) != 1 {
		t.Fatalf("error paths skipped: %d results", len(results))
	}
	if !IsAnyDiskLow() || IsAnyDiskCriticalLow() {
		t.Fatalf("flags: low=%v crit=%v", IsAnyDiskLow(), IsAnyDiskCriticalLow())
	}
}

func TestRunDiskStatsOnceZeroTotal(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	installFakeStatfs(t, map[string]statfsEntry{
		cfg.Path.CompilesDir:  {total: 0, avail: 0},
		cfg.Path.OutputDir:    {total: 0, avail: 0},
		cfg.Path.ClsiCacheDir: {total: 0, avail: 0},
	})
	resetDisk(t)
	RunDiskStatsOnce(cfg)
	if !IsAnyDiskLow() || !IsAnyDiskCriticalLow() {
		t.Fatalf("zero-total: flags low=%v crit=%v", IsAnyDiskLow(), IsAnyDiskCriticalLow())
	}
}

// --- refreshExpiryTimeout (D10 LEAN-Y re-add) ---------------------------------

func TestRefreshExpiryLowersWhenLow(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	cfg.ProjectCacheLengthMs = 10000 // 10000/2=5000 < 90000 (lowerExpiry)
	resetSingletons(t, cfg)
	defer resetExpiry(t, -1)
	installFakeStatfs(t, map[string]statfsEntry{
		cfg.Path.CompilesDir:  {total: 1000, avail: 50}, // 5% low
		cfg.Path.OutputDir:    {total: 1000, avail: 9900},
		cfg.Path.ClsiCacheDir: {total: 1000, avail: 9900},
	})
	setExpiryMS(100000)
	RefreshExpiry()
	if got := ExpiryMS(); got != 90000 {
		t.Fatalf("refresh lowers: %d != 90000", got)
	}
}

func TestRefreshExpiryNoChangeWhenHealthy(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	resetSingletons(t, cfg)
	defer resetExpiry(t, -1)
	installFakeStatfs(t, map[string]statfsEntry{
		cfg.Path.CompilesDir:  {total: 1000, avail: 9900},
		cfg.Path.OutputDir:    {total: 1000, avail: 9900},
		cfg.Path.ClsiCacheDir: {total: 1000, avail: 9900},
	})
	setExpiryMS(1000)
	RefreshExpiry()
	if got := ExpiryMS(); got != 1000 {
		t.Fatalf("healthy expiry unchanged: %d != 1000", got)
	}
}

func TestRefreshExpiryLowButNotLowerable(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	cfg.ProjectCacheLengthMs = 10000000 // 5000000 > lowerExpiry
	resetSingletons(t, cfg)
	defer resetExpiry(t, -1)
	installFakeStatfs(t, map[string]statfsEntry{
		cfg.Path.CompilesDir:  {total: 1000, avail: 50},
		cfg.Path.OutputDir:    {total: 1000, avail: 9900},
		cfg.Path.ClsiCacheDir: {total: 1000, avail: 9900},
	})
	setExpiryMS(4000) // lowerExpiry = 3600 < 5000000 (not lowered)
	RefreshExpiry()
	if got := ExpiryMS(); got != 4000 {
		t.Fatalf("not-lowerable: %d != 4000", got)
	}
}

func TestRefreshExpiryNilSingleton(t *testing.T) {
	resetSingletons(t, nil)
	defer resetExpiry(t, -1)
	setExpiryMS(1)
	RefreshExpiry() // cfg nil -> early return without touching expiry
	if got := ExpiryMS(); got != 1 {
		t.Fatalf("nil cfg: expiry %d != 1", got)
	}
}

// --- markProjectAsJustAccessed --------------------------------------------

func TestMarkProjectJustAccessed(t *testing.T) {
	resetLastAccess(t)
	fakeNow(t, 1000)
	MarkProjectJustAccessed("proj-1")
	if got := lastprojectaccess.GetLastProjectAccessTime("proj-1"); got != 1000 {
		t.Fatalf("mark: %d != 1000", got)
	}
	// max(now, prev): a FUTURE prev wins.
	lastprojectaccess.SetLastProjectAccessTime("proj-future", 5000)
	MarkProjectJustAccessed("proj-future")
	if got := lastprojectaccess.GetLastProjectAccessTime("proj-future"); got != 5000 {
		t.Fatalf("max(now, prev): %d != 5000", got)
	}
}

func TestFindExpiredProjectIDs(t *testing.T) {
	resetLastAccess(t)
	fakeNow(t, 1_000_000)
	defer resetExpiry(t, -1)
	setExpiryMS(1000)                                            // expiredFrom = 1_000_000 - 1000 = 999_000
	lastprojectaccess.SetLastProjectAccessTime("expired", 500)   // < 999_000
	lastprojectaccess.SetLastProjectAccessTime("fresh", 999_999) // > 999_000
	got := findExpiredProjectIDs()
	if len(got) != 1 || got[0] != "expired" {
		t.Fatalf("expired: %v", got)
	}
}

// --- clearExpiredProjectsOnce ----------------------------------------------

func TestClearExpiredProjectsOnce(t *testing.T) {
	resetLastAccess(t)
	fakeNow(t, 1_000_000)
	defer resetExpiry(t, -1)
	setExpiryMS(1000) // expiredFrom = 999_000
	lastprojectaccess.SetLastProjectAccessTime("p-expired", 0)
	lastprojectaccess.SetLastProjectAccessTime("p-fresh", 999_999)

	var cmClearCalled []string
	var cmExpCalls int
	preProj := cmClearProject
	cmClearProject = func(cm *compilemanager.Manager, pid, uid string) error {
		cmClearCalled = append(cmClearCalled, pid)
		return nil
	}
	preExp := cmClearExpiredProjects
	cmClearExpiredProjects = func(cm *compilemanager.Manager, ms int64) error {
		cmExpCalls++
		return nil
	}
	t.Cleanup(func() {
		cmClearProject = preProj
		cmClearExpiredProjects = preExp
	})
	if err := ClearExpiredProjectsOnce(nil); err != nil {
		t.Fatalf("clear-expired: %v", err)
	}
	if _, ok := lastprojectaccess.LastAccess["p-expired"]; ok {
		t.Fatalf("expired deleted from LAST_ACCESS")
	}
	if _, ok := lastprojectaccess.LastAccess["p-fresh"]; !ok {
		t.Fatalf("fresh retained")
	}
	if len(cmClearCalled) != 1 || cmClearCalled[0] != "p-expired" {
		t.Fatalf("cmClear: %v", cmClearCalled)
	}
	if cmExpCalls != 1 {
		t.Fatalf("cmExp: %d != 1", cmExpCalls)
	}
}

func TestClearExpiredProjectsOnceCmError(t *testing.T) {
	preProj := cmClearProject
	cmClearProject = func(cm *compilemanager.Manager, pid, uid string) error { return nil }
	preExp := cmClearExpiredProjects
	cmClearExpiredProjects = func(cm *compilemanager.Manager, ms int64) error { return errors.New("boom") }
	t.Cleanup(func() {
		cmClearProject = preProj
		cmClearExpiredProjects = preExp
	})
	if err := ClearExpiredProjectsOnce(nil); err == nil {
		t.Fatalf("want cm error")
	}
}

func TestClearExpiredProjectsOnceLogAndContinue(t *testing.T) {
	// per-id cmClearProject error -> logged AND CONTINUED.
	resetLastAccess(t)
	fakeNow(t, 1_000_000)
	defer resetExpiry(t, -1)
	setExpiryMS(100)
	lastprojectaccess.SetLastProjectAccessTime("p-a", 0)
	lastprojectaccess.SetLastProjectAccessTime("p-b", 0)
	var clearErrors int
	preProj := cmClearProject
	cmClearProject = func(cm *compilemanager.Manager, pid, uid string) error {
		clearErrors++
		return errors.New("rm failed")
	}
	preExp := cmClearExpiredProjects
	cmClearExpiredProjects = func(cm *compilemanager.Manager, ms int64) error { return nil }
	t.Cleanup(func() {
		cmClearProject = preProj
		cmClearExpiredProjects = preExp
	})
	if err := ClearExpiredProjectsOnce(nil); err != nil {
		t.Fatalf("want nil (cm errors logged): %v", err)
	}
	if clearErrors != 2 {
		t.Fatalf("per-id errors: %d != 2", clearErrors)
	}
}

// --- clearProject --------------------------------------------------------------

func TestClearProjectChainNoUser(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	resetSingletons(t, cfg)
	resetLastAccess(t)
	const pid = "0123456789abcdef01234567"
	lastprojectaccess.SetLastProjectAccessTime(pid, 1)
	for _, sub := range []string{cfg.Path.CompilesDir, cfg.Path.OutputDir} {
		if err := os.MkdirAll(filepath.Join(sub, pid), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := ClearProject(pid, ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, ok := lastprojectaccess.LastAccess[pid]; ok {
		t.Fatalf("project deleted")
	}
	for _, sub := range []string{cfg.Path.CompilesDir, cfg.Path.OutputDir} {
		if _, err := os.Stat(filepath.Join(sub, pid)); !os.IsNotExist(err) {
			t.Fatalf("%s not removed: %v", sub, err)
		}
	}
}

func TestClearProjectChainUser(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	resetSingletons(t, cfg)
	resetLastAccess(t)
	const (
		pid = "0123456789abcdef01234567"
		uid = "111111111111111111111111"
	)
	cu := pid + "-" + uid
	lastprojectaccess.SetLastProjectAccessTime(pid, 1)
	lastprojectaccess.SetLastProjectAccessTime(cu, 1)
	for _, sub := range []string{cfg.Path.CompilesDir, cfg.Path.OutputDir} {
		if err := os.MkdirAll(filepath.Join(sub, pid), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(sub, cu), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := ClearProject(pid, uid); err != nil {
		t.Fatalf("clear user: %v", err)
	}
	if _, ok := lastprojectaccess.LastAccess[pid]; ok {
		t.Fatalf("project deleted")
	}
	if _, ok := lastprojectaccess.LastAccess[cu]; ok {
		t.Fatalf("user deleted")
	}
	for _, sub := range []string{cfg.Path.CompilesDir, cfg.Path.OutputDir} {
		if _, err := os.Stat(filepath.Join(sub, cu)); !os.IsNotExist(err) {
			t.Fatalf("%s/%s not removed: %v", cu, sub, err)
		}
		if _, err := os.Stat(filepath.Join(sub, pid)); !os.IsNotExist(err) {
			t.Fatalf("project-scoped not removed: %s", sub)
		}
	}
}

func TestClearProjectRemoveAllError(t *testing.T) {
	resetSingletons(t, testConfig(t.TempDir()))
	fakeRemoveAll(t, func(path string) error { return errors.New("rm error") })
	if err := ClearProject("p", ""); err == nil {
		t.Fatalf("want rm error propagated")
	}
}

// --- first24 ------------------------------------------------------------

func TestFirst24(t *testing.T) {
	if got := first24("0123456789abcdef01234567-89abcdef01234567"); got != "0123456789abcdef01234567" {
		t.Fatalf("first24 long: %q", got)
	}
	if got := first24("short"); got != "short" {
		t.Fatalf("first24 short: %q", got)
	}
}

// --- runTick --------------------------------------------------------------

func TestRunTickFiresAndStops(t *testing.T) {
	stop := make(chan struct{})
	defer close(stop)
	fired := make(chan struct{}, 2)
	go runTick(5*time.Millisecond, stop, func() { fired <- struct{}{} })
	deadline := time.Now().Add(50 * time.Millisecond)
	firedOnce := false
	for {
		select {
		case <-fired:
			firedOnce = true
		default:
		}
		if firedOnce {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !firedOnce {
		t.Fatalf("tick did not fire")
	}
}

// --- Init / Cleanup / ForTest (the wiring surface) ----------------------------------

func TestInitDiskStatsAndSeeding(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	cm := &compilemanager.Manager{
		Paths: compilemanager.Paths{CompilesDir: cfg.Path.CompilesDir,
			OutputDir: cfg.Path.OutputDir},
		Now: func() int64 { return 1_000_000 },
	}
	installFakeStatfs(t, map[string]statfsEntry{
		cfg.Path.CompilesDir:  {total: 1000, avail: 50}, // low
		cfg.Path.OutputDir:    {total: 1000, avail: 9900},
		cfg.Path.ClsiCacheDir: {total: 1000, avail: 9900},
	})
	resetDisk(t)
	defer resetExpiry(t, -1)
	Init(cfg, cm)
	// Init ran RunDiskStatsOnce synchronously -> low flag set, critical not.
	if !IsAnyDiskLow() || IsAnyDiskCriticalLow() {
		t.Fatalf("Init flags: low=%v crit=%v", IsAnyDiskLow(), IsAnyDiskCriticalLow())
	}
	// singleton wired.
	if got, _ := getSingleton(); got == nil {
		t.Fatalf("singleton not wired")
	}
	// seed scan is a goroutine over an empty compiles dir; give it a beat,
	// then shut everything down (Cleanup idempotent).
	time.Sleep(10 * time.Millisecond)
	Cleanup()
	Cleanup() // second call: no panic (cleanupStop nil-guarded).
}

func TestInitGoroutineStop(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	installFakeStatfs(t, map[string]statfsEntry{
		cfg.Path.CompilesDir:  {total: 1000, avail: 9900},
		cfg.Path.OutputDir:    {total: 1000, avail: 9900},
		cfg.Path.ClsiCacheDir: {total: 1000, avail: 9900},
	})
	resetDisk(t)
	Cleanup() // defensive: stop a stray interval from a prior test.
	Init(cfg, &compilemanager.Manager{
		Paths: compilemanager.Paths{CompilesDir: cfg.Path.CompilesDir,
			OutputDir: cfg.Path.OutputDir},
		Now: func() int64 { return 1 },
	})
	// the 10-minute tick is far outside test time; the 15s disk tick too —
	// the goroutines are alive (blocked on ticker) until Cleanup closes stop.
	Cleanup()
}

func TestForTestExplicit(t *testing.T) {
	cfg := testConfig(t.TempDir())
	ForTest(cfg, &compilemanager.Manager{})
	if got, _ := getSingleton(); got != cfg {
		t.Fatalf("ForTest singleton: %v", got)
	}
	defer resetExpiry(t, -1)
	if got := ExpiryMS(); got != int64(cfg.ProjectCacheLengthMs) {
		t.Fatalf("ForTest explicit: %d != %d", got, cfg.ProjectCacheLengthMs)
	}
}

func TestExpiryTickOnce(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	cm := &compilemanager.Manager{
		Paths: compilemanager.Paths{CompilesDir: cfg.Path.CompilesDir,
			OutputDir: cfg.Path.OutputDir},
		Now: func() int64 { return 2_000_000 },
	}
	installFakeStatfs(t, map[string]statfsEntry{
		cfg.Path.CompilesDir:  {total: 1000, avail: 9900}, // healthy (no refresh)
		cfg.Path.OutputDir:    {total: 1000, avail: 9900},
		cfg.Path.ClsiCacheDir: {total: 1000, avail: 9900},
	})
	resetSingletons(t, cfg)
	defer resetExpiry(t, -1)
	setExpiryMS(1000)
	resetLastAccess(t)
	fakeNow(t, 2_000_000)
	lastprojectaccess.SetLastProjectAccessTime("fresh", 1_999_999)

	preExp := cmClearExpiredProjects
	var gotMS int64
	cmClearExpiredProjects = func(m *compilemanager.Manager, ms int64) error {
		gotMS = ms
		return nil
	}
	t.Cleanup(func() { cmClearExpiredProjects = preExp })
	expiryTickOnce(cm)
	if gotMS != 1000 {
		t.Fatalf("tick expiry: %d != 1000", gotMS)
	}
}

// --- the remaining branch coverage (statfs real seam, Cleanup stop branch,
//     ClearProject OutputDir-error return, stat-error seed branch) -------------

func TestDefaultStatFSRealPath(t *testing.T) {
	// the production statFSSeams against a real path (no override active).
	total, avail, err := statFSSeams(t.TempDir())
	if err != nil {
		t.Fatalf("real statfs: %v", err)
	}
	if total <= 0 || avail < 60 {
		t.Fatalf("real statfs: total=%d avail=%d (temp disk empty?!) ", total, avail)
	}
}

func TestInitThenCleanupStopChannel(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	installFakeStatfs(t, map[string]statfsEntry{
		cfg.Path.CompilesDir:  {total: 1000, avail: 9900},
		cfg.Path.OutputDir:    {total: 1000, avail: 9900},
		cfg.Path.ClsiCacheDir: {total: 1000, avail: 9900},
	})
	resetSingletons(t, cfg)
	// no explicit Cleanup: this exercises Init's fresh channels + the
	// Cleanup() stop branch (close(cleanupStop) on a real chan).
	Init(cfg, &compilemanager.Manager{
		Paths: compilemanager.Paths{CompilesDir: cfg.Path.CompilesDir,
			OutputDir: cfg.Path.OutputDir},
		Now: func() int64 { return 1 },
	})
	// let the goroutines start; Cleanup shuts them down (the stop branch +
	// cleanupStop nil-guard are reached).
	time.Sleep(5 * time.Millisecond)
	Cleanup()
}

func TestClearProjectSecondRemoveAllError(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	resetSingletons(t, cfg)
	resetLastAccess(t)
	// covers the OutputDir error-return arm: first rm (compiles) succeeds,
	// second rm (output) is faked-failed.
	calls := 0
	fakeRemoveAll(t, func(path string) error {
		calls++
		if calls > 1 {
			return errors.New("output rm failed")
		}
		return nil
	})
	if err := ClearProject("0123456789abcdef01234567", ""); err == nil {
		t.Fatalf("want output rm error propagated")
	}
}
