// Package syncmanager is the 1:1 port of
// services/project-history/app/js/SyncManager.js (1651 L) — resync state
// machine, skip-during-sync, and the SyncUpdateExpander.
package syncmanager

import (
	"context"
	"fmt"
	"time"
)

const maxStringLength = 3 * 1024 * 1024 // vendor TextOperation.MAX_STRING_LENGTH

// Deps — external seam (vendor imports: db, LockManager, ErrorRecorder,
// RedisManager, WebApiManager, SnapshotManager, UpdateCompressor,
// UpdateTranslator, HashManager, TextOperation).
type Deps struct {
	// mongo (projectHistorySyncState + projects)
	FindOneState      func(ctx context.Context, projectID string) (map[string]any, bool, error)
	InsertOneState    func(ctx context.Context, doc map[string]any) error
	UpdateStateDoc    func(ctx context.Context, projectID string, update map[string]any, upsert bool) error
	DeleteStateDoc    func(ctx context.Context, projectID string, match map[string]any) error
	UpdateProjectsDoc func(ctx context.Context, projectID string, update map[string]any) error

	// lock (vendor LockManager.runWithLock(key, runner, done))
	RunWithLock func(key string, runner func(extend func() error, release func(error, ...any) error), done func(error, ...any))

	// error recorder
	RecordSyncStart func(ctx context.Context, projectID string) error
	RecordError     func(ctx context.Context, projectID string, queueSize int, err error) error

	// redis
	ClearFirstOpTimestamp  func(ctx context.Context, projectID string) error
	DestroyDocUpdatesQueue func(ctx context.Context, projectID string) error
	DeleteAppliedDocUpdate func(ctx context.Context, projectID string, update map[string]any) error

	// web
	RequestResync func(ctx context.Context, projectID string, opts map[string]any) error
	GetHistoryID  func(ctx context.Context, projectID string) (string, error)

	// snapshot
	GetLatestSnapshotFilesForChunk func(ctx context.Context, historyID string, chunk map[string]any) (map[string]*File, error)

	// content load (vendor file.load('eager', blobStore) + getContent)
	LoadFileContent func(ctx context.Context, projectID string, f *File) error

	// blob hash + diff + path conversion
	GetBlobHashFromString func(s string) string
	DiffAsShareJsOps      func(a, b string) []any
	ConvertPathname       func(s string) string

	// corruption detection (vendor isDataCorruptionError)
	IsDataCorruption func(err error) bool

	// metrics / log / clock
	Inc      func(name string, n int, info map[string]any)
	LogDebug func(info map[string]any, msg string)
	LogWarn  func(info map[string]any, msg string)
	LogErr   func(info map[string]any, msg string)
	Now      func() time.Time
}

