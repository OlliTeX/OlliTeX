// Package httpcontroller is a 1:1 port of
// services/project-history/app/js/HttpController.js (1396 L, 33 handlers).
//
// The vendor handlers read express req (params/query/body) and write res
// (json/status/send/stream). Here each handler is a method on *Controller
// taking a parsed *Req and returning a *Result (see below). The
// request-validation schemas in the vendor (parseReq + zod) collapse to the
// typed fields Req exposes; their failure modes are exercised by the router
// layer (D5) and are not re-modelled here.
package httpcontroller

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"ollitex/go/services/project-history/internal/diffmanager"
	"ollitex/go/services/project-history/internal/errrecorder"
	"ollitex/go/services/project-history/internal/flushmanager"
	"ollitex/go/services/project-history/internal/healthchecker"
	"ollitex/go/services/project-history/internal/historyapimanager"
	"ollitex/go/services/project-history/internal/historystoremanager"
	"ollitex/go/services/project-history/internal/labelsmanager"
	"ollitex/go/services/project-history/internal/redismanager"
	"ollitex/go/services/project-history/internal/retrymanager"
	"ollitex/go/services/project-history/internal/snapshotmanager"
	"ollitex/go/services/project-history/internal/summarizedupdatesmanager"
	"ollitex/go/services/project-history/internal/syncmanager"
	"ollitex/go/services/project-history/internal/updatesprocessor"
	"ollitex/go/services/project-history/internal/webapimanager"
)

const oneDayInSeconds = 24 * 60 * 60

// Req is the parsed request: path params (strings, already coerced by the
// router), raw query strings, the JSON body, and headers (needed by
// retryFailures' X-CALLBACK-* forwarding).
type Req struct {
	Params  map[string]string
	Query   map[string]string
	Body    map[string]any
	Headers map[string]string
}

// Result is the HTTP response: a status, a JSON payload, a raw stream body,
// or an empty body (204/409/404).
type Result struct {
	Status  int    // 0 → 200
	JSON    any    // res.json / res.send(payload)
	Body    []byte // raw body (stream endpoints)
	Headers map[string]any
	NoBody  bool
}

func jsonResult(v any) *Result      { return &Result{JSON: v} }
func statusResult(code int) *Result { return &Result{Status: code, NoBody: true} }
func streamResult(b []byte) *Result { return &Result{Body: b} }

// Manager seams — the vendor imports module *functions*; the Go port exposes
// those as methods on each package's *Deps. The controller drives the method
// interfaces below, so the router (D5) can inject the real *Deps bundles and
// tests inject fakes.

type UPManager interface {
	ProcessUpdatesForProject(ctx context.Context, projectID string) error
	ProcessSingleUpdateForProject(ctx context.Context, projectID string) error
	ProcessUpdatesForProjectUsingBisect(ctx context.Context, projectID string, batchSize int) error
	GetRawUpdates(ctx context.Context, projectID string, batchSize int) (map[string]any, error)
	FlushResyncUpdates(ctx context.Context, projectID string) error
}

type SUMManager interface {
	GetSummarizedProjectUpdates(ctx context.Context, projectID string, options summarizedupdatesmanager.Options) ([]map[string]any, int, error)
}

type DIFFManager interface {
	GetDiff(ctx context.Context, projectID, pathname string, fromVersion, toVersion int) (any, error)
	GetFileTreeDiff(ctx context.Context, projectID string, fromVersion, toVersion int) (any, error)
}

type HSMManager interface {
	InitializeProject(ctx context.Context, historyID *string) (string, error)
	CloneProject(ctx context.Context, sourceProjectID, targetProjectID string) ([]byte, error)
	GetMostRecentVersion(ctx context.Context, projectID, historyID string) (int, map[string]any, map[string]any, map[string]any, error)
	GetProjectBlobStream(ctx context.Context, historyID, blobHash string) ([]byte, error)
}

type WEBManager interface {
	GetHistoryId(ctx context.Context, projectID string) (string, error)
}

