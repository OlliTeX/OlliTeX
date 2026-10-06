package sso

import (
	"encoding/base64"
	"strings"
	"testing"
)

// TestIDPCertBase64_ — crewjam v0.5.1 IDPCertificate format contract
// (raw base64 DER, no PEM): PEms and raw base64 inputs must both normalize
// to decodable base64; garbage must yield "".
func TestIDPCertBase64(t *testing.T) {
	der := []byte{0x30, 0x82, 0x01, 0x00, 0x01}
	derB64 := base64.StdEncoding.EncodeToString(der)
	pem := "-----BEGIN CERTIFICATE-----\n" + strings.Replace(derB64, "AAAA", "BBBB", 1) + "\n-----END CERTIFICATE-----\n"
	// build a *real*-shaped pem body of 64-col wrapped base64 with a known
	// DER-ish payload (value only needs to base64-decode)
	der2 := make([]byte, 20)
	der2[0] = 0x30
	b64 := base64.StdEncoding.EncodeToString(der2)
	pem2 := "-----BEGIN CERTIFICATE-----\n" + b64 + "\n-----END CERTIFICATE-----\n"

	// PEM input -> base64 of the same DER
	if got := idpCertBase64(pem2); got != b64 {
		t.Fatalf("PEM in: got %q want %q", got, b64)
	}
	// raw base64 input (with newlines, as in provider configs) -> unchanged
	if got := idpCertBase64(b64[:20] + "\n" + b64[20:]); got != b64 {
		t.Fatalf("raw b64 in: got %q want %q", got, b64)
	}
	// empty -> empty
	if got := idpCertBase64(""); got != "" {
		t.Fatalf("empty in: got %q", got)
	}
	// garbage pem-typed but undecodable -> "" (parseCert would reject it anyway)
	if got := idpCertBase64("-----BEGIN CERTIFICATE-----\n!!!!\n-----END CERTIFICATE-----"); got != "" {
		t.Fatalf("garbage: got %q", got)
	}
	_ = pem
	_ = der
	_ = derB64
}
