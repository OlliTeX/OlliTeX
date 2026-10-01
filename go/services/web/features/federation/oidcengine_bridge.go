// oidcengine_bridge.go — the B-side interaction bridge (overleaf-fed
// modules/federation/oidc/bridge.mjs, byte-for-byte oracle-pinned) +
// the vendored provider surface it drives (interactionDetails =
// #getInteraction cookie cross-check, interactionFinished merge +
// re-save + 303, Grant mint/re-save).
//
// Routes (vendored mountBridge, mounted BEFORE the provider catch-all):
//
//	GET  /federation/oidc/interact/<uid>          302 B /login (not
//	          logged in) | finishLogin (prompt.login) | silent-303 or
//	          renderConsent (prompt.consent) | 501 otherwise
//	POST /federation/oidc/interact/<uid>/consent  Grant mint -> 303 resume
//	POST /federation/oidc/interact/<uid>/deny     access_denied -> 303 resume
//
// The bridge runs on the B WEB router: user identity = c.Sess
// (IsLoggedIn / UserIDHex), NOT the OP _session cookie. The OP _interaction
// cookie (minted by the OP at GET /auth) keys the bridge handlers.
//
// Bridge handlers emit NO OP _session cookie (they run outside the v9
// OP session middleware); only the resume route writes it.
package federation

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"ollitex/go/services/web/core"
)

// --- vendored provider.#getInteraction (provider.js:426, byte-pinned) ---
//
//	cookie _interaction -> miss -> SessionNotFound 'interaction session
//	id cookie not found'; Interaction.find(cookieId) -> miss -> 'interaction
//	session not found'; interaction.session?.uid -> Session.findByUid(uid)
//	(adapter sub-index federation:oidc:sub:<uid>) -> miss -> 'session not
//	found'; interaction.session.accountId !== session.accountId -> 'session
//	principal changed'. SessionNotFound extends InvalidRequest -> 400
//	invalid_request wire (the SPECIFIC message rides the wire).
func (e *OidcEngine) opInteractionDoc(req *http.Request) (map[string]any, *opErr) {
	cookieID := opCookieValue(req, "_interaction")
	if cookieID == "" {
		return nil, opInvalidRequest("interaction session id cookie not found")
	}
	doc, err := e.opFind("Interaction", cookieID)
	if err != nil || doc == nil {
		return nil, opInvalidRequest("interaction session not found")
	}
	if isess, _ := doc["session"].(map[string]any); isess != nil {
		uid := docStr(isess["uid"])
		if uid != "" {
			sdoc, serr := (&OidcAdapter{ModelName: "Session", Redis: e.Redis}).FindByUID(uid)
			if serr != nil || sdoc == nil {
				return nil, opInvalidRequest("session not found")
			}
			if sacc := docStr(sdoc["accountId"]); sacc != docStr(isess["accountId"]) {
				return nil, opInvalidRequest("session principal changed")
			}
		}
	}
	return doc, nil
}

// --- vendored provider.interactionFinished (provider.js:242, byte-pinned) ---
//
//	interactionResult:
//	  if (mergeWithLastSubmission && !('error' in result))
//	    interaction.result = {...interaction.lastSubmission, ...result}
//	  else interaction.result = result
//	  interaction.save(interaction.exp - epochTime())
//	res 303 + Location=interaction.returnTo + Content-Length: 0 + end().
//
// Go: opSave re-saves the full doc map with a fresh 600s TTL (vendor
// remainingTTL = exp - now; the re-saved doc carries the original iat
// and a reset exp = now + ttl — models/interaction.js save(remaining)
// contract; iat/exp both preserved through opSave).
func (e *OidcEngine) opInteractionFinished(res *core.Res, doc map[string]any, result map[string]any, mergeWithLastSubmission bool) {
	if mergeWithLastSubmission && !opMapHasKey(result, "error") {
		result = opMergeLS(doc, result)
	}
	doc["result"] = result
	_ = e.opSave("Interaction", docStr(doc["jti"]), pickPayload(doc, interactionInPayload...), oidcTTLInteraction)
	res.W.Header().Set("Location", docStr(doc["returnTo"]))
	res.W.WriteHeader(http.StatusSeeOther)
}

// opMapHasKey — vendored `'error' in result` (JS in-operator key check).
func opMapHasKey(m map[string]any, key string) bool {
	_, ok := m[key]
	return ok
}

// opMergeLS — bridge.mjs result construction: `{...lastSubmission, ...out}`
// (lastSubmission spread first; out wins on key clashes).
func opMergeLS(doc, out map[string]any) map[string]any {
	merged := map[string]any{}
	if ls, _ := doc["lastSubmission"].(map[string]any); ls != nil {
		for k, v := range ls {
			merged[k] = v
		}
	}
	for k, v := range out {
		merged[k] = v
	}
	return merged
}

