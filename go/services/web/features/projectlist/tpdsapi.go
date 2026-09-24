//go:build !nocgo

package projectlist

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf16"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
)

// TPDS / Dropbox-GitHub-sync private-API write endpoints (router.mjs,
// privateApiRouter + requirePrivateApiAuth). The target deployment USES
// Dropbox/GitHub sync, so these are part of the 1:1 drop-in (Node :3000).
//
// Node sources (oracle):
// TpdsController.createProject (POST /user/:user_id/project/new):
//
//	parseReq(createProjectSchema={params.user_id:zz.objectId, body.projectName?:string},
//	  logOnly) -> generateUniqueName(user_id, projectName) -> createBlankProject(
//	  user_id, name, {}, {skipCreatingInTPDS}) -> res.json({projectId:_id}).
//
// Pinned Node :3000 wire (2026-09-23):
//
//	unauth / wrong-cred                -> 401 text/plain "Unauthorized" (12B) + WA + XPB
//	user_id !24hex (notanoid, all-digit) -> 404 JSON `params.user_id` (91B) + XPB
//	valid uid + valid name             -> 200 JSON {"projectId":"<24hex>"} (40B) + XPB  (CREATES a BLANK project)
//	valid uid + invalid name           -> 500 text/plain "Internal Server Error" (21B) + XPB
//	  (invalid = absent/empty/too-long/slash/backslash/leading-or-trailing-whitespace;
//	   the TPDS path does NOT map name-validation errors to 4xx — Node falls through to
//	   the Express 500; no project is created)
//
// The 200 path reuses the P4.7 blank-creation primitives (crInsertProject blank
// shape — rootFolder with empty docs/fileRefs, no main.tex/docstore — + crInitHistory).
var tpdsProjectNewPat = regexp.MustCompile(`^/user/([^/]+)/project/new$`)

// tpdsNameOK — mirrors Node validateProjectName for the TPDS path: any failure
// (absent / blank / >150 UTF-16 / contains '/' / contains '\' / leading-or-
// trailing whitespace) rejects the create (Node surfaces it as the Express 500).
func tpdsNameOK(present bool, name string) bool {
	if !present {
		return false
	}
	if name == "" {
		return false
	}
	if len(utf16.Encode([]rune(name))) > 150 {
		return false
	}
	if strings.ContainsAny(name, "/\\") {
		return false
	}
	if name != strings.TrimSpace(name) {
		return false
	}
	return true
}

// tpdsPlain500 — Node's Express 500 for an unmapped error (name validation in the
// TPDS path): text/plain "Internal Server Error" (21B) + XPB + weak ETag (==
// "15-…" since 21 == 0x15).
func tpdsPlain500(r *core.Res) {
	r.W.Header().Set("X-Powered-By", "Express")
	r.W.Header().Set("Content-Type", "text/plain; charset=utf-8")
	r.W.Header().Set("Content-Length", "21")
	r.W.Header().Set("ETag", core.EtagWeakBody("Internal Server Error"))
	r.W.WriteHeader(500)
	_, _ = r.W.Write([]byte("Internal Server Error"))
}

func tpdsCreateProjectHandler(a *core.App) func(c *core.Cxt, r *core.Res) {
	return func(c *core.Cxt, r *core.Res) {
		req := c.Req
		if c.A.Cfg.Profile != "api" {
			// Defensive: core.App skips APIOnly routes on the web profile, so this
			// is unreachable on :4000; if it ever is, 404 (Node web :4000 404s it).
			r.JSON(404, delParamVA("user_id"))
			return
		}
		mm := tpdsProjectNewPat.FindStringSubmatch(req.URL.Path)
		if mm == nil {
			return // unreachable (dispatch pattern-gates)
		}
		uidHex := mm[1]
		if !a.APIBasicGate401(c, r, req) {
			return // unauth / wrong basic → 401 challenge wire
		}
		if !delHex24(uidHex) {
			// Node expressify 404-VA (res.json path → X-Powered-By).
			r.W.Header().Set("X-Powered-By", "Express")
			r.JSON(404, delParamVA("user_id"))
			return
		}
		uid := strings.ToLower(uidHex)

		// Read the body and detect `projectName` presence (Node schema is
		// non-strict; only projectName is used).
		raw, _ := io.ReadAll(io.LimitReader(req.Body, 1<<20))
		var bodyObj map[string]json.RawMessage
		present := false
		var name string
		if len(bytes.TrimSpace(raw)) > 0 {
			if err := json.Unmarshal(raw, &bodyObj); err == nil {
				if v, ok := bodyObj["projectName"]; ok {
					present = true
					_ = json.Unmarshal(v, &name)
				}
			}
		}
		if !tpdsNameOK(present, name) {
			tpdsPlain500(r) // Node: invalid name → Express 500 (no project created)
			return
		}

		// generateUniqueName (ensure-unique against the owner's project names) +
		// createBlankProject (BLANK: rootFolder w/ empty docs/fileRefs, no main.tex).
		unique := nzipEnsureUnique(nzipUserNames(a, c, uid), name)
		sp := "en"
		if u, okU := loadOwnerUser(a, c, uid); okU && u.spellCheckLanguage != "" {
			sp = u.spellCheckLanguage
		}
		pj := primitive.NewObjectID()
		rootID := primitive.NewObjectID()
		crInsertProject(a, c, pj, rootID, nil, unique, uid, sp, "pdflatex", bson.A{}, bson.A{}, 0)
		crInitHistory(c, pj.Hex())

		r.W.Header().Set("X-Powered-By", "Express")
		r.JSON(200, []byte(`{"projectId":"`+pj.Hex()+`"}`))
	}
}

