// Command clsitex is the Go CLSI service (compile + conversion), the 1:1
// drop-in replacement for the Node services/clsi app (app.js).
//
// Node parity mapping (services/clsi/app.js):
//   - app.listen(port, host)            -> main http.Server on :3013 (default)
//   - req.setTimeout(TIMEOUT) 630s      -> Read/WriteTimeout 630s (long compiles)
//   - loadTcpServer (load agent)       -> apps.LoadAgent.ServeTCP on :3048
//   - loadHttpServer (state endpoint)  -> apps.LoadAgent.StateHandler on :3049
//   - Settings.smokeTest -> runSmokeTest() on startup -> smoketest.Run (go)
//   - CatchErrors / uncaughtException  -> log fatal on startup errors; per-request
//     panics are recovered by net/http (logged by the server).
package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	apps "ollitex/go/services/clsitex/apps"
	clsicache "ollitex/go/services/clsitex/clsicachehandler"
	"ollitex/go/services/clsitex/commandrunner"
	cc "ollitex/go/services/clsitex/compilecontroller"
	cm "ollitex/go/services/clsitex/compilemanager"
	"ollitex/go/services/clsitex/config"
	cv "ollitex/go/services/clsitex/conversioncontroller"
	cvm "ollitex/go/services/clsitex/conversionmanager"
	"ollitex/go/services/clsitex/dockerrunner"
	fum "ollitex/go/services/clsitex/fileuploadmiddleware"
	"ollitex/go/services/clsitex/latexrunner"
	"ollitex/go/services/clsitex/logger"
	ppm "ollitex/go/services/clsitex/projectpersistence"
	rp "ollitex/go/services/clsitex/requestparser"
	smoketest "ollitex/go/services/clsitex/smoketest"
)

// requestTimeout mirrors Node app.js `const TIMEOUT = 630 * 1000`
// (10.5 minutes - 30 seconds download allowance), applied per request in Node.
const requestTimeout = 630 * time.Second

func fatalf(format string, args ...any) {
	log.Printf("clsitex: "+format, args...)
	os.Exit(1)
}

