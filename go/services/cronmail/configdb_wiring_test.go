package cronmail

// D23/D6 wiring (owner order 2026-09-28): cronmail resolves its registry
// keys config-DB → env → default. The Env seam stays for the legacy env
// tiers (the resolver reads os.Getenv underneath it — same source of
// truth).

import (
	"path/filepath"
	"testing"

	"ollitex/go/libraries/configstore"
)

func TestSettingsFromConfigDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "configdb.sqlite3")
	st, err := configstore.New(dbPath)
	if err != nil {
		t.Fatalf("configstore.New: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	for _, kv := range [][2]string{
		{"APP_NAME", "ConfigDB Mailer"},
		{"SITE_URL", "http://configdb.example"},
	} {
		if err := st.Set(kv[0], kv[1], "test"); err != nil {
			t.Fatalf("Set %s: %v", kv[0], err)
		}
	}
	t.Setenv("CONFIG_DB_PATH", dbPath)
	t.Setenv("APP_NAME", "Env Name")
	t.Setenv("PUBLIC_URL", "http://env.example")

	s := NewSettingsFromEnv()
	if s.AppName != "ConfigDB Mailer" {
		t.Errorf("DB APP_NAME not applied: %q", s.AppName)
	}
	if s.SiteURL != "http://configdb.example" {
		t.Errorf("DB SITE_URL not applied: %q", s.SiteURL)
	}
}

func TestSettingsEnvChainUnchangedWithoutDB(t *testing.T) {
	t.Setenv("CONFIG_DB_PATH", filepath.Join(t.TempDir(), "absent.sqlite3"))
	t.Setenv("APP_NAME", "Env Name")

	s := NewSettingsFromEnv()
	if s.AppName != "Env Name" {
		t.Errorf("env APP_NAME not applied: %q", s.AppName)
	}
	if s.SiteURL != "http://127.0.0.1:3000" {
		t.Errorf("default SiteURL drifted: %q", s.SiteURL)
	}
	if envName := s.Env; envName != "server-ce" {
		t.Errorf("Env drifted: %q", envName)
	}
}

func TestConfigFromConfigDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "configdb.sqlite3")
	st, err := configstore.New(dbPath)
	if err != nil {
		t.Fatalf("configstore.New: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	for _, kv := range [][2]string{
		{"PROCESS_NOTIFICATIONS_BATCH_SIZE", "250"},
		{"OVERLEAF_NOTIFICATIONS_MAX_ATTEMPTS", "7"},
		{"OVERLEAF_NOTIFICATIONS_DRY_RUN", "true"},
	} {
		if err := st.Set(kv[0], kv[1], "test"); err != nil {
			t.Fatalf("Set %s: %v", kv[0], err)
		}
	}
	t.Setenv("CONFIG_DB_PATH", dbPath)
	t.Setenv("PROCESS_NOTIFICATIONS_BATCH_SIZE", "10")

	c := NewConfigFromEnv()
	if c.BatchSize != 250 {
		t.Errorf("DB batch size not applied: %d", c.BatchSize)
	}
	if c.MaxAttempts != 7 {
		t.Errorf("DB max attempts not applied: %d", c.MaxAttempts)
	}
	if !c.DryRun {
		t.Errorf("DB dry-run not applied")
	}
}

func TestConfigEnvDefaultsUnchangedWithoutDB(t *testing.T) {
	t.Setenv("CONFIG_DB_PATH", filepath.Join(t.TempDir(), "absent.sqlite3"))

	c := NewConfigFromEnv()
	if c.BatchSize != 100 || c.MaxAttempts != 3 || c.DryRun {
		t.Errorf("defaults drifted: %+v", c)
	}
}
