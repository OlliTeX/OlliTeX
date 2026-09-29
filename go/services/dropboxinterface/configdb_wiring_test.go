package dropboxinterface

import (
	http "net/http"
	http_test "net/http/httptest"
	filepath "path/filepath"
	strings "strings"
	testing "testing"

	"ollitex/go/libraries/configstore"
)

// TestMux_BodyLimitFromConfigDB — the JSON body limit is admin-tunable via the
// config-DB (DROPBOXINTERFACE_MAX_BODY_MB); a 1MB DB limit must reject a 2MB body with 413.
func TestMux_BodyLimitFromConfigDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "configdb.sqlite3")
	st, err := configstore.New(dbPath)
	if err != nil {
		t.Fatalf("configstore.New: %v", err)
	}
	if err := st.Set("DROPBOXINTERFACE_MAX_BODY_MB", "1", "test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	st.Close()
	t.Setenv("CONFIG_DB_PATH", dbPath)

	h := DropboxHandlers{Cfg: DropboxConfig{}}
	mux := h.Mux()
	req := http_test.NewRequest(http.MethodPost, "/check", strings.NewReader(strings.Repeat("x", 2*1024*1024)))
	req.Header.Set("Content-Type", "application/json")
	rw := http_test.NewRecorder()
	mux.ServeHTTP(rw, req)
	if rw.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (DB body limit 1MB applied to 2MB body)", rw.Code)
	}
}
