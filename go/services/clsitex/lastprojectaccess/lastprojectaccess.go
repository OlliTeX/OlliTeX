// Package lastprojectaccess ports services/clsi/app/js/LastProjectAccess.js.
//
// Node parity notes:
//
// LAST_ACCESS is the projectId -> timestamp (ms) mapping "owned" by
// ProjectPersistenceManager but needed by DockerRunner (cyclic import
// breaker). getLastProjectAccessTime(projectId) defaults to 0 when the
// projectId is absent.
package lastprojectaccess

import "sync"

// LastAccess mirrors the module-level LAST_ACCESS Map.
var LastAccess = map[string]int64{}

var lastAccessMu sync.Mutex

// GetLastProjectAccessTime ports getLastProjectAccessTime: absent => 0.
func GetLastProjectAccessTime(projectID string) int64 {
	lastAccessMu.Lock()
	defer lastAccessMu.Unlock()
	return LastAccess[projectID]
}

// SetLastProjectAccessTime mirrors the ProjectPersistenceManager's write
// (LAST_ACCESS.set(projectId, Date.now())).
func SetLastProjectAccessTime(projectID string, nowMs int64) {
	lastAccessMu.Lock()
	defer lastAccessMu.Unlock()
	LastAccess[projectID] = nowMs
}

// Delete mirrors LAST_ACCESS.delete(projectId) (ProjectPersistenceManager
// _clearProjectFromDatabase).
func Delete(projectID string) {
	lastAccessMu.Lock()
	defer lastAccessMu.Unlock()
	delete(LastAccess, projectID)
}

// SetIfNewer mirrors the Node setLastAccessIfNewer: LAST_ACCESS.set(id, ts)
// only when ts >= the current value (absent -> 0).
func SetIfNewer(projectID string, ts int64) {
	lastAccessMu.Lock()
	defer lastAccessMu.Unlock()
	if ts > LastAccess[projectID] {
		LastAccess[projectID] = ts
	}
}

// EachExpired mirrors PPM._findExpiredProjectIds: collect the project ids
// whose last access is older than beforeMs, without holding the lock longer
// than the scan (Node: "may be a fairly busy loop, continue detached").
func EachExpired(beforeMs int64) []string {
	lastAccessMu.Lock()
	defer lastAccessMu.Unlock()
	expired := make([]string, 0, len(LastAccess))
	for pid, lastAccess := range LastAccess {
		if lastAccess < beforeMs {
			expired = append(expired, pid)
		}
	}
	return expired
}
