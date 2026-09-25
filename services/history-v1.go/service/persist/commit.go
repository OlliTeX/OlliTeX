package persist

import (
	"fmt"

	"history-v1/internal/core"
)

// CommitOptions — Node commit_changes options: { historyBufferLevel,
// forcePersistBuffer }.
type CommitOptions struct {
	// HistoryBufferLevel — 0..4 (Node `historyBufferLevel || 0`).
	HistoryBufferLevel int
	// ForcePersistBuffer — Node `forcePersistBuffer`: force the Redis persist
	// buffer to be persisted before committing.
	ForcePersistBuffer bool
}

// NotPortedError — explicit marker that the requested path depends on the
// Redis backend, which is intentionally NOT ported to Go (the Go port is
// hermetic: mongo is the FakePersister, blob store is in-memory, and the
// Redis persist buffer does not exist).
type NotPortedError struct{ Path string }

func (e *NotPortedError) Error() string {
	return "not ported (Redis-dependent): " + e.Path
}

// CommitChanges — Node commitChanges(projectId, changes, limits,
// endVersion, options). The only portable level is 0 (persistChanges
// straight to the chunk store). Levels 1-4 and forcePersistBuffer depend on
// the Redis persist buffer (redisBackend / queueChanges / persistBuffer),
// which is not ported and returns *NotPortedError.
//
// Node:
//
//	if (forcePersistBuffer) { redisBackend.expireProject / persistBuffer }
//	switch (historyBufferLevel) {
//	  case 4: queueChanges; return {}
//	  case 3: queueChanges; return persistBuffer(projectId, limits)
//	  case 2: queueChangesFake; return persistChanges(projectId, changes, limits, endVersion)
//	  case 1: queueChangesFakeOnlyIfExists; return persistChanges(...)
//	  case 0: return persistChanges(projectId, changes, limits, endVersion)
//	  default: throw new Error(`Invalid history buffer level: ${historyBufferLevel}`)
//	}
func (s *Service) CommitChanges(projectID string, allChanges []*core.Change, limits Limits, endVersion int, opts CommitOptions) (*PersistResult, error) {
	if opts.ForcePersistBuffer {
		return nil, &NotPortedError{Path: "forcePersistBuffer"}
	}
	switch opts.HistoryBufferLevel {
	case 0:
		return s.PersistChanges(projectID, allChanges, limits, endVersion)
	case 1, 2, 3, 4:
		return nil, &NotPortedError{Path: fmt.Sprintf("historyBufferLevel %d", opts.HistoryBufferLevel)}
	default:
		return nil, fmt.Errorf("invalid history buffer level: %d", opts.HistoryBufferLevel)
	}
}
