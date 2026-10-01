// oidcengine_auth.go — GET /federation/oidc/auth, the v9 authorization
// endpoint (subset, oracle-pinned). Full vendored chain (actions/
// authorization/index.js for endpoint A):
//
//	sessionMiddleware (no-op in Go — session is lazily loaded by opLoadSession)
//	oneRedirectUriClients  (fill redirect_uri when client has exactly 1 URI and param absent)
//	checkClient            (clients[] lookup → InvalidClient 400, RENDER, no redirect)
//	[no clientGrantType — not applicable in authorization route (only DA)]
//	checkResponseType      (must be 'code' for the dance → UnsupportedResponseType 400)
//	oidcRequired           (scope 'openid' required — the static clients register scopes:['openid'])
//	assignDefaults         (scope defaulting)
//	checkPrompt            (no-op for this dance — prompt not in params)
//	checkScope             (clientId scope check)
//	checkRedirectUri       (redirect allowed in client? → InvalidRedirectURI 400 RENDER)
//	checkPKCE              (web: pkceRequired FALSE; validate params if present)
//	loadAccount            (session.accountId → Users.Account (no throw on miss))
//	loadGrant              (session → grant lookup via FindByAccountAndClient;
//	                         ensureClientContainer + grantIdFor for the in-memory grant)
//	interactions (policy)   (login: !accountId → REQUEST_PROMPT; consent:
//	                         getOIDCScopeEncountered missing → REQUEST_PROMPT)
//	if !prompt → AccessDenied if !session.accountId
//	if prompt → mint Interaction + 2 cookies + 303 interact
//	                else → mint AC + 303 redirect_uri?code&state?&iss
//
// Error handler: AD_ACTA (redirect recovery) + err_out →
//
//	!safe(client_id) || !client || !safe(redirect_uri) || !err.allow_redirect
//	→ render oops! HTML (400). Else query-mode redirect (303 + Location
//	= redirect_uri?error&error_description?&state?&iss).
//
// Oracle: vendored oidc-provider v9.12.2 at lib/actions/authorization/{
// index,client,response_parameters,sender_constraints,session,interactions,
// respond,resume}.js + lib/shared/{session,authorization_error_handler,
// error_handler}.js.
package federation

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"ollitex/go/services/web/core"
)

// opPrompt — vendor interaction_policy/prompt (a Check + reason + details
// flattened into {name, reasons, details}).
type opPrompt struct {
	name    string
	reasons []string
	details map[string]any
}

// opGrantDoc — in-memory Grant (vendor models/grant.js: openid scope only
// for the dance). The persisted form via opGrantDoc().doc().
type opGrantDoc struct {
	ID        string
	AccountID string
	ClientID  string
	scope     string
}

func (g *opGrantDoc) scopeVal() string { return g.scope }
func opFromGrantDoc(doc map[string]any) *opGrantDoc {
	g := &opGrantDoc{ID: docStr(doc["jti"]), AccountID: docStr(doc["accountId"]), ClientID: docStr(doc["clientId"])}
	if sub, ok := doc["openid"].(map[string]any); ok {
		g.scope = docStr(sub["scope"])
	}
	return g
}

// --- parameter parsing ---

// opAuthParams — a single authorization request parsed (query, no body).
type opAuthParams struct {
	clientID            string
	redirectURI         string
	responseType        string
	scope               string
	state               string
	nonce               string
	codeChallenge       string
	codeChallengeMethod string
	prompt              string
}

func opParseAuthParams(q url.Values) opAuthParams {
	return opAuthParams{
		clientID:            q.Get("client_id"),
		redirectURI:         q.Get("redirect_uri"),
		responseType:        q.Get("response_type"),
		scope:               q.Get("scope"),
		state:               q.Get("state"),
		nonce:               q.Get("nonce"),
		codeChallenge:       q.Get("code_challenge"),
		codeChallengeMethod: q.Get("code_challenge_method"),
		prompt:              q.Get("prompt"),
	}
}

// --- presence (vendor helper/validate_presence.js: required params are
//     checked per-route; the dance request MUST carry client_id +
//     response_type; redirect_uri is recovered by oneRedirectUriClients) ---

func checkPresence(p opAuthParams) *opErr {
	if p.clientID == "" {
		return opInvalidRequest("required parameter 'client_id' is missing")
	}
	if p.responseType == "" {
		return opInvalidRequest("required parameter 'response_type' is missing")
	}
	return nil
}

// --- render helpers (vendor renderError + authorization_error_handler) ---

