package projecthistoryredis

// projecthistoryredis.go — hermetic port of the vendored
// `app/js/ProjectHistoryRedisManager.js` (project-history Redis manager).
//
// Every public method JSON-stringifies a project-update wire object and
// pushes it onto the per-project Redis project-history ops list, with a
// SETNX oldest-op timestamp and (resync doc content) a doc-size guard.
//
//	vendor  app/js/ProjectHistoryRedisManager.js
//	oracle  test/unit/js/ProjectHistoryRedisManager/ProjectHistoryRedisManagerTests.js
//
// Hermetic seam design (mirroring the oracle stubs):
//   - `Client` — the rclient surface (Multi() -> Tx: RPush, SetNx, Exec).
//   - `Metrics` — the vendored metrics.summary (oracle sinon stub).
//   - `DocIsTooLarge` / `StringFileDataContentIsTooLarge` — the vendored
//     Limits (oracle stubs the module; nil defaults use the committed
//     limits port = SandboxedModule real-require).
//   - `ToHistoryRanges` / `AddTrackedDeletesToContent` — the vendored
//     HistoryConversions / Utils (nil defaults = the committed ports).
//   - `Now` — the wall (ms since epoch), the vendored `Date.now()` /
//     `new Date()` (timekeeper.freeze in the oracle).
//   - `QueueOpsSeam` — mirrors the oracle's `promises.queueOps = stub()`.

import (
	"time"

	"document-updater/internal/historyconversions"
	"document-updater/internal/limits"
	"document-updater/internal/utils"
)

// Keys mirrors `Settings.redis.project_history.key_schema`
// (the vendored projectHistoryKeys).
type Keys struct {
	// ProjectHistoryOps — the per-project project-history ops list
	// (`ProjectHistory:Ops:<project>`).
	ProjectHistoryOps func(projectID string) string
	// ProjectHistoryFirstOpTimestamp — the per-project oldest-op age
	// timestamp (SETNX on the first op pushed to the list).
	ProjectHistoryFirstOpTimestamp func(projectID string) string
}

// Tx mirrors `rclient.multi()` — a redis MULTI: commands queue in call
// order, Exec surfaces the exec replies.
type Tx interface {
	// RPush — `multi.rpush(key, ...values)`.
	RPush(key string, values ...string) Tx
	// SetNx — `multi.setnx(key, value)`.
	SetNx(key string, value any) Tx
	// Exec — `multi.exec()` (replies in queued order).
	Exec() []any
}

// Client mirrors the vendored rclient (redis-wrapper project_history pool
// client): `multi()` is the whole surface this module uses of it.
type Client interface {
	Multi() Tx
}

// Manager mirrors the vendored ProjectHistoryRedisManager module object
// (promises). All redis traffic goes through C; the rest are the oracle's
// injectable seams.
type Manager struct {
	// Keys — the vendored projectHistoryRedis key schema.
	Keys Keys
	// C — the vendored rclient (hermetic).
	C Client

	// Now — the wall in ms since unix epoch (the vendored Date.now() /
	// new Date() instant).
	Now func() int64

	// Summary — the vendored metrics.summary (oracle sinon stub).
	Summary func(metric string, n int, labels map[string]any)

	// MaxDocLength — the vendored Settings.max_doc_length.
	MaxDocLength int

	// DocIsTooLarge — the vendored limits.docIsTooLarge
	// (the oracle stubs it; nil uses the committed limits port).
	DocIsTooLarge func(estimatedSize int, lines []string, maxDocLength int) bool
	// StringFileDataContentIsTooLarge — the vendored limits.
	StringFileDataContentIsTooLarge func(raw *limits.StringFileRawData, maxDocLength int) bool

	// ToHistoryRanges — the vendored HistoryConversions.toHistoryRanges
	// (nil uses the committed port).
	ToHistoryRanges func(r historyconversions.Ranges) historyconversions.HistoryRanges

	// AddTrackedDeletesToContent — the vendored Utils (nil uses the
	// committed port).
	AddTrackedDeletesToContent func(content string, trackedChanges []utils.TrackedChange) string

	// QueueOpsSeam — mirrors the oracle's `promises.queueOps` stub (nil
	// uses the real Redis path).
	QueueOpsSeam func(projectID string, ops ...string) error
}

// DefaultKeys mirrors `Settings.redis.project_history.key_schema` (the
// vendored key names the oracle pins).
func DefaultKeys() Keys {
	return Keys{
		ProjectHistoryOps:              func(p string) string { return "ProjectHistory:Ops:" + p },
		ProjectHistoryFirstOpTimestamp: func(p string) string { return "ProjectHistory:FirstOpTimestamp:" + p },
	}
}

// New builds a Manager with all injectable seams defaulted (the vendored
// defaults: no-op metrics, wall Now, committed limits + history conversions;
// the vendored Settings.max_doc_length default).
func New() *Manager {
	return &Manager{
		Keys:         DefaultKeys(),
		Now:          func() int64 { return time.Now().UnixMilli() },
		Summary:      func(metric string, n int, labels map[string]any) {},
		MaxDocLength: 5_000_000, // vendored Settings default (not oracle-pinned).
		DocIsTooLarge: func(estimatedSize int, lines []string, maxDocLength int) bool {
			return limits.DocIsTooLarge(estimatedSize, lines, maxDocLength)
		},
		StringFileDataContentIsTooLarge: limits.StringFileDataContentIsTooLarge,
		ToHistoryRanges:                 historyconversions.ToHistoryRanges,
		AddTrackedDeletesToContent:      utils.AddTrackedDeletesToContent,
	}
}
