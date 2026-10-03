// rp_test.go — S10 (A-side RP) unit gates: state sign/verify (HMAC +
// TTL + tamper), discovery fetch, authorize-URL wire, code exchange
// (real ES256 against a fetched JWKS) + the honest error legs, the
// suspended classifier, and the mounted handler error legs.
//
// No network beyond httptest, no mongo — the prod glue (prodMirror /
// prodGrant) rides the f2 dual-instance live fixture.

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
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"ollitex/go/services/web/core"
)

// ---- shared pins ----

const (
	rpSite     = "https://a.example"
	rpPeerOrig = "a.example"
	rpIssuer   = "https://b.example/federation/oidc"
)

func rpTestState() *RPState {
	now := time.Now().Unix()
	return &RPState{
		Peer:      "b.example:3000",
		LocalName: "jane",
		ProjectID: "64f1c2b1a5f8d9c0b1a2b3c4",
		Inviter:   "64f1c2b1a5f8d9c0b1a2b3c0",
		Nonce:     "nonce-123",
		Verifier:  strings.Repeat("a", 64),
		TS:        now,
		Exp:       now + RPStateTTL,
	}
}

// ---- state sign / verify ----

func TestRPState_SignVerify(t *testing.T) {
	st := rpTestState()

	sig, err := SignRPState(rpSite, st)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	now := time.Now().Unix()
	got, err := VerifyRPState(rpSite, sig, now)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.Peer != st.Peer || got.LocalName != "jane" || got.Nonce != "nonce-123" ||
		got.Verifier != st.Verifier || got.ProjectID != st.ProjectID || got.Inviter != st.Inviter {
		t.Fatalf("round trip mismatch: %+v", got)
	}

	// tampered payload — flip one base64 char of the JSON segment
	msg, sigPart, ok := strings.Cut(sig, ".")
	if !ok {
		t.Fatalf("wire missing sig: %s", sig)
	}
	if _, err := VerifyRPState(rpSite, "B"+msg[1:]+"."+sigPart, now); err == nil {
		t.Fatalf("tampered payload must fail")
	}
	if _, err := VerifyRPState(rpSite, msg+".AAAA", now); err == nil {
		t.Fatalf("bad signature must fail")
	}
	// another site's signature must NOT verify (site-scoped key)
	other, _ := SignRPState("https://evil.example", st)
	if _, err := VerifyRPState(rpSite, other, now); err == nil {
		t.Fatalf("cross-site state must fail")
	}
	// expired
	exp := rpTestState()
	exp.Exp = now - 10
	esig, _ := SignRPState(rpSite, exp)
	if _, err := VerifyRPState(rpSite, esig, now); err == nil {
		t.Fatalf("expired state must fail")
	}
	// absurd window
	crazy := rpTestState()
	crazy.Exp = now + 2*86400
	csig, _ := SignRPState(rpSite, crazy)
	if _, err := VerifyRPState(rpSite, csig, now); err == nil {
		t.Fatalf("over-large window must fail")
	}
	// missing required fields
	bad := rpTestState()
	bad.Verifier = ""
	if _, err := SignRPState(rpSite, bad); err == nil {
		t.Fatalf("missing verifier must fail at sign time")
	}
	// malformed wires
	if _, err := VerifyRPState(rpSite, "garbage", now); err == nil {
		t.Fatalf("malformed state must fail")
	}
}

func TestS256Challenge_Vector(t *testing.T) {
	// RFC 7636 §4.2 test vector
	v := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXw"
	sum := sha256.Sum256([]byte(v))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if got := S256Challenge(v); got != want {
		t.Fatalf("S256: got %s want %s", got, want)
	}
}

// ---- helpers: B-side OP surface on httptest ----

func newRPKey(t *testing.T) (pub, priv *JWK) {
	t.Helper()
	pub, priv, err := GenerateES256()
	if err != nil {
		t.Fatalf("GenerateES256: %v", err)
	}
	return pub, priv
}

