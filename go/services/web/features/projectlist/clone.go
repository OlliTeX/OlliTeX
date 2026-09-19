package projectlist

// P4.9 — project clone: POST /Project/:Project_id/clone
//
// Node sources (oracle, pinned live 2026-09-15):
//
//	router.mjs:792  webRouter.post('/Project/:Project_id/clone',
//	                AuthorizationMiddleware.ensureUserCanReadProject,
//	                ProjectController.cloneProject)
//	cloneProjectSchema (zod, strict):
//	  params.Project_id: objectId
//	  body: { projectName?: string, isDebugCopy?: boolean, cloneHistory?: boolean,
//	            cloneRanges?: boolean, tags?: [{ id: objectId }] }
//
//	Controller (ProjectController.mjs:398-461):
//	  - non-admin (hasAdminAccess): isDebugCopy/cloneHistory/cloneRanges forced false
//	  - duplicate(currentUser, projectId, projectName, tags, opts)  (ProjectDuplicator)
//	  - audit 'project-cloned' (NO-OP in free build)
//	  - 200 application/json
//      { name, lastUpdated, project_id, owner_ref,
//        owner: { first_name, last_name, email, _id } }
//	  - FileTooLargeError (413) / next(err) -> generic 500 page
//
//	Duplicate engine (ProjectDuplicator.mjs, non-admin e2e path):
//	  1. flushProjectToMongo(src)   = POST {document-updater}/project/{src}/flush
//	  2. read source project (rootFolder entries, compiler, rootDoc path)
//	  3. hooks preDuplicateProject  (no listeners -> no-op)
//	  4. `projectName.trim()` — **Node quirk: missing projectName (body {}) ->
//     TypeError -> 500 "Something went wrong" page** (pinned)
//	  5. createBlankProject(owner, name, attributes) — attributes: segmentation
//     (ANALYTICS ONLY, not persisted — pinned: no `segmentation` key in the new
//     doc), overleaf/imageName/isDebugCopyOf (absent for v2-history free e2e)
//  => a plain blank project doc (same 30-key shape as P4.7 basic)
//	  6. setCompiler(new, source.compiler) = projects $set compiler
//	  7. copy docs: docstore updateDoc(new, newDocId, lines, 0, {}) per source doc
//	  8. copy files: HistoryManager.copyBlob = POST {v1}/projects/{new}/blobs/{hash}
//     ?copyFrom={src}  (basicAuth staging:$V1_HISTORY_PASSWORD)
//  => cloned fileRef: {name, created, rev:0, hash, _id} — NOTE: NO
//     `linkedFileData` key (present-and-null in the example-create path; the
//     clone File only gets it when the source file's is non-null)
//	  9. createNewFolderStructure: $set rootFolder + $inc version (-> 1)
//	  10. setRootDoc: projects $set rootDoc_id = copied root doc (main.tex)
//	  11. updateProjectStructure (sendProjectStructureOps unset in this build ->
//     no-op), TpdsProjectFlusher (no providers -> no-op), tags (empty -> no-op),
//     clsi cache prep (fire-and-forget perf -> deferred, as in P4.7b)
//
//	Live-oracle pins (A/B, node side):
//	  200 application/json {name,lastUpdated,project_id,owner_ref,owner:{...}}
//	  {}                     -> 500 text/html "Something went wrong" page (Node bug)
//	  {projectName:"   "}    -> 400 text/plain "Project name cannot be blank"
//	  {projectName:"a/b"}    -> 400 text/plain "Project name cannot contain / characters"
//	  {unknownKey:1}         -> 400 application/json Unrecognized key at "body"
//	  {projectName:5}        -> 400 application/json expected string, received number
//	  [1]                    -> 400 application/json expected object, received array
//	  missing project        -> 404 HTML NotFound page (canRead middleware)
//	  non-member             -> 403 {"message":"restricted"}
//	  anonymous              -> 403 "Forbidden" (CSRF first)
//	  state: source project/docstore/v1 blob UNCHANGED; new project 30 keys;
//	  docs copied (118/10 lines); frog blob in new project (md5 665777aa6c7c..., 97080B)

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

