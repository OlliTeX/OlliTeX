package dropbox

import (
	"encoding/base64"
	"strings"
	"testing"
)

// TestDbxNormalizeDropboxPath — Node normalizeDropboxPath byte-for-byte.
func TestDbxNormalizeDropboxPath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "/"},
		{"/", "/"},
		{"Overleaf Dev", "/"},
		{"/Overleaf/Dropbox", "/"},
		{"/My%20Docs", "/My Docs"},
		{"/raw-keep", "/raw-keep"},
	}
	for _, c := range cases {
		if got := normalizeDropboxPath(c.in); got != c.want {
			t.Errorf("normalizeDropboxPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestDbxCipherNoSecret — without WEBDAV_TOKEN_CIPHER_PASSWORD / SECRET_TOKEN
// both encrypt and decrypt must fail (the pinned 500 on connect in the
// sandbox).
func TestDbxCipherNoSecret(t *testing.T) {
	t.Setenv("WEBDAV_TOKEN_CIPHER_PASSWORD", "")
	t.Setenv("SECRET_TOKEN", "")
	if _, err := dbxEncryptToken("sl.somerawtoken"); err == nil {
		t.Fatal("expected encryption to fail with no secret")
	}
	if _, err := dbxDecryptToken("AAAA"); err == nil {
		t.Fatal("expected decryption to fail with no secret")
	}
}

// TestDbxCipherRoundTrip — Node's pair round-trips (the 'base64' input
// encoding over buffer.toString('base64') is an identity): Go must
// encrypt/decrypt the same way under the same key.
func TestDbxCipherRoundTrip(t *testing.T) {
	t.Setenv("WEBDAV_TOKEN_CIPHER_PASSWORD", "gate-secret-123")
	tok, err := dbxEncryptToken("sl.oracletoken-1234567890")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(tok)
	if err != nil {
		t.Fatalf("token not std base64: %v", err)
	}
	if len(decoded) < dbxIVLen+dbxTagLen {
		t.Fatalf("token too short: %d bytes", len(decoded))
	}
	got, err := dbxDecryptToken(tok)
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	if got != "sl.oracletoken-1234567890" {
		t.Fatalf("round trip = %q", got)
	}
}

// TestDbxCipherGarbage — malformed stored tokens must fail (both the
// format-check and the all-keys-fail paths return distinguishable
// messages; callers map their own 500 bodies).
func TestDbxCipherGarbage(t *testing.T) {
	t.Setenv("WEBDAV_TOKEN_CIPHER_PASSWORD", "gate-secret-123")
	// too short after base64 decode (< 28 bytes) → 'Invalid encrypted data format'
	_, err := dbxDecryptToken("AAAA")
	if err == nil {
		t.Fatal("expected failure on garbage token")
	}
	// well-formed length but undecryptable → wrapped message
	fake := base64.StdEncoding.EncodeToString([]byte("0123456789ABCDEF0123456789ABCDEF"))
	if _, err := dbxDecryptToken(fake); err == nil {
		t.Fatal("expected failure on well-formed but invalid token")
	}
}

// TestDbxLegacyKeys — String.padEnd(32,'x').slice(0,32) semantics.
func TestDbxLegacyKeys(t *testing.T) {
	t.Setenv("NODE_ENV", "development")
	got := string(dbxLegacyNodeEnvKey())
	want := "overleaf-dropbox-credentials-v2|" // prefix is exactly 32 chars
	if got != want || len(got) != 32 {
		t.Fatalf("legacyNodeEnvKey = %q, want %q", got, want)
	}
	raw := string(dbxLegacyRawKey())
	wantRaw := "development" + strings.Repeat("x", 21) // 11 + 21 = 32
	if raw != wantRaw || len(raw) != 32 {
		t.Fatalf("legacyRawKey = %q (len %d)", raw, len(raw))
	}
}
