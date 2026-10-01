package gsync

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/features/projectlist"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// ---------- work root (bridge clone/commit/push paths) ----------

func gsWorkRoot() string {
	if v := os.Getenv("GSYNC_GHIF_WORK_ROOT"); v != "" {
		return v
	}
	if v := os.Getenv("GITHUBINTERFACE_WORKDIR_ROOT"); v != "" {
		return v
	}
	return filepath.Join("/var/lib/overleaf", "ghif")
}

func gsRandHex(n int) string {
	b := make([]byte, n/2)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------- small helpers ----------

func gsCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 12*time.Minute)
}

func gsSessionUID(cxt *core.Cxt) string {
	if cxt.Sess == nil {
		return ""
	}
	_, uid := core.PassportUser(cxt.Sess)
	return uid
}

func gsJSONArr(vals ...any) string {
	b, _ := json.Marshal(vals)
	return string(b)
}

// gsAssertHttpServerUrl — Node assertHttpServerUrl (P0-4/C5).
func gsAssertHttpServerUrl(u string) error {
	parsed, err := url.Parse(u)
	if err != nil {
		return &gsError{Status: 400, Message: "Server URL must be http(s)"}
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return &gsError{Status: 400, Message: "Server URL must be http(s)"}
	}
	return nil
}

// gsMapPatCheckError — Node mapPatCheckError (P0-9/U2).
func gsMapPatCheckError(err *gsError) string {
	if err == nil {
		return "Connection check failed"
	}
	switch err.Status {
	case 401:
		return "Token invalid or expired. Re-link with a fresh PAT."
	case 403:
		return "Token valid but lacks permission. Check the required scopes in the form help."
	case 404:
		return "Repository not found or invalid URL."
	}
	return truncate500(err.Message)
}

func truncate500(s string) string {
	if r := []rune(s); len(r) > 500 {
		return string(r[:500])
	}
	return s
}

// ---------- status / servers ----------

// statusHandler — GET /user/github-sync/status (Node getConnectionStatus).
func statusHandler(cxt *core.Cxt, res *core.Res) {
	uid := gsSessionUID(cxt)
	doc := gsGetCredsDoc(cxt.Req.Context(), cxt.A, uid)
	providers := gsGetPublicServers(doc)
	linked, oauthUser := gsGetOAuth(doc)
	connected := len(providers) > 0 || linked
	b, _ := json.Marshal(map[string]any{
		"connected": connected,
		"providers": providers,
		"oauth":     map[string]any{"linked": linked, "username": oauthUser},
	})
	res.JSON(200, b)
}

// getServerHandler — GET /user/git-servers (Node getUserServers).
func getServerHandler(cxt *core.Cxt, res *core.Res) {
	uid := gsSessionUID(cxt)
	doc := gsGetCredsDoc(cxt.Req.Context(), cxt.A, uid)
	providers := gsGetPublicServers(doc)
	b, _ := json.Marshal(providers)
	res.JSON(200, b)
}

// addServerHandler — POST /user/git-servers (Node addServerConfig).
func addServerHandler(cxt *core.Cxt, res *core.Res) {
	uid := gsSessionUID(cxt)
	body, err := gsBody(cxt.Req, 10<<20)
	if err != nil {
		res.JSON(400, []byte(`{"message":"Bad request"}`))
		return
	}
	provider := gsStr(body, "provider")
	u := gsStr(body, "url")
	username := gsStr(body, "username")
	if provider == "" || u == "" || username == "" {
		res.JSON(400, []byte(`{"message":"Missing required fields"}`))
		return
	}
	if err := gsAssertHttpServerUrl(u); err != nil {
		var ge *gsError
		if asGSerr(err, &ge) {
			res.JSON(ge.Status, []byte(`{"message":`+gsJSONStr(ge.Message)+`}`))
			return
		}
		res.JSON(500, []byte(`{"message":"internal error"}`))
		return
	}
	if err := gsAddServerConfig(cxt.Req.Context(), cxt.A, uid, provider, u, username); err != nil {
		gsResErr(res, err)
		return
	}
	res.JSON(200, []byte(`{"success":true}`))
}

