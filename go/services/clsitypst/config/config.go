// Package config mirrors services/clsi_typst/config/settings.defaults.cjs
// 1:1 — the TYPST surface of the deployment env (CLSI_TYPST_* / TYPST_* /
// COMPILE_TYPEST_ENABLED / TYPST_DOCKER_IMAGE), NOT clsi's tex surface.
//
// D22: this package (clsi_typst/config) is local to the clsi_typst module;
// clsi's clsi/config is tex (texlive images, port 3013, pdflatex defaults).
// The two surfaces coexist in the deployment env (see D23): clsi_generic
// packages (lockmanager, urlfetcher, urlcache, resourcewriter) import
// clsi/config and read the shared CLSI_* names under their default env
// values, which the typst deployment does not override.
package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MaxUploadSize mirrors settings maxUploadSize (Node: 50 * 1024 * 1024).
const MaxUploadSize = 50 * 1024 * 1024

// MaxTimeout is the port of RequestParser.MAX_TIMEOUT (seconds), mirrored
// verbatim from clsitex/config (const MaxTimeout = 600). Required by the
// self-contained copies of lockmanager (clsibase canceled; owner: clsitypst
// keeps fully independent, no clsitex imports) — the shared generic helpers
// read config.MaxTimeout for the compile timeout bound.
const MaxTimeout = 600

// DefaultDockerImage is the OWNER-DECISION (2026-09-10) pinned default,
// verbatim from settings.defaults.cjs (digest pinned 0.15.1-era; record the
// live-pull digest in HANDOFF).
const DefaultDockerImage = "pandoc/typst:latest-alpine@sha256:ae9dfa3c58cae72d363484442993b761ff4bc30202ec12823bc1e59fa952c892"

// seccompProfileBytes is clsi_typst's OWN copy of the seccomp profile
// (owner verified during P0 that it loads and Typst compiles under it —
// plan §3.8). Node: path.resolve(__dirname, '../seccomp/clsi-profile.json').
//
//go:embed seccomp/clsi-profile.json
var seccompProfileBytes []byte

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// parseIntOr mirrors Node `parseInt(env, 10) || fallback` (0/NaN => fallback).
func parseIntOr(key string, def int) int {
	n, _ := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if n == 0 {
		return def
	}
	return n
}

func hostnameNoCtr() string {
	hostname, _ := os.Hostname()
	return strings.ReplaceAll(hostname, "-ctr", "")
}

// ClsiCacheShard mirrors one element of CLSI_CACHE_INSTANCES.
type ClsiCacheShard struct {
	Shard    string `json:"shard"`
	Zone     string `json:"zone"`
	URL      string `json:"url"`
	ReadOnly bool   `json:"readOnly"`
}

// Docker mirrors the settings.clsi.docker block (TYPST image surface).
type Docker struct {
	Runtime     string
	Image       string
	Env         map[string]string
	SocketPath  string
	User        string
	SeccompProf string
	Apparmor    string
	Allowed     []string
}

// Config is the full surface of settings.defaults.cjs plus the app.js
// TYPST_COMPILES_DIR override (D22: single Go stage — no separate app.js).
//
// Node camelCase => Go names (documented mapping, parity per D22):
//
//	compile_typst_enabled => CompileTypstEnabled, clsiServerId => ClsiServerID.
type Config struct {
	CompileTypstEnabled bool
	CompileSizeLimit    string
	ProcessLifespanMs   int64
	CatchErrors         bool
	MaxUploadSize       int
	PreciousFilePattern string

	Path struct {
		CompilesDir  string
		OutputDir    string
		ClsiCacheDir string
		UploadFolder string
		// SynctexBase mirrors the settings override `() => '/compile'`.
		SynctexBase string
		// SandboxHost* mirror SANDBOXED_COMPILES_HOST_DIR_{COMPILES,CACHE,OUTPUT}.
		SandboxHostCompiles string
		SandboxHostCache    string
		SandboxHostOutput   string
	}

	Internal PortHost
	APIs     APIs

	SmokeTest                        bool
	ProjectCacheLengthMs             int
	ParallelFileDownloads            int
	FileStoreDomainOverride          string
	TexliveImageNameOverride         string
	TexliveOpenoutAny                string
	TexliveMaxPrintLine              string
	EnablePdfCaching                 bool
	EnablePdfCachingDark             bool
	PdfCachingMinChunkSize           int
	PdfCachingMaxProcessingTime      int
	PdfCachingEnableWorkerPool       bool
	PdfCachingWorkerPoolSize         int
	PdfCachingWorkerPoolBackLogLimit int
	CompileConcurrencyLimit          int
	PerfLogSamplingPct               float64

	AllowedCompileGroups []string // settings.allowedCompileGroups (space-split)

	ClSI struct {
		DockerRunner             bool
		Docker                   Docker
		OptimiseInDocker         bool
		ExpireProjectAfterIdleMs int
		CheckProjectsIntervalMs  int
		CompileGroupConfig       map[string]map[string]interface{}
	}
}

