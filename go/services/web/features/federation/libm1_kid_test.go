package federation

// M1 (D-GOFED) — D-G1 closure: the lib-derived kid (lestrrat jwx v4
// RFC 8550 thumbprint) equals byte-for-byte both (a) our own RFC 8550
// recomputation and (b) the pinned npm @oidfed/core v1.0.0 fixture kid
// (testdata/key.json, kid "A2P6TlLJVZBG-wa9pxzHYAtoS-hO6AN7-4eIuK9EHnQ").
// D-G1 was previewed in M0 on a Go-generated key; M1 pins on the npm
// cross-implementation fixture (the one we are interop-pinned to).

import (
	"encoding/json"
	"os"
	"testing"

	oidc_jwx "github.com/go-oidfed/lib/jwx"
	"github.com/lestrrat-go/jwx/v4/jwa"
)

func TestLibM1_KidPinnedOnNpmFixture(t *testing.T) {
	b, err := os.ReadFile("testdata/key.json")
	if err != nil {
		t.Skip("no fixtures")
	}
	var raw struct {
		Pub  JWK `json:"pub"`
		Priv JWK `json:"priv"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("fixture key.json: %v", err)
	}

	// (a) lib kid over the npm fixture private key
	lsk, _, err := libES256Signer(&raw.Priv)
	if err != nil {
		t.Fatalf("fixture priv JWK → lib SigningKey: %v", err)
	}
	_, libKid, err := oidc_jwx.SignerToPublicJWK(lsk, jwa.ES256())
	if err != nil {
		t.Fatal(err)
	}
	if libKid != raw.Pub.Kid {
		t.Fatalf("D-G1 FAIL: lib kid %q != npm fixture kid %q", libKid, raw.Pub.Kid)
	}
	if libKid != raw.Priv.Kid {
		t.Fatalf("D-G1 FAIL: priv kid %q", raw.Priv.Kid)
	}

	// (b) and == our own recomputation (self-consistency on the fixture)
	ourKid, err := KeyThumbprint(&raw.Pub)
	if err != nil {
		t.Fatal(err)
	}
	if ourKid != raw.Pub.Kid || ourKid != libKid {
		t.Fatalf("three-way kid mismatch: npm=%q lib=%q ours=%q", raw.Pub.Kid, libKid, ourKid)
	}
}
