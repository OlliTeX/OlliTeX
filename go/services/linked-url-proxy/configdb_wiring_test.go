package linkedurlproxy

// D23/D6 wiring (owner order 2026-09-28): the proxy's policy keys resolve
// config-DB -> env (injected) -> default.

import (
	"path/filepath"
	"testing"

	"ollitex/go/libraries/configstore"
)

func TestConfigDBPolicyWins(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "configdb.sqlite3")
	st, err := configstore.New(dbPath)
	if err != nil {
		t.Fatalf("configstore.New: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Set("OVERLEAF_LINKED_URL_BLOCKED_NETWORKS", "10.99.0.0/16", "test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	t.Setenv("CONFIG_DB_PATH", dbPath)
	getEnv := func(k string) string { return "192.168.0.0/16" }
	cfg := NewLinkedURLProxyConfigFromEnv(getEnv)
	if len(cfg.BlockedNetworks) != 1 || cfg.BlockedNetworks[0] != "10.99.0.0/16" {
		t.Errorf("DB blocked-networks not applied: %v", cfg.BlockedNetworks)
	}
}

func TestConfigEnvUnchangedWithoutDB(t *testing.T) {
	t.Setenv("CONFIG_DB_PATH", filepath.Join(t.TempDir(), "absent.sqlite3"))
	getEnv := func(k string) string {
		if k == "OVERLEAF_LINKED_URL_BLOCKED_NETWORKS" {
			return "192.168.0.0/16"
		}
		return ""
	}
	cfg := NewLinkedURLProxyConfigFromEnv(getEnv)
	if len(cfg.BlockedNetworks) != 1 || cfg.BlockedNetworks[0] != "192.168.0.0/16" {
		t.Errorf("env blocked-networks changed: %v", cfg.BlockedNetworks)
	}
}
