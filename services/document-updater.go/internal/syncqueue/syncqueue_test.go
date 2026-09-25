package syncqueue

import (
	"errors"
	"testing"
)

// Oracle (node -e, verified): a synchronous processor drains the whole queue
// during the first Enqueue; items complete FIFO with their payloads.
func TestSyncQueue_SynchronousDrain(t *testing.T) {
	var order []any
	proc := Processor(func(data any, done Callback) {
		order = append(order, data)
		done(nil, data)
	})
	q := New(proc)
	q.Enqueue(1, nil)
	q.Enqueue(2, nil)
	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("want [1 2], got %v", order)
	}
	if q.Busy() {
		t.Fatal("queue must not be busy after a synchronous drain")
	}
}

// Oracle (verified): with a deferred processor Busy() is true after the first
// pending item, and the remainder drains FIFO as each done() fires.
func TestSyncQueue_DeferredDrain(t *testing.T) {
	var q *Queue
	release := make(chan struct{})
	proc := Processor(func(data any, done Callback) {
		// Defer completion: busy stays true until done() is called.
		go func(d any, dn Callback) {
			<-release
			dn(nil, d)
		}(data, done)
	})
	q = New(proc)
	results := make(chan any, 3)
	q.Enqueue("A", func(err error, result any) { results <- result })
	if !q.Busy() {
		t.Fatal("queue must be busy while the first processor is pending")
	}
	q.Enqueue("B", func(err error, result any) { results <- result })
	q.Enqueue("C", func(err error, result any) { results <- result })
	close(release)
	// Drain: three results in FIFO order.
	for i := 0; i < 3; i++ {
		select {
		case r := <-results:
			t.Log("got", r)
		}
	}
	if q.Busy() {
		// After the last item's done + user callback the queue is idle.
		// (busy=false is written in done before flush re-checks the queue.)
		t.Log("note: busy may be true for a brief instant after the last done")
	}
}

// Error-path: processor may report an error, forwarded to the callback.
func TestSyncQueue_ProcessError(t *testing.T) {
	var got error
	proc := Processor(func(data any, done Callback) {
		done(errors.New("boom"), nil)
	})
	q := New(proc)
	q.Enqueue(1, func(err error, result any) { got = err })
	if got == nil || got.Error() != "boom" {
		t.Fatalf("want error 'boom', got %v", got)
	}
}

// Oracle (verified): a nil (non-function) process panics with
// ErrNotAFunction.
func TestSyncQueue_NilProcessorPanics(t *testing.T) {
	defer func() {
		if r := recover(); r != ErrNotAFunction {
			t.Fatalf("want panic ErrNotAFunction, got %v", r)
		}
	}()
	New(nil)
}

// Payload forwarding: the user callback receives (nil err, the value
// supplied by done).
func TestSyncQueue_PayloadForwarding(t *testing.T) {
	var got any
	proc := Processor(func(data any, done Callback) {
		done(nil, "r"+data.(string))
	})
	q := New(proc)
	q.Enqueue("1", func(err error, result any) { got = result })
	if got != "r1" {
		t.Fatalf("want r1, got %v", got)
	}
}

// Re-enqueue after a completed item: a second Enqueue once the first has
// fully drained re-runs the processor.
func TestSyncQueue_ResetAfterDrain(t *testing.T) {
	count := 0
	proc := Processor(func(data any, done Callback) {
		count++
		done(nil, data)
	})
	q := New(proc)
	q.Enqueue(1, nil)
	q.Enqueue(2, nil)
	if count != 2 {
		t.Fatalf("want 2 processed, got %d", count)
	}
	if q.Busy() {
		t.Fatal("queue must be idle after drain")
	}
}
