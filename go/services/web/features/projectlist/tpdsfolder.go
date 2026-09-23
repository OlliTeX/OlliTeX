//go:build !nocgo

package projectlist

// POST /tpds/folder-update (TPDS/Dropbox-sync folder create; router.mjs
// privateApiRouter + requirePrivateApiAuth). The target deployment USES
// Dropbox/GitHub sync, so this is part of the 1:1 drop-in of Node :3000.
//
// Node sources (oracle):
//
//	TpdsController.updateFolder (TpdsController.mjs:175):
//	  parseReq(updateFolderSchema={userId:zz.objectId, projectId:optional,
//	    path:z.string}, logOnly) -> splitPath(projectId, path) ->
//	    TpdsUpdateHandler.createFolder(userId, projectId, projectName, filePath)
//	    -> metadata==null ? conflict('Could not create folder') : res.json.
//
//	TpdsUpdateHandler.createFolder (TpdsUpdateHandler.mjs:198):
//	  getOrCreateProject(uid, projectId, projectName) == null -> null (409);
//	  FileTypeManager.shouldIgnore(path) -> null (409);
//	  UpdateMerger.createFolder -> EditorController.mkdirp ->
//	    ProjectEntityMongoUpdateHandler.mkdirp (per-segment find-or-addFolder).
//
//	splitPath (TpdsController.mjs:289) — path = Path.join('/', path) FIRST
//	  (resolves `..`/`.`/dup-slashes, e.g. /a/../b -> /b, //a//b -> /a/b), then:
//	    projectId? -> filePath=path, projectName=''
//	    no id, single segment -> filePath='/', projectName=that segment
//	    no id, multi segment -> firstName=projectName, rest=filePath.
//
// Pinned Node :3000 wire (2026-09-23):
//
//	unauth / wrong            -> 401 text/plain "Unauthorized" (12B) + WA + XPB + strict-CSP
//	{!userId, !path}          -> 400 JSON `...undefined at "body.userId"; ...at "body.path"` (185B)
//	{userId only}             -> 400 JSON `...undefined at "body.path"` (114B)
//	{path only}               -> 400 JSON `...undefined at "body.userId"` (116B)
//	{userId:bad-oid}          -> 400 JSON `Invalid Mongo ObjectId at "body.userId"` (88B)
//	project ghost / not-RW     -> 409 HTML 767B "Could not create folder" (nonce-CSP + XPB)
//	shouldIgnore(path)        -> 409 HTML 767B (e.g. /sub/.git, /.a/.b, /x.aux)
//	valid path                 -> 200 JSON {entityId,projectId,path,folderId} (deepest + parent)
//	  path="/" (or /a/../b->/b, "", nameonly) -> entityId=rootFolder, folderId=null
//	  (by-name + no project -> CREATES a blank project named that (get-or-create) -> 200)
//
// Reuses the P4.7/TPDS shared core from tpdsapi.go (getOrCreate-by-id,
// tpdsGetOrCreateByName, active check) + the P4.11b folder primitive upMkdirp
// (upload.go) seeded at the rootFolder; Node's addFolder shape (name/_id/docs/
// fileRefs/folders) is exactly upMkdirp's pushed shape.
import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/minimatch"
	"ollitex/go/services/web/core"
)

var tpdsFolderUpdatePat = regexp.MustCompile(`^/tpds/folder-update$`)

// tpdsFileIgnorePatternDefault — Node services/web/config/settings.defaults.js:943
// default (process.env.FILE_IGNORE_PATTERN || "..."). The running Node app uses
// this exact string (no FILE_IGNORE_PATTERN env in this deployment) — byte-pinned
// via FileTypeManager.shouldIgnore (minimatch {dot, nocase}).
const tpdsFileIgnorePatternDefault = `**/{{__MACOSX,.git,.texpadtmp,.R,.venv,venv}{,/**},.!(latexmkrc),*.{dvi,aux,log,toc,out,pdfsync,synctex,synctex(busy),fdb_latexmk,fls,nlo,ind,glo,gls,glg,bbl,blg,doc,docx,gz,swp}}`

