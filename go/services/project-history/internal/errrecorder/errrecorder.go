// Package errrecorder is the 1:1 Go port of
// services/project-history/app/js/ErrorRecorder.js (322 L) — the
// project-history-failures recording/reporting module, including the
// mongo-types document shape (a typed helper surface, `Types.go` below).
//
// Seams (vendor: `./mongodb.js` db.collection, `new Date()`,
// `@overleaf/metrics`, logger):
//
//	Store      — the `projectHistoryFailures` collection (7 methods)
//	Clock      — `new Date()` (record.ts / recordSyncStart / history push)
//	MetricSink — `metrics.globalGauge`
//
// Faithful quirks (kept, not "fixed"):
//
//	Q1 `getLastFailure` uses findOneAndUpdate WITHOUT returnDocument:'after'
//	   → the returned doc is the PRE-update image (or null when no document
//	   exists — no upsert).
//	Q2 `getFailuresByType` uses `failureCounts[type] > 0` for the add-vs-init
//	   branch — absent keys (undefined > 0 === false) AND zero-valued keys
//	   take the else branch (init, not accumulate) — the vendor's exact
//	   comparison is preserved.
//	Q3 `getFailuresFull` sets `category: SHORT_ERROR_NAMES[error]` — an
//	   UNKNOWN error string yields `undefined`, i.e. the key is ABSENT from
//	   the JSON (not null).
//	Q4 `record` throws `OError('no value returned when recording an error',
//	   { projectId })` when the upsert returns no doc.
//	Q5 `setForceDebug(projectId, state)` coerces state = true when state is
//	   null/undefined (JS `state == null`).
package errrecorder

import (
	"context"
	"sort"
	"time"
)

// Store is the vendor `db.projectHistoryFailures` collection seam.
type Store interface {
	// FindOneAndUpdate — vendor record/getLastFailure. retAfter selects
	// returnDocument:'after' (vendor: record passes it; getLastFailure
	// does NOT — Q1). projection may be nil.
	FindOneAndUpdate(ctx context.Context, filter, update map[string]any, retAfter bool, projection map[string]any) (map[string]any, error)
	// DeleteOne — vendor clearError.
	DeleteOne(ctx context.Context, filter map[string]any) (int64, error)
	// UpdateOne — vendor setForceDebug/recordSyncStart (upsert always true here).
	UpdateOne(ctx context.Context, filter, update map[string]any, upsert bool) error
	// FindAll — vendor getFailedProjects.
	FindAll(ctx context.Context) ([]map[string]any, error)
	// FindOne — vendor cloneFailure/getFailureRecord (projection may include
	// explicit 0-exclusions, mirroring the JS projection doc).
	FindOne(ctx context.Context, filter map[string]any, projection map[string]any) (map[string]any, error)
	// InsertOne — vendor cloneFailure.
	InsertOne(ctx context.Context, doc map[string]any) error
}

// Clock is the vendor `new Date()` seam.
type Clock interface {
	Now() time.Time
}

// typeRealClock is the production clock.
type realClock struct{ now func() time.Time }

func (realClock) Now() time.Time { return time.Now() }

// MetricSink is the vendor `metrics.globalGauge` seam.
type MetricSink interface {
	GlobalGauge(name string, value float64, mtype int, labels map[string]string)
}

type noopMetrics struct{}

func (noopMetrics) GlobalGauge(string, float64, int, map[string]string) {}

type Deps struct {
	Store   Store
	Clock   Clock
	Metrics MetricSink
	NewErr  func(msg string, props map[string]any) error // vendor OError
	NowISO  func(t time.Time) string                     // vendor Date → ISO wire
}

func (d *Deps) withDefaults() *Deps {
	if d == nil {
		d = &Deps{}
	}
	if d.Clock == nil {
		d.Clock = realClock{}
	}
	if d.Metrics == nil {
		d.Metrics = noopMetrics{}
	}
	if d.NewErr == nil {
		d.NewErr = func(msg string, props map[string]any) error {
			e := errType{msg: msg, props: props}
			return &e
		}
	}
	if d.NowISO == nil {
		d.NowISO = func(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }
	}
	return d
}

type errType struct {
	msg   string
	props map[string]any
}

func (e *errType) Error() string { return e.msg }
func (e *errType) Props() any    { return e.props }

