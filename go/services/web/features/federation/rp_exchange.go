// rp_exchange.go — S10 (A-side RP): discovery fetch, authorization
// redirect build, code exchange, id_token verification.
//
// Pinned wire (overleaf-fed 01 §5 + the B-side v9 OP contract in
// oidcengine*.go):
//
//	client_id        urn:overleaf-federation:client:<A-origin>
//	                   (B registers exactly this id for us —
//	                    oidcprovider.go BuildOidcProviderClients)
//	redirect_uri     https://<A-origin>/federation/oidc/rp/callback
//	                   (must be in B's clients[].redirect_uris — pinned in
//	                    oidcprovider.go:76 against the peer origin)
//	scope            "openid" (string)
//	PKCE             S256 only — public client, token_endpoint_auth
//	                   method "none" (clients.mjs pin)
//	authorization    params: response_type=code + client_id + redirect_uri
//	                   + scope + nonce + code_challenge + method + state
//	token request    form: grant_type=authorization_code + code +
//	                   redirect_uri + code_verifier (NO client auth)
//	token response   {id_token, ...}; id_token REQUIRED (scope openid),
//	                   ES256, kid from B's /federation/oidc/jwks.
//	id_token claims  iss, aud, iat, exp, nonce, sub, origin, localName,
//	                   displayName, institution (null when empty per the
//	                    v9 findAccount pin)

package federation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// RPDiscovery — the B-side endpoints the A-side flow needs.
type RPDiscovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JwksURI               string `json:"jwks_uri"`
}

// FetchDiscovery — GET <base>/federation/oidc/.well-known/openid-
// configuration (base = https://<peer-origin>, the B-side s4Routes
// surface). 10 s timeout (FEDERATION_S2S_FETCH_TIMEOUT_MS family).
func FetchDiscovery(ctx context.Context, c *http.Client, base string) (RPDiscovery, error) {
	if c == nil {
		c = http.DefaultClient
	}
	u := strings.TrimRight(base, "/") + "/federation/oidc/.well-known/openid-configuration"
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return RPDiscovery{}, err
	}
	res, err := c.Do(req)
	if err != nil {
		return RPDiscovery{}, errors.New("federation rp: peer discovery unreachable (" + err.Error() + ")")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return RPDiscovery{}, errors.New("federation rp: peer discovery " + strconv.Itoa(res.StatusCode))
	}
	var d RPDiscovery
	if err := json.NewDecoder(res.Body).Decode(&d); err != nil {
		return RPDiscovery{}, errors.New("federation rp: peer discovery decode")
	}
	if d.Issuer == "" || d.AuthorizationEndpoint == "" || d.TokenEndpoint == "" || d.JwksURI == "" {
		return RPDiscovery{}, errors.New("federation rp: peer discovery incomplete")
	}
	return d, nil
}

// RPClientID — the client id B registered for us (oidcprovider.go pin).
func RPClientID(localOrigin string) string {
	return "urn:overleaf-federation:client:" + localOrigin
}

// RPRedirectURI — our callback (oidcprovider.go:76 pin, per peer origin).
func RPRedirectURI(localOrigin string) string {
	return "https://" + localOrigin + "/federation/oidc/rp/callback"
}

// S256Challenge — base64url(SHA-256(ASCII(verifier))) (RFC 7636 §4.2).
func S256Challenge(verifier string) string {
	d := sha256.Sum256([]byte(verifier))
	return b64url.EncodeToString(d[:])
}

// BuildAuthorizeURL — the B authorization-endpoint GET the invited
// user's browser is sent to. State is signed with OUR site (only this
// deployment may verify it).
func BuildAuthorizeURL(site string, disc RPDiscovery, st *RPState) (string, error) {
	origin, err := getOriginGo(site)
	if err != nil {
		return "", err
	}
	sig, err := SignRPState(site, st)
	if err != nil {
		return "", err
	}
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", RPClientID(origin))
	q.Set("redirect_uri", RPRedirectURI(origin))
	q.Set("scope", "openid")
	q.Set("nonce", st.Nonce)
	q.Set("code_challenge", S256Challenge(st.Verifier))
	q.Set("code_challenge_method", "S256")
	q.Set("state", sig)
	return disc.AuthorizationEndpoint + "?" + q.Encode(), nil
}

// IDTokenClaims — the A-side useful set (v9 account claim set +
// sub/nonce).
type IDTokenClaims struct {
	Sub         string
	Origin      string
	LocalName   string
	DisplayName string
	Institution string // "" when B sent null/absent
	Nonce       string
}

