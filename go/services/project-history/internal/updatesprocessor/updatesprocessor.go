// Package updatesprocessor is the 1:1 port of
// services/project-history/app/js/UpdatesProcessor.js (933 L) — the flush
// pipeline (redis queue → history-v1) and its lock/error/recording flow.
//
// Faithful semantics:
//
//	U1 REDIS_READ_BATCH_SIZE=500.
//	U2 getRawUpdates: getRawUpdatesBatch → parseDocUpdates (parse error
//	   tagged) → _getHistoryId → getMostRecentChunk → {project_id, chunk,
//	   updates}.
//	U3 _getHistoryId — SIX branches with EXACT metric names:
//	   'updates.batches.project-history-id.inconsistent-update' (mixed ids in
//	   updates → OError 'inconsistent project history id between updates'
//	   {projectId, idFromUpdates, currentId});
//	   web error + idFromUpdates → '…from-updates' (return idFromUpdates);
//	   web error + no updates id → OError.tag(error);
//	   neither → null; web only → '…from-web'; updates only → '…from-updates';
//	   MISMATCH web/updates → '…inconsistent-with-web' + warn-log + OError
//	   'inconsistent project history id between updates and web'; same →
//	   '…from-updates' (return idFromWeb).
//	U4 processUpdatesForProject = _processUpdatesForProjectWithLock
//	   {checkResyncState:true}; flushResyncUpdates = {checkResyncState:false}.
//	U5 lock callback flow (BOTH entry points share the done-callback shape):
//	   flushError → OError.tag (decorative) + ErrorRecorder.record(pid,
//	   queueSize, flushError, (recordError, failure) => …): recordError →
//	   log 'failed to record error' + callback(recordError); else
//	   isFirstFailure(failure) && isHardFailure(failure) → warn 'Flush
//	   failed, attempting resync' + resyncProject; else callback(flushError).
//	   NO flushError → ErrorRecorder.clearError → clearError logged 'failed
//	   to clear error' (non-fatal); then resyncNeeded → warn 'Resyncing
//	   project as requested by full project history' + resyncProject; else
//	   callback().
//	U6 done-callback tail (queueSize > 0): historyFlushDurationSeconds
//	   (now-start)/1000 + historyFlushQueueSize observes; then
//	   clearDanglingFirstOpTimestamp (fire & forget).
//	U7 countAndProcessUpdates: count → 0 → {queueSize:0, resyncNeeded:false};
//	   else getUpdatesInBatches → per batch _processUpdatesBatch (resyncNeeded
//	   OR-ed across batches); the callback is ALWAYS called exactly once with
//	   (error?, {queueSize, resyncNeeded}) even on batch error (the caller
//	   needs the queueSize for recording).
//	U8 processUpdatesForProjectUsingBisect: amount 0/queueSize 0 → terminal
//	   (error → record w/ tagged error + cb(flushError); else cb()); else
//	   recurse with floor(amount/2) on error, same amount on success.
//	U9 processSingleUpdateForProject: batch size 1; same record/clear tail;
//	   NO clearDanglingFirstOpTimestamp and NO metrics (single-stepping).
//	U10 startResyncAndProcessUpdatesUnderLock: startResyncWithoutLock under
//	   the lock → extendLock → countAndProcess(500); done-callback: error →
//	   record → cb(flushError); else clearError → cb(); + U6 tail.
//	U11 resyncProject: startHardResync → error tagged & rethrown; else under
//	   lock countAndProcess(500) → error → record → cb(recordError || tagged
//	   flushError); else clearError → cb().
//	U12 _processUpdates pipeline (waterfall order):
//	   skipUpdatesDuringSync → filtered empty → setResyncState(newSyncState)
//	   → {resyncNeeded:false} EARLY;
//	   → _getMostRecentVersionWithDebug (5 results; OpsOutOfOrder →
//	   forceDebug bypass via failure record) → psdv null → {project:null,
//	   docs:{}};
//	   → expandSyncUpdates → _skipAlreadyAppliedUpdates →
//	   compressRawUpdates → createBlobsForUpdates → convertToChanges (toRaw;
//	   convert errors propagate) → metrics (histogram
//	   'history-store.request.changes' = numChanges;
//	   'history-store.request.bytes' = byteLength(JSON(changes));
//	   'history-store.request.operations' = op count; thresholds:
//	   numChanges > 1000 → 'history-store.request.exceeds-threshold.changes';
//	   byteLength > 1MiB → '…exceeds-threshold.bytes' + warn with per-change
//	   lengths) → extendLock → sendChanges ONLY when changes.length > 0
//	   (resyncNeeded from the response) → setResyncState.
//	U13 _skipAlreadyAppliedUpdates: two passes over the updates:
//	   1) ORDER check against the INCOMING stream: isProjectStructureUpdate
//	   with version <= incomingSoFar → OpsOutOfOrderError 'project structure
//	   version out of order on incoming updates' (warn-logged); isTextUpdate
//	   with v <= incoming[doc].v → 'doc version out of order on incoming
//	   updates'. Version tracking updates per accepted order.
//	   2) DISCARD check against the persisted psdv: project version gte
//	   previous → metric 'updates.discarded_project_structure_version' + drop;
//	   doc v gte previous[doc].v → 'updates.discarded_doc_version' + drop;
//	   else _sanitizeUpdate + keep.
//	U14 _sanitizeUpdate: replace JS surrogate chars [\u{D800}-\u{DFFF}] in
//	   update.op[].i and update.docLines with U+FFFD (vendor: high+low
//	   surrogate replacement).
//	U15 isTextUpdate — doc/op non-null (vendor UpdateTranslator.isTextUpdate);
//	    isProjectStructureUpdate — version non-null (vendor shape).
package updatesprocessor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	pherr "ollitex/go/services/project-history/internal/errors"
)

