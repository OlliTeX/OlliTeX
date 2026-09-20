package urlfetcher

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clsi/config"
)

// withEnv pins CLSI_PERF_HOST/PORT + FILESTORE_DOMAIN_OVERRIDE and re-builds
// the config singleton (restored on cleanup).
func withEnv(t *testing.T, override string) {
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", "/tmp/c")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_CACHE", "/tmp/nc")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_OUTPUT", "/tmp/o")
	t.Setenv("CLSI_PERF_HOST", "127.0.0.1")
	t.Setenv("CLSI_PERF_PORT", "3043")
	t.Setenv("FILESTORE_DOMAIN_OVERRIDE", override)
	config.ForTest()
}

type fakeReader struct {
	data []byte
	err  error
}

func (f *fakeReader) Read(p []byte) (int, error) {
	if len(f.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, f.data)
	f.data = f.data[n:]
	return n, nil
}
func (f *fakeReader) Close() error { return nil }

func newFake(data string) *fakeReader {
	return &fakeReader{data: []byte(data)}
}

func TestInferSource(t *testing.T) {
	withEnv(t, "")
	cases := map[string]string{
		"http://filestore/project/abc/file/def":       "user-files",
		"http://filestore/bucket/project-blobs/key/x": "project-blobs",
		"http://example.com/foo/":                     "unknown",
		"http://foo.com/key/nothing/":                 "unknown",
	}
	// perf-host case needs a URL containing the configured perf host.
	perf := "http://127.0.0.1:3043/file"
	if got := InferSource(perf); got != "clsi-perf" {
		t.Errorf("InferSource(perf) = %q, want 'clsi-perf'", got)
	}
	for in, want := range cases {
		if got := InferSource(in); got != want {
			t.Errorf("InferSource(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPipeUrlToFileWithRetry_WritesFile(t *testing.T) {
	withEnv(t, "")
	file := filepath.Join(t.TempDir(), "file.txt")
	orig := fetchReader
	fetchReader = func(u string) (io.ReadCloser, error) {
		if strings.Contains(u, "127.0.0.1:3043") {
			return newFake("perf bytes"), nil
		}
		return newFake("hello filestore"), nil
	}
	defer func() { fetchReader = orig }()

	if err := PipeUrlToFileWithRetry("http://filestore/project/p/file/f", "", file); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "hello filestore" {
		t.Errorf("data = %q, want 'hello filestore'", data)
	}
	if _, err := os.Stat(file + "~"); !os.IsNotExist(err) {
		t.Errorf("atomic '~' path should be renamed away")
	}
}

func TestPipeUrlToFileWithRetry_FallbackOn404(t *testing.T) {
	withEnv(t, "")
	file := filepath.Join(t.TempDir(), "out.txt")
	orig := fetchReader
	fetchReader = func(u string) (io.ReadCloser, error) {
		if strings.Contains(u, "primary") {
			return nil, &RequestFailedError{Response: &http.Response{StatusCode: 404}}
		}
		return newFake("fallback ok"), nil
	}
	defer func() { fetchReader = orig }()

	if err := PipeUrlToFileWithRetry("http://example/primary", "http://example/fallback", file); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	data, _ := os.ReadFile(file)
	if string(data) != "fallback ok" {
		t.Errorf("data = %q, want 'fallback ok'", data)
	}
}

func TestPipeUrlToFileWithRetry_NoFallbackWhenMissing(t *testing.T) {
	withEnv(t, "")
	file := filepath.Join(t.TempDir(), "out.txt")
	orig := fetchReader
	fetchReader = func(u string) (io.ReadCloser, error) {
		return nil, &RequestFailedError{Response: &http.Response{StatusCode: 404}}
	}
	defer func() { fetchReader = orig }()

	if err := PipeUrlToFileWithRetry("http://example/primary", "", file); err == nil {
		t.Fatalf("want error (no fallback url)")
	}
}

func TestPipeUrlToFileWithRetry_RetriesThenSucceeds(t *testing.T) {
	withEnv(t, "")
	file := filepath.Join(t.TempDir(), "out.txt")
	calls := 0
	orig := fetchReader
	fetchReader = func(u string) (io.ReadCloser, error) {
		calls++
		if calls < 3 {
			return nil, &RequestFailedError{Err: errors.New("transient")}
		}
		return newFake("retry"), nil
	}
	defer func() { fetchReader = orig }()

	if err := PipeUrlToFileWithRetry("http://whatever", "", file); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if calls != 3 {
		t.Errorf("called %d, want 3 (two failures + one success)", calls)
	}
}

func TestPipeUrlToFileWithRetry_FailsAfterThree(t *testing.T) {
	withEnv(t, "")
	file := filepath.Join(t.TempDir(), "out.txt")
	calls := 0
	orig := fetchReader
	fetchReader = func(u string) (io.ReadCloser, error) {
		calls++
		return nil, &RequestFailedError{Err: errors.New("hard fail")}
	}
	defer func() { fetchReader = orig }()

	if err := PipeUrlToFileWithRetry("http://whatever", "", file); err == nil {
		t.Fatalf("want error after 3 attempts")
	}
	if calls != 3 {
		t.Errorf("called %d, want 3", calls)
	}
}

func TestRewriteThroughOverrideHostRewrite(t *testing.T) {
	withEnv(t, "filestore.example.local")
	got, ok := rewriteThroughOverride("http://filestore/project/p/file/f?foo=bar")
	if !ok {
		t.Fatalf("want rewrite")
	}
	if got != "filestore.example.local/project/p/file/f?foo=bar" {
		t.Errorf("rewritten = %q, want 'filestore.example.local/project/p/file/f?foo=bar'", got)
	}
}

func TestRewriteThroughOverrideSkipsForPerfHost(t *testing.T) {
	withEnv(t, "filestore.example.local")
	u := "http://127.0.0.1:3043/project/p/file/f"
	got, ok := rewriteThroughOverride(u)
	if ok {
		t.Fatalf("perf host should not be rewritten")
	}
	if got != u {
		t.Errorf("url = %q, want unchanged %q", got, u)
	}
}

func TestRewriteThroughOverrideEmptyOverride(t *testing.T) {
	withEnv(t, "")
	u := "http://filestore/project/p/file/f"
	got, ok := rewriteThroughOverride(u)
	if ok {
		t.Fatalf("empty override must be a no-op")
	}
	if got != u {
		t.Errorf("url = %q, want %q", got, u)
	}
}

func TestRequestFailedError_ErrorForms(t *testing.T) {
	r := &RequestFailedError{Response: &http.Response{StatusCode: 418}}
	if got := r.Error(); got != "request failed: 418" {
		t.Errorf("resp form = %q", got)
	}
	e := &RequestFailedError{Err: errors.New("dial tcp: no")}
	if got := e.Error(); got != "request failed: dial tcp: no" {
		t.Errorf("err form = %q", got)
	}
	n := &RequestFailedError{}
	if got := n.Error(); got != "request failed" {
		t.Errorf("bare form = %q", got)
	}
}

func TestPipeUrlToFile_Non404Propagates(t *testing.T) {
	withEnv(t, "")
	file := filepath.Join(t.TempDir(), "out.txt")
	orig := fetchReader
	fetchReader = func(u string) (io.ReadCloser, error) {
		return nil, &RequestFailedError{Err: errors.New("boom")}
	}
	defer func() { fetchReader = orig }()
	if err := pipeUrlToFile("http://example/primary", "http://example/fallback", file); err == nil {
		t.Fatalf("want error propagation (non-404 primary, fallback NOT used)")
	}
}

func TestPipeUrlToFile_AtomicFailureCleansUp(t *testing.T) {
	withEnv(t, "")
	// point filePath at a directory so os.Create(filePath+"~") fails
	orig := fetchReader
	fetchReader = func(u string) (io.ReadCloser, error) { return newFake("x"), nil }
	defer func() { fetchReader = orig }()
	if err := pipeUrlToFile("http://example/primary", "", t.TempDir()); err == nil {
		t.Fatalf("want error (atomic path is a directory)")
	}
}

func TestPipeUrlToFile_404_FallbackRewrite(t *testing.T) {
	withEnv(t, "http://override")
	// primary 404 → fallback used AND rewritten (override host matches, so
	// rewritten URL host differs from perf host → fallback rewrite applies).
	file := filepath.Join(t.TempDir(), "out.txt")
	orig := fetchReader
	fetchReader = func(u string) (io.ReadCloser, error) {
		// rewriteThroughOverride("http://example/primary") returns rewritten
		// (filestoreDomainOverride "http://override" != perf host), so u here
		// is "http://override/project/p/file/f?". Signal that 404 via response.
		return nil, &RequestFailedError{Response: &http.Response{StatusCode: 404}, Err: nil}
	}
	defer func() { fetchReader = orig }()
	// fallback URL must be rewritten: rewriteThroughOverride(fallback) is
	// entered (fallbackURL != "") and its ok=true path taken.
	if err := pipeUrlToFile("http://filestore/project/p/file/f", "http://filestore/fallback/file/f", file); err == nil {
		t.Fatalf("want error (fallback fetch not faked)")
	}
}

func TestPipeUrlToFile_RewriteFallbackNoop(t *testing.T) {
	// With override == "" fallback rewrite is skipped (no-op branch).
	withEnv(t, "")
	orig := fetchReader
	fetchReader = func(u string) (io.ReadCloser, error) { return nil, &RequestFailedError{Err: errors.New("x")} }
	defer func() { fetchReader = orig }()
	file := filepath.Join(t.TempDir(), "out.txt")
	// exercises "primary fetch fails, no fallback" path (line 97 else)
	err := pipeUrlToFile("http://x", "", file)
	if err == nil {
		t.Errorf("want error from pipeUrlToFile")
	}
}

// brokenReader returns an error on Read, forcing io.Copy to fail so
// copyToAtomic takes its error + cleanup path.
type brokenReader struct{}

func (brokenReader) Read(p []byte) (int, error) {
	return 0, errors.New("boom")
}

func TestCopyToAtomic_CopyErrorCleansUp(t *testing.T) {
	dir := t.TempDir()
	atomicPath := filepath.Join(dir, "file.bin~")
	if err := copyToAtomic(brokenReader{}, atomicPath); err == nil {
		t.Fatalf("want copy error (broken reader), got nil")
	}
	if _, statErr := os.Stat(atomicPath); !os.IsNotExist(statErr) {
		t.Errorf("atomic path should be removed after copy error (stat err = %v)", statErr)
	}
}

func TestDefaultFetchReader_Happy(t *testing.T) {
	withEnv(t, "")
	origFetchURL := fetchURL
	origFetchReader := fetchReader
	body := newFake("payload")
	fetchURL = func(u string) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: body}, nil
	}
	fetchReader = defaultFetchReader
	defer func() {
		fetchURL = origFetchURL
		fetchReader = origFetchReader
	}()

	file := t.TempDir() + "/out.txt"
	if err := pipeUrlToFile("http://filestore/x", "", file); err != nil {
		t.Fatalf("default reader pipe: %v", err)
	}
	data, _ := os.ReadFile(file)
	if string(data) != "payload" {
		t.Errorf("data = %q, want 'payload'", data)
	}
}

func TestDefaultFetchReader_Non404Status(t *testing.T) {
	withEnv(t, "")
	origFetchURL := fetchURL
	fetchURL = func(u string) (*http.Response, error) {
		return &http.Response{StatusCode: 404, Body: newFake("nf")}, nil
	}
	defer func() { fetchURL = origFetchURL }()
	// no fallback: a 404 must surface as RequestFailedError w/ response 404
	err := pipeUrlToFile("http://filestore/x", "", t.TempDir()+"/out.txt")
	rfe, ok := err.(*RequestFailedError)
	if !ok || rfe.Response == nil || rfe.Response.StatusCode != 404 {
		t.Errorf("want *RequestFailedError w/ 404 response, got %v", err)
	}
}

func TestDefaultFetchReader_RawGetError(t *testing.T) {
	withEnv(t, "")
	origFetchURL := fetchURL
	fetchURL = func(u string) (*http.Response, error) {
		return nil, errors.New("dial tcp: i/o timeout")
	}
	defer func() { fetchURL = origFetchURL }()
	if _, rerr := defaultFetchReader("http://x"); rerr == nil {
		t.Errorf("want error from raw fetch failure")
	}
}