// fetchPeerJWKS — B's federation oidc jwks (kid → public JWK map).
func fetchPeerJWKS(ctx context.Context, c *http.Client, jwksURI string) (map[string]*JWK, error) {
	if c == nil {
		c = http.DefaultClient
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURI, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.Do(req)
	if err != nil {
		return nil, errors.New("federation rp: peer jwks unreachable")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, errors.New("federation rp: peer jwks " + strconv.Itoa(res.StatusCode))
	}
	var doc struct {
		Keys []JWK `json:"keys"`
	}
	if err := json.NewDecoder(res.Body).Decode(&doc); err != nil {
		return nil, errors.New("federation rp: peer jwks decode")
	}
	out := map[string]*JWK{}
	for i := range doc.Keys {
		k := &doc.Keys[i]
		if k.Kid == "" || k.Kty != "EC" || k.X == "" || k.Y == "" {
			continue // not a usable P-256 publishing key
		}
		out[k.Kid] = k
	}
	if len(out) == 0 {
		return nil, errors.New("federation rp: peer jwks has no usable EC key")
	}
	return out, nil
}

// verifyPeerIDToken — kid-selection + ES256 verify against the fetched
// JWKS (each candidate VerifyJWT pins its own kid, so a wrong kid
// fails as unknown-kid without touching the signature).
func verifyPeerIDToken(jwks map[string]*JWK, jws string) (kid string, payload []byte, err error) {
	for _, k := range jwks {
		kid, payload, err = VerifyJWT(k, jws)
		if err == nil {
			return kid, payload, nil
		}
	}
	return kid, nil, errors.New("unknown kid or bad signature")
}

// CodeExchange — POST the B token endpoint (public client, PKCE S256)
// and verify the id_token against B's JWKS.
//
// Honest error codes (envelope detail): "token-fetch" (transport),
// "token-error:<b-code>" (wire error), "idtoken-missing",
// "idtoken-verify:<reason>", "idtoken-claims:<reason>".
func CodeExchange(
	ctx context.Context,
	c *http.Client,
	site string,
	disc RPDiscovery,
	st *RPState,
	code string,
	now int64,
) (*IDTokenClaims, error) {
	if code == "" {
		return nil, errors.New("federation rp: missing code")
	}
	origin, err := getOriginGo(site)
	if err != nil {
		return nil, err
	}
	redirectURI := RPRedirectURI(origin)
	if c == nil {
		c = http.DefaultClient
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {getClientIdGo(origin)},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {st.Verifier},
	}
	// client_id is MANDATORY at the vendored token endpoint
	// (authenticateClient: no client_id -> invalid_request 'no client
	// authentication mechanism provided' — the public-client model is
	// client_id + PKCE, no secret/assertion). The original CodeExchange
	// omitted it, so every live RP callback token swap 400'd (dual
	// instance E2E 2026-10-09: 'token-error:invalid_request'); pinned by
	// the live S11 leg (TestFedB_LiveS11RoundTrip mint step).
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, disc.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := c.Do(req)
	if err != nil {
		return nil, errors.New("federation rp: token-fetch (" + err.Error() + ")")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		var body struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(res.Body).Decode(&body)
		e := "unknown"
		if body.Error != "" {
			e = body.Error
		}
		return nil, errors.New("federation rp: token-error:" + e)
	}
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&tok); err != nil {
		return nil, errors.New("federation rp: token decode")
	}
	if tok.IDToken == "" {
		return nil, errors.New("federation rp: idtoken-missing")
	}

	// Verify against B's JWKS (kid-selected, ES256).
	jwks, err := fetchPeerJWKS(ctx, c, disc.JwksURI)
	if err != nil {
		return nil, err
	}
	kid, payload, err := verifyPeerIDToken(jwks, tok.IDToken)
	if err != nil {
		return nil, errors.New("federation rp: idtoken-verify:" + err.Error() + " (kid=" + kid + ")")
	}

	var claims struct {
		Iss         string          `json:"iss"`
		Aud         json.RawMessage `json:"aud"`
		Iat         int64           `json:"iat"`
		Exp         int64           `json:"exp"`
		Nonce       string          `json:"nonce"`
		Sub         string          `json:"sub"`
		Origin      string          `json:"origin"`
		LocalName   string          `json:"localName"`
		DisplayName string          `json:"displayName"`
		Institution *string         `json:"institution"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, errors.New("federation rp: idtoken-claims:decode")
	}
	if claims.Iss != disc.Issuer {
		return nil, errors.New("federation rp: idtoken-claims:issuer")
	}
	if rpAud(claims.Aud) != RPClientID(origin) {
		return nil, errors.New("federation rp: idtoken-claims:audience")
	}
	if claims.Exp <= now {
		return nil, errors.New("federation rp: idtoken-claims:expired")
	}
	if claims.Iat > now+120 { // 120 s clock skew, iat direction only
		return nil, errors.New("federation rp: idtoken-claims:iat-future")
	}
	if claims.Nonce == "" || claims.Nonce != st.Nonce {
		return nil, errors.New("federation rp: idtoken-claims:nonce")
	}
	if claims.Sub == "" || claims.LocalName == "" || claims.Origin == "" {
		return nil, errors.New("federation rp: idtoken-claims:ident")
	}
	out := &IDTokenClaims{
		Sub:         claims.Sub,
		Origin:      claims.Origin,
		LocalName:   claims.LocalName,
		DisplayName: claims.DisplayName,
		Nonce:       claims.Nonce,
	}
	if claims.Institution != nil {
		out.Institution = *claims.Institution
	}
	return out, nil
}

// rpAud — RFC 7519 allows string or [string] for aud; the v9 B-side
// here emits the single string (aud: client_id). Accept both, first
// element wins.
func rpAud(raw json.RawMessage) string {
	var s string
	if raw != nil && len(raw) > 0 && raw[0] == '"' {
		_ = json.Unmarshal(raw, &s)
	}
	var arr []string
	if raw != nil && len(raw) > 0 && raw[0] == '[' {
		_ = json.Unmarshal(raw, &arr)
	}
	if len(arr) > 0 {
		return arr[0]
	}
	return s
}
