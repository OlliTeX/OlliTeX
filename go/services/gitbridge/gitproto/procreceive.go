// Package gitproto — Option 3a proc-receive hook (HANDOFF §14.3 / §15).
//
// The Java write path is: WLReceivePackFactory (smart-HTTP) → JGit
// receive-pack → WriteLatexPutHook (PreReceiveHook) → Bridge.push → ok/ng per
// ref + band-2 side-band lines (gate3a-verified byte-exact against a real git
// 2.53 client).
//
// Option 3a port: the wire plumbing is a SHIELLED `git receive-pack
// --stateless-rpc` (see receivepack.go); this file ports WriteLatexPutHook —
// the per-reference evaluator and the Java catch ladder's client-visible
// output (band-2 lines + ng reasons) — and runs it as the proc-receive hook.
//
// PROTOCOL (live-probed git 2.53.0, /tmp/gate3a.py + live qe5 probe):
//
//	hook stdin  (git → hook) : "version=1" pkt, FLUSH — receive-pack reads
//	                           BOTH pkts before streaming commands — then one
//	                           "<old> <new> <refname>" pkt per reference,
//	                           FLUSH.
//	hook stdout (hook → git) : "version=1" pkt, FLUSH (REQUIRED — receive-
//	                           pack blocks until it sees it), then one
//	                           "ok <refname>" / "ng <refname> <reason>" pkt
//	                           per reference, each followed by a FLUSH, and a
//	                           final FLUSH.
//	hook stderr              : band 2 — git forwards it to the client as
//	                           `remote: <line>` (sideband ch2). Per ref the
//	                           band-2 lines are emitted BEFORE that ref's
//	                           ok/ng pkt (Java ordering, gate C byte-exact).
//
// ENV (handler contract, HANDOFF §14.3): the shelled receive-pack handler
// exports PROJECT/HOST/OAUTH2_TOKEN (if set)/GIT_PROTOCOL (if set). git adds
// ONLY GIT_DIR='.' to the hook environment; the hook's cwd is the project's
// .git dir (live-probed). All child git commands pop the quarantine vars and
// run with cwd = the project work dir (git discovers <workDir>/.git). The
// pushed objects are ALREADY migrated into <workDir>/.git/objects when the
// hook runs (no quarantine env vars exist for proc-receive).
//
// REF UPDATES: procreceiverefs-managed refs are NOT moved by git itself (the
// refs the hook manages must be updated by the hook process):
// `git -C <workDir> update-ref <ref> <new>` after the reference is ok.

package gitproto

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"ollitex/go/services/gitbridge/data"
	"ollitex/go/services/gitbridge/filestore"
	"ollitex/go/services/gitbridge/giterrors"
	"ollitex/go/services/gitbridge/repo"
)

// Pusher is the seam over bridge.Bridge.Push (HANDOFF §15) so gitproto
// evaluates pushes without an Overleaf postback round-trip. Production binds
// the *bridge.Bridge built in the hook process (cmd/git_bridge); tests bind
// an env-driven stub.
type Pusher interface {
	Push(oauth2 *data.Oauth2, projectName string, newDir, oldDir *filestore.RawDirectory, hostname string) error
}

// ProcCommand is one "<old> <new> <refname>" command (Java ReceiveCommand);
// IsZero maps the Java Type.ADD (old is zero) / Type.DELETE (new is zero).
type ProcCommand struct {
	Old, New, RefName string

	OldIsZero, NewIsZero bool
}

// HookEvaluator evaluates proc-receive commands for one project, mirroring
// Java WriteLatexPutHook (repo store + Pusher + hostname + oauth2).
type HookEvaluator struct {
	Project    string
	ProjectDir string
	Oauth2     *data.Oauth2
	Pusher     Pusher
	Hostname   string
	Store      *repo.FSGitRepoStore
}

// evalResult is one reference's outcome: ok/ng + the band-2 lines the Java
// catch ladder emits (see classify).
type evalResult struct {
	ok         bool
	reason     string
	band2Lines []string
}

