package federation

// M0 spike (D-GOFED) — prove go-oidfed/lib v0.11.x works inside OUR build
// before any production file is touched:
//
//  1. lib kid (lestrrat jwx AssignKeyID) == our RFC 8550 thumbprint
//  2. SingleKey VersatileSigner wired from our JWK private half (the seam
//     shape of oidcfed.libES256Signer)
//  3. S2S client assertion via lib NewRequestObjectProducer
//     (iss/sub/iat/exp/aud/jti), verified by our existing VerifyJWT
//  4. leaf EC build + parse + verify round-trip over the lib (no fiber in
//     the stack; EC typ = entity-statement+jwt; metadata openid_provider key)
//
// libES256Signer itself lives in oidcfed.go (production seam, M2 revision).

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	oidf "github.com/go-oidfed/lib"
	oidc_jwx "github.com/go-oidfed/lib/jwx"
	"github.com/lestrrat-go/jwx/v4/jwa"
)

func TestLibM0_Spike(t *testing.T) {
	oidf.DisableDebugLogging()

	pub, priv, err := GenerateES256()
	if err != nil {
		t.Fatal(err)
	}

	// 1. D-G1 preview: lib kid == our RFC 8550 thumbprint.
	// The lib derives the header kid from the handed key's PUBLIC half
	// (RFC 8550 lestrrat keyid, ignores any pinned kid field). M1 pins that
	// derived kid == our npm fixture kid three-way.
	lsk, _, err := libES256Signer(priv)
	if err != nil {
		t.Fatal(err)
	}
	_, libKid, err := oidc_jwx.SignerToPublicJWK(lsk, jwa.ES256())
	if err != nil {
		t.Fatalf("lib SignerToPublicJWK: %v", err)
	}
	ourKid, err := KeyThumbprint(pub)
	if err != nil {
		t.Fatal(err)
	}
	if libKid != ourKid {
		t.Fatalf("D-G1 PREVIEW FAIL: lib kid %q != our RFC 8550 %q", libKid, ourKid)
	}

	// 2. SingleKey signer (the Mongo-KMS seam shape)
	signer := oidc_jwx.NewSingleKeyVersatileSigner(lsk, jwa.ES256())
	if sk, _ := signer.DefaultSigner(); sk == nil {
		t.Fatal("signer: no default signer")
	}

	// 3. S2S client assertion via lib Request Object Producer
	rop := oidf.NewRequestObjectProducer("urn:overleaf-federation:client:peer.example", signer, time.Minute)
	assertion, err := rop.ClientAssertion("https://b.example/federation/s2s", "ES256")
	if err != nil {
		t.Fatal("lib ClientAssertion:", err)
	}
	kid, payload, err := VerifyJWT(pub, string(assertion))
	if err != nil {
		t.Fatalf("our VerifyJWT over lib-signed assertion: %v", err)
	}
	if kid != ourKid {
		t.Fatalf("assertion kid %q != %q", kid, ourKid)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal("claim payload not json:", err)
	}
	if claims["iss"] != "urn:overleaf-federation:client:peer.example" {
		t.Fatalf("iss claim = %v", claims["iss"])
	}
	if claims["sub"] != claims["iss"] {
		t.Fatalf("sub claim = %v (lib sets sub=iss)", claims["sub"])
	}
	if claims["aud"] != "https://b.example/federation/s2s" {
		t.Fatalf("aud claim = %v", claims["aud"])
	}
	if claims["jti"] == nil || claims["iat"] == nil || claims["exp"] == nil {
		t.Fatalf("assertion claims incomplete: %s", payload)
	}

	// 4. Leaf EC: build, parse, verify over the lib.
	ep, err := oidcEndpointsGo("https://a.example")
	if err != nil {
		t.Fatal(err)
	}
	meta := &oidf.Metadata{
		OpenIDProvider: &oidf.OpenIDProviderMetadata{
			Issuer:                 ep.Issuer,
			AuthorizationEndpoint:  ep.Authorization,
			TokenEndpoint:          ep.Token,
			ResponseTypesSupported: []string{"code"},
			SubjectTypesSupported:  []string{"public"},
		},
	}
	ec, err := oidf.NewFederationEntity(
		"https://a.example", nil, nil, meta,
		oidc_jwx.NewEntityStatementSigner(signer), time.Hour*24, nil,
	)
	if err != nil {
		t.Fatal("lib NewFederationEntity:", err)
	}
	ecJWT, err := ec.EntityConfigurationJWT()
	if err != nil {
		t.Fatal("lib EntityConfigurationJWT:", err)
	}
	ecSt, err := oidf.ParseEntityStatement(ecJWT)
	if err != nil {
		t.Fatal("lib ParseEntityStatement:", err)
	}
	ecKeys, err := oidc_jwx.KeyToJWKS(lsk.Public(), jwa.ES256())
	if err != nil {
		t.Fatal(err)
	}
	if !ecSt.Verify(ecKeys) {
		t.Fatal("lib EC verify failed (round trip)")
	}
	if ecSt.Issuer != "https://a.example" || ecSt.Subject != "https://a.example" {
		t.Fatalf("EC iss/sub = %q/%q", ecSt.Issuer, ecSt.Subject)
	}
	if ecSt.Metadata.OpenIDProvider.Issuer != ep.Issuer {
		t.Fatalf("EC metadata openid_provider.issuer = %q", ecSt.Metadata.OpenIDProvider.Issuer)
	}
	subj, _ := json.Marshal(ecSt.Metadata)
	if !strings.Contains(string(subj), "openid_provider") {
		t.Fatalf("EC metadata missing openid_provider key: %s", subj)
	}
	_ = signer
}
