package toolkit

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/moby/moby/client"
)

// Shells — the interactive shell actions (parity with the toolkit's
// mongo/pg/app shells; target = the live container of the project).
var Shells = []shellSpec{
	{Label: "mongo (mongosh)", Service: "mongo", Cmdline: []string{"mongosh", "sharelatex"}},
	{Label: "postgres (psql)", Service: "postgres", Cmdline: []string{"psql", "-U", "overleaf", "overleaf-history-v1"}},
	{Label: "app sh (/app)", Service: "ollitex", Cmdline: []string{"sh", "-c", "cd /app && exec sh"}},
	{Label: "app sh (/home/overleaf)", Service: "ollitex", Cmdline: []string{"sh", "-c", "cd /home/overleaf && exec sh"}},
}

type shellSpec struct {
	Label   string
	Service string
	Cmdline []string
}

// ShellSession is a live exec attach (duplex) into a container.
type ShellSession struct {
	Conn  net.Conn
	label string
}

// NewShell starts the exec and attaches (duplex, non-tty so we demux the
// stdout/stderr multiplex ourselves; psql/mongosh are line-based).
func NewShell(ctx context.Context, d *Docker, project string, label string) (*ShellSession, error) {
	var spec *shellSpec
	for i := range Shells {
		if Shells[i].Label == label {
			spec = &Shells[i]
		}
	}
	if spec == nil {
		return nil, fmt.Errorf("unknown shell %q", label)
	}
	cid, err := d.containerByService(ctx, project, spec.Service)
	if err != nil {
		return nil, err
	}
	ex, err := d.cli.ExecCreate(ctx, cid, client.ExecCreateOptions{
		Cmd:          spec.Cmdline,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return nil, fmt.Errorf("exec create %s: %w", spec.Label, err)
	}
	// attach starts + hijacks the exec (this client's pattern — a separate
	// ExecStart is not allowed once attach has run, and detach-start + attach
	// is rejected by the daemon).
	att, err := d.cli.ExecAttach(ctx, ex.ID, client.ExecAttachOptions{TTY: false})
	if err != nil {
		return nil, fmt.Errorf("exec attach %s: %w", spec.Label, err)
	}
	return &ShellSession{Conn: att.Conn, label: label}, nil
}

// containerByService returns the container ID of one service of the project.
func (d *Docker) containerByService(ctx context.Context, project, service string) (string, error) {
	rows, err := d.ListProject(ctx, project)
	if err != nil {
		return "", err
	}
	for _, c := range rows {
		// prefer the compose service label; fall back to name suffix
		if svc := c.Labels["com.docker.compose.service"]; svc == service {
			return c.Name, nil
		}
		base := strings.TrimPrefix(c.Name, "/")
		if base == service {
			return c.Name, nil
		}
		// project-prefixed: <project>-<service>-<n>
		for _, part := range strings.Split(base, "-") {
			if part == service {
				return c.Name, nil
			}
		}
	}
	return "", fmt.Errorf("no %q container in project %q — is the stack up?", service, project)
}

// Write forwards operator input.
func (s *ShellSession) Write(b []byte) (int, error) { return s.Conn.Write(b) }

// ReadOne blocks for the next chunk.
func (s *ShellSession) ReadOne(buf []byte) (int, error) { return s.Conn.Read(buf) }

// Close ends the session.
func (s *ShellSession) Close() error { return s.Conn.Close() }

// demuxFrame decodes stdcopy multiplexed frames (8-byte header:
// [stream,3 pad, size BE32]) — transcribed from moby stdcopy semantics.
// Returns bytes consumed and whether the buffer is fully consumed.
func demuxFrame(b []byte, out *strings.Builder) (int, bool) {
	consumed := 0
	for consumed+8 <= len(b) {
		size := int(uint32(b[consumed+4])<<24 | uint32(b[consumed+5])<<16 | uint32(b[consumed+6])<<8 | uint32(b[consumed+7]))
		if consumed+8+size > len(b) {
			return consumed, false // partial frame: keep the tail (do not emit it)
		}
		out.Write(b[consumed+8 : consumed+8+size])
		consumed += 8 + size
	}
	if len(b) < 8 {
		// smaller than one header: raw (tty) output — pass through
		out.Write(b)
		return len(b), true
	}
	// one or more complete frames consumed; nothing pending
	return consumed, true
}

// DecodeShellChunk demultiplexes a non-tty stream chunk into text.
func DecodeShellChunk(b []byte) string {
	var sb strings.Builder
	demuxFrame(b, &sb)
	return sb.String()
}