type SNAPManager interface {
	GetFileSnapshotStream(ctx context.Context, projectID string, version int, pathname string) ([]byte, error)
	GetRangesSnapshot(ctx context.Context, projectID string, version int, pathname string) (map[string]any, error)
	GetFileMetadataSnapshot(ctx context.Context, projectID string, version int, pathname string) (map[string]any, error)
	GetProjectSnapshot(ctx context.Context, projectID string, version int) (map[string]any, error)
	GetPathsAtVersion(ctx context.Context, projectID string, version int) (map[string]any, error)
	GetLatestSnapshotFull(ctx context.Context, projectID, historyID string) (*snapshotmanager.Snapshot, int, error)
	GetChangesInChunkSince(ctx context.Context, projectID, historyID string, sinceVersion int) (int, []map[string]any, error)
}

type HCManager interface {
	Check(ctx context.Context) error
	CheckLock(ctx context.Context) (bool, error)
}

type SYNCManager interface {
	GetResyncState(ctx context.Context, projectID string) (*syncmanager.SyncState, error)
	CloneResyncState(ctx context.Context, sourceProjectID, targetProjectID string) error
	StartResync(ctx context.Context, projectID string, options map[string]any) error
	StartHardResync(ctx context.Context, projectID string, options map[string]any) error
	ClearResyncState(ctx context.Context, projectID string) error
}

type LBLManager interface {
	GetLabels(ctx context.Context, projectID string) ([]map[string]any, error)
	CreateLabel(ctx context.Context, projectID, userID string, version int, comment string, createdAt any, shouldValidateExists bool) (map[string]any, error)
	DeleteLabelForUser(ctx context.Context, projectID, userID, labelID string) error
	DeleteLabel(ctx context.Context, projectID, labelID string) error
	TransferLabels(ctx context.Context, fromUserID, toUserID string) error
	CloneLabels(ctx context.Context, sourceProjectID, targetProjectID string) error
}

type APIManager interface {
	ShouldUseProjectHistory(ctx context.Context, projectID string) (bool, error)
}

type RETRYManager interface {
	RetryFailures(ctx context.Context, options retrymanager.Options) (*retrymanager.BatchResult, error)
}

type FLUSHManager interface {
	FlushOldOps(ctx context.Context, options flushmanager.Options) (*flushmanager.FlushResult, error)
}

// Deps bundles the manager seams plus the few controller-local seams
// (clone incremental response, callback ping, loggers).
type Deps struct {
	UP    UPManager
	SUM   SUMManager
	DIFF  DIFFManager
	HSM   HSMManager
	WEB   WEBManager
	SNAP  SNAPManager
	HC    HCManager
	SYNC  SYNCManager
	ER    *errrecorder.Deps
	RED   *redismanager.RedisManager
	LBL   LBLManager
	API   APIManager
	RETRY RETRYManager
	FLUSH FLUSHManager

	// cloneProject — IncrementalResponse.sendUpdate / .fail / res.destroyed
	CloneSendUpdate func(label string)
	CloneFail       func(err error) bool // return true if the run should stop
	Aborted         func() bool

	// retryFailures — fetchNothing(callbackUrl, {headers})
	FetchCallback func(url string, headers map[string]string) error
	// logger.warn { err } 'failed to ping callback url'
	LogWarn func(info map[string]any, msg string)
	// logger.debug
	LogDebug func(info map[string]any, msg string)
	// logger.error 'not using v2 history' (createLabel)
	LogErr func(info map[string]any, msg string)
	// logger.err { err } health check / lock check
	LogErrLvl func(info map[string]any, msg string)

	// UpdatesProcessor.REDIS_READ_BATCH_SIZE
	RedisReadBatchSize int
}

// tag mirrors OError.tag(err, [message]) where message is optional.
func tag(err error, msg ...string) error {
	if msg != nil && msg[0] != "" {
		return errors.New(msg[0] + ": " + err.Error())
	}
	return fmt.Errorf("%w", err)
}

