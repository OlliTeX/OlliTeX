// Package githubinterface is the Go 1:1 rewrite of the Node githubinterface
// service (services/githubinterface/app/src/*). Split from the single top-level
// services file into logical files mirroring the Node module layout (server.mjs
// routes, GitServerClient.mjs, runGit). Shared HTTP helpers are imported from
// ollitex/go/pbhttp.
package githubinterface

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// GitError captures git CLI failure (stdout + stderr) for messages/redaction.
type GitError struct {
	Stdout string
	Stderr string
	Err    error
}

func (e *GitError) Error() string {
	if e.Stderr != "" {
		return e.Stderr
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "git error"
}
func (e *GitError) Unwrap() error { return e.Err }

// GHIError carries an HTTP status + message.
type GHIError struct {
	StatusCode int
	Message    string
}

func (e *GHIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "githubinterface error"
}

func ghiStatusCode(err error) int {
	var ge *GHIError
	if errors.As(err, &ge) {
		return ge.StatusCode
	}
	return 0
}

func ghiStatusErr(code int, msg string) *GHIError {
	return &GHIError{StatusCode: code, Message: msg}
}

// GHIConfig is the GithubInterface runtime configuration.
type GHIConfig struct {
	WorkRoot     string
	MaxOps       int
	ServiceToken string
	GitExe       string // default "git"
	HTTPClient   *http.Client
}

func (c *GHIConfig) withDefaults() {
	if c.WorkRoot == "" {
		c.WorkRoot = filepath.Join(os.TempDir(), "ghif")
	}
	if c.MaxOps <= 0 {
		c.MaxOps = 8
	}
	if c.GitExe == "" {
		c.GitExe = "git"
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
}
