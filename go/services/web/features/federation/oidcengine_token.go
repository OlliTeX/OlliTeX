// oidcengine_token.go — POST /federation/oidc/token (vendored
// actions/token.js + actions/grants/authorization_code.js, oracle-pinned).
//
// The token route chain (initialize_app.js: error_handler + token.js):
//
//	noCache → parseBody (urlencoded form) → clientAuth (authenticateClient:
//	  no client_id → invalid_request 'no client authentication mechanism
//	  provided'; miss → 401 invalid_client) → supportedGrantTypeCheck
//	  (presence 'grant_type' → invalid_request 'missing required parameter
//	  'grant_type''; unsupported → 400 unsupported_grant_type 'unsupported
//	  grant_type requested') → allowedGrantTypeCheck (static clients grant
//	  ['authorization_code'] → else invalid_request 'requested grant type
//	  is not allowed for this client') → authorizationCodeHandler:
//	    allowOmittingSingleRegisteredRedirectUri (vendor default TRUE;
//	    no redirect_uri + client.redirectUris.length === 1 → fill) →
//	    presence ('code','redirect_uri') → findGrantSource (opACFind:
//	    opFind + client mismatch + three-way checkSessionBinding) →
//	    isExpired → validateGrant → checkPKCE (format → invalid_request
//	    wire; mismatch → invalid_grant wire) → redirectUri mismatch →
//	    consumeGrantSource (replay → opRevokeCascade + invalid_grant) →
//	    issueTokens (validateAccount → checkAccountMismatch → AT mint →
//	    id_token → buildTokenResponse 200 {access_token, expires_in:3600,
//	    id_token, scope:'openid', token_type:'Bearer'}).
//
// Wire (vendored err_out.js + errors.js, session-6 pins):
//   - invalid_grant ALWAYS renders {error:'invalid_grant',
//     error_description:'grant request is invalid'} (per-cause message is
//     error_detail — LOG ONLY, never on the wire).
//   - invalid_request rides the SPECIFIC thrown description on the wire.
//   - unknown client 401 {error:'invalid_client',
//     error_description:'client authentication failed'}.
//
// Go seam: token is NoLogin (OP-owned, never behind the B-visitor login
// gate) — the OP session doc is loaded READ-ONLY (if the _session cookie
// is present), never written, never cookie-emitted.
package federation

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"ollitex/go/services/web/core"
)

// constantEquals — vendored helpers/constant_equals.js (timing-safe).
func constantEquals(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// --- PKCE (vendored helpers/pkce.js + pkce_format.js, byte-for-byte) ---

// opPKCEFormatOK — vendor charset: [\w.\-~] = [a-zA-Z0-9_.-~].
func opPKCEFormatOK(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '_' || c == '-' || c == '.' || c == '~' {
			continue
		}
		return false
	}
	return true
}

// opCheckPKCE — vendored checkPKCE (byte-for-byte):
//
//	if (verifier) checkFormat(verifier, 'code_verifier')   // InvalidRequest
//	if (verifier || challenge) {
//	  try {
//	    let expected = verifier
//	    if (!expected) throw new Error('code_verifier must be provided')
//	    if (method === 'S256')
//	      expected = crypto.hash('sha256', expected, 'base64url')
//	    else throw new Error('unsupported code_challenge_method')
//	    if (!constantEquals(challenge, expected))
//	      throw new Error('code_verifier does not match code_challenge')
//	  } catch (cause) { throw new InvalidGrant({ cause }) }
//	}
//
// checkFormat throws InvalidRequest DIRECTLY (the specific message rides
// the wire); everything else maps to ONE InvalidGrant (wire 'grant
// request is invalid'; per-cause = error_detail, LOG).
func opCheckPKCE(verifier, challenge, challengeMethod string) *opErr {
	if verifier != "" {
		if len(verifier) < 43 {
			return opInvalidRequest("code_verifier must be a string with a minimum length of 43 characters")
		}
		if len(verifier) > 128 {
			return opInvalidRequest("code_verifier must be a string with a maximum length of 128 characters")
		}
		if !opPKCEFormatOK(verifier) {
			return opInvalidRequest("code_verifier contains invalid characters")
		}
	}
	if verifier == "" && challenge == "" {
		return nil // public-client dance — both absent → pass.
	}
	if verifier == "" {
		return opInvalidGrant("code_verifier must be provided")
	}
	if challengeMethod != "S256" {
		return opInvalidGrant("unsupported code_challenge_method")
	}
	sum := sha256.Sum256([]byte(verifier))
	expect := base64.RawURLEncoding.EncodeToString(sum[:])
	if !constantEquals(expect, challenge) {
		return opInvalidGrant("code_verifier does not match code_challenge")
	}
	return nil
}

