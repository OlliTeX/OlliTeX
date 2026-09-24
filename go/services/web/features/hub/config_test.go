package hub

import (
	"path/filepath"
	"testing"
)

func TestHubConfigAllowedKeys(t *testing.T) {
	// Curated (admin-editable — the core.ConfigDBOverridableKeys set):
	for _, k := range []string{"AppName", "SiteURL", "CacheStaticAssets"} {
		if !hubConfigAllowed(k) {
			t.Errorf("hubConfigAllowed(%q) = false, want true (curated key)", k)
		}
	}
	// Excluded (infra / secret / security toggle / unknown):
	for _, k := range []string{"MongoURI", "RedisAddr", "RedisPassword", "SessionSecrets", "APIUser", "APIPassword", "CookieName", "CookieDomain", "AllowPublicAccess", "nope"} {
		if hubConfigAllowed(k) {
			t.Errorf("hubConfigAllowed(%q) = true, want false (excluded key)", k)
		}
	}
}

// TestHubConfigStoreRoundTrip exercises the same store read/write the
// /api/hub/config handlers use (open → set → get → close → reopen → get) for a
// curated key, proving the admin-managed value persists across reopen.
func TestHubConfigStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub-configdb.sqlite3")
	t.Setenv("CONFIG_DB_PATH", path) // drives openHubConfigStore → core.ConfigDBPath()
	st, err := openHubConfigStore()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.Set("AppName", "HubSet", "test"); err != nil {
		t.Fatalf("set: %v", err)
	}
	v, err := st.Get("AppName")
	if err != nil || v != "HubSet" {
		t.Fatalf("get = %q, %v; want %q, nil", v, err, "HubSet")
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
	v2, err := st2.Get("AppName")
	if err != nil || v2 != "HubSet" {
		t.Fatalf("reopen get = %q, %v; want %q, nil", v2, err, "HubSet")
	}
}