// --- GET /federation/oidc/interact/<uid> (bridge.mjs handleInteractGet) ---

func (e *OidcEngine) opBridgeGet(c *core.Cxt, res *core.Res) {
	if c.Sess == nil || !c.Sess.IsLoggedIn() {
		// bridge.mjs: AuthenticationController.setRedirectInSession(req,
		// originalUrl) then res.redirect('/login') -> 302 + Location
		// /login (postLoginRedirect rides the B visitor session doc).
		if c.Sess != nil {
			c.Sess.Set("postLoginRedirect", c.Req.URL.RequestURI())
		}
		res.Redirect(c.Req, http.StatusFound, "/login")
		return
	}

	doc, ierr := e.opInteractionDoc(c.Req)
	if ierr != nil {
		opRenderAuthError(res, ierr)
		return
	}
	prompt, _ := doc["prompt"].(map[string]any)
	pm, _ := doc["params"].(map[string]any)
	pname := ""
	if prompt != nil {
		pname = docStr(prompt["name"])
	}

	switch pname {
	case "login":
		// finishLogin (the handler-top gate above already resolved the
		// logged-in user; vendor finishLogin re-checks — degrade is the
		// same 401, unreachable here).
		e.opInteractionFinished(res, doc, map[string]any{
			"login": map[string]any{
				"accountId": c.Sess.UserIDHex(),
				"ts":        opEpoch(),
			},
		}, true)
		return
	case "consent":
		clientID := docStr(pm["client_id"])
		userID := c.Sess.UserIDHex()
		// vendored findExistingGrant (soft-degrade: ANY miss -> nil ->
		// fresh-consent render; a live grant hit -> grant.save() re-save
		// (TTL refresh) + silent 303, NOT a 200 HTML render).
		gid, gerr := FindByAccountAndClient(e.Redis, userID, clientID)
		if gerr == nil && gid != "" {
			if gdoc, gerr := e.opFind("Grant", gid); gerr == nil && gdoc != nil {
				if docStr(gdoc["accountId"]) == userID && docStr(gdoc["clientId"]) == clientID {
					_ = e.opSave("Grant", gid, pickPayload(gdoc, grantInPayload...), oidcTTLGrant)
					e.opInteractionFinished(res, doc, map[string]any{
						"consent": map[string]any{"grantId": gid},
					}, true)
					return
				}
			}
		}
		e.renderConsentHTML(res, doc, pm)
		return
	default:
		res.PlainText(http.StatusNotImplemented, fmt.Sprintf("interaction prompt %s not supported", pname))
	}
}

// --- POST /federation/oidc/interact/<uid>/consent (handleConsent) ---

func (e *OidcEngine) opBridgeConsent(c *core.Cxt, res *core.Res) {
	if c.Sess == nil || !c.Sess.IsLoggedIn() {
		res.PlainText(http.StatusUnauthorized, "no logged-in user on B")
		return
	}
	doc, ierr := e.opInteractionDoc(c.Req)
	if ierr != nil {
		opRenderAuthError(res, ierr)
		return
	}
	prompt, _ := doc["prompt"].(map[string]any)
	pm, _ := doc["params"].(map[string]any)
	userID := c.Sess.UserIDHex()
	clientID := docStr(pm["client_id"])

	// new Grant({accountId, clientId}) + addOIDCScope(
	// prompt.details.missingOIDCScope.join(' ')) + addOIDCClaims(
	// prompt.details.missingOIDCClaims) (vendored bridge + grant.js).
	scope := ""
	claims := []string{}
	if prompt != nil {
		if details, _ := prompt["details"].(map[string]any); details != nil {
			scope = opDocSliceJoin(details["missingOIDCScope"])
			claims = opDocStrings(details["missingOIDCClaims"])
		}
	}
	gid := opOpaqUID()
	gdoc := map[string]any{
		"iat":       opEpoch(),
		"exp":       opEpoch() + oidcTTLGrant,
		"jti":       gid,
		"kind":      "Grant",
		"accountId": userID,
		"clientId":  clientID,
	}
	if scope != "" {
		openid := map[string]any{"scope": scope}
		if len(claims) > 0 {
			openid["claims"] = claims
		}
		gdoc["openid"] = openid
	} else if len(claims) > 0 {
		gdoc["openid"] = map[string]any{"claims": claims}
	}
	// grant.save() (TTL 30d; Upsert writes the account:<uid>:<client>
	// index).
	_ = e.opSave("Grant", gid, pickPayload(gdoc, grantInPayload...), oidcTTLGrant)
	e.opInteractionFinished(res, doc, map[string]any{
		"consent": map[string]any{"grantId": gid},
	}, true)
}

