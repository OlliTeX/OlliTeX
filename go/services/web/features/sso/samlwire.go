package sso

// samlwire.go — SAML SP plumbing on github.com/crewjam/saml v0.5.1
// (Go's standard SAML SP — the Go-side equivalent of the Node
// @node-saml stack; the wire oracle stays the Node one).
//
// Oracle pins honored / adapted:
//
//   - crewjam.ParseXMLResponse enforces the node-saml shape: response
//     signed OR (when the response is unsigned) the assertion signed —
//     Node wantAuthnResponseSigned=true + wantAssertionsSigned=true
//     allow the same two shapes; digest sha1/256/512 + RSA PKCS1v1_5
//     (+ECDSA) via goxmldsig; exc-c14n; EncryptedAssertion
//     xml-encryption decryption with the decryption private key
//     (sp.Key doubles as the decrypt key in crewjam); Issuer vs IdP
//     entityID; Destination vs ACS URL; StatusCode Success;
//     IssueInstant freshness; Conditions NotBefore/NotOnOrAfter with
//     saml.MaxClockSkew (pinned to Node acceptedClockSkewMs when set).
//   - Node validateInResponseTo (default 'never') ⇒ sp.ValidateRequestID
//     / AllowIDPInitiated mapping in buildSP.
//   - Node audience (default: no check) ⇒ sp.ValidateAudienceRestriction
//     override only when the provider sets audience.

import (
	"bytes"
	"compress/zlib"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	saml "github.com/crewjam/saml"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// newZlibReader — zlib (0x78 header) inflate (Node compress heuristic).
func newZlibReader(b []byte) (io.ReadCloser, error) {
	return zlib.NewReader(bytes.NewReader(b))
}

var errAudience = errors.New("saml: audience restriction not satisfied")

// ---- env fallback providers (Node env mode) ----

// envSAMLProvider — synthetic 'saml' provider from OVERLEAF_SAML_*
// (Node env mode: EXTERNAL_AUTH includes 'saml', providerId '1').
func envSAMLProvider() *SAMLProvider {
	if !strings.Contains(os.Getenv("EXTERNAL_AUTH"), "saml") {
		return nil
	}
	e := os.Getenv
	p := &SAMLProvider{
		ID:                       "1",
		Type:                     "saml",
		Issuer:                   e("OVERLEAF_SAML_ISSUER"),
		EntryPoint:               e("OVERLEAF_SAML_ENTRYPOINT"),
		Audience:                 e("OVERLEAF_SAML_AUDIENCE"),
		IdpCert:                  e("OVERLEAF_SAML_IDP_CERT"),
		PrivateKey:               e("OVERLEAF_SAML_PRIVATE_KEY"),
		PublicCert:               e("OVERLEAF_SAML_PUBLIC_CERT"),
		DecryptionPvk:            e("OVERLEAF_SAML_DECRYPTION_PVK"),
		ForceAuthn:               e("OVERLEAF_SAML_FORCE_AUTHN") == "true",
		IsPassive:                e("OVERLEAF_SAML_IS_PASSIVE") == "true",
		IdentifierFormat:         e("OVERLEAF_SAML_IDENTIFIER_FORMAT"),
		AuthnContext:             e("OVERLEAF_SAML_AUTHN_CONTEXT"),
		LogoutURL:                e("OVERLEAF_SAML_LOGOUT_URL"),
		AttributeConsumingSvcIdx: e("OVERLEAF_SAML_ATTRIBUTE_CONSUMING_SERVICE_INDEX"),
		AuthnRequestBinding:      e("OVERLEAF_SAML_AUTHN_REQUEST_BINDING"),
		AttUserID:                e("OVERLEAF_SAML_ATT_USER_ID"),
		AttEmail:                 e("OVERLEAF_SAML_ATT_EMAIL"),
		AttFirstName:             e("OVERLEAF_SAML_ATT_FIRST_NAME"),
		AttLastName:              e("OVERLEAF_SAML_ATT_LAST_NAME"),
		AttAdmin:                 e("OVERLEAF_SAML_ATT_IS_ADMIN"),
		ValAdmin:                 e("OVERLEAF_SAML_VAL_IS_ADMIN"),
		UpdateUserDetails:        e("OVERLEAF_SAML_UPDATE_USER_DETAILS_ON_LOGIN") == "true",
		ValidateInResponseTo:     e("OVERLEAF_SAML_VALIDATE_IN_RESPONSE_TO"),
	}
	n, _ := strconvI64(e("OVERLEAF_SAML_ACCEPTED_CLOCK_SKEW_MS"))
	if n >= 0 {
		p.AcceptedClockSkewMs = n
	}
	w := e("OVERLEAF_SAML_WANT_ASSERTIONS_SIGNED")
	p.WantAssertionsSigned = w != "false"
	w = e("OVERLEAF_SAML_WANT_AUTHN_RESPONSE_SIGNED")
	p.WantAuthnResponseSigned = w != "false"
	return p
}

// envOIDCProvider — synthetic 'oidc' provider from OVERLEAF_OIDC_*.
func envOIDCProvider() *OIDCProvider {
	if !strings.Contains(os.Getenv("EXTERNAL_AUTH"), "oidc") {
		return nil
	}
	e := os.Getenv
	p := &OIDCProvider{
		ID:               "1",
		Type:             "oidc",
		Issuer:           e("OVERLEAF_OIDC_ISSUER"),
		ClientID:         e("OVERLEAF_OIDC_CLIENT_ID"),
		ClientSecret:     e("OVERLEAF_OIDC_CLIENT_SECRET"),
		AuthorizationURL: e("OVERLEAF_OIDC_AUTHORIZATION_URL"),
		TokenURL:         e("OVERLEAF_OIDC_TOKEN_URL"),
		UserInfoURL:      e("OVERLEAF_OIDC_USER_INFO_URL"),
		LogoutURL:        e("OVERLEAF_OIDC_LOGOUT_URL"),
		Scope:            e("OVERLEAF_OIDC_SCOPE"),
	}
	if p.Scope == "" {
		p.Scope = "openid profile email"
	}
	return p
}

func strconvI64(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	sign := int64(1)
	i := 0
	if s[0] == '-' || s[0] == '+' {
		if s[0] == '-' {
			sign = -1
		}
		i = 1
	}
	n := int64(0)
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		n = n*10 + int64(s[i]-'0')
	}
	return n * sign, true
}

