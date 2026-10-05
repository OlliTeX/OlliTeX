package sso

import (
	"context"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

func httpGetBytes(t *testing.T, url string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s -> HTTP %d: %s", url, res.StatusCode, string(b[:min(len(b), 300)]))
	}
	return b
}

// TestSsoProbeLive verifies the REAL IdPs (tests/tools/sso-test) end-to-end: it
// parses the SAML IdP's live metadata (entityID/SSO/SLO/cert) and resolves an
// OIDC IdP's live discovery doc — the same logic the /admin/sso/test probe
// uses. Skipped unless LIVE_SSO_PROBE=1 is set (needs the sso-test IdPs up).
func TestSsoProbeLive(t *testing.T) {
	if os.Getenv("LIVE_SSO_PROBE") == "" {
		t.Skip("set LIVE_SSO_PROBE=1 to run the live SSO probe E2E (needs tests/tools/sso-test IdPs up)")
	}
	ctx := context.Background()

	samlURL := os.Getenv("LIVE_SAML_META_URL")
	if samlURL == "" {
		// Default = tests/tools/sso-test boxyhq/mock-saml IdP metadata (or point LIVE_SAML_META_URL elsewhere).
		samlURL = "http://127.0.0.1:4100/api/saml/metadata"
	}
	t.Run("SAML metadata parse (live IdP)", func(t *testing.T) {
		data := httpGetBytes(t, samlURL)
		meta, err := parseSAMLIdPMetadata(data)
		if err != nil {
			snip := samlURL
			t.Fatalf("parse live metadata failed: %v (url=%s, first-%d-bytes=%q)",
				err, snip, min(len(data), 300), string(data[:min(len(data), 300)]))
		}
		if meta.EntityID == "" {
			t.Fatal("entityID empty")
		}
		if meta.SSO == "" {
			t.Fatal("SSO Location empty (IdPSSODescriptor/SingleSignOnService not found)")
		}
		if meta.CertCount == 0 {
			t.Fatal("no X509Certificate found in live IdP metadata (needed for SP signing verify)")
		}
		t.Logf("SAML live: entityID=%q sso=%q slo=%q certs=%d", meta.EntityID, meta.SSO, meta.SLO, meta.CertCount)
	})

	issuer := os.Getenv("LIVE_OIDC_ISSUER")
	if issuer == "" {
		// Default = tests/tools/sso-test soluto oidc-server-mock (root issuer, no realm path).
		issuer = "http://127.0.0.1:8080"
	}
	t.Run("OIDC discovery (live IdP)", func(t *testing.T) {
		d, err := fetchOIDCDiscovery(ctx, issuer)
		if err != nil {
			t.Fatalf("live OIDC discovery: %v", err)
		}
		if d.Auth == "" || d.Token == "" {
			t.Fatalf("incomplete live discovery: %+v", d)
		}
		t.Logf("OIDC live: issuer=%q auth=%q token=%q userinfo=%q", d.Issuer, d.Auth, d.Token, d.Userinfo)
	})
}
