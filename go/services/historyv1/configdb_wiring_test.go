package historyv1

// D23/D6 wiring (owner order 2026-09-28): historyv1 resolves its Mongo +
// PG-DSN registry keys config-DB -> env -> default.

import (
	"os"
	"path/filepath"
	"testing"

	"ollitex/go/libraries/configstore"
)

func TestFromEnv_ConfigDBWins(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "configdb.sqlite3")
	st, err := configstore.New(dbPath)
	if err != nil {
		t.Fatalf("configstore.New: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	for _, kv := range [][2]string{
		{"OVERLEAF_MONGO_URL", "mongodb://db-mongo:27017/ollitex"},
		{"HISTORY_CONNECTION_STRING", "postgres://db-pg:5432/ol"},
	} {
		if err := st.Set(kv[0], kv[1], "test"); err != nil {
			t.Fatalf("Set %s: %v", kv[0], err)
		}
	}
	t.Setenv("CONFIG_DB_PATH", dbPath)
	if err := os.Setenv("MONGO_CONNECTION_STRING", "mongodb://env:27017/env"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Unsetenv("MONGO_CONNECTION_STRING") })

	c := FromEnv()
	if c.MongoURI != "mongodb://db-mongo:27017/ollitex" {
		t.Errorf("DB mongo not applied: %q", c.MongoURI)
	}
	if c.PGDSN != "postgres://db-pg:5432/ol" {
		t.Errorf("DB PG DSN not applied: %q", c.PGDSN)
	}
}

func TestFromEnv_EnvChainUnchangedWithoutDB(t *testing.T) {
	t.Setenv("CONFIG_DB_PATH", filepath.Join(t.TempDir(), "absent.sqlite3"))
	if err := os.Setenv("MONGO_CONNECTION_STRING", "mongodb://env:27017/env"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Unsetenv("MONGO_CONNECTION_STRING") })

	c := FromEnv()
	if c.MongoURI != "mongodb://env:27017/env" {
		t.Errorf("env chain changed: %q", c.MongoURI)
	}
}
