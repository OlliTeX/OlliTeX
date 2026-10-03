package sso

// slo.go — single-logout URL builders (Node module logout dispatch):
//
//   - SAML: Node passport-saml strategy.logout:
//     {logoutUrl}?SAMLRequest=b64(deflateRaw(LogoutRequest)) — Node
//     builds a LogoutRequest (Issuer + NameID); the IdP-initiated tail
//     lands on GET /saml/logout/callback.
//   - OIDC: OIDCAuthenticationController.passportLogout:
//     {logoutURL}?id_token_hint=<session idToken>&
//     post_logout_redirect_uri=<encodeURIComponent(siteUrl)>

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"encoding/base64"
	"io"
	"net/url"
	"strings"
	"time"

	"ollitex/go/services/web/core"
)

// b64enc — standard base64.
func b64enc(d []byte) string { return base64.StdEncoding.EncodeToString(d) }

// deflateRaw — Node zlib.deflateRawSync (raw DEFLATE, no zlib
// header). A zlib frame is exactly [0x78 0x9C][raw-DEFLATE][adler32]
// at window 15, so we deflate through the zlib writer and strip the
// 6 framing bytes — byte-identical to a raw DEFLATE stream.
func deflateRaw(in []byte) []byte {
	framed := &appendSink{}
	w := zlib.NewWriter(framed)
	_, _ = w.Write(in)
	_ = w.Close()
	b := framed.b
	if len(b) < 6 {
		return b
	}
	return b[2 : len(b)-4]
}

// appendSink — io.Writer appending to a slice (no allocation per Write).
// appendSink — io.Writer appending to a slice (no allocation per Write).
type appendSink struct {
	b []byte
}

func (a *appendSink) Write(p []byte) (int, error) {
	a.b = append(a.b, p...)
	return len(p), nil
}

// newFlateReaderRaw — raw-DEFLATE reader (compress/flate) for the Node
// decompress heuristic's raw path.
func newFlateReaderRaw(b []byte) (io.Reader, error) {
	return flate.NewReader(bytes.NewReader(b)), nil
}

// samlNowISO — SAML 2.0 instant (2006-01-02T15:04:05.000Z).
func samlNowISO() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

// cfgSPOf — SP metadata config from the loaded ssoConfigs doc.
func cfgSPOf(a *core.App, cxt *core.Cxt) *SPConfig {
	if db, err := ssoDB(a, cxt); err == nil {
		if cfg := loadSSOConfig(db, cxt); cfg != nil {
			return cfg.SPMetadata
		}
	}
	return nil
}

// lookupSamlProviderForSLO — provider for the logout hook.
func lookupSamlProviderForSLO(a *core.App, cxt *core.Cxt, providerID string) (*SAMLProvider, bool) {
	var cfg *SSOConfig
	if db, err := ssoDB(a, cxt); err == nil {
		cfg = loadSSOConfig(db, cxt)
	}
	p, ok := resolveSAMLProvider(cfg, providerID)
	if !ok && providerID != "" {
		p, ok = resolveSAMLProvider(cfg, "")
	}
	if !ok {
		// CE legacy: the Manage-Site "sso-saml" section (default id only).
		if providerID == "" || providerID == "saml" {
			if p2 := samlFromSiteSettings(a, cxt.Req.Context()); p2 != nil {
				p, ok = p2, true
			}
		}
	}
	return p, ok
}

// samlSLOURL — "" when SLO is not configured.
func samlSLOURL(a *core.App, cxt *core.Cxt, providerID string) string {
	p, ok := lookupSamlProviderForSLO(a, cxt, providerID)
	if !ok {
		return ""
	}
	nameID := ""
	if cxt.Sess != nil {
		if raw, ok := cxt.Sess.GetRaw("samlExtce"); ok && len(raw) > 0 {
			var ext struct {
				NameID string `json:"nameID"`
			}
			_ = jsonUnmarshal(raw, &ext)
			nameID = ext.NameID
		}
	}
	return samlSLOURLRaw(p, spEntityID(p, cfgSPOf(a, cxt), cxt.SiteURL), nameID)
}

// samlSLOURLRaw — pure builder (Node strategy.logout parity):
// {logoutUrl}[?]SAMLRequest=b64(deflateRaw(LogoutRequest w/ NameID)).
func samlSLOURLRaw(p *SAMLProvider, issuer, nameID string) string {
	if p.LogoutURL == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<samlp:LogoutRequest xmlns="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion"`)
	b.WriteString(` ID="` + newSamlID() + `" Version="2.0" IssueInstant="` + samlNowISO() + `"`)
	b.WriteString(`><saml:Issuer>` + xmlEscape(issuer) + `</saml:Issuer>`)
	if nameID != "" {
		b.WriteString(`<samlp:NameID>` + xmlEscape(nameID) + `</samlp:NameID>`)
	}
	b.WriteString(`</samlp:LogoutRequest>`)
	req := b64enc(deflateRaw([]byte(b.String())))
	sep := "?"
	if strings.Contains(p.LogoutURL, "?") {
		sep = "&"
	}
	return p.LogoutURL + sep + "SAMLRequest=" + url.QueryEscape(req)
}

// oidcSLOURL — Node passportLogout parity.
func oidcSLOURL(a *core.App, cxt *core.Cxt, providerID string) string {
	var cfg *SSOConfig
	if db, err := ssoDB(a, cxt); err == nil {
		cfg = loadSSOConfig(db, cxt)
	}
	p, ok := resolveOIDCProvider(cfg, providerID)
	if !ok {
		return ""
	}
	idToken := ""
	if cxt.Sess != nil {
		if raw, ok := cxt.Sess.GetRaw("idToken"); ok && len(raw) >= 2 {
			var ss string
			_ = jsonUnmarshal(raw, &ss)
			idToken = ss
		}
	}
	return oidcSLOURLRaw(p, strings.TrimRight(cxt.SiteURL, "/"), idToken)
}

// oidcSLOURLRaw — pure builder (Node OIDCAuthenticationController.
// passportLogout parity).
func oidcSLOURLRaw(p *OIDCProvider, siteURL, idToken string) string {
	if p.LogoutURL == "" {
		return ""
	}
	return p.LogoutURL + "?id_token_hint=" + url.QueryEscape(idToken) +
		"&post_logout_redirect_uri=" + url.QueryEscape(siteURL)
}
