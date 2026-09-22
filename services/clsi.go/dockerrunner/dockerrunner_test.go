package dockerrunner

// dockerrunner_test.go: pipeline / Run / Kill / destroy / monitor / demux /
// cappedSink / buildOpts / UnixEngine wire tests targeting >=90% coverage.
//
// fakeEngine mirrors a live Docker engine:
//   - Inspect 404 until Create
//   - Attach returns a demux-able stream (8-byte frames); Read blocks until
//     the engine releases it (container stops / is killed / is removed)
//   - Wait blocks until release (a live container) or returns immediately
//   - per-phase injectable errors (one-shot and permanent), 404/304/500 API

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"

	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"clsi/commandrunner"
	"clsi/lastprojectaccess"
)

// frame builds a Docker multiplex frame: [type][pad3][u32BE len][content].
func frame(typ byte, data []byte) []byte {
	h := make([]byte, 8)
	h[0] = typ
	binary.BigEndian.PutUint32(h[4:8], uint32(len(data)))
	return append(h, data...)
}

// fakeStream: Read delivers preloaded bytes, then blocks until released
// (simulating the attach stream staying open until the container stops).
// Single reader (the drain goroutine).
type fakeStream struct {
	data        []byte
	off         int
	waitRelease chan struct{}
	rerr        error // injected Read error after the data (nil => EOF)
	released    atomic.Bool
}

func (s *fakeStream) Read(p []byte) (int, error) {
	if s.off < len(s.data) {
		n := copy(p, s.data[s.off:])
		s.off += n
		return n, nil
	}
	<-s.waitRelease
	if s.rerr != nil {
		return 0, s.rerr
	}
	return 0, io.EOF
}

func (s *fakeStream) Release() {
	if !s.released.CompareAndSwap(false, true) {
		return
	}
	close(s.waitRelease)
}

func (s *fakeStream) Close() error { return nil }

// fakeEngine: stateful Engine fake.
//
//	*Once error fields fire on the next call of that phase.
//	bare error fields are sticky (permanent).
type fakeEngine struct {
	inspErrOnce   error
	inspErr       error
	createErrOnce error
	createErr     error
	attachErr     error
	attachData    []byte
	streamErr     error // injected into the draining stream (after data)
	startErrOnce  error
	start304Once  error
	startErr      error
	waitErrOnce   error
	waitErr       error
	waitExit      int
	blockWait     bool // Wait blocks until release (a live container)
	killErrOnce   error
	killErr       error
	removeErrOnce error
	removeErr     error
	listErr       error
	listResult    []ListedContainer
	listCalls     atomic.Int64

	mu           sync.Mutex
	containers   map[string]bool
	waitReleases map[string]*fakeStream

	created []string
	removed []string
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{
		containers:   map[string]bool{},
		waitReleases: map[string]*fakeStream{},
		blockWait:    true,
	}
}

func (f *fakeEngine) Inspect(id string) (*ContainerInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.inspErrOnce != nil {
		err := f.inspErrOnce
		f.inspErrOnce = nil
		return nil, err
	}
	if f.inspErr != nil {
		return nil, f.inspErr
	}
	if !f.containers[id] {
		return nil, &APIError{StatusCode: 404, Reason: "no such container",
			Message: "No such container: " + id}
	}
	return &ContainerInfo{ID: id, Name: "/" + id, Running: false}, nil
}

func (f *fakeEngine) Create(id string, opts CreateOpts) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, id)
	f.containers[id] = true
	if f.createErrOnce != nil {
		err := f.createErrOnce
		f.createErrOnce = nil
		return err
	}
	return f.createErr
}

func (f *fakeEngine) Attach(id string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.attachErr != nil {
		return nil, f.attachErr
	}
	stream := &fakeStream{data: f.attachData, waitRelease: make(chan struct{}), rerr: f.streamErr}
	f.waitReleases[id] = stream
	return stream, nil
}

func (f *fakeEngine) Start(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErrOnce != nil {
		err := f.startErrOnce
		f.startErrOnce = nil
		return err
	}
	if f.start304Once != nil {
		err := f.start304Once
		f.start304Once = nil
		return err
	}
	return f.startErr
}

func (f *fakeEngine) Wait(id string) (int, error) {
	f.mu.Lock()
	var once, perm error
	once, perm = f.waitErrOnce, f.waitErr
	f.waitErrOnce = nil
	block := f.blockWait
	rel := f.waitReleases[id]
	f.mu.Unlock()
	if !block || once != nil || perm != nil {
		// finished (or erroring) container: the engine closes the attach
		// stream
		if rel != nil {
			rel.Release()
		}
		return f.waitExit, once
	}
	<-rel.waitRelease
	return f.waitExit, once
}

func (f *fakeEngine) Kill(id string) error {
	f.mu.Lock()
	var once, perm error
	once, perm = f.killErrOnce, f.killErr
	f.killErrOnce = nil
	if s, ok := f.waitReleases[id]; ok && perm == nil {
		// a successful kill stops the container: the engine releases wait
		s.Release()
	}
	f.mu.Unlock()
	if perm != nil {
		return perm
	}
	return once
}