const RedisReadBatchSize = 500

// SyncState — the C16 SyncState surface C17 needs (the adapter fills it from
// internal/syncmanager when that port lands).
type SyncState struct {
	Ongoing             bool
	StuckClearCount     int
	ResyncDocContents   []string
	ResyncPendingSince  string
	ResyncProjectStruct bool
	Raw                 any
}

// Sync — the SyncManager seam (C16).
type Sync interface {
	SkipUpdatesDuringSync(ctx context.Context, projectID string, updates []map[string]any) ([]map[string]any, *SyncState, error)
	SetResyncState(ctx context.Context, projectID string, state *SyncState) error
	ExpandSyncUpdates(ctx context.Context, projectID, historyID string, mostRecentChunk map[string]any, updates []map[string]any) ([]map[string]any, error)
	GetResyncState(ctx context.Context, projectID string) (*SyncState, error)
	StartResyncWithoutLock(ctx context.Context, projectID string, opts map[string]any) error
	StartHardResync(ctx context.Context, projectID string, opts map[string]any) error
}

// Deps — the vendor imports as seams.
type Deps struct {
	Sync Sync
	// RedisManager.
	CountUnprocessedUpdates       func(ctx context.Context, projectID string) (int, error)
	GetRawUpdatesBatch            func(ctx context.Context, projectID string, batchSize int) ([]string, error)
	ParseDocUpdates               func(jsonUpdates []string) ([]map[string]any, error)
	GetUpdatesInBatches           func(ctx context.Context, projectID string, batchSize int, runner func([]map[string]any) error) error
	ClearDanglingFirstOpTimestamp func(ctx context.Context, projectID string) error
	// LockManager.
	RunWithLock func(ctx context.Context, key string, runner func(extend func() error) error) error
	// ErrorRecorder (B12 shape: record returns the failure record).
	Record           func(ctx context.Context, projectID string, queueSize int, err error) (map[string]any, error)
	ClearError       func(ctx context.Context, projectID string) error
	GetFailureRecord func(ctx context.Context, projectID string) (map[string]any, error)
	// WebApiManager.
	GetHistoryID func(ctx context.Context, projectID string) (string, error)
	// HistoryStoreManager (C2).
	GetMostRecentChunk   func(ctx context.Context, projectID, historyID string) (map[string]any, error)
	GetMostRecentVersion func(ctx context.Context, projectID, historyID string) (version int, psdv map[string]any, lastChange map[string]any, chunk map[string]any, err error)
	SendChanges          func(ctx context.Context, projectID, historyID string, changes []any, baseVersion int) (resyncNeeded bool, err error)
	// CreateBlobs — C3 (extendLock is passed by the vendor; the port folds
	// it in — the runner already extends before calling).
	CreateBlobsForUpdates func(ctx context.Context, projectID, historyID string, updates []map[string]any) ([]map[string]any, error)
	// Compress — B8 compressRawUpdates (metric-aware wrapper is the D-phase
	// concern; the port uses the plain port).
	CompressRawUpdates func(rawUpdates []map[string]any) ([]map[string]any, error)
	// ConvertToChanges — B5 (returns raw-ish change maps with `operations`).
	ConvertToChanges func(projectID string, updates []map[string]any) ([]map[string]any, error)
	// Metrics.
	Inc     func(name string)
	Timing  func(name string, summary int, count int)
	Observe func(name string, value float64)
	// Log (the warn lines above are kept at their vendor messages).
	LogWarn func(info map[string]any, msg string)
	LogErr  func(info map[string]any, msg string)
	// Now — the clock (flush duration metrics).
	Now func() int64 // epoch ms
}