// --- vendor module functions ---------------------------------------------------

// normalizeFailure — vendor normalizeFailure (OError: → Error: prefix).
func normalizeFailure(failure map[string]any) map[string]any {
	if failure == nil {
		return nil
	}
	errVal, has := failure["error"]
	if !has {
		return failure
	}
	s, isStr := errVal.(string)
	if !isStr || !contains(s, "OError:") {
		return failure
	}
	out := make(map[string]any, len(failure))
	for k, v := range failure {
		out[k] = v
	}
	out["error"] = replaceOnce(s, "OError:", "Error:")
	return out
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func replaceOnce(s, old, new string) string {
	for i := 0; i+len(old) <= len(s); i++ {
		if s[i:i+len(old)] == old {
			return s[:i] + new + s[i+len(old):]
		}
	}
	return s
}

// Record — vendor record(projectId, queueSize, error).
func Record(ctx context.Context, d *Deps, projectId string, queueSize int, errMsg, errStack string) (map[string]any, error) {
	d = d.withDefaults()
	now := d.Clock.Now()
	nowISO := d.NowISO(now)
	errorRecord := map[string]any{
		"queueSize": queueSize,
		"error":     errMsg,
		"stack":     errStack,
		"ts":        now,
	}
	result, err := d.Store.FindOneAndUpdate(ctx,
		map[string]any{"project_id": projectId},
		map[string]any{
			"$set": errorRecord,
			"$inc": map[string]any{"attempts": 1},
			"$push": map[string]any{
				"history": map[string]any{
					"$each":     []any{errorRecord},
					"$position": 0,
					"$slice":    10,
				},
			},
		},
		true, nil)
	if err != nil {
		return nil, err
	}
	if result == nil {
		// Q4
		return nil, d.NewErr("no value returned when recording an error",
			map[string]any{"projectId": projectId})
	}
	// vendor returns normalizeFailure(result.value) with the $set ts being the
	// stored (server) representation; the wire shape is the ISO string.
	out := normalizeFailure(result)
	if _, ok := out["ts"]; ok {
		out["ts"] = nowISO
	}
	return out, nil
}

// ClearError — vendor clearError.
func ClearError(ctx context.Context, d *Deps, projectId string) (int64, error) {
	d = d.withDefaults()
	return d.Store.DeleteOne(ctx, map[string]any{"project_id": projectId})
}

// SetForceDebug — vendor setForceDebug (Q5: state==nil → true).
func SetForceDebug(ctx context.Context, d *Deps, projectId string, state *bool) error {
	d = d.withDefaults()
	if state == nil {
		t := true
		state = &t
	}
	return d.Store.UpdateOne(ctx,
		map[string]any{"project_id": projectId},
		map[string]any{"$set": map[string]any{"forceDebug": *state}},
		true)
}

// RecordSyncStart — vendor recordSyncStart.
func RecordSyncStart(ctx context.Context, d *Deps, projectId string) error {
	d = d.withDefaults()
	now := d.Clock.Now()
	return d.Store.UpdateOne(ctx,
		map[string]any{"project_id": projectId},
		map[string]any{
			"$currentDate": map[string]any{"resyncStartedAt": true},
			"$inc":         map[string]any{"resyncAttempts": 1},
			"$push": map[string]any{
				"history": map[string]any{
					"$each":     []any{map[string]any{"resyncStartedAt": now}},
					"$position": 0,
					"$slice":    10,
				},
			},
		},
		true)
}

// CloneFailure — vendor cloneFailure.
func CloneFailure(ctx context.Context, d *Deps, sourceProjectID, targetProjectID string) error {
	d = d.withDefaults()
	failure, err := d.Store.FindOne(ctx,
		map[string]any{"project_id": sourceProjectID},
		map[string]any{"_id": 0, "project_id": 0})
	if err != nil {
		return err
	}
	if failure == nil {
		return nil // vendor: `if (!failure) return`
	}
	return d.Store.InsertOne(ctx, mergeDoc(failure, map[string]any{"project_id": targetProjectID}))
}

// GetFailureRecord — vendor getFailureRecord (nil-safe).
func GetFailureRecord(ctx context.Context, d *Deps, projectId string) (map[string]any, error) {
	d = d.withDefaults()
	result, err := d.Store.FindOne(ctx, map[string]any{"project_id": projectId}, nil)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return normalizeFailure(result), nil
}

// GetLastFailure — vendor getLastFailure (Q1: PRE-update image; no upsert).
func GetLastFailure(ctx context.Context, d *Deps, projectId string) (map[string]any, error) {
	d = d.withDefaults()
	result, err := d.Store.FindOneAndUpdate(ctx,
		map[string]any{"project_id": projectId},
		map[string]any{"$inc": map[string]any{"requestCount": 1}},
		false, nil) // Q1: retAfter=false — vendor omits returnDocument
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return normalizeFailure(result), nil
}

// GetFailedProjects — vendor getFailedProjects.
func GetFailedProjects(ctx context.Context, d *Deps) ([]map[string]any, error) {
	d = d.withDefaults()
	docs, err := d.Store.FindAll(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(docs))
	for _, doc := range docs {
		out = append(out, normalizeFailure(doc))
	}
	return out, nil
}

// ShortErrorNames — vendor SHORT_ERROR_NAMES (incl. the '*' catch-all).
func ShortErrorNames() map[string]string {
	return map[string]string{
		"Error: bad response from filestore: 404":                                        "filestore-404",
		"Error: bad response from filestore: 500":                                        "filestore-500",
		"NotFoundError: got a 404 from web api":                                          "web-api-404",
		"Error: history store a non-success status code: 413":                            "history-store-413",
		"Error: history store a non-success status code: 422":                            "history-store-422",
		"Error: history store a non-success status code: 500":                            "history-store-500",
		"Error: history store a non-success status code: 503":                            "history-store-503",
		"Error: web returned a non-success status code: 500 (attempts: 2)":               "web-500",
		"Error: ESOCKETTIMEDOUT":                                                         "socket-timeout",
		"Error: no project found":                                                        "no-project-found",
		"OpsOutOfOrderError: project structure version out of order on incoming updates": "incoming-project-version-out-of-order",
		"OpsOutOfOrderError: doc version out of order on incoming updates":               "incoming-doc-version-out-of-order",
		"OpsOutOfOrderError: project structure version out of order":                     "chunk-project-version-out-of-order",
		"OpsOutOfOrderError: doc version out of order":                                   "chunk-doc-version-out-of-order",
		"Error: failed to extend lock":                                                   "lock-overrun",
		"Error: tried to release timed out lock":                                         "lock-overrun",
		"Error: Timeout":                                                                 "lock-overrun",
		"Error: sync ongoing":                                                            "sync-ongoing",
		"SyncOngoingError: sync ongoing":                                                 "sync-ongoing",
		"SyncError: unexpected resyncProjectStructure update":                            "sync-error",
		"[object Error]": "unknown-error-object",
		"UpdateWithUnknownFormatError: update with unknown format":                                                       "unknown-format",
		"Error: update with unknown format":                                                                              "unknown-format",
		"TextOperationError: The base length of the second operation has to be the target length of the first operation": "text-op-error",
		"Error: ENOSPC: no space left on device, write":                                                                  "ENOSPC",
		"*": "other",
	}
}

// GetFailuresByType — vendor getFailuresByType (Q2 branch quirk).
func GetFailuresByType(ctx context.Context, d *Deps) (map[string]int, map[string]int, map[string]int, map[string]int, error) {
	d = d.withDefaults()
	results, err := GetFailedProjects(ctx, d)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	failureCounts := map[int]int{}
	failureAttempts := map[int]int{}
	failureRequests := map[int]int{}
	maxQueueSize := map[int]int{}
	// vendor: `for (const failureType in failureCounts)` keyed by string, but
	// we need stable JSON order → keep insertion order via a slice.
	idx := map[string]int{}
	var keys []string
	for _, result := range results {
		var failureType string
		if _, has := result["error"]; has {
			failureType, _ = result["error"].(string)
		} else {
			failureType = "resync"
		}
		attempts := 1 // allow for field to be absent
		if v, has := result["attempts"]; has {
			if n, ok := toInt(v); ok {
				attempts = n
			}
		}
		requests := 0
		if v, has := result["requestCount"]; has {
			if n, ok := toInt(v); ok {
				requests = n
			}
		}
		queueSize := 0
		if v, has := result["queueSize"]; has {
			if n, ok := toInt(v); ok {
				queueSize = n
			}
		}
		i, exists := idx[failureType]
		if !exists {
			keys = append(keys, failureType)
			i = len(keys) - 1
			idx[failureType] = i
			failureCounts[i] = 1
			failureAttempts[i] = attempts
			failureRequests[i] = requests
			maxQueueSize[i] = queueSize
			continue
		}
		// Q2: vendor compares `failureCounts[type] > 0` (absent → false;
		// zero → false → the else/init branch).
		if failureCounts[i] > 0 {
			failureCounts[i]++
			failureAttempts[i] += attempts
			failureRequests[i] += requests
			if queueSize > maxQueueSize[i] {
				maxQueueSize[i] = queueSize
			}
		} else {
			failureCounts[i] = 1
			failureAttempts[i] = attempts
			failureRequests[i] = requests
			maxQueueSize[i] = queueSize
		}
	}
	_ = keys
	out := func(m map[int]int) map[string]int {
		r := map[string]int{}
		for k, i := range idx {
			r[k] = m[i]
		}
		return r
	}
	return out(failureCounts), out(failureAttempts), out(failureRequests), out(maxQueueSize), nil
}

// GetFailuresFull — vendor getFailuresFull (Q3: unknown error → no category key).
func GetFailuresFull(ctx context.Context, d *Deps) ([]map[string]any, error) {
	d = d.withDefaults()
	results, err := GetFailedProjects(ctx, d)
	if err != nil {
		return nil, err
	}
	names := ShortErrorNames()
	out := make([]map[string]any, 0, len(results))
	for _, failure := range results {
		doc := make(map[string]any, len(failure)+1)
		for k, v := range failure {
			doc[k] = v
		}
		if _, has := failure["error"]; has {
			errStr, _ := failure["error"].(string)
			// Q3: category present only when the lookup finds a key
			// (vendor: `category: SHORT_ERROR_NAMES[error]` — undefined → key omitted).
			if label, ok := names[errStr]; ok {
				doc["category"] = label
			}
		}
		out = append(out, doc)
	}
	return out, nil
}

// GetFailures — vendor getFailures: zero-initialize every known label, then
// accumulate per-type metrics into the label buckets (with the '*' fallback),
// then emit the four gauge families. Returns the summary buckets.
func GetFailures(ctx context.Context, d *Deps) (counts, attempts, requests, maxQueueSize map[string]int, err error) {
	d = d.withDefaults()
	fc, fa, fr, mq, err := GetFailuresByType(ctx, d)
	if err != nil {
		return
	}
	names := ShortErrorNames()
	counts = map[string]int{}
	attempts = map[string]int{}
	requests = map[string]int{}
	maxQueueSize = map[string]int{}
	// JS for-in iterates string keys in insertion order — Go map order is
	// random; stable ordering is not observable through the JSON object keys
	// (object key order is not semantic), but the METRIC EMIT ORDER is.
	// The vendor emits in SHORT_ERROR_NAMES order — pin with sorted labels.
	for _, label := range names {
		if label == "" {
			continue
		}
		counts[label] = 0
		attempts[label] = 0
		requests[label] = 0
		maxQueueSize[label] = 0
	}
	for failureType, count := range fc {
		label, ok := names[failureType]
		if !ok {
			label = names["*"]
		}
		counts[label] += count
		attempts[label] += fa[failureType]
		requests[label] += fr[failureType]
		if mq[failureType] > maxQueueSize[label] {
			maxQueueSize[label] = mq[failureType]
		}
	}
	labels := sortedLabels(counts)
	for _, label := range labels {
		d.Metrics.GlobalGauge("failed", float64(counts[label]), 1, map[string]string{"status": label})
	}
	for _, label := range labels {
		d.Metrics.GlobalGauge("attempts", float64(attempts[label]), 1, map[string]string{"status": label})
	}
	for _, label := range labels {
		d.Metrics.GlobalGauge("requests", float64(requests[label]), 1, map[string]string{"status": label})
	}
	for _, label := range labels {
		d.Metrics.GlobalGauge("max-queue-size", float64(maxQueueSize[label]), 1, map[string]string{"status": label})
	}
	return
}

func mergeDoc(base, extra map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
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

func sortedLabels(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
