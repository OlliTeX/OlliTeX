// Package bridge ports bridge/Bridge.java — the heart of the Git Bridge. It
// is the central coordinator between the git-protocol layer (JGit hooks in
// Java), the snapshot API, and the on-disk/git/db/swap stores.
//
// It holds:
//
//   - a ProjectLock (data.ProjectLock) — shared with the swap job
//   - a RepoStore, DBStore, SwapStore
//   - a SwapJob and a GcJob
//   - a SnapshotApiFacade (narrowed here to SnapshotAPI)
//   - a ResourceCache
//   - a PostbackManager (created internally)
//
// The constructor is split into a factory (BridgeFromConfig, mirroring
// Bridge.make) that wires up shared collaborators from a config and a
// concrete *repo.FSGitRepoStore, and a full-component constructor (NewBridge)
// that takes narrow interfaces so tests can substitute fakes (mirroring the
// package-private Bridge(...) constructor Java's Mockito tests use).
package bridge

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"ollitex/go/services/gitbridge/config"
	"ollitex/go/services/gitbridge/data"
	"ollitex/go/services/gitbridge/db"
	"ollitex/go/services/gitbridge/filestore"
	"ollitex/go/services/gitbridge/gc"
	"ollitex/go/services/gitbridge/giterrors"
	"ollitex/go/services/gitbridge/repo"
	"ollitex/go/services/gitbridge/resource"
	"ollitex/go/services/gitbridge/snapshot"
	"ollitex/go/services/gitbridge/swap"
	"ollitex/go/services/gitbridge/wglog"
)

// ---------------------------------------------------------------------------
// Narrow seams (mirror the Java interfaces Mockito fakes in BridgeTest).
//
// *repo.Project satisfies ProjectRepo and *repo.FSGitRepoStore's concrete
// methods are adapted into RepoStore (Go does not do covariance on return
// types, so a shim translates the *repo.Project returns).
// ---------------------------------------------------------------------------

// ProjectRepo ports ProjectRepo — the per-project repo access the Bridge
// needs (faked in tests). *repo.Project satisfies it directly.
type ProjectRepo interface {
	// GetProjectName ports getProjectName.
	GetProjectName() string
	// ProjectDir ports getProjectDir / getDotGitDir.
	ProjectDir() string
	// GetDirectory ports getDirectory (walk HEAD/commit tree to files).
	GetDirectory() (map[string]filestore.RawFile, error)
	// CommitAndGetMissing ports doCommitAndGetMissing.
	CommitAndGetMissing(contents *filestore.GitDirectoryContents) ([]string, error)
	RunGC() error
	DeleteIncomingPacks() error
}

// RepoStore ports the RepoStore methods the Bridge uses (faked in tests).
// Production adapts *repo.FSGitRepoStore into this via repoShim.
type RepoStore interface {
	// GetRootDirectory ports getRootDirectory.
	GetRootDirectory() string
	// InitRepo ports initRepo.
	InitRepo(project string) (ProjectRepo, error)
	// GetExistingRepo ports getExistingRepo.
	GetExistingRepo(project string) (ProjectRepo, error)
	// PurgeNonexistentProjects ports purgeNonexistentProjects.
	PurgeNonexistentProjects(existing []string) error
	// Remove ports remove.
	Remove(project string) error
}

// SnapshotAPI ports the SnapshotApiFacade methods the Bridge uses (faked in
// tests). *snapshot.Facade satisfies it. Nil token == Java Optional.empty().
type SnapshotAPI interface {
	GetDoc(token *data.Oauth2, projectName string) (*snapshot.GetDocResult, error)
	GetSnapshots(token *data.Oauth2, projectName string, afterVersionID int) ([]*data.Snapshot, error)
	Push(token *data.Oauth2, projectName string, body []byte) (*snapshot.PushResult, error)
}

// ResourceCacheIface ports the ResourceCache seam. *resource.UrlResourceCache
// satisfies it.
type ResourceCacheIface interface {
	Get(projectName, url, path string, fileTable map[string]filestore.RawFile, fetchedUrls map[string][]byte, maxFileSize *int64) (filestore.RawFile, error)
}

