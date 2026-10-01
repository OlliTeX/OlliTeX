// oidcengine_resume.go — the GET /federation/oidc/auth/<uid> resume route
// (vendored actions/authorization/resume.js) + the shared auth tail
// (loadAccount → loadGrant → interactions → respond) that the resume
// action re-enters via `await next()`.
//
// initialize_app.js:166 mounts `get('resume', routes.authorization + '/
// :uid', authError, ...resume)` where resume = getAuthorization('resume')
// = the FULL authorization stack. After getResume's steps land, the
// engine re-runs the SAME tail as GET /auth (opAuthTail below).
//
// Oracle: vendored v9.12.2 resume.js + the session 6 wire pins (HANDOFF).
package federation

import (
	"net/http"

	"ollitex/go/services/web/core"
)

// opAuthCtx — the (client, params, result) triple the shared tail
// consumes. result = the interaction `result` (login/consent); nil for
// the bare GET /auth path.
type opAuthCtx struct {
	client *OidcProviderClient
	params opAuthParams
	result map[string]any
}

// opAuthLoadGrant — loadExistingGrant (vendor actions/authorization/
// session.js:17-43): grantId = result.consent.grantId || session.
// grantIdFor(clientId); if a live Grant doc is found, mismatch-guard +
// grantIdForSet (marks touched); else in-memory unsaved Grant (no scope
// → policy prompts). Returns the grant (grantScope via grant.scopeVal()).
func opAuthLoadGrant(e *OidcEngine, sess *opSess, clientID string, result map[string]any) (grant *opGrantDoc) {
	gid := ""
	if result != nil {
		if csm, _ := result["consent"].(map[string]any); csm != nil {
			gid = docStr(csm["grantId"])
		}
	}
	if gid == "" {
		gid = sess.grantIdFor(clientID)
	}
	if gid != "" {
		if gd, _ := e.opFind("Grant", gid); gd != nil {
			g := opFromGrantDoc(gd)
			if g.AccountID == sess.accountId() && g.ClientID == clientID {
				sess.grantIdForSet(clientID, gid)
				return g
			}
		}
	}
	// in-memory unsaved Grant (accountId/clientId, no scope).
	return &opGrantDoc{AccountID: sess.accountId(), ClientID: clientID}
}

// opAuthTail — the shared loadAccount → loadGrant → policy → respond
// tail (GET /auth and GET /auth/<uid> resume both end here). Vendor
// actions/authorization/{session,interactions,respond}.
func opAuthTail(e *OidcEngine, c *core.Cxt, res *core.Res, sess *opSess, oax opAuthCtx) {
	// loadAccount: session.accountId → Users.Account (NO throw on miss;
	// the entity is simply absent when findAccount returns null).
	accountFound := false
	if sess.accountId() != "" && e.Users != nil {
		_, accountFound = e.Users.Account(c.Req.Context(), sess.accountId())
	}

	// loadGrant (loadExistingGrant; requires an account to establish).
	var grant *opGrantDoc
	if accountFound {
		grant = opAuthLoadGrant(e, sess, oax.client.ClientID, oax.result)
	}
	if grant == nil {
		grant = &opGrantDoc{AccountID: sess.accountId(), ClientID: oax.client.ClientID}
	}

	// policy (login no_session / consent op_scopes_missing).
	prompt := opPromptPolicy(sess, oax.params, OidcUserClaims{}, accountFound, grant.scopeVal())

	// session finally (cookie only when !new || touched).
	e.sessFinally(res.W, sess)

	opAuthRespond(e, res, sess, oax, grant, prompt)
}

// opAuthRespond — vendor respond.js: prompt → mint Interaction + 2
// cookies + 303 interact/<u>; !accountId → AccessDenied (render 400);
// no granted scope → AccessDenied (render 400); else mint AC + 303
// redirect_uri?code&state?&iss.
func opAuthRespond(e *OidcEngine, res *core.Res, sess *opSess, oax opAuthCtx, grant *opGrantDoc, prompt *opPrompt) {
	if prompt != nil {
		uid := opOpaqUID()
		doc := opMintInteractionDoc(e, uid, oax.params, sess, prompt, grant, oax.result)
		e.opMintInteraction(res, uid, doc)
		return
	}
	if sess.accountId() == "" {
		opRenderAuthError(res, opAccessDenied("authorization request resolved without requesting interactions but no account id was resolved"))
		return
	}
	if grant.scopeVal() == "" {
		opRenderAuthError(res, opAccessDenied("authorization request resolved without requesting interactions but no scope was granted"))
		return
	}
	if mErr := e.opMintCode(res, sess, oax.client.ClientID, oax.params.redirectURI, oax.params.state, oax.params.nonce, "openid", oax.params.codeChallenge, oax.params.codeChallengeMethod); mErr != nil {
		_ = e.opDestroy("Session", sess.opJTI())
		opRenderAuthError(res, opInvalidRequest("code mint failed"))
	}
}

