package federation

// M4 + M5 (D-GOFED) close-outs.
//
//   M4 — VERIFY seam (D-G4): the S2S assertion wire is ours (overleaf claim
//        set, S3-pinned); the lib's own ClientAssertion verifies on our
//        S3-pinned VerifyJWT, and the lib's derived kid for the SAME pub key
//        equals our pinned kid (M1 three-way on the npm fixture).
//
//   M5 — OIDF §5.2.1 signed-JWKS (typ jwk-set+jwt) is the TA-side verify path
//        for PEER metadata's signed_jwks_uri: lib build → parse → structural
//        validation → verify against the pinned pub key. D-G8: our SERVED
//        historical-keys wire stays Node-shape {iss, iat, keys} (no sub — the
//        lib parser would reject it); the lib is used for TA-side verify only.

import (
	"encoding/json"
	"testing"
	"time"

	oidf "github.com/go-oidfed/lib"
	oidc_jwx "github.com/go-oidfed/lib/jwx"
	"github.com/lestrrat-go/jwx/v4/jwa"
)

func TestLibM4_S2sWireAndVerifySeam(t *testing.T) {
	oidf.DisableDebugLogging()

	// (a) Our hand-rolled S2S wire (D-G4: peer wire = contract): overleaf
	//     claim set, ES256, kid = RFC 8550 pinned, verified against the pub.
	pub, priv := keyFromFixture(t)
	assertion, err := signClientAssertion(pub, priv, "b.example.org", "https://b.example.org/federation/s2s", time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	kid, payload, err := VerifyJWT(pub, assertion)
	if err != nil {
		t.Fatalf("S2S verify: %v", err)
	}
	if kid != pub.Kid {
		t.Fatalf("S2S kid %q != pinned %q", kid, pub.Kid)
	}
	var claims struct {
		Iss string `json:"iss"`
		Sub string `json:"sub"`
		Aud string `json:"aud"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	want := "urn:overleaf-federation:client:b.example.org"
	if claims.Iss != want || claims.Sub != want {
		t.Fatalf("S2S iss/sub = %q/%q (overleaf wire)", claims.Iss, claims.Sub)
	}
	if claims.Aud != "https://b.example.org/federation/s2s" {
		t.Fatalf("S2S aud = %q", claims.Aud)
	}

	// (b) Lib's derived kid for the SAME key == our pinned kid (M1 three-way,
	//     npm fixture key). lib keyid = lestrrat AssignKeyID (RFC 8550).
	lsk, _, err := libES256Signer(priv)
	if err != nil {
		t.Fatal(err)
	}
	_, libKid, err := oidc_jwx.SignerToPublicJWK(lsk, jwa.ES256())
	if err != nil {
		t.Fatal(err)
	}
	if libKid != pub.Kid {
		t.Fatalf("lib kid %q != pub kid %q", libKid, pub.Kid)
	}

	// (c) The lib's own assertion verifies on our S3-pinned VerifyJWT (the
	//     verify direction is implementation-interchangeable given a pinned
	//     kid).
	rop := oidf.NewRequestObjectProducer(want,
		oidc_jwx.NewSingleKeyVersatileSigner(lsk, jwa.ES256()), time.Minute)
	la, err := rop.ClientAssertion("https://b.example.org/federation/s2s", "ES256")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := VerifyJWT(pub, string(la)); err != nil {
		t.Fatalf("lib-built assertion must verify on our S3-pinned VerifyJWT: %v", err)
	}
}

func TestLibM5_SignedJWKSTASideRoundTrip(t *testing.T) {
	oidf.DisableDebugLogging()

	pub, priv := keyFromFixture(t)
	lsk, _, err := libES256Signer(priv)
	if err != nil {
		t.Fatal(err)
	}

	// OIDF §5.2.1 payload (JWKS object; per SignedJWKS struct: Keys jwx.JWKS json:
	// "keys" where JWKS = struct{Set []Key `json:"keys"`} → wire = {"keys": {"keys": [...]}})
	payload, err := json.Marshal(map[string]any{
		"iss": "https://a.example.org",
		"sub": "https://a.example.org",
		"keys": map[string]any{"keys": []any{
			map[string]any{
				"kty": "EC", "crv": "P-256", "x": pub.X, "y": pub.Y,
				"kid": pub.Kid, "alg": "ES256", "use": "sig",
			},
		},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := oidc_jwx.SignWithType(payload, nil, "jwk-set+jwt", jwa.ES256(), lsk)
	if err != nil {
		t.Fatalf("lib SignWithType (typ jwk-set+jwt): %v", err)
	}

	sj, err := oidf.ParseSignedJWKS(token)
	if err != nil {
		t.Fatalf("lib ParseSignedJWKS (§5.2.1 structure): %v", err)
	}
	if sj.Issuer != "https://a.example.org" || sj.Subject != "https://a.example.org" {
		t.Fatalf("signedJWKS iss/sub = %q/%q", sj.Issuer, sj.Subject)
	}
	kids, ok := sj.KID()
	if !ok || kids != pub.Kid {
		t.Fatalf("signedJWKS kid header %q (want %q)", kids, pub.Kid)
	}
	pubSet, err := oidc_jwx.KeyToJWKS(lsk.Public(), jwa.ES256())
	if err != nil {
		t.Fatal(err)
	}
	if !sj.Verify(pubSet) {
		t.Fatalf("signedJWKS verify failed against the pinned pub key")
	}

	// D-G8 wire pin: the lib parser REQUIRES iss+sub+keys (§5.2.1); our SERVED
	// GET /federation/federation-keys wire is Node-shape {iss, iat, keys}
	// (no sub — see s3_test.go pins). The lib path above is for TA-side
	// verification of PEER metadata's §5.2.1 signed_jwks_uri responses.
}