func main() {
	cfg, err := config.New()
	if err != nil {
		fatalf("config: %v", err)
	}

	logInfo := func(attrs map[string]any, msg string) {
		logger.Info(attrs, msg)
	}

	// --- command runner (Node: CommandRunner.js + DockerRunner.mjs) ----------
	docker := cfg.ClSI.Docker
	engine := &dockerrunner.UnixEngine{SocketPath: docker.SocketPath}
	runnerImpl := dockerrunner.New(dockerrunner.RunnerConfig{
		DockerRuntime:      docker.Runtime,
		DockerUser:         docker.User,
		DockerEnv:          docker.Env,
		SeccompProfile:     docker.SeccompProfile,
		ApparmorProfile:    docker.ApparmorProfile,
		DockerImage:        docker.Image,
		AllowedImages:      docker.AllowedImages,
		MaxContainerAgeMS:  docker.MaxContainerAge * 1000,
		Override:           cfg.TexliveImageNameOverride,
		HostDirCompiles:    cfg.PathSandbox.Compiles,
		HostDirOutput:      cfg.PathSandbox.Output,
		CompileGroupConfig: docker.CompileGroupConfig,
	}, engine)
	runner, err := commandrunner.New(cfg.ClSI.DockerRunner, runnerImpl, func(msg string, attrs map[string]any) {
		logInfo(attrs, msg)
	})
	if err != nil {
		fatalf("commandrunner: %v", err)
	}

	// --- compile controller (Node: CompileController.js) ---------------------
	latex := latexrunner.New(runner, logInfo, logInfo, logInfo)
	manager := cm.New(runner, latex)
	controller := &cc.Controller{
		Manager: manager,
		Notify:  clsicache.Notify,
		MarkProjectAccessed: func(projectID string, nowMs int64) {
			ppm.MarkProjectJustAccessed(projectID)
		},
		ClearProject: ppm.ClearProject,
		Config: cc.ControllerConfig{
			InstanceType:         cfg.APIs.Compile.InstanceType,
			Zone:                 cfg.APIs.Compile.Zone,
			IsSpotInstance:       cfg.APIs.Compile.IsSpotInstance,
			OutputURLPrefix:      cfg.APIs.Compile.OutputURLPrefix,
			DownloadHost:         cfg.APIs.Compile.DownloadHost,
			AllowedImages:        docker.AllowedImages,
			AllowedCompileGroups: cfg.AllowedCompileGroups,
			PdfCachingMinChunk:   cfg.PdfCachingMinChunkSize,
			ClearProject:         ppm.ClearProject,
		},
	}

	// --- conversion controller (Node: ConversionController.js) ---------------
	conversionManager := cvm.New(runner, cfg, func(msg string, attrs map[string]any) { logInfo(attrs, msg) })
	conversionCtrl := cv.New(conversionManager, cv.Settings{
		CompilesDir:             cfg.Path.CompilesDir,
		OutputDir:               cfg.Path.OutputDir,
		ClsiCacheDir:            cfg.Path.ClsiCacheDir,
		EnablePandocConversions: cfg.EnablePandocConversions,
		EnablePdfConversions:    cfg.EnablePdfConversions,
	}, rp.Config{
		PdfCachingMinChunkSize:  cfg.PdfCachingMinChunkSize,
		AllowedImages:           docker.AllowedImages,
		AllowedCompileGroups:    cfg.AllowedCompileGroups,
		AllowedCompileGroupsSet: len(cfg.AllowedCompileGroups) > 0,
	})

	// --- upload middleware (Node: FileUploadMiddleware.js) -------------------
	upload := fum.New(cfg.Path.UploadFolder, int64(cfg.MaxUploadSize),
		func(attrs map[string]any, msg string) { logInfo(attrs, msg) },
		func(attrs map[string]any, msg string) { logInfo(attrs, msg) })

	// --- app + smoke test ----------------------------------------------------
	var processTooOld bool
	smoke := smoketest.New(cfg)
	smoke.SetCompileURL(smoketest.DefaultURLFor(cfg.Internal.Compile.Host, cfg.Internal.Compile.Port))
	app := apps.New(cfg, controller, conversionCtrl, upload, &processTooOld, smoke)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	host := cfg.Internal.Compile.Host
	if host == "" {
		host = "0.0.0.0"
	}
	mainAddr := net.JoinHostPort(host, strconv.Itoa(cfg.Internal.Compile.Port))
	mainSrv := &http.Server{
		Addr:              mainAddr,
		Handler:           app.Router(),
		ReadHeaderTimeout: 60 * time.Second,
		ReadTimeout:       requestTimeout,
		WriteTimeout:      requestTimeout,
		IdleTimeout:       2 * time.Minute,
	}

	// --- load agent listeners (Node: loadTcpServer + loadHttpServer) --------
	agent := apps.NewLoadAgent(cfg.Internal.LoadBalancerAgent.AllowMaintenance,
		cfg.Internal.LoadBalancerAgent.ReportLoad)
	agentTCP, err := net.Listen("tcp", net.JoinHostPort(host,
		strconv.Itoa(cfg.Internal.LoadBalancerAgent.LoadPort)))
	if err != nil {
		fatalf("load agent tcp: %v", err)
	}
	agentHTTP := &http.Server{
		Addr:              net.JoinHostPort(host, strconv.Itoa(cfg.Internal.LoadBalancerAgent.LocalPort)),
		Handler:           http.HandlerFunc(agent.StateHandler),
		ReadHeaderTimeout: 30 * time.Second,
	}

	go func() {
		log.Printf("clsitex: load tcp agent listening on %s", agentTCP.Addr())
		agent.ServeTCP(ctx, agentTCP)
	}()
	go func() {
		log.Printf("clsitex: load http agent listening on %s", agentHTTP.Addr)
		if err := agentHTTP.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("clsitex: load http agent: %v", err)
		}
	}()

	log.Printf("clsitex: CLSI starting up, listening on %s", mainAddr)
	if cfg.SmokeTest {
		go func() {
			if err := smoke.Run(ctx); err != nil {
				log.Printf("clsitex: smoke test failed: %v", err)
			}
		}()
	}

	errCh := make(chan error, 1)
	go func() { errCh <- mainSrv.ListenAndServe() }()

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			fatalf("main server: %v", err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = mainSrv.Shutdown(shutdownCtx)
		_ = agentHTTP.Close()
		_ = agentTCP.Close()
	}
}