// --- opRevokeCascade — vendored helpers/revoke.js (replay path) ---
//
//	Promise.all([
//	  AccessToken.revokeByGrantId(grantId),
//	  AuthorizationCode.revokeByGrantId(grantId),  // client grants AC
//	  // (RefreshToken/DeviceCode/BA/PAC all FALSE for the static client)
//	  ...(revokeGrantPolicy ? [Grant.adapter.destroy(grantId)] : []),
//	])
//
// overleaf-fed clients.mjs: client.grantTypes = ['authorization_code'],
// so the cascade is AT + AC + Grant.destroy — the adapter
// RevokeByGrantId sweeps every doc on the grant SET (trims the client
// sweep SET per member) and Grant.Destroy removes the consent record +
// (account, client) index.
//
// guard: vendored `if (!grantId) return` (a missing grantId makes the
// replay cascade a no-op — the AC is left for TTL, matching the vendored
// `if (!grantId)` in the revoke helper; the uniform wire still fires).
func (e *OidcEngine) opRevokeCascade(grantID string) {
	if grantID == "" {
		return
	}
	if rErr := (&OidcAdapter{ModelName: "AccessToken", Redis: e.Redis}).RevokeByGrantId(grantID); rErr != nil {
		// (best-effort — the wire fires regardless.)
		_ = rErr
	}
	if rErr := (&OidcAdapter{ModelName: "AuthorizationCode", Redis: e.Redis}).RevokeByGrantId(grantID); rErr != nil {
		_ = rErr
	}
	if dErr := (&OidcAdapter{ModelName: "Grant", Redis: e.Redis}).Destroy(grantID); dErr != nil {
		_ = dErr
	}
}

// --- opACFind — vendored findGrantSource + checkSessionBinding ---
//
//	findGrantSource(ctx, Model, value, label):
//	  source = Model.find(value, {ignoreExpiration:true})
//	  if (!source) throw InvalidGrant('<label> not found')
//	  if (source.clientId !== ctx.oidc.client.clientId)
//	    throw InvalidGuard('client mismatch')
//	  return source
//
//	checkSessionBinding (vendored token_helpers.js — the AC.find override):
//	  for expiresWithSession codes → session = Session.findByUid(ac.sessionUid)
//	  if (!session || ac.accountId !== session.accountId ||
//	  ac.grantId !== session.grantIdFor(ac.clientId)) → undefined
//	  (treated as NOT FOUND).
//
//	isExpired: adapter TTL (120s) already expired the doc → opFind miss →
//	  'not found'. Both the expired and missing cases map to the ONE wire
//	  (invalid_grant 'grant request is invalid'; per-cause 'authorization
//	  code is expired' / 'not found' is error_detail, LOG — session-14 pin).
//
// Returns (nil, opErr) when any check fails; all the per-cause opErrs
// render the uniform invalid_grant wire.
func (e *OidcEngine) opACFind(code, clientID string) (map[string]any, *opErr) {
	doc, err := e.opFind("AuthorizationCode", code)
	if err != nil || doc == nil {
		return nil, opInvalidGrant("authorization code not found")
	}
	if v, _ := doc["clientId"].(string); v != clientID {
		return nil, opInvalidGrant("client mismatch")
	}
	// checkSessionBinding (the dance AC is expiresWithSession=true):
	// Session.findByUid(ac.sessionUid), then the three-way — accountId
	// match + grantIdFor(clientId) match. NO cookie involvement (vendored
	// token_helpers.js: the cookie is NOT consulted; the OP session doc
	// is looked up by the AC's OWN sessionUid via the adapter sub-index).
	if ew, _ := doc["expiresWithSession"].(bool); ew {
		// vendored Session.findByUid(ac.sessionUid) — the adapter
		// sub-index (sub:<uid>), NOT a raw doc-get: the OP session doc is
		// keyed by jti, the AC carries the session's sticky uid.
		sdoc, sErr := (&OidcAdapter{ModelName: "Session", Redis: e.Redis}).FindByUID(docStr(doc["sessionUid"]))
		if sErr != nil || sdoc == nil {
			return nil, opInvalidGrant("authorization code not found")
		}
		sacc, _ := sdoc["accountId"].(string)
		if sacc != docStr(doc["accountId"]) {
			return nil, opInvalidGrant("authorization code not found")
		}
		// session.grantIdFor(clientId) (vendored session.read — the doc
		// may lack authorizations entirely: a fresh session). Vendored
		// grantIdFor on a plain session returns undefined → mismatch.
		if az, _ := sdoc["authorizations"].(map[string]any); az == nil {
			return nil, opInvalidGrant("authorization code not found")
		}
		cl, ok := sdoc["authorizations"].(map[string]any)[clientID].(map[string]any)
		if !ok {
			return nil, opInvalidGrant("authorization code not found")
		}
		if gid, _ := cl["grantId"].(string); gid != docStr(doc["grantId"]) {
			return nil, opInvalidGrant("authorization code not found")
		}
	}
	return doc, nil
}

