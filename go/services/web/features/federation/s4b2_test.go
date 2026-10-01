// S4b-2/3 engine battery (a)–(h) + the full OIDC dance (login +
// consent + token), end-to-end over fake Redis + fake user source,
// cookie-jar mirroring live-smoke. Oracle: vendored oidc-provider
// v9.12.2 wire (sessions 5/6/13/14 HANDOFF pins).
//
//	(a) two consent forms in renderConsent (consent.pug allow + deny)
//	(b) code-mint 303 -> redirect_uri?code&state&iss (nonce NOT carried)
//	(c) replay cascade (AT + AC + Grant destroy) + uniform invalid_grant
//	(d) PKCE S256 mismatch -> invalid_grant (wire) / format -> invalid_request
//	(e) silent grant reuse (loadExistingGrant, NO re-consent)
//	(f) fresh GET /auth = NO _session cookie vs resume WRITEs it
//	(g) cookie paths: _session /federation/oidc; _interaction
//	   /federation/oidc/interact/<uid>; _interaction_resume auth/<uid>
//	(h) unknown client = RENDER 400 oops! + the deny-redirect wire
package federation

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"ollitex/go/services/web/core"
)

const (
	aliceB  = "0123456789abcdef01234567"
	sallyB  = "fedcba543210abcdef01234567"
	bOrigin = "b.example.org"
	aClient = "urn:overleaf-federation:client:a.example.net"
	aCB     = "https://a.example.net/federation/oidc/rp/callback"
)

// fakeOidcUsers — OidcUserSource (createProvider.mjs findAccount
// equivalent): sub -> OidcUserClaims; suspended/unknown -> false.
type fakeOidcUsers struct {
	claims map[string]OidcUserClaims
	known  map[string]bool
}

func (f *fakeOidcUsers) Account(_ context.Context, userID string) (OidcUserClaims, bool) {
	if !f.known[userID] {
		return OidcUserClaims{}, false
	}
	return f.claims[userID], true
}

func newFakeOidcUsers() *fakeOidcUsers {
	return &fakeOidcUsers{
		claims: map[string]OidcUserClaims{
			aliceB: {Origin: bOrigin, LocalName: "alice@" + bOrigin, DisplayName: "Alice B"},
			sallyB: {Origin: bOrigin, LocalName: "sally@" + bOrigin, DisplayName: "Sally B", Institution: "B Inc"},
		},
		known: map[string]bool{aliceB: true, sallyB: true},
	}
}

// bLoggedInSess — the B-visitor session (c.Sess) for a logged-in dance
// user (passport.user._id + SessID drive IsLoggedIn/UserIDHex).
func bLoggedInSess(id string) *core.Session {
	return &core.Session{
		SessID: "testsessid00000000000000000000000000",
		Doc: map[string]json.RawMessage{
			"passport": json.RawMessage(`{"user":{"_id":"` + id + `"}}`),
		},
	}
}

// opDanceEngine — the production-shaped engine over the fake seams.
func opDanceEngine(r *fakeOidcRedis, users *fakeOidcUsers, key *FederationKey) *OidcEngine {
	return &OidcEngine{
		Redis:    r,
		Users:    users,
		Key:      key,
		Issuer:   "https://" + bOrigin + "/federation/oidc",
		SiteURL:  "https://" + bOrigin,
		Clients:  []OidcProviderClient{{ClientID: aClient, RedirectURIs: []string{aCB}, Scope: "openid"}},
		Provider: oidcPurpose,
	}
}

func danceEngine(t *testing.T) (*fakeOidcRedis, *fakeOidcUsers, *OidcEngine) {
	pub, priv, err := GenerateES256()
	if err != nil {
		t.Fatal(err)
	}
	key := &FederationKey{Purpose: oidcPurpose, Kid: pub.Kid, PublicKey: pub, PrivateKey: priv, State: "active"}
	r := newFakeOidcRedis()
	return r, newFakeOidcUsers(), opDanceEngine(r, newFakeOidcUsers(), key)
}