func (f *fakeEngine) Remove(id string, force bool) error {
	f.mu.Lock()
	var once, perm error
	once, perm = f.removeErrOnce, f.removeErr
	f.removeErrOnce = nil
	f.removed = append(f.removed, id)
	delete(f.containers, id)
	if s, ok := f.waitReleases[id]; ok {
		s.Release()
		delete(f.waitReleases, id)
	}
	f.mu.Unlock()
	if perm != nil {
		return perm
	}
	return once
}

func (f *fakeEngine) List() ([]ListedContainer, error) {
	f.listCalls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}

func (f *fakeEngine) createdList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.created))
	copy(out, f.created)
	return out
}

func (f *fakeEngine) removedList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.removed))
	copy(out, f.removed)
	return out
}

func (f *fakeEngine) streamReleased(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.waitReleases[id]
	if !ok {
		return false
	}
	return s.released.Load()
}

var testCounter atomic.Int64

func uniqueName() string {
	return fmt.Sprintf("prtest-%d-%x", time.Now().UnixNano(), testCounter.Add(1))
}

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		i := 0
		for i < len(kv) && kv[i] != '=' {
			i++
		}
		if i >= len(kv) {
			continue
		}
		m[kv[:i]] = kv[i+1:]
	}
	return m
}

func TestBuildOptsWire(t *testing.T) {
	f := newFakeEngine()
	r := New(RunnerConfig{
		DockerImage:     "texlive:2024.05.01",
		DockerUser:      "www-data",
		DockerRuntime:   "runc",
		SeccompProfile:  "custom.json",
		ApparmorProfile: "unconfined",
		DockerEnv:       map[string]string{"DOCKER_ENV": "on"},
		HostDirCompiles: "/h/compiles",
		HostDirOutput:   "/h/output",
	}, f)
	opts, err := r.buildOpts("p1", []string{"latexmk", "$COMPILE_DIR/x"}, "/home/proj/lat", "texlive:2024.05.01",
		12000, map[string]string{"EXTRA": "1"}, "", "")
	if err != nil {
		t.Fatalf("buildOpts: %v", err)
	}
	if opts.Image != "texlive:2024.05.01" {
		t.Errorf("image = %q", opts.Image)
	}
	if opts.Cmd[0] != "latexmk" || opts.Cmd[1] != "/compile/x" {
		t.Errorf("cmd = %v", opts.Cmd)
	}
	if opts.WorkingDir != "/compile" {
		t.Errorf("workdir = %q", opts.WorkingDir)
	}
	if !opts.NetworkDisabled {
		t.Error("networkDisabled not set")
	}
	if opts.Memory != int64(1024*1024*1024*1024) {
		t.Errorf("memory = %d", opts.Memory)
	}
	if opts.User != "www-data" {
		t.Errorf("user = %q", opts.User)
	}
	bc := opts.HostConfig.Binds[0]
	if bc != "/h/compiles/lat:/compile:rw" {
		t.Errorf("binds = %q", bc)
	}
	u := opts.HostConfig.Ulimits[0]
	if u.Name != "cpu" || u.Soft != 17 || u.Hard != 22 {
		t.Errorf("ulimit = %+v", u)
	}
	if len(opts.HostConfig.CapDrop) != 1 || opts.HostConfig.CapDrop[0] != "ALL" {
		t.Errorf("capdrop = %v", opts.HostConfig.CapDrop)
	}
	sec := opts.HostConfig.SecurityOpt
	if len(sec) != 3 || sec[0] != "no-new-privileges" || sec[1] != "seccomp=custom.json" || sec[2] != "apparmor=unconfined" {
		t.Errorf("securityOpt = %v", sec)
	}
	if opts.HostConfig.Runtime != "runc" {
		t.Errorf("runtime = %q", opts.HostConfig.Runtime)
	}
	if opts.HostConfig.LogConfig.Type != "none" {
		t.Errorf("logconfig = %+v", opts.HostConfig.LogConfig)
	}
	env := envMap(opts.Env)
	wantPath := "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usr/local/texlive/2024/bin/x86_64-linux/"
	if env["PATH"] != wantPath {
		t.Errorf("PATH = %q want %q", env["PATH"], wantPath)
	}
	if env["EXTRA"] != "1" || env["DOCKER_ENV"] != "on" {
		t.Errorf("env merge = %v", env)
	}
	if !strings.HasPrefix(opts.Name, "project-p1-") {
		t.Errorf("name = %q", opts.Name)
	}
}

