package configstore

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func newTempStore(t *testing.T) (*ConfigStore, string) {
	t.Helper()
	dir := t.TempDir()
	dbFile := filepath.Join(dir, "nested", "config.db") // exercises MkdirAll of the parent
	s, err := New(dbFile)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, dbFile
}

func mustSet(t *testing.T, s *ConfigStore, k, v, src string) {
	t.Helper()
	if err := s.Set(k, v, src); err != nil {
		t.Fatalf("Set(%q): %v", k, err)
	}
}

func TestSetGetRoundTrip(t *testing.T) {
	s, _ := newTempStore(t)
	mustSet(t, s, "SiteTitle", "My OlliTeX", "test")
	got, err := s.Get("SiteTitle")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "My OlliTeX" {
		t.Fatalf("Get = %q, want %q", got, "My OlliTeX")
	}
}

func TestGetMissingReturnsErrMissing(t *testing.T) {
	s, _ := newTempStore(t)
	_, err := s.Get("absent")
	if !errors.Is(err, ErrMissing) {
		t.Fatalf("Get(absent) err = %v, want ErrMissing", err)
	}
	if s.Has("absent") {
		t.Fatal("Has(absent) = true, want false")
	}
}

func TestSetUpsertOverrides(t *testing.T) {
	s, _ := newTempStore(t)
	mustSet(t, s, "k", "first", "a")
	mustSet(t, s, "k", "second", "b")
	got, err := s.Get("k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "second" {
		t.Fatalf("Get = %q, want %q (upsert)", got, "second")
	}
	if keys, _ := s.Keys(); len(keys) != 1 {
		t.Fatalf("Keys len = %d, want 1 (upsert, not duplicate)", len(keys))
	}
}

func TestDelete(t *testing.T) {
	s, _ := newTempStore(t)
	mustSet(t, s, "k", "v", "test")
	if err := s.Delete("k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get("k"); !errors.Is(err, ErrMissing) {
		t.Fatalf("Get after Delete = %v, want ErrMissing", err)
	}
	// deleting an absent key is a no-op, not an error
	if err := s.Delete("absent"); err != nil {
		t.Fatalf("Delete(absent) = %v, want nil", err)
	}
}

func TestKeysAllOrdered(t *testing.T) {
	s, _ := newTempStore(t)
	mustSet(t, s, "b", "2", "test")
	mustSet(t, s, "a", "1", "test")
	mustSet(t, s, "c", "3", "test")
	keys, err := s.Keys()
	if err != nil {
		t.Fatalf("Keys: %v", err)
	}
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("Keys = %v, want %v", keys, want)
	}
	all, err := s.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if want := map[string]string{"a": "1", "b": "2", "c": "3"}; !reflect.DeepEqual(all, want) {
		t.Fatalf("All = %v, want %v", all, want)
	}
}

func TestAllEmptyIsNonNil(t *testing.T) {
	s, _ := newTempStore(t)
	all, err := s.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if all == nil {
		t.Fatal("All on empty store should be non-nil map, got nil")
	}
	if len(all) != 0 {
		t.Fatalf("All len = %d, want 0", len(all))
	}
}

func TestPersistenceAcrossCloseReopen(t *testing.T) {
	dir := t.TempDir()
	dbFile := filepath.Join(dir, "config.db")
	s1, err := New(dbFile)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustSet(t, s1, "persist", "yes", "test")
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s2, err := New(dbFile)
	if err != nil {
		t.Fatalf("reopen New: %v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	got, err := s2.Get("persist")
	if err != nil || got != "yes" {
		t.Fatalf("reopen Get = (%q,%v), want (yes,nil)", got, err)
	}
}

func TestDumpRestoreRoundTrip(t *testing.T) {
	s, _ := newTempStore(t)
	mustSet(t, s, "SiteTitle", "OlliTeX", "test")
	mustSet(t, s, "RegistrationMode", "invite", "test")
	dumpFile := filepath.Join(t.TempDir(), "backup.json")
	dumped, err := s.Dump(dumpFile)
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	if want := map[string]string{"SiteTitle": "OlliTeX", "RegistrationMode": "invite"}; !reflect.DeepEqual(dumped, want) {
		t.Fatalf("Dump map = %v, want %v", dumped, want)
	}
	if _, err := os.Stat(dumpFile); err != nil {
		t.Fatalf("backup file not written: %v", err)
	}
	// restore into a fresh store
	dir2 := t.TempDir()
	restored, err := New(filepath.Join(dir2, "restore.db"))
	if err != nil {
		t.Fatalf("New(restored): %v", err)
	}
	t.Cleanup(func() { _ = restored.Close() })
	n, err := restored.Restore(dumpFile)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if n != 2 {
		t.Fatalf("Restore count = %d, want 2", n)
	}
	got, err := restored.Get("RegistrationMode")
	if err != nil || got != "invite" {
		t.Fatalf("restored Get = (%q,%v), want (invite,nil)", got, err)
	}
}

func TestRestoreOverwritesExisting(t *testing.T) {
	s, _ := newTempStore(t)
	mustSet(t, s, "k", "old", "test")
	backup := filepath.Join(t.TempDir(), "b.json")
	if err := os.WriteFile(backup, []byte(`{"k":"new"}`), 0o644); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	n, err := s.Restore(backup)
	if err != nil || n != 1 {
		t.Fatalf("Restore = (%d,%v), want (1,nil)", n, err)
	}
	got, err := s.Get("k")
	if err != nil || got != "new" {
		t.Fatalf("Get after Restore = (%q,%v), want (new,nil)", got, err)
	}
}

// TestNewRejectsFileAsParent ensures New fails cleanly when the parent path is
// an existing file (not a directory), rather than panicking.
func TestNewRejectsFileAsParent(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "afile")
	if err := os.WriteFile(parent, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// filepath.Dir of the db file is `parent`, an existing FILE -> must error.
	if _, err := New(filepath.Join(parent, "config.db")); err == nil {
		t.Fatal("New should fail when the parent path is an existing file")
	}
}