func (d *Deps) withDefaults() *Deps {
	if d == nil {
		d = &Deps{}
	}
	if d.Inc == nil {
		d.Inc = func(string) {}
	}
	if d.Timing == nil {
		d.Timing = func(string, int, int) {}
	}
	if d.Observe == nil {
		d.Observe = func(string, float64) {}
	}
	if d.LogWarn == nil {
		d.LogWarn = func(map[string]any, string) {}
	}
	if d.LogErr == nil {
		d.LogErr = func(map[string]any, string) {}
	}
	if d.Now == nil {
		d.Now = func() int64 { return 0 }
	}
	return d
}

// --- U2: getRawUpdates ------------------------------------------------------

// GetRawUpdates — vendor getRawUpdates (U2).
func (d *Deps) GetRawUpdates(ctx context.Context, projectID string, batchSize int) (map[string]any, error) {
	d = d.withDefaults()
	rawBatch, err := d.GetRawUpdatesBatch(ctx, projectID, batchSize)
	if err != nil {
		return nil, err
	}
	updates, err := d.ParseDocUpdates(rawBatch)
	if err != nil {
		return nil, err
	}
	historyID, err := d.getHistoryID(ctx, projectID, updates)
	if err != nil {
		return nil, err
	}
	chunk, err := d.GetMostRecentChunk(ctx, projectID, historyID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"project_id": projectID, "chunk": chunk, "updates": updates}, nil
}

// --- U3: _getHistoryId ------------------------------------------------------

// getHistoryID — vendor _getHistoryId (U3, exact metric names).
func (d *Deps) getHistoryID(ctx context.Context, projectID string, updates []map[string]any) (string, error) {
	d = d.withDefaults()
	var idFromUpdates *string
	for _, update := range updates {
		if v, ok := update["projectHistoryId"]; ok && v != nil {
			s := fmt.Sprintf("%v", v)
			if idFromUpdates == nil {
				idFromUpdates = &s
			} else if *idFromUpdates != s {
				d.Inc("updates.batches.project-history-id.inconsistent-update")
				return "", &taggedErr{
					msg: "inconsistent project history id between updates",
					info: map[string]any{
						"projectId":     projectID,
						"idFromUpdates": *idFromUpdates,
						"currentId":     v,
					},
				}
			}
		}
	}
	idFromWeb, webErr := d.GetHistoryID(ctx, projectID)
	switch {
	case webErr != nil && idFromUpdates != nil:
		d.Inc("updates.batches.project-history-id.from-updates")
		return *idFromUpdates, nil
	case webErr != nil:
		return "", webErr
	case idFromWeb == "" && idFromUpdates == nil:
		return "", nil
	case idFromWeb != "" && idFromUpdates == nil:
		d.Inc("updates.batches.project-history-id.from-web")
		return idFromWeb, nil
	case idFromWeb == "" && idFromUpdates != nil:
		d.Inc("updates.batches.project-history-id.from-updates")
		return *idFromUpdates, nil
	case idFromWeb != *idFromUpdates:
		d.Inc("updates.batches.project-history-id.inconsistent-with-web")
		d.LogWarn(map[string]any{
			"projectId":     projectID,
			"idFromWeb":     idFromWeb,
			"idFromUpdates": *idFromUpdates,
			"updates":       updates,
		}, "inconsistent project history id between updates and web")
		return "", &taggedErr{msg: "inconsistent project history id between updates and web"}
	default:
		d.Inc("updates.batches.project-history-id.from-updates")
		return idFromWeb, nil
	}
}

// taggedErr — OError(msg, info) / OError.tag(err) parity.
type taggedErr struct {
	msg  string
	info map[string]any
}

