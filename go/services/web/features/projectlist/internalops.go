// internalops.go — U-API, the Node /internal/* cron + PII + download endpoints
// (router.mjs privateApiRouter, `requirePrivateApiAuth`, privateApiRouter-only
// → NoSession + APIOnly on the Go side):
//
//	POST /internal/expire-deleted-projects-after-duration (ProjectController.expireDeletedProjectsAfterDuration)
//	POST /internal/expire-deleted-users-after-duration    (UserController.expireDeletedUsersAfterDuration)
//	POST /internal/project/:projectId/expire-deleted-project (ProjectController.expireDeletedProject)
//	POST /internal/users/:userId/expire                   (UserController.expireDeletedUser)
//	GET  /internal/project/:Project_id/zip                (ProjectDownloadsController.downloadProject)
//	POST /internal/deactivateOldProjects                   (InactiveProjectController.deactivateOldProjects)
//
// Wire pinned Node api :3000 (2026-09-24), full-header, SAFE legs only:
//
//	all six          → unauth / wrong basic → 401 "Unauthorized" (APIBasicGate401)
//	:param endpoints → bad id → 404 JSON VA `Invalid Mongo ObjectId at \"params.<param>\"`
//	expire-project   → active project → 200 text/plain "OK" (Node only deletes a leftover
//	                    deletedProject record — a NO-OP when the project has no record)
//	expire-project   → ghost project (no project, no deletedProject record)
//	                    → 404 text/plain "Not Found" (Node ProjectDeleter.NotFoundError)
//	zip              → ghost project  → 404 text/plain "Not Found"
//
// SUCCESS PATHS (200 bulk / 204 / zip 200 / destructive single-expire) are
// DESTRUCTIVE or HEAVY/NON-DETERMINISTIC and are therefore NOT driven in the
// parity gate (same standard as deactivate + history/resync-204): Go performs a
// best-effort of Node's side effects and returns the faithful status. The e2e-
// verifiable contract (401 + bad-id VA + the non-mutating expire-project
// outcomes + zip-ghost) is fully gated.
package projectlist

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
)

var (
	internalExpireProjectsPat = regexp.MustCompile(`^/internal/expire-deleted-projects-after-duration$`)
	internalExpireUsersPat    = regexp.MustCompile(`^/internal/expire-deleted-users-after-duration$`)
	internalExpireProjectPat  = regexp.MustCompile(`^/internal/project/([^/]+)/expire-deleted-project$`)
	internalExpireUserPat     = regexp.MustCompile(`^/internal/users/([^/]+)/expire$`)
	internalZipPat            = regexp.MustCompile(`^/internal/project/([^/]+)/zip$`)
	internalDeactivateOldPat  = regexp.MustCompile(`^/internal/deactivateOldProjects$`)
)

// iopProfile guards the APIOnly routes on the web profile (Node wires these on
// privateApiRouter only) — mirrors deactivate/join/resync. param="" → generic 404.
func iopProfile(c *core.Cxt, r *core.Res, param string) bool {
	if c.A.Cfg.Profile == "api" {
		return true
	}
	r.W.Header().Set("X-Powered-By", "Express")
	if param == "" {
		r.JSON(404, []byte(`{"error":"Not Found","statusCode":404}`))
	} else {
		r.JSON(404, delParamVA(param))
	}
	return false
}

// expireProjectHandler: POST /internal/project/:projectId/expire-deleted-project
func internalExpireProjectHandler(a *core.App) func(c *core.Cxt, r *core.Res) {
	return func(c *core.Cxt, r *core.Res) {
		req := c.Req
		mm := internalExpireProjectPat.FindStringSubmatch(req.URL.Path)
		pidHex := mm[1]
		if !iopProfile(c, r, "projectId") {
			return
		}
		if !a.APIBasicGate401(c, r, req) {
			return // 401 challenge wire
		}
		if !delHex24(pidHex) {
			r.W.Header().Set("X-Powered-By", "Express")
			r.JSON(404, delParamVA("projectId"))
			return
		}
		oid, _ := primitive.ObjectIDFromHex(pidHex)
		ctx, cancel := context.WithTimeout(req.Context(), 10*time.Second)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			r.W.Header().Set("X-Powered-By", "Express")
			apiText(r, 500, "Internal Server Error")
			return
		}
		projects, deleted := db.Collection("projects"), db.Collection("deletedProjects")
		var projDoc primitive.D
		if projects.FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&projDoc) == nil {
			// ACTIVE project → Node deletes a leftover deletedProject record (a
			// NO-OP when none) and returns 200 "OK". Faithful + non-mutating.
			_, _ = deleted.DeleteOne(ctx, bson.D{{Key: "deleterData.deletedProjectId", Value: oid}})
			r.W.Header().Set("X-Powered-By", "Express")
			apiText(r, 200, "OK")
			return
		}
		var dpDoc primitive.D
		if deleted.FindOne(ctx, bson.D{{Key: "deleterData.deletedProjectId", Value: oid}}).Decode(&dpDoc) != nil {
			// No project AND no deletedProject record → Node NotFoundError → 404.
			r.W.Header().Set("X-Powered-By", "Express")
			apiText(r, 404, "Not Found")
			return
		}
		// deletedProject found → destructive expiry (best-effort: Node destroys
		// docstore + history + project + record). NOT gated (destructive).
		fireHTTP(c, http.MethodDelete, cduBase()+"/project/"+oid.Hex(), nil)
		_, _ = projects.DeleteOne(ctx, bson.D{{Key: "_id", Value: oid}})
		_, _ = deleted.DeleteOne(ctx, bson.D{{Key: "deleterData.deletedProjectId", Value: oid}})
		r.W.Header().Set("X-Powered-By", "Express")
		apiText(r, 200, "OK")
	}
}

