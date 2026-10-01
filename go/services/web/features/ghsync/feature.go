package gsync

import (
	"net/http"
	"regexp"

	"ollitex/go/services/web/core"
)

// rePid — Go ServeMux capture for the 24-hex project id (Node
// validateObjectId — core route matching uses capture groups).
var rePid = `[0-9a-fA-F]{24}`

var (
	reUnlink     = regexp.MustCompile(`^/user/github-sync/unlink$`)
	reStatus     = regexp.MustCompile(`^/user/github-sync/status$`)
	reServersL   = regexp.MustCompile(`^/user/git-servers$`)
	reServersA   = regexp.MustCompile(`^/user/git-servers$`)
	reServerDel  = regexp.MustCompile(`^/user/git-servers/([^/]+)$`)
	rePAT        = regexp.MustCompile(`^/user/git-pat/link$`)
	reTest       = regexp.MustCompile(`^/user/git-servers/test$`)
	reOrgs       = regexp.MustCompile(`^/user/github-sync/orgs$`)
	reRepos      = regexp.MustCompile(`^/user/github-sync/repos$`)
	reImport     = regexp.MustCompile(`^/project/new/github-sync$`)
	reExport     = regexp.MustCompile(`^/project/(?<project_id>[0-9a-fA-F]{24})/github-sync/export$`)
	reState      = regexp.MustCompile(`^/project/(?<project_id>[0-9a-fA-F]{24})/github-sync/state$`)
	reMergeOv    = regexp.MustCompile(`^/project/(?<project_id>[0-9a-fA-F]{24})/github-sync/merge/overview$`)
	reMerge      = regexp.MustCompile(`^/project/(?<project_id>[0-9a-fA-F]{24})/github-sync/merge$`)
	reUnlinkProj = regexp.MustCompile(`^/project/(?<project_id>[0-9a-fA-F]{24})/github-sync$`)
)

// Feature — the GitHub/project-provider sync surface (Node github-sync
// module, GitHubSyncRouter.mjs). All routes session-protected
// (requireGlobalLogin); OAuth2 callback is anonymous (Node
// NoAuthentication).
func Feature() core.Feature {
	return core.Feature{
		Name: "ghsync",
		Routes: []core.Route{
			{Method: http.MethodGet, Path: "/user/github-sync/oauth2", Handler: oauth2RedirectHandler},
			{Method: http.MethodGet, Path: "/user/github-sync/oauth2/callback", Handler: oauth2CallbackHandler},
			{Method: http.MethodPost, Pattern: reUnlink, Handler: unlinkGitHubHandler},
			{Method: http.MethodGet, Pattern: reStatus, Handler: statusHandler},
			{Method: http.MethodGet, Pattern: reServersL, Handler: getServerHandler},
			{Method: http.MethodPost, Pattern: reServersA, Handler: addServerHandler},
			{Method: http.MethodDelete, Pattern: reServerDel, Handler: removeServerHandler},
			{Method: http.MethodPost, Pattern: rePAT, Handler: linkPATHandler},
			{Method: http.MethodPost, Pattern: reTest, Handler: testServerHandler},
			{Method: http.MethodGet, Pattern: reOrgs, Handler: orgsHandler},
			{Method: http.MethodGet, Pattern: reRepos, Handler: reposHandler},
			{Method: http.MethodPost, Pattern: reImport, Handler: importProjectHandler},
			{Method: http.MethodPost, Pattern: reExport, Handler: exportProjectHandler},
			{Method: http.MethodGet, Pattern: reState, Handler: projectStateHandler},
			{Method: http.MethodGet, Pattern: reMergeOv, Handler: mergeOverviewHandler},
			{Method: http.MethodPost, Pattern: reMerge, Handler: mergeProjectHandler},
			{Method: http.MethodDelete, Pattern: reUnlinkProj, Handler: unlinkProjectHandler},
		},
	}
}

var serverPat = reServerDel

var _ = serverPat
