package gitproto

// upload-pack: 1:1 drop-in of WLUploadPackFactory. The Java side simply
// execs `git upload-pack --stateless-rpc <repoPath>` and pipes
// request body (stdin) to HTTP response (stdout), so the same shell-proxy
// approach is a faithful port.

import (
	"fmt"
	"io"
	"os/exec"
)

// UploadPackHandler serves upload-pack requests by spawning
// `git upload-pack --stateless-rpc <workDir>` and piping the request body
// (stdin) to the response (stdout).
//
// Mirrors WLUploadPackFactory.create:
//
//	Env env = new Env(Arrays.asList("--stateless-rpc", repository.getRepositoryPath()));
//	if (protocol != null) env.environment.put("GIT_PROTOCOL", protocol);
type UploadPackHandler struct {
	gitBinary string
}

func NewUploadPackHandler(gitBinary string) *UploadPackHandler {
	if gitBinary == "" {
		gitBinary = "git"
	}
	return &UploadPackHandler{gitBinary: gitBinary}
}

// ServeAdvertise streams the upload-pack advertisement (GET /info/refs?
// service=git-upload-pack). Live-probed (gate3a A2 clone): the GIT
// PROTOCOL must match the client's (the "Git-Protocol" header value, e.g.
// "version=2", forwarded as the GIT_PROTOCOL env) — git then emits the
// matching advertise stream (v2: the capability/info pkt stream WITHOUT
// refs, the refs arrive over the two POSTs; v0: the raw ref pkts). rc=1
// ("no refs to advertise") is a legitimate empty advertise — serve it.
func (h *UploadPackHandler) ServeAdvertise(workDir, protocol string, response io.Writer) error {
	cmd := exec.Command(
		h.gitBinary, "upload-pack", "--stateless-rpc", "--advertise-refs", workDir,
	)
	if protocol != "" {
		cmd.Env = append(cmd.Environ(), "GIT_PROTOCOL="+protocol)
	}
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			if protocol == "" {
				response.Write(encodePkt([]byte("# service=git-upload-pack\n")))
				response.Write(EncodeFlush())
			}
			_, _ = response.Write(out)
			return nil
		}
		return fmt.Errorf("gitproto: upload-pack advertise: %w", err)
	}
	// v0 advertise (no Git-Protocol header): prepend the smart service
	// banner + flush (the raw stream starts at the ref pkts; git
	// --stateless-rpc does NOT emit the banner — the smart-HTTP layer
	// does, as it does for receive-pack). v2+ advertise is self-describing
	// ("version 2.0..." stream) and is served verbatim (live-verified
	// against gate3a A2 clone).
	if protocol == "" {
		response.Write(encodePkt([]byte("# service=git-upload-pack\n")))
		response.Write(EncodeFlush())
	}
	_, _ = response.Write(out)
	return nil
}

// Handle processes an upload-pack request. protocol is the "Git-Protocol"
// header value ("" if absent); it is forwarded to git via GIT_PROTOCOL,
// exactly as the Java factory does. git's raw stdout bytes are streamed
// directly to response.
func (h *UploadPackHandler) Handle(workDir, protocol string, requestBody io.Reader, response io.Writer) error {
	cmd := exec.Command(h.gitBinary, "upload-pack", "--stateless-rpc", workDir)
	if protocol != "" {
		cmd.Env = append(cmd.Environ(), "GIT_PROTOCOL="+protocol)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("gitproto: upload-pack: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("gitproto: upload-pack: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("gitproto: failed to start upload-pack: %w", err)
	}

	// Drain child stdout → response until EOF, in parallel with pumping
	// request body → child stdin (the v2 advertise→fetch exchange is
	// interleaved, so neither side can be copied before the other).
	done := make(chan struct{})
	go func() {
		defer stdin.Close()
		_, _ = io.Copy(stdin, requestBody)
	}()
	go func() {
		defer close(done)
		_, _ = io.Copy(response, stdout)
	}()

	// Wait for the child; git exits 1 after writing protocol errors to
	// stdout (already streamed to the client). The Java factory likewise
	// propagates only the piped bytes, never the exit code.
	_ = cmd.Wait()
	<-done
	return nil
}