// repoShim adapts the concrete *repo.FSGitRepoStore into the narrow RepoStore
// seam, translating the concrete *repo.Project returns into ProjectRepo.
type repoShim struct{ fs *repo.FSGitRepoStore }

func (s repoShim) GetRootDirectory() string { return s.fs.GetRootDirectory() }
func (s repoShim) InitRepo(project string) (ProjectRepo, error) {
	p, err := s.fs.InitRepo(project)
	if err != nil {
		return nil, err
	}
	return p, nil
}
func (s repoShim) GetExistingRepo(project string) (ProjectRepo, error) {
	p, err := s.fs.GetExistingRepo(project)
	if err != nil {
		return nil, err
	}
	return p, nil
}
func (s repoShim) PurgeNonexistentProjects(existing []string) error {
	return s.fs.PurgeNonexistentProjects(existing)
}
func (s repoShim) Remove(project string) error { return s.fs.Remove(project) }

// gcRepoAdapter adapts *repo.FSGitRepoStore into gc.RepoStore (the GC job's
// narrow RepoStore seam), translating GetExistingRepo's *repo.Project return
// into gc.ProjectRepo.
type gcRepoAdapter struct{ fs *repo.FSGitRepoStore }

func (a gcRepoAdapter) GetExistingRepo(project string) (gc.ProjectRepo, error) {
	p, err := a.fs.GetExistingRepo(project)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// lockWaiter ports the ProjectLockImpl waiter `(int) -> Log.debug("Waiting
// for N projects...")`.
type lockWaiter struct{}

func (lockWaiter) ThreadsRemaining(threads int) {
	wglog.Debug("Waiting for %d projects...", threads)
}

// ---------------------------------------------------------------------------
// Bridge.
// ---------------------------------------------------------------------------

// Bridge is the constructed coordinator. Field receivers are unexported.
type Bridge struct {
	config          *config.Config
	lock            *data.ProjectLock
	repoStore       RepoStore
	dbStore         db.DBStore
	swapStore       swap.SwapStore
	swapJob         swap.SwapJob
	gcJob           gc.GcJob
	snapshotAPI     SnapshotAPI
	resourceCache   ResourceCacheIface
	postbackManager *snapshot.PostbackManager
	hookInstaller   func() (string, error)
}

// NewBridge ports the package-private Bridge(...) constructor. It creates the
// PostbackManager and purges non-existent projects from disk (keep the names
// the DB knows about + ".wlgb"). swapJob/gcJob snapshots are wired by the
// caller so tests can fake them.
//
// NOTE: Java's constructor also registers a JVM shutdown hook
// (`Runtime.getRuntime().addShutdownHook(new Thread(this::doShutdown))`). Go
// has no per-object JVM hook; the next layer (the server) drives Shutdown on
// a signal. The behaviour is otherwise identical.
func NewBridge(
	cfg *config.Config,
	lock *data.ProjectLock,
	repoStore RepoStore,
	dbStore db.DBStore,
	swapStore swap.SwapStore,
	swapJob swap.SwapJob,
	gcJob gc.GcJob,
	snapshotAPI SnapshotAPI,
	resourceCache ResourceCacheIface,
) *Bridge {
	b := &Bridge{
		config:          cfg,
		lock:            lock,
		repoStore:       repoStore,
		dbStore:         dbStore,
		swapStore:       swapStore,
		swapJob:         swapJob,
		gcJob:           gcJob,
		snapshotAPI:     snapshotAPI,
		resourceCache:   resourceCache,
		postbackManager: snapshot.NewPostbackManager(),
	}
	if err := b.repoStore.PurgeNonexistentProjects(dbStore.GetProjectNames()); err != nil {
		wglog.Warn("failed to purge non-existent projects: %v", err)
	}
	// Cross-process postback promise state (Option 3a): wire the shared
	// sqlite store when the db store provides it. Pure in-memory behavior
	// is preserved when it doesn't (unit fakes).
	type postbackStoreIf interface {
		PostbackPut(project, key, status string, versionID int, body string)
		PostbackGet(project string) (string, string, int, string, bool)
		PostbackDelete(project string)
	}
	if ps, ok := dbStore.(postbackStoreIf); ok {
		b.postbackManager.SetStore(ps)
	}
	return b
}

// BridgeFromConfig ports Bridge.make: it wires up the shared collaborators
// (the lock is shared with the swap job, GcJob and SnapshotApiFacade are built
// from the raw API, UrlResourceCache is built from dbStore) and delegates to
// NewBridge. repoStore is the concrete store so the swap job can tar it and
// the GC job can run `git gc` on it.
func BridgeFromConfig(cfg *config.Config, fsRepoStore *repo.FSGitRepoStore, dbStore db.DBStore, swapStore swap.SwapStore, api snapshot.SnapshotApi) *Bridge {
	lock := data.NewProjectLock(lockWaiter{})
	swapJob := swap.FromConfig(cfg.SwapJob, lock, fsRepoStore, dbStore, swapStore)
	gcJob := gc.NewGcJobDefault(gcRepoAdapter{fsRepoStore}, lock)
	facade := snapshot.NewFacade(api)
	resourceCache := resource.NewUrlResourceCache(dbStore)
	return NewBridge(cfg, lock, repoShim{fsRepoStore}, dbStore, swapStore, swapJob, gcJob, facade, resourceCache)
}

// ---------------------------------------------------------------------------
// Shutdown + background jobs.
// ---------------------------------------------------------------------------

// Shutdown ports doShutdown. It stops the swap and GC jobs and then acquires
// the project write lock (LockAll), which blocks any further project lock
// acquisitions and waits for existing readers to drain — the graceful-shutdown
// barrier.
func (b *Bridge) Shutdown() {
	wglog.Info("Shutdown received.")
	wglog.Info("Stopping SwapJob")
	b.swapJob.Stop()
	wglog.Info("Stopping GcJob")
	b.gcJob.Stop()
	wglog.Info("Waiting for projects")
	b.lock.LockAll()
	wglog.Info("Bye")
}

// StartBackgroundJobs ports startBackgroundJobs: start the swap job and the
// GC job.
func (b *Bridge) StartBackgroundJobs() {
	b.swapJob.Start()
	b.gcJob.Start()
}

// HealthCheck ports healthCheck: probe the DB and confirm the filesystem root
// exists, returning whether the bridge is healthy.
func (b *Bridge) HealthCheck() bool {
	_ = b.dbStore.GetNumProjects()
	if !pathExists("/") {
		wglog.Error("[HealthCheck] FAILED! bad filesystem state, root directory does not exist")
		return false
	}
	wglog.Debug("[HealthCheck] passed")
	return true
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// CheckDB ports checkDB: scan the repo root and repair DB rows for project
// dirs that are on disk but missing a DB row. A lock failure is surfaced as a
// wrapped error (Java wraps CannotAcquireLockException in a RuntimeException).
func (b *Bridge) CheckDB() error {
	wglog.Debug("Checking DB")
	root := b.repoStore.GetRootDirectory()
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() {
		return nil // nothing to check (Java: listFiles() on a missing/non-dir root returns nothing or NPEs; the Go port is lenient)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.Name() == ".wlgb" {
			continue
		}
		if err := b.checkDBProject(e.Name()); err != nil {
			if _, ok := err.(data.CannotAcquireLockException); ok {
				return fmt.Errorf("checkDB: %w", err)
			}
			return err
		}
	}
	return nil
}

// checkDBProject ports one loop iteration of checkDB (holding the project
// lock for the duration of the iteration, as Java's try-with-resources does).
func (b *Bridge) checkDBProject(projName string) error {
	guard, err := b.lock.LockForProjectGuard(projName)
	if err != nil {
		return err
	}
	defer guard.Close()
	dotGit := filepath.Join(b.repoStore.GetRootDirectory(), projName, ".git")
	if !pathExists(dotGit) {
		wglog.Warn("Project: %s has no .git", projName)
		return nil
	}
	if b.dbStore.GetProjectState(projName) != db.ProjectStateNotPresent {
		return nil
	}
	wglog.Warn("Project: %s not in swap_store, adding", projName)
	// Java: new File(f, ".git").lastModified() — 0 when the file cannot be stat
	// (lastModifiedMillis ports that exactly).
	ms := lastModifiedMillis(dotGit)
	b.dbStore.SetLastAccessedTime(projName, &ms)
	return nil
}

// lastModifiedMillis ports java.io.File.lastModified: the file's
// last-modified epoch millis, or 0 when it cannot be stat.
func lastModifiedMillis(path string) int64 {
	info, err := os.Lstat(path)
	if err != nil {
		return 0
	}
	return info.ModTime().UnixMilli()
}

// GetUpdatedRepo ports getUpdatedRepo: lock the project, fetch the doc from
// the snapshot API, and synchronise the repo with Overleaf. A missing doc is
// a RepositoryNotFoundException.
func (b *Bridge) GetUpdatedRepo(oauth2 *data.Oauth2, projectName string) (ProjectRepo, error) {
	holder, err := b.lock.Acquire(projectName)
	if err != nil {
		return nil, err
	}
	defer b.lock.Release(projectName, holder)
	doc, err := b.snapshotAPI.GetDoc(oauth2, projectName)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, &giterrors.RepositoryNotFoundException{ProjectName: projectName}
	}
	wglog.Debug("[%s] Updating repository", projectName)
	return b.getUpdatedRepoCritical(oauth2, projectName, holder)
}

