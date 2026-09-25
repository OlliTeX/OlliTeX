// server.go — Go port of services/history-v1/app.js (the express app) plus
// the route table in api/routes/{projects,project_import}.js.
//
// A manual dispatcher is used (instead of http.ServeMux) because the Node
// (Express) semantics must be reproduced exactly:
//   - an unmatched (method, path) pair yields the terminal 404
//     (app.js catchAllAndWrap) — Go's mux would answer 405 for a known path
//     with a wrong method;
//   - the top-level routes (/, /status, /health_check, /docs) and the two
//     /api routers are mounted before the terminal 404 handler.
//
// All backends are in-memory (hermetic); there is no Redis/Postgres/Mongo/S3.
//
// SCOPE (as of this commit): the routing + auth dispatch below is COMPLETE
// and authoritative — the per-route auth mode mirrors
// api/routes/{projects,project_import}.js exactly, and auth runs BEFORE
// param validation (Node middleware order). The small, store-less handlers
// (/, /status, /health_check, /docs, terminal 404) are fully implemented.
// The store-backed handlers return a terminal 501 "not ported" until their
// backing-store plumbing is written (next work item; see HANDOFF).
package api

import (
	"net/http"
	"strconv"
	"strings"
	"sync"

	"history-v1/internal/config"
	"history-v1/service/blobstore"
	"history-v1/service/chunkstore"
	"history-v1/service/persist"
)

// API — the Go equivalent of the express app. Owns the hermetic stores, the
// persist service, and the auth configuration (security.go reads the cfg
// fields through a.cfg.*).
type API struct {
	cs      *chunkstore.Store
	bs      *blobstore.Store
	persist *persist.Service
	cfg     config.Config

	mu  sync.Mutex
	seq int
}

// New builds the API layer over the given stores and config. cfg must carry
// the fields security.go reads: BasicHttpAuthPassword, BasicHttpAuthOldPassword,
// JWTAuthKey, JWTAuthOldKey (a *config.Config with those populated). The
// persist service is constructed here so import/set-content share the same
// chunkstore+blobstore pair.
func New(cs *chunkstore.Store, bs *blobstore.Store, cfg config.Config) *API {
	return &API{
		cs:      cs,
		bs:      bs,
		persist: persist.NewService(cs, bs),
		cfg:     cfg,
	}
}

// generateID — hermetic stand-in for Node generateProjectId
// (nextval('docs_id_seq')::integer): "1", "2", ...
func (a *API) generateID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.seq++
	return strconv.Itoa(a.seq)
}

// ServeHTTP — top-level dispatch (Node app.js mount order). All top-level
// routes are GET (Node app.get); Express answers HEAD via the GET handler and
// any other method falls through to the catchAll 404.
func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api") {
		a.apiDispatch(w, r)
		return
	}
	if m := r.Method; m != http.MethodGet && m != http.MethodHead {
		a.catchAll(w)
		return
	}
	known := false
	switch r.URL.Path {
	case "/":
		// Node: res.send('') — 200, empty body, text/html.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		known = true
	case "/status":
		a.plain(w, "history-v1 is up")
		known = true
	case "/health_check":
		a.plain(w, "OK")
		known = true
	case "/docs":
		// basic-auth guard; 401 empty on fail (WWW-Authenticate set, NO body),
		// res.send('OK') on success.
		if e := a.authorize(authBasic, r, ""); e != nil {
			for k, v := range e.Headers {
				w.Header().Set(k, v)
			}
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		a.plain(w, "OK")
		known = true
	}
	if !known {
		a.catchAll(w)
	}
}

// plain — a plain-text 200 response (Node res.send(<string>)).
func (a *API) plain(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

// catchAll — the app.js terminal 404: { message: 'Not Found', error: {} }.
func (a *API) catchAll(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound,
		map[string]any{"message": "Not Found", "error": map[string]any{}})
}

// notPorted — terminal 501 for store handlers not yet wired (hermetic: the
// backing-store plumbing is the next work item).
func (a *API) notPorted(w http.ResponseWriter) {
	handleAPIError(w, &NotPorted{Path: "handler not yet ported"})
}

// --- manual dispatch over the two routers, path after "/api" ---

