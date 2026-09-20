package server

import (
	"net/http"
	"regexp"

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
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodDelete {
		return false
	}
	m := deleteProjectRe.FindStringSubmatch(r.URL.Path)
	if m == nil {
		return false
	}
	projectName := m[1]
	wglog.Debug("DELETE <- /api/projects/%s", projectName)
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusNoContent)
	// Delete the project: DB row + repo dir + swap-store entry.
	s.br.DeleteProject(projectName)
	return true
}
