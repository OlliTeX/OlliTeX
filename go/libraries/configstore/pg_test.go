package configstore

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// pgTestDSN returns the Postgres DSN for live PG tests, or "" (→ tests skip).
// Set CONFIGSTORE_TEST_PG_DSN to run (e.g. a scratch postgres:18-alpine).
func pgTestDSN() string { return os.Getenv("CONFIGSTORE_TEST_PG_DSN") }

// TestPGCRUD — the Postgres backend implements the same Store contract as the
// SQLite backend (CRUD + ordering + absence semantics).
func TestPGCRUD(t *testing.T) {
	dsn := pgTestDSN()
	if dsn == "" {
		t.Skip("CONFIGSTORE_TEST_PG_DSN not set (live PG backend test)")
	}
	s, err := NewPG(dsn)
	if err != nil {
		t.Fatalf("NewPG: %v", err)
	}
	defer s.Close()

	if err := s.Set("pg_test_a", "alpha", "test"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := s.Set("pg_test_b", "beta", "test"); err != nil {
		t.Fatalf("set b: %v", err)
	}
	if got, err := s.Get("pg_test_a"); err != nil || got != "alpha" {
		t.Fatalf("get = %q, %v", got, err)
	}
	// upsert semantics
	if err := s.Set("pg_test_a", "alpha2", "test:again"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if got, err := s.Get("pg_test_a"); err != nil || got != "alpha2" {
		t.Fatalf("upsert get = %q, %v", got, err)
	}
	if _, err := s.Get("pg_test_absent"); err != ErrMissing {
		t.Fatalf("get absent err = %v, want ErrMissing", err)
	}
	if !s.Has("pg_test_b") || s.Has("pg_test_absent") {
		t.Fatal("Has semantics broken")
	}
	keys, err := s.Keys()
	if err != nil {
		t.Fatalf("keys: %v", err)
	}
	// ascending order
	for i := 1; i < len(keys); i++ {
		if keys[i-1] > keys[i] {
			t.Fatalf("keys not ascending: %v", keys)
		}
	}
	m, err := s.All()
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if m["pg_test_a"] != "alpha2" || m["pg_test_b"] != "beta" {
		t.Fatalf("all = %v", m)
	}
	// dump + restore (backup/restore is backend-agnostic)
	dest := filepath.Join(t.TempDir(), "backup.json")
	if _, err := s.Dump(dest); err != nil {
		t.Fatalf("dump: %v", err)
	}
	if err := s.Delete("pg_test_a"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n, err := s.Restore(dest); err != nil || n < 2 {
		t.Fatalf("restore = %d, %v", n, err)
	}
	if got, err := s.Get("pg_test_a"); err != nil || got != "alpha2" {
		t.Fatalf("after restore get = %q, %v", got, err)
	}
	// idempotent schema re-open
	s2, err := NewPG(dsn)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	defer s2.Close()
	if got, err := s2.Get("pg_test_b"); err != nil || got != "beta" {
		t.Fatalf("re-open get = %q, %v", got, err)
	}
	// cleanup test keys
	for _, k := range []string{"pg_test_a", "pg_test_b"} {
		if err := s2.Delete(k); err != nil {
			t.Fatalf("cleanup delete %q: %v", k, err)
		}
	}
	if desc := s2.Describe(); desc == "" {
		t.Fatal("Describe empty")
	}
}

// rawStoredValue reads the raw stored bytes for key with a direct keyless
// connection (bypassing the ConfigStore seam) so tests can assert the seal
// prefix actually hit Postgres.
func rawStoredValue(t *testing.T, dsn, key string) (string, error) {
	t.Helper()
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	var v string
	if err := conn.QueryRow("SELECT value FROM configdb WHERE key = $1", key).Scan(&v); err != nil {
		return "", err
	}
	return v, nil
}

// TestPGEncryption — the crypto seam works on the Postgres backend: sealed at
// rest, transparently decoded on read.
func TestPGEncryption(t *testing.T) {
	dsn := pgTestDSN()
	if dsn == "" {
		t.Skip("CONFIGSTORE_TEST_PG_DSN not set (live PG crypto test)")
	}
	// random 32-byte key for THIS test store (independent of env key)
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	key := hex.EncodeToString(buf)
	t.Setenv("CONFIG_DB_ENCRYPTION_KEY", key)

	s, err := NewPG(dsn)
	if err != nil {
		t.Fatalf("NewPG: %v", err)
	}
	defer s.Close()

	const k, secret = "pg_test_enc", "hunter2-secret-value"
	if err := s.Set(k, secret, "crypto-test"); err != nil {
		t.Fatalf("set: %v", err)
	}

	// 1) raw stored bytes must NOT be plaintext (crypto.go seam seals on Set)
	if got, err := s.Get(k); err != nil || got != secret {
		t.Fatalf("decoded get = %q, %v", got, err)
	}
	rawStore, err := rawStoredValue(t, dsn, k)
	if err != nil {
		t.Fatalf("raw read: %v", err)
	}
	if !strings.HasPrefix(rawStore, encPrefix) {
		t.Fatalf("raw stored value %q lacks the %q seal prefix — PG backend did not seal", rawStore, encPrefix)
	}
	// 2) correct key reads back
	t.Setenv("CONFIG_DB_ENCRYPTION_KEY", key)
	s2, err := NewPG(dsn)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if got, err := s2.Get(k); err != nil || got != secret {
		t.Fatalf("encrypted round-trip = %q, %v", got, err)
	}
	if err := s2.Delete(k); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
}
