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

// Path — the canonical SQLite config-DB location (same file core.ConfigDBPath
// and cmd/configdb manage):
//
//	$CONFIG_DB_PATH → $OVERLEAF_HOME/configdb/configdb.sqlite3 → ./configdb/configdb.sqlite3
func Path() string {
	if p := os.Getenv("CONFIG_DB_PATH"); p != "" {
		return p
	}
	if h := os.Getenv("OVERLEAF_HOME"); h != "" {
		return h + "/configdb/configdb.sqlite3"
	}
	return "configdb/configdb.sqlite3"
}

// Open — opens the shared config DB for reading WITHOUT creating it: nil
// (not an error) when the file is absent or unreadable, which callers treat
// as the pre-config-DB deployment (env/default chain unchanged).
func Open() *configstore.ConfigStore {
	if st, err := os.Stat(Path()); err != nil || st.IsDir() {
		return nil
	}
	s, err := configstore.New(Path())
	if err != nil {
		return nil
	}
	return s
}

// Int — config-DB key → env var → dflt.
func Int(st *configstore.ConfigStore, key, envName string, dflt int) int {
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
func String(st *configstore.ConfigStore, key, envName string, dflt string) string {
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
func Bool(st *configstore.ConfigStore, key, envName string, dflt bool) bool {
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
