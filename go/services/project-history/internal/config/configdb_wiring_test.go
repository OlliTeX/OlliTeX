package config

// D23/D6 wiring (owner order 2026-09-28): project-history resolves its
// registry-backed keys (WEB_API_USER/PASSWORD, Mongo) config-DB -> env ->
// default.

import (
	"os"
	"path/filepath"
	"testing"

	"ollitex/go/libraries/configstore"
)

func TestLoad_ConfigDBWins(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "configdb.sqlite3")
	st, err := configstore.New(dbPath)
	if err != nil {
		t.Fatalf("configstore.New: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	for _, kv := range [][2]string{
		{"WEB_API_USER", "dbuser"},
		{"WEB_API_PASSWORD", "dbpass"},
		{"OVERLEAF_MONGO_URL", "mongodb://db-mongo:27017/ollitex"},
	} {
		if err := st.Set(kv[0], kv[1], "test"); err != nil {
			t.Fatalf("Set %s: %v", kv[0], err)
		}
	}
	t.Setenv("CONFIG_DB_PATH", dbPath)
	if err := os.Setenv("WEB_API_USER", "envuser"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Unsetenv("WEB_API_USER") })
	if err := os.Setenv("MONGO_CONNECTION_STRING", "mongodb://env:27017/env"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Unsetenv("MONGO_CONNECTION_STRING") })

	cfg := Load()
	if cfg.WebUser != "dbuser" {
		t.Errorf("DB WEB_API_USER not applied: %q", cfg.WebUser)
	}
	if cfg.WebPass != "dbpass" {
		t.Errorf("DB WEB_API_PASSWORD not applied: %q", cfg.WebPass)
	}
	if cfg.MongoURL != "mongodb://db-mongo:27017/ollitex" {
		t.Errorf("DB mongo not applied: %q", cfg.MongoURL)
	}
}

func TestLoad_EnvUnchangedWithoutDB(t *testing.T) {
	t.Setenv("CONFIG_DB_PATH", filepath.Join(t.TempDir(), "absent.sqlite3"))
	if err := os.Setenv("WEB_API_USER", "envuser"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Unsetenv("WEB_API_USER") })
	if err := os.Setenv("MONGO_CONNECTION_STRING", "mongodb://env:27017/env"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Unsetenv("MONGO_CONNECTION_STRING") })

	cfg := Load()
	if cfg.WebUser != "envuser" {
		t.Errorf("env WEB_API_User changed: %q", cfg.WebUser)
	}
	if cfg.MongoURL != "mongodb://env:27017/env" {
		t.Errorf("env mongo changed: %q", cfg.MongoURL)
	}
}
