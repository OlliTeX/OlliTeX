package core

import (
	"path/filepath"
	"testing"

	"ollitex/go/libraries/configstore"
)

// seedConfigDB writes key→value pairs into a fresh config DB at dbPath (and
// closes it) so the LoadConfig override path can read them back.
func seedConfigDB(t *testing.T, dbPath string, kv map[string]string) {
	t.Helper()
	st, err := configstore.New(dbPath)
	if err != nil {
		t.Fatalf("configstore.New: %v", err)
	}
	for k, v := range kv {
		if err := st.Set(k, v, "test"); err != nil {
			t.Fatalf("Set(%q): %v", k, err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func minConfigEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	t.Setenv("OVERLEAF_SESSION_SECRET", "test-secret") // LoadConfig requires this
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func TestConfigDBOverride_NoDB_EnvUnchanged(t *testing.T) {
	t.Setenv("CONFIG_DB_PATH", filepath.Join(t.TempDir(), "doesnotexist.sqlite3"))
	minConfigEnv(t, map[string]string{
		"APP_NAME": "EnvName",
		"SITE_URL": "http://env.example",
	})

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.AppName != "EnvName" {
		t.Errorf("AppName = %q, want %q (no DB → env-only)", cfg.AppName, "EnvName")
	}
	if cfg.SiteURL != "http://env.example" {
		t.Errorf("SiteURL = %q, want %q (no DB → env-only)", cfg.SiteURL, "http://env.example")
	}
}

func TestConfigDBOverride_DbOverridesCuratedEnv(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "configdb.sqlite3")
	seedConfigDB(t, dbPath, map[string]string{
		"AppName":           "DBName",
		"SiteURL":           "https://db.example",
		"CacheStaticAssets": "false",
		// Infra/secret keys that must NOT override (asserted below):
		"MongoURI":    "mongodb://evil",
		"APIPassword": "EVIL-apipass",
	})

	t.Setenv("CONFIG_DB_PATH", dbPath)
	minConfigEnv(t, map[string]string{
		"APP_NAME":            "EnvName",
		"SITE_URL":            "http://env.example",
		"CACHE_STATIC_ASSETS": "true",
		"OVERLEAF_MONGO_URL":  "mongodb://env.mongo",
		"WEB_API_PASSWORD":    "env-api-pass",
	})

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.AppName != "DBName" {
		t.Errorf("AppName = %q, want %q (curated DB override)", cfg.AppName, "DBName")
	}
	if cfg.SiteURL != "https://db.example" {
		t.Errorf("SiteURL = %q, want %q (curated DB override)", cfg.SiteURL, "https://db.example")
	}
	if cfg.CacheStaticAssets != false {
		t.Errorf("CacheStaticAssets = %v, want false (curated DB override)", cfg.CacheStaticAssets)
	}
	// Exclusions:
	if cfg.MongoURI == "mongodb://evil" {
		t.Errorf("MongoURI (infra) was overridden by the DB — infra keys must be excluded")
	}
	if cfg.APIPassword != "env-api-pass" {
		t.Errorf("APIPassword = %q, want env value (secret must NOT be overridable)", cfg.APIPassword)
	}
}

func TestConfigDBOverride_SkipsAllInfraAndSecrets(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "configdb.sqlite3")
	seedConfigDB(t, dbPath, map[string]string{
		"MongoURI":       "EVIL-mongo",
		"RedisAddr":      "EVIL-redis",
		"APIUser":        "EVIL-user",
		"APIPassword":    "EVIL-pass",
		"CookieName":     "EVIL-sid",
		"SessionSecrets": "EVIL-secret",
	})

	t.Setenv("CONFIG_DB_PATH", dbPath)
	minConfigEnv(t, map[string]string{
		"WEB_API_PASSWORD": "env-api-pass",
		"WEB_API_USER":     "env-api-user",
		"COOKIE_NAME":      "env-sid",
	})

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.MongoURI == "EVIL-mongo" {
		t.Errorf("MongoURI was overridden — infra key must be excluded")
	}
	if cfg.APIPassword != "env-api-pass" {
		t.Errorf("APIPassword = %q, want env value (credential must NOT be overridable)", cfg.APIPassword)
	}
	if cfg.APIUser != "env-api-user" {
		t.Errorf("APIUser = %q, want env value (credential must NOT be overridable)", cfg.APIUser)
	}
	if cfg.CookieName != "env-sid" {
		t.Errorf("CookieName = %q, want env value (cookie secret must NOT be overridable)", cfg.CookieName)
	}
}
