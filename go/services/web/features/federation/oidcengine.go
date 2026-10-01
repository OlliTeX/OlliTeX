// oidcengine.go — B-side OIDC provider engine (oidc-provider v9.12.2
// subset, oracle-pinned). Core file: engine struct + per-request build,
// v9 nanoid opaque ids, the error wire (errors.js / err_out.js), the OP
// cookie wire, adapter doc ops, the IN_PAYLOAD key lists, and the
// engine-internal session doc model (vendored models/session.js).
//
// Sub-files (user-requested split, each compiles against this core):
//
//	oidcengine_auth.go    GET /federation/oidc/auth (checks + policy + code mint)
//	oidcengine_resume.go  GET /federation/oidc/auth/<uid> (resume)
//	oidcengine_bridge.go  GET /interact/<uid> + POST consent/deny + MongoOidcUserSource
//	oidcengine_token.go   POST /token (PKCE, AT mint, id_token, replay cascade)//
//
// Oracle pins (vendored v9.12.2 at node-oidc-provider/lib/ + overleaf-fed
// modules/federation/oidc/{createProvider,bridge,clients}.mjs):
//
//   - id: nanoid 43 chars (formats.bitsOfOpaqueRandomness 256/6 bits,
//     helpers/nanoid.js customAlphabet) — ALL opaque ids (session jti/uid,
//     interaction uid, code, AT value, cid)
//   - cookie names (provider defaults): _session (Path=/federation/oidc,
//     expires attr, 8h), _interaction (Path=/federation/oidc/interact/<uid>,
//     Max-Age 600), _interaction_resume (Path=/federation/oidc/auth/<uid>,
//     Max-Age 600)
//   - TTLs (createProvider.mjs wins over vendor): Session 8h, Interaction
//     600, Grant 30d, AuthorizationCode 120, AccessToken 3600 (default)
//   - wire: invalid_grant ALWAYS {error:'invalid_grant',
//     error_description:'grant request is invalid'} (per-cause message is
//     error_detail = LOG only); unknown client (token) 401
//     {error:'invalid_client', error_description:'client
//     authentication failed'}; unknown client (auth) 400
//     {error:'invalid_client', error_description:'client is invalid'}
//   - id_token: oidc key, header {alg:'ES256', kid} with NO typ
package federation

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"ollitex/go/services/web/core"
)

// --- v9 TTLs (createProvider.mjs overrides vendor defaults) ---

const (
	oidcTTLSession           = 8 * 3600 // Session 8h
	oidcTTLInteraction       = 600      // Interaction 600s
	oidcTTLGrant             = 30 * 86400
	oidcTTLAuthorizationCode = 120
	oidcTTLAccessToken       = 3600 // vendor default
	oidcTTLIdToken           = 3600 // vendor default
	oidcCookieInteractionTTL = 600  // cookie Max-Age (seconds; interactions.js: cookie maxAge = ttl*1000 ms)
)

// --- v9 nanoid (helpers/nanoid.js — customAlphabet, 64 symbols) ---

// nanoidAlphabet — verbatim from vendored helpers/nanoid.js.
const nanoidAlphabet = "useandom-26T198340PX75pxJACKVERYMINDBUSHWOLF_GQZbfghjklqvwyzrict"

// opOpaqUID — ONE opaque id: 43 symbols (nanoid(ceil(256/6))). Each 6
// random bits (byte & 0x3F) — v9's exact byte-preserving distribution.
func opOpaqUID() string {
	out := make([]byte, 43)
	buf := make([]byte, 43)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("federation: nanoid rand: %v", err))
	}
	for i := 0; i < 43; i++ {
		out[i] = nanoidAlphabet[buf[i]&0x3F]
	}
	return string(out)
}

func opEpoch() int64 { return time.Now().Unix() }

// --- the per-request engine ---

