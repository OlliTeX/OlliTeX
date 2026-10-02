package config

import (
	"os"
	"testing"
)

// setEnvs sets env vars for a test and restores them on cleanup.
func setEnvs(t *testing.T, m map[string]string) {
	t.Helper()
	c := map[string]string{}
	for k := range m {
		v, ok := os.LookupEnv(k)
		if ok {
			c[k] = v
		} else {
			c[k] = "\x00unset"
		}
	}
	for k, v := range m {
		if v == "\x00unset" {
			os.Unsetenv(k)
		} else {
			os.Setenv(k, v)
		}
	}
	t.Cleanup(func() {
		for k, v := range c {
			if v == "\x00unset" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}
		}
	})
}

// clearEnv unsets every env var read by New().
func clearEnv() {
	clearList := []string{
		"COMPILE_TYPEST_ENABLED", "COMPILE_SIZE_LIMIT",
		"PROCESS_LIFE_SPAN_LIMIT_MS", "CATCH_ERRORS", "PRECIOUS_FILE_PATTERN",
		"CLSI_TYPST_COMPILES_PATH", "CLSI_TYPST_OUTPUT_PATH",
		"CLSI_TYPST_CACHE_PATH", "CLSI_TYPST_UPLOAD_PATH", "TYPST_COMPILES_DIR",
		"LISTEN_ADDRESS", "CLSI_TYPST_PORT", "LOAD_BALANCER_AGENT_REPORT_LOAD",
		"CLSI_TYPST_LOAD_PORT", "CLSI_TYPST_LOCAL_PORT",
		"LOAD_BALANCER_AGENT_ALLOW_MAINTENANCE", "PREEMPTIBLE",
		"CLSI_TYPST_HOST", "CLSI_TYPST_SERVER_ID", "ZONE", "INSTANCE_TYPE",
		"DOWNLOAD_HOST", "CLSI_PERF_HOST", "CLSI_PERF_PORT",
		"FILESTORE_HOST", "FILESTORE_DOMAIN_OVERRIDE", "CLSI_CACHE_INSTANCES",
		"CLSI_CACHE_CURRENT_SHARDS", "CLSI_CACHE_DESIRED_SHARDS",
		"SMOKE_TEST", "FILESTORE_PARALLEL_FILE_DOWNLOADS",
		"TEX_LIVE_DOCKER_IMAGE_ROOT_4TYPST", "TEXLIVE_OPENOUT_ANY",
		"TEXLIVE_MAX_PRINT_LINE", "ENABLE_PDF_CACHING", "ENABLE_PDF_CACHING_DARK",
		"PDF_CACHING_MIN_CHUNK_SIZE", "PDF_CACHING_MAX_PROCESSING_TIME",
		"PDF_CACHING_ENABLE_WORKER_POOL", "PDF_CACHING_WORKER_POOL_SIZE",
		"PDF_CACHING_WORKER_POOL_BACK_LOG_LIMIT", "CLSI_PERFORMANCE_LOG_SAMPLING",
		"ALLOWED_COMPILE_GROUPS", "SANDBOXED_COMPILES", "DOCKER_RUNNER",
		"DOCKER_RUNTIME", "TYPST_IMAGE", "TYPST_DOCKER_IMAGE",
		"ALL_TYPST_DOCKER_IMAGES", "TYPST_IMAGE_USER", "SECCOMP_PROFILE",
		"SANDBOXED_COMPILES_HOST_DIR_COMPILES", "SANDBOXED_COMPILES_HOST_DIR",
		"COMPILES_HOST_DIR", "SANDBOXED_COMPILES_HOST_DIR_CACHE",
		"SANDBOXED_COMPILES_HOST_DIR_OUTPUT", "OUTPUT_HOST_DIR",
	}
	for _, k := range clearList {
		os.Unsetenv(k)
	}
}

