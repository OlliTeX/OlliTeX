package federation

// oidcfed.go — D-GOFED adapter (go-oidfed/lib v0.11.x, OIDF Final 1.0).
//
// Per MIGRATION-GOFED.md M0–M3 (2026-09-26): the lib supplies the OIDF
// ARTIFACT LAYER (kid = RFC 8550 thumbprint per M1, EC signing per M3,
// historical JWKS §5.2.1 per M5) over our Node-oracle keystore (D-G7).
// The S2S assertion BUILD stays hand-rolled (D-G4: peer wire is the
// contract). S4 OP crypto (v9 port) is untouched.

import (
	"crypto/ecdsa"

	oidc_jwx "github.com/go-oidfed/lib/jwx"
	"github.com/go-oidfed/lib/oidfedconst"
	"github.com/lestrrat-go/jwx/v4/jwa"
)

// libES256Signer — our keystore's private JWK (the `d` field) → the lib's
// `SigningKey` (a *ecdsa.PrivateKey natively satisfies it). D-G7 seam: our
// Mongo-backed keystore stays the source of truth; the lib only signs with
// the handed-over key.
func libES256Signer(priv *JWK) (oidc_jwx.SigningKey, *ecdsa.PrivateKey, error) {
	pk, err := jwkPrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}
	return pk, pk, nil
}

// libEntityStatementSign — the M3 EC signing path: raw payload bytes → lib
// Compact JWS with typ = oidfedconst "entity-statement+jwt" and the lib's
// RFC 8550 kid (M1 three-way proven equal to our npm fixture kid). The
// payload bytes are EXACTLY our Node-leaf wire (our marshal, our key order);
// the lib only produces the signature + protected header (kid, typ, ES256).
func libEntityStatementSign(payload []byte, priv *JWK) (string, error) {
	lsk, _, err := libES256Signer(priv)
	if err != nil {
		return "", err
	}
	jwt, err := oidc_jwx.SignWithType(payload, nil, oidfedconst.JWTTypeEntityStatement, jwa.ES256(), lsk)
	if err != nil {
		return "", err
	}
	return string(jwt), nil
}