func rpJwksServer(t *testing.T, pub *JWK) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []JWK{*pub}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func rpTokenServer(t *testing.T, idToken string, checks map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("token method: %s", r.Method)
		}
		_ = r.ParseForm()
		for k, want := range checks {
			if r.Form.Get(k) != want {
				t.Errorf("token %s: got %q want %q", k, r.Form.Get(k), want)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": idToken, "access_token": "opaque-1"})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func rpSign(t *testing.T, priv *JWK, claims map[string]any) string {
	t.Helper()
	plain, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	idt, err := SignJWT(priv, "JWT", plain)
	if err != nil {
		t.Fatalf("SignJWT: %v", err)
	}
	return idt
}

func rpBaseClaims(now int64, nonce string) map[string]any {
	return map[string]any{
		"iss":         rpIssuer,
		"aud":         RPClientID(rpPeerOrig),
		"iat":         now,
		"exp":         now + 3600,
		"nonce":       nonce,
		"sub":         "acct-b-77",
		"origin":      "b.example:3000",
		"localName":   "jane",
		"displayName": "Jane Roe",
		"institution": nil,
	}
}

// ---- code exchange ----

func TestCodeExchange_OK(t *testing.T) {
	pub, priv := newRPKey(t)
	now := time.Now().Unix()
	st := rpTestState()

	idt := rpSign(t, priv, rpBaseClaims(now, st.Nonce))
	jwks := rpJwksServer(t, pub)
	tok := rpTokenServer(t, idt, map[string]string{
		"grant_type":    "authorization_code",
		"code":          "code-abc",
		"redirect_uri":  RPRedirectURI(rpPeerOrig),
		"code_verifier": st.Verifier,
	})
	disc := RPDiscovery{Issuer: rpIssuer, TokenEndpoint: tok.URL, JwksURI: jwks.URL}

	claims, err := CodeExchange(context.Background(), nil, rpSite, disc, st, "code-abc", now)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if claims.Sub != "acct-b-77" || claims.LocalName != "jane" || claims.DisplayName != "Jane Roe" {
		t.Fatalf("claims: %+v", claims)
	}
	if claims.Institution != "" {
		t.Errorf("institution should be '' for null, got %q", claims.Institution)
	}
	if claims.Nonce != st.Nonce {
		t.Errorf("nonce: %s", claims.Nonce)
	}
}

func TestCodeExchange_Errors(t *testing.T) {
	pub, priv := newRPKey(t)
	now := time.Now().Unix()
	jwks := rpJwksServer(t, pub)
	mkDisc := func(token *httptest.Server) RPDiscovery {
		return RPDiscovery{Issuer: rpIssuer, TokenEndpoint: token.URL, JwksURI: jwks.URL}
	}
	st := rpTestState()

	// nonce mismatch
	tokNonce := rpTokenServer(t, rpSign(t, priv, rpBaseClaims(now, "WRONG")), nil)
	if _, err := CodeExchange(context.Background(), nil, rpSite, mkDisc(tokNonce), st, "c", now); err == nil || !strings.Contains(err.Error(), "nonce") {
		t.Fatalf("want nonce error, got %v", err)
	}
	// expired token
	tokExp := rpTokenServer(t, rpSign(t, priv, mergeClaims(rpBaseClaims(now, st.Nonce), map[string]any{"exp": now - 100})), nil)
	if _, err := CodeExchange(context.Background(), nil, rpSite, mkDisc(tokExp), st, "c", now); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("want expired, got %v", err)
	}
	// wrong audience
	tokAud := rpTokenServer(t, rpSign(t, priv, mergeClaims(rpBaseClaims(now, st.Nonce), map[string]any{"aud": "urn:overleaf-federation:client:evil"})), nil)
	if _, err := CodeExchange(context.Background(), nil, rpSite, mkDisc(tokAud), st, "c", now); err == nil || !strings.Contains(err.Error(), "audience") {
		t.Fatalf("want audience, got %v", err)
	}
	// missing identity (no localName)
	tokNoIdent := rpTokenServer(t, rpSign(t, priv, mergeClaims(rpBaseClaims(now, st.Nonce), map[string]any{"localName": ""})), nil)
	if _, err := CodeExchange(context.Background(), nil, rpSite, mkDisc(tokNoIdent), st, "c", now); err == nil || !strings.Contains(err.Error(), "ident") {
		t.Fatalf("want ident, got %v", err)
	}
	// bad issuer
	tokIss := rpTokenServer(t, rpSign(t, priv, mergeClaims(rpBaseClaims(now, st.Nonce), map[string]any{"iss": "https://evil/federation/oidc"})), nil)
	if _, err := CodeExchange(context.Background(), nil, rpSite, mkDisc(tokIss), st, "c", now); err == nil || !strings.Contains(err.Error(), "issuer") {
		t.Fatalf("want issuer, got %v", err)
	}

	// token endpoint 400 (invalid_grant)
	srv400 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"grant request is invalid"}`))
	}))
	t.Cleanup(srv400.Close)
	if _, err := CodeExchange(context.Background(), nil, rpSite, mkDisc(srv400), st, "c", now); err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("want invalid_grant, got %v", err)
	}

	// no id_token in the response
	srvNoID := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"opaque"}`))
	}))
	t.Cleanup(srvNoID.Close)
	if _, err := CodeExchange(context.Background(), nil, rpSite, mkDisc(srvNoID), st, "c", now); err == nil || !strings.Contains(err.Error(), "idtoken-missing") {
		t.Fatalf("want idtoken-missing, got %v", err)
	}

	// missing code
	if _, err := CodeExchange(context.Background(), nil, rpSite, mkDisc(srvNoID), st, "", now); err == nil || !strings.Contains(err.Error(), "missing code") {
		t.Fatalf("want missing code, got %v", err)
	}
}

