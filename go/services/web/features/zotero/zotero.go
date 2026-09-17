// Package zotero implements the Node services/web/modules/zotero surface
// (ZoteroRouter.mjs + ZoteroController.mjs + ZoteroSection.mjs +
// TokenManager.mjs + ZoteroApiClient.mjs + ZoteroOAuth.mjs) on the Go web
// service.
//
// Routes (Node registration order; all requireLogin — anonymous is bounced
// by the core global chain before any handler runs, pinned P1):
//
//	GET    /user/zotero/groups              ensureZoteroEnabled getGroups
//	DELETE /user/zotero                                 unlink
//	GET    /user/zotero/status                                 getConnectionStatus
//	GET    /user/zotero/oauth             ensureZoteroEnabled oauth
//	GET    /user/zotero/oauth/callback                           oauthCallback
//	GET    /user/zotero/picker/libraries   ensureZoteroEnabled getPickerLibraries
//	GET    /user/zotero/picker/collections ensureZoteroEnabled getPickerCollections
//	GET    /user/zotero/picker/items       ensureZoteroEnabled getPickerItems
//	GET    /user/zotero/picker/bibtex      ensureZoteroEnabled getPickerBibtex
//
// Offline parity contract (e2e user is NOT zotero-linked → no zotero.org
// calls are ever made on the pinned paths):
//
//	disabled (site_settings.zotero.enabled === false — the e2e default):
//	  groups / oauth / picker/libraries|collections|items|bibtex
//	    → 403 text/html "Zotero is disabled on this site"
//	  status   → 200 application/json false
//	  unlink   → 200 text/plain OK
//	  callback → 403 application/json {"message":"Invalid OAuth token"}
//
//	enabled (site_settings.zotero.enabled === true):
//	  groups                → 200 application/json null
//	  picker/libraries      → 409 application/json {"message":"zotero_not_linked"}
//	  picker/collections    → 409 {"message":"zotero_not_linked"}
//	  picker/items          → 409 {"message":"zotero_not_linked"}
//	  picker/bibtex no-keys → 400 application/json {"message":"no items selected"}
//	  picker/bibtex keys    → 409 {"message":"zotero_not_linked"}
//	  status   → 200 application/json false
//	  unlink   → 200 text/plain OK
//	  callback → 403 {"message":"Invalid OAuth token"}
//
//	The live-network branches (linked user or the oauth requestToken start)
//
// ARE implemented faithfully but are NOT part of the offline gate.
package zotero

import (
	"context"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
)

func mustObjectID(hex string) primitive.ObjectID {
	oid, _ := primitive.ObjectIDFromHex(hex)
	return oid
}

func ctxWith(cxt *core.Cxt) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 35*time.Second)
	return ctx, cancel
}

func serve500(a *core.App, cxt *core.Cxt, res *core.Res) {
	if a.Render500 != nil {
		a.Render500(cxt, res)
		return
	}
	res.SendStatus(500)
}

func uidOf(cxt *core.Cxt) string {
	if cxt.Sess == nil {
		return ""
	}
	return cxt.Sess.UserIDHex()
}

// jsonMsg — res.status(code).json({ message: '<msg>' }) exact body.
func jsonMsg(res *core.Res, code int, msg string) {
	res.JSON(code, []byte(`{"message":"`+jsonString(msg)+`"}`))
}

// jsonString — minimal JSON string escape for messages (the pinned messages
// contain no special chars; this keeps the body byte-exact).
func jsonString(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			out = append(out, '\\', '"')
		case '\\':
			out = append(out, '\\', '\\')
		default:
			out = append(out, c)
		}
	}
	return string(out)
}

// apiErr mirrors Node's OError { status, message } for linked/error paths.
type apiErr struct {
	status int
	msg    string
}

func (e apiErr) Error() string {
	return e.msg
}

// ---------- the feature ----------

