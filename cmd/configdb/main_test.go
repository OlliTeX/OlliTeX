package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withConfigDB points CONFIG_DB_PATH at a fresh temp DB file for the test.
func withConfigDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "configdb.sqlite3")
	t.Setenv("CONFIG_DB_PATH", path)
	return path
}

func runArgs(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	err := run(args, &buf)
	return buf.String(), err
}

func TestSetThenGet(t *testing.T) {
	withConfigDB(t)
	if out, err := runArgs(t, "set", "SiteTitle", "OlliTeX"); err != nil || !strings.Contains(out, "set SiteTitle") {
		t.Fatalf("set = (%q,%v)", out, err)
	}
	out, err := runArgs(t, "get", "SiteTitle")
	if err != nil || strings.TrimSpace(out) != "OlliTeX" {
		t.Fatalf("get = (%q,%v), want OlliTeX", out, err)
	}
}

func TestGetMissingErrors(t *testing.T) {
	withConfigDB(t)
	if _, err := runArgs(t, "get", "absent"); err == nil {
		t.Fatal("get(absent) should return an error")
	}
}

func TestListAndExport(t *testing.T) {
	withConfigDB(t)
	_, _ = runArgs(t, "set", "a", "1")
	_, _ = runArgs(t, "set", "b", "2")

	out, err := runArgs(t, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "a") || !strings.Contains(out, "b") {
		t.Fatalf("list = %q, want to contain a and b", out)
	}

	exp, err := runArgs(t, "export")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if !strings.Contains(exp, `"a"`) || !strings.Contains(exp, `"1"`) {
		t.Fatalf("export = %q, want JSON with a=1", exp)
	}
}

func TestDeleteIsIdempotent(t *testing.T) {
	withConfigDB(t)
	_, _ = runArgs(t, "set", "k", "v")
	if out, err := runArgs(t, "delete", "k"); err != nil || !strings.Contains(out, "deleted k") {
		t.Fatalf("delete = (%q,%v)", out, err)
	}
	if out, err := runArgs(t, "delete", "k"); err != nil { // second delete: no-op, still success
		t.Fatalf("second delete = (%q,%v), want success", out, err)
	}
	if _, err := runArgs(t, "get", "k"); err == nil {
		t.Fatal("get(k) after delete should error")
	}
}

func TestBackupRestoreRoundTrip(t *testing.T) {
	withConfigDB(t)
	_, _ = runArgs(t, "set", "SiteTitle", "OlliTeX")
	_, _ = runArgs(t, "set", "RegistrationMode", "invite")
	backup := filepath.Join(t.TempDir(), "bk.json")

	out, err := runArgs(t, "backup", backup)
	if err != nil || !strings.Contains(out, "backed up 2 keys to") {
		t.Fatalf("backup = (%q,%v)", out, err)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("backup file not written: %v", err)
	}

	// Restore into a DIFFERENT db (simulates a fresh/migrated instance).
	newPath := filepath.Join(t.TempDir(), "fresh.sqlite3")
	t.Setenv("CONFIG_DB_PATH", newPath)
	out2, err := runArgs(t, "restore", backup)
	if err != nil || !strings.Contains(out2, "restored 2 keys") {
		t.Fatalf("restore = (%q,%v)", out2, err)
	}
	got, err := runArgs(t, "get", "RegistrationMode")
	if err != nil || strings.TrimSpace(got) != "invite" {
		t.Fatalf("get after restore = (%q,%v), want invite", got, err)
	}
}

func TestBackupDefaultNameWritesFile(t *testing.T) {
	withConfigDB(t)
	_, _ = runArgs(t, "set", "k", "v")
	// run from a temp CWD so the default backup lands in a controlled dir
	old, _ := os.Getwd()
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })

	out, err := runArgs(t, "backup")
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if !strings.Contains(out, "backed up 1 keys to") {
		t.Fatalf("backup default = %q", out)
	}
	// some file matches the default pattern
	entries, _ := os.ReadDir(dir)
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "configdb-backup-") && strings.HasSuffix(e.Name(), ".json") {
			found = true
		}
	}
	if !found {
		t.Fatal("default backup file not created")
	}
}

func TestUnknownCommandErrors(t *testing.T) {
	withConfigDB(t)
	if _, err := runArgs(t, "frobnicate"); err == nil {
		t.Fatal("unknown command should error")
	}
}

func TestNoArgsErrors(t *testing.T) {
	withConfigDB(t)
	if _, err := runArgs(t); err == nil {
		t.Fatal("no args should error")
	}
}