const zeroRef = "0000000000000000000000000000000000000000"

// ---------------------------------------------------------------------------
// pkt readers (proc-receive streaming over os.Stdin) and writers. pktline.go
// decodes a byte slice; the reader below parses ONE pkg per read via
// io.ReadFull on a bufio.Reader. The write side is one pkg + a raw "0000".
// ---------------------------------------------------------------------------

type procPkt struct {
	flush     bool
	terminate bool
	data      []byte
}

func readProcPkt(r *bufio.Reader) (*procPkt, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		if err == io.EOF {
			return &procPkt{flush: true}, nil
		}
		return nil, fmt.Errorf("gitproto: proc-receive: short pkt header: %w", err)
	}
	n := 0
	for _, c := range hdr[:4] {
		d, okd := hexValue(c)
		if !okd {
			return nil, fmt.Errorf("gitproto: proc-receive: invalid length byte %c", c)
		}
		n = n*16 + d
	}
	switch n {
	case 0:
		return &procPkt{flush: true}, nil
	case 0xff:
		return &procPkt{terminate: true}, nil
	}
	payload := make([]byte, n-4)
	if _, err := io.ReadFull(r, payload); err != nil {
		if err == io.EOF {
			return &procPkt{flush: true}, nil
		}
		return nil, fmt.Errorf("gitproto: proc-receive: short pkt payload: %w", err)
	}
	return &procPkt{data: payload}, nil
}

func procPktStr(s string) string { return fmt.Sprintf("%04x%s", len(s)+4, s) }

func procFlushStr() string { return "0000" }

// ---------------------------------------------------------------------------
// quarantine env — the hook runs with GIT_DIR='.' and cwd=<project>/.git;
// pushed objects are already migrated into <workDir>/.git/objects. Child git
// commands pop those vars and run from the project work dir.
// ---------------------------------------------------------------------------

// popQuarantineEnv removes the receive-pack quarantine env vars from this
// process's environment so child `git` commands resolve the project repo by
// path. (git receive-pack sets these for the quarantine object dir; the
// proc-receive hook must not inherit them — gate3a-verified stand-in pops
// the same vars.)
func popQuarantineEnv() {
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_QUARANTINE_PATH",
		"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES"} {
		os.Unsetenv(k)
	}
}

func quarantineEnv(environ []string) []string {
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		key := kv[:strings.Index(kv, "=")]
		switch key {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_QUARANTINE_PATH",
			"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES":
			continue
		default:
			out = append(out, kv)
		}
	}
	return out
}

