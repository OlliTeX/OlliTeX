package compilemanager

import (
	"os"
	"path/filepath"

	clsl "ollitex/go/services/clsitypst/logger"
)

// --- stopCompile (Node: stopCompile) --------------------------------------------

// StopCompile mirrors:
//
//	compileName = getCompileName(projectId, userId)
//	lock = LockManager.getExistingLock(getCompileDir(projectId, userId))
//	let lockReleased
//	if (lock) { lockReleased = lock.waitForRelease() }
//	else {
//	  if (!TypstRunner.isRunning(compileName)) return
//	  logger.warn({ projectId, userId }, 'found running compile without lock')
//	}
//	await TypstRunner.promises.killTypst(compileName)
//	await lockReleased
func (m *Manager) StopCompile(projectID, userID string) error {
	name := compileName(projectID, userID)
	var waitCh <-chan struct{}
	if lock := m.GetLock(compileDirOf(m.Paths.CompilesDir, projectID, userID)); lock != nil {
		waitCh = lock.WaitForRelease()
	} else {
		if !m.IsRunning(name) {
			// Nothing to stop: no lock AND no running typst process.
			return nil
		}
		clsl.Warn(map[string]any{"projectId": projectID, "userId": userID},
			"found running compile without lock")
	}

	var killErr error
	done := make(chan struct{}, 1)
	m.KillTypst(name, func(err error) {
		killErr = err
		done <- struct{}{}
	})
	<-done
	if killErr != nil {
		return killErr
	}
	if waitCh != nil {
		<-waitCh
	}
	return nil
}

// --- clearProject (Node: clearProject) ------------------------------------------

// ClearProject mirrors clearProject: fs.rm(compileDir, {force, recursive}).
func (m *Manager) ClearProject(projectID, userID string) error {
	compileDir := compileDirOf(m.Paths.CompilesDir, projectID, userID)
	return os.RemoveAll(compileDir)
}

// --- clearExpiredProjects (Node: clearExpiredProjects) --------------------------

// ClearExpiredProjects mirrors: readdir compilesDir (.catch(() => []) on ANY
// error), per dir stat (errors → continue/ignore), age > maxCacheAgeMs → rm
// {force, recursive}.
func (m *Manager) ClearExpiredProjects(maxCacheAgeMs int64) error {
	// Node: fsPromises.readdir(root).catch(() => []).
	entries, _ := os.ReadDir(m.Paths.CompilesDir)
	now := m.Now()
	for _, entry := range entries {
		dir := filepath.Join(m.Paths.CompilesDir, entry.Name())
		fi, serr := os.Stat(dir)
		if serr != nil {
			continue // errors checking the directory are ignored (Node: continue)
		}
		age := now - fi.ModTime().UnixMilli()
		if age > maxCacheAgeMs {
			if rerr := os.RemoveAll(dir); rerr != nil {
				return rerr
			}
		}
	}
	return nil
}
