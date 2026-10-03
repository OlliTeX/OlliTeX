package sso

// ssoCertExpiry.go — SSO/SP certificate-expiry awareness (fedgap-6e).
//
// Oracle-pinned to Node modules/authentication/ssoCertExpiry (X509 notAfter
// parse + sweep, warn window = SSO_CERT_EXPIRY_WARN_DAYS, default 30) and the
// federation seam ssoCertExpiryWarnDays. The classification logic is pure and
// fully unit-tested with generated certificates.

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"ollitex/go/services/web/core"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// CertExpiryStatus — the three possible classifications.
const (
	CertExpired  = "expired"
	CertExpiring = "expiring"
	CertOK       = "ok"
)

// CertExpiryRow — one certificate's expiry classification.
type CertExpiryRow struct {
	Scope       string `json:"scope"`           // e.g. "spMetadata" / "provider:<id>"
	Label       string `json:"label,omitempty"` // e.g. "idpCert" / "publicCert"
	NotAfter    string `json:"notAfter"`        // RFC3339 UTC
	DaysLeft    int    `json:"daysLeft"`
	Status      string `json:"status"`      // expired | expiring | ok
	Fingerprint string `json:"fingerprint"` // hex(sha256(Raw))
}

var errNotPEMCert = errors.New("sso: no PEM CERTIFICATE block")

// ssoCertExpiryWarnDays — warn window (Node: SSO_CERT_EXPIRY_WARN_DAYS, 30).
func ssoCertExpiryWarnDays() int {
	if v := os.Getenv("SSO_CERT_EXPIRY_WARN_DAYS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
			return n
		}
	}
	return 30
}

// parsePEMCert — the first PEM CERTIFICATE block → *x509.Certificate.
func parsePEMCert(pemStr string) (*x509.Certificate, error) {
	for {
		block, rest := pem.Decode([]byte(pemStr))
		if block == nil {
			return nil, errNotPEMCert
		}
		if block.Type == "CERTIFICATE" {
			return x509.ParseCertificate(block.Bytes)
		}
		pemStr = string(rest)
	}
}

// certFingerprint — hex(sha256(cert.Raw)).
func certFingerprint(c *x509.Certificate) string {
	sum := sha256.Sum256(c.Raw)
	return hex.EncodeToString(sum[:])
}

// classifyCertExpiry — pure: expired (<= now) / expiring (<= now+warnDays) / ok.
func classifyCertExpiry(notAfter, now time.Time, warnDays int) (status string, daysLeft int) {
	na := notAfter.UTC()
	// daysLeft is measured from the caller's `now` (the same clock the
	// status switch below uses) — NOT the wall clock: the injectable-clock
	// contract (tests + any caller with a reference time) pins it, and
	// production passes time.Now() so behavior is unchanged there.
	days := int(na.Sub(now).Hours() / 24) // na-now measured from the caller's `now` (positive while valid)
	switch {
	case !na.After(now):
		status = CertExpired
	case na.Before(now.Add(time.Duration(warnDays) * 24 * time.Hour)):
		status = CertExpiring
	default:
		status = CertOK
	}
	return status, days
}

// certExpiryRow — classify one PEM (a cert or a bundle); ok=false if no cert.
func certExpiryRow(scope, label, pemStr string, now time.Time, warnDays int) (CertExpiryRow, bool) {
	c, err := parsePEMCert(pemStr)
	if err != nil || c == nil {
		return CertExpiryRow{}, false
	}
	status, days := classifyCertExpiry(c.NotAfter, now, warnDays)
	return CertExpiryRow{
		Scope:       scope,
		Label:       label,
		NotAfter:    c.NotAfter.UTC().Format(time.RFC3339),
		DaysLeft:    days,
		Status:      status,
		Fingerprint: certFingerprint(c),
	}, true
}

// collectSSOCertificates — every parseable SSO certificate from the config:
// the SP cert (spMetadata.publicCert) + each SAML provider's idpCert,
// publicCert, decryptionCert. Deterministic order (scope,label).
func collectSSOCertificates(cfg *SSOConfig, now time.Time, warnDays int) (out []CertExpiryRow) {
	if cfg == nil {
		return
	}
	add := func(scope, label, pemStr string) {
		if pemStr == "" {
			return
		}
		if r, ok := certExpiryRow(scope, label, pemStr, now, warnDays); ok {
			out = append(out, r)
		}
	}
	if sp := cfg.SPMetadata; sp != nil {
		add("spMetadata", "publicCert", sp.PublicCert)
	}
	for _, p := range cfg.Providers {
		var s SAMLProvider
		if err := bson.Unmarshal(p, &s); err != nil || s.ID == "" {
			continue
		}
		sc := "provider:" + s.ID
		add(sc, "idpCert", s.IdpCert)
		add(sc, "publicCert", s.PublicCert)
		add(sc, "decryptionCert", s.DecryptionCert)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Scope != out[j].Scope {
			return out[i].Scope < out[j].Scope
		}
		return out[i].Label < out[j].Label
	})
	return out
}

// ssoCertExpiry — the /admin/sso/cert-expiry payload + warn count (the boot
// sweep calls the same classifier and logs the expiring/expired certs).
func ssoCertExpiry(a *core.App, cxt *core.Cxt) (rows []CertExpiryRow, warnCount int) {
	db, err := ssoDB(a, cxt)
	if err != nil {
		return nil, 0
	}
	cfg := loadSSOConfig(db, cxt)
	rows = collectSSOCertificates(cfg, time.Now().UTC(), ssoCertExpiryWarnDays())
	for _, r := range rows {
		if r.Status == CertExpired || r.Status == CertExpiring {
			warnCount++
		}
	}
	return rows, warnCount
}
