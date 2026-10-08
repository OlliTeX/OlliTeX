// Keyset provider + leaf EC + S2S client assertion (S3 core).
//
// Oracle: npm @oidfed/core v1.0.0 (keystore.mjs / leaf.mjs /
// ClientAssertionClient.mjs / verify.mjs). Fixtures in testdata/ pin the
// bytes.
package federation

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var contextBackground = context.Background

// --- keyset provider (02 §5 keystore.mjs oracle) ---

// KeyProvider — the keystore ops (02 §5). Go seam over the S2 Store
// (production: Mongo-backed; tests: MapStore). Node caches the keyset
// in-memory and invalidates per publish/switch/revoke; Go re-reads the
// Store per request (the Store is the seam — memoization is an
// orthogonal optimization).
type KeyProvider struct {
	Store Store
	Site  string // Settings.siteUrl
}

func (p *KeyProvider) getEntityId() (string, error) { return getEntityIdGo(p.Site) }

func (p *KeyProvider) getOrigin() (string, error) { return getOriginGo(p.Site) }

// ActiveKey — the single signing key for `purpose` (02 §5 "the single
// signing key"). Bootstrap error mirrors Node ("No active federation
// signing key (bootstrap has not been run)").
func (p *KeyProvider) ActiveKey(purpose string) (*FederationKey, error) {
	k, err := p.Store.ActiveKey(contextBackground(), purpose)
	if err != nil {
		if IsNotFound(err) {
			return nil, Errorf("No active %s signing key (bootstrap has not been run)", purpose)
		}
		return nil, err
	}
	return k, nil
}

// Bootstrap — node `ensureBootstrapped` (07 §2, before listen): generate
// both key sets (federation + oidc) if empty, persist as ACTIVE,
// expiresAt = now + LEAF_TTL_SECONDS (48h). Idempotent per purpose.
func (p *KeyProvider) Bootstrap() error {
	now := time.Now().Unix()
	for _, purpose := range []string{"federation", "oidc"} {
		if _, err := p.Store.ActiveKey(contextBackground(), purpose); err == nil {
			continue // already bootstrapped
		}
		pub, priv, err := GenerateES256()
		if err != nil {
			return err
		}
		if err := p.Store.SaveKey(contextBackground(), &FederationKey{
			Purpose:        purpose,
			Kid:            priv.Kid,
			Algorithm:      "ES256",
			PublicKey:      pub,
			PrivateKey:     priv,
			State:          "active",
			ExpiresAt:      now + LEAF_TTL_SECONDS,
			PublishedAt:    now,
			StateChangedAt: now,
		}); err != nil {
			return err
		}
	}
	return nil
}

// LeafSigningKey — the ACTIVE federation key (signs leaf ECs + S2S client
// assertions; npm `createFederationSigningKey`). The OIDC purpose key is
// separate (S4 signs id_tokens with it; it is NOT the leaf signer).
func (p *KeyProvider) LeafSigningKey() (*FederationKey, error) {
	return p.ActiveKey("federation")
}

// --- leaf jwks + historical payload (02 §5) ---

// leafJwksPayload — public halves of every non-revoked federation key
// (published + active + retiring = the grace window, 02 §5).
func (p *KeyProvider) leafJwksPayload(ctx context.Context) ([]JWK, error) {
	all, err := p.Store.AllKeys(ctx, "federation")
	if err != nil {
		return nil, err
	}
	out := make([]JWK, 0, len(all))
	for _, k := range all {
		if k.State == "revoked" {
			continue
		}
		out = append(out, k.PublicKey.PublicHalf())
	}
	return out, nil
}

// HistoricalKeySetPayload — GET /federation/federation-keys payload
// (02 §5, 04 §4), shape per @oidfed/core HistoricalKeysPayloadSchema:
// { iss, iat, keys: [{kty,kid,alg?,use?,exp,iat?}] }. All non-revoked keys,
// sorted by publishedAt ascending (02 §5 "grace sweep").
func HistoricalKeySetPayload(all []*FederationKey, entityId string, now int64) map[string]any {
	type histKey struct {
		Kty string `json:"kty"`
		Kid string `json:"kid"`
		Alg string `json:"alg,omitempty"`
		Use string `json:"use,omitempty"`
		Exp int64  `json:"exp"`
		Iat int64  `json:"iat,omitempty"`
	}
	keys := make([]histKey, 0, len(all))
	for _, k := range all {
		if k.State == "revoked" {
			continue
		}
		keys = append(keys, histKey{
			Kty: k.PublicKey.PublicHalf().Kty, Kid: k.Kid,
			Alg: k.PublicKey.PublicHalf().Alg, Use: k.PublicKey.PublicHalf().Use,
			Exp: k.ExpiresAt, Iat: k.PublishedAt,
		})
	}
	return map[string]any{"iss": entityId, "iat": now, "keys": keys}
}

