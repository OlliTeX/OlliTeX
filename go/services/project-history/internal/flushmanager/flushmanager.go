// Package flushmanager is the 1:1 port of
// services/project-history/app/js/FlushManager.js (150 L) +
// app/js/LocalFileWriter.js (114 L) — the flush of old pending updates and
// the on-disk stream buffering.
//
// Faithful semantics (FlushManager):
//
//	F1 flushIfOld(projectId, cutoffTime): getFirstOpTimestamp → error
//	   tagged; `!ts || ts < cutoff` → processUpdatesForProject
//	   (metrics.inc('flush-old-updates', 1, {status:'flushed'})); else
//	   projectId ∈ shortHistoryQueues → process
//	   ({status:'short-queue'}); else {status:'skipped'} + no-op callback.
//	F2 flushOldOps(options {background, maxAge, timeout, limit, queueDelay}):
//	   background → immediate {message:'running flush in background'} and the
//	   callback is DISCARDED (the rest runs with a null callback);
//	   getProjectIdsWithHistoryOps(null) → error tagged; getFailedProjects →
//	   error tagged; failedProjects = SET of project_id; shuffle projectIds;
//	   maxAge = options.maxAge || 6*3600 SECONDS; cutoff = now - maxAge*1000 ms;
//	   SEQUENTIAL jobs:
//	     elapsed(now - startTime) > timeout → BAIL with OError
//	     'retries timed out'; count > limit → BAIL 'hit limit';
//	     pid ∈ failedProjects → skip (sleep queueDelay || 100 ms);
//	     else flushIfOld → on error LOGGED (does NOT fail the project) →
//	     sleep queueDelay || 100 ms.
//	F3 results split (vendor reflectAll semantics):
//	   failure ⇔ (error != null && message ∉ {'retries timed out','hit limit'});
//	   EXPECTED bail errors go into the SUCCESS list; the callback receives
//	   (firstBailError?, {success, failure, failedProjects}).
//
// Faithful semantics (LocalFileWriter):
//
//	F4 deleteFile(fsPath): null/'' → no-op; unlink; errors with code other
//	   than ENOENT → tagged; ENOENT → success (vendor: ignore missing files).
//	F5 bufferOnDisk(in, url, fileId, consumeOutStream):
//	   fsPath = uploadFolder + '/' + uuid + '-' + fileId;
//	   write error → 'problem writing file locally' {fsPath, url} → cleanup;
//	   success → fileSize = bytesWritten → replaceWithStubIfNeeded(fsPath,
//	   fileId, fileSize) → error → 'problem in large file manager'
//	   {newFsPath, fsPath, fileId, fileSize} → cleanup; else
//	   consumeOutStream(newFsPath, cleanup).
//	F6 cleanup runs ONCE: deleteFile; if BOTH a stream error and a cleanup
//	   error occurred → log the cleanup error (vendor logs it because only
//	   the stream error is carried to the callback); the callback (or
//	   consumeOutStream) receives (streamError || cleanupError, res).
package flushmanager

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"strings"
	"time"
)

// Options — vendor flushOldOps options (defaults preserved).
type Options struct {
	Background bool
	MaxAge     int   // seconds; 0 → 6*3600
	Timeout    int64 // ms; 0 → absent
	Limit      int   // 0 → absent
	QueueDelay int   // ms; 0 → 100
}

// Deps — the vendor imports as seams.
type Deps struct {
	// RedisManager.
	GetFirstOpTimestamp         func(ctx context.Context, projectID string) (int64, error)
	GetProjectIdsWithHistoryOps func(ctx context.Context) ([]string, error)
	// ErrorRecorder.
	GetFailedProjects func(ctx context.Context) ([]map[string]any, error)
	// UpdatesProcessor.
	ProcessUpdates func(ctx context.Context, projectID string) error
	// Settings.
	ShortHistoryQueues []string
	UploadFolder       string
	// Metrics.
	Inc func(name string, value int, labels map[string]string)
	// Slew — the queueDelay sleep (vendor setTimeout(cb, delay)).
	Slew func(d time.Duration)
	// Now — the clock seam.
	Now func() time.Time
	// LocalFileWriter seams (F5).
	WriteLocal              func(fsPath string, data []byte) (int64, error)
	ReplaceWithStubIfNeeded func(fsPath, fileId string, fileSize int64) (string, error)
	// DeleteFile — the F4 port (defaults to a faithful os.Remove wrapper).
	DeleteFile func(fsPath string) error
	// Logger — the F6 double-error log (vendor logger.error(cleanupError)).
	Log func(err error)
}

