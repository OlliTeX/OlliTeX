// Package gitproto — Option 3a shelled git receive-pack (HANDOFF §14.3).
//
// The Java write path is SHIELLED here (the receive-pack twin of
// uploadpack.go, mirrored from it): the smart-HTTP wire is handled by
// `git receive-pack --stateless-rpc <workDir>` (git 2.53). The handler
// forwards the handler-contract env (PROJECT / HOST / OAUTH2_TOKEN /
// GIT_PROTOCOL — HANDOFF §14.3) and, on the result stream, streams git's
// stdout verbatim (result pkts + band-2 that git itself repackages from the
// proc-receive hook's stderr into side-band ch2).
//
// Hook: .git/hooks/proc-receive (a Go binary — cmd/git_bridge — running
// HookEvaluator per reference and doing the `update-ref` for
// procreceiverefs-managed refs; git does NOT move them itself).
package gitproto

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

// ReceivePackHandler handles one PUT (smart receive-pack) against a
// project. Project/Hostname/OauthToken are the handler-contract env
// (§14.3); git itself adds GIT_DIR='.' (+ no quarantine vars on 2.53) to
// the proc-receive hook environment.
type ReceivePackHandler struct {
	gitBinary  string
	Project    string
	Hostname   string
	OauthToken string
}

// NewReceivePackHandler returns a handler execing gitBinary (default "git"
// when empty, mirroring NewUploadPackHandler).
func NewReceivePackHandler(gitBinary, project, hostname, oauthToken string) *ReceivePackHandler {
	if gitBinary == "" {
		gitBinary = "git"
	}
	return &ReceivePackHandler{
		gitBinary:  gitBinary,
		Project:    project,
		Hostname:   hostname,
		OauthToken: oauthToken,
	}
}

// ServeAdvertise streams the full receive-pack advertisement: the
// `# service=git-receive-pack` capability pkt + FLUSH, then git's raw
// stateless-rpc ref advertisement verbatim. (git does NOT emit a service
// banner in stateless-rpc mode; the Java http-backend factory prepends it —
// this handler plays that role.) rc=1 ("no refs to advertise") is a
// legitimate advertise (empty ref list).
func (h *ReceivePackHandler) ServeAdvertise(workDir string, response io.Writer) error {
	cmd := exec.Command(
		h.gitBinary, "receive-pack", "--stateless-rpc", "--advertise-refs", workDir,
	)
	cmd.Env = h.env("")
	out, err := cmd.Output()
	if err != nil {
		// rc=1: no refs — still serve (empty advertise stream). rc>=2: 5xx.
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			response.Write(encodePkt([]byte("# service=git-receive-pack\n")))
			response.Write(EncodeFlush())
			_, _ = response.Write(out)
			return nil
		}
		return fmt.Errorf("gitproto: receive-pack advertise: %w", err)
	}
	response.Write(encodePkt([]byte("# service=git-receive-pack\n")))
	response.Write(EncodeFlush())
	_, _ = response.Write(out)
	return nil
}

// Handle processes a push (POST receive-pack): request body → git stdin,
// git stdout (result stream: ok/ng pkts + band-2 side-band) → response.
// receive-pack exits 0 (all ok), 1 (some refs rejected), 2 (fatal; the
// result stream still carries the ng reasons) — all three are served to the
// client as HTTP 200 (the client reads pkt results, Java does the same).
// Only an exec-setup failure (git binary missing) is a real 5xx.
func (h *ReceivePackHandler) Handle(workDir, gitProtocol string, requestBody io.Reader, response io.Writer) error {
	cmd := exec.Command(h.gitBinary, "receive-pack", "--stateless-rpc", workDir)
	cmd.Env = h.env(gitProtocol)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("gitproto: receive-pack: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("gitproto: receive-pack: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("gitproto: failed to start receive-pack: %w", err)
	}

	// Drain child stdout → response until EOF, in parallel with pumping
	// request body → child stdin (the stateless-rpc push is a single
	// body, but the copy ordering matters for backpressure as above).
	done := make(chan struct{})
	go func() {
		defer stdin.Close()
		_, _ = io.Copy(stdin, requestBody)
	}()
	go func() {
		defer close(done)
		_, _ = io.Copy(response, stdout)
	}()

	_ = cmd.Wait() // exit 0/1/2 are all servable; stderr dropped (transport
	<-done
	return nil
}

// env — os.Environ passthrough (PATH etc.) + the handler-contract env
// (§14.3). GIT_PROTOCOL forwarded only when present.
func (h *ReceivePackHandler) env(gitProtocol string) []string {
	env := make([]string, 0, len(os.Environ())+4)
	for _, kv := range os.Environ() {
		env = append(env, kv)
	}
	env = append(env,
		"PROJECT="+h.Project,
		"HOST="+h.Hostname,
	)
	if h.OauthToken != "" {
		env = append(env, "OAUTH2_TOKEN="+h.OauthToken)
	}
	if gitProtocol != "" {
		env = append(env, "GIT_PROTOCOL="+gitProtocol)
	}
	return env
}
