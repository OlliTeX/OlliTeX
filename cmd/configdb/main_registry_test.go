package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestListAllRegistry — the single source of truth is listable end-to-end.
func TestListAllRegistry(t *testing.T) {
	t.Setenv("CONFIG_DB_PATH", filepath.Join(t.TempDir(), "db.sqlite3"))
	if out, err := runArgs(t, "set", "APP_NAME", "OlliTeX"); err != nil {
		t.Fatalf("set: (%q,%v)", out, err)
	}
	out, err := runArgs(t, "list", "--all")
	if err != nil {
		t.Fatalf("list --all: %v", err)
	}
	for _, want := range []string{"APP_NAME", "OVERLEAF_EMAIL_SMTP_PASS", "security", "[secret]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("list --all missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "set") || !strings.Contains(out, "default") {
		t.Fatalf("list --all should show both set & default statuses:\n%s", out)
	}
	if !strings.Contains(out, "OVERLEAF_EMAIL_SMTP_PASS") {
		t.Fatal("email secrets must be registered")
	}
}

// TestGetMasksSecrets — registered secrets are masked unless --reveal.
func TestGetMasksSecrets(t *testing.T) {
	t.Setenv("CONFIG_DB_PATH", filepath.Join(t.TempDir(), "db.sqlite3"))
	if out, err := runArgs(t, "set", "OVERLEAF_EMAIL_SMTP_PASS", "hunter2"); err != nil {
		t.Fatalf("set: (%q,%v)", out, err)
	}
	out, err := runArgs(t, "get", "OVERLEAF_EMAIL_SMTP_PASS")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if strings.Contains(out, "hunter2") {
		t.Fatalf("secret leaked in listing: %q", out)
	}
	out, err = runArgs(t, "get", "OVERLEAF_EMAIL_SMTP_PASS", "--reveal")
	if err != nil {
		t.Fatalf("get --reveal: %v", err)
	}
	if !strings.Contains(out, "hunter2") {
		t.Fatalf("reveal missing value: %q", out)
	}
	// non-secret still plain
	if out, err := runArgs(t, "set", "APP_NAME", "OlliTeX"); err != nil {
		t.Fatal(out)
	}
	out, err = runArgs(t, "get", "APP_NAME")
	if err != nil || !strings.Contains(out, "OlliTeX") {
		t.Fatalf("APP_NAME get = %q, %v", out, err)
	}
}

// TestSetValidatesKinds — bool/int typing from the registry is enforced.
func TestSetValidatesKinds(t *testing.T) {
	t.Setenv("CONFIG_DB_PATH", filepath.Join(t.TempDir(), "db.sqlite3"))
	if _, err := runArgs(t, "set", "COOKIE_ROLLING_SESSION", "notabool"); err == nil {
		t.Fatal("bool validation should reject 'notabool'")
	}
	if out, err := runArgs(t, "set", "COOKIE_ROLLING_SESSION", "true"); err != nil {
		t.Fatal(out)
	}
	if _, err := runArgs(t, "set", "GIT_BRIDGE_PORT", "abc"); err == nil {
		t.Fatal("int validation should reject 'abc'")
	}
	if out, err := runArgs(t, "set", "GIT_BRIDGE_PORT", "8080"); err != nil {
		t.Fatal(out)
	}
	// unknown keys still pass (forward-compat)
	if out, err := runArgs(t, "set", "FUTURE_KEY", "x"); err != nil {
		t.Fatal(out)
	}
}

// TestImportEnv — the bootstrap/env-file path (D6: set all env params in one
// shot from a .env-shaped file).
func TestImportEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CONFIG_DB_PATH", filepath.Join(dir, "db.sqlite3"))
	envfile := filepath.Join(dir, "env")
	content := `# overleaf env
export APP_NAME="My OlliTeX"
SITE_URL=https://ollitex.example
OVERLEAF_EMAIL_SMTP_PASS=pass123
OVERLEAF_EMAIL_SMTP_PORT=2525
UNKNOWN_KEY=should-skip
`
	if err := os.WriteFile(envfile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runArgs(t, "import-env", envfile)
	if err != nil {
		t.Fatalf("import-env: %v", err)
	}
	if !strings.Contains(out, "imported 4") {
		t.Fatalf("import-env = %q", out)
	}
	out, err = runArgs(t, "get", "APP_NAME")
	if err != nil || !strings.Contains(out, "My OlliTeX") {
		t.Fatalf("APP_NAME after import = %q, %v", out, err)
	}
	out, err = runArgs(t, "list")
	if err != nil || !strings.Contains(out, "SITE_URL") || !strings.Contains(out, "APP_NAME") {
		t.Fatalf("list after import = %q, %v", out, err)
	}
	if strings.Contains(out, "UNKNOWN_KEY") {
		t.Fatalf("unknown key must not be imported: %q", out)
	}
}

// TestInitAndDoctor — the emergency/bootstrap pair.
func TestInitAndDoctor(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CONFIG_DB_PATH", filepath.Join(dir, "db.sqlite3"))
	t.Setenv("CONFIG_DB_ENCRYPTION_KEY", "") // start without a key
	t.Setenv("APP_NAME", "InitFromEnv")

	out, err := runArgs(t, "init")
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if !strings.Contains(out, "encryption: NO key configured") {
		t.Fatalf("init should report missing key: %q", out)
	}
	if !strings.Contains(out, "CONFIG_DB_ENCRYPTION_KEY=") {
		t.Fatalf("init should print the generated key line: %q", out)
	}
	if !strings.Contains(out, "seeded 2 from the process env") { // APP_NAME + CONFIG_DB_PATH
		t.Fatalf("init should seed from env: %q", out)
	}
	out, err = runArgs(t, "get", "APP_NAME")
	if err != nil || !strings.Contains(out, "InitFromEnv") {
		t.Fatalf("APP_NAME after init = %q, %v", out, err)
	}

	out, err = runArgs(t, "doctor")
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if !strings.Contains(out, "encryption: NOT configured") || !strings.Contains(out, "read-back: OK") {
		t.Fatalf("doctor = %q", out)
	}
}