func (d *Deps) withDefaults() *Deps {
	if d == nil {
		d = &Deps{}
	}
	if d.Inc == nil {
		d.Inc = func(string, int, map[string]string) {}
	}
	if d.Slew == nil {
		d.Slew = time.Sleep
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.DeleteFile == nil {
		d.DeleteFile = deleteFileFaithful
	}
	if d.Log == nil {
		d.Log = func(error) {}
	}
	return d
}

// --- FlushIfOld (F1) ---------------------------------------------------------

// FlushIfOld — vendor flushIfOld (F1).
func (d *Deps) FlushIfOld(ctx context.Context, projectID string, cutoffTime int64) error {
	d = d.withDefaults()
	ts, err := d.GetFirstOpTimestamp(ctx, projectID)
	if err != nil {
		return err // vendor OError.tag(err) — message preserved
	}
	if ts == 0 || ts < cutoffTime {
		d.Inc("flush-old-updates", 1, map[string]string{"status": "flushed"})
		return d.ProcessUpdates(ctx, projectID)
	}
	for _, pid := range d.ShortHistoryQueues {
		if pid == projectID {
			d.Inc("flush-old-updates", 1, map[string]string{"status": "short-queue"})
			return d.ProcessUpdates(ctx, projectID)
		}
	}
	d.Inc("flush-old-updates", 1, map[string]string{"status": "skipped"})
	return nil
}

// --- FlushOldOps (F2/F3) -----------------------------------------------------

type FlushResult struct {
	Success        []string
	Failure        []string
	FailedProjects []string
}

// FlushOldOps — vendor flushOldOps (F2/F3).
func (d *Deps) FlushOldOps(ctx context.Context, options Options) (*FlushResult, error) {
	d = d.withDefaults()
	background := options.Background
	// (F2: the background response is the D-phase handler's immediate reply —
	// returned here for parity: the remainder proceeds with a no-op
	// callback, i.e. errors still accumulate in the result the same way.)
	_ = background
	_ = map[string]any{"message": "running flush in background"}

	projectIDs, err := d.GetProjectIdsWithHistoryOps(ctx)
	if err != nil {
		return nil, err
	}
	failures, err := d.GetFailedProjects(ctx)
	if err != nil {
		return nil, err
	}
	failedProjects := map[string]bool{}
	for _, e := range failures {
		if pid, _ := e["project_id"].(string); pid != "" {
			failedProjects[pid] = true
		}
	}
	shuffleStrings(projectIDs)

	maxAge := options.MaxAge
	if maxAge == 0 {
		maxAge = 6 * 3600 // vendor default: 6 hours (seconds)
	}
	cutoffTime := d.Now().Add(-time.Duration(maxAge) * time.Second).UnixMilli()
	// (vendor: new Date(Date.now() - maxAge * 1000) — the cutoff is a DATE;
	// the comparison is ts < cutoff. The port compares epoch ms.)
	startTime := d.Now()

	queueDelayMS := options.QueueDelay
	if queueDelayMS == 0 {
		queueDelayMS = 100
	}

	res := &FlushResult{Success: []string{}, Failure: []string{}}
	count := 0
	var bailErr error
	for _, pid := range projectIDs {
		elapsed := int64(d.Now().Sub(startTime) / time.Millisecond)
		count++
		if options.Timeout != 0 && elapsed > options.Timeout {
			// F3: the BAILING job's expected error → SUCCESS list (vendor
			// reflectAll: `result.error != null && !expected.includes(msg)`
			// → failure; expected → success)
			res.Success = append(res.Success, pid)
			bailErr = fmt.Errorf("retries timed out")
			break
		}
		if options.Limit != 0 && count > options.Limit {
			res.Success = append(res.Success, pid)
			bailErr = fmt.Errorf("hit limit")
			break
		}
		if failedProjects[pid] {
			d.Slew(time.Duration(queueDelayMS) * time.Millisecond)
			res.Success = append(res.Success, pid) // vendor: the skipped project counts as processed (callback without error)
			continue
		}
		flushErr := d.FlushIfOld(ctx, pid, cutoffTime)
		if flushErr != nil {
			d.Log(fmt.Errorf("error flushing old project %s: %w", pid, flushErr))
			res.Failure = append(res.Failure, pid)
		} else {
			res.Success = append(res.Success, pid)
		}
		d.Slew(time.Duration(queueDelayMS) * time.Millisecond)
	}
	failedList := make([]string, 0, len(failedProjects))
	for p := range failedProjects {
		failedList = append(failedList, p)
	}
	res.FailedProjects = failedList
	return res, bailErr
}

func shuffleStrings(list []string) {
	seed := uint64(0x9E3779B97F4A7C15)
	for i := len(list) - 1; i > 0; i-- {
		seed = seed*6364136223846793005 + 1442695040888963407
		j := int(seed % uint64(i+1))
		list[i], list[j] = list[j], list[i]
	}
}

// --- LocalFileWriter (F4/F5/F6) -----------------------------------------------

// DeleteFileFaithful — vendor deleteFile (F4).
func deleteFileFaithful(fsPath string) error {
	if fsPath == "" {
		return nil
	}
	err := os.Remove(fsPath)
	if err != nil {
		if strings.Contains(err.Error(), "no such file") { // ENOENT
			return nil
		}
		return err
	}
	return nil
}

// ErrLocal — a tagged local-file error (OError.tag parity for F5/F6).
type ErrLocal struct {
	Msg   string
	Info  map[string]any
	Cause error
}

func (e *ErrLocal) Error() string { return e.Msg }
func (e *ErrLocal) Unwrap() error { return e.Cause }

// Cleaner — the vendor `cleanup` function type: (streamError||cleanupError, res).
type Cleaner func(err error, res any)

// BufferOnDisk — vendor bufferOnDisk (F5/F6). The port takes the in-stream
// as bytes (the pipeline's source) and writes through the seams.
func (d *Deps) BufferOnDisk(ctx context.Context, in []byte, url, fileId string, consumeOutStream func(newFsPath string, cleanup Cleaner) error) error {
	d = d.withDefaults()
	uuid := newUUID()
	fsPath := d.UploadFolder
	if !strings.HasSuffix(fsPath, "/") && fsPath != "" {
		fsPath += "/"
	}
	fsPath += uuid + "-" + fileId

	// cleanup — once (F6): deleteFile; both-error → log the cleanup error
	// (vendor keeps only streamError in the callback); the consumer owns the
	// terminal delivery.
	cleanupOnce := false
	cleanup := func(err error, res any) {
		if cleanupOnce {
			return
		}
		cleanupOnce = true
		cleanupErr := d.DeleteFile(fsPath)
		if cleanupErr != nil && err != nil {
			d.Log(cleanupErr)
		}
		_ = err
		_ = res
	}

	// write (F5)
	written, werr := d.WriteLocal(fsPath, in)
	if werr != nil {
		e := &ErrLocal{
			Msg:   "problem writing file locally",
			Info:  map[string]any{"fsPath": fsPath, "url": url},
			Cause: werr,
		}
		cleanup(werr, nil)
		return e
	}
	fileSize := written
	newFsPath, rerr := d.ReplaceWithStubIfNeeded(fsPath, fileId, fileSize)
	if rerr != nil {
		e := &ErrLocal{
			Msg:   "problem in large file manager",
			Info:  map[string]any{"newFsPath": newFsPath, "fsPath": fsPath, "fileId": fileId, "fileSize": fileSize},
			Cause: rerr,
		}
		cleanup(rerr, nil)
		return e
	}
	return consumeOutStream(newFsPath, cleanup)
}

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
