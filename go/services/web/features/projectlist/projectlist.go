// Package projectlist ports GET /user/projects (P4.1 — the project list).
//
// Node sources (oracle):
//
//	services/web/app/src/router.mjs                                    (route wiring)
//	services/web/app/src/Features/Project/ProjectController.mjs
//		· userProjectsJson        (the handler)
//		· _buildProjectList       (bucket order + token dedup)
//		· _buildProjectViewModel  (archived/trashed per user)
//	services/web/app/src/Features/Project/ProjectGetter.mjs            (findAllUsersProjects)
//	services/web/app/src/Features/Collaborators/CollaboratorsGetter.mjs (getProjectsUserIsMemberOf)
//	services/web/app/src/Features/Project/ProjectHelper.mjs            (isArchived/isTrashed)
//	services/web/app/src/Features/Authorization/Sources.mjs            (OWNER/INVITE/TOKEN)
//	services/web/app/src/Features/Authorization/PrivilegeLevels.mjs
//	services/web/app/src/Features/Authorization/PublicAccessLevels.mjs (TOKEN_BASED='tokenBased')
//
// Route (web profile):
//
//	GET /user/projects  → requireLogin → { projects: [ {_id, name, accessLevel} ] }
//
// Contract (pinned 2026-09-14 against the LIVE Node oracle — do not trust the
// higher-level ProjectListController; the wired handler is the simpler
// ProjectController.userProjectsJson):
//
//   - projects = owned ∪ invite(readWrite/review/readOnly) ∪ token(readAndWrite/
//     readOnly), in EXACTLY that bucket order (NO sorting), token buckets
//     de-duplicated against any id already listed (cascading access wins).
//
//   - accessLevel per bucket (NOTE the exact strings):
//
//     owner / readWrite / review / readOnly / readAndWrite / readOnly
//     OWNER   INVITE    INVITE   INVITE    TOKEN         TOKEN
//
//   - projects where the user is a member of `archived[]` or `trashed[]` are
//     OMITTED (`.filter(p => !(p.archived || p.trashed))`).
//
//   - each entry is exactly `{ _id, name, accessLevel }` (no totalSize, no
//     user objects, no lastUpdated).
//
// The wire JSON is a plain Go struct (Go marshals struct fields in declaration
// order, so the key order is deterministic without a custom encoder).
package projectlist

import (
	"regexp"

	"ollitex/go/services/web/core"
)

// ---------- legacy project-dashboard redirects (P7 cutover gap) ----------
//
// Node source (oracle): services/web/app/src/router.mjs
// projectDashboardRedirects (owner queue 7, 2026-09-10): the legacy
// project-list pages are REMOVED — every dashboard state renders in the hub
// (/hub#/projects.*). The routes 301 so bookmarks and SSO deep links land in
// the hub. Project APIs (POST /project/new*, POST /api/project,
// /user/projects, /project/:id/entities) are untouched; editor deep links
// (/editor/:id, legacy /Project/:id) stay.
//
// Node oracle (captured 2026-09-22 on the e2e stack, Node v22.21.1):
//
//	authed:     301 + Location + text/plain "Moved Permanently. Redirecting to <target>"
//	anonymous: 302 /login (requireLogin bounce; exact Express body)
//
// NOTE the literal `/project/tags/:tag` target — Node itself 301s every tag
// to the SAME hub route `/hub#/projects.tags.tags` (Node's static string); we
// pin that 1:1 rather than "fixing" it.
var dashTagPat = regexp.MustCompile(`^/project/tags/[^/]+$`)

// dashSlashPat — Node/Express routing is case-insensitive AND loose on the
// trailing slash: /Project/ (any case, exactly one trailing slash) hits the
// /project dashboard 301 (pinned live 2026-09-22 U2: 301 →
// /hub#/projects.all, "Moved Permanently. Redirecting to …"). The exact
// paths above already cover the canonical lowercase forms.
var dashSlashPat = regexp.MustCompile(`^/(?i:project)/$`)