func tokenReq(form url.Values) *http.Request {
	rq := httptest.NewRequest("POST", "/federation/oidc/token", strings.NewReader(form.Encode()))
	rq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return rq
}

func opAuthDanceURL() string {
	return "/federation/oidc/auth?client_id=" + aClient +
		"&redirect_uri=" + aCB + "&response_type=code&scope=openid&state=st&nonce=nn"
}

// --- cookie helpers (the recorder carries full Set-Cookie headers) ---

func cookieRawValue(w *httptest.ResponseRecorder, name string) string {
	for _, sc := range w.Header().Values("Set-Cookie") {
		if !strings.HasPrefix(sc, name+"=") {
			continue
		}
		rest := sc[len(name)+1:]
		if i := strings.IndexByte(rest, ';'); i >= 0 {
			return rest[:i]
		}
		return rest
	}
	return ""
}

func cookiePath(w *httptest.ResponseRecorder, name string) string {
	for _, sc := range w.Header().Values("Set-Cookie") {
		if !strings.HasPrefix(sc, name+"=") {
			continue
		}
		for _, attr := range strings.Split(sc[len(name)+1:], ";") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(attr), "Path="); ok {
				return v
			}
		}
	}
	return ""
}

func interactUID(r *httptest.ResponseRecorder) string {
	const pre = "https://" + bOrigin + "/federation/oidc/interact/"
	loc := r.Header().Get("Location")
	if !strings.HasPrefix(loc, pre) {
		return ""
	}
	uid := loc[len(pre):]
	if i := strings.IndexByte(uid, '?'); i >= 0 {
		uid = uid[:i]
	}
	return uid
}

// seedDocs — a live (Grant, Session, AC) triplet that passes opACFind's
// three-way checkSessionBinding (session uid sub-index + accountId +
// grantIdFor). The Session doc carries authorizations[aClient].grantId
// so the AC (expiresWithSession) resolves.
func seedDocs(t *testing.T, e *OidcEngine, gid, suid, sessJti, code string) {
	t.Helper()
	now := opEpoch()
	ttlGrant := int64(86400)
	ttl := int64(120)
	_ = (&OidcAdapter{ModelName: "Grant", Redis: e.Redis}).Upsert(gid, map[string]any{
		"iat": now, "exp": now + 86400, "jti": gid, "kind": "Grant",
		"accountId": aliceB, "clientId": aClient,
		"openid": map[string]any{"scope": "openid"},
	}, &ttlGrant)
	// The Session doc carries a `uid` (the opACFind checkSessionBinding
	// resolves Session.findByUid(ac.sessionUid) via the sub:<uid> index —
	// the adapter writes sub:<uid> only when the payload has a `uid` key).
	sessTTL := int64(8 * 3600)
	_ = (&OidcAdapter{ModelName: "Session", Redis: e.Redis}).Upsert(sessJti, map[string]any{
		"iat": now, "exp": now + 8*3600, "jti": sessJti, "kind": "Session",
		"uid": suid, "accountId": aliceB,
		"authorizations": map[string]any{aClient: map[string]any{"grantId": gid, "sid": "sid1"}},
	}, &sessTTL)
	_ = (&OidcAdapter{ModelName: "AuthorizationCode", Redis: e.Redis}).Upsert(code, map[string]any{
		"iat": now, "exp": now + 120, "jti": code, "kind": "AuthorizationCode",
		"clientId": aClient, "accountId": aliceB, "authTime": 0, "nonce": "nn",
		"scope": "openid", "sessionUid": suid, "expiresWithSession": true,
		"grantId": gid, "redirectUri": aCB,
	}, &ttl)
}