func TestBuildOptsCwdSynctexOutputReadOnly(t *testing.T) {
	f := newFakeEngine()
	r := New(RunnerConfig{
		DockerImage:     "texlive",
		HostDirCompiles: "/h/compiles",
		HostDirOutput:   "/h/output",
	}, f)
	// synctex-output: remap into output dir + ro
	opts, _ := r.buildOpts("p1", nil, "/home/999/888/777/latex", "texlive", 5000, nil, "synctex-output", "sub/dir")
	if opts.WorkingDir != "/compile/sub/dir" {
		t.Errorf("cwd = %q", opts.WorkingDir)
	}
	if opts.HostConfig.Binds[0] != "/h/output/888/777/latex:/compile:ro" {
		t.Errorf("binds = %q", opts.HostConfig.Binds[0])
	}
	// synctex: ro
	opts2, _ := r.buildOpts("p1", nil, "/home/p/latex", "texlive", 5000, nil, "synctex", "")
	if opts2.HostConfig.Binds[0] != "/h/compiles/latex:/compile:ro" {
		t.Errorf("binds = %q", opts2.HostConfig.Binds[0])
	}
	// wordcount: ro
	opts3, _ := r.buildOpts("p1", nil, "/home/p/latex", "texlive", 5000, nil, "wordcount", "")
	if opts3.HostConfig.Binds[0] != "/h/compiles/latex:/compile:ro" {
		t.Errorf("binds = %q", opts3.HostConfig.Binds[0])
	}
	// override image name
	rOverride := New(RunnerConfig{DockerImage: "texlive", Override: "registry.local"}, f)
	opts4, _ := rOverride.buildOpts("p1", nil, "/h/latex", "texlive:2023", 5000, nil, "", "")
	if opts4.Image != "registry.local/texlive:2023" {
		t.Errorf("override image = %q", opts4.Image)
	}
	// rolling year default
	opts5, _ := r.buildOpts("p1", nil, "/h/latex", "texlive", 5000, nil, "", "")
	env5 := envMap(opts5.Env)
	if !strings.Contains(env5["PATH"], "texlive/rolling/") {
		t.Errorf("rolling PATH = %q", env5["PATH"])
	}
}

func TestBuildOptsImageNotAllowed(t *testing.T) {
	f := newFakeEngine()
	r := New(RunnerConfig{DockerImage: "texlive:2024.05.01", AllowedImages: []string{"texlive:2024.05.01"}}, f)
	opts, err := r.buildOpts("p1", nil, "/h/latex", "evil:1", 5000, nil, "", "")
	if err == nil {
		t.Fatalf("want not-allowed error, got %v", opts)
	}
	if err.Error() != "image not allowed" {
		t.Errorf("err = %q", err.Error())
	}
	// allowed image passes
	_, err2 := r.buildOpts("p1", nil, "/h/latex", "texlive:2024.05.01", 5000, nil, "", "")
	if err2 != nil {
		t.Errorf("allowed image rejected: %v", err2)
	}
}

func TestKillHappy(t *testing.T) {
	f := newFakeEngine()
	name := uniqueName()
	r := New(RunnerConfig{DockerImage: "texlive"}, f)
	done := make(chan error, 1)
	r.Kill(name, func(err error) { done <- err })
	if err := <-done; err != nil {
		t.Fatalf("kill err = %v", err)
	}
}

func TestKillCapitalCNotRunningSwallowed(t *testing.T) {
	f := newFakeEngine()
	name := uniqueName()
	f.killErrOnce = &APIError{StatusCode: 409, Reason: "conflict",
		Message: "Cannot kill container " + name + " is not running"}
	r := New(RunnerConfig{DockerImage: "texlive"}, f)
	done := make(chan error, 1)
	r.Kill(name, func(err error) { done <- err })
	if err := <-done; err != nil {
		t.Fatalf("capital-C regex should swallow, got %v", err)
	}
}

func TestKillLowercaseNotRunningPropagates(t *testing.T) {
	f := newFakeEngine()
	name := uniqueName()
	f.killErrOnce = &APIError{StatusCode: 409, Reason: "conflict",
		Message: "cannot kill container " + name + " is not running"}
	r := New(RunnerConfig{DockerImage: "texlive"}, f)
	done := make(chan error, 1)
	r.Kill(name, func(err error) { done <- err })
	if err := <-done; err == nil {
		t.Fatalf("lowercase 409 must propagate (divergence 3)")
	}
}

func TestErrorShapes(t *testing.T) {
	api := &APIError{StatusCode: 404, Reason: "no such container", Message: "No such container: x"}
	if api.Error() != "(HTTP code 404) no such container - No such container: x " {
		t.Errorf("api err = %q", api.Error())
	}
	var te error = &TerminatedError{}
	if te.Error() != "terminated" {
		t.Errorf("terminated = %q", te.Error())
	}
	var ee error = &ExitedError{Code: 1}
	if ee.Error() != "exited" {
		t.Errorf("exited = %q", ee.Error())
	}
	var to error = &TimedOutError{}
	if to.Error() != "container timed out" {
		t.Errorf("timeout = %q", to.Error())
	}
	if got := reason(304); got != "container already started" {
		t.Errorf("reason304 = %q", got)
	}
	if got := reason(500); got != "server error" {
		t.Errorf("reason500 = %q", got)
	}
	if got := reason(418); got != "unexpected" {
		t.Errorf("reason418 = %q", got)
	}
}

