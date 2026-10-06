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
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
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

	"ollitex/go/services/web/core"
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
	// acceptedClockSkewMs — Node stores a STRING ("" = unset); keep the raw
	// value and parse at the point of use (saml.MaxClockSkew below).
	if s := e("OVERLEAF_SAML_ACCEPTED_CLOCK_SKEW_MS"); s != "" {
		p.AcceptedClockSkewMs = s
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
		Name:             e("OVERLEAF_OIDC_IDENTITY_SERVICE_NAME"),
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
// samlFromSiteSettings — CE legacy (Node samlLogin reads the Manage-Site
// "sso-saml" siteSettings section when the SSO-framework provider list has
// no match). The e2e SAML seed + operator /hub SSO section write THAT store;
// without this fallback the SAML login/ACS/meta surface 404s
// ("provider not found or disabled") right after a successful section PUT.
func samlFromSiteSettings(a *core.App, c context.Context) *SAMLProvider {
	if a == nil || a.Mongo == nil {
		return nil
	}
	db, err := a.Mongo.DB(c)
	if err != nil {
		return nil
	}
	var global struct {
		SsoSaml *struct {
			Enabled      bool   `bson:"enabled"`
			Identity     string `bson:"identityServiceName"`
			Issuer       string `bson:"issuer"`
			EntryPoint   string `bson:"entryPoint"`
			Audience     string `bson:"audience"`
			IdpCert      string `bson:"idpCert"`
			PrivateKey   string `bson:"privateKey"`
			DecryptionPv string `bson:"decryptionPvk"`
		} `bson:"sso-saml"`
	}
	if err := db.Collection("site_settings").FindOne(c, bson.D{{Key: "_id", Value: "global"}}).Decode(&global); err != nil {
		return nil
	}
	sec := global.SsoSaml
	if sec == nil || !sec.Enabled || sec.EntryPoint == "" {
		return nil
	}
	return &SAMLProvider{
		ID:                  "saml",
		Type:                "saml",
		Enabled:             true,
		IdentityServiceName: sec.Identity,
		Issuer:              sec.Issuer,
		EntryPoint:          sec.EntryPoint,
		Audience:            sec.Audience,
		IdpCert:             sec.IdpCert,
		PrivateKey:          sec.PrivateKey,
		DecryptionPvk:       sec.DecryptionPv,
	}
}

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
		// Env mode (EXTERNAL_AUTH contains 'oidc'): the login-slot button
		// links /oidc/login/<envID> even when the ssoConfigs table is empty
		// (Node parity: env providers carry synthetic ids). Without this
		// fallback the env-mode button 404s and the callback (providerID
		// from the session) would too.
		if p := envOIDCProvider(); p != nil {
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
	// IdP SSO binding location: crewjam resolves the SSO service through
	// sp.IDPMetadata (nil ⇒ runtime panic in Make*AuthenticationRequest AND
	// in GetSSOBindingLocation itself — live 500 found in the 2026-10-06 SSO
	// positive-leg E2E). The provider entryPoint IS the SSO service location;
	// expose it for both bindings (the boxyhq/soluto-class IdPs serve the same
	// endpoint on both). A nil IDPMetadata is never exposed.
	md := &saml.EntityDescriptor{EntityID: p.Issuer}
	if p.EntryPoint != "" {
		ep := mustURL(p.EntryPoint)
		md.IDPSSODescriptors = []saml.IDPSSODescriptor{{
			SingleSignOnServices: []saml.Endpoint{
				{Binding: saml.HTTPRedirectBinding, Location: ep.String()},
				{Binding: saml.HTTPPostBinding, Location: ep.String()},
			},
		}}
	}
	sp.IDPMetadata = md
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
	if s := strings.TrimSpace(p.AcceptedClockSkewMs); s != "" {
		if n, ok := strconvI64(s); ok && n >= 0 {
			saml.MaxClockSkew = time.Duration(n) * time.Millisecond
		}
	}
	// IdP signing cert. crewjam v0.5.1 IDPCertificate is RAW BASE64 (DER),
	// not PEM: parseCert base64-decodes it after stripping whitespace — a
	// PEM header lands at byte 0 and the decode fails ("illegal base64 data
	// at input byte 0" ⇒ ACS 401 on every signed response, found live in the
	// 2026-10-06 SAML positive-leg E2E). Accept both PEM and raw base64 in
	// the provider config and normalize to base64 here.
	certPEM := p.IdpCert
	if certPEM == "" && spCfg != nil {
		certPEM = spCfg.PublicCert
	}
	if idpCert := idpCertBase64(certPEM); idpCert != "" {
		sp.IDPCertificate = &idpCert
	}
	// Decryption (EncryptedAssertion): crewjam uses sp.Key for both
	// request signing and xml-enc decryption.
	if pk := firstRSAKey(p.DecryptionPvk, p.PrivateKey); pk != nil {
		sp.Key = pk
	}
	return sp
}

// idpCertBase64 — normalize a provider IdP signing cert to the form crewjam
// v0.5.1 expects in SP.IDPCertificate: raw base64 DER (no PEM headers).
// Input may be PEM (-----BEGIN CERTIFICATE-----) or already-raw base64.
// Returns "" when nothing usable is found.
func idpCertBase64(cert string) string {
	c := strings.TrimSpace(cert)
	if c == "" {
		return ""
	}
	if block, _ := pem.Decode([]byte(c)); block != nil && block.Type == "CERTIFICATE" {
		return base64.StdEncoding.EncodeToString(block.Bytes)
	}
	// assume raw base64 (strip any whitespace/headers defensively)
	b64 := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, c)
	if _, err := base64.StdEncoding.DecodeString(b64); err == nil {
		return b64
	}
	return ""
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
