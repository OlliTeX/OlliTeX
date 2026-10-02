package gsync

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	ghi "ollitex/go/services/githubinterface"
)

// ---------- bridge client (Node GitServerClient.mjs — in-process) ----------
//
// TPDS→web merge (owner-approved 2026-09-29, TODO-d414c964): the git-provider
// bridge surface is ABSORBED INTO THE WEB BINARY. This client now calls the
// go/services/githubinterface package ops DIRECTLY (the same *GHI op surface
// NewGHIHandlerMux used to expose over the Node wire: /check /clone /push
// /commit /orgs /create-repo /list-repos /can-push /branch-head /commits).
// The standalone runit service + cmd binary are retired (see
// junk/runit-githubinterface-overleaf, junk/cmd-githubinterface) — there is
// no :4013 hop, no port to bind, no JSON serialization, and no localhost
// reachability class of bug.
//
// Op semantics are preserved 1:1 with the former wire: each GHI method
// returns (status, JSON body); non-2xx maps through gsBridgeFail exactly
// like the old HTTP 4xx/5xx responses, and the former MaxOps semaphore
// (503 "service busy") is kept in-process.
//
// Work root agreement: GHI.WorkRoot = gsWorkRoot() — the same env chain the
// web-side import/export dirs use (GSYNC_GHIF_WORK_ROOT ||
// GITHUBINTERFACE_WORKDIR_ROOT || /var/lib/overleaf/ghif), so the bridge
// resolveWorkDir and the web gsWorkRoot can never drift (006 pin).

type bridgeClient struct {
	g   *ghi.GHI
	sem chan struct{} // former bridge MaxOps semaphore (busy → 503 parity)
}

func newBridge(c *gsCfg) *bridgeClient {
	cfg := ghi.GHIConfig{WorkRoot: gsWorkRoot()}
	cfg.MaxOps = 8
	if v := os.Getenv("GITHUBINTERFACE_MAX_OPS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxOps = n
		}
	}
	return &bridgeClient{
		g:   ghi.NewGHI(cfg),
		sem: make(chan struct{}, cfg.MaxOps),
	}
}

// call — run one bridge op under the concurrency semaphore; the bridge's
// former 503 busy response is preserved verbatim for clients.
func (b *bridgeClient) call(ctx context.Context, fn func() (int, map[string]any)) (int, map[string]any) {
	select {
	case b.sem <- struct{}{}:
		defer func() { <-b.sem }()
		return fn()
	default:
		return 503, map[string]any{"error": "service busy; try again shortly"}
	}
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

// ---------- history client (Node HistoryManager.mjs — separate service) ----------
//
// The project-history service (:3050/:3054, project-history-go-overleaf) is
// NOT part of the TPDS merge (it stays a standalone service per its own
// cutover) — so this client keeps talking HTTP to it, exactly as before.

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

// ---------- ops (in-process bridge ops) ----------

func (b *bridgeClient) Check(ctx context.Context, serverUrl, username, token string) error {
	code, m := b.call(ctx, func() (int, map[string]any) {
		return b.g.Check(ctx, serverUrl, username, token)
	})
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
	code, m := b.call(ctx, func() (int, map[string]any) {
		return b.g.Clone(ctx, repoUrl, ref, targetDir, serverUrl, username, token)
	})
	if code >= 400 {
		return gsBridgeFail(code, m)
	}
	return nil
}

func (b *bridgeClient) Push(ctx context.Context, dir, remote, ref string, c *gsResolvedCred) error {
	code, m := b.call(ctx, func() (int, map[string]any) {
		return b.g.Push(ctx, dir, remote, ref, c.Username, c.Token)
	})
	if code >= 400 {
		return gsBridgeFail(code, m)
	}
	return nil
}

func (b *bridgeClient) Commit(ctx context.Context, dir string, files []string, message, authorName, authorEmail string, c *gsResolvedCred) (string, error) {
	code, m := b.call(ctx, func() (int, map[string]any) {
		return b.g.Commit(ctx, dir, files, message, authorName, authorEmail, c.Username)
	})
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
	code, m := b.call(ctx, func() (int, map[string]any) {
		return b.g.CreateRepo(ctx, c.ServerURL, c.Username, c.Token, opts.Name, opts.Description, isPublic, opts.Org)
	})
	if code >= 400 {
		return "", "", gsBridgeFail(code, m)
	}
	full, _ := m["full_name"].(string)
	db, _ := m["default_branch"].(string)
	return full, db, nil
}

func (b *bridgeClient) ListRepos(ctx context.Context, c *gsResolvedCred) ([]map[string]any, error) {
	code, m := b.call(ctx, func() (int, map[string]any) {
		return b.g.ListRepos(ctx, c.ServerURL, c.Username, c.Token)
	})
	if code >= 400 {
		return nil, gsBridgeFail(code, m)
	}
	// In-process: GHI returns a NATIVE `[]map[string]any` (not the JSON
	// decoded `[]any` the old HTTP wire produced) — accept both.
	v, ok := m["repos"]
	if !ok {
		return []map[string]any{}, nil
	}
	switch arr := v.(type) {
	case []map[string]any:
		return arr, nil
	case []any:
		out := []map[string]any{}
		for _, e := range arr {
			if em, ok2 := e.(map[string]any); ok2 {
				out = append(out, em)
			}
		}
		return out, nil
	}
	return []map[string]any{}, nil
}

func (b *bridgeClient) ListUserAndOrgs(ctx context.Context, c *gsResolvedCred) (string, []string, error) {
	code, m := b.call(ctx, func() (int, map[string]any) {
		return b.g.Orgs(ctx, c.ServerURL, c.Username, c.Token)
	})
	// Node: user: orgsData.user || stored username; orgs: ok ? [] : []
	orgs := []string{}
	switch arr := m["orgs"].(type) {
	case []string: // in-process native shape (GHI.Orgs)
		orgs = arr
	case []any:
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
	code, m := b.call(ctx, func() (int, map[string]any) {
		return b.g.CanPush(ctx, c.ServerURL, repo, c.Username, c.Token)
	})
	if code >= 400 {
		return false, nil // Node: 403/404 → false
	}
	if v, ok := m["can_push"].(bool); ok {
		return v, nil
	}
	return true, nil
}

func (b *bridgeClient) BranchHead(ctx context.Context, repo, branch string, c *gsResolvedCred) (string, error) {
	code, m := b.call(ctx, func() (int, map[string]any) {
		return b.g.BranchHead(ctx, c.ServerURL, repo, branch, c.Username, c.Token)
	})
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
	code, m := b.call(ctx, func() (int, map[string]any) {
		return b.g.Commits(ctx, c.ServerURL, repo, branch, since, 0, c.Username, c.Token)
	})
	if code >= 400 {
		return nil, false, gsBridgeFail(code, m)
	}
	commits := []gsCommitInfo{}
	// In-process: GHI.Commits returns a NATIVE `[]map[string]any` (old
	// HTTP wire: JSON `[]any`) — accept both.
	var entries []map[string]any
	switch v := m["commits"].(type) {
	case []map[string]any:
		entries = v
	case []any:
		for _, e := range v {
			if em, ok2 := e.(map[string]any); ok2 {
				entries = append(entries, em)
			}
		}
	}
	for _, em := range entries {
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
	div, _ := m["diverged"].(bool)
	return commits, div, nil
}
