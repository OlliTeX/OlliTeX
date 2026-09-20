// Receive-pack loopback (Option 3a, HANDOFF §14/§15/§16): a REAL git 2.53
// client pushes over smart-HTTP against the shelled
// `git receive-pack --stateless-rpc` handler + RunProcReceive hook path,
// byte-comparing client stderr to the gate3a-verified Java push blocks.
//
// HOOK SEAM. git execs .git/hooks/proc-receive in a SEPARATE process, so the
// hook cannot call in-process state. The installed shim execs this test
// binary itself:
//
//	#!/bin/sh
//	exec <testbinary> -hookproctest
//
// TestMain intercepts that flag before m.Run(); hookTestMode builds a
// HookEvaluator with a stub Pusher from env and drives one RunProcReceive
// session:
//
//	PROJECT (handler contract, set by the shelled handler)  — required
//	HOST    (handler contract)                              — informational
//	HROOT   (test seam: FSGitRepoStore root)                — required
//	HMODE   (test seam) ok | ood | invalidfiles | ...        — stub error
//
// SCENARIOS (wire shapes live-verified against gate3a.py; assertions mirror
// the Java IT normalization — skip 2 stderr lines, drop blank):
//
//	A OK          HMODE=ok, FF push → ok pkt, hook update-ref moves the ref;
//	              a second clone (via upload-pack) proves the ref moved.
//	B OODate      HMODE=ood (stub OutOfDateException = Java postback-409
//	              parity) → NO band-2, ng "non-fast forward"; the client
//	              renders its own gate-B-byte-exact block:
//	                error: failed to push some refs to '<url>'
//	                hint: Updates were rejected because the tip of your
//	                      current branch is behind
//	                hint: its remote counterpart. If you want to integrate
//	                      the remote changes,
//	                hint: use 'git pull' before pushing again.
//	                hint: See the 'Note about fast-forwards' in 'git push
//	                      --help' for details.
//	B' ForcedPush (in-process evaluator; wire shape proven by B) → band-2
//	              error + blank + 3 hints, ng "forced push prohibited".
//	C InvalidFiles HMODE=invalidfiles → band-2 error + blank + 5 hint lines,
//	              ng "invalid files" (gate C byte-exact order).
//	D Delete      ref deletion → InternalErrorException band-2 + ng
//	              "internal error" (Java NPE parity on deletes).
//	E WrongBranch push to refs/heads/dev → WrongBranchException band-2 +
//	              ng "wrong branch".
//	Skips when the git binary is unavailable.

package gitproto

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ollitex/go/services/gitbridge/data"
	"ollitex/go/services/gitbridge/filestore"
	"ollitex/go/services/gitbridge/giterrors"
	"ollitex/go/services/gitbridge/repo"
)

// TestMain intercepts the hook-shim flag BEFORE the test framework parses
// flags (go test testmain only flag.Parse-s during m.Run, which is too
// late for the shim path): .git/hooks/proc-receive execs
// `<testbinary> -hookproctest`, which must not run the whole suite.
func TestMain(m *testing.M) {
	if hookModeArg() {
		os.Exit(hookTestMode())
	}
	os.Exit(m.Run())
}

func hookModeArg() bool {
	for _, a := range os.Args {
		if a == "-hookproctest" || a == "--hookproctest" {
			return true
		}
	}
	return false
}

// invalidFilesBody mirrors gate3a.py scenario C's postback body: 4 errors
// exercising all three errorEntry.describe() branches.
const invalidFilesBody = `{"errors":[
 {"file":"file1.invalid","state":"unexpected"},
 {"file":"file2.exe","state":"disallowed"},
 {"file":"hello world.png","cleanFile":"hello_world.png","state":"unexpected"},
 {"file":"an image.jpg","cleanFile":"an_image.jpg","state":"unexpected"}
]}`

// stubPusherHook simulates bridge.Push for the hook subprocess (the real
// postback round-trip is not reachable in loopback). Production binds
// *bridge.Bridge (cmd/git_bridge main.go); bridge_test.go fakes the
// postback server itself.
type stubPusherHook struct{ err error }

