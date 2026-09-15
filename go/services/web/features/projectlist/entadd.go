// P4.11a — editor entity creation: POST /project/:id/doc and /folder.
//
// Node oracle (services/web/app/src/Features/):
//
//	Router        router.mjs / EditorRouter.mjs —
//	  POST /project/:Project_id/doc    ensureUserCanWriteProjectContent →
//	                                   rateLimit → EditorHttpController.addDoc
//	  POST /project/:Project_id/folder ensureUserCanWriteProjectContent →
//	                                   rateLimit → EditorHttpController.addFolder
//	Controller    EditorHttpController.mjs (trims the name; error mapping):
//	  · addDoc     name gate (0<len<150 raw) → 400 sendStatus("Bad Request")
//	              TooManyFiles → 400 json "This project has reached the
//	                             2000 file limit"; other errors → next(err)
//	              (OError → 400 text/plain with the message)
//	  · addFolder  same name gate; TooManyFiles → 400 json i18n limit text;
//	              'invalid element name' → 400 json "Invalid File Name"
//	              (the i18n string, JSON-encoded); else next(err)
//	  · anonymous (valid CSRF) → 401 "Unauthorized" (pinned 2026-09-15)
//	  · non-member → 403 {"message":"restricted"}
//	Handler       EditorController (trim) → ProjectEntityUpdateHandler:
//	  addDoc      beforeLock: !isCleanFilename(trimmed) → OError
//	              'invalid element name' (400 text/plain); docstore
//	              updateDoc POST (127.0.0.1:3016) — NO-OP state in the free
//	              build but the side-call order is pinned for parity;
//	              withLock → MongoUpdateHandler.addDoc → _putElement
//	  addFolder   straight to _putElement (no docstore call)
//	Mongo layer   ProjectEntityMongoUpdateHandler._putElement (check order):
//	  1 !isCleanFilename(name)       → 'invalid element name'
//	  2 folder resolve by id         → 404 "Page Not Found" page
//	  3 element count > 2000         → TooManyFiles (mapped by controller)
//	  4 fs path (folder/name) >1024  → 'path too long'
//	  5 top-level doc|file blocked   → 'blocked element name'
//	  6 duplicate name (doc|file|folder in the target folder)
//	                                    → 'file already exists'
//	  7 write  $push <folderpath>.<docs|folders>, $inc version,
//	            $set lastUpdated/lastUpdatedBy, filter <folderpath>:{$exists}
//
// Pinned live (2026-09-15, probes p411a_probe{1,2,3,4} + gate leg 1):
//
//	addDoc  200 {"name":...,"_id":"<24hex>"} / trim ' lead  '→'lead' /
//	      'toString' top 400 'blocked element name' / 'toString' in
//	      subfolder 200 / dup (doc|file|folder sibling) 400 'file already
//	      exists' / a*b, a\b, ctrl, '..', spaces-only 400 'invalid element
//	      name' / len>=150 400 text/plain 'Bad Request' / bad parent 400 VA /
//	      absent-folder parent 404 page / ghost 404 page / anon+valid 401.
//	addFold 200 {"name","_id","docs":[],"fileRefs":[],"folders":[]} /
//	      '../escape' 400 json "Invalid File Name" / ' constructor' 200
//	      (trimmed; folders exempt from blocked words) / dup 400
//	      'file already exists' / spaces-only 400 json "Invalid File Name".
//
// Not replicated: updateProjectStructure (document-updater history
// pipeline) — verified no-op in this free build (0 rows docHistory /
// docSnapshots / docHistoryIndex after the Node battery).
package projectlist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var (
	entDocPat    = regexp.MustCompile(`^/project/([^/]+)/doc$`)
	entFolderPat = regexp.MustCompile(`^/project/([^/]+)/folder$`)

	// SafePath.mjs BADCHAR_RX (lone surrogates from JSON escapes decode to
	// U+FFFD in Go, outside this set — no divergence for well-formed UTF-8).
	entBadChar = regexp.MustCompile(`[/\\\x00-\x1F\x7F\x80-\x9F*]`)
	// SafePath.mjs BADFILE_RX: exactly "." / "..", leading/trailing space.
	entBadFile = regexp.MustCompile(`^\.$|^\.\.$|^\s+|\s+$`)

	entMaxPath = 1024 // SafePath MAX_PATH
	entMaxEnt  = 2000 // Settings.maxEntitiesPerProject default
)

