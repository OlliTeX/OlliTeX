// Package config mirrors services/clsi/config/settings.defaults.cjs 1:1.
// Every environment variable, default and derived value from the Node config
// is reproduced so the Go clsi runs under the same compose environment.
//
// Note on booleans: Node uses loose `!= 'false'` / `!== false` semantics in a
// few places; we reproduce the documented behaviour (unset => default).
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

// MaxTimeout is the port of RequestParser.MAX_TIMEOUT (seconds).
const MaxTimeout = 600

// seccompProfileBytes is the OlliTeX clsi seccomp profile, identical to the
// Node service's seccomp/clsi-profile.json (copied verbatim).
//
//go:embed seccomp/clsi-profile.json
var seccompProfileBytes []byte

var (
	currentMu     sync.Mutex
	currentConfig *Config
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

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
	ReadOnly bool   `json:"readOnly"`
	URL      string `json:"url"`
}

// Docker mirrors the settings.clsi.docker block (always present in Go because
// sandboxed compiles are mandatory and the runner is compiled in).
type Docker struct {
	Runtime                  string
	Image                    string
	Env                      map[string]string
	SocketPath               string
	User                     string
	OptimiseInDocker         bool
	ExpireProjectAfterIdleMs int
	CheckProjectsIntervalMs  int
	MaxContainerAge          int
	CompileGroupConfig       map[string]map[string]interface{}
	SeccompProfile           string
	ApparmorProfile          string
	NewApparmorProfile       string
	AllowedImages            []string
}

// Config is the full surface of settings.defaults.cjs.
type Config struct {
	CompileSizeLimit       string
	ProcessLifespanLimitMs int64
	CatchErrors            bool

	Path struct {
		CompilesDir  string
		OutputDir    string
		ClsiCacheDir string
		UploadFolder string
		SynctexBase  string // sandboxed override: always '/compile'
	}
	PathSandbox struct {
		Compiles string
		Cache    string
		Output   string
	}

	ConversionTimeoutSeconds int
	PandocImage              string
	EnablePandocConversions  bool
	PdftocairoImage          string
	EnablePdfConversions     bool
	Png2pdfImage             string
	EnablePng2pdfConversions bool
	Png2pdfMinFileSizeBytes  int
	MaxUploadSize            int
	PreciousFilePattern      string

	Internal struct {
		Compile struct {
			Port int
			Host string
		}
		LoadBalancerAgent struct {
			ReportLoad       bool
			LoadPort         int
			LocalPort        int
			AllowMaintenance bool
		}
	}

	APIs struct {
		Compile struct {
			URL             string
			OutputURLPrefix string
			ServerID        string
			InstanceType    string
			Zone            string
			IsSpotInstance  bool
			DownloadHost    string
		}
		Perf struct {
			Host string
		}
		OutputCache struct {
			Enabled       bool
			Shards        []ClsiCacheShard
			CurrentShards int
			DesiredShards int
			ReshardFrom   *time.Time
			ReshardUntil  *time.Time
		}
		FileStore struct {
			URL string
		}
	}

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

	AllowedCompileGroups    []string
	CompileConcurrencyLimit int
	PerfLogSamplingPct      float64

	ClSI struct {
		DockerRunner bool
		Docker       Docker
	}
}

