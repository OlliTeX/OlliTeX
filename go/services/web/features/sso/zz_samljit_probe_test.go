package sso

// zz_samljit_probe_test.go — LIVE regression test for the empty-passport SSO
// session (A4 positive-leg, 2026-10-06). Two bugs, both proven live:
//   1) samlJIT/oidcJIT `else { return nil, err }` sat on the SUCCESS branch
//      → (nil user, nil error) → ACS logged in an EMPTY passport user.
//   2) driver v2 does NOT coerce a hex-STRING _id filter to ObjectID →
//      samlIdentifiers/confirmedAt $set silently no-op'd.
// Requires SAMLJIT_MONGO_URI (e2e mongo, a `jackson@example.com` seed user)
// — skip-gated like the other zz_ probes.

import (
	"context"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestZZ_SAMLJITLIVE(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	u := os.Getenv("SAMLJIT_MONGO_URI")
	if u == "" {
		t.Skip("SAMLJIT_MONGO_URI not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(u))
	if err != nil {
		t.Skipf("connect: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		t.Skipf("ping: %v", err)
	}
	db := client.Database("sharelatex")
	users := db.Collection("users")

	var seed map[string]any
	if err := users.FindOne(ctx, bson.D{{Key: "emails.email", Value: "jackson@example.com"}}).Decode(&seed); err != nil {
		t.Skipf("seed user jackson@example.com not found: %v", err)
	}

	profile := ssoProfile{"nameID": "jackson@example.com", "email": "jackson@example.com"}
	p := &SAMLProvider{ID: "sso-saml-e2e", Enabled: true}
	out, jerr := samlJIT(ctx, db, p, profile, "sso-saml-e2e", "user")
	if jerr != nil {
		t.Fatalf("samlJIT error: %v", jerr)
	}
	if out == nil {
		t.Fatal("samlJIT returned nil user (empty-passport regression)")
	}
	h := userIDHex(out)
	if h == "" {
		t.Fatal("user _id empty in samlJIT result (empty-passport regression)")
	}
	if e, _ := out["email"].(string); e != "jackson@example.com" {
		t.Fatalf("email = %q, want jackson@example.com", e)
	}
	// identifiers link must be written (ObjectID-filter regression).
	linked := users.FindOne(ctx, bson.D{
		{Key: "emails.email", Value: "jackson@example.com"},
		{Key: "samlIdentifiers.0.providerId", Value: "sso-saml-e2e"},
	})
	if linked.Err() != nil {
		t.Fatalf("samlIdentifiers not linked for jackson: %v", linked.Err())
	}
	t.Logf("PASS: samlJIT out _id=%s email=%s, identifiers linked", h, out["email"])

	_ = seed
}