func (s *stubPusherHook) Push(_ *data.Oauth2, _ string,
	_, _ *filestore.RawDirectory, _ string) error {
	return s.err
}

func hookTestMode() int {
	proj, root := os.Getenv("PROJECT"), os.Getenv("HROOT")
	if proj == "" || root == "" {
		fmt.Fprintln(os.Stderr, "hook mode: PROJECT and HROOT env required")
		return 2
	}
	var stubErr error
	switch os.Getenv("HMODE") {
	case "", "ok":
		stubErr = nil
	case "ood":
		stubErr = &giterrors.OutOfDateException{}
	case "invalidfiles":
		stubErr = giterrors.NewInvalidFilesException([]byte(invalidFilesBody))
	case "invalidproject":
		stubErr = &giterrors.InvalidProjectException{Errors: []string{"This project is disabled."}}
	case "unexpected":
		stubErr = &giterrors.UnexpectedErrorException{}
	default:
		fmt.Fprintf(os.Stderr, "hook mode: unknown HMODE %q\n", os.Getenv("HMODE"))
		return 2
	}
	store := repo.NewFSGitRepoStore(root, nil)
	eval := &HookEvaluator{
		Project:    proj,
		ProjectDir: filepath.Join(root, proj),
		Oauth2:     nil,
		Pusher:     &stubPusherHook{err: stubErr},
		Hostname:   os.Getenv("HOST"),
		Store:      store,
	}
	if err := RunProcReceive(os.Stdin, os.Stdout, os.Stderr, eval); err != nil {
		fmt.Fprintf(os.Stderr, "hook mode: proc-receive: %v\n", err)
		return 2
	}
	return 0
}

// ---------------------------------------------------------------------------
// loopback plumbing (real git client; shelled handlers on httptest)
// ---------------------------------------------------------------------------

func gitAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
}

func identityEnv() map[string]string {
	return map[string]string{
		"GIT_AUTHOR_NAME": "W", "GIT_AUTHOR_EMAIL": "w@w",
		"GIT_COMMITTER_NAME": "W", "GIT_COMMITTER_EMAIL": "w@w",
	}
}

// newLoopback inits the project store with a seed commit, installs the
// proc-receive hook shim (execs the test binary in hook mode), and serves
// both upload-pack and receive-pack smart-HTTP endpoints through the
// shelled handlers. Returns the project (for ref inspection) and the
// remote URL.
func newLoopback(t *testing.T, proj string) (p *repo.Project, remote string) {
	t.Helper()
	gitAvailable(t)

	root := t.TempDir() + "/gitbridge"
	store := repo.NewFSGitRepoStore(root, nil)
	p, err := store.InitRepo("testproj")
	if err != nil {
		t.Fatalf("InitRepo: %v", err)
	}

	// seed commit (identity env; the client pushes with its own identity)
	if out, err := p.GitRaw(identityEnv(), "commit", "--allow-empty", "--quiet", "-m", "seed"); err != nil {
		t.Fatalf("seed commit: %v %s", err, out)
	}
	if seed, _ := p.GitRaw(nil, "rev-parse", "HEAD"); strings.TrimSpace(seed) == "" {
		t.Fatalf("rev-parse seed: %v", err)
	}

	// hook shim: execs this test binary in hook mode; seams carry through
	// test process -> shelled handler (os.Environ passthrough) -> git -> hook.
	exeb, err := os.Executable()
	if err != nil {
		t.Fatalf("test executable: %v", err)
	}
	shim := "#!/bin/sh\nexec \"" + exeb + "\" -hookproctest\n"
	if err := p.InstallProcReceiveHook(shim); err != nil {
		t.Fatalf("InstallProcReceiveHook: %v", err)
	}
	os.Setenv("HROOT", root) // FSGitRepoStore root the hook store reads from
	t.Cleanup(func() { os.Unsetenv("HROOT"); os.Unsetenv("HMODE") })

	up := NewUploadPackHandler("")
	rp := NewReceivePackHandler("", "testproj", "", "")
	p_ := p.ProjectDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// the service name is in the ?service= query param, so match the
		// full URL (gate3a matches self.path incl. query).
		target := r.URL.String()
		hasUpload := strings.Contains(target, "git-upload-pack")
		hasReceive := strings.Contains(target, "git-receive-pack")
		switch {
		case r.Method == http.MethodGet && hasUpload:
			// advertisement (GET) requires the -advertisement Content-Type,
			// not raw bytes, so git accepts it.
			w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
			_ = up.ServeAdvertise(p_, r.Header.Get("Git-Protocol"), w)
		case r.Method == http.MethodPost && hasUpload:
			w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
			_ = up.Handle(p_, r.Header.Get("Git-Protocol"), r.Body, w)
		case r.Method == http.MethodGet && hasReceive:
			w.Header().Set("Content-Type", "application/x-git-receive-pack-advertisement")
			_ = rp.ServeAdvertise(p_, w)
		case r.Method == http.MethodPost && hasReceive:
			w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
			_ = rp.Handle(p_, r.Header.Get("Git-Protocol"), r.Body, w)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return p, srv.URL + "/" + proj
}

// clientEnv builds an isolated client git env (no config leak; the extra
// carries the identity + test seams).
func clientEnv(t *testing.T, extra map[string]string) []string {
	t.Helper()
	home := t.TempDir()
	env := []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"HOME=" + home,
	}
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

func runCmd(t *testing.T, dir string, env []string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = env
	var outb, errb strings.Builder
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return outb.String() + errb.String(), err
	}
	return outb.String() + errb.String(), nil
}

