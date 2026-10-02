// Command clsi_typst is the production entry point (T11).
//
// Ported from the `import.meta.main` block of services/clsi_typst/app.js
// (Node 229-260) plus the Node settings guard, with the LOCKED production
// seams wired HERE (the only consumer):
//
//   - §3.4 fork lever: ofa.AssignFind -> walk <contentDir>/generated-files/
//     <build>/, contentDir-relative paths (frozen READER serves what the
//     frozen WRITER persisted).
//   - D10: projectpersistence.Init(cfg, cm) BEFORE serving.
//   - D14 gate / 630-s server timeout / load agents / lifespan guard.
//
// Node parity notes:
//   - Node fs.existsSync seccomp guard: settings re-serialise the EMBEDDED
//     profile (or SECCOMP_PROFILE env) and Node app.js dies (exit 1) with
//     "could not load seccomp profile" when it is missing/invalid. Here:
//     config.New() already embeds+validates; main additionally hard-fails
//     (exit 2) when DockerRunner is enabled but the profile is empty.
//   - Node catchErrors: process.on('uncaughtException', logger.error). Go:
//     a panic-catcher goroutine logs "uncaughtException" (the Go runtime
//     has no process-level uncaught-exception hook).
//   - Graceful shutdown (SIGTERM/SIGINT): cancel ctx (stops lifespan guard
//   - load TCP accept), http.Server.Shutdown(30s budget) on both HTTP
//     servers, close TCP listener, PPM Cleanup — a documented Go add
//     (Node relies on process exit).
package main

import (
	"context"
	"fmt"
	"io/fs"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"ollitex/go/services/clsitypst/apps"
	"ollitex/go/services/clsitypst/compilecontroller"
	"ollitex/go/services/clsitypst/compilemanager"
	"ollitex/go/services/clsitypst/config"
	"ollitex/go/services/clsitypst/dockerrunner"
	clfofa "ollitex/go/services/clsitypst/outputfilearchivemanager"
	"ollitex/go/services/clsitypst/projectpersistence"
	"ollitex/go/services/clsitypst/smoketest"
	"ollitex/go/services/clsitypst/typstrunner"

	"ollitex/go/services/clsitypst/logger"
	ooutputcache "ollitex/go/services/clsitypst/outputcachemanager"
)

// startLifespanGuard implements the PROCESS_LIFESPAN_LIMIT_MS guard
// (reference DESIGN-ONLY source: comp checkout apps/error.go
// startLifespanGuard — jitter the limit DOWN by up to 10%, then flip the
// too-old flag at limitMs-from-now; /health_check reports 500 so the load
// balancer drains this instance). jitter() = rand.Float64() ∈ [0,1): formula
// limitMs -= int64(float64(limitMs)*(rand.Float64()/10)). Blocks until stop
// closes (ctx cancel at shutdown) or the timer fires.
func startLifespanGuard(setTooOld func(bool), limitMs int64, stop <-chan struct{}) {
	if limitMs <= 0 {
		return
	}
	effective := limitMs - int64(float64(limitMs)*(rand.Float64()/10))
	if effective < 0 {
		effective = 0
	}
	logger.Info(map[string]any{
		"limitMs": effective,
		"target":  time.Now().Add(time.Duration(effective) * time.Millisecond).Unix(),
	}, "Lifespan limited")
	timer := time.NewTimer(time.Duration(effective) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-stop:
		return
	case <-timer.C:
		logger.Info(map[string]any{}, "shutting down, process is too old")
		setTooOld(true)
	}
}

