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

	"github.com/charmbracelet/log"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
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
		// The TUI session — the AI-era replacement for the bubbletea
		// middleware (2026-10-07): the retained-mode tview app runs
		// in-process on a screen bound to the ssh session's io (the
		// server-side pipe is not a /dev/tty, so the stdio screen cannot
		// Start — SessionScreen injects a session-backed tcell.Tty). Each
		// connection keeps its own state model against the live docker
		// socket (the old per-session contract), and every screen boot
		// target (`ssh host hub`) still lands through the pendingStart
		// gate above.
		func(next ssh.Handler) ssh.Handler {
			return func(sess ssh.Session) {
				start := ""
				if v, ok := pendingStartScreens.LoadAndDelete(sess); ok {
					if ss, ok2 := v.(string); ok2 {
						start = ss
					}
				}
				runTUIOverSession(sess, t, start, o.Log)
				_ = next
			}
		},
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
					if start, ok := parseTUIBoot(cmd); ok {
						pendingStartScreens.Store(sess, start)
						next(sess)
						return
					}
					// a wrapped screen boot (`timeout N doctor`, `timeout 5 hub`) also
					// lands in the in-process TUI — a child exec has no controlling tty.
					if inner := stripTimeout(cmd); inner != cmd {
						if v, ok := parseTUIBoot(inner); ok {
							pendingStartScreens.Store(sess, v)
							next(sess)
							return
						}
						if fs := strings.Fields(inner); len(fs) == 1 && validScreen(newApp(t), fs[0]) {
							pendingStartScreens.Store(sess, fs[0])
							next(sess)
							return
						}
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

// runTUIOverSession — the TUI over one ssh session (in-process, the
// retained-mode replacement for the tea program over WithInput/WithOutput).
func runTUIOverSession(sess ssh.Session, t *Toolkit, start string, l *log.Logger) {
	app := newApp(t, start)
	// the client's terminal size (ssh pty allocation + live window-change
	// channel) — without this the TUI is pinned to 80x24 no matter the
	// operator's terminal (owner report: "controls don't work" / tiny UI).
	cols, rows := 0, 0
	var wins <-chan ssh.Window
	if pty, ch, ok := sess.Pty(); ok {
		cols, rows = int(pty.Window.Width), int(pty.Window.Height)
		wins = ch
	}
	screen, tty, err := SessionScreen(sess, cols, rows)
	if err != nil {
		l.Error("tui screen", "err", err.Error())
		fmt.Fprintln(sess, "toolkit: cannot attach the TUI screen: "+err.Error())
		sess.Close()
		return
	}
	if wins != nil {
		go func() {
			for w := range wins {
				tty.SetSize(int(w.Width), int(w.Height))
			}
		}()
	}
	// tview.Run only Init's a screen it created itself — an externally
	// injected screen must be Init'ed by its owner or inputLoop never
	// starts and every key is lost (verified on the live ssh smoke).
	if ierr := screen.Init(); ierr != nil {
		l.Error("tui screen init", "err", ierr.Error())
		fmt.Fprintln(sess, "toolkit: cannot start the TUI screen: "+ierr.Error())
		sess.Close()
		return
	}
	tv := NewTUI(app, screen)
	// bind the session io so teardown (q→yes / menu quit / bare q) can
	// write the terminal teardown and close the stream directly — tcell's
	// own Fini deadlocks on the blocked ssh read (see tviewApp.tearDown).
	tv.SetRIO(sess)
	if rerr := tv.Run(); rerr != nil {
		l.Debug("tui session ended", "err", rerr.Error())
	}
	sess.Close()
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

// parseTUIBoot — a TUI boot command (`tui` / `toolkit tui <flags>`, an
// optional leading `timeout N`) runs IN-PROCESS on the session screen — a
// child exec has no controlling tty, so the terminal screen cannot attach
// (open /dev/tty ENXIO). Returns the requested --screen ("" = dashboard).
func parseTUIBoot(raw string) (string, bool) {
	fields := strings.Fields(raw)
	i := 0
	if i < len(fields) && fields[i] == "timeout" {
		if i+2 > len(fields) {
			return "", false
		}
		i += 2
	}
	if i < len(fields) && fields[i] == "toolkit" && i+1 < len(fields) {
		i++
	}
	if i >= len(fields) || fields[i] != "tui" {
		return "", false
	}
	start := ""
	for j := i + 1; j < len(fields); j++ {
		switch {
		case fields[j] == "--screen" && j+1 < len(fields):
			start = fields[j+1]
			j++
		case strings.HasPrefix(fields[j], "--screen="):
			start = strings.TrimPrefix(fields[j], "--screen=")
		}
	}
	return start, true
}

// stripTimeout drops a leading `timeout N` (or `timeout -s KILL N`) so
// the wrapped form of a TUI boot is recognized.
func stripTimeout(raw string) string {
	f := strings.Fields(raw)
	if len(f) < 2 || f[0] != "timeout" {
		return raw
	}
	i := 1
	if f[i] == "-s" {
		i++
	}
	if i >= len(f) {
		return raw
	}
	return strings.Join(f[i+1:], " ")
}
