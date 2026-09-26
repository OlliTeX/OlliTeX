package ometrics

import (
	"sync"
	"time"
)

// WarnLogger is the narrow logger the event_loop monitor drives (Node:
// `logger.warn({offset}, 'slow event loop')`).
type WarnLogger interface {
	Warn(info map[string]any, msg string, args ...any)
}

// Monitor mirrors event_loop.monitor: on each interval tick, measures the
// event-loop delay (now - previous - interval), warns when it exceeds the
// threshold, records `event-loop-millsec`, and registers a destructor that
// stops the interval. With a nil logger it panics `logger is undefined`
// (Node: `throw new Error('logger is undefined')`).
func EventLoopMonitor(logger WarnLogger, interval, logThreshold int) {
	if logger == nil {
		panic("logger is undefined")
	}
	if interval == 0 {
		interval = 1000
	}
	if logThreshold == 0 {
		logThreshold = 100
	}
	previous := NowMS()
	ticker := time.NewTicker(time.Duration(interval) * time.Millisecond)
	stopped := make(chan struct{})
	loopWait.Add(1)
	go func() {
		defer loopWait.Done()
		// select-based exit: Go 1.27 no longer wakes a `range` receiver
		// parked on a stopped Ticker (empirically verified with a minimal
		// repro), so the destructor signals via the stop channel.
		for {
			select {
			case <-stopped:
				return
			case tick, ok := <-ticker.C:
				if !ok {
					return
				}
				now := NowMS()
				offset := now - previous - int64(interval)
				_ = tick
				if int64(logThreshold) < offset {
					logger.Warn(map[string]any{"offset": offset}, "slow event loop")
				}
				previous = now
				recorder.Timing("event-loop-millsec", float64(offset), nil)
			}
		}
	}()
	RegisterDestructor(func() { close(stopped); ticker.Stop() })
}

// loopWait guards every started monitor tick-goroutine: tests (and shutdown
// paths) can Wait on it to guarantee a tick has fully drained before touching
// the package globals the loop reads (recorder / NowMS) — a fixed sleep does
// not provide that guarantee under -race scheduling.
var loopWait sync.WaitGroup

// WaitLoopMonitors blocks until every started event-loop ticker goroutine has
// exited (called AFTER its destructor/ticker.Stop()).
func WaitLoopMonitors() { loopWait.Wait() }
