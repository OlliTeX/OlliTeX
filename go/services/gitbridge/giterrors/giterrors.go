// Package giterrors ports the user-facing exception hierarchies:
//
//	git/exception/*          (git user errors)
//	snapshot/base/*          (MissingRepository, Forbidden)
//	snapshot/exception/*     (FailedConnection)
//	snapshot/push/exception/* (postback + push errors)
//	git/handler/hook/exception/* (WrongBranch, ForcedPush)
//
// Java shape: abstract GitUserException { message, descriptionLines }
// extends Exception; SnapshotAPIException adds JSONSource. The git-client
// path prints message + "\n" + descriptionLines.
//
// Go shape: concrete struct errors, each with Error() = message and
// Description() = description lines.
package giterrors

import "fmt"

// serviceName is bound to util.GetServiceName (Java: Util.getServiceName()).
var serviceNameFn func() string

// SetServiceNameFn binds the util getter (called at startup).
func SetServiceNameFn(fn func() string) {
	serviceNameFn = fn
}

func serviceName() string {
	if serviceNameFn != nil {
		return serviceNameFn()
	}
	return "Overleaf"
}

// ---------------------------------------------------------------------------
// git/exception/*
// ---------------------------------------------------------------------------

// SizeLimitExceededException — ports git/exception/SizeLimitExceededException.
// Path "" corresponds to Java's Optional.empty() (unnamed file).
type SizeLimitExceededException struct {
	Path       string
	ActualSize int64
	MaxSize    int64
}

func (e *SizeLimitExceededException) Error() string { return "file too big" }
func (e *SizeLimitExceededException) Description() []string {
	filename := "There's a file"
	if e.Path != "" {
		filename = "File '" + e.Path + "' is"
	}
	return []string{
		filename + " too large to push to " + serviceName() + " via git",
		"the recommended maximum file size is 50 MiB",
	}
}

// FileLimitExceededException — ports git/exception/FileLimitExceededException.
type FileLimitExceededException struct{ NumFiles, MaxFiles int64 }

func (e *FileLimitExceededException) Error() string { return "too many files" }
func (e *FileLimitExceededException) Description() []string {
	return []string{
		fmt.Sprintf("repository contains %d files, which exceeds the limit of %d files", e.NumFiles, e.MaxFiles),
	}
}

// InvalidGitRepository — ports git/exception/InvalidGitRepository.
type InvalidGitRepository struct{}

func (InvalidGitRepository) Error() string { return "invalid git repo" }
func (InvalidGitRepository) Description() []string {
	return []string{
		"Your Git repository contains a reference we cannot resolve.",
		"If your project contains a Git submodule,",
		"please remove it and try again.",
	}
}

// RepositoryNotFoundException — JGit's type (not in source): message is the
// project name; no description lines.
type RepositoryNotFoundException struct{ ProjectName string }

func (e *RepositoryNotFoundException) Error() string         { return e.ProjectName }
func (e *RepositoryNotFoundException) Description() []string { return nil }

// ---------------------------------------------------------------------------
// git/handler/hook/exception/*
// ---------------------------------------------------------------------------

// WrongBranchException — ports WrongBranchException.
type WrongBranchException struct{ ExpectedRef string }

func NewWrongBranchException(expectedRef string) WrongBranchException {
	return WrongBranchException{ExpectedRef: expectedRef}
}

// branchName ports expectedRef.substring(lastIndexOf('/')+1).
func (WrongBranchException) branchName(expectedRef string) string {
	i := lastIndexN(expectedRef, '/')
	if i < 0 {
		return expectedRef
	}
	return expectedRef[i+1:]
}

func (e WrongBranchException) Error() string { return "wrong branch" }
func (e WrongBranchException) Description() []string {
	name := e.branchName(e.ExpectedRef)
	return []string{"You can't push any new branches.", "Please use the " + name + " branch."}
}

// ForcedPushException — ports ForcedPushException.
type ForcedPushException struct{}

func (ForcedPushException) Error() string { return "forced push prohibited" }
func (ForcedPushException) Description() []string {
	return []string{
		"You can't git push --force to a " + serviceName() + " project.",
		"Try to put your changes on top of the current head.",
		"If everything else fails, delete and reclone your repository, make your changes, then push again.",
	}
}

func lastIndexN(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}
