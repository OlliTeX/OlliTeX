package sso

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

func genCertPEM(t *testing.T, notAfter time.Time) string {
	t.Helper()
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1234),
		Subject:      pkix.Name{CommonName: "sso-cert-expiry-test"},
		NotBefore:    time.Now().UTC().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	seed, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, seed.Public(), seed)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestClassifyCertExpiry(t *testing.T) {
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	warn := 30
	cases := []struct {
		name  string
		na    time.Time
		want  string
		wantD int
	}{
		{"expired: in the past", now.Add(-time.Hour), CertExpired, -1},
		{"expired: exactly now", now, CertExpired, 0},
		{"expiring: 29d out", now.Add(29 * 24 * time.Hour), CertExpiring, 29},
		{"ok: beyond 30d warn window", now.Add(100 * 24 * time.Hour), CertOK, 100},
	}
	for _, c := range cases {
		st, d := classifyCertExpiry(c.na, now, warn)
		if st != c.want {
			t.Errorf("%s: status = %q; want %q", c.name, st, c.want)
		}
		// daysLeft truncates a sub-second epsilon, so it lands at wantD or wantD-1.
		if c.want != CertExpired && (d < c.wantD-1 || d > c.wantD) {
			t.Errorf("%s: daysLeft = %d; want %d or %d", c.name, d, c.wantD, c.wantD-1)
		}
	}
}

func TestCertExpiryRow(t *testing.T) {
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	exp := genCertPEM(t, now.Add(-time.Hour))
	if r, ok := certExpiryRow("spMetadata", "publicCert", exp, now, 30); !ok || r.Status != CertExpired || r.Fingerprint == "" {
		t.Errorf("expired cert: ok=%v status=%q fp=%q", ok, r.Status, r.Fingerprint)
	}
	near := genCertPEM(t, now.Add(10*24*time.Hour))
	if r, ok := certExpiryRow("provider:a", "idpCert", near, now, 30); !ok || r.Status != CertExpiring {
		t.Errorf("near cert: ok=%v status=%q (want expiring)", ok, r.Status)
	}
	far := genCertPEM(t, now.Add(200*24*time.Hour))
	if r, ok := certExpiryRow("provider:a", "idpCert", far, now, 30); !ok || r.Status != CertOK {
		t.Errorf("far cert: ok=%v status=%q (want ok)", ok, r.Status)
	}
	if _, ok := certExpiryRow("x", "y", "this is not a pem", now, 30); ok {
		t.Errorf("malformed PEM should not parse (ok=true)")
	}
}

func TestCollectSSOCertificates_Certified(t *testing.T) {
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	cfg := &SSOConfig{
		SPMetadata: &SPConfig{PublicCert: genCertPEM(t, now.Add(-time.Hour))},
	}
	rows := collectSSOCertificates(cfg, now, 30)
	if len(rows) != 1 {
		t.Fatalf("got %d rows; want 1 (spMetadata.publicCert)", len(rows))
	}
	if rows[0].Scope != "spMetadata" || rows[0].Label != "publicCert" || rows[0].Status != CertExpired {
		t.Errorf("row = %+v; want spMetadata.publicCert expired", rows[0])
	}
	// nil config → no rows
	if r := collectSSOCertificates(nil, now, 30); len(r) != 0 {
		t.Errorf("nil config: got %d rows; want 0", len(r))
	}
}
