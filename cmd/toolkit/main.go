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

	"errors"
	"ollitex/go/services/toolkit"
)

var ver = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		code := 1
		var ec toolkit.ExitCode
		if errors.As(&ec, &err) {
			code = int(ec)
		} else {
			fmt.Fprintln(os.Stderr, "toolkit: "+err.Error())
		}
		if code != 0 {
			os.Exit(code)
		}
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
	case "init":
		return initcmd(rest)
	case "plan":
		return plancmd(rest)
	case "health":
		return healthcmd(rest)
	case "autofix":
		return autofixcmd(rest)
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
  toolkit init      first-boot seed of the config store (key + env + defaults, never clobbers)
  toolkit local     run the TUI on the local TTY (docker run -it / dev)
  toolkit doctor    one-shot health check
  toolkit health    cron-friendly container health (exit codes)
  toolkit autofix   autoheal pass (--once | --interval loop)
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

func plancmd(args []string) error {
	f := flag.NewFlagSet("plan", flag.ExitOnError)
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
		return err
	}
	defer t.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	plan, perr := t.Plan()
	if perr != nil {
		fmt.Fprintln(os.Stderr, "✗ plan: "+perr.Error())
		return perr
	}
	if rerr := t.RenderEnvFile(plan); rerr != nil {
		fmt.Fprintln(os.Stderr, "✗ env render: "+rerr.Error())
		return rerr
	}
	fmt.Println("project :", plan.Project)
	fmt.Println("overlays:")
	for _, f := range plan.Files {
		fmt.Println("  -", f)
	}
	fmt.Println("env file:", plan.EnvFile)
	if len(plan.Notes) > 0 {
		fmt.Println("notes:")
		for _, n := range plan.Notes {
			fmt.Println("  !", n)
		}
	}
	fmt.Println("validating (docker compose config --quiet):")
	out, verr := t.PlanValidate(ctx)
	if verr != nil {
		fmt.Println("✗ compose config FAILED")
		if out != "" {
			fmt.Println(out)
		}
		return verr
	}
	fmt.Println("✔ compose config OK — the plan is daemon-valid")
	return nil
}

func initcmd(args []string) error {
	f := flag.NewFlagSet("init", flag.ExitOnError)
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
		return err
	}
	defer t.Close()
	return toolkit.InitStore(t, os.Stdout)
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

// healthcmd: cron-friendly container health report (owner addendum C).
// Exit codes: 0 all up, 1 some unhealthy, 2 daemon/error, 3 project missing.
func healthcmd(args []string) error {
	f := flag.NewFlagSet("health", flag.ExitOnError)
	project := f.String("project", "ollitex", "stack project name")
	sock := f.String("sock", "/var/run/docker.sock", "docker socket")
	failOn := f.String("fail-on", "unhealthy", "exit 1 when a container is in this state")
	_ = f.Parse(args)

	d, err := toolkit.NewDocker(*sock)
	if err != nil {
		fmt.Fprintln(os.Stderr, "docker: "+err.Error())
		return fmt.Errorf("%w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	rows, err := d.ListProject(ctx, *project)
	if err != nil {
		fmt.Fprintln(os.Stderr, "list: "+err.Error())
		return err
	}
	if len(rows) == 0 {
		fmt.Println("no containers in project " + *project)
		return errNoProject
	}
	rc := 0
	for _, c := range rows {
		health := strings.TrimSpace(c.Health)
		state := c.State
		line := fmt.Sprintf("%-28s state=%-10s health=%s", c.Name, state, health)
		if health == "" {
			line += "  (no healthcheck)"
		}
		fmt.Println(line)
		if *failOn != "" && health == *failOn {
			rc = 1
		}
		if state != "running" && state != "exited" {
			rc = max(rc, 1)
		}
	}
	if rc == 0 {
		fmt.Println("OK: all containers of " + *project + " up")
	}
	return toolkit.ExitCode(rc)
}

// autofixcmd: one autoheal pass (owner addendum D).
func autofixcmd(args []string) error {
	f := flag.NewFlagSet("autofix", flag.ExitOnError)
	sock := f.String("sock", "/var/run/docker.sock", "docker socket")
	project := f.String("project", "ollitex", "stack project name")
	interval := f.Duration("interval", 15*time.Second, "poll interval (loop mode)")
	cooldown := f.Int("cooldown", 300, "per-container restart cooldown seconds (loop guard)")
	once := f.Bool("once", false, "run exactly one pass and exit")
	_ = f.Parse(args)

	d, err := toolkit.NewDocker(*sock)
	if err != nil {
		return err
	}
	h := toolkit.NewHealer(d, *project, toolkit.HealerPolicy{
		Interval:           *interval,
		StopTimeoutSeconds: 10,
		CooldownSeconds:    *cooldown,
	})
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	if *once {
		evs := h.Tick(ctx)
		if len(evs) == 0 {
			fmt.Println("no unhealthy containers — nothing to do")
		}
		for _, e := range evs {
			fmt.Printf("autoheal: %s %s %s\n", e.Action, e.Container, e.Reason)
		}
		return nil
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go h.Run(ctx)
	<-sig
	ctx.Done()
	fmt.Println("autofixer stopped")
	return nil
}

var errNoProject = fmt.Errorf("project not found")

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
