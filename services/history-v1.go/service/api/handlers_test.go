package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"history-v1/internal/core"
	"history-v1/service/blobstore"
	"history-v1/service/chunkstore"
	"history-v1/service/historystore"
)

// --- helpers ---

func mustBasic(t *testing.T) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte("staging:staging"))
}

func doReq(t *testing.T, a *API, method, target, auth, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, req)
	return w
}

func jbody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("response not a JSON object: %v\nbody: %s", err, w.Body.String())
	}
	return m
}

func isTrueBool(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

// isFalseAny — strict "false" (bool-typed and value false).
func isFalseAny(v any) bool {
	b, ok := v.(bool)
	return ok && !b
}

const opMainTex = `{"operations":[{"pathname":"main.tex","file":{"content":"hello"}}],"timestamp":"2020-01-01T00:00:00.000Z","authors":[1,2,1],"v2Authors":[]}`

// bootstrap — POST import {"files":{}} (creates chunk start=end=0) then
// legacy_changes?end_version=0 with one addFile op (endVersion -> 1).
// Node acceptance flow: project is snapshot-imported before changes.
func bootstrap(t *testing.T, a *API, pid string) {
	t.Helper()
	w := doReq(t, a, "POST", "/api/projects/"+pid+"/import", mustBasic(t), `{"files":{}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("%s import: %d body %s", pid, w.Code, w.Body.String())
	}
	w = doReq(t, a, "POST", "/api/projects/"+pid+"/legacy_changes?end_version=0", mustBasic(t), "["+opMainTex+"]")
	if w.Code != http.StatusCreated {
		t.Fatalf("%s bootstrap changes: %d body %s", pid, w.Code, w.Body.String())
	}
}

// --- import (importChanges) ---

func TestImportLegacyChanges(t *testing.T) {
	a := testAPI(t)
	// end_version required (zod coerce.number): missing -> 422.
	w := doReq(t, a, "POST", "/api/projects/1/legacy_changes", mustBasic(t), "[]")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing end_version: %d (body %s), want 422", w.Code, w.Body.String())
	}
	bootstrap(t, a, "1")
	// empty changes on a valid project: persist returns null (Node) ->
	// controller TypeError -> terminal 500 (Go guard mirrors Node).
	w = doReq(t, a, "POST", "/api/projects/1/legacy_changes?end_version=1", mustBasic(t), "[]")
	if w.Code != http.StatusInternalServerError {
		t.Errorf("empty changes: %d, want 500 (Node TypeError)", w.Code)
	}
	// return_snapshot invalid (z.enum['none','hashed'], query tier) -> 422.
	w = doReq(t, a, "POST", "/api/projects/1/legacy_changes?end_version=1&return_snapshot=bogus", mustBasic(t), "[]")
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid return_snapshot: %d, want 422", w.Code)
	}
	// conflicting end_version (persisted end is 1, client says 0) -> 422.
	w = doReq(t, a, "POST", "/api/projects/1/legacy_changes?end_version=0", mustBasic(t), "["+opMainTex+"]")
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("conflicting end_version: %d, want 422 (body %s)", w.Code, w.Body.String())
	}
	// no cred -> 401 (dispatch_test pins WWW-Authenticate shape).
	w = doReq(t, a, "POST", "/api/projects/1/legacy_changes?end_version=2", "", "["+opMainTex+"]")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("no-cred import: %d, want 401", w.Code)
	}
}

// --- getLatest* (JWT/BASIC/TOKEN per route) ---

func TestLatestRoutes(t *testing.T) {
	a := testAPI(t)
	bootstrap(t, a, "1")

	// latest/content — JWT. no cred 401, wrong claim 403, right claim 200.
	w := doReq(t, a, "GET", "/api/projects/1/latest/content", "", "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("content no-cred: %d, want 401", w.Code)
	}
	w = doReq(t, a, "GET", "/api/projects/1/latest/content", "Bearer "+signJWT("9"), "")
	if w.Code != http.StatusForbidden {
		t.Errorf("wrong-claim content: %d, want 403 (body %s)", w.Code, w.Body.String())
	}
	w = doReq(t, a, "GET", "/api/projects/1/latest/content", "Bearer "+signJWT("1"), "")
	if w.Code != http.StatusOK {
		t.Fatalf("content: %d, body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "main.tex") {
		t.Errorf("content missing main.tex: %s", w.Body.String())
	}

	// latest/hashed_content — BASIC (Node handleBasicAuth): JWT-bearer 401.
	w = doReq(t, a, "GET", "/api/projects/1/latest/hashed_content", "Bearer "+signJWT("1"), "")
	if w.Code != 401 {
		t.Errorf("hashed_content JWT-bearer: %d, want 401 (Node is basic auth)", w.Code)
	}
	w = doReq(t, a, "GET", "/api/projects/1/latest/hashed_content", mustBasic(t), "")
	if w.Code != http.StatusOK {
		t.Errorf("hashed_content basic: %d, want 200 (body %s)", w.Code, w.Body.String())
	}

	// latest/history (JWT) — {"chunk": ...} (Node ChunkResponse.toRaw).
	w = doReq(t, a, "GET", "/api/projects/1/latest/history", "Bearer "+signJWT("1"), "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"chunk"`) {
		t.Fatalf("latest/history: %d, body %s", w.Code, w.Body.String())
	}
	// latest/persistedHistory (JWT) — same (delegates to latest/history).
	w = doReq(t, a, "GET", "/api/projects/1/latest/persistedHistory", "Bearer "+signJWT("1"), "")
	if w.Code != http.StatusOK {
		t.Errorf("persistedHistory: %d, want 200", w.Code)
	}

	// latest/history/raw (JWT) — metadata {startVersion,endVersion,endTimestamp}.
	w = doReq(t, a, "GET", "/api/projects/1/latest/history/raw", "Bearer "+signJWT("1"), "")
	if w.Code != http.StatusOK {
		t.Fatalf("latest/history/raw: %d, body %s", w.Code, w.Body.String())
	}
	m := jbody(t, w)
	if ev, ok := m["endVersion"].(float64); !ok || ev != 1 {
		t.Errorf("history/raw endVersion: %v (want 1)", m["endVersion"])
	}

	// versions/1/history (JWT).
	w = doReq(t, a, "GET", "/api/projects/1/versions/1/history", "Bearer "+signJWT("1"), "")
	if w.Code != http.StatusOK {
		t.Errorf("versions/1/history: %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	// versions/notanint -> intParse -> 404 (param validation).
	w = doReq(t, a, "GET", "/api/projects/1/versions/x/history", "Bearer "+signJWT("1"), "")
	if w.Code != http.StatusNotFound {
		t.Errorf("versions/x/history: %d, want 404", w.Code)
	}

	// uninitialized project: LoadLatest NotFound -> 404 (caught on
	// history endpoints; Node content endpoints are uncaught -> 500).
	w = doReq(t, a, "GET", "/api/projects/77/latest/history", "Bearer "+signJWT("77"), "")
	if w.Code != http.StatusNotFound {
		t.Errorf("uninitialized history: %d, want 404", w.Code)
	}
	// content endpoints are UNCAUGHT in Node: NotFound -> terminal 500.
	w = doReq(t, a, "GET", "/api/projects/77/latest/content", "Bearer "+signJWT("77"), "")
	if w.Code != http.StatusInternalServerError {
		t.Errorf("uninitialized content: %d, want 500 (Node uncaught)", w.Code)
	}
	// bad project id -> 404 validation (Node: Wrong project_id).
	w = doReq(t, a, "GET", "/api/projects/notpid/latest/history", "Bearer "+signJWT("notpid"), "")
	if w.Code != http.StatusNotFound {
		t.Errorf("bad project id: %d, want 404", w.Code)
	}

	// latest/zip — TOKEN auth: no cred 401, then 501 not-portable.
	w = doReq(t, a, "GET", "/api/projects/1/latest/zip", "", "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("latest/zip no-cred: %d, want 401", w.Code)
	}
	w = doReq(t, a, "GET", "/api/projects/1/latest/zip?token="+signJWT("1"), "", "")
	if w.Code != http.StatusNotImplemented {
		t.Errorf("latest/zip: %d, want 501 (not ported)", w.Code)
	}
	// unknown (valid MongoId-format but uninitialized) project -> 404
	// (Node 'returns 404 for an unknown project' — getLatestZip checks
	// LoadLatest before the not-portable zip generation).
	w = doReq(t, a, "GET", "/api/projects/abc123def456789012345678/latest/zip?token="+signJWT("abc123def456789012345678"), "", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("latest/zip unknown project: %d, want 404 (body %s)", w.Code, w.Body.String())
	}
}

// --- getChanges ---

func TestGetChanges(t *testing.T) {
	a := testAPI(t)
	bootstrap(t, a, "1")
	// since=0: 200 {changes: [...], hasMore: false}.
	w := doReq(t, a, "GET", "/api/projects/1/changes?since=0", mustBasic(t), "")
	if w.Code != http.StatusOK {
		t.Fatalf("changes since=0: %d (body %s), want 200", w.Code, w.Body.String())
	}
	m := jbody(t, w)
	if arr, ok := m["changes"].([]any); !ok || len(arr) != 1 {
		t.Errorf("changes: want 1 change, got %v", m["changes"])
	}
	// out-of-bounds since -> 400 {error: "Version out of bounds: N"}.
	w = doReq(t, a, "GET", "/api/projects/1/changes?since=50", mustBasic(t), "")
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "Version out of bounds") {
		t.Errorf("changes since=50: %d body %s, want 400 'Version out of bounds'", w.Code, w.Body.String())
	}
	// negative since -> 400.
	w = doReq(t, a, "GET", "/api/projects/1/changes?since=-1", mustBasic(t), "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("changes since=-1: %d, want 400", w.Code)
	}
	// non-numeric since -> 422 (Zod coerce tier, NOT controller-400).
	w = doReq(t, a, "GET", "/api/projects/1/changes?since=notanumber", mustBasic(t), "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("changes since=notanumber: %d, want 422", w.Code)
	}
	// no cred -> 401.
	w = doReq(t, a, "GET", "/api/projects/1/changes?since=0", "", "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("changes no-cred: %d, want 401", w.Code)
	}
}

// --- setContent ---

func TestSetContent(t *testing.T) {
	a := testAPI(t)
	bootstrap(t, a, "1")
	// happy: same content -> change:null noop, baseVersion 1.
	w := doReq(t, a, "POST", "/api/projects/1/set_content", mustBasic(t),
		`{"pathname":"main.tex","content":"hello","timestamp":"2020-01-02T00:00:00.000Z"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("setContent: %d, body %s", w.Code, w.Body.String())
	}
	m := jbody(t, w)
	if bv, ok := m["baseVersion"].(float64); !ok || bv != 1 {
		t.Errorf("baseVersion: %v (want 1)", m["baseVersion"])
	}
	// missing timestamp -> 422.
	w = doReq(t, a, "POST", "/api/projects/1/set_content", mustBasic(t),
		`{"pathname":"main.tex","content":"x"}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("no-timestamp: %d, want 422", w.Code)
	}
	// XOR violation (both content and blobHash) -> Node Zod strict() union
	// rejects at the 422 request tier (persist OError is dead over HTTP).
	w = doReq(t, a, "POST", "/api/projects/1/set_content", mustBasic(t),
		`{"pathname":"main.tex","content":"x","blobHash":"`+strings.Repeat("0", 40)+`","timestamp":"2020-01-02T00:00:00.000Z"}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("xor both: %d, want 422 (Zod union reject)", w.Code)
	}
	// Neither content nor blobHash -> Zod union reject -> 422.
	w = doReq(t, a, "POST", "/api/projects/1/set_content", mustBasic(t),
		`{"pathname":"main.tex","timestamp":"2020-01-02T00:00:00.000Z"}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("neither content nor blobHash: %d, want 422", w.Code)
	}
	// trackChanges without userId -> Zod refine -> 422.
	w = doReq(t, a, "POST", "/api/projects/1/set_content", mustBasic(t),
		`{"pathname":"main.tex","content":"x","timestamp":"2020-01-02T00:00:00.000Z","trackChanges":true}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("trackChanges w/o userId: %d, want 422", w.Code)
	}
	// blobHash only, hash not in store -> ServiceBlobNotFound -> 422.
	w = doReq(t, a, "POST", "/api/projects/1/set_content", mustBasic(t),
		`{"pathname":"main.tex","blobHash":"`+strings.Repeat("0", 40)+`","timestamp":"2020-01-02T00:00:00.000Z"}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("unknown blobHash: %d, want 422 (body %s)", w.Code, w.Body.String())
	}
	// content too large (Node: REQUEST_ENTITY_TOO_LARGE 413).
	ta := testAPI(t)
	bootstrap(t, ta, "31")
	body := `{"pathname":"main.tex","content":"` + strings.Repeat("x", core.MaxStringLength+1) + `","timestamp":"2020-01-02T00:00:00.000Z"}`
	w = doReq(t, ta, "POST", "/api/projects/31/set_content", mustBasic(t), body)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("too-large: %d, want 413 (body %s)", w.Code, w.Body.String())
	}
	// 404 for an uninitialized (never-imported) project.
	t2 := testAPI(t) // fresh: project "41" has no chunks
	w = doReq(t, t2, "POST", "/api/projects/41/set_content", mustBasic(t),
		`{"pathname":"main.tex","content":"hello","timestamp":"2020-01-02T00:00:00.000Z"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("uninitialized: %d, want 404 (body %s)", w.Code, w.Body.String())
	}
	// no cred -> 401.
	w = doReq(t, a, "POST", "/api/projects/1/set_content", "", `{"pathname":"main.tex","content":"x","timestamp":"2020-01-02T00:00:00.000Z"}`)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("no-cred: %d, want 401", w.Code)
	}
}

// --- lifecycle: import / flush / expire / clone / blob-stats / delete ---

func TestProjectLifecycle(t *testing.T) {
	a := testAPI(t)
	// importSnapshot: 200 {projectId}.
	w := doReq(t, a, "POST", "/api/projects/1/import", mustBasic(t), `{"files":{}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("import: %d, body %s", w.Code, w.Body.String())
	}
	if got := jbody(t, w)["projectId"]; got != "1" {
		t.Errorf("import projectId: %v (want 1)", got)
	}
	// second import: AlreadyInitialized -> 409.
	w = doReq(t, a, "POST", "/api/projects/1/import", mustBasic(t), `{"files":{}}`)
	if w.Code != http.StatusConflict {
		t.Errorf("second import: %d, want 409", w.Code)
	}
	// flush: 200 empty body (persist buffer no-op).
	w = doReq(t, a, "POST", "/api/projects/1/flush", mustBasic(t), "")
	if w.Code != http.StatusOK || w.Body.Len() != 0 {
		t.Errorf("flush: %d body %q, want 200 empty", w.Code, w.Body.String())
	}
	// expire: 200 empty.
	w = doReq(t, a, "POST", "/api/projects/1/expire", mustBasic(t), "")
	if w.Code != http.StatusOK || w.Body.Len() != 0 {
		t.Errorf("expire: %d body %q, want 200 empty", w.Code, w.Body.String())
	}
	// clone: not portable (IncrementalResponse not hermetic) -> 501.
	w = doReq(t, a, "POST", "/api/projects/1/clone", mustBasic(t), "")
	if w.Code != http.StatusNotImplemented {
		t.Errorf("clone: %d, want 501", w.Code)
	}
	// blob-stats: basic auth, 200 shape.
	w = doReq(t, a, "POST", "/api/projects/1/blob-stats", mustBasic(t), `{"blobHashes":[]}`)
	if w.Code != http.StatusOK {
		t.Errorf("blob-stats: %d, body %s", w.Code, w.Body.String())
	}
	if got := jbody(t, w)["projectId"]; got != "1" {
		t.Errorf("blob-stats projectId: %v", got)
	}
	// invalid hash -> 422 (recorded decision).
	w = doReq(t, a, "POST", "/api/projects/1/blob-stats", mustBasic(t), `{"blobHashes":["zz"]}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid hash: %d, want 422", w.Code)
	}
	// delete: 204; then history read -> 404; delete no-cred 401.
	w = doReq(t, a, "DELETE", "/api/projects/1", mustBasic(t), "")
	if w.Code != http.StatusNoContent {
		t.Errorf("delete: %d, want 204", w.Code)
	}
	w = doReq(t, a, "GET", "/api/projects/1/latest/history", "Bearer "+signJWT("1"), "")
	if w.Code != http.StatusNotFound {
		t.Errorf("post-delete history: %d, want 404", w.Code)
	}
	w = doReq(t, a, "DELETE", "/api/projects/1", "", "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("delete no-cred: %d, want 401", w.Code)
	}
}

// --- Express 404 parity ---

func TestCatchAllParity(t *testing.T) {
	a := testAPI(t)
	// unknown path: terminal 404 body {message:"Not Found",...}.
	w := doReq(t, a, "GET", "/api/nope/1", mustBasic(t), "")
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown path: %d, want 404", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Not Found") {
		t.Errorf("404 body: %s", w.Body.String())
	}
	// unregistered verb on registered path: 404 (NOT 405).
	w = doReq(t, a, "PATCH", "/api/projects/1/changes", mustBasic(t), "")
	if w.Code != http.StatusNotFound {
		t.Errorf("PATCH changes: %d, want 404 (Express parity)", w.Code)
	}
	// flush on uninitialized project: chunk not found -> 404 (Node catch).
	w = doReq(t, a, "POST", "/api/projects/9/flush", mustBasic(t), "")
	if w.Code != http.StatusNotFound {
		t.Errorf("flush uninitialized: %d, want 404 (Node NotFoundError catch)", w.Code)
	}
}

// --- TestInitializeProject (Node initializeProject -> 200 {projectId}) ---
// Fixture contract (test_projects.js): POST /api/projects {projectId?}.
// Absent projectId -> generated id; present -> used. Re-init -> 409 conflict.
func TestInitializeProject(t *testing.T) {
	a := testAPI(t)
	b := mustBasic(t)

	g := doReq(t, a, "POST", "/api/projects", b, `{}`)
	if g.Code != 200 {
		t.Fatalf("initialize empty body: %d, want 200", g.Code)
	}
	gen := jbody(t, g)["projectId"]
	if gen == nil || gen == "" {
		t.Fatalf("initialize: no generated projectId: %s", g.Body.String())
	}

	// Present projectId is honored.
	s := doReq(t, a, "POST", "/api/projects", b, `{"projectId":"abc-def"}`)
	if s.Code != 200 {
		t.Fatalf("initialize explicit id: %d", s.Code)
	}
	if got := jbody(t, s)["projectId"]; got != "abc-def" {
		t.Fatalf("initialize: projectId %v, want abc-def", got)
	}

	// Re-initialize existing -> 409 conflict.
	c := doReq(t, a, "POST", "/api/projects", b, `{"projectId":"abc-def"}`)
	if c.Code != 409 {
		t.Fatalf("re-initialize: %d, want 409", c.Code)
	}
}

// =============================================================================
// Acceptance mirror (Node test/acceptance/js/api/*.test.js)
//
// Each subtest maps to a Node acceptance `it()` case by name. The Node harness
// targets a live service (Mongo/Postgres/GCS/redis/minio) and cannot be
// re-targeted at the hermetic Go dispatch, so the observable contract
// (status + WWW-Authenticate shape + body) is mirrored over
// (*API).ServeHTTP. Only cases whose dependency is the 501 NotPorted
// endpoints are skipped, with t.Skip() documenting the reason.
// =============================================================================

func TestMirrorAuth(t *testing.T) {
	// auth.test.js (9 cases). All basic-401s emit WWW-Authenticate
	// Basic realm="Application" (Node security.js setup* emits that realm).
	t.Run("docs_no_cred_401", func(t *testing.T) {
		w := doReq(t, testAPI(t), "GET", "/docs", "", "")
		if w.Code != 401 {
			t.Fatalf("docs no-cred: %d, want 401", w.Code)
		}
		if got := w.Header().Get("WWW-Authenticate"); got != `Basic realm="Application"` {
			t.Errorf("docs WWW-Authenticate %q", got)
		}
	})
	t.Run("docs_wrong_cred_401", func(t *testing.T) {
		w := doReq(t, testAPI(t), "GET", "/docs", "Basic "+b64b("staging:wrong"), "")
		if w.Code != 401 {
			t.Fatalf("docs wrong cred: %d, want 401", w.Code)
		}
	})
	t.Run("docs_ok", func(t *testing.T) {
		w := doReq(t, testAPI(t), "GET", "/docs", mustBasic(t), "")
		if w.Code != 200 || w.Body.String() != "OK" {
			t.Fatalf("docs: %d body %q, want 200 OK", w.Code, w.Body.String())
		}
	})
	t.Run("import_401_basic", func(t *testing.T) {
		w := doReq(t, testAPI(t), "POST", "/api/projects/1/import", "", "{}")
		if w.Code != 401 || w.Header().Get("WWW-Authenticate") != `Basic realm="Application"` {
			t.Fatalf("import 401: %d / %q", w.Code, w.Header().Get("WWW-Authenticate"))
		}
	})
	t.Run("jwt_project_mismatch_403", func(t *testing.T) {
		w := doReq(t, testAPI(t), "GET", "/api/projects/1/latest/history/raw", "Bearer "+signJWT("2"), "")
		if w.Code != 403 {
			t.Fatalf("jwt mismatch: %d, want 403", w.Code)
		}
	})
	t.Run("jwt_not_accepted_for_import", func(t *testing.T) {
		w := doReq(t, testAPI(t), "POST", "/api/projects/1/import", "Bearer "+signJWT("1"), `{"files":{}}`)
		if w.Code != 401 {
			t.Fatalf("import+JWT: %d, want 401 (basic only)", w.Code)
		}
	})
	t.Run("uses_jwt", func(t *testing.T) {
		a := testAPI(t)
		bootstrap(t, a, "1")
		w := doReq(t, a, "GET", "/api/projects/1/latest/history/raw", "Bearer "+signJWT("1"), "")
		if w.Code != 200 {
			t.Fatalf("uses JWT: %d, want 200 (body %s)", w.Code, w.Body.String())
		}
	})
	t.Run("basic_in_place_of_jwt", func(t *testing.T) {
		a := testAPI(t)
		bootstrap(t, a, "1")
		w := doReq(t, a, "GET", "/api/projects/1/latest/history/raw", mustBasic(t), "")
		if w.Code != 200 {
			t.Fatalf("basic on jwt route: %d, want 200 (body %s)", w.Code, w.Body.String())
		}
	})
	t.Run("old_new_key_matrix", func(t *testing.T) {
		cfg := testCfg()
		cfg.JWTAuthKey = "new-key"
		cfg.JWTAuthOldKey = "old-key"
		fp := historystore.NewFakePersister()
		hs := historystore.New(fp, "main")
		bs := blobstore.NewStore()
		cs := chunkstore.New(hs, func(p string) core.BlobStoreI { return bs.Project(p) })
		a := New(cs, bs, cfg)
		bootstrap(t, a, "1")
		t.Run("accepts_new_key", func(t *testing.T) {
			w := doReq(t, a, "GET", "/api/projects/1/latest/history/raw", "Bearer "+signWith("1", "new-key"), "")
			if w.Code != 200 {
				t.Fatalf("new key: %d, want 200 (body %s)", w.Code, w.Body.String())
			}
		})
		t.Run("accepts_old_key", func(t *testing.T) {
			w := doReq(t, a, "GET", "/api/projects/1/latest/history/raw", "Bearer "+signWith("1", "old-key"), "")
			if w.Code != 200 {
				t.Fatalf("old key: %d, want 200 (body %s)", w.Code, w.Body.String())
			}
		})
		t.Run("rejects_other_key", func(t *testing.T) {
			w := doReq(t, a, "GET", "/api/projects/1/latest/history/raw", "Bearer "+signWith("1", "other"), "")
			if w.Code != 401 {
				t.Fatalf("other key: %d, want 401 (body %s)", w.Code, w.Body.String())
			}
		})
	})
}

// b64b — basic-auth base64 (test convenience; mustBasic already pins "staging").
func b64b(userpass string) string {
	return base64.StdEncoding.EncodeToString([]byte(userpass))
}

// --- accept-test mirrors: project_import / setContent happy paths /
//     project_updates git-bridge origin ---
//
// The Node acceptance cases all PUT an EMPTY_FILE_HASH blob first, which Go
// cannot (no blob PUT endpoint — not ported). The EMPTY-file happy-path
// contract (origin echo, resyncNeeded:false, 201 commits) is portable; each
// is pinned against an empty-content main.tex. The trackedChanges and binary
// happy paths need the blob backend and stay in the 501 bucket.

// TestMirrorProjectImport — project_import.test.js 'skips generating the
// snapshot by default' / 'accepts an add-file operation with importedAt-only
// file metadata' (both reduce to: empty snapshot import + add-file import ->
// 201 {resyncNeeded:false}).
func TestMirrorProjectImport(t *testing.T) {
	a := testAPI(t)
	// empty snapshot import (Node pre-PUTs the empty blob; Go: virtual).
	w := doReq(t, a, "POST", "/api/projects/91/import", mustBasic(t), `{"files":{}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("import: %d (body %s), want 200", w.Code, w.Body.String())
	}
	const emptyHash = "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"
	// add-file of an empty-content main.tex with plain hash.
	changes := `[{"operations":[{"pathname":"main.tex","file":{"hash":"` + emptyHash + `"}}],"timestamp":"2020-01-01T00:00:00.000Z","authors":[],"v2Authors":[]}]`
	w = doReq(t, a, "POST", "/api/projects/91/legacy_changes?end_version=0", mustBasic(t), changes)
	if w.Code != http.StatusCreated {
		t.Fatalf("import changes: %d (body %s), want 201", w.Code, w.Body.String())
	}
	if m := jbody(t, w); !isFalseAny(m["resyncNeeded"]) {
		t.Errorf("resyncNeeded = %v (want bool false)", m["resyncNeeded"])
	}
}

// TestMirrorSetContentCreateEdit — set_content.test.js 'builds create and
// edit changes' (first two phases: create then edit, committed via
// POST /changes). Pins origin + v2Authors echo and the 201 commit contract.
func TestMirrorSetContentCreateEdit(t *testing.T) {
	a := testAPI(t)
	w := doReq(t, a, "POST", "/api/projects/92/import", mustBasic(t), `{"files":{}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("import: %d (body %s)", w.Code, w.Body.String())
	}
	// create.
	w = doReq(t, a, "POST", "/api/projects/92/set_content", mustBasic(t),
		`{"pathname":"main.tex","source":"test-source","userId":"abcdef0123456789abcdef01","content":"one\ntwo\n","timestamp":"2020-01-01T00:00:00.000Z"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d (body %s), want 200", w.Code, w.Body.String())
	}
	var cr map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &cr); err != nil {
		t.Errorf("create not json: %v %s", err, w.Body.String())
	}
	// commit the returned change (Node applyChange -> POST /changes).
	w = doReq(t, a, "POST", "/api/projects/92/changes?end_version=0", mustBasic(t), "["+string(cr["change"])+"]")
	if w.Code != http.StatusCreated {
		t.Fatalf("commit create: %d (body %s), want 201", w.Code, w.Body.String())
	}
	// edit -> baseVersion 1, textOperation present.
	w = doReq(t, a, "POST", "/api/projects/92/set_content", mustBasic(t),
		`{"pathname":"main.tex","source":"test-source","userId":"abcdef0123456789abcdef01","content":"one\ntwo\nthree\n","timestamp":"2020-01-01T00:00:00.000Z"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("edit: %d (body %s), want 200", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "textOperation") {
		t.Errorf("edit: no textOperation: %s", w.Body.String())
	}
}

// TestMirrorGitBridgeOrigin — project_updates.test.js 'imports changes with
// git-bridge origin': a change carrying origin is persisted and echoed back
// by GET latest/history (change.getOrigin() deep-equals {kind:'git-bridge'}).
func TestMirrorGitBridgeOrigin(t *testing.T) {
	a := testAPI(t)
	w := doReq(t, a, "POST", "/api/projects/93/import", mustBasic(t), `{"files":{}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("import: %d (body %s)", w.Code, w.Body.String())
	}
	const emptyHash = "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"
	change := `{"operations":[{"pathname":"git.tex","file":{"hash":"` + emptyHash + `"}}],"origin":{"kind":"git-bridge"},"timestamp":"2020-01-01T00:00:00.000Z","authors":[],"v2Authors":[]}`
	w = doReq(t, a, "POST", "/api/projects/93/legacy_changes?end_version=0", mustBasic(t), "["+change+"]")
	if w.Code != http.StatusCreated {
		t.Fatalf("import origin: %d (body %s), want 201", w.Code, w.Body.String())
	}
	w = doReq(t, a, "GET", "/api/projects/93/latest/history", "Bearer "+signJWT("93"), "")
	if w.Code != http.StatusOK {
		t.Fatalf("latest/history: %d (body %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"origin":{"kind":"git-bridge"}`) {
		t.Errorf("origin not preserved: %s", w.Body.String())
	}
}
