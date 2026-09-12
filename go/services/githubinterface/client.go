package githubinterface

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// --- client (1:1 with the /routes in server.mjs) ----------------------------

type GHI struct {
	Cfg GHIConfig
}

func NewGHI(cfg GHIConfig) *GHI {
	cfg.withDefaults()
	if err := os.MkdirAll(cfg.WorkRoot, 0o777); err != nil {
		fmt.Printf("warn: could not pre-create githubinterface work root: %v\n", err)
	}
	return &GHI{Cfg: cfg}
}

// Check verifies credentials (REST then git protocol fallback). 1:1.
func (g *GHI) Check(ctx context.Context, serverUrl, username, token string) (int, map[string]interface{}) {
	base := detectApiBase(ctx, g.Cfg.HTTPClient, serverUrl)
	if code, body, err := ghiHTTP(ctx, g.Cfg.HTTPClient, http.MethodGet, base+"/user", apiHeaders(token, base), nil); err == nil {
		if code == 200 {
			var j struct {
				Login    string `json:"login"`
				Username string `json:"username"`
			}
			_ = json.Unmarshal(body, &j)
			login := j.Login
			if login == "" {
				login = j.Username
			}
			return 200, map[string]interface{}{"status": "ok", "message": "Connection successful", "login": login}
		}
		if code == 401 || code == 403 {
			return 401, map[string]interface{}{"status": "error", "error": "Token rejected by server", "detail": scrubDetail(string(body), token)}
		}
	}
	if _, gerr := runGit(ctx, g.Cfg, []string{"ls-remote", "--exit-code", serverUrl}, gitOpts{user: username, pass: token}); gerr != nil {
		return 401, map[string]interface{}{"status": "error", "error": "Token rejected by server (git protocol fallback)"}
	}
	return 200, map[string]interface{}{"status": "ok", "message": "Connection successful"}
}

// Clone mirrors /clone.
func (g *GHI) Clone(ctx context.Context, repoUrl, ref, targetDir, serverUrl, username, password string) (int, map[string]interface{}) {
	if repoUrl == "" || targetDir == "" {
		return 400, map[string]interface{}{"error": "Missing required fields: repo_url and target_dir"}
	}
	target, err := resolveWorkDir(g.Cfg.WorkRoot, targetDir)
	if err != nil {
		return ghiStatusCode(err), map[string]interface{}{"error": err.(*GHIError).Message}
	}
	if serverUrl != "" && assertGitServerUrl(serverUrl) == "" {
		return 400, map[string]interface{}{"error": "server_url must be an http(s) git server URL"}
	}
	if !isAllowedServerUrl(repoUrl) {
		return 400, map[string]interface{}{"error": "repo_url must be an http(s) URL"}
	}
	if serverUrl != "" {
		eu, _ := url.Parse(serverUrl)
		fu, _ := url.Parse(repoUrl)
		if strings.ToLower(fu.Hostname()) != strings.ToLower(eu.Hostname()) {
			return 400, map[string]interface{}{"error": "repo_url host must match server_url"}
		}
	} else {
		ru, _ := url.Parse(repoUrl)
		if ru.Scheme != "https" {
			return 400, map[string]interface{}{"error": "insecure repo_url without server_url"}
		}
	}
	cloneArgs := []string{"clone", "--progress"}
	if ref != "" && ref != "HEAD" {
		cloneArgs = append(cloneArgs, "--branch="+ref)
	}
	cloneArgs = append(cloneArgs, repoUrl, target)
	if _, gerr := runGit(ctx, g.Cfg, cloneArgs, gitOpts{user: username, pass: password}); gerr != nil {
		return 500, map[string]interface{}{"error": gerr.Error()}
	}
	return 200, map[string]interface{}{"success": true, "message": "Repository cloned successfully"}
}

// Push mirrors /push.
func (g *GHI) Push(ctx context.Context, dir, remote, ref, username, password string) (int, map[string]interface{}) {
	if dir == "" {
		return 400, map[string]interface{}{"error": "Missing required field: dir"}
	}
	if remote != "origin" {
		return 400, map[string]interface{}{"error": "only origin remote supported"}
	}
	if !validRef(ref) {
		return 400, map[string]interface{}{"error": "invalid ref"}
	}
	workDir, err := resolveWorkDir(g.Cfg.WorkRoot, dir)
	if err != nil {
		return ghiStatusCode(err), map[string]interface{}{"error": err.(*GHIError).Message}
	}
	if _, gerr := runGit(ctx, g.Cfg, []string{"push", "origin", ref}, gitOpts{user: username, pass: password, cwd: workDir}); gerr != nil {
		return 500, map[string]interface{}{"error": gerr.Error()}
	}
	return 200, map[string]interface{}{"success": true, "message": "Push completed successfully"}
}

