// B-side OIDC provider engine (oidc-provider v9 subset, owner decision
// D-OP option 1: subset port, oracle-pinned against plan 05 §8).
//
// The v9 contracts implemented here (S4b-1): clients[] rebuild (05
// §8.3, clients.mjs), the OP JWKS surface (05 §8.1), discovery
// metadata, and the v9 findAccount seam (05 §8.5). S4b-2: /auth +
// interaction; S4b-3: /token + PKCE + id_token claims.
package federation

import (
	"context"
)

// oidcPurpose — the id_token signing key purpose (02 §5: TWO key
// purposes; the 'oidc' key signs id_tokens and is served at
// /federation/oidc/jwks — it is NOT the leaf/S2S signing key).
const oidcPurpose = "oidc"

// ttlOIDCAuthorizationCodeSeconds — v9 Provider.ttl (createProvider.mjs,
// 05 §8.6): AuthorizationCode 120 s (01 §5 step 5, single-use code).
const ttlOIDCAuthorizationCodeSeconds = 120

// --- v9 `findAccount` data seam (05 §8.5) ---

// OidcUserClaims — the identity claims findAccount supplies (01 §5).
// A binds on the (origin, localName) pair, NOT sub (06 §3). sub itself
// = the accountId, injected by v9, never from findAccount.
type OidcUserClaims struct {
	Origin      string // B's origin FQDN (portless)
	LocalName   string // user.email (colon-forbidden, 04 §1.1) — the anchor
	DisplayName string // first+last, fallback email
	Institution string // optional
}

// OidcUserSource — the B-side account lookup for v9 `findAccount`
// (User.findById in Node). Production: Mongo users; tests: fake.
// Suspended / unknown users -> ok=false (v9: null = not available).
type OidcUserSource interface {
	Account(ctx context.Context, userID string) (OidcUserClaims, bool)
}

// --- OIDC clients[] (clients.mjs oracle, 05 §8.3/§8.8) ---

// OidcProviderClient — one static v9 client row (clientDefaults +
// per-peer redirect_uris). v9 hard requirements (verified against
// oidc-provider 9.12.2 + the repo probe):
//
//   - client_id is MANDATORY for static clients (initialize_clients throws)
//   - id_token_signed_response_alg: ES256 mandatory for public clients
//   - metadata keys SNAKE_CASE; `scope` a STRING (not array)
//   - token_endpoint_auth_method: none (PKCE public client)
type OidcProviderClient struct {
	ClientID     string   `json:"client_id"`
	RedirectURIs []string `json:"redirect_uris"`
	Scope        string   `json:"scope"`
}

// BuildOidcProviderClients — the boot-time static clients[] rebuild
// from approved FederationPeers (05 §8.3 "clients[] is a boot-time
// snapshot... v9 has no findClient hook"). Node oracle:
// FederationPeer.find({ status: 'approved' }) — direction NOT filtered
// (both approved peers get clients; OP clients serve incoming auth from
// EITHER role's browser on this instance).
func BuildOidcProviderClients(store Store, ctx context.Context) []OidcProviderClient {
	peers, err := store.AllPeers(ctx)
	if err != nil {
		return nil
	}
	out := []OidcProviderClient{}
	for _, peer := range peers {
		if peer.Status != "approved" {
			continue
		}
		out = append(out, OidcProviderClient{
			ClientID:     getClientIdGo(peer.Origin),
			RedirectURIs: []string{"https://" + peer.Origin + "/federation/oidc/rp/callback"},
			Scope:        "openid",
		})
	}
	return out
}

// ProviderClientByID — the minting gate (06 §3.1, e2e-verified): no
// clients[] row for the minting origin -> unsupported client -> no code
// mint. A fresh approval must be re-read (Node rebuilds at provider
// construction; Go re-derives per authorization, S4b-2).
func ProviderClientByID(clients []OidcProviderClient, clientID string) *OidcProviderClient {
	for i := range clients {
		if clients[i].ClientID == clientID {
			return &clients[i]
		}
	}
	return nil
}

// --- GET /federation/oidc/jwks (05 §8.1) ---
//
// The provider's serving JWKS: oidc-purpose unrevoked keys, publishedAt
// ascending (02 §5). Public halves only (never d — redact.go). The
// A-side fetches this (CodeExchange: Redis `federation:jwks:<origin>`
// TTL 1 h, refetch on kid mismatch).
func OIDCJwksPayload(store Store, ctx context.Context) (map[string]any, error) {
	keys, err := store.AllKeys(ctx, oidcPurpose)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(keys))
	for _, k := range keys {
		if k.State == "revoked" {
			continue
		}
		out = append(out, k.PublicKey.PublicHalf())
	}
	return map[string]any{"keys": out}, nil
}

// --- GET /federation/oidc/.well-known/openid-configuration (v9
// discovery; Node mounts it, live interop fetches it) ---

// OidcDiscoveryMetadata — the static discovery doc for this instance
// (iss is absolute per OidcEndpoints).
func OidcDiscoveryMetadata(endpoints OidcEndpoints) map[string]any {
	return map[string]any{
		"issuer":                                endpoints.Issuer,
		"authorization_endpoint":                endpoints.Authorization,
		"token_endpoint":                        endpoints.Token,
		"jwks_uri":                              endpoints.Jwks,
		"scopes_supported":                      []string{"openid"},
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"grant_types_supported":                 []string{"authorization_code"},
		"id_token_signing_alg_values_supported": []string{"ES256"},
		"claims_supported":                      []string{"sub", "origin", "localName", "displayName", "institution"},
	}
}