func runGitInProjectDir(projectDir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = projectDir
	cmd.Env = quarantineEnv(os.Environ())
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("git %s: %v (stderr: %s)",
			strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// ---------------------------------------------------------------------------
// The runner (production hook entry; cmd/git_bridge binds os.Stdin/Stdout/
// Stderr to it).
// ---------------------------------------------------------------------------

// RunProcReceive drives one proc-receive session: handshake, per-reference
// evaluation, ref updates, result pkgs. Band-2 lines go to stderrWriter,
// result pkgs to resultWriter.
func RunProcReceive(stdin io.Reader, stdout, stderr io.Writer, eval *HookEvaluator) error {
	// quarantine env — receive-pack execs the proc-receive hook with
	// GIT_DIR='.' (+ GIT_WORK_TREE etc., build-dependent). Those leak into
	// every os.Environ()-sourced child `git` and break repo resolution;
	// pop them up front (gate3a shim does the same). quarantineEnv() exists
	// for per-command use; os.Unsetenv covers this process's env for good.
	popQuarantineEnv()

	r := bufio.NewReaderSize(stdin, 4096)

	// Handshake (gate3a-verified): git sends version pkt + flush (its own
	// receive-pack blocks until it READS the hook's reply), then streams
	// commands.
	first, err := readProcPkt(r)
	if err != nil {
		return fmt.Errorf("gitproto: proc-receive handshake: %w", err)
	}
	if !first.flush {
		if strings.Trim(string(first.data), "\r\n") != "version=1" {
			return fmt.Errorf("gitproto: proc-receive: unexpected first pkg %q", first.data)
		}
		fl, err := readProcPkt(r)
		if err != nil {
			return fmt.Errorf("gitproto: proc-receive handshake flush: %w", err)
		}
		if !fl.flush && !fl.terminate {
			return fmt.Errorf("gitproto: proc-receive: expected flush after handshake, got pkg %q", fl.data)
		}
	}
	fmt.Fprintf(stdout, "%s%s", procPktStr("version=1"), procFlushStr())

	// Commands: one "<old> <new> <ref>" pkg per reference, terminated by a
	// flush/EOF (the proc-receive stream does not use 00ff).
	commands := make([]ProcCommand, 0, 4)
	for {
		pkt, err := readProcPkt(r)
		if err != nil {
			return fmt.Errorf("gitproto: proc-receive command: %w", err)
		}
		if pkt.flush || pkt.terminate {
			break
		}
		cmd, ok := parseProcCommand(strings.Trim(string(pkt.data), "\r\n"))
		if !ok {
			// Cannot happen from a well-formed receive-pack (it validates
			// upstream). Fail closed: abort the session.
			return fmt.Errorf("gitproto: proc-receive: malformed command pkg %q", pkt.data)
		}
		commands = append(commands, cmd)
	}

	for _, cmd := range commands {
		res := eval.evaluate(cmd)
		for _, line := range res.band2Lines {
			fmt.Fprintln(stderr, line)
		}
		if res.ok && !cmd.NewIsZero {
			// procreceiverefs: git does NOT move the ref; the hook does.
			// (Old value intentionally omitted — update-ref re-checks FF.)
			if _, err := runGitInProjectDir(eval.ProjectDir, "update-ref", cmd.RefName, cmd.New); err != nil {
				fmt.Fprintf(stderr, "warn: proc-receive ref update failed: %v\n", err)
			}
		} else if res.ok {
			// Delete is rejected upstream (NewIsZero → InternalError), so ok
			// refs always have a new OID.
		}
		if res.ok {
			fmt.Fprintf(stdout, "%s", procPktStr("ok "+cmd.RefName))
		} else {
			fmt.Fprintf(stdout, "%s", procPktStr("ng "+cmd.RefName+" "+res.reason))
		}
		fmt.Fprint(stdout, procFlushStr())
	}
	fmt.Fprint(stdout, procFlushStr())
	return nil
}

func parseProcCommand(line string) (ProcCommand, bool) {
	parts := strings.Split(line, " ")
	if len(parts) != 3 {
		return ProcCommand{}, false
	}
	oid, newID, ref := parts[0], parts[1], parts[2]
	if len(oid) != 40 || len(newID) != 40 {
		return ProcCommand{}, false
	}
	return ProcCommand{
		Old: oid, New: newID, RefName: ref,
		OldIsZero: oid == zeroRef,
		NewIsZero: newID == zeroRef,
	}, true
}

// ---------------------------------------------------------------------------
// evaluate runs one reference's Java flow (WriteLatexPutHook
// handleReceiveCommand + checkForcedPush) and classifies the outcome per the
// catch ladder (see classify):
func (h *HookEvaluator) evaluate(cmd ProcCommand) evalResult {
	// (1) checkBranch — Java checkBranch(receiveChannel, command): the
	// ref must be the repo's full branch. Java: WrongBranchException.
	p, err := h.Store.GetExistingRepo(h.Project)
	if err != nil {
		// Java: IOException bucket (raw sendError).
		return classify(err)
	}
	branch, err := p.GetFullBranch()
	if err != nil {
		// Java: IllegalStateException (git user exception? no) → Throwable →
		// InternalErrorException.
		return classify(&giterrors.InternalErrorException{})
	}
	if cmd.RefName != branch {
		return classify(giterrors.NewWrongBranchException(branch))
	}

	// (2) delete guard — Java `useJGitRepo(repository, null)` on a DELETE
	// command NPEs (HANDOFF §15 locked parity) → InternalErrorException.
	if cmd.NewIsZero {
		return classify(&giterrors.InternalErrorException{})
	}

	// (3) forced push — old != zero and NOT old-ancestor-of-new (gate B
	// live-verified byte-exact client output).
	if !cmd.OldIsZero && !isAncestorOf(h.ProjectDir, cmd.Old, cmd.New) {
		return classify(&giterrors.ForcedPushException{})
	}

	// (4) directory contents (Java getPushed/getOld DirectoryContents):
	// newDir at the pushed commit, oldDir at HEAD.
	newDir, err := h.directoryAt(cmd.New)
	if err != nil {
		return classify(err)
	}
	oldDir, err := h.directoryAtHead()
	if err != nil {
		return classify(err)
	}

	// (5) bridge.push (Java bridge.push(oauth2, project, newDir, oldDir,
	// hostname)) — the error value decides classification.
	if err := h.Pusher.Push(h.Oauth2, h.Project, newDir, oldDir, h.Hostname); err != nil {
		return classify(err)
	}
	return evalResult{ok: true}
}

func (h *HookEvaluator) directoryAt(commit string) (*filestore.RawDirectory, error) {
	p, err := h.Store.UseJGitRepoAt(h.Project, commit)
	if err != nil {
		return nil, err
	}
	dir, err := p.GetDirectory()
	if err != nil {
		return nil, err
	}
	return filestore.NewRawDirectory(dir), nil
}

func (h *HookEvaluator) directoryAtHead() (*filestore.RawDirectory, error) {
	p, err := h.Store.GetExistingRepo(h.Project)
	if err != nil {
		return nil, err
	}
	dir, err := p.GetDirectory()
	if err != nil {
		return nil, err
	}
	return filestore.NewRawDirectory(dir), nil
}

// isAncestorOf is `git merge-base --is-ancestor old new` (JGit
// repository.isAncestor(old, new) parity). rc=1: not ancestor (forced).
// rc>1 (git error): treats NOT-ancestor — conservative; any follow-up
// update-ref would still fail FF and the push is rejected.
func isAncestorOf(projectDir, old, new string) bool {
	if old == new {
		return true
	}
	if _, err := runGitInProjectDir(projectDir, "merge-base", "--is-ancestor", old, new); err != nil {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// classify — the Java catch ladder's client-visible mapping (HANDOFF §15
// locked table, gate B/C live-verified byte-exact):
//
//	*OutOfDateException     → NO band-2; ng "non-fast forward" (JGit
//	                          REJECTED_NONFASTFORWARD; the client prints its
//	                          own 4 hint lines — gate B byte-exact).
//	GitUserException family (error carries a Description()) →
//	                          band-2 "error: <msg>" + blank line + one
//	                          "hint: <line>" per Description() line; ng <msg>.
//	CannotAcquireLockException (NO Description) →
//	                          band-2 "error: <msg>" ONLY; ng <msg>.
//	plain Go error (the IOException / raw Throwable bucket) →
//	                          band-2 "error: <msg>" ONLY; ng <msg>.
func classify(err error) evalResult {
	type describable interface{ Description() []string }

	if err == nil {
		return evalResult{ok: true}
	}
	if isOutOfDate(err) {
		return evalResult{reason: "non-fast forward"}
	}
	if ge, ok := err.(describable); ok {
		band2 := []string{"error: " + err.Error(), ""}
		for _, l := range ge.Description() {
			band2 = append(band2, "hint: "+l)
		}
		return evalResult{reason: err.Error(), band2Lines: band2}
	}
	return evalResult{reason: err.Error(), band2Lines: []string{"error: " + err.Error()}}
}

func isOutOfDate(err error) bool {
	// matches `instanceof OutOfDateException` across the error chain (Java
	// catch ladder); the bridge returns the pointer raw (bridge.go:444), so
	// errors.As unwraps one level and catches raw and wrapped forms.
	var ood *giterrors.OutOfDateException
	return errors.As(err, &ood)
}
