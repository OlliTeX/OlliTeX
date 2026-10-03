package configstore

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // driver name "pgx"
)

// PGStore is the Postgres-backed key/value configuration store — the PRIMARY
// backend (the stack's shared config DB; the same PG plane historyv1's
// chunk/blob stores use, one DSN: CONFIG_DB_DSN || DATABASE_URL ||
// HISTORY_CONNECTION_STRING).
//
// Table: `configdb` (key TEXT PRIMARY KEY, value TEXT, source TEXT,
// updated_at BIGINT) — distinct from historyv1's tables (chunks, pending_
// chunks, project_blobs, old_chunks). Created if absent (idempotent), so the
// first /hub write or CLI seed initializes the schema.
type PGStore struct {
	db  *sql.DB
	key []byte
	dsn string // for Describe() (redacted on output)
}

// NewPG opens (and pings) the Postgres store named by dsn, ensures the
// `configdb` table exists, and returns the Store. Connection failure is a
// hard error (the PG backend is explicit — unlike the SQLite offline path,
// nobody "accidentally" lands here).
func NewPG(dsn string) (*PGStore, error) {
	key, err := KeyFromEnv()
	if err != nil {
		return nil, err
	}
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("configstore/pg: open: %w", err)
	}
	pingCtxTimeout := 5 // generous but bounded: a dead PG must not hang boot
	conn.SetConnMaxLifetime(time.Hour)
	if err := pingWithTimeout(conn, pingCtxTimeout); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("configstore/pg: connect: %w", err)
	}
	if _, err := conn.Exec(
		`CREATE TABLE IF NOT EXISTS configdb (
			key        TEXT PRIMARY KEY,
			value      TEXT NOT NULL,
			source     TEXT NOT NULL DEFAULT '',
			updated_at BIGINT NOT NULL
		)`,
	); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("configstore/pg: create table: %w", err)
	}
	return &PGStore{db: conn, key: key, dsn: dsn}, nil
}

func pingWithTimeout(db *sql.DB, seconds int) error {
	done := make(chan error, 1)
	go func() { done <- db.Ping() }()
	select {
	case err := <-done:
		return err
	case <-time.After(time.Duration(seconds) * time.Second):
		return fmt.Errorf("connect timeout after %ds", seconds)
	}
}

// Describe identifies the store for operator output (credentials redacted).
func (s *PGStore) Describe() string { return "postgres:" + describeDSN(s.dsn) }

// Close closes the underlying connection pool.
func (s *PGStore) Close() error { return s.db.Close() }

// Get returns the value stored for key (ErrMissing when absent).
func (s *PGStore) Get(key string) (string, error) {
	var v string
	if err := s.db.QueryRow("SELECT value FROM configdb WHERE key = $1", key).Scan(&v); err != nil {
		if err == sql.ErrNoRows {
			return "", ErrMissing
		}
		return "", fmt.Errorf("configstore/pg: get %q: %w", key, err)
	}
	return Open(s.key, v)
}

// Has reports whether key is present.
func (s *PGStore) Has(key string) bool {
	var ok bool
	if err := s.db.QueryRow("SELECT EXISTS(SELECT 1 FROM configdb WHERE key = $1)", key).Scan(&ok); err != nil {
		return false
	}
	return ok
}

// Set upserts key -> value (sealed when the store carries an encryption key).
func (s *PGStore) Set(key, value, source string) error {
	if s.key != nil {
		sev, err := Seal(s.key, value)
		if err != nil {
			return fmt.Errorf("configstore/pg: set %q: %w", key, err)
		}
		value = sev
	}
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(
		`INSERT INTO configdb (key, value, source, updated_at) VALUES ($1,$2,$3,$4)
		 ON CONFLICT (key) DO UPDATE SET
			value = EXCLUDED.value,
			source = EXCLUDED.source,
			updated_at = EXCLUDED.updated_at`,
		key, value, source, now)
	if err != nil {
		return fmt.Errorf("configstore/pg: set %q: %w", key, err)
	}
	return nil
}

// Delete removes key (no-op when absent).
func (s *PGStore) Delete(key string) error {
	if _, err := s.db.Exec("DELETE FROM configdb WHERE key = $1", key); err != nil {
		return fmt.Errorf("configstore/pg: delete %q: %w", key, err)
	}
	return nil
}

// Keys returns the stored keys in ascending order.
func (s *PGStore) Keys() ([]string, error) {
	rows, err := s.db.Query("SELECT key FROM configdb ORDER BY key")
	if err != nil {
		return nil, fmt.Errorf("configstore/pg: keys: %w", err)
	}
	defer rows.Close()
	keys := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("configstore/pg: keys scan: %w", err)
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("configstore/pg: keys rows: %w", err)
	}
	return keys, nil
}

// All returns the current decoded key -> value map.
func (s *PGStore) All() (map[string]string, error) {
	rows, err := s.db.Query("SELECT key, value FROM configdb")
	if err != nil {
		return nil, fmt.Errorf("configstore/pg: all: %w", err)
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("configstore/pg: all scan: %w", err)
		}
		dv, err := Open(s.key, v)
		if err != nil {
			return nil, fmt.Errorf("configstore/pg: all decode %q: %w", k, err)
		}
		m[k] = dv
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("configstore/pg: all rows: %w", err)
	}
	return m, nil
}

// Dump writes a JSON backup of the store to dest (backend-agnostic by design).
func (s *PGStore) Dump(dest string) (map[string]string, error) {
	m, err := s.All()
	if err != nil {
		return nil, err
	}
	return writeDump(m, dest)
}

// Restore upserts every entry of a JSON backup from src (source "restore").
func (s *PGStore) Restore(src string) (int, error) {
	return restoreInto(s, src)
}
