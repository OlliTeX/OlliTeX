package sso

import "testing"

func TestParseSAMLIdPMetadata(t *testing.T) {
	const xml = `<?xml version="1.0"?>
<md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata"
    entityID="https://idp.example/saml/idp">
  <md:IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <md:SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect"
        Location="https://idp.example/saml/idp/SSOService"/>
    <md:SingleLogoutService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect"
        Location="https://idp.example/saml/idp/SingleLogoutService"/>
    <md:KeyDescriptor use="signing"><md:KeyInfo>
      <ds:X509Data xmlns:ds="http://www.w3.org/2000/09/xmldsig#">
        <ds:X509Certificate>MIIBVjCB
AQIBADANCERTEND</ds:X509Certificate>
      </ds:X509Data>
    </md:KeyInfo></md:KeyDescriptor>
  </md:IDPSSODescriptor>
</md:EntityDescriptor>`
	meta, err := parseSAMLIdPMetadata([]byte(xml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if meta.EntityID != "https://idp.example/saml/idp" {
		t.Fatalf("entityID = %q", meta.EntityID)
	}
	if meta.SSO != "https://idp.example/saml/idp/SSOService" {
		t.Fatalf("sso = %q", meta.SSO)
	}
	if meta.SLO != "https://idp.example/saml/idp/SingleLogoutService" {
		t.Fatalf("slo = %q", meta.SLO)
	}
	// The multi-line certificate must be joined into a single base64 blob.
	if meta.CertCount != 1 || meta.CertPEMs[0] != "MIIBVjCBAQIBADANCERTEND" {
		t.Fatalf("certs = %v", meta.CertPEMs)
	}
}

func TestParseSAMLIdPMetadata_errors(t *testing.T) {
	cases := []string{
		`<foo/>`, // not an EntityDescriptor
		`<md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata"/>`, // no entityID attr
		`not xml at all`, // malformed
	}
	for _, in := range cases {
		if _, err := parseSAMLIdPMetadata([]byte(in)); err == nil {
			t.Fatalf("expected parse error for input %q", in)
		}
	}
}