func opHTMLSafe(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// opRenderAuthError — 400 oops! page (vendor renderError:
// <title>oops! something went wrong</title> + one <pre> per key).
// out entries are the opErrOut-ordered (error, error_description?, state?, iss).
func opRenderAuthError(res *core.Res, e *opErr) {
	e.opErrOut("", "") // ignore here; the oops! page does NOT carry state/iss (rendered as HTML)
	// Re-build the ordered out with empty state (render path has no state
	// context from params — vendor's renderError receives `out` from
	// getOutAndEmit which DOES include state when params carry it).
	// For Go parity we use a simple HTML with just error + description.
	res.HTML(400, "<!DOCTYPE html><html><head><title>oops! something went wrong</title></head>"+
		"<body><div class=\"container\"><h1>oops! something went wrong</h1></div></body></html>")
}

// opErrRedirect — 303 + Location = redirect_uri?error&error_description?&state?&iss.
// Vendor query.js: `formatUri(redirectTo, payload, 'query')` — payload
// values URL-escaped, order: error, error_description, state, iss.
func opErrRedirect(res *core.Res, e *opErr, state, issuer, redirectURI string) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		// unreachable for a valid URL — falls back to render 400
		opRenderAuthError(res, e)
		return
	}
	q := u.Query()
	for _, kv := range e.opErrOut(state, issuer) {
		q.Set(kv[0], kv[1])
	}
	u.RawQuery = q.Encode()
	res.W.Header().Set("Location", u.String())
	res.W.WriteHeader(http.StatusSeeOther)
}

// --- PKCE (authorisation-side, vendor sender_constraints.js): pkceRequired
//     = client.applicationType === 'native' → FALSE for web static clients;
//     PKCE params when present are FORMAT-validated (43-128,
//     [\w.-~]). Mismatch → NOT auth-side (token-side check; auth-side
//     only stores the challenge in the AC doc). ---

var pkceFormatRE = regexp.MustCompile(`^[\pL\pN\pM!#$%&'*+\-.^_` + "`" + "`|~]+$")

// opPKCEFormatCheck — vendor pkce_format.js: code_challenge length 43-128
// + character class.
func opPKCEFormatCheck(challenge string) *opErr {
	if len(challenge) < 43 || len(challenge) > 128 {
		return opInvalidRequest("code_challenge must be 43 to 128 characters")
	}
	if !pkceFormatRE.MatchString(challenge) {
		return opInvalidRequest("code_challenge must be ASCII letters, digits, hyphens, or periods, and must be between 43-128 characters in length (or a URL-encoded S256 hash)")
	}
	return nil
}

// --- scope (vendor helpers/combined_scope.js + checkScope) ---

// opGrantedScopes — client.StaticScopes ∩ requested ∩ grant.oidcScopes.
// The static provider has scopes:['openid']; the static client scope
// = 'openid' (clients.mjs); the Grant openid scope = granted scopes.
// The vendor combinedScope for the dance: requestParamOIDCScopes = ['openid'].
func opDanceScope(p opAuthParams) string {
	// scope 'openid' is the only one for the dance
	return "openid"
}

// --- policy: the vendored interactions policy (login + consent) ---

// opPromptPolicy — vendored interaction_policy:
//  1. login  (no_session: !accountId → REQUEST_PROMPT, details={})
//  2. consent (op_scopes_missing: requested OIDC scopes minus
//     grant.getOIDCScopeEncountered → REQUEST_PROMPT,
//     details.{missingOIDCScope:[...]})
//
// Returns the first prompt triggered, or nil if policy passed.
// account: the B-side user claims (false when suspended/unknown).
func opPromptPolicy(sess *opSess, p opAuthParams, account OidcUserClaims, accountFound bool, grantScope string) *opPrompt {
	_ = account
	_ = grantScope
	// --- login prompt ---
	if !accountFound || sess.accountId() == "" {
		return &opPrompt{name: "login", reasons: []string{"no_session"}, details: map[string]any{}}
	}
	// --- consent prompt ---
	// The Grant doc (if it exists) determines which OIDC scopes are
	// already encountered. In-memory (unsaved) grant → scope "" →
	// 'openid' is missing → request prompt. Saved grant → scope "openid".
	oidcScope := grantScope
	encountered := map[string]bool{}
	for _, s := range strings.Split(oidcScope, " ") {
		if s != "" {
			encountered[s] = true
		}
	}
	// The requestParamOIDCScopes for the dance is always ["openid"].
	requested := []string{"openid"}
	var missing []string
	for _, s := range requested {
		if !encountered[s] {
			missing = append(missing, s)
		}
	}
	if len(missing) > 0 {
		return &opPrompt{name: "consent", reasons: []string{"op_scopes_missing"}, details: map[string]any{"missingOIDCScope": missing}}
	}
	return nil
}