func (e *taggedErr) Error() string {
	if e.msg != "" {
		return e.msg
	}
	return "error"
}
func (e *taggedErr) Info() map[string]any {
	if e.info == nil {
		return map[string]any{}
	}
	return e.info
}

// --- U4–U7: process entry points -------------------------------------------

// ProcessUpdatesForProject — vendor processUpdatesForProject (U4).
func (d *Deps) ProcessUpdatesForProject(ctx context.Context, projectID string) error {
	return d.processForProjectWithLock(ctx, projectID, true)
}

// FlushResyncUpdates — vendor flushResyncUpdates (U4).
func (d *Deps) FlushResyncUpdates(ctx context.Context, projectID string) error {
	return d.processForProjectWithLock(ctx, projectID, false)
}

// processForProjectWithLock — vendor _processUpdatesForProjectWithLock (U5).
func (d *Deps) processForProjectWithLock(ctx context.Context, projectID string, checkResyncState bool) error {
	d = d.withDefaults()
	startTime := d.Now()
	var queueSize int
	var resyncNeeded bool
	runErr := error(nil)
	err := d.RunWithLock(ctx, "projectHistoryLock:"+projectID, func(extend func() error) error {
		if checkResyncState {
			e, qs, rn := d.flushAndCheckResyncState(ctx, projectID, extend, RedisReadBatchSize)
			runErr, queueSize, resyncNeeded = e, qs, rn
		} else {
			e, qs, rn := d.countAndProcess(ctx, projectID, extend, RedisReadBatchSize)
			runErr, queueSize, resyncNeeded = e, qs, rn
		}
		return nil // the vendor's inner error flows to the done-callback, not the lock
	})
	if err != nil && runErr == nil {
		runErr = err // lock acquisition failure (vendor: runWithLock rejects)
	}

	// done-callback (U5)
	if runErr != nil {
		failure, recordErr := d.Record(ctx, projectID, queueSize, runErr)
		if recordErr != nil {
			d.LogErr(map[string]any{"projectId": projectID}, "failed to record error")
			return recordErr
		}
		if d.isFirstFailure(failure) && d.isHardFailure(failure) {
			d.LogWarn(map[string]any{"projectId": projectID}, "Flush failed, attempting resync")
			return d.ResyncProject(ctx, projectID)
		}
		return runErr
	}
	if clearErr := d.ClearError(ctx, projectID); clearErr != nil {
		d.LogErr(map[string]any{"projectId": projectID}, "failed to clear error")
	}
	if resyncNeeded {
		d.LogWarn(map[string]any{"projectId": projectID}, "Resyncing project as requested by full project history")
		return d.ResyncProject(ctx, projectID)
	}
	// U6 tail
	if queueSize > 0 {
		d.Observe("historyFlushDurationSeconds", float64(d.Now()-startTime)/1000)
		d.Observe("historyFlushQueueSize", float64(queueSize))
	}
	if d.ClearDanglingFirstOpTimestamp != nil {
		_ = d.ClearDanglingFirstOpTimestamp(ctx, projectID)
	}
	return nil
}

// flushAndCheckResyncState — vendor _flushAndCheckResyncState (U7 + the
// resync-ongoing assertion).
func (d *Deps) flushAndCheckResyncState(ctx context.Context, projectID string, extend func() error, batchSize int) (error, int, bool) {
	runErr, queueSize, resyncNeeded := d.countAndProcess(ctx, projectID, extend, batchSize)
	if runErr != nil {
		return runErr, queueSize, resyncNeeded
	}
	if extend != nil {
		if e := extend(); e != nil {
			return e, queueSize, resyncNeeded
		}
	}
	state, err := d.Sync.GetResyncState(ctx, projectID)
	if err != nil {
		return err, queueSize, resyncNeeded
	}
	if state.Ongoing {
		e := &pherr.Error{}
		_ = e
		return &syncOngoingErr{
			msg: "sync ongoing",
			info: map[string]any{
				"projectId":              projectID,
				"stuckClearCount":        state.StuckClearCount,
				"stuckDocPaths":          state.ResyncDocContents,
				"resyncPendingSince":     state.ResyncPendingSince,
				"resyncProjectStructure": state.ResyncProjectStruct,
			},
		}, queueSize, resyncNeeded
	}
	return nil, queueSize, resyncNeeded
}

type syncOngoingErr struct {
	msg  string
	info map[string]any
}

func (e *syncOngoingErr) Error() string { return e.msg }
func (e *syncOngoingErr) Info() map[string]any {
	if e.info == nil {
		return map[string]any{}
	}
	return e.info
}