// tpdsShouldIgnore — FileTypeManager.shouldIgnore: new Minimatch(
// Settings.fileIgnorePattern, {nocase,dot}).match(path). Reuses the Go
// byte-exact minimatch 10.2.6 port (1:1 with Node).
func tpdsShouldIgnore(p string) bool {
	pat := os.Getenv("FILE_IGNORE_PATTERN")
	if pat == "" {
		pat = tpdsFileIgnorePatternDefault
	}
	m, err := minimatch.New(pat, &minimatch.Options{Nocase: true, Dot: true})
	if err != nil {
		return false
	}
	return m.Match(p)
}

// tpdsConflict409 — Node HttpErrorHandler.conflict('Could not create folder')
// error page (views/general/500 with the message box). 767B byte-exact;
// content-type text/html, X-Powered-By Express, per-render nonce-CSP.
const tpdsConflict409 = `<!DOCTYPE html><html lang="en"><head><title>Something went wrong</title><link rel="icon" href="/favicon.ico"></head><body class="full-height"><main class="content content-alt full-height" id="main-content"><div class="container full-height"><div class="error-container full-height"><div class="error-details"><p class="error-status">Something went wrong, sorry.</p><p class="error-description">There was a problem with your request.
The error is:</p><p class="error-box">Could not create folder</p><p class="error-description">Please go back and try again.
If the problem persists, please contact us at
<a href="mailto:undefined" target="_blank"></a>.</p><p class="error-actions"><a class="btn btn-primary" href="/">Home</a></p></div></div></div></main></body></html>`

func tpdsFolder409(r *core.Res) {
	body := tpdsConflict409
	r.W.Header().Set("X-Powered-By", "Express")
	r.W.Header().Set("Content-Type", "text/html; charset=utf-8")
	r.W.Header().Set("Content-Security-Policy", core.CSPViewPolicy(core.NewCspNonce()))
	r.W.Header().Set("Content-Length", strconv.Itoa(len(body)))
	r.W.Header().Set("ETag", core.EtagWeakBody(body))
	r.W.WriteHeader(409)
	_, _ = r.W.Write([]byte(body))
}

// tpdsFolder200 — {entityId, projectId, path, folderId} in Node key order;
// folderId null for the root-folder (path=="/") case.
func tpdsFolder200(r *core.Res, entity string, pid primitive.ObjectID, filePath, parent string) {
	pj, _ := json.Marshal(filePath)
	fc := "null"
	if parent != "" {
		fc = `"` + parent + `"`
	}
	body := `{"entityId":"` + entity + `","projectId":"` + pid.Hex() + `","path":` + string(pj) + `,"folderId":` + fc + `}`
	r.W.Header().Set("X-Powered-By", "Express")
	r.JSON(200, []byte(body))
}

// tpdsFolderVal400 — one validation-error message (folded into the shared
// `Validation error: ...` envelope by the caller).
func tpdsFolderVal400(msgs ...string) []byte {
	return []byte(`{"error":"Validation error: ` + strings.Join(msgs, "; ") + `","statusCode":400}`)
}

// tpdsJsType — a best-effort JS `typeof` name for the non-string 400 message
// (`expected string, received <type>`); Node's Zod emits the received type.
func tpdsJsType(v json.RawMessage) string {
	trimmed := bytes.TrimSpace(v)
	switch {
	case len(trimmed) == 0:
		return "undefined"
	case trimmed[0] == 'n':
		return "null"
	case trimmed[0] == 't' || trimmed[0] == 'f':
		return "boolean"
	case trimmed[0] == '[' || trimmed[0] == '{':
		return "object"
	case trimmed[0] == '"':
		return "string"
	default:
		return "number"
	}
}

