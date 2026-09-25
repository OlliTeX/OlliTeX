// Package syncqueue is a port of app/js/sharejs/server/syncqueue.js
// (identical copy at app/js/sharejs/types/syncqueue.js; verified diff-clean).
//
// Node source (60 LOC):
//
//		module.exports = function (process) {
//		  if (typeof process !== 'function') throw new Error('process is not a function')
//		  const queue = []
//		  const enqueue = function (data, callback) { queue.push([data, callback]); return flush() }
//		  enqueue.busy = false
//		  function flush() {
//		    if (enqueue.busy || queue.length === 0) return
//		    enqueue.busy = true
//		    const [data, callback] = queue.shift()
//		    return process(data, function (...result) {
//		      enqueue.busy = false
//		      if (callback) callback.apply(null, result)
//		      return flush()
//		    })
//		  }
//		  return enqueue
//	}
//
// Go modelling: Node's `enqueue(data, callback)` with the varargs callback is
// modelled as Enqueue(data, Callback) where Callback is (err error, result
// any). The queue serialises process() invocations: at most one in flight at
// a time; Busy() mirrors the exported `enqueue.busy` flag, which the model
// reads (refreshReapingTimeout) and the deferred done path clears *before*
// the user callback runs.
package syncqueue

import "errors"

// ErrNotAFunction mirrors the Node `throw new Error('process is not a function')`.
var ErrNotAFunction = errors.New("process is not a function")

// Callback mirrors Node's `callback(...result)`. Node's first arg is
// error-or-null; the remainder are result values. The model only ever passes
// (errorString) or (null, version), so (err error, result any) covers the
// full app usage. result is nil unless the success path provides one.
type Callback func(err error, result any)

// Processor mirrors `process(data, callback)`. It MUST eventually call done
// for any data handed to it (Node: "process(data, callback) _MUST_
// eventually call its callback").
type Processor func(data any, done Callback)

// Queue mirrors the exported `enqueue` function object (a function carrying a
// `busy` property).
type Queue struct {
	process Processor
	queue   []item
	busy    bool
}

type item struct {
	data any
	cb   Callback
}

// New wraps process in a serial queue.
func New(process Processor) *Queue {
	if process == nil {
		panic(ErrNotAFunction)
	}
	return &Queue{process: process}
}

// Enqueue pushes data onto the queue and drains. With a synchronous
// processor this drains the entire queue during the call (FIFO); with a
// deferred processor, Busy() is true after the call and the remaining items
// drain as previous done() calls complete.
func (q *Queue) Enqueue(data any, cb Callback) {
	q.queue = append(q.queue, item{data, cb})
	q.flush()
}

// Busy mirrors the exported `enqueue.busy` flag: true while an item is being
// processed, false once done() returns.
func (q *Queue) Busy() bool { return q.busy }

func (q *Queue) flush() {
	if q.busy || len(q.queue) == 0 {
		return
	}
	q.busy = true
	it := q.queue[0]
	q.queue = q.queue[1:]
	// Node: busy=false is set before the user callback runs, then flush()
	// restarts the drain, so the user callback may observe Busy() either way
	// depending on when it reads. We mirror the write ordering exactly.
	done := func(err error, result any) {
		q.busy = false
		if it.cb != nil {
			it.cb(err, result)
		}
		q.flush()
	}
	q.process(it.data, done)
}
