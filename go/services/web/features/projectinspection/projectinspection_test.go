package projectinspection

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"ollitex/go/services/web/core"

	"go.mongodb.org/mongo-driver/v2/bson"
)

const (
	testPID   = "aaaaaaaaaaaaaaaaaaaaaaaa"
	testUID   = "bbbbbbbbbbbbbbbbbbbbbbbb"
	testDocID = "cccccccccccccccccccccccc"
)

// newTestSvc builds the feature with pure in-memory seams (no mongo, no
// network, no node).
func hexOID(t *testing.T, h string) bson.ObjectID {
	o, err := bson.ObjectIDFromHex(h)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func newTestSvc(t *testing.T,
	docLines []string,
	bibErr error,
	bibBody []byte,
	workerOut func(snapshot []byte) (string, error),
) *svc {
	a := &core.App{}
	s := newSvc(a) // wires the defaults — override every seam:
	s.loadProject = func(ctx context.Context, pid string) (*bson.D, bool, error) {
		if pid != testPID {
			return nil, false, nil
		}
		proj := bson.D{
			{Key: "_id", Value: hexOID(t, testPID)},
			{Key: "owner_ref", Value: testUID},
			{Key: "publicAccesLevel", Value: "private"},
			{Key: "overleaf", Value: bson.D{
				{Key: "history", Value: bson.D{
					{Key: "id", Value: hexOID(t, "dddddddddddddddddddddddd")},
				}},
			}},
			{Key: "rootFolder", Value: bson.A{bson.D{
				{Key: "name", Value: "/"},
				{Key: "docs", Value: bson.A{bson.D{
					{Key: "_id", Value: hexOID(t, testDocID)},
					{Key: "name", Value: "main.tex"},
				}}},
				{Key: "fileRefs", Value: bson.A{bson.D{
					{Key: "_id", Value: hexOID(t, "eeeeeeeeeeeeeeeeeeeeeeee")},
					{Key: "name", Value: "refs.bib"},
					{Key: "hash", Value: "cafe"},
				}}},
				{Key: "folders", Value: bson.A{}},
			}}},
		}
		return &proj, true, nil
	}
	s.fetchDoc = func(ctx context.Context, pid, docID string) (string, error) {
		if docID != testDocID {
			return "", nil
		}
		return strings.Join(docLines, "\n"), nil
	}
	s.fetchBlob = func(ctx context.Context, hid, hash string) ([]byte, error) {
		return bibBody, bibErr
	}
	s.runWorker = func(ctx context.Context, snapshot []byte) ([]byte, error) {
		body, err := workerOut(snapshot)
		if err != nil {
			return nil, err
		}
		return []byte(body), nil
	}
	return s
}

func testSession(uid string) *core.Session {
	return &core.Session{
		SessID: "testsess",
		Doc: map[string]json.RawMessage{
			"passport": json.RawMessage(`{"user":{"email":"t@x","_id":"` + uid + `"}}`),
		},
	}
}

func doPost(t *testing.T, s *svc, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/project/"+testPID+"/project-inspection/analyze",
		strings.NewReader(body))
	req.Header.Set("Accept", "application/json")
	cxt := &core.Cxt{Req: req, Params: map[string]string{"1": testPID}, Sess: testSession(testUID)}
	res := &core.Res{}
	w := httptest.NewRecorder()
	res.W = w
	s.analyze(cxt, res)
	return w
}

func successWorker(t *testing.T, verifySnap func([]byte)) func([]byte) (string, error) {
	return func(snapshot []byte) (string, error) {
		var sm struct {
			Docs []struct {
				ID      string `json:"id"`
				Path    string `json:"path"`
				Content string `json:"content"`
			} `json:"documents"`
			Bibs []map[string]any `json:"binaryBibliographies"`
			IDs  []string         `json:"entryPointIds"`
		}
		if json.Unmarshal(snapshot, &sm) != nil {
			t.Fatalf("snapshot not JSON: %v", t.TempDir())
		}
		if sm.Docs[0].ID != testDocID || sm.Docs[0].Path != "main.tex" {
			t.Fatalf("snapshot docs = %+v", sm.Docs)
		}
		if len(sm.Bibs) != 1 || sm.Bibs[0]["path"] != "refs.bib" {
			t.Fatalf("snapshot bibs = %+v", sm.Bibs)
		}
		if verifySnap != nil {
			verifySnap(snapshot)
		}
		return `{"ok":true,"result":{"entryPoints":["main.tex"],"overview":{"fileCount":1}}}`, nil
	}
}