// Pull mirrors /pull.
func (g *GHI) Pull(ctx context.Context, dir, remote, ref, username, password string) (int, map[string]interface{}) {
	if dir == "" {
		return 400, map[string]interface{}{"error": "Missing required field: dir"}
	}
	if remote != "origin" {
		return 400, map[string]interface{}{"error": "only origin remote supported"}
	}
	if !validRef(ref) {
		return 400, map[string]interface{}{"error": "invalid ref"}
	}
	workDir, err := resolveWorkDir(g.Cfg.WorkRoot, dir)
	if err != nil {
		return ghiStatusCode(err), map[string]interface{}{"error": err.(*GHIError).Message}
	}
	if _, gerr := runGit(ctx, g.Cfg, []string{"pull", "origin", ref}, gitOpts{user: username, pass: password, cwd: workDir}); gerr != nil {
		return 500, map[string]interface{}{"error": gerr.Error()}
	}
	return 200, map[string]interface{}{"success": true, "message": "Pull completed successfully"}
}

// Commit mirrors /commit (stages + commits; content must pre-exist). 1:1.
func (g *GHI) Commit(ctx context.Context, dir string, files []string, message string, authorName, authorEmail, username string) (int, map[string]interface{}) {
	if dir == "" || len(files) == 0 || message == "" {
		return 400, map[string]interface{}{"error": "Missing required fields: dir, files, and message"}
	}
	workDir, err := resolveWorkDir(g.Cfg.WorkRoot, dir)
	if err != nil {
		return ghiStatusCode(err), map[string]interface{}{"error": err.(*GHIError).Message}
	}
	var missing []string
	for _, p := range files {
		if p == "" {
			continue
		}
		if _, serr := os.Stat(filepath.Join(workDir, p)); serr != nil {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		n := len(missing)
		if n > 10 {
			n = 10
		}
		return 400, map[string]interface{}{
			"error": "files not found in dir (content must be written before /commit): " + strings.Join(missing[:n], ", "),
		}
	}
	if authorName == "" {
		if username != "" {
			authorName = username
		} else {
			authorName = "Overleaf"
		}
	}
	email := identityEmail(authorName, authorEmail)
	if _, aerr := runGit(ctx, g.Cfg, append([]string{"add", "--"}, files...), gitOpts{cwd: workDir}); aerr != nil {
		return 500, map[string]interface{}{"error": aerr.Error()}
	}
	extraEnv := []string{
		"GIT_AUTHOR_NAME=" + authorName, "GIT_AUTHOR_EMAIL=" + email,
		"GIT_COMMITTER_NAME=" + authorName, "GIT_COMMITTER_EMAIL=" + email,
	}
	commitArgs := []string{"commit", "-m", message, "--author=" + authorName + " <" + email + ">"}
	if _, cerr := runGit(ctx, g.Cfg, commitArgs, gitOpts{cwd: workDir, extraEnv: extraEnv}); cerr != nil {
		return 500, map[string]interface{}{"error": cerr.Error()}
	}
	shaOut, serr := runGit(ctx, g.Cfg, []string{"rev-parse", "HEAD"}, gitOpts{cwd: workDir})
	if serr != nil {
		return 500, map[string]interface{}{"error": serr.Error()}
	}
	return 200, map[string]interface{}{"success": true, "commit_sha": strings.TrimSpace(shaOut)}
}

// logLine is one parsed git-log entry (format: %H %an %ae %aI %P %s).
type logLine struct {
	SHA     string
	Name    string
	Email   string
	Date    string
	Parents []string
	Subject string
}

func parseGitLog6(stdout string) []logLine {
	var out []logLine
	for _, raw := range strings.Split(stdout, "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\x1f")
		for len(parts) < 6 {
			parts = append(parts, "")
		}
		par := []string{}
		if parts[4] != "" {
			par = strings.Fields(parts[4])
		}
		out = append(out, logLine{SHA: parts[0], Name: parts[1], Email: parts[2], Date: parts[3], Parents: par, Subject: parts[5]})
	}
	return out
}

