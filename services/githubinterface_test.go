package services

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var hex40 = regexp.MustCompile(`^[0-9a-f]{40}$`)

// --- pure helper tests (1:1) ------------------------------------------------

func TestGHIShellQuote(t *testing.T) {
	if got := shellQuote("it's"); got != `'it'\''s'` {
		t.Fatalf("shellQuote = %q", got)
	}
}

func TestGHIScrubDetail(t *testing.T) {
	got := scrubDetail("tok=SECRET123 more", "SECRET123")
	if strings.Contains(got, "SECRET123") {
		t.Fatalf("secret leaked: %q", got)
	}
	long := strings.Repeat("a", 600)
	if len(scrubDetail(long, "")) != 500 {
		t.Fatalf("not truncated to 500")
	}
}

func TestGHIUrlGuards(t *testing.T) {
	if assertGitServerUrl("http://x.git") == "" || assertGitServerUrl("https://y/git") == "" {
		t.Fatal("valid http(s) should pass")
	}
	if assertGitServerUrl("file:///x") != "" || assertGitServerUrl("gopher://x") != "" {
		t.Fatal("non-http should be rejected")
	}
	if !isAllowedServerUrl("https://gh.com/a") {
		t.Fatal("allowed expected")
	}
	if isAllowedServerUrl("file:///a") {
		t.Fatal("file should be disallowed")
	}
}

func TestGHIWorkDirEscape(t *testing.T) {
	root := "/tmp/ghi-root"
	if _, err := resolveWorkDir(root, "relative/path"); err == nil {
		t.Fatal("relative should be rejected")
	}
	if _, err := resolveWorkDir(root, "/etc/passwd"); err == nil {
		t.Fatal("outside root should be rejected")
	}
	if got, err := resolveWorkDir(root, "/tmp/ghi-root/proj"); err != nil || got != filepath.Clean("/tmp/ghi-root/proj") {
		t.Fatalf("inside root should pass, got %q err %v", got, err)
	}
}

func TestGHIStructuralHelpers(t *testing.T) {
	if !validRef("") || !validRef("main") {
		t.Fatal("plain refs valid")
	}
	if validRef("-x") || validRef("a b") || validRef("a:b") || validRef("a+b") || validRef("x:y") {
		t.Fatal("refs with -:whitespace or + or : must be invalid")
	}
	if identityEmail("John Doe", "") != "JohnDoe@localhost" {
		t.Fatalf("identityEmail = %q", identityEmail("John Doe", ""))
	}
	if identityEmail("???", "") != "overleaf@localhost" {
		t.Fatalf("identityEmail fallback = %q", identityEmail("???", ""))
	}
	if identityEmail("x", "me@x.io") != "me@x.io" {
		t.Fatalf("explicit email should win")
	}
	if baseFamily("http://x/api/v4") != "gitlab" || baseFamily("http://x/api/v3") != "github" || baseFamily("http://x/api/v1") != "gitea" {
		t.Fatal("baseFamily")
	}
	if h := apiHeaders("tok", "x/api/v4"); h["Authorization"] != "Bearer tok" || h["PRIVATE-TOKEN"] != "tok" {
		t.Fatalf("gitlab headers = %v", h)
	}
	if h := apiHeaders("tok", "x/api/v1"); h["Authorization"] != "token tok" {
		t.Fatalf("gitea headers = %v", h)
	}
	if mapPorcelain("??") != "?" || mapPorcelain(" M") != "M" || mapPorcelain("D ") != "D" || mapPorcelain("A ") != "A" || mapPorcelain("M ") != "A" {
		t.Fatalf("mapPorcelain: M=%q D=%q A=%q", mapPorcelain(" M"), mapPorcelain("D "), mapPorcelain("A "))
	}
	if !ghiTimingEqual("abc", "abc") || ghiTimingEqual("abc", "abd") || ghiTimingEqual("abc", "ab") {
		t.Fatal("ghiTimingEqual")
	}
}

// --- dumb git-over-HTTP fake (backed by a real git repo) -------------------

