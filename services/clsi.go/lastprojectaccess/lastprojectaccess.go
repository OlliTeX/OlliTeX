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