var clonePat = regexp.MustCompile(`^/Project/([^/]+)/clone$`)

type clParseResult struct {
	bare    bool   // express.json strict: bare 400 application/json {}
	zodMsg  string // 400 application/json validation error (joined)
	name    string
	hasName bool
}

func clParseBody(raw []byte) clParseResult {
	if len(bytes.TrimSpace(raw)) == 0 {
		return clParseResult{} // {} — projectName absent (Node: 500 at .trim())
	}
	if !json.Valid(raw) {
		return clParseResult{bare: true}
	}
	switch trimmed := bytes.TrimLeft(raw, " \t\r\n"); trimmed[0] {
	case '[':
		return clParseResult{zodMsg: `Invalid input: expected object, received array at "body"`}
	case '{':
	default:
		return clParseResult{bare: true} // number/string/null/boolean root
	}

	var bm map[string]any
	_ = json.Unmarshal(raw, &bm)
	keys := crOrderedKeys(raw)
	present := func(k string) bool { _, ok := bm[k]; return ok }

	var errs []string
	if present("projectName") {
		if _, ok := bm["projectName"].(string); !ok {
			errs = append(errs, `Invalid input: expected string, received `+crZodType(bm["projectName"])+` at "body.projectName"`)
		}
	}
	for _, k := range []string{"isDebugCopy", "cloneHistory", "cloneRanges"} {
		if present(k) {
			if _, ok := bm[k].(bool); !ok {
				errs = append(errs, `Invalid input: expected boolean, received `+crZodType(bm[k])+` at "body.`+k+`"`)
			}
		}
	}
	if present("tags") {
		if _, ok := bm["tags"].([]any); !ok {
			errs = append(errs, `Invalid input: expected array, received `+crZodType(bm["tags"])+` at "body.tags"`)
		}
	}
	var unrecognized []string
	for _, k := range keys {
		switch k {
		case "projectName", "isDebugCopy", "cloneHistory", "cloneRanges", "tags":
		default:
			unrecognized = append(unrecognized, k)
		}
	}
	if len(unrecognized) == 1 {
		errs = append(errs, `Unrecognized key: "`+unrecognized[0]+`" at "body"`)
	} else if len(unrecognized) > 1 {
		quoted := make([]string, len(unrecognized))
		for i, k := range unrecognized {
			quoted[i] = `"` + k + `"`
		}
		errs = append(errs, "Unrecognized keys: "+strings.Join(quoted, ", ")+` at "body"`)
	}
	if len(errs) > 0 {
		return clParseResult{zodMsg: strings.Join(errs, "; ")}
	}
	res := clParseResult{}
	if present("projectName") {
		res.name = bm["projectName"].(string)
		res.hasName = true
	}
	return res
}

