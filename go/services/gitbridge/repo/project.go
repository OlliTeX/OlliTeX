package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"ollitex/go/services/gitbridge/filestore"
	"ollitex/go/services/gitbridge/giterrors"
)

// Project ports GitProjectRepo (folds WalkOverrideGitRepo: a non-empty
// commitID mirrors the WalkOverride wrapper walking an explicit commit).
//
// Layout: workDir = <storeRoot>/<project>; gitDir = <workDir>/.git — the
// same layout as JGit (work-tree directory holding the .git). Both Java's
// getProjectDir() and getDotGitDir() resolve to this project directory.
// The work tree is ephemeral: it is cleared (keeping .git) after each commit.
type Project struct {
	store    *FSGitRepoStore // owner (for per-commit accessor construction)
	name     string
	workDir  string
	commitID string // walk override: "" => HEAD
	maxSize  int64  // single-file limit (Java Optional<Long>: 0 => unlimited)
}

// NewProject ports GitProjectRepo.fromName(projectName) bound to a store
// (WalkOverride with maxFileSize bound, no commit override).
func NewProject(store *FSGitRepoStore, name string) *Project {
	return &Project{store: store, name: name, workDir: store.projectPath(name), maxSize: store.maxFileSize}
}

// NewProjectAtCommit ports store.useJGitRepo(repo, commitId) (hook-side
// accessor walking an explicit commit: newId for the pushed tree, old HEAD
// for the old contents).
func NewProjectAtCommit(store *FSGitRepoStore, name, commitID string) *Project {
	p := NewProject(store, name)
	p.commitID = commitID
	return p
}

// GetProjectName ports getProjectName.
func (p *Project) GetProjectName() string { return p.name }

// ProjectDir ports getProjectDir / getDotGitDir (both == the project dir).
func (p *Project) ProjectDir() string { return p.workDir }

// Store exposes the owning store so higher layers can build the per-commit
// hook-side accessors (useJGitRepoAt / NewProjectAtCommit). Java hands the
// JGit Repository and the RepoStore to the hook and does
// repoStore.useJGitRepo(repository, id); the Go equivalent needs a *store
// handle, which is what this returns.
func (p *Project) Store() *FSGitRepoStore { return p.store }

// gitDir is <workDir>/.git.
func (p *Project) gitDir() string { return filepath.Join(p.workDir, ".git") }

// git runs `git <args>` from the project directory (workDir, like Java's
// ProcessBuilder .directory(getProjectDir())), with an extra KEY=VALUE env
// layer, and returns stdout (trimmed) + error.
func (p *Project) git(env map[string]string, args ...string) (string, error) {
	return runGitInDir(p.workDir, env, args...)
}

// GitRaw runs `git <args>` from the project directory (exported for
// tests; mirrors the JGit repo access other layers rely on).
func (p *Project) GitRaw(env map[string]string, args ...string) (string, error) {
	return runGitInDir(p.workDir, env, args...)
}

// ---------------------------------------------------------------------------
// GitProjectRepo — init / use / commit / gc / incoming.
// ---------------------------------------------------------------------------

// InitRepo ports GitProjectRepo.initRepo:
//   - a no-op (warn in Java) when the object database already exists;
//   - otherwise scaffold <workDir>/.git, unborn HEAD, then link HEAD to
//     refs/heads/main (JGit create() + updateRef HEAD -> main).
func (p *Project) InitRepo() error {
	if pathExists(filepath.Join(p.gitDir(), "objects")) {
		// already initialised (objects db present) — Java warns and returns.
		return nil
	}
	if err := os.MkdirAll(p.workDir, 0o755); err != nil {
		return err
	}
	// `git init` in the work-tree dir (gitDir <workDir>/.git, like JGit).
	if _, err := p.git(nil, "init", "--quiet"); err != nil {
		return fmt.Errorf("git init: %w", err)
	}
	// JGit create() leaves HEAD unborn then links it to the default branch;
	// the bridge sets that to main. Net result: unborn HEAD -> refs/heads/main.
	if _, err := p.git(nil, "symbolic-ref", "HEAD", "refs/heads/main"); err != nil {
		return fmt.Errorf("set HEAD: %w", err)
	}
	return nil
}

