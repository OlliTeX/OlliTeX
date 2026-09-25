// Package ratelimit — 1:1 Go port of `app/js/RateLimitManager.js`
// (overleaf/document-updater).
//
// Node shape (1:1):
//
//	module.exports = class RateLimiter {
//	  constructor(number) { this.ActiveWorkerCount=0; this.CurrentWorkerLimit=number;
//	    this.BaseWorkerCount=number }
//	  _adjustLimitUp()  — CurrentWorkerLimit += 0.1; Metrics.gauge('currentLimit', ceil)
//	  _adjustLimitDown()— CurrentWorkerLimit = max(Base, limit*0.9); gauge
//	  _trackAndRun(task, cb) — ActiveWorkerCount++; gauge; task(err => { --; gauge; cb(err) })
//	  run(task, cb) — below limit: background + immediate cb, optional down-adjust;
//	    else: synchronous + up-adjust on success
//	}
//
// Hermetic note: in Node a task defers its completion callback to a later
// tick (setTimeout). The Go `Task` mirrors that contract — the task invokes
// `done` when it completes — and the tests drive completion explicitly, so
// every observable transition (ActiveWorkerCount, the current-limit gauge)
// is deterministic.
package ratelimit

import "math"

// Task mirrors the Node `task` — a function that takes a callback the task
// invokes when it completes (with an error, or nil for success).
type Task func(done func(error))

// Gauge is the subset of @overleaf/metrics used here (Metrics.gauge).
type Gauge interface {
	Gauge(name string, value int)
}

type nullGauge struct{}

func (nullGauge) Gauge(string, int) {}

// RateLimiter mirrors the Node RateLimiter class.
type RateLimiter struct {
	ActiveWorkerCount  int
	CurrentWorkerLimit float64
	BaseWorkerCount    int
	Metrics            Gauge
}

// New mirrors `new RateLimitManager(number)`. Node's `number == null → 10`
// default is expressed by NewDefault; any value is used as the worker limit.
func New(number int) *RateLimiter {
	if number <= 0 {
		number = 10
	}
	return &RateLimiter{
		CurrentWorkerLimit: float64(number),
		BaseWorkerCount:    number,
		Metrics:            nullGauge{},
	}
}

// NewDefault mirrors `new RateLimitManager()` (number == null → 10).
func NewDefault() *RateLimiter { return New(10) }

func (rl *RateLimiter) gauge(name string, value int) {
	if rl.Metrics != nil {
		rl.Metrics.Gauge(name, value)
	}
}

func (rl *RateLimiter) adjustLimitUp() {
	rl.CurrentWorkerLimit += 0.1
	rl.gauge("currentLimit", int(math.Ceil(rl.CurrentWorkerLimit)))
}

func (rl *RateLimiter) adjustLimitDown() {
	rl.CurrentWorkerLimit = math.Max(float64(rl.BaseWorkerCount), rl.CurrentWorkerLimit*0.9)
	rl.gauge("currentLimit", int(math.Ceil(rl.CurrentWorkerLimit)))
}

// trackAndRun mirrors Node `_trackAndRun` — active-worker counter + gauge
// around the task lifetime.
func (rl *RateLimiter) trackAndRun(task Task, callback func(error)) {
	rl.ActiveWorkerCount++
	rl.gauge("processingUpdates", rl.ActiveWorkerCount)
	task(func(err error) {
		rl.ActiveWorkerCount--
		rl.gauge("processingUpdates", rl.ActiveWorkerCount)
		callback(err)
	})
}

// Run mirrors Node `run(task, callback)`.
//
// Below the worker limit the task is run in the background: the caller's
// callback fires immediately and a (successful) completion just logs. At or
// over the limit the run blocks on the task; a successful completion raises
// the limit so more parallelism is admitted over time.
func (rl *RateLimiter) Run(task Task, callback func(error)) {
	if float64(rl.ActiveWorkerCount) < rl.CurrentWorkerLimit {
		rl.trackAndRun(task, func(error) {
			// Node logs the error here and calls a no-op callback.
		})
		callback(nil)
		if rl.CurrentWorkerLimit > float64(rl.BaseWorkerCount) {
			rl.adjustLimitDown()
		}
		return
	}
	rl.trackAndRun(task, func(err error) {
		if err == nil {
			rl.adjustLimitUp()
		}
		callback(err)
	})
}