func (f *fakeEngine) stream(id string) *fakeStream {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.waitReleases[id]
}

func awaitDone(t *testing.T, label string, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: callback did not fire within 5s", label)
	}
}

type runResult struct {
	err error
	out *commandrunner.RunOutput
}

type directRun struct {
	done chan struct{}
	res  *runResult
}

// runOnceGo launches r.runOnce in a goroutine with a first-wins callback.
func runOnceGo(r *DockerRunner, opts CreateOpts, timeout int64) *directRun {
	d := &directRun{done: make(chan struct{}), res: &runResult{}}
	go func() {
		var once sync.Once
		r.runOnce(opts, timeout, func(err error, out *commandrunner.RunOutput) {
			once.Do(func() {
				d.res.err = err
				d.res.out = out
				close(d.done)
			})
		})
	}()
	return d
}

// waitForStream polls until the attach stream exists (attach happened).
func waitForStream(t *testing.T, f *fakeEngine, name string) *fakeStream {
	t.Helper()
	for i := 0; i < 2000; i++ {
		if s := f.stream(name); s != nil {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("attach stream for %s never appeared", name)
	return nil
}

func newPipelineTest(t *testing.T, attach []byte, waitExit int, waitErrOnce error) (*fakeEngine, *DockerRunner, CreateOpts) {
	t.Helper()
	f := newFakeEngine()
	f.waitExit = waitExit
	f.waitErrOnce = waitErrOnce
	f.attachData = attach
	r := New(RunnerConfig{DockerImage: "texlive:2024.05.01"}, f)
	opts, err := r.buildOpts("t1", []string{"latexmk"}, "/h/latex", "texlive:2024.05.01",
		60000, nil, "", "")
	if err != nil {
		t.Fatalf("buildOpts: %v", err)
	}
	f.containers[opts.Name] = true // skip the create path (covered separately)
	return f, r, opts
}

func asAPIError(t *testing.T, err error, want int) {
	t.Helper()
	var api *APIError
	if !errors.As(err, &api) {
		t.Fatalf("want *APIError, got %T %v", err, err)
	}
	if api.StatusCode != want {
		t.Fatalf("want status %d, got %d (%s)", want, api.StatusCode, api.Error())
	}
}

func TestRunSuccess(t *testing.T) {
	f, r, opts := newPipelineTest(t,
		func() []byte {
			b := frame(1, []byte("OUT"))
			b = append(b, frame(2, []byte("ERR"))...)
			return b
		}(), 0, nil)
	d := runOnceGo(r, opts, 60000)
	waitForStream(t, f, opts.Name).Release()
	awaitDone(t, "success", d.done)
	if d.res.err != nil {
		t.Fatalf("err = %v", d.res.err)
	}
	o := d.res.out
	if o.Stdout != "OUT" || o.Stderr != "ERR" || o.ExitCode != 0 {
		t.Fatalf("out = %+v", o)
	}
	if o.Terminated || o.Exited || o.TimedOut {
		t.Fatalf("flags = %+v", o)
	}
}

func TestRunExit137(t *testing.T) {
	f, r, opts := newPipelineTest(t, nil, 137, nil)
	d := runOnceGo(r, opts, 60000)
	waitForStream(t, f, opts.Name).Release()
	awaitDone(t, "137", d.done)
	var te *TerminatedError
	if !errors.As(d.res.err, &te) {
		t.Fatalf("want *TerminatedError, got %v", d.res.err)
	}
	if d.res.out != nil {
		t.Fatalf("out must be nil on error, got %+v", d.res.out)
	}
}

func TestRunExit1(t *testing.T) {
	f, r, opts := newPipelineTest(t, nil, 1, nil)
	d := runOnceGo(r, opts, 60000)
	waitForStream(t, f, opts.Name).Release()
	awaitDone(t, "exit1", d.done)
	var ee *ExitedError
	if !errors.As(d.res.err, &ee) {
		t.Fatalf("want *ExitedError, got %v", d.res.err)
	}
}

func TestRunWaitErrorNon404(t *testing.T) {
	boom := &APIError{StatusCode: 503, Reason: "server error", Message: "boom"}
	_, r, opts := newPipelineTest(t, nil, 0, boom)
	d := runOnceGo(r, opts, 60000)
	awaitDone(t, "wait-err", d.done)
	asAPIError(t, d.res.err, 503)
}

func TestRunWait404AutoRemoved(t *testing.T) {
	_, r, opts := newPipelineTest(t, nil, 0,
		&APIError{StatusCode: 404, Reason: "no such container", Message: "No such container"})
	// AutoRemove on => 404 wait treated as exit 0 (Node: "destroyed before
	// starting to wait").
	opts.HostConfig.AutoRemove = true
	d := runOnceGo(r, opts, 60000)
	awaitDone(t, "404-autoremove", d.done)
	if d.res.err != nil {
		t.Fatalf("err = %v", d.res.err)
	}
	if d.res.out.ExitCode != 0 {
		t.Fatalf("exit = %d", d.res.out.ExitCode)
	}
}

func TestRunAttachError(t *testing.T) {
	f, r, opts := newPipelineTest(t, nil, 0, nil)
	f.attachErr = &APIError{StatusCode: 406, Reason: "impossible to attach", Message: "no"}
	d := runOnceGo(r, opts, 60000)
	awaitDone(t, "attach-err", d.done)
	asAPIError(t, d.res.err, 406)
}

func TestRunCreateError(t *testing.T) {
	f, r, opts := newPipelineTest(t, nil, 0, nil)
	delete(f.containers, opts.Name) // force the create path
	f.createErrOnce = &APIError{StatusCode: 500, Reason: "server error", Message: "create boom"}
	d := runOnceGo(r, opts, 60000)
	awaitDone(t, "create-err", d.done)
	asAPIError(t, d.res.err, 500)
}

func TestRunStartError(t *testing.T) {
	f, r, opts := newPipelineTest(t, nil, 0, nil)
	f.startErrOnce = &APIError{StatusCode: 500, Reason: "server error", Message: "start boom"}
	d := runOnceGo(r, opts, 60000)
	awaitDone(t, "start-err", d.done)
	asAPIError(t, d.res.err, 500)
}

func TestRunStart304(t *testing.T) {
	f, r, opts := newPipelineTest(t, nil, 0, nil)
	f.start304Once = &APIError{StatusCode: 304, Reason: "container already started",
		Message: "container already started"}
	d := runOnceGo(r, opts, 60000)
	waitForStream(t, f, opts.Name).Release()
	awaitDone(t, "304", d.done)
	if d.res.err != nil {
		t.Fatalf("304 must be success, got %v", d.res.err)
	}
	if d.res.out.ExitCode != 0 {
		t.Fatalf("exit = %d", d.res.out.ExitCode)
	}
}

func TestRunTimeout(t *testing.T) {
	_, r, opts := newPipelineTest(t, nil, 0, nil)
	d := runOnceGo(r, opts, 50) // 50ms; the timer kills and releases
	awaitDone(t, "timeout", d.done)
	var to *TimedOutError
	if !errors.As(d.res.err, &to) {
		t.Fatalf("want *TimedOutError (kill-on-timeout), got %v", d.res.err)
	}
}

func TestRunTimeoutKillFails(t *testing.T) {
	f, r, opts := newPipelineTest(t, nil, 0, nil)
	f.killErrOnce = &APIError{StatusCode: 409, Reason: "conflict", Message: "cannot kill"}
	d := runOnceGo(r, opts, 50)
	// the timer fires at 50ms, its kill 409s (logged, swallowed), and the
	// wait is still blocked; release it after the timer so the timedOut
	// check wins (a failed kill does NOT release wait).
	time.Sleep(100 * time.Millisecond)
	waitForStream(t, f, opts.Name).Release()
	awaitDone(t, "timeout-killfail", d.done)
	var to *TimedOutError
	if !errors.As(d.res.err, &to) {
		t.Fatalf("want *TimedOutError, got %v", d.res.err)
	}
}

func TestRunStreamReadError(t *testing.T) {
	f, r, opts := newPipelineTest(t, nil, 0, nil)
	f.streamErr = fmt.Errorf("stream broke")
	// stream error => drain logs and finalizes (documented divergence:
	// Go finalizes where Node would hang)
	d := runOnceGo(r, opts, 60000)
	waitForStream(t, f, opts.Name).Release()
	awaitDone(t, "stream-err", d.done)
	// the finalization is the success path (stream ended, wait returned 0)
	if d.res.err != nil {
		t.Fatalf("err = %v", d.res.err)
	}
}

func TestRunImageNotAllowed(t *testing.T) {
	f := newFakeEngine()
	r := New(RunnerConfig{DockerImage: "texlive:2024.05.01",
		AllowedImages: []string{"texlive:2024.05.01"}}, f)
	res := &runResult{}
	var once sync.Once
	done := make(chan struct{})
	syncErr := func(err error, out *commandrunner.RunOutput) {
		once.Do(func() {
			res.err, res.out = err, out
			close(done)
		})
	}
	name := r.Run("p1", nil, "/h/latex", "evil:latest", 60000, nil, "", "", syncErr)
	if name != "" {
		t.Fatalf("want empty name, got %q", name)
	}
	awaitDone(t, "not-allowed", done)
	if res.err == nil || res.err.Error() != "image not allowed" {
		t.Fatalf("err = %v", res.err)
	}
	if res.out != nil {
		t.Fatalf("out must be nil, got %+v", res.out)
	}
}

func TestRun500DestroyAndRetry(t *testing.T) {
	f := newFakeEngine()
	f.waitExit = 0
	f.waitErrOnce = &APIError{StatusCode: 500, Reason: "server error", Message: "wait boom"}
	r := New(RunnerConfig{DockerImage: "texlive:2024.05.01"}, f)
	want, err := r.buildOpts("p1", []string{"latexmk"}, "/h/latex", "texlive:2024.05.01", 60000, nil, "", "")
	if err != nil {
		t.Fatalf("buildOpts: %v", err)
	}
	name := want.Name
	res := &runResult{}
	var once sync.Once
	done := make(chan struct{})
	cb := func(err error, out *commandrunner.RunOutput) {
		once.Do(func() {
			res.err, res.out = err, out
			close(done)
		})
	}
	got := r.Run("p1", []string{"latexmk"}, "/h/latex", "texlive:2024.05.01", 60000, nil, "", "", cb)
	if got != name {
		t.Fatalf("name = %q want %q", got, name)
	}
	// The 500 path force-destroys the container and re-attaches (retry):
	// the retry's attach stream is the one that stays open.
	var s *fakeStream
	for i := 0; i < 5000; i++ {
		s = f.stream(name)
		if s != nil && !s.released.Load() {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if s == nil {
		if res.err != nil {
			t.Fatalf("early error: %v", res.err)
		}
		t.Fatal("retry attach stream never appeared")
	}
	// now simulate the retried container exiting
	s.Release()
	awaitDone(t, "500-retry", done)
	if res.err != nil {
		t.Fatalf("retry should succeed, got %v", res.err)
	}
	if res.out.ExitCode != 0 {
		t.Fatalf("exit = %d", res.out.ExitCode)
	}
	rem := f.removedList()
	if len(rem) != 1 || rem[0] != name {
		t.Fatalf("removed = %v want [%s]", rem, name)
	}
}

func TestDestroy404Swallowed(t *testing.T) {
	f := newFakeEngine()
	r := New(RunnerConfig{DockerImage: "texlive"}, f)
	name := uniqueName()
	f.removeErrOnce = &APIError{StatusCode: 404, Reason: "no such container",
		Message: "No such Container: " + name}
	if err := r.destroy(name, name, true); err != nil {
		t.Fatalf("404 remove must be swallowed, got %v", err)
	}
}

func TestDestroyErrorPropagates(t *testing.T) {
	f := newFakeEngine()
	r := New(RunnerConfig{DockerImage: "texlive"}, f)
	name := uniqueName()
	f.removeErrOnce = &APIError{StatusCode: 500, Reason: "server error", Message: "boom"}
	if err := r.destroy(name, name, true); err == nil {
		t.Fatal("remove error must propagate")
	}
}

func TestDestroyEmptyIDUsesName(t *testing.T) {
	f := newFakeEngine()
	r := New(RunnerConfig{DockerImage: "texlive"}, f)
	name := uniqueName()
	if err := r.destroy(name, "", false); err != nil {
		t.Fatalf("err = %v", err)
	}
	rem := f.removedList()
	if len(rem) != 1 || rem[0] != name {
		t.Fatalf("removed = %v", rem)
	}
}

func TestDestroyOldContainers(t *testing.T) {
	f := newFakeEngine()
	r := New(RunnerConfig{DockerImage: "texlive", MaxContainerAgeMS: 5000}, f)
	now := time.Now().UnixMilli()
	oldSec := (now - 3600*1000) / 1000
	recentSec := now / 1000
	idOld := "id-old"
	idRecent := "id-recent"
	idRecentProject := "id-recentproj"
	recentProjectID := strings.Repeat("b", 24)
	lastprojectaccess.SetLastProjectAccessTime(recentProjectID, now)
	f.listResult = []ListedContainer{
		{Id: idOld, Name: "/project-" + strings.Repeat("e", 24) + "-stale", Created: oldSec},
		{Id: idRecent, Name: "/project-" + strings.Repeat("a", 24) + "-stale", Created: recentSec},
		{Id: idRecentProject, Name: "/project-" + recentProjectID + "-proj", Created: oldSec},
	}
	// the e-24 one: old, no last access record -> destroy;
	// the a-24 one: recent -> skip;
	// the recentProject one: old but recently accessed -> skip (grace).
	if err := r.destroyOldContainers(); err != nil {
		t.Fatalf("err = %v", err)
	}
	rem := f.removedList()
	want := map[string]bool{}
	for _, id := range rem {
		want[id] = true
	}
	if !want[idOld] {
		t.Fatalf("old container %s not destroyed: %v", idOld, rem)
	}
	if want[idRecent] || want[idRecentProject] {
		t.Fatalf("recent/graced containers destroyed: %v", rem)
	}
}

func TestDestroyOldContainersListError(t *testing.T) {
	f := newFakeEngine()
	r := New(RunnerConfig{DockerImage: "texlive"}, f)
	f.listErr = fmt.Errorf("list boom")
	if err := r.destroyOldContainers(); err == nil {
		t.Fatal("list error must be returned")
	}
}

func TestStartAndWaitStopMonitor(t *testing.T) {
	f := newFakeEngine()
	r := New(RunnerConfig{DockerImage: "texlive", MaxContainerAgeMS: 5000}, f)
	f.listResult = []ListedContainer{
		{Id: "m1", Name: "/project-" + strings.Repeat("c", 24) + "-x",
			Created: (time.Now().UnixMilli() - 90000) / 1000},
	}
	r.StartContainerMonitor(10 * time.Millisecond)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if f.listCalls.Load() >= 1 && len(f.removedList()) >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if f.listCalls.Load() < 1 {
		t.Fatal("monitor tick did not run")
	}
	StopContainerMonitor()
	// stopping a second time is a safe no-op
	StopContainerMonitor()
	// restarting after stop keeps the singleton consistent
	r.StartContainerMonitor(100 * time.Millisecond)
	StopContainerMonitor()
}

func TestDemuxerFrameSequence(t *testing.T) {
	stdout := &cappedSink{name: "stdout"}
	stderr := &cappedSink{name: "stderr"}
	dm := &demuxer{pendingType: noPendingType, stdout: stdout, stderr: stderr}
	input := frame(1, []byte("A"))
	input = append(input, frame(2, []byte("B"))...)
	input = append(input, frame(0, []byte("C"))...)
	input = append(input, frame(1, []byte("D"))...)
	for i := 0; i < len(input); i++ {
		dm.Write(input[i : i+1])
	}
	if string(stdout.data) != "AD" {
		t.Fatalf("stdout = %q", stdout.data)
	}
	if string(stderr.data) != "BC" {
		t.Fatalf("stderr = %q", stderr.data)
	}
}

func TestDemuxerRawFallback(t *testing.T) {
	stdout := &cappedSink{name: "stdout"}
	stderr := &cappedSink{name: "stderr"}
	dm := &demuxer{pendingType: noPendingType, stdout: stdout, stderr: stderr}
	dm.Write(frame(3, []byte("garbage")))
	if string(stdout.data) != "garbage" {
		t.Fatalf("raw passthrough to stdout failed: %q", stdout.data)
	}
	if len(stderr.data) != 0 {
		t.Fatalf("stderr should be empty: %q", stderr.data)
	}
	// subsequent writes: docker-modem switches to raw passthrough, so every
	// following byte lands verbatim in stdout (the fallback is sticky).
	extra := append(frame(1, []byte("X")), frame(2, []byte("Y"))...)
	dm.Write(extra)
	want := "garbage" + string(extra)
	if string(stdout.data) != want {
		t.Fatalf("raw mode lost: %q want %q", stdout.data, want)
	}
}

func TestCappedSink(t *testing.T) {
	s := &cappedSink{name: "s"}
	s.write([]byte("hello"))
	if string(s.data) != "hello" {
		t.Fatalf("data = %q", s.data)
	}
	// fill to the cap boundary, cross it, and confirm the marker
	s2 := &cappedSink{name: "big"}
	s2.write(make([]byte, maxOutput-3))
	s2.write([]byte("abcd")) // len is now maxOutput-1
	// one more write crosses the cap
	s2.write([]byte("x"))
	if !s2.overflowed {
		t.Fatal("overflow flag not set")
	}
	if !strings.Contains(string(s2.data), truncMarker) {
		t.Fatalf("truncation marker missing")
	}
	ln := len(s2.data)
	s2.write([]byte("junk"))
	if len(s2.data) != ln {
		t.Fatal("overflowed sink must stop appending")
	}
}

func TestCanRunSyncTeXInOutputDir(t *testing.T) {
	f := newFakeEngine()
	yes := New(RunnerConfig{HostDirOutput: "/o"}, f)
	no := New(RunnerConfig{}, f)
	if !yes.CanRunSyncTeXInOutputDir() {
		t.Fatal("expected canRunSyncTeX = true")
	}
	if no.CanRunSyncTeXInOutputDir() {
		t.Fatal("expected canRunSyncTeX = false")
	}
}

// --- UnixEngine wire tests (httptest server over a unix socket) ---

// startUnixTestServer runs an httptest server bound to a unix socket,
// returning the engine + socket path (cleaned up via t.Cleanup).
func startUnixTestServer(t *testing.T, handler http.HandlerFunc) *UnixEngine {
	t.Helper()
	l, err := net.Listen("unix", filepath.Join(t.TempDir(), "docker.sock"))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ready := make(chan struct{})
	srv := &http.Server{Handler: handler}
	go func() {
		close(ready)
		srv.Serve(l)
	}()
	<-ready
	t.Cleanup(func() { srv.Shutdown(context.Background()) })
	return &UnixEngine{SocketPath: l.Addr().String()}
}

// --- UnixEngine wire tests (live unix-socket httptest server) ---

func TestUnixEngineRoutes(t *testing.T) {
	e := startUnixTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/containers/abc/json":
			fmt.Fprint(w, `{"Id":"abc","Name":"/abc","State":true}`)
		case r.Method == "POST" && r.URL.Path == "/containers/create" && r.URL.Query().Get("name") == "abc":
		case r.Method == "POST" && r.URL.Path == "/containers/abc/attach" && r.URL.Query().Get("stdout") == "1" && r.URL.Query().Get("stream") == "1":
			w.WriteHeader(200)
			fmt.Fprint(w, string(frame(1, []byte("hello"))))
		case r.Method == "POST" && r.URL.Path == "/containers/abc/start":
			w.WriteHeader(200)
		case r.Method == "POST" && r.URL.Path == "/containers/abc/wait":
			w.WriteHeader(200)
			fmt.Fprint(w, `{"StatusCode":9}`)
		case r.Method == "POST" && r.URL.Path == "/containers/abc/kill":
			w.WriteHeader(409)
			fmt.Fprint(w, `{"message":"Cannot kill container abc is not running"}`)
		case r.Method == "DELETE" && r.URL.Path == "/containers/abc" && r.URL.Query().Get("force") == "true" && r.URL.Query().Get("v") == "true":
			w.WriteHeader(200)
		case r.Method == "GET" && r.URL.Path == "/containers/json" && r.URL.Query().Get("all") == "true":
			w.WriteHeader(200)
			fmt.Fprint(w, `[{"Id":"abc","Name":null,"Names":["/abc"],"Created":1000}]`)
		default:
			w.WriteHeader(404)
			fmt.Fprint(w, `{"message":"No such container: abc"}`)
		}
	})

	info, err := e.Inspect("abc")
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if info.ID != "abc" || info.Name != "/abc" || !info.Running {
		t.Fatalf("info = %+v", info)
	}

	if err := e.Create("abc", CreateOpts{Image: "texlive", WorkingDir: "/compile"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	stream, err := e.Attach("abc")
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer stream.Close()
	wantFrame := frame(1, []byte("hello"))
	buf := make([]byte, len(wantFrame))
	if n, err := io.ReadFull(stream, buf); n != len(wantFrame) {
		t.Fatalf("attach stream short: read %d (%v)", n, err)
	}
	if string(buf) != string(wantFrame) {
		t.Fatalf("attach stream payload = %x want %x", buf, wantFrame)
	}

	if err := e.Start("abc"); err != nil {
		t.Fatalf("start: %v", err)
	}

	code, err := e.Wait("abc")
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code != 9 {
		t.Fatalf("code = %d", code)
	}

	err = e.Kill("abc")
	var api *APIError
	if !errors.As(err, &api) || api.StatusCode != 409 {
		t.Fatalf("kill want 409 APIError, got %v", err)
	}
	if api.Error() != "(HTTP code 409) unexpected - Cannot kill container abc is not running " {
		t.Fatalf("kill err = %q", api.Error())
	}

	if err := e.Remove("abc", true); err != nil {
		t.Fatalf("remove: %v", err)
	}

	list, err := e.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Id != "abc" || list[0].Names[0] != "/abc" || list[0].Created != 1000 {
		t.Fatalf("list = %+v", list)
	}
}

func TestUnixEngineErrors(t *testing.T) {
	e := startUnixTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/containers/missing/json":
			w.WriteHeader(404)
			fmt.Fprint(w, `{"message":"No such container: missing"}`)
		case r.Method == "POST" && r.URL.Path == "/containers/boom/attach":
			w.WriteHeader(500)
			fmt.Fprint(w, `{"cause":"internal boom","message":"boom"}`)
		case r.Method == "GET" && r.URL.Path == "/containers/json":
			w.WriteHeader(502)
			fmt.Fprint(w, `raw non-json body`)
		case r.URL.Path == "/containers/abc/json":
			w.WriteHeader(200)
			fmt.Fprint(w, `not-json`)
		default:
			w.WriteHeader(404)
			fmt.Fprint(w, `{"message":"no"}`)
		}
	})

	_, err := e.Inspect("missing")
	var api *APIError
	if !errors.As(err, &api) || api.StatusCode != 404 {
		t.Fatalf("inspect 404 want APIError, got %v", err)
	}

	_, err = e.Attach("boom")
	if !errors.As(err, &api) || api.StatusCode != 500 || api.Message != "boom" {
		t.Fatalf("attach error-shape want message-precedence, got %v", err)
	}

	_, err = e.List()
	if !errors.As(err, &api) || api.Message != "raw non-json body" {
		t.Fatalf("list raw body fallback = %v", err)
	}

	_, err = e.Inspect("abc")
	if err == nil {
		t.Fatal("inspect non-JSON must fail")
	}

	if _, err := e.Wait("abc"); err == nil {
		t.Fatal("wait non-JSON must fail")
	}
}

func TestUnixEngineAttachNon2xx(t *testing.T) {
	e := startUnixTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
		fmt.Fprint(w, `{"message":"container is running"}`)
	})
	_, err := e.Attach("abc")
	var api *APIError
	if !errors.As(err, &api) || api.StatusCode != 409 {
		t.Fatalf("attach non-2xx want 409, got %v", err)
	}
}

func TestReasonTable(t *testing.T) {
	for code, want := range map[int]string{
		304: "container already started",
		400: "bad parameter",
		404: "no such container",
		406: "impossible to attach",
		500: "server error",
		418: "unexpected",
		409: "unexpected",
		200: "unexpected",
	} {
		if got := reason(code); got != want {
			t.Fatalf("reason(%d) = %q want %q", code, got, want)
		}
	}
}