// productionFind is the §3.4 READER/WRITER FORK lever (cmd = MY lever, D22:
// NO clsi.go change). The frozen READER (clsi/outputfilearchivemanager)
// assembles dir = <outputDir>/<pid>[-uid]/<build>/. and ArchiveTo opens
// filepath.Join(<contentDir>/, f.Path) — i.e. f.Path is contentDir-relative.
// The frozen WRITER (clsi/outputcachemanager.SaveOutputFiles) persists at
// <contentDir>/<CacheSubdir>/<build>/ (the frozen CacheSubdir const, imported
// above to avoid drift).
// Therefore: resolve the build + contentDir from dir, walk the WRITER dir,
// and return contentDir-relative paths ("generated-files/<build>/<file>").
// ENOENT on the writer dir => *fs.PathError{ENONENT} => READER's
// OpenErrorToNotFound => NotFoundError => otc (404, nil) => /output 404.
func productionFind() clfofa.FindFunc {
	return func(_ []string, dir string) ([]clfofa.OutputFile, error) {
		clean := filepath.Clean(dir)
		build := filepath.Base(clean)
		contentDir := filepath.Dir(clean)
		writerDir := filepath.Join(contentDir, ooutputcache.CacheSubdir, build)

		info, err := os.Stat(writerDir)
		if err != nil {
			// ENOENT/PathError: the reader's OpenErrorToNotFound maps it to
			// NotFoundError("Output files not found") -> 404.
			return nil, err
		}
		if !info.IsDir() {
			// ENOTDIR equivalent: the reader detects the rendered "not a
			// directory" message (frozen-package quirk, see OpenErrorToNotFound).
			return nil, &fs.PathError{Op: "stat", Path: writerDir, Err: syscall.ENOTDIR}
		}

		out := []clfofa.OutputFile{}
		err = filepath.WalkDir(writerDir, func(p string, de fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if de.IsDir() {
				return nil
			}
			rel, rerr := filepath.Rel(contentDir, p)
			if rerr != nil {
				return rerr
			}
			out = append(out, clfofa.OutputFile{Path: rel})
			return nil
		})
		if err != nil {
			return nil, err
		}
		return out, nil
	}
}

