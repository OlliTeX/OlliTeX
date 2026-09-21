package mongoutils

import (
	"os"
	"strconv"
	"sync"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// module-scope batched-update state (Node batchedUpdate.js let-variables).
// Node is single-threaded so the plain module vars are safe there; Go gets
// the mutex too (the single-flight flag is the main safety).
var (
	batchStateMu         sync.Mutex
	BatchDescending      bool
	BatchSize            int
	VerboseLogging       bool
	BatchLastID          primitive.ObjectID // (Node: set via options only)
	BatchRangeStart      primitive.ObjectID
	BatchRangeEnd        primitive.ObjectID
	BatchMaxTimeSpanMs   int64
	BatchedUpdateRunning bool
	ideEdgePast          primitive.ObjectID
	hasIdeEdgePast       bool
)

// readPreferenceSecondary mirrors the Node module-scope selection, computed
// ONCE at module load (init in Go — Node evaluates it when the module is
// first imported):
//
//	const READ_PREFERENCE_SECONDARY =
//	  process.env.MONGO_HAS_SECONDARIES === 'true'
//	    ? ReadPreference.secondary.mode
//	    : ReadPreference.secondaryPreferred.mode
var readPreferenceSecondary = func() string {
	if os.Getenv("MONGO_HAS_SECONDARIES") == "true" {
		return "secondary"
	}
	return "secondaryPreferred"
}()

// BatchedUpdateOptions mirrors the Node BatchedUpdateOptions typedef (string
// fields — the Node API is string-based, `BATCH_DESCENDING === 'true'`).
// TrackProgress replaces Node's `trackProgress(progress)` callback.
type BatchedUpdateOptions struct {
	BatchDescending    string // 'true' → descend
	BatchLastID        string // takes precedence over BatchRangeStart
	BatchRangeStart    string
	BatchRangeEnd      string
	BatchSize          string // decimal string, default "1000"
	BatchMaxTimeSpanMs string // decimal string, default ONE_MONTH_IN_MS
	VerboseLogging     string // 'true'
	TrackProgress      func(progress string)
}

// refreshGlobalOptionsForBatchedUpdate mirrors the Node function 1:1,
// including the env merge (`Object.assign({}, options, process.env)` — env
// wins over the explicit options).
func refreshGlobalOptionsForBatchedUpdate(o BatchedUpdateOptions) {
	batchStateMu.Lock()
	defer batchStateMu.Unlock()

	// Object.assign({}, options, process.env): start from the explicit
	// options, let the environment override (Node: process.env entries win).
	val := func(key string, explicit string) string {
		if env, ok := os.LookupEnv(key); ok {
			return env
		}
		return explicit
	}

	BatchDescending = val("BATCH_DESCENDING", o.BatchDescending) == "true"
	BatchSize = parseIntOr(val("BATCH_SIZE", o.BatchSize), 1000)
	VerboseLogging = val("VERBOSE_LOGGING", o.VerboseLogging) == "true"

	if last := val("BATCH_LAST_ID", o.BatchLastID); last != "" {
		BatchRangeStart, _ = ObjectIdFromInput(last)
	} else if start := val("BATCH_RANGE_START", o.BatchRangeStart); start != "" {
		BatchRangeStart, _ = ObjectIdFromInput(start)
	} else {
		if BatchDescending {
			BatchRangeStart = IEdgeFuture
		} else {
			BatchRangeStart = edgePastIDLocked()
		}
	}

	BatchMaxTimeSpanMs = int64(parseIntOr(val("BATCH_MAX_TIME_SPAN_IN_MS", o.BatchMaxTimeSpanMs), ONE_MONTH_IN_MS))

	if end := val("BATCH_RANGE_END", o.BatchRangeEnd); end != "" {
		BatchRangeEnd, _ = ObjectIdFromInput(end)
	} else {
		if BatchDescending {
			BatchRangeEnd = edgePastIDLocked()
		} else {
			BatchRangeEnd = IEdgeFuture
		}
	}
}

func parseIntOr(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n
	}
	return def
}

// edgePastIDLocked — caller holds batchStateMu (the only caller is
// refreshGlobalOptionsForBatchedUpdate, which runs under it).
func edgePastIDLocked() primitive.ObjectID {
	if hasIdeEdgePast {
		return ideEdgePast
	}
	return primitive.ObjectID{}
}