func cloneClient(t *testing.T, env []string, remote, dir string) {
	t.Helper()
	if out, err := runCmd(t, ".", env, "clone", "--quiet", remote, dir); err != nil {
		t.Fatalf("git clone: %v %s", err, out)
	}
}

func commitClient(t *testing.T, dir string, env []string, file, content, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", file, err)
	}
	if out, err := runCmd(t, dir, env, "add", "-A"); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	if out, err := runCmd(t, dir, env, "commit", "--quiet", "-m", message); err != nil {
		t.Fatalf("git commit: %v %s", err, out)
	}
}

// pushClient returns (combined stderr, error) of `git push [args...]`.
func pushClient(t *testing.T, dir string, env []string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"push"}, args...)...)
	cmd.Dir = dir
	cmd.Env = env
	var outb, errb strings.Builder
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return errb.String(), err
	}
	return errb.String(), nil
}

// norm mirrors the Java IT normalization (Util.linesFromStream skip=2):
// drop the first two stderr lines, then per line: trim (Java line.trim(),
// which strips git's band-2 right-padding of `remote:` lines), drop the
// \p{C} control chars, and cut before a [K (OSC) escape.
func norm(stderr string) []string {
	lines := strings.Split(strings.ReplaceAll(stderr, "\r", "\n"), "\n")
	if len(lines) >= 2 {
		lines = lines[2:]
	} else {
		lines = nil
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		t := l
		if i := strings.Index(t, "[K"); i >= 0 {
			t = t[:i]
		}
		t = stripCtrl(t)
		t = strings.TrimSpace(t)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func stripCtrl(s string) string {
	r := []rune(s)
	for i, c := range r {
		if c >= 0x7f || (c < 0x20) {
			r[i] = 0
		}
	}
	return string(r)
}

func refValue(t *testing.T, p *repo.Project, ref string) string {
	t.Helper()
	out, err := p.GitRaw(nil, "rev-parse", ref)
	if err != nil {
		t.Fatalf("rev-parse %s: %v", ref, err)
	}
	return strings.TrimSpace(out)
}

// assertClientBlock asserts each want line appears in the client stderr, in
// order (ordered containment; wrap-tolerant on the "see git push --help"
// continuation).
func assertClientBlock(t *testing.T, name, url, stderr string, want []string) {
	t.Helper()
	got := strings.ReplaceAll(stderr, "\r", "\n")
	pos := 0
	for i, w := range want {
		rest := got
		if pos < len(got) {
			rest = got[pos:]
		}
		j := strings.Index(rest, w)
		if j < 0 {
			t.Fatalf("%s: missing %q (%d/%d) in client stderr:\n%s", name, w, i, len(want), got)
		}
		pos = pos + j + len(w)
	}
}

// ---------------------------------------------------------------------------
// A: OK push
// ---------------------------------------------------------------------------

func TestReceiveOKPush(t *testing.T) {
	p, remote := newLoopback(t, "testproj")
	seed := refValue(t, p, "refs/heads/main")

	env := clientEnv(t, identityEnv())
	clone := t.TempDir()
	cloneClient(t, env, remote, clone+"/cl")

	os.Setenv("HMODE", "ok")
	commitClient(t, clone+"/cl", env, "push.tex", "v1", "push1")
	stderr, err := pushClient(t, clone+"/cl", env)
	if err != nil {
		t.Fatalf("OK push failed: %v (stderr: %s)", err, stderr)
	}
	for _, marker := range []string{"error:", "rejected", "non-fast"} {
		if strings.Contains(stderr, marker) {
			t.Fatalf("unexpected client stderr marker %q: %s", marker, stderr)
		}
	}
	after := refValue(t, p, "refs/heads/main")
	if after == seed {
		t.Fatal("ref not moved by OK push")
	}
	// A2 (gate A): the second clone proves upload-pack still works after
	// the procreceiverefs-managed hook ref update.
	cloneClient(t, env, remote, clone+"/cl2")
	content, err := os.ReadFile(filepath.Join(clone, "cl2", "push.tex"))
	if err != nil || string(content) != "v1" {
		t.Fatalf("pushed content missing after OK push: %q %v", content, err)
	}
}

// ---------------------------------------------------------------------------
// B: OODate (stub postback-409 parity) — gate B byte-exact
// ---------------------------------------------------------------------------

func TestReceiveOutOfDate(t *testing.T) {
	p, remote := newLoopback(t, "testproj")
	seed := refValue(t, p, "refs/heads/main")

	env := clientEnv(t, identityEnv())
	clone := t.TempDir()
	cloneClient(t, env, remote, clone+"/cl")

	os.Setenv("HMODE", "ood")
	commitClient(t, clone+"/cl", env, "push.tex", "d1", "pushB")
	stderr, err := pushClient(t, clone+"/cl", env)
	if err == nil {
		t.Fatal("expected rejected push (ng 'non-fast forward')")
	}
	want := []string{
		"error: failed to push some refs to '" + remote + "'",
		"hint: Updates were rejected because the tip of your current branch is behind",
		"hint: its remote counterpart. If you want to integrate the remote changes,",
		"hint: use 'git pull' before pushing again.",
		"hint: See the 'Note about fast-forwards' in 'git push --help' for details.",
	}
	assertClientBlock(t, "ood", remote, stderr, want)
	if got := refValue(t, p, "refs/heads/main"); got != seed {
		t.Fatalf("ref moved on rejected push: %s", got)
	}
}

// forcedPushWire asserts the forced-push band-2 shape (in-process evaluator
// drive — the wire path is the same classify() + RunProcReceive stream; the
// OOD scenario B proves the shelled exec path end-to-end).
func forcedPushShape(t *testing.T, root, proj string) {
	t.Helper()
	store := repo.NewFSGitRepoStore(root, nil)
	p, err := store.GetExistingRepo(proj)
	if err != nil {
		t.Fatalf("GetExistingRepo: %v", err)
	}
	// old = server tip (HEAD), new = client rewrites to HEAD~1 (an OLDER
	// commit): old is NOT an ancestor of new → forced push (Java JGit
	// isAncestor parity via `git merge-base --is-ancestor`).
	out, err := p.GitRaw(nil, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	old := strings.TrimSpace(out)
	out, err = p.GitRaw(nil, "rev-parse", "HEAD~1")
	if err != nil {
		t.Fatalf("rev-parse HEAD~1: %v", err)
	}
	new := strings.TrimSpace(out)

	eval := &HookEvaluator{
		Project: proj, ProjectDir: p.ProjectDir(),
		Oauth2: nil, Pusher: &stubPusherHook{}, Hostname: "127.0.0.1", Store: store,
	}
	stdin := procPktStr("version=1") + procFlushStr() +
		procPktStr(old+" "+new+" refs/heads/main") + procFlushStr()
	var stdout, stderr strings.Builder
	if err := RunProcReceive(strings.NewReader(stdin), &stdout, &stderr, eval); err != nil {
		t.Fatalf("RunProcReceive: %v", err)
	}
	want := strings.Join([]string{
		"error: forced push prohibited",
		"",
		"hint: You can't git push --force to a Overleaf project.",
		"hint: Try to put your changes on top of the current head.",
		"hint: If everything else fails, delete and reclone your repository, make your changes, then push again.",
	}, "\n")
	if got := strings.TrimSpace(stderr.String()); got != want {
		t.Fatalf("forced push band-2:\nwant:\n%s\ngot:\n%s", want, got)
	}
	if out := stdout.String(); !strings.Contains(out, "ng refs/heads/main forced push prohibited") {
		t.Fatalf("missing ng reason: %q", out)
	}
}

// ---------------------------------------------------------------------------
// C: InvalidFiles — gate C byte-exact
// ---------------------------------------------------------------------------

func TestReceiveInvalidFiles(t *testing.T) {
	p, remote := newLoopback(t, "testproj")
	seed := refValue(t, p, "refs/heads/main")

	env := clientEnv(t, identityEnv())
	clone := t.TempDir()
	cloneClient(t, env, remote, clone+"/cl")

	os.Setenv("HMODE", "invalidfiles")
	commitClient(t, clone+"/cl", env, "push.tex", "p2", "pushC")
	stderr, err := pushClient(t, clone+"/cl", env)
	if err == nil {
		t.Fatal("expected rejected push (ng 'invalid files')")
	}
	got := norm(stderr)
	// Gate C shape (skip 2, drop blank): band-2 lines first (the error+
	// blank pair consumes raw lines 0-1), then the To/!/error block.
	want := []string{
		"remote: hint: You have 4 invalid files in your Overleaf project:",
		"remote: hint: file1.invalid (error)",
		"remote: hint: file2.exe (invalid file extension)",
		"remote: hint: hello world.png (rename to: hello_world.png)",
		"remote: hint: an image.jpg (rename to: an_image.jpg)",
		"To " + remote,
		"! [remote rejected] main -> main (invalid files)",
		"error: failed to push some refs to '" + remote + "'",
	}
	for i, w := range want {
		if i >= len(got) {
			t.Fatalf("client stderr short: got %v, want %d more", got, len(want)-i)
		}
		if got[i] != w {
			t.Fatalf("line %d: want %q got %q (full: %v)", i, w, got[i], got)
		}
	}
	if got := refValue(t, p, "refs/heads/main"); got != seed {
		t.Fatalf("ref moved on rejected push: %s", got)
	}
}

// ---------------------------------------------------------------------------
// D: delete an existing ref → InternalError (Java NPE parity)
// ---------------------------------------------------------------------------

func TestReceiveDelete(t *testing.T) {
	p, remote := newLoopback(t, "testproj")
	seed := refValue(t, p, "refs/heads/main")

	env := clientEnv(t, identityEnv())
	clone := t.TempDir()
	cloneClient(t, env, remote, clone+"/cl")

	os.Setenv("HMODE", "ok")
	stderr, err := pushClient(t, clone+"/cl", env, "origin", ":main")
	if err == nil {
		t.Fatal("expected rejected delete (ng 'internal error')")
	}
	for _, w := range []string{
		"remote: error: internal error",
		"remote: hint: There was an internal error with the Git server.",
		"remote: hint: Please contact Overleaf.",
		"! [remote rejected] main (internal error)",
		"error: failed to push some refs to '" + remote + "'",
	} {
		assertClientBlock(t, "delete", remote, stderr, []string{w})
	}
	if got := refValue(t, p, "refs/heads/main"); got != seed {
		t.Fatalf("ref moved on rejected delete: %s", got)
	}
}

// ---------------------------------------------------------------------------
// E: push a non-default ref → WrongBranch
// ---------------------------------------------------------------------------

func TestReceiveWrongBranch(t *testing.T) {
	p, remote := newLoopback(t, "testproj")
	seed := refValue(t, p, "refs/heads/main")

	env := clientEnv(t, identityEnv())
	clone := t.TempDir()
	cloneClient(t, env, remote, clone+"/cl")

	os.Setenv("HMODE", "ok")
	stderr, err := pushClient(t, clone+"/cl", env, "origin", "HEAD:dev")
	if err == nil {
		t.Fatal("expected rejected push (ng 'wrong branch')")
	}
	for _, w := range []string{
		"remote: error: wrong branch",
		"remote: hint: You can't push any new branches.",
		"remote: hint: Please use the main branch.",
		"! [remote rejected]",
		"(wrong branch)",
	} {
		assertClientBlock(t, "wrongbranch", remote, stderr, []string{w})
	}
	if got := refValue(t, p, "refs/heads/main"); got != seed {
		t.Fatalf("main ref moved on rejected push: %s", got)
	}
	// refs/heads/dev (the rejected one) must NOT exist.
	if out, _ := p.GitRaw(nil, "rev-parse", "--verify", "refs/heads/dev"); strings.TrimSpace(out) != "" {
		t.Fatalf("refs/heads/dev exists despite rejection: %q", out)
	}
}

// in-process evaluator drives (no HTTP client — classify + stream proofs)
// ---------------------------------------------------------------------------

// TestForcedPushShape: B' (Java catch-ladder forced push) band-2 byte shape.
func TestForcedPushShape(t *testing.T) {
	gitAvailable(t)
	root := t.TempDir()
	store := repo.NewFSGitRepoStore(root, nil)
	p, err := store.InitRepo("testproj")
	if err != nil {
		t.Fatalf("InitRepo: %v", err)
	}
	ie := identityEnv()
	for _, m := range []string{"one", "two"} {
		if _, err := p.GitRaw(ie, "commit", "--allow-empty", "--quiet", "-m", m); err != nil {
			t.Fatalf("commit %s: %v", m, err)
		}
	}
	forcedPushShape(t, root, "testproj")
}

// TestEvalDelete: the DELETE command's (Java NPE parity) InternalError
// result — in-process.
func TestEvalDelete(t *testing.T) {
	gitAvailable(t)
	root := t.TempDir()
	store := repo.NewFSGitRepoStore(root, nil)
	p, err := store.InitRepo("testproj")
	if err != nil {
		t.Fatalf("InitRepo: %v", err)
	}
	if _, err := p.GitRaw(identityEnv(), "commit", "--allow-empty", "--quiet", "-m", "one"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	sha, _ := p.GitRaw(nil, "rev-parse", "HEAD")
	sha = strings.TrimSpace(sha)

	eval := &HookEvaluator{
		Project: "testproj", ProjectDir: p.ProjectDir(),
		Oauth2: nil, Pusher: &stubPusherHook{}, Hostname: "127.0.0.1", Store: store,
	}
	stdin := procPktStr("version=1") + procFlushStr() +
		procPktStr(sha+" "+zeroRef+" refs/heads/main") + procFlushStr()
	var stdout, stderr strings.Builder
	if err := RunProcReceive(strings.NewReader(stdin), &stdout, &stderr, eval); err != nil {
		t.Fatalf("RunProcReceive: %v", err)
	}
	if got := stderr.String(); !strings.Contains(got, "error: internal error") {
		t.Fatalf("delete band-2: %q", got)
	}
	if got := stdout.String(); !strings.Contains(got, "ng refs/heads/main internal error") {
		t.Fatalf("delete ng: %q", got)
	}
}