// Compile-time checks: the concrete manager bundles satisfy the seams.
var (
	_ UPManager    = (*updatesprocessor.Deps)(nil)
	_ SUMManager   = (*summarizedupdatesmanager.Deps)(nil)
	_ DIFFManager  = (*diffmanager.Deps)(nil)
	_ HSMManager   = (*historystoremanager.Deps)(nil)
	_ WEBManager   = (*webapimanager.Deps)(nil)
	_ SNAPManager  = (*snapshotmanager.Deps)(nil)
	_ HCManager    = (*healthchecker.Deps)(nil)
	_ SYNCManager  = (*syncmanager.Deps)(nil)
	_ LBLManager   = (*labelsmanager.Deps)(nil)
	_ APIManager   = (*historyapimanager.Deps)(nil)
	_ RETRYManager = (*retrymanager.Deps)(nil)
	_ FLUSHManager = (*flushmanager.Deps)(nil)
)

// Controller implements the 33 HttpController handlers.
type Controller struct {
	d   *Deps
	ctx context.Context
}

func New(ctx context.Context, d *Deps) *Controller {
	if d.RedisReadBatchSize == 0 {
		d.RedisReadBatchSize = 500
	}
	return &Controller{d: d, ctx: ctx}
}

// ---------------------------------------------------------------------------
// cloneProject
// ---------------------------------------------------------------------------

// CloneResult captures the incremental pipeline outcome: the cloned history
// bytes (the streamed body), the sendUpdate labels emitted in order, and a
// failure (message) if the pipeline aborted early.
type CloneResult struct {
	Body    []byte
	Updates []string
	Failure string
}

func (c *Controller) cloneSendUpdate(label string) {
	if c.d.CloneSendUpdate != nil {
		c.d.CloneSendUpdate(label)
	}
}

func (c *Controller) CloneProject(sourceProjectID, targetProjectID string) *CloneResult {
	out := &CloneResult{}
	stop := func(err error) {
		out.Failure = err.Error()
		if c.d.CloneFail != nil {
			c.d.CloneFail(err)
		}
	}
	record := func(label string) {
		out.Updates = append(out.Updates, label)
		c.cloneSendUpdate(label)
	}

	record("best effort history flush: pending")
	if err := c.d.UP.ProcessUpdatesForProject(c.ctx, sourceProjectID); err != nil {
		if c.d.LogWarn != nil {
			c.d.LogWarn(map[string]any{"err": err.Error(), "sourceProjectId": sourceProjectID}, "failed to flush during history clone")
		}
		record("best effort history flush: failed, a resync will be required")
	} else {
		record("best effort history flush: done")
	}

	targetHID, err := c.d.WEB.GetHistoryId(c.ctx, targetProjectID)
	if err != nil {
		stop(tag(err, "get target historyId"))
		return out
	}
	sourceHID, err := c.d.WEB.GetHistoryId(c.ctx, sourceProjectID)
	if err != nil {
		stop(tag(err, "get source historyId"))
		return out
	}

	record("cloning full project history data: pending")
	body, err := c.d.HSM.CloneProject(c.ctx, sourceHID, targetHID)
	if err != nil {
		stop(tag(err, "clone history-v1 data"))
		return out
	}
	if c.d.Aborted != nil && c.d.Aborted() {
		stop(errors.New("request aborted"))
		return out
	}
	out.Body = body

	record("clone labels: pending")
	if err := c.d.LBL.CloneLabels(c.ctx, sourceProjectID, targetProjectID); err != nil {
		stop(tag(err, "clone labels"))
		return out
	}
	record("clone labels: done")

	record("clone resync state: pending")
	if err := c.d.SYNC.CloneResyncState(c.ctx, sourceProjectID, targetProjectID); err != nil {
		stop(tag(err, "clone resync state"))
		return out
	}
	record("clone resync state: done")

	record("clone failure record: pending")
	if err := errrecorder.CloneFailure(c.ctx, c.d.ER, sourceProjectID, targetProjectID); err != nil {
		stop(tag(err, "clone failure"))
		return out
	}
	record("clone failure record: done")

	record("done")
	return out
}

// ---------------------------------------------------------------------------
// getProjectBlob / getLatestSnapshot family (streams)
// ---------------------------------------------------------------------------

