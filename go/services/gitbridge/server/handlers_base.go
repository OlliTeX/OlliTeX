// handlers_base.go — the Java base-context handlers live inline in
// shared.go (the dispatch step chain); this file keeps the /metrics
// content builder, which is the "no new Go dependencies" variant
// (HANDOFF §19.8): Go runtime stats in Prometheus text format.

package server

import (
	"runtime"
)

// runtimeMetrics emits the standard go-runtime gauges in Prometheus text
// format (no external library — HANDOFF §19.8: minimal, no new deps).
// Java emits the JVM registry + hotspot exporter; the metric names differ
// (JVM vs Go runtime), which the live diff documents.
func runtimeMetrics() string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return "# HELP go_mem_alloc_bytes Memory, as allocated by Go.\n" +
		"# TYPE go_mem_alloc_bytes gauge\n" +
		"go_mem_alloc_bytes " + fmtUint(m.Alloc) + "\n" +
		"# HELP go_mem_sys_bytes Total memory obtained from the OS.\n" +
		"# TYPE go_mem_sys_bytes gauge\n" +
		"go_mem_sys_bytes " + fmtUint(m.Sys) + "\n" +
		"# HELP go_mem_heap_alloc_bytes Heap bytes allocated by Go.\n" +
		"# TYPE go_mem_heap_alloc_bytes gauge\n" +
		"go_mem_heap_alloc_bytes " + fmtUint(m.HeapAlloc) + "\n" +
		"# HELP go_mem_heap_sys_bytes Total heap bytes as seen by the OS.\n" +
		"# TYPE go_mem_heap_sys_bytes gauge\n" +
		"go_mem_heap_sys_bytes " + fmtUint(m.HeapSys) + "\n" +
		"# HELP go_mem_stack_bytes Bytes of stack used by Go.\n" +
		"# TYPE go_mem_stack_bytes gauge\n" +
		"go_mem_stack_bytes " + fmtUint(m.StackSys) + "\n" +
		"# HELP go_cpu_count Number of logical CPUs running in the user namespace.\n" +
		"# TYPE go_cpu_count gauge\n" +
		"go_cpu_count " + fmtUint(uint64(runtime.NumCPU())) + "\n"
}

// fmtUint is a tiny uint64→string with no strconv dependency (the value is
// used only in test assertions, the string is emitted to /metrics).
func fmtUint(v uint64) string {
	if v == 0 {
		return "0"
	}
	const digits = "0123456789"
	buf := make([]byte, 0, 20)
	for v > 0 {
		buf = append(buf, digits[v%10])
		v /= 10
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}