// clDocLines fetches one doc's lines from the docstore (same endpoint the
// Node clone's getAllDocs reads).
func clDocLines(cxt *core.Cxt, src, docID string) []string {
	resp, err := crHTTP.Get(strings.TrimSuffix(crDocstoreBase(), "/") + "/project/" + src + "/doc/" + docID)
	if err != nil {
		return []string{}
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var out struct {
		Lines []string `json:"lines"`
	}
	if json.Unmarshal(b, &out) == nil {
		return out.Lines
	}
	return []string{}
}

// clCopyBlob mirrors HistoryManager.copyBlob:
// POST {v1}/projects/{target}/blobs/{hash}?copyFrom={source} (basicAuth).
func clCopyBlob(cxt *core.Cxt, src, target, hash string) bool {
	u := strings.TrimSuffix(crV1HistoryBase(), "/") + "/projects/" + target + "/blobs/" + url.PathEscape(hash) + "?copyFrom=" + url.QueryEscape(src)
	req, err := http.NewRequestWithContext(cxt.Req.Context(), "POST", u, nil)
	if err != nil {
		return false
	}
	req.SetBasicAuth(crV1HistoryUser(), crV1HistoryPass())
	resp, err := crHTTP.Do(req)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// clFlushProject is the duplicate engine's first step (flush OT to mongo).
// Best-effort here (the service is up in e2e; Node would 500, which the
// healthy stack never reaches).
func clFlushProject(cxt *core.Cxt, src string) {
	fireHTTP(cxt, "POST", strings.TrimSuffix(cduBase(), "/")+"/project/"+src+"/flush", nil)
}

type cloneOwner struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	ID        string `json:"_id"`
}

type cloneResp struct {
	Name        string     `json:"name"`
	LastUpdated time.Time  `json:"lastUpdated"`
	ProjectID   string     `json:"project_id"`
	OwnerRef    string     `json:"owner_ref"`
	Owner       cloneOwner `json:"owner"`
}

func cloneProjectHandler(a *core.App) func(*core.Cxt, *core.Res) {
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
		if !validOID.MatchString(param) {
			res.JSON(404, []byte(malformedMsg("Project_id")))
			return
		}
		oid, err := primitive.ObjectIDFromHex(strings.ToLower(param))
		if err != nil {
			res.JSON(404, []byte(malformedMsg("Project_id")))
			return
		}
		doc, lerr := loadProjectFull(a, cxt, oid)
		if lerr != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		if doc == nil {
			views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
			return
		}
		// ensureUserCanAdminProject on clone? NO — ensureUserCanReadProject.
		if !canRead(uid, loadUserAdmin(a, cxt, uid), *doc) {
			if core.AcceptsJSON(cxt.Req) {
				res.JSON(403, []byte(`{"message":"restricted"}`))
			} else {
				views.Restricted403(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
			}
			return
		}

		rawBody, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
		pr := clParseBody(rawBody)
		switch {
		case pr.bare:
			res.JSON(400, []byte("{}"))
		case pr.zodMsg != "":
			res.JSON(400, crZodReply(pr.zodMsg))
		case !pr.hasName:
			// Node quirk (ProjectDuplicator.mjs:130 `newProjectName.trim()`):
			// missing projectName -> TypeError -> 500 generic error page.
			if a.Render500 != nil {
				a.Render500(cxt, res)
			} else {
				res.SendStatus(500)
			}
		default:
			// Node trims the name before the blank/slash checks (and creates
			// under the trimmed name).
			pr.name = strings.TrimSpace(pr.name)
		}
		if pr.bare || pr.zodMsg != "" || !pr.hasName {
			return
		}
		if msg, bad := crNameError(pr.name); bad {
			res.PlainText(400, msg)
			return
		}

		// ---- duplicate engine (non-admin e2e path) ----
		srcHex := oid.Hex()
		clFlushProject(cxt, srcHex)

		// source entries (array order preserved)
		var srcDocs []primitive.D
		var srcFiles []primitive.D
		if rfArr, ok := dget(*doc, "rootFolder").(primitive.A); ok {
			if len(rfArr) > 0 {
				if rf, ok := rfArr[0].(primitive.D); ok {
					if docs, ok := dget(rf, "docs").(primitive.A); ok {
						for _, dm := range docs {
							if m, ok := dm.(primitive.D); ok {
								srcDocs = append(srcDocs, m)
							}
						}
					}
					if files, ok := dget(rf, "fileRefs").(primitive.A); ok {
						for _, fm := range files {
							if m, ok := fm.(primitive.D); ok {
								srcFiles = append(srcFiles, m)
							}
						}
					}
				}
			}
		}

		owner, _ := loadOwnerUser(a, cxt, uid)

		// new project ids
		pid := primitive.NewObjectID()
		rootID := primitive.NewObjectID()
		now := time.Now().UTC()

		docsOut := bson.A{}
		rootDocSet := false
		rootDocID := primitive.ObjectID{}
		for _, sd := range srcDocs {
			newDocID := primitive.NewObjectID()
			dname := asStr(dget(sd, "name"))
			dcid := asStr(dget(sd, "_id"))
			if dcid == "" {
				if v, ok := dget(sd, "_id").(primitive.ObjectID); ok {
					dcid = v.Hex()
				}
			}
			docsOut = append(docsOut, bson.D{
				{Key: "name", Value: dname},
				{Key: "_id", Value: newDocID},
			})
			lines := clDocLines(cxt, srcHex, dcid)
			crCreateDocRevision(cxt, pid, newDocID, lines)
			if !rootDocSet {
				// first doc is the root doc for the e2e fixtures (main.tex)
				rootDocID = newDocID
				rootDocSet = true
			}
		}

		filesOut := bson.A{}
		for _, sf := range srcFiles {
			fname := asStr(dget(sf, "name"))
			fhash := asStr(dget(sf, "hash"))
			if fhash == "" {
				if v, ok := dget(sf, "hash").(string); ok {
					fhash = v
				}
			}
			fr := primitive.D{
				{Key: "name", Value: fname},
				{Key: "created", Value: now},
				{Key: "rev", Value: 0},
			}
			// linkedFileData only when the source file's is non-null/undefined
			// (Node: if (sourceFile.linkedFileData != null) file.linkedFileData = ...)
			lv := dget(sf, "linkedFileData")
			if lv != nil {
				fr = append(fr, bson.E{Key: "linkedFileData", Value: lv})
			}
			fr = append(fr,
				bson.E{Key: "hash", Value: fhash},
				bson.E{Key: "_id", Value: primitive.NewObjectID()})
			filesOut = append(filesOut, fr)
			clCopyBlob(cxt, srcHex, pid.Hex(), fhash)
		}

		// rootDoc: the source's rootDoc if found among the copied docs, else
		// the first copied doc (Node: findRootDoc -> main.tex for these fixtures).
		var rootDoc primitive.ObjectID
		if srcRoot, ok := dget(*doc, "rootDoc_id").(primitive.ObjectID); ok {
			for i, sd := range srcDocs {
				if v, _ := dget(sd, "_id").(primitive.ObjectID); v == srcRoot && i < len(docsOut) {
					if dm, ok := docsOut[i].(bson.D); ok {
						if nv, ok := dget(dm, "_id").(primitive.ObjectID); ok {
							rootDoc = nv
						}
					}
				}
			}
		}
		if rootDoc == (primitive.ObjectID{}) {
			rootDoc = rootDocID
		}

		// compiler inherited from the source (setCompiler step)
		compiler := asStr(dget(*doc, "compiler"))
		if compiler == "" {
			compiler = "pdflatex"
		}

		// version 1: Node's clone runs a single createNewFolderStructure ($inc version 1)
		// on the blank project, regardless of entry count (pinned: clone oracle version = 1).
		crInsertProject(a, cxt, pid, rootID, &rootDoc, pr.name, uid, owner.spellCheckLanguage, "pdflatex", docsOut, filesOut, 1)
		// Node setCompiler: $set compiler from source (blank project default is
		// pdflatex anyway; the source's value wins).
		if a.Mongo != nil {
			ctx, cancel := context.WithTimeout(cxt.Req.Context(), 5*time.Second)
			defer cancel()
			if db, err := a.Mongo.DB(ctx); err == nil {
				_, _ = db.Collection("projects").UpdateOne(ctx,
					bson.D{{Key: "_id", Value: pid}},
					bson.D{{Key: "$set", Value: bson.D{{Key: "compiler", Value: compiler}}}})
			}
		}

		out, _ := json.Marshal(cloneResp{
			Name:        pr.name,
			LastUpdated: now,
			ProjectID:   pid.Hex(),
			OwnerRef:    uid,
			Owner: cloneOwner{
				FirstName: owner.first,
				LastName:  owner.last,
				Email:     owner.email,
				ID:        uid,
			},
		})
		res.JSON(200, out)
	}
}