// GetProjectBlob — vendor getProjectBlob: 404 for a 404 from the history
// service; Cache-Control `private, max-age=86400` on success.
func (c *Controller) GetProjectBlob(historyID, blobHash string) (*Result, error) {
	body, err := c.d.HSM.GetProjectBlobStream(c.ctx, historyID, blobHash)
	if err != nil {
		if historicalBlobNotFound(err) {
			return statusResult(404), nil
		}
		return nil, tag(err)
	}
	return &Result{Body: body, Headers: map[string]any{
		"Cache-Control": fmt.Sprintf("private, max-age=%d", oneDayInSeconds),
	}}, nil
}

// historicalBlobNotFound — the vendor's `err instanceof RequestFailedError &&
// err.response.status === 404` (the seam's requestError is unexported, so we
// use historystoremanager's accessor).
func historicalBlobNotFound(err error) bool {
	return historystoremanager.RequestFailedStatus(err) == 404
}

// InitializeProject — res.json({ project: { id } }).
func (c *Controller) InitializeProject(historyID *string) (*Result, error) {
	id, err := c.d.HSM.InitializeProject(c.ctx, historyID)
	if err != nil {
		return nil, tag(err)
	}
	return jsonResult(map[string]any{"project": map[string]any{"id": id}}), nil
}

// ---------------------------------------------------------------------------
// flushProject
// ---------------------------------------------------------------------------

// FlushRequest mirrors the parsed query (debug/bisect are stringbool).
type FlushRequest struct {
	ProjectID  string
	Debug      bool
	Bisect     bool
	Background bool
}

func (c *Controller) FlushProject(r *FlushRequest) (*Result, error) {
	info := map[string]any{"projectId": r.ProjectID}
	if r.Debug {
		if c.d.LogDebug != nil {
			c.d.LogDebug(info, "compressing project history in single-step mode")
		}
		if err := c.d.UP.ProcessSingleUpdateForProject(c.ctx, r.ProjectID); err != nil {
			return nil, tag(err)
		}
		return statusResult(204), nil
	}
	if r.Bisect {
		if c.d.LogDebug != nil {
			c.d.LogDebug(info, "compressing project history in bisect mode")
		}
		if err := c.d.UP.ProcessUpdatesForProjectUsingBisect(c.ctx, r.ProjectID, c.d.RedisReadBatchSize); err != nil {
			return nil, tag(err)
		}
		return statusResult(204), nil
	}
	if c.d.LogDebug != nil {
		c.d.LogDebug(info, "compressing project history")
	}
	if err := c.d.UP.ProcessUpdatesForProject(c.ctx, r.ProjectID); err != nil {
		return nil, tag(err)
	}
	return statusResult(204), nil
}

// DumpProject — res.json(rawUpdates) (count defaults to REDIS_READ_BATCH_SIZE).
func (c *Controller) DumpProject(projectID string, count *int) (*Result, error) {
	batchSize := c.d.RedisReadBatchSize
	if count != nil && *count != 0 {
		batchSize = *count
	}
	if c.d.LogDebug != nil {
		c.d.LogDebug(map[string]any{"projectId": projectID}, "retrieving raw updates")
	}
	raw, err := c.d.UP.GetRawUpdates(c.ctx, projectID, batchSize)
	if err != nil {
		return nil, tag(err)
	}
	return jsonResult(raw), nil
}

// FlushOld — res.send(results).
type FlushOldQuery struct {
	MaxAge     int64
	QueueDelay int64
	Limit      int
	Timeout    int64
	Background bool
}

func (c *Controller) FlushOld(q *FlushOldQuery) (*Result, error) {
	opts := flushmanager.Options{
		Background: q.Background,
		MaxAge:     int(q.MaxAge),
		QueueDelay: int(q.QueueDelay),
		Limit:      q.Limit,
		Timeout:    q.Timeout,
	}
	res, err := c.d.FLUSH.FlushOldOps(c.ctx, opts)
	if err != nil {
		return nil, tag(err)
	}
	return jsonResult(res), nil
}

