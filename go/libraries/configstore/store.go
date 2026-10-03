// Package configstore — shared doc for the backend split (Postgres-primary).
//
// Backend selection (single decision point, env-based):
//
//   - Postgres (PRIMARY — the stack's shared config DB, the same plane
//     historyv1's chunk/blob stores live on):
//     CONFIG_DB_DSN || DATABASE_URL (|| HISTORY_CONNECTION_STRING) set →
//     table `configdb` (key/value/source/updated_at) in that database.
//   - SQLite (OFFLINE EMERGENCY FALLBACK — the original P7 local path):
//     no DSN in env → $CONFIG_DB_PATH, else $OVERLEAF_HOME/configdb/…,
//     else ./configdb/configdb.sqlite3.
//
// Both backends implement the Store interface with identical semantics,
// encryption seam (crypto.go), and CRUD surface, so the /hub admin, the
// operator CLI (cmd/configdb), and the toolkit (toolkit/bin/config) manage
// one logical store regardless of backend.
package configstore

import (
	"net/url"
	"os"
	"strings"
)

// Store is the unified key/value configuration store contract.
//
// Implementations: *ConfigStore (SQLite, offline emergency path) and
// *PGStore (Postgres, primary shared store). Callers (hub admin, cmd/configdb,
// configres) program against Store only.
type Store interface {
	// Get returns the value for key, or ErrMissing when absent.
	Get(key string) (string, error)
	// Has reports whether key is present.
	Has(key string) bool
	// Set upserts key -> value (source recorded for audit).
	Set(key, value, source string) error
	// Delete removes key (absent key is a no-op).
	Delete(key string) error
	// Keys returns all keys in ascending order.
	Keys() ([]string, error)
	// All returns the full decoded key -> value map.
	All() (map[string]string, error)
	// Close releases the underlying connection/file handle.
	Close() error
	// Dump writes a JSON backup to dest and returns the map.
	Dump(dest string) (map[string]string, error)
	// Restore upserts every entry of a JSON backup from src (source "restore").
	Restore(src string) (int, error)
	// Describe identifies the concrete store for operator output
	// (credentials redacted).
	Describe() string
}

// DSNEnvNames is the precedence list of Postgres DSN environment variables:
// configstore-specific first, then the stack-wide historyv1/Overleaf 8.x
// connection strings (one shared DSN for the whole PG plane).
var DSNEnvNames = []string{"CONFIG_DB_DSN", "DATABASE_URL", "HISTORY_CONNECTION_STRING"}

// DSNFromEnv returns the first non-empty DSN from DSNEnvNames ("" = none).
func DSNFromEnv() string {
	for _, n := range DSNEnvNames {
		if v := strings.TrimSpace(os.Getenv(n)); v != "" {
			return v
		}
	}
	return ""
}

// Dial resolves the configured backend and opens it (creating objects if
// needed): Postgres when a DSN is in env, otherwise the SQLite file chain.
// This is the single entry point for the web service, the /hub admin, and the
// operator CLI.
func Dial() (Store, error) {
	if dsn := DSNFromEnv(); dsn != "" {
		return NewPG(dsn)
	}
	return New(SQLitePath())
}

// DialFile is the explicit SQLite form (offline emergency path, tests).
func DialFile(dbFile string) (Store, error) { return New(dbFile) }

// DialPG is the explicit Postgres form (key from CONFIG_DB_ENCRYPTION_KEY).
func DialPG(dsn string) (Store, error) { return NewPG(dsn) }

// describeDSN redacts credentials from a connection string for operator
// output (user:pass@… → user:****@…).
func describeDSN(dsn string) string {
	if u, err := url.Parse(dsn); err == nil && u.Host != "" {
		q := u.Query()
		if pw, ok := u.User.Password(); ok && pw != "" {
			u2 := u
			u2.User = url.UserPassword(u.User.Username(), "****")
			if q.Get("password") != "" {
				q.Set("password", "****")
				u2.RawQuery = q.Encode()
			}
			return u2.String()
		}
	}
	return dsn
}
