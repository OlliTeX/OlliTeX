// git-bridge cmd: the PRODUCTION proc-receive hook entry (HANDOFF §14.3/§15).
//
// Option 3a splits the Java write path across two processes:
//
//  1. the shelled `git receive-pack --stateless-rpc` (exec'd by the HTTP
//     server layer, receivepack.go) — owns the smart-HTTP wire and spawns
//     the proc-receive hook for every push;
//  2. this binary — .git/hooks/proc-receive (a shim `#!/bin/sh` +
//     `exec git_bridge -hook-proc-receive <config-path>`), which ports Java
//     WriteLatexPutHook + Bridge.
//
// The HTTP server boot was Phase 5 (the shelled handler is wired there).
//
// Env handler-contract (HANDOFF §14.3): PROJECT=<name> (required),
// HOST=<hostname> (informational), OAUTH2_TOKEN=<bearer> (if set),
// GIT_PROTOCOL=<proto> (if set). git itself adds GIT_DIR='.' (live-probed);
// procreceive.go pops it before any child git command.
package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"ollitex/go/services/gitbridge/bridge"
	"ollitex/go/services/gitbridge/config"
	"ollitex/go/services/gitbridge/data"
	"ollitex/go/services/gitbridge/db"
	"ollitex/go/services/gitbridge/giterrors"
	"ollitex/go/services/gitbridge/gitproto"
	"ollitex/go/services/gitbridge/repo"
	"ollitex/go/services/gitbridge/server"
	"ollitex/go/services/gitbridge/snapshot"
	gitswap "ollitex/go/services/gitbridge/swap"
	"ollitex/go/services/gitbridge/util"
	"ollitex/go/services/gitbridge/wglog"
)

func main() {
	for i, a := range os.Args {
		if a == "-hook-proc-receive" {
			if i+1 >= len(os.Args) {
				fmt.Fprintln(os.Stderr, "git_bridge: -hook-proc-receive requires <config-path>")
				os.Exit(2)
			}
			runHook(os.Args[i+1])
			return
		}
		if a == "serve" {
			if i+1 >= len(os.Args) {
				fmt.Fprintln(os.Stderr, "git_bridge: serve requires <config-path>")
				os.Exit(2)
			}
			runServe(os.Args[i+1])
			return
		}
	}
	fmt.Fprintf(os.Stderr, "usage:\n  git_bridge -hook-proc-receive <config-path>\n  git_bridge serve <config-path>\n")
	os.Exit(2)
}

// dbPath returns the shared sqlite DB file location (Java: <root>/.wlgb/wlgb.db).
func dbPath(root string) string {
	return filepath.Join(root, ".wlgb", "wlgb.db")
}

// shQuote quotes a word for /bin/sh (single-quote wrap, ” escape).
const shSafe = " \t\n\r'\"$`\\()|&;*?#\u003c\u003e~!&"

func shQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, shSafe) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// runHook builds the real Bridge IN THIS PROCESS and runs one proc-receive
// session (Java: WLReceivePackFactory.create → WriteLatexPutHook + Bridge).
func runHook(cfgPath string) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git_bridge: load config: %v\n", err)
		os.Exit(1)
	}

	project := os.Getenv("PROJECT")
	if project == "" {
		fmt.Fprintln(os.Stderr, "git_bridge: hook requires handler contract env PROJECT")
		os.Exit(2)
	}

	// --- globals (Java GitBridgeServer wires the same values at startup) ---
	// serviceName: git-error messages name the service (a git-bridge project
	// name); the hook is per-project, so bind the fn to return the project.
	giterrors.SetServiceNameFn(func() string { return project })
	// postbackURL: the base Overleaf's push endpoint POSTs version-IDs back
	// to; Java passes it as the "hostname" to the put hook.
	util.SetPostbackURL(cfg.PostbackURL)
	// snapshot API base (Java Config.apiBaseUrl) + our own port/service name.
	snapshot.SetAPIBaseURL(cfg.APIBaseURL)
	util.SetPort(cfg.GetPort())
	util.SetServiceName(cfg.ServiceName)

	// --- collaborators (Java GitBridgeServer main, Push-only subset) ---
	var maxFileSize *int64
	if cfg.RepoStore != nil {
		maxFileSize = cfg.RepoStore.MaxFileSize
	}
	store := repo.NewFSGitRepoStore(cfg.RootGitDirectory, maxFileSize)
	dbFile := dbPath(cfg.RootGitDirectory)
	dbStore, err := db.NewSqliteDBStore(dbFile, cfg.SQLiteHeapLimitBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git_bridge: sqlite db: %v\n", err)
		os.Exit(1)
	}
	defer dbStore.Close()
	// Push never uploads to the swap store; InMemory satisfies the seam for
	// the Push-only lifecycle (Phase 5 server uses the config-driven store
	// from config.getSwapStore()).
	b := bridge.BridgeFromConfig(cfg, store, dbStore, gitswap.NewInMemorySwapStore(), &snapshot.NetSnapshotApi{})

	oauth2 := (*data.Oauth2)(nil)
	if tok := os.Getenv("OAUTH2_TOKEN"); tok != "" {
		oauth2 = &data.Oauth2{Token: tok}
	}
	hostname := os.Getenv("HOST")
	if hostname == "" {
		hostname = cfg.PostbackURL
	}

	eval := &gitproto.HookEvaluator{
		Project:    project,
		ProjectDir: cfg.RootGitDirectory + string(os.PathSeparator) + project,
		Oauth2:     oauth2,
		Pusher:     b,
		Hostname:   hostname,
		Store:      store,
	}
	if err := gitproto.RunProcReceive(os.Stdin, os.Stdout, os.Stderr, eval); err != nil {
		fmt.Fprintf(os.Stderr, "git_bridge: proc-receive: %v\n", err)
		os.Exit(2)
	}
}