func dumbGitServer(t *testing.T, repoName string, forceStatus int) (*httptest.Server, string) {
	t.Helper()
	root := t.TempDir()
	normal := t.TempDir()
	runN := func(args ...string) {
		if _, e := gitIn(t, normal, args...); e != nil {
			t.Fatalf("git %v: %v", args, e)
		}
	}
	runN("init", "-b", "main")
	runN("config", "user.email", "g@h.io")
	runN("config", "user.name", "Gserver")
	if err := os.WriteFile(filepath.Join(normal, "README"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runN("add", "README")
	runN("commit", "-m", "c1")
	runN("branch", "develop")
	gitdir := filepath.Join(root, repoName)
	if _, e := gitIn(t, normal, "clone", "--bare", normal, gitdir); e != nil {
		t.Fatalf("clone --bare: %v", e)
	}
	if _, e := gitIn(t, gitdir, "update-server-info"); e != nil {
		t.Fatalf("update-server-info: %v", e)
	}
	out, e := gitIn(t, normal, "rev-parse", "HEAD")
	if e != nil {
		t.Fatal(e)
	}
	sha := strings.TrimSpace(out)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if forceStatus != 0 {
			w.WriteHeader(forceStatus)
			return
		}
		http.FileServer(http.Dir(root)).ServeHTTP(w, r)
	})
	return httptest.NewServer(mux), sha
}

// --- local git repo helper --------------------------------------------------

func gitIn(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), err
	}
	return string(out), nil
}

func makeTestConfig(t *testing.T) GHIConfig {
	t.Helper()
	return GHIConfig{WorkRoot: t.TempDir(), GitExe: "git"}
}

func makeLocalRepo(t *testing.T, g *GHI, name string) string {
	t.Helper()
	dir := filepath.Join(g.Cfg.WorkRoot, name)
	if e := os.MkdirAll(dir, 0o755); e != nil {
		t.Fatal(e)
	}
	if _, e := gitIn(t, dir, "init", "-b", "main"); e != nil {
		t.Fatalf("git init: %v", e)
	}
	if _, e := gitIn(t, dir, "config", "user.email", "a@b.io"); e != nil {
		t.Fatal(e)
	}
	if _, e := gitIn(t, dir, "config", "user.name", "Testy"); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# hi\n"), 0o644); e != nil {
		t.Fatal(e)
	}
	if _, e := gitIn(t, dir, "add", "README.md"); e != nil {
		t.Fatalf("git add: %v", e)
	}
	if _, e := gitIn(t, dir, "commit", "-m", "initial"); e != nil {
		t.Fatalf("git commit: %v", e)
	}
	return dir
}

// --- client behavior tests --------------------------------------------------

func TestGHICommitLogStatus(t *testing.T) {
	g := NewGHI(makeTestConfig(t))
	ctx := context.Background()
	dir := makeLocalRepo(t, g, "proj")

	// commit a new file
	if e := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644); e != nil {
		t.Fatal(e)
	}
	code, res := g.Commit(ctx, dir, []string{"a.txt"}, "add a", "username", "", "")
	if code != 200 {
		t.Fatalf("commit code=%d res=%v", code, res)
	}
	sha, _ := res["commit_sha"].(string)
	if !hex40.MatchString(sha) {
		t.Fatalf("commit_sha not hex40: %q", sha)
	}
	if res["success"] != true {
		t.Fatalf("success = %v", res["success"])
	}

	// log
	lcode, lres := g.Log(ctx, dir, "HEAD", 10, 1)
	if lcode != 200 {
		t.Fatalf("log code=%d res=%v", lcode, lres)
	}
	commits, _ := lres["commits"].([]map[string]interface{})
	if len(commits) < 2 {
		t.Fatalf("expected >=2 commits, got %d", len(commits))
	}
	if commits[0]["message"] != "add a" {
		t.Fatalf("latest message = %v", commits[0]["message"])
	}
	auth, _ := commits[0]["author"].(map[string]interface{})
	if auth["name"] != "username" || auth["email"] != "username@localhost" {
		t.Fatalf("latest author = %v", auth)
	}
	if p0, ok := commits[0]["parents"].([]string); !ok || len(p0) != 1 {
		t.Fatalf("latest commit should have 1 parent, got %v", commits[0]["parents"])
	}
	if p1, ok := commits[1]["parents"].([]string); !ok || len(p1) != 0 {
		t.Fatalf("root commit should have 0 parents, got %v", commits[1]["parents"])
	}

	// status: modify + untracked
	if e := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("new\n"), 0o644); e != nil {
		t.Fatal(e)
	}
	scode, sres := g.Status(ctx, dir)
	if scode != 200 {
		t.Fatalf("status code=%d res=%v", scode, sres)
	}
	if sres["branch"] != "main" {
		t.Fatalf("branch = %v", sres["branch"])
	}
	ufs, _ := sres["uncommitted_files"].([]map[string]interface{})
	foundB := false
	for _, u := range ufs {
		if u["path"] == "b.txt" && u["status"] == "?" {
			foundB = true
		}
	}
	if !foundB {
		t.Fatalf("untracked b.txt not listed: %v", ufs)
	}
}

