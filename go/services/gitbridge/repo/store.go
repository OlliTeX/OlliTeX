// Package repo ports Java bridge/repo + git/util (JGit -> `git` CLI).
//
// Java class                   -> Go
// -----------------------------  ----------------------
//
//	RepoStore (interface)        -> (folded) FSGitRepoStore (only impl)
//	FSGitRepoStore               -> FSGitRepoStore (store.go)
//	ProjectRepo (interface)      -> (folded) methods on *Project
//	GitProjectRepo               -> Project (project.go)
//	WalkOverrideGitRepo          -> (folded) Project.commitID != ""
//	NoGitignoreIterator          -> `git add --force`
//	RepositoryObjectTreeWalker   -> walkTree (ls-tree + cat-file)
//	util.Tar (util/Tar.java)     -> tar.go (archive/tar + gzip + bzip2)
//
// Repo layout (unchanged from the bridge): <storeRoot>/<project>/.git —
// the `.git` directory lives INSIDE the project work-tree directory (JGit
// FileRepositoryBuilder().setWorkTree(projectDir)). The work tree is
// ephemeral: it is cleared (keeping .git) after each commit.
//
// bzip2 note: the compressed swap archives use github.com/dsnet/compress
// (the same library Forgejo uses to stream bzip2 payloads, e.g.
// routers/api/packages/conda/conda.go — see the README credit).
package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"ollitex/go/services/gitbridge/util"
)

// DefaultMaxFileSize ports FSGitRepoStore.DEFAULT_MAX_FILE_SIZE.
const DefaultMaxFileSize = 50 * 1024 * 1024

// FSGitRepoStore ports FSGitRepoStore. RootDirectory holds the project
// dirs (<root>/<project>) and the .wlgb store dir.
//
// fsSizer ports the `Function<File, Long> fsSizer` field (Java default:
// File.getTotalSpace() - File.getFreeSpace(), i.e. the USED size of the
// underlying filesystem, NOT the directory size); tests may override it.
type FSGitRepoStore struct {
	RootDirectory string
	maxFileSize   int64
	fsSizer       func(absPath string) (int64, error)
}

// NewFSGitRepoStore ports FSGitRepoStore(repoStorePath, Optional<Long>
// maxFileSize): nil -> DEFAULT_MAX_FILE_SIZE.
func NewFSGitRepoStore(root string, maxFileSize *int64) *FSGitRepoStore {
	max := int64(DefaultMaxFileSize)
	if maxFileSize != nil {
		max = *maxFileSize
	}
	_ = os.MkdirAll(root, 0o755) // Java initRootGitDirectory
	return &FSGitRepoStore{RootDirectory: root, maxFileSize: max, fsSizer: diskUsedSizer}
}

// NewFSGitRepoStoreWithSizer ports the test constructor with an explicit
// maxFileSize + custom sizer (fsSizer seam; nil -> default).
func NewFSGitRepoStoreWithSizer(root string, maxFileSize int64, fsSizer func(absPath string) (int64, error)) *FSGitRepoStore {
	_ = os.MkdirAll(root, 0o755)
	if fsSizer == nil {
		fsSizer = diskUsedSizer
	}
	return &FSGitRepoStore{RootDirectory: root, maxFileSize: maxFileSize, fsSizer: fsSizer}
}

// diskUsedSizer ports the Java default `d -> d.getTotalSpace() -
// d.getFreeSpace()` (File total/free space, not du): on Linux statfs
// Blocks*Bsize - Bavail*Bsize.
func diskUsedSizer(absPath string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(absPath, &st); err != nil {
		return 0, err
	}
	bsize := int64(st.Bsize)
	return int64(st.Blocks)*bsize - int64(st.Bavail)*bsize, nil
}

// MaxFileSize ports the primitive `long maxFileSize` field.
func (s *FSGitRepoStore) MaxFileSize() int64 { return s.maxFileSize }

// ---------------------------------------------------------------------------
// RepoStore — ports RepoStore.java interface methods on *FSGitRepoStore.
// ---------------------------------------------------------------------------

// GetRepoStorePath ports getRepositoryStorePath.
func (s *FSGitRepoStore) GetRepoStorePath() string { return s.RootDirectory }

// GetRootDirectory ports getRootDirectory.
func (s *FSGitRepoStore) GetRootDirectory() string { return s.RootDirectory }

// projectPath ports getProjectDirectory (<root>/<name>, may not exist).
func (s *FSGitRepoStore) projectPath(project string) string {
	return filepath.Join(s.RootDirectory, project)
}

// NewProject builds the Project (JGit wrapper) for a project name — ports
// GitProjectRepo.fromName bound to this store.
func (s *FSGitRepoStore) NewProject(project string) *Project {
	return NewProject(s, project)
}

// NewProjectAtCommit ports useJGitRepo(repo, commitId): the hook-side
// accessor walking an explicit commit.
func (s *FSGitRepoStore) NewProjectAtCommit(project, commitID string) *Project {
	return NewProjectAtCommit(s, project, commitID)
}