// removeServerHandler — DELETE /user/git-servers/:id
// (Node removeServerConfig; id = provider:url[:username]).
func removeServerHandler(cxt *core.Cxt, res *core.Res) {
	uid := gsSessionUID(cxt)
	id := cxt.Params["id"]
	sep := strings.Index(id, ":")
	if sep < 1 {
		res.JSON(400, []byte(`{"message":"Invalid server ID format"}`))
		return
	}
	provider := id[:sep]
	rest := id[sep+1:]
	lastSep := strings.LastIndex(rest, ":")
	hasUsername := lastSep > 7
	var serverUrl, username string
	var removeUsername bool
	if hasUsername {
		serverUrl = rest[:lastSep]
		username = rest[lastSep+1:]
		removeUsername = true
	} else {
		serverUrl = rest
	}
	if err := gsRemovePAT(cxt.Req.Context(), cxt.A, uid, provider, serverUrl, username, removeUsername); err != nil {
		gsResErr(res, err)
		return
	}
	res.JSON(200, []byte(`{"success":true}`))
}

// linkPATHandler — POST /user/git-pat/link (Node linkPAT).
func linkPATHandler(cxt *core.Cxt, res *core.Res) {
	uid := gsSessionUID(cxt)
	body, err := gsBody(cxt.Req, 10<<20)
	if err != nil {
		res.JSON(400, []byte(`{"message":"Bad request"}`))
		return
	}
	provider := gsStr(body, "provider")
	u := gsStr(body, "url")
	username := gsStr(body, "username")
	pat := gsStr(body, "pat")
	if provider == "" || u == "" || username == "" || pat == "" {
		res.JSON(400, []byte(`{"message":"Missing required fields"}`))
		return
	}
	if err := gsAssertHttpServerUrl(u); err != nil {
		var ge *gsError
		if asGSerr(err, &ge) {
			res.JSON(ge.Status, []byte(`{"message":`+gsJSONStr(ge.Message)+`}`))
			return
		}
		res.JSON(500, []byte(`{"message":"internal error"}`))
		return
	}
	if err := gsSavePAT(cxt.Req.Context(), cxt.A, uid, provider, u, username, pat); err != nil {
		gsResErr(res, err)
		return
	}
	// Non-fatal check
	check := map[string]any{"ok": false, "message": "not checked"}
	if cerr := gsTestServerConnection(cxt.Req.Context(), cxt.A, uid, provider, u, username, ""); cerr != nil {
		ge := &gsError{Message: cerr.Error()}
		if e, ok2 := cerr.(*gsError); ok2 {
			ge = e
		}
		check = map[string]any{"ok": false, "message": gsMapPatCheckError(ge)}
	} else {
		check = map[string]any{"ok": true, "login": ""}
	}
	b, _ := json.Marshal(map[string]any{"success": true, "check": check})
	res.JSON(200, b)
}

// testServerHandler — POST /user/git-servers/test (Node testServer).
func testServerHandler(cxt *core.Cxt, res *core.Res) {
	uid := gsSessionUID(cxt)
	body, err := gsBody(cxt.Req, 10<<20)
	if err != nil {
		res.JSON(400, []byte(`{"message":"Bad request"}`))
		return
	}
	provider := gsStr(body, "provider")
	u := gsStr(body, "url")
	username := gsStr(body, "username")
	if provider == "" || u == "" {
		res.JSON(400, []byte(`{"message":"Missing provider or url"}`))
		return
	}
	if err := gsTestServerConnection(cxt.Req.Context(), cxt.A, uid, provider, u, username, ""); err != nil {
		ge := &gsError{Message: err.Error()}
		if e, ok2 := err.(*gsError); ok2 {
			ge = e
		}
		res.JSON(200, []byte(`{"ok":false,"message":`+gsJSONStr(gsMapPatCheckError(ge))+`}`))
		return
	}
	res.JSON(200, []byte(`{"ok":true}`))
}

