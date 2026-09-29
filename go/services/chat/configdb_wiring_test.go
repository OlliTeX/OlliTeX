package chat

// D23/D6 wiring (owner order 2026-09-28): chat is a config-DB consumer —
// the Mongo connection key resolves config-DB → legacy env chain, where the
// legacy chain keeps the Node 1:1 order untouched.

import (
	"path/filepath"
	"testing"

	"ollitex/go/libraries/configstore"
)

func TestWithDefaults_ConfigDBMongoWins(t *testing.T) {
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
	// both tiers of the legacy env chain set — the DB tier must beat all:
	t.Setenv("MONGO_CONNECTION_STRING", "mongodb://env-a:27017/env")
	t.Setenv("MONGO_HOST", "env-b")

	c := Config{}
	c.WithDefaults()
	if c.MongoURI != "mongodb://configdb-host:27017/ollitex" {
		t.Errorf("DB OVERLEAF_MONGO_URL must win: got %q", c.MongoURI)
	}
}

func TestWithDefaults_EnvChainUnchangedWithoutDB(t *testing.T) {
	t.Setenv("CONFIG_DB_PATH", filepath.Join(t.TempDir(), "absent.sqlite3"))
	t.Setenv("MONGO_HOST", "envhost")

	c := Config{}
	c.WithDefaults()
	if c.MongoURI != "mongodb://envhost/sharelatex" {
		t.Errorf("env chain changed: got %q", c.MongoURI)
	}
	if c.Port != 3010 {
		t.Errorf("port drifted: %d", c.Port)
	}
}