func apiFolderUpdateHandler(a *core.App) func(c *core.Cxt, r *core.Res) {
	return func(c *core.Cxt, r *core.Res) {
		req := c.Req
		if c.A.Cfg.Profile != "api" {
			// Defensive: core.App skips APIOnly routes on the web profile, so
			// this is unreachable on :4000 (Node web 404s it). 404-JSON fallback.
			r.W.Header().Set("X-Powered-By", "Express")
			r.JSON(404, delParamVA("userId"))
			return
		}
		if !a.APIBasicGate401(c, r, req) {
			return // unauth / wrong basic → 401 challenge wire (Node-matched)
		}

		// parseReq(body={userId, projectId?, path}, logOnly).
		raw, _ := io.ReadAll(io.LimitReader(req.Body, 1<<20))
		var body map[string]json.RawMessage
		if len(bytes.TrimSpace(raw)) > 0 {
			_ = json.Unmarshal(raw, &body)
		}
		uidRaw, hasUid := body["userId"]
		pidRaw, hasPid := body["projectId"]
		pathRaw, hasPath := body["path"]

		var errs []string
		var uidStr, pidStr, pathStr string
		pidValid, pathValid := false, false
		// userId (zz.objectId, required)
		if !hasUid {
			errs = append(errs, `Invalid input: expected string, received undefined at \"body.userId\"`)
		} else if e := json.Unmarshal(uidRaw, &uidStr); e != nil || !delHex24(uidStr) {
			errs = append(errs, `Invalid Mongo ObjectId at \"body.userId\"`)
		}
		// projectId (optional zz.objectId) — only validated when present.
		if hasPid {
			if e := json.Unmarshal(pidRaw, &pidStr); e == nil && delHex24(pidStr) {
				pidValid = true
			}
			// A present-but-optional-invalid projectId still resolves to a by-id
			// lookup in Node (splitPath uses its truthiness); keep pidStr as-is.
		}
		// path (z.string, required)
		if !hasPath {
			errs = append(errs, `Invalid input: expected string, received undefined at \"body.path\"`)
		} else if p, err := func() (string, error) {
			var s string
			return s, json.Unmarshal(pathRaw, &s)
		}(); err == nil {
			pathStr, pathValid = p, true
		} else {
			_ = pathStr
			errs = append(errs, `Invalid input: expected string, received `+tpdsJsType(pathRaw)+` at \"body.path\"`)
		}
		if len(errs) > 0 {
			r.W.Header().Set("X-Powered-By", "Express")
			r.JSON(400, tpdsFolderVal400(errs...))
			return
		}
		uidHex := strings.ToLower(uidStr)
		if !pathValid { // unreachable (covered above) but keep it safe
			tpdsPlain500(r)
			return
		}
		uid, _ := primitive.ObjectIDFromHex(uidHex)

		// splitPath(projectId, path) — Node Path.join('/', path) first.
		np := path.Join("/", pathStr)
		hasId := hasPid && pidValid && pidStr != ""
		var filePath, projectName string
		if hasId {
			filePath, projectName = np, ""
		} else {
			rem := np[1:]
			if i := strings.Index(rem, "/"); i == -1 {
				filePath, projectName = "/", rem
			} else {
				idx := i + 1 // index of that '/' in np
				filePath = np[idx:]
				projectName = strings.TrimPrefix(np[:idx], "/")
			}
		}

		// getOrCreateProject(uid, projectId, projectName).
		var pd primitive.D
		found := false
		if hasId {
			o, _ := primitive.ObjectIDFromHex(strings.ToLower(pidStr))
			ctx := req.Context()
			if db, err := a.Mongo.DB(ctx); err == nil {
				var doc primitive.D
				if db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: o}}).Decode(&doc) == nil && doc != nil {
					pd = doc
					ow, _ := dgetOID(pd, "owner_ref")
					found = (ow == uid || darrContainsOID(pd, "collaborator_refs", uid)) && tpdsProjectActive(pd, uid)
				}
			}
		} else if projectName != "" {
			pd, found = tpdsGetOrCreateByName(a, c, uid, projectName)
		}
		// else: no projectId + single "/" (projectName "") -> Node getOrCreate
		// by empty name -> null -> 409.
		if !found {
			tpdsFolder409(r)
			return
		}
		if tpdsShouldIgnore(filePath) {
			tpdsFolder409(r)
			return
		}

		// mkdirp (find-or-create each segment under the rootFolder).
		rootArr := entParseTree(entFld(pd, "rootFolder"))
		if len(rootArr) == 0 {
			tpdsPlain500(r) // defensive: a project with no rootFolder.
			return
		}
		rootID := rootArr[0].idHex
		if rootID == "" {
			tpdsPlain500(r)
			return
		}
		pid, _ := dgetOID(pd, "_id")
		if filePath == "/" {
			tpdsFolder200(r, rootID, pid, "/", "") // root: folderId=null
			return
		}
		lastID, lastParent, ok := tpdsMkdirp(a, uidHex, &pd, pid, filePath)
		if !ok || lastID == "" {
			tpdsPlain500(r) // Node: mkdirp error -> Express 500.
			return
		}
		tpdsFolder200(r, lastID, pid, filePath, lastParent)
	}
}