func Feature(a *core.App) core.Feature {
	return core.Feature{
		Name: "zotero",
		Routes: []core.Route{
			{Method: "GET", Path: "/user/zotero/groups", Handler: hGroups(a)},
			{Method: "DELETE", Path: "/user/zotero", Handler: hUnlink(a)},
			{Method: "GET", Path: "/user/zotero/status", Handler: hStatus(a)},
			{Method: "GET", Path: "/user/zotero/oauth", Handler: hOAuth(a)},
			{Method: "GET", Path: "/user/zotero/oauth/callback", Handler: hOAuthCallback(a)},
			{Method: "GET", Path: "/user/zotero/picker/libraries", Handler: hPickerLibraries(a)},
			{Method: "GET", Path: "/user/zotero/picker/collections", Handler: hPickerCollections(a)},
			{Method: "GET", Path: "/user/zotero/picker/items", Handler: hPickerItems(a)},
			{Method: "GET", Path: "/user/zotero/picker/bibtex", Handler: hPickerBibtex(a)},
		},
	}
}

// ---------- handlers (ZoteroController) ----------

// GET /user/zotero/status — getConnectionStatus. not linked → 200 false.
func hStatus(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx, cancel := ctxWith(cxt)
		defer cancel()
		apiKey, _, linked := userZoteroCreds(ctx, a, uidOf(cxt))
		if !linked {
			res.JSON(200, []byte("false"))
			return
		}
		// Linked: live check GET /keys/{apiKey}.
		if err := zoteroCheckKey(ctx, apiKey); err != nil {
			sendAPIErr(res, err)
			return
		}
		res.JSON(200, []byte("true"))
	}
}

// DELETE /user/zotero — unlink. not linked → 200 OK (no-op).
func hUnlink(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx, cancel := ctxWith(cxt)
		defer cancel()
		uid := uidOf(cxt)
		apiKey, _, linked := userZoteroCreds(ctx, a, uid)
		if linked {
			if uerr := unsetUserZotero(ctx, a, uid); uerr != nil {
				res.SendStatus(500)
				return
			}
			_ = zoteroRevokeKey(ctx, apiKey) // best-effort, errors ignored (Node parity)
		}
		res.SendStatus(200)
	}
}

// GET /user/zotero/groups — ensureZoteroEnabled + getGroups.
// disabled → 403 text/html; not linked → 200 null.
func hGroups(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !ensureEnabled(a, cxt, res) {
			return
		}
		ctx, cancel := ctxWith(cxt)
		defer cancel()
		apiKey, zoteroUID, linked := userZoteroCreds(ctx, a, uidOf(cxt))
		if !linked {
			res.JSON(200, []byte("null"))
			return
		}
		body, err := zoteroGroupsJSON(ctx, apiKey, zoteroUID)
		if err != nil {
			sendAPIErr(res, err)
			return
		}
		res.JSON(200, body)
	}
}

// GET /user/zotero/oauth — ensureZoteroEnabled + oauth start (live network).
func hOAuth(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !ensureEnabled(a, cxt, res) {
			return
		}
		ctx, cancel := ctxWith(cxt)
		defer cancel()
		cred, ok := zoteroAppCreds(ctx, a)
		if !ok || cred == nil {
			jsonMsg(res, 503, "Failed to start Zotero authorization (credentials not configured)")
			return
		}
		tok, err := zoteroRequestToken(ctx, cred.ClientKey, cred.ClientSecret, callbackURLFrom(cxt))
		if err != nil {
			jsonMsg(res, 400, "Failed to start Zotero authorization")
			return
		}
		cxt.Sess.Set("zoteroOAuth", map[string]any{
			"token":       tok.OAuthToken,
			"tokenSecret": tok.OAuthTokenSecret,
			"isPopup":     cxt.Req.URL.Query().Get("popup") == "1",
		})
		res.Redirect(cxt.Req, 302, zoteroAuthorizationURL(tok.OAuthToken))
	}
}

