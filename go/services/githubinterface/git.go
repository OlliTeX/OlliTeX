package githubinterface

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// --- git CLI runner (1:1 with runGit) ---------------------------------------

type gitOpts struct {
	user, pass, cwd string
	extraEnv        []string
}

func ghiRandHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func runGit(ctx context.Context, cfg GHIConfig, args []string, o gitOpts) (string, error) {
	askpass := filepath.Join(os.TempDir(), fmt.Sprintf("ghif_askpass_%d_%s.sh", os.Getpid(), ghiRandHex(8)))
	user := o.user
	if user == "" {
		user = "git"
	}
	script := "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		fmt.Sprintf("  Username*) printf '%%s\\n' %s ;;\n", shellQuote(user)) +
		fmt.Sprintf("  Passw*) printf '%%s\\n' %s ;;\n", shellQuote(o.pass)) +
		"  *) printf '' ;;\n" +
		"esac\n"
	if werr := os.WriteFile(askpass, []byte(script), 0o700); werr != nil {
		return "", werr
	}
	defer os.Remove(askpass)

	fullArgs := append([]string{"-c", "core.quotepath=false"}, args...)
	cmd := exec.CommandContext(ctx, cfg.GitExe, fullArgs...)
	if o.cwd != "" {
		cmd.Dir = o.cwd
	}
	env := append(append(os.Environ(), "GIT_ASKPASS="+askpass, "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never"), o.extraEnv...)
	cmd.Env = env

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		return string(stdout), &GitError{Stdout: string(stdout), Stderr: stderr.String(), Err: err}
	}
	return string(stdout), nil
}