// --- leaf entity configuration (02 §2 pairwise) ---

// BuildLeafEntityConfiguration — the signed self-attesting leaf EC (02 §2):
// pairwise mode — NO authority_hints (nothing above us; peers pin us
// directly); leaf check 16: openid_provider.issuer = the OP issuer.
// Served raw at GET /.well-known/openid-federation with Content-Type
// application/entity-statement+jwt; charset=utf-8.
func (p *KeyProvider) BuildLeafEntityConfiguration(now time.Time) (string, error) {
	entityId, err := p.getEntityId()
	if err != nil {
		return "", err
	}
	key, err := p.LeafSigningKey()
	if err != nil {
		return "", err
	}
	jwks, err := p.leafJwksPayload(contextBackground())
	if err != nil {
		return "", err
	}
	payload := map[string]any{
		"iss":      entityId,
		"sub":      entityId,
		"iat":      now.Unix(),
		"exp":      now.Unix() + LEAF_TTL_SECONDS,
		"jwks":     map[string]any{"keys": jwks},
		"metadata": buildLeafMetadata(entityId, p.siteEndpoints()),
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	// D-GOFED M3 (2026-09-26): signing via go-oidfed/lib — typ
	// entity-statement+jwt (oidfedconst), kid = lib RFC 8550 (M1), the payload
	// bytes are ours, unchanged (Node leaf.mjs wire).
	return libEntityStatementSign(payloadJSON, key.PrivateKey)
}

func (p *KeyProvider) siteEndpoints() OidcEndpoints {
	ep, _ := oidcEndpointsGo(p.Site)
	return ep
}

// buildLeafMetadata (oracle: npm leaf.mjs buildLeafMetadata, 02 §2).
func buildLeafMetadata(entityId string, endpoints OidcEndpoints) map[string]any {
	return map[string]any{
		"openid_provider": map[string]any{
			"issuer":                                endpoints.Issuer, // = <entity id>/federation/oidc (check 16)
			"authorization_endpoint":                endpoints.Authorization,
			"token_endpoint":                        endpoints.Token,
			"jwks_uri":                              endpoints.Jwks,
			"grant_types_supported":                 []string{"authorization_code"},
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"ES256"},
		},
		"openid_relying_party": map[string]any{
			"client_id":      entityId, // deterministic client id (04 §7)
			"client_name":    "Overleaf Federation (CEP)",
			"redirect_uris":  []string{endpoints.Callback},
			"grant_types":    []string{"authorization_code"},
			"response_types": []string{"code"},
			"response_modes": []string{"fragment"},
		},
	}
}

// --- S2S client assertion (npm ClientAssertionClient oracle) ---
//
// The assertion (03 §1: "one signing key (federation key), one JWT shape,
// one replay store"):
//
//	iss:  urn:overleaf-federation:client:<origin FQDN, no port>
//	sub:  same (npm createClientAssertion: sub = clientId)
//	aud:  https://<peer origin>/federation/s2s
//	jti:  UUIDv4 (Redis replay dedup, 03 §3)
//	iat:  now; exp: now + 300 s (03 §2)
//
// SIGNED BY THE FEDERATION KEY (NOT the OIDC key). typ = "JWT" (npm
// createClientAssertion typ:'JWT').

// signClientAssertion — the S2S assertion Compact JWS.
func signClientAssertion(pub, priv *JWK, origin, aud string, nowSec int64) (string, error) {
	clientId := "urn:overleaf-federation:client:" + origin
	assertion, err := SignJWT(priv, "JWT", mustMarshal(map[string]any{
		"iss": clientId,
		"sub": clientId,
		"aud": aud,
		"jti": UUIDv4(),
		"iat": nowSec,
		"exp": nowSec + CLIENT_ASSERTION_TTL_SECONDS,
	}))
	if err != nil {
		return "", err
	}
	_ = pub
	return assertion, nil
}

func mustMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// S2sRequest — one outbound S2S call (03 §2): the signed headers + the
// envelope body. The assertion is the signature itself: no body signature,
// no RFC 9421 (03 §1).
type S2sRequest struct {
	Headers map[string]string
	Body    map[string]any
}

// BuildS2sRequest — node `buildS2sRequest(peerOrigin, action, payload)`.
func (p *KeyProvider) BuildS2sRequest(peerOrigin, action string, payload any) (*S2sRequest, error) {
	origin, err := p.getOrigin()
	if err != nil {
		return nil, err
	}
	aud := "https://" + peerOrigin + "/federation/s2s"
	key, err := p.LeafSigningKey()
	if err != nil {
		return nil, err
	}
	assertion, err := signClientAssertion(key.PublicKey, key.PrivateKey, origin, aud, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	return &S2sRequest{
		Headers: map[string]string{
			"Content-Type":          "application/json",
			"client_assertion":      assertion,
			"client_assertion_type": S2sClientAssertionType,
		},
		Body: map[string]any{
			"action":  action,
			"from":    origin,
			"to":      peerOrigin,
			"ts":      time.Now().UnixMilli(),
			"payload": payload,
		},
	}, nil
}

// --- S2S inbound verification (npm verify.mjs oracle, 03 §2/§3/§6) ---
//
// Ordering 03 §6 "verify → dedup → apply, or refuse":
//
//  1. peer lookup by `from` (approved only; the pinned key is the
//     authority — NO per-call leaf refetch, kid-mismatch refetch is the
//     admin flow, 06 §2)
//  2. kid pre-check (header kid must be the peer's pinned kid)
//  3. signature + iss + aud + exp (ES256 against peer.anchorJwks)
//  4. replay (claimJti — applied by the S5 router; this function is
//     idempotent)

// VerifiedCaller — the successful verification result.
type VerifiedCaller struct {
	Peer      *FederationPeer
	ClientID  string
	IssuedAt  int64
	ExpiresAt int64
	Jti       string
	Sub       string
	// AnchorSource — how the trusted anchor was resolved (02 §4):
	// "pairwise" = the direct anchor pin (pinning IS establishment);
	// "institutional" = the registered child anchor, gated by the
	// explicit-registration trust-chain expiry (chain resolve on the peer
	// PIN — institutional TOFU).
	AnchorSource string
}

// VerifyS2sClientAssertion — steps ①–③ (④ replay is the router's
// CheckReplay); returns (caller, machineCode, detail, err).
// machineCode: 03 §6 codes ('peer-unknown' | 'unknown-kid' |
// 'bad-signature' | 'timestamp-skew' | ...).
func (p *KeyProvider) VerifyS2sClientAssertion(assertion string, from string) (v *VerifiedCaller, code, detail string, err error) {
	peer, perr := p.Store.PeerByOrigin(contextBackground(), from)
	if perr != nil || peer == nil || peer.Status != "approved" {
		return nil, "peer-unknown", "no approved peer for " + from, Errorf("peer not approved for %s", from)
	}
	// (2) kid pre-check (03 §6 `unknown-kid`).
	kid, _, _, ok := ParseProtectedHeader(assertion)
	if !ok {
		return nil, "bad-signature", "undecodable assertion", ErrBadAssertion
	}
	anchorNow := time.Now().Unix()
	anchorJwk, anchorSource, mcode, aerr := resolvePeerAnchor(peer, anchorNow)
	if aerr != nil {
		if mcode != "" {
			return nil, mcode, aerr.Error(), aerr
		}
		return nil, "bad-signature", aerr.Error(), aerr
	}
	// kid pre-check (03 §6 `unknown-kid`): the accepted kids are the
	// direct pin (pairwise) and the registered child anchor kid
	// (institutional, 02 §4).
	if kid != "" {
		accepted := map[string]bool{}
		if peer.Kid != "" {
			accepted[peer.Kid] = true
		}
		if anchorSource == "institutional" && peer.Registration != nil && peer.Registration.ChildAnchorKid != "" {
			accepted[peer.Registration.ChildAnchorKid] = true
		}
		if len(accepted) > 0 && !accepted[kid] {
			return nil, "unknown-kid", "kid " + kid + " not pinned for " + from, ErrUnknownKid
		}
	}
	verifiedKid, payload, verr := VerifyJWT(anchorJwk, assertion)
	if verr == ErrUnknownKid {
		return nil, "unknown-kid", "header kid mismatch: " + verifiedKid, verr
	}
	if verr != nil {
		return nil, "bad-signature", "client assertion signature failed", verr
	}
	var claims struct {
		Iss string `json:"iss"`
		Sub string `json:"sub"`
		Aud string `json:"aud"`
		Jti string `json:"jti"`
		Iat int64  `json:"iat"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, "bad-signature", "malformed claim payload", ErrBadAssertion
	}
	// iss must be derived from the PEER's PINNED origin (never a body
	// field — 03 §2 step 2).
	if claims.Iss != "urn:overleaf-federation:client:"+peer.Origin {
		return nil, "bad-signature", "iss mismatch", ErrBadAssertion
	}
	// aud must be OUR S2S endpoint (fixed per instance, 03 §8).
	entityId, _ := p.getEntityId()
	if claims.Aud != getS2sEndpointGo(entityId) {
		return nil, "bad-signature", "aud mismatch", ErrBadAssertion
	}
	if claims.Jti == "" {
		return nil, "bad-signature", "missing jti", ErrBadAssertion
	}
	nowSec := time.Now().Unix()
	if claims.Iat > nowSec+int64(ClockSkewSeconds) {
		return nil, "timestamp-skew", "iat in the future beyond clock tolerance", ErrBadAssertion
	}
	if claims.Exp+int64(ClockSkewSeconds) < nowSec {
		return nil, "bad-signature", "assertion expired", ErrBadAssertion
	}
	return &VerifiedCaller{
		Peer:         peer,
		ClientID:     claims.Iss,
		IssuedAt:     claims.Iat,
		ExpiresAt:    claims.Exp,
		Jti:          claims.Jti,
		Sub:          claims.Sub,
		AnchorSource: anchorSource,
	}, "", "", nil
}

// resolvePeerAnchor — 02 §4 institutional chain resolve vs the pairwise
// pin (the "heavy lift" adminPinPeer defers): the anchor the assertion is
// verified against depends on the peer MODE.
//
//	institutional (02 §4 explicit registration): the trusted key is the
//	REGISTERED child anchor (the registration statement result stored on
//	the peer row). The chain (child → trust anchor) is re-verified
//	offline against the stored registration — the pin is ground truth —
//	and is only valid until TrustChainExpiresAt (02 §4:
//	"how long the trust chain (up to the trust anchor) is valid").
//	Expired chain → `chain-expired`; no usable registration →
//	`anchor-missing`. An institutional row does NOT fall back to a direct
//	pin: trust is only what the registration established.
//
//	pairwise (02 §3): pinning IS establishment — the direct AnchorJwks is
//	the trusted anchor (legacy path, unchanged).
func resolvePeerAnchor(peer *FederationPeer, nowSec int64) (*JWK, string, string, error) {
	if peer.Mode == string(PeerModeInstitutional) {
		reg := peer.Registration
		if reg == nil || reg.ChildAnchorJwks == "" {
			return nil, "institutional", "anchor-missing", Errorf("federation: institutional peer %s has no registered child anchor", peer.Origin)
		}
		if reg.TrustChainExpiresAt > 0 && reg.TrustChainExpiresAt <= nowSec {
			return nil, "institutional", "chain-expired", Errorf("federation: trust chain for %s expired at %d", peer.Origin, reg.TrustChainExpiresAt)
		}
		var jwk JWK
		if err := json.Unmarshal([]byte(reg.ChildAnchorJwks), &jwk); err != nil {
			return nil, "institutional", "", Errorf("federation: child anchor JWK malformed: %v", err)
		}
		if jwk.Kty == "" || jwk.X == "" || jwk.Y == "" || jwk.Crv == "" {
			return nil, "institutional", "", Errorf("federation: child anchor JWK incomplete (kty/crv/x/y)")
		}
		return &jwk, "institutional", "", nil
	}
	jwk, err := parsePeerAnchorJwks(peer.AnchorJwks)
	if err != nil {
		return nil, "pairwise", "", err
	}
	return jwk, "pairwise", "", nil
}

func parsePeerAnchorJwks(s string) (*JWK, error) {
	if s == "" {
		return nil, errors.New("federation: peer anchor JWK empty")
	}
	var jwk JWK
	if err := json.Unmarshal([]byte(s), &jwk); err != nil {
		return nil, err
	}
	if jwk.Kty == "" || jwk.X == "" || jwk.Y == "" || jwk.Crv == "" {
		return nil, errors.New("federation: peer anchor JWK missing kty/crv/x/y")
	}
	return &jwk, nil
}