func TestAnalyzeSuccessEnvelope(t *testing.T) {
	s := newTestSvc(t, []string{"\\documentclass{article}", "\\begin{document}x\\end{document}"},
		nil, nil, successWorker(t, nil))
	w := doPost(t, s, `{"entryPointIds":["`+testDocID+`"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var env map[string]any
	if json.Unmarshal(w.Body.Bytes(), &env) != nil {
		t.Fatalf("not JSON")
	}
	for _, k := range []string{"schemaVersion", "analysisId", "analyzedAt", "entryPoints", "overview"} {
		if _, ok := env[k]; !ok {
			t.Errorf("envelope missing %q (have %v)", k, mapKeys(env))
		}
	}
	if env["schemaVersion"].(float64) != 1 {
		t.Errorf("schemaVersion = %v", env["schemaVersion"])
	}
}

func mapKeys(m map[string]any) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestAnalyzeSnapshotCarriesBibContent(t *testing.T) {
	s := newTestSvc(t, []string{"a"},
		nil, []byte("@article{k,v=1}"),
		successWorker(t, func(snap []byte) {
			var m map[string]any
			if json.Unmarshal(snap, &m) != nil {
				return
			}
			bibs := m["binaryBibliographies"].([]any)
			first := bibs[0].(map[string]any)
			if first["content"] != "@article{k,v=1}" {
				t.Errorf("bib content = %v", first["content"])
			}
		}),
	)
	w := doPost(t, s, `{"entryPointIds":["`+testDocID+`"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestAnalyzeMissingBibIsRecordedNotFatal(t *testing.T) {
	s := newTestSvc(t, []string{"a"}, errMissingBlob, nil, successWorker(t, nil))
	w := doPost(t, s, `{"entryPointIds":["`+testDocID+`"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestAnalyzeInvalidEntryPointID(t *testing.T) {
	s := newTestSvc(t, []string{"a"}, nil, nil, successWorker(t, nil))
	w := doPost(t, s, `{"entryPointIds":["ffffffffffffffffffffffff"]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "INVALID_ENTRY_POINT") {
		t.Fatalf("body = %s", w.Body.String())
	}
}

func TestAnalyzeInvalidEntryPointType(t *testing.T) {
	// point the (valid doc) entry at the .bib file id → not compilable
	s := newTestSvc(t, []string{"a"}, nil, nil,
		func([]byte) (string, error) { t.Fatal("worker must not run"); return "", nil })
	w := doPost(t, s, `{"entryPointIds":["eeeeeeeeeeeeeeeeeeeeeeee"]}`)
	if w.Code != http.StatusBadRequest ||
		!strings.Contains(w.Body.String(), "INVALID_ENTRY_POINT") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAnalyzeEntryCountBounds(t *testing.T) {
	s := newTestSvc(t, []string{"a"}, nil, nil,
		func([]byte) (string, error) { t.Fatal("worker must not run"); return "", nil })
	w := doPost(t, s, `{"entryPointIds":[]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty: status = %d", w.Code)
	}
	ids := `["` + testDocID + `"`
	for i := 0; i < 50; i++ {
		ids += `,"5555"`
	}
	w = doPost(t, s, `{"entryPointIds":`+ids+`}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("51 ids: status = %d", w.Code)
	}
}

func TestAnalyzeWorkerFailureCodes(t *testing.T) {
	cases := []struct {
		name   string
		outFn  func([]byte) (string, error)
		status int
		code   string
	}{
		{
			"worker-reported INVALID_ENTRY_POINT",
			func([]byte) (string, error) {
				return `{"ok":false,"error":{"code":"INVALID_ENTRY_POINT","message":"bad entry"}}`, nil
			},
			500, "INVALID_ENTRY_POINT",
		},
		{
			"worker-reported PROJECT_TOO_LARGE",
			func([]byte) (string, error) {
				return `{"ok":false,"error":{"code":"PROJECT_TOO_LARGE","message":"big"}}`, nil
			},
			500, "PROJECT_TOO_LARGE",
		},
		{
			"timeout",
			func([]byte) (string, error) { return "", errTimeout },
			504, "ANALYSIS_TIMEOUT",
		},
		{
			"cancel",
			func([]byte) (string, error) { return "", errCancelled },
			499, "ANALYSIS_CANCELLED",
		},
		{
			"internal",
			func([]byte) (string, error) { return "", errInternal("boom") },
			500, "PROJECT_INSPECTION_ERROR",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newTestSvc(t, []string{"a"}, nil, nil, c.outFn)
			w := doPost(t, s, `{"entryPointIds":["`+testDocID+`"]}`)
			if w.Code != c.status {
				t.Fatalf("status = %d want %d body=%s", w.Code, c.status, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), `"error":"`+c.code+`"`) {
				t.Fatalf("body = %s", w.Body.String())
			}
		})
	}
}

func TestRouteTable(t *testing.T) {
	f := Feature(nil)
	if f.Name != "project-inspection" {
		t.Fatalf("name = %q", f.Name)
	}
	if len(f.Routes) != 1 {
		t.Fatalf("routes = %d", len(f.Routes))
	}
	r := f.Routes[0]
	if r.Method != http.MethodPost || r.Pattern == nil || r.Handler == nil {
		t.Fatalf("route = %+v", r)
	}
	for path, want := range map[string]bool{
		"/project/" + testPID + "/project-inspection/analyze":       true,
		"/project/" + testPID + "/PROJECT-INSPECTION/ANALYZE":       true, // (?i:)
		"/project/" + testPID + "/project-inspection/analyze/":      true, // /?
		"/project/" + testPID + "/project-inspection/analyze/extra": false,
		"/project/bad-id/project-inspection/analyze":                false,
	} {
		if got := r.Pattern.String() != "" && analyzePat.MatchString(path); got != want {
			t.Errorf("%q: match=%v want %v", path, got, want)
		}
	}
}

func TestConfigDefaults(t *testing.T) {
	os.Unsetenv("PROJECT_INSPECTION_NODE_BIN")
	os.Unsetenv("PROJECT_INSPECTION_WORKER")
	os.Unsetenv("PROJECT_INSPECTION_TIMEOUT_MS")
	c := cfgFromEnv()
	if c.NodeBin != "node" || c.Worker == "" || c.Timeout.Seconds() != 30 {
		t.Fatalf("cfg = %+v", c)
	}
	if c.MaxSource != 25<<20 || c.MaxBib != 6<<20 || c.MaxTotalBib != 25<<20 {
		t.Fatalf("bounds = %+v", c)
	}
}

// TestPiWorkerPathPrecedence — the worker bundle is the analyze endpoint's
// ONLY dependency (the de-shipping wave removed it from the image once;
// the endpoint then died on every request). Pin the resolution contract:
// env override wins; otherwise the current-image path (frontend/ hosts the
// module) is the fallback so a fresh bake always resolves.
func TestPiWorkerPathPrecedence(t *testing.T) {
	t.Setenv("PROJECT_INSPECTION_WORKER", "/custom/work.cjs")
	if got := piWorkerPath(); got != "/custom/work.cjs" {
		t.Fatalf("env override ignored: %q", got)
	}
	os.Unsetenv("PROJECT_INSPECTION_WORKER")
	got := piWorkerPath()
	for _, p := range []string{
		"/overleaf/custom",
		"/overleaf/junk/services-web",
	} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			t.Fatalf("unexpected worker present in the test env: %q", p)
		}
	}
	want := "/overleaf/frontend/modules/project-inspection/dist/analyze-worker.cjs"
	if got != want {
		t.Fatalf("fallback = %q; want the current-image path %q (a bake without the bundle MUST resolve to the spot the image actually ships it)", got, want)
	}
}

// TestWorkerStderrSurfaces — a worker that fails with a stderr message must
// put that message into the returned error (the live 500 "Project
// inspection failed" with zero diagnostics was the discarded-stderr
// regression; a silent failure is no longer silently passed on).
func TestWorkerStderrSurfaces(t *testing.T) {
	d := t.TempDir()
	fake := d + "/fakenode"
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho 'lezer: panic in parse loop (test signal XYZ123)' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := &core.App{}
	s := newSvc(a)
	s.cfg.NodeBin = fake
	s.cfg.Worker = d + "/worker.cjs"
	s.cfg.Timeout = 10 * time.Second
	out, werr := s.runWorkerDefault(context.Background(), []byte(`{}`))
	if out != nil || werr == nil {
		t.Fatalf("out=%v err=%v — want an error", out, werr)
	}
	if !strings.Contains(werr.Error(), "XYZ123") {
		t.Fatalf("stderr not surfaced in the error: %v", werr)
	}
	if !strings.Contains(werr.Error(), "worker exited") {
		t.Fatalf("error should name the worker failure: %v", werr)
	}
}

// TestAnalyzeRealWorker — end-to-end against the ACTUAL bundled worker
// (default seam = spawn `node dist/analyze-worker.cjs`). Integration test:
// proves the Go reader → worker exec → envelope pipeline with the real JS
// engine.
func TestAnalyzeRealWorker(t *testing.T) {
	worker := os.Getenv("PROJECT_TEST_WORKER")
	if worker == "" {
		// the bundle ships with the UI module (frontend/ hosts the module;
		// the image COPYs it under /overleaf/frontend/...).
		worker = "../../../../../frontend/modules/project-inspection/dist/analyze-worker.cjs"
	}
	if _, err := os.Stat(worker); err != nil {
		t.Skipf("bundle not built (run the module vitest or the webpack worker build): %v", err)
	}
	a := &core.App{}
	s := newSvc(a)
	s.cfg.Worker = worker
	s.cfg.NodeBin = "node"
	s.runWorker = s.runWorkerDefault // explicitly the default exec seam
	s.loadProject = newTestSvc(t, nil, nil, nil, nil).loadProject
	s.fetchDoc = newTestSvc(t, nil, nil, nil, nil).fetchDoc
	s.fetchBlob = newTestSvc(t, nil, nil, nil, nil).fetchBlob
	w := doPost(t, s, `{"entryPointIds":["`+testDocID+`"]}`)
	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, body)
	}
	for _, k := range []string{"schemaVersion", "analysisId", "analyzedAt", "entryPoints", "overview", "graph", "coverage"} {
		if !strings.Contains(body, `"`+k+`"`) {
			t.Errorf("real-worker response missing %q (body=%s)", k, body)
		}
	}
	if !strings.Contains(body, `"entryPoints":[{"id":"`+testDocID+`","path":"main.tex"}]`) {
		t.Errorf("engine did not resolve main.tex as entry: %s", body)
	}
	if !strings.Contains(body, `"path":"main.tex"`) {
		t.Errorf("main.tex missing from result: %s", body)
	}
}