// InstallProcReceiveHook installs the proc-receive write path for one
// project: .git/hooks/proc-receive (chmod 0755) + the two receive-pack
// config keys (HANDOFF §14.3 / §15):
//   - receive.procreceiverefs "adm:refs/heads/" — git does NOT move refs
//     under the prefix (the hook does — procreceiverefs requires the snake-
//     case spell; `receive.procReceiveRefs` is a NO-OP on git 2.53);
//   - receive.denyCurrentBranch "ignore" — the pushed branch may be the
//     checked-out HEAD (no work tree update; the objects-only contract).
//
// hookBody is the full executable shim (a shebang is required on the
// first line). Production: `#!/bin/sh\nexec <binary> -hook-proc-receive
// <config-path>`; test seam: execs the compiled test binary itself
// (TestMain intercept).
func (p *Project) InstallProcReceiveHook(hookBody string) error {
	if !pathExists(filepath.Join(p.gitDir(), "objects")) {
		return fmt.Errorf("repo: proc-receive install requires an initialized repo: %s", p.Project())
	}
	if _, err := p.git(nil, "config", "receive.procreceiverefs", "adm:refs/heads/"); err != nil {
		return fmt.Errorf("repo: set receive.procreceiverefs: %w", err)
	}
	if _, err := p.git(nil, "config", "receive.denyCurrentBranch", "ignore"); err != nil {
		return fmt.Errorf("repo: set receive.denyCurrentBranch: %w", err)
	}
	hookPath := filepath.Join(p.gitDir(), "hooks", "proc-receive")
	if err := os.MkdirAll(filepath.Dir(hookPath), 0o755); err != nil {
		return fmt.Errorf("repo: hooks dir: %w", err)
	}
	if err := os.WriteFile(hookPath, []byte(hookBody), 0o755); err != nil {
		return fmt.Errorf("repo: write proc-receive hook: %w", err)
	}
	return nil
}

// Project returns the project name.
func (p *Project) Project() string { return p.name }

// UseExistingRepository ports useExistingRepository: error if the
// object database does not exist.
func (p *Project) UseExistingRepository() error {
	if !pathExists(filepath.Join(p.gitDir(), "objects")) {
		return &giterrors.InvalidGitRepository{}
	}
	return nil
}