// GetDiff — res.json({ diff }).
func (c *Controller) GetDiff(projectID, pathname string, from, to int) (*Result, error) {
	if c.d.LogDebug != nil {
		c.d.LogDebug(map[string]any{"projectId": projectID, "pathname": pathname, "from": from, "to": to}, "getting diff")
	}
	diff, err := c.d.DIFF.GetDiff(c.ctx, projectID, pathname, from, to)
	if err != nil {
		return nil, tag(err)
	}
	return jsonResult(map[string]any{"diff": diff}), nil
}

// GetFileTreeDiff — res.json({ diff }).
func (c *Controller) GetFileTreeDiff(projectID string, from, to int) (*Result, error) {
	diff, err := c.d.DIFF.GetFileTreeDiff(c.ctx, projectID, from, to)
	if err != nil {
		return nil, tag(err)
	}
	return jsonResult(map[string]any{"diff": diff}), nil
}

// GetUpdates — vendor: converts each update's pathnames set→sorted array,
// then res.json({ updates, nextBeforeTimestamp }).
func (c *Controller) GetUpdates(projectID string, before, minCount *int) (*Result, error) {
	opts := summarizedupdatesmanager.Options{}
	if before != nil {
		opts.Before = before
	}
	if minCount != nil {
		opts.MinCount = *minCount
	}
	updates, nextBefore, err := c.d.SUM.GetSummarizedProjectUpdates(c.ctx, projectID, opts)
	if err != nil {
		return nil, tag(err)
	}
	for _, u := range updates {
		if pn, ok := u["pathnames"]; ok && pn != nil {
			// Sets don't JSONify → sorted array (vendor).
			list := []string{}
			switch v := pn.(type) {
			case []string:
				list = append(list, v...)
			case []any:
				for _, e := range v {
					if s, ok := e.(string); ok {
						list = append(list, s)
					}
				}
			}
			sort.Strings(list)
			u["pathnames"] = list
		}
	}
	return jsonResult(map[string]any{"updates": updates, "nextBeforeTimestamp": nextBefore}), nil
}

// GetResyncPending — { resyncPending, syncStuck }.
func (c *Controller) GetResyncPending(projectID string) (*Result, error) {
	state, err := c.d.SYNC.GetResyncState(c.ctx, projectID)
	if err != nil {
		return nil, err
	}
	return jsonResult(map[string]any{
		"resyncPending": state.IsSyncOngoing(),
		"syncStuck":     state.IsSyncStuck(time.Now()),
	}), nil
}

// GetDebugInfo — { failureRecord, syncState: { ...toRaw, resyncPending, ... } }.
func (c *Controller) GetDebugInfo(projectID string) (*Result, error) {
	state, err := c.d.SYNC.GetResyncState(c.ctx, projectID)
	if err != nil {
		return nil, err
	}
	failureRecord, err := errrecorder.GetFailureRecord(c.ctx, c.d.ER, projectID)
	if err != nil {
		return nil, err
	}
	syncState := map[string]any{
		"resyncPending":      state.IsSyncOngoing(),
		"resyncCount":        state.ResyncCount,
		"resyncPendingSince": state.ResyncPendingSince,
		"lastUpdated":        state.LastUpdated,
		"history":            state.History,
	}
	for k, v := range state.ToRaw() {
		syncState[k] = v
	}
	return jsonResult(map[string]any{"failureRecord": failureRecord, "syncState": syncState}), nil
}

// LatestVersion — res.json({ version, timestamp?, v2Authors? }).
func (c *Controller) LatestVersion(projectID string) (*Result, error) {
	if c.d.LogDebug != nil {
		c.d.LogDebug(map[string]any{"projectId": projectID}, "compressing project history and getting version")
	}
	if err := c.d.UP.ProcessUpdatesForProject(c.ctx, projectID); err != nil {
		return nil, tag(err)
	}
	historyID, err := c.d.WEB.GetHistoryId(c.ctx, projectID)
	if err != nil {
		return nil, tag(err)
	}
	version, _, lastChange, _, err := c.d.HSM.GetMostRecentVersion(c.ctx, projectID, historyID)
	if err != nil {
		return nil, tag(err)
	}
	out := map[string]any{"version": version}
	if lastChange != nil {
		if ts, ok := lastChange["timestamp"]; ok {
			out["timestamp"] = ts
		}
		if va, ok := lastChange["v2Authors"]; ok {
			out["v2Authors"] = va
		}
	}
	return jsonResult(out), nil
}

