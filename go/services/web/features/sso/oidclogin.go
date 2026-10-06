package sso

// oidclogin.go — N-provider OIDC relying-party (routes in sso.go).
//
// Node pins (passport-openidconnect 0.1.x over overleaf 6.3.0_post base
// + overleaf-fed N-provider):
//
//   - GET /oidc/login[/:providerId] — 302
//     {authorizationURL|discovery}?response_type=code&client_id&
//     redirect_uri={site}/oidc/login/callback&scope[&state][&nonce].
//     Node state/nonce: random hex (crypto.randomBytes(16).hex());
//     no PKCE (client_secret present). Go (hardening) stores
//     state+nonce in the session and verifies the state echo at the
//     callback.
//   - GET /oidc/login/callback — POST {tokenURL} form
//     (code, client_id, client_secret, grant_type, redirect_uri) →
//     {access_token, id_token?}; profile: userinfo GET (Bearer) when
//     userInfoURL set, else id_token claims. Go (hardening) verifies
//     the id_token (RS256/ES256/HS256) via go-oidc v3 against the
//     issuer JWKS — iss/aud/exp/nonce.
//   - JIT (oidcJIT): ThirdPartyIdentity → email → create + link;
//     allowedEmailDomains `*.`/exact gate; admin field; $unset
//     hashedPassword; loginEpoch++ (parallel-login gate).
//   - SLO: POST /logout (externalAuth 'oidc') ⇒ destroy session ⇒
//     302 {logoutURL}?id_token_hint&post_logout_redirect_uri={site};
//     GET /oidc/logout/callback ⇒ 302 {site}.
//   - POST /user/oauth-unlink — drop ThirdPartyIdentity rows (scoped
//     when providerId given).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ollitex/go/services/web/core"

	oidc "github.com/coreos/go-oidc/v3/oidc"
	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/oauth2"
)

// rndHex — random hex (Node crypto.randomBytes(n).toString('hex')).
func rndHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ssoDBTimeout — bounded context for provider I/O.
func ssoHTTPCtx(cxt *core.Cxt) (context.Context, context.CancelFunc) {
	return context.WithTimeout(cxt.Req.Context(), 15*time.Second)
}

// oidcLogin — GET /oidc/login and /oidc/login/:providerId.
func oidcLogin(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		pathID := pathProviderID(cxt)
		var cfg *SSOConfig
		if db, err := ssoDB(a, cxt); err != nil {
			if pathID != "" && pathID != "oidc" {
				res.PlainText(404, "OIDC provider '"+pathID+"' not found or disabled")
				return
			}
		} else {
			cfg = loadSSOConfig(db, cxt)
		}
		if pathID == "callback" {
			// "callback" is the exact callback-route segment (oidcCBPattern).
			// RE2 can't lookahead it out of oidcProviderPattern, so if route
			// order ever regresses, fail loudly instead of a bogus 404.
			res.PlainText(404, "'callback' is the OIDC callback route, not a provider id")
			return
		}
		p, ok := resolveOIDCProvider(cfg, pathID)
		if !ok {
			id := pathID
			if id == "" {
				id = "oidc"
			}
			res.PlainText(404, "OIDC provider '"+id+"' not found or disabled")
			return
		}
		if cxt.Sess != nil {
			cxt.Sess.Set("oidcProviderId", p.ID)
			cxt.Sess.Set("ssoProviderId", p.ID)
		}
		cfgCtx, cancel := ssoHTTPCtx(cxt)
		defer cancel()
		authURL, tokenURL, userinfoURL, err := oidcEndpoints(cfgCtx, p)
		if err != nil {
			res.JSON(502, []byte(fmt.Sprintf(`{"message":{"text":"OIDC discovery failed","type":"error","status":502,"detail":%q}}`, err.Error())))
			return
		}
		state, nonce := rndHex(16), rndHex(16)
		if cxt.Sess != nil {
			cxt.Sess.Set("oidcState", state)
			cxt.Sess.Set("oidcNonce", nonce)
		}
		oauthCfg := &oauth2.Config{
			ClientID:     p.ClientID,
			ClientSecret: p.ClientSecret,
			Endpoint:     oauth2.Endpoint{AuthURL: authURL, TokenURL: tokenURL},
			RedirectURL:  strings.TrimRight(cxt.SiteURL, "/") + "/oidc/login/callback",
		}
		authURLF := oauthCfg.AuthCodeURL(state, oauth2.SetAuthURLParam("nonce", nonce))
		// Provider-configured scope (parity: the authorize request needs the
		// scope the provider requested; soluto/IS4-class IdPs REJECT authorize
		// requests without it). Empty provider scope = unchanged behavior.
		if sc := strings.TrimSpace(p.Scope); sc != "" {
			authURLF += "&scope=" + url.QueryEscape(sc)
		}
		_ = userinfoURL
		res.Redirect(cxt.Req, 302, authURLF)
	}
}