// GetFullBranch returns the full ref HEAD points to (e.g. "refs/heads/main")
// or "" when HEAD is unborn/unresolvable (ports JGit Repository.getFullBranch).
func (p *Project) GetFullBranch() (string, error) {
	out, err := p.git(nil, "symbolic-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// GetDirectory ports getDirectory(): walk HEAD (or override commitID) with
// the maxFileSize applied (RepositoryObjectTreeWalker).
func (p *Project) GetDirectory() (map[string]filestore.RawFile, error) {
	commit := p.commitID
	if commit == "" {
		commit = resolveHead(p.workDir)
	}
	if commit == "" {
		return map[string]filestore.RawFile{}, nil // unborn
	}
	return walkTree(p.gitDir(), commit, p.maxSize)
}

// CommitAndGetMissing ports doCommitAndGetMissing.
//
// Returns the missing paths: files in the committed HEAD that are absent
// from the new contents — the JGit `resetHard(); write(); status().getMissing()`
// view (committed-index entries whose on-disk file was deleted by write()).
var NL = "\\n"

func (p *Project) CommitAndGetMissing(contents *filestore.GitDirectoryContents) ([]string, error) {
	// resetHard first (Java does it up front); a no-op when HEAD is unborn.
	if committed := resolveHead(p.workDir); committed != "" {
		if _, err := p.git(nil, "reset", "--hard", "--quiet"); err != nil {
			return nil, fmt.Errorf("git reset --hard: %w", err)
		}
	}
	// missing = committed-HEAD paths not present in the new contents.
	missing := computeMissing(p, contents)
	// contents.write(): clear work-tree (keep .git) + write snapshot files.
	if err := contents.Write(); err != nil {
		return nil, err
	}
	// git rm --cached each missing (JGit rm().setCached(true)).
	for _, m := range missing {
		if _, err := p.git(nil, "rm", "--cached", "--quiet", "--", m); err != nil {
			return nil, fmt.Errorf("git rm %s: %w", m, err)
		}
	}
	// git add everything, ignoring .gitignore (NoGitignoreIterator).
	if _, err := p.git(nil, "add", "--force", "--", "."); err != nil {
		return nil, fmt.Errorf("git add: %w", err)
	}
	// commit as the snapshot user at the snapshot time.
	env := map[string]string{
		"GIT_AUTHOR_NAME":     contents.UserName,
		"GIT_AUTHOR_EMAIL":    contents.UserEmail,
		"GIT_COMMITTER_NAME":  contents.UserName,
		"GIT_COMMITTER_EMAIL": contents.UserEmail,
		"GIT_AUTHOR_DATE":     gitDate(contents.When),
		"GIT_COMMITTER_DATE":  gitDate(contents.When),
	}
	if _, err := p.git(env, "commit", "--allow-empty", "--quiet", "-m", contents.CommitMessage); err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
	}
	// clear work-tree (keep .git) — Util.deleteInDirectoryApartFrom.
	removeInDirectoryApartFrom(p.workDir, ".git")
	return missing, nil
}

// computeMissing = committed-HEAD paths minus the new contents' paths.
func computeMissing(p *Project, contents *filestore.GitDirectoryContents) []string {
	committed := resolveHead(p.workDir)
	if committed == "" {
		return nil
	}
	oldPaths, err := lsTreePaths(p.gitDir(), committed)
	if err != nil {
		// A corrupt repo here is treated like Java's IOException path.
		return nil
	}
	written := map[string]struct{}{}
	for _, f := range contents.Files {
		written[f.GetPath()] = struct{}{}
	}
	var missing []string
	for path := range oldPaths {
		if _, ok := written[path]; !ok {
			missing = append(missing, path)
		}
	}
	sort.Strings(missing)
	return missing
}

// gitDate formats epoch millis as git's "@<sec> +0000" (UTC identity tz).
func gitDate(whenMillis int64) string { return fmt.Sprintf("@%d +0000", whenMillis/1000) }

// ---------------------------------------------------------------------------
// RepositoryObjectTreeWalker (ported).
//
// JGit TreeWalk (recursive) enumerates non-tree entries at their full path
// and open()s each oid — which for a missing object (e.g. a gitlink to an
// absent commit in the tree) throws. Ported to `git ls-tree -r <sha>` +
// per-object `git cat-file` (size + existence + contents).
//
// Size check is `size > maxSize` (strictly greater, per the Java walker).
// ---------------------------------------------------------------------------

// resolveHead returns the HEAD commit sha or "" when unborn/unresolvable.
func resolveHead(workDir string) string {
	sha, _ := runGitInDir(workDir, nil, "rev-parse", "--quiet", "--verify", "HEAD^{commit}")
	return strings.TrimSpace(sha)
}

// walkTree ports RepositoryObjectTreeWalker.getDirectoryContents.
func walkTree(gitDir, commit string, maxSize int64) (map[string]filestore.RawFile, error) {
	out, err := runGitInDir(gitDir, nil, "ls-tree", "-r", commit)
	if err != nil {
		return nil, &giterrors.InvalidGitRepository{}
	}
	result := map[string]filestore.RawFile{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// "<mode> <type> <oid>\t<path>"
		tab := strings.Index(line, "\t")
		if tab < 0 {
			continue
		}
		meta := line[:tab]
		path := line[tab+1:]
		fields := strings.Split(meta, " ")
		if len(fields) != 3 {
			continue
		}
		oid := fields[2]
		// object must be present (gitlink to a missing commit => invalid).
		if _, err := runGitInDir(gitDir, nil, "cat-file", "-e", oid); err != nil {
			return nil, &giterrors.InvalidGitRepository{}
		}
		sizeOut, err := runGitInDir(gitDir, nil, "cat-file", "-s", oid)
		if err != nil {
			return nil, &giterrors.InvalidGitRepository{}
		}
		size, _ := strconv.ParseInt(strings.TrimSpace(sizeOut), 10, 64)
		if maxSize > 0 && size > maxSize {
			return nil, &giterrors.SizeLimitExceededException{Path: path, ActualSize: size, MaxSize: maxSize}
		}
		body, err := runGitInDir(gitDir, nil, "cat-file", "blob", oid)
		if err != nil {
			// a non-blob entry or an uncattable object.
			return nil, &giterrors.InvalidGitRepository{}
		}
		result[path] = filestore.NewRepositoryFile(path, []byte(body))
	}
	return result, nil
}

// lsTreePaths returns the set of paths in a commit's tree (all entries).
func lsTreePaths(gitDir, commit string) (map[string]struct{}, error) {
	out, err := runGitInDir(gitDir, nil, "ls-tree", "-r", "--name-only", commit)
	if err != nil {
		return nil, err
	}
	paths := map[string]struct{}{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		paths[line] = struct{}{}
	}
	return paths, nil
}

// ---------------------------------------------------------------------------
// GC + incoming-pack cleanup.
// ---------------------------------------------------------------------------

// RunGC ports runGC: `git gc` from the project directory; a non-zero exit
// maps to the Java `IOException("git gc error")`.
func (p *Project) RunGC() error {
	if _, err := p.git(nil, "gc"); err != nil {
		return fmt.Errorf("git gc error: %w", err)
	}
	return nil
}

// DeleteIncomingPacks ports deleteIncomingPacks: walk <project>/.git and
// delete every file whose name starts with "incoming_" and ends with ".pack".
func (p *Project) DeleteIncomingPacks() error {
	return deleteIncoming(p.gitDir())
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func removeInDirectoryApartFrom(directory string, keep ...string) {
	keepSet := map[string]bool{}
	for _, k := range keep {
		keepSet[k] = true
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	for _, e := range entries {
		if keepSet[e.Name()] {
			continue
		}
		full := filepath.Join(directory, e.Name())
		if e.IsDir() {
			removeRecursive(full)
			continue
		}
		os.Remove(full)
	}
}

func removeRecursive(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		if e.IsDir() {
			removeRecursive(full)
			continue
		}
		os.Remove(full)
	}
	os.Remove(dir)
}

// deleteIncoming walks the project dir deleting files named incoming_*.pack
// (JGit: name.startsWith("incoming_") && name.endsWith(".pack")).
func deleteIncoming(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		if e.IsDir() {
			if err := deleteIncoming(full); err != nil {
				return err
			}
			continue
		}
		if isIncomingPack(e.Name()) {
			if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

func isIncomingPack(name string) bool {
	return len(name) >= 14 &&
		strings.HasPrefix(name, "incoming_") &&
		strings.HasSuffix(name, ".pack")
}
