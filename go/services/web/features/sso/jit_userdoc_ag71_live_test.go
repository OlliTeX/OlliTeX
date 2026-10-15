package sso

import (
	"context"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ag-71 (2026-10-15 live incident, davrot@ /hub 500): OIDC and SAML JIT users
// were created with a hand-rolled 11-field doc (camelCase firstName/lastName,
// no ace/enrollment/flags/features) — the full NewUserDoc baseline every
// downstream render assumes — so /hub 500'd for every SSO JIT user. LDAP JIT
// was already fixed on the same shape (leg-b closeout 2026-10-14); this test
// pins the OIcD + SAML parity to the same full bundle.
//
// Live-standalone E2E (same pattern as ldaplogin_live_test.go): skipped
// unless AG71_JIT_MONGO is set to a MongoDB URI. Recommended disposable env:
//
//	docker run -d --rm -p 27127:27017 mongo:9
//	AG71_JIT_MONGO="mongodb://127.0.0.1:27127" go test ./go/services/web/features/sso/ -run TestAg71JIT -v
func TestAg71JITUserDocFullBundleLive(t *testing.T) {
	dsn := os.Getenv("AG71_JIT_MONGO")
	if dsn == "" {
		t.Skip("AG71_JIT_MONGO not set (run with a disposable mongo for the JIT parity live test)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(dsn))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatalf("ping: %v", err)
	}
	db := client.Database("ag71jit")
	defer func() { _ = db.Drop(context.Background()) }()
	usersCol := db.Collection("users")

	// ---- OIDC JIT
	email := "ag71-oidc@example.test"
	u, err := oidcJIT(ctx, db, &OIDCProvider{}, ssoProfile{
		"email": email, "sub": "ag71-sub", "given_name": "Ag", "family_name": "SeventyOne",
	}, "1", "")
	if err != nil || u == nil {
		t.Fatalf("oidcJIT failed: u=%v err=%v", u, err)
	}
	raw, err := findUserByEmail(ctx, usersCol, email)
	if err != nil {
		t.Fatalf("oidc user not found: %v", err)
	}
	assertFullBundle(t, "oidc", raw)

	// ---- SAML JIT
	sEmail := "ag71-saml@example.test"
	su, err := samlJIT(ctx, db, &SAMLProvider{}, ssoProfile{
		"email": sEmail, "nameID": "ag71-sid",
	}, "2", "")
	if err != nil || su == nil {
		t.Fatalf("samlJIT failed: u=%v err=%v", su, err)
	}
	sRaw, err := findUserByEmail(ctx, usersCol, sEmail)
	if err != nil {
		t.Fatalf("saml user not found: %v", err)
	}
	assertFullBundle(t, "saml", sRaw)
}

// findUserByEmail decodes the user doc by its emails.email entry.
func findUserByEmail(ctx context.Context, col *mongo.Collection, email string) (bson.M, error) {
	var m bson.M
	err := col.FindOne(ctx, bson.D{{Key: "emails.email", Value: email}}).Decode(&m)
	return m, err
}

// assertFullBundle pins the NewUserDoc-baseline parity keys the downstream
// renders (hub, settings, features) assume, plus snake_case name parity
// (Node: first_name/last_name) and ObjectID _id.
func assertFullBundle(t *testing.T, which string, raw bson.M) {
	t.Helper()
	for _, k := range []string{"ace", "enrollment", "flags", "features", "first_name", "last_name"} {
		if _, ok := raw[k]; !ok {
			t.Fatalf("%s JIT user doc missing %q (full-bundle parity broken: /hub-style renderers 500)", which, k)
		}
	}
	// NOTE: camelCase firstName/lastName may also be present — the NewUserDoc
	// base carries legacy aliases identical to local registration parity; the
	// renderers read snake_case, which is the pin above.
	if _, ok := raw["_id"].(bson.ObjectID); !ok {
		t.Fatalf("%s JIT user _id is %T (want bson.ObjectID per Node parity)", which, raw["_id"])
	}
	t.Logf("%s JIT user doc: %d fields, ObjectID _id, full bundle present ✓", which, len(raw))
}