func TestNew_Defaults(t *testing.T) {
	clearEnv()
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !c.CompileTypstEnabled {
		t.Error("gate default must be ON")
	}
	if c.CompileSizeLimit != "7mb" {
		t.Errorf("compileSizeLimit = %q", c.CompileSizeLimit)
	}
	if c.ProcessLifespanMs != 60*60*24*1000*2 {
		t.Errorf("processLifespanMs = %d", c.ProcessLifespanMs)
	}
	if c.CatchErrors {
		t.Error("catchErrors default must be false")
	}
	if c.MaxUploadSize != 50*1024*1024 {
		t.Errorf("maxUploadSize = %d", c.MaxUploadSize)
	}
	if c.Path.CompilesDir != "compiles" || c.Path.OutputDir != "output" ||
		c.Path.ClsiCacheDir != "cache" || c.Path.UploadFolder != "uploads" {
		t.Errorf("path defaults wrong: %+v", c.Path)
	}
	if c.Path.SynctexBase != "/compile" {
		t.Errorf("synctexBase = %q", c.Path.SynctexBase)
	}
	if c.Internal.Port != 3014 || c.Internal.Host != "127.0.0.1" {
		t.Errorf("listen wrong: %+v", c.Internal)
	}
	if !c.Internal.Load.ReportLoad {
		t.Error("reportLoad default must be true")
	}
	if c.Internal.Load.LoadPort != 3046 || c.Internal.Load.LocalPort != 3047 {
		t.Errorf("load ports wrong: %+v", c.Internal.Load)
	}
	if !c.Internal.Load.AllowMaintenance {
		t.Error("allowMaintenance default must be true")
	}
	if c.APIs.Clsi.URL != "http://127.0.0.1:3014" {
		t.Errorf("clsi url = %q", c.APIs.Clsi.URL)
	}
	if c.APIs.Clsi.OutputURLPrefix != "" {
		t.Errorf("outputUrlPrefix = %q (no zone)", c.APIs.Clsi.OutputURLPrefix)
	}
	if c.APIs.Clsi.DownloadHost != "http://localhost:8080" {
		t.Errorf("downloadHost = %q", c.APIs.Clsi.DownloadHost)
	}
	if c.APIs.Perf.Host != "127.0.0.1:3043" {
		t.Errorf("perf host = %q", c.APIs.Perf.Host)
	}
	if c.APIs.FileStore.URL != "http://127.0.0.1:3009" {
		t.Errorf("filestore url = %q", c.APIs.FileStore.URL)
	}
	if c.APIs.Cache.Enabled || len(c.APIs.Cache.Shards) != 0 {
		t.Error("cache default must be disabled")
	}
	if c.SmokeTest {
		t.Error("smokeTest default must be false")
	}
	if c.ProjectCacheLengthMs != 1000*60*60*24 {
		t.Errorf("projectCacheLengthMs = %d", c.ProjectCacheLengthMs)
	}
	if c.ParallelFileDownloads != 1 {
		t.Errorf("parallelFileDownloads = %d", c.ParallelFileDownloads)
	}
	if c.TexliveImageNameOverride != "" || c.TexliveOpenoutAny != "" ||
		c.TexliveMaxPrintLine != "" {
		t.Error("texlive overrides default empty")
	}
	if c.EnablePdfCaching || c.EnablePdfCachingDark {
		t.Error("pdf caching defaults must be false")
	}
	if c.PdfCachingMinChunkSize != 1024 ||
		c.PdfCachingMaxProcessingTime != 10*1000 {
		t.Errorf("pdf caching sizes wrong: %d / %d",
			c.PdfCachingMinChunkSize, c.PdfCachingMaxProcessingTime)
	}
	if c.PdfCachingEnableWorkerPool ||
		c.PdfCachingWorkerPoolSize != 4 ||
		c.PdfCachingWorkerPoolBackLogLimit != 40 {
		t.Errorf("worker pool wrong: %+v", c)
	}
	if c.CompileConcurrencyLimit != 64 {
		t.Errorf("concurrency = %d, want 64 (not spot)", c.CompileConcurrencyLimit)
	}
	if len(c.AllowedCompileGroups) != 0 {
		t.Errorf("allowedCompileGroups = %v", c.AllowedCompileGroups)
	}
	// sandboxed NOT set -> clsi docker block empty, sandbox dirs empty.
	if c.ClSI.DockerRunner || c.ClSI.Docker.Image != "" ||
		c.ClSI.Docker.User != "" {
		t.Errorf("clsi docker block must be empty without sandbox: %+v", c.ClSI)
	}
	if c.ClSI.CompileGroupConfig != nil {
		t.Errorf("compileGroupConfig = %v", c.ClSI.CompileGroupConfig)
	}
}

