package toolkit

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/subtle"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/log"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	teaMiddleware "github.com/charmbracelet/wish/bubbletea"
)

// ServerOpts configures the wish SSH endpoint.
type ServerOpts struct {
	// Listen, e.g. ":2222" or "0.0.0.0:2222".
	Listen string
	// User is the allowed SSH user (the only authenticated user).
	User string
	// Password is the (non-empty) allowed SSH password.
	Password string
	// Logger.
	Log *log.Logger
}

// Serve runs the wish SSH server until ctx is canceled.
func (t *Toolkit) Serve(ctx context.Context, o ServerOpts) error {
	if o.Listen == "" {
		o.Listen = ":2222"
	}
	if o.User == "" {
		o.User = "ollitex"
	}
	if o.Log == nil {
		o.Log = log.Default().With("svc", "toolkit-tui")
	}
	if o.Password == "" {
		return fmt.Errorf("no SSH password configured (set %s or a password file)", EnvSSHPassword)
	}

	hostKey := t.HostKeyPath()
	if _, err := os.Stat(hostKey); os.IsNotExist(err) {
		if cerr := writeHostKey(hostKey); cerr != nil {
			return fmt.Errorf("host key: %w", cerr)
		}
		o.Log.Info("generated SSH host key", "path", hostKey)
	}

	userGate := func(user string) bool {
		return subtle.ConstantTimeCompare([]byte(user), []byte(o.User)) == 1
	}

	srv, err := wish.NewServer(
		wish.WithHostKeyPath(hostKey),
		wish.WithPasswordAuth(func(ctx ssh.Context, pass string) bool {
			user := ctx.User()
			ok := userGate(user) &&
				subtle.ConstantTimeCompare([]byte(pass), []byte(o.Password)) == 1
			o.Log.Info("ssh auth", "user", user, "ok", ok)
			return ok
		}),
		wish.WithMiddleware(
			teaMiddleware.Middleware(func(sess ssh.Session) (tea.Model, []tea.ProgramOption) {
				return newApp(t), []tea.ProgramOption{tea.WithInput(sess), tea.WithOutput(sess), tea.WithAltScreen()}
			}),
			// Both middlewares go in ONE WithMiddleware call: wish.WithMiddleware
			// does `s.Handler = h` per option, so two separate options REPLACE
			// each other (last wins) — which silently drops the TUI middleware
			// and makes every interactive session close right after auth
			// ("Connection to ... closed", zero bytes out; the CLI path kept
			// working because the surviving gate implements it itself). Order in
			// the call: last-added runs first → gate/route runs first and its
			// next() lands in the TUI.
			// Gate + routing (outermost). exec with a command string
			// (e.g. `ssh host "toolkit health"`) runs the CLI and exits; an
			// interactive shell lands in the TUI below.
			func(next ssh.Handler) ssh.Handler {
				return func(sess ssh.Session) {
					if !userGate(sess.User()) {
						fmt.Fprintln(sess, "toolkit: access denied for user "+sess.User())
						return
					}
					if cmd := strings.TrimSpace(sess.RawCommand()); cmd != "" {
						runCLI(sess, cmd)
						return
					}
					next(sess)
				}
			},
		),
	)
	if err != nil {
		return fmt.Errorf("wish server: %w", err)
	}
	defer srv.Close()

	ln, err := net.Listen("tcp", o.Listen)
	if err != nil {
		return fmt.Errorf("listen %s: %w", o.Listen, err)
	}
	o.Log.Info("toolkit TUI listening (ssh)",
		"listen", o.Listen, "user", o.User,
		"data", t.DataDir, "store", t.StoreDescribe)

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
		o.Log.Info("shutting down")
		_ = srv.Shutdown(context.WithoutCancel(ctx))
		select {
		case <-time.After(10 * time.Second):
		case <-serveErr:
		}
		return nil
	case err := <-serveErr:
		return err
	}
}

// to the session (the multi-command owner usage: `ssh host "toolkit ..."`).
func runCLI(sess ssh.Session, cmd string) {
	c := exec.CommandContext(sess.Context(), "/bin/sh", "-c", cmd)
	c.Env = append(os.Environ(), "PATH=/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	var out bytes.Buffer
	c.Stdout = &out
	c.Stderr = &out
	err := c.Run()
	sess.Write(out.Bytes())
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			sess.Exit(ee.ExitCode())
			return
		}
		fmt.Fprintf(sess, "\ntoolkit: %v\n", err)
		sess.Exit(1)
		return
	}
	sess.Exit(0)
}

// writeHostKey generates a persistent ECDSA P-256 host key (0600).
func writeHostKey(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: "PRIVATE KEY", Bytes: der})
}
