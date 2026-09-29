package notifications

// D23/D6 wiring (owner order 2026-09-28): notifications resolves its Mongo
// key config-DB -> env (Node 1:1 chain) -> default.

import (
	"path/filepath"
	"testing"

	"ollitex/go/libraries/configstore"
)

func TestConfigWithDefaults_ConfigDBMongoWins(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "configdb.sqlite3")
	st, err := configstore.New(dbPath)
	if err != nil {
		t.Fatalf("configstore.New: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Set("OVERLEAF_MONGO_URL", "mongodb://configdb-host:27017/ollitex", "test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	t.Setenv("CONFIG_DB_PATH", dbPath)
	t.Setenv("MONGO_CONNECTION_STRING", "mongodb://env:27017/env")
	t.Setenv("MONGO_HOST", "envhost")

	c := Config{}
	c.WithDefaults()
	if c.MongoURI != "mongodb://configdb-host:27017/ollitex" {
		t.Errorf("DB OVERLEAF_MONGO_URL must win: %q", c.MongoURI)
	}
}

func TestConfigWithDefaults_EnvChainUnchangedWithoutDB(t *testing.T) {
	t.Setenv("CONFIG_DB_PATH", filepath.Join(t.TempDir(), "absent.sqlite3"))
	t.Setenv("MONGO_HOST", "envhost")

	c := Config{}
	c.WithDefaults()
	if c.MongoURI != "mongodb://envhost/sharelatex" {
		t.Errorf("env chain changed: %q", c.MongoURI)
	}
	if c.Port != 3042 {
		t.Errorf("port drifted: %d", c.Port)
	}
}