// resolveSAMLProvider — bare / "saml" ⇒ first enabled DB row, else env
// synthetic; DB id ⇒ that row.
func resolveSAMLProvider(cfg *SSOConfig, pathID string) (*SAMLProvider, bool) {
	if pathID != "" && pathID != "saml" {
		if p := samlProviderByID(cfg, pathID); p != nil {
			return p, true
		}
		return nil, false
	}
	if cfg != nil {
		for i := range cfg.Providers {
			var p SAMLProvider
			if uerr := bson.Unmarshal(cfg.Providers[i], &p); uerr != nil {
				continue
			}
			if p.Type == "saml" && p.Enabled {
				out := p
				return &out, true
			}
		}
	}
	if p := envSAMLProvider(); p != nil {
		return p, true
	}
	return nil, false
}

func resolveOIDCProvider(cfg *SSOConfig, pathID string) (*OIDCProvider, bool) {
	if pathID != "" && pathID != "oidc" {
		if p := oidcProviderByID(cfg, pathID); p != nil {
			return p, true
		}
		return nil, false
	}
	if cfg != nil {
		for i := range cfg.Providers {
			var p OIDCProvider
			if uerr := bson.Unmarshal(cfg.Providers[i], &p); uerr != nil {
				continue
			}
			if p.Type == "oidc" && p.Enabled {
				out := p
				return &out, true
			}
		}
	}
	if p := envOIDCProvider(); p != nil {
		return p, true
	}
	return nil, false
}

// ---- SP construction (crewjam) ----