// getUpdatedRepoCritical ports getUpdatedRepoCritical (pre: the project lock
// is held). It materialises the project (init / restore / existing) then
// applies any pending snapshots and records the last-accessed time.
func (b *Bridge) getUpdatedRepoCritical(oauth2 *data.Oauth2, projectName string, holder *data.Holder) (ProjectRepo, error) {
	var repo ProjectRepo
	switch b.dbStore.GetProjectState(projectName) {
	case db.ProjectStateNotPresent:
		wglog.Debug("[%s] Repo not present", projectName)
		r, err := b.repoStore.InitRepo(projectName)
		if err != nil {
			return nil, err
		}
		repo = r
	case db.ProjectStateSwapped:
		// Restore re-enters the project lock that this call already holds, so
		// pass the holder token down (Java: same-thread re-entrant unlock).
		if err := b.swapJob.RestoreWith(holder, projectName); err != nil {
			return nil, err
		}
		r, err := b.repoStore.GetExistingRepo(projectName)
		if err != nil {
			return nil, err
		}
		repo = r
	default:
		r, err := b.repoStore.GetExistingRepo(projectName)
		if err != nil {
			return nil, err
		}
		repo = r
	}
	if err := b.ensureHook(repo); err != nil {
		return nil, fmt.Errorf("bridge: proc-receive hook: %w", err)
	}
	if err := b.updateProject(oauth2, repo); err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	b.dbStore.SetLastAccessedTime(projectName, &now)
	return repo, nil
}

