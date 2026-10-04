// Command toolkit is the OlliTeX Toolkit TUI: a single static binary
// exposing the operator surface (stack lifecycle, logs, settings, doctor,
// backup) over SSH (charmbracelet/wish) with a Bubble Tea UI.
//
// Host contract: docker support (socket) + one mounted folder (data dir) +
// the config store DSN (Postgres — the single source of truth). No docker
// env settings plane: settings live in the config store, period.
//
// Run:
//
//	toolkit serve            SSH server (OLLITEX_TOOLKIT_SSH_LISTEN, etc.)
//	toolkit local            Bubble Tea UI on the local TTY (dev / docker -it)
//	toolkit doctor           one-shot health check (exits non-zero on failure)
//	toolkit version
package main

import (
	tea "github.com/charmbracelet/bubbletea"

	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/log"

	"ollitex/go/services/toolkit"
)

var ver = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "toolkit: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "version", "-v", "--version":
		fmt.Println("ollitex toolkit tui " + ver)
		return nil
	case "help", "-h", "--help":
		return usage()
	case "serve":
		return serve(rest)
	case "local":
		return local(rest)
	case "doctor":
		return doctor(rest)
	default:
		return fmt.Errorf("unknown command %q (see: toolkit help)", cmd)
	}
}

func usage() error {
	fmt.Print(`ollitex toolkit tui

Usage:
  toolkit serve     run the SSH (wish) server — for ssh host:2222
  toolkit local     run the TUI on the local TTY (docker run -it / dev)
  toolkit doctor    one-shot health check
  toolkit version

Environment:
  OLLITEX_TOOLKIT_SSH_LISTEN     default :2222
  OLLITEX_TOOLKIT_USER           default ollitex
  OLLITEX_TOOLKIT_PASSWORD       SSH password (or OLLITEX_TOOLKIT_PASSWORD_FILE)
  OLLITEX_TOOLKIT_DATA_DIR       the one mounted folder (default /opt/ollitex/data)
  OLLITEX_TOOLKIT_COMPOSE_FILE   default /opt/ollitex/toolkit.yaml
  OLLITEX_TOOLKIT_PROJECT        stack project name (default ollitex)
  CONFIG_DB_DSN                  Postgres config store DSN (single source of truth)
  CONFIG_DB_PATH                 EXPLICIT offline-emergency SQLite file (loud)
`)
	return nil
}

func passwordFrom(o *flag.FlagSet) (string, error) {
	v := o.Lookup("password").Value.String()
	if v == "" {
		v = os.Getenv(toolkit.EnvSSHPassword)
	}
	f := o.Lookup("password-file").Value.String()
	if f == "" {
		f = os.Getenv(toolkit.EnvSSHPasswordFl)
	}
	if v != "" && f != "" {
		return "", fmt.Errorf("set exactly one of -password / -password-file (or the env pair)")
	}
	if v != "" {
		return v, nil
	}
	if f != "" {
		b, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		s := strings.TrimSpace(string(b))
		if s == "" {
			return "", fmt.Errorf("password file %s is empty", f)
		}
		return s, nil
	}
	return "", fmt.Errorf("no SSH password: set OLLITEX_TOOLKIT_PASSWORD or %s (or -password / -password-file)", toolkit.EnvSSHPasswordFl)
}

func serve(args []string) error {
	f := flag.NewFlagSet("serve", flag.ExitOnError)
	listen := f.String("listen", envOr(toolkit.EnvSSHListen, ":2222"), "SSH listen address")
	user := f.String("user", envOr(toolkit.EnvSSHUser, "ollitex"), "SSH user")
	f.String("password", "", "SSH password (preferred: env/file)")
	f.String("password-file", "", "file containing the SSH password")
	dsn := f.String("dsn", os.Getenv(toolkit.EnvDSN), "config store Postgres DSN (overrides env chain)")
	dataDir := f.String("data-dir", envOr(toolkit.EnvDataDir, "/opt/ollitex/data"), "mounted data dir")
	composeFile := f.String("compose", envOr(toolkit.EnvComposeFile, "/opt/ollitex/toolkit.yaml"), "compose file")
	project := f.String("project", envOr(toolkit.EnvProjectName, "ollitex"), "stack project name")
	_ = f.Parse(args)

	pwd, err := passwordFrom(f)
	if err != nil {
		return err
	}
	t, err := toolkit.New(toolkit.Options{
		DSN:         *dsn,
		DataDir:     *dataDir,
		ComposeFile: *composeFile,
		Project:     *project,
	})
	if err != nil {
		return err
	}
	defer t.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return t.Serve(ctx, toolkit.ServerOpts{
		Listen:   *listen,
		User:     *user,
		Password: pwd,
		Log:      log.Default().With("svc", "toolkit"),
	})
}

func local(args []string) error {
	f := flag.NewFlagSet("local", flag.ExitOnError)
	dsn := f.String("dsn", os.Getenv(toolkit.EnvDSN), "config store Postgres DSN (overrides env chain)")
	dataDir := f.String("data-dir", envOr(toolkit.EnvDataDir, "./.ollitex-toolkit"), "mounted data dir")
	composeFile := f.String("compose", envOr(toolkit.EnvComposeFile, "./toolkit.yaml"), "compose file")
	project := f.String("project", envOr(toolkit.EnvProjectName, "ollitex"), "stack project name")
	_ = f.Parse(args)

	t, err := toolkit.New(toolkit.Options{
		DSN:         *dsn,
		DataDir:     *dataDir,
		ComposeFile: *composeFile,
		Project:     *project,
	})
	if err != nil {
		return err
	}
	defer t.Close()

	p := tea.NewProgram(toolkit.NewAppForLocal(t), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}

func doctor(args []string) error {
	f := flag.NewFlagSet("doctor", flag.ExitOnError)
	dsn := f.String("dsn", os.Getenv(toolkit.EnvDSN), "config store Postgres DSN (overrides env chain)")
	dataDir := f.String("data-dir", envOr(toolkit.EnvDataDir, "/opt/ollitex/data"), "mounted data dir")
	composeFile := f.String("compose", envOr(toolkit.EnvComposeFile, "/opt/ollitex/toolkit.yaml"), "compose file")
	project := f.String("project", envOr(toolkit.EnvProjectName, "ollitex"), "stack project name")
	_ = f.Parse(args)

	t, err := toolkit.New(toolkit.Options{
		DSN:         *dsn,
		DataDir:     *dataDir,
		ComposeFile: *composeFile,
		Project:     *project,
	})
	if err != nil {
		fmt.Println("✗ toolkit init: " + err.Error())
		return err
	}
	defer t.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := toolkit.Doctor(ctx, t)
	if err != nil {
		fmt.Println("✗ doctor: " + err.Error())
		return err
	}
	fail := false
	for _, r := range rows {
		if r.OK {
			fmt.Printf("  ● OK    %-16s %s\n", r.Label, r.Detail)
		} else {
			fmt.Printf("  ● FAIL  %-16s %s\n", r.Label, r.Detail)
			fail = true
		}
	}
	if fail {
		fmt.Println("doctor: FAIL")
		return fmt.Errorf("one or more checks failed")
	}
	fmt.Println("doctor: OK")
	return nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
