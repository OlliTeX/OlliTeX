// Package blobmanager is the 1:1 port of
// services/project-history/app/js/BlobManager.js (129 L).
package blobmanager

import (
	"context"
	"sync"
	"time"

	"ollitex/go/services/project-history/internal/updatetranslator"
)

// Vendor module-level constants (kept exact).
const (
	MaxConcurrentRequests = 4
	RetryAttempts         = 3
	RetryInterval         = 100 * time.Millisecond
)

// Deps — the vendor module-level imports as seams.
type Deps struct {
	// CreateBlob — HistoryStoreManager.createBlobForUpdate.
	CreateBlob func(ctx context.Context, projectID, historyID string, update map[string]any) (map[string]any, error)
	// ExtendLock — the caller's lock extender (vendor `extendLock`).
	ExtendLock func(ctx context.Context) error
	// Slew — the retry delay (async.retry interval); defaults to time.Sleep.
	Slew func(d time.Duration)
	// Attempts / Interval — the vendor constants, overridable in tests.
	Attempts int
	Interval time.Duration
}

func (d *Deps) withDefaults() *Deps {
	if d == nil {
		d = &Deps{}
	}
	if d.Slew == nil {
		d.Slew = time.Sleep
	}
	if d.Attempts <= 0 {
		d.Attempts = RetryAttempts
	}
	if d.Interval <= 0 {
		d.Interval = RetryInterval
	}
	return d
}

// errBlob — vendor OError.tag(err[, message, info]).
type errBlob struct {
	msg  string
	info map[string]any
}

func (e *errBlob) Error() string { return e.msg }
func (e *errBlob) Info() map[string]any {
	if e.info == nil {
		return map[string]any{}
	}
	return e.info
}

func tagBlob(err error, message string, info map[string]any) *errBlob {
	if err == nil {
		return nil
	}
	eb, ok := err.(*errBlob)
	if !ok {
		eb = &errBlob{msg: err.Error()}
	}
	if message != "" {
		eb.msg = message
	}
	if info != nil {
		if eb.info == nil {
			eb.info = map[string]any{}
		}
		for k, v := range info {
			eb.info[k] = v
		}
	}
	return eb
}

// CreateBlobsForUpdates — vendor createBlobsForUpdates:
// async.mapLimit(4) over the updates; non-add updates pass through
// untouched ({update}); add updates get a blob created (up to `times`
// attempts, extending the lock per attempt AND again on success); the
// FIRST error (creation failure or success-path extendLock) is reported
// after ALL jobs have finished. Order is preserved (mapLimit contract).
//
// Element shape (faithful): success → {update, blobHashes}; failure →
// {update} (vendor `blobHashes` is undefined → the key is absent).
func CreateBlobsForUpdates(ctx context.Context, d *Deps, projectID, historyID string, updates []map[string]any) ([]map[string]any, error) {
	d = d.withDefaults()
	outputs := make([]map[string]any, len(updates))
	var mu sync.Mutex
	var firstBlobCreationError *errBlob

	workers := MaxConcurrentRequests
	if len(updates) < workers {
		workers = len(updates)
	}
	var wg sync.WaitGroup
	jobs := make(chan int)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				outputs[idx] = processUpdate(ctx, d, projectID, historyID, updates[idx], &mu, &firstBlobCreationError)
			}
		}()
	}
	for i := range updates {
		jobs <- i
	}
	close(jobs)
	wg.Wait() // async.mapLimit returns after ALL jobs settle

	if firstBlobCreationError != nil {
		return nil, firstBlobCreationError
	}
	return outputs, nil
}

// processUpdate — one update through the vendor retry/lock pipeline.
func processUpdate(ctx context.Context, d *Deps, projectID, historyID string, update map[string]any, mu *sync.Mutex, first **errBlob) map[string]any {
	if !updatetranslator.IsAddUpdate(update) {
		// vendor: `return async.setImmediate(() => cb(null, {update}))`
		return map[string]any{"update": update}
	}
	var lastErr *errBlob
	var hashes map[string]any
	success := false
	for attempt := 1; attempt <= d.Attempts; attempt++ {
		if attempt > 1 {
			// vendor: retry delay (async.retry interval)
			d.Slew(d.Interval)
		}
		// extend the lock for each file because large files may take a long time
		if d.ExtendLock != nil {
			if err := d.ExtendLock(ctx); err != nil {
				lastErr = tagBlob(err, "", nil)
				// vendor: extendLock failure aborts the retry loop
				break
			}
		}
		h, err := d.CreateBlob(ctx, projectID, historyID, update)
		if err != nil {
			lastErr = tagBlob(err, "retry: error creating blob", map[string]any{
				"projectId": projectID,
				"doc":       update["doc"],
				"file":      update["file"],
			})
			continue
		}
		hashes = h
		success = true
		break
	}
	if !success {
		mu.Lock()
		if *first == nil && lastErr != nil {
			*first = lastErr
		}
		mu.Unlock()
		return map[string]any{"update": update}
	}
	// success path: extendLock once more (vendor); failures only collected
	if d.ExtendLock != nil {
		if err := d.ExtendLock(ctx); err != nil {
			mu.Lock()
			if *first == nil {
				*first = tagBlob(err, "", nil)
			}
			mu.Unlock()
		}
	}
	return map[string]any{"update": update, "blobHashes": hashes}
}
