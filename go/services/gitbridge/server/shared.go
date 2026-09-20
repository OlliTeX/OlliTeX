// Package server — ports GitBridgeServer + all Jetty handlers/filters/servlets.
//
// The dispatch model mirrors the Java Jetty chain (HANDOFF §17):
//
//	CORS (outermost middleware)
//	  → bare /api (301 → /api/, all methods)
//	  → /api/* (File → Postback → ProjectDeletion → Default 404)
//	  → baseCtx (/ → Status → HealthCheck → LFS → Prometheus → Diagnostics;
//	    else falls through to gitCtx)
//	  → gitCtx (Oauth2Filter → WLGitServlet GitServlet dispatch)
//
// In Go we implement this as a single http.Handler with internal step
// handlers (a step returns true if it handled the request, else false so the
// next step runs) — mirroring Java's Handler.Sequence (with no DefaultHandler
// in baseCtx per the live grid: unmatched /api and non-GET /api/* fall into
// the gitCtx chain).

package server

import (
	"net/http"

	"ollitex/go/services/gitbridge/bridge"
	"ollitex/go/services/gitbridge/config"
	"ollitex/go/services/gitbridge/gitproto"
)

// step is a handler that returns true if it handled the request (wrote a
// response), false if it should fall through to the next step.
type step func(w http.ResponseWriter, r *http.Request) bool

// Server holds the shared state for all sub-handlers (Java GitBridgeServer).
type Server struct {
	cfg         *config.Config
	br          *bridge.Bridge
	uploadPack  *gitproto.UploadPackHandler
	gitBinary   string
	hookBinary  string
	receiveHost string
	origins     originSet
	oauthClient OAuthClient
}

// NewServer builds the full dispatch handler. hookBinary is the path of the
// git_bridge executable itself (used to install the proc-receive hook shim on
// repo init, mirroring the shim the Java launcher writes: `#!/bin/sh` +
// `exec <binary> -hook-proc-receive <config-path>`). oauthClient seam: nil
// default (HTTP) for live parity tests.
func NewServer(cfg *config.Config, br *bridge.Bridge, gitBinary, hookBinary string) http.Handler {
	return NewServerWithSeams(cfg, br, gitBinary, hookBinary, "", nil)
}

// NewServerWithSeams builds with explicit seams (test injection):
// recvHost is the HOST handler-contract value (Go: os.Hostname port 1:1 from
// Java), oauthClient the token-info HTTP client (nil = HTTP to cfg.Oauth2Server).
func NewServerWithSeams(cfg *config.Config, br *bridge.Bridge, gitBinary, hookBinary string, recvHost string, oauthClient OAuthClient) http.Handler {
	s := &Server{
		cfg:         cfg,
		br:          br,
		uploadPack:  gitproto.NewUploadPackHandler(gitBinary),
		gitBinary:   gitBinary,
		hookBinary:  hookBinary,
		origins:     newOriginSet(cfg.AllowedCorsOrigins),
		receiveHost: recvHost,
		oauthClient: oauthClient,
	}
	return http.HandlerFunc(s.dispatch)
}

// dispatch is the top-level router (Java Handler.Sequence(cors, api, base, git)).
func (s *Server) dispatch(w http.ResponseWriter, r *http.Request) {
	// CORS middleware (outermost, Java CORSHandler): allowed origin → 5
	// ACA headers on every response; OPTIONS → 200 (allowed) / 403
	// (disallowed); no Origin or non-CORS method → passthrough untouched.
	origin := r.Header.Get("Origin")
	if origin != "" {
		_, ok := s.origins[origin]
		if ok {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, POST, DELETE")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}
		if r.Method == http.MethodOptions {
			if ok {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusForbidden)
			}
			return
		}
	}
	// Bare /api redirect (all methods, 301 Location /api/ CL 0).
	if r.URL.Path == "/api" {
		w.Header().Set("Location", "/api/")
		w.WriteHeader(http.StatusMovedPermanently)
		return
	}
	// /api context.
	if len(r.URL.Path) > 4 && r.URL.Path[:5] == "/api/" {
		if s.handleApiCtx(w, r) {
			return
		}
		// Fall-through (baseCtx can't match /api/… paths) → gitCtx chain,
		// exactly as Jetty's Handler.Sequence does.
		s.handleGitCtx(w, r)
		return
	}
	// baseCtx context.
	if s.handleBaseCtx(w, r) {
		return
	}
	// gitCtx catch-all (Java ServletContextHandler /*, with its
	// ProductionErrorHandler — a panic here becomes the 500 28B grid cell).
	s.handleGitCtx(w, r)
}