// oidcEndpoints — discovery when URLs are not explicit (Node parity:
// issuer ⇒ .well-known/openid-configuration).
func oidcEndpoints(ctx context.Context, p *OIDCProvider) (auth, token, userinfo string, err error) {
	if p.AuthorizationURL != "" {
		auth = p.AuthorizationURL
	}
	if p.TokenURL != "" {
		token = p.TokenURL
	}
	if p.UserInfoURL != "" {
		userinfo = p.UserInfoURL
	}
	if auth != "" && token != "" {
		return auth, token, userinfo, nil
	}
	if p.Issuer == "" {
		return "", "", "", fmt.Errorf("issuer/authorizationURL/tokenURL all missing")
	}
	issuer := strings.TrimRight(p.Issuer, "/")
	provider, perr := oidc.NewProvider(ctx, issuer)
	if perr != nil {
		return "", "", "", perr
	}
	if auth == "" {
		auth = provider.Endpoint().AuthURL
	}
	if token == "" {
		token = provider.Endpoint().TokenURL
	}
	if userinfo == "" {
		userinfo = provider.UserInfoEndpoint()
	}
	return auth, token, userinfo, nil
}

// oidcCallback — GET /oidc/login/callback.
func oidcCallback(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		providerID := ""
		var stateIn, nonceIn string
		if cxt.Sess != nil {
			if raw, ok := cxt.Sess.GetRaw("oidcProviderId"); ok {
				_ = jsonUnmarshal(raw, &providerID)
			}
			if raw, ok := cxt.Sess.GetRaw("oidcState"); ok {
				_ = jsonUnmarshal(raw, &stateIn)
			}
			if raw, ok := cxt.Sess.GetRaw("oidcNonce"); ok {
				_ = jsonUnmarshal(raw, &nonceIn)
			}
		}
		q := cxt.Req.URL.Query()
		code := q.Get("code")
		stateEcho := q.Get("state")
		if providerID == "" || stateIn == "" {
			res.JSON(400, []byte(`{"message":{"text":"OIDC state missing from session","type":"error","status":400}}`))
			return
		}
		if stateEcho != stateIn {
			res.JSON(403, []byte(`{"message":{"text":"OIDC state mismatch","type":"error","status":403}}`))
			return
		}
		if errSess := cxt.Sess.Del; errSess != nil {
		}
		// clear the one-shot state (Node: consumed).
		clearSessOneShot(cxt)
		var cfg *SSOConfig
		db, err := ssoDB(a, cxt)
		if err != nil {
			dbErr500(cxt, res)
			return
		}
		cfg = loadSSOConfig(db, cxt)
		p, ok := resolveOIDCProvider(cfg, providerID)
		if !ok {
			res.JSON(400, []byte(`{"message":{"text":"OIDC provider from session not found","type":"error","status":400}}`))
			return
		}
		if code == "" {
			res.JSON(400, []byte(`{"message":{"text":"Missing code","type":"error","status":400}}`))
			return
		}
		cfgCtx, cancel := ssoHTTPCtx(cxt)
		defer cancel()
		authURL, tokenURL, userinfoURL, derr := oidcEndpoints(cfgCtx, p)
		if derr != nil {
			res.JSON(502, []byte(`{"message":{"text":"OIDC discovery failed","type":"error","status":502}}`))
			return
		}
		oauthCfg := &oauth2.Config{
			ClientID:     p.ClientID,
			ClientSecret: p.ClientSecret,
			Endpoint:     oauth2.Endpoint{AuthURL: authURL, TokenURL: tokenURL},
			RedirectURL:  strings.TrimRight(cxt.SiteURL, "/") + "/oidc/login/callback",
		}
		tok, terr := oauthCfg.Exchange(cfgCtx, code)
		if terr != nil {
			res.JSON(401, []byte(`{"message":{"text":"OIDC token exchange failed","type":"error","status":401}}`))
			return
		}
		var idRaw string
		var claims map[string]any
		if tok.Extra("id_token") != nil {
			idRaw = tok.Extra("id_token").(string)
		}
		if idRaw != "" {
			var idToken *oidc.IDToken
			if p.Issuer != "" {
				// Go-first hardening (Node checkIdToken default off): full
				// verification — signature (RS256/ES256), iss/aud/exp — via
				// go-oidc v3 against the issuer JWKS.
				if verr := verifyIDToken(cfgCtx, p, idRaw, nonceIn, oauthCfg.ClientID, &idToken); verr != nil {
					res.JSON(401, []byte(`{"message":{"text":"OIDC id_token verification failed","type":"error","status":401}}`))
					return
				}
			}
			if idToken != nil {
				var c map[string]any
				if cerr := idToken.Claims(&c); cerr == nil {
					claims = c
				}
			}
		}
		profile := ssoProfile{}
		if claims != nil {
			for k, v := range claims {
				if s, ok := v.(string); ok {
					profile[k] = s
				} else if aArr, ok := v.([]any); ok {
					vs := make([]string, 0, len(aArr))
					for _, x := range aArr {
						if ss, ok := x.(string); ok {
							vs = append(vs, ss)
						}
					}
					profile[k] = vs
				}
			}
		}
		if userinfoURL != "" {
			if up, uerr := fetchUserinfo(cfgCtx, userinfoURL, tok.AccessToken, profile); uerr == nil {
				profile = up
			}
		}
		role := evaluateAttrFilter(p.AttrFilter, profile).role()
		if role == "blocked" {
			samlLog(a, cxt, p.ID, "denied", "oidc attrFilter blocked")
			auditSsoDenied(a, cxt, "", p.ID, "oidc-attrFilter-blocked")
			res.JSON(401, []byte(`{"message":{"text":"Login denied by SSO role filter","type":"error","status":401}}`))
			return
		}
		user, jerr := oidcJIT(cfgCtx, db, p, profile, p.ID, role)
		if jerr != nil {
			if errDomainDenied == jerr {
				res.JSON(403, []byte(`{"message":{"text":"Email domain not allowed","type":"error","status":403}}`))
				return
			}
			res.JSON(500, []byte(`{"message":{"text":"OIDC login failed","type":"error","status":500}}`))
			return
		}
		sessFields := map[string]any{
			"oidcProviderId": p.ID,
		}
		if idRaw != "" {
			sessFields["idToken"] = idRaw
		}
		finishSSOLogin(a, cxt, res, user, "oidc", p.ID, sessFields, q.Get("redir"))
	}
}