// resolveProject (POST /user/:user_id/project/resolve) ---------------------
//
// Node TpdsController.resolveProject (router.mjs:1041): parseReq(strict
// {projectId:zz.objectId} .or {projectName:min1}, NOT logOnly) ->
// getOrCreateProject(user_id, projectId, projectName) ->
//
//	project==null -> 200 {"status":"rejected"}
//	else          -> 200 {"status":"success",projectId,historyId,otMigrationStage??0}
//
// getOrCreateProject = if projectId -> findProjectByIdWithRWAccess(owner/RW +
//
//	not archived/trashed) else getOrCreateProjectByName (find owned/RW by
//	case-insensitive name; none->createBlankProject; all archived/trashed->null;
//	>1->duplicate->null; 1 active->it).
//
// Pinned Node :3000 wire (2026-09-23):
//
//	unauth / wrong          -> 401 (12B) + WA + XPB
//	user_id !24hex          -> 404 JSON VA `params.user_id` (91B) + XPB
//	{projectId:!24hex}      -> 400 JSON `Invalid Mongo ObjectId at "body.projectId"` (91B)
//	{} (no keys)            -> 400 JSON ..."received undefined at body.projectId or ...body.projectName" (197B)
//	{projectName:""}         -> 400 JSON `Too small: ...>=1 characters at "body.projectName"` (120B)
//	{projectId,projectName}  -> 400 JSON `Unrecognized key ...` (139B)
//	{projectId:ghost}        -> 200 {"status":"rejected"} (21B)
//	{projectName:existing}   -> 200 {"status":"success","projectId","historyId","otMigrationStage":0} (119B)
//	{projectName:new-name}   -> 200 success (CREATES a BLANK project named exactly that name)
//
// historyId == the project _id (Node initializeProject(_id) returns _id); otMigrationStage 0.
var tpdsProjectResolvePat = regexp.MustCompile(`^/user/([^/]+)/project/resolve$`)

func dgetOID(pd primitive.D, key string) (primitive.ObjectID, bool) {
	for _, kv := range pd {
		if kv.Key == key {
			if o, ok := kv.Value.(primitive.ObjectID); ok {
				return o, true
			}
			return primitive.ObjectID{}, false
		}
	}
	return primitive.ObjectID{}, false
}

func darrContainsOID(pd primitive.D, key string, uid primitive.ObjectID) bool {
	for _, kv := range pd {
		if kv.Key == key {
			if arr, ok := kv.Value.(primitive.A); ok {
				for _, v := range arr {
					if o, ok2 := v.(primitive.ObjectID); ok2 && o == uid {
						return true
					}
				}
			}
			return false
		}
	}
	return false
}

func dspellLang(pd primitive.D) string {
	var sb strings.Builder
	for _, kv := range pd {
		if kv.Key == "name" {
			if s, ok := kv.Value.(string); ok {
				sb.WriteString(s)
			}
			return sb.String()
		}
	}
	return ""
}

// tpdsProjectActive — !isArchivedOrTrashed(project, uid): neither the
// project's `archived` nor `trashed` user-id array contains uid.
func tpdsProjectActive(pd primitive.D, uid primitive.ObjectID) bool {
	return !darrContainsOID(pd, "archived", uid) && !darrContainsOID(pd, "trashed", uid)
}

// tpdsOwnedOrRWProjects — Node findUsersProjectsByName's candidate set:
// owner_ref==uid concat collaborator_refs contains uid, deduped by _id.
func tpdsOwnedOrRWProjects(a *core.App, c *core.Cxt, uid primitive.ObjectID) []primitive.D {
	if a.Mongo == nil {
		return nil
	}
	ctx := c.Req.Context()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []primitive.D
	push := func(coll primitive.D) {
		if o, ok := dgetOID(coll, "_id"); ok && !seen[o.Hex()] {
			seen[o.Hex()] = true
			out = append(out, coll)
		}
	}
	for _, key := range []string{"owner_ref", "collaborator_refs"} {
		cur, _ := db.Collection("projects").Find(ctx, bson.D{{Key: key, Value: uid}})
		var list []primitive.D
		_ = cur.All(ctx, &list)
		for _, d := range list {
			push(d)
		}
	}
	return out
}

