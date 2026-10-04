package toolkit

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// Docker is the official moby client wrapper (docker socket). Structured
// status + log reads go through the SDK; whole-stack orchestration (ordered
// up/down, health gating) goes through the bundled compose CLI.
type Docker struct {
	cli  *client.Client
	sock string
}

// NewDocker dials the daemon over the mounted unix socket.
func NewDocker(dockerSock string) (*Docker, error) {
	if !strings.HasPrefix(dockerSock, "unix://") {
		dockerSock = "unix://" + dockerSock
	}
	cli, err := client.New(client.WithHost(dockerSock))
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &Docker{cli: cli, sock: dockerSock}, nil
}

// Ping checks daemon reachability (Doctor screen).
func (d *Docker) Ping(ctx context.Context) error {
	_, err := d.cli.Ping(ctx, client.PingOptions{})
	return err
}

// Status is one daemon container in operator form.
type Status struct {
	Name   string
	Image  string
	State  string            // created | running | paused | restarting | exiting | dead
	Status string            // daemon status string
	Health string            // "" | starting | healthy | unhealthy
	Ports  string            // compact published port list
	Labels map[string]string
	Up     bool
}

// projectFilter builds the compose-project filter (label first).
func projectFilter(project string) client.Filters {
	return make(client.Filters).Add("label", "com.docker.compose.project="+project)
}

// ListProject returns the stack's containers (compose project label; name-
// prefix fallback for non-compose stacks).
func (d *Docker) ListProject(ctx context.Context, project string) ([]Status, error) {
	res, err := d.cli.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: projectFilter(project),
	})
	if err != nil {
		return nil, fmt.Errorf("container list: %w", err)
	}
	if len(res.Items) == 0 {
		res, err = d.cli.ContainerList(ctx, client.ContainerListOptions{
			All:     true,
			Filters: make(client.Filters).Add("name", project),
		})
		if err != nil {
			return nil, fmt.Errorf("container list: %w", err)
		}
	}
	out := make([]Status, 0, len(res.Items))
	for _, c := range res.Items {
		st := Status{
			Name:   firstNames(c.Names),
			Image:  shortName(c.Image),
			State:  string(c.State),
			Status: c.Status,
			Ports:  compactPorts(c.Ports),
			Labels: c.Labels,
			Up:     c.State == "running",
		}
		if c.Health != nil {
			st.Health = string(c.Health.Status)
		}
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Images returns local images in stable order (ID, repo tags).
type ImageRow struct {
	ID   string
	Tags string
	Size int64
}

func (d *Docker) Images(ctx context.Context) ([]ImageRow, error) {
	res, err := d.cli.ImageList(ctx, client.ImageListOptions{All: true})
	if err != nil {
		return nil, err
	}
	out := make([]ImageRow, 0, len(res.Items))
	for _, im := range res.Items {
		id := shortID(im.ID)
		if id == "" {
			continue
		}
		out = append(out, ImageRow{ID: id, Tags: strings.Join(im.RepoTags, ", "), Size: im.Size})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tags != "" && out[j].Tags == "" {
			return true
		}
		if out[i].Tags == "" && out[j].Tags != "" {
			return false
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// LogStreamer tails a container's logs (follow).
type LogStreamer struct {
	sc  *bufio.Scanner
	rsc io.ReadCloser
}

// TailLog starts a follow-tail on the container.
func (d *Docker) TailLog(ctx context.Context, name string, n int) (*LogStreamer, error) {
	rc, err := d.cli.ContainerLogs(ctx, name, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Tail:       fmt.Sprintf("%d", n),
	})
	if err != nil {
		return nil, fmt.Errorf("container logs %s: %w", name, err)
	}
	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &LogStreamer{sc: sc, rsc: rc}, nil
}

// ScanLog decodes one multiplexed (or tty) log frame from the stream.
func (l *LogStreamer) ScanLog() (bool, string) {
	if !l.sc.Scan() {
		return false, ""
	}
	raw := l.sc.Bytes()
	// non-tty streams are stdcopy-multiplexed: 8-byte header + payload.
	if len(raw) > 8 {
		return true, string(raw[8:])
	}
	return true, string(raw)
}

func (l *LogStreamer) Err() error { return l.sc.Err() }
func (l *LogStreamer) Close() error {
	err := l.rsc.Close()
	l.rsc = nil
	return err
}

func firstNames(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.TrimPrefix(names[0], "/")
}

func shortName(tag string) string {
	// ollitex/main-amd64:latest → main-amd64:latest
	if i := strings.LastIndex(tag, "/"); i >= 0 {
		return tag[i+1:]
	}
	return tag
}

func shortID(id string) string {
	if len(id) >= 12 {
		return id[:12]
	}
	return id
}

func compactPorts(ps []container.PortSummary) string {
	if len(ps) == 0 {
		return "-"
	}
	var b strings.Builder
	for i, p := range ps {
		if i > 0 {
			b.WriteString(" ")
		}
		if p.PublicPort > 0 {
			fmt.Fprintf(&b, "%s:%d/%s", p.IP, p.PublicPort, p.Type)
		} else {
			fmt.Fprintf(&b, "%d/%s", p.PrivatePort, p.Type)
		}
	}
	return b.String()
}

// ---------- compose lifecycle (bundled CLI) ---------------------------------

func (t *Toolkit) composeExec(ctx context.Context, args ...string) (string, error) {
	full := append([]string{"compose", "-f", t.ComposeFile, "--project-name", t.Project}, args...)
	cmd := exec.CommandContext(ctx, "docker", full...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		return strings.TrimSpace(out.String() + "\n" + errb.String()), err
	}
	return strings.TrimSpace(out.String()), nil
}

// StackUp starts the whole stack.
func (t *Toolkit) StackUp(ctx context.Context) (string, error) {
	return t.composeExec(ctx, "up", "-d")
}

// StackDown stops the stack (keeps containers/volumes unless asked).
func (t *Toolkit) StackDown(ctx context.Context, removeVolumes bool) (string, error) {
	args := []string{"down"}
	if removeVolumes {
		args = append(args, "-v")
	}
	return t.composeExec(ctx, args...)
}

// StackRestart restarts the stack.
func (t *Toolkit) StackRestart(ctx context.Context) (string, error) {
	if _, err := t.StackDown(ctx, false); err != nil {
		return "", err
	}
	return t.StackUp(ctx)
}

// PullImages pulls the stack's images (compose pull).
func (t *Toolkit) PullImages(ctx context.Context) (string, error) {
	return t.composeExec(ctx, "pull")
}

// StackImages lists the stack's image references (compose config --images).
func (t *Toolkit) StackImages(ctx context.Context) ([]string, error) {
	out, err := t.composeExec(ctx, "config", "--images")
	if err != nil {
		return nil, fmt.Errorf("compose config --images: %w: %s", err, out)
	}
	var imgs []string
	for _, line := range strings.Split(out, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			imgs = append(imgs, s)
		}
	}
	return imgs, nil
}