func spEntityID(p *SAMLProvider, spCfg *SPConfig, siteURL string) string {
	if spCfg != nil && spCfg.EntityID != "" {
		return spCfg.EntityID
	}
	if p.Issuer != "" {
		return p.Issuer
	}
	return strings.TrimRight(siteURL, "/") + "/saml/login/callback"
}

// buildSP — crewjam ServiceProvider (Node option mapping).
func buildSP(p *SAMLProvider, spCfg *SPConfig, siteURL string) *saml.ServiceProvider {
	acs := mustURL(strings.TrimRight(siteURL, "/") + "/saml/login/callback")
	sp := &saml.ServiceProvider{
		EntityID:          spEntityID(p, spCfg, siteURL),
		AcsURL:            acs,
		MetadataURL:       mustURL(strings.TrimRight(siteURL, "/") + "/saml/meta"),
		AuthnNameIDFormat: nameIDFormat(p.IdentifierFormat),
		ForceAuthn:        boolPtr(p.ForceAuthn),
	}
	if p.SignatureAlgorithm != "" {
		sp.SignatureMethod = p.SignatureAlgorithm
	}
	// audience (Node: skip the check entirely when unset).
	if p.Audience != "" {
		want := p.Audience
		sp.ValidateAudienceRestriction = func(assertion *saml.Assertion) error {
			if len(assertion.Conditions.AudienceRestrictions) == 0 {
				return nil
			}
			for _, ar := range assertion.Conditions.AudienceRestrictions {
				if ar.Audience.Value == want {
					return nil
				}
			}
			return errAudience
		}
	}
	// Node validateInResponseTo (default 'never' ⇒ IDP-initiated OK,
	// InResponseTo unchecked).
	switch p.ValidateInResponseTo {
	case "ifPresent", "always":
		sp.ValidateRequestID = func(response saml.Response, possibleRequestIDs []string) error {
			if response.InResponseTo == "" && p.ValidateInResponseTo == "ifPresent" {
				return nil
			}
			for _, id := range possibleRequestIDs {
				if response.InResponseTo == id {
					return nil
				}
			}
			return errors.New("saml: InResponseTo mismatch")
		}
	default: // 'never' / unset — allow IDP-initiated, no request-id check
		sp.AllowIDPInitiated = true
	}
	// Node acceptedClockSkewMs (default 0).
	if p.AcceptedClockSkewMs >= 0 {
		saml.MaxClockSkew = time.Duration(p.AcceptedClockSkewMs) * time.Millisecond
	}
	// IdP signing cert.
	certPEM := p.IdpCert
	if certPEM == "" && spCfg != nil {
		certPEM = spCfg.PublicCert
	}
	if certPEM != "" {
		sp.IDPCertificate = &certPEM
	}
	// Decryption (EncryptedAssertion): crewjam uses sp.Key for both
	// request signing and xml-enc decryption.
	if pk := firstRSAKey(p.DecryptionPvk, p.PrivateKey); pk != nil {
		sp.Key = pk
	}
	return sp
}

func nameIDFormat(f string) saml.NameIDFormat {
	if f == "" {
		return saml.EmailAddressNameIDFormat
	}
	return saml.NameIDFormat(f)
}

func boolPtr(b bool) *bool { return &b }

func mustURL(s string) url.URL {
	u, _ := url.Parse(s)
	return *u
}

func firstRSAKey(cands ...string) *rsa.PrivateKey {
	for _, c := range cands {
		if pk := parseRSAPrivateKeyPEM(c); pk != nil {
			return pk
		}
	}
	return nil
}

// newSamlID — Node: '_' + crypto.randomBytes(20).toString('hex').
func newSamlID() string {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	return "_" + hex.EncodeToString(b)
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

func parseRSAPrivateKeyPEM(pemString string) *rsa.PrivateKey {
	block, _ := pem.Decode([]byte(pemString))
	if block == nil {
		return nil
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rk, ok := k.(*rsa.PrivateKey); ok {
			return rk
		}
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k
	}
	return nil
}