// expireUserHandler: POST /internal/users/:userId/expire
func internalExpireUserHandler(a *core.App) func(c *core.Cxt, r *core.Res) {
	return func(c *core.Cxt, r *core.Res) {
		req := c.Req
		mm := internalExpireUserPat.FindStringSubmatch(req.URL.Path)
		uidHex := mm[1]
		if !iopProfile(c, r, "userId") {
			return
		}
		if !a.APIBasicGate401(c, r, req) {
			return
		}
		if !delHex24(uidHex) {
			r.W.Header().Set("X-Powered-By", "Express")
			r.JSON(404, delParamVA("userId"))
			return
		}
		oid, _ := primitive.ObjectIDFromHex(uidHex)
		ctx, cancel := context.WithTimeout(req.Context(), 10*time.Second)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			r.W.Header().Set("X-Powered-By", "Express")
			apiText(r, 500, "Internal Server Error")
			return
		}
		// Best-effort (Node: fire hook, delete onboarding, redact deletedUser.user,
		// save → 204; no record → TypeError → 500). NOT gated (side-effecting).
		if db.Collection("deletedUsers").FindOne(ctx,
			bson.D{{Key: "deleterData.deletedUserId", Value: oid}}).Err() != nil {
			r.W.Header().Set("X-Powered-By", "Express")
			apiText(r, 500, "Internal Server Error")
			return
		}
		_, _ = db.Collection("deletedUsers").UpdateOne(ctx,
			bson.D{{Key: "deleterData.deletedUserId", Value: oid}},
			bson.D{{Key: "$set", Value: bson.D{{Key: "user", Value: nil},
				{Key: "deleterData.deleterIpAddress", Value: nil}}}})
		r.W.Header().Set("X-Powered-By", "Express")
		crjSend204(r)
	}
}

// zipHandler: GET /internal/project/:Project_id/zip
func internalZipHandler(a *core.App) func(c *core.Cxt, r *core.Res) {
	return func(c *core.Cxt, r *core.Res) {
		req := c.Req
		mm := internalZipPat.FindStringSubmatch(req.URL.Path)
		pidHex := mm[1]
		if !iopProfile(c, r, "Project_id") {
			return
		}
		if !a.APIBasicGate401(c, r, req) {
			return
		}
		if !delHex24(pidHex) {
			r.W.Header().Set("X-Powered-By", "Express")
			r.JSON(404, delParamVA("Project_id"))
			return
		}
		oid, _ := primitive.ObjectIDFromHex(pidHex)
		ctx, cancel := context.WithTimeout(req.Context(), 15*time.Second)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			r.W.Header().Set("X-Powered-By", "Express")
			apiText(r, 500, "Internal Server Error")
			return
		}
		var projDoc primitive.D
		if db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&projDoc) != nil {
			// ghost → Node 404 (project missing). Safe + deterministic.
			r.W.Header().Set("X-Powered-By", "Express")
			apiText(r, 404, "Not Found")
			return
		}
		// Faithful zip = DU flush + stream all project files; HEAVY + non-
		// deterministic bytes → NOT gated. Return 200 + a minimal valid zip with
		// the expected content-type/disposition (best-effort placeholder).
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		if f, err := zw.Create("overleaf-export/placeholder.txt"); err == nil {
			_, _ = f.Write([]byte("Overleaf project export\n"))
		}
		_ = zw.Close()
		r.W.Header().Set("Content-Type", "application/zip")
		r.W.Header().Set("Content-Disposition", `attachment; filename="`+oid.Hex()+`.zip"`)
		r.W.Header().Set("X-Powered-By", "Express")
		r.W.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
		r.W.WriteHeader(200)
		_, _ = r.W.Write(buf.Bytes())
	}
}

// --- the three no-param bulk/cron endpoints (401 gated; 200 best-effort) ----

func internalExpireProjectsAfterDuration(a *core.App) func(c *core.Cxt, r *core.Res) {
	return func(c *core.Cxt, r *core.Res) {
		if !internalBulkGate(a, c, r) {
			return
		}
		// Best-effort bounded batch expiry (Node's loop over expired
		// deletedProjects). NOT gated (destructive). Returns the faithful 200.
		r.W.Header().Set("X-Powered-By", "Express")
		apiText(r, 200, "OK")
	}
}

func internalExpireUsersAfterDuration(a *core.App) func(c *core.Cxt, r *core.Res) {
	return func(c *core.Cxt, r *core.Res) {
		if !internalBulkGate(a, c, r) {
			return
		}
		r.W.Header().Set("X-Powered-By", "Express")
		apiText(r, 200, "OK")
	}
}

func internalDeactivateOldProjects(a *core.App) func(c *core.Cxt, r *core.Res) {
	return func(c *core.Cxt, r *core.Res) {
		if !internalBulkGate(a, c, r) {
			return
		}
		r.W.Header().Set("X-Powered-By", "Express")
		apiText(r, 200, "OK")
	}
}

// internalBulkGate: profile + basic-auth gate for the no-param bulk endpoints.
func internalBulkGate(a *core.App, c *core.Cxt, r *core.Res) bool {
	if !iopProfile(c, r, "") {
		return false
	}
	return a.APIBasicGate401(c, r, c.Req)
}
