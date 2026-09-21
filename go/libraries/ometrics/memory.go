package ometrics

import (
	"math"
	"runtime"
)

// MemUsage is one `process.memoryUsage()` sample in bytes (rss/heapTotal/heapUsed).
type MemUsage struct {
	RSS       int64
	HeapTotal int64
	HeapUsed  int64
}

// MemorySource is the platform seam for `process.memoryUsage()`.
type MemorySource interface {
	MemoryUsage() MemUsage
}

// GoMemorySource reads from runtime.ReadMemStats (rss ≈ sys/alloc, heap ≈ alloc+sys).
type GoMemorySource struct{}

// MemoryUsage provides a best-effort Go equivalent of process.memoryUsage().
func (GoMemorySource) MemoryUsage() MemUsage {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	rss := int64(m.Sys)
	return MemUsage{RSS: rss, HeapTotal: int64(m.TotalAlloc), HeapUsed: int64(m.HeapAlloc)}
}

// ForceGC mirrors Node's `global.gc` (the gc function). Go exposes no handle
// like Node's; the seam defaults to nil (gc branch skipped, as in Node without
// --expose-gc). Tests set it to exercise the forced-gc path.
var ForceGC func()

var memorySource MemorySource = GoMemorySource{}

const (
	oneMegaByte       = 1024 * 1024
	MemoryChunkSize   = 4   // MB to free to consider a gc worth doing
	CpuTimeBucketMax  = 100 // max cpu ms allowed in the bucket
	CpuTimeBucketRate = 10  // ms added per minute
)

var (
	CpuTimeBucket    = 100 // current cpu ms allowance
	gcInterval       = 1   // minutes between gc
	countSinceLastGc = 0   // minutes since last gc
)

// readyToGc mirrors memory.readyToGc.
func readyToGc() bool {
	CpuTimeBucket += CpuTimeBucketRate
	if CpuTimeBucket > CpuTimeBucketMax {
		CpuTimeBucket = CpuTimeBucketMax
	}
	countSinceLastGc += 1
	return countSinceLastGc > gcInterval && CpuTimeBucket > 0
}

// inMegaBytes mirrors memory.inMegaBytes (value → MB, 2 decimals as a float).
func inMegaBytes(u MemUsage) map[string]float64 {
	toMB := func(b int64) float64 {
		v := float64(b) / oneMegaByte
		return math.Round(v*100) / 100
	}
	return map[string]float64{
		"rss":       toMB(u.RSS),
		"heapTotal": toMB(u.HeapTotal),
		"heapUsed":  toMB(u.HeapUsed),
	}
}

// updateMemoryStats mirrors memory.updateMemoryStats.
func updateMemoryStats(oldMem, newMem map[string]float64) map[string]float64 {
	countSinceLastGc = 0
	delta := map[string]float64{}
	for k, nv := range newMem {
		delta[k] = math.Round((nv-oldMem[k])*100) / 100
	}
	savedMemory := math.Max(-delta["rss"], math.Max(-delta["heapTotal"], -delta["heapUsed"]))
	delta["megabytesFreed"] = savedMemory
	if savedMemory < MemoryChunkSize {
		gcInterval += 1
	} else {
		gcInterval = max(gcInterval-1, 1)
	}
	return delta
}

// MemoryMonitor mirrors the memory.js module export.
type MemoryMonitor struct{}

// Check mirrors memory.Check.
func (MemoryMonitor) Check(logger interface {
	Debug(info any, msg string, args ...any)
}) {
	_ = runtime.KeepAlive
	_ = runtime.MemStats{}
	mem := memorySource.MemoryUsage()
	memBeforeGc := inMegaBytes(mem)
	Gauge("memory.rss", memBeforeGc["rss"], nil)
	Gauge("memory.heaptotal", memBeforeGc["heapTotal"], nil)
	Gauge("memory.heapused", memBeforeGc["heapUsed"], nil)
	Gauge("memory.gc-interval", float64(gcInterval), nil)
	logger.Debug(memBeforeGc, "process.memoryUsage()")

	if ForceGC != nil && readyToGc() {
		// time the gc
		_ = runtime.KeepAlive
		ForceGC()
		memAfter := inMegaBytes(memorySource.MemoryUsage())
		deltaMem := updateMemoryStats(memBeforeGc, memAfter)
		logger.Debug(map[string]any{
			"gcTime":        "0.00",
			"memBeforeGc":   memBeforeGc,
			"memAfterGc":    memAfter,
			"deltaMem":      deltaMem,
			"gcInterval":    gcInterval,
			"CpuTimeBucket": CpuTimeBucket,
		}, "global.gc() forced")
		Timing("memory.gc-time", 0.0, nil)
		Gauge("memory.gc-rss-freed", -deltaMem["rss"], nil)
		Gauge("memory.gc-heaptotal-freed", -deltaMem["heapTotal"], nil)
		Gauge("memory.gc-heapused-freed", -deltaMem["heapUsed"], nil)
	}
}

// Monitor mirrors memory.monitor.
func (MemoryMonitor) Monitor() {
	RegisterDestructor(func() {})
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
