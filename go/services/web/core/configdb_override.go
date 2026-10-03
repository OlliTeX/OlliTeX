package core

import (
	"fmt"
	"os"
	"strconv"

	"ollitex/go/libraries/configstore"
)

// Postgres config-DB override (single source of truth — owner decision
// 2026-10-03: the SQLite "fallback" is DANGEROUS and violates one-source-of-
// truth, so the runtime contract is now):
//   - Postgres DSN in env → that store IS the config DB (primary, shared by
//     all instances — same plane as historyv1's chunk/blob stores).
//     Configured but unreachable = HARD BOOT ERROR (never a silent env-only
//     fallback with a potentially divergent view).
//   - No DSN + explicit CONFIG_DB_PATH → the operator-named OFFLINE EMERGENCY
//     SQLite file (loud, never auto-selected).
//   - Neither → env-only (pre-config-DB deployments unchanged).
//
// LoadConfig first reads the env contract (container-boot params keep
// working), THEN applies overrides from the config DB for a curated set of
// NON-SECRET, NON-INFRA keys:
//   - Only clearly-safe site/feature keys may be overridden.
//   - All infra (MongoURI, Redis*, ports, API endpoints) and secrets (session
//     secrets, API credentials, cookie name/domain) and security toggles
//     (AllowPublicAccess) are EXCLUDED — they are container-boot, security,
//     or credential parameters that must not be changed through the config DB.
//
// The curated set is a conservative starter pending owner curation; extend it
// only by review.

// curatedConfigDBKeys is the single source of truth for which config keys may
// be SQLite-overridden. This set is intentionally small and non-secret.
var curatedConfigDBKeys = []string{"AppName", "SiteURL", "CacheStaticAssets"}

// configDBPath — the explicit offline-emergency SQLite file (CONFIG_DB_PATH);
// "" when unset. Postgres (DSN in env) is the single source of truth; this
// file is the operator-named offline bootstrap only, never an implicit
// fallback.
func configDBPath() string { return configstore.OfflinePath() }

// applyConfigDBOverrides applies curated config-DB values to cfg (single
// source of truth: configured store MUST be readable, else boot error).
func applyConfigDBOverrides(cfg *Config) error {
	if dsn := configstore.DSNFromEnv(); dsn != "" {
		store, err := configstore.DialPG(dsn)
		if err != nil {
			return fmt.Errorf("config: shared config DB (Postgres) configured but unreachable: %w", err)
		}
		defer store.Close()
		applyFromStore(cfg, store)
		return nil
	}

	p := configDBPath()
	if p == "" {
		return nil // no config store configured for this deployment → env-only
	}
	if st, err := os.Stat(p); err != nil || st.IsDir() {
		return nil // offline emergency DB not (yet) initialized → env-only
	}
	store, err := configstore.New(p)
	if err != nil {
		return fmt.Errorf("config: explicit offline config DB (CONFIG_DB_PATH=%s) set but unreadable: %w", p, err)
	}
	defer store.Close()
	applyFromStore(cfg, store)
	return nil
}

// applyFromStore reads the curated keys out of store onto cfg.
func applyFromStore(cfg *Config, store configstore.Store) {
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

// ConfigDBPath — the explicit offline-emergency SQLite file ("" when unset),
// shared by the /hub registry admin and the operator CLI. Postgres (DSN in
// env) is the single source of truth; this file is the operator-named
// offline bootstrap only.
func ConfigDBPath() string { return configDBPath() }

// ConfigDBOverridableKeys — a copy of the curated non-secret keys the /hub
// admin may read (GET) / write (PUT). The PUT endpoint validates against
// this set; infra / secrets / security toggles are intentionally absent.
func ConfigDBOverridableKeys() []string {
	out := make([]string, len(curatedConfigDBKeys))
	copy(out, curatedConfigDBKeys)
	return out
}
