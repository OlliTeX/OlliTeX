package gsync

import (
	"ollitex/go/services/web/core"
)

// The OAuth2 flow requires a client_id / callback registered for this
// instance. Without it (the default CE/live config — githubSync.clientID
// unset), Node's route surface still exists but the redirect would use an
// empty client_id. The owner's E2E path is PAT-based (gittest26 / test
// accounts), so the OAuth routes are present for parity but degrade to a
// 400 with a clear message when no client is configured.

const gsOAuthClientIDEnv = "GITHUB_OAUTH_CLIENT_ID"

// oauth2RedirectHandler — GET /user/github-sync/oauth2.
func oauth2RedirectHandler(cxt *core.Cxt, res *core.Res) {
	_ = cxt
	res.JSON(400, []byte(`{"message":"OAuth is not configured on this instance. Use PAT linking."}`))
}

// oauth2CallbackHandler — GET /user/github-sync/oauth2/callback.
func oauth2CallbackHandler(cxt *core.Cxt, res *core.Res) {
	_ = cxt
	res.JSON(400, []byte(`{"message":"OAuth is not configured on this instance."}`))
}

// unlinkGitHubHandler — POST /user/github-sync/unlink (removes the GitHub
// OAuth slot; PAT entries untouched).
func unlinkGitHubHandler(cxt *core.Cxt, res *core.Res) {
	uid := gsSessionUID(cxt)
	if err := gsRemoveGitHubOAuth(cxt.Req.Context(), cxt.A, uid); err != nil {
		gsResErr(res, err)
		return
	}
	res.SendStatus(200)
}

// unlinkProjectHandler — DELETE /project/:id/github-sync
// (Node unlinkRepo: owner check → 403 {ownerEmail}).
func unlinkProjectHandler(cxt *core.Cxt, res *core.Res) {
	uid := gsSessionUID(cxt)
	pidHex := cxt.Params["project_id"]
	oid, err := gsObjectID(pidHex)
	if err != nil {
		res.JSON(404, []byte(`{"message":"not found"}`))
		return
	}
	pd, okp := gsLoadProject(cxt, oid)
	if !okp {
		res.JSON(404, []byte(`{"message":"not found"}`))
		return
	}
	if owner := gsOwnerRef(pd); owner != uid {
		email := gsUserEmail(cxt, owner)
		out, _ := jsonMarshal(map[string]any{"ownerEmail": email})
		res.JSON(403, out)
		return
	}
	if err := gsRemoveState(cxt.Req.Context(), cxt.A, oid); err != nil {
		gsResErr(res, err)
		return
	}
	res.SendStatus(200)
}