// countAndProcess — vendor _countAndProcessUpdates (U7).
func (d *Deps) countAndProcess(ctx context.Context, projectID string, extend func() error, batchSize int) (error, int, bool) {
	d = d.withDefaults()
	queueSize, err := d.CountUnprocessedUpdates(ctx, projectID)
	if err != nil {
		return err, 0, false
	}
	if queueSize == 0 {
		return nil, 0, false
	}
	resyncNeeded := false
	var batchErr error
	batchErr = d.GetUpdatesInBatches(ctx, projectID, batchSize, func(updates []map[string]any) error {
		response, err := d.processUpdatesBatch(ctx, projectID, updates, extend)
		if err != nil {
			return err
		}
		if response != nil {
			if rn, ok := response["resyncNeeded"].(bool); ok && rn {
				resyncNeeded = true
			}
		}
		return nil
	})
	// the CONVENTION (vendor): the caller needs the queueSize on error too —
	// the port returns (error, queueSize, resyncNeeded) in one tuple, which
	// is exactly the vendor's `callback(error, {queueSize, resyncNeeded})`.
	return batchErr, queueSize, resyncNeeded
}

// processUpdatesBatch — vendor _processUpdatesBatch.
func (d *Deps) processUpdatesBatch(ctx context.Context, projectID string, updates []map[string]any, extend func() error) (map[string]any, error) {
	historyID, err := d.getHistoryID(ctx, projectID, updates)
	if err != nil {
		return nil, err
	}
	if historyID == "" {
		// vendor: discard, success with {} response
		return map[string]any{}, nil
	}
	response, err := d.processUpdates(ctx, projectID, historyID, updates, extend)
	if err != nil {
		return nil, err
	}
	return response, nil
}

// --- U8/U9: bisect + single --------------------------------------------------

// ProcessUpdatesForProjectUsingBisect — vendor (U8).
func (d *Deps) ProcessUpdatesForProjectUsingBisect(ctx context.Context, projectID string, amountToProcess int) error {
	d = d.withDefaults()
	var runErr error
	var queueSize int
	_ = d.RunWithLock(ctx, "projectHistoryLock:"+projectID, func(extend func() error) error {
		runErr, queueSize, _ = d.countAndProcess(ctx, projectID, extend, amountToProcess)
		return nil
	})
	if runErr != nil {
		if amountToProcess == 0 || queueSize == 0 {
			// terminal: record + callback(flushError)
			if _, recordErr := d.Record(ctx, projectID, queueSize, runErr); recordErr != nil {
				d.LogErr(map[string]any{"projectId": projectID}, "failed to record error")
			}
			return runErr
		}
		return d.ProcessUpdatesForProjectUsingBisect(ctx, projectID, amountToProcess/2)
	}
	if amountToProcess == 0 || queueSize == 0 {
		return nil
	}
	return d.ProcessUpdatesForProjectUsingBisect(ctx, projectID, amountToProcess)
}

// ProcessSingleUpdateForProject — vendor (U9).
func (d *Deps) ProcessSingleUpdateForProject(ctx context.Context, projectID string) error {
	d = d.withDefaults()
	var runErr error
	var queueSize int
	_ = d.RunWithLock(ctx, "projectHistoryLock:"+projectID, func(extend func() error) error {
		runErr, queueSize, _ = d.countAndProcess(ctx, projectID, extend, 1)
		return nil
	})
	if runErr != nil {
		if _, recordErr := d.Record(ctx, projectID, queueSize, runErr); recordErr != nil {
			d.LogErr(map[string]any{"projectId": projectID}, "failed to record error")
		}
		return runErr
	}
	if clearErr := d.ClearError(ctx, projectID); clearErr != nil {
		d.LogErr(map[string]any{"projectId": projectID}, "failed to clear error")
	}
	return nil
}

// --- U10/U11 ---------------------------------------------------------------

