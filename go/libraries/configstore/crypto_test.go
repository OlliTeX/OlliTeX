package configstore

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func tmpDB(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "configdb.sqlite3")
}

func keyOrPanic(t *testing.T) []byte {
	t.Helper()
	k, err := ParseKey(GenerateKeyOrPanic(t))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestEncryptionRoundTrip(t *testing.T) {
	s, err := NewWithKey(tmpDB(t), keyOrPanic(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cases := []struct{ k, v string }{
		{"secret-pass", "hunter2"},
		{"secret-user", "smtp-user"},
		{"secret-unix", "über-secret-✓"},
		{"secret-empty", ""},
		{"secret-spaces", "with spaces & symbols !"},
	}
	for _, c := range cases {
		if err := s.Set(c.k, c.v, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := s.Get("secret-pass"); err != nil || got != "hunter2" {
		t.Fatalf("roundtrip = %q, %v", got, err)
	}
	if got, _ := s.Get("secret-empty"); got != "" {
		t.Fatalf("empty roundtrip = %q", got)
	}
	if got, _ := s.Get("secret-spaces"); got != "with spaces & symbols !" {
		t.Fatalf("spaces roundtrip = %q", got)
	}
	if _, err := s.Get("nope"); !errors.Is(err, ErrMissing) {
		t.Fatalf("missing = %v", err)
	}
	raw, err := rawAll(t, s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw["secret-pass"], encPrefix) {
		t.Fatalf("expected encrypted row, got %q", raw["secret-pass"])
	}
	if strings.Contains(raw["secret-pass"], "hunter2") {
		t.Fatal("plaintext visible in stored row")
	}
	if m, _ := s.All(); m["secret-pass"] != "hunter2" || m["secret-unix"] != "über-secret-✓" {
		t.Fatalf("All = %v", m)
	}
}

func TestEncryptionTamperAndWrongKey(t *testing.T) {
	k1 := keyOrPanic(t)
	db := tmpDB(t)
	s, err := NewWithKey(db, k1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("k", "value-1", "test"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	k2 := keyOrPanic(t)
	s2, err := NewWithKey(db, k2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Get("k"); err == nil {
		t.Fatal("expected decrypt failure with wrong key")
	}
	s2.Close()

	s3, err := NewWithKey(db, k1)
	if err != nil {
		t.Fatal(err)
	}
	defer s3.Close()
	var stored string
	if err := s3.db.QueryRow("SELECT value FROM config WHERE key='k'").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored, encPrefix) || len(stored) < 20 {
		t.Fatalf("unexpected stored form %q", stored)
	}
	corrupted := stored[:len(encPrefix)+3] + "AAAA"
	if _, err := s3.db.Exec("UPDATE config SET value=? WHERE key='k'", corrupted); err != nil {
		t.Fatal(err)
	}
	if _, err := s3.Get("k"); err == nil {
		t.Fatal("expected tamper detection")
	}
}

func TestLegacyPlaintextReadableWithAndWithoutKey(t *testing.T) {
	db := tmpDB(t)
	s0, err := NewWithKey(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s0.Set("legacy", "plain-value", "seed"); err != nil {
		t.Fatal(err)
	}
	if err := s0.Set("legacy2", "another", "seed"); err != nil {
		t.Fatal(err)
	}
	s0.Close()

	s, err := NewWithKey(db, keyOrPanic(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, err := s.Get("legacy"); err != nil || got != "plain-value" {
		t.Fatalf("legacy read = %q, %v", got, err)
	}
	if m, _ := s.All(); m["legacy2"] != "another" {
		t.Fatalf("legacy all = %v", m)
	}
	// re-set under the key → encrypted; the untouched legacy row stays readable
	if err := s.Set("legacy", "rotated", "hub"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get("legacy"); got != "rotated" {
		t.Fatalf("re-set = %q", got)
	}
	if got, _ := s.Get("legacy2"); got != "another" {
		t.Fatalf("untouched legacy = %q", got)
	}
	raw, _ := rawAll(t, s)
	if !strings.HasPrefix(raw["legacy"], encPrefix) {
		t.Fatalf("re-set row not encrypted: %q", raw["legacy"])
	}
}

func TestEncryptedValueWithoutKeyIsAHardError(t *testing.T) {
	db := tmpDB(t)
	s, err := NewWithKey(db, keyOrPanic(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("secret", "s3cr3t", "cli"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := NewWithKey(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if _, err := s2.Get("secret"); err == nil {
		t.Fatal("expected 'no key configured' error")
	}
}

func TestParseKeyFormats(t *testing.T) {
	hex64 := GenerateKeyOrPanic(t)
	k, err := ParseKey(hex64)
	if err != nil || len(k) != 32 {
		t.Fatalf("hex: %v %d", err, len(k))
	}
	b64 := base64.StdEncoding.EncodeToString(k)
	if len(b64) != 44 {
		t.Fatalf("b64 len = %d", len(b64))
	}
	k2, err := ParseKey(b64)
	if err != nil || !bytes.Equal(k, k2) {
		t.Fatalf("b64 roundtrip: %v", err)
	}
	for _, bad := range []string{"", "short", hex.EncodeToString(make([]byte, 16)), strings.Repeat("a", 70)} {
		if _, err := ParseKey(bad); err == nil {
			t.Fatalf("ParseKey(%q) should fail", bad)
		}
	}
}

func TestKeyFromEnv(t *testing.T) {
	key := GenerateKeyOrPanic(t)
	t.Setenv(EncryptionKeyEnv, key)
	k, err := KeyFromEnv()
	if err != nil || len(k) != 32 {
		t.Fatalf("KeyFromEnv = %v", err)
	}
	t.Setenv(EncryptionKeyEnv, "")
	if k, err := KeyFromEnv(); err != nil || k != nil {
		t.Fatalf("empty env should be no-key: %v %v", k, err)
	}
	t.Setenv(EncryptionKeyEnv, "not-a-key")
	if _, err := KeyFromEnv(); err == nil {
		t.Fatal("invalid env key should fail")
	}
}

// --- helpers ---

func GenerateKeyOrPanic(t *testing.T) string {
	t.Helper()
	k, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// rawAll reads the RAW stored rows (bypassing decryption) for on-disk checks.
func rawAll(t *testing.T, s *ConfigStore) (map[string]string, error) {
	t.Helper()
	rows, err := s.db.Query("SELECT key, value FROM config")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, nil
}