// InitRepo ports initRepo(project): scaffold <root>/<project>/.git, unborn
// HEAD -> refs/heads/main (no-op if the object database already exists).
func (s *FSGitRepoStore) InitRepo(project string) (*Project, error) {
	p := s.NewProject(project)
	if err := p.InitRepo(); err != nil {
		return nil, err
	}
	return p, nil
}

// GetExistingRepo ports getExistingRepo(project): error if the object
// database does not exist.
func (s *FSGitRepoStore) GetExistingRepo(project string) (*Project, error) {
	p := s.NewProject(project)
	if err := p.UseExistingRepository(); err != nil {
		return nil, err
	}
	return p, nil
}

// UseJGitRepoAt ports useJGitRepo(repo, commitId): the hook-side accessor
// walking an explicit commit (newId for the pushed tree, old HEAD for the
// old contents).
func (s *FSGitRepoStore) UseJGitRepoAt(project, commitID string) (*Project, error) {
	p := s.NewProjectAtCommit(project, commitID)
	if err := p.UseExistingRepository(); err != nil {
		return nil, err
	}
	return p, nil
}

// PurgeNonexistentProjects ports purgeNonexistentProjects: delete every
// top-level name that is not in existingProjectNames + ".wlgb".
func (s *FSGitRepoStore) PurgeNonexistentProjects(existing []string) error {
	keep := map[string]bool{".wlgb": true}
	for _, n := range existing {
		keep[n] = true
	}
	entries, err := os.ReadDir(s.RootDirectory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if keep[e.Name()] {
			continue
		}
		if err := util.DeleteDirectory(filepath.Join(s.RootDirectory, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// TotalSize ports totalSize() (fsSizer over the root directory).
func (s *FSGitRepoStore) TotalSize() (int64, error) {
	return s.fsSizer(s.RootDirectory)
}

// ---------------------------------------------------------------------------
// Swap (de)compression, ports util/Tar + FSGitRepoStore {b,g}zipProject +
// {un,}gzip/unbzip2Project.
//
// Java zips getDotGitForProject(project) = <root>/<project>/.git with entry
// names relative to <root>/<project> (so the top-level tar entry is ".git"),
// and unzips into <root>/<project>. Only the .git directory is swapped.
// ---------------------------------------------------------------------------

// Bzip2Project ports bzip2Project(project, sizePtr).
func (s *FSGitRepoStore) Bzip2Project(project string) (data []byte, err error) {
	if !util.IsValidProjectName(project) {
		return nil, fmt.Errorf("[%s] invalid project name: ", project)
	}
	return compressDir(filepath.Join(s.projectPath(project), ".git"), s.projectPath(project), "bzip2")
}

// GzipProject ports gzipProject(project, sizePtr).
func (s *FSGitRepoStore) GzipProject(project string) (data []byte, err error) {
	if !util.IsValidProjectName(project) {
		return nil, fmt.Errorf("[%s] invalid project name: ", project)
	}
	return compressDir(filepath.Join(s.projectPath(project), ".git"), s.projectPath(project), "gzip")
}

// Unbzip2Project ports unbzip2Project(project, dataStream).
func (s *FSGitRepoStore) Unbzip2Project(project string, data []byte) error {
	if !util.IsValidProjectName(project) {
		return fmt.Errorf("[%s] invalid project name: ", project)
	}
	target := s.projectPath(project)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("[%s] directories for evicted project already exist", project)
	}
	return untarStream(data, target, "bzip2")
}

// UngzipProject ports ungzipProject(project, dataStream).
func (s *FSGitRepoStore) UngzipProject(project string, data []byte) error {
	if !util.IsValidProjectName(project) {
		return fmt.Errorf("[%s] invalid project name: ", project)
	}
	target := s.projectPath(project)
	_ = os.MkdirAll(target, 0o755)
	return untarStream(data, target, "gzip")
}

// GcProject ports gcProject: `git gc` on the project (via its repo).
func (s *FSGitRepoStore) GcProject(project string) error {
	if !util.IsValidProjectName(project) {
		return fmt.Errorf("[%s] invalid project name", project)
	}
	p, err := s.GetExistingRepo(project)
	if err != nil {
		return err
	}
	return p.RunGC()
}

// Remove ports remove: delete the whole project dir (incl. .git).
func (s *FSGitRepoStore) Remove(project string) error {
	if !util.IsValidProjectName(project) {
		return fmt.Errorf("[%s] invalid project name", project)
	}
	return util.DeleteDirectory(s.projectPath(project))
}

// IsProjectPresent mirrors the on-disk presence check (objects db exists).
func (s *FSGitRepoStore) IsProjectPresent(project string) bool {
	_, err := os.Stat(filepath.Join(s.projectPath(project), ".git", "objects"))
	return err == nil
}