// Push ports push: lock the project, perform the snapshot push (creating a
// postback key, candidate snapshot and snapshot-API push) and queue a GC.

// HookInstaller is implemented by repos that accept a proc-receive hook shim
// (production *repo.Project does; unit-test fakes do not — the seam is then
// a no-op, preserving their behavior).
type HookInstaller interface {
	InstallProcReceiveHook(hookBody string) error
}

// SetProcReceiveHook wires the proc-receive hook installer (Option 3a,
// HANDOFF §14.3/§15). Shelled `git receive-pack` requires the hook ON DISK:
// without it a push is a plain git ref update and the bridge's write path
// (candidate snapshot → Overleaf push → postback) is bypassed entirely. Java's
// twin is the in-process JGit WriteLatexPutHook (no on-disk artifact), so the
// Go port must materialise it. fn returns the hook shim body.
func (b *Bridge) SetProcReceiveHook(fn func() (string, error)) { b.hookInstaller = fn }

// ensureHook installs the hook when the repo supports it (idempotent — the
// installer overwrites shim + config).
func (b *Bridge) ensureHook(repo ProjectRepo) error {
	if b.hookInstaller == nil {
		return nil
	}
	hi, ok := repo.(HookInstaller)
	if !ok {
		return nil
	}
	body, err := b.hookInstaller()
	if err != nil {
		return err
	}
	return hi.InstallProcReceiveHook(body)
}

