// S4b-1 provider HTTP routes (05 §1.1): GET /federation/oidc/jwks +
// GET /federation/oidc/.well-known/openid-configuration.
// NoLogin on both (op_* cookies are oidc-provider-owned — the OP is a
// cross-origin surface, never behind the B-visitor session gate).
package federation

import (
	"encoding/json"
	"net/http"
	"regexp"

	"ollitex/go/services/web/core"
)

// federStore — per-request production Store + KeyProvider
// (MongoStore over c.A.Mongo; false when Mongo unset).
func federStore(c *core.Cxt) (Store, *KeyProvider, bool) {
	if c.A == nil || c.A.Mongo == nil {
		return nil, nil, false
	}
	db, err := c.A.Mongo.DB(c.Req.Context())
	if err != nil {
		return nil, nil, false
	}
	store := NewMongoStore(db)
	site := c.SiteURL
	if site == "" {
		site = c.A.Cfg.SiteURL
	}
	return store, &KeyProvider{Store: store, Site: site}, true
}

// federOidcEngine — the per-request B-side OP engine (the seams: the
// site Redis doc store, the B-side user source over the site Mongo, the
// active oidc-purpose signing key, and the static clients[] rebuild
// from approved peers). ok=false when the B side is not up (no
// Redis/Mongo, SITE_URL unwritable) -> the routes degrade with 503 (the
// surface is mounted, like the S2S envelope 200 `federation-off` on a
// gated-off instance, but this surface answers nothing useful until
// keystore bootstrap has run).
func federOidcEngine(c *core.Cxt) (*OidcEngine, bool) {
	store, kp, ok := federStore(c)
	if !ok || c.A.Redis == nil {
		return nil, false
	}
	// keystore bootstrap (Node `ensureBootstrapped` at boot; the Go
	// equivalent is idempotent — it runs once per key purpose and no-ops
	// afterwards). Without it the /auth GET would 503 on cold instances.
	if berr := kp.Bootstrap(); berr != nil {
		return nil, false
	}
	clients := BuildOidcProviderClients(store, c.Req.Context())
	key, err := kp.ActiveKey(oidcPurpose)
	if err != nil {
		return nil, false
	}
	if key.PrivateKey == nil || key.PrivateKey.D == "" {
		return nil, false
	}
	site := c.SiteURL
	if site == "" {
		site = c.A.Cfg.SiteURL
	}
	ep, err := oidcEndpointsGo(site)
	if err != nil {
		return nil, false
	}
	origin, rerr := getOriginGo(site)
	if rerr != nil {
		return nil, false
	}
	db, derr := c.A.Mongo.DB(c.Req.Context())
	if derr != nil {
		return nil, false
	}
	return &OidcEngine{
		Redis:    c.A.Redis,
		Users:    &MongoOidcUserSource{DB: db, Origin: origin},
		Key:      key,
		Issuer:   ep.Issuer,
		SiteURL:  oidcSiteOrigin(site),
		Clients:  clients,
		Provider: oidcPurpose,
	}, true
}

// --- S4b-2/3 route patterns (uid = v9 nanoid 43, never "/") ---

// opAuthResumePattern — GET /federation/oidc/auth + GET
// /federation/oidc/auth/<uid> (resume; vendor mount is a SUFFIX of the
// auth path, NOT a /resume segment — doc drift in createProvider.mjs
// comment, session-6 pin).
var opAuthResumePattern = regexp.MustCompile(`^/federation/oidc/auth(?:/(?P<uid>[^/]+))?$`)

// opInteractBridgePattern — GET /interact/<uid> + POST
// /interact/<uid>/{consent,deny} (bridge.mjs, mounted BEFORE the
// OP catch-all, plan 05 §1.1).
var opInteractBridgePattern = regexp.MustCompile(`^/federation/oidc/interact/(?P<uid>[^/]+)(?://(?P<act>consent|deny))?$`)

