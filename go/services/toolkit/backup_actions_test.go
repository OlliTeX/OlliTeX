package toolkit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBackup_PanelDataActions — the Backup screen presents BOTH planes:
// the config-store snapshot (b/r) and the data-store round + drill (d/v).
func TestBackup_PanelDataActions(t *testing.T) {
	tk, _ := offlineToolkit(t)
	a := newApp(tk)
	a.screen = "backup"
	v := a.View()
	for _, want := range []string{"b backup", "r restore", "d data backup", "v drill"} {
		if !strings.Contains(v, want) {
			t.Errorf("backup panel missing action %q:\n%s", want, v)
		}
	}
	if !strings.Contains(v, "data root") {
		t.Errorf("backup panel must show the data-root row (…/backups):\n%s", v)
	}
}

// TestBackupDir_EnvOverride — the image path by default; the repo copy when
// the dev override is set (host-side testing drives the real suite).
func TestBackupDir_EnvOverride(t *testing.T) {
	tk, _ := offlineToolkit(t)
	os.Unsetenv("OLLITEX_TOOLKIT_BACKUP_DIR")
	if got := tk.BackupDir(); got != "/opt/ollitex/backup" {
		t.Fatalf("default BackupDir = %q, want /opt/ollitex/backup", got)
	}
	os.Setenv("OLLITEX_TOOLKIT_BACKUP_DIR", "/tmp/fake-backup")
	defer os.Unsetenv("OLLITEX_TOOLKIT_BACKUP_DIR")
	if got := tk.BackupDir(); got != "/tmp/fake-backup" {
		t.Fatalf("override BackupDir = %q, want /tmp/fake-backup", got)
	}
}

// TestRunBackupScript_TailAndExit — the TUI runs the sh suite and surfaces
// its tail: >40 lines collapse to the last 40 with a marker, and a failing
// script returns its output + an exit note (never a Go error — the TUI
// renders a job result, not a crash).
func TestRunBackupScript_TailAndExit(t *testing.T) {
	dir := t.TempDir()

	ok := filepath.Join(dir, "ok.sh")
	var b strings.Builder
	for i := 1; i <= 60; i++ {
		b.WriteString(fmt.Sprintf("echo '[backup] line %d'\n", i))
	}
	b.WriteString("echo '[backup] FINAL line 60'\n")
	if werr := os.WriteFile(ok, []byte("#!/bin/sh\n"+b.String()), 0o755); werr != nil {
		t.Fatal(werr)
	}
	out, err := runBackupScript(dir, "ok.sh", context.Background())
	if err != nil {
		t.Fatalf("ok script must not return a Go error: %v", err)
	}
	if !strings.Contains(out, "earlier lines…") {
		t.Errorf("expected the tail-truncation marker, got:\n%s", out)
	}
	if !strings.Contains(out, "[backup] FINAL line 60") {
		t.Errorf("expected the final line to survive the tail cut, got:\n%s", out)
	}

	bad := filepath.Join(dir, "bad.sh")
	if werr := os.WriteFile(bad, []byte("#!/bin/sh\necho '[backup] about to fail'\nexit 7\n"), 0o755); werr != nil {
		t.Fatal(werr)
	}
	out, err = runBackupScript(dir, "bad.sh", context.Background())
	if err != nil {
		t.Fatalf("failing script must surface as output, not a Go error: %v", err)
	}
	if !strings.Contains(out, "exit") || !strings.Contains(out, "about to fail") {
		t.Errorf("expected the failure tail + exit note, got:\n%s", out)
	}
}