// New builds the Config from the environment, mirroring settings.defaults.cjs.
func New() (*Config, error) {
	var c Config

	c.CompileSizeLimit = envOr("COMPILE_SIZE_LIMIT", "7mb")
	if n := parseIntOr("PROCESS_LIFE_SPAN_LIMIT_MS", 0); n != 0 {
		c.ProcessLifespanLimitMs = int64(n)
	} else {
		c.ProcessLifespanLimitMs = 60 * 60 * 24 * 1000 * 2
	}
	c.CatchErrors = os.Getenv("CATCH_ERRORS") == "true"

	c.Path.CompilesDir = envOr("CLSI_COMPILES_PATH", "/clsi/compiles")
	c.Path.OutputDir = envOr("CLSI_OUTPUT_PATH", "/clsi/output")
	c.Path.ClsiCacheDir = envOr("CLSI_CACHE_PATH", "/clsi/cache")
	c.Path.UploadFolder = envOr("CLSI_UPLOAD_PATH", "/clsi/uploads")
	c.Path.SynctexBase = "/compile"

	c.ConversionTimeoutSeconds = parseIntOr("CLSI_CONVERSION_TIMEOUT_SECONDS", 60)
	c.PandocImage = envOr("PANDOC_IMAGE", "quay.io/sharelatex/pandoc:3.9")
	c.EnablePandocConversions = os.Getenv("ENABLE_PANDOC_CONVERSIONS") == "true"
	c.PdftocairoImage = envOr("PDFTOCAIRO_IMAGE", "quay.io/sharelatex/pdftocairo:24.02")
	c.EnablePdfConversions = os.Getenv("ENABLE_PDF_CONVERSIONS") == "true"
	c.Png2pdfImage = envOr("PNG2PDF_IMAGE", "quay.io/sharelatex/png2pdf:2026-06-24")
	c.EnablePng2pdfConversions = os.Getenv("ENABLE_PNG2PDF_CONVERSIONS") == "true"
	c.Png2pdfMinFileSizeBytes = parseIntOr("PNG2PDF_MIN_FILE_SIZE_BYTES", 1024*1024)
	c.MaxUploadSize = 50 * 1024 * 1024
	c.PreciousFilePattern = os.Getenv("PRECIOUS_FILE_PATTERN")

	c.Internal.Compile.Port = 3013
	c.Internal.Compile.Host = envOr("LISTEN_ADDRESS", "127.0.0.1")
	c.Internal.LoadBalancerAgent.ReportLoad = os.Getenv("LOAD_BALANCER_AGENT_REPORT_LOAD") != "false"
	c.Internal.LoadBalancerAgent.LoadPort = parseIntOr("LOAD_BALANCER_AGENT_LOAD_PORT", 3048)
	c.Internal.LoadBalancerAgent.LocalPort = parseIntOr("LOAD_BALANCER_AGENT_LOCAL_PORT", 3049)
	c.Internal.LoadBalancerAgent.AllowMaintenance =
		strings.ToLower(os.Getenv("LOAD_BALANCER_AGENT_ALLOW_MAINTENANCE")) != "false"

	isSpot := os.Getenv("PREEMPTIBLE") == "TRUE"
	c.APIs.Compile.URL = "http://" + envOr("CLSI_HOST", "127.0.0.1") + ":3013"
	if zone := os.Getenv("ZONE"); zone != "" {
		c.APIs.Compile.OutputURLPrefix = "/zone/" + zone
	}
	c.APIs.Compile.ServerID = envOr("CLSI_SERVER_ID", hostnameNoCtr())
	c.APIs.Compile.InstanceType = os.Getenv("INSTANCE_TYPE")
	c.APIs.Compile.Zone = os.Getenv("ZONE")
	c.APIs.Compile.IsSpotInstance = isSpot
	c.APIs.Compile.DownloadHost = envOr("DOWNLOAD_HOST", "http://localhost:8080")
	c.APIs.Perf.Host = envOr("CLSI_PERF_HOST", "127.0.0.1") + ":" +
		envOr("CLSI_PERF_PORT", "3043")
	c.APIs.FileStore.URL = envOr("FILESTORE_DOMAIN_OVERRIDE",
		fmt.Sprintf("http://%s:3009", envOr("FILESTORE_HOST", "127.0.0.1")))
	if raw := os.Getenv("CLSI_CACHE_INSTANCES"); raw != "" {
		var shards []ClsiCacheShard
		if err := json.Unmarshal([]byte(raw), &shards); err != nil {
			return nil, fmt.Errorf("could not parse CLSI_CACHE_INSTANCES: %w", err)
		}
		c.APIs.OutputCache.Enabled = true
		zone := os.Getenv("ZONE")
		filtered := make([]ClsiCacheShard, 0, len(shards))
		for _, s := range shards {
			if s.Zone == zone && !s.ReadOnly {
				filtered = append(filtered, s)
			}
		}
		c.APIs.OutputCache.Shards = filtered
	}
	c.APIs.OutputCache.CurrentShards = parseIntOr("CLSI_CACHE_CURRENT_SHARDS", 0)
	c.APIs.OutputCache.DesiredShards = parseIntOr("CLSI_CACHE_DESIRED_SHARDS", 0)
	if v := os.Getenv("CLSI_CACHE_RESHARD_FROM"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			c.APIs.OutputCache.ReshardFrom = &t
		}
	}
	if v := os.Getenv("CLSI_CACHE_RESHARD_UNTIL"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			c.APIs.OutputCache.ReshardUntil = &t
		}
	}

	c.SmokeTest = os.Getenv("SMOKE_TEST") != ""
	c.ProjectCacheLengthMs = 1000 * 60 * 60 * 24
	c.ParallelFileDownloads = parseIntOr("FILESTORE_PARALLEL_FILE_DOWNLOADS", 1)
	c.FileStoreDomainOverride = os.Getenv("FILESTORE_DOMAIN_OVERRIDE")
	c.TexliveImageNameOverride = envOr("TEX_LIVE_DOCKER_IMAGE_ROOT",
		os.Getenv("TEX_LIVE_IMAGE_NAME_OVERRIDE"))
	c.TexliveOpenoutAny = os.Getenv("TEXLIVE_OPENOUT_ANY")
	c.TexliveMaxPrintLine = os.Getenv("TEXLIVE_MAX_PRINT_LINE")
	c.EnablePdfCaching = os.Getenv("ENABLE_PDF_CACHING") == "true"
	c.EnablePdfCachingDark = os.Getenv("ENABLE_PDF_CACHING_DARK") == "true"
	c.PdfCachingMinChunkSize = parseIntOr("PDF_CACHING_MIN_CHUNK_SIZE", 1024)
	c.PdfCachingMaxProcessingTime = parseIntOr("PDF_CACHING_MAX_PROCESSING_TIME", 10*1000)
	c.PdfCachingEnableWorkerPool = os.Getenv("PDF_CACHING_ENABLE_WORKER_POOL") == "true"
	c.PdfCachingWorkerPoolSize = parseIntOr("PDF_CACHING_WORKER_POOL_SIZE", 4)
	c.PdfCachingWorkerPoolBackLogLimit = parseIntOr("PDF_CACHING_WORKER_POOL_BACK_LOG_LIMIT", 40)
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

	// 2026-09 (owner #10): the Node settings module gates sandboxed compiles
	// on the presence of app/js/DockerRunner.mjs; the Go binary bundles the
	// runner so the file-existence check is satisfied by construction.
	c.ClSI.DockerRunner = true
	d, err := dockerFromEnv()
	if err != nil {
		return nil, err
	}
	c.ClSI.Docker = d

	compilesHost := os.Getenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES")
	if compilesHost == "" {
		compilesHost = os.Getenv("SANDBOXED_COMPILES_HOST_DIR")
	}
	if compilesHost == "" {
		compilesHost = os.Getenv("COMPILES_HOST_DIR")
	}
	if compilesHost == "" {
		return nil, fmt.Errorf("SANDBOXED_COMPILES enabled, but SANDBOXED_COMPILES_HOST_DIR_COMPILES not set")
	}
	c.PathSandbox.Compiles = compilesHost
	c.PathSandbox.Cache = os.Getenv("SANDBOXED_COMPILES_HOST_DIR_CACHE")
	c.PathSandbox.Output = envOr("SANDBOXED_COMPILES_HOST_DIR_OUTPUT", os.Getenv("OUTPUT_HOST_DIR"))

	return &c, nil
}