func s256sum(s string) string {
	sum := sha256.Sum256([]byte(s))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// --- (f)/(g) the cookie lifecycle ---

// TestOidcBatteryFG — (f) a fresh GET /auth emits NO _session cookie
// (untouched opSess); (g) the 303 mint carries the Path-scoped
// interaction pair; the resume WRITEs _session (Path=/federation/oidc).
func TestOidcBatteryFG(t *testing.T) {
	_, _, e := danceEngine(t)

	// 1. fresh GET /auth -> 303 interact/<uid1>, NO _session cookie.
	w1 := httptest.NewRecorder()
	opAuthorize(e, &core.Cxt{Req: httptest.NewRequest("GET", opAuthDanceURL(), nil)}, &core.Res{W: w1})
	if w1.Code != http.StatusSeeOther {
		t.Fatalf("1: %d want 303: loc %s body %s", w1.Code, w1.Header().Get("Location"), w1.Body.String())
	}
	uid1 := interactUID(w1)
	if uid1 == "" {
		t.Fatalf("1: no interact uid in Location %q", w1.Header().Get("Location"))
	}
	if got := cookieRawValue(w1, "_session"); got != "" {
		t.Fatalf("(f) fresh GET must NOT emit _session: %v", w1.Header().Values("Set-Cookie"))
	}
	if got := cookieRawValue(w1, "_interaction"); got != uid1 {
		t.Fatalf("(g) cookie _interaction: want %q got %q", uid1, got)
	}
	if got := cookieRawValue(w1, "_interaction_resume"); got != uid1 {
		t.Fatalf("(g) cookie _interaction_resume: want %q got %q", uid1, got)
	}
	if got := cookiePath(w1, "_interaction"); got != "/federation/oidc/interact/"+uid1 {
		t.Fatalf("(g) _interaction Path: want %q got %q (cookies %v)", "/federation/oidc/interact/"+uid1, got, w1.Header().Values("Set-Cookie"))
	}
	if got := cookiePath(w1, "_interaction_resume"); got != "/federation/oidc/auth/"+uid1 {
		t.Fatalf("(g) _interaction_resume Path: want %q got %q", "/federation/oidc/auth/"+uid1, got)
	}

	// 2. B logged-in GET bridge -> finishLogin 303 (auth/<uid1>).
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/federation/oidc/interact/"+uid1, nil)
	req2.AddCookie(&http.Cookie{Name: "_interaction", Value: uid1})
	e.opBridgeGet(&core.Cxt{Req: req2, Sess: bLoggedInSess(aliceB)}, &core.Res{W: w2})
	if w2.Code != http.StatusSeeOther || !strings.Contains(w2.Header().Get("Location"), "/federation/oidc/auth/"+uid1) {
		t.Fatalf("2 finishLogin: %d loc %s", w2.Code, w2.Header().Get("Location"))
	}

	// 3. resume -> login applied -> op_scopes_missing -> WRITE
	// _session (loginAccount touched) + mint consent interact/<uid2>.
	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest("GET", "/federation/oidc/auth/"+uid1, nil)
	req3.AddCookie(&http.Cookie{Name: "_interaction", Value: uid1})
	req3.AddCookie(&http.Cookie{Name: "_interaction_resume", Value: uid1})
	opAuthResume(e, &core.Cxt{Req: req3}, &core.Res{W: w3})
	if w3.Code != http.StatusSeeOther {
		t.Fatalf("3 resume: %d want 303: loc %s body %s", w3.Code, w3.Header().Get("Location"), w3.Body.String())
	}
	if got := cookieRawValue(w3, "_session"); got == "" {
		t.Fatalf("(f) resume must WRITE _session (loginAccount touched): %v", w3.Header().Values("Set-Cookie"))
	}
	if got := cookiePath(w3, "_session"); got != "/federation/oidc" {
		t.Fatalf("(g) _session Path: want /federation/oidc got %q", got)
	}
	uid2 := interactUID(w3)
	if uid2 == "" || uid2 == uid1 {
		t.Fatalf("3: must mint a new consent interaction (uid1=%q uid2=%q)", uid1, uid2)
	}
	if sdoc, _ := e.opFind("Session", cookieRawValue(w3, "_session")); sdoc == nil {
		t.Fatalf("session doc missing at _session cookie value")
	}
}

// --- (h) unknown client = RENDER 400 oops! ---

func TestOidcBatteryUnknownClientRender(t *testing.T) {
	_, _, e := danceEngine(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/federation/oidc/auth?client_id=urn:overleaf-federation:client:stranger.net&response_type=code&scope=openid", nil)
	opAuthorize(e, &core.Cxt{Req: req}, &core.Res{W: w})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("(h) unknown client: %d want 400 render: loc %q", w.Code, w.Header().Get("Location"))
	}
	if loc := w.Header().Get("Location"); loc != "" {
		t.Fatalf("(h) must RENDER (no redirect); Location %q", loc)
	}
	if !strings.Contains(w.Body.String(), "oops! something went wrong") {
		t.Fatalf("(h) oops! page missing: %s", w.Body.String())
	}
}

// --- (a)/(b)/(e) + (f) the full A->B dance ---

// TestOidcDanceFullFlow — 1 GET /auth (login mint, no _session cookie)
// -> 2 bridge finishLogin 303 -> 3 resume (WRITE _session; consent
// mint) -> 4 bridge consent 200 (TWO forms; no silent grant) ->
// 5 POST consent 303 -> 6 resume (e: silent grant reuse; b: code mint
// 303 redirect_uri?code&state&iss, NO nonce) -> 7 POST /token 200
// {access_token, expires_in:3600, id_token, scope, token_type} with a
// NO-typ ES256 id_token (institution null when unset).
func TestOidcDanceFullFlow(t *testing.T) {
	_, users, e := danceEngine(t)
	_ = users

	// 1. fresh GET /auth -> 303 login interact.
	w1 := httptest.NewRecorder()
	opAuthorize(e, &core.Cxt{Req: httptest.NewRequest("GET", opAuthDanceURL(), nil)}, &core.Res{W: w1})
	uid1 := interactUID(w1)
	if w1.Code != http.StatusSeeOther || uid1 == "" {
		t.Fatalf("1: %d loc %q", w1.Code, w1.Header().Get("Location"))
	}

	// 2. GET bridge -> finishLogin 303 resume.
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/federation/oidc/interact/"+uid1, nil)
	req2.AddCookie(&http.Cookie{Name: "_interaction", Value: uid1})
	e.opBridgeGet(&core.Cxt{Req: req2, Sess: bLoggedInSess(aliceB)}, &core.Res{W: w2})
	if w2.Code != http.StatusSeeOther {
		t.Fatalf("2: %d loc %s", w2.Code, w2.Header().Get("Location"))
	}

	// 3. resume -> login applied -> op_scopes_missing -> consent mint.
	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest("GET", "/federation/oidc/auth/"+uid1, nil)
	req3.AddCookie(&http.Cookie{Name: "_interaction", Value: uid1})
	req3.AddCookie(&http.Cookie{Name: "_interaction_resume", Value: uid1})
	opAuthResume(e, &core.Cxt{Req: req3}, &core.Res{W: w3})
	if w3.Code != http.StatusSeeOther {
		t.Fatalf("3: %d body %s", w3.Code, w3.Body.String())
	}
	uid2 := interactUID(w3)
	if uid2 == "" || uid2 == uid1 {
		t.Fatalf("3: new consent interaction uid2=%q (uid1=%q)", uid2, uid1)
	}

	// 4. GET bridge (consent, no silent grant) -> 200 consent-form HTML.
	w4 := httptest.NewRecorder()
	req4 := httptest.NewRequest("GET", "/federation/oidc/interact/"+uid2, nil)
	req4.AddCookie(&http.Cookie{Name: "_interaction", Value: uid2})
	e.opBridgeGet(&core.Cxt{Req: req4, Sess: bLoggedInSess(aliceB)}, &core.Res{W: w4})
	if w4.Code != http.StatusOK {
		t.Fatalf("4: %d want 200 body %s", w4.Code, w4.Body.String())
	}
	// (a) consent.pug: exactly TWO <form class=consent-form> (allow + deny).
	if got := strings.Count(w4.Body.String(), `<form class="consent-form"`); got != 2 {
		t.Fatalf("(a) want exactly TWO consent-form forms; got %d: %s", got, w4.Body.String())
	}
	if !strings.Contains(w4.Body.String(), `content="consent"`) {
		t.Fatalf("(a) consent meta missing: %s", w4.Body.String())
	}

	// 5. POST consent -> 303 resume.
	w5 := httptest.NewRecorder()
	req5 := httptest.NewRequest("POST", "/federation/oidc/interact/"+uid2+"/consent", nil)
	req5.AddCookie(&http.Cookie{Name: "_interaction", Value: uid2})
	e.opBridgeConsent(&core.Cxt{Req: req5, Sess: bLoggedInSess(aliceB)}, &core.Res{W: w5})
	if w5.Code != http.StatusSeeOther {
		t.Fatalf("5: %d want 303", w5.Code)
	}

	// 6. resume (cycling the _session cookie written by step 3) ->
	// (e) loadExistingGrant silent reuse -> (b) mint AC -> 303
	// redirect_uri?code&state&iss (nonce NOT in the redirect).
	sessJti := cookieRawValue(w3, "_session")
	if sessJti == "" {
		t.Fatalf("3: no _session cookie written by the resume: %v", w3.Header().Values("Set-Cookie"))
	}
	sdoc, _ := e.opFind("Session", sessJti)
	if sdoc == nil {
		t.Fatalf("3: session doc missing at _session cookie value")
	}
	w6 := httptest.NewRecorder()
	req6 := httptest.NewRequest("GET", "/federation/oidc/auth/"+uid2, nil)
	req6.AddCookie(&http.Cookie{Name: "_interaction", Value: uid2})
	req6.AddCookie(&http.Cookie{Name: "_interaction_resume", Value: uid2})
	req6.AddCookie(&http.Cookie{Name: "_session", Value: sessJti})
	opAuthResume(e, &core.Cxt{Req: req6}, &core.Res{W: w6})
	if w6.Code != http.StatusSeeOther {
		t.Fatalf("6: %d body %s", w6.Code, w6.Body.String())
	}
	loc6, _ := url.Parse(w6.Header().Get("Location"))
	if loc6.Scheme != "https" || loc6.Host != "a.example.net" || loc6.Path != "/federation/oidc/rp/callback" {
		t.Fatalf("(b) Location must be A's callback: %s", w6.Header().Get("Location"))
	}
	code := loc6.Query().Get("code")
	if code == "" {
		t.Fatalf("(b) no code: %s", w6.Header().Get("Location"))
	}
	if loc6.Query().Get("state") != "st" || loc6.Query().Get("iss") != e.Issuer || loc6.Query().Get("nonce") != "" {
		t.Fatalf("(b) redirect must carry state+iss and NO nonce: %s", w6.Header().Get("Location"))
	}

	// (e) the AC references the silent-reuse grant (accountId = alice).
	acDoc, _ := e.opFind("AuthorizationCode", code)
	gdoc, _ := e.opFind("Grant", docStr(acDoc["grantId"]))
	if gdoc == nil || docStr(gdoc["accountId"]) != aliceB {
		t.Fatalf("(e) AC grantId must resolve to the silent-reuse Grant")
	}

	// 7. POST /token -> 200 wire.
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", aClient)
	form.Set("code", code)
	form.Set("redirect_uri", aCB)
	w7 := httptest.NewRecorder()
	e.opToken(&core.Cxt{Req: tokenReq(form)}, &core.Res{W: w7})
	if w7.Code != http.StatusOK {
		t.Fatalf("7: %d want 200 body %s", w7.Code, w7.Body.String())
	}
	var tr map[string]any
	if err := json.Unmarshal(w7.Body.Bytes(), &tr); err != nil {
		t.Fatalf("7: token JSON: %v (body %s)", err, w7.Body.String())
	}
	if tr["scope"] != "openid" || tr["token_type"] != "Bearer" {
		t.Fatalf("7: wire scope/token_type: %v", tr)
	}
	if ei, ok := tr["expires_in"].(float64); !ok || ei != 3600 {
		t.Fatalf("7: expires_in want 3600 got %v", tr["expires_in"])
	}
	if _, has := tr["refresh_token"]; has {
		t.Fatalf("7: NO refresh_token key (grant_types=['authorization_code']): %v", tr)
	}
	idTok, _ := tr["id_token"].(string)
	// id_token header = {alg, kid} with NO typ (vendored idtoken use).
	if dot := strings.Index(idTok, "."); dot < 0 || strings.Contains(idTok[:dot], `"typ"`) {
		t.Fatalf("7: id_token header must omit typ: %s", idTok)
	}
	// ES256 verify against the oidc public key.
	pub := e.Key.PublicKey
	_, payload, verr := VerifyJWT(pub, idTok)
	if verr != nil {
		t.Fatalf("7: id_token verify: %v", verr)
	}
	var it map[string]any
	if err := json.Unmarshal(payload, &it); err != nil {
		t.Fatalf("7: id_token payload: %v (payload %s)", err, payload)
	}
	if it["sub"] != aliceB || it["origin"] != bOrigin || it["localName"] != "alice@"+bOrigin || it["displayName"] != "Alice B" {
		t.Fatalf("7: identity claims: %v", it)
	}
	if it["aud"] != aClient || it["iss"] != e.Issuer || it["nonce"] != "nn" {
		t.Fatalf("7: iss/aud/nonce: %v", it)
	}
	if it["institution"] != nil {
		t.Fatalf("7: institution must be null when unset (alice); got %v", it["institution"])
	}
	if at, ok := tr["access_token"].(string); !ok || at == "" {
		t.Fatalf("7: access_token: %v", tr)
	}
}

// --- (c) replay cascade + uniform wire ---

// TestOidcBatteryReplayCascade — a second token delivery for a
// consumed code: opRevokeCascade (AT + AC + Grant destroy; client
// sweep SET empty) + the uniform invalid_grant wire (the per-cause
// "already consumed" rides error_detail = LOG only, never the wire).
func TestOidcBatteryReplayCascade(t *testing.T) {
	_, users, e := danceEngine(t)
	_ = users
	seedDocs(t, e, "replaygid0123456789abcdef012", "replaysuid0123456789abcdef", "replaysjti0123456789abcdef", "replaycode0123456789abcdef")
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", aClient)
	form.Set("code", "replaycode0123456789abcdef")
	form.Set("redirect_uri", aCB)
	// first delivery: 200 (the code is consumed; doc KEPT, flag set).
	w1 := httptest.NewRecorder()
	e.opToken(&core.Cxt{Req: tokenReq(form)}, &core.Res{W: w1})
	if w1.Code != http.StatusOK {
		t.Fatalf("replay first: %d want 200 body %s", w1.Code, w1.Body.String())
	}
	// second delivery: cascade + uniform invalid_grant wire.
	w2 := httptest.NewRecorder()
	e.opToken(&core.Cxt{Req: tokenReq(form)}, &core.Res{W: w2})
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("(c) replay second: %d want 400 body %s", w2.Code, w2.Body.String())
	}
	var re map[string]any
	if err := json.Unmarshal(w2.Body.Bytes(), &re); err != nil {
		t.Fatalf("(c) wire: %v", err)
	}
	if re["error"] != "invalid_grant" || re["error_description"] != "grant request is invalid" {
		t.Fatalf("(c) uniform wire: %v", re)
	}
	if m, _ := e.Redis.SMEMBERS("federation:oidc:client:" + aClient); len(m) != 0 {
		t.Fatalf("(c) client sweep SET must be empty after the cascade; members %v", m)
	}
	if g, _ := e.opFind("Grant", "replaygid0123456789abcdef012"); g != nil {
		t.Fatalf("(c) Grant must be destroyed by the cascade")
	}
}