func (b *Bridge) Push(oauth2 *data.Oauth2, projectName string, directoryContents, oldDirectoryContents *filestore.RawDirectory, hostname string) error {
	wglog.Debug("[%s] pushing to Overleaf", projectName)
	holder, err := b.lock.Acquire(projectName)
	if err != nil {
		return err
	}
	defer b.lock.Release(projectName, holder)
	wglog.Debug("[%s] got project lock", projectName)
	if err := b.pushCritical(oauth2, projectName, directoryContents, oldDirectoryContents); err != nil {
		// Java distinguishes SevereSnapshotPostException / SnapshotPostException /
		// IOException and re-throws each. Go threads the same error value up;
		// user-facing git exceptions (with Description()) are logged at warn level.
		if ge, ok := err.(interface{ Description() []string }); ok {
			_ = ge
			wglog.Warn("[%s] push failed: %v", projectName, err)
		} else {
			wglog.Warn("[%s] IOException on put: %v", projectName, err)
		}
		return err
	}
	b.gcJob.QueueForGc(projectName)
	return nil
}

// pushCritical ports pushCritical (pre: the project lock is held). It enforces
// the file-count limit, creates the postback key and candidate snapshot,
// pushes to the snapshot API and — on success — waits for the postback and
// approves the snapshot.
func (b *Bridge) pushCritical(oauth2 *data.Oauth2, projectName string, directoryContents, oldDirectoryContents *filestore.RawDirectory) error {
	if rs := b.config.RepoStore; rs != nil && rs.MaxFileNum != nil {
		if n := len(directoryContents.FileTable); n > int(*rs.MaxFileNum) {
			wglog.Warn("[%s] Too many files: %d/%d", projectName, n, int(*rs.MaxFileNum))
			return &giterrors.FileLimitExceededException{NumFiles: int64(n), MaxFiles: *rs.MaxFileNum}
		}
	}
	wglog.Debug("[%s] Pushing files (%d new, %d old)", projectName,
		len(directoryContents.FileTable), len(oldDirectoryContents.FileTable))
	postbackKey := b.postbackManager.MakeKeyForProject(projectName)
	wglog.Debug("[%s] Created postback key: %s", projectName, postbackKey)
	candidate, err := b.createCandidateSnapshot(projectName, directoryContents, oldDirectoryContents)
	if err != nil {
		return err
	}
	defer candidate.DeleteServletFiles()
	wglog.Debug("[%s] Candidate snapshot created", projectName)
	body, err := candidate.JSONRepresentation(postbackKey)
	if err != nil {
		return err
	}
	result, err := b.snapshotAPI.Push(oauth2, projectName, []byte(body))
	if err != nil {
		return err
	}
	if result.WasSuccessful {
		wglog.Debug("[%s] Push to Overleaf successful", projectName)
		wglog.Debug("[%s] Waiting for postback...", projectName)
		versionID, err := b.postbackManager.WaitProjectForVersionIdOrThrow(projectName)
		if err != nil {
			return err
		}
		wglog.Debug("[%s] Got version ID for push: %d", projectName, versionID)
		b.approveSnapshot(versionID, candidate)
		wglog.Debug("[%s] Approved version ID: %d", projectName, versionID)
		now := time.Now().UnixMilli()
		b.dbStore.SetLastAccessedTime(projectName, &now)
		return nil
	}
	wglog.Warn("[%s] Went out of date while waiting for push", projectName)
	return &giterrors.OutOfDateException{}
}

// updateProject ports updateProject: fetch the snapshots newer than the last
// version we have, commit them to the repo, and record the new latest version.
func (b *Bridge) updateProject(oauth2 *data.Oauth2, r ProjectRepo) error {
	projectName := r.GetProjectName()
	latestVersionId := b.dbStore.GetLatestVersionForProject(projectName)
	snapshots, err := b.snapshotAPI.GetSnapshots(oauth2, projectName, latestVersionId)
	if err != nil {
		return err
	}
	if err := b.makeCommitsFromSnapshots(r, snapshots); err != nil {
		return err
	}
	if len(snapshots) > 0 {
		last := snapshots[len(snapshots)-1]
		b.dbStore.SetLatestVersionForProject(projectName, last.VersionID)
	}
	return nil
}