func mergeClaims(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// ---- discovery ----

func TestFetchDiscovery_OK(t *testing.T) {
	disc := RPDiscovery{
		Issuer:                rpIssuer,
		AuthorizationEndpoint: "https://b.example:3000/federation/oidc/auth",
		TokenEndpoint:         "https://b.example:3000/federation/oidc/token",
		JwksURI:               "https://b.example:3000/federation/oidc/jwks",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/federation/oidc/.well-known/openid-configuration" {
			t.Errorf("path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(disc)
	}))
	t.Cleanup(srv.Close)

	got, err := FetchDiscovery(context.Background(), nil, srv.URL)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.Issuer != rpIssuer || got.TokenEndpoint != disc.TokenEndpoint || got.JwksURI != disc.JwksURI {
		t.Fatalf("mismatch: %+v", got)
	}
}

func TestFetchDiscovery_Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"issuer": rpIssuer}) // incomplete
	}))
	t.Cleanup(srv.Close)
	if _, err := FetchDiscovery(context.Background(), nil, srv.URL); err == nil {
		t.Fatalf("incomplete discovery must fail")
	}
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	t.Cleanup(srv2.Close)
	if _, err := FetchDiscovery(context.Background(), nil, srv2.URL); err == nil {
		t.Fatalf("500 discovery must fail")
	}
}

// ---- authenticate URL wire ----

func TestBuildAuthorizeURL(t *testing.T) {
	st := rpTestState()
	disc := RPDiscovery{
		Issuer:                rpIssuer,
		AuthorizationEndpoint: "https://b.example:3000/federation/oidc/auth",
		TokenEndpoint:         "https://b.example:3000/federation/oidc/token",
		JwksURI:               "https://b.example:3000/federation/oidc/jwks",
	}
	got, err := BuildAuthorizeURL(rpSite, disc, st)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if u.Host != "b.example:3000" || u.Path != "/federation/oidc/auth" {
		t.Fatalf("endpoint: %s", u.String())
	}
	q := u.Query()
	if q.Get("response_type") != "code" {
		t.Errorf("response_type: %s", q.Get("response_type"))
	}
	if q.Get("client_id") != RPClientID(rpPeerOrig) {
		t.Errorf("client_id: %s", q.Get("client_id"))
	}
	if q.Get("redirect_uri") != RPRedirectURI(rpPeerOrig) {
		t.Errorf("redirect_uri: %s", q.Get("redirect_uri"))
	}
	if q.Get("scope") != "openid" {
		t.Errorf("scope: %s", q.Get("scope"))
	}
	if q.Get("nonce") != st.Nonce {
		t.Errorf("nonce: %s", q.Get("nonce"))
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("method: %s", q.Get("code_challenge_method"))
	}
	if q.Get("code_challenge") != S256Challenge(st.Verifier) {
		t.Errorf("challenge mismatch")
	}
	round, err := VerifyRPState(rpSite, q.Get("state"), time.Now().Unix())
	if err != nil {
		t.Fatalf("state in URL must verify: %v", err)
	}
	if round.Verifier != st.Verifier {
		t.Errorf("state verifier lost")
	}
}

