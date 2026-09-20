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
		"COMPILE_SIZE_LIMIT", "PROCESS_LIFE_SPAN_LIMIT_MS", "CATCH_ERRORS",
		"CLSI_COMPILES_PATH", "CLSI_OUTPUT_PATH", "CLSI_CACHE_PATH", "CLSI_UPLOAD_PATH",
		"CLSI_CONVERSION_TIMEOUT_SECONDS", "PANDOC_IMAGE", "ENABLE_PANDOC_CONVERSIONS",
		"PDFTOCAIRO_IMAGE", "ENABLE_PDF_CONVERSIONS", "PNG2PDF_IMAGE",
		"ENABLE_PNG2PDF_CONVERSIONS", "PNG2PDF_MIN_FILE_SIZE_BYTES",
		"PRECIOUS_FILE_PATTERN", "LISTEN_ADDRESS", "LOAD_BALANCER_AGENT_REPORT_LOAD",
		"LOAD_BALANCER_AGENT_LOAD_PORT", "LOAD_BALANCER_AGENT_LOCAL_PORT",
		"LOAD_BALANCER_AGENT_ALLOW_MAINTENANCE", "PREEMPTIBLE", "CLSI_HOST",
		"CLSI_SERVER_ID", "ZONE", "INSTANCE_TYPE", "DOWNLOAD_HOST", "CLSI_PERF_HOST",
		"CLSI_PERF_PORT", "FILESTORE_HOST", "FILESTORE_DOMAIN_OVERRIDE",
		"CLSI_CACHE_INSTANCES", "CLSI_CACHE_CURRENT_SHARDS", "CLSI_CACHE_DESIRED_SHARDS",
		"CLSI_CACHE_RESHARD_FROM", "CLSI_CACHE_RESHARD_UNTIL", "SMOKE_TEST",
		"FILESTORE_PARALLEL_FILE_DOWNLOADS", "TEX_LIVE_DOCKER_IMAGE_ROOT",
		"TEX_LIVE_IMAGE_NAME_OVERRIDE", "TEXLIVE_OPENOUT_ANY", "TEXLIVE_MAX_PRINT_LINE",
		"ENABLE_PDF_CACHING", "ENABLE_PDF_CACHING_DARK", "PDF_CACHING_MIN_CHUNK_SIZE",
		"PDF_CACHING_MAX_PROCESSING_TIME", "PDF_CACHING_ENABLE_WORKER_POOL",
		"PDF_CACHING_WORKER_POOL_SIZE", "PDF_CACHING_WORKER_POOL_BACK_LOG_LIMIT",
		"CLSI_PERFORMANCE_LOG_SAMPLING", "ALLOWED_COMPILE_GROUPS", "DOCKER_RUNTIME",
		"TEXLIVE_IMAGE", "TEX_LIVE_DOCKER_IMAGE", "ALL_TEX_LIVE_DOCKER_IMAGES",
		"TEXLIVE_IMAGE_USER", "COMPILE_GROUP_DOCKER_CONFIGS", "SECCOMP_PROFILE",
		"APPARMOR_PROFILE", "NEW_APPARMOR_PROFILE", "ALLOWED_IMAGES",
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
	os.Setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", "/c")
	t.Cleanup(func() { os.Unsetenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES") })
	c, err := New()
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	if c.CompileSizeLimit != "7mb" {
		t.Errorf("CompileSizeLimit = %q, want 7mb", c.CompileSizeLimit)
	}
	if c.ProcessLifespanLimitMs != 60*60*24*1000*2 {
		t.Errorf("ProcessLifespanLimitMs = %d", c.ProcessLifespanLimitMs)
	}
	if c.CatchErrors {
		t.Errorf("CatchErrors default should be false")
	}
	if c.MaxUploadSize != 50*1024*1024 {
		t.Errorf("MaxUploadSize = %d", c.MaxUploadSize)
	}
	if c.Internal.Compile.Port != 3013 {
		t.Errorf("port = %d", c.Internal.Compile.Port)
	}
	if c.Internal.Compile.Host != "127.0.0.1" {
		t.Errorf("host = %q", c.Internal.Compile.Host)
	}
	if !c.Internal.LoadBalancerAgent.ReportLoad {
		t.Errorf("ReportLoad default")
	}
	if c.Internal.LoadBalancerAgent.LoadPort != 3048 || c.Internal.LoadBalancerAgent.LocalPort != 3049 {
		t.Errorf("load agent ports = %d/%d", c.Internal.LoadBalancerAgent.LoadPort, c.Internal.LoadBalancerAgent.LocalPort)
	}
	if c.APIs.Compile.URL != "http://127.0.0.1:3013" {
		t.Errorf("clsi url = %q", c.APIs.Compile.URL)
	}
	if c.PandocImage != "quay.io/sharelatex/pandoc:3.9" {
		t.Errorf("pandocImage = %q", c.PandocImage)
	}
	if c.PdftocairoImage != "quay.io/sharelatex/pdftocairo:24.02" {
		t.Errorf("pdftocairoImage = %q", c.PdftocairoImage)
	}
	if c.Png2pdfImage != "quay.io/sharelatex/png2pdf:2026-06-24" {
		t.Errorf("png2pdfImage = %q", c.Png2pdfImage)
	}
	if c.Png2pdfMinFileSizeBytes != 1024*1024 {
		t.Errorf("png2pdfMinFileSizeBytes = %d", c.Png2pdfMinFileSizeBytes)
	}
	if c.ProjectCacheLengthMs != 1000*60*60*24 {
		t.Errorf("projectCacheLengthMs = %d", c.ProjectCacheLengthMs)
	}
	if c.CompileConcurrencyLimit != 64 {
		t.Errorf("compileConcurrencyLimit = %d (want 64)", c.CompileConcurrencyLimit)
	}
	if !c.ClSI.DockerRunner {
		t.Errorf("clsi.dockerRunner must be true (Go bundles the runner)")
	}
	if c.ClSI.Docker.Image != "texlive/texlive:latest-full" {
		t.Errorf("docker image = %q", c.ClSI.Docker.Image)
	}
	if c.ClSI.Docker.User != "www-data" {
		t.Errorf("docker user = %q", c.ClSI.Docker.User)
	}
	if c.Path.SynctexBase != "/compile" {
		t.Errorf("synctexBase = %q", c.Path.SynctexBase)
	}
	if c.SmokeTest {
		t.Errorf("smokeTest default should be false")
	}
	if c.ClSI.Docker.SeccompProfile == "" || c.ClSI.Docker.SeccompProfile[0] != '{' {
		t.Errorf("seccompProfile default not the embedded JSON")
	}
}

func TestNew_EnvOverrides(t *testing.T) {
	clearEnv()
	clearEnv()
	setEnvs(t, map[string]string{
		"SANDBOXED_COMPILES_HOST_DIR_COMPILES": "/c",
		"COMPILE_SIZE_LIMIT":                   "15mb",
		"CATCH_ERRORS":                         "true",
		"PREEMPTIBLE":                          "TRUE",
		"ENABLE_PDF_CACHING":                   "true",
		"CLSI_CACHE_CURRENT_SHARDS":            "3",
		"CLSI_CACHE_INSTANCES":                 `[{"zone":"us-east-1a","readOnly":false,"url":"http://a"},{"zone":"us-east-1b","readOnly":false,"url":"http://b"}]`,
		"ZONE":                                 "us-east-1a",
	})
	c, err := New()
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	if c.CompileSizeLimit != "15mb" {
		t.Errorf("CompileSizeLimit = %q", c.CompileSizeLimit)
	}
	if !c.CatchErrors {
		t.Errorf("CatchErrors should be true")
	}
	if c.CompileConcurrencyLimit != 32 {
		t.Errorf("spot limit = %d (want 32)", c.CompileConcurrencyLimit)
	}
	if !c.EnablePdfCaching {
		t.Errorf("EnablePdfCaching should be true")
	}
	if !c.APIs.OutputCache.Enabled {
		t.Errorf("OutputCache.Enabled should be true")
	}
	if len(c.APIs.OutputCache.Shards) != 1 || c.APIs.OutputCache.Shards[0].URL != "http://a" {
		t.Errorf("shard filter = %+v (want only zone match)", c.APIs.OutputCache.Shards)
	}
	if c.APIs.Compile.IsSpotInstance {
		// PREEMPTIBLE=TRUE => spot
	}
}

func TestNew_SandboxMissingError(t *testing.T) {
	clearEnv()
	if _, err := New(); err == nil {
		t.Errorf("expected error when SANDBOXED_COMPILES host dir unset")
	}
}

func TestDockerFromEnv_CompileGroupMerge(t *testing.T) {
	clearEnv()
	os.Setenv("COMPILE_GROUP_DOCKER_CONFIGS", `{"custom":{"HostConfig.Memory":4194304}}`)
	t.Cleanup(func() { os.Unsetenv("COMPILE_GROUP_DOCKER_CONFIGS") })
	d, err := dockerFromEnv()
	if err != nil {
		t.Fatalf("dockerFromEnv(): %v", err)
	}
	if _, ok := d.CompileGroupConfig["custom"]; !ok {
		t.Errorf("custom group missing: %+v", d.CompileGroupConfig)
	}
	v, _ := d.CompileGroupConfig["wordcount"]["HostConfig.AutoRemove"].(bool)
	if !v {
		t.Errorf("wordcount default missing")
	}
	if d.SeccompProfile == "" {
		t.Errorf("seccompProfile default (embedded) not set")
	}
}

func TestDockerFromEnv_BadConfigsError(t *testing.T) {
	clearEnv()
	os.Setenv("COMPILE_GROUP_DOCKER_CONFIGS", `notjson`)
	t.Cleanup(func() { os.Unsetenv("COMPILE_GROUP_DOCKER_CONFIGS") })
	if _, err := dockerFromEnv(); err == nil {
		t.Errorf("expected error for invalid COMPILE_GROUP_DOCKER_CONFIGS (Node exits 1)")
	}
}

func TestDockerFromEnv_AllowedImages(t *testing.T) {
	clearEnv()
	os.Setenv("ALLOWED_IMAGES", "img1 img2 img3")
	t.Cleanup(func() { os.Unsetenv("ALLOWED_IMAGES") })
	d, err := dockerFromEnv()
	if err != nil {
		t.Fatalf("dockerFromEnv(): %v", err)
	}
	want := []string{"img1", "img2", "img3"}
	if len(d.AllowedImages) != 3 || d.AllowedImages[0] != "img1" {
		t.Errorf("AllowedImages = %v want %v", d.AllowedImages, want)
	}
}
