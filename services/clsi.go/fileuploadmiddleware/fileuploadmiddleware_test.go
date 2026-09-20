package fileuploadmiddleware

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"clsi/config"
)

// makeBody builds a multipart body: `fields` field (non-file) parts plus,
// if `fileBody != ""`, one "qqfile" file part. Returns the Content-Type
// (matching the body's real boundary) and the body bytes.
func makeBody(t *testing.T, fields int, fileBody string) (ct string, body []byte) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for i := 0; i < fields; i++ {
		if err := w.WriteField("f"+string(rune('A'+i)), "v"); err != nil {
			t.Fatalf("field: %v", err)
		}
	}
	if fileBody != "" {
		fw, err := w.CreateFormFile("qqfile", "dummy.docx")
		if err != nil {
			t.Fatalf("file part: %v", err)
		}
		if _, err := fw.Write([]byte(fileBody)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return "multipart/form-data; boundary=" + w.Boundary(), buf.Bytes()
}

type harness struct {
	rec  *httptest.ResponseRecorder
	next bool
	file *UploadedFile
}

func (h *harness) runReq(c *Middleware, ct string, body []byte) {
	h.rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/u", bytes.NewReader(body))
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	req.ContentLength = int64(len(body))
	c.Handle(func(w http.ResponseWriter, _ *http.Request, f *UploadedFile) {
		h.next = true
		h.file = f
		w.WriteHeader(200)
	})(h.rec, req)
}

func (h *harness) run(c *Middleware, t *testing.T, fields int, fileBody string) {
	ct, body := makeBody(t, fields, fileBody)
	h.runReq(c, ct, body)
}

func TestHappyPath(t *testing.T) {
	dir := t.TempDir()
	h := &harness{}
	h.run(newClient(dir, 50*1024*1024), t, 0, "hello world")
	if h.rec.Code != 200 {
		t.Fatalf("expected 200 got %d (%s)", h.rec.Code, h.rec.Body.String())
	}
	if !h.next || h.file == nil || h.file.Path == "" {
		t.Fatalf("no file: %+v %v", h.file, h.next)
	}
	if _, err := os.Stat(h.file.Path); err != nil {
		t.Fatalf("saved file missing: %v", err)
	}
	if h.file.Size != int64(len("hello world")) {
		t.Fatalf("size: got %d want %d", h.file.Size, len("hello world"))
	}
	if h.file.Fieldname != "qqfile" {
		t.Fatalf("fieldname: %q", h.file.Fieldname)
	}
	if fileExt(h.file.Filename) == "" {
		t.Fatalf("filename should have extension: %q", h.file.Filename)
	}
}

func TestFileTooLarge(t *testing.T) {
	dir := t.TempDir()
	h := &harness{}
	h.run(newClient(dir, 5), t, 0, "0123456789")
	if h.rec.Code != 422 {
		t.Fatalf("expected 422 got %d (%s)", h.rec.Code, h.rec.Body.String())
	}
	if h.next {
		t.Fatalf("next called on 422")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("partial file not removed: %v", entries)
	}
}

func TestNoFile(t *testing.T) {
	dir := t.TempDir()
	h := &harness{}
	h.run(newClient(dir, 50*1024*1024), t, 1, "")
	if h.rec.Code != 400 {
		t.Fatalf("expected 400 got %d (%s)", h.rec.Code, h.rec.Body.String())
	}
	if h.next {
		t.Fatalf("next called on 400")
	}
}

func TestMissingBody(t *testing.T) {
	h := &harness{}
	h.runReq(newClient(t.TempDir(), 50*1024*1024), "", nil)
	if h.rec.Code != 400 {
		t.Fatalf("expected 400 got %d", h.rec.Code)
	}
	if h.next {
		t.Fatalf("next called on missing body")
	}
}

func TestMalformedMultipart(t *testing.T) {
	// Claims multipart Content-Type but body is not a valid multipart stream
	// => MultipartReader() fails => Handle returns error (500, mirroring
	// next(err)).
	h := &harness{}
	h.runReq(newClient(t.TempDir(), 50*1024*1024),
		"multipart/form-data; boundary=XXX", []byte("not-a-multipart-body"))
	if h.next {
		t.Fatalf("next called on malformed multipart")
	}
}

func TestSavePartWriteError(t *testing.T) {
	// writeFile seam error => savePart returns write error.
	c := newClient(t.TempDir(), 50*1024*1024)
	c.writeFile = func(*os.File, []byte) (int, error) { return 0, os.ErrClosed }
	h := &harness{}
	ct, body := makeBody(t, 0, "payload")
	h.runReq(c, ct, body)
	if h.rec.Code != 500 {
		t.Fatalf("write error: expected 500 got %d", h.rec.Code)
	}
	if h.next {
		t.Fatalf("next called on write error")
	}
}

func TestSecondFilePartIgnored(t *testing.T) {
	// Two qqfile parts => only the first is extracted; the second is drained.
	// (upload.single semantics.)
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for i := 0; i < 2; i++ {
		fw, err := w.CreateFormFile("qqfile", strings.Repeat("f", i) + ".tex")
		if err != nil {
			t.Fatalf("part: %v", err)
		}
		fw.Write([]byte("body"))
	}
	w.Close()
	h := &harness{}
	h.runReq(newClient(t.TempDir(), 50*1024*1024),
		"multipart/form-data; boundary="+w.Boundary(), buf.Bytes())
	if h.rec.Code != 200 {
		t.Fatalf("first-file-only: expected 200 got %d", h.rec.Code)
	}
	if h.file == nil || h.file.Size == 0 {
		t.Fatalf("file not extracted")
	}
}

func TestPartsLimit(t *testing.T) {
	h := &harness{}
	h.run(newClient(t.TempDir(), 50*1024*1024), t, 2, "v")
	if h.rec.Code != 500 {
		t.Fatalf("expected 500 got %d (%s)", h.rec.Code, h.rec.Body.String())
	}
	if h.next {
		t.Fatalf("next called on error")
	}
}

func TestConfigValues(t *testing.T) {
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", "/tmp/c")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_CACHE", "/tmp/nc")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_OUTPUT", "/tmp/o")
	c := config.ForTest()
	if c == nil {
		t.Fatalf("config.ForTest() nil")
	}
	if c.Path.UploadFolder == "" {
		t.Fatalf("Path.UploadFolder empty")
	}
	if c.MaxUploadSize <= 0 {
		t.Fatalf("MaxUploadSize not positive: %d", c.MaxUploadSize)
	}
}

