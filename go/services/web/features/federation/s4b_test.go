// S4b-1 tests: clients[] rebuild (oracle: clients.mjs), OP JWKS payload
// (oracle: oidcSigningKeys), discovery metadata (v9 mount), GET handlers.
package federation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ollitex/go/services/web/core"
)

// TestBuildOidcProviderClientsOracle — clients.mjs oracle (05 §8.3):
// one client per approved peer (Direction NOT filtered — outbound,
// inbound, and all of "both" peers listed); pending/revoked excluded.
// client_id = urn convention (03 §2); redirect =
// https://<origin>/federation/oidc/rp/callback.
func TestBuildOidcProviderClientsOracle(t *testing.T) {
	ctx := context.Background()
	store := NewMapStore()
	now := time.Now()
	for _, p := range []FederationPeer{
		{Origin: "out.example.net", Status: "approved", Direction: "outbound", FederatedAt: now},
		{Origin: "both.example.net", Status: "approved", Direction: "both", FederatedAt: now},
		{Origin: "in.example.net", Status: "approved", Direction: "inbound", FederatedAt: now},
		{Origin: "pending.example.net", Status: "pending", Direction: "outbound", FederatedAt: now},
		{Origin: "revoked.example.net", Status: "revoked", Direction: "outbound", FederatedAt: now},
	} {
		if err := store.SavePeer(ctx, &p); err != nil {
			t.Fatal(err)
		}
	}
	clients := BuildOidcProviderClients(store, ctx)
	if len(clients) != 3 {
		t.Fatalf("clients must cover the 3 approved peers (direction NOT filtered, 05 §8.3); got %d", len(clients))
	}
	for _, p := range clients {
		if p.ClientID != "urn:overleaf-federation:client:"+hostOf(p.ClientID) {
			t.Fatalf("client_id convention (03 §2): %q", p.ClientID)
		}
		if len(p.RedirectURIs) != 1 || p.RedirectURIs[0] != "https://"+hostOf(p.ClientID)+"/federation/oidc/rp/callback" {
			t.Fatalf("redirect_uris: %+v", p.RedirectURIs)
		}
		if p.Scope != "openid" {
			t.Fatalf("scope STRING, not array (v9): %q", p.Scope)
		}
	}
	if ProviderClientByID(clients, "urn:overleaf-federation:client:stranger.example.net") != nil {
		t.Fatal("unknown client must NOT be in clients[] (mint gate 06 §3.1)")
	}
	if ProviderClientByID(clients, "urn:overleaf-federation:client:pending.example.net") != nil {
		t.Fatal("pending peer must NOT have a clients[] row")
	}
}

func hostOf(clientID string) string {
	urn := "urn:overleaf-federation:client:"
	if len(clientID) <= len(urn) {
		return clientID
	}
	return clientID[len(urn):]
}