// s4Routes — the OP routes (S4b-1: jwks + discovery; S4b-2/3: auth /
// resume + bridge + token). Mount order = vendored (bridge BEFORE the
// provider catch-all, plan 05 §1.1) — dispatch is first-match, so the
// bridge patterns precede the auth pattern (interact/ and auth/ paths
// are disjoint anyway).
func s4Routes(a *core.App) []core.Route {
	return []core.Route{
		{
			Method:  "GET",
			Path:    "/federation/oidc/jwks",
			NoLogin: true,
			Handler: func(c *core.Cxt, res *core.Res) {
				store, _, ok := federStore(c)
				if !ok {
					res.SendStatus(http.StatusInternalServerError)
					return
				}
				handleOidcJwks(store, c, res)
			},
		},
		{
			Method:  "GET",
			Path:    "/federation/oidc/.well-known/openid-configuration",
			NoLogin: true,
			Handler: func(c *core.Cxt, res *core.Res) {
				_, kp, ok := federStore(c)
				if !ok {
					res.SendStatus(http.StatusInternalServerError)
					return
				}
				handleOidcDiscovery(kp, res)
			},
		},

		// --- S4b-2/3 engine routes (bridge BEFORE provider, plan 05
		// §1.1). All six NO NoLogin-bounce (the OP surface is a cross-
		// origin whitelist — vendored overleaf-fed: the dance's anonymous
		// GET /auth hop reaches the OP, never the /login gate); session
		// stack ON (vendored mount is on webRouter — the overleaf session
		// cookie rides the dance jar; the OP's own op_* cookies are read
		// by the engine directly). ---

		// Bridge GET: not logged in on B -> 302 /login + postLoginRedirect
		// stash (opBridgeGet — vendored bridge.mjs setRedirectInSession is
		// reproduced in-band, NOT via the global bounce).
		{
			Method:  "GET",
			Pattern: opInteractBridgePattern,
			NoLogin: true,
			Handler: func(c *core.Cxt, res *core.Res) {
				e, ok := federOidcEngine(c)
				if !ok {
					res.SendStatus(http.StatusServiceUnavailable)
					return
				}
				if c.Sess == nil || !c.Sess.IsLoggedIn() {
					res.Redirect(c.Req, http.StatusFound, "/login")
					return
				}
				if uid := c.Params["uid"]; uid != "" && opCookieValue(c.Req, "_interaction") != uid {
					// vendored #getInteraction is COOKIE-driven (the URL uid
					// is not consulted by Node); the dance jar carries the
					// matching cookie — a mismatch is a foreign-uid replay,
					// rejected on the same wire as a missing cookie.
					opRenderAuthError(res, opInvalidRequest("interaction session not found"))
					return
				}
				e.opBridgeGet(c, res)
			},
		},
		{
			Method:  "POST",
			Pattern: opInteractBridgePattern,
			NoLogin: true, // vendored: no per-route gate (defense in depth handled in opBridgeConsent)
			NoCSRF:  true, // machine/dance POST (plan 05 §1.1 nonCsrfRouter)
			Handler: func(c *core.Cxt, res *core.Res) {
				e, ok := federOidcEngine(c)
				if !ok {
					res.SendStatus(http.StatusServiceUnavailable)
					return
				}
				uid, act := c.Params["uid"], c.Params["act"]
				if opCookieValue(c.Req, "_interaction") != uid {
					opRenderAuthError(res, opInvalidRequest("interaction session not found"))
					return
				}
				switch act {
				case "consent":
					e.opBridgeConsent(c, res)
				case "deny":
					e.opBridgeDeny(c, res)
				}
			},
		},

		// OP core: auth + resume (GET) — NoLogin (cross-origin OP surface;
		// the OP session is op_*-cookie-owned, vendored on the same
		// session-ON web router, so NoSession stays false... see NoLogin
		// note: this route is NoLogin so anonymous dance hops are served
		// (the vendored mount has no per-route auth gate — the OP resolves
		// its own session doc via the _session cookie). The overleaf
		// session cookie is unrelated and rides the jar forward.
		{
			Method:  "GET",
			Pattern: opAuthResumePattern,
			NoLogin: true,
			Handler: func(c *core.Cxt, res *core.Res) {
				e, ok := federOidcEngine(c)
				if !ok {
					res.SendStatus(http.StatusServiceUnavailable)
					return
				}
				if c.Params["uid"] != "" {
					opAuthResume(e, c, res)
					return
				}
				opAuthorize(e, c, res)
			},
		},

		// token: POST form (A-side machine call, no B visitor session)
		{
			Method:  "POST",
			Path:    "/federation/oidc/token",
			NoLogin: true,
			NoCSRF:  true,
			Handler: func(c *core.Cxt, res *core.Res) {
				e, ok := federOidcEngine(c)
				if !ok {
					res.SendStatus(http.StatusServiceUnavailable)
					return
				}
				e.opToken(c, res)
			},
		},
	}
}

// handleOidcJwks — GET /federation/oidc/jwks: the oidc-purpose JWKS
// (05 §8.1). Body: { keys: [pub JWKs] } (no d — PublicHalf).
func handleOidcJwks(store Store, c *core.Cxt, res *core.Res) {
	payload, err := OIDCJwksPayload(store, c.Req.Context())
	if err != nil {
		res.SendStatus(http.StatusInternalServerError)
		return
	}
	body, _ := json.Marshal(payload)
	res.JSON(http.StatusOK, body)
}

// handleOidcDiscovery — GET /federation/oidc/.well-known/openid-
// configuration (v9 discovery doc, absolute endpoints).
func handleOidcDiscovery(kp *KeyProvider, res *core.Res) {
	endpoints, err := oidcEndpointsGo(kp.Site)
	if err != nil {
		res.SendStatus(http.StatusInternalServerError)
		return
	}
	body, _ := json.Marshal(OidcDiscoveryMetadata(endpoints))
	res.JSON(http.StatusOK, body)
}
