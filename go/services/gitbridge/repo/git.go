package repo

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// runGit runs `git <args>` with working directory dir. An optional env
// layer (map of KEY=VALUE) is appended to the current environment. It takes
// an optional stdin string. Returns stdout (trimmed) + error (nil when the
// process exits 0).
func runGit(dir string, env map[string]string, stdin string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	envSlice := os.Environ()
	for k, v := range env {
		envSlice = append(envSlice, k+"="+v)
	}
	cmd.Env = envSlice
	var out, stderr strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	if err := cmd.Start(); err != nil {
		return out.String(), err
	}
	if err := cmd.Wait(); err != nil {
		return out.String(), fmt.Errorf("git %v: %v (stderr: %s)", args, err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}

// runGitInDir runs `git <args>` in dir (no stdin), returning stdout.
func runGitInDir(dir string, env map[string]string, args ...string) (string, error) {
	return runGit(dir, env, "", args...)
}