// runServe boots the HTTP bridge (Java: GitBridgeServer.start + Jetty ServerConnector).
func runServe(cfgPath string) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git_bridge: load config: %v\n", err)
		os.Exit(1)
	}

	// --- globals (Java GitBridgeServer wires the same values at startup) ---
	util.SetPostbackURL(cfg.GetPostbackURL())
	util.SetPort(cfg.GetPort())
	util.SetServiceName(cfg.GetServiceName())
	snapshot.SetAPIBaseURL(cfg.GetAPIBaseURL())

	// --- collaborators (Java GitBridgeServer main) ---
	var maxFileSize *int64
	if cfg.RepoStore != nil {
		maxFileSize = cfg.RepoStore.MaxFileSize
	}
	store := repo.NewFSGitRepoStore(cfg.GetRootGitDirectory(), maxFileSize)
	dbFile := dbPath(cfg.GetRootGitDirectory())
	dbStore, err := db.NewSqliteDBStore(dbFile, cfg.SQLiteHeapLimitBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git_bridge: sqlite db: %v\n", err)
		os.Exit(1)
	}
	defer dbStore.Close()
	swapStore := gitswap.FromConfigStore(cfg.SwapStore)
	api := &snapshot.NetSnapshotApi{}
	br := bridge.BridgeFromConfig(cfg, store, dbStore, swapStore, api)

	// Proc-receive hook shim (Option 3a): the shelled `git receive-pack`
	// write path needs the hook on disk (see Bridge.SetProcReceiveHook).
	absCfg, err := filepath.Abs(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git_bridge: abs config: %v\n", err)
		os.Exit(1)
	}
	if exe, err2 := os.Executable(); err2 == nil {
		absExe, errAbs := filepath.Abs(exe)
		if errAbs == nil {
			br.SetProcReceiveHook(func() (string, error) {
				return "#!/bin/sh\nexec " + shQuote(absExe) + " -hook-proc-receive " + shQuote(absCfg) + "\n", nil
			})
		}
	}

	// checkDB before we start serving (Java: bridge.checkDB() at start()).
	if err := br.CheckDB(); err != nil {
		wglog.Warn("checkDB (non-fatal): %v", err)
	}

	// --- HTTP handler (Java configureJettyServer + ServletContextHandler chain) ---
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "git_bridge: executable path: %v\n", err)
		os.Exit(1)
	}
	// gitBinary: empty → default "git" (Java shells "git"; no config knob).
	// receiveHost: Java WLReceivePackFactory line 61: `Util.getPostbackURL()`.
	h := server.NewServerWithSeams(cfg, br, "", executable, cfg.GetPostbackURL(), nil)
	addr := fmt.Sprintf("%s:%d", cfg.GetBindIp(), cfg.GetPort())
	wglog.Info("%s-Git Bridge server starting", cfg.GetServiceName())

	// Graceful shutdown on SIGTERM/SIGINT (Java Runtime shutdownHook drives
	// Bridge.doShutdown).
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		wglog.Info("signal %s; shutting down", sig)
		br.Shutdown()
		os.Exit(0)
	}()

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		wglog.Error("Failed to bridge: %v", err)
		os.Exit(1)
	}
	wglog.Info("Listening on port %d", cfg.GetPort())
	wglog.Info("Bridged to: %s", cfg.GetAPIBaseURL())
	wglog.Info("Postback base URL: %s", cfg.GetPostbackURL())
	wglog.Info("Root git directory path: %s", cfg.GetRootGitDirectory())

	if err := http.Serve(ln, h); err != nil {
		wglog.Error("http.Serve: %v", err)
		os.Exit(1)
	}
}