func TestNew_EnvOverrides(t *testing.T) {
	clearEnv()
	setEnvs(t, map[string]string{
		"COMPILE_TYPEST_ENABLED":                 "false",
		"COMPILE_SIZE_LIMIT":                     "9mb",
		"PROCESS_LIFE_SPAN_LIMIT_MS":             "123456",
		"CATCH_ERRORS":                           "true",
		"PRECIOUS_FILE_PATTERN":                  "keep\\.(pdf|log)",
		"CLSI_TYPST_COMPILES_PATH":               "/clsi/compiles",
		"CLSI_TYPST_OUTPUT_PATH":                 "/clsi/output",
		"CLSI_TYPST_CACHE_PATH":                  "/clsi/cache",
		"CLSI_TYPST_UPLOAD_PATH":                 "/clsi/uploads",
		"LISTEN_ADDRESS":                         "10.0.0.1",
		"CLSI_TYPST_PORT":                        "9090",
		"LOAD_BALANCER_AGENT_REPORT_LOAD":        "false",
		"CLSI_TYPST_LOAD_PORT":                   "4000",
		"CLSI_TYPST_LOCAL_PORT":                  "4001",
		"LOAD_BALANCER_AGENT_ALLOW_MAINTENANCE":  "false",
		"PREEMPTIBLE":                            "TRUE",
		"CLSI_TYPST_HOST":                        "clsihost",
		"CLSI_TYPST_SERVER_ID":                   "srv-1",
		"ZONE":                                   "zone-1",
		"INSTANCE_TYPE":                          "a1.small",
		"DOWNLOAD_HOST":                          "https://dl.example",
		"CLSI_PERF_HOST":                         "perfhost",
		"CLSI_PERF_PORT":                         "4300",
		"FILESTORE_HOST":                         "fshost",
		"FILESTORE_DOMAIN_OVERRIDE":              "http://fs.example",
		"CLSI_CACHE_INSTANCES":                   `[{"shard":"s0","zone":"zone-1","readOnly":true},{"shard":"s1","zone":"zone-1"}]`,
		"CLSI_CACHE_CURRENT_SHARDS":              "1",
		"CLSI_CACHE_DESIRED_SHARDS":              "2",
		"SMOKE_TEST":                             "true",
		"FILESTORE_PARALLEL_FILE_DOWNLOADS":      "3",
		"TEX_LIVE_DOCKER_IMAGE_ROOT_4TYPST":      "texlive/typst",
		"TEXLIVE_OPENOUT_ANY":                    "1",
		"TEXLIVE_MAX_PRINT_LINE":                 "100",
		"ENABLE_PDF_CACHING":                     "true",
		"ENABLE_PDF_CACHING_DARK":                "true",
		"PDF_CACHING_MIN_CHUNK_SIZE":             "4096",
		"PDF_CACHING_MAX_PROCESSING_TIME":        "20000",
		"PDF_CACHING_ENABLE_WORKER_POOL":         "true",
		"PDF_CACHING_WORKER_POOL_SIZE":           "8",
		"PDF_CACHING_WORKER_POOL_BACK_LOG_LIMIT": "5",
		"CLSI_PERFORMANCE_LOG_SAMPLING":          "0.25",
		"ALLOWED_COMPILE_GROUPS":                 "a b c",
		"SANDBOXED_COMPILES":                     "true",
		"SANDBOXED_COMPILES_HOST_DIR_COMPILES":   "/h/c",
		"SANDBOXED_COMPILES_HOST_DIR_CACHE":      "/h/cache",
		"SANDBOXED_COMPILES_HOST_DIR_OUTPUT":     "/h/o",
	})
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.CompileTypstEnabled {
		t.Error("gate override must be false")
	}
	if c.CompileSizeLimit != "9mb" || c.ProcessLifespanMs != 123456 ||
		!c.CatchErrors || c.PreciousFilePattern != "keep\\.(pdf|log)" {
		t.Errorf("scalar overrides wrong: %+v", c)
	}
	if c.Path.CompilesDir != "/clsi/compiles" ||
		c.Path.OutputDir != "/clsi/output" ||
		c.Path.ClsiCacheDir != "/clsi/cache" ||
		c.Path.UploadFolder != "/clsi/uploads" {
		t.Errorf("path overrides wrong: %+v", c.Path)
	}
	if c.Internal.Port != 9090 || c.Internal.Host != "10.0.0.1" ||
		c.Internal.Load.ReportLoad || c.Internal.Load.LoadPort != 4000 ||
		c.Internal.Load.LocalPort != 4001 ||
		c.Internal.Load.AllowMaintenance {
		t.Errorf("internal overrides wrong: %+v", c.Internal)
	}
	if c.APIs.Clsi.URL != "http://clsihost:9090" ||
		c.APIs.Clsi.OutputURLPrefix != "/zone/zone-1" ||
		c.APIs.Clsi.ClsiServerID != "srv-1" ||
		c.APIs.Clsi.InstanceType != "a1.small" || c.APIs.Clsi.Zone != "zone-1" ||
		!c.APIs.Clsi.IsSpotInstance ||
		c.APIs.Clsi.DownloadHost != "https://dl.example" {
		t.Errorf("apis overrides wrong: %+v", c.APIs.Clsi)
	}
	if c.APIs.Perf.Host != "perfhost:4300" {
		t.Errorf("perf host = %q", c.APIs.Perf.Host)
	}
	if c.APIs.FileStore.URL != "http://fs.example" {
		t.Errorf("filestore = %q", c.APIs.FileStore.URL)
	}
	if !c.APIs.Cache.Enabled || c.APIs.Cache.CurrentShards != 1 ||
		c.APIs.Cache.DesiredShards != 2 {
		t.Errorf("cache scalars wrong: %+v", c.APIs.Cache)
	}
	if len(c.APIs.Cache.Shards) != 1 || c.APIs.Cache.Shards[0].Shard != "s1" {
		t.Errorf("readOnly shard must be filtered, kept: %+v", c.APIs.Cache.Shards)
	}
	if !c.SmokeTest || c.ParallelFileDownloads != 3 ||
		c.FileStoreDomainOverride != "http://fs.example" ||
		c.TexliveImageNameOverride != "texlive/typst" ||
		c.TexliveOpenoutAny != "1" || c.TexliveMaxPrintLine != "100" {
		t.Errorf("misc overrides wrong: %+v", c)
	}
	if !c.EnablePdfCaching || !c.EnablePdfCachingDark ||
		c.PdfCachingMinChunkSize != 4096 ||
		c.PdfCachingMaxProcessingTime != 20000 ||
		!c.PdfCachingEnableWorkerPool || c.PdfCachingWorkerPoolSize != 8 ||
		c.PdfCachingWorkerPoolBackLogLimit != 5 {
		t.Errorf("pdf caching overrides wrong: %+v", c)
	}
	if c.CompileConcurrencyLimit != 32 {
		t.Errorf("spot concurrency = %d, want 32", c.CompileConcurrencyLimit)
	}
	if c.PerfLogSamplingPct != 0.25 {
		t.Errorf("sampling pct = %f", c.PerfLogSamplingPct)
	}
	if len(c.AllowedCompileGroups) != 3 ||
		c.AllowedCompileGroups[0] != "a" || c.AllowedCompileGroups[2] != "c" {
		t.Errorf("allowedCompileGroups = %v", c.AllowedCompileGroups)
	}
}

