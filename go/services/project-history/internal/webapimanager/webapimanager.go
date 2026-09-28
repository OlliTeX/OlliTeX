// Package webapimanager is the 1:1 port of
// services/project-history/app/js/WebApiManager.js (112 L).
//
// Faithful semantics:
//
//	W1 getHistoryId: Metrics.inc('history_id_cache_requests_total') ALWAYS;
//	   redis-cache hit → inc('history_id_cache_hits_total') + return cached.
//	W2 miss: _getProjectDetails → `project.overleaf?.history?.id`; when
//	   != null → setCachedHistoryId (the redis write may fail? vendor does
//	   not catch it — propagate) → return the id (may be undefined).
//	W3 _getProjectDetails: GET {web}/project/{id}/details, 16 s timeout,
//	   basic auth; 404 → NotFoundError 'got a 404 from web api' withCause
//	   (never retried); ELSE: attempts < 2 → sleep RETRY_TIMEOUT_MS + retry
//	   once; second failure → rethrow the original error.
//	W4 requestResync: POST {web}/project/{id}/history/resync, 6 min timeout,
//	   basic auth, body {historyRangesMigration?, resyncProjectStructureOnly?}
//	   (only when truthy); 404 → NotFoundError 'got a 404 from web api'
//	   withCause; other errors rethrown untouched.
//	W5 setRetryTimeoutMs(ms) — the mutable module-level RETRY_TIMEOUT_MS
//	   (vendor default 5000).
package webapimanager

import (
	"context"
	"time"
)

// Vendor module state (W5).
var RETRY_TIMEOUT_MS time.Duration = 5000 * time.Millisecond

const (
	DetailsFetchTimeout = 16 * time.Second
	ResyncTimeout       = 6 * 60 * time.Second
)

// RequestFailedError — the @overleaf/fetch-utils error shape (status-bearing).
type RequestFailedError struct {
	Status int
	Msg    string
}

func (e *RequestFailedError) Error() string { return e.Msg }

// NotFoundError — the vendor Errors.NotFoundError (withCause chain preserved).
type NotFoundError struct {
	Msg   string
	Cause error
}

func (e *NotFoundError) Error() string { return e.Msg }
func (e *NotFoundError) Unwrap() error { return e.Cause }

// Deps — the vendor imports as seams.
type Deps struct {
	// Web api endpoints.
	WebAPIURL string
	WebUser   string
	WebPass   string
	// FetchNothing — fetch-utils POST-no-body (requestResync uses json body —
	// carried in Body).
	FetchNothing func(ctx context.Context, url string, body map[string]any, timeout time.Duration) (*Response, error)
	// FetchJson — fetch-utils GET-JSON (_getProjectDetails).
	FetchJson func(ctx context.Context, url string, timeout time.Duration) (map[string]any, error)
	// RedisManager (C8-shim shape).
	GetCachedHistoryId func(ctx context.Context, projectID string) (string, error)
	SetCachedHistoryId func(ctx context.Context, projectID, historyID string) error
	// Metrics — the two counters (W1/W2).
	Inc func(name string)
	// Sleep — the W3 retry wait seam.
	Sleep func(d time.Duration)
}

// Response — minimal fetch response surface (the 404 check needs status).
type Response struct {
	Status int
}

func (d *Deps) withDefaults() *Deps {
	if d == nil {
		d = &Deps{}
	}
	if d.Inc == nil {
		d.Inc = func(string) {}
	}
	if d.Sleep == nil {
		d.Sleep = time.Sleep
	}
	return d
}

// SetRetryTimeoutMs — vendor exported `setRetryTimeoutMs` (W5).
func SetRetryTimeoutMs(timeoutMs int) {
	RETRY_TIMEOUT_MS = time.Duration(timeoutMs) * time.Millisecond
}

// GetHistoryId — vendor getHistoryId (W1/W2).
func (d *Deps) GetHistoryId(ctx context.Context, projectID string) (string, error) {
	d = d.withDefaults()
	d.Inc("history_id_cache_requests_total")
	if d.GetCachedHistoryId != nil {
		cached, e := d.GetCachedHistoryId(ctx, projectID)
		if e == nil && cached != "" {
			d.Inc("history_id_cache_hits_total")
			return cached, nil
		}
	}
	project, e := d.getProjectDetails(ctx, projectID)
	if e != nil {
		return "", e
	}
	historyId := ""
	if m, ok := project["overleaf"].(map[string]any); ok {
		if h, ok := m["history"].(map[string]any); ok {
			if v, ok := h["id"].(string); ok {
				historyId = v
			}
		}
	}
	if historyId != "" {
		if d.SetCachedHistoryId != nil {
			if e2 := d.SetCachedHistoryId(ctx, projectID, historyId); e2 != nil {
				return "", e2
			}
		}
	}
	return historyId, nil
}

// RequestResync — vendor requestResync (W4).
func (d *Deps) RequestResync(ctx context.Context, projectID string, opts map[string]any) error {
	d = d.withDefaults()
	body := map[string]any{}
	if v, ok := opts["historyRangesMigration"]; ok && truthy(v) {
		body["historyRangesMigration"] = v
	}
	if v, ok := opts["resyncProjectStructureOnly"]; ok && truthy(v) {
		body["resyncProjectStructureOnly"] = v
	}
	if d.FetchNothing == nil {
		return &RequestFailedError{Msg: "web api seam not wired"}
	}
	url := d.WebAPIURL + "/project/" + projectID + "/history/resync"
	_, e := d.FetchNothing(ctx, url, body, ResyncTimeout)
	if e != nil {
		rfe, ok := e.(*RequestFailedError)
		if ok && rfe.Status == 404 {
			return &NotFoundError{Msg: "got a 404 from web api", Cause: e}
		}
	}
	return e
}

// getProjectDetails — vendor _getProjectDetails (W3).
func (d *Deps) getProjectDetails(ctx context.Context, projectID string) (map[string]any, error) {
	d = d.withDefaults()
	if d.FetchJson == nil {
		return nil, &RequestFailedError{Msg: "web api seam not wired"}
	}
	url := d.WebAPIURL + "/project/" + projectID + "/details"
	attempts := 0
	for {
		attempts++
		project, err := d.FetchJson(ctx, url, DetailsFetchTimeout)
		if err == nil {
			return project, nil
		}
		rfe, ok := err.(*RequestFailedError)
		if ok && rfe.Status == 404 {
			return nil, &NotFoundError{Msg: "got a 404 from web api", Cause: err}
		}
		if attempts < 2 {
			// vendor: retry after RETRY_TIMEOUT_MS (module-level, mutable)
			d.Sleep(RETRY_TIMEOUT_MS)
			continue
		}
		return nil, err
	}
}

func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case nil:
		return false
	case string:
		return t != ""
	default:
		return true
	}
}