// ---- suspended classifier (the seam's LOCKED check) ----

func TestFedUserSuspended(t *testing.T) {
	cases := []struct {
		name string
		doc  bson.M
		want bool
	}{
		{"active", bson.M{"suspended": false}, false},
		{"suspendedBool", bson.M{"suspended": true}, true},
		{"suspendedStr", bson.M{"suspended": "true"}, true},
		{"absent", bson.M{}, false},
	}
	for _, c := range cases {
		if got := fedUserSuspended(c.doc); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

// ---- origin / localName validation ----

func TestRPValidation(t *testing.T) {
	if !rpValidOrigin("b.example") || !rpValidOrigin("b.example:3000") || !rpValidOrigin("sub.host-1.example.org") {
		t.Errorf("good origins must pass")
	}
	for _, bad := range []string{"", "http://x", "a b", "x/y", "has#frag", "!!!"} {
		if rpValidOrigin(bad) {
			t.Errorf("bad origin must fail: %q", bad)
		}
	}
	if !rpValidLocalName("jane.doe-1") {
		t.Errorf("localname good must pass")
	}
	for _, bad := range []string{"", "a@b", "a b", strings.Repeat("x", 255)} {
		if rpValidLocalName(bad) {
			t.Errorf("bad localname must fail: %q", bad)
		}
	}
}

// ---- mounted handler error legs (no network) ----

func rpDeadPeer(t *testing.T) {
	t.Helper()
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "peer down", http.StatusInternalServerError)
	}))
	t.Cleanup(dead.Close)
	old := rpDiscoverBase
	rpDiscoverBase = func(origin string) string { return dead.URL }
	t.Cleanup(func() { rpDiscoverBase = old })
}

func TestRPHandler_CallbackLegs(t *testing.T) {
	t.Setenv("FEDERATION_ENABLED", "true")
	a := core.New(&core.Config{Profile: "web"}, nil)
	feat := Feature(a)
	rpDeadPeer(t)
	found := false
	for _, r := range feat.Routes {
		if r.Pattern != patRpCallback {
			continue
		}
		found = true
		// valid state + dead peer (deterministic) → honest 502
		// peer-unreachable — the expected unit boundary beyond state.
		sig, err := SignRPState(rpSite, rpTestState())
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		req, _ := http.NewRequest(http.MethodGet, "/federation/oidc/rp/callback?code=x&state="+url.QueryEscape(sig), nil)
		cxt := &core.Cxt{Req: req, A: a, SiteURL: rpSite}
		rec := httptest.NewRecorder()
		r.Handler(cxt, &core.Res{W: rec})
		if !strings.Contains(rec.Body.String(), "peer-unreachable") {
			t.Fatalf("peer-unreachable leg: %s", rec.Body.String())
		}

		// error wire: B deny → auth-denied (no discovery fetch on this leg)
		req2, _ := http.NewRequest(http.MethodGet,
			"/federation/oidc/rp/callback?error=access_denied&error_description=denied&state="+url.QueryEscape(sig), nil)
		cxt2 := &core.Cxt{Req: req2, A: a, SiteURL: rpSite}
		rec2 := httptest.NewRecorder()
		r.Handler(cxt2, &core.Res{W: rec2})
		if !strings.Contains(rec2.Body.String(), "auth-denied") {
			t.Fatalf("auth-denied leg: %s", rec2.Body.String())
		}

		// garbage state → bad-state
		req3, _ := http.NewRequest(http.MethodGet, "/federation/oidc/rp/callback?code=x&state=garbage", nil)
		cxt3 := &core.Cxt{Req: req3, A: a, SiteURL: rpSite}
		rec3 := httptest.NewRecorder()
		r.Handler(cxt3, &core.Res{W: rec3})
		if !strings.Contains(rec3.Body.String(), "bad-state") {
			t.Fatalf("bad-state leg: %s", rec3.Body.String())
		}
		// feature OFF → federation-off
		t.Setenv("FEDERATION_ENABLED", "false")
		req4, _ := http.NewRequest(http.MethodGet, "/federation/oidc/rp/callback?code=x&state=y", nil)
		cxt4 := &core.Cxt{Req: req4, A: a, SiteURL: rpSite}
		rec4 := httptest.NewRecorder()
		r.Handler(cxt4, &core.Res{W: rec4})
		if !strings.Contains(rec4.Body.String(), "federation-off") {
			t.Fatalf("off leg: %s", rec4.Body.String())
		}
		break
	}
	if !found {
		t.Fatalf("rp callback route not mounted")
	}
}