// StartResyncAndProcessUpdatesUnderLock — vendor (U10).
func (d *Deps) StartResyncAndProcessUpdatesUnderLock(ctx context.Context, projectID string, opts map[string]any) error {
	d = d.withDefaults()
	startTime := d.Now()
	var queueSize int
	var runErr error
	err := d.RunWithLock(ctx, "projectHistoryLock:"+projectID, func(extend func() error) error {
		if e := d.Sync.StartResyncWithoutLock(ctx, projectID, opts); e != nil {
			runErr = e
			return nil
		}
		if e := extend(); e != nil {
			runErr = e
			return nil
		}
		runErr, queueSize, _ = d.countAndProcess(ctx, projectID, extend, RedisReadBatchSize)
		return nil
	})
	if err != nil && runErr == nil {
		runErr = err
	}
	if runErr != nil {
		if _, recordErr := d.Record(ctx, projectID, queueSize, runErr); recordErr != nil {
			d.LogErr(map[string]any{"projectId": projectID}, "failed to record error")
		}
		return runErr
	}
	if clearErr := d.ClearError(ctx, projectID); clearErr != nil {
		d.LogErr(map[string]any{"projectId": projectID}, "failed to clear error")
	}
	if queueSize > 0 {
		d.Observe("historyFlushDurationSeconds", float64(d.Now()-startTime)/1000)
		d.Observe("historyFlushQueueSize", float64(queueSize))
	}
	if d.ClearDanglingFirstOpTimestamp != nil {
		_ = d.ClearDanglingFirstOpTimestamp(ctx, projectID)
	}
	return nil
}

// ResyncProject — vendor resyncProject (U11).
func (d *Deps) ResyncProject(ctx context.Context, projectID string) error {
	d = d.withDefaults()
	if e := d.Sync.StartHardResync(ctx, projectID, map[string]any{}); e != nil {
		return &taggedErr{msg: e.Error()}
	}
	var queueSize int
	var runErr error
	lockErr := d.RunWithLock(ctx, "projectHistoryLock:"+projectID, func(extend func() error) error {
		runErr, queueSize, _ = d.countAndProcess(ctx, projectID, extend, RedisReadBatchSize)
		return nil
	})
	if lockErr != nil && runErr == nil {
		runErr = lockErr
	}
	if runErr != nil {
		if _, recordErr := d.Record(ctx, projectID, queueSize, runErr); recordErr != nil {
			d.LogErr(map[string]any{"projectId": projectID}, "failed to record error")
			return recordErr
		}
		return &taggedErr{msg: runErr.Error()}
	}
	if clearErr := d.ClearError(ctx, projectID); clearErr != nil {
		d.LogErr(map[string]any{"projectId": projectID}, "failed to clear error")
	}
	return nil
}

// --- isFirstFailure/isHardFailure (RetryManager predicates, C8) -------------

// The vendor imports RetryManager.isFirstFailure/isHardFailure. The port
// takes them as seams with the C8 semantics (attempts <= 1 / hard-list) so
// C17 does not import C8's concrete types.
// Vendor RetryManager.isFirstFailure: failure.attempts <= 1 (JS undefined →
// false, i.e. the field must be present and numeric).
func (d *Deps) isFirstFailure(failure map[string]any) bool {
	a, ok := toInt(failure["attempts"])
	return ok && a <= 1
}

// Vendor RetryManager.isHardFailure: the error message is in the hard list.
func (d *Deps) isHardFailure(failure map[string]any) bool {
	var errMsg string
	switch e := failure["error"].(type) {
	case string:
		errMsg = e
	case map[string]any:
		errMsg, _ = e["message"].(string)
	}
	for _, s := range hardFailureList {
		if errMsg == s {
			return true
		}
	}
	return false
}

