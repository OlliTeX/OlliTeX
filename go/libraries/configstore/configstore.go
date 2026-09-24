// Package configstore is the SQLite-backed key/value store for the overleaf
// runtime configuration database (P7-post item 1: "SQLite config DB: env
// params -> SQLite, /hub admin + CLI backup").
//
// It layers over the env-based Go config (go/services/web/core/config.go):
// a SQLite-stored value overrides the env/default for the same key. This
// slice delivers only the store itself (CRUD + dump/restore + persistence);
// the /hub admin endpoints (GET/PUT the managed keys) and the
// `go run ./go/cmd/configdb backup|restore` CLI are later slices that use
// this store.
//
// The store is intentionally minimal and dependency-light: one `config`
// table (key/value/source/updated_at), the mattn/go-sqlite3 driver (already
// in go.mod and proven by go/services/gitbridge/db), and a few typed
// methods. It is safe for one-writer/many-reader use (the web process writes
// via /hub; the CLI reads/dumps); SQLite's WAL mode keeps readers unblocked
// by writers.
package configstore

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// ErrMissing is returned by Get when the key is not present in the store.
// Callers use errors.Is(err, ErrMissing) to distinguish "absent" from a real
// error.
var ErrMissing = fmt.Errorf("configstore: key not present")

// ConfigStore is a SQLite-backed key/value configuration store.
type ConfigStore struct {
	db *sql.DB
}

// New opens (creating if needed) the SQLite database at dbFile and ensures
// the `config` table exists. The parent directory is created if absent
// (0o755). It returns an error rather than panicking or returning a nil store
// (matching the gitbridge/db.go convention).
func New(dbFile string) (*ConfigStore, error) {
	parent := filepath.Dir(dbFile)
	if info, err := os.Stat(parent); err == nil && !info.IsDir() {
		return nil, fmt.Errorf("configstore: %s exists and is not a directory", parent)
	}
	if info, _ := os.Stat(parent); info == nil {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return nil, fmt.Errorf("configstore: mkdir %s: %w", parent, err)
		}
	}
	conn, err := sql.Open("sqlite3", dbFile)
	if err != nil {
		return nil, fmt.Errorf("configstore: open %s: %w", dbFile, err)
	}
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("configstore: ping %s: %w", dbFile, err)
	}
	// A single connection avoids cross-goroutine "database is locked" races on
	// the write path (the web process writes serialized via /hub); WAL keeps
	// readers unblocked by writers.
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("configstore: pragma journal_mode: %w", err)
	}
	if _, err := conn.Exec(
		"CREATE TABLE IF NOT EXISTS config (\n" +
			"    key        TEXT PRIMARY KEY,\n" +
			"    value      TEXT NOT NULL,\n" +
			"    source     TEXT NOT NULL DEFAULT '',\n" +
			"    updated_at INTEGER NOT NULL\n" +
			")",
	); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("configstore: create table: %w", err)
	}
	return &ConfigStore{db: conn}, nil
}

// Close closes the underlying connection.
func (s *ConfigStore) Close() error { return s.db.Close() }

// Get returns the value stored for key. It returns ErrMissing when key is
// not present, or a wrapped error on failure.
func (s *ConfigStore) Get(key string) (string, error) {
	var v string
	if err := s.db.QueryRow("SELECT value FROM config WHERE key = ?", key).Scan(&v); err != nil {
		if err == sql.ErrNoRows {
			return "", ErrMissing
		}
		return "", fmt.Errorf("configstore: get %q: %w", key, err)
	}
	return v, nil
}

// Has reports whether key is present in the store.
func (s *ConfigStore) Has(key string) bool {
	var n int
	if err := s.db.QueryRow("SELECT 1 FROM config WHERE key = ?", key).Scan(&n); err != nil {
		return false
	}
	return true
}

// Set stores (inserts or updates) key -> value. source records where the
// change came from (e.g. "hub:/hub-admin", "cli:restore"), for auditing.
func (s *ConfigStore) Set(key, value, source string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(
		"INSERT INTO config (key, value, source, updated_at) VALUES (?,?,?,?)\n"+
			"ON CONFLICT(key) DO UPDATE SET\n"+
			"    value = excluded.value,\n"+
			"    source = excluded.source,\n"+
			"    updated_at = excluded.updated_at",
		key, value, source, now)
	if err != nil {
		return fmt.Errorf("configstore: set %q: %w", key, err)
	}
	return nil
}

// Delete removes key from the store. Deleting an absent key is a no-op.
func (s *ConfigStore) Delete(key string) error {
	if _, err := s.db.Exec("DELETE FROM config WHERE key = ?", key); err != nil {
		return fmt.Errorf("configstore: delete %q: %w", key, err)
	}
	return nil
}

// Keys returns the stored keys in ascending order.
func (s *ConfigStore) Keys() ([]string, error) {
	rows, err := s.db.Query("SELECT key FROM config ORDER BY key")
	if err != nil {
		return nil, fmt.Errorf("configstore: keys: %w", err)
	}
	defer rows.Close()
	keys := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("configstore: keys scan: %w", err)
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("configstore: keys rows: %w", err)
	}
	return keys, nil
}

// All returns the current key -> value map (empty map when the store is
// empty, never nil).
func (s *ConfigStore) All() (map[string]string, error) {
	rows, err := s.db.Query("SELECT key, value FROM config")
	if err != nil {
		return nil, fmt.Errorf("configstore: all: %w", err)
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("configstore: all scan: %w", err)
		}
		m[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("configstore: all rows: %w", err)
	}
	return m, nil
}

// Dump writes a JSON backup of the current store (key -> value) to dest and
// returns the map. It is the backing for the `go run` backup CLI.
func (s *ConfigStore) Dump(dest string) (map[string]string, error) {
	m, err := s.All()
	if err != nil {
		return nil, err
	}
	buf, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("configstore: dump marshal: %w", err)
	}
	if err := os.WriteFile(dest, buf, 0o644); err != nil {
		return nil, fmt.Errorf("configstore: dump write %s: %w", dest, err)
	}
	return m, nil
}

// Restore loads a JSON backup (a key -> value map) from src and upserts every
// entry into the store (source recorded as "restore"). It returns the count
// of keys restored.
func (s *ConfigStore) Restore(src string) (int, error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return 0, fmt.Errorf("configstore: restore read %s: %w", src, err)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return 0, fmt.Errorf("configstore: restore parse %s: %w", src, err)
	}
	for k, v := range m {
		if err := s.Set(k, v, "restore"); err != nil {
			return 0, err
		}
	}
	return len(m), nil
}
