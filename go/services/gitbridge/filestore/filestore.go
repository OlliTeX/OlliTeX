// Package filestore ports data/filestore/* and the data/filestore exceptions:
// RawFile, RawDirectory, RepositoryFile, GitDirectoryContents, FileList,
// LockFile and the size-limit exception hierarchy.
package filestore

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// RawFile (abstract)
// ---------------------------------------------------------------------------

// RawFile is the abstract file node (port of RawFile).
type RawFile interface {
	GetPath() string
	GetContents() []byte
	Size() int64
}

func (r *repoFile) GetPath() string     { return r.path }
func (r *repoFile) GetContents() []byte { return r.contents }
func (r *repoFile) Size() int64         { return int64(len(r.contents)) }

// repoFile is an in-memory RawFile.
type repoFile struct {
	path     string
	contents []byte
}

// Equal ports RawFile.equals (path + contents byte equality).
func (r *repoFile) Equal(o interface{}) bool {
	of, ok := o.(*repoFile)
	if !ok {
		return false
	}
	return r.path == of.path && bytes.Equal(r.contents, of.contents)
}

// RepositoryFile ports data/filestore/RepositoryFile.
type RepositoryFile struct{ *repoFile }

// NewRepositoryFile constructs a RepositoryFile.
func NewRepositoryFile(path string, contents []byte) *RepositoryFile {
	return &RepositoryFile{&repoFile{path: path, contents: contents}}
}

// WriteFileToDisk ports RawFile.writeToDisk(directory, path).
func WriteFileToDisk(directory string, f RawFile) error {
	full := filepath.Join(directory, f.GetPath())
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(full, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = out.Write(f.GetContents())
	return err
}

// ---------------------------------------------------------------------------
// RawDirectory
// ---------------------------------------------------------------------------

// RawDirectory ports data/filestore/RawDirectory.
type RawDirectory struct {
	FileTable map[string]RawFile
}

// NewRawDirectory ports the RawDirectory constructor.
func NewRawDirectory(fileTable map[string]RawFile) *RawDirectory {
	if fileTable == nil {
		fileTable = map[string]RawFile{}
	}
	return &RawDirectory{FileTable: fileTable}
}

// ---------------------------------------------------------------------------
// GitDirectoryContents
// ---------------------------------------------------------------------------

// GitDirectoryContents ports data/filestore/GitDirectoryContents.
type GitDirectoryContents struct {
	Files         []RawFile
	ProjectName   string
	GitDirectory  string
	UserName      string
	UserEmail     string
	CommitMessage string
	When          int64
}

// NewGitDirectoryContents ports the GitDirectoryContents constructor.
func NewGitDirectoryContents(files []RawFile, rootGitDirectory, projectName, userName, userEmail, commitMessage string, when int64) *GitDirectoryContents {
	return &GitDirectoryContents{
		Files:         files,
		ProjectName:   projectName,
		GitDirectory:  filepath.Join(rootGitDirectory, projectName),
		UserName:      userName,
		UserEmail:     userEmail,
		CommitMessage: commitMessage,
		When:          when,
	}
}

func (g *GitDirectoryContents) GetDirectory() string { return g.GitDirectory }

// Write ports write(): clears the git dir (leaving ".git") then writes files.
func (g *GitDirectoryContents) Write() error {
	if err := g.deleteInDirectoryApartFrom(); err != nil {
		return err
	}
	for _, f := range g.Files {
		if err := WriteFileToDisk(g.GitDirectory, f); err != nil {
			return err
		}
	}
	return nil
}

// deleteInDirectoryApartFrom mirrors Util.deleteInDirectoryApartFrom(dir, ".git").
func (g *GitDirectoryContents) deleteInDirectoryApartFrom() error {
	entries, err := os.ReadDir(g.GitDirectory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.Name() == ".git" {
			continue
		}
		full := filepath.Join(g.GitDirectory, e.Name())
		if e.IsDir() {
			_ = deleteRecursive(full)
		}
		if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func deleteRecursive(dir string) error {
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
			if err := deleteRecursive(full); err != nil {
				return err
			}
			continue
		}
		_ = os.Remove(full)
	}
	return os.Remove(dir)
}

// ---------------------------------------------------------------------------
// Exceptions (data/filestore/exception + data/exceptions)
// ---------------------------------------------------------------------------

// SizeLimitExceededException ports SizeLimitExceededException.
type SizeLimitExceededException struct{ MaxBytes int64 }

// NewSizeLimitExceededException constructs one.
func NewSizeLimitExceededException(maxBytes int64) *SizeLimitExceededException {
	return &SizeLimitExceededException{MaxBytes: maxBytes}
}
func (e *SizeLimitExceededException) Error() string {
	return fmt.Sprintf("Size limit exceeded (%d bytes)", e.MaxBytes)
}

// TooLargeFile ports TooLargeFile.
type TooLargeFile struct {
	Path  string
	Limit int64
}

func (t *TooLargeFile) GetDescription() string {
	return fmt.Sprintf("File %s exceeds the %d byte limit", t.Path, t.Limit)
}

// RepositoryFileTooLarge ports RepositoryFileTooLarge.
type RepositoryFileTooLarge struct{ File *TooLargeFile }

// NewRepositoryFileTooLarge constructs one.
func NewRepositoryFileTooLarge(file *TooLargeFile) *RepositoryFileTooLarge {
	return &RepositoryFileTooLarge{File: file}
}
func (e *RepositoryFileTooLarge) Error() string { return e.File.GetDescription() }

// FilesTooLarge ports FilesTooLarge.
type FilesTooLarge struct{ Files []*TooLargeFile }

// NewFilesTooLarge constructs one.
func NewFilesTooLarge(files []*TooLargeFile) *FilesTooLarge { return &FilesTooLarge{Files: files} }
func (e *FilesTooLarge) Error() string {
	parts := make([]string, 0, len(e.Files))
	for _, f := range e.Files {
		parts = append(parts, f.GetDescription())
	}
	return strings.Join(parts, ", ")
}
