package core

import (
	"os"
	"strconv"

	"ollitex/go/libraries/configstore"
)

// SQLite config-DB override (P7-post item 1: "move non-container-boot env
// params into a SQLite config DB").
//
// LoadConfig first reads the env contract (container-boot params keep
// working), THEN applies overrides from the SQLite config DB for a curated
// set of NON-SECRET, NON-INFRA keys. The override is opt-in and additive:
//   - If the DB file is absent, env-only behavior is completely unchanged.
//   - Only clearly-safe site/feature keys may be overridden.
//   - All infra (MongoURI, Redis*, ports, API endpoints) and secrets (session
//     secrets, API credentials, cookie name/domain) and security toggles
//     (AllowPublicAccess) are EXCLUDED — they are container-boot, security,
//     or credential parameters that must not be changed through the admin
//     config DB.
//
// The curated set is a conservative starter pending owner curation; extend it
// only by review.

// curatedConfigDBKeys is the single source of truth for which config keys may
// be SQLite-overridden. This set is intentionally small and non-secret.
var curatedConfigDBKeys = []string{"AppName", "SiteURL", "CacheStaticAssets"}

// configDBPath resolves the SQLite config-DB file in precedence order:
//
//	CONFIG_DB_PATH → $OVERLEAF_HOME/configdb/configdb.sqlite3 → ./configdb/configdb.sqlite3
//
// (same contract as the cmd/configdb operator CLI, so the /hub admin and the
// CLI manage the SAME database file).
func configDBPath() string {
	if p := os.Getenv("CONFIG_DB_PATH"); p != "" {
		return p
	}
	if h := os.Getenv("OVERLEAF_HOME"); h != "" {
		return h + "/configdb/configdb.sqlite3"
	}
	return "./configdb/configdb.sqlite3"
}

// applyConfigDBOverrides applies curated config-DB values to cfg. It is a
// no-op when the store is absent, unreadable, or has none of the curated keys
// (the caller keeps the env-derived values).
func applyConfigDBOverrides(cfg *Config) {
	path := configDBPath()
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return // no config DB → env-only behavior (unchanged)
	}
	store, err := configstore.New(path)
	if err != nil {
		return // cannot open → env-only behavior (unchanged)
	}
	defer store.Close()

	for _, key := range curatedConfigDBKeys {
		v, err := store.Get(key)
		if err != nil || v == "" {
			continue // absent (ErrMissing) or empty → keep the env value
		}
		switch key {
		case "AppName":
			cfg.AppName = v
		case "SiteURL":
			cfg.SiteURL = v
		case "CacheStaticAssets":
			if b, err := strconv.ParseBool(v); err == nil {
				cfg.CacheStaticAssets = b
			}
		}
	}
}