func TestGHICanPushOK(t *testing.T) {
	g := NewGHI(makeTestConfig(t))
	srv, _ := dumbGitServer(t, "repo.git", 0)
	defer srv.Close()
	code, res := g.CanPush(context.Background(), srv.URL, "repo", "user", "pass")
	if code != 200 || res["can_push"] != true {
		t.Fatalf("can-push = %d %v", code, res)
	}
}

func TestGHICanPushForbidden(t *testing.T) {
	g := NewGHI(makeTestConfig(t))
	srv, _ := dumbGitServer(t, "repo.git", 403)
	defer srv.Close()
	code, res := g.CanPush(context.Background(), srv.URL, "repo", "user", "pass")
	if code != 403 || res["can_push"] != false {
		t.Fatalf("expected 403 reject, got %d %v", code, res)
	}
}

func TestGHICanPushNotFound(t *testing.T) {
	g := NewGHI(makeTestConfig(t))
	srv, _ := dumbGitServer(t, "exists.git", 0)
	defer srv.Close()
	// a repo that does not exist -> 404
	code, _ := g.CanPush(context.Background(), srv.URL, "missing", "user", "pass")
	if code != 404 {
		t.Fatalf("expected 404, got %d", code)
	}
}

func TestGHIBranchHead(t *testing.T) {
	g := NewGHI(makeTestConfig(t))
	srv, sha := dumbGitServer(t, "repo.git", 0)
	defer srv.Close()
	code, res := g.BranchHead(context.Background(), srv.URL, "repo", "main", "user", "pass")
	if code != 200 || res["sha"] != sha {
		t.Fatalf("branch-head = %d %v (want sha %s)", code, res, sha)
	}
	if c2, _ := g.BranchHead(context.Background(), srv.URL, "repo", "nope", "user", "pass"); c2 != 404 {
		t.Fatalf("missing branch expected 404, got %d", c2)
	}
}

// --- validation tests (no network) ------------------------------------------

func TestGHIValidation(t *testing.T) {
	g := NewGHI(makeTestConfig(t))
	ctx := context.Background()
	root := g.Cfg.WorkRoot
	in := filepath.Join(root, "x")

	if c, _ := g.Clone(ctx, "http://a/x", "HEAD", in, "", "", ""); c != 400 {
		t.Fatalf("clone without server_url + http should be 400, got %d", c)
	}
	if c, _ := g.Clone(ctx, "http://a/x", "HEAD", in, "https://b.org", "", ""); c != 400 {
		t.Fatalf("clone host mismatch should be 400, got %d", c)
	}
	if c, _ := g.Clone(ctx, "file:///x", "HEAD", in, "", "", ""); c != 400 {
		t.Fatalf("clone file:// should be 400, got %d", c)
	}
	if c, _ := g.Push(ctx, in, "upstream", "HEAD", "", ""); c != 400 {
		t.Fatalf("push non-origin should be 400, got %d", c)
	}
	if c, _ := g.Pull(ctx, in, "origin", "-force", "", ""); c != 400 {
		t.Fatalf("pull invalid ref should be 400, got %d", c)
	}
	if c, _ := g.Commit(ctx, in, []string{}, "m", "", "", ""); c != 400 {
		t.Fatalf("commit no files should be 400, got %d", c)
	}
	if c, _ := g.Commit(ctx, "/outside/root", []string{"a"}, "m", "", "", ""); c != 400 {
		t.Fatalf("commit outside workdir should be 400, got %d", c)
	}
	if c, _ := g.Log(ctx, "", "HEAD", 5, 1); c != 400 {
		t.Fatalf("log no dir should be 400, got %d", c)
	}
	if c, _ := g.Status(ctx, ""); c != 400 {
		t.Fatalf("status no dir should be 400, got %d", c)
	}
	if c, _ := g.CanPush(ctx, "", "repo", "", ""); c != 400 {
		t.Fatalf("can-push missing should be 400, got %d", c)
	}
	if c, _ := g.BranchHead(ctx, "", "repo", "b", "", ""); c != 400 {
		t.Fatalf("branch-head missing should be 400, got %d", c)
	}
	if c, _ := g.Commits(ctx, "", "repo", "b", "", 5, "", ""); c != 400 {
		t.Fatalf("commits missing should be 400, got %d", c)
	}
}

