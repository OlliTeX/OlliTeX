package projectlist

import (
	"context"
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

// P4.5 — POST /project/:Project_id/rename  (project rename). Node sources
// (oracle, pinned live 2026-09-14):
//
//	router.mjs:798  webRouter.post('/project/:Project_id/rename',
//	                          requireLogin, ensureUserCanAdminProject, renameProject)
//	ProjectController.renameProject
//	  -> EditorController.renameProject -> ProjectDetailsHandler.renameProject
//	       newName = newName.trim(); validateProjectName(newName) [blank -> 400]
//	       Project.updateOne({_id}, { name: newName })        (NO audit entry)
//	       TpdsUpdateSender.moveEntity(...)                   (external no-op here)
//	  -> res.sendStatus(200)                                  (body "OK", text/plain)
//
// Contract (live oracle):
//
//   - 200 text/plain  body "OK" (2 bytes) — project `name` set to the trimmed value
//   - present, not admin  -> 403  json {"message":"restricted"} | html Restricted
//     (guard = canUserAdminProject = owner || site-admin; ADMIN_PRIVILEGE_AVAILABLE=true)
//   - valid id, project absent -> 404 HTML general/404 (NOT accept-dep)
//   - INVALID (non-hex) id     -> 404 application/json malformed (NOT accept-dep)
//   - blank name (trim -> "")  -> 400 text/plain "Project name cannot be blank"
//   - body.newProjectName missing / non-string -> 400 application/json
//     {"error":"Validation error: Invalid input: expected string, received <T> at \"body.newProjectName\"","statusCode":400}
//   - anon  accept json -> 401 ; accept html -> 302 /login
//
// Validation order (faithful to Node):  401 (anon) > 404 (absent/malformed)
//
//	> 403 (not admin) > 400 (body) > 200 (OK). No audit log entry for rename.
var renPat = regexp.MustCompile(`^/project/([^/]+)/rename$`)

// zodReceived maps a decoded body.newProjectName value to zod's
// "expected string, received <T>" label (Node zod v3/v4 type names).
func zodReceived(v any, present bool) string {
	if !present {
		return "undefined"
	}
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case int:
		return "number"
	case int64:
		return "number"
	case primitive.A:
		return "array"
	case nil:
		return "null"
	default:
		return "object"
	}
}

func renameHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
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
		relPath := strings.TrimPrefix(cxt.Req.URL.Path, "/")

		if !validOID.MatchString(param) {
			res.JSON(404, []byte(malformed404))
			return
		}
		oid, err := primitive.ObjectIDFromHex(strings.ToLower(param))
		if err != nil {
			res.JSON(404, []byte(malformed404))
			return
		}
		doc, lerr := loadProject(a, cxt, oid)
		if lerr != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		if doc == nil {
			views.NotFoundPage(res.W, pageBase(cxt, relPath))
			return
		}
		if !canAdmin(uid, loadUserAdmin(a, cxt, uid), *doc) {
			if core.AcceptsJSON(cxt.Req) {
				res.JSON(403, []byte(`{"message":"restricted"}`))
			} else {
				views.Restricted403(res.W, pageBase(cxt, relPath))
			}
			return
		}

		// body: { newProjectName: "<string>" }
		raw, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
		var bm map[string]any
		if len(strings.TrimSpace(string(raw))) > 0 {
			_ = json.Unmarshal(raw, &bm)
		}
		var nameVal any
		present := false
		if bm != nil {
			if v, ok := bm["newProjectName"]; ok {
				present = true
				nameVal = v
			}
		}
		if !present {
			res.JSON(400, []byte(`{"error":"Validation error: Invalid input: expected string, received `+zodReceived(nameVal, false)+` at \"body.newProjectName\"","statusCode":400}`))
			return
		}
		s, ok := nameVal.(string)
		if !ok {
			res.JSON(400, []byte(`{"error":"Validation error: Invalid input: expected string, received `+zodReceived(nameVal, true)+` at \"body.newProjectName\"","statusCode":400}`))
			return
		}
		newName := strings.TrimSpace(s)
		if newName == "" {
			res.PlainText(400, "Project name cannot be blank")
			return
		}

		// Project.updateOne({_id}, { name: newName }) — Node does not emit an
		// audit entry for a rename.
		writeRename(a, cxt, oid, newName)
		res.SendStatus(200)
	}
}

// writeRename sets the project's name.
func writeRename(a *core.App, cxt *core.Cxt, oid primitive.ObjectID, name string) {
	if a.Mongo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 8*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return
	}
	_, _ = db.Collection("projects").UpdateOne(ctx,
		bson.D{{Key: "_id", Value: oid}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "name", Value: name}}}})
}
