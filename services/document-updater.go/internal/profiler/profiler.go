// Package profiler is a port of app/js/Profiler.js (46 LOC).
//
// Node source:
//
//	class Profiler {
//	  LOG_CUTOFF_TIME = 15 * 1000
//	  LOG_SYNC_CUTOFF_TIME = 1000
//	  constructor(name, args) { t0 = t = hrtime; start = new Date; updateTimes = []; totalSyncTime = 0 }
//	  log(label, options = {}) { t1 = hrtime; dtMilliSec = delta(t1, t); t = t1; totalSyncTime += options.sync ? dt : 0; updateTimes.push([label, dt]); return this }
//	  end() {
//	    totalTime = delta(t, t0)
//	    exceedsCutoff = totalTime > LOG_CUTOFF_TIME
//	    exceedsSyncCutoff = totalSyncTime > LOG_SYNC_CUTOFF_TIME
//	    if (either) logger.warn({...args, updateTimes, start, end, status}, name)
//	    return totalTime
//	  }
//	}
//
// Go modelling: process.hrtime([sec, ns]) -> unix-nanos. logger.warn ->
// package-level Warn hook (no-op by default; the model sets it for tests /
// live logging). Timings are floor-truncated millis, exactly like
// Math.floor(nanoseconds * 1e-6).
package profiler

import (
	"fmt"
	"time"
)

// Warn mirrors logger.warn(args, name). args includes the profiler's update
// times, start/end and status map on cutoff exceedance. nil by default.
var Warn func(args map[string]any, name string)

// LogCutoffTime / LogSyncCutoffTime: 15s / 1s, per LOG_CUTOFF_TIME /
// LOG_SYNC_CUTOFF_TIME.
const (
	LogCutoffTime     = 15 * 1000
	LogSyncCutoffTime = 1000
)

// Profiler mirrors the Node class. Construction + Log are cheap; End returns
// total milliseconds.
type Profiler struct {
	Name string

	t0, t int64 // unix nanos (hrtime equivalent)

	start       time.Time
	updateTimes [][2]any // [label, ms]
	totalSyncMs int64
}

// New mirrors constructor(name, args). args is dropped because the Go model
// passes no args; the Node signature is Profiler(name).
func New(name string) *Profiler {
	return &Profiler{Name: name, t0: nowNano(), t: nowNano(), start: time.Now()}
}

func (p *Profiler) now() int64 { return nowNano() }

// deltaMs floors (ta - tb) nanos to integer millis, mirroring
// Math.floor(nanoSeconds * 1e-6). (ta, tb) are unix-nano values.
func deltaMs(ta, tb int64) int64 {
	return (ta - tb) / 1_000_000
}

// Log mirrors log(label, {sync:...}). Chainable.
func (p *Profiler) Log(label string, sync bool) *Profiler {
	now := p.now()
	dt := deltaMs(now, p.t)
	p.t = now
	if sync {
		p.totalSyncMs += dt
	}
	p.updateTimes = append(p.updateTimes, [2]any{label, dt})
	return p
}

// End mirrors end(): logs a warn if any cutoff exceeded and returns total
// whole-run milliseconds.
func (p *Profiler) End() int64 {
	totalTime := deltaMs(p.t, p.t0)
	exceedsCutoff := totalTime > LogCutoffTime
	exceedsSyncCutoff := p.totalSyncMs > LogSyncCutoffTime
	if exceedsCutoff || exceedsSyncCutoff {
		if Warn != nil {
			Warn(map[string]any{
				"updateTimes":       p.updateTimes,
				"start":             p.start.Format(time.RFC3339),
				"end":               time.Now().Format(time.RFC3339),
				"exceedsCutoff":     exceedsCutoff,
				"exceedsSyncCutoff": exceedsSyncCutoff,
			}, p.Name)
		}
	}
	return totalTime
}

// nowNano mirrors process.hrtime() -> [sec, ns] flattened to unix nanos. A
// hook so tests can force a known clock.
var nowNano = func() int64 { return time.Now().UnixNano() }

// String keeps the profiler printable in debug output without pulling in
// log/slog.
func (p *Profiler) String() string {
	return fmt.Sprintf("Profiler(%s, %dms)", p.Name, deltaMs(p.t, p.t0))
}
