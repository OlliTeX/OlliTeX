// Package retrymanager is the 1:1 port of
// services/project-history/app/js/RetryManager.js (205 L) — the background
// re-retry of recorded project-history failures.
//
// Faithful constants & strings (vendor module-level):
//
//	R1 TEMPORARY_FAILURES — 5 exact error strings (ENOSPC, ESOCKETTIMEDOUT,
//	   'failed to extend lock', 'tried to release timed out lock', Timeout).
//	R2 HARD_FAILURES — 6 exact strings (the 422 history store message + the
//	   four OpsOutOfOrderError variants).
//	R3 MAX_RESYNC_ATTEMPTS=2, MAX_SOFT_RESYNC_ATTEMPTS=1,
//	   SYNC_ONGOING_ERROR_MESSAGE='sync ongoing'.
//	R4 selectors (failure shape: {project_id, error, attempts?,
//	   resyncAttempts?}):
//	   soft:   (temporary && !repeated) || (first && !hard)
//	   hard:   ongoing → true; else (hard || repeated) && !stuck
//	   first:  attempts <= 1        (undefined → false — JS comparison)
//	   repeated: attempts > 3       (undefined → false)
//	   stuck:  resyncAttempts != null && >= MAX_RESYNC_ATTEMPTS
//	R5 isOngoingSyncFailure: `failure.error?.includes('sync ongoing') ?? false`.
//	R6 hard-when: resyncAttempts != null && >= MAX_SOFT_RESYNC_ATTEMPTS.
//	R7 batch: filter → SHUFFLE → limit slice(0, limit) when limit > 0.
//	R8 loop: sequential; elapsed = now - startTime; when timeout != 0 &&
//	   elapsed > timeout → break early; handler err → failed += project_id,
//	   else succeeded += project_id (project-level, no stack).
//	R9 resync: projectId !~ /^[0-9a-f]{24}$/ → clearError + RETURN (no throw);
//	   hid nil → OError 'no history id'; hard → startHardResync else
//	   startResync; waitUntilRedisQueueIsEmpty (30 × (count==0 → return,
//	   sleep 1 s)) else OError 'queue not empty'; record still present →
//	   OError 'failure record still exists'; ANY error rethrown as
//	   OError 'failed to resync project' {projectId, hard} withCause(err).
package retrymanager

import (
	"context"
	"regexp"
	"time"
)

// Vendor constants (R1–R3).
var (
	TEMPORARY_FAILURES = []string{
		"Error: ENOSPC: no space left on device, write",
		"Error: ESOCKETTIMEDOUT",
		"Error: failed to extend lock",
		"Error: tried to release timed out lock",
		"Error: Timeout",
	}
	HARD_FAILURES = []string{
		"Error: history store a non-success status code: 422",
		"OpsOutOfOrderError: project structure version out of order",
		"OpsOutOfOrderError: project structure version out of order on incoming updates",
		"OpsOutOfOrderError: doc version out of order",
		"OpsOutOfOrderError: doc version out of order on incoming updates",
	}
)

const (
	retryWaitDefaultSleeperSkip = iota
)

var _ = retryWaitDefaultSleeperSkip

const (
	MaxResyncAttempts     = 2
	MaxSoftResyncAttempts = 1
	SyncOngoingErrMessage = "sync ongoing"
	queueWaitAttempts     = 30
	queueWaitInterval     = 1 * time.Second
)

var defaultSleeper = time.Sleep

var projectIDRe = regexp.MustCompile(`^[0-9a-f]{24}$`)

// Failure — the vendor record shape (mongo docs; attempts/resyncAttempts may
// be ABSENT → nil, preserving the vendor's undefined-comparison semantics).
type Failure struct {
	ProjectID      string
	Error          string
	Attempts       *int
	ResyncAttempts *int
}

// Deps — the vendor module-level imports as seams.
type Deps struct {
	// ErrorRecorder (B12).
	GetFailedProjects func(ctx context.Context) ([]Failure, error)
	ClearError        func(ctx context.Context, projectID string) error
	GetFailureRecord  func(ctx context.Context, projectID string) (*Failure, error)
	// WebApiManager (C10 shim).
	GetHistoryID func(ctx context.Context, projectID string) (string, error)
	// SyncManager (C16 shim).
	StartHardResync func(ctx context.Context, projectID string) error
	StartResync     func(ctx context.Context, projectID string) error
	// RedisManager.
	CountUnprocessedUpdates func(ctx context.Context, projectID string) (int, error)
	// UpdatesProcessor (C17 shim) — the soft-retry handler.
	ProcessUpdatesForProject func(ctx context.Context, projectID string) error
	// Sleep — the vendor `sleep(1000)` seam (tests use a null sleep).
	Sleep func(d time.Duration)
	// Now — the clock seam (elapsed accounting; defaults to time.Now).
	Now func() time.Time
}

