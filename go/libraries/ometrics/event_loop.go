package ometrics

import (
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
	go func() {
		for range ticker.C {
			now := NowMS()
			offset := now - previous - int64(interval)
			if int64(logThreshold) < offset {
				logger.Warn(map[string]any{"offset": offset}, "slow event loop")
			}
			previous = now
			recorder.Timing("event-loop-millsec", float64(offset), nil)
		}
	}()
	RegisterDestructor(func() { ticker.Stop() })
}