// tpdsMkdirp — faithful port of Node EditorController/ProjectEntityUpdateHandler
// + ProjectEntityMongoUpdateHandler.mkdirp for the TPDS create-folder path:
// find-or-create each path segment under the container folder (case-insensitive
// child match == Node findElementByPath default). Unlike the upload upMkdirp
// (which assumes rootFolder._id == project._id in its _id filter — true for the
// upload fixtures), here the UpdateOne filter uses the PROJECT _id (pid) so it
// is correct for ANY project (Go- and Node-created). Folder shape (name/_id/docs/
// fileRefs/folders) == Node's addFolder / upMkdirp push. Returns the deepest
// folder id + the folder that CONTAINS it (parent; "" only if the path is the
// root, which the caller special-cases).
func tpdsMkdirp(a *core.App, uidHex string, pd *primitive.D, pid primitive.ObjectID, np string) (string, string, bool) {
	root := entParseTree(entFld(*pd, "rootFolder"))
	if len(root) == 0 || root[0].idHex == "" {
		return "", "", false
	}
	seg := upSegments(np)
	if len(seg) == 0 {
		return root[0].idHex, "", false // "/" -> caller returns root + null parent
	}
	containerID := root[0].idHex
	lastID, lastParent := "", ""
	for _, name := range seg {
		rootNow := entParseTree(entFld(*pd, "rootFolder"))
		containerMp, _, ctr, fok := entFindLoc(rootNow, containerID)
		if !fok || ctr == nil {
			return lastID, lastParent, false
		}
		child := ""
		for i := range ctr.fold {
			if strings.EqualFold(ctr.fold[i].name, name) {
				child = ctr.fold[i].idHex
				break
			}
		}
		if child != "" {
			lastID, lastParent = child, containerID
			containerID = child
			continue
		}
		newID := primitive.NewObjectID()
		opCtx, opCancel := context.WithTimeout(context.Background(), 20*time.Second)
		db, derr := a.Mongo.DB(opCtx)
		if derr != nil {
			opCancel()
			return lastID, lastParent, false
		}
		pushPath := containerMp + ".folders"
		ure, err := db.Collection("projects").UpdateOne(opCtx,
			bson.D{
				{Key: "_id", Value: pid},
			},
			bson.D{
				{Key: "$push", Value: bson.D{{Key: pushPath, Value: bson.D{
					{Key: "name", Value: name},
					{Key: "_id", Value: newID},
					{Key: "docs", Value: bson.A{}},
					{Key: "fileRefs", Value: bson.A{}},
					{Key: "folders", Value: bson.A{}},
				}}}},
				{Key: "$inc", Value: bson.D{{Key: "version", Value: 1}}},
				{Key: "$set", Value: bson.D{
					{Key: "lastUpdated", Value: time.Now()},
					{Key: "lastUpdatedBy", Value: mustObjectID(uidHex)},
				}},
			})
		var ndoc primitive.D
		ferr := db.Collection("projects").FindOne(opCtx, bson.D{{Key: "_id", Value: pid}}).Decode(&ndoc)
		opCancel()
		if err != nil || ure.MatchedCount == 0 {
			return lastID, lastParent, false
		}
		if ferr != nil {
			return lastID, lastParent, false
		}
		*pd = ndoc
		lastID, lastParent = newID.Hex(), containerID
		containerID = newID.Hex()
	}
	return lastID, lastParent, true
}
