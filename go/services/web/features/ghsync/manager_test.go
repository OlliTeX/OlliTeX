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
