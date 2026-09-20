// CandidateSnapshot + ServletFile — ports data/CandidateSnapshot +
// data/ServletFile.
//
// A CandidateSnapshot is the diff of a git push (new directory contents vs
// the old commit contents): every NEW file becomes a ServletFile (with a
// UUID); every OLD file missing from the new contents becomes a deleted
// path. writeServletFiles persists the changed files into
// <rootGitDirectory>/.wlgb/atts/<project>/<uuid> so the Overleaf app can
// fetch them (with the postback key) during the push; deleteServletFiles
// removes them when the push completes (Java's AutoCloseable close()).
package data

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"ollitex/go/services/gitbridge/filestore"
	"ollitex/go/services/gitbridge/util"
)

// postbackURL ports Util.getPostbackURL, bound at startup.
func postbackURL() string { return util.GetPostbackURL() }

// ---------------------------------------------------------------------------
// ServletFile — ports data/ServletFile.
// ---------------------------------------------------------------------------

// ServletFile wraps a RawFile plus the UUID its push-time blob is written
// to, and whether it changed vs the previous commit.
type ServletFile struct {
	Path     string
	Contents []byte
	uuid     string
	changed  bool
}

// newServletFile ports the ServletFile(RawFile, RawFile) constructor.
// old==null => changed (Java: equals(false) against null).
func newServletFile(file, old filestore.RawFile) *ServletFile {
	changed := true
	if old != nil {
		// Java RawFile.equals: path == path && contents equal.
		changed = file.GetPath() != old.GetPath() || !bytesEqual(file.GetContents(), old.GetContents())
	}
	return &ServletFile{
		Path:     file.GetPath(),
		Contents: file.GetContents(),
		uuid:     randomUUIDString(),
		changed:  changed,
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// WriteToDiskWithName ports RawFile.writeToDiskWithName: writes the file's
// contents to <directory>/<name>, creating parent dirs.
func (f *ServletFile) WriteToDiskWithName(directory, name string) error {
	full := filepath.Join(directory, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(full, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = out.Write(f.Contents)
	return err
}

func (f *ServletFile) isChanged() bool             { return f.changed }
func (f *ServletFile) getUniqueIdentifier() string { return f.uuid }

// randomUUIDString ports UUID.randomUUID().toString() (RFC-4122 v4, lowercase
// hyphenated).
func randomUUIDString() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("data: random uuid failed: " + err.Error())
	}
	b[6] = (b[6] & 0x0F) | 0x40 // version 4
	b[8] = (b[8] & 0x3F) | 0x80 // variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ---------------------------------------------------------------------------
// CandidateSnapshot — ports data/CandidateSnapshot.
// ---------------------------------------------------------------------------

// pushFileJSON is one entry of the "files" array of the push request body.
type pushFileJSON struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

// CandidateSnapshot diff of a push.
type CandidateSnapshot struct {
	projectName    string
	currentVersion int
	files          []*ServletFile
	deleted        []string
	attsDirectory  string
}

// NewCandidateSnapshot ports the CandidateSnapshot constructor:
// diff (every new file -> ServletFile) + deleted (old paths not in new).
// Iteration order: sorted by path (Java: HashMap iteration, arbitrary).
func NewCandidateSnapshot(projectName string, currentVersion int, contents, oldContents *filestore.RawDirectory) *CandidateSnapshot {
	cs := &CandidateSnapshot{projectName: projectName, currentVersion: currentVersion}
	paths := make([]string, 0, len(contents.FileTable))
	for p := range contents.FileTable {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		cs.files = append(cs.files, newServletFile(contents.FileTable[p], oldContents.FileTable[p]))
	}
	oldPaths := make([]string, 0, len(oldContents.FileTable))
	for p := range oldContents.FileTable {
		oldPaths = append(oldPaths, p)
	}
	sort.Strings(oldPaths)
	for _, p := range oldPaths {
		if _, ok := contents.FileTable[p]; !ok {
			cs.deleted = append(cs.deleted, p)
		}
	}
	return cs
}

// GetProjectName ports getProjectName.
func (c *CandidateSnapshot) GetProjectName() string { return c.projectName }

// CurrentVersion ports the field.
func (c *CandidateSnapshot) CurrentVersion() int { return c.currentVersion }

// GetDeleted ports getDeleted.
func (c *CandidateSnapshot) GetDeleted() []string { return c.deleted }

// Files (test support).
func (c *CandidateSnapshot) Files() []*ServletFile { return c.files }

// WriteServletFiles ports writeServletFiles(rootGitDirectory): writes every
// CHANGED file to <root>/.wlgb/atts/<project>/<uuid>.
func (c *CandidateSnapshot) WriteServletFiles(rootGitDirectory string) error {
	c.attsDirectory = filepath.Join(rootGitDirectory, ".wlgb", "atts", c.projectName)
	for _, f := range c.files {
		if f.isChanged() {
			if err := f.WriteToDiskWithName(c.attsDirectory, f.getUniqueIdentifier()); err != nil {
				return err
			}
		}
	}
	return nil
}

// DeleteServletFiles ports deleteServletFiles (AutoCloseable close).
func (c *CandidateSnapshot) DeleteServletFiles() error {
	if c.attsDirectory != "" {
		return deleteDirectory(c.attsDirectory)
	}
	return nil
}

func deleteDirectory(dir string) error {
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
			_ = deleteDirectory(full)
		} else {
			_ = os.Remove(full)
		}
	}
	return os.Remove(dir)
}

// JSONRepresentation ports getJsonRepresentation(postbackKey), the push
// POST body. JSON field order matches Java (latestVerId, files,
// postbackUrl).
func (c *CandidateSnapshot) JSONRepresentation(postbackKey string) (string, error) {
	projectURL := postbackURL() + "api/" + c.projectName
	type pushBody struct {
		LatestVerId int            `json:"latestVerId"`
		Files       []pushFileJSON `json:"files"`
		PostbackURL string         `json:"postbackUrl"`
	}
	files := make([]pushFileJSON, 0, len(c.files))
	for _, f := range c.files {
		entry := pushFileJSON{Name: f.Path}
		if f.isChanged() {
			entry.URL = projectURL + "/" + f.getUniqueIdentifier() + "?key=" + postbackKey
		}
		files = append(files, entry)
	}
	body, err := json.Marshal(pushBody{
		LatestVerId: c.currentVersion,
		Files:       files,
		PostbackURL: projectURL + "/" + postbackKey + "/postback",
	})
	if err != nil {
		return "", err
	}
	// Java: .toString() — no pretty print.
	return string(body), nil
}