// --- (d) PKCE ---

// TestOidcBatteryPKCE — S256 mismatch -> invalid_grant (uniform wire;
// the per-cause rides error_detail); short verifier -> invalid_request
// (the FORMAT check is outside the try/catch; per-cause message rides
// the wire — session-13 vendored pin).
func TestOidcBatteryPKCE(t *testing.T) {
	_, users, e := danceEngine(t)
	_ = users
	// AC "pkceA": challenge = S256 of a DIFFERENT verifier than the
	// token form sends -> S256 compare fails -> invalid_grant.
	seedDocs(t, e, "pkcegid0123456789abcdef012", "pkesuid0123456789abcdef0", "pkesjti0123456789abcdef0", "pkcecode0123456789abcdef0")
	chal := s256sum("challengeverifier0123456789012345678901234567890")
	ac, _ := e.opFind("AuthorizationCode", "pkcecode0123456789abcdef0")
	ac["codeChallenge"] = chal
	ac["codeChallengeMethod"] = "S256"
	_ = e.opSave("AuthorizationCode", "pkcecode0123456789abcdef0", ac, 120)
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", aClient)
	form.Set("code", "pkcecode0123456789abcdef0")
	form.Set("redirect_uri", aCB)
	form.Set("code_verifier", "wrongverifier01234567890123456789012345678901abcd") // 44 chars, [\w.-~]
	w1 := httptest.NewRecorder()
	e.opToken(&core.Cxt{Req: tokenReq(form)}, &core.Res{W: w1})
	if w1.Code != http.StatusBadRequest {
		t.Fatalf("(d) mismatch: %d want 400 body %s", w1.Code, w1.Body.String())
	}
	var re1 map[string]any
	_ = json.Unmarshal(w1.Body.Bytes(), &re1)
	if re1["error"] != "invalid_grant" || re1["error_description"] != "grant request is invalid" {
		t.Fatalf("(d) mismatch uniform wire: %v", re1)
	}
	// AC "pkceB": same seed; SHORT verifier -> the FORMAT check fires
	// (43-min) -> invalid_request wire (per-cause message ON the wire).
	seedDocs(t, e, "pkcegidB0123456789abcdef01", "pkesuidB0123456789abcdef", "pkesjtiB0123456789abcdef0", "pkcecodeB0123456789abcdef0")
	form2 := url.Values{}
	form2.Set("grant_type", "authorization_code")
	form2.Set("client_id", aClient)
	form2.Set("code", "pkcecodeB0123456789abcdef0")
	form2.Set("redirect_uri", aCB)
	form2.Set("code_verifier", "short") // 5 chars -> 43-min format fail
	w2 := httptest.NewRecorder()
	e.opToken(&core.Cxt{Req: tokenReq(form2)}, &core.Res{W: w2})
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("(d) short verifier: %d want 400 body %s", w2.Code, w2.Body.String())
	}
	var re2 map[string]any
	_ = json.Unmarshal(w2.Body.Bytes(), &re2)
	if re2["error"] != "invalid_request" || re2["error_description"] != "code_verifier must be a string with a minimum length of 43 characters" {
		t.Fatalf("(d) short verifier wire: %v", re2)
	}
}

