package projectlist

import (
	"regexp"
	"sort"
	"strings"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

// entPat — Express `webRouter.get('/project/:Project_id/entities')`: one
// non-empty path segment for the id, then literal `/entities`. The empty-id
// URL (`/project//entities`) does NOT match Node's route (it falls through to
// the 404 catch-all), so we mirror that by requiring `[^/]+`.
var entPat = regexp.MustCompile(`^/project/([^/]+)/entities$`)

// validOID — zz.objectId in _getProjectId: a 24-char hex Mongo ObjectId.
var validOID = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// malformed404 — Node's 404 JSON when the id is not a valid ObjectId
// (pinned live 2026-09-14; NOT accept-dependent):
//
//	{"error":"Validation error: Invalid Mongo ObjectId at \"params.Project_id\"","statusCode":404}
const malformed404 = `{"error":"Validation error: Invalid Mongo ObjectId at \"params.Project_id\"","statusCode":404}`

// entOut / entResp — Node's `{ project_id, entities:[{path,type}] }`
// (key order: project_id then entities; entity keys path then type).
type entOut struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

type entResp struct {
	ProjectID string   `json:"project_id"`
	Entities  []entOut `json:"entities"`
}

func entitiesHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		// The global login gate (Route.NoLogin=false) bounces anonymous
		// requests to 401(json)/302(html) before this runs — mirroring Node's
		// AuthenticationController.requireLogin(). Defensive fallback:
		if cxt.Sess == nil || cxt.Sess.UserIDHex() == "" {
			if core.AcceptsJSON(cxt.Req) {
				res.SendStatus(401)
			} else {
				res.Redirect(cxt.Req, 302, "/login")
			}
			return
		}
		uid := cxt.Sess.UserIDHex()
		param := cxt.Params["1"]
		// The 403/404 views' <link rel=alternate> is `settings.url +
		// currentUrl` (origin + trailing slash + RELATIVE path), so the Path
		// slot takes the path WITHOUT a leading slash.
		relPath := strings.TrimPrefix(cxt.Req.URL.Path, "/")

		// ensureUserCanReadProject -> _getProjectId -> parseReq(zod objectId):
		// invalid ObjectId -> 404 JSON validation error (NOT accept-dependent).
		if !validOID.MatchString(param) {
			res.JSON(404, []byte(malformed404))
			return
		}

		oid, err := primitive.ObjectIDFromHex(strings.ToLower(param))
		if err != nil {
			res.JSON(404, []byte(malformed404))
			return
		}

		// valid id -> load project. Absent -> 404 general/404 (HTML, NOT
		// accept-dependent). Node: getProjectAccess -> getProject -> NotFound.
		doc, lerr := loadProject(a, cxt, oid)
		if lerr != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		if doc == nil {
			views.NotFoundPage(res.W, pageBase(cxt, relPath))
			return
		}

		can := canRead(uid, loadUserAdmin(a, cxt, uid), *doc)
		if !can {
			// 403 — accept-dependent (pinned):
			//   accept json -> {"message":"restricted"}
			//   accept html -> restricted view
			if core.AcceptsJSON(cxt.Req) {
				res.JSON(403, []byte(`{"message":"restricted"}`))
			} else {
				views.Restricted403(res.W, pageBase(cxt, relPath))
			}
			return
		}

		ents := collectEntities(doc)
		sort.SliceStable(ents, func(i, j int) bool { return ents[i].path < ents[j].path })
		out := make([]entOut, 0, len(ents))
		for _, e := range ents {
			out = append(out, entOut{Path: e.path, Type: e.typ})
		}
		b := core.JSON(entResp{ProjectID: param, Entities: out})
		res.JSON(200, b)
	}
}

// pageBase builds the views.PageData for the 403/404 renders (nonce + origin
// + session slots + the request path embedded in the 404 link).
func pageBase(cxt *core.Cxt, reqPath string) views.PageData {
	d := views.PageData{Nonce: views.NewNonce(), Path: reqPath}
	origin := cxt.SiteURL
	if origin == "" {
		origin = "http://" + cxt.Req.Host
	}
	d.Origin = origin
	if cxt.Sess != nil {
		d.CSRFToken = cxt.Sess.CsrfToken()
		d.UserEmail, d.UserID = core.PageUserSlots(cxt.Sess)
	}
	return d
}