// --- REST family tests (fake provider) --------------------------------------

type fakeProviderRoute struct {
	status int
	body   string
}

func fakeProvider(routes map[string]fakeProviderRoute) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		if rr, ok := routes[key]; ok {
			if rr.status > 0 {
				w.WriteHeader(rr.status)
			} else {
				w.WriteHeader(200)
			}
			io.WriteString(w, rr.body)
			return
		}
		w.WriteHeader(404)
	})
	return httptest.NewServer(mux)
}

func TestGHIProviderCheckAndOrgsGitHub(t *testing.T) {
	g := NewGHI(makeTestConfig(t))
	ctx := context.Background()
	routes := map[string]fakeProviderRoute{
		"GET /api/v4/user":      {status: 404},
		"GET /api/v1/user":      {status: 404},
		"GET /api/v3/user":      {status: 200, body: `{"login":"me"}`},
		"GET /api/v3/user/orgs": {status: 200, body: `[{"login":"orgA"},{"login":""},{"login":"orgB"}]`},
	}
	srv := fakeProvider(routes)
	defer srv.Close()

	// detectApiBase should pick /api/v3 (github family)
	if base := detectApiBase(ctx, g.Cfg.HTTPClient, srv.URL); !strings.HasSuffix(base, "/api/v3") {
		t.Fatalf("detectApiBase = %q", base)
	}

	code, res := g.Check(ctx, srv.URL, "user", "tok")
	if code != 200 || res["login"] != "me" {
		t.Fatalf("check = %d %v", code, res)
	}

	oc, ores := g.Orgs(ctx, srv.URL, "user", "tok")
	if oc != 200 {
		t.Fatalf("orgs = %d %v", oc, ores)
	}
	orgs, _ := ores["orgs"].([]string)
	if len(orgs) != 2 || orgs[0] != "orgA" || orgs[1] != "orgB" {
		t.Fatalf("orgs = %v", orgs)
	}
	if ores["user"] != "me" {
		t.Fatalf("user = %v", ores["user"])
	}
}

func TestGHIProviderCheckRejected(t *testing.T) {
	g := NewGHI(makeTestConfig(t))
	ctx := context.Background()
	routes := map[string]fakeProviderRoute{
		"GET /api/v4/user": {status: 401, body: `{"message":"bad token"}`},
	}
	srv := fakeProvider(routes)
	defer srv.Close()
	code, res := g.Check(ctx, srv.URL, "user", "tok")
	if code != 401 {
		t.Fatalf("expected 401, got %d (%v)", code, res)
	}
	if res["error"] != "Token rejected by server" {
		t.Fatalf("error = %v", res["error"])
	}
}

