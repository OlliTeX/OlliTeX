package toolkit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crypto/subtle"
)

// fakeToolkits builds a Toolkit with an explicit offline (SQLite) store —
// the loud offline-emergency path (same contract the TUI uses against
// Postgres in production).
func offlineToolkit(t *testing.T) (*Toolkit, string) {
	t.Helper()
	dir := t.TempDir()
	dbf := filepath.Join(dir, "config.db")
	os.Setenv("CONFIG_DB_PATH", dbf)
	t.Cleanup(func() { os.Unsetenv("CONFIG_DB_PATH") })
	tk, err := New(Options{
		DataDir:     dir,
		ComposeFile: filepath.Join(dir, "toolkit.yaml"),
		Project:     "ollitex-test",
		DockerSock:  filepath.Join(dir, "nonexistent.sock"),
	})
	if err != nil {
		t.Fatalf("new toolkit: %v", err)
	}
	t.Cleanup(tk.Close)
	return tk, dir
}

func TestWriteHostKey_0600(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ssh_host_key")
	if err := writeHostKey(p); err != nil {
		t.Fatalf("writeHostKey: %v", err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v, want 0600", fi.Mode().Perm())
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(b), "PRIVATE KEY") {
		t.Fatalf("not a PEM private key block")
	}
}

func TestNewApp_DashboardRenders(t *testing.T) {
	tk, _ := offlineToolkit(t)
	a := newApp(tk)
	a.width, a.height = 100, 30
	a.kind = scrDashboard
	a.Init()
	v := a.View()
	if !strings.Contains(v, "OlliTeX Toolkit") {
		t.Fatalf("dashboard missing title:\n%s", v)
	}
	if !strings.Contains(v, "ollitex-test") {
		t.Fatalf("dashboard missing project: %s", v)
	}
}

func TestNewApp_AllScreensRender(t *testing.T) {
	tk, _ := offlineToolkit(t)
	a := newApp(tk)
	a.width, a.height = 100, 30
	for _, kind := range []screenKind{scrDashboard, scrSettings, scrStack, scrLogs, scrDoctor, scrBackup, scrAbout} {
		a.kind = kind
		v := a.View()
		if strings.TrimSpace(v) == "" {
			t.Fatalf("screen %d rendered empty", kind)
		}
	}
}

func TestSettings_SetGetBoolKind(t *testing.T) {
	tk, _ := offlineToolkit(t)
	s := newSettings(tk)
	if err := s.Set("SITE_OPEN", "yes", "test"); err == nil {
		t.Fatal("expected kind validation to reject non-bool")
	}
	if err := s.Set("SITE_OPEN", "true", "test"); err != nil {
		t.Fatalf("set SITE_OPEN: %v", err)
	}
	v, present, err := s.Get("SITE_OPEN")
	if err != nil || !present || v != "true" {
		t.Fatalf("get SITE_OPEN = %q %v %v", v, present, err)
	}
	if err := s.Delete("SITE_OPEN", "test"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, present, _ := s.Get("SITE_OPEN"); present {
		t.Fatal("key still present after delete")
	}
}

func TestSettings_UnknownKeyRejected(t *testing.T) {
	tk, _ := offlineToolkit(t)
	s := newSettings(tk)
	if err := s.Set("NOT_A_REGISTERED_KEY", "x", "test"); err == nil {
		t.Fatal("expected rejection of unknown key")
	}
}

func TestDoctor_ReportsRows(t *testing.T) {
	tk, _ := offlineToolkit(t)
	rows, err := Doctor(context.Background(), tk)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if len(rows) < 4 {
		t.Fatalf("want >=4 rows, got %d: %+v", len(rows), rows)
	}
	labels := map[string]bool{}
	for _, r := range rows {
		labels[r.Label] = true
	}
	for _, want := range []string{"docker socket", "config store", "compose file", "data dir"} {
		if !labels[want] {
			t.Fatalf("missing doctor row %q in %+v", want, rows)
		}
	}
	// config store must pass (offline sqlite opened fine)
	for _, r := range rows {
		if r.Label == "config store" && !r.OK {
			t.Fatalf("config store row should pass: %+v", r)
		}
	}
}

func subtleEq(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func TestPasswordGate_ConstantTimeBothInputs(t *testing.T) {
	gate := func(user, pass, wantUser, wantPass string) bool {
		return subtleEq(user, wantUser) && subtleEq(pass, wantPass)
	}
	if !gate("ollitex", "s3cret", "ollitex", "s3cret") {
		t.Fatal("valid pair rejected")
	}
	if gate("root", "s3cret", "ollitex", "s3cret") {
		t.Fatal("wrong user accepted")
	}
	if gate("ollitex", "wrong", "ollitex", "s3cret") {
		t.Fatal("wrong pass accepted")
	}
}