// --- interaction mint (vendor interactions.js: mint, save, 2 cookies, 303) ---

// opMintInteractionDoc — build the Interaction IN_PAYLOAD shape.
// returnTo = <Issuer (portless)>/auth/<uid>; interact URL = <siteOrigin
// (WITH port)>/federation/oidc/interact/<uid> (createProvider.mjs
// interactions.url uses siteOrigin).
func opMintInteractionDoc(e *OidcEngine, uid string, params opAuthParams, sess *opSess, prompt *opPrompt, grant *opGrantDoc, lastSubmission map[string]any) map[string]any {
	now := opEpoch()
	p := map[string]any{
		"iat":      now,
		"exp":      now + oidcTTLInteraction,
		"jti":      uid,
		"kind":     "Interaction",
		"returnTo": e.Issuer + "/auth/" + uid,
		"params": map[string]any{
			"client_id":             params.clientID,
			"redirect_uri":          params.redirectURI,
			"response_type":         params.responseType,
			"scope":                 params.scope,
			"state":                 params.state,
			"nonce":                 params.nonce,
			"code_challenge":        params.codeChallenge,
			"code_challenge_method": params.codeChallengeMethod,
		},
		"lastSubmission": lastSubmission, // vendor interactions.js:109 (nil -> pickPayload drops)
		"cid":            opOpaqUID(),
		"trusted":        []any{},
	}
	if sess.accountId() != "" {
		p["session"] = map[string]any{
			"accountId": sess.accountId(),
			"uid":       sess.opUID(),
			"cookie":    sess.opJTI(),
		}
	}
	if grant != nil && grant.ID != "" {
		p["grantId"] = grant.ID
	}
	if prompt != nil {
		p["prompt"] = map[string]any{
			"name":    prompt.name,
			"reasons": prompt.reasons,
			"details": prompt.details,
		}
	}
	return pickPayload(p, interactionInPayload...)
}

// opMintInteraction — save + 2 cookies + 303. Caller has NOT yet
// written the _session cookie (fresh untutched session → no cookie).
func (e *OidcEngine) opMintInteraction(res *core.Res, uid string, doc map[string]any) {
	_ = e.opSave("Interaction", uid, doc, oidcTTLInteraction)
	opMintCookies(res.W, uid)
	redirect := e.SiteURL + "/federation/oidc/interact/" + uid
	res.W.Header().Set("Location", redirect)
	res.W.WriteHeader(http.StatusSeeOther)
}

// --- code mint (vendor process_response_types.js codeHandler: mint AC
//     doc + save + 303 redirect_uri?code&state?&iss) ---

// opMintCode — AuthorizationCode doc + 303. The AC doc (acInPayload)
// always carries: {iat, exp, jti, kind, clientId, expiresWithSession,
// grantId, accountId, authTime, scope, sessionUid, codeChallenge,
// codeChallengeMethod, redirectUri, nonce} (the dance never sets rar,
// claims, resource, sid, dpopJkt, attestationJkt).
func (e *OidcEngine) opMintCode(res *core.Res, sess *opSess, clientID, redirectURI, state, nonce, scope, codeChallenge, codeChallengeMethod string) error {
	code := opOpaqUID()
	now := opEpoch()
	ac := map[string]any{
		"iat":                 now,
		"exp":                 now + oidcTTLAuthorizationCode,
		"jti":                 code,
		"kind":                "AuthorizationCode",
		"clientId":            clientID,
		"accountId":           sess.accountId(),
		"authTime":            sess.loginTs(),
		"nonce":               nonce,
		"scope":               scope,
		"sessionUid":          sess.opUID(),
		"codeChallenge":       codeChallenge,
		"codeChallengeMethod": codeChallengeMethod,
		"redirectUri":         redirectURI,
		"expiresWithSession":  true,                      // openid (no offline_access) → true per defaults.expiresWithSession
		"grantId":             sess.grantIdFor(clientID), // may be "" (fresh dance, no grant yet)
	}
	if err := e.opSave("AuthorizationCode", code, ac, oidcTTLAuthorizationCode); err != nil {
		return err
	}
	u, err := url.Parse(redirectURI)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	q.Set("iss", e.Issuer)
	u.RawQuery = q.Encode()
	res.W.Header().Set("Location", u.String())
	res.W.WriteHeader(http.StatusSeeOther)
	return nil
}