// BLOCKEDFILE_RX (top-level docs/files only; folders exempt).
var entBlocked = map[string]bool{
	"prototype": true, "constructor": true, "toString": true,
	"toLocaleString": true, "valueOf": true, "hasOwnProperty": true,
	"isPrototypeOf": true, "propertyIsEnumerable": true,
	"__defineGetter__": true, "__lookupGetter__": true,
	"__defineSetter__": true, "__lookupSetter__": true, "__proto__": true,
}

func entCleanName(name string) bool {
	if name == "" || len([]rune(name)) > entMaxPath {
		return false
	}
	if entBadChar.MatchString(name) {
		return false
	}
	return !entBadFile.MatchString(name)
}

func entHex24(s string) bool {
	if len(s) != 24 {
		return false
	}
	return regexp.MustCompile(`^[0-9a-fA-F]{24}$`).MatchString(s)
}

type entElement struct {
	name  string
	idHex string
}

type entFolder struct {
	name  string
	idHex string
	docs  []entElement
	files []entElement
	fold  []entFolder
}

func entFld(d any, key string) any {
	switch t := d.(type) {
	case primitive.D:
		for _, e := range t {
			if e.Key == key {
				return e.Value
			}
		}
	case map[string]any:
		return t[key]
	}
	return nil
}

func entArr(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case primitive.A:
		return t
	}
	return nil
}

func entIsDocObj(v any) bool {
	switch v.(type) {
	case primitive.D, primitive.M, map[string]any:
		return true
	}
	return false
}

func entParseTree(v any) []entFolder {
	arr := entArr(v)
	if arr == nil {
		return nil
	}
	out := make([]entFolder, 0, len(arr))
	for _, fv := range arr {
		if !entIsDocObj(fv) {
			continue
		}
		fld := entFolder{name: asStr(entFld(fv, "name")), idHex: oidHex(entFld(fv, "_id"))}
		if dv := entArr(entFld(fv, "docs")); dv != nil {
			for _, x := range dv {
				if entIsDocObj(x) {
					fld.docs = append(fld.docs, entElement{name: asStr(entFld(x, "name")), idHex: oidHex(entFld(x, "_id"))})
				}
			}
		}
		if fv2 := entArr(entFld(fv, "fileRefs")); fv2 != nil {
			for _, x := range fv2 {
				if entIsDocObj(x) {
					fld.files = append(fld.files, entElement{name: asStr(entFld(x, "name")), idHex: oidHex(entFld(x, "_id"))})
				}
			}
		}
		fld.fold = entParseTree(entFld(fv, "folders"))
		out = append(out, fld)
	}
	return out
}

func entFindLoc(root []entFolder, idHex string) (string, string, *entFolder, bool) {
	type item struct {
		f     []entFolder
		mongo string
		fs    string
	}
	stack := []item{{f: root, mongo: "rootFolder", fs: ""}}
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for i, f := range it.f {
			mong := fmt.Sprintf("%s.%d", it.mongo, i)
			if f.idHex == idHex {
				return mong, it.fs, &f, true
			}
			if len(f.fold) > 0 {
				stack = append(stack, item{
					f: f.fold, mongo: mong + ".folders", fs: it.fs + "/" + f.name,
				})
			}
		}
	}
	return "", "", nil, false
}

// ---------- docstore (Node: DocstoreManager.updateDoc) ----------

func entDocstoreURL() string {
	h := os.Getenv("DOCSTORE_HOST")
	if h == "" {
		h = "127.0.0.1"
	}
	return "http://" + h + ":3016"
}

var entHTTP = &http.Client{Timeout: 30 * time.Second}