const ghiLogFormat = "--pretty=format:%H%x1f%an%x1f%ae%x1f%aI%x1f%P%x1f%s"

// Log mirrors /log.
func (g *GHI) Log(ctx context.Context, dir, ref string, limit int, page int) (int, map[string]interface{}) {
	if dir == "" {
		return 400, map[string]interface{}{"error": "Missing required field: dir"}
	}
	workDir, err := resolveWorkDir(g.Cfg.WorkRoot, dir)
	if err != nil {
		return ghiStatusCode(err), map[string]interface{}{"error": err.(*GHIError).Message}
	}
	if limit <= 0 {
		limit = 50
	}
	if page <= 0 {
		page = 1
	}
	if ref == "" {
		ref = "HEAD"
	}
	skip := (page - 1) * limit
	args := []string{"log", "-n", strconv.Itoa(limit + skip), ref, ghiLogFormat}
	stdout, gerr := runGit(ctx, g.Cfg, args, gitOpts{cwd: workDir})
	if gerr != nil {
		return 500, map[string]interface{}{"error": gerr.Error()}
	}
	all := parseGitLog6(stdout)
	start := skip
	if start > len(all) {
		start = len(all)
	}
	end := start + limit
	if end > len(all) {
		end = len(all)
	}
	slice := all[start:end]
	commits := make([]map[string]interface{}, 0, len(slice))
	for _, c := range slice {
		commits = append(commits, map[string]interface{}{
			"sha":     c.SHA,
			"message": c.Subject,
			"author":  map[string]interface{}{"name": c.Name, "email": c.Email, "date": c.Date},
			"parents": c.Parents,
		})
	}
	return 200, map[string]interface{}{
		"commits":      commits,
		"has_more":     len(commits) >= limit,
		"current_page": page,
	}
}

// Status mirrors /status.
func (g *GHI) Status(ctx context.Context, dir string) (int, map[string]interface{}) {
	if dir == "" {
		return 400, map[string]interface{}{"error": "Missing required field: dir"}
	}
	workDir, err := resolveWorkDir(g.Cfg.WorkRoot, dir)
	if err != nil {
		return ghiStatusCode(err), map[string]interface{}{"error": err.(*GHIError).Message}
	}
	branch, _ := runGit(ctx, g.Cfg, []string{"symbolic-ref", "--short", "HEAD"}, gitOpts{cwd: workDir})
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = "(unknown)"
	}
	var ahead, behind interface{}
	if branch != "(unknown)" {
		out, gerr := runGit(ctx, g.Cfg, []string{"rev-list", "--left-right", "--count", "HEAD...origin/" + branch}, gitOpts{cwd: workDir})
		if gerr == nil {
			if fields := strings.Fields(strings.TrimSpace(out)); len(fields) == 2 {
				if a, e1 := strconv.Atoi(fields[0]); e1 == nil {
					ahead = a
				}
				if b, e2 := strconv.Atoi(fields[1]); e2 == nil {
					behind = b
				}
			}
		}
	}
	uncommitted := []map[string]interface{}{}
	if out, gerr := runGit(ctx, g.Cfg, []string{"status", "--porcelain"}, gitOpts{cwd: workDir}); gerr == nil {
		for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			if len(line) < 4 || strings.TrimSpace(line) == "" {
				continue
			}
			path := strings.TrimSpace(line[3:])
			if path == "" {
				continue
			}
			status := mapPorcelain(line[:2])
			if status == "" {
				status = "?"
			}
			uncommitted = append(uncommitted, map[string]interface{}{"path": path, "status": status})
		}
	}
	return 200, map[string]interface{}{
		"branch":            branch,
		"ahead":             ahead,
		"behind":            behind,
		"uncommitted_files": uncommitted,
	}
}

func mapPorcelain(xy string) string {
	if len(xy) < 2 {
		return ""
	}
	x, y := xy[0], xy[1]
	switch {
	case x == '?' && y == '?':
		return "?"
	case x == 'A':
		return "A"
	case x == 'D' || y == 'D':
		return "D"
	case x == 'M' && y == ' ':
		return "A"
	case y == 'M':
		return "M"
	default:
		return "?"
	}
}

var forbiddenRe = regexp.MustCompile(`(?i)403|401|forbidden|unauthorized`)