// PortHost mirrors internal.clsi { port, host }.
type PortHost struct {
	Port int
	Host string
	Load struct {
		ReportLoad       bool
		LoadPort         int
		LocalPort        int
		AllowMaintenance bool
	}
}

// APIs mirrors the settings.apis block.
type APIs struct {
	Clsi struct {
		URL             string
		OutputURLPrefix string
		ClsiServerID    string
		InstanceType    string
		Zone            string
		IsSpotInstance  bool
		DownloadHost    string
	}
	Perf struct {
		Host string
	}
	Cache struct {
		Enabled       bool
		Shards        []ClsiCacheShard
		CurrentShards int
		DesiredShards int
		// ReshardFrom/Until are the CLSI cache-resharding window (ported from
		// clsitex/config so the self-contained copy of clsicachehandler compiles
		// against this canonical config; clsibase was canceled per owner).
		ReshardFrom  *time.Time
		ReshardUntil *time.Time
	}
	FileStore struct {
		URL string
	}
}

// New builds the Config from the environment, mirroring settings.defaults.cjs.
func New() (*Config, error) {
	var c Config

	c.CompileTypstEnabled = os.Getenv("COMPILE_TYPEST_ENABLED") != "false"
	c.CompileSizeLimit = envOr("COMPILE_SIZE_LIMIT", "7mb")
	c.ProcessLifespanMs = int64(parseIntOr("PROCESS_LIFE_SPAN_LIMIT_MS",
		60*60*24*1000*2))
	c.CatchErrors = os.Getenv("CATCH_ERRORS") == "true"
	c.MaxUploadSize = MaxUploadSize
	c.PreciousFilePattern = os.Getenv("PRECIOUS_FILE_PATTERN")

	// Node path defaults: Path.resolve(__dirname, '../X'); Go: the env
	// always sets the CLSI_TYPST_*_PATH or TYPST_COMPILES_DIR override in
	// practice; the fallback mirrors the Node relative default (deployment
	// cwd-scoped).
	c.Path.CompilesDir = envOr("CLSI_TYPST_COMPILES_PATH", envOr("CLSI_COMPILES_PATH", "compiles"))
	// app.js override: TYPST_COMPILES_DIR wins over CLSI_TYPST_COMPILES_PATH.
	if v := os.Getenv("TYPST_COMPILES_DIR"); v != "" {
		c.Path.CompilesDir = v
	}
	c.Path.OutputDir = envOr("CLSI_TYPST_OUTPUT_PATH", envOr("CLSI_OUTPUT_PATH", "output"))
	// Cache dir: per-namespace CLSI_TYPST_CACHE_PATH wins; the generic
	// CLSI_CACHE_PATH (clsi_generic shared surface — D23, same as the existing
	// CLSI_PERF_* precedent) is the honored fallback so the self-contained
	// generic helpers (urlcache, resourcewriter) resolve their dir without a
	// separate namespace. Deployments that set CLSI_TYPST_CACHE_PATH always win.
	c.Path.ClsiCacheDir = envOr("CLSI_TYPST_CACHE_PATH", envOr("CLSI_CACHE_PATH", "cache"))
	c.Path.UploadFolder = envOr("CLSI_TYPST_UPLOAD_PATH", envOr("CLSI_UPLOAD_PATH", "uploads"))
	c.Path.SynctexBase = "/compile"

	isSpot := os.Getenv("PREEMPTIBLE") == "TRUE"
	c.Internal.Port = parseIntOr("CLSI_TYPST_PORT", 3014)
	c.Internal.Host = envOr("LISTEN_ADDRESS", "127.0.0.1")
	c.Internal.Load.ReportLoad =
		os.Getenv("LOAD_BALANCER_AGENT_REPORT_LOAD") != "false"
	c.Internal.Load.LoadPort = parseIntOr("CLSI_TYPST_LOAD_PORT", 3046)
	c.Internal.Load.LocalPort = parseIntOr("CLSI_TYPST_LOCAL_PORT", 3047)
	c.Internal.Load.AllowMaintenance =
		strings.ToLower(os.Getenv("LOAD_BALANCER_AGENT_ALLOW_MAINTENANCE")) != "false"

	c.APIs.Clsi.URL = "http://" + envOr("CLSI_TYPST_HOST", c.Internal.Host) + ":" +
		strconv.Itoa(c.Internal.Port)
	if zone := os.Getenv("ZONE"); zone != "" {
		c.APIs.Clsi.OutputURLPrefix = "/zone/" + zone
	}
	c.APIs.Clsi.ClsiServerID = envOr("CLSI_TYPST_SERVER_ID", hostnameNoCtr())
	c.APIs.Clsi.InstanceType = os.Getenv("INSTANCE_TYPE")
	c.APIs.Clsi.Zone = os.Getenv("ZONE")
	c.APIs.Clsi.IsSpotInstance = isSpot
	c.APIs.Clsi.DownloadHost = envOr("DOWNLOAD_HOST", "http://localhost:8080")
	c.APIs.Perf.Host = envOr("CLSI_PERF_HOST", "127.0.0.1") + ":" +
		envOr("CLSI_PERF_PORT", "3043")
	c.APIs.FileStore.URL = envOr("FILESTORE_DOMAIN_OVERRIDE",
		"http://"+envOr("FILESTORE_HOST", "127.0.0.1")+":3009")

	if raw := os.Getenv("CLSI_CACHE_INSTANCES"); raw != "" {
		var shards []ClsiCacheShard
		if err := json.Unmarshal([]byte(raw), &shards); err != nil {
			// Node: JSON.parse throws -> the settings module throws ->
			// process exits. New() returns an error (cmd panics in Get()).
			return nil, fmt.Errorf("could not parse CLSI_CACHE_INSTANCES: %w", err)
		}
		c.APIs.Cache.Enabled = true
		zone := os.Getenv("ZONE")
		for _, s := range shards {
			if s.Zone == zone && !s.ReadOnly {
				c.APIs.Cache.Shards = append(c.APIs.Cache.Shards, s)
			}
		}
	}
	c.APIs.Cache.CurrentShards = parseIntOr("CLSI_CACHE_CURRENT_SHARDS", 0)
	c.APIs.Cache.DesiredShards = parseIntOr("CLSI_CACHE_DESIRED_SHARDS", 0)

	c.SmokeTest = os.Getenv("SMOKE_TEST") != ""
	c.ProjectCacheLengthMs = 1000 * 60 * 60 * 24
	c.ParallelFileDownloads = parseIntOr("FILESTORE_PARALLEL_FILE_DOWNLOADS", 1)
	c.FileStoreDomainOverride = os.Getenv("FILESTORE_DOMAIN_OVERRIDE")
	c.TexliveImageNameOverride = os.Getenv("TEX_LIVE_DOCKER_IMAGE_ROOT_4TYPST")
	c.TexliveOpenoutAny = os.Getenv("TEXLIVE_OPENOUT_ANY")
	c.TexliveMaxPrintLine = os.Getenv("TEXLIVE_MAX_PRINT_LINE")
	c.EnablePdfCaching = os.Getenv("ENABLE_PDF_CACHING") == "true"
	c.EnablePdfCachingDark = os.Getenv("ENABLE_PDF_CACHING_DARK") == "true"
	c.PdfCachingMinChunkSize = parseIntOr("PDF_CACHING_MIN_CHUNK_SIZE", 1024)
	c.PdfCachingMaxProcessingTime =
		parseIntOr("PDF_CACHING_MAX_PROCESSING_TIME", 10*1000)
	c.PdfCachingEnableWorkerPool =
		os.Getenv("PDF_CACHING_ENABLE_WORKER_POOL") == "true"
	c.PdfCachingWorkerPoolSize = parseIntOr("PDF_CACHING_WORKER_POOL_SIZE", 4)
	c.PdfCachingWorkerPoolBackLogLimit =
		parseIntOr("PDF_CACHING_WORKER_POOL_BACK_LOG_LIMIT", 40)
	if isSpot {
		c.CompileConcurrencyLimit = 32
	} else {
		c.CompileConcurrencyLimit = 64
	}
	if p := os.Getenv("CLSI_PERFORMANCE_LOG_SAMPLING"); p != "" {
		if f, err := strconv.ParseFloat(p, 64); err == nil {
			c.PerfLogSamplingPct = f
		}
	}
	if list := os.Getenv("ALLOWED_COMPILE_GROUPS"); list != "" {
		c.AllowedCompileGroups = strings.Split(list, " ") // Node: .split(' ')
	}

	// Docker runner gate: (DOCKER_RUNNER || SANDBOXED_COMPILES) === 'true'.
	// The Go binary bundles the runner (no DockerRunner.mjs file check to
	// fail) so the gate reduces to the env flag; cmd validates sandboxed
	// compiles are on (D4: no local runner).
	sandboxed := os.Getenv("SANDBOXED_COMPILES") == "true" ||
		os.Getenv("DOCKER_RUNNER") == "true"

	compilesHost := os.Getenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES")
	if compilesHost == "" {
		compilesHost = os.Getenv("SANDBOXED_COMPILES_HOST_DIR")
	}
	if compilesHost == "" {
		compilesHost = os.Getenv("COMPILES_HOST_DIR")
	}
	if sandboxed && compilesHost == "" {
		return nil, fmt.Errorf("SANDBOXED_COMPILES enabled, but " +
			"SANDBOXED_COMPILES_HOST_DIR_COMPILES not set")
	}
	c.Path.SandboxHostCompiles = compilesHost
	c.Path.SandboxHostCache = os.Getenv("SANDBOXED_COMPILES_HOST_DIR_CACHE")
	c.Path.SandboxHostOutput = envOr("SANDBOXED_COMPILES_HOST_DIR_OUTPUT",
		os.Getenv("OUTPUT_HOST_DIR"))

	// clsi block is only populated when sandboxed compiles are enabled
	// (Node: module.exports.clsi under the same gate).
	if sandboxed {
		c.ClSI.DockerRunner = true
		c.ClSI.OptimiseInDocker = true
		c.ClSI.ExpireProjectAfterIdleMs = 24 * 60 * 60 * 1000
		c.ClSI.CheckProjectsIntervalMs = 10 * 60 * 1000
		// image: TYPST_IMAGE || TYPST_DOCKER_IMAGE || ALL_TYPST_DOCKER_IMAGES
		// (first, trimmed) || pinned OWNER-DECISION default.
		image := envOr("TYPST_IMAGE", os.Getenv("TYPST_DOCKER_IMAGE"))
		if image == "" {
			image = strings.TrimSpace(strings.Split(
				envOr("ALL_TYPST_DOCKER_IMAGES", DefaultDockerImage), ",")[0])
		}
		c.ClSI.Docker.Runtime = os.Getenv("DOCKER_RUNTIME")
		c.ClSI.Docker.Image = image
		c.ClSI.Docker.Env = map[string]string{"HOME": "/tmp", "CLSI": "1"}
		c.ClSI.Docker.SocketPath = "/var/run/docker.sock"
		// pandoc/typst has no www-data; run as numeric uid/gid 33 (P0-verified).
		c.ClSI.Docker.User = envOr("TYPST_IMAGE_USER", "33:33")
		// clsi_typst ships its own seccomp copy: SECCOMP_PROFILE env wins,
		// else re-serialize the embedded file (Node: JSON.stringify of the
		// parsed file — semantically identical).
		if v := os.Getenv("SECCOMP_PROFILE"); v != "" {
			c.ClSI.Docker.SeccompProf = v
		} else {
			var v interface{}
			if err := json.Unmarshal(seccompProfileBytes, &v); err != nil {
				return nil, fmt.Errorf("could not load seccomp profile: %w", err)
			}
			b, err := json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("could not load seccomp profile: %w", err)
			}
			c.ClSI.Docker.SeccompProf = string(b)
		}
		c.ClSI.Docker.Apparmor = os.Getenv("APPARMOR_PROFILE")
		if raw := os.Getenv("ALLOWED_IMAGES"); raw != "" {
			c.ClSI.Docker.Allowed = strings.Split(raw, " ")
		}
		// defaultCompileGroupConfig = { wordcount: {HostConfig.AutoRemove: true} }
		merged := map[string]map[string]interface{}{
			"wordcount": {"HostConfig.AutoRemove": true},
		}
		if raw := os.Getenv("COMPILE_GROUP_DOCKER_CONFIGS"); raw != "" {
			var extra map[string]map[string]interface{}
			if err := json.Unmarshal([]byte(raw), &extra); err != nil {
				return nil, fmt.Errorf("could not apply compile group "+
					"docker configs: %w", err)
			}
			for k, v := range extra {
				merged[k] = v
			}
		}
		c.ClSI.CompileGroupConfig = merged
	}

	return &c, nil
}

var (
	currentMu     sync.Mutex
	currentConfig *Config
)

// Get returns the lazily-built live Config (mirrors the Node singleton
// `@overleaf/settings`; clsi's config.Get is the tex twin).
func Get() *Config {
	currentMu.Lock()
	defer currentMu.Unlock()
	if currentConfig == nil {
		c, err := New()
		if err != nil {
			panic(err)
		}
		currentConfig = c
	}
	return currentConfig
}

// ForTest rebuilds the live singleton from a fresh environment (tests only).
func ForTest() *Config {
	c, err := New()
	if err != nil {
		panic(err)
	}
	currentMu.Lock()
	currentConfig = c
	currentMu.Unlock()
	return currentConfig
}
