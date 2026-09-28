// Package healthchecker is the 1:1 port of
// services/project-history/app/js/HealthChecker.js (52 L).
//
// Faithful semantics:
//
//	H1 check(): fetchNothing {127.0.0.1:port}/check_lock (3 s) → on error
//	   OError.tag(err, 'error checking lock for health check',
//	   {project_id: <settings.history.healthCheck.project_id>}).
//	H2 then {url}/flush POST (10 s) → 'error flushing for health check'.
//	H3 then {url}/updates GET (10 s) → 'error getting updates for health check'
//	   (url = http://127.0.0.1:{port}/project/{project_id}).
//	H4 checkLock — vendor `LockManager.healthCheck` (the B-phase LockManager
//	   healthCheck seam).
package healthchecker

import (
	"context"
	"time"
)

const (
	CheckLockTimeout = 3 * time.Second
	FlushTimeout     = 10 * time.Second
	UpdatesTimeout   = 10 * time.Second
)

// Deps — the vendor imports as seams.
type Deps struct {
	// The service's own port + the health-check project id (settings).
	Port      string
	ProjectID string
	// FetchNothing — the fetch-utils helper (the check_lock/flush/updates calls).
	FetchNothing func(ctx context.Context, url string, timeout time.Duration) error
	// CheckLockFn — vendor LockManager.healthCheck (H4); named *Fn to avoid
	// colliding with the exported CheckLock method.
	CheckLockFn func(ctx context.Context) (bool, error)
}

// tagged — OError.tag(err, message, info) parity (message REPLACES, info
// MERGES — see the C2 E-series).
type tagged struct {
	msg   string
	info  map[string]any
	cause error
}

func (e *tagged) Error() string        { return e.msg }
func (e *tagged) Unwrap() error        { return e.cause }
func (e *tagged) Info() map[string]any { return e.info }

// Check — vendor check (H1–H3).
func (d *Deps) Check(ctx context.Context) error {
	if d.FetchNothing == nil {
		return &tagged{msg: "fetch seam not wired"}
	}
	pid := d.ProjectID
	base := "http://127.0.0.1:" + d.Port

	if err := d.FetchNothing(ctx, base+"/check_lock", CheckLockTimeout); err != nil {
		return &tagged{
			msg:   "error checking lock for health check",
			info:  map[string]any{"project_id": pid},
			cause: err,
		}
	}
	url := base + "/project/" + pid
	if err := d.FetchNothing(ctx, url+"/flush", FlushTimeout); err != nil {
		return &tagged{
			msg:   "error flushing for health check",
			info:  map[string]any{"project_id": pid},
			cause: err,
		}
	}
	if err := d.FetchNothing(ctx, url+"/updates", UpdatesTimeout); err != nil {
		return &tagged{
			msg:   "error getting updates for health check",
			info:  map[string]any{"project_id": pid},
			cause: err,
		}
	}
	return nil
}

// CheckLock — vendor checkLock (H4).
func (d *Deps) CheckLock(ctx context.Context) (bool, error) {
	if d.CheckLockFn == nil {
		return false, &tagged{msg: "LockManager.healthCheck seam not wired"}
	}
	return d.CheckLockFn(ctx)
}