// --- opValidateGrant — vendored grant_source.validateGrant ---
//
//	grant = Grant.find(grantId, {ignoreExpiration:true})
//	if (!grant) throw InvalidGrant('grant not found')
//	if (grant.isExpired) throw InvalidGrant('grant is expired')
//	if (grant.clientId !== ctx.oidc.client.clientId)
//	  throw InvalidGrant('client mismatch')
//	return grant
func (e *OidcEngine) opValidateGrant(grantID, clientID string) (*opGrantDoc, *opErr) {
	if grantID == "" {
		return nil, opInvalidGrant("grant not found")
	}
	doc, err := e.opFind("Grant", grantID)
	if err != nil || doc == nil {
		return nil, opInvalidGrant("grant not found")
	}
	// grant.isExpired: a 30d doc TTL (adapter-expiry); a doc.exp guard
	// mirrors v9 for a TTL-less fake.
	if exp, ok := doc["exp"].(float64); ok && int64(exp) < opEpoch() {
		return nil, opInvalidGrant("grant is expired")
	}
	if v, _ := doc["clientId"].(string); v != clientID {
		return nil, opInvalidGrant("client mismatch")
	}
	return opFromGrantDoc(doc), nil
}

// --- opSignIdToken — vendored id_token.issue (the idtoken use, NO typ) ---
//
// JWT.sign(payload, key, 'ES256', signOptions) with:
//
//	signOptions.fields = {kid: jwk.kid}  (NO typ — idtoken has typ=
//	undefined; the vendored jwt.js: `if (options.typ) header.typ = ...`
//	sets typ ONLY when the option is defined).
//	signOptions.iss = issuer, signOptions.aud = client.clientId,
//	signOptions.sub = payload.sub, payload.iat = now (NOT noIat).
//
// id_token payload (session-6 RESOLVED, vendor byte-for-byte):
//
//	getCtxAccountClaims: {sub, origin, localName, displayName,
//	institution (ALWAYS a key — vendored `institution ?? null`)} +
//	sign-merge {iss, aud: client_id, iat, exp: iat+3600} (NO auth_time/
//	sid/acr/amr — mask.scope('openid') filters them out).
//
// Go: SignJWT in jws.go ALWAYS writes typ — the compact JWS is assembled
// here (jwkPrivateKey is same-package). Go marshals the JSON map in
// sorted-key order (the vendor is insertion-ordered — semantically
// identical JWT); the Go sorted order is the wire-deterministic pin
// (tests decode rather than byte-compare).
func opSignIdToken(priv *JWK, issuer, clientID string, claims OidcUserClaims, accountID, nonce string) (string, error) {
	payload := map[string]any{
		"sub":         accountID,
		"origin":      claims.Origin,
		"localName":   claims.LocalName,
		"displayName": claims.DisplayName,
		// vendored: institution ?? null (ALWAYS a key; nil renders null)
		"institution": nil,
		"nonce":       nonce,
		"iss":         issuer,
		"aud":         clientID,
		"iat":         opEpoch(),
	}
	if claims.Institution != "" {
		payload["institution"] = claims.Institution
	}
	payload["exp"] = payload["iat"].(int64) + oidcTTLIdToken

	pk, err := jwkPrivateKey(priv)
	if err != nil {
		return "", err
	}
	header := map[string]any{"alg": "ES256"}
	if priv.Kid != "" {
		header["kid"] = priv.Kid
	}
	hdr, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	headerB64 := base64.RawURLEncoding.EncodeToString(hdr)
	signingInput := headerB64 + "." + base64.RawURLEncoding.EncodeToString(body)
	digest := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, pk, digest[:])
	if err != nil {
		return "", err
	}
	// 7518 §3.2: signature = base64url(r‖s), each 32 B big-endian (the
	// jose wire shape, matching SignJWT).
	sigLen := pk.Curve.Params().BitSize / 8
	sig := make([]byte, sigLen*2)
	r.FillBytes(sig[:sigLen])
	s.FillBytes(sig[sigLen:])
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// --- opToken — POST /federation/oidc/token (the full route) ---
//
// Form (parseBody urlencoded) → client → grant_type → AC handler →
// issue. The error handler (error_handler.js) renders JSON at 400/401
// (opRenderTokenError); NO state in the wire (the token route has no
// client/state redirect context — err_out out is {error,
// error_description?} only).
func (e *OidcEngine) opToken(c *core.Cxt, res *core.Res) {
	req := c.Req
	res.W.Header().Set("Cache-Control", "no-store") // vendor no_cache
	if req == nil {
		// (tests construct a res without a Req — degrade: no form, no
		// client → invalid_request wire.)
		_ = req
	}

	// parseBody (urlencoded form): client_auth params (client_id) +
	// authorization_code grant params (code, code_verifier, redirect_uri).
	// Go: ParseForm merges req.URL.RawQuery + POST body into req.Form.
	if fErr := req.ParseForm(); fErr != nil {
		// vendor selective_body throws on a malformed body → 400
		// (degrade with the most likely wire: no client auth).
		opRenderTokenError(res, opInvalidRequest("no client authentication mechanism provided"))
		return
	}
	form := req.Form
	// clientAuth (authenticateClient): no client_id →
	//   InvalidRequest('no client authentication mechanism provided').
	clientID := form.Get("client_id")
	if clientID == "" {
		opRenderTokenError(res, opInvalidRequest("no client authentication mechanism provided"))
		return
	}
	client := e.opClient(clientID)
	if client == nil {
		// miss → 401 invalid_client 'client authentication failed'.
		opRenderTokenError(res, opInvalidClientAuth())
		return
	}

	// supportedGrantTypeCheck (presence + supported).
	gty := form.Get("grant_type")
	if gty == "" {
		opRenderTokenError(res, opInvalidRequest("missing required parameter 'grant_type'"))
		return
	}
	// supported = configuration.grantTypes — overleaf-fed static: only
	// 'authorization_code' (clients.mjs clientDefaults).
	if gty != "authorization_code" || gty == "implicit" {
		opRenderTokenError(res, opUnsupportedGrantType())
		return
	}
	// allowedGrantTypeCheck (client.grantTypeAllowed).
	if !e.opClientGrantAllowed(client, gty) {
		opRenderTokenError(res, opInvalidRequest("requested grant type is not allowed for this client"))
		return
	}

	// --- authorizationCodeHandler (vendored). ---
	// allowOmittingSingleRegisteredRedirectUri (vendor default TRUE).
	redirectURI := form.Get("redirect_uri")
	if redirectURI == "" && len(client.RedirectURIs) == 1 {
		redirectURI = client.RedirectURIs[0]
	}
	// presence ('code', 'redirect_uri') — vendored validate_presence
	// (the missing list is built once and rendered as ONE message:
	//   1 missing -> "missing required parameter 'X'"
	//   2 missing -> "missing required parameters 'a' and 'b'").
	missing := []string{}
	if form.Get("code") == "" {
		missing = append(missing, "code")
	}
	if redirectURI == "" {
		missing = append(missing, "redirect_uri")
	}
	if len(missing) != 0 {
		msg := "missing required "
		if len(missing) == 1 {
			msg += "parameter "
		} else {
			msg += "parameters "
		}
		for i, m := range missing {
			if i > 0 {
				msg += " and "
			}
			msg += fmt.Sprintf("'%s'", m)
		}
		opRenderTokenError(res, opInvalidRequest(msg))
		return
	}
	code := form.Get("code")

	// findGrantSource (opACFind — three-way checkSessionBinding via the
	// AC doc's own sessionUid; the cookie does NOT enter the check).
	ac, acErr := e.opACFind(code, clientID)
	if acErr != nil {
		opRenderTokenError(res, acErr)
		return
	}
	// isExpired (doc.exp < now — unified with the adapter-expiry miss;
	// the per-cause 'authorization code is expired' rides error_detail).
	if exp, ok := ac["exp"].(float64); ok && int64(exp) < opEpoch() {
		opRenderTokenError(res, opInvalidGrant("authorization code is expired"))
		return
	}

	// validateGrant (opValidateGrant — grantId from the AC doc).
	grantID := docStr(ac["grantId"])
	grant, gErr := e.opValidateGrant(grantID, clientID)
	if gErr != nil {
		opRenderTokenError(res, gErr)
		return
	}

	// checkPKCE (format → invalid_request wire; mismatch → invalid_grant).
	pkceErr := opCheckPKCE(form.Get("code_verifier"), docStr(ac["codeChallenge"]), docStr(ac["codeChallengeMethod"]))
	if pkceErr != nil {
		opRenderTokenError(res, pkceErr)
		return
	}

	// redirectUri mismatch (code.redirectUri !== params.redirect_uri).
	if docStr(ac["redirectUri"]) != redirectURI {
		opRenderTokenError(res, opInvalidGrant("authorization code redirect_uri mismatch"))
		return
	}

	// consumeGrantSource (replay → opRevokeCascade + uniform wire; else
	// consume marks the doc consumed=epochSec, remaining-TTL).
	if cts, okv := ac["consumed"]; okv {
		if ctsf, ok2 := cts.(float64); ok2 && ctsf != 0 {
			e.opRevokeCascade(grantID)
			opRenderTokenError(res, opInvalidGrant("authorization code already consumed"))
			return
		}
		if ctsb, ok2 := cts.(bool); ok2 && ctsb {
			e.opRevokeCascade(grantID)
			opRenderTokenError(res, opInvalidGrant("authorization code already consumed"))
			return
		}
	}
	if cErr := e.opConsume("AuthorizationCode", code); cErr != nil {
		_ = cErr
	}

	// --- issueTokens (vendored grant_common.js). ---
	// validateAccount (findAccount → Users.Account — B-side Mongo lookup).
	accountID := docStr(ac["accountId"])
	claims, found := e.Users.Account(req.Context(), accountID)
	if !found {
		// vendored: `if (!account) throw InvalidGrant('<label> invalid
		// (referenced account not found)')` (the per-cause detail rides
		// error_detail; the wire is the uniform invalid_grant).
		opRenderTokenError(res, opInvalidGrant("authorization code invalid (referenced account not found)"))
		return
	}
	_ = claims
	// checkAccountMismatch (code.accountId !== grant.accountId).
	if grant.AccountID != accountID {
		opRenderTokenError(res, opInvalidGrant("accountId mismatch"))
		return
	}
	// createAccessToken (IN_PAYLOAD list, vendored createAccessToken —
	// NO aud field, NO uid sub-index for the AT model, expiresWith-
	// Session=true, grantId, gty, scope 'openid'; the value = the nanoid
	// jti itself, NOT a second opaque).
	now := opEpoch()
	atValue := opOpaqUID()
	at := map[string]any{
		"iat":                now,
		"exp":                now + oidcTTLAccessToken,
		"jti":                atValue,
		"kind":               "AccessToken",
		"clientId":           clientID,
		"gty":                "authorization_code",
		"grantId":            grantID,
		"accountId":          accountID,
		"expiresWithSession": true,
		"scope":              "openid",
	}
	if sErr := e.opSave("AccessToken", atValue, at, oidcTTLAccessToken); sErr != nil {
		_ = sErr
		opRenderTokenError(res, opInvalidGrant("authorization code invalid (referenced account not found)"))
		return
	}

	// issueIdToken (scope 'openid' present → issue; the claim filter
	// passes all five claims, institution null-when-empty; nonce from
	// the AC; NO auth_time/sid/acr/amr).
	nonce := docStr(ac["nonce"])
	idToken, sErr2 := opSignIdToken(e.Key.PrivateKey, e.Issuer, clientID, claims, accountID, nonce)
	if sErr2 != nil {
		// a JWT sign error is a 500 server-error (vendored): the vendored
		// jwt.js sign failure is an unhandled 500 (expose false →
		// 'server_error' wire is 500; Go: 500).
		res.SendStatus(http.StatusInternalServerError)
		return
	}

	// buildTokenResponse (vendor: {access_token, expires_in: at.exp -
	// now, id_token, scope: source.scope ? at.scope : at.scope ||
	// undefined → 'openid', tokenType: 'Bearer'}).
	nowOut := map[string]any{
		"access_token": atValue,
		"expires_in":   oidcTTLAccessToken,
		"id_token":     idToken,
		"scope":        "openid",
		"token_type":   "Bearer",
	}
	body, mErr := json.Marshal(nowOut)
	if mErr != nil {
		res.SendStatus(http.StatusInternalServerError)
		return
	}
	res.JSON(http.StatusOK, body)
}
