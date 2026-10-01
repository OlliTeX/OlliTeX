// Package gsync — the GitHub/project-provider sync (github-sync module)
// Go drop-in. Node authority: frontend/modules/github-sync +
// services/web/modules/github-sync (GitHubSyncRouter/Controller/Handler,
// TokenManager, GitServerClient, githubSyncUserCredentials,
// githubSyncProjectStates, GitSyncErrors, GitMerge, GitHubApiClient,
// HistoryManager, UpdateMerger/TpdsController seam).
//
// Architecture (1:1 with Node):
//   - web layer (this package) owns: user credential store (V3-cipher PATs
//     per provider/server/username), connection state, project sync state,
//     import/export orchestration, and the git-data REST merge engine.
//   - githubinterface microservice (go/services/githubinterface, runit on
//     127.0.0.1:4013) owns the git protocol ops: /check /clone /push
//     /pull /commit /create-repo /list-repos /branch-head /can-push.
//
// Wire parity notes (pinned from the Node oracles, 2026-10-02):
//
//	linkPAT      POST /user/git-pat/link
//	  req  {provider, serverUrl, username, token}
//	  200  {"status":"ok","username":...,"message":"Connection successful"}
//	  400  provider/username/token missing; 500 GHI errors (detail passthru)
//	testServer   POST /user/git-servers/test
//	  req  {serverUrl, username, token}
//	  200  {"status":"ok","username":...}  500 error passthru
//	getServers   GET  /user/git-servers
//	  200  [ {provider, serverUrl, username} ... ]
//	getStatus    GET  /user/github-sync/status
//	  200  {connected, providers:[...], oauth:{linked, username}}
//	unlink       POST /user/github-sync/unlink   200 {"status":"ok"}
//	orgs         GET  /user/github-sync/orgs
//	  200  [{login, html_url, type, email?} ...]  500 error
//	repos        GET  /user/github-sync/repos
//	  200  [{name, full_name, owner:login, description, private,
//	          default_branch, html_url, ...}, ...]
//	import       POST /project/new/github-sync
//	  req  {repoFullName, branchName?, projectName?, owner?}
//	  200  {"success":true,"project_id":...}
//	export       POST /project/:id/github-sync/export
//	  req  {repoFullName?, repoName?, description?, isPublic?, org?}
//	  200  {repoFullName, htmlUrl, defaultBranchName}
//	state        GET  /project/:id/github-sync/state
//	  404  not linked ({detail?} — Node: {detail:"Project not linked..."})
//	  200  {repoFullName, lastSyncCommit, lastSyncVersion, mergeStatus, ...}
//	mergeOv      GET  /project/:id/github-sync/merge/overview
//	  200  {commits:[{sha,message,author,email,date}], diverged,
//	          isProjectUpdated}
//	merge        POST /project/:id/github-sync/merge
//	  200  {status:"clean-remote"|..., lastSyncCommit, lastSyncVersion, ...}
//	unlinkProj   DELETE /project/:id/github-sync   200 {status:"ok"}
package gsync