func (d *Deps) withDefaults() *Deps {
	if d == nil {
		d = &Deps{}
	}
	if d.Sleep == nil {
		d.Sleep = defaultSleeper
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return d
}

// --- predicates (R4–R6, vendor-faithful undefined semantics) ---------------

func in(v string, list []string) bool {
	for _, e := range list {
		if e == v {
			return true
		}
	}
	return false
}

func isTemporaryFailure(f *Failure) bool { return in(f.Error, TEMPORARY_FAILURES) }

// IsHardFailure — vendor exported.
func IsHardFailure(f *Failure) bool { return in(f.Error, HARD_FAILURES) }

// IsFirstFailure — vendor exported. JavaScript: `failure.attempts <= 1` —
// undefined attempts → false (nil *int here mirrors undefined).
func IsFirstFailure(f *Failure) bool { return f.Attempts != nil && *f.Attempts <= 1 }

func isRepeatedFailure(f *Failure) bool { return f.Attempts != nil && *f.Attempts > 3 }

// IsOngoingSyncFailure — vendor exported:
// `failure.error?.includes(SYNC_ONGOING_ERROR_MESSAGE) ?? false` (R5).
func IsOngoingSyncFailure(f *Failure) bool {
	if f.Error == "" {
		return false
	}
	return contains(f.Error, SyncOngoingErrMessage)
}

func contains(s, sub string) bool {
	return len(sub) > 0 && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func isStuckFailure(f *Failure) bool {
	return f.ResyncAttempts != nil && *f.ResyncAttempts >= MaxResyncAttempts
}

func failureRequiresHardResync(f *Failure) bool {
	return f.ResyncAttempts != nil && *f.ResyncAttempts >= MaxSoftResyncAttempts
}

// SoftErrorSelector (vendor softErrorSelector).
func SoftErrorSelector(f *Failure) bool {
	return (isTemporaryFailure(f) && !isRepeatedFailure(f)) || (IsFirstFailure(f) && !IsHardFailure(f))
}

// HardErrorSelector (vendor hardErrorSelector).
func HardErrorSelector(f *Failure) bool {
	if IsOngoingSyncFailure(f) {
		return true
	}
	return (IsHardFailure(f) || isRepeatedFailure(f)) && !isStuckFailure(f)
}

// --- retry engine -----------------------------------------------------------

// Options — vendor retryFailures options.
type Options struct {
	FailureType string
	Timeout     int64 // ms; 0 = absent
	Limit       int   // 0 = absent
}

type BatchResult struct {
	Succeeded []string `json:"succeeded"`
	Failed    []string `json:"failed"`
}

// RetryFailures — vendor promises.retryFailures (R7, R8).
func (d *Deps) RetryFailures(ctx context.Context, options Options) (*BatchResult, error) {
	d = d.withDefaults()
	if options.FailureType == "soft" {
		batch, err := d.getFailureBatch(ctx, SoftErrorSelector, options.Limit)
		if err != nil {
			return nil, err
		}
		return d.retryFailureBatch(ctx, batch, options.Timeout, func(c context.Context, f *Failure) error {
			if d.ProcessUpdatesForProject == nil {
				return errNotWired("UpdatesProcessor.processUpdatesForProject")
			}
			return d.ProcessUpdatesForProject(c, f.ProjectID)
		})
	}
	if options.FailureType == "hard" {
		batch, err := d.getFailureBatch(ctx, HardErrorSelector, options.Limit)
		if err != nil {
			return nil, err
		}
		return d.retryFailureBatch(ctx, batch, options.Timeout, func(c context.Context, f *Failure) error {
			// vendor: ongoing-sync failures ALWAYS soft resync
			hard := failureRequiresHardResync(f) && !IsOngoingSyncFailure(f)
			return d.resyncProject(c, f.ProjectID, hard)
		})
	}
	// vendor: unknown failureType → no batch, no result (undefined)
	return nil, nil
}

// getFailureBatch — vendor getFailureBatch (R7).
func (d *Deps) getFailureBatch(ctx context.Context, selector func(*Failure) bool, limit int) ([]*Failure, error) {
	failures, err := d.GetFailedProjects(ctx)
	if err != nil {
		return nil, err
	}
	out := []*Failure{}
	for i := range failures {
		f := failures[i]
		if selector(&f) {
			out = append(out, &f)
		}
	}
	shuffle(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// retryFailureBatch — vendor retryFailureBatch (R8).
func (d *Deps) retryFailureBatch(ctx context.Context, failures []*Failure, timeout int64, retryHandler func(context.Context, *Failure) error) (*BatchResult, error) {
	d = d.withDefaults()
	startTime := d.Now()
	res := &BatchResult{Succeeded: []string{}, Failed: []string{}}
	for _, failure := range failures {
		if timeout != 0 {
			elapsed := int64(d.Now().Sub(startTime) / time.Millisecond)
			if elapsed > timeout {
				break
			}
		}
		pid := failure.ProjectID
		err := retryHandler(ctx, failure)
		if err == nil {
			res.Succeeded = append(res.Succeeded, pid)
		} else {
			res.Failed = append(res.Failed, pid)
		}
	}
	return res, nil
}

// resyncProject — vendor resyncProject (R9).
func (d *Deps) resyncProject(ctx context.Context, projectID string, hard bool) error {
	d = d.withDefaults()
	var cause error
	if !projectIDRe.MatchString(projectID) {
		if d.ClearError == nil {
			return errNotWired("ErrorRecorder.clearError")
		}
		if e := d.ClearError(ctx, projectID); e != nil {
			return e
		}
		return nil // vendor: return, no throw
	}
	cause = func() error {
		if d.GetHistoryID == nil {
			return errNotWired("WebApiManager.getHistoryId")
		}
		hid, e := d.GetHistoryID(ctx, projectID)
		if e != nil {
			return e
		}
		if hid == "" {
			return eRetry("no history id")
		}
		var e2 error
		if hard {
			if d.StartHardResync == nil {
				return errNotWired("SyncManager.startHardResync")
			}
			e2 = d.StartHardResync(ctx, projectID)
		} else {
			if d.StartResync == nil {
				return errNotWired("SyncManager.startResync")
			}
			e2 = d.StartResync(ctx, projectID)
		}
		if e2 != nil {
			return e2
		}
		if e3 := d.waitUntilEmpty(ctx, projectID); e3 != nil {
			return e3
		}
		return d.checkRecordGone(ctx, projectID)
	}()
	if cause != nil {
		return &retryError{
			msg:   "failed to resync project",
			info:  map[string]any{"projectId": projectID, "hard": hard},
			cause: cause,
		}
	}
	return nil
}

// waitUntilRedisQueueIsEmpty — vendor (30 × (count==0 → return; sleep 1 s)).
func (d *Deps) waitUntilEmpty(ctx context.Context, projectID string) error {
	d = d.withDefaults()
	for i := 0; i < queueWaitAttempts; i++ {
		if d.CountUnprocessedUpdates == nil {
			return errNotWired("RedisManager.countUnprocessedUpdates")
		}
		n, e := d.CountUnprocessedUpdates(ctx, projectID)
		if e != nil {
			return e
		}
		if n == 0 {
			return nil
		}
		d.Sleep(queueWaitInterval)
	}
	return eRetry("queue not empty")
}

// checkFailureRecordWasRemoved — vendor.
func (d *Deps) checkRecordGone(ctx context.Context, projectID string) error {
	if d.GetFailureRecord == nil {
		return errNotWired("ErrorRecorder.getFailureRecord")
	}
	rec, e := d.GetFailureRecord(ctx, projectID)
	if e != nil {
		return e
	}
	if rec != nil {
		return eRetry("failure record still exists")
	}
	return nil
}

// --- errors (OError parity) --------------------------------------------------

type retryError struct {
	msg   string
	info  map[string]any
	cause error
}

func (e *retryError) Error() string { return e.msg }
func (e *retryError) Info() map[string]any {
	if e.cause != nil && e.info == nil {
		return map[string]any{}
	}
	if e.info == nil {
		return map[string]any{}
	}
	return e.info
}
func (e *retryError) Unwrap() error { return e.cause }

func eRetry(msg string) *retryError { return &retryError{msg: msg} }

type notWiredError struct{ what string }

func (e *notWiredError) Error() string { return "seam not wired: " + e.what }

func errNotWired(what string) error { return &notWiredError{what: what} }

// --- deterministic shuffle (vendor lodash _.shuffle — random order) ---------

// shuffle — Fisher–Yates with a fixed seed (the vendor order is random per
// call; the tests pin the SELECTION, the order is asserted set-wise).
func shuffle(list []*Failure) {
	seed := uint64(0x2545F4914F6CDD1D)
	for i := len(list) - 1; i > 0; i-- {
		seed = seed*6364136223846793005 + 1442695040888963407
		j := int(seed % uint64(i+1))
		list[i], list[j] = list[j], list[i]
	}
}