func (d *Deps) withDefaults() *Deps {
	if d == nil {
		d = &Deps{}
	}
	if d.Inc == nil {
		d.Inc = func(string, int, map[string]any) {}
	}
	if d.LogDebug == nil {
		d.LogDebug = func(map[string]any, string) {}
	}
	if d.LogWarn == nil {
		d.LogWarn = func(map[string]any, string) {}
	}
	if d.LogErr == nil {
		d.LogErr = func(map[string]any, string) {}
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.ConvertPathname == nil {
		d.ConvertPathname = func(s string) string { return s }
	}
	return d
}

// isDataCorruptionError — vendor: UnprocessableError family (ApplyError,
// InvalidInsertionError, TooLongError), SyntaxError (invalid JSON blob),
// FileContentEmptyError.
func (d *Deps) isDataCorruptionError(err error) bool {
	if d.IsDataCorruption != nil {
		return d.IsDataCorruption(err)
	}
	switch err.(type) {
	case *UnprocessableError, *FileContentEmptyError, *TooLongError:
		return true
	}
	return err.Error() == "invalid syntax" || err.Error() == "invalid JSON"
}

// lockKey — vendor keys.projectHistoryLock({project_id}).
func lockKey(projectID string) string { return "projectHistoryLock:" + projectID }

// --- cloneResyncState (vendor) ---------------------------------------------

func (d *Deps) CloneResyncState(ctx context.Context, sourceProjectID, targetProjectID string) error {
	d = d.withDefaults()
	raw, found, err := d.FindOneState(ctx, sourceProjectID)
	if err != nil {
		return err
	}
	if !found || raw == nil {
		return nil
	}
	doc := map[string]any{}
	for k, v := range raw {
		if k == "_id" || k == "project_id" {
			continue
		}
		doc[k] = v
	}
	doc["project_id"] = targetProjectID
	return d.InsertOneState(ctx, doc)
}

// --- getResyncState / setResyncState (vendor) -------------------------------

func (d *Deps) GetResyncState(ctx context.Context, projectID string) (*SyncState, error) {
	d = d.withDefaults()
	raw, _, err := d.FindOneState(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return FromRaw(projectID, raw), nil
}

func (d *Deps) SetResyncState(ctx context.Context, projectID string, state *SyncState) error {
	d = d.withDefaults()
	if state == nil {
		return nil
	}
	now := d.Now()
	update := map[string]any{
		"$set": state.ToRaw(),
		"$push": map[string]any{
			"history": map[string]any{
				"$each":     []any{map[string]any{"syncState": state.ToRaw(), "timestamp": now}},
				"$position": 0,
				"$slice":    MaxResyncHistoryRecords,
			},
		},
		"$currentDate": map[string]any{"lastUpdated": true},
	}
	if state.IsSyncOngoing() {
		update["$inc"] = map[string]any{"resyncCount": 1}
		update["$unset"] = map[string]any{"expiresAt": true}
		update["$min"] = map[string]any{"resyncPendingSince": now}
	} else {
		update["$set"].(map[string]any)["expiresAt"] = now.Add(time.Duration(ExpireResyncHistoryMs) * time.Millisecond)
		update["$unset"] = map[string]any{
			"resyncPendingSince": 1,
			"stuckClearCount":    1,
			"lastStuckClearAt":   1,
			"lastStuckDocPaths":  1,
		}
	}
	if err := d.UpdateStateDoc(ctx, projectID, update, true); err != nil {
		return err
	}
	if !state.IsSyncOngoing() {
		return d.UpdateProjectsDoc(ctx, projectID, map[string]any{
			"$max": map[string]any{"overleaf.history.lastResyncedAt": now},
		})
	}
	return nil
}

// clearResyncState (vendor).
func (d *Deps) ClearResyncState(ctx context.Context, projectID string) error {
	d = d.withDefaults()
	return d.DeleteStateDoc(ctx, projectID, map[string]any{"project_id": projectID})
}

// recordStuckClearInSyncState — vendor _recordStuckClearInSyncState.
func (d *Deps) recordStuckClearInSyncState(ctx context.Context, projectID string, stuckDocPaths []string) error {
	d = d.withDefaults()
	return d.UpdateStateDoc(ctx, projectID, map[string]any{
		"$inc":   map[string]any{"stuckClearCount": 1},
		"$set":   map[string]any{"lastStuckClearAt": d.Now(), "lastStuckDocPaths": stuckDocPaths},
		"$unset": map[string]any{"resyncPendingSince": 1},
	}, false)
}

// clearResyncStateIfAllAfter — vendor.
func (d *Deps) ClearResyncStateIfAllAfter(ctx context.Context, projectID string, date time.Time) error {
	d = d.withDefaults()
	raw, found, err := d.FindOneState(ctx, projectID)
	if err != nil {
		return err
	}
	if !found || raw == nil {
		return nil // already cleared
	}
	state := FromRaw(projectID, raw)
	if state.IsSyncOngoing() {
		return nil // new sync started
	}
	for _, entry := range state.History {
		if ts, ok := entry["timestamp"].(time.Time); ok && ts.Before(date) {
			return nil // preserve old resync states
		}
	}
	return d.DeleteStateDoc(ctx, projectID, map[string]any{
		"project_id": projectID,
		"expiresAt":  raw["expiresAt"],
	})
}

// --- startResync / startHardResync / startResyncWithoutLock (vendor) --------

func (d *Deps) StartResync(ctx context.Context, projectID string, options map[string]any) error {
	d = d.withDefaults()
	d.Inc("project_history_resync", 1, nil)
	var runErr error
	d.RunWithLock(lockKey(projectID),
		func(extend func() error, release func(error, ...any) error) {
			runErr = d.StartResyncWithoutLock(ctx, projectID, options)
			release(runErr)
		},
		func(err error, args ...any) {
			if runErr == nil {
				runErr = err
			}
		})
	if runErr != nil {
		if d.RecordError != nil {
			_ = d.RecordError(ctx, projectID, -1, runErr)
		}
		return runErr
	}
	return nil
}

func (d *Deps) StartHardResync(ctx context.Context, projectID string, options map[string]any) error {
	d = d.withDefaults()
	d.Inc("project_history_hard_resync", 1, nil)
	if options == nil {
		options = map[string]any{}
	}
	merged := map[string]any{}
	for k, v := range options {
		merged[k] = v
	}
	merged["hard"] = true
	var runErr error
	d.RunWithLock(lockKey(projectID),
		func(extend func() error, release func(error, ...any) error) {
			if e := d.ClearResyncState(ctx, projectID); e != nil {
				runErr = e
				release(e)
				return
			}
			if d.ClearFirstOpTimestamp != nil {
				if e := d.ClearFirstOpTimestamp(ctx, projectID); e != nil {
					runErr = e
					release(e)
					return
				}
			}
			if d.DestroyDocUpdatesQueue != nil {
				if e := d.DestroyDocUpdatesQueue(ctx, projectID); e != nil {
					runErr = e
					release(e)
					return
				}
			}
			runErr = d.StartResyncWithoutLock(ctx, projectID, merged)
			release(runErr)
		},
		func(err error, args ...any) {
			if runErr == nil {
				runErr = err
			}
		})
	if runErr != nil {
		if d.RecordError != nil {
			_ = d.RecordError(ctx, projectID, -1, runErr)
		}
		return runErr
	}
	return nil
}

// StartResyncWithoutLock — vendor (the stuck/perma-stuck/ongoing branches and
// the non-ongoing setup, verbatim order).
func (d *Deps) StartResyncWithoutLock(ctx context.Context, projectID string, options map[string]any) error {
	d = d.withDefaults()
	if options == nil {
		options = map[string]any{}
	}
	if d.RecordSyncStart != nil {
		if err := d.RecordSyncStart(ctx, projectID); err != nil {
			return err
		}
	}
	syncState, err := d.GetResyncState(ctx, projectID)
	if err != nil {
		return err
	}
	if syncState.IsSyncOngoing() {
		stuckDocPaths := append([]string{}, syncState.ResyncDocContents...)
		stuckClearCount := syncState.StuckClearCount
		if syncState.IsSyncStuck(d.Now()) {
			if syncState.IsSyncPermanentlyStuck() {
				if e := d.recordStuckClearInSyncState(ctx, projectID, stuckDocPaths); e != nil {
					return e
				}
				d.Inc("project_history_sync_stuck_permanent", 1, nil)
				if stuckClearCount == MaxStuckClearAttempts {
					d.LogErr(map[string]any{
						"projectId":          projectID,
						"stuckClearCount":    stuckClearCount + 1,
						"stuckDocPaths":      stuckDocPaths,
						"resyncPendingSince": pendingSinceValue(syncState),
					}, "sync permanently stuck — exceeded auto-clear limit")
				}
				return fmt.Errorf("sync permanently stuck")
			}
			d.LogWarn(map[string]any{
				"projectId":          projectID,
				"stuckClearCount":    stuckClearCount + 1,
				"stuckDocPaths":      stuckDocPaths,
				"resyncPendingSince": pendingSinceValue(syncState),
			}, "sync stuck, clearing state and restarting")
			d.Inc("project_history_sync_stuck_cleared", 1, nil)
			if e := d.recordStuckClearInSyncState(ctx, projectID, stuckDocPaths); e != nil {
				return e
			}
		} else {
			return &SyncOngoingError{
				Msg: SyncOngoingErrorMessage,
				Info: map[string]any{
					"projectId":          projectID,
					"stuckClearCount":    stuckClearCount + 1,
					"stuckDocPaths":      stuckDocPaths,
					"resyncPendingSince": pendingSinceValue(syncState),
				},
			}
		}
	}
	if origin, ok := options["origin"].(map[string]any); ok && origin != nil {
		syncState.SetOrigin(origin)
	} else {
		syncState.SetOrigin(map[string]any{"kind": "history-resync"})
	}
	syncState.StartProjectStructureSync()
	syncState.HardResync = options["hard"] == true
	syncState.RecoverCorruptedFiles = options["recoverCorruptedFiles"] == true

	webOpts := map[string]any{}
	if truthy(options["historyRangesMigration"]) {
		webOpts["historyRangesMigration"] = options["historyRangesMigration"]
	}
	if truthy(options["resyncProjectStructureOnly"]) {
		webOpts["resyncProjectStructureOnly"] = options["resyncProjectStructureOnly"]
	}
	if err := d.RequestResync(ctx, projectID, webOpts); err != nil {
		return err
	}
	return d.SetResyncState(ctx, projectID, syncState)
}

func pendingSinceValue(s *SyncState) any {
	if s.ResyncPendingSince == nil {
		return nil
	}
	return *s.ResyncPendingSince
}

// --- skipUpdatesDuringSync (vendor) ------------------------------------------

// SkipUpdatesDuringSync — vendor.
func (d *Deps) SkipUpdatesDuringSync(ctx context.Context, projectID string, updates []map[string]any) ([]map[string]any, *SyncState, error) {
	d = d.withDefaults()
	syncState, err := d.GetResyncState(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	if !syncState.IsSyncOngoing() {
		d.LogDebug(map[string]any{"projectId": projectID}, "not skipping updates: no resync in progress")
		return updates, nil, nil
	}
	filtered := []map[string]any{}
	for _, update := range updates {
		if err := syncState.UpdateState(update); err != nil {
			return nil, nil, err
		}
		if syncState.ShouldSkipUpdate(update) {
			d.Inc("project_history_sync_update_skipped", 1, nil)
			d.LogDebug(map[string]any{"projectId": projectID, "update": update}, "skipping update due to resync")
			continue
		}
		filtered = append(filtered, update)
	}
	return filtered, syncState, nil
}

// --- expandSyncUpdates (vendor) ----------------------------------------------

// ExpandSyncUpdates — vendor (no-sync-updates fast path, snapshot, validity,
// per-update expansion + extendLock).
func (d *Deps) ExpandSyncUpdates(ctx context.Context, projectID, projectHistoryID string, mostRecentChunk map[string]any, updates []map[string]any, extendLock func() error) ([]map[string]any, error) {
	d = d.withDefaults()
	areSyncUpdatesQueued := false
	for _, u := range updates {
		if v, has := u["resyncProjectStructure"]; has && v != nil {
			areSyncUpdatesQueued = true
			break
		}
		if v, has := u["resyncDocContent"]; has && v != nil {
			areSyncUpdatesQueued = true
			break
		}
	}
	if !areSyncUpdatesQueued {
		d.LogDebug(map[string]any{"projectId": projectID}, "no resync updates to expand")
		return updates, nil
	}

	syncState, err := d.GetResyncState(ctx, projectID)
	if err != nil {
		return nil, err
	}

	var snapshotFiles map[string]*File
	if d.GetLatestSnapshotFilesForChunk != nil {
		snapshotFiles, err = d.GetLatestSnapshotFilesForChunk(ctx, projectHistoryID, mostRecentChunk)
		if err != nil {
			return nil, err
		}
	} else {
		snapshotFiles = map[string]*File{}
	}

	// vendor: check every file is editable-capable (has the method) — in the
	// port a nil entry or non-struct is the invalid case.
	invalid := map[string]any{}
	for k, v := range snapshotFiles {
		if v == nil {
			invalid[k] = v
		}
	}
	if len(invalid) > 0 {
		return nil, &SyncError{Msg: "file is missing isEditable method", Info: map[string]any{
			"projectId":    projectID,
			"invalidFiles": invalid,
		}}
	}

	e := &expander{
		d:                     d,
		projectID:             projectID,
		files:                 snapshotFiles,
		expandedUpdates:       []map[string]any{},
		origin:                syncState.Origin,
		hardResync:            syncState.HardResync,
		recoverCorruptedFiles: syncState.RecoverCorruptedFiles,
	}
	for _, update := range updates {
		if err := e.expandUpdate(ctx, update); err != nil {
			return nil, err
		}
		if extendLock != nil {
			if err := extendLock(); err != nil {
				return nil, err
			}
		}
	}
	return e.expandedUpdates, nil
}
