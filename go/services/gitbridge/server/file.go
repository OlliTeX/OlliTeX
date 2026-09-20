// file.go — ports FileHandler.java (ResourceHandler serving the atts files
// referenced by snapshots sent to the Overleaf API).
//
// Java (applied in the /api context, after FileHandler's own gate falls to
// Postback/Deletion/Default):
//
//	method != GET                    → false (fall-through)
//	pathInContext !~ ^/(\w+)/.+$     → false
//	no "key" query parameter          → false
//	checkPostbackKey(docKey, key) fails → false + InvalidPostbackKeyException log
//	else ResourceHandler.serveResource → 200 the att file from
//	root/.wlgb/atts (Jetty default CT sniffing), 404 grid if the file is missing.
//
// pathInContext is the request path with the "/api" context stripped, so the
// Go equivalent is r.URL.Path minus the "/api/" prefix. Live grid: there is
// no live postback for the fixture project ids, so every cell (no key, wrong
// key) is the fall-through DefaultHandler 404 — `{"message":"HTTP error 404"}`.

package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"ollitex/go/services/gitbridge/wglog"
)

// handleFile ports FileHandler.handle (the /api context FileHandler step;
// fileHandlerDocKeyRe is the shared `^/(\w+)/.+$` regex, defined in delete.go
// alongside the other Java handler patterns).
func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	// pathInContext: r.URL.Path minus the "/api" context prefix.
	in := strings.TrimPrefix(r.URL.Path, "/api")
	m := fileHandlerDocKeyRe.FindStringSubmatch(in)
	if m == nil {
		return false
	}
	docKey := m[1]
	// key=... query parameter (Jetty Fields.getValue("key")).
	apiKey := r.URL.Query().Get("key")
	if apiKey == "" {
		return false
	}
	if err := s.br.CheckPostbackKey(docKey, apiKey); err != nil {
		wglog.Warn("INVALID POST BACK KEY: docKey=%s", docKey)
		return false
	}
	// Serve from base (root/.wlgb/atts), the in-context path (the request
	// path minus the "/api" context) mapped relative to that base — Java
	// ResourceHandler.serveResource mapping. Missing file → 404 grid.
	full := filepath.Join(s.cfg.GetRootGitDirectory(), ".wlgb", "atts", in)
	if fi, err := os.Stat(full); err != nil || fi.IsDir() {
		grid(w, http.StatusNotFound)
		return true
	}
	http.ServeFile(w, r, full)
	return true
}