func TestAccessors(t *testing.T) {
	a := New("/x", 99, nil, nil)
	if a.MaxFileSize() != 99 || a.MaxParts() != 2 || a.UploadDir() != "/x" {
		t.Fatalf("accessors mismatch")
	}
}

func TestSavePartOpenError(t *testing.T) {
	c := newClient(t.TempDir(), 50)
	c.openFile = func(string) (*os.File, error) { return nil, os.ErrNotExist }
	h := &harness{}
	// A valid one-file multipart drives savePart; openFile fails -> 500.
	ct, body := makeBody(t, 0, "payload")
	h.runReq(c, ct, body)
	if h.rec.Code != 500 {
		t.Fatalf("open error: expected 500 got %d", h.rec.Code)
	}
	if h.next {
		t.Fatalf("next called on open error")
	}
}

func TestFileExtEdge(t *testing.T) {
	cases := map[string]string{
		"noext":   "",
		".hidden": "",
		"a.b.c":   ".c",
		"a.b":     ".b",
		".":       "",
	}
	for in, want := range cases {
		if got := fileExt(in); got != want {
			t.Fatalf("fileExt(%s) = %q want %q", in, got, want)
		}
	}
}

func TestRandomName(t *testing.T) {
	m := New("/tmp", 0, nil, nil)
	if name := m.randomName("foo.tex"); !strings.HasSuffix(name, ".tex") || len(name) != 20 {
		t.Fatalf("ext name: %q", name)
	}
	if name := m.randomName("noext"); len(name) != 16 {
		t.Fatalf("noext name len: %d (got %q)", len(name), name)
	}
	// fallback path: randBytes returns nil bytes => deterministic 'a's.
	m2 := New("/tmp", 0, nil, nil)
	m2.randBytes = func(int) ([]byte, error) { return nil, nil }
	name := m2.randomName("foo.tex")
	if !strings.HasSuffix(name, ".tex") || len(name) != 20 {
		t.Fatalf("fallback name: %q", name)
	}
	if name[:3] != "aaa" {
		t.Fatalf("fallback should be a's: %q", name)
	}
}

func newClient(dir string, maxSize int64) *Middleware {
	return New(dir, maxSize, nil, nil)
}
