package sso

// zz_samlprobe_test.go — offline verification probe for the live ACS 401
// (A4 positive-leg). Runs crewjam ParseXMLResponse against the exact saved
// boxyhq/mock-saml SAMLResponse value with the exact seeded provider shape,
// printing the full error chain.

import (
	"encoding/base64"
	"net/url"
	"os"
	"strings"
	"testing"

	saml "github.com/crewjam/saml"
)

func TestZZ_SAMLProbe_ACSValue(t *testing.T) {
	raw, err := os.ReadFile("/tmp/samlresp.b64")
	if err != nil {
		t.Skipf("probe value not present: %v", err)
	}
	certPEM := os.Getenv("SAMLPROBE_CERT_PEM")
	if certPEM == "" {
		t.Skip("SAMLPROBE_CERT_PEM not set")
	}
	// mirror samlogin.go: base64-decode the form value first
	xmlBytes, decErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if decErr == nil && len(xmlBytes) > 0 && xmlBytes[0] == 0x78 {
		if z, zerr := nodeSamlInflate(xmlBytes); zerr == nil {
			xmlBytes = z
		}
	}
	if decErr != nil || len(xmlBytes) == 0 || xmlBytes[0] != '<' {
		t.Fatalf("form value is not base64(xml)/deflated: decErr=%v head=%x", decErr, raw[:4])
	}
	p := &SAMLProvider{
		ID:         "sso-saml-e2e",
		Enabled:    true,
		Issuer:     "https://saml.example.com/entityid",
		EntryPoint: "http://172.20.0.1:4101/api/saml/sso",
		IdpCert:    certPEM,
	}
	sp := buildSP(p, nil, "http://127.0.0.1:7420")

	reqID := os.Getenv("SAMLPROBE_REQID")
	poss := []string{}
	if reqID != "" {
		poss = append(poss, reqID)
	}
	acs, _ := url.Parse("http://127.0.0.1:7420/saml/login/callback")
	assertion, perr := sp.ParseXMLResponse(xmlBytes, poss, *acs)
	if ivr, ok := perr.(*saml.InvalidResponseError); ok {
		t.Fatalf("ParseXMLResponse failed: PrivateErr=%v\nresponse=%s", ivr.PrivateErr, ivr.Response)
	}
	if perr != nil {
		t.Fatalf("ParseXMLResponse failed: %v", perr)
	}
	t.Logf("VERIFY OK: subj=%s", assertion.Subject.NameID.Value)
}