func clearSessOneShot(cxt *core.Cxt) {
	if cxt.Sess == nil {
		return
	}
	cxt.Sess.Del("oidcState")
	cxt.Sess.Del("oidcNonce")
}

// fetchUserinfo — GET {userInfoURL} with Bearer (Node passport-openid
// connect userinfo path).
func fetchUserinfo(ctx context.Context, urlStr, token string, fallback ssoProfile) (ssoProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fallback, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return fallback, nil
	}
	out := ssoProfile{}
	for k, v := range m {
		switch t := v.(type) {
		case string:
			out[k] = t
		case []any:
			vs := make([]string, 0, len(t))
			for _, x := range t {
				if ss, ok := x.(string); ok {
					vs = append(vs, ss)
				}
			}
			out[k] = vs
		}
	}
	return out, nil
}

// verifyIDToken — Go-first hardening over the Node default
// (checkIdToken off): signature (RS256/ES256/HS256) via go-oidc v3
// against the issuer JWKS + iss/aud/exp/nonce. HS256 only when the
// issuer is ours and the secret matches (shared-secret deployment).
func verifyIDToken(ctx context.Context, p *OIDCProvider, idTokenStr, nonce, clientID string, outTok **oidc.IDToken) error {
	issuer := strings.TrimRight(p.Issuer, "/")
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return err
	}
	v := provider.Verifier(&oidc.Config{ClientID: clientID})
	tok, terr := v.Verify(ctx, idTokenStr)
	if terr != nil {
		return terr
	}
	if nonce != "" && tok.Nonce != nonce {
		return fmt.Errorf("nonce mismatch")
	}
	if outTok != nil {
		*outTok = tok
	}
	return nil
}

// oidcLogoutCallbackH — GET /oidc/logout/callback (Node: 302 siteUrl).
func oidcLogoutCallbackH(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		target := strings.TrimRight(cxt.SiteURL, "/")
		if target == "" {
			target = "/login"
		}
		res.Redirect(cxt.Req, 302, target)
	}
}

// oauthUnlink — POST /user/oauth-unlink (Node: unlink the user's SSO
// account rows).
func oauthUnlink(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		uid := cxt.Sess.UserIDHex()
		if uid == "" {
			res.JSON(401, []byte(`{"message":{"text":"Unauthorized","type":"error","status":401}}`))
			return
		}
		var body struct {
			ProviderID string `json:"providerId"`
		}
		if cxt.Req.Body != nil {
			raw, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<16))
			_ = jsonUnmarshal(raw, &body)
		}
		db, err := ssoDB(a, cxt)
		if err != nil {
			dbErr500(cxt, res)
			return
		}
		filter := bson.D{{Key: "user_id", Value: uid}}
		if body.ProviderID != "" {
			filter = append(filter, bson.E{Key: "providerId", Value: body.ProviderID})
		}
		delRes, derr := db.Collection("ThirdPartyIdentity").DeleteMany(cxt.Req.Context(), filter)
		if derr != nil {
			dbErr500(cxt, res)
			return
		}
		res.JSON(200, []byte(fmt.Sprintf(`{"deleted":%d}`, delRes.DeletedCount)))
	}
}

var _ = url.Values{}
