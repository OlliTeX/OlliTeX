// rp_routes.go — S10 (A-side RP) mounted routes:
//
//	POST /api/federation/invite/authorize — wizard starter (01 §5
//	     steps 1–3: validate, mint state, 302 the browser to B's
//	     authorization endpoint). Logged-in A user (global gate).
//	GET  /federation/oidc/rp/callback — B's 303 lands here (NoLogin +
//	     NoCSRF): verify state → exchange code (PKCE) → mirror user →
//	     optional project grant → 302 into the project / hub.
//
// The deep B-side consent mint (S4b/S11) is the OP engine; this file
// is the A-side half of the SAME dance.

package federation

import (
	"encoding/json"
	"strings"
	"time"

	"ollitex/go/services/web/core"
)

// fedSite — the configured site URL (cxt seam → app cfg → empty).
func fedSite(cxt *core.Cxt) string {
	if cxt.SiteURL != "" {
		return cxt.SiteURL
	}
	if cxt.A != nil && cxt.A.Cfg != nil && cxt.A.Cfg.SiteURL != "" {
		return cxt.A.Cfg.SiteURL
	}
	return ""
}

// rpErr — the honest envelope (feature-off / pending codes live in
// userfed.go; the deep codes here carry the failure detail).
func rpErr(res *core.Res, status int, code, detail string) {
	dd := strings.NewReplacer(`"`, `\"`, `\`, `\\`).Replace(detail)
	res.JSON(status, []byte(`{"ok":false,"code":"`+code+`","detail":"`+dd+`"}`))
}

// rpDiscoverBase — test seam (unit tests point it at httptest).
var rpDiscoverBase = func(origin string) string { return "https://" + origin }

func rpValidOrigin(s string) bool {
	if s == "" || len(s) > 253 {
		return false
	}
	if strings.ContainsAny(s, " /?#@") {
		return false
	}
	hasLabel := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			hasLabel = true
		case r == '.' || r == '-' || r == ':':
		default:
			return false
		}
	}
	return hasLabel
}

func rpValidLocalName(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 254 {
		return false
	}
	for _, r := range s {
		// reject '@' (origin separator), ALL whitespace (space, tab, \n,
		// \r) and control/DEL bytes — a localName is a bare identifier.
		if r == '@' || r <= ' ' || r == 0x7f {
			return false
		}
	}
	return true
}

// rpAuthorizeStarter — POST /api/federation/invite/authorize.
//
//	body: { "origin": "b.example", "localName": "jane",
//	         "project": "<24-hex>"? }   (01 §5 steps 1–3)
//
//	→ 302 Location: B's authorization endpoint, signed state + PKCE S256.
//
//	Error codes: federation-off (feature), bad-body, bad-origin,
//	bad-localname, rp-misconfigured, not-logged-in, peer-unreachable.
func rpAuthorizeStarter(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !loadSettings().Enabled {
			rpErr(res, 200, "federation-off", "federation disabled")
			return
		}
		var body struct {
			Origin    string `json:"origin"`
			LocalName string `json:"localName"`
			Project   string `json:"project"`
		}
		if err := json.NewDecoder(cxt.Req.Body).Decode(&body); err != nil {
			rpErr(res, 400, "bad-body", "JSON body required")
			return
		}
		origin := strings.ToLower(strings.TrimSpace(body.Origin))
		localName := strings.TrimSpace(body.LocalName)
		if !rpValidOrigin(origin) {
			rpErr(res, 400, "bad-origin", "origin must be a host[:port]")
			return
		}
		if !rpValidLocalName(localName) {
			rpErr(res, 400, "bad-localname", "localName invalid")
			return
		}
		site := fedSite(cxt)
		if site == "" {
			rpErr(res, 500, "rp-misconfigured", "site URL not configured")
			return
		}
		inviter := ""
		if cxt.Sess != nil {
			inviter = cxt.Sess.UserIDHex()
		}
		if inviter == "" {
			rpErr(res, 401, "not-logged-in", "a logged-in A user starts the invite")
			return
		}

		now := time.Now().Unix()
		st := &RPState{
			Peer:      origin,
			LocalName: localName,
			ProjectID: body.Project,
			Inviter:   inviter,
			Nonce:     rpRandomHex(16),
			Verifier:  rpRandomHex(32), // 64 hex chars — within RFC 7636 43–128
			TS:        now,
			Exp:       now + RPStateTTL,
		}
		flow := ProdRPFlow(a)
		disc, err := FetchDiscovery(cxt.Req.Context(), flow.HTTP, rpDiscoverBase(origin))
		if err != nil {
			rpErr(res, 502, "peer-unreachable", err.Error())
			return
		}
		loc, err := BuildAuthorizeURL(site, disc, st)
		if err != nil {
			rpErr(res, 500, "rp-misconfigured", err.Error())
			return
		}
		res.Redirect(cxt.Req, 302, loc)
	}
}

// rpCallback — GET /federation/oidc/rp/callback (NoLogin + NoCSRF).
//
//	B's final hop is a 303 → ?code=...&state=... (success) or
//	?error=...&state=... (the v9 error-wire deny flow).
//
//	Happy leg (unit-green seams; live leg = f2 dual fixture):
//	  1. VerifyRPState (HMAC + TTL)
//	  2. B discovery → CodeExchange (PKCE S256, ES256-vs-JWKS,
//	     iss/aud/exp/iat/nonce/sub/ident claims)
//	  3. identity-mismatch guard (claim localName == state localName)
//	  4. Mirror user (LOCKED federation marker shape)
//	  5. optional project grant (collaborator)
//	  6. 302 /project/<id> (or /hub)
//
//	Error codes: federation-off, auth-denied (B error wire), bad-state,
//	peer-unreachable, token-exchange, identity-mismatch, mirror-failed,
//	grant-failed, rp-misconfigured.
func rpCallback(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !loadSettings().Enabled {
			rpErr(res, 200, "federation-off", "federation disabled")
			return
		}
		site := fedSite(cxt)
		if site == "" {
			rpErr(res, 500, "rp-misconfigured", "site URL not configured")
			return
		}
		q := cxt.Req.URL.Query()

		// B's error wire (deny / interaction failure): ?error=...&state=...
		if e := q.Get("error"); e != "" {
			desc := q.Get("error_description")
			d := strings.NewReplacer(`"`, `\"`, `\`, `\\`).Replace(desc)
			res.JSON(401, []byte(`{"ok":false,"code":"auth-denied","error":"`+e+`","description":"`+d+`"}`))
			return
		}
		code := q.Get("code")
		st, err := VerifyRPState(site, q.Get("state"), time.Now().Unix())
		if err != nil {
			rpErr(res, 400, "bad-state", err.Error())
			return
		}
		flow := ProdRPFlow(a)
		ctx := cxt.Req.Context()
		disc, err := FetchDiscovery(ctx, flow.HTTP, rpDiscoverBase(st.Peer))
		if err != nil {
			rpErr(res, 502, "peer-unreachable", err.Error())
			return
		}
		claims, err := CodeExchange(ctx, flow.HTTP, site, disc, st, code, time.Now().Unix())
		if err != nil {
			rpErr(res, 502, "token-exchange", err.Error())
			return
		}
		if claims.LocalName != st.LocalName {
			rpErr(res, 400, "identity-mismatch", "id_token localName != state localName")
			return
		}
		mir, err := flow.Mirror(ctx, st.Peer, st.LocalName, claims.DisplayName)
		if err != nil {
			rpErr(res, 502, "mirror-failed", err.Error())
			return
		}
		if st.ProjectID != "" {
			if err := flow.Grant(ctx, st.ProjectID, mir.UserHex, st.Inviter); err != nil {
				rpErr(res, 502, "grant-failed", err.Error())
				return
			}
		}
		target := "/hub"
		if st.ProjectID != "" {
			target = "/project/" + st.ProjectID
		}
		res.Redirect(cxt.Req, 302, target)
	}
}
