package conversioncontroller

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clserrors "ollitex/go/services/clsitex/errors"
	histwriter "ollitex/go/services/clsitex/historyresourcewriter"
	"ollitex/go/services/clsitex/requestparser"
	"ollitex/go/services/clsitex/resourcewriter"
)

// --- fake ConversionManager ---------------------------------------------------

type fakeManager struct {
	toLaTeX func(id, path, typ string) (string, error)
	toDoc   func(id, compileDir, root, typ string) (string, error)
	toJpeg  func(id, path, mode string) (string, error)
	hrwErr  error
	rwErr   error
}

func (f *fakeManager) ConvertToLaTeXWithLock(id, path, typ string) (string, error) {
	if f.toLaTeX != nil {
		return f.toLaTeX(id, path, typ)
	}
	return "", nil
}

func (f *fakeManager) ConvertLaTeXToDocumentInDirWithLock(id, compileDir, root, typ string) (string, error) {
	if f.toDoc != nil {
		return f.toDoc(id, compileDir, root, typ)
	}
	return "", nil
}

func (f *fakeManager) ConvertPDFToJPEGWithLock(id, path, mode string) (string, error) {
	if f.toJpeg != nil {
		return f.toJpeg(id, path, mode)
	}
	return "", nil
}

func newTestController(t *testing.T) (*Controller, *fakeManager) {
	t.Helper()
	fm := &fakeManager{}
	c := &Controller{
		Manager:         fm,
		Config:          Settings{EnablePandocConversions: true, EnablePdfConversions: true},
		NewUUID:         func() string { return "test-uuid-000" },
		GenerateBuildId: func() string { return "test-build-id" },
	}
	return c, fm
}

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "upload")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func uploadFile(t *testing.T, content string) *UploadedFile {
	t.Helper()
	return &UploadedFile{Path: writeFile(t, content)}
}

func mustJSON(t *testing.T, body []byte) map[string]any {
	t.Helper()
	m := map[string]any{}
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("body not JSON: %v %s", err, string(body))
	}
	return m
}

func validCompileBody() map[string]any {
	return map[string]any{
		"compile": map[string]any{
			"options": map[string]any{"metricsPath": "test"},
			"resources": []any{
				map[string]any{"path": "main.tex", "content": "\\documentclass{article}\\end{document}"},
			},
		},
	}
}

func historyBody() map[string]any {
	body := validCompileBody()
	compile := body["compile"].(map[string]any)
	compile["baseHistoryVersion"] = float64(7)
	compile["rawChangeOperations"] = []any{[]any{
		map[string]any{"op": map[string]any{"type": "insert", "data": "x"}},
	}}
	compile["globalBlobs"] = []any{"blobA"}
	return body
}

// --- convertDocxToLaTeX / convertDocumentToLaTeX ------------------------------

