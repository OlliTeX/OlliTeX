// git.go — the git servlet context (Java WLGitServlet extends org.eclipse
// jgit GitServlet), reached from handleGitCtx after the Oauth2Filter passes
// (oauth2.go: (proceed=true, token)).
//
// In the live grid the mock snapshot has NO seeded doc for any project id, so
// bridge.GetUpdatedRepo fails for every project and every git op surfaces
// the captured 403s:
//
//	smart 403 (upadv_miss rcadv_miss post_upcot post_rccot):
//	    headers:  Content-Type: <service CT> + Expires + Pragma +
//	              Cache-Control: "no-cache, max-age=0, must-revalidate"
//	    body:     advertise → banner pkt + flush + ERR pkt (63/64 B)
//	              fetch/push → ERR pkt (29 B)
//	grid 403 (nosvc_miss dumb_miss upadv_nosvc unknown_service post_nocot):
//	    headers:  Pragma: no-cache (Jetty 4xx default), CL auto
//	    body:     `{"message":"HTTP error 403"}` 28 B (no CT — Go sniffing is
//	              a documented divergence vs Java)
//
// When the doc resolves (not fixture-reachable), the 200 stream delegates to
// gitproto (Option 3a shelled `git`), mirroring WLGitServlet factories.

package server

import (
	"io"
	"net/http"
	"strings"

	"ollitex/go/services/gitbridge/bridge"
	"ollitex/go/services/gitbridge/data"
	"ollitex/go/services/gitbridge/gitproto"
	"ollitex/go/services/gitbridge/util"
	"ollitex/go/services/gitbridge/wglog"
)

type gitRoute int

const (
	routeGrid        gitRoute = iota
	routeUploadAdv            // GET  .../info/refs?service=git-upload-pack
	routeUploadFetch          // POST .../git-upload-pack (CT x-git-upload-pack-request)
	routeReceiveAdv           // GET  .../info/refs?service=git-receive-pack
	routeReceivePush          // POST .../git-receive-pack (CT x-git-receive-pack-request)
)

// JGit GitSmartHttpTools smart-HTTP content types (byte-captured live).
const (
	gitCTUploadAdv     = "application/x-git-upload-pack-advertisement"
	gitCTUploadResult  = "application/x-git-upload-pack-result"
	gitCTReceiveAdv    = "application/x-git-receive-pack-advertisement"
	gitCTReceiveResult = "application/x-git-receive-pack-result"
)

// JGit smart-HTTP 403 pkt bodies — byte-captured live. Banner payload
// "# service=git-upload-pack\n" is 26B → "001e"; "# service=git-receive-
// pack\n" is 27B → "001f"; "ERR internal server error" is 25B → "001d".
var (
	pktErrBody        = []byte("001dERR internal server error")
	errBodyUploadAdv  = []byte("001e# service=git-upload-pack\n0000" + string(pktErrBody))
	errBodyReceiveAdv = []byte("001f# service=git-receive-pack\n0000" + string(pktErrBody))
)

// handleGitServlet — entry from handleGitCtx (post-filter).
func (s *Server) handleGitServlet(w http.ResponseWriter, r *http.Request, path, token string) {
	// repoName = first path segment minus ".git" (Java: WLRepositoryResolver
	// open(request, name) gets the JGit pathInfo segment; the Oauth2Filter
	// already validated the id shape).
	repoName := gitRepoName(path)
	route := gitRouteFor(r, path)

	// Resolve (lock + fetch doc + materialise the repo). Live mock has no
	// seeded doc → error → per-route 403 (byte-captured grid).
	repo, err := s.br.GetUpdatedRepo(&data.Oauth2{Token: token}, repoName)
	if err != nil {
		wglog.Warn("git-bridge: resolve repo %s: %v", repoName, err)
		s.gitErr(w, r, route)
		return
	}
	s.gitServe(w, r, repo, route, repoName, token)
}

// gitRepoName — the project id from the git path's first segment
// (Util.removeAllSuffixes(seg, ".git")).
func gitRepoName(path string) string {
	p := strings.TrimPrefix(path, "/")
	if i := strings.IndexByte(p, '/'); i >= 0 {
		p = p[:i]
		return util.RemoveAllSuffixes(p, ".git")
	}
	return util.RemoveAllSuffixes(p, ".git")
}

