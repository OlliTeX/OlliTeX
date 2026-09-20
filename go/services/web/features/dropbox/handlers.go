package dropbox

import (
	"fmt"
	"strings"

	"ollitex/go/services/web/core"
)

// hDbxStatus — GET /user/dropbox/status.
func hDbxStatus(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		uid := dbxUID(cxt)
		cred, ok := dbxGetCreds(cxt.Req.Context(), a, uid)
		if !ok || cred == nil {
			res.JSON(200, []byte(`{"connected":false}`))
			return
		}
		path := normalizeDropboxPath(cred.Path)
		projects := dbxLinkedStates(cxt.Req.Context(), a, path)
		// Node: lastSyncAt/lastSyncError = projects[0]?.x || null
		lastSyncAt, lastSyncError := "null", "null"
		if len(projects) > 0 {
			if ts, ok := isoTime(projects[0]["lastSyncAt"]); ok {
				lastSyncAt = jsonStr(ts)
			}
			if se, ok := projects[0]["lastSyncError"].(string); ok {
				lastSyncError = jsonStr(se)
			}
		}
		var projParts []string
		for _, pr := range projects {
			var f []string
			if s, ok := pr["projectId"].(string); ok {
				f = append(f, `"projectId":`+jsonStr(s))
			}
			if s, ok := pr["path"].(string); ok {
				f = append(f, `"path":`+jsonStr(s))
			}
			if nm, ok := pr["projectName"].(string); ok && nm != "" {
				f = append(f, `"projectName":`+jsonStr(nm))
			} else {
				f = append(f, `"projectName":null`)
			}
			if pp, ok := pr["projectPath"].(string); ok && pp != "" {
				f = append(f, `"projectPath":`+jsonStr(pp))
			} else {
				f = append(f, `"projectPath":null`)
			}
			// Node's JSON.stringify drops UNDEFINED (absent) keys, keeps nulls —
			// absent fields simply contribute nothing.
			if v, ok := pr["lastSyncAt"]; ok {
				if ts, ok2 := isoTime(v); ok2 {
					f = append(f, `"lastSyncAt":`+jsonStr(ts))
				}
			}
			if v, ok := pr["lastSyncError"].(string); ok {
				f = append(f, `"lastSyncError":`+jsonStr(v))
			}
			projParts = append(projParts, "{"+strings.Join(f, ",")+"}")
		}
		res.JSON(200, []byte(fmt.Sprintf(`{"connected":true,"path":%s,"projects":[%s],"lastSyncAt":%s,"lastSyncError":%s}`,
			jsonStr(path), strings.Join(projParts, ","), lastSyncAt, lastSyncError)))
	}
}

// hDbxConnect — POST /user/dropbox/connect.
func hDbxConnect(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		uid := dbxUID(cxt)
		body := dbxReadBody(cxt)
		token, present := dbxStr(body, "access_token")
		if !present || token == "" {
			res.JSON(400, []byte(`{"error":"Missing access_token"}`))
			return
		}
		if err := dbxEncryptAndSave(cxt.Req.Context(), a, uid, token); err != nil {
			res.JSON(500, []byte(fmt.Sprintf(`{"error":%s}`, jsonStr(err.Error()))))
			return
		}
		res.JSON(200, []byte(`{"success":true}`))
	}
}

// hDbxDisconnect — POST /user/dropbox/disconnect.
func hDbxDisconnect(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		uid := dbxUID(cxt)
		pathToUnlink := "/"
		if cred, ok := dbxGetCreds(cxt.Req.Context(), a, uid); ok && cred != nil {
			pathToUnlink = normalizeDropboxPath(cred.Path)
			dbxRemoveStatesByOwner(cxt.Req.Context(), a, pathToUnlink, uid)
		}
		dbxRemoveCreds(cxt.Req.Context(), a, uid)
		res.JSON(200, []byte(fmt.Sprintf(`{"success":true,"unlinkedProjects":%s}`, jsonStr(pathToUnlink))))
	}
}

// hDbxOAuth2 — GET /user/dropbox/oauth2 (live 302 target outside sandbox).
func hDbxOAuth2(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		appKey := dropboxAppKey()
		appSecret := dropboxAppSecret()
		if appKey == "" || appSecret == "" {
			// Node: res.status(503).send('...') — text/html pin
			dbxText(res, 503, "Dropbox OAuth is not configured")
			return
		}
		state := dbxOauthState(cxt)
		res.Redirect(cxt.Req, 302, dropboxAuthorizeURL(appKey, appSecret, state))
	}
}