func TestNew_TypstCompilesDirPrecedence(t *testing.T) {
	clearEnv()
	setEnvs(t, map[string]string{
		"CLSI_TYPST_COMPILES_PATH": "/clsi/compiles",
		"TYPST_COMPILES_DIR":       "/deploy/scoped/compiles",
	})
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.Path.CompilesDir != "/deploy/scoped/compiles" {
		t.Errorf("TYPST_COMPILES_DIR must win: %q", c.Path.CompilesDir)
	}
}

func TestNew_SandboxMissingError(t *testing.T) {
	clearEnv()
	setEnvs(t, map[string]string{"SANDBOXED_COMPILES": "true"})
	if _, err := New(); err == nil {
		t.Fatal("sandbox without compiles host dir must error")
	}
}

func TestNew_SandboxDockerDefaults(t *testing.T) {
	clearEnv()
	setEnvs(t, map[string]string{
		"COMPILES_HOST_DIR":           "/h/fallback",
		"SANDBOXED_COMPILES":          "true",
		"SANDBOXED_COMPILES_HOST_DIR": "/h/main",
		"OUTPUT_HOST_DIR":             "/h/out",
	})
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !c.ClSI.DockerRunner {
		t.Error("DockerRunner must be true under sandbox")
	}
	if c.ClSI.OptimiseInDocker != true ||
		c.ClSI.ExpireProjectAfterIdleMs != 24*60*60*1000 ||
		c.ClSI.CheckProjectsIntervalMs != 10*60*1000 {
		t.Errorf("clsi docker scalars wrong: %+v", c.ClSI)
	}
	d := c.ClSI.Docker
	if d.Image != DefaultDockerImage {
		t.Errorf("default image = %q, want pinned default", d.Image)
	}
	if d.SocketPath != "/var/run/docker.sock" || d.User != "33:33" {
		t.Errorf("docker socket/user wrong: %+v", d)
	}
	if d.Env["HOME"] != "/tmp" || d.Env["CLSI"] != "1" {
		t.Errorf("docker env wrong: %v", d.Env)
	}
	if d.SeccompProf == "" {
		t.Error("seccomp profile must be embedded")
	}
	if c.ClSI.CompileGroupConfig["wordcount"]["HostConfig.AutoRemove"] != true {
		t.Errorf("default compileGroupConfig wrong: %+v", c.ClSI.CompileGroupConfig)
	}
	if c.Path.SandboxHostCompiles != "/h/main" ||
		c.Path.SandboxHostOutput != "/h/out" ||
		c.Path.SandboxHostCache != "" {
		t.Errorf("sandbox dirs wrong: %+v", c.Path)
	}
}