// --- (h) deny-redirect wire ---

// TestOidcBatteryDenyRedirect — bridge POST deny (NO login check) ->
// the stored error -> resume re-throws -> 303 redirect_uri?error=
// access_denied&error_description=End-User%20denied%20consent&state&iss.
func TestOidcBatteryDenyRedirect(t *testing.T) {
	_, _, e := danceEngine(t)
	uid := "denyuid0123456789abcdef01234"
	now := opEpoch()
	_ = e.opSave("Interaction", uid, map[string]any{
		"iat": now, "exp": now + 600, "jti": uid, "kind": "Interaction",
		"returnTo": e.Issuer + "/auth/" + uid,
		"params": map[string]any{
			"client_id": aClient, "redirect_uri": aCB, "response_type": "code",
			"scope": "openid", "state": "st", "nonce": "nn",
		},
		"prompt": map[string]any{"name": "consent", "reasons": []any{"op_scopes_missing"},
			"details": map[string]any{"missingOIDCScope": []any{"openid"}}},
	}, 600)
	// bridge POST deny (vendored handleDeny: no B-login check).
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/federation/oidc/interact/"+uid+"/deny", nil)
	req.AddCookie(&http.Cookie{Name: "_interaction", Value: uid})
	e.opBridgeDeny(&core.Cxt{Req: req}, &core.Res{W: w})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("(h) deny: %d want 303 loc %s", w.Code, w.Header().Get("Location"))
	}
	if !strings.Contains(w.Header().Get("Location"), "/federation/oidc/auth/"+uid) {
		t.Fatalf("(h) deny 303 must target resume: %s", w.Header().Get("Location"))
	}
	// resume (the 303 target) re-throws the stored error -> redirect.
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/federation/oidc/auth/"+uid, nil)
	req2.AddCookie(&http.Cookie{Name: "_interaction", Value: uid})
	req2.AddCookie(&http.Cookie{Name: "_interaction_resume", Value: uid})
	opAuthResume(e, &core.Cxt{Req: req2}, &core.Res{W: w2})
	if w2.Code != http.StatusSeeOther {
		t.Fatalf("(h) deny resume: %d want 303 body %s", w2.Code, w2.Body.String())
	}
	loc, _ := url.Parse(w2.Header().Get("Location"))
	q := loc.Query()
	if loc.Host != "a.example.net" || loc.Path != "/federation/oidc/rp/callback" {
		t.Fatalf("(h) deny redirect must target A's callback: %s", w2.Header().Get("Location"))
	}
	if q.Get("error") != "access_denied" || q.Get("error_description") != "End-User denied consent" {
		t.Fatalf("(h) deny wire: error=%q desc=%q", q.Get("error"), q.Get("error_description"))
	}
	if q.Get("state") != "st" || q.Get("iss") != e.Issuer {
		t.Fatalf("(h) deny wire state/iss: %s", w2.Header().Get("Location"))
	}
}

