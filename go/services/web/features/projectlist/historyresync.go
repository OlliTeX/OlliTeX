// historyresync.go — U-API, POST /project/:Project_id/history/resync (Node
// HistoryRouter, privateApiRouter only → APIOnly; Node
// HistoryController.resyncProjectHistory → ProjectEntityUpdateHandler.
// resyncProjectHistory(wrapWithLock) → ProjectGetter + overleaf.history.id
// check + _checkFiletree + DocumentUpdaterHandler.resyncProjectHistory
// (DU POST /project/{pid}/history/resync) [+ setHistoryRangesSupport]).
//
// Wire pinned Node api :3000 (2026-09-24), full-header, non-204 legs only
// (the history-enabled 204 is heavy/cross-service — NOT live-driven in the
// gate, best-effort DU resync in Go, same standard as deactivate):
//
//	unauth / wrong basic            → 401 "Unauthorized" (APIBasicGate401)
//	params.Project_id not hex24     → 404 JSON VA "Invalid Mongo ObjectId at \"params.Project_id\""
//	body invalid                    → 400 JSON VA (see below), joined "; " in order
//	params+body both bad            → 404 (param precedence), param-first
//	ghost project                   → 500 text/plain "Internal Server Error" (Node quirk:
//	                                              getProject→null→TypeError→throw→500)
//	overleaf.history.id absent      → 404 text/plain "Not Found" (ProjectHistoryDisabledError)
//	overleaf.history.id present     → 204 (best-effort DU resync; not gated)
//
// Body schema (strict): historyRangesMigration : z.enum(['forwards','backwards'])
//   .optional() (present + not in enum →
//   `Invalid option: expected one of "forwards"|"backwards" at "body.historyRangesMigration"`;
//   explicit null also fails); resyncProjectStructureOnly : z.boolean().default(false)
//   (present + not boolean →
//   `Invalid input: expected boolean, received <type> at "body.resyncProjectStructureOnly"`);
//   unknown key → `Unrecognized key: "<k>" at "body"`. Join order (zod declaration):
//   historyRangesMigration, resyncProjectStructureOnly, unknown-key(s).

package projectlist

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

var rsyncPat = regexp.MustCompile(`^/project/([^/]+)/history/resync$`)

func resyncHistoryHandler(a *core.App) func(c *core.Cxt, r *core.Res) {
	return func(c *core.Cxt, r *core.Res) {
		req := c.Req
		mm := rsyncPat.FindStringSubmatch(req.URL.Path)
		if mm == nil {
			views.NotFoundPage(r.W, pageBase(c, strings.TrimPrefix(req.URL.Path, "/")))
			return
		}
		pidHex := mm[1]
		if c.A.Cfg.Profile != "api" {
			// APIOnly → web profile skips; Node wires this on privateApiRouter only.
			r.W.Header().Set("X-Powered-By", "Express")
			r.JSON(404, delParamVA("Project_id"))
			return
		}
		if !a.APIBasicGate401(c, r, req) {
			return // 401 challenge wire
		}

		// --- validation (params.Project_id : body), param-first precedence ---
		body, tag, isObj := apReadBody(req)
		var segs []string
		paramBad := false
		if !delHex24(pidHex) {
			paramBad = true
			segs = append(segs, `Invalid Mongo ObjectId at \"params.Project_id\"`)
		}
		if isObj {
			if unk, ok := apUnknown(body, "body", "historyRangesMigration", "resyncProjectStructureOnly"); !ok {
				segs = append(segs, unk)
			}
			if v, has := body.M["historyRangesMigration"]; has {
				if s, ok := v.(string); !(ok && (s == "forwards" || s == "backwards")) {
					segs = append(segs, `Invalid option: expected one of \"forwards\"|\"backwards\" at \"body.historyRangesMigration\"`)
				}
			}
			if v, has := body.M["resyncProjectStructureOnly"]; has {
				if _, ok := v.(bool); !ok {
					segs = append(segs, `Invalid input: expected boolean, received `+apType(v)+` at \"body.resyncProjectStructureOnly\"`)
				}
			}
		} else {
			segs = append(segs, `Invalid input: expected object, received `+tag+` at \"body\"`)
		}
		if len(segs) > 0 {
			status := 400
			if paramBad {
				status = 404
			}
			r.W.Header().Set("X-Powered-By", "Express")
			r.JSON(status, joinVA(status, segs...))
			return
		}

		// --- load project: ghost→500 (Node quirk), history-disabled→404, else 204 ---
		oid, _ := primitive.ObjectIDFromHex(pidHex)
		ctx, cancel := context.WithTimeout(req.Context(), 10*time.Second)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			// Node: getProject throws / unavailable → 500.
			r.W.Header().Set("X-Powered-By", "Express")
			apiText(r, 500, "Internal Server Error")
			return
		}
		var pd primitive.D
		if err := db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&pd); err != nil {
			// ghost → Node 500 "Internal Server Error" (getProject null → TypeError → throw)
			r.W.Header().Set("X-Powered-By", "Express")
			apiText(r, 500, "Internal Server Error")
			return
		}
		hidVal, _ := dpath(pd, "overleaf", "history", "id")
		hid := ""
		switch x := hidVal.(type) {
		case string:
			hid = x
		case primitive.ObjectID:
			hid = x.Hex()
		}
		if hid == "" {
			// ProjectHistoryDisabledError → res.sendStatus(404) → text/plain "Not Found"
			r.W.Header().Set("X-Powered-By", "Express")
			apiText(r, 404, "Not Found")
			return
		}

		// --- history-enabled → best-effort DU resync → 204 (NOT gated; heavy) ---
		fireHTTP(c, http.MethodPost, strings.TrimSuffix(cduBase(), "/")+"/project/"+oid.Hex()+"/history/resync", rsyncDUBody(hid, body.M))
		// Node res.sendStatus(204): empty body, weak ETag over "No Content", XPB.
		r.W.Header().Set("X-Powered-By", "Express")
		r.W.Header().Set("ETag", core.EtagWeakBody("No Content"))
		r.NoContent()
	}
}

// rsyncDUBody — best-effort minimal DU resync body (projectHistoryId + any
// present options). Node also sends a computed docs/files filetree payload
// (heavy cross-service build); this mirrors the 204 wire + the DU POST, best-
// effort on the payload (same standard as deactivate's best-effort side effects).
func rsyncDUBody(hid string, m map[string]any) []byte {
	var sb strings.Builder
	sb.WriteString(`{"projectHistoryId":"` + hid + `","docs":[],"files":[]`)
	if v, ok := m["historyRangesMigration"].(string); ok {
		sb.WriteString(`,"historyRangesMigration":"` + v + `"`)
	}
	if v, ok := m["resyncProjectStructureOnly"].(bool); ok {
		if v {
			sb.WriteString(`,"resyncProjectStructureOnly":true`)
		} else {
			sb.WriteString(`,"resyncProjectStructureOnly":false`)
		}
	}
	sb.WriteString(`}`)
	return []byte(sb.String())
}