// dockerFromEnv mirrors the settings.clsi.docker block exactly. The runner is
// always present in Go so the Node "DockerRunner.mjs exists" gate passes by
// construction.
func dockerFromEnv() (Docker, error) {
	var d Docker
	d.Runtime = os.Getenv("DOCKER_RUNTIME")
	image := envOr("TEXLIVE_IMAGE", os.Getenv("TEX_LIVE_DOCKER_IMAGE"))
	if image == "" {
		all := envOr("ALL_TEX_LIVE_DOCKER_IMAGES", "texlive/texlive:latest-full")
		image = strings.TrimSpace(strings.Split(all, ",")[0])
	}
	d.Image = image
	d.Env = map[string]string{"HOME": "/tmp", "CLSI": "1"}
	d.SocketPath = "/var/run/docker.sock"
	d.User = envOr("TEXLIVE_IMAGE_USER", "www-data") // texlive images ship uid 33 (www-data)
	d.OptimiseInDocker = true
	d.ExpireProjectAfterIdleMs = 24 * 60 * 60 * 1000
	d.CheckProjectsIntervalMs = 10 * 60 * 1000
	if n := parseIntOr("DOCKERRUNNER_MAX_CONTAINER_AGE", 0); n != 0 {
		d.MaxContainerAge = n
	}

	// Node: Object.assign(defaultCompileGroupConfig, JSON.parse(env || '{}'))
	// — env wins per key (top-level shallow), mirroring Object.assign.
	merged := map[string]map[string]interface{}{
		"wordcount":      {"HostConfig.AutoRemove": true},
		"synctex":        {"HostConfig.AutoRemove": true},
		"synctex-output": {"HostConfig.AutoRemove": true},
		"conversions":    {"HostConfig.AutoRemove": true},
		"png2pdf": {"HostConfig.AutoRemove": true,
			"User": fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())},
	}
	if raw := os.Getenv("COMPILE_GROUP_DOCKER_CONFIGS"); raw != "" {
		var extra map[string]map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &extra); err != nil {
			// Node: console.error + process.exit(1)
			return d, fmt.Errorf(
				"could not apply compile group docker configs: %w", err)
		}
		for k, v := range extra {
			merged[k] = v
		}
	}
	d.CompileGroupConfig = merged

	// Node: seccomp_profile = env || JSON.stringify(JSON.parse(<file>)). Go:
	// re-serialize the verbatim file (semantically identical JSON).
	if v := os.Getenv("SECCOMP_PROFILE"); v != "" {
		d.SeccompProfile = v
	} else {
		var v interface{}
		if err := json.Unmarshal(seccompProfileBytes, &v); err != nil {
			return d, fmt.Errorf("could not load seccomp profile: %w", err)
		}
		b, err := json.Marshal(v)
		if err != nil {
			return d, fmt.Errorf("could not load seccomp profile: %w", err)
		}
		d.SeccompProfile = string(b)
	}

	d.ApparmorProfile = os.Getenv("APPARMOR_PROFILE")
	d.NewApparmorProfile = os.Getenv("NEW_APPARMOR_PROFILE")
	if raw := os.Getenv("ALLOWED_IMAGES"); raw != "" {
		d.AllowedImages = strings.Split(raw, " ") // Node: .split(' ')
	}
	return d, nil
}

// Get returns the lazily-built live Config (mirrors the Node singleton
// `@overleaf/settings`). It panics on first call if the environment is
// unusable, exactly as Node's process would have exited.
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

// ForTest rebuilds the config singleton from a fresh environment (tests
// only) and returns it, storing it as the cached current config.
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