// makeCommitsFromSnapshots ports makeCommitsFromSnapshots: per snapshot,
// enforce the file-size limit, write files to disk (fetching atts from the
// resource cache) and commit them, deleting files that are now gone.
func (b *Bridge) makeCommitsFromSnapshots(r ProjectRepo, snapshots []*data.Snapshot) error {
	name := r.GetProjectName()
	var maxSize *int64
	if b.config.RepoStore != nil {
		maxSize = b.config.RepoStore.MaxFileSize
	}
	for _, snapshot := range snapshots {
		fileTable, err := r.GetDirectory()
		if err != nil {
			return err
		}
		files := []filestore.RawFile{}
		for _, sf := range snapshot.Srcs {
			if maxSize != nil && sf.Size() >= *maxSize {
				return &giterrors.SizeLimitExceededException{Path: sf.Path, ActualSize: sf.Size(), MaxSize: *maxSize}
			}
			files = append(files, filestore.NewRepositoryFile(sf.Path, sf.Contents))
		}
		fetchedUrls := map[string][]byte{}
		for _, att := range snapshot.Atts {
			rf, err := b.resourceCache.Get(name, att.URL, att.Path, fileTable, fetchedUrls, maxSize)
			if err != nil {
				return err
			}
			files = append(files, rf)
		}
		wglog.Debug("[%s] Committing version ID: %d", name, snapshot.VersionID)
		missing, err := r.CommitAndGetMissing(filestore.NewGitDirectoryContents(
			files,
			b.repoStore.GetRootDirectory(),
			name,
			snapshot.User.Name,
			snapshot.User.Email,
			snapshot.Comment,
			snapshot.CreatedAt,
		))
		if err != nil {
			return err
		}
		b.dbStore.DeleteFilesForProject(name, missing...)
	}
	return nil
}

// createCandidateSnapshot ports createCandidateSnapshot: build the candidate
// snapshot (diff of new vs old contents) and write its atts to the atts dir.
func (b *Bridge) createCandidateSnapshot(projectName string, directoryContents, oldDirectoryContents *filestore.RawDirectory) (*data.CandidateSnapshot, error) {
	candidate := data.NewCandidateSnapshot(projectName, b.dbStore.GetLatestVersionForProject(projectName), directoryContents, oldDirectoryContents)
	if err := candidate.WriteServletFiles(b.repoStore.GetRootDirectory()); err != nil {
		return nil, err
	}
	return candidate, nil
}

// approveSnapshot ports approveSnapshot: record the new version and drop the
// deleted files from the DB.
func (b *Bridge) approveSnapshot(versionID int, candidateSnapshot *data.CandidateSnapshot) {
	deleted := candidateSnapshot.GetDeleted()
	b.dbStore.SetLatestVersionForProject(candidateSnapshot.GetProjectName(), versionID)
	b.dbStore.DeleteFilesForProject(candidateSnapshot.GetProjectName(), deleted...)
}

// CheckPostbackKey ports checkPostbackKey (FileHandler): the key must match
// the pending postback for the project or the file is not served.
func (b *Bridge) CheckPostbackKey(projectName, postbackKey string) error {
	return b.postbackManager.CheckPostbackKey(projectName, postbackKey)
}

// PostbackReceivedSuccessfully ports postbackReceivedSuccessfully: the
// Overleaf app finished an atts fetch and committed the push; fulfil the
// promise with the new version id.
func (b *Bridge) PostbackReceivedSuccessfully(projectName, postbackKey string, versionID int) error {
	wglog.Debug("[%s] Postback received by postback thread, version: %d", projectName, versionID)
	return b.postbackManager.PostVersionIDForProject(projectName, versionID, postbackKey)
}

// PostbackReceivedWithException ports postbackReceivedWithException: the
// Overleaf app reported an error on the postback; fulfil the promise with the
// exception.
func (b *Bridge) PostbackReceivedWithException(projectName, postbackKey string, exception error) error {
	wglog.Warn("[%s] Postback received with exception", projectName)
	return b.postbackManager.PostExceptionForProject(projectName, exception, postbackKey)
}

// DeleteProject ports deleteProject: drop the project's DB row, repo dir and
// swap-store entry.
func (b *Bridge) DeleteProject(projectName string) {
	wglog.Info("[%s] deleting project", projectName)
	b.dbStore.DeleteProject(projectName)
	if err := b.repoStore.Remove(projectName); err != nil {
		wglog.Warn("Failed to delete repository for project %s: %v", projectName, err)
	}
	_ = b.swapStore.Remove(projectName)
}