func TestGHIProviderCreateRepoAndReuse(t *testing.T) {
	g := NewGHI(makeTestConfig(t))
	ctx := context.Background()
	// create-repo success (github family, no org)
	routes := map[string]fakeProviderRoute{
		"GET /api/v4/user":        {status: 404},
		"GET /api/v1/user":        {status: 404},
		"GET /api/v3/user":        {status: 200, body: `{"login":"me"}`},
		"POST /api/v3/user/repos": {status: 201, body: `{"full_name":"me/newrepo","default_branch":"main"}`},
	}
	srv := fakeProvider(routes)
	code, res := g.CreateRepo(ctx, srv.URL, "me", "tok", "newrepo", "desc", true, "")
	if code != 200 || res["full_name"] != "me/newrepo" || res["default_branch"] != "main" {
		t.Fatalf("create-repo = %d %v", code, res)
	}
	srv.Close()

	// reuse on 422
	routes2 := map[string]fakeProviderRoute{
		"GET /api/v4/user":             {status: 404},
		"GET /api/v1/user":             {status: 404},
		"GET /api/v3/user":             {status: 200, body: `{"login":"me"}`},
		"POST /api/v3/user/repos":      {status: 422, body: `{"message":"name exists"}`},
		"GET /api/v3/repos/me/newrepo": {status: 200, body: `{"full_name":"me/newrepo","default_branch":"master"}`},
	}
	srv2 := fakeProvider(routes2)
	defer srv2.Close()
	code2, res2 := g.CreateRepo(ctx, srv2.URL, "me", "tok", "newrepo", "desc", true, "")
	if code2 != 200 || res2["reused"] != true || res2["default_branch"] != "master" {
		t.Fatalf("create-repo reuse = %d %v", code2, res2)
	}
}

func TestGHIProviderListReposGitLab(t *testing.T) {
	g := NewGHI(makeTestConfig(t))
	ctx := context.Background()
	routes := map[string]fakeProviderRoute{
		"GET /api/v4/user":     {status: 200, body: `{"login":"gl"}`},
		"GET /api/v4/projects": {status: 200, body: `[{"name":"p1","path_with_namespace":"ns/p1","default_branch":"main"}]`},
	}
	srv := fakeProvider(routes)
	defer srv.Close()
	code, res := g.ListRepos(ctx, srv.URL, "gl", "tok")
	if code != 200 {
		t.Fatalf("list-repos = %d %v", code, res)
	}
	repos, _ := res["repos"].([]map[string]interface{})
	if len(repos) != 1 || repos[0]["fullName"] != "ns/p1" || repos[0]["name"] != "p1" {
		t.Fatalf("repos = %v", repos)
	}
}

// --- server-level: /health + service token ---------------------------------

func TestGHIHealthEndpoint(t *testing.T) {
	mux := NewGHIHandlerMux(GHIConfig{WorkRoot: t.TempDir()})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("health = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "githubinterface") {
		t.Fatalf("body = %s", body)
	}
}

func ghipost(t *testing.T, url string, body string, header map[string]string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestGHIAuthEnforced(t *testing.T) {
	mux := NewGHIHandlerMux(GHIConfig{WorkRoot: t.TempDir(), ServiceToken: "sekret"})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	code, body := ghipost(t, srv.URL+"/check", `{"server_url":"https://gh.com","token":"t"}`, nil)
	if code != 401 || !strings.Contains(body, "service token") {
		t.Fatalf("missing token expected auth 401, got %d (%s)", code, body)
	}
	code, body = ghipost(t, srv.URL+"/check", `{"server_url":"https://gh.com","token":"t"}`, map[string]string{"X-Service-Token": "wrong"})
	if code != 401 || !strings.Contains(body, "service token") {
		t.Fatalf("wrong token expected auth 401, got %d (%s)", code, body)
	}
	// correct service token must pass AUTH (the /check itself may still 401 because the
	// upstream server is unreachable, but it must not be an auth rejection).
	code, body = ghipost(t, srv.URL+"/check", `{"server_url":"https://gh.com","token":"t"}`, map[string]string{"X-Service-Token": "sekret"})
	if strings.Contains(body, "service token") {
		t.Fatalf("correct service token should pass auth, got auth rejection (%d %s)", code, body)
	}
}

func TestGHIValidationViaHTTP(t *testing.T) {
	mux := NewGHIHandlerMux(GHIConfig{WorkRoot: t.TempDir()})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	code, _ := ghipost(t, srv.URL+"/commit", `{"dir":"x","message":"m"}`, nil)
	if code != 400 {
		t.Fatalf("commit via http expected 400, got %d", code)
	}
}