// GetFileSnapshot — stream.
func (c *Controller) GetFileSnapshot(projectID string, version int, pathname string) (*Result, error) {
	body, err := c.d.SNAP.GetFileSnapshotStream(c.ctx, projectID, version, pathname)
	if err != nil {
		return nil, tag(err)
	}
	return streamResult(body), nil
}

// GetRangesSnapshot — res.json(ranges).
func (c *Controller) GetRangesSnapshot(projectID string, version int, pathname string) (*Result, error) {
	ranges, err := c.d.SNAP.GetRangesSnapshot(c.ctx, projectID, version, pathname)
	if err != nil {
		return nil, tag(err)
	}
	return jsonResult(ranges), nil
}

// GetFileMetadataSnapshot — res.json(data).
func (c *Controller) GetFileMetadataSnapshot(projectID string, version int, pathname string) (*Result, error) {
	data, err := c.d.SNAP.GetFileMetadataSnapshot(c.ctx, projectID, version, pathname)
	if err != nil {
		return nil, tag(err)
	}
	return jsonResult(data), nil
}

// GetLatestSnapshot — { snapshot, version }.
func (c *Controller) GetLatestSnapshot(projectID string) (*Result, error) {
	historyID, err := c.d.WEB.GetHistoryId(c.ctx, projectID)
	if err != nil {
		return nil, tag(err)
	}
	snap, version, err := c.d.SNAP.GetLatestSnapshotFull(c.ctx, projectID, historyID)
	if err != nil {
		return nil, err
	}
	return jsonResult(map[string]any{"snapshot": snap.ToRaw(), "version": version}), nil
}

// GetChangesInChunkSince — { latestStartVersion, changes: [toRaw...] }.
func (c *Controller) GetChangesInChunkSince(projectID string, since int) (*Result, error) {
	historyID, err := c.d.WEB.GetHistoryId(c.ctx, projectID)
	if err != nil {
		return nil, tag(err)
	}
	latestStart, changes, err := c.d.SNAP.GetChangesInChunkSince(c.ctx, projectID, historyID, since)
	if err != nil {
		return nil, err
	}
	return jsonResult(map[string]any{"latestStartVersion": latestStart, "changes": changes}), nil
}

// GetProjectSnapshot — res.json(snapshotData).
func (c *Controller) GetProjectSnapshot(projectID string, version int) (*Result, error) {
	data, err := c.d.SNAP.GetProjectSnapshot(c.ctx, projectID, version)
	if err != nil {
		return nil, err
	}
	return jsonResult(data), nil
}

// GetPathsAtVersion — res.json(result).
func (c *Controller) GetPathsAtVersion(projectID string, version int) (*Result, error) {
	result, err := c.d.SNAP.GetPathsAtVersion(c.ctx, projectID, version)
	if err != nil {
		return nil, err
	}
	return jsonResult(result), nil
}

// HealthCheck — 500 / 200.
func (c *Controller) HealthCheck() int {
	if err := c.d.HC.Check(c.ctx); err != nil {
		if c.d.LogErrLvl != nil {
			c.d.LogErrLvl(map[string]any{"err": err.Error()}, "error performing health check")
		}
		return 500
	}
	return 200
}

// CheckLock — 500 / 200.
func (c *Controller) CheckLock() int {
	if _, err := c.d.HC.CheckLock(c.ctx); err != nil {
		if c.d.LogErrLvl != nil {
			c.d.LogErrLvl(map[string]any{"err": err.Error()}, "error performing lock check")
		}
		return 500
	}
	return 200
}

