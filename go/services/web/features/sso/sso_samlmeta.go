package sso

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// SAML idp metadata — extracted with a namespace-agnostic XML token scanner
// (not a rigid struct) so it finds the entityID, SSO/SLO locations and every
// X509Certificate regardless of the IdP's exact KeyInfo/X509Data nesting or
// prefixes (md:*, ds:*). Powers the POST /admin/sso/test SAML probe with a REAL
// parse (not just "contains <EntityDescriptor>") and surfaces the signing cert
// for import into the SP (6c, feeds the 6e cert-expiry classifier).

// idpMetaResult — the fields the probe reports back to the admin.
type idpMetaResult struct {
	EntityID  string   `json:"entityID"`
	SSO       string   `json:"sso"`
	SLO       string   `json:"slo,omitempty"`
	CertPEMs  []string `json:"certificates,omitempty"`
	CertCount int      `json:"certCount"`
}

func firstAttr(attrs []xml.Attr, local string) string {
	for _, a := range attrs {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// parseSAMLIdPMetadata extracts the entityID, first SSO/SLO bindings and all
// X509 certificates from an IdP metadata document.
func parseSAMLIdPMetadata(data []byte) (*idpMetaResult, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	out := &idpMetaResult{}
	foundEntity := false
	inCert := false
	certText := ""
	for {
		tok, derr := dec.Token()
		if derr == io.EOF {
			break
		}
		if derr != nil {
			return nil, fmt.Errorf("saml metadata: xml: %w", derr)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "EntityDescriptor":
				if id := firstAttr(t.Attr, "entityID"); id != "" {
					out.EntityID = id
					foundEntity = true
				}
			case "SingleSignOnService":
				if out.SSO == "" {
					out.SSO = firstAttr(t.Attr, "Location")
				}
			case "SingleLogoutService":
				if out.SLO == "" {
					out.SLO = firstAttr(t.Attr, "Location")
				}
			case "X509Certificate":
				inCert, certText = true, ""
			}
		case xml.CharData:
			if inCert {
				certText += string(t)
			}
		case xml.EndElement:
			if inCert && t.Name.Local == "X509Certificate" {
				// Strip all whitespace (multi-line base64) into one blob.
				c := strings.Join(strings.Fields(certText), "")
				if c != "" {
					out.CertPEMs = append(out.CertPEMs, c)
				}
				inCert = false
				certText = ""
			}
		}
	}
	if !foundEntity || out.EntityID == "" {
		return nil, fmt.Errorf("saml metadata: no entityID on EntityDescriptor")
	}
	out.CertCount = len(out.CertPEMs)
	return out, nil
}