func entUpdateDoc(projectHex, docHex string) bool {
	u := entDocstoreURL() + "/project/" + projectHex + "/doc/" + docHex
	body, _ := json.Marshal(map[string]any{"lines": []any{}, "version": 0, "ranges": map[string]any{}})
	req, err := http.NewRequest("POST", u, bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("content-type", "application/json")
	resp, err := entHTTP.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	code := resp.StatusCode
	return code >= 200 && code < 300
}

// ---------- handler ----------

func addEntityHandler(a *core.App, kind string) func(*core.Cxt, *core.Res) {
	isDoc := kind == "doc"
	return func(cxt *core.Cxt, res *core.Res) {
		uid := ""
		if cxt.Sess != nil {
			uid = cxt.Sess.UserIDHex()
		}
		if uid == "" {
			// Node: anonymous → 401 sendStatus (both real + ghost project).
			res.SendStatus(401)
			return
		}
		oid, ok := paramProject(cxt.Params["1"], res)
		if !ok {
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
		if !entCanWrite(uid, *doc) {
			if core.AcceptsJSON(cxt.Req) {
				res.JSON(403, []byte(colRestricted))
			} else {
				views.Restricted403(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
			}
			return
		}

		// strict body {name?:string, parent_folder_id?:oid|null}
		raw, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
		raw = bytes.TrimSpace(raw)
		var bm map[string]any
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &bm); err != nil || bm == nil {
				res.JSON(400, []byte("{}"))
				return
			}
		} else {
			bm = map[string]any{}
		}
		var (
			name      string
			nameSet   bool
			parentHex string
		)
		for k, v := range bm {
			switch k {
			case "name":
				switch t := v.(type) {
				case nil:
					res.JSON(400, colVa("Invalid input: expected string, received null", "body.name", 400))
					return
				case string:
					name, nameSet = t, true
				default:
					res.JSON(400, colVa("Invalid input: expected string, received "+zodReceived(v, true), "body.name", 400))
					return
				}
			case "parent_folder_id":
				if v == nil {
					break
				}
				s, isStr := v.(string)
				if !isStr {
					res.JSON(400, colVa("Invalid input: expected string, received "+zodReceived(v, true), "body.parent_folder_id", 400))
					return
				}
				if !entHex24(s) {
					res.JSON(400, colVa("Invalid Mongo ObjectId", "body.parent_folder_id", 400))
					return
				}
				parentHex = strings.ToLower(s)
			default:
				res.JSON(400, colVa("Unrecognized key: \""+k+"\"", "body", 400))
				return
			}
		}

		// HTTP-layer name gate (raw, pre-trim): 0 < len < 150 → 400.
		if !nameSet || name == "" || len(name) >= 150 {
			res.SendStatus(400)
			return
		}
		name = strings.Trim(name, " \t\n\r") // EditorController addDoc/addFolder trim

		// resolve the target folder (Node: _confirmFolder → ProjectLocator)
		root := entParseTree(dget(*doc, "rootFolder"))
		rf := dget(*doc, "rootFolder")
		log.Printf("entadd: rfType=%T rfNil=%v docKeys=%v\n", rf, rf == nil, func() []string {
			var k []string
			for _, e := range *doc {
				k = append(k, e.Key)
			}
			return k
		}())
		if len(root) == 0 {
			views.Error500Page(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
			return
		}
		var mongoPath, fsPath string
		var foldElem *entFolder
		if parentHex == "" {
			mongoPath, fsPath = "rootFolder.0", ""
			f0 := root[0]
			foldElem = &f0
		} else {
			mp, fp, f, found := entFindLoc(root, parentHex)
			if !found {
				views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
				return
			}
			mongoPath, fsPath, foldElem = mp, fp, f
		}

		// _putElement checks, Node order. Error shape differs per kind:
		// doc  -> 400 text/plain "invalid element name"
		// folder -> 400 application/json ""Invalid File Name"" (i18n)
		if !entCleanName(name) {
			if isDoc {
				res.PlainText(400, "invalid element name")
			} else {
				res.JSON(400, []byte(`"Invalid File Name"`))
			}
			return
		}
		newDocID := primitive.NewObjectID()
		newFoldID := primitive.NewObjectID()
		if isDoc {
			// docstore updateDoc — before the remaining checks, so
			// blocked/duplicate 400s leave the same side calls as Node.
			if !entUpdateDoc(strings.ToLower(oidHex(oid)), newDocID.Hex()) {
				views.Error500Page(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
				return
			}
		}
		if entCountElements(root) > entMaxEnt {
			// TooManyFilesError, controller-mapped to the i18n string
			// (JSON-encoded string body).
			res.JSON(400, []byte(`"This project has reached the 2000 file limit"`))
			return
		}
		if len([]rune(fsPath+"/"+name)) > entMaxPath {
			res.PlainText(400, "path too long")
			return
		}
		// blocked top-level doc names (folders exempt).
		topLevel := fsPath == ""
		if topLevel && !isDoc {
			// folders are never blocked (Node _blockedFilename).
		} else if topLevel && entBlocked[name] {
			res.PlainText(400, "blocked element name")
			return
		}
		for _, d := range foldElem.docs {
			if d.name == name {
				res.PlainText(400, "file already exists")
				return
			}
		}
		for _, f := range foldElem.files {
			if f.name == name {
				res.PlainText(400, "file already exists")
				return
			}
		}
		for _, f := range foldElem.fold {
			if f.name == name {
				res.PlainText(400, "file already exists")
				return
			}
		}

		// write (Node _putElement: findOneAndUpdate $push/$inc/$set)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		db, merr := a.Mongo.DB(ctx)
		if merr != nil {
			views.Error500Page(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
			return
		}
		var element bson.D
		if isDoc {
			element = bson.D{{Key: "name", Value: name}, {Key: "_id", Value: newDocID}}
		} else {
			element = bson.D{
				{Key: "name", Value: name},
				{Key: "_id", Value: newFoldID},
				{Key: "docs", Value: bson.A{}},
				{Key: "fileRefs", Value: bson.A{}},
				{Key: "folders", Value: bson.A{}},
			}
		}
		seg := "docs"
		if !isDoc {
			seg = "folders"
		}
		ure, err := db.Collection("projects").UpdateOne(ctx,
			bson.D{
				{Key: "_id", Value: oid},
				{Key: mongoPath, Value: bson.D{{Key: "$exists", Value: true}}},
			},
			bson.D{
				{Key: "$push", Value: bson.D{{Key: mongoPath + "." + seg, Value: element}}},
				{Key: "$inc", Value: bson.D{{Key: "version", Value: 1}}},
				{Key: "$set", Value: bson.D{
					{Key: "lastUpdated", Value: time.Now()},
					{Key: "lastUpdatedBy", Value: mustObjectID(uid)},
				}},
			})
		if err != nil || ure.MatchedCount == 0 {
			views.Error500Page(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
			return
		}

		if isDoc {
			res.JSON(200, []byte(`{"name":`+jstr(name)+`,"_id":"`+newDocID.Hex()+`"}`))
		} else {
			res.JSON(200, []byte(`{"name":`+jstr(name)+`,"_id":"`+newFoldID.Hex()+`","docs":[],"fileRefs":[],"folders":[]}`))
		}
	}
}

// entCanWrite: Node canUserWriteProjectContent for token-less sessions —
// owner_ref or collab_refs membership (readAndWrite|owner). Reviewer and
// readOnly refs are NOT write access.
func entCanWrite(uidHex string, doc primitive.D) bool {
	own := oidHex(dget(doc, "owner_ref"))
	if own != "" && own == uidHex {
		return true
	}
	for _, c := range entArr(dget(doc, "collab_refs")) {
		if h := oidHex(c); h == uidHex {
			return true
		}
	}
	return false
}

func entCountElements(root []entFolder) int {
	var walk func(f *entFolder) int
	walk = func(f *entFolder) int {
		n := len(f.docs) + len(f.files)
		for _, s := range f.fold {
			n += walk(&s)
		}
		return n
	}
	n := 0
	for i := range root {
		n += walk(&root[i])
	}
	return n
}