func tpdsVal400(msg string) []byte {
	return []byte(`{"error":"Validation error: ` + msg + `","statusCode":400}`)
}

func tpdsResolveProjectHandler(a *core.App) func(c *core.Cxt, r *core.Res) {
	return func(c *core.Cxt, r *core.Res) {
		req := c.Req
		if c.A.Cfg.Profile != "api" {
			r.JSON(404, delParamVA("user_id")) // defensive (APIOnly skipped on web)
			return
		}
		mm := tpdsProjectResolvePat.FindStringSubmatch(req.URL.Path)
		if mm == nil {
			return
		}
		uidHex := mm[1]
		if !a.APIBasicGate401(c, r, req) {
			return // 401
		}
		if !delHex24(uidHex) {
			r.W.Header().Set("X-Powered-By", "Express")
			r.JSON(404, delParamVA("user_id"))
			return
		}
		uid, _ := primitive.ObjectIDFromHex(strings.ToLower(uidHex))

		// Body branch (strict {projectId} .or {projectName}).
		raw, _ := io.ReadAll(io.LimitReader(req.Body, 1<<20))
		var bodyObj map[string]json.RawMessage
		if len(bytes.TrimSpace(raw)) > 0 {
			_ = json.Unmarshal(raw, &bodyObj)
		}
		hasPid, hasName := false, false
		var pidRaw, nameRaw json.RawMessage
		if bodyObj != nil {
			if v, ok := bodyObj["projectId"]; ok {
				hasPid, pidRaw = true, v
			}
			if v, ok := bodyObj["projectName"]; ok {
				hasName, nameRaw = true, v
			}
		}
		pidOK := func() bool { // pid is a 24-hex string
			var s string
			if json.Unmarshal(pidRaw, &s) != nil || !delHex24(s) {
				return false
			}
			return true
		}()
		nameOK := func() bool {
			var s string
			return json.Unmarshal(nameRaw, &s) == nil && len(s) > 0
		}()

		switch {
		case hasPid && !hasName:
			if !pidOK {
				r.W.Header().Set("X-Powered-By", "Express")
				r.JSON(400, tpdsVal400(`Invalid Mongo ObjectId at \"body.projectId\"`))
				return
			}
			o, _ := primitive.ObjectIDFromHex(strings.ToLower(strings.Trim(string(pidRaw), `"`)))
			var pd primitive.D
			ctx := req.Context()
			db, err := a.Mongo.DB(ctx)
			if err == nil {
				err = db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: o}}).Decode(&pd)
			}
			if err != nil || pd == nil {
				r.W.Header().Set("X-Powered-By", "Express")
				r.JSON(200, []byte(`{"status":"rejected"}`)) // ghost -> null -> rejected
				return
			}
			owner, _ := dgetOID(pd, "owner_ref")
			if owner != uid && !darrContainsOID(pd, "collaborator_refs", uid) {
				r.W.Header().Set("X-Powered-By", "Express")
				r.JSON(200, []byte(`{"status":"rejected"}`))
				return
			}
			if !tpdsProjectActive(pd, uid) {
				r.W.Header().Set("X-Powered-By", "Express")
				r.JSON(200, []byte(`{"status":"rejected"}`))
				return
			}
			tpdsResolved200(r, pd)
		case hasName && !hasPid:
			if !nameOK {
				r.W.Header().Set("X-Powered-By", "Express")
				r.JSON(400, tpdsVal400(`Too small: expected string to have >=1 characters at \"body.projectName\"`))
				return
			}
			var nameStr string
			_ = json.Unmarshal(nameRaw, &nameStr)
			pd, ok := tpdsGetOrCreateByName(a, c, uid, nameStr)
			if !ok {
				r.W.Header().Set("X-Powered-By", "Express")
				r.JSON(200, []byte(`{"status":"rejected"}`))
				return
			}
			tpdsResolved200(r, pd)
		case !hasPid && !hasName:
			r.W.Header().Set("X-Powered-By", "Express")
			r.JSON(400, tpdsVal400(`Invalid input: expected string, received undefined at \"body.projectId\" or Invalid input: expected string, received undefined at \"body.projectName\"`))
			return
		default: // both keys present — zod .or() joins the matched branches' errors:
			// branch1 {projectId}: [invalid-pid? (only when pid !24hex), "Unrecognized key: projectName"]
			//  + " or " + branch2 {projectName}: ["Unrecognized key: projectId"].
			// (A missing-required-key branch contributes only when no branch
			//  matches — see res-empty-body above.)
			var b1 []string
			if !pidOK {
				b1 = append(b1, `Invalid Mongo ObjectId at \"body.projectId\"`)
			}
			b1 = append(b1, `Unrecognized key: \"projectName\" at \"body\"`)
			msg := strings.Join(b1, "; ") + ` or ` + `Unrecognized key: \"projectId\" at \"body\"`
			r.W.Header().Set("X-Powered-By", "Express")
			r.JSON(400, tpdsVal400(msg))
			return
		}
	}
}

