// Tests port the Java assertion-based exception tests (WrongBranchException
// Test patterns applied across the exception classes) plus wire-layer
// builder assertions.
package giterrors

import (
	"strings"
	"testing"
)

// TestWrongBranchExceptionMain ports WrongBranchExceptionTest
// (messageIncludesMainBranch).
func TestWrongBranchExceptionMain(t *testing.T) {
	e := NewWrongBranchException("refs/heads/main")
	if e.Error() != "wrong branch" {
		t.Fatalf("Error() = %q, want %q", e.Error(), "wrong branch")
	}
	// WrongBranchException's description does not embed the service name.
	if got := e.Description(); len(got) != 2 || got[0] != "You can't push any new branches." || got[1] != "Please use the main branch." {
		t.Fatalf("Description() = %v", got)
	}
}

// TestWrongBranchExceptionMaster ports messageIncludesMasterBranch.
func TestWrongBranchExceptionMaster(t *testing.T) {
	e := NewWrongBranchException("refs/heads/master")
	if got := e.Description(); len(got) != 2 || got[1] != "Please use the master branch." {
		t.Fatalf("Description() = %v", got)
	}
}

// TestWrongBranchExceptionNoSlash covers the no-ref-branch edge (the Java
// substring edge where lastIndexOf('/') < 0).
func TestWrongBranchExceptionNoSlash(t *testing.T) {
	e := NewWrongBranchException("main")
	if got := e.Description(); len(got) != 2 || got[1] != "Please use the main branch." {
		t.Fatalf("Description() = %v", got)
	}
}

func TestSizeLimitExceededDescription(t *testing.T) {
	SetServiceNameFn(func() string { return "Overleaf" })
	defer SetServiceNameFn(func() string { return "Overleaf" })

	e := &SizeLimitExceededException{Path: "big/file.pdf", ActualSize: 51 << 20, MaxSize: 50 << 20}
	if e.Error() != "file too big" {
		t.Fatalf("Error() = %q, want %q", e.Error(), "file too big")
	}
	desc := e.Description()
	if len(desc) != 2 {
		t.Fatalf("len(Description) = %d, want 2", len(desc))
	}
	if !strings.Contains(desc[0], "File 'big/file.pdf' is") {
		t.Fatalf("size limit description = %v, want it to name the file", desc)
	}
	if desc[1] != "the recommended maximum file size is 50 MiB" {
		t.Fatalf("desc[1] = %q", desc[1])
	}

	unnamed := &SizeLimitExceededException{}
	if got := unnamed.Description(); len(got) != 2 || !strings.Contains(got[0], "There's a file") {
		t.Fatalf("unnamed size limit description = %v, want 'There's a file'", got)
	}
}

func TestFileLimitExceededError(t *testing.T) {
	e := &FileLimitExceededException{NumFiles: 501, MaxFiles: 500}
	if e.Error() != "too many files" {
		t.Fatalf("Error() = %q, want %q", e.Error(), "too many many files")
	}
	if got := e.Description(); len(got) != 1 || got[0] != "repository contains 501 files, which exceeds the limit of 500 files" {
		t.Fatalf("Description() = %v", got)
	}
}

func TestInvalidGitRepositoryError(t *testing.T) {
	e := InvalidGitRepository{}
	if e.Error() != "invalid git repo" {
		t.Fatalf("Error() = %q, want %q", e.Error(), "invalid git repo")
	}
	if len(e.Description()) != 3 {
		t.Fatalf("len(Description) = %d, want 3", len(e.Description()))
	}
}

func TestRepositoryNotFoundError(t *testing.T) {
	e := RepositoryNotFoundException{ProjectName: "some-project"}
	if e.Error() != "some-project" {
		t.Fatalf("Error() = %q, want the project name", e.Error())
	}
	if e.Description() != nil {
		t.Fatalf("Description() = %v, want nil", e.Description())
	}
}

func TestForcedPushError(t *testing.T) {
	SetServiceNameFn(func() string { return "Overleaf" })
	defer SetServiceNameFn(func() string { return "Overleaf" })
	e := ForcedPushException{}
	if e.Error() != "forced push prohibited" {
		t.Fatalf("Error() = %q, want %q", e.Error(), "forced push prohibited")
	}
	desc := e.Description()
	if len(desc) != 3 || desc[0] != "You can't git push --force to a Overleaf project." {
		t.Fatalf("Description() = %v", desc)
	}
}