func TestRPStarter_ErrorLegs(t *testing.T) {
	t.Setenv("FEDERATION_ENABLED", "true")
	a := core.New(&core.Config{Profile: "web"}, nil)
	feat := Feature(a)
	found := 0
	for _, r := range feat.Routes {
		if r.Pattern != patInviteAuthorize {
			continue
		}
		found++
		// bad body
		req, _ := http.NewRequest(http.MethodPost, "/api/federation/invite/authorize", strings.NewReader("not json"))
		req.Header.Set("Content-Type", "application/json")
		cxt := &core.Cxt{Req: req, A: a, SiteURL: rpSite}
		rec := httptest.NewRecorder()
		r.Handler(cxt, &core.Res{W: rec})
		if !strings.Contains(rec.Body.String(), "bad-body") {
			t.Fatalf("bad-body leg: %s", rec.Body.String())
		}
		// bad origin (validation fires before the session check)
		req2, _ := http.NewRequest(http.MethodPost, "/api/federation/invite/authorize",
			strings.NewReader(`{"origin":"http://x","localName":"jane"}`))
		cxt2 := &core.Cxt{Req: req2, A: a, SiteURL: rpSite}
		rec2 := httptest.NewRecorder()
		r.Handler(cxt2, &core.Res{W: rec2})
		if !strings.Contains(rec2.Body.String(), "bad-origin") {
			t.Fatalf("bad-origin leg: %s", rec2.Body.String())
		}
		// bad localName
		req3, _ := http.NewRequest(http.MethodPost, "/api/federation/invite/authorize",
			strings.NewReader(`{"origin":"b.example","localName":"bad name"}`))
		cxt3 := &core.Cxt{Req: req3, A: a, SiteURL: rpSite}
		rec3 := httptest.NewRecorder()
		r.Handler(cxt3, &core.Res{W: rec3})
		if !strings.Contains(rec3.Body.String(), "bad-localname") {
			t.Fatalf("bad-localname leg: %s", rec3.Body.String())
		}
		// not logged in (no session) → 401 before any network
		req0, _ := http.NewRequest(http.MethodPost, "/api/federation/invite/authorize",
			strings.NewReader(`{"origin":"b.example","localName":"jane"}`))
		cxt0 := &core.Cxt{Req: req0, A: a, SiteURL: rpSite}
		rec0 := httptest.NewRecorder()
		r.Handler(cxt0, &core.Res{W: rec0})
		if !strings.Contains(rec0.Body.String(), "not-logged-in") {
			t.Fatalf("not-logged-in leg: %s", rec0.Body.String())
		}
		break
	}
	if found == 0 {
		t.Fatalf("invite authorize route not mounted")
	}
}