func TestConvertDocxToLaTeXDisabled(t *testing.T) {
	c, _ := newTestController(t)
	c.Config.EnablePandocConversions = false
	path := writeFile(t, "doc")
	rec := httptest.NewRecorder()
	code, err := c.ConvertDocxToLaTeX(rec, &UploadedFile{Path: path})
	if err != nil || code != http.StatusNotFound {
		t.Fatalf("want 404 nil, got %d %v", code, err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("upload should be unlinked")
	}
}

func TestConvertDocxToLaTeXStreamsSuccess(t *testing.T) {
	c, fm := newTestController(t)
	fm.toLaTeX = func(id, path, typ string) (string, error) {
		if typ != "docx" {
			t.Fatalf("type %q", typ)
		}
		return writeFile(t, "zip-bytes"), nil
	}
	path := writeFile(t, "doc")
	rec := httptest.NewRecorder()
	code, err := c.ConvertDocxToLaTeX(rec, &UploadedFile{Path: path})
	if err != nil || code != 200 {
		t.Fatalf("want 200 nil, got %d %v", code, err)
	}
	if !strings.Contains(rec.Body.String(), "zip-bytes") {
		t.Fatalf("body: %s", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Disposition"); ct != "attachment; filename=conversion.zip" {
		t.Fatalf("disposition: %q", ct)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("nosniff missing")
	}
}

func TestConvertDocxToLaTeX422UserFacing(t *testing.T) {
	c, fm := newTestController(t)
	fm.toLaTeX = func(id, path, typ string) (string, error) {
		return "", clserrors.NewConversionErrorT("boom", typ, "stderr-64", 64)
	}
	path := writeFile(t, "doc")
	rec := httptest.NewRecorder()
	code, err := c.ConvertDocxToLaTeX(rec, &UploadedFile{Path: path})
	if err != nil || code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422 nil, got %d %v", code, err)
	}
	m := mustJSON(t, rec.Body.Bytes())
	if m["error"] != "stderr-64" || m["exitCode"] != float64(64) {
		t.Fatalf("422 body: %v", m)
	}
}

func TestConvertDocumentToLaTeXDisabled(t *testing.T) {
	c, _ := newTestController(t)
	c.Config.EnablePandocConversions = false
	path := writeFile(t, "doc")
	rec := httptest.NewRecorder()
	code, err := c.ConvertDocumentToLaTeX(rec, &UploadedFile{Path: path}, TypeQuery{Type: "markdown"})
	if err != nil || code != http.StatusNotFound {
		t.Fatalf("want 404 nil, got %d %v", code, err)
	}
}

func TestConvertDocumentToLaTeXUnsupportedType(t *testing.T) {
	c, _ := newTestController(t)
	path := writeFile(t, "doc")
	rec := httptest.NewRecorder()
	_, err := c.ConvertDocumentToLaTeX(rec, &UploadedFile{Path: path}, TypeQuery{Type: "pdf"})
	if err == nil {
		t.Fatal("want returned error")
	}
	if _, perr := os.Stat(path); !os.IsNotExist(perr) {
		t.Fatal("upload should be unlinked on schema failure")
	}
}

func TestConvertDocumentToLaTeX422UserFacing(t *testing.T) {
	c, fm := newTestController(t)
	fm.toLaTeX = func(id, path, typ string) (string, error) {
		return "", clserrors.NewConversionErrorT("boom", typ, "stderr-64", 64)
	}
	path := writeFile(t, "doc")
	rec := httptest.NewRecorder()
	_, err := c.ConvertDocumentToLaTeX(rec, &UploadedFile{Path: path}, TypeQuery{Type: "markdown"})
	if err != nil {
		t.Fatalf("error should be swallowed: %v", err)
	}
	m := mustJSON(t, rec.Body.Bytes())
	if m["error"] != "stderr-64" || m["exitCode"] != float64(64) {
		t.Fatalf("422 body: %v", m)
	}
}

func TestConvertDocumentToLaTeX422NotUserFacing(t *testing.T) {
	c, fm := newTestController(t)
	fm.toLaTeX = func(id, path, typ string) (string, error) {
		return "", clserrors.NewConversionErrorT("boom", typ, "hidden-stderr", 137)
	}
	path := writeFile(t, "doc")
	rec := httptest.NewRecorder()
	rec.Code = 0
	_, _ = c.ConvertDocumentToLaTeX(rec, &UploadedFile{Path: path}, TypeQuery{Type: "markdown"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d", rec.Code)
	}
	m := mustJSON(t, rec.Body.Bytes())
	if m["error"] != nil || m["exitCode"] != nil {
		t.Fatalf("body should be empty: %v", m)
	}
	// stderr logged, not sent — can't assert logs here; covered.
}

func TestConvertDocumentToLaTeXReturnedError(t *testing.T) {
	c, fm := newTestController(t)
	fm.toLaTeX = func(id, path, typ string) (string, error) {
		return "", stderrors.New("boom")
	}
	path := writeFile(t, "doc")
	rec := httptest.NewRecorder()
	_, err := c.ConvertDocumentToLaTeX(rec, &UploadedFile{Path: path}, TypeQuery{Type: "markdown"})
	if err == nil {
		t.Fatal("want returned error")
	}
}

// --- convertPDFToJPEG ---------------------------------------------------------

func TestConvertPDFToJPEGDisabled(t *testing.T) {
	c, _ := newTestController(t)
	c.Config.EnablePdfConversions = false
	path := writeFile(t, "pdf")
	rec := httptest.NewRecorder()
	_, err := c.ConvertPDFToJPEG(rec, &UploadedFile{Path: path}, PDFQuery{Mode: "preview"})
	if err != nil {
		t.Fatalf("404 should be written+swallowed: %v", err)
	}
	if _, perr := os.Stat(path); !os.IsNotExist(perr) {
		t.Fatal("upload should be unlinked")
	}
}

func TestConvertPDFToJPEGUnsupportedMode(t *testing.T) {
	c, _ := newTestController(t)
	path := writeFile(t, "pdf")
	rec := httptest.NewRecorder()
	_, err := c.ConvertPDFToJPEG(rec, &UploadedFile{Path: path}, PDFQuery{Mode: "preview2"})
	if err == nil {
		t.Fatal("want returned error")
	}
	if _, perr := os.Stat(path); !os.IsNotExist(perr) {
		t.Fatal("upload should be unlinked")
	}
}

func TestConvertPDFToJPEGStreamsSuccess(t *testing.T) {
	c, fm := newTestController(t)
	fm.toJpeg = func(id, path, mode string) (string, error) {
		if mode != "preview" {
			t.Fatalf("mode %q", mode)
		}
		return writeFile(t, "jpeg-bytes"), nil
	}
	path := writeFile(t, "pdf")
	rec := httptest.NewRecorder()
	code, err := c.ConvertPDFToJPEG(rec, &UploadedFile{Path: path}, PDFQuery{Mode: "preview"})
	if err != nil || code != 200 {
		t.Fatalf("want 200 nil, got %d %v", code, err)
	}
	if !strings.Contains(rec.Body.String(), "jpeg-bytes") {
		t.Fatalf("body: %s", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Disposition"); ct != "attachment; filename=output.jpg" {
		t.Fatalf("disposition: %q", ct)
	}
}

func TestConvertPDFToJPEGErrorReturned(t *testing.T) {
	c, fm := newTestController(t)
	fm.toJpeg = func(id, path, mode string) (string, error) {
		return "", stderrors.New("pdftocairo died")
	}
	path := writeFile(t, "pdf")
	rec := httptest.NewRecorder()
	_, err := c.ConvertPDFToJPEG(rec, &UploadedFile{Path: path}, PDFQuery{Mode: "thumbnail"})
	if err == nil {
		t.Fatal("Node: no ConversionError catch -> error forwarded (500)")
	}
}

// --- convertProjectToDocument --------------------------------------------------

func TestConvertProjectToDocumentDisabled(t *testing.T) {
	c, _ := newTestController(t)
	c.Config.EnablePandocConversions = false
	rec := httptest.NewRecorder()
	_, err := c.ConvertProjectToDocument(rec, ProjectUser{"p1", "u1"},
		ProjectToDocumentQuery{Type: "docx"}, validCompileBody())
	if err != nil {
		t.Fatalf("404 should be written+swallowed: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestConvertProjectToDocumentBadType(t *testing.T) {
	c, _ := newTestController(t)
	rec := httptest.NewRecorder()
	_, err := c.ConvertProjectToDocument(rec, ProjectUser{"p1", "u1"},
		ProjectToDocumentQuery{Type: "rtf"}, validCompileBody())
	if err == nil {
		t.Fatal("want returned error for unsupported type")
	}
}

func TestConvertProjectToDocumentBadResponseFormat(t *testing.T) {
	c, _ := newTestController(t)
	rec := httptest.NewRecorder()
	_, err := c.ConvertProjectToDocument(rec, ProjectUser{"p1", "u1"},
		ProjectToDocumentQuery{Type: "docx", ResponseFormat: "pdf"}, validCompileBody())
	if err == nil {
		t.Fatal("want returned error for bad responseFormat")
	}
}

func TestConvertProjectToDocumentParseError(t *testing.T) {
	c, _ := newTestController(t)
	rec := httptest.NewRecorder()
	_, err := c.ConvertProjectToDocument(rec, ProjectUser{"p1", "u1"},
		ProjectToDocumentQuery{Type: "docx"}, map[string]any{"no-compile": true})
	if err == nil {
		t.Fatal("want parse error forwarded")
	}
}

// --- convertProjectToDocument (full flow) ------------------------------------

// p2dController wires Config to a real tmp layout and injects capture seams.
// capState captures injected-seam traffic.
type capState struct {
	lastCleanup  string
	hrwReq       *histwriter.Request
	rwReq        *resourcewriter.Request
	metricsCalls int
}

func newP2D(t *testing.T) (*Controller, *fakeManager, *capState) {
	t.Helper()
	fm := &fakeManager{}
	base := t.TempDir()
	cs := &capState{}
	c := &Controller{
		Manager: fm,
		Config: Settings{
			CompilesDir:             filepath.Join(base, "compiles"),
			ClsiCacheDir:            filepath.Join(base, "cache"),
			OutputDir:               filepath.Join(base, "output"),
			EnablePandocConversions: true,
		},
		NewUUID:         func() string { return "conv-uuid-7" },
		GenerateBuildId: func() string { return "build-id-8" },
		MetricsInc:      func(compileFromHistory bool, method string) { cs.metricsCalls++ },
		ScheduleOutputCleanup: func(outputDir, conversionID string) {
			cs.lastCleanup = outputDir + "/" + conversionID
		},
		HistorySync: func(ctx context.Context, projectID, userID string,
			req *histwriter.Request, compileDir string,
			timings, stats map[string]any) (*histwriter.Result, error) {
			cs.hrwReq = req
			if fm.hrwErr != nil {
				return nil, fm.hrwErr
			}
			return &histwriter.Result{BaseHistoryVersion: 1}, nil
		},
		ResourceSync: func(req *resourcewriter.Request, basePath string) ([]resourcewriter.Resource, error) {
			cs.rwReq = req
			if fm.rwErr != nil {
				return nil, fm.rwErr
			}
			return []resourcewriter.Resource{}, nil
		},
	}
	return c, fm, cs
}

func TestP2DStreamSuccessRW(t *testing.T) {
	c, fm, cs := newP2D(t)
	fm.toDoc = func(id, compileDir, root, typ string) (string, error) {
		if id != "conv-uuid-7" || typ != "docx" {
			t.Fatalf("id %q type %q", id, typ)
		}
		p := filepath.Join(t.TempDir(), "doc.docx")
		if err := os.WriteFile(p, []byte("doc-bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p, nil
	}
	rec := httptest.NewRecorder()
	code, err := c.ConvertProjectToDocument(rec, ProjectUser{"p1", "u1"},
		ProjectToDocumentQuery{Type: "docx"}, validCompileBody())
	if err != nil || code != 200 {
		t.Fatalf("want 200 nil, got %d %v", code, err)
	}
	if !strings.Contains(rec.Body.String(), "doc-bytes") {
		t.Fatalf("body: %s", rec.Body.String())
	}
	// capture wiring
	if cs.rwReq == nil {
		t.Fatal("RW request not captured")
	}
	if cs.rwReq.ProjectID != "p1" || cs.rwReq.UserID != "u1" {
		t.Fatalf("rw req: %+v", cs.rwReq)
	}
	if len(cs.rwReq.Resources) != 1 || cs.rwReq.Resources[0].Path != "main.tex" {
		t.Fatalf("resources: %+v", cs.rwReq.Resources)
	}
	if cs.lastCleanup != "" {
		t.Fatalf("stream branch must not schedule cleanup: %q", cs.lastCleanup)
	}
	// cleanup: conversionDir + conversionCacheDir removed; projectCacheDir
	// kept (Node divergence: the created-check is always falsy).
	compilesDir := c.Config.CompilesDir
	if _, err := os.Stat(filepath.Join(compilesDir, "conv-uuid-7")); !os.IsNotExist(err) {
		t.Fatal("conversionDir should be cleaned")
	}
	if _, err := os.Stat(filepath.Join(c.Config.ClsiCacheDir, "p1")); err != nil {
		t.Fatal("projectCacheDir should remain (Node divergence)")
	}
}

func TestP2DJSONSuccess(t *testing.T) {
	c, fm, cs := newP2D(t)
	fm.toDoc = func(id, compileDir, root, typ string) (string, error) {
		p := filepath.Join(t.TempDir(), "doc.docx")
		if err := os.WriteFile(p, []byte("staged-doc"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p, nil
	}
	rec := httptest.NewRecorder()
	code, err := c.ConvertProjectToDocument(rec, ProjectUser{"p1", "u1"},
		ProjectToDocumentQuery{Type: "markdown", ResponseFormat: "json"}, validCompileBody())
	if err != nil || code != http.StatusOK {
		t.Fatalf("want 200 nil, got %d %v", code, err)
	}
	m := mustJSON(t, rec.Body.Bytes())
	if m["conversionId"] != "conv-uuid-7" || m["buildId"] != "build-id-8" {
		t.Fatalf("json: %v", m)
	}
	if m["file"] != "output.zip" {
		t.Fatalf("file: %v", m["file"])
	}
	// staged copy
	staged := filepath.Join(c.Config.OutputDir, "conv-uuid-7", "generated-files", "build-id-8", "output.zip")
	data, rerr := os.ReadFile(staged)
	if rerr != nil || string(data) != "staged-doc" {
		t.Fatalf("staged: %v %q", rerr, data)
	}
	if cs.lastCleanup != filepath.Join(c.Config.OutputDir, "conv-uuid-7") {
		t.Fatalf("cleanup not scheduled: %q", cs.lastCleanup)
	}
}

func TestP2DJSONCopyError(t *testing.T) {
	c, fm, _ := newP2D(t)
	fm.toDoc = func(id, compileDir, root, typ string) (string, error) {
		return "/nonexistent/doc.docx", nil // copy will fail
	}
	rec := httptest.NewRecorder()
	_, err := c.ConvertProjectToDocument(rec, ProjectUser{"p1", "u1"},
		ProjectToDocumentQuery{Type: "markdown", ResponseFormat: "json"}, validCompileBody())
	if err == nil {
		t.Fatal("want copied error forwarded")
	}
}

func TestP2DHistorySyncSuccess(t *testing.T) {
	c, fm, cs := newP2D(t)
	fm.toDoc = func(id, compileDir, root, typ string) (string, error) {
		p := filepath.Join(t.TempDir(), "doc.docx")
		if err := os.WriteFile(p, []byte("hrw-doc"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p, nil
	}
	rec := httptest.NewRecorder()
	code, err := c.ConvertProjectToDocument(rec, ProjectUser{"p1", "u1"},
		ProjectToDocumentQuery{Type: "docx"}, historyBody())
	if err != nil || code != 200 {
		t.Fatalf("want 200 nil, got %d %v", code, err)
	}
	if !strings.Contains(rec.Body.String(), "hrw-doc") {
		t.Fatalf("body: %s", rec.Body.String())
	}
	if cs.hrwReq == nil {
		t.Fatal("HRW request not captured")
	}
	if cs.hrwReq.BaseHistoryVersion != 7 {
		t.Fatalf("baseHistoryVersion: %+v", cs.hrwReq)
	}
	if len(cs.hrwReq.GlobalBlobs) != 1 || cs.hrwReq.GlobalBlobs[0] != "blobA" {
		t.Fatalf("blobs: %+v", cs.hrwReq.GlobalBlobs)
	}
	if cs.hrwReq.Png2pdf {
		t.Fatal("Png2pdf must be forced false")
	}
	if cs.hrwReq.MetricsPath != "" {
		t.Fatalf("metricsPath: %q", cs.hrwReq.MetricsPath)
	}
	if len(cs.hrwReq.RawChangeOperations) != 1 {
		t.Fatalf("ops: %+v", cs.hrwReq.RawChangeOperations)
	}
}

func TestP2DHistoryMissingUpdates409(t *testing.T) {
	c, fm, _ := newP2D(t)
	fm.hrwErr = clserrors.NewMissingUpdatesError("missing updates",
		map[string]any{"baseHistoryVersion": 42})
	rec := httptest.NewRecorder()
	code, err := c.ConvertProjectToDocument(rec, ProjectUser{"p1", "u1"},
		ProjectToDocumentQuery{Type: "docx"}, historyBody())
	if err != nil || code != http.StatusConflict {
		t.Fatalf("want 409 nil, got %d %v", code, err)
	}
	m := mustJSON(t, rec.Body.Bytes())
	if m["baseHistoryVersion"] != float64(42) {
		t.Fatalf("409 body: %v", m)
	}
}

func TestP2DHistoryOtherError(t *testing.T) {
	c, fm, _ := newP2D(t)
	fm.hrwErr = stderrors.New("history failure")
	rec := httptest.NewRecorder()
	_, err := c.ConvertProjectToDocument(rec, ProjectUser{"p1", "u1"},
		ProjectToDocumentQuery{Type: "docx"}, historyBody())
	if err == nil {
		t.Fatal("want forwarded error")
	}
}

func TestP2DResourceSyncError(t *testing.T) {
	c, fm, _ := newP2D(t)
	fm.rwErr = stderrors.New("resource failure")
	rec := httptest.NewRecorder()
	_, err := c.ConvertProjectToDocument(rec, ProjectUser{"p1", "u1"},
		ProjectToDocumentQuery{Type: "docx"}, validCompileBody())
	if err == nil {
		t.Fatal("want forwarded error")
	}
}

func TestP2DConversionErrorUserFacing(t *testing.T) {
	c, fm, _ := newP2D(t)
	fm.toDoc = func(id, compileDir, root, typ string) (string, error) {
		return "", clserrors.NewConversionErrorT("boom", typ, "stderr-97", 97)
	}
	rec := httptest.NewRecorder()
	code, err := c.ConvertProjectToDocument(rec, ProjectUser{"p1", "u1"},
		ProjectToDocumentQuery{Type: "html"}, validCompileBody())
	if err != nil || code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422 nil, got %d %v", code, err)
	}
	m := mustJSON(t, rec.Body.Bytes())
	if m["error"] != "stderr-97" || m["exitCode"] != float64(97) {
		t.Fatalf("422: %v", m)
	}
}

func TestP2DConversionErrorNotUserFacing(t *testing.T) {
	c, fm, _ := newP2D(t)
	fm.toDoc = func(id, compileDir, root, typ string) (string, error) {
		return "", clserrors.NewConversionErrorT("boom", typ, "secret", 137)
	}
	rec := httptest.NewRecorder()
	_, err := c.ConvertProjectToDocument(rec, ProjectUser{"p1", "u1"},
		ProjectToDocumentQuery{Type: "html"}, validCompileBody())
	if err != nil {
		t.Fatalf("422 should be swallowed: %v", err)
	}
	m := mustJSON(t, rec.Body.Bytes())
	if len(m) != 0 {
		t.Fatalf("422 body should be empty: %v", m)
	}
}

// --- narrows + new() + remaining helpers -------------------------------------

func TestNewDefaultSeams(t *testing.T) {
	c := New(&fakeManager{}, Settings{}, requestparser.Config{})
	if c.NewUUID == nil {
		t.Fatal("NewUUID nil")
	}
	if c.GenerateBuildId == nil {
		t.Fatal("GenerateBuildId nil")
	}
	if c.MetricsInc == nil {
		t.Fatal("MetricsInc nil")
	}
	if c.HistorySync == nil {
		t.Fatal("HistorySync nil")
	}
	if c.ResourceSync == nil {
		t.Fatal("ResourceSync nil")
	}
	if c.ScheduleOutputCleanup == nil {
		t.Fatal("ScheduleOutputCleanup nil")
	}
	// all should generate values
	if c.NewUUID() == "" {
		t.Fatal("NewUUID returned empty")
	}
	if c.GenerateBuildId() == "" {
		t.Fatal("GenerateBuildId returned empty")
	}
}

func TestSnapshotOfNil(t *testing.T) {
	if snapshotOf(nil) != nil {
		t.Fatal("nil -> nil")
	}
}

func TestSnapshotOfNonMap(t *testing.T) {
	if snapshotOf("string-value") != nil {
		t.Fatal("non-map -> nil")
	}
}

func TestSnapshotOfMap(t *testing.T) {
	m := map[string]any{"a": float64(1)}
	out := snapshotOf(m)
	if out == nil || out["a"] != float64(1) {
		t.Fatalf("round-trip: %v", out)
	}
}

func TestGlobalBlobsOfNil(t *testing.T) {
	if globalBlobsOf(nil) != nil {
		t.Fatal("nil -> nil")
	}
}

func TestGlobalBlobsOfNonSlice(t *testing.T) {
	if globalBlobsOf("nope") != nil {
		t.Fatal("non-slice -> nil")
	}
}

func TestGlobalBlobsOfSlice(t *testing.T) {
	out := globalBlobsOf([]any{"a", "b", "c"})
	if len(out) != 3 || out[0] != "a" || out[2] != "c" {
		t.Fatalf("slice round-trip: %v", out)
	}
}

func TestRawChangeOpsOfNil(t *testing.T) {
	if rawChangeOpsOf(nil) != nil {
		t.Fatal("nil -> nil")
	}
}

func TestRawChangeOpsOfNonSlice(t *testing.T) {
	if rawChangeOpsOf("nope") != nil {
		t.Fatal("non-slice -> nil")
	}
}

func TestRawChangeOpsOfNested(t *testing.T) {
	in := []any{
		[]any{map[string]any{"op": "x"}},
		[]any{map[string]any{"op": "y"}},
	}
	out := rawChangeOpsOf(in)
	if len(out) != 2 || out[0][0]["op"] != "x" || out[1][0]["op"] != "y" {
		t.Fatalf("nested: %v", out)
	}
}

func TestNum64(t *testing.T) {
	if num64(float64(5.0)) != 5 {
		t.Fatal("float64 -> int64")
	}
	if num64(7) != 7 {
		t.Fatal("int -> int64")
	}
	if num64(int64(9)) != 9 {
		t.Fatal("int64 -> int64")
	}
	if num64("not-a-number") != 0 {
		t.Fatal("string -> 0")
	}
}

func TestStrOf(t *testing.T) {
	if strOf("hello") != "hello" {
		t.Fatal("string case")
	}
	if strOf(42) != "" {
		t.Fatal("non-string -> empty")
	}
}

func TestModifiedTimeNil(t *testing.T) {
	if modifiedTime(nil) != nil {
		t.Fatal("nil -> nil")
	}
}

func TestModifiedTimeNonNumber(t *testing.T) {
	if modifiedTime("no") != nil {
		t.Fatal("non-number -> nil")
	}
}

func TestModifiedTimeFloat(t *testing.T) {
	tm := modifiedTime(float64(1000))
	if tm == nil {
		t.Fatal("float64 -> time")
	}
	if tm.UnixMilli() != 1000 {
		t.Fatalf("unix: %d", tm.UnixMilli())
	}
}

func TestCleanDirsNoOp(t *testing.T) {
	// cleanDirs on nonexistent dirs is safe
	cleanDirs([]string{"/nonexistent/path/that/doesnt/exist"})
}

func TestStreamDownloadNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	err := streamDownload(rec, "/nonexistent/no/such/file", "missing.zip")
	if err == nil {
		t.Fatal("want error")
	}
}

func TestStreamDownloadEmpty(t *testing.T) {
	rec := httptest.NewRecorder()
	f := t.TempDir() + "/empty.txt"
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	err := streamDownload(rec, f, "empty.txt")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rec.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("content-type: %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Content-Length") != "0" {
		t.Fatalf("content-length: %q", rec.Header().Get("Content-Length"))
	}
}