// GET /user/zotero/oauth/callback — verifier check → 403 or exchange+store.
func hOAuthCallback(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx, cancel := ctxWith(cxt)
		defer cancel()
		uid := uidOf(cxt)
		saved := popZoteroOAuth(cxt)
		if saved == nil || saved.token != cxt.Req.URL.Query().Get("oauth_token") {
			jsonMsg(res, 403, "Invalid OAuth token")
			return
		}
		cred, ok := zoteroAppCreds(ctx, a)
		if !ok || cred == nil {
			jsonMsg(res, 400, "Failed to obtain Zotero access token")
			return
		}
		acc, err := zoteroExchangeToken(ctx, cred.ClientKey, cred.ClientSecret,
			saved.token, saved.tokenSecret,
			cxt.Req.URL.Query().Get("oauth_verifier"))
		if err != nil {
			jsonMsg(res, 400, "Failed to obtain Zotero access token")
			return
		}
		if serr := storeCreds(ctx, a, uid, acc.AccessToken, acc.ZoteroUserID); serr != nil {
			jsonMsg(res, 400, "Failed to obtain Zotero access token")
			return
		}
		sendOAuthCallbackHTML(res, saved.isPopup)
	}
}

// GET /user/zotero/picker/libraries — gate + not linked → 409.
func hPickerLibraries(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !ensureEnabled(a, cxt, res) {
			return
		}
		ctx, cancel := ctxWith(cxt)
		defer cancel()
		apiKey, zoteroUID, linked := userZoteroCreds(ctx, a, uidOf(cxt))
		if !linked {
			jsonMsg(res, 409, "zotero_not_linked")
			return
		}
		body := []byte(`[{"id":"","kind":"user","name":"My Library"}]`)
		if groups, gerr := zoteroGroupList(ctx, apiKey, zoteroUID); gerr == nil {
			body = appendLibrariesAndGroups(body, groups)
		}
		res.JSON(200, body)
	}
}

// GET /user/zotero/picker/collections — gate + not linked → 409.
func hPickerCollections(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !ensureEnabled(a, cxt, res) {
			return
		}
		ctx, cancel := ctxWith(cxt)
		defer cancel()
		apiKey, zoteroUID, linked := userZoteroCreds(ctx, a, uidOf(cxt))
		if !linked {
			jsonMsg(res, 409, "zotero_not_linked")
			return
		}
		body, err := zoteroCollectionsJSON(ctx, apiKey, scopeFromQuery(cxt.Req.URL.Query()), zoteroUID)
		if err != nil {
			sendPickerErr(res, err)
			return
		}
		res.JSON(200, body)
	}
}

// GET /user/zotero/picker/items — gate + not linked → 409.
func hPickerItems(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !ensureEnabled(a, cxt, res) {
			return
		}
		ctx, cancel := ctxWith(cxt)
		defer cancel()
		apiKey, zoteroUID, linked := userZoteroCreds(ctx, a, uidOf(cxt))
		if !linked {
			jsonMsg(res, 409, "zotero_not_linked")
			return
		}
		q := cxt.Req.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		start, _ := strconv.Atoi(q.Get("start"))
		body, err := zoteroItemsJSON(ctx, apiKey, scopeFromQuery(q), zoteroUID, q.Get("collection"), limit, start)
		if err != nil {
			sendPickerErr(res, err)
			return
		}
		res.JSON(200, body)
	}
}

// GET /user/zotero/picker/bibtex — gate + keys?→400 + not linked → 409.
func hPickerBibtex(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !ensureEnabled(a, cxt, res) {
			return
		}
		// Node checks the keys BEFORE the linked/credentials state.
		keys := parseKeys(cxt.Req.URL.Query().Get("keys"))
		if len(keys) == 0 {
			jsonMsg(res, 400, "no items selected")
			return
		}
		ctx, cancel := ctxWith(cxt)
		defer cancel()
		apiKey, zoteroUID, linked := userZoteroCreds(ctx, a, uidOf(cxt))
		if !linked {
			jsonMsg(res, 409, "zotero_not_linked")
			return
		}
		bibtex, err := zoteroItemsBibtex(ctx, apiKey, scopeFromQuery(cxt.Req.URL.Query()), zoteroUID, keys)
		if err != nil {
			sendPickerErr(res, err)
			return
		}
		res.JSON(200, []byte(`{"bibtex":"`+jsonString(bibtex)+`"}`))
	}
}
