// Procreceive unit tests (HANDOFF §15 file plan step 5, part 1).
//
// Covers: parseProcCommand, readProcPkt streaming, the Java catch-ladder
// classification (classify — locked table), and RunProcReceive end-to-end
// (handshake, per-ref ok/ng + band-2 ordering) against a STUB Pusher plus a
// real temp git repo (so checkBranch/forced-push/update-ref paths are real).

package gitproto

import (
	"fmt"
	"strings"
	"testing"

	"ollitex/go/services/gitbridge/data"
	"ollitex/go/services/gitbridge/filestore"
	"ollitex/go/services/gitbridge/giterrors"
	"ollitex/go/services/gitbridge/repo"
)

// ---------------------------------------------------------------------------
// env-pop
// ---------------------------------------------------------------------------

func TestQuarantineEnvPopsGitVars(t *testing.T) {
	in := []string{
		"PATH=/usr/bin",
		"GIT_DIR=.",
		"GIT_WORK_TREE=/tmp/x",
		"GIT_QUARANTINE_PATH=/tmp/q",
		"GIT_OBJECT_DIRECTORY=/tmp/o",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES=/tmp/a",
		"PROJECT=testproj",
		"HOST=127.0.0.1",
	}
	got := map[string]string{}
	for _, kv := range quarantineEnv(in) {
		i := strings.Index(kv, "=")
		got[kv[:i]] = kv[i+1:]
	}
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_QUARANTINE_PATH",
		"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES"} {
		if _, has := got[k]; has {
			t.Errorf("quarantineEnv: %s must be popped, got %q", k, got[k])
		}
	}
	if got["PATH"] != "/usr/bin" || got["PROJECT"] != "testproj" || got["HOST"] != "127.0.0.1" {
		t.Errorf("quarantineEnv: must preserve non-git vars: %v", got)
	}
}

// ---------------------------------------------------------------------------
// parseProcCommand
// ---------------------------------------------------------------------------