// CanPush mirrors /can-push.
func (g *GHI) CanPush(ctx context.Context, serverUrl, repo, username, password string) (int, map[string]interface{}) {
	if serverUrl == "" || repo == "" {
		return 400, map[string]interface{}{"error": "Missing server_url or repo"}
	}
	if assertGitServerUrl(serverUrl) == "" {
		return 400, map[string]interface{}{"error": "server_url must be an http(s) git server URL"}
	}
	repoUrl := strings.TrimRight(serverUrl, "/") + "/" + repo + ".git"
	_, gerr := runGit(ctx, g.Cfg, []string{"ls-remote", "--exit-code", repoUrl}, gitOpts{user: username, pass: password})
	if gerr == nil {
		return 200, map[string]interface{}{"can_push": true}
	}
	msg := gerr.Error()
	code := 404
	if forbiddenRe.MatchString(msg) {
		code = 403
	}
	return code, map[string]interface{}{"can_push": false, "error": msg}
}

// BranchHead mirrors /branch-head.
func (g *GHI) BranchHead(ctx context.Context, serverUrl, repo, branch, username, password string) (int, map[string]interface{}) {
	if serverUrl == "" || repo == "" || branch == "" {
		return 400, map[string]interface{}{"error": "Missing server_url, repo or branch"}
	}
	if assertGitServerUrl(serverUrl) == "" {
		return 400, map[string]interface{}{"error": "server_url must be an http(s) git server URL"}
	}
	repoUrl := strings.TrimRight(serverUrl, "/") + "/" + repo + ".git"
	out, gerr := runGit(ctx, g.Cfg, []string{"ls-remote", repoUrl, "refs/heads/" + branch}, gitOpts{user: username, pass: password})
	if gerr != nil {
		return 500, map[string]interface{}{"error": gerr.Error()}
	}
	for _, raw := range strings.Split(out, "\n") {
		if line := strings.TrimSpace(raw); line != "" {
			return 200, map[string]interface{}{"sha": strings.Fields(line)[0]}
		}
	}
	return 404, map[string]interface{}{"error": "Branch " + branch + " not found"}
}

// Commits mirrors /commits (shallow/full clone + log + divergence). 1:1.
func (g *GHI) Commits(ctx context.Context, serverUrl, repo, branch, since string, limit int, username, password string) (int, map[string]interface{}) {
	if serverUrl == "" || repo == "" || branch == "" {
		return 400, map[string]interface{}{"error": "Missing server_url, repo or branch"}
	}
	if assertGitServerUrl(serverUrl) == "" {
		return 400, map[string]interface{}{"error": "server_url must be an http(s) git server URL"}
	}
	if limit <= 0 {
		limit = 50
	}
	repoUrl := strings.TrimRight(serverUrl, "/") + "/" + repo + ".git"
	shallow := since == ""
	dir := filepath.Join(g.Cfg.WorkRoot, "commits_"+strconv.FormatInt(time.Now().UnixNano()/1e6, 10)+"_"+ghiRandHex(8))
	defer os.RemoveAll(dir)

	cloneErr := (func() error {
		if shallow {
			args := []string{"clone", "--progress", "--depth", strconv.Itoa(limit + 1), "--single-branch", "--branch", branch, repoUrl, dir}
			if _, e := runGit(ctx, g.Cfg, args, gitOpts{user: username, pass: password}); e == nil {
				return nil
			}
			os.RemoveAll(dir)
		}
		_, e := runGit(ctx, g.Cfg, []string{"clone", "--progress", "--branch", branch, repoUrl, dir}, gitOpts{user: username, pass: password})
		return e
	})()
	if cloneErr != nil {
		return 500, map[string]interface{}{"error": cloneErr.Error()}
	}

	var log []logLine
	if since != "" {
		out, err := runGit(ctx, g.Cfg, []string{"log", ghiLogFormat, since + "..HEAD"}, gitOpts{cwd: dir})
		if err != nil {
			return 200, map[string]interface{}{"commits": []interface{}{}, "diverged": true}
		}
		log = parseGitLog6(out)
	} else {
		out, err := runGit(ctx, g.Cfg, []string{"log", ghiLogFormat}, gitOpts{cwd: dir})
		if err != nil {
			return 500, map[string]interface{}{"error": err.Error()}
		}
		log = parseGitLog6(out)
	}

	commits := make([]map[string]interface{}, 0, min(limit, len(log)))
	for i, c := range log {
		if i >= limit {
			break
		}
		commits = append(commits, map[string]interface{}{
			"sha":     c.SHA,
			"message": c.Subject,
			"author":  map[string]interface{}{"name": c.Name, "email": c.Email, "date": c.Date},
		})
	}
	return 200, map[string]interface{}{"commits": commits, "diverged": false}
}