// apiDispatch — the full route table (see HANDOFF route table). Auth mode
// per route matches api/routes/{projects,project_import}.js. Unmatched
// (method, path) → catchAll (terminal 404).
func (a *API) apiDispatch(w http.ResponseWriter, r *http.Request) {
	segs := splitPath(strings.TrimPrefix(r.URL.Path, "/api"))
	m := r.Method

	// POST /api/projects (initialize). segs = ["projects"].
	if len(segs) == 1 {
		if segs[0] == "projects" && m == http.MethodPost {
			if e := a.authorize(authBasic, r, ""); e != nil {
				handleAPIError(w, e)
				return
			}
			a.initializeProject(w, r)
			return
		}
		a.catchAll(w)
		return
	}
	if segs[0] != "projects" {
		a.catchAll(w)
		return
	}
	pid := segs[1]

	switch {
	// /api/projects/blob-stats (literal id segment) and
	// /api/projects/:id (DELETE).
	case len(segs) == 2:
		if segs[1] == "blob-stats" {
			if m == http.MethodPost {
				if e := a.authorize(authBasic, r, ""); e != nil {
					handleAPIError(w, e)
					return
				}
				a.notPorted(w) // getProjectBlobsStats
				return
			}
			a.catchAll(w)
			return
		}
		if m != http.MethodDelete {
			a.catchAll(w)
			return
		}
		if e := a.authorize(authBasic, r, pid); e != nil {
			handleAPIError(w, e)
			return
		}
		a.deleteProject(w, pid)
	// /api/projects/:id/<action> (3 segments).
	case len(segs) == 3:
		a.dispatch3(w, r, pid, segs[2], m)

	// /api/projects/:id/<a>/<b> (4 segments).
	case len(segs) == 4:
		a.dispatch4(w, r, pid, segs[2], segs[3], m)

	// /api/projects/:id/<a>/<b>/<c> (5 segments).
	case len(segs) == 5:
		a.dispatch5(w, r, pid, segs[2], segs[3], segs[4], m)

	default:
		a.catchAll(w)
	}
}

// dispatch3 — /api/projects/:id/<action>.
func (a *API) dispatch3(w http.ResponseWriter, r *http.Request, pid, action, m string) {
	// clone / blob-stats / changes (GET|POST) / import / legacy_import /
	// legacy_changes / set_content / flush / expire — all basic auth.
	switch action {
	case "clone":
		if m != http.MethodPost {
			a.catchAll(w)
			return
		}
		if e := a.authorize(authBasic, r, pid); e != nil {
			handleAPIError(w, e)
			return
		}
		a.cloneProject(w, r, pid)
	case "changes":
		switch m {
		case http.MethodGet:
			if e := a.authorize(authBasic, r, pid); e != nil {
				handleAPIError(w, e)
				return
			}
			a.getChanges(w, r, pid)
		case http.MethodPost:
			if e := a.authorize(authBasic, r, pid); e != nil {
				handleAPIError(w, e)
				return
			}
			a.importChanges(w, r, pid)
		default:
			a.catchAll(w)
			return
		}
	case "import", "legacy_import":
		if m != http.MethodPost {
			a.catchAll(w)
			return
		}
		if e := a.authorize(authBasic, r, pid); e != nil {
			handleAPIError(w, e)
			return
		}
		a.importSnapshot(w, r, pid)
	case "legacy_changes":
		if m != http.MethodPost {
			a.catchAll(w)
			return
		}
		if e := a.authorize(authBasic, r, pid); e != nil {
			handleAPIError(w, e)
			return
		}
		a.importChanges(w, r, pid)
	case "set_content":
		if m != http.MethodPost {
			a.catchAll(w)
			return
		}
		if e := a.authorize(authBasic, r, pid); e != nil {
			handleAPIError(w, e)
			return
		}
		a.setContent(w, r, pid)
	case "flush", "expire":
		if m != http.MethodPost {
			a.catchAll(w)
			return
		}
		if e := a.authorize(authBasic, r, pid); e != nil {
			handleAPIError(w, e)
			return
		}
		if action == "flush" {
			a.flush(w, r, pid)
		} else {
			a.expire(w, pid)
		}
	case "blob-stats":
		if m != http.MethodPost {
			a.catchAll(w)
			return
		}
		if e := a.authorize(authBasic, r, pid); e != nil {
			handleAPIError(w, e)
			return
		}
		a.getBlobStats(w, r, pid)
	default:
		a.catchAll(w)
	}
}