var hardFailureList = []string{
	"Error: history store a non-success status code: 422",
	"OpsOutOfOrderError: project structure version out of order",
	"OpsOutOfOrderError: project structure version out of order on incoming updates",
	"OpsOutOfOrderError: doc version out of order",
	"OpsOutOfOrderError: doc version out of order on incoming updates",
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

// --- U12: _processUpdates -----------------------------------------------------

// processUpdates — vendor _processUpdates (U12).
func (d *Deps) processUpdates(ctx context.Context, projectID, historyID string, updates []map[string]any, extend func() error) (map[string]any, error) {
	d = d.withDefaults()
	// 1. skip during sync
	filtered, newState, err := d.Sync.SkipUpdatesDuringSync(ctx, projectID, updates)
	if err != nil {
		return nil, err
	}
	if len(filtered) == 0 {
		e := d.Sync.SetResyncState(ctx, projectID, newState)
		if e != nil {
			return nil, e
		}
		return map[string]any{"resyncNeeded": false}, nil
	}
	// 2. most recent version (+ debug bypass)
	version, psdv, lastChange, chunk, e2 := d.getMostRecentVersionWithDebug(ctx, projectID, historyID)
	if e2 != nil {
		return nil, e2
	}
	_ = lastChange
	if psdv == nil {
		psdv = map[string]any{"project": nil, "docs": map[string]any{}}
	}
	// 3. expand sync updates
	expanded, e3 := d.Sync.ExpandSyncUpdates(ctx, projectID, historyID, chunk, filtered)
	if e3 != nil {
		return nil, e3
	}
	// 4. skip already applied
	unapplied, e4 := d.skipAlreadyAppliedUpdates(projectID, expanded, psdv)
	if e4 != nil {
		return nil, e4
	}
	// 5. compress
	compressed, e5 := d.CompressRawUpdates(unapplied)
	if e5 != nil {
		return nil, e5
	}
	// 6. blobs
	updatesWithBlobs, e6 := d.CreateBlobsForUpdates(ctx, projectID, historyID, compressed)
	if e6 != nil {
		return nil, e6
	}
	// 7. convert to changes (toRaw)
	changes, e7 := d.ConvertToChanges(projectID, updatesWithBlobs)
	if e7 != nil {
		return nil, e7
	}
	// 8. metrics
	numChanges := len(changes)
	b, _ := json.Marshal(changes)
	byteLength := len(b)
	numOperations := 0
	for _, change := range changes {
		if ops, ok := change["operations"]; ok && ops != nil {
			if l, ok := ops.([]any); ok {
				numOperations += len(l)
			}
		}
	}
	d.Timing("history-store.request.changes", numChanges, 1)
	d.Timing("history-store.request.bytes", byteLength, 1)
	d.Timing("history-store.request.operations", numOperations, 1)
	if numChanges > 1000 {
		d.Inc("history-store.request.exceeds-threshold.changes")
	}
	if byteLength > 1024*1024 {
		d.Inc("history-store.request.exceeds-threshold.bytes")
		lengths := make([]int, 0, len(changes))
		for _, change := range changes {
			c, _ := json.Marshal(change)
			lengths = append(lengths, len(c))
		}
		d.LogWarn(map[string]any{"projectId": projectID, "byteLength": byteLength, "changeLengths": lengths}, "change size exceeds limit")
	}
	// 9. send (extend lock first; skip empty)
	resyncNeeded := false
	if extend != nil {
		if e := extend(); e != nil {
			return nil, e
		}
	}
	if numChanges > 0 {
		rn, e9 := d.SendChanges(ctx, projectID, historyID, toAnyChanges(changes), version)
		if e9 != nil {
			return nil, e9
		}
		resyncNeeded = rn
	}
	// 10. set resync state
	if e10 := d.Sync.SetResyncState(ctx, projectID, newState); e10 != nil {
		return nil, e10
	}
	return map[string]any{"resyncNeeded": resyncNeeded}, nil
}

func toAnyChanges(changes []map[string]any) []any {
	out := make([]any, 0, len(changes))
	for _, c := range changes {
		out = append(out, c)
	}
	return out
}

// getMostRecentVersionWithDebug — vendor _getMostRecentVersionWithDebug +
// _handleOpsOutOfOrderError (U12 step 2).
func (d *Deps) getMostRecentVersionWithDebug(ctx context.Context, projectID, historyID string) (int, map[string]any, map[string]any, map[string]any, error) {
	d = d.withDefaults()
	version, psdv, lastChange, chunk, err := d.GetMostRecentVersion(ctx, projectID, historyID)
	if err != nil {
		if isOpsOutOfOrder(err) {
			if d.GetFailureRecord != nil {
				rec, rerr := d.GetFailureRecord(ctx, projectID)
				if rerr != nil {
					return 0, nil, nil, nil, rerr
				}
				if rec != nil && rec["forceDebug"] == true {
					d.LogWarn(map[string]any{"projectId": projectID, "projectHistoryId": historyID}, "ops out of order in chunk, forced continue")
					return version, psdv, lastChange, chunk, nil
				}
			}
		}
		return 0, nil, nil, nil, err
	}
	return version, psdv, lastChange, chunk, nil
}

func isOpsOutOfOrder(err error) bool {
	if ph, ok := err.(*pherr.Error); ok {
		return string(ph.Kind) == string(pherr.KindOpsOutOfOrder)
	}
	return strings.Contains(err.Error(), "out of order")
}

// --- U13/U14: skip already applied + sanitize --------------------------------

// isProjectStructureUpdate — vendor: version non-null.
func isProjectStructureUpdate(update map[string]any) bool {
	_, ok := update["version"]
	if !ok {
		return false
	}
	return update["version"] != nil
}

// isTextUpdate — vendor: doc/op non-null (U15).
func isTextUpdate(update map[string]any) bool {
	return (update["doc"] != nil || update["op"] != nil)
}

// versionGte — vendor Versions.gte on integer versions.
func versionGte(v1 any, v2 any) bool {
	n1, ok1 := toInt(v1)
	n2, ok2 := toInt(v2)
	if !ok1 || !ok2 {
		s1, _ := v1.(string)
		s2, _ := v2.(string)
		return s1 >= s2
	}
	return n1 >= n2
}

// skipAlreadyAppliedUpdates — vendor _skipAlreadyAppliedUpdates (U13).
func (d *Deps) skipAlreadyAppliedUpdates(projectID string, updates []map[string]any, psdv map[string]any) ([]map[string]any, error) {
	d = d.withDefaults()
	// pass 1: incoming order check
	var incomingProjectVersion *int
	incomingDocVersions := map[string]int{}
	for _, update := range updates {
		v, hasV := toInt(update["version"])
		if isProjectStructureUpdate(update) && hasV && update["version"] != nil {
			if incomingProjectVersion != nil && v <= *incomingProjectVersion {
				d.LogWarn(map[string]any{"projectId": projectID, "update": update, "incomingProjectStructureVersion": *incomingProjectVersion}, "incoming project structure updates are out of order")
				return nil, pherr.OpsOutOfOrder("project structure version out of order on incoming updates")
			}
		}
		if isTextUpdate(update) && update["v"] != nil {
			docID := fmt.Sprintf("%v", update["doc"])
			dv, _ := toInt(update["v"])
			if prev, has := incomingDocVersions[docID]; has && dv <= prev {
				d.LogWarn(map[string]any{"projectId": projectID, "update": update, "incomingDocVersions": incomingDocVersions}, "incoming doc updates are out of order")
				return nil, pherr.OpsOutOfOrder("doc version out of order on incoming updates")
			}
		}
		// track
		if isProjectStructureUpdate(update) {
			if v, ok := toInt(update["version"]); ok {
				incomingProjectVersion = &v
			}
		} else if isTextUpdate(update) {
			if v, ok := toInt(update["v"]); ok {
				incomingDocVersions[fmt.Sprintf("%v", update["doc"])] = v
			}
		}
	}
	// pass 2: discard already-applied + sanitize
	previousProjectVersion := psdv["project"]
	previousDocVersions, _ := psdv["docs"].(map[string]any)
	out := []map[string]any{}
	for _, update := range updates {
		if isProjectStructureUpdate(update) && update["version"] != nil && previousProjectVersion != nil {
			if pv, ok := toInt(previousProjectVersion); ok {
				if uv, ok2 := toInt(update["version"]); ok2 && pv >= uv {
					d.Inc("updates.discarded_project_structure_version")
					continue
				}
			}
		}
		if isTextUpdate(update) && update["v"] != nil && previousDocVersions != nil {
			docID := fmt.Sprintf("%v", update["doc"])
			if prevDoc, has := previousDocVersions[docID]; has {
				if pm, ok := prevDoc.(map[string]any); ok {
					if pv, ok2 := toInt(pm["v"]); ok2 {
						if uv, ok3 := toInt(update["v"]); ok3 && pv >= uv {
							d.Inc("updates.discarded_doc_version")
							continue
						}
					}
				}
			}
		}
		sanitizeUpdate(update)
		out = append(out, update)
	}
	return out, nil
}

// sanitizeUpdate — vendor _sanitizeUpdate (U14): JS surrogate replacement.
func sanitizeUpdate(update map[string]any) {
	removeBad := func(s string) string {
		var b strings.Builder
		for _, r := range s {
			if r >= 0xD800 && r <= 0xDFFF {
				b.WriteRune(0xFFFD)
			} else {
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	if ops, ok := update["op"].([]any); ok {
		for _, opAny := range ops {
			if op, ok := opAny.(map[string]any); ok {
				if s, ok := op["i"].(string); ok {
					op["i"] = removeBad(s)
				}
			}
		}
	}
	if s, ok := update["docLines"].(string); ok {
		update["docLines"] = removeBad(s)
	}
}