func dashRedir(loc string) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		res.Redirect(cxt.Req, 301, loc)
	}
}

// Feature registers the project-list route. `NoLogin` is left false so the
// global login gate bounces anonymous requests exactly like Node's
// `AuthenticationController.requireLogin()` on this route.
func Feature(a *core.App) core.Feature {
	return core.Feature{
		Name: "projectlist",
		Routes: []core.Route{
			// Node registers projectDashboardRedirects BEFORE the other
			// /project routes — keep that order (first match wins).
			{Method: "GET", Path: "/project", Handler: dashRedir("/hub#/projects.all")},
			{Method: "GET", Path: "/project/owned", Handler: dashRedir("/hub#/projects.owned")},
			{Method: "GET", Path: "/project/shared", Handler: dashRedir("/hub#/projects.shared")},
			{Method: "GET", Path: "/project/archived", Handler: dashRedir("/hub#/projects.archived")},
			{Method: "GET", Path: "/project/trashed", Handler: dashRedir("/hub#/projects.trashed")},
			{Method: "GET", Path: "/project/untagged", Handler: dashRedir("/hub#/projects.all")},
			{Method: "GET", Pattern: dashTagPat, Handler: dashRedir("/hub#/projects.tags.tags")},
			{Method: "GET", Pattern: dashSlashPat, Handler: dashRedir("/hub#/projects.all")},
			{Method: "GET", Path: "/user/projects", Handler: handler(a)},
			{Method: "GET", Pattern: entPat, Handler: entitiesHandler(a)},
			{Method: "GET", Pattern: memPat, Handler: membersHandler(a)},
			{Method: "GET", Pattern: arPat, Handler: accessRequestsHandler(a)},
			{Method: "POST", Pattern: renPat, Handler: renameHandler(a)},
			{Method: "POST", Path: "/project/new", Handler: newProjectHandler(a)},
			// U1 (P7): the JSON project list (Node: /project/new → /api/project order)
			{Method: "POST", Path: "/api/project", Handler: apProjectHandler(a)},
			// P6.16 typst module (web-p616 flip) — Node TypstRouter route order
			{Method: "POST", Path: "/project/new/typst", Handler: newTypstProjectHandler(a)},
			{Method: "POST", Pattern: archPat, Handler: flagHandler(a, opArchive)},
			{Method: "DELETE", Pattern: archPat, Handler: flagHandler(a, opUnarchive)},
			{Method: "POST", Pattern: trashPat, Handler: flagHandler(a, opTrash)},
			{Method: "DELETE", Pattern: trashPat, Handler: flagHandler(a, opUntrash)},
			{Method: "POST", Pattern: restPat, Handler: restoreProjectHandler(a)},
			{Method: "POST", Pattern: clonePat, Handler: cloneProjectHandler(a)},
			{Method: "DELETE", Pattern: delPat, Handler: delProjectHandler(a)},
			// P4.10a collaborators (web-p4col flip)
			{Method: "POST", Pattern: leavePat, Handler: leaveHandler(a)},
			{Method: "POST", Pattern: reqAccPat, Handler: requestAccessHandler(a)},
			{Method: "PUT", Pattern: userPat, Handler: setUserLevelHandler(a, gateAdmin)},
			{Method: "DELETE", Pattern: userPat, Handler: removeUserHandler(a, gateAdmin)},
			{Method: "DELETE", Pattern: accDeclPat, Handler: declineReqHandler(a)},
			{Method: "POST", Pattern: accGrantPat, Handler: grantReqHandler(a)},
			{Method: "POST", Pattern: xferPat, Handler: transferOwnerHandler(a)},
			// P4.10b invites + sharing links (web-p4inv flip; Node route order)
			{Method: "POST", Pattern: invCreatePat, Handler: inviteCreateHandler(a, gateAdmin)},
			{Method: "GET", Pattern: invListPat, Handler: inviteListHandler(a, gateAdmin)},
			{Method: "DELETE", Pattern: invRevokePat, Handler: inviteRevokeHandler(a, gateAdmin)},
			{Method: "POST", Pattern: invResendPat, Handler: inviteResendHandler(a, gateAdmin)},
			{Method: "POST", Pattern: invAcceptPat, Handler: inviteAcceptHandler(a), NoLogin: true},
			{Method: "GET", Pattern: invViewTokPat, Handler: inviteViewHandler(a), NoLogin: true},
			{Method: "GET", Pattern: invTokensPat, Handler: tokensHandler(a)},
			{Method: "GET", Pattern: invSplitPat, Handler: splitForbiddenHandler(a, true), NoLogin: true},
			{Method: "POST", Pattern: invSplitPat, Handler: splitForbiddenHandler(a, true), NoLogin: true},
			{Method: "GET", Pattern: invSharePat, Handler: splitForbiddenHandler(a, false), NoLogin: true},
			{Method: "POST", Pattern: invSharePat, Handler: splitForbiddenHandler(a, true), NoLogin: true},
			{Method: "POST", Pattern: invShareValPat, Handler: splitForbiddenHandler(a, false), NoLogin: true},
			// P4.11a editor entity creation (web-p411a flip)
			{Method: "POST", Pattern: entDocPat, Handler: addEntityHandler(a, "doc")},
			{Method: "POST", Pattern: entFolderPat, Handler: addEntityHandler(a, "folder")},
			// U10.2b — entity rename/move/duplicate (Node EditorRouter POST family).
			{Method: "POST", Pattern: entRenPat, Handler: entRenameHandler(a)},
			{Method: "POST", Pattern: entMovPat, Handler: entMoveHandler(a)},
			{Method: "POST", Pattern: entDupPat, Handler: entDuplicateHandler(a)},
			// U10.3 — linked files (Node LinkedFilesRouter; CE: agents all
			// disabled -> _getAgent null -> bare 400 after validation;
			// validation/403/404/409 branches pinned by the LF gate).
			{Method: "POST", Pattern: lfCreatePat, Handler: lfCreateHandler(a)},
			{Method: "POST", Pattern: lfRefreshPat, Handler: lfRefreshHandler(a)},
			// P4.11b editor entity deletion (web-p411b flip)
			{Method: "DELETE", Pattern: delDocPat, Handler: delEntityHandler(a, "doc")},
			{Method: "DELETE", Pattern: delFilePat, Handler: delEntityHandler(a, "file")},
			{Method: "DELETE", Pattern: delFolderPat, Handler: delEntityHandler(a, "folder")},
			// P4.12a file proxy (web-p412 flip)
			{Method: "GET", Pattern: fproxyPat, Handler: fileProxyHandler(a)},
			// P4.12b document download (web-p412 flip)
			{Method: "GET", Pattern: docdlPat, Handler: docDownloadHandler(a)},
			// P4.13a file upload (POST /Project/:id/upload — capital P, pinned;
			// session+csrf applied by core — matches Node's csrf'd route)
			{Method: "POST", Pattern: upPat, Handler: uploadHandler(a)},
			// P4.13b new-project zip upload (POST /project/new/upload — session+csrf)
			{Method: "POST", Pattern: nzipPat, Handler: newzipHandler(a)},
			// P4.12c private API doc trio (web-p413 flip; basic auth in handler).
			// U10.3r (pinned live 2026-09-23, web :4000): these are NoSession
			// (the private-API gate issues its own fresh sid). The GET gate
			// 401/302 carry the FULL helmet set but NO X-Powered-By (pinned);
			// the valid-cred 200 res.send path carries XPB (set inside the
			// handler); the rendered 404 page carries NEITHER XPB set on the page.
			{Method: "GET", Pattern: docapiDlPat, NoSession: true, Handler: docapiGetHandler(a)},
			// U10.3r (pinned live 2026-09-23) — PROFILE-AWARE (2026-09-23 api fix):
			//   web: Node's session+csrf chain (csrf BEFORE helmet) blocks BOTH
			//         POST routes → 403 text/plain "Forbidden" + XPB + CSP + fresh
			//         sid, NO helmet set (the handler's web branch calls
			//         core.APISend403; identical wire to the former apiCSRF403).
			//   api: NO session/csrf chain — the 401 gate (core.APIBasicGate401)
			//         answers 401 for unauth/wrong (any Accept/method); a valid-
			//         cred POST reaches the setDocument / reject logic in the
			//         handler. Pinned vs Node api :3000.
			{Method: "POST", Pattern: docapiDlPat, NoSession: true, Handler: apiXPB(docapiPostHandler(a))},
			{Method: "POST", Pattern: docapiRejPat, NoSession: true, Handler: apiXPB(docapiRejectHandler(a))},
			// U-API — GET /project/:id/details (privateApiRouter, API-ONLY).
			// APIOnly: the web profile SKIPS this route (core.App filter) and
			// falls through to the already-verified web 404 tail — so :4000 is
			// unchanged. On the api profile it serves: unauth→401, bad-oid/ghost
			// →404, valid→200 JSON (owner features, document order).
			{Method: "GET", Pattern: detailsPat, NoSession: true, APIOnly: true, Handler: detailsGetHandler(a)},
			// U-API — GET /user/:user_id/personal_info (privateApiRouter, basic-auth;
			// the webRouter variant is the distinct path /user/personal_info — no
			// collision). unauth→401, bad-uid→404 JSON VA, ghost→404 text, valid→
			// 200 user-JSON (id-first). APIOnly → web profile skips it (unchanged).
			{Method: "GET", Pattern: personalInfoPat, NoSession: true, APIOnly: true, Handler: personalInfoGetHandler(a)},
		},
	}
}