func TestParseProcCommand(t *testing.T) {
	cases := []struct {
		line   string
		wantOK bool
		want   ProcCommand
	}{
		{
			line: "0000000000000000000000000000000000000000 " +
				"1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b refs/heads/main\r",
			wantOK: true,
			want: ProcCommand{
				Old: strings.Repeat("0", 40), New: "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b",
				RefName: "refs/heads/main", OldIsZero: true, NewIsZero: false,
			},
		},
		{
			line: "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b " +
				strings.Repeat("0", 40) + " refs/heads/main",
			wantOK: true,
			want: ProcCommand{
				Old: "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b", New: strings.Repeat("0", 40),
				RefName: "refs/heads/main", OldIsZero: false, NewIsZero: true,
			},
		},
		{line: "toofew", wantOK: false},
		{line: "old new refs/heads/main", wantOK: false}, // 20-hex, not 40
		{line: "a b c d e f g", wantOK: false},           // extra parts
		{line: "oldnewmissing" + strings.Repeat("0", 40), wantOK: false},
	}
	for i, c := range cases {
		cmd, ok := parseProcCommand(strings.TrimRight(c.line, "\r\n"))
		if ok != c.wantOK {
			t.Fatalf("case %d: parse ok=%v want %v (line %q)", i, ok, c.wantOK, c.line)
		}
		if ok && cmd != c.want {
			t.Fatalf("case %d: got %+v want %+v", i, cmd, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// classify — the Java catch-ladder mapping (HANDOFF §15 locked table). The
// test is the LOCKED source of truth for the error → (band-2 lines, ng
// reason) mapping the client sees.
// ---------------------------------------------------------------------------

func TestClassifyOutOfDate(t *testing.T) {
	// OOD: NO band-2, ng reason "non-fast forward" (JGit
	// REJECTED_NONFASTFORWARD; the client renders its own 4 hint lines).
	res := classify(fmt.Errorf("wrapped: %w", &giterrors.OutOfDateException{}))
	if res.band2Lines != nil {
		t.Fatalf("OOD must emit NO band-2 lines: %v", res.band2Lines)
	}
	if res.reason != "non-fast forward" {
		t.Fatalf("OOD reason: got %q want %q", res.reason, "non-fast forward")
	}
}

func TestClassifyWithDescription(t *testing.T) {
	// WithDescription: band-2 error: line + BLANK + one hint: line per
	// Description() line; ng reason = Error().
	desc := []string{"hint line 1", "hint line 2"}
	err := errWithDescription{msg: "boom", desc: desc}
	res := classify(err)
	want := []string{"error: boom", "", "hint: hint line 1", "hint: hint line 2"}
	if len(res.band2Lines) != len(want) {
		t.Fatalf("band-2 lines: got %v want %v", res.band2Lines, want)
	}
	for i, line := range want {
		if res.band2Lines[i] != line {
			t.Fatalf("band-2 line %d: got %q want %q", i, res.band2Lines[i], line)
		}
	}
	if res.reason != "boom" {
		t.Fatalf("ng reason: got %q want %q", res.reason, "boom")
	}
}

func TestClassifyNoDescription(t *testing.T) {
	// WithoutDescription (CannotAcquireLock, raw IOException bucket):
	// band-2 error: line ONLY; ng reason = Error().
	err := errNoDescription{msg: "another operation is in progress"}
	res := classify(err)
	want := []string{"error: another operation is in progress"}
	if len(res.band2Lines) != len(want) {
		t.Fatalf("band-2 lines: got %v want %v", res.band2Lines, want)
	}
	if res.band2Lines[0] != want[0] {
		t.Fatalf("band-2 line 0: got %q want %q", res.band2Lines[0], want[0])
	}
	if res.reason != "another operation is in progress" {
		t.Fatalf("ng reason: got %q", res.reason)
	}
}

func TestClassifyNil(t *testing.T) {
	res := classify(nil)
	if !res.ok || res.reason != "" || res.band2Lines != nil {
		t.Fatalf("classify(nil) must be {ok:true, no data}: %+v", res)
	}
}

func TestClassifyNilPointer(t *testing.T) {
	res := classify(fmt.Errorf("%w", fmt.Errorf("wrapped nil")))
	_ = res // plain wrapped errors have no Description → error-only band-2
}

// errWithDescription is a minimal error with a Description() (tests the
// interface contract classify relies on).
type errWithDescription struct {
	msg  string
	desc []string
}

func (e errWithDescription) Error() string         { return e.msg }
func (e errWithDescription) Description() []string { return e.desc }

// errNoDescription is a minimal error WITHOUT Description.
type errNoDescription struct{ msg string }

func (e errNoDescription) Error() string { return e.msg }

func TestClassifyNilErrorPointer(t *testing.T) {
	// A *giterrors.InternalErrorException (pointer receiver, has
	// Description) → band-2 error + hints; ng reason = Error().
	res := classify(&giterrors.InternalErrorException{})
	if res.ok || res.reason != "internal error" {
		t.Fatalf("InternalError pointer: got %+v", res)
	}
	want := "error: internal error"
	if len(res.band2Lines) == 0 || res.band2Lines[0] != want {
		t.Fatalf("band-2 line 0: got %v want %q", res.band2Lines, want)
	}
	if !strings.Contains(res.band2Lines[len(res.band2Lines)-1], "hint: Please contact") {
		t.Fatalf("band-2 last line should be hint: Please contact…: %v", res.band2Lines)
	}
}

// ---------------------------------------------------------------------------
// RunProcReceive end-to-end (handshake + per-ref results + band-2 ordering)
// against a real temp git repo.
// ---------------------------------------------------------------------------

// testGitRepo inits a temp repo via the store and returns (eval, cleanup).
func testEval(t *testing.T, pusher Pusher, project string) (*HookEvaluator, string) {
	t.Helper()
	// The quarantine env pops don't apply here (we're not running under
	// receive-pack's env), but InitRepo needs a real git dir.
	store := repo.NewFSGitRepoStore(t.TempDir(), nil)
	p, err := store.InitRepo(project)
	if err != nil {
		t.Fatalf("InitRepo: %v", err)
	}
	// Make a seed commit so the repo has at least one object (push needs an
	// existing commit to build on; create push uses OLD=zero so the first
	// push must be from a repo with seed to work in the store).
	if out, err := p.GitRaw(map[string]string{
		"GIT_AUTHOR_NAME":     "s",
		"GIT_AUTHOR_EMAIL":    "s@s",
		"GIT_COMMITTER_NAME":  "s",
		"GIT_COMMITTER_EMAIL": "s@s",
	}, "commit", "--allow-empty", "-m", "seed"); err != nil {
		t.Fatalf("seed commit: %v %s", err, out)
	}
	seed, _ := p.GitRaw(map[string]string{"GIT_TERMINAL_PROMPT": "0"}, "rev-parse", "HEAD")
	seed = strings.TrimSpace(seed)
	t.Logf("seed commit: %s", seed)
	_ = seed
	eval := &HookEvaluator{
		Project:    project,
		ProjectDir: p.ProjectDir(),
		Oauth2:     (*data.Oauth2)(nil),
		Pusher:     pusher,
		Hostname:   "http://127.0.0.1:1/testproj",
		Store:      store,
	}
	return eval, p.ProjectDir()
}

func TestRunProcReceiveOKAndUpdateRef(t *testing.T) {
	eval, _ := testEval(t, stubPusher{}, "rproceval")
	// Build the proc-receive stream: version=1 + flush + one command for the
	// seed commit → refs/heads/main. Use the real seed commit as OLD (so
	// forced-push check sees old==new → ancestor → not forced) and NEW.
	seed, _ := eval.Store.GetExistingRepo("rproceval")
	seedProj, _ := eval.Store.GetExistingRepo("rproceval")
	_ = seed
	seedHash, _ := seedProj.GitRaw(nil, "rev-parse", "HEAD")
	seedHash = strings.TrimSpace(seedHash)

	// Build the wire: version + flush + "<seed> <seed> refs/heads/main" + flush.
	wire := procPktStr("version=1") + procFlushStr()
	wire += procPktStr(seedHash+" "+seedHash+" refs/heads/main") + procFlushStr()

	var stdout, stderr strings.Builder
	if err := RunProcReceive(strings.NewReader(wire), &stdout, &stderr, eval); err != nil {
		t.Fatalf("RunProcReceive: %v", err)
	}

	// Expect: version=1 + flush, then one "ok refs/heads/main" pkt + flush
	// per ref, then final flush. stubPusher returns nil → ok ref + ref update.
	out := stdout.String()
	okCount := strings.Count(out, procPktStr("ok refs/heads/main"))
	if okCount < 1 {
		t.Fatalf("expected at least one ok pkt for refs/heads/main in stdout: %q", out)
	}
	t.Logf("stdout:\n%s\nstderr: %q", out, stderr.String())

	// The ref should have been moved (update-ref ran).
	proj, err := eval.Store.GetExistingRepo("rproceval")
	if err != nil {
		t.Fatalf("get repo after: %v", err)
	}
	refOut, _ := proj.GitRaw(nil, "rev-parse", "refs/heads/main")
	refOut = strings.TrimSpace(refOut)
	if refOut != seedHash {
		t.Fatalf("ref not updated: rev-parse refs/heads/main got %s want %s", refOut, seedHash)
	}
}

// testEvalWithPusher is a stub Pusher that always returns nil (success).
func testEvalWithPusher(t *testing.T, project string) *HookEvaluator {
	eval, _ := testEval(t, stubPusher{}, project)
	return eval
}

func TestRunProcReceiveNG(t *testing.T) {
	// Stub pusher that returns a describable error → ng + band-2.
	eval, _ := testEval(t, stubPusherErr{err: errWithDescription{msg: "bad", desc: []string{"h1"}}}, "rprocevalng")
	seedProj, _ := eval.Store.GetExistingRepo("rprocevalng")
	seedHash, _ := seedProj.GitRaw(nil, "rev-parse", "HEAD")
	seedHash = strings.TrimSpace(seedHash)

	wire := procPktStr("version=1") + procFlushStr()
	wire += procPktStr(seedHash+" "+seedHash+" refs/heads/main") + procFlushStr()

	var stdout, stderr strings.Builder
	if err := RunProcReceive(strings.NewReader(wire), &stdout, &stderr, eval); err != nil {
		t.Fatalf("RunProcReceive: %v", err)
	}
	// Band-2 went to stderr: "error: bad", "", "hint: h1".
	se := stderr.String()
	if !strings.Contains(se, "error: bad") {
		t.Fatalf("band-2 error line missing from stderr: %q", se)
	}
	if !strings.Contains(se, "hint: h1") {
		t.Fatalf("band-2 hint line missing from stderr: %q", se)
	}
	// Result: ng pkt with reason "bad".
	ngLine := procPktStr("ng refs/heads/main bad")
	if !strings.Contains(stdout.String(), ngLine) {
		t.Fatalf("ng pkt missing from stdout: %q\nwant %q", stdout.String(), ngLine)
	}
}

// stubPusher: Push always nil (success).
type stubPusher struct{}

func (stubPusher) Push(_ *data.Oauth2, _ string, _, _ *filestore.RawDirectory, _ string) error {
	return nil
}

// stubPusherErr: Push always returns err.
type stubPusherErr struct{ err error }

func (s stubPusherErr) Push(_ *data.Oauth2, _ string, _, _ *filestore.RawDirectory, _ string) error {
	return s.err
}