// --- opAuthResume — GET /federation/oidc/auth/<uid> (15 steps) ---
//
//  1. cookie _interaction_resume → miss → 400 'authorization request has
//     expired'
//  2. Interaction.find(cookieId) → miss → 400 'interaction session not
//     found'
//  3. cookieId !== interaction.uid → 400 '...identifier mismatch'
//  4. interaction.session?.uid && !== session.uid → 400 '...session
//     mismatch'
//  5. (result.login mismatch → end_session_confirm form_post — OUT OF
//     SCOPE for Go: rpInitiatedLogout disabled in createProvider.mjs)
//  6. interaction.destroy()
//  7. params = storedParams (restored into opAuthParams)
//  8. clear _interaction_resume cookie (path /federation/oidc/auth/<uid>)
//  9. result.error → AccessDenied(error_description) → error-handler
//     303 redirect_uri?error&error_description?&state?&iss
//  10. result.login → session.loginAccount({accountId, loginTs})
//  11. if (!session.new) session.resetIdentifier()
//     12-15. → opAuthTail (loadAccount → loadGrant → policy → respond)
func opAuthResume(e *OidcEngine, c *core.Cxt, res *core.Res) {
	sess := e.opLoadSession(c.Req)

	// 1. cookie _interaction_resume
	cookieID := opCookieValue(c.Req, "_interaction_resume")
	if cookieID == "" {
		e.sessFinally(res.W, sess)
		opRenderAuthError(res, opInvalidRequest("authorization request has expired"))
		return
	}

	// 2. Interaction.find(cookieId)
	doc, err := e.opFind("Interaction", cookieID)
	if err != nil || doc == nil {
		e.sessFinally(res.W, sess)
		opRenderAuthError(res, opInvalidRequest("interaction session not found"))
		return
	}

	// 3. cookieId !== interaction.uid → mismatch (vendored: uid is a
	//    getter alias for jti; IN_PAYLOAD stores only jti).
	uid := docStr(doc["jti"])
	if cookieID != uid {
		e.sessFinally(res.W, sess)
		opRenderAuthError(res, opInvalidRequest("authorization session and cookie identifier mismatch"))
		return
	}

	// 4. interaction.session?.uid cross-check (opSess: uid = OP session
	//    uid, NOT jti).
	if isess, _ := doc["session"].(map[string]any); isess != nil {
		if isuid := docStr(isess["uid"]); isuid != "" && isuid != sess.opUID() {
			e.sessFinally(res.W, sess)
			opRenderAuthError(res, opInvalidRequest("interaction session and authentication session mismatch"))
			return
		}
	}

	// 7. params = storedParams (interaction.params flat map).
	params := opAuthParams{}
	if pd, _ := doc["params"].(map[string]any); pd != nil {
		params.clientID = docStr(pd["client_id"])
		params.redirectURI = docStr(pd["redirect_uri"])
		params.responseType = docStr(pd["response_type"])
		params.scope = docStr(pd["scope"])
		params.state = docStr(pd["state"])
		params.nonce = docStr(pd["nonce"])
		params.codeChallenge = docStr(pd["code_challenge"])
		params.codeChallengeMethod = docStr(pd["code_challenge_method"])
		params.prompt = docStr(pd["prompt"])
	}
	client := e.opClient(params.clientID)

	// 6. interaction.destroy().
	_ = e.opDestroy("Interaction", uid)

	// 8. clear _interaction_resume cookie.
	opClearResumeCookie(res.W, uid)
	e.sessFinally(res.W, sess)

	// 9. result handling.
	var result map[string]any
	if rd, _ := doc["result"].(map[string]any); rd != nil {
		result = rd
	}
	if result != nil && docStr(result["error"]) != "" {
		// result.error → AccessDenied(error_description) → error-handler
		// 303 redirect (allow_redirect TRUE for AccessDenied).
		errAuth := opAccessDenied(docStr(result["error_description"]))
		if !errAuth.AllowRedirect || client == nil || params.redirectURI == "" {
			opRenderAuthError(res, errAuth)
			return
		}
		opErrRedirect(res, errAuth, params.state, e.Issuer, params.redirectURI)
		return
	}
	if result != nil {
		if lg, _ := result["login"].(map[string]any); lg != nil {
			if acct := docStr(lg["accountId"]); acct != "" {
				ts := int64(0)
				switch t := lg["ts"].(type) {
				case float64:
					ts = int64(t)
				case int64:
					ts = t
				case int:
					ts = int64(t)
				}
				// 10. session.loginAccount.
				sess.loginAccount(acct, ts)
				// 11. if (!session.new) resetIdentifier (fresh-OP cookie
				//     case is new=true → no reset).
				if !sess.newSess {
					e.resetIdentifier(sess)
				}
				e.sessFinally(res.W, sess)
			}
		}
	}

	// 12-15. shared auth tail.
	opAuthTail(e, c, res, sess, opAuthCtx{client: client, params: params, result: result})
}

var _ = http.StatusSeeOther
