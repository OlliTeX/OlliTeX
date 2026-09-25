package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"history-v1/internal/config"
	"history-v1/internal/core"
	"history-v1/service/blobstore"
	"history-v1/service/chunkstore"
	"history-v1/service/historystore"
)

// --- fixtures ---

func testCfg() config.Config {
	return config.Config{
		BasicHttpAuthPassword: "staging",
		JWTAuthKey:            "staging",
	}
}

func testAPI(t *testing.T) *API {
	t.Helper()
	fp := historystore.NewFakePersister()
	hs := historystore.New(fp, "main")
	bs := blobstore.NewStore()
	cs := chunkstore.New(hs, func(p string) core.BlobStoreI { return bs.Project(p) })
	return New(cs, bs, testCfg())
}

// signJWT — HS256-sign a payload with key "staging" (the test JWT key).
func signJWT(projectID string) string { return signWith(projectID, "staging") }

// signWith — sign with an explicit key (old/non-new-key matrix tests).
func signWith(projectID, key string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, _ := json.Marshal(map[string]any{"project_id": projectID})
	payloadEnc := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(header + "." + payloadEnc))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return header + "." + payloadEnc + "." + sig
}

// --- top-level routes ---

func TestTopLevelRoutes(t *testing.T) {
	a := testAPI(t)

	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.Len() != 0 {
		t.Errorf("GET /: status %d body %q, want 200 empty", w.Code, w.Body.String())
	}

	r = httptest.NewRequest("GET", "/status", nil)
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "history-v1 is up") {
		t.Errorf("GET /status: status %d body %q, want 'history-v1 is up'", w.Code, w.Body.String())
	}

	r = httptest.NewRequest("GET", "/health_check", nil)
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "OK") {
		t.Errorf("GET /health_check: status %d body %q, want OK", w.Code, w.Body.String())
	}

	// Unknown top-level path → terminal 404 {message:"Not Found"}.
	r = httptest.NewRequest("GET", "/nope", nil)
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 404 || !strings.Contains(w.Body.String(), `"message":"Not Found"`) {
		t.Errorf("GET /nope: %d %q, want 404 Not Found", w.Code, w.Body.String())
	}

	// POST / (no route) → catchAll 404.
	r = httptest.NewRequest("POST", "/", nil)
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Errorf("POST /: status %d, want 404", w.Code)
	}
}

func TestDocsAuth(t *testing.T) {
	a := testAPI(t)

	// No credentials → 401, empty body, WWW-Authenticate header.
	r := httptest.NewRequest("GET", "/docs", nil)
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("GET /docs: status %d, want 401", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("GET /docs 401: body %q, want empty", w.Body.String())
	}
	if got := w.Header().Get("WWW-Authenticate"); got != `Basic realm="Application"` {
		t.Errorf("GET /docs 401: WWW-Authenticate %q", got)
	}

	// Valid basic auth → 200 body containing OK.
	r = httptest.NewRequest("GET", "/docs", nil)
	r.SetBasicAuth("staging", "staging")
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "OK") {
		t.Errorf("GET /docs ok: status %d body %q", w.Code, w.Body.String())
	}
}

// --- auth 401/403 shapes per route family ---

func TestAuth401Shapes(t *testing.T) {
	a := testAPI(t)
	hash := strings.Repeat("a", 40)

	// basic route: POST /api/projects, no creds → 401 + WWW-Authenticate,
	// body {message:"",error:{}}.
	r := httptest.NewRequest("POST", "/api/projects", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("no-cred POST /api/projects: %d, want 401", w.Code)
	}
	if got := w.Header().Get("WWW-Authenticate"); got != `Basic realm="Application"` {
		t.Errorf("POST /api/projects 401: WWW-Authenticate %q", got)
	}
	if !strings.Contains(w.Body.String(), `"message":""`) {
		t.Errorf("POST /api/projects 401: body %q, want message empty", w.Body.String())
	}

	// jwt route: PUT blob, no creds → 401 {message:"jwt missing"}.
	r = httptest.NewRequest("PUT", "/api/projects/1/blobs/"+hash, strings.NewReader("{}"))
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("PUT blob no-cred: %d, want 401", w.Code)
	}
	if !strings.Contains(w.Body.String(), `jwt missing`) {
		t.Errorf("PUT blob 401: body %q, want 'jwt missing'", w.Body.String())
	}

	// jwt route with a valid token but WRONG project_id claim → 403.
	r = httptest.NewRequest("GET", "/api/projects/2/blobs/"+hash, nil)
	r.Header.Set("Authorization", "Bearer "+signJWT("1"))
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Errorf("GET blob wrong project_id claim: %d, want 403", w.Code)
	}

	// jwt route with correct claim → authed (placeholder 501, NOT 401/403).
	r = httptest.NewRequest("GET", "/api/projects/2/blobs/"+hash, nil)
	r.Header.Set("Authorization", "Bearer "+signJWT("2"))
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code == 401 || w.Code == 403 {
		t.Errorf("GET blob correct claim: %d, want not 401/403", w.Code)
	}
}

// --- catchAll 404 on unmatched (method, path) ---

func TestCatchAllUnknownRoutes(t *testing.T) {
	a := testAPI(t)

	// GET on a POST-only route (import) → catchAll 404.
	r := httptest.NewRequest("GET", "/api/projects/1/import", nil)
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Errorf("GET /api/projects/1/import: %d, want 404", w.Code)
	}

	// PUT on the GET-only latest/zip → catchAll 404.
	r = httptest.NewRequest("PUT", "/api/projects/1/latest/zip", nil)
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Errorf("PUT /api/projects/1/latest/zip: %d, want 404", w.Code)
	}

	// Unknown top-level action → catchAll 404.
	r = httptest.NewRequest("POST", "/api/projects/1/nope", nil)
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Errorf("POST /api/projects/1/nope: %d, want 404", w.Code)
	}

	// Unknown 5-seg layout → catchAll 404.
	r = httptest.NewRequest("GET", "/api/projects/1/versions/5/nope", nil)
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Errorf("GET /api/projects/1/versions/5/nope: %d, want 404", w.Code)
	}
}