// hDbxCallback — GET /user/dropbox/oauth/callback.
func hDbxCallback(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		expected := dbxOauthState(cxt)
		q := cxt.Req.URL.Query()
		state := q.Get("state")
		if state == "" || state != expected {
			dbxText(res, 400, "Invalid Dropbox OAuth state")
			return
		}
		// Network branches unreachable in the sandbox (no app keys → the
		// earlier check already 503'd; with keys the token exchange 404s
		// identically on both stacks — see scope note).
		if q.Get("error") != "" {
			res.Redirect(cxt.Req, 302, "/user/settings")
			return
		}
		if dropboxAppKey() == "" || dropboxAppSecret() == "" || q.Get("code") == "" {
			dbxText(res, 400, "Missing Dropbox OAuth configuration or code")
			return
		}
		dbxText(res, 502, "Dropbox OAuth connection failed")
	}
}

// hDbxState — GET /project/:project_id/dropbox/state (LOGIN ONLY — no authz,
// no zod param check: the handler treats project_id as an opaque string).
func hDbxState(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		seg := cxt.Params["1"]
		state, ok := dbxGetState(cxt.Req.Context(), a, seg)
		if !ok {
			res.JSON(200, []byte(`{"connected":false}`))
			return
		}
		// Stored state serialization (sandbox: no state docs exist — the
		// enrichment branch is live-only best effort, see scope note).
		res.JSON(200, []byte(dbxStateJSON(state)))
	}
}

// hDbxUnlink — DELETE /project/:project_id/dropbox/state (no-op ok).
func hDbxUnlink(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		seg, ok := dbxAuthzProject(a, cxt, res)
		if !ok {
			return
		}
		dbxRemoveState(cxt.Req.Context(), a, seg)
		res.JSON(200, []byte(`{"success":true}`))
	}
}

// hDbxLink — POST /project/:project_id/dropbox/link.
func hDbxLink(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, ok := dbxAuthzProject(a, cxt, res); !ok {
			return
		}
		uid := dbxUID(cxt)
		cred, ok := dbxGetCreds(cxt.Req.Context(), a, uid)
		if !ok || cred == nil {
			res.JSON(409, []byte(`{"error":"Not connected to Dropbox. Please connect your account first."}`))
			return
		}
		if _, derr := dbxDecryptToken(cred.Token); derr != nil {
			res.JSON(500, []byte(`{"error":"Token decryption failed"}`))
			return
		}
		// Live: checkConnection + state upsert + mirror export (scope note).
		res.JSON(500, []byte(`{"error":"Dropbox connection check failed"}`))
	}
}

// hDbxPull — POST /project/:project_id/dropbox/pull.
func hDbxPull(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		seg, ok := dbxAuthzProject(a, cxt, res)
		if !ok {
			return
		}
		hDbxProjectGate(a, cxt, res, seg)
	}
}

// hDbxPush — POST /project/:project_id/dropbox/push.
func hDbxPush(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		seg, ok := dbxAuthzProject(a, cxt, res)
		if !ok {
			return
		}
		hDbxProjectGate(a, cxt, res, seg)
	}
}

// hDbxFiles — GET /project/:project_id/dropbox/files.
func hDbxFiles(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		seg, ok := dbxAuthzProject(a, cxt, res)
		if !ok {
			return
		}
		hDbxProjectGate(a, cxt, res, seg)
	}
}

// hDbxProjectGate — shared pull/push/files pre-network gate:
// 409 "Project not linked to Dropbox" when the state doc is absent or
// not connected (the pinned sandbox behavior on all three routes).
func hDbxProjectGate(a *core.App, cxt *core.Cxt, res *core.Res, seg string) {
	state, ok := dbxGetState(cxt.Req.Context(), a, seg)
	if !ok {
		res.JSON(409, []byte(`{"error":"Project not linked to Dropbox"}`))
		return
	}
	if connected, _ := state["connected"].(bool); !connected {
		res.JSON(409, []byte(`{"error":"Project not linked to Dropbox"}`))
		return
	}
	// Live: credentials decrypt + Dropbox API mirror (scope note).
	res.JSON(500, []byte(`{"error":"Dropbox sync is not available in this environment"}`))
}

// hDbxNew — POST /project/new/dropbox.
func hDbxNew(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		body := dbxReadBody(cxt)
		name, ok := dbxStr(body, "projectName")
		if !ok || name == "" {
			res.JSON(400, []byte(`{"error":"projectName is required"}`))
			return
		}
		uid := dbxUID(cxt)
		cred, ok := dbxGetCreds(cxt.Req.Context(), a, uid)
		if !ok || cred == nil {
			res.JSON(409, []byte(`{"error":"Dropbox credentials not found"}`))
			return
		}
		// Live: import (scope note). Offline: the decrypt gate runs before
		// any Dropbox call; Node's catch passes the raw err.message through
		// for THIS route (unlike link/pull/push/files) — mirror that.
		if _, derr := dbxDecryptToken(cred.Token); derr != nil {
			res.JSON(500, []byte(fmt.Sprintf(`{"error":%s}`, jsonStr(derr.Error()))))
			return
		}
		res.JSON(500, []byte(`{"error":"Dropbox import failed"}`))
	}
}
