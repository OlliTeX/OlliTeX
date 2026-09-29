package config

// D6/D23 wiring (owner order 2026-09-28): clsitex is a config-DB consumer —
// the registry-backed keys in New()/dockerFromEnv resolve through the shared
// configres chain (config-DB → env → default) against the same DB file the
// /hub admin and the operator CLI manage. The tests pin the three tiers:
// DB value beats env; env beats default; malformed DB values fall through.

import (
	"os"
	"path/filepath"
	"testing"

	"ollitex/go/libraries/configstore"
)

// TestNew_ResolvesFromConfigDB — a value present in the config DB overrides
// the env/default tier even when the env var is also set.
func TestNew_ResolvesFromConfigDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "configdb.sqlite3")
	st, err := configstore.New(dbPath)
	if err != nil {
		t.Fatalf("configstore.New: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	for _, kv := range [][2]string{
		{"ENABLE_PNG2PDF_CONVERSIONS", "true"},
		{"PNG2PDF_MIN_FILE_SIZE_BYTES", "2097152"},
		{"ALLOWED_COMPILE_GROUPS", "ol-compiler ol-fast"},
		{"PDF_CACHING_ENABLE_WORKER_POOL", "true"},
		{"PRECIOUS_FILE_PATTERN", `\.pdf$`},
	} {
		if err := st.Set(kv[0], kv[1], "test"); err != nil {
			t.Fatalf("Set %s: %v", kv[0], err)
		}
	}
	setenv := func(k, v string) {
		if err := os.Setenv(k, v); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Unsetenv(k) })
	}
	setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", "/c")
	t.Setenv("CONFIG_DB_PATH", dbPath)
	// env set too — the DB tier must still win (precedence pinned):
	t.Setenv("PNG2PDF_MIN_FILE_SIZE_BYTES", "1024")
	t.Setenv("ALLOWED_COMPILE_GROUPS", "env-group")

	c, err := New()
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	if !c.EnablePng2pdfConversions {
		t.Errorf("DB bool ENABLE_PNG2PDF_CONVERSIONS not applied")
	}
	if c.Png2pdfMinFileSizeBytes != 2097152 {
		t.Errorf("DB int PNG2PDF_MIN_FILE_SIZE_BYTES not applied: got %d", c.Png2pdfMinFileSizeBytes)
	}
	if len(c.AllowedCompileGroups) != 2 || c.AllowedCompileGroups[0] != "ol-compiler" {
		t.Errorf("DB ALLOWED_COMPILE_GROUPS not applied: %v", c.AllowedCompileGroups)
	}
	if !c.PdfCachingEnableWorkerPool {
		t.Errorf("DB bool ENABLE_PDF_CACHING_WORKER_POOL not applied")
	}
	if c.PreciousFilePattern != `\.pdf$` {
		t.Errorf("DB PRECIOUS_FILE_PATTERN not applied: %q", c.PreciousFilePattern)
	}
}

// TestNew_DockerSocketFromConfigDB — dockerFromEnv's registry-backed keys
// (socket path, user, image) resolve through the same chain.
func TestNew_DockerSocketFromConfigDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "configdb.sqlite3")
	st, err := configstore.New(dbPath)
	if err != nil {
		t.Fatalf("configstore.New: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Set("DOCKER_SOCKET_PATH", "/custom/docker.sock", "test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	t.Setenv("CONFIG_DB_PATH", dbPath)
	if err := os.Setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", "/c"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Unsetenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES") })

	c, err := New()
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	if c.ClSI.Docker.SocketPath != "/custom/docker.sock" {
		t.Errorf("DB DOCKER_SOCKET_PATH not applied: %q", c.ClSI.Docker.SocketPath)
	}
}

// TestNew_EnvTierStillWinsWithoutDB — with no config DB, the legacy env chain
// is bit-identical (additive-by-default contract).
func TestNew_EnvTierStillWinsWithoutDB(t *testing.T) {
	if err := os.Setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", "/c"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Unsetenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES") })
	t.Setenv("CONFIG_DB_PATH", filepath.Join(t.TempDir(), "absent.sqlite3"))
	t.Setenv("ENABLE_PNG2PDF_CONVERSIONS", "true")
	t.Setenv("PRECIOUS_FILE_PATTERN", `env-pattern`)

	c, err := New()
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	if !c.EnablePng2pdfConversions {
		t.Errorf("env tier ENABLE_PNG2PDF_CONVERSIONS not applied")
	}
	if c.PreciousFilePattern != "env-pattern" {
		t.Errorf("env tier PRECIOUS_FILE_PATTERN not applied: %q", c.PreciousFilePattern)
	}
	if c.Png2pdfMinFileSizeBytes != 1024*1024 {
		t.Errorf("default PNG2PDF_MIN_FILE_SIZE_BYTES drifted: %d", c.Png2pdfMinFileSizeBytes)
	}
}

// TestNew_MalformedDBValueFallsThrough — an unparseable DB int must fall
// through to env/default (a /hub typo must never crash boot or silently zero
// a limit).
func TestNew_MalformedDBValueFallsThrough(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "configdb.sqlite3")
	st, err := configstore.New(dbPath)
	if err != nil {
		t.Fatalf("configstore.New: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Set("PNG2PDF_MIN_FILE_SIZE_BYTES", "not-a-number", "test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := os.Setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", "/c"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Unsetenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES") })
	t.Setenv("CONFIG_DB_PATH", dbPath)
	t.Setenv("PNG2PDF_MIN_FILE_SIZE_BYTES", "4096")

	c, err := New()
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	if c.Png2pdfMinFileSizeBytes != 4096 {
		t.Errorf("malformed DB value must fall through to env: got %d", c.Png2pdfMinFileSizeBytes)
	}
}

// TestNew_PdftocairoImageFromConfigDB — the conversion image is admin
// tunable (SaaS constraint: local builds like ollitex/pdftocairo).
func TestNew_PdftocairoImageFromConfigDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "configdb.sqlite3")
	st, err := configstore.New(dbPath)
	if err != nil {
		t.Fatalf("configstore.New: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Set("PDFTOCAIRO_IMAGE", "local/pdftocairo:custom", "test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := os.Setenv("CONFIG_DB_PATH", dbPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", "/c"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Unsetenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES") })

	c, err := New()
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	if c.PdftocairoImage != "local/pdftocairo:custom" {
		t.Errorf("DB PDFTOCAIRO_IMAGE not applied: %q", c.PdftocairoImage)
	}
}