// ---------- wire types (key order == Node's object key order) ----------

type ProjectItem struct {
	ID          string `json:"_id"`
	Name        string `json:"name"`
	AccessLevel string `json:"accessLevel"`
}

type ProjectListResp struct {
	Projects []ProjectItem `json:"projects"`
}

// view is one formatted project (pre archive/trash filter).
type view struct {
	id          string
	name        string
	accessLevel string
	archived    bool
	trashed     bool
}

func handler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if cxt.Sess == nil || !cxt.Sess.IsLoggedIn() {
			res.JSON(401, []byte(`{}`))
			return
		}
		uid := cxt.Sess.UserIDHex()

		projects, err := buildList(a, cxt, uid)
		if err != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		b := core.JSON(ProjectListResp{Projects: projects})
		res.JSON(200, b)
	}
}

// buildList mirrors userProjectsJson → _buildProjectList → filter → map.
func buildList(a *core.App, cxt *core.Cxt, uid string) ([]ProjectItem, error) {
	buckets, err := loadProjectBuckets(a, cxt, uid)
	if err != nil {
		return nil, err
	}

	var formatted []view
	seen := map[string]bool{}
	add := func(v view, _ bool) {
		// Node dedups across ALL buckets in order (owned first → best access
		// wins when the user is e.g. owner AND collaborator on the same
		// project — p4col-gate leftovers expose the double entry).
		if seen[v.id] {
			return
		}
		seen[v.id] = true
		formatted = append(formatted, v)
	}
	for _, p := range buckets.owned {
		add(viewModel(p, "owner", uid), false)
	}
	for _, p := range buckets.readWrite {
		add(viewModel(p, "readWrite", uid), false)
	}
	for _, p := range buckets.review {
		add(viewModel(p, "review", uid), false)
	}
	for _, p := range buckets.readOnly {
		add(viewModel(p, "readOnly", uid), false)
	}
	for _, p := range buckets.tokenReadAndWrite {
		add(viewModel(p, "readAndWrite", uid), true)
	}
	for _, p := range buckets.tokenReadOnly {
		add(viewModel(p, "readOnly", uid), true)
	}

	out := make([]ProjectItem, 0, len(formatted))
	for _, v := range formatted {
		if v.archived || v.trashed {
			continue
		}
		out = append(out, ProjectItem{ID: v.id, Name: v.name, AccessLevel: v.accessLevel})
	}
	return out, nil
}