// --- REST endpoints (1:1) ---------------------------------------------------

func orgLogin(o map[string]interface{}) string {
	if s, ok := o["login"].(string); ok {
		return s
	}
	if s, ok := o["username"].(string); ok {
		return s
	}
	return ""
}

// Orgs mirrors /orgs.
func (g *GHI) Orgs(ctx context.Context, serverUrl, username, token string) (int, map[string]interface{}) {
	if serverUrl == "" || token == "" {
		return 400, map[string]interface{}{"error": "Missing server_url and token"}
	}
	if assertGitServerUrl(serverUrl) == "" {
		return 400, map[string]interface{}{"error": "server_url must be an http(s) git server URL"}
	}
	base := detectApiBase(ctx, g.Cfg.HTTPClient, serverUrl)
	family := baseFamily(base)
	urlStr := base + "/user/orgs?limit=100"
	if family == "gitlab" {
		urlStr = base + "/groups?membership=true&per_page=100"
	}
	code, body, err := ghiHTTP(ctx, g.Cfg.HTTPClient, http.MethodGet, urlStr, apiHeaders(token, base), nil)
	if err != nil {
		return 500, map[string]interface{}{"error": "Failed to list orgs"}
	}
	if code < 200 || code >= 300 {
		msg := scrubDetail(string(body), token)
		if msg == "" {
			msg = "HTTP " + strconv.Itoa(code)
		}
		return code, map[string]interface{}{"error": msg}
	}
	var arr []map[string]interface{}
	_ = json.Unmarshal(body, &arr)
	orgs := []string{}
	for _, o := range arr {
		if s := orgLogin(o); s != "" {
			orgs = append(orgs, s)
		}
	}
	user := ""
	if ucode, ubody, uerr := ghiHTTP(ctx, g.Cfg.HTTPClient, http.MethodGet, base+"/user", apiHeaders(token, base), nil); uerr == nil && ucode == 200 {
		var uj map[string]interface{}
		_ = json.Unmarshal(ubody, &uj)
		if s, ok := uj["login"].(string); ok && s != "" {
			user = s
		} else if s, ok := uj["username"].(string); ok {
			user = s
		}
	}
	return 200, map[string]interface{}{"orgs": orgs, "user": user}
}

func (g *GHI) tryReuseExistingRepo(ctx context.Context, base, family, name, org, username, token string) (map[string]interface{}, bool) {
	owner := org
	if owner == "" {
		owner = username
	}
	var urlStr string
	if family == "gitlab" {
		urlStr = base + "/projects/" + url.QueryEscape(owner+":"+name)
	} else {
		urlStr = base + "/repos/" + url.QueryEscape(owner) + "/" + url.QueryEscape(name)
	}
	code, body, err := ghiHTTP(ctx, g.Cfg.HTTPClient, http.MethodGet, urlStr, apiHeaders(token, base), nil)
	if err != nil || code < 200 || code >= 300 {
		return nil, false
	}
	var j map[string]interface{}
	_ = json.Unmarshal(body, &j)
	fullName := ""
	if s, ok := j["path_with_namespace"].(string); ok {
		fullName = s
	}
	if fullName == "" {
		if s, ok := j["full_name"].(string); ok {
			fullName = s
		}
	}
	if fullName == "" {
		fullName = owner + "/" + name
	}
	db := ""
	if s, ok := j["default_branch"].(string); ok {
		db = s
	}
	if db == "" {
		db = "main"
	}
	return map[string]interface{}{"full_name": fullName, "default_branch": db, "reused": true}, true
}