// TestOidcJwksPayloadPurposeOidc — oracle: createProvider.mjs
// `jwks.keys = oidcSigningKeys()` (purpose='oidc', non-revoked).
// The leaf/federation key must NOT leak (02 §5 two separate stores).
func TestOidcJwksPayloadPurposeOidc(t *testing.T) {
	ctx := context.Background()
	store := NewMapStore()
	now := time.Now().Unix()
	oPub, oPriv, err := GenerateES256()
	if err != nil {
		t.Fatal(err)
	}
	fPub, fPriv, err := GenerateES256()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveKey(ctx, &FederationKey{
		Purpose: oidcPurpose, Kid: oPub.Kid,
		PublicKey: oPub, PrivateKey: oPub, State: "active",
		ExpiresAt: now + 1, PublishedAt: now, StateChangedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	_ = oPriv
	if err := store.SaveKey(ctx, &FederationKey{
		Purpose: "federation", Kid: fPub.Kid,
		PublicKey: fPub, PrivateKey: fPub, State: "active",
		ExpiresAt: now + 1, PublishedAt: now, StateChangedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	_ = fPriv
	rPub, rPriv, err := GenerateES256()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveKey(ctx, &FederationKey{
		Purpose: oidcPurpose, Kid: rPub.Kid,
		PublicKey: rPub, PrivateKey: rPub, State: "revoked",
		ExpiresAt: now, PublishedAt: now, StateChangedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	_ = rPriv
	payload, err := OIDCJwksPayload(store, ctx)
	if err != nil {
		t.Fatal(err)
	}
	keys, _ := payload["keys"].([]any)
	if len(keys) != 1 {
		t.Fatalf("must serve exactly 1 active oidc key (not federation, not revoked); got %d", len(keys))
	}
	k, _ := keys[0].(JWK)
	if k.Kid != oPub.Kid {
		t.Fatalf("served kid %q != active oidc key %q (federation + revoked must be excluded)", k.Kid, oPub.Kid)
	}
	if k.D != "" {
		t.Fatal("private d must NOT be served (06 §7)")
	}
	// A-side resolver (CodeExchange.mjs) needs kty + crv + x + y.
	if k.Kty != "EC" || k.Crv != "P-256" {
		t.Fatalf("JWK shape: kty=%q crv=%q", k.Kty, k.Crv)
	}
	if k.X == "" || k.Y == "" {
		t.Fatal("JWK must carry x/y (ES256, CodeExchange resolveJwk)")
	}
}

// TestOidcDiscoveryMetadata — v9 discovery doc (absolute issuer +
// endpoints per OidcEndpoints).
func TestOidcDiscoveryMetadata(t *testing.T) {
	ep, err := oidcEndpointsGo("https://b.example.org:9443")
	if err != nil {
		t.Fatal(err)
	}
	meta := OidcDiscoveryMetadata(ep)
	if meta["issuer"] != "https://b.example.org/federation/oidc" {
		t.Fatalf("issuer: %v", meta["issuer"])
	}
	if meta["jwks_uri"] != "https://b.example.org/federation/oidc/jwks" {
		t.Fatalf("jwks_uri: %v", meta["jwks_uri"])
	}
	if meta["token_endpoint"] != "https://b.example.org/federation/oidc/token" {
		t.Fatalf("token_endpoint: %v", meta["token_endpoint"])
	}
	if st, ok := meta["subject_types_supported"].([]string); !ok || len(st) != 1 || st[0] != "public" {
		t.Fatalf("subject_types_supported: %v", meta["subject_types_supported"])
	}
	if sc, ok := meta["scopes_supported"].([]string); !ok || len(sc) != 1 || sc[0] != "openid" {
		t.Fatalf("scopes_supported: %v", meta["scopes_supported"])
	}
	claims, _ := meta["claims_supported"].([]string)
	want := map[string]bool{"sub": true, "origin": true, "localName": true, "displayName": true, "institution": true}
	if len(claims) != len(want) {
		t.Fatalf("claims_supported: %v", claims)
	}
	for _, c := range claims {
		if !want[c] {
			t.Fatalf("unexpected claim: %q", c)
		}
	}
}

// TestHandleOidcJwksRoute — GET route handler (httptest, MapStore).
func TestHandleOidcJwksRoute(t *testing.T) {
	store := NewMapStore()
	w := httptest.NewRecorder()
	pub, priv, err := GenerateES256()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveKey(context.Background(), &FederationKey{
		Purpose: oidcPurpose, Kid: priv.Kid,
		PublicKey: pub, PrivateKey: pub, State: "active",
		ExpiresAt: 0, PublishedAt: 0, StateChangedAt: 0,
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/federation/oidc/jwks", nil)
	cxt := &core.Cxt{Req: req}
	handleOidcJwks(store, cxt, &core.Res{W: w})
	if w.Code != http.StatusOK {
		t.Fatalf("jwks route: %d %s", w.Code, w.Body.String())
	}
}
