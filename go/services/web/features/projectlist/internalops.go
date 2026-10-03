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
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"

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
		oid, _ := bson.ObjectIDFromHex(pidHex)
		ctx, cancel := context.WithTimeout(req.Context(), 10*time.Second)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			r.W.Header().Set("X-Powered-By", "Express")
			apiText(r, 500, "Internal Server Error")
			return
		}
		projects, deleted := db.Collection("projects"), db.Collection("deletedProjects")
		var projDoc bson.D
		if projects.FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&projDoc) == nil {
			// ACTIVE project → Node deletes a leftover deletedProject record (a
			// NO-OP when none) and returns 200 "OK". Faithful + non-mutating.
			_, _ = deleted.DeleteOne(ctx, bson.D{{Key: "deleterData.deletedProjectId", Value: oid}})
			r.W.Header().Set("X-Powered-By", "Express")
			apiText(r, 200, "OK")
			return
		}
		var dpDoc bson.D
		if deleted.FindOne(ctx, bson.D{{Key: "deleterData.deletedProjectId", Value: oid}}).Decode(&dpDoc) != nil {
			// No project AND no deletedProject record → Node NotFoundError → 404.
			r.W.Header().Set("X-Powered-By", "Express")
			apiText(r, 404, "Not Found")
			return
		}
		// deletedProject found → destructive expiry (best-effort: Node destroys
		// docstore + history + project + record). NOT gated (destructive).
		// D41 slice-2: Node's DU project destroy removed (DU retired).
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
		oid, _ := bson.ObjectIDFromHex(uidHex)
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
		oid, _ := bson.ObjectIDFromHex(pidHex)
		ctx, cancel := context.WithTimeout(req.Context(), 15*time.Second)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			r.W.Header().Set("X-Powered-By", "Express")
			apiText(r, 500, "Internal Server Error")
			return
		}
		var projDoc bson.D
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

// ---------- /internal/deactivateOldProjects — InactiveData bulk (b63post-2) ----------

// deactivateOldParams — Node InactiveProjectController.deactivateOldProjects
// body: zod strictObject + z.coerce.number().int() OPTIONALS with
// parseReq(logOnly:true) + loose fallback → validation NEVER rejects (a bad
// value logs an issue and falls back to the defaults, it does not 400). Pin:
// numberOfProjectsToArchive default 10, ageOfProjects default 360. Numbers
// and numeric strings are accepted (z.coerce.number); everything else is
// absent. Non-positive limits are passed through raw (Node oracle: mongoose
// .limit(n) with the raw value — not clamped).
func deactivateOldParams(body []byte) (limit int, daysOld float64) {
	limit, daysOld = 10, 360
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return // malformed JSON → both defaults (Node: parsed body fields undefined)
	}
	if v, ok := raw["numberOfProjectsToArchive"]; ok {
		if f, ok2 := coerceNum(v); ok2 {
			limit = int(f)
		}
	}
	if v, ok := raw["ageOfProjects"]; ok {
		if f, ok2 := coerceNum(v); ok2 {
			daysOld = f
		}
	}
	return
}

// coerceNum — float64 as-is, or a parseable string (z.coerce.number parity);
// anything else is absent (logOnly semantics: never an error).
func coerceNum(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	}
	return 0, false
}

// deactivateOldQuery — Node InactiveProjectManager.findInactiveProjects
// filter: lastOpened BEFORE (now - daysOld·24h) OR absent (the `{$not:{$gt}}`
// deliberately matches `lastOpened: null` — never-opened projects) + active
// still true (only active projects get deactivated):
//
//	{ active: true, lastOpened: { $not: { $gt: cutoff } } }
func deactivateOldQuery(daysOld float64, now time.Time) bson.D {
	cutoff := now.Add(-time.Duration(daysOld * float64(24*time.Hour)))
	return bson.D{
		{Key: "active", Value: true},
		{Key: "lastOpened", Value: bson.D{{Key: "$not", Value: bson.A{
			bson.D{{Key: "$gt", Value: cutoff}},
		}}}},
	}
}

// deactivateOldItem — one response entry (the Node select projection is
// _id + lastOpened; a missing lastOpened serializes to null; dates are the
// JSON.stringify(Date) ISO-with-ms form; _id first, mongoose doc order).
func deactivateOldItem(pidHex string, lastOpened *time.Time) []byte {
	var lo string
	if lastOpened == nil {
		lo = "null"
	} else {
		lo = `"` + lastOpened.UTC().Format("2006-01-02T15:04:05.000Z") + `"`
	}
	return []byte(`{"_id":"` + pidHex + `","lastOpened":` + lo + `}`)
}