// --- institution claim (id_token wire: null when unset, set when present) ---

func TestOidcBatteryInstitutionClaim(t *testing.T) {
	_, users, e := danceEngine(t)
	aliceClaims, _ := users.Account(context.Background(), aliceB)
	sallyClaims, _ := users.Account(context.Background(), sallyB)
	mint := func(c OidcUserClaims, sub string) map[string]any {
		tok, sErr := opSignIdToken(e.Key.PrivateKey, e.Issuer, aClient, c, sub, "nn")
		if sErr != nil {
			t.Fatalf("id_token sign: %v", sErr)
		}
		_, payload, verr := VerifyJWT(e.Key.PublicKey, tok)
		if verr != nil {
			t.Fatalf("id_token verify: %v", verr)
		}
		m := map[string]any{}
		if err := json.Unmarshal(payload, &m); err != nil {
			t.Fatalf("id_token payload: %v (payload %s)", err, payload)
		}
		return m
	}
	if m := mint(aliceClaims, aliceB); m["institution"] != nil {
		t.Fatalf("alice institution must be null (unset); got %v", m["institution"])
	}
	if m := mint(sallyClaims, sallyB); m["institution"] != "B Inc" {
		t.Fatalf("sally institution must be 'B Inc'; got %v", m["institution"])
	}
}