// ResyncQuery mirrors the parsed resync route input (query force/recover flags +
// body origin/historyRangesMigration; body force/recover booleans folded in).
type ResyncQuery struct {
	ProjectID              string
	Force                  bool
	RecoverCorruptedFiles  bool
	Origin                 map[string]any
	HistoryRangesMigration string
}

// ResyncProject — hard (force) or soft resync, then flush resync updates; 204.
func (c *Controller) ResyncProject(q *ResyncQuery) (*Result, error) {
	options := map[string]any{}
	if q.Origin != nil {
		options["origin"] = q.Origin
	}
	if q.HistoryRangesMigration != "" {
		options["historyRangesMigration"] = q.HistoryRangesMigration
	}
	var startErr error
	if q.Force {
		if q.RecoverCorruptedFiles {
			options["recoverCorruptedFiles"] = true
		}
		startErr = c.d.SYNC.StartHardResync(c.ctx, q.ProjectID, options)
	} else {
		startErr = c.d.SYNC.StartResync(c.ctx, q.ProjectID, options)
	}
	if startErr != nil {
		return nil, startErr
	}
	if err := c.d.UP.FlushResyncUpdates(c.ctx, q.ProjectID); err != nil {
		return nil, err
	}
	return statusResult(204), nil
}

// ForceDebugProject — set forceDebug (unless ?clear), then the failure record.
func (c *Controller) ForceDebugProject(projectID string, clear bool) (*Result, error) {
	state := !clear
	if err := errrecorder.SetForceDebug(c.ctx, c.d.ER, projectID, &state); err != nil {
		return nil, err
	}
	result, err := errrecorder.GetFailureRecord(c.ctx, c.d.ER, projectID)
	if err != nil {
		return nil, err
	}
	return jsonResult(result), nil
}

// GetFailures — res.send({ failures }).
func (c *Controller) GetFailures() (*Result, error) {
	counts, attempts, requests, maxQueue, err := errrecorder.GetFailures(c.ctx, c.d.ER)
	if err != nil {
		return nil, err
	}
	return jsonResult(map[string]any{"failures": map[string]any{
		"counts": counts, "attempts": attempts,
		"requests": requests, "maxQueueSize": maxQueue,
	}}), nil
}

// GetFailuresFull — res.send(result).
func (c *Controller) GetFailuresFull() (*Result, error) {
	result, err := errrecorder.GetFailuresFull(c.ctx, c.d.ER)
	if err != nil {
		return nil, err
	}
	return jsonResult(result), nil
}

// GetQueueCounts — res.send({ queuedProjects }).
func (c *Controller) GetQueueCounts() (*Result, error) {
	n, err := c.d.RED.GetProjectIDsWithHistoryOpsCount()
	if err != nil {
		return nil, err
	}
	return jsonResult(map[string]any{"queuedProjects": n}), nil
}

// GetLabels — v2? labels : 409.
func (c *Controller) GetLabels(projectID string) (*Result, error) {
	shouldUse, err := c.d.API.ShouldUseProjectHistory(c.ctx, projectID)
	if err != nil {
		return nil, err
	}
	if !shouldUse {
		return statusResult(409), nil
	}
	labels, err := c.d.LBL.GetLabels(c.ctx, projectID)
	if err != nil {
		return nil, err
	}
	return jsonResult(labels), nil
}

// CreateLabelRequest mirrors the parsed body+params (userId = param || body).
type CreateLabelRequest struct {
	ProjectID      string
	UserID         string
	Version        int
	Comment        string
	CreatedAt      any
	ValidateExists bool
}

func (c *Controller) CreateLabel(r *CreateLabelRequest) (*Result, error) {
	shouldUse, err := c.d.API.ShouldUseProjectHistory(c.ctx, r.ProjectID)
	if err != nil {
		return nil, err
	}
	if !shouldUse {
		if c.d.LogErr != nil {
			c.d.LogErr(map[string]any{
				"projectId": r.ProjectID, "userId": r.UserID,
				"version": r.Version, "comment": r.Comment,
				"createdAt": r.CreatedAt, "validateExists": r.ValidateExists,
			}, "not using v2 history")
		}
		return statusResult(409), nil
	}
	label, err := c.d.LBL.CreateLabel(c.ctx, r.ProjectID, r.UserID, r.Version, r.Comment, r.CreatedAt, r.ValidateExists)
	if err != nil {
		return nil, err
	}
	return jsonResult(label), nil
}

