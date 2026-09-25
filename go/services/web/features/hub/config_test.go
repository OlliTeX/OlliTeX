package hub

import (
	"path/filepath"
	"sort"
	"testing"

	"ollitex/go/libraries/configschema"
)

// TestHubConfigAllowedKeys — the admin surface follows the registry (the
// single source of truth, D6): every registered key is admin-editable —
// including secrets — and nothing unregistered is.
func TestHubConfigAllowedKeys(t *testing.T) {
	for _, k := range []string{"APP_NAME", "SITE_URL", "CACHE_STATIC_ASSETS", "OVERLEAF_EMAIL_SMTP_PASS", "COOKIE_DOMAIN", "GIT_BRIDGE_PORT"} {
		if !hubConfigAllowed(k) {
			t.Errorf("hubConfigAllowed(%q) = false, want true (registered key)", k)
		}
	}
	for _, k := range []string{"MongoURI", "SessionSecrets", "APIPassword", "nope"} {
		if hubConfigAllowed(k) {
			t.Errorf("hubConfigAllowed(%q) = true, want false (unregistered key)", k)
		}
	}
	for _, p := range configschema.Registry {
		if !hubConfigAllowed(p.Key) {
			t.Errorf("registry key %q must be admin-editable", p.Key)
		}
	}
}

// TestHubConfigStoreRoundTrip exercises the same store read/write the
// /api/hub/config handlers use (open → set → get → close → reopen → get) for a
// registered key, proving the admin-managed value persists across reopen.
func TestHubConfigStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub-configdb.sqlite3")
	t.Setenv("CONFIG_DB_PATH", path) // drives openHubConfigStore → core.ConfigDBPath()
	st, err := openHubConfigStore()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.Set("APP_NAME", "HubSet", "test"); err != nil {
		t.Fatalf("set: %v", err)
	}
	v, err := st.Get("APP_NAME")
	if err != nil || v != "HubSet" {
		t.Fatalf("get = %q, %v; want HubSet", v, err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Reopen — the admin-managed value must persist across restarts:
	st2, err := openHubConfigStore()
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	v2, err := st2.Get("APP_NAME")
	if err != nil || v2 != "HubSet" {
		t.Fatalf("reopen get = %q, %v; want HubSet", v2, err)
	}
}

// TestBuildHubConfigGet — full-registry shape, masking, source attribution.
func TestBuildHubConfigGet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub-configdb.sqlite3")
	t.Setenv("CONFIG_DB_PATH", path)
	t.Setenv("SITE_URL", "https://from-env.example") // env source (not set in DB)
	st, err := openHubConfigStore()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	if err := st.Set("APP_NAME", "HubOlli", "test"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := st.Set("OVERLEAF_EMAIL_SMTP_PASS", "realpass", "test"); err != nil {
		t.Fatalf("set secret: %v", err)
	}

	out, err := buildHubConfigGet(st)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// full registry present
	if len(out) != len(configschema.Registry) {
		t.Fatalf("len(out) = %d, want %d (full registry)", len(out), len(configschema.Registry))
	}
	// db-sourced non-secret: visible
	e := out["APP_NAME"]
	if e.Value == nil || *e.Value != "HubOlli" || e.Source != "db" || e.Masked || e.Secret {
		t.Fatalf("APP_NAME entry = %+v", e)
	}
	// db-sourced secret: masked
	e = out["OVERLEAF_EMAIL_SMTP_PASS"]
	if !e.Secret || !e.Masked || e.Value == nil || *e.Value != "••••" || e.Source != "db" {
		t.Fatalf("secret entry = %+v", e)
	}
	// env-sourced: null value + source env
	e = out["SITE_URL"]
	if e.Value != nil || e.Source != "env" {
		t.Fatalf("SITE_URL entry = %+v", e)
	}
}

// TestValidateHubConfigPut — set/reset/type-check/unknown semantics.
func TestValidateHubConfigPut(t *testing.T) {
	// normal set
	set, reset, bad, msg := validateHubConfigPut(map[string]any{"APP_NAME": "X", "COOKIE_ROLLING_SESSION": "false", "GIT_BRIDGE_PORT": "9999"})
	if bad != "" {
		t.Fatalf("bad = %q: %s", bad, msg)
	}
	if len(set) != 3 || len(reset) != 0 {
		t.Fatalf("set=%v reset=%v", set, reset)
	}
	// reset via empty string
	_, reset, bad, _ = validateHubConfigPut(map[string]any{"APP_NAME": ""})
	if bad != "" || len(reset) != 1 || reset["APP_NAME"] != "" {
		t.Fatalf("reset = %v bad=%q", reset, bad)
	}
	// type violations
	if set, _, bad, _ = validateHubConfigPut(map[string]any{"COOKIE_ROLLING_SESSION": "notabool"}); len(set) != 0 || bad == "" {
		t.Fatalf("bool type-check: set=%v bad=%q", set, bad)
	}
	if _, _, bad, _ = validateHubConfigPut(map[string]any{"GIT_BRIDGE_PORT": "abc"}); bad == "" {
		t.Fatal("int type-check should fail")
	}
	// unknown key
	if _, _, bad, _ = validateHubConfigPut(map[string]any{"NO_SUCH": "x"}); bad != "NO_SUCH" {
		t.Fatalf("unknown = %q", bad)
	}
}

// TestSortedKeys — stable output arrays for the PUT receipt.
func TestSortedKeys(t *testing.T) {
	in := map[string]string{"b": "", "a": "", "c": ""}
	out := sortedKeys(in)
	want := []string{"a", "b", "c"}
	if len(out) != 3 {
		t.Fatalf("len = %d", len(out))
	}
	for i := range out {
		if out[i] != want[i] {
			t.Fatalf("sorted = %v", out)
		}
	}
	sort.Strings(out)
}
