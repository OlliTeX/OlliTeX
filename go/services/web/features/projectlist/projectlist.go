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
	"encoding/json"

	"ollitex/go/services/web/core"
)

// Feature registers the project-list route. `NoLogin` is left false so the
// global login gate bounces anonymous requests exactly like Node's
// `AuthenticationController.requireLogin()` on this route.
func Feature(a *core.App) core.Feature {
	return core.Feature{
		Name: "projectlist",
		Routes: []core.Route{
			{Method: "GET", Path: "/user/projects", Handler: handler(a)},
			{Method: "GET", Pattern: entPat, Handler: entitiesHandler(a)},
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
		b, jerr := json.Marshal(ProjectListResp{Projects: projects})
		if jerr != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
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
	add := func(v view, dedup bool) {
		if dedup && seen[v.id] {
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