func tpdsResolved200(r *core.Res, pd primitive.D) {
	pid, _ := dgetOID(pd, "_id")
	// Node: historyId = project.overleaf?.history?.id (the key is OMITTED when
	// undefined — fixtures inserted without `overleaf` have no historyId);
	// otMigrationStage = project.overleaf?.history?.otMigrationStage ?? 0.
	histStr := ""
	histPresent := false
	var otm int
	if v, ok := dpath(pd, "overleaf"); ok {
		if hd, ok2 := v.(primitive.D); ok2 {
			if hv, ok3 := dpath(hd, "history"); ok3 {
				if hd2, ok4 := hv.(primitive.D); ok4 {
					if iv, ok5 := dpath(hd2, "id"); ok5 {
						if s, ok6 := iv.(string); ok6 && len(s) == 24 {
							histStr = s
							histPresent = true
						} else if o, ok6 := iv.(primitive.ObjectID); ok6 {
							histStr = o.Hex()
							histPresent = true
						}
					}
					if sv, ok5 := dpath(hd2, "otMigrationStage"); ok5 {
						switch n := sv.(type) {
						case int32:
							otm = int(n)
						case int64:
							otm = int(n)
						case int:
							otm = n
						case float64:
							otm = int(n)
						}
					}
				}
			}
		}
	}
	var sb strings.Builder
	sb.WriteString(`{"status":"success","projectId":"` + pid.Hex() + `"`)
	if histPresent {
		sb.WriteString(`,"historyId":"` + histStr + `"`)
	}
	sb.WriteString(`,"otMigrationStage":` + fmt.Sprint(otm) + `}`)
	r.W.Header().Set("X-Powered-By", "Express")
	r.JSON(200, []byte(sb.String()))
}

// tpdsGetOrCreateByName — Node getOrCreateProjectByName: find owned/RW projects
// by case-insensitive name; none -> createBlankProject (exact name); all
// archived/trashed -> null; >1 -> duplicate -> null; exactly one active -> it.
func tpdsGetOrCreateByName(a *core.App, c *core.Cxt, uid primitive.ObjectID, name string) (primitive.D, bool) {
	cands := tpdsOwnedOrRWProjects(a, c, uid)
	lower := strings.ToLower(name)
	var matches []primitive.D
	for _, pd := range cands {
		if strings.ToLower(dspellLang(pd)) == lower {
			matches = append(matches, pd)
		}
	}
	if len(matches) == 0 {
		// createBlankProject(uid, name) — exact name (NOT uniquified in this path).
		pj := primitive.NewObjectID()
		rootID := primitive.NewObjectID()
		sp := "en"
		if u, okU := loadOwnerUser(a, c, uid.Hex()); okU && u.spellCheckLanguage != "" {
			sp = u.spellCheckLanguage
		}
		crInsertProject(a, c, pj, rootID, nil, name, uid.Hex(), sp, "pdflatex", bson.A{}, bson.A{}, 0)
		crInitHistory(c, pj.Hex())
		// The created project's overleaf.history.id is its own _id (crInsertProject
		// sets it; Node: initializeProject(project._id) returns project._id). Node's
		// resolveProject therefore INCLUDES historyId (== projectId) for a freshly
		// created project, so mirror that: build the resolved doc with the history id
		// so tpdsResolved200 emits historyId. (An earlier unconditional bare-`_id`
		// fallback here discarded it — the P7 hard-cutover exposed this parity gap on
		// the uapi `res-new-name` create leg before the GET legs matched it.)
		return bson.D{
			{Key: "_id", Value: pj},
			{Key: "overleaf", Value: bson.D{{Key: "history", Value: bson.D{
				{Key: "id", Value: pj.Hex()},
			}}}},
		}, true
	}
	var active []primitive.D
	for _, pd := range matches {
		if tpdsProjectActive(pd, uid) {
			active = append(active, pd)
		}
	}
	if len(active) == 0 {
		return primitive.D{}, false // all archived/trashed -> rejected
	}
	if len(matches) > 1 {
		return primitive.D{}, false // duplicate -> rejected
	}
	return active[0], true
}