// gitRouteFor — JGit GitSmartHttpTools isInfoRefs/isUploadPack/isReceivePack
// dispatch (path suffix + service param + POST Content-Type).
func gitRouteFor(r *http.Request, path string) gitRoute {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		if strings.HasSuffix(path, "/info/refs") {
			switch r.URL.Query().Get("service") {
			case "git-upload-pack":
				return routeUploadAdv
			case "git-receive-pack":
				return routeReceiveAdv
			}
			// GET info/refs without a smart service (nosvc_miss,
			// upadv_nosvc, unknown_service) → grid.
			return routeGrid
		}
		// Dumb dumb (dumb_miss .../HEAD, objects/...) → grid.
		return routeGrid
	}
	if r.Method == http.MethodPost {
		switch {
		case strings.HasSuffix(path, "/git-upload-pack"):
			if r.Header.Get("Content-Type") == "application/x-git-upload-pack-request" {
				return routeUploadFetch
			}
		case strings.HasSuffix(path, "/git-receive-pack"):
			if r.Header.Get("Content-Type") == "application/x-git-receive-pack-request" {
				return routeReceivePush
			}
		}
		// POST without the matching request CT (post_nocot) → grid.
		return routeGrid
	}
	// Other methods → grid (no fixture cell; JGit 405/403 grid fallback).
	return routeGrid
}

// gitErr — 403 for the failed repo resolve (live: every project; JGit
// RepositoryFilter/JGitServlet error → sendError(403) per route kind).
func (s *Server) gitErr(w http.ResponseWriter, r *http.Request, route gitRoute) {
	// JGit consumeRequestBody before the error reply (POST ops).
	if r.Body != nil {
		_, _ = io.Copy(io.Discard, r.Body)
	}
	switch route {
	case routeUploadAdv:
		s.gitSmartErr(w, gitCTUploadAdv, errBodyUploadAdv)
	case routeUploadFetch:
		s.gitSmartErr(w, gitCTUploadResult, pktErrBody)
	case routeReceiveAdv:
		s.gitSmartErr(w, gitCTReceiveAdv, errBodyReceiveAdv)
	case routeReceivePush:
		s.gitSmartErr(w, gitCTReceiveResult, pktErrBody)
	default:
		s.gitGrid403(w)
	}
}

// gitGrid403 — grid (dumb / no-service / unknown-service / no-CT-POST) 403:
// Jetty sendError → ProductionErrorHandler body `{"message":"HTTP error 403"}`
// + Pragma (Jetty auto 4xx header). CL is Go-auto.
func (s *Server) gitGrid403(w http.ResponseWriter) {
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusForbidden)
	w.Write(productionErrorBody(403)) //nolint:errcheck
}

// gitSmartErr — smart 403 (JGit sendError on a smart route): NoCacheFilter
// headers (Expires/Pragma/Cache-Control) + service CT + pkt body.
func (s *Server) gitSmartErr(w http.ResponseWriter, ct string, body []byte) {
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Expires", "Fri, 01 Jan 1980 00:00:00 GMT")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Cache-Control", "no-cache, max-age=0, must-revalidate")
	w.WriteHeader(http.StatusForbidden)
	w.Write(body) //nolint:errcheck
}

// gitServe — the 200 path (repo materialised). Not fixture-reachable live
// (mock: no doc) but wired for parity: Java WLGitServlet delegates to
// WLUploadPackFactory/WLReceivePackFactory which exec `git upload-pack
// --stateless-rpc <repoPath>` / `git receive-pack --stateless-rpc
// <repoPath>` and pipe request→stdin, stdout→response.
func (s *Server) gitServe(w http.ResponseWriter, r *http.Request, repo bridge.ProjectRepo, route gitRoute, repoName, token string) {
	workDir := repo.ProjectDir()
	protocol := r.Header.Get("Git-Protocol")
	// JGit NoCacheFilter applies to every smart-HTTP response (live capture
	// on the 200 advertise: Expires + Pragma + Cache-Control).
	w.Header().Set("Expires", "Fri, 01 Jan 1980 00:00:00 GMT")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Cache-Control", "no-cache, max-age=0, must-revalidate")
	switch route {
	case routeUploadAdv:
		w.Header().Set("Content-Type", gitCTUploadAdv)
		w.WriteHeader(http.StatusOK)
		_ = s.uploadPack.ServeAdvertise(workDir, protocol, w)
	case routeUploadFetch:
		w.Header().Set("Content-Type", gitCTUploadResult)
		w.WriteHeader(http.StatusOK)
		_ = s.uploadPack.Handle(workDir, protocol, r.Body, w)
	case routeReceiveAdv:
		w.Header().Set("Content-Type", gitCTReceiveAdv)
		w.WriteHeader(http.StatusOK)
		_ = gitproto.NewReceivePackHandler(s.gitBinary, repoName, s.receiveHost, token).ServeAdvertise(workDir, w)
	case routeReceivePush:
		w.Header().Set("Content-Type", gitCTReceiveResult)
		w.WriteHeader(http.StatusOK)
		_ = gitproto.NewReceivePackHandler(s.gitBinary, repoName, s.receiveHost, token).Handle(workDir, protocol, r.Body, w)
	default:
		// Dumb-serve with a live repo: the Java bridge does not dumb-serve
		// (JGit AsIsFileService off by default is not the case here, but the
		// live grid never reaches this with a live doc) → grid 403 fallback.
		s.gitGrid403(w)
	}
}