// handleApiCtx dispatches within the /api context.
func (s *Server) handleApiCtx(w http.ResponseWriter, r *http.Request) bool {
	// FileHandler (GET ^/api/(\w+)/.+$ + query key → ResourceHandler).
	if s.handleFile(w, r) {
		return true
	}
	// PostbackHandler (POST …/postback).
	if s.handlePostback(w, r) {
		return true
	}
	// ProjectDeletionHandler (DELETE ^/api/projects/([0-9a-f]{24})$).
	if s.handleDelete(w, r) {
		return true
	}
	// Live-captured (Java 43102): OPTIONS (no origin) anywhere in /api that no
	// other /api handler matches → Jetty 500, EMPTY body (CL 0), and
	// Cache-Control must-revalidate,no-cache,no-store (Jetty 12 500-with-handler
	// default; body suppressed for OPTIONS, contrast root OPTIONS' 28B grid).
	if r.Method == http.MethodOptions {
		w.Header().Set("Cache-Control", "must-revalidate,no-cache,no-store")
		w.WriteHeader(http.StatusInternalServerError)
		return true
	}
	// DefaultHandler: 404 (live: 404 28B `{"message":"HTTP error 404"}` for
	// POST/PUT/OPTIONS; GET exact /api/ → Jetty 1011B context-list HTML).
	if r.Method == http.MethodGet && r.URL.Path == "/api/" {
		w.Header().Set("Content-Type", "text/html;charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		w.Write(must4041011)
		return true
	}
	grid(w, http.StatusNotFound)
	return true
}

// must4041011 is the Jetty "no context matched" HTML for GET /api/
// (1011 bytes live-captured from Java 43102, byte-identical across requests,
// INCLUDING the trailing newline after </html>). A Go raw string literal
// drops the final newline, so this is an explicit trailing "\n".
var must4041011 = []byte(`<!DOCTYPE html>
<html lang="en">
<head>
<title>Error 404 - Not Found</title>
<meta charset="utf-8">
<style>body { font-family: sans-serif; } table, td { border: 1px solid #333; } td, th { padding: 5px; } thead, tfoot { background-color: #333; color: #fff; } </style>
</head>
<body>
<h2>Error 404 - Not Found.</h2>
<p>No context on this server matched or handled this request.</p>
<p>Contexts known to this server are:</p>
<table class="contexts"><thead><tr><th>Context Path</th><th>Display Name</th><th>Status</th><th>LifeCycle</th></tr></thead><tbody>
<tr><td><a href="/api/">/api</a></td><td>/api&nbsp;</td><td>Available</td><td>STARTED</td></tr>
<tr><td><a href="/">/</a></td><td>ROOT&nbsp;</td><td>Available</td><td>STARTED</td></tr>
<tr><td><a href="/">/</a></td><td>ROOT&nbsp;</td><td>Available</td><td>STARTED</td></tr>
</tbody></table><hr/>
<a href="https://jetty.org"><img alt="icon" src="/favicon.ico"/></a>&nbsp;<a href="https://jetty.org">Powered by Eclipse Jetty:// Server</a><hr/>
</body>
</html>
`)

// handleBaseCtx dispatches within the base context (/) and returns false on
// fall-through (Java baseCtx has NO DefaultHandler — unmatched requests fall
// into the gitCtx chain).
func (s *Server) handleBaseCtx(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	switch {
	// StatusHandler (GET|HEAD ^/status/?$), always 200.
	// (CT text/plain — live; Java never calls setContentType here, so Go
	// suppresses automatic CT sniffing for the CL-carrying parity.)
	case (r.Method == http.MethodGet || r.Method == http.MethodHead) && statusRe.MatchString(path):
		w.Header().Set("Content-Type", "text/plain")
		if r.Method == http.MethodGet {
			w.Write([]byte("ok\n"))
		}
		return true
	// HealthCheckHandler (GET|HEAD ^/health_check/?$).
	case (r.Method == http.MethodGet || r.Method == http.MethodHead) && healthRe.MatchString(path):
		w.Header().Set("Content-Type", "text/plain")
		if s.br.HealthCheck() {
			if r.Method == http.MethodGet {
				w.Write([]byte("ok\n"))
			}
			return true
		}
		w.WriteHeader(http.StatusInternalServerError)
		if r.Method == http.MethodGet {
			w.Write([]byte("failed\n"))
		}
		return true
	}
	// PrometheusHandler (GET ^/metrics/?$): Go runtime gauges (Java emits JVM
	// hotspot metrics — names differ, documented).
	if r.Method == http.MethodGet && metricsRe.MatchString(path) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Write([]byte(runtimeMetrics()))
		return true
	}
	// DiagnosticsHandler (GET ^/diags/?$).
	if r.Method == http.MethodGet && diagsRe.MatchString(path) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(diagnosticsBody))
		return true
	}
	// GitLfsHandler (POST ^/[0-9a-z]+\.git/info/lfs/objects/batch/?$).
	// Reached for git-context LFS batch requests (baseCtx falls into gitCtx
	// first per the chain; here it catches any direct match).
	if r.Method == http.MethodPost && lfsBatchRe.MatchString(path) {
		w.Header().Set("Content-Type", "application/vnd.git-lfs+json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write(lfsBatchBody)
		return true
	}
	return false
}

var diagnosticsBody = []byte("Native memory tracking is not enabled\n" +
	"\n----------\n\n" +
	"Native memory tracking is not enabled\n")

// lfsBatchBody — Java GitLfsHandler body (59 bytes incl trailing \n, space
// after the colon).
var lfsBatchBody = []byte("{\"message\": \"ERROR: Git LFS is not supported on Overleaf\"}\n")

// handleGitCtx is the git context: Oauth2Filter (when oauth2Server is set,
// runs for EVERY method incl. OPTIONS — opts_root → 500 grid, opts_git/
// opts_status → 401 287B) → WLGitServlet / GitServlet dispatch.
func (s *Server) handleGitCtx(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if s.cfg.Oauth2Server != "" {
		proceed, token := s.handleOauth2(w, r, path)
		if !proceed {
			return
		}
		s.handleGitServlet(w, r, path, token)
		return
	}
	// Filter disabled: straight to the servlet (token empty → resolver treats
	// it unauthed in the live grid; the 200 flow requires the filter).
	s.handleGitServlet(w, r, path, "")
}
