package languagetool

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLtLevel(t *testing.T) {
	os.Unsetenv("LANGUAGE_TOOL_LEVEL")
	if got := ltLevel(map[string]any{"picky": true}); got != "picky" {
		t.Fatalf("picky:true → %q", got)
	}
	if got := ltLevel(map[string]any{"picky": false}); got != "default" {
		t.Fatalf("picky:false → %q", got)
	}
	if got := ltLevel(map[string]any{"picky": "true"}); got != "default" {
		t.Fatalf("picky:\"true\" (Node picky === true is false) → %q", got)
	}
	if got := ltLevel(map[string]any{}); got != "picky" {
		t.Fatalf("absent, no env → %q", got)
	}
	os.Setenv("LANGUAGE_TOOL_LEVEL", "DEFAULT")
	if got := ltLevel(map[string]any{}); got != "default" {
		t.Fatalf("env DEFAULT (lowercased) → %q", got)
	}
	os.Setenv("LANGUAGE_TOOL_LEVEL", "xx")
	if got := ltLevel(map[string]any{}); got != "picky" {
		t.Fatalf("env xx (invalid) → %q", got)
	}
	os.Unsetenv("LANGUAGE_TOOL_LEVEL")
}

func TestLtResolve(t *testing.T) {
	dir := t.TempDir()
	adminPath := filepath.Join(dir, "admin.json")
	os.Setenv("LLM_ADMIN_SETTINGS_PATH", adminPath)
	os.Unsetenv("LANGUAGE_TOOL_URL")
	os.Unsetenv("LANGUAGETOOL_URL")
	os.Unsetenv("LANGUAGE_TOOL_HOST")
	os.Unsetenv("LANGUAGE_TOOL_PORT")

	// Nothing configured → unavailable.
	r := ltResolve()
	if r.available || r.url != "" {
		t.Fatalf("empty config: %+v", r)
	}

	// Host/port fallback.
	os.Setenv("LANGUAGE_TOOL_HOST", "lt.example")
	if got := ltResolve(); !got.available || got.url != "http://lt.example:8010" {
		t.Fatalf("host fallback: %+v", got)
	}
	os.Unsetenv("LANGUAGE_TOOL_HOST")

	// LANGUAGETOOL_URL trailing-slash trim.
	os.Setenv("LANGUAGETOOL_URL", "http://lt:8550///")
	if got := ltResolve(); got.url != "http://lt:8550" {
		t.Fatalf("env trim: %+v", got)
	}
	os.Unsetenv("LANGUAGETOOL_URL")

	// LANGUAGE_TOOL_URL wins as-is (then trimmed by the resolver).
	os.Setenv("LANGUAGE_TOOL_URL", "http://lt2:9999/")
	if got := ltResolve(); got.url != "http://lt2:9999" {
		t.Fatalf("LANGUAGE_TOOL_URL: %+v", got)
	}
	os.Unsetenv("LANGUAGE_TOOL_URL")

	// Admin JSON wins + admin disable forces unavailable.
	os.WriteFile(adminPath, []byte(`{"languageToolUrl":"http://admin:1234/","languageToolDisabledByAdmin":true}`), 0600)
	if got := ltResolve(); got.available {
		t.Fatalf("admin disabled: %+v", got)
	}
	os.WriteFile(adminPath, []byte(`{"languageToolUrl":"http://admin:1234/","languageToolDisabledByAdmin":false}`), 0600)
	if got := ltResolve(); !got.available || got.url != "http://admin:1234" {
		t.Fatalf("admin url: %+v", got)
	}

	// Bad admin JSON → {}, fall through.
	os.WriteFile(adminPath, []byte(`{bad`), 0600)
	os.Setenv("LANGUAGE_TOOL_URL", "http://fallback:1/")
	if got := ltResolve(); got.url != "http://fallback:1" {
		t.Fatalf("bad admin json fallback: %+v", got)
	}
}
