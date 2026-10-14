package gsync

import (
	"testing"
)

// TestNilCredsDocReadViews — a user WITHOUT a credential doc (fresh account)
// must read as "unlinked / no servers" in the status path, not panic.
//
// Regression: statusHandler (GET /user/github-sync/status) passed the raw
// gsGetCredsDoc result (nil for such users) to gsGetOAuth /
// gsOAuthSlotToken, which dereferenced doc.GitHub → nil pointer dereference
// → 500. Observed on the forged-stack fixture run 2026-10-02 (fresh user
// /user/github-sync/status panicked at manager.go gsGetOAuth).
func TestNilCredsDocReadViews(t *testing.T) {
	linked, uname := gsGetOAuth(nil)
	if linked || uname != "" {
		t.Fatalf("gsGetOAuth(nil) = (%v,%q); want (false,\"\")", linked, uname)
	}
	tok, uname2 := gsOAuthSlotToken(nil)
	if tok != "" || uname2 != "" {
		t.Fatalf("gsOAuthSlotToken(nil) = (%q,%q); want (\"\",\"\")", tok, uname2)
	}
	rows := gsGetPublicServers(nil)
	if len(rows) != 0 {
		t.Fatalf("gsGetPublicServers(nil) = %v; want empty", rows)
	}
}

// TestGetOAuthShapes — legacy string slot, object slot, and absence.
func TestGetOAuthShapes(t *testing.T) {
	// legacy: plain string token
	linked, uname := gsGetOAuth(&gsCredsDoc{GitHub: "enc-legacy"})
	if !linked || uname != "" {
		t.Fatalf("legacy string slot = (%v,%q); want (true,\"\")", linked, uname)
	}
	// object slot
	linked, uname = gsGetOAuth(&gsCredsDoc{GitHub: map[string]any{"token": "enc", "username": "octo"}})
	if !linked || uname != "octo" {
		t.Fatalf("object slot = (%v,%q); want (true,\"octo\")", linked, uname)
	}
	// empty object slot = unlinked
	linked, uname = gsGetOAuth(&gsCredsDoc{GitHub: map[string]any{}})
	if linked || uname != "" {
		t.Fatalf("empty object slot = (%v,%q); want (false,\"\")", linked, uname)
	}
}

// ---- BH-4 surgical SSRF guard (round-2, owner-approved: block loopback/link-local/0/8 only) ----

func mustGSAvailable(t *testing.T, u string) {
	t.Helper()
	if err := gsAssertHttpServerUrl(u); err != nil {
		t.Fatalf("expected ALLOW %q, got: %v", u, err)
	}
}

func mustGSBlocked(t *testing.T, u string) {
	t.Helper()
	err := gsAssertHttpServerUrl(u)
	if err == nil {
		t.Fatalf("expected BLOCK %q, got allow", u)
	}
}

func TestGSAssertServerUrl_SurgicalGuard(t *testing.T) {
	// BLOCKED: loopback / link-local (metadata) / this-network — never legit git hosts
	for _, u := range []string{
		"http://127.0.0.1:3000", "http://127.9.9.9/", "https://0.0.0.0/", "http://0.255.255.255:8080",
		"http://169.254.169.254/", "http://169.254.0.1:80",
		"http://[::1]/", "http://[fe80::1]/", "http://[fe80::abcd%eth0]/", "http://[::]/", "http://[::ffff:127.0.0.1]/",
	} {
		mustGSBlocked(t, u)
	}
	// ALLOWED: legit self-hosted git targets (RFC1918, ULA, public, names)
	for _, u := range []string{
		"http://192.168.1.10:3000", "http://10.0.0.5/", "http://172.16.0.10/", "https://ghe.corp.example/git",
		"https://github.com", "https://8.8.8.8", "http://193.57.139.244:8080", "http://forgejo-test:3000",
		"http://[fd12:3456::7]:3000", "http://[2606:4700:4400::1111]/", "http://93.184.216.34/",
	} {
		mustGSAvailable(t, u)
	}
	// scheme/shape rules preserved
	mustGSBlocked(t, "ftp://github.com")
	mustGSBlocked(t, "http://")
}