// CreateRepo mirrors /create-repo (with repo-reuse fallback). 1:1.
func (g *GHI) CreateRepo(ctx context.Context, serverUrl, username, token, name, description string, isPublic bool, org string) (int, map[string]interface{}) {
	if serverUrl == "" || token == "" || name == "" {
		return 400, map[string]interface{}{"error": "Missing server_url, token or name"}
	}
	if assertGitServerUrl(serverUrl) == "" {
		return 400, map[string]interface{}{"error": "server_url must be an http(s) git server URL"}
	}
	base := detectApiBase(ctx, g.Cfg.HTTPClient, serverUrl)
	family := baseFamily(base)
	var urlStr string
	var body map[string]interface{}
	if family == "gitlab" {
		if org != "" {
			urlStr = base + "/groups/" + url.QueryEscape(org) + "/projects"
		} else {
			urlStr = base + "/projects"
		}
		vis := "private"
		if isPublic {
			vis = "public"
		}
		body = map[string]interface{}{"name": name, "description": description, "visibility": vis, "initialize_with_readme": true}
	} else {
		if org != "" {
			urlStr = base + "/orgs/" + url.QueryEscape(org) + "/repos"
		} else {
			urlStr = base + "/user/repos"
		}
		body = map[string]interface{}{"name": name, "description": description, "private": !isPublic, "auto_init": true}
	}
	b, _ := json.Marshal(body)
	code, rbody, err := ghiHTTP(ctx, g.Cfg.HTTPClient, http.MethodPost, urlStr, apiHeaders(token, base), b)
	if err != nil {
		return 500, map[string]interface{}{"error": "Failed to create repository"}
	}
	if code < 200 || code >= 300 {
		if code >= 400 && code < 500 {
			if reused, ok := g.tryReuseExistingRepo(ctx, base, family, name, org, username, token); ok {
				return 200, reused
			}
		}
		msg := scrubDetail(string(rbody), token)
		if msg == "" {
			msg = "HTTP " + strconv.Itoa(code)
		}
		return code, map[string]interface{}{"error": msg}
	}
	var j map[string]interface{}
	_ = json.Unmarshal(rbody, &j)
	db := "main"
	if s, ok := j["default_branch"].(string); ok && s != "" {
		db = s
	}
	if family == "gitlab" {
		fullName := ""
		if s, ok := j["path_with_namespace"].(string); ok && s != "" {
			fullName = s
		}
		if fullName == "" {
			if s, ok := j["name"].(string); ok {
				fullName = s
			}
		}
		return 200, map[string]interface{}{"full_name": fullName, "default_branch": db}
	}
	fullName := ""
	if s, ok := j["full_name"].(string); ok && s != "" {
		fullName = s
	}
	if fullName == "" {
		owner := org
		if owner == "" {
			owner = username
		}
		fullName = owner + "/" + name
	}
	return 200, map[string]interface{}{"full_name": fullName, "default_branch": db}
}

// ListRepos mirrors /list-repos.
func (g *GHI) ListRepos(ctx context.Context, serverUrl, username, token string) (int, map[string]interface{}) {
	if serverUrl == "" || token == "" {
		return 400, map[string]interface{}{"error": "Missing server_url or token"}
	}
	if assertGitServerUrl(serverUrl) == "" {
		return 400, map[string]interface{}{"error": "server_url must be an http(s) git server URL"}
	}
	base := detectApiBase(ctx, g.Cfg.HTTPClient, serverUrl)
	family := baseFamily(base)
	urlStr := base + "/user/repos?limit=100"
	if family == "gitlab" {
		urlStr = base + "/projects?membership=true&per_page=100"
	}
	code, body, err := ghiHTTP(ctx, g.Cfg.HTTPClient, http.MethodGet, urlStr, apiHeaders(token, base), nil)
	if err != nil {
		return 500, map[string]interface{}{"error": "Failed to list repositories"}
	}
	if code < 200 || code >= 300 {
		msg := string(body)
		if len(msg) > 500 {
			msg = msg[:500]
		}
		if msg == "" {
			msg = "HTTP " + strconv.Itoa(code)
		}
		return code, map[string]interface{}{"error": msg}
	}
	var raw []map[string]interface{}
	if jerr := json.Unmarshal(body, &raw); jerr != nil {
		return 502, map[string]interface{}{"error": "invalid list-repos payload from git server"}
	}
	repos := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		name, _ := item["name"].(string)
		db := ""
		if s, ok := item["default_branch"].(string); ok && s != "" {
			db = s
		}
		if db == "" {
			db = "main"
		}
		fullName := ""
		if family == "gitlab" {
			if s, ok := item["path_with_namespace"].(string); ok && s != "" {
				fullName = s
			}
		}
		if fullName == "" {
			if s, ok := item["full_name"].(string); ok && s != "" {
				fullName = s
			}
		}
		if fullName == "" {
			fullName = username + "/" + name
		}
		repos = append(repos, map[string]interface{}{"name": name, "fullName": fullName, "defaultBranchName": db})
	}
	return 200, map[string]interface{}{"repos": repos}
}
