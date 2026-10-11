package server

import (
	"net/http"
	"regexp"

	"ollitex/go/services/gitbridge/util"
	"ollitex/go/services/gitbridge/wglog"
)

var deleteProjectRe = regexp.MustCompile(`^/api/projects/([0-9a-f]{24})$`)

// Java handler regexes (Java Pattern, applied to pathInContext).
var (
	fileHandlerDocKeyRe = regexp.MustCompile(`^/(\w+)/.+$`)
	lfsBatchRe          = regexp.MustCompile(`^/[0-9a-z]+\.git/info/lfs/objects/batch/?$`)
	statusRe            = regexp.MustCompile(`^/status/?$`)
	healthRe            = regexp.MustCompile(`^/health_check/?$`)
	diagsRe             = regexp.MustCompile(`^/diags/?$`)
	metricsRe           = regexp.MustCompile(`^/metrics/?$`)
)

// handleDelete ports ProjectDeletionHandler: DELETE ^/api/projects/<hex24>
// (apiCtx) → CT text/plain + 204, bridge.deleteProject. No other shape is
// handled (live: unmatched DELETE /api/… → 404 28B handled elsewhere).
//
// AUTH GATE (hardening OVER the Java original, which was authless): the
// bridge had NO published ports and web-side project deletion is
// self-contained (web→bridge cleanup travels the signed postback route),
// yet the open form let ANY network-adjacent position delete a project's
// bridge-side state with zero credentials. Deletion takes exactly the
// same credential model as the git routes — Basic `git:<valid PAT>`
// through the shared token choke point — anything else 401s with no
// side effects.
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodDelete {
		return false
	}
	m := deleteProjectRe.FindStringSubmatch(r.URL.Path)
	if m == nil {
		return false
	}
	projectName := m[1]

	user, pass, res := parseBasicAuth(r)
	if res != credOK || user != "git" {
		grid(w, http.StatusUnauthorized)
		return true
	}
	code, _, _ := s.oauthScopedCheck(pass, util.ClientIp(r))
	switch {
	case code >= 200 && code < 400:
		// valid token → proceed.
	case code == http.StatusTooManyRequests:
		grid(w, http.StatusTooManyRequests)
	default:
		grid(w, http.StatusUnauthorized)
	}
	if code < 200 || code >= 300 {
		return true
	}

	wglog.Debug("DELETE <- /api/projects/%s", projectName)
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusNoContent)
	// Delete the project: DB row + repo dir + swap-store entry.
	s.br.DeleteProject(projectName)
	return true
}