// --- POST /federation/oidc/interact/<uid>/deny (handleDeny) ---
//
// The vendored handleDeny does NOT login-check (explicit: whoever holds
// the consent URL may deny it — logged or not).
func (e *OidcEngine) opBridgeDeny(c *core.Cxt, res *core.Res) {
	doc, ierr := e.opInteractionDoc(c.Req)
	if ierr != nil {
		opRenderAuthError(res, ierr)
		return
	}
	e.opInteractionFinished(res, doc, map[string]any{
		"error":             "access_denied",
		"error_description": "End-User denied consent",
	}, true)
}

// --- renderConsent (bridge.mjs renderConsent + consent.pug, byte-pinned) ---
//
// Markers: <meta name="federation" content="consent">, <body
// class="consent-view">, TWO <form class="consent-form"> (grantUrl +
// denyUrl), <title>Allow access</title>, client_id rendered.
func (e *OidcEngine) renderConsentHTML(res *core.Res, doc map[string]any, pm map[string]any) {
	clientID := ""
	if pm != nil {
		clientID = docStr(pm["client_id"])
	}
	prompt, _ := doc["prompt"].(map[string]any)
	var scopes, claims []any
	if prompt != nil {
		if details, _ := prompt["details"].(map[string]any); details != nil {
			if v, ok := details["missingOIDCScope"].([]any); ok {
				scopes = v
			}
			if v, ok := details["missingOIDCClaims"].([]any); ok {
				claims = v
			}
		}
	}
	uid := docStr(doc["jti"])
	out := `<!DOCTYPE html>
<html lang="en">
<head>
<title>Allow access</title>
<meta name="federation" content="consent">
<link rel="icon" href="/favicon.ico">
</head>
<body class="consent-view">
<main class="consent-container">
<h1>Allow access</h1>
<p>The federated service ` + opHTMLSafe(clientID) + ` is requesting a federated session.</p>
`
	for _, s := range scopes {
		out += "<li>" + opHTMLSafe(fmt.Sprint(s)) + "</li>"
	}
	for _, s := range claims {
		out += "<li>" + opHTMLSafe(fmt.Sprint(s)) + "</li>"
	}
	out += `<form class="consent-form" action="/federation/oidc/interact/` + uid + `/consent" method="POST">
<button class="btn btn-primary">Allow access</button>
</form>
<form class="consent-form" action="/federation/oidc/interact/` + uid + `/deny" method="POST">
<button class="btn btn-secondary">Deny</button>
</form>
</main>
</body>
</html>`
	res.HTML(http.StatusOK, out)
}

// --- doc list helpers (bridge) ---

func opDocStrings(v any) []string {
	a, _ := v.([]any)
	out := make([]string, 0, len(a))
	for _, x := range a {
		out = append(out, fmt.Sprint(x))
	}
	return out
}

func opDocSliceJoin(v any) string {
	return strings.Join(opDocStrings(v), " ")
}

// production OidcUserSource (createProvider.mjs findAccount) ---
//
// User.findById(sub) — suspended / unknown -> (zero, false). Claims:
//
//	origin        = Settings.siteUrl hostname (portless FQDN, vendor
//	                new URL(Settings.siteUrl).hostname)
//	localName     = user.email
//	displayName   = (first_name + ' ' + last_name).trim() (JS-
//	                semantics trim) || user.email fallback
//	institution   = user.institution ("" -> id_token institution:null)
type MongoOidcUserSource struct {
	DB     *mongo.Database // the site Mongo (OlliTeX single-DB contract)
	Origin string          // portless FQDN (getOriginGo(c.A.Cfg.SiteURL))
}

func (s *MongoOidcUserSource) Account(ctx context.Context, userID string) (OidcUserClaims, bool) {
	if s.DB == nil {
		return OidcUserClaims{}, false
	}
	o, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return OidcUserClaims{}, false
	}
	var u struct {
		Email       *string `bson:"email"`
		FirstName   *string `bson:"first_name"`
		LastName    *string `bson:"last_name"`
		Deleted     *bool   `bson:"deleted"`
		Suspended   *bool   `bson:"suspended"`
		Institution string  `bson:"institution"`
	}
	if err := s.DB.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: o}}).Decode(&u); err != nil {
		return OidcUserClaims{}, false
	}
	if u.Deleted != nil && *u.Deleted {
		return OidcUserClaims{}, false
	}
	if u.Suspended != nil && *u.Suspended {
		return OidcUserClaims{}, false
	}
	localName := ""
	if u.Email != nil {
		localName = *u.Email
	}
	display := ""
	if u.FirstName != nil {
		display = *u.FirstName
	}
	if u.LastName != nil {
		display += " " + *u.LastName
	}
	display = strings.TrimSpace(display)
	if display == "" {
		display = localName
	}
	return OidcUserClaims{
		Origin:      s.Origin,
		LocalName:   localName,
		DisplayName: display,
		Institution: u.Institution,
	}, true
}
