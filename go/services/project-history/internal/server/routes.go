// Package server: routes (Router.js exact) + wire-spec middleware.
package server

import (
	"net/http"
)

// Server hosts optional backend seams; nil seams → route returns 500 wire-equivalent.
type Server struct{}

// newNotWired returns a placeholder handler for routes whose module has not
// been ported yet (matches server.js 500 fallthrough).
func newNotWired() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonBody(w, 500, map[string]any{"message": "an internal error occurred"})
	}
}

// Routes builds the exact Router.js route table.
// Go mux matches literal segment counts: {version} = 1 segment, {pathname...} = 1+.
// So the version pair ({version} vs {version}/{pathname...}) is unambiguous.
func (s *Server) Routes() http.Handler {
	stub := newNotWired()
	mux := http.NewServeMux()

	mux.HandleFunc("POST /project", stub)                                                 // initializeProject
	mux.HandleFunc("DELETE /project/{project_id}", stub)                                  // deleteProject
	mux.HandleFunc("GET /project/{project_id}/snapshot", stub)                            // getLatestSnapshot
	mux.HandleFunc("GET /project/{project_id}/diff", stub)                                // getDiff
	mux.HandleFunc("GET /project/{project_id}/filetree/diff", stub)                       // getFileTreeDiff
	mux.HandleFunc("GET /project/{project_id}/updates", stub)                             // getUpdates
	mux.HandleFunc("GET /project/{project_id}/changes-in-chunk", stub)                    // getChangesInChunkSince
	mux.HandleFunc("GET /project/{project_id}/version", stub)                             // latestVersion
	mux.HandleFunc("POST /project/{project_id}/flush", stub)                              // flushProject
	mux.HandleFunc("GET /project/{project_id}/resync-pending", stub)                      // getResyncPending
	mux.HandleFunc("GET /project/{project_id}/debug-info", stub)                          // getDebugInfo
	mux.HandleFunc("POST /project/{project_id}/resync", stub)                             // resyncProject
	mux.HandleFunc("GET /project/{project_id}/dump", stub)                                // dumpProject
	mux.HandleFunc("GET /project/{project_id}/labels", stub)                              // getLabels
	mux.HandleFunc("POST /project/{project_id}/labels", stub)                             // createLabel
	mux.HandleFunc("DELETE /project/{project_id}/user/{user_id}/labels/{label_id}", stub) // deleteLabelForUser
	mux.HandleFunc("DELETE /project/{project_id}/labels/{label_id}", stub)                // deleteLabel
	mux.HandleFunc("POST /user/{from_user}/labels/transfer/{to_user}", stub)              // transferLabels
	// version pair: {version} (1 seg) vs {version}/{pathname...} (1+ seg) — distinct.
	mux.HandleFunc("GET /project/{project_id}/version/{version}/{pathname...}", stub)          // getFileSnapshot
	mux.HandleFunc("GET /project/{project_id}/version/{version}", stub)                        // getProjectSnapshot
	mux.HandleFunc("GET /project/{project_id}/ranges/version/{version}/{pathname...}", stub)   // getRangesSnapshot
	mux.HandleFunc("GET /project/{project_id}/metadata/version/{version}/{pathname...}", stub) // getFileMetadataSnapshot
	mux.HandleFunc("GET /project/{project_id}/paths/version/{version}", stub)                  // getPathsAtVersion
	mux.HandleFunc("POST /project/{project_id}/force", stub)                                   // forceDebugProject
	mux.HandleFunc("GET /project/{project_id}/blob/{hash}", stub)                              // getProjectBlob
	mux.HandleFunc("POST /project/{project_id}/clone", stub)                                   // cloneProject
	mux.HandleFunc("GET /status/failures", stub)                                               // getFailures
	mux.HandleFunc("GET /status/failures-full", stub)                                          // getFailuresFull
	mux.HandleFunc("GET /status/queue", stub)                                                  // getQueueCounts
	mux.HandleFunc("POST /retry/failures", stub)                                               // retryFailures
	mux.HandleFunc("POST /flush/old", stub)                                                    // flushOld
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		textPlain(w, "project-history is up")
	})
	mux.HandleFunc("GET /check_lock", stub)   // checkLock
	mux.HandleFunc("GET /health_check", stub) // healthCheck
	// Express unmatched = 404 (res.sendStatus(404) "Not Found" via error mw? no,
	// Express unmatched route → default 404 HTML).
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		sendStatus(w, 404)
	})
	return mux
}