func TestNew_SandboxImagePrecedence(t *testing.T) {
	clearEnv()
	setEnvs(t, map[string]string{
		"SANDBOXED_COMPILES_HOST_DIR": "/h",
		"SANDBOXED_COMPILES":          "true",
		"ALL_TYPST_DOCKER_IMAGES":     "a:1, b:2",
	})
	c, _ := New()
	if c.ClSI.Docker.Image != "a:1" {
		t.Errorf("ALL_TYPST first image = %q", c.ClSI.Docker.Image)
	}
	setEnvs(t, map[string]string{
		"SANDBOXED_COMPILES_HOST_DIR": "/h",
		"SANDBOXED_COMPILES":          "true",
		"ALL_TYPST_DOCKER_IMAGES":     "a:1, b:2",
		"TYPST_DOCKER_IMAGE":          "mid:2",
	})
	c, _ = New()
	if c.ClSI.Docker.Image != "mid:2" {
		t.Errorf("TYPST_DOCKER_IMAGE precedence = %q", c.ClSI.Docker.Image)
	}
	setEnvs(t, map[string]string{
		"SANDBOXED_COMPILES_HOST_DIR": "/h",
		"SANDBOXED_COMPILES":          "true",
		"ALL_TYPST_DOCKER_IMAGES":     "a:1",
		"TYPST_DOCKER_IMAGE":          "mid:2",
		"TYPST_IMAGE":                 "top:3",
		"SECCOMP_PROFILE":             "explicit-json",
		"DOCKER_RUNTIME":              "runc",
		"DOCKER_RUNNER":               "true",
	})
	c, _ = New()
	if c.ClSI.Docker.Image != "top:3" {
		t.Errorf("TYPST_IMAGE precedence = %q", c.ClSI.Docker.Image)
	}
	if c.ClSI.Docker.Runtime != "runc" ||
		c.ClSI.Docker.SeccompProf != "explicit-json" {
		t.Errorf("runtime/seccomp overrides wrong: %+v", c.ClSI.Docker)
	}
}

