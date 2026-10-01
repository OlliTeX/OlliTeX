package gsync

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ---------- bridge client (Node GitServerClient.mjs) ----------
//
//	Node posts JSON to GITHUBINTERFACE_API_URL (default http://localhost:4013)
//	with an x-service-token header when SHARED_SERVICE_TOKEN is set. The Go
//	bridge (go/services/githubinterface) speaks the same routes:
//	/check /clone /push /pull /commit /orgs /create-repo /list-repos
//	/can-push /branch-head /commits.
//
//	Bridge error wire: {error, detail?} — the Node controller passes
//	`detail` (or error) through in JSON bodies.

type bridgeClient struct {
	base   string
	tok    string
	client *http.Client
}

func newBridge(c *gsCfg) *bridgeClient {
	return &bridgeClient{
		base:   c.bridgeBase,
		tok:    c.serviceTok,
		client: &http.Client{Timeout: 10 * time.Minute},
	}
}

func (b *bridgeClient) post(ctx context.Context, path string, reqBody map[string]any) (int, map[string]any, error) {
	bj, err := json.Marshal(reqBody)
	if err != nil {
		return 0, nil, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, b.base+path, bytes.NewReader(bj))
	if err != nil {
		return 0, nil, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	if b.tok != "" {
		hreq.Header.Set("x-service-token", b.tok)
	}
	resp, err := b.client.Do(hreq)
	if err != nil {
		return 0, nil, &gsError{Status: 500, Message: "request failed", Detail: err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	m := map[string]any{}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &m)
	}
	return resp.StatusCode, m, nil
}

// gsBridgeFail — non-2xx bridge response → typed error (status 401/403/404
// preserved for the mapPatCheckError mapping; the rest is 500 detail
// passthrough, matching the Node controller behavior).
func gsBridgeFail(code int, m map[string]any) *gsError {
	detail := ""
	if d, ok := m["detail"]; ok {
		switch v := d.(type) {
		case string:
			detail = v
		default:
			bb, _ := json.Marshal(v)
			detail = string(bb)
		}
	}
	msg, _ := m["error"].(string)
	if msg == "" {
		msg = "request failed"
	}
	st := code
	if st < 400 || st > 599 {
		st = 500
	}
	return &gsError{Status: st, Message: msg, Detail: detail}
}

// ---------- ops ----------

func (b *bridgeClient) Check(ctx context.Context, serverUrl, username, token string) error {
	code, m, err := b.post(ctx, "/check", map[string]any{
		"server_url": serverUrl, "username": username, "token": token,
	})
	if err != nil {
		return err
	}
	if code >= 400 {
		return gsBridgeFail(code, m)
	}
	return nil
}

func (b *bridgeClient) Clone(ctx context.Context, repoUrlOrName, ref, targetDir, serverUrl, username, token string) error {
	// Node: full URL or serverUrl/fullName.git
	repoUrl := repoUrlOrName
	if !strings.HasPrefix(repoUrl, "http://") && !strings.HasPrefix(repoUrl, "https://") {
		repoUrl = strings.TrimRight(serverUrl, "/") + "/" + repoUrlOrName + ".git"
	}
	code, m, err := b.post(ctx, "/clone", map[string]any{
		"repo_url":   repoUrl,
		"ref":        ref,
		"target_dir": targetDir,
		"server_url": serverUrl,
		"username":   username,
		"token":      token,
	})
	if err != nil {
		return err
	}
	if code >= 400 {
		return gsBridgeFail(code, m)
	}
	return nil
}

func (b *bridgeClient) Push(ctx context.Context, dir, remote, ref string, c *gsResolvedCred) error {
	code, m, err := b.post(ctx, "/push", map[string]any{
		"dir": dir, "remote": remote, "ref": ref,
		"server_url": c.ServerURL, "username": c.Username, "token": c.Token,
	})
	if err != nil {
		return err
	}
	if code >= 400 {
		return gsBridgeFail(code, m)
	}
	return nil
}

func (b *bridgeClient) Commit(ctx context.Context, dir string, files []string, message, authorName, authorEmail string, c *gsResolvedCred) (string, error) {
	code, m, err := b.post(ctx, "/commit", map[string]any{
		"dir": dir, "files": files, "message": message,
		"author":   map[string]any{"name": authorName, "email": authorEmail},
		"username": c.Username, "server_url": c.ServerURL, "token": c.Token,
	})
	if err != nil {
		return "", err
	}
	if code >= 400 {
		return "", gsBridgeFail(code, m)
	}
	return m["commit_sha"].(string), nil
}

type gsRepoOptions struct {
	Name        string
	Description string
	IsPublic    *bool
	Org         string
}

func (b *bridgeClient) CreateRepo(ctx context.Context, opts *gsRepoOptions, c *gsResolvedCred) (string, string, error) {
	isPublic := true
	if opts.IsPublic != nil {
		isPublic = *opts.IsPublic
	}
	body := map[string]any{
		"server_url": c.ServerURL, "username": c.Username, "token": c.Token,
		"name": opts.Name, "description": opts.Description,
		"is_public": isPublic,
	}
	if opts.Org != "" {
		body["org"] = opts.Org
	}
	code, m, err := b.post(ctx, "/create-repo", body)
	if err != nil {
		return "", "", err
	}
	if code >= 400 {
		return "", "", gsBridgeFail(code, m)
	}
	full, _ := m["full_name"].(string)
	db, _ := m["default_branch"].(string)
	return full, db, nil
}

func (b *bridgeClient) ListRepos(ctx context.Context, c *gsResolvedCred) ([]map[string]any, error) {
	code, m, err := b.post(ctx, "/list-repos", map[string]any{
		"server_url": c.ServerURL, "username": c.Username, "token": c.Token,
	})
	if err != nil {
		return nil, err
	}
	if code >= 400 {
		return nil, gsBridgeFail(code, m)
	}
	out := []map[string]any{}
	if arr, ok := m["repos"].([]any); ok {
		for _, e := range arr {
			if mm, ok2 := e.(map[string]any); ok2 {
				out = append(out, mm)
			}
		}
	}
	return out, nil
}

func (b *bridgeClient) ListUserAndOrgs(ctx context.Context, c *gsResolvedCred) (string, []string, error) {
	code, m, err := b.post(ctx, "/orgs", map[string]any{
		"server_url": c.ServerURL, "username": c.Username, "token": c.Token,
	})
	if err != nil {
		return "", nil, err
	}
	// Node: user: orgsData.user || stored username; orgs: ok ? [] : []
	orgs := []string{}
	if arr, ok := m["orgs"].([]any); ok {
		for _, e := range arr {
			if s, ok2 := e.(string); ok2 {
				orgs = append(orgs, s)
			}
		}
	}
	user, _ := m["user"].(string)
	if user == "" {
		user = c.Username
	}
	if code >= 400 {
		orgs = []string{}
	}
	return user, orgs, nil
}

func (b *bridgeClient) CanPush(ctx context.Context, repo string, c *gsResolvedCred) (bool, error) {
	code, m, err := b.post(ctx, "/can-push", map[string]any{
		"server_url": c.ServerURL, "repo": repo,
		"username": c.Username, "token": c.Token,
	})
	if err != nil {
		return false, nil // Node: 403/404 → false; transport err → throw
	}
	if code >= 400 {
		return false, nil
	}
	if v, ok := m["can_push"].(bool); ok {
		return v, nil
	}
	return true, nil
}

func (b *bridgeClient) BranchHead(ctx context.Context, repo, branch string, c *gsResolvedCred) (string, error) {
	code, m, err := b.post(ctx, "/branch-head", map[string]any{
		"server_url": c.ServerURL, "repo": repo, "branch": branch,
		"username": c.Username, "token": c.Token,
	})
	if err != nil {
		return "", err
	}
	if code >= 400 {
		return "", gsBridgeFail(code, m)
	}
	s, _ := m["sha"].(string)
	return s, nil
}

type gsCommitInfo struct {
	SHA     string         `json:"sha"`
	Message string         `json:"message"`
	Author  map[string]any `json:"author,omitempty"`
}

func (b *bridgeClient) CommitsSince(ctx context.Context, repo, branch, since string, c *gsResolvedCred) ([]gsCommitInfo, bool, error) {
	code, m, err := b.post(ctx, "/commits", map[string]any{
		"server_url": c.ServerURL, "repo": repo, "branch": branch,
		"since":    since,
		"username": c.Username, "token": c.Token,
	})
	if err != nil {
		return nil, false, err
	}
	if code >= 400 {
		return nil, false, gsBridgeFail(code, m)
	}
	commits := []gsCommitInfo{}
	if arr, ok := m["commits"].([]any); ok {
		for _, e := range arr {
			if em, ok2 := e.(map[string]any); ok2 {
				var ci gsCommitInfo
				if s, ok3 := em["sha"].(string); ok3 {
					ci.SHA = s
				}
				if s, ok3 := em["message"].(string); ok3 {
					ci.Message = s
				}
				if am, ok3 := em["author"].(map[string]any); ok3 {
					ci.Author = am
				}
				commits = append(commits, ci)
			}
		}
	}
	div, _ := m["diverged"].(bool)
	return commits, div, nil
}

// ---------- history client (Node HistoryManager.mjs) ----------

type histClient struct {
	base   string
	client *http.Client
}

func newHist(c *gsCfg) *histClient {
	return &histClient{base: c.histBase, client: &http.Client{Timeout: 5 * time.Minute}}
}

func (h *histClient) do(ctx context.Context, method, path string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, h.base+path, nil)
	if err != nil {
		return 0, nil, err
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	return resp.StatusCode, b, nil
}

var _ = strconv.Itoa
var _ = url.Values{}