// gsTestServerConnection — Node testServerConnection.
func gsTestServerConnection(ctx context.Context, a *core.App, uid, provider, serverUrl, username, pat string) error {
	var token, uname string
	if pat != "" {
		token = pat
		uname = username
	} else {
		cred, err := gsGetPATCred(ctx, a, uid, provider, serverUrl, username)
		if err != nil {
			return err
		}
		token = cred.Token
		if username == "" {
			uname = cred.Username
		} else {
			uname = username
		}
	}
	return gsBridge().Check(ctx, serverUrl, uname, token)
}

// ---------- orgs / repos ----------

// orgsHandler — GET /user/github-sync/orgs (Node getUserAndOrgs).
func orgsHandler(cxt *core.Cxt, res *core.Res) {
	uid := gsSessionUID(cxt)
	q := cxt.Req.URL.Query()
	provider := q.Get("provider")
	serverUrl := q.Get("serverUrl")
	username := q.Get("username")
	cred, err := gsResolveCreds(cxt.Req.Context(), cxt.A, uid, provider, serverUrl, username)
	if err != nil {
		gsResErr(res, err)
		return
	}
	user, orgs, err := gsBridge().ListUserAndOrgs(cxt.Req.Context(), cred)
	if err != nil {
		gsResErr(res, err)
		return
	}
	b, _ := json.Marshal(map[string]any{"user": user, "orgs": orgs})
	res.JSON(200, b)
}

// reposHandler — GET /user/github-sync/repos (Node listUserRepos).
func reposHandler(cxt *core.Cxt, res *core.Res) {
	uid := gsSessionUID(cxt)
	q := cxt.Req.URL.Query()
	provider := q.Get("provider")
	serverUrl := q.Get("serverUrl")
	username := q.Get("username")
	cred, err := gsResolveCreds(cxt.Req.Context(), cxt.A, uid, provider, serverUrl, username)
	if err != nil {
		var ge *gsError
		if asGSerr(err, &ge) && ge.Status == 400 {
			// Node: InvalidTokenError → res.json(null)
			res.JSON(200, []byte(`null`))
			return
		}
		gsResErr(res, err)
		return
	}
	repos, err := gsBridge().ListRepos(cxt.Req.Context(), cred)
	if err != nil {
		gsResErr(res, err)
		return
	}
	b, _ := json.Marshal(map[string]any{"repos": repos})
	res.JSON(200, b)
}

// gsResolveCreds — Node GitHubSyncHandler.resolveCreds.
func gsResolveCreds(ctx context.Context, a *core.App, uid, provider, serverUrl, usernameHint string) (*gsResolvedCred, error) {
	prov := provider
	if prov == "" {
		prov = "github"
	}
	u := gsNormURL(serverUrl)
	doc := gsGetCredsDoc(ctx, a, uid)
	if u == "" {
		for _, s := range gsGetPublicServers(doc) {
			if s.Provider == prov {
				u = s.URL
				break
			}
		}
		if u == "" {
			u = gsDefaultServerUrl(prov)
		}
	}
	wantU := strings.TrimSpace(usernameHint)
	cred, err := gsGetPATCred(ctx, a, uid, prov, u, wantU)
	if err != nil {
		return nil, err
	}
	return &gsResolvedCred{
		Provider:  prov,
		Token:     cred.Token,
		ServerURL: cred.ServerURL,
		Username:  cred.Username,
		Source:    cred.Source,
	}, nil
}

var _ = gsJSONArr
var _ = gsRandHex
var _ = gsWorkRoot
var _ = bson.ObjectID{}
var _ = projectlist.GSProjFile{}
var _ = fmt.Sprintf
