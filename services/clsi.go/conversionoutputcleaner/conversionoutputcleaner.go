// Package conversionoutputcleaner ports services/clsi/app/js/ConversionOutputCleaner.js.
//
// Node parity:
//
//   - TTL_MS = 60 * 1000 (60 seconds), exported as a module constant.
//   - scheduleCleanup(conversionId[, ttlMs]) schedules fs.rm(path.join(outputDir,
//     conversionId), {recursive, force}) after ttlMs (default TTL_MS). All
//     removal errors are warn-logged and swallowed.
//
// Port: the timer is routed through ScheduleAfter (default: time.AfterFunc)
// so tests can substitute a deterministic fake (mirroring Node's
// sinon.useFakeTimers + clock.tick(TTL_MS)).
package conversionoutputcleaner

import (
	"os"
	"path"
	"time"
)

// TTL_MS is the default schedule-cleanup delay in milliseconds (Node: TTL_MS).
const TTL_MS = 60 * 1000

// defaultScheduleAfter is the production timer default (mirrors time.AfterFunc).
// A named function so tests can call it directly to cover its body rather than
// relying on the package-init closure (which earlier tests replace).
func defaultScheduleAfter(delayMS int, f func()) {
	time.AfterFunc(time.Duration(delayMS)*time.Millisecond, f)
}

// ScheduleAfter is the injectable timer. Tests override it with a capturing
// fake (Node parity: fake timers). Default: defaultScheduleAfter.
var ScheduleAfter = defaultScheduleAfter

// defaultWarnLog is the production no-op warn sink.
func defaultWarnLog(err error, dir string) {
	_ = err
	_ = dir
}

// WarnLog is the injectable warn sink for cleanup errors (Node: logger.warn in
// the setTimeout catch — swallowed after logging). Default: no-op; tests
// substitute a capturing fake and assert on the call.
var WarnLog = defaultWarnLog

// ScheduleCleanup ports scheduleCleanup(conversionId, ttlMs=60000).
// It arms a one-shot removal of <outputDir>/<conversionId> after ttlMs.
// Removal errors are warn-logged and swallowed.
func ScheduleCleanup(outputDir, conversionId string, ttlMs ...int) {
	ttl := TTL_MS
	if len(ttlMs) > 0 {
		ttl = ttlMs[0]
	}
	dir := path.Join(outputDir, conversionId)
	ScheduleAfter(ttl, func() {
		if err := RemoveOutputDir(dir); err != nil {
			// Node: logger.warn({err, conversionId, conversionOutputDir},
			//       'failed to clean up conversion output directory')
			WarnLog(err, dir)
		}
	})
}

// removeFunc is the injectable removal primitive (default os.RemoveAll).
var removeFunc = os.RemoveAll

// RemoveOutputDir ports the fs.rm(...) call inside the setTimeout callback:
// recursively remove the directory, tolerate "already gone" (force: true).
func RemoveOutputDir(dir string) error {
	err := removeFunc(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err // Node: caught and warn-logged by ScheduleCleanup's closure
	}
	return nil
}