// catchErrors ports the Node catchErrors middleware (app.js:
// process.on('uncaughtException', (error) => logger.error({err}, 'uncaughtException'))).
// Documented Go divergence: the Go runtime offers no process-level
// uncaught-exception hook; this middleware recovers panics in the request
// path and logs them (Node: the listener alone — no response is written
// either, and the process survives).
func catchErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error(map[string]any{"err": fmt.Sprintf("%v", rec)},
					"uncaughtException")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func main() {
	cfg, err := config.New()
	if err != nil {
		logger.Error(map[string]any{"err": err}, "could not load configuration")
		os.Exit(1)
	}

	// Seccomp guard (Node app.js fs.existsSync): config.New() has already
	// embedded+validated the profile; hard-fail if the docker runner is on
	// but the profile is somehow empty.
	if cfg.ClSI.DockerRunner && cfg.ClSI.Docker.SeccompProf == "" {
		logger.Error(map[string]any{}, "could not load seccomp profile")
		os.Exit(2)
	}

	// Production dockerrunner (clsi.go REUSE, D22): unix engine + RunnerConfig
	// snapshot from the typst config.
	eng := &dockerrunner.UnixEngine{SocketPath: cfg.ClSI.Docker.SocketPath}
	runnerCfg := dockerrunner.RunnerConfig{
		DockerRuntime:      cfg.ClSI.Docker.Runtime,
		DockerUser:         cfg.ClSI.Docker.User,
		DockerEnv:          cfg.ClSI.Docker.Env,
		SeccompProfile:     cfg.ClSI.Docker.SeccompProf,
		ApparmorProfile:    cfg.ClSI.Docker.Apparmor,
		DockerImage:        cfg.ClSI.Docker.Image,
		AllowedImages:      cfg.ClSI.Docker.Allowed,
		MaxContainerAgeMS:  0, // 0 => the monitor's one-hour fallback (Node parity).
		Override:           cfg.TexliveImageNameOverride,
		HostDirCompiles:    cfg.Path.SandboxHostCompiles,
		HostDirOutput:      cfg.Path.SandboxHostOutput,
		CompileGroupConfig: cfg.ClSI.CompileGroupConfig,
	}
	dr := dockerrunner.New(runnerCfg, eng)
	// Node: the monitor starts at module import (destroyOldContainers via
	// setTimeout(delay), then hourly ticks). delay 0 => first tick immediate.
	dr.StartContainerMonitor(0)

	tr := typstrunner.New(dr, logger.Debug, logger.Warn)
	cm := compilemanager.New(cfg, tr, dr)
	cc := compilecontroller.New(cm, cfg)

	// D10: PPM singleton BEFORE serving.
	projectpersistence.Init(cfg, cm)

	// §3.4 production lever (frozen READER/WRITER fork).
	clfofa.AssignFind(productionFind())

	// Smoke test (D12 lean-Y, optional).
	var smoke *smoketest.SmokeTest
	if cfg.SmokeTest {
		smoke = smoketest.New(cfg)
	}

	app := apps.New(cfg, cc, smoke)
	tooOld := new(atomic.Bool)
	app.ProcessTooOld = tooOld.Load

	la := apps.NewLoadAgent(cfg.Internal.Load.AllowMaintenance, cfg.Internal.Load.ReportLoad)

	ctx, cancel := context.WithCancel(context.Background())
	go startLifespanGuard(tooOld.Store, cfg.ProcessLifespanMs, ctx.Done())

	// Main HTTP server (Node: app.listen(port, host) + 630s req/res timeout).
	mainHandler := app.Handler()
	if cfg.CatchErrors {
		mainHandler = catchErrors(mainHandler)
	}
	mainServer := &http.Server{
		Addr:    net.JoinHostPort(cfg.Internal.Host, strconv.Itoa(cfg.Internal.Port)),
		Handler: mainHandler,
		// Node app.js: req.setTimeout(630000); res.setTimeout(630000) — Go
		// 1.27: no Server.Timeout; ReadTimeout = req.setTimeout, WriteTimeout
		// = res.setTimeout (IdleTimeout falls back to ReadTimeout, same value).
		ReadTimeout:  630 * time.Second,
		WriteTimeout: 630 * time.Second,
	}

	// Load agent TCP (Node: loadTcpServer.listen(loadTcpPort, host)).
	tcplis, err := net.Listen("tcp",
		net.JoinHostPort(cfg.Internal.Host, strconv.Itoa(cfg.Internal.Load.LoadPort)))
	if err != nil {
		logger.Error(map[string]any{"err": err},
			"could not listen on load tcp port "+strconv.Itoa(cfg.Internal.Load.LoadPort))
		os.Exit(1)
	}
	go la.ServeTCP(ctx, tcplis)
	logger.Info(map[string]any{"port": cfg.Internal.Load.LoadPort},
		"Load tcp agent listening on load port "+strconv.Itoa(cfg.Internal.Load.LoadPort))

	// Load agent HTTP (Node: loadHttpServer.listen(loadHttpPort, host)).
	stateMux := http.NewServeMux()
	stateMux.HandleFunc("POST /state/{state}", la.StateHandler)
	stateServer := &http.Server{
		Addr:         net.JoinHostPort(cfg.Internal.Host, strconv.Itoa(cfg.Internal.Load.LocalPort)),
		Handler:      stateMux,
		ReadTimeout:  30 * time.Second, // Node: no explicit timeout; defense-in-depth.
		WriteTimeout: 30 * time.Second,
	}
	stateErrCh := make(chan error, 1)
	go func() { stateErrCh <- stateServer.ListenAndServe() }()
	logger.Info(map[string]any{"port": cfg.Internal.Load.LocalPort},
		"Load http agent listening on load port "+strconv.Itoa(cfg.Internal.Load.LocalPort))

	mainErrCh := make(chan error, 1)
	go func() { mainErrCh <- mainServer.ListenAndServe() }()
	logger.Info(map[string]any{"addr": mainServer.Addr},
		"clsi_typst starting up, listening on "+mainServer.Addr)

	// Graceful shutdown (documented Go add; Node relies on process exit).
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-sigc:
		logger.Info(map[string]any{"signal": sig.String()}, "shutting down")
		cancel() // stops lifespan guard + load TCP accept
		shutdownCtx, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		_ = mainServer.Shutdown(shutdownCtx)
		_ = stateServer.Shutdown(shutdownCtx)
		_ = tcplis.Close()
		projectpersistence.Cleanup()
		logger.Info(map[string]any{}, "shutdown complete")
	case err := <-mainErrCh:
		logger.Error(map[string]any{"err": err}, "main HTTP server error")
		os.Exit(1)
	case err := <-stateErrCh:
		if err != nil && err != http.ErrServerClosed {
			logger.Error(map[string]any{"err": err}, "load-http server error")
			os.Exit(1)
		}
	}
}
