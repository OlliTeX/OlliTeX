package filestore

import (
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// --- SafeExec (1:1 with app/js/SafeExec.js): exec with a timeout ------------

type fseExecResult struct {
	Stdout string
	Stderr string
}

func fseExec(command []string, timeout time.Duration) (*fseExecResult, error) {
	if len(command) == 0 {
		return nil, fseFailedCommand("empty command")
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fseFailedCommand(err.Error())
	}
	if timeout > 0 {
		time.AfterFunc(timeout, func() {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		})
	}
	err := cmd.Wait()
	res := &fseExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		return res, fseFailedCommand(err.Error())
	}
	return res, nil
}
