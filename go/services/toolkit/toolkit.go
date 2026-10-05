// Package toolkit is the OlliTeX Toolkit TUI (owner directive 2026-10-06):
// a single static Go binary exposing the toolkit's operations over
// charmbracelet/wish (SSH) with a Bubble Tea UI — replacing the toolkit/bin
// scripts. The host contract is just: docker support (socket mounted into
// the image) + one mounted local folder (data directory).
//
// Design points:
//   - The Postgres config store (go/libraries/configstore, DSN per the
//     existing single-source-of-truth decision) is the ONE settings source:
//     the TUI reads/writes settings through the configstore Store interface
//     — no per-key docker env fallbacks.
//   - Docker operations go through the official moby client (docker socket)
//     for structured status + log streaming; container *orchestration*
//     (ordered up/down of the whole stack) goes through the compose CLI
//     bundled in the image.
//   - Community/SaaS split: removed. The toolkit models THIS stack's
//     features (mongo, redis, postgres, seaweedfs, nginx/tls, languagetool,
//     sibling compile containers) only.
package toolkit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"ollitex/go/libraries/configstore"
)

// Defaults (env-overridable; the TUI surfaces these in About/Doctor so an
// operator always knows which plane it is talking to).
const (
	EnvSSHListen     = "OLLITEX_TOOLKIT_SSH_LISTEN" // default 0.0.0.0:2222
	EnvSSHUser       = "OLLITEX_TOOLKIT_USER"       // default ollitex
	EnvSSHPassword   = "OLLITEX_TOOLKIT_PASSWORD"
	EnvSSHPasswordFl = "OLLITEX_TOOLKIT_PASSWORD_FILE"
	EnvDataDir       = "OLLITEX_TOOLKIT_DATA_DIR" // the one mounted folder
	EnvComposeFile   = "OLLITEX_TOOLKIT_COMPOSE_FILE"
	EnvProjectName   = "OLLITEX_TOOLKIT_PROJECT" // default ollitex
	EnvDSN           = "CONFIG_DB_DSN"           // configstore single source of truth
)

// Toolkit is the shared application handle for a TUI session (or the local
// dev run). It owns the config store handle + docker client + paths.
type Toolkit struct {
	// Store is the config store (Postgres primary; explicit SQLite offline
	// emergency only — same contract as cmd/configdb).
	Store configstore.Store
	// StoreDescribe is the redacted identity of the backing store (for the
	// Dashboard/About surfaces).
	StoreDescribe string
	// DataDir is the one mounted folder (backups, host key, logs, state).
	DataDir string
	// ComposeFile drives stack orchestration (bundled default: /opt/ollitex/toolkit.yaml).
	ComposeFile string
	// Project is the stack identity (project name / container prefix).
	Project string
	// DockerSocket is the mounted docker socket path.
	DockerSocket string
	// Ver is the build revision string (set at link time via -ldflags).
	Ver string
}

// Options builds a Toolkit.
type Options struct {
	DataDir     string
	ComposeFile string
	Project     string
	DockerSock  string
	// DSN is the configstore Postgres DSN ("" = resolve from the standard
	// env chain CONFIG_DB_DSN ‖ DATABASE_URL ‖ HISTORY_CONNECTION_STRING).
	DSN string
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// New constructs a Toolkit and opens the config store (strict: no implicit
// fallbacks — mirroring cmd/configdb's single-source contract).
func New(opts Options) (*Toolkit, error) {
	if opts.DataDir == "" {
		opts.DataDir = envOr(EnvDataDir, "/opt/ollitex/data")
	}
	if opts.ComposeFile == "" {
		opts.ComposeFile = envOr(EnvComposeFile, "/opt/ollitex/toolkit.yaml")
	}
	if opts.Project == "" {
		opts.Project = envOr(EnvProjectName, "ollitex")
	}
	if opts.DockerSock == "" {
		opts.DockerSock = envOr("DOCKER_HOST_UNIX", os.Getenv("DOCKER_SOCKET_PATH"))
	}
	if opts.DockerSock == "" {
		opts.DockerSock = "/var/run/docker.sock"
	}
	if err := os.MkdirAll(opts.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("data dir: %w", err)
	}

	var ds configstore.Store
	var offline bool
	dsn := opts.DSN
	if dsn == "" {
		dsn = configstore.DSNFromEnv()
	}
	if dsn != "" {
		var err error
		ds, err = configstore.DialPG(dsn)
		if err != nil {
			return nil, fmt.Errorf("open PG config store: %w", err)
		}
	} else if p := configstore.OfflinePath(); p != "" {
		var err error
		ds, err = configstore.New(p)
		if err != nil {
			return nil, fmt.Errorf("open explicit offline config store (%s): %w", p, err)
		}
		offline = true
	} else {
		return nil, fmt.Errorf("no config store: set %s (Postgres — the single source of truth) or %s (EXPLICIT offline-emergency SQLite file)", EnvDSN, configstore.SQLiteEnv)
	}
	if offline {
		fmt.Fprintln(os.Stderr, "⚠ OFFLINE EMERGENCY MODE: local SQLite file — NOT the shared config store")
	}

	return &Toolkit{
		Store:         ds,
		StoreDescribe: ds.Describe(),
		DataDir:       opts.DataDir,
		ComposeFile:   opts.ComposeFile,
		Project:       opts.Project,
		DockerSocket:  opts.DockerSock,
	}, nil
}

// Close releases the store handle.
func (t *Toolkit) Close() {
	if t.Store != nil {
		_ = t.Store.Close()
	}
}

// HostKeyPath is where the persistent SSH host key lives (inside the one
// mounted folder, so it survives image upgrades).
func (t *Toolkit) HostKeyPath() string { return filepath.Join(t.DataDir, "ssh_host_key") }

// BackupPath returns the canonical backup file location.
func (t *Toolkit) BackupPath() string { return filepath.Join(t.DataDir, "config-backup.json") }

// BackupDir is where the toolkit/backup shell suite lives for the running
// TUI: baked into the image at /opt/ollitex/backup (canonical), or
// OLLITEX_TOOLKIT_BACKUP_DIR when the repo copy should be driven instead
// (host-side dev/testing).
func (t *Toolkit) BackupDir() string {
	if d := os.Getenv("OLLITEX_TOOLKIT_BACKUP_DIR"); d != "" {
		return d
	}
	return "/opt/ollitex/backup"
}

// Context is the ctx used by background work.
func (t *Toolkit) Context(ctx context.Context) context.Context { return ctx }

// ExitCode is a sentinel error carrying a process exit code (0 = OK).
type ExitCode int

func (e ExitCode) Error() string { return "exit " + itoa(int(e)) }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	d := ""
	for n > 0 {
		d = string(rune('0'+n%10)) + d
		n /= 10
	}
	return d
}