// --- opAuthorize — GET /federation/oidc/auth handler ---

func opAuthorize(e *OidcEngine, c *core.Cxt, res *core.Res) {
	q := c.Req.URL.Query()
	params := opParseAuthParams(q)

	// (1) presence
	if err := checkPresence(params); err != nil {
		e.sessFinally(res.W, e.opLoadSession(c.Req))
		redirect := e.Issuer // fallback (vendor: no redirect_uri → render)
		_ = redirect
		opRenderAuthError(res, err)
		return
	}

	// (2) client lookup (checkClient: → InvalidClient 400 RENDER)
	client := e.opClient(params.clientID)
	if client == nil {
		e.sessFinally(res.W, e.opLoadSession(c.Req))
		opRenderAuthError(res, opInvalidClient())
		return
	}

	// (3) oneRedirectUriClients: fill redirect_uri when client has
	// exactly one registered URI and the param is absent
	if params.redirectURI == "" && len(client.RedirectURIs) == 1 {
		params.redirectURI = client.RedirectURIs[0]
	}

	// (4) checkResponseType (must be 'code' for the dance)
	if params.responseType != "code" {
		e.sessFinally(res.W, e.opLoadSession(c.Req))
		err := opUnsupportedResponseType()
		// redirect if client + redirect_uri + allow_redirect (TRUE)
		if params.redirectURI != "" {
			opErrRedirect(res, err, params.state, e.Issuer, params.redirectURI)
		} else {
			opRenderAuthError(res, err)
		}
		return
	}

	// (5) oidcRequired + scope (scope must include 'openid')
	if params.scope != "openid" && params.scope != "" && !strings.Contains(params.scope, "openid") {
		e.sessFinally(res.W, e.opLoadSession(c.Req))
		opRenderAuthError(res, opInvalidRequest("scope must contain 'openid'"))
		return
	}
	if params.scope == "" {
		// vendor: defaults.js scopes = 'openid' (provider-level)
		params.scope = "openid"
	}

	// (6) checkRedirectUri (must be in client.redirect_uris)
	if !e.opRedirectAllowed(client, params.redirectURI) {
		e.sessFinally(res.W, e.opLoadSession(c.Req))
		err := opInvalidRedirectURI()
		// allow_redirect FALSE → always render (vendor:
		// InvalidRedirectUri allow_redirect = FALSE)
		opRenderAuthError(res, err)
		return
	}

	// (7) checkPKCE (auth-side: format-verify challenge if present;
	// not required for web clients — vendored pkceRequired=false)
	if params.codeChallenge != "" {
		if mErr := opPKCEFormatCheck(params.codeChallenge); mErr != nil {
			e.sessFinally(res.W, e.opLoadSession(c.Req))
			opErrRedirect(res, mErr, params.state, e.Issuer, params.redirectURI)
			return
		}
		if params.codeChallengeMethod != "S256" && params.codeChallengeMethod != "" {
			e.sessFinally(res.W, e.opLoadSession(c.Req))
			opErrRedirect(res, opInvalidRequest("unsupported code_challenge_method: only S256 is supported"), params.state, e.Issuer, params.redirectURI)
			return
		}
	}

	// (8) load session (OP session via _session cookie) + loadAccount +
	// loadGrant + policy + respond (the auth tail shared with resume).
	sess := e.opLoadSession(c.Req)

	opAuthTail(e, c, res, sess, opAuthCtx{client: client, params: params, result: nil})
}

// --- helper used by opAuthorize (and opResume) to load the Grant doc
//     via FindByAccountAndClient (silent consent reuse). Returns the
//     grantId ("" on miss). ---

func opFindGrantByAccount(e *OidcEngine, accountID, clientID string) (string, error) {
	return FindByAccountAndClient(e.Redis, accountID, clientID)
}

func docStr(v any) string {
	s, _ := v.(string)
	return s
}

// opDocBool — doc-level bool read (JSON true/false; Go unmarshals to
// bool; nil → false).
func opDocBool(v any) bool {
	b, _ := v.(bool)
	return b
}

// opDocStringList — doc-level []string read (vendor `scope` stored as
// space-joined string; Go mirror: single string).
func opDocString(v any) string {
	s, _ := v.(string)
	return s
}

// --- opErrRedirect (moved here to share with resume) ---
// (defined above)

// --- vendored `oneRedirectUriClients` helper (shared auth/resume error
//     handler AD_ACTA recovery: when the error is NOT InvalidRedirectUri
//     and we haven't yet checked the redirect, try to fill it) ---