// DeleteLabelForUser — 204.
func (c *Controller) DeleteLabelForUser(projectID, userID, labelID string) (*Result, error) {
	if err := c.d.LBL.DeleteLabelForUser(c.ctx, projectID, userID, labelID); err != nil {
		return nil, err
	}
	return statusResult(204), nil
}

// DeleteLabel — 204.
func (c *Controller) DeleteLabel(projectID, labelID string) (*Result, error) {
	if err := c.d.LBL.DeleteLabel(c.ctx, projectID, labelID); err != nil {
		return nil, err
	}
	return statusResult(204), nil
}

// RetryFailuresQuery mirrors the parsed query.
type RetryFailuresQuery struct {
	FailureType string // "soft" | "hard" | ""
	Timeout     int64
	Limit       int
	CallbackURL string
}

// RetryFailures — callbackUrl → immediate ack + background run + fire-and-
// forget ping (X-CALLBACK-* forward); else block and { retryStatus: result }.
func (c *Controller) RetryFailures(q *RetryFailuresQuery, r *Req) (*Result, error) {
	opts := retrymanager.Options{
		FailureType: q.FailureType,
		Timeout:     q.Timeout,
		Limit:       q.Limit,
	}
	var earlyAck *Result
	if q.CallbackURL != "" {
		earlyAck = jsonResult(map[string]any{
			"retryStatus": "running retryFailures in background",
		})
	}
	result, err := c.d.RETRY.RetryFailures(c.ctx, opts)
	if q.CallbackURL != "" {
		if err == nil && c.d.FetchCallback != nil {
			hdrs := callbackHeaders(r.Headers)
			if ferr := c.d.FetchCallback(q.CallbackURL, hdrs); ferr != nil {
				if c.d.LogWarn != nil {
					c.d.LogWarn(map[string]any{"err": ferr.Error()}, "failed to ping callback url")
				}
			}
		}
		return earlyAck, nil
	}
	if err != nil {
		return nil, err
	}
	return jsonResult(map[string]any{"retryStatus": result}), nil
}

var callbackRe = regexp.MustCompile(`^X-CALLBACK-(.*)$`)

// callbackHeaders mirrors the vendor's dynamic X-CALLBACK-* header forward
// (case-insensitive match; the suffix is re-capitalization preserved verbatim
// in the vendor — we lowercase the matched suffix exactly like JS toLowerCase
// is NOT applied (vendor uses `found[1]` verbatim)).
func callbackHeaders(headers map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range headers {
		if m := callbackRe.FindStringSubmatch(k); m != nil {
			out[m[1]] = v
		}
	}
	return out
}

// TransferLabels — 204.
func (c *Controller) TransferLabels(fromUser, toUser string) (*Result, error) {
	if err := c.d.LBL.TransferLabels(c.ctx, fromUser, toUser); err != nil {
		return nil, err
	}
	return statusResult(204), nil
}

// DeleteProject — vendor order: clear first-op timestamp (BEFORE clearing the
// queue — the queue location is used in the migration) → clear cached history
// id → destroy queue → clear resync state → clear error; 204.
func (c *Controller) DeleteProject(projectID string) (*Result, error) {
	if err := c.d.RED.ClearFirstOpTimestamp(projectID); err != nil {
		return nil, err
	}
	if err := c.d.RED.ClearCachedHistoryID(projectID); err != nil {
		return nil, err
	}
	if err := c.d.RED.DestroyDocUpdatesQueue(projectID); err != nil {
		return nil, err
	}
	if err := c.d.SYNC.ClearResyncState(c.ctx, projectID); err != nil {
		return nil, err
	}
	if _, err := errrecorder.ClearError(c.ctx, c.d.ER, projectID); err != nil {
		return nil, err
	}
	return statusResult(204), nil
}

var _ = strings.ToUpper
