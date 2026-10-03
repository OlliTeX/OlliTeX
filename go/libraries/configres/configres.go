// Package configres is the shared resolution contract for runtime
// configuration (D6, 2026-09-25): every Go service that reads a parameter
// from the SQLite config DB resolves it in the SAME order —
//
//	config-DB value  →  legacy env var  →  static default
//
// — against the SAME DB file, so the /hub admin, the operator CLI
// (cmd/configdb), the toolkit, and the services themselves (web, collab, …)
// agree on one store and one precedence chain — with neither re-derived per
// binary.
//
// Values bind at SERVICE START (boot-time semantics, like every other boot
// parameter): a change made in /hub or via the CLI takes effect the next
// time the affected service starts. A service that needs live reload must
// adopt a reload seam deliberately.
//
// The config DB is strictly ADDITIVE: when the file is absent (or a key is
// unset in it) the env/default chain is bit-identical to a pre-config-DB
// deployment — that is what lets a deployment opt into config-DB management
// incrementally, one service at a time.
//
// Precedence rule: the first tier that yields a USABLE value wins. An
// unparseable int (or an empty string) falls through to the next tier — a
// typo in an admin field must never crash a service boot. Zero IS a usable
// value (e.g. COLLAB_KEEP_VERSIONS=0 meaning "keep all") and never falls
// through.
package configres

import (
	"os"
	"strconv"
	"strings"

	"ollitex/go/libraries/configstore"
)

// Path — the explicit offline-emergency SQLite file (CONFIG_DB_PATH). It is
// NEVER an implicit fallback: Postgres (DSN in env) is the single source of
// truth for the shared config DB; this file is for operator-named offline
// bootstrap only.
func Path() string { return configstore.OfflinePath() }

// Open — opens the shared config DB WITHOUT creating it:
//   - Postgres DSN in env  → that store IS the config DB (single source of
//     truth). Configured but unreachable = HARD ERROR (panic at the caller's
//     boot path) — never a silent env-only fallback (owner decision
//     2026-10-03: "feels dangerous, violates the one source of truth").
//   - Explicit CONFIG_DB_PATH (offline emergency, operator-named) → that
//     SQLite file (absent/unreadable → nil / hard error respectively).
//   - Neither configured → nil: pre-config-DB deployment, env/default chain.
func Open() configstore.Store {
	if dsn := configstore.DSNFromEnv(); dsn != "" {
		s, err := configstore.DialPG(dsn)
		if err != nil {
			panic("configres: shared config DB (Postgres) is configured but unreachable: " + err.Error() +
				" — refusing env-only fallback (single source of truth)")
		}
		return s
	}
	if p := configstore.OfflinePath(); p != "" {
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			return nil // offline emergency DB not (yet) initialized → env chain
		}
		s, err := configstore.New(p)
		if err != nil {
			panic("configres: explicit offline config DB (CONFIG_DB_PATH=" + p + ") is set but unreadable: " + err.Error())
		}
		return s
	}
	return nil // no config DB configured for this deployment → env/default chain
}

// Int — config-DB key → env var → dflt.
func Int(st configstore.Store, key, envName string, dflt int) int {
	if st != nil {
		if v, err := st.Get(key); err == nil {
			if n, ok := atoi(v); ok {
				return n
			}
		}
	}
	if envName != "" {
		if v := os.Getenv(envName); v != "" {
			if n, ok := atoi(v); ok {
				return n
			}
		}
	}
	return dflt
}

// String — config-DB key → env var → dflt.
func String(st configstore.Store, key, envName string, dflt string) string {
	if st != nil {
		if v, err := st.Get(key); err == nil && strings.TrimSpace(v) != "" {
			return v
		}
	}
	if envName != "" {
		if v := os.Getenv(envName); v != "" {
			return v
		}
	}
	return dflt
}

// Bool — config-DB key → env var → dflt (standard parse: 0/1/t/f/true/false).
func Bool(st configstore.Store, key, envName string, dflt bool) bool {
	if st != nil {
		if v, err := st.Get(key); err == nil {
			if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
				return b
			}
		}
	}
	if envName != "" {
		if v := os.Getenv(envName); v != "" {
			if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
				return b
			}
		}
	}
	return dflt
}

func atoi(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	return n, err == nil
}
