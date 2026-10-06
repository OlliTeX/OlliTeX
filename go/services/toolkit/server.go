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
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/log"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	teaMiddleware "github.com/charmbracelet/wish/bubbletea"
	xssh "golang.org/x/crypto/ssh"
)

// ServerOpts configures the wish SSH endpoint.
type ServerOpts struct {
	// Listen, e.g. ":2222" or "0.0.0.0:2222".
	Listen string
	// User is the allowed SSH user (the only authenticated user).
	User string
	// Password is the allowed SSH password. Empty disables password auth —
	// a key-only deployment (owner directive 2026-10-06) leaves this unset.
	Password string
	// KeyFile is the authorized_keys file (publickeys only; one per line, ssh-keygen
	// format). When the file is absent, key auth simply has no keys to accept.
	// Owner flow: the host keeps ${PREFIX}/ssh mounted at /opt/ollitex/ssh —
	// the init step generates an ed25519 pair there when authorized_keys is
	// missing, so a fresh install is key-ready with zero manual steps.
	KeyFile string
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
	if o.Password == "" && o.KeyFile == "" {
		return fmt.Errorf("no SSH auth method configured (set %s or a key file / authorized_keys)", EnvSSHPassword)
	}
	if o.KeyFile == "" {
		o.KeyFile = "/opt/ollitex/ssh/authorized_keys"
	}

	// Public-key auth (preferred path, owner directive: keys over passwords).
	var pubKeys []ssh.PublicKey
	if kdata, kerr := os.ReadFile(o.KeyFile); kerr == nil {
		for ln, line := range strings.Split(string(kdata), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if k, _, _, _, perr := xssh.ParseAuthorizedKey([]byte(line)); perr == nil {
				pubKeys = append(pubKeys, k)
			} else {
				o.Log.Warn("authorized_keys: skipping line", "line", ln+1, "err", perr.Error())
			}
		}
		if len(pubKeys) > 0 {
			o.Log.Info("ssh pubkey auth enabled", "keys", len(pubKeys), "file", o.KeyFile)
		} else {
			o.Log.Info("ssh pubkey auth: no authorized_keys found (password-only until one is added)", "path", o.KeyFile)
		}
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

	// Auth options: public keys (preferred) + optional password fallback.
	// Both are CONDITIONAL options (a key-only deployment has no password, and
	// a password-only bootstrap has no key file yet).
	authOpts := []ssh.Option{}
	if len(pubKeys) > 0 {
		authOpts = append(authOpts, wish.WithPublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
			if !userGate(ctx.User()) {
				o.Log.Info("ssh pubkey auth rejected (user)", "user", ctx.User())
				return false
			}
			for _, ak := range pubKeys {
				if ssh.KeysEqual(ak, key) {
					o.Log.Info("ssh pubkey auth ok", "user", ctx.User(), "keytype", key.Type())
					return true
				}
			}
			return false
		}))
	}
	if o.Password != "" {
		authOpts = append(authOpts, wish.WithPasswordAuth(func(ctx ssh.Context, pass string) bool {
			user := ctx.User()
			// Guard against the "empty password matches empty password" hole:
			// password auth is only active for a NON-EMPTY configured password.
			ok := userGate(user) && pass != "" &&
				subtle.ConstantTimeCompare([]byte(pass), []byte(o.Password)) == 1
			return ok
		}))
	}
	// FAIL CLOSED: with NO active auth method, wish would fall back to
	// permitting password logins — exactly the hole that lets an unconfigured
	// toolkit accept any password. Refuse to serve instead.
	if len(authOpts) == 0 {
		return fmt.Errorf("refusing to serve SSH with no auth method active (no %s found and no password configured)", o.KeyFile)
	}

	// pendingStartScreens: the gate records a screen-boot request (e.g.
	// `ssh host hub`) keyed by the session; the TUI middleware consumes it
	// once (both middlewares share the same ssh.Session instance).
	var pendingStartScreens sync.Map
	allOpts := append([]ssh.Option{wish.WithHostKeyPath(hostKey)}, authOpts...)
	allOpts = append(allOpts, wish.WithMiddleware(
		teaMiddleware.Middleware(func(sess ssh.Session) (tea.Model, []tea.ProgramOption) {
			start := ""
			if v, ok := pendingStartScreens.LoadAndDelete(sess); ok {
				if ss, ok2 := v.(string); ok2 {
					start = ss
				}
			}
			return newApp(t, start), []tea.ProgramOption{tea.WithInput(sess), tea.WithOutput(sess), tea.WithAltScreen()}
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
					// a master-list screen id boots the TUI straight there
					// (`ssh host hub` · `doctor` · `backup` ...)
					if validScreen(newApp(t), cmd) {
						pendingStartScreens.Store(sess, cmd)
						next(sess)
						return
					}
					runCLI(sess, cmd)
					return
				}
				next(sess)
			}
		},
	))
	srv, err := wish.NewServer(allOpts...)
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