// OidcEngine — the B-side OP per request: Redis doc seam, B-side user
// source, the active oidc key (id_token signer), and the two site forms:
// Issuer (portless entity id + /federation/oidc, vendor urlFor) and the
// siteOrigin (Settings.siteUrl origin WITH port, createProvider.mjs
// interactions.url). Clients = the static clients[] rebuild (S1 oracle).
type OidcEngine struct {
	Redis    OidcRedisSeam
	Users    OidcUserSource
	Key      *FederationKey // oidc-purpose active key
	Issuer   string         // https://<b.fqdn>/federation/oidc (no port)
	SiteURL  string         // Settings.siteUrl (WITH port if present)
	Clients  []OidcProviderClient
	Provider string // "oidc" purpose marker
}

// oidcSiteOrigin — new URL(siteUrl).origin (WITH port if present).
func oidcSiteOrigin(siteURL string) string {
	u, err := url.Parse(siteURL)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// --- v9 error wire (helpers/errors.js OIDCProviderError + helpers/
// err_out.js) ---

// opErr — one OIDCProviderError. Code = wire `error` (err_out maps
// `err.message` to `error`); Description = `error_description` when set
// (absent otherwise); Status 0 -> 400. AllowRedirect = class default TRUE
// except InvalidRedirectUri (FALSE, errors.js). Expose = status < 500.
type opErr struct {
	Code          string
	Description   string
	AllowRedirect bool
	NoClient      bool
	Status        int
}

func (e *opErr) Error() string {
	if e.Description != "" {
		return e.Code + ": " + e.Description
	}
	return e.Code
}

// opInvalidRequest — E-factory invalid_request. Wire:
// {error:'invalid_request', error_description:<specific msg>} (the specific
// thrown description rides the wire).
func opInvalidRequest(description string) *opErr {
	return &opErr{Code: "invalid_request", Description: description, AllowRedirect: true}
}

// opInvalidGrant — InvalidGrant (class property description — wire is
// ALWAYS 'grant request is invalid'; per-cause message = error_detail,
// LOG ONLY).
func opInvalidGrant(detail string) *opErr {
	_ = detail
	return &opErr{Code: "invalid_grant", Description: "grant request is invalid", AllowRedirect: true}
}

// opAccessDenied — E('access_denied'): description only when explicitly
// passed (bridge deny: 'End-User denied consent'; policy denies: no
// description).
func opAccessDenied(description string) *opErr {
	return &opErr{Code: "access_denied", Description: description, AllowRedirect: true}
}

// opInvalidClientAuth — 401 (authenticateClient miss); wire fixed
// 'client authentication failed' (the per-cause detail is error_detail,
// log only).
func opInvalidClientAuth() *opErr {
	return &opErr{Code: "invalid_client", Description: "client authentication failed", AllowRedirect: true, Status: 401}
}

// opInvalidRedirectURI — 400, allow_redirect FALSE (render path).
func opInvalidRedirectURI() *opErr {
	return &opErr{Code: "invalid_redirect_uri", Description: "redirect_uri did not match any of the client's registered redirect_uris", AllowRedirect: false}
}

// opUnsupportedGrantType — 400 {error:'unsupported_grant_type',
// error_description:'unsupported grant_type requested'} (E factory class
// property — the E-factory default arg rides the wire).
func opUnsupportedGrantType() *opErr {
	return &opErr{Code: "unsupported_grant_type", Description: "unsupported grant_type requested", AllowRedirect: true}
}

// opUnsupportedResponseType — 400 {error:'unsupported_response_type',
// error_description:'unsupported response_type requested'} (E factory
// class property).
func opUnsupportedResponseType() *opErr {
	return &opErr{Code: "unsupported_response_type", Description: "unsupported response_type requested", AllowRedirect: true}
}

// opInvalidClient — checkClient miss: 400 {error:'invalid_client',
// error_description:'client is invalid'}; nocclient=true — the error
// handler cannot redirect (no client context; client.js sets
// ctx.oidc.nocclient = true).
func opInvalidClient() *opErr {
	return &opErr{Code: "invalid_client", Description: "client is invalid", NoClient: true}
}

// opErrOut — err_out.js (expose true here, all status < 500). Ordered
// keys: error, error_description?, state? (vendor spread order), then iss
// (getOutAndEmit adds it last — rides in BOTH redirect and render out).
func (e *opErr) opErrOut(state, issuer string) [][2]string {
	out := [][2]string{{"error", e.Code}}
	if e.Description != "" {
		out = append(out, [2]string{"error_description", e.Description})
	}
	if state != "" {
		out = append(out, [2]string{"state", state})
	}
	out = append(out, [2]string{"iss", issuer})
	return out
}

// opRenderTokenError — token-route error_handler: JSON
// {error, error_description?} at 400/401.
func opRenderTokenError(res *core.Res, e *opErr) {
	status := e.Status
	if status == 0 {
		status = 400
	}
	out := map[string]any{"error": e.Code}
	if e.Description != "" {
		out["error_description"] = e.Description
	}
	body, _ := json.Marshal(out)
	res.JSON(status, body)
}

// --- v9 cookie wire ---
//
// Go stdlib rendering (Go 1.27.1, probed): Expires field ->
// "Expires=... GMT"; MaxAge>0 -> "Max-Age=<n>" (MaxAge 0 renders NEITHER
// when Expires zero); MaxAge -1 -> "Max-Age: 0" (delete).

func opCookie(w http.ResponseWriter, name, value, path string, expires time.Time, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		Expires:  expires,
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// opMintCookies — interactions.js:120-135: BOTH cookies value=<uid> (the
// interaction jti), path-scoped, Max-Age 600s.
func opMintCookies(w http.ResponseWriter, uid string) {
	opCookie(w, "_interaction", uid, "/federation/oidc/interact/"+uid, time.Time{}, oidcCookieInteractionTTL)
	opCookie(w, "_interaction_resume", uid, "/federation/oidc/auth/"+uid, time.Time{}, oidcCookieInteractionTTL)
}

// opSetSessionCookie — shared/session.js finally: cookie is written ONLY
// when (!session.new || session.touched); value=jti, expires = session.exp
// (8h), Path=/federation/oidc. Go pin: Expires attr only (NO Max-Age).
func opSetSessionCookie(w http.ResponseWriter, jti string, expires time.Time) {
	opCookie(w, "_session", jti, "/federation/oidc", expires, 0)
}

// opClearResumeCookie — resume.js:103-108: cookies.set(null) renders
// "_interaction_resume=; Path=/federation/oidc/auth/<uid>; <delete attrs>".
// Go clear render (probed): value "" + MaxAge 0 + zero Expires renders
// NEITHER expiry attribute.
func opClearResumeCookie(w http.ResponseWriter, uid string) {
	opCookie(w, "_interaction_resume", "", "/federation/oidc/auth/"+uid, time.Time{}, 0)
}

func opCookieValue(r *http.Request, name string) string {
	if c, err := r.Cookie(name); err == nil {
		return c.Value
	}
	return ""
}

// --- doc ops (adapter, S4a) ---

func (e *OidcEngine) opSave(modelName, id string, payload map[string]any, ttlSec int64) error {
	a := &OidcAdapter{ModelName: modelName, Redis: e.Redis}
	return a.Upsert(id, payload, &ttlSec)
}

func (e *OidcEngine) opFind(modelName, id string) (map[string]any, error) {
	a := &OidcAdapter{ModelName: modelName, Redis: e.Redis}
	return a.Find(id)
}

func (e *OidcEngine) opDestroy(modelName, id string) error {
	a := &OidcAdapter{ModelName: modelName, Redis: e.Redis}
	return a.Destroy(id)
}

func (e *OidcEngine) opConsume(modelName, id string) error {
	a := &OidcAdapter{ModelName: modelName, Redis: e.Redis}
	return a.Consume(id)
}

// --- IN_PAYLOAD key lists (exact, vendored models) ---

var sessionInPayload = []string{"iat", "exp", "jti", "kind", "uid", "accountId", "acr", "amr", "loginTs", "transient", "state", "authorizations"}
var interactionInPayload = []string{"iat", "exp", "jti", "kind", "session", "params", "prompt", "result", "returnTo", "trusted", "grantId", "lastSubmission", "deviceCode", "cid", "parJti"}
var acInPayload = []string{"iat", "exp", "jti", "kind", "clientId", "consumed", "sessionUid", "expiresWithSession", "grantId", "attestationJkt", "accountId", "acr", "amr", "authTime", "claims", "nonce", "resource", "scope", "sid", "codeChallenge", "codeChallengeMethod", "redirectUri", "dpopJkt", "rar"}
var atInPayload = []string{"iat", "exp", "jti", "kind", "clientId", "gty", "grantId", "x5t#S256", "jkt", "sessionUid", "expiresWithSession", "accountId", "aud", "rar", "claims", "extra", "scope", "sid"}
var grantInPayload = []string{"accountId", "clientId", "resources", "openid", "rejected", "rar", "iat", "exp", "jti", "kind"}

// pickPayload — models/payload.js: known keys with non-nil values.
func pickPayload(src map[string]any, keys ...string) map[string]any {
	out := make(map[string]any, len(keys))
	for _, k := range keys {
		v, ok := src[k]
		if ok && v != nil {
			out[k] = v
		}
	}
	return out
}

// --- static client surface (oidcprovider.go OidcProviderClient) ---

// opClient — the static clients[] row for id (nil if absent).
func (e *OidcEngine) opClient(id string) *OidcProviderClient {
	return ProviderClientByID(e.Clients, id)
}

// opRedirectAllowed — vendored client.redirectUriAllowed(uri):
// redirect_uris.includes(uri) (string identity).
func (e *OidcEngine) opRedirectAllowed(c *OidcProviderClient, uri string) bool {
	if c == nil || uri == "" {
		return false
	}
	for _, r := range c.RedirectURIs {
		if r == uri {
			return true
		}
	}
	return false
}

// opClientGrantAllowed — vendored client.grantTypeAllowed(t): static
// clients register grant_types ['authorization_code'] (clients.mjs
// clientDefaults).
func (e *OidcEngine) opClientGrantAllowed(c *OidcProviderClient, t string) bool {
	return c != nil && t == "authorization_code"
}

// --- OP session doc (vendored models/session.js subset) ---
//
// Doc shape: {iat, exp: iat+8h, jti (nanoid), kind:'Session',
// uid (nanoid, STABLE across resetIdentifier), accountId?, loginTs?,
// transient?, authorizations?{client:{sid?,grantId?}}}.
//
// v9 semantics (shared/session.js Proxy + finally):
//   - cookie _session absent       -> FRESH session, new=true -> NO cookie
//     written unless touched (loginAccount / reset mark touched)
//   - cookie present, doc gone     -> instantiate({}) (NOT new) -> cookie
//     rewritten (fresh doc replaces it, TTL 8h)
//   - ANY Proxy prop set           -> touched (engine mirrors: opSess.touched)
//   - finally: (!new || touched) && !destroyed -> cookie + save (ttl 8h)

type opSess struct {
	doc     map[string]any
	newSess bool
	touched bool
}

func opSessDoc() map[string]any {
	now := opEpoch()
	return map[string]any{
		"iat":  now,
		"exp":  now + oidcTTLSession,
		"jti":  opOpaqUID(),
		"kind": "Session",
		"uid":  opOpaqUID(),
	}
}

// opLoadSession — Session.get (vendored shared flow): cookie -> find ->
// doc. Cookie present but doc missing/expired -> instantiate({}) (fresh
// doc, NOT new). Cookie absent -> fresh doc, new=true.
func (e *OidcEngine) opLoadSession(r *http.Request) *opSess {
	jti := opCookieValue(r, "_session")
	if jti == "" {
		return &opSess{doc: opSessDoc(), newSess: true}
	}
	doc, err := e.opFind("Session", jti)
	if err != nil || doc == nil {
		return &opSess{doc: opSessDoc()}
	}
	return &opSess{doc: doc}
}

func (s *opSess) accountId() string {
	v, _ := s.doc["accountId"].(string)
	return v
}

func (s *opSess) opUID() string {
	v, _ := s.doc["uid"].(string)
	return v
}

func (s *opSess) opJTI() string {
	v, _ := s.doc["jti"].(string)
	return v
}

func (s *opSess) loginTs() int64 {
	f, _ := s.doc["loginTs"].(float64)
	return int64(f)
}

// grantIdFor — session.grantIdFor(clientId) (read).
func (s *opSess) grantIdFor(clientID string) string {
	a, _ := s.doc["authorizations"].(map[string]any)
	if a == nil {
		return ""
	}
	c, _ := a[clientID].(map[string]any)
	if c == nil {
		return ""
	}
	g, _ := c["grantId"].(string)
	return g
}

// grantIdForSet — session.grantIdFor(clientId, value) (set, marks
// touched) + ensureClientContainer (sid mint when absent).
func (s *opSess) grantIdForSet(clientID, grantID string) {
	a, _ := s.doc["authorizations"].(map[string]any)
	if a == nil {
		a = map[string]any{}
	}
	c, _ := a[clientID].(map[string]any)
	if c == nil {
		c = map[string]any{}
	}
	if sid, ok := c["sid"].(string); !ok || sid == "" {
		c["sid"] = opOpaqUID()
	}
	c["grantId"] = grantID
	a[clientID] = c
	s.doc["authorizations"] = a
	s.touched = true
}

// loginAccount — session.loginAccount({accountId, loginTs}) (marks
// touched).
func (s *opSess) loginAccount(accountID string, ts int64) {
	s.doc["accountId"] = accountID
	s.doc["loginTs"] = ts
	s.touched = true
}

// resetIdentifier — session.resetIdentifier(): NEW jti, SAME uid, vendor
// Object.assign(this, {id: nanoid}) — accountId/loginTs/authorizations
// SURVIVE (they ride the save payload); old jti doc destroyed first
// (base_model.save: destroy oldId before write).
func (e *OidcEngine) resetIdentifier(s *opSess) {
	old := s.opJTI()
	_ = e.opDestroy("Session", old)
	newDoc := map[string]any{
		"iat":  opEpoch(),
		"exp":  opEpoch() + oidcTTLSession,
		"jti":  opOpaqUID(),
		"kind": "Session",
		"uid":  s.opUID(),
	}
	if a := s.accountId(); a != "" {
		newDoc["accountId"] = a
	}
	if ts := s.loginTs(); ts != 0 {
		newDoc["loginTs"] = ts
	}
	if ts, ok := s.doc["transient"]; ok {
		newDoc["transient"] = ts
	}
	if st, ok := s.doc["state"]; ok {
		newDoc["state"] = st
	}
	if az, ok := s.doc["authorizations"].(map[string]any); ok && len(az) > 0 {
		newDoc["authorizations"] = az
	}
	s.doc = newDoc
	s.touched = true
}

// sessFinally — shared/session.js finally: (!new || touched) && !destroyed
// -> cookie rewrite + save (8h TTL). A fresh UNTOUCHED session emits NO
// _session cookie and NO save.
func (e *OidcEngine) sessFinally(w http.ResponseWriter, s *opSess) {
	if s.newSess && !s.touched {
		return
	}
	opSetSessionCookie(w, s.opJTI(), time.Unix(s.docExp(), 0))
	_ = e.opSave("Session", s.opJTI(), s.doc, oidcTTLSession)
}

func (s *opSess) docExp() int64 {
	if f, ok := s.doc["exp"].(int64); ok {
		return f
	}
	if f, ok := s.doc["exp"].(float64); ok {
		return int64(f)
	}
	_ = s.doc["exp"]
	return opEpoch() + oidcTTLSession
}
