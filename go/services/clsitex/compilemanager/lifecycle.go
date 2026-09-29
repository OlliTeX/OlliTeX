package compilemanager

import (
	"errors"
	"os"
	"path/filepath"

	cerrors "ollitex/go/services/clsitex/errors"
	clsl "ollitex/go/services/clsitex/logger"
)

// killLatexOut blocks on the KillLatex callback seam (Node: promisify /
// callbackify of killLatex(compileName, cb)).
func killLatexOut(kill func(compileName string, cb func(err error)), compileName string) error {
	errCh := make(chan error, 1)
	kill(compileName, func(err error) {
		errCh <- err
	})
	return <-errCh
}

// StopCompile ports stopCompile: kill the latex process, then wait for the
// lock to release (when one exists). No-lock + not-running returns
// silently; no-lock + running is warned, then the kill is attempted anyway.
// NOTE (Node parity: kill error short-circuits before the release wait).
func (m *Manager) StopCompile(projectID, userID string) error {
	compileName := compileName(projectID, userID)
	compileDir := compileDirOf(m.Paths.CompilesDir, projectID, userID)

	var waitCh <-chan struct{}
	if lock := m.GetLock(compileDir); lock != nil {
		waitCh = lock.WaitForRelease()
	} else {
		if !m.IsRunning(compileName) {
			return nil
		}
		clsl.Warn(map[string]any{
			"projectId": projectID,
			"userId":    userID,
		}, "found running compile without lock")
	}

	if err := killLatexOut(m.KillLatex, compileName); err != nil {
		return err
	}
	if waitCh != nil {
		<-waitCh
	}
	return nil
}

// ClearProject ports clearProject: rm {force, recursive}.
func (m *Manager) ClearProject(projectID, userID string) error {
	compileDir := compileDirOf(m.Paths.CompilesDir, projectID, userID)
	return os.RemoveAll(compileDir)
}

// ClearProjectWithListing ports clearProjectWithListing: per allEntries
// from findOutputFiles, unlink files / rmdir dirs (off.AllEntries carries
// "/"-suffixed directory entries, so branch on the suffix), then rmdir
// the now-empty compile directory. Skips removal when the directory is
// absent.
func (m *Manager) ClearProjectWithListing(projectID, userID string, allEntries []string) error {
	compileDir := compileDirOf(m.Paths.CompilesDir, projectID, userID)

	exists, err := m.CheckDirectory(compileDir)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	for _, pathInProject := range allEntries {
		path := filepath.Join(compileDir, pathInProject)
		var rmErr error
		if len(pathInProject) > 0 && pathInProject[len(pathInProject)-1:] == "/" {
			rmErr = os.Remove(path) // rmdir
		} else {
			rmErr = os.Remove(path) // unlink
		}
		if rmErr != nil {
			return rmErr
		}
	}
	// rmdir the (now empty) project directory
	return os.Remove(compileDir)
}

// ClearExpiredProjects ports clearExpiredProjects: readdir compilesDir,
// per-directory stat; age > maxCacheAgeMs -> rm (force, recursive). Stats
// errors are individual and swallowed (Node: continue).
func (m *Manager) ClearExpiredProjects(maxCacheAgeMs int64) error {
	now := m.Now()
	entries, err := os.ReadDir(m.Paths.CompilesDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		dir := filepath.Join(m.Paths.CompilesDir, entry.Name())
		fi, err := os.Stat(dir)
		if err != nil {
			continue // errors checking the directory are ignored
		}
		age := now - fi.ModTime().UnixMilli()
		if age > maxCacheAgeMs {
			if err := os.RemoveAll(dir); err != nil {
				return err
			}
		}
	}
	return nil
}

// CheckDirectory ports _checkDirectory: lstat; ENOENT -> false; other
// stat errors -> tagged error (Node's tag call is a no-op there: the
// ORIGINAL error is thrown, mirrored); non-directory -> OError.
func (m *Manager) CheckDirectory(compileDir string) (bool, error) {
	fi, err := os.Lstat(compileDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		// Node: OError.tag(err, ...) is called but the tagged value is
		// discarded — the raw error is thrown. Parity: return the raw error.
		return false, err
	}
	if !fi.IsDir() {
		return false, cerrors.NewOError("project directory is not directory",
			map[string]any{"dir": compileDir, "stats": fi})
	}
	return true, nil
}