func TestSnapshotPostErrors(t *testing.T) {
	SetServiceNameFn(func() string { return "Overleaf" })
	defer SetServiceNameFn(func() string { return "Overleaf" })

	outOfDate := &OutOfDateException{}
	if outOfDate.Error() != "out of date" {
		t.Fatalf("OutOfDate Error() = %q", outOfDate.Error())
	}

	invalid := buildInvalidProjectException([]byte(`{"errors":["err one","err two"]}`))
	if invalid.Error() != "invalid project" {
		t.Fatalf("InvalidProject Error() = %q", invalid.Error())
	}
	if got := invalid.Description(); len(got) != 2 || got[0] != "err one" || got[1] != "err two" {
		t.Fatalf("InvalidProject Description() = %v", got)
	}

	invalidFilesErr := buildInvalidFilesException([]byte(`{"errors":[{"file":"f.tex","cleanFile":"f_clean.tex","state":"invalid"},{"file":"g.tex","state":"disallowed"}]}`))
	if invalidFilesErr.Error() != "invalid files" {
		t.Fatalf("InvalidFiles Error() = %q", invalidFilesErr.Error())
	}
	desc := invalidFilesErr.Description()
	if len(desc) != 3 {
		t.Fatalf("len(InvalidFiles Description) = %d, want 3", len(desc))
	}
	if !strings.Contains(desc[0], "You have 2 invalid files in your Overleaf project:") {
		t.Fatalf("InvalidFiles desc[0] = %q", desc[0])
	}
	if desc[1] != "f.tex (rename to: f_clean.tex)" {
		t.Fatalf("InvalidFiles desc[1] = %q, want the rename hint", desc[1])
	}
	if desc[2] != "g.tex (invalid file extension)" {
		t.Fatalf("InvalidFiles desc[2] = %q, want the invalid-extension hint", desc[2])
	}

	// the errorEntry 'error' fallback (state not "disallowed", no cleanFile)
	errsEntry := errorEntry{File: "h.tex"}
	if got := errsEntry.describe(); got != "h.tex (error)" {
		t.Fatalf("describe fallback = %q, want 'h.tex (error)'", got)
	}

	unexpected := &UnexpectedErrorException{}
	if unexpected.Error() != "Overleaf error" {
		t.Fatalf("UnexpectedError Error() = %q", unexpected.Error())
	}
	if len(unexpected.Description()) != 2 {
		t.Fatalf("UnexpectedError Description() = %v", unexpected.Description())
	}

	internal := &InternalErrorException{}
	if internal.Error() != "internal error" {
		t.Fatalf("InternalError Error() = %q", internal.Error())
	}

	timeout := &PostbackTimeoutException{TimeoutSeconds: 360}
	if timeout.Error() != "Request timed out (after 360 seconds)" {
		t.Fatalf("PostbackTimeout Error() = %q", timeout.Error())
	}

	if (InvalidPostbackKeyException{}).Error() == "" {
		t.Fatalf("InvalidPostbackKey must have an error message")
	}
	if (UnexpectedPostbackException{}).Error() == "" {
		t.Fatalf("UnexpectedPostback must have an error message")
	}
}

func TestSnapshotAPIErrors(t *testing.T) {
	if (ForbiddenException{}).Error() != "forbidden" {
		t.Fatalf("Forbidden Error() wrong")
	}
	if (FailedConnectionException{}).Error() != "Overleaf server not available. Please try again later." {
		t.Fatalf("FailedConnection Error() wrong")
	}
	gen := GenericMissingRepositoryException()
	if gen.Error() != "no git access" {
		t.Fatalf("MissingRepository Error() wrong")
	}
	if len(gen.Description()) == 0 {
		t.Fatalf("MissingRepository has no description lines")
	}
	if got := (&GetDocInvalidProjectException{}).Error(); got != "invalid project" {
		t.Fatalf("GetDocInvalidProject Error() wrong")
	}
}

func TestMessageBuilders(t *testing.T) {
	// GenReason != empty
	if len(GenReason()) == 0 {
		t.Fatalf("GenReason empty")
	}
	// deprecated: with and without URL
	if len(DeprecatedMessage("")) == 0 {
		t.Fatalf("DeprecatedMessage(\"\") empty")
	}
	if got := DeprecatedMessage("https://new.example.com/edit/x"); len(got) == 0 || !strings.Contains(got[4], "https://new.example.com/edit/x") {
		t.Fatalf("DeprecatedMessage(url) does not embed url: %v", got)
	}
	// exported to v2: with and without URL
	if len(ExportV2Message("")) == 0 {
		t.Fatalf("ExportV2Message(\"\") empty")
	}
	if got := ExportV2Message("http://x"); len(got) == 0 || !strings.Contains(got[3], "http://x") {
		t.Fatalf("ExportV2Message(url) does not embed url: %v", got)
	}
}

func TestSetServiceNameFallback(t *testing.T) {
	// No bound fn: the default "Overleaf" is used (Java: the static bound in
	// Util). Bind and unbind.
	SetServiceNameFn(nil)
	if (&UnexpectedPostbackException{}).Error() == "" {
		t.Fatalf("unexpected postback must have a message")
	}
	SetServiceNameFn(func() string { return "Overleaf" })
}