func TestNew_SandboxAllowedImages(t *testing.T) {
	clearEnv()
	setEnvs(t, map[string]string{
		"SANDBOXED_COMPILES_HOST_DIR": "/h",
		"SANDBOXED_COMPILES":          "true",
		"ALLOWED_IMAGES":              "imgA imgB",
	})
	c, _ := New()
	if len(c.ClSI.Docker.Allowed) != 2 || c.ClSI.Docker.Allowed[0] != "imgA" {
		t.Errorf("allowedImages = %v", c.ClSI.Docker.Allowed)
	}
}

func TestNew_BadCacheInstancesError(t *testing.T) {
	clearEnv()
	setEnvs(t, map[string]string{"CLSI_CACHE_INSTANCES": "{not json"})
	if _, err := New(); err == nil {
		t.Fatal("bad CLSI_CACHE_INSTANCES must error")
	}
}

func TestNew_BadGroupConfigsError(t *testing.T) {
	clearEnv()
	setEnvs(t, map[string]string{
		"SANDBOXED_COMPILES_HOST_DIR":  "/h",
		"SANDBOXED_COMPILES":           "true",
		"COMPILE_GROUP_DOCKER_CONFIGS": "{not json",
	})
	if _, err := New(); err == nil {
		t.Fatal("bad COMPILE_GROUP_DOCKER_CONFIGS must error")
	}
	setEnvs(t, map[string]string{
		"SANDBOXED_COMPILES_HOST_DIR":  "/h",
		"DOCKER_RUNNER":                "true",
		"COMPILE_GROUP_DOCKER_CONFIGS": `{"beta":{"HostConfig.CpuShares":2}}`,
	})
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	merged := c.ClSI.CompileGroupConfig
	if merged["beta"]["HostConfig.CpuShares"] != 2.0 {
		t.Errorf("group merge override wrong: %+v", merged)
	}
	if merged["wordcount"]["HostConfig.AutoRemove"] != true {
		t.Errorf("default wordcount entry lost: %+v", merged)
	}
}

func TestGet_Singleton(t *testing.T) {
	clearEnv()
	setEnvs(t, map[string]string{"CLSI_TYPST_PORT": "4444"})
	c := Get()
	if c.Internal.Port != 4444 {
		t.Errorf("Get() = %+v", c.Internal)
	}
	// second call returns the cached instance
	if Get() != c {
		t.Error("Get() must return the cached singleton")
	}
}

func TestForTest_Rebuilds(t *testing.T) {
	clearEnv()
	setEnvs(t, map[string]string{"CLSI_TYPST_PORT": "5555"})
	c := ForTest()
	if c.Internal.Port != 5555 {
		t.Errorf("ForTest() = %+v", c.Internal)
	}
	if Get() != c {
		t.Error("ForTest must store as the cached config")
	}
}