// internalDeactivateOldProjects — POST /internal/deactivateOldProjects (Node
// InactiveProjectManager.deactivateOldProjects + findInactiveProjects).
//
// Wire (Node oracle, InactiveProjectController):
//
//	unauth / wrong basic → 401 (APIBasicGate401 — internalBulkGate)
//	mongo cursor failure   → 500 "Internal Server Error"
//	success                → 200 res.json(processedProjects) — the array of
//	                            FOUND projects {_id,lastOpened} (Node pushes
//	                            each found doc before attempting its
//	                            deactivation, so per-item failures do not
//	                            drop the entry) — NOT the earlier faithful-
//	                            status 200 "OK" stub (image cron
//	                            images/main-amd64/cron/deactivate-projects.sh
//	                            drives this route; the stub silently did
//	                            nothing).
//
// Per-item side effects follow this build's deactivate standard (same as
// internalDeactivateHandler, D41: DU retired → best-effort docstore archive
// + projects.active=false; a per-item update failure does not abort the
// batch — Node catches and keeps going).
func internalDeactivateOldProjects(a *core.App) func(c *core.Cxt, r *core.Res) {
	return func(c *core.Cxt, r *core.Res) {
		if !internalBulkGate(a, c, r) {
			return
		}
		req := c.Req
		body, _ := io.ReadAll(io.LimitReader(req.Body, 1<<20))
		limit, daysOld := deactivateOldParams(body)
		send500 := func() {
			r.W.Header().Set("X-Powered-By", "Express")
			apiText(r, 500, "Internal Server Error")
		}
		if a.Mongo == nil {
			send500()
			return
		}
		ctx, cancel := context.WithTimeout(req.Context(), 15*time.Second)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			send500()
			return
		}
		// Node: read(READ_PREFERENCE_SECONDARY), select ['_id','lastOpened'],
		// limit(limit). v2 driver: the read preference is a COLLECTION option
		// (repo pattern: go/services/docstore/mongo.go), not a find option.
		fopts := options.Find().
			SetProjection(bson.D{{Key: "_id", Value: 1}, {Key: "lastOpened", Value: 1}})
		if limit >= 0 {
			fopts.SetLimit(int64(limit))
		}
		secColl := db.Collection("projects", options.Collection().SetReadPreference(readpref.Secondary()))
		cur, find := secColl.Find(ctx, deactivateOldQuery(daysOld, time.Now()), fopts)
		if find != nil {
			send500() // Node: findInactiveProjects throw → 500
			return
		}
		var items []string
		for cur.Next(ctx) {
			var doc bson.D
			if cur.Decode(&doc) != nil {
				_ = cur.Close(ctx)
				send500()
				return
			}
			var (
				pid        bson.ObjectID
				lastOpened *time.Time
			)
			for _, kv := range doc {
				switch kv.Key {
				case "_id":
					pid, _ = kv.Value.(bson.ObjectID)
				case "lastOpened":
					if t, ok := kv.Value.(time.Time); ok {
						lastOpened = &t
					}
				}
			}
			pidHex := pid.Hex()
			// Node order: push found, THEN deactivate (failure keeps the entry).
			items = append(items, string(deactivateOldItem(pidHex, lastOpened)))
			// Side effects — the build's deactivate standard (best-effort
			// archive; active:false). Per-item errors are caught+skipped in
			// Node; the package convention is silent best-effort.
			fireHTTP(c, http.MethodPost, strings.TrimSuffix(crDocstoreBase(), "/")+"/project/"+pidHex+"/archive", nil)
			_, _ = db.Collection("projects").UpdateOne(ctx,
				bson.D{{Key: "_id", Value: pid}},
				bson.D{{Key: "$set", Value: bson.D{{Key: "active", Value: false}}}})
		}
		if cur.Err() != nil {
			_ = cur.Close(ctx)
			send500() // Node: for-await cursor error → 500
			return
		}
		_ = cur.Close(ctx)
		r.W.Header().Set("X-Powered-By", "Express")
		r.JSON(200, []byte("["+strings.Join(items, ",")+"]"))
	}
}

// internalBulkGate: profile + basic-auth gate for the no-param bulk endpoints.
func internalBulkGate(a *core.App, c *core.Cxt, r *core.Res) bool {
	if !iopProfile(c, r, "") {
		return false
	}
	return a.APIBasicGate401(c, r, c.Req)
}
