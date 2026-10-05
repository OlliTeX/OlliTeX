package toolkit

// TestServeInteractiveTUIOverSSH — regression pin for the live "sign-in
// closes instantly" defect (owner report: `ssh -p 2222 ollitex@host` →
// password accepted → "Connection to localhost closed" immediately, every
// time, while `ssh ... "echo hi"` kept working).
//
// Root cause: wish.WithMiddleware does `s.Handler = h` per option, so TWO
// separate WithMiddleware options REPLACE each other (the last one wins)
// — silently dropping the TUI middleware. Interactive (PTY, no-command)
// sessions then hit the no-op tail and close right after auth with zero
// bytes; the CLI exec path kept working because the surviving gate
// middleware implements runCLI itself.
//
// Fix under test: ONE WithMiddleware call carrying both middlewares
// (order: last-added runs first → gate/route first, its next() into the
// TUI). This test boots the real Serve() on a local port and asserts both
// halves with the same toolchain an operator uses:
//  1. an interactive `ssh -tt` session receives TUI frame bytes (no instant close),
//  2. a command (`ssh ... "echo ..."`) session still runs the CLI.

import (
	"context"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	clog "github.com/charmbracelet/log"
	xssh "golang.org/x/crypto/ssh"
)

func startToolkitServe(t *testing.T) (port int) {
	t.Helper()
	dataDir := t.TempDir()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port = l.Addr().(*net.TCPAddr).Port
	_ = l.Close() // Serve re-listens on the same port

	tk := &Toolkit{
		Store:        nil, // no store: the app must still boot (offline mode)
		DataDir:      dataDir,
		Project:      "ollitex",
		DockerSocket: dataDir + "/no-such-sock", // no docker here: offline banner, not a quit
		Ver:          "test",
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- tk.Serve(ctx, ServerOpts{
			Listen:   fmt.Sprintf("127.0.0.1:%d", port),
			User:     "ollitex",
			Password: "tui-pw-123",
			Log:      clog.New(io.Discard),
		})
	}()
	t.Cleanup(cancel)

	deadline := time.Now().Add(5 * time.Second)
	for {
		c, derr := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if derr == nil {
			c.Close()
			return port
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not come up on 127.0.0.1:%d: %v", port, derr)
		}
		select {
		case serr := <-errCh:
			t.Fatalf("serve exited early: %v", serr)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// testInteractiveSSH drives the SAME client an operator uses (ssh -tt over
// sshpass). A LIVE TUI session outlives the client deadline (the ctx kill
// is the success mode); an instant-close regression returns quickly with
// nothing on the wire.
func testInteractiveSSH(t *testing.T, port int, password string) string {
	if _, err := exec.LookPath("sshpass"); err != nil {
		t.Skipf("sshpass not available: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx,
		"sshpass", "-p", password,
		"ssh", "-tt",
		"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
		"-p", strconv.Itoa(port), "ollitex@127.0.0.1")
	out, _ := cmd.CombinedOutput()
	return string(out)
}

func TestServeInteractiveTUIOverSSH(t *testing.T) {
	port := startToolkitServe(t)

	// --- 1) interactive PTY session must receive TUI bytes -------------
	out := testInteractiveSSH(t, port, "tui-pw-123")
	if strings.TrimSpace(out) == "" {
		t.Fatalf("interactive session produced ZERO bytes — the TUI middleware is not wired in (two WithMiddleware options overwrite each other; use ONE call). Owner symptom: 'Connection closed' right after the password.")
	}
	hasUI := strings.Contains(out, "OlliTeX Toolkit") ||
		strings.Contains(out, "\x1b[?1049h") || // alt-screen on
		strings.Contains(out, "\x1b[?25") // cursor hide/show
	if !hasUI {
		t.Errorf("expected TUI frame bytes (alt-screen / toolbar), got %d bytes starting %q", len(out), out[:min(120, len(out))])
	}

	// --- 2) the CLI/exec path still works (no over-correction) ---------
	cfg := &xssh.ClientConfig{
		User:            "ollitex",
		Auth:            []xssh.AuthMethod{xssh.Password("tui-pw-123")},
		HostKeyCallback: xssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	c, derr := xssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), cfg)
	if derr != nil {
		t.Fatalf("dial: %v", derr)
	}
	defer c.Close()
	s, serr := c.NewSession()
	if serr != nil {
		t.Fatalf("cli session: %v", serr)
	}
	defer s.Close()
	cmdOut, rerr := s.CombinedOutput("echo CLI-OK-PI")
	if rerr != nil {
		t.Fatalf("cli run: %v (out=%s)", rerr, cmdOut)
	}
	if !strings.Contains(string(cmdOut), "CLI-OK-PI") {
		t.Errorf("cli output = %q, want CLI-OK-PI", cmdOut)
	}
}
