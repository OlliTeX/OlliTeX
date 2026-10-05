// Config — the Go-side env contract for the federation module.
//
// Mirrors the Node `services/web/config/settings.defaults.js` `federation`
// block (per-instance values; the Node module reads these via
// `Settings.federation.*`). The Go read path uses env vars named
// `FEDERATION_*` so the run scripts can flip the module without touching
// the Node settings file. Defaults match the Node defaults (module OFF).
//
// The master toggle, `FEDERATION_ENABLED`, gates EVERY mount in the
// Feature. When off, the S2S endpoint still answers the 200
// `federation-off` envelope (Node: router always mounted, 03 §8).
package federation

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
)

// Settings mirrors the Node `settings.defaults.js` `federation` block
// (the Go side reads the same knobs via FEDERATION_* env; see config.go).
type Settings struct {
	Enabled                     bool
	AllowFederatedProjectCreate bool
	RequireAdminApproval        bool
	KeyRotationGraceDays        int
	InstitutionID               string
	InstitutionAuthorityHints   []string
	S2sFetchTimeoutMS           int
	TokenFetchTimeoutMS         int
	JwksFetchTimeoutMS          int
	ExportEnabled               bool
	ExportMaxTTLSeconds         int
	ExportSweepOnRevoke         bool
	// S2SPeerURLs — peerOrigin → absolute S2S URL (the S2SUROverride
	// local-dev path, s2scall.go: the assertion's iss/aud identity STAYS
	// the https origin (03 §8: entityId is the host WITHOUT port); only
	// the transport dials here). Production leaves this empty (the wire
	// is always https://<origin>/federation/s2s — the trust anchor). Local
	// dual-instance dev (http transport) sets a JSON object:
	// {"peer-origin.example": "http://127.0.0.1:4481/federation/s2s"}.
	S2SPeerURLs map[string]string
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(k string, def bool) bool {
	switch os.Getenv(k) {
	case "true":
		return true
	case "false":
		return false
	default:
		return def
	}
}

// loadSettings reads the env contract (call at boot from start(); the
// Feature reads it lazily per request so `export/rotate` flips take
// effect without a restart).
func loadSettings() Settings {
	var s Settings
	s.Enabled = envBool("FEDERATION_ENABLED", false)
	s.AllowFederatedProjectCreate = envBool("FEDERATION_ALLOW_FEDERATED_PROJECT_CREATE", false)
	s.RequireAdminApproval = envBool("FEDERATION_REQUIRE_ADMIN_APPROVAL", true)
	s.KeyRotationGraceDays = envInt("FEDERATION_KEY_ROTATION_GRACE_DAYS", 14)
	s.InstitutionID = os.Getenv("FEDERATION_INSTITUTION_ID")
	if hints := os.Getenv("FEDERATION_INSTITUTION_AUTHORITY_HINTS"); hints != "" {
		s.InstitutionAuthorityHints = strings.Split(hints, ",")
	}
	s.S2sFetchTimeoutMS = envInt("FEDERATION_S2S_FETCH_TIMEOUT_MS", 10000)
	s.TokenFetchTimeoutMS = envInt("FEDERATION_TOKEN_FETCH_TIMEOUT_MS", 30000)
	s.JwksFetchTimeoutMS = envInt("FEDERATION_JWKS_FETCH_TIMEOUT_MS", 5000)
	s.ExportEnabled = envBool("FEDERATION_EXPORT_ENABLED", false)
	s.ExportMaxTTLSeconds = envInt("FEDERATION_EXPORT_MAX_TTL_SECONDS", 86400)
	s.ExportSweepOnRevoke = envBool("FEDERATION_EXPORT_SWEEP_ON_REVOKE", true)
	if raw := os.Getenv("FEDERATION_S2S_PEER_URLS"); raw != "" {
		m := map[string]string{}
		if json.Unmarshal([]byte(raw), &m) == nil {
			s.S2SPeerURLs = m
		}
	}
	return s
}

// s2sPeerURLOverride — the local-dev transport map (nil in production;
// empty env = production wire — always https://<origin>/federation/s2s).
// Identity rules (iss/aud, https entity id) are UNCHANGED by this: it
// redirects the dial only, the S2SUROverride seam (03 §8 split).
func s2sPeerURLOverride() map[string]string {
	if m := loadSettings().S2SPeerURLs; len(m) > 0 {
		return m
	}
	return nil
}

// SSO env knobs (overleaf-fed plan/10 R1 + Phase 4), read where the
// S8/S9 Node-sync seams land (synthetic-email JIT domain, cert-expiry
// warn window).
func ssoCertExpiryWarnDays() int { return envInt("SSO_CERT_EXPIRY_WARN_DAYS", 30) }

func samlSyntheticEmailDomain() string { return os.Getenv("OVERLEAF_SAML_SYNTHETIC_EMAIL_DOMAIN") }