// dispatch4 — /api/projects/:id/<a>/<b>.
func (a *API) dispatch4(w http.ResponseWriter, r *http.Request, pid, a4, b4, m string) {
	switch a4 {
	// /api/projects/:id/blobs/:hash
	case "blobs":
		switch m {
		case http.MethodGet, http.MethodHead: // jwt or token
			if e := a.authorize(authEither, r, pid); e != nil {
				handleAPIError(w, e)
				return
			}
			a.notPorted(w) // getProjectBlob / headProjectBlob
		case http.MethodPut, http.MethodPost: // jwt
			if e := a.authorize(authJWT, r, pid); e != nil {
				handleAPIError(w, e)
				return
			}
			a.notPorted(w) // createProjectBlob / copyProjectBlob
		default:
			a.catchAll(w)
		}

	// /api/projects/:id/latest/<X>
	case "latest":
		if m != http.MethodGet {
			a.catchAll(w)
			return
		}
		switch b4 {
		case "content", "history", "persistedHistory":
			if e := a.authorize(authJWT, r, pid); e != nil {
				handleAPIError(w, e)
				return
			}
		case "hashed_content":
			// Node route is handleBasicAuth (a.jwt would 401 here).
			if e := a.authorize(authBasic, r, pid); e != nil {
				handleAPIError(w, e)
				return
			}
		case "zip":
			// Token auth (Node handleTokenAuth).
			if e := a.authorize(authToken, r, pid); e != nil {
				handleAPIError(w, e)
				return
			}
		default:
			a.catchAll(w)
			return
		}
		switch b4 {
		case "content":
			a.getLatestContent(w, pid)
		case "hashed_content":
			a.getLatestHashedContent(w, r, pid)
		case "history":
			a.getLatestHistory(w, pid)
		case "persistedHistory":
			a.getLatestPersistedHistory(w, pid)
		case "zip":
			a.getLatestZip(w, pid)
		}

	// /api/projects/:id/versions/:v — bare (no trailing segment) has no route.
	case "versions", "timestamp":
		a.catchAll(w)
	default:
		a.catchAll(w)
	}
}

// dispatch5 — /api/projects/:id/<a>/<b>/<c>.
func (a *API) dispatch5(w http.ResponseWriter, r *http.Request, pid, a5, b5, c5, m string) {
	switch a5 {
	// /api/projects/:id/latest/history/raw
	case "latest":
		if b5 != "history" || c5 != "raw" || m != http.MethodGet {
			a.catchAll(w)
			return
		}
		if e := a.authorize(authJWT, r, pid); e != nil {
			handleAPIError(w, e)
			return
		}
		a.getLatestHistoryRaw(w, pid)

	// /api/projects/:id/versions/:v/<history|content>
	// (Express: route-level auth runs per registered path, so an
	// unknown sub-path 404s before auth.)
	case "versions":
		if m != http.MethodGet {
			a.catchAll(w)
			return
		}
		switch c5 {
		case "history", "content":
		default:
			a.catchAll(w)
			return
		}
		if e := a.authorize(authJWT, r, pid); e != nil {
			handleAPIError(w, e)
			return
		}
		switch c5 {
		case "history":
			a.getHistory(w, pid, b5)
		case "content":
			a.getVersionContent(w, pid, b5)
		}
	case "version":
		// Node zip route segment is singular: /version/:version/zip
		// (history/content use plural /versions/:version).
		switch m {
		case http.MethodGet:
			if e := a.authorize(authToken, r, pid); e != nil {
				handleAPIError(w, e)
				return
			}
			if c5 != "zip" {
				a.catchAll(w)
				return
			}
			a.getZip(w, pid, b5)
		case http.MethodPost:
			if e := a.authorize(authBasic, r, pid); e != nil {
				handleAPIError(w, e)
				return
			}
			if c5 != "zip" {
				a.catchAll(w)
				return
			}
			a.createZip(w, pid, b5)
		default:
			a.catchAll(w)
		}

	// /api/projects/:id/timestamp/:ts/history
	case "timestamp":
		if c5 != "history" || m != http.MethodGet {
			a.catchAll(w)
			return
		}
		if e := a.authorize(authJWT, r, pid); e != nil {
			handleAPIError(w, e)
			return
		}
		a.getHistoryBefore(w, pid, b5)
	default:
		a.catchAll(w)
	}
}

// splitPath — split a "/a/b/c" path into its non-empty segments.
func splitPath(path string) []string {
	if path == "" || path == "/" {
		return nil
	}
	return strings.Split(strings.TrimPrefix(path, "/"), "/")
}
