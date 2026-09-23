// U10.2b — editor entity rename / move / duplicate:
//
//	POST /project/:Project_id/:entity_type/:entity_id/rename
//	POST /project/:Project_id/:entity_type/:entity_id/move
//	POST /project/:Project_id/:entity_type/:entity_id/duplicate
//
// Node oracle (Editor/EditorRouter POST routes → EditorHttpController.
// renameEntity|moveEntity|duplicateEntity → ProjectEntityUpdateHandler →
// ProjectEntityMongoUpdateHandler → DocumentUpdaterHandler), pinned live
// 2026-09-22 (u102b oracle batteries, node side):
//
//	Auth (all three): ensureUserCanWriteProjectContent — anon → core CSRF
//	403 "Forbidden"; non-member → 403 {"message":"restricted"} (JSON) /
//	restricted page (HTML); ghost project → 404 page (rename/move) or
//	bare 404 text/plain "Not Found" (duplicate — `res.sendStatus(404)`
//	before any handler error plumbing).
//
//	Rename (no limiter):
//	  params {Project_id:oid, entity_id:oid, entity_type:enum
//	    doc|file|folder} → 404 JSON "Invalid Mongo ObjectId at
//	    \"params.X\"" / "Invalid option: expected one of
//	    \"doc\"|\"file\"|\"folder\" at \"params.entity_type\""
//	  body strict {name?:string, source?:string='editor'} → 400 JSON
//	    multi-issue "; " join + Unrecognized key(s) AFTER field issues
//	  name gate (JS utf16 len: null/''/>=150) → bare 400 "Bad Request"
//	  lock{ clean name → 400 text "invalid element name" →
//	    DU flush → findElement (missing → 404 page) → blocked TOP name →
//	    400 text "blocked element name" → same name in parent (incl.
//	    itself) → 400 text "file already exists" →
//	    $set <mp>.name + lastUpdated + lastUpdatedBy, $inc version
//	    (0 matched → 500) → DU structure with ONE rename-{doc|file} op
//	    (folder renames → ZERO ops → Node skips the call entirely) }
//	    → 204 sendStatus (ETag W/"a-…")
//
//	Move (no limiter):
//	  body strict {folder_id:oid, source?:string='editor'} → 400 JSON
//	    (missing → "Invalid input: expected string, received undefined
//	    at \"body.folder_id\""; multiple issues "; "-joined)
//	  lock{ findElement (404 page) → blocked CURRENT top name 400 text →
//	    findElement(dest, folder) (missing → 404 page) → dest already
//	    holds the name (docs/fileRefs/folders, incl. self when dest==src)
//	    → 400 text "file already exists" → folder into (a descendant of)
//	    itself → 400 text "destination folder is the same as me" /
//	    "destination folder is a child folder of me" → _putElement: clean
//	    name 400 "invalid element name" / >2000 elements 400 "project has
//	    too many files" / new FS path >1024 400 "path too long" / dest
//	    blocked top 400 "blocked element name" / dest dup (re-check) 400
//	    "file already exists" → $push dest.<seg> = FULL original element
//	    + $inc version + $set lastUpdated(,By) (0 matched → 500) →
//	    $pull src.<seg> {_id} + $inc version + $set lastUpdated(,By)
//	    (0 matched → 500) → DU structure (doc|file rename op: version is
//	    post-PUT project version) } → 204
//
//	Duplicate (limiter add-folder-to-project 60pts/60s, key
//	<projectId>:<uid>; Node consumes it BEFORE the controller checks):
//	  entity_type not doc|file → bare 400 "Bad Request" (BEFORE the
//	    project fetch) → ghost project → bare 404 "Not Found" → entity
//	    missing or kind mismatch → bare 404
//	  newName = generateDuplicateName(name, docs+fileRefs sibling names)
//	    a.b → a_copy.b → a_copy(1).b … (extension split at LAST dot >0)
//	  doc:  clean newName check (400 "invalid element name") →
//	    docstore GET {DS}/project/:p/doc/:d (404 → lines=[]) → NEW oid →
//	    docstore POST {DS}/project/:p/doc/:new {lines, version:0,
//	    ranges:undefined} OUTSIDE the lock (Node beforeLock) →
//	    lock{ $push dest.docs {name,_id} + $inc version + $set
//	    lastUpdated(,By) (0 matched → 500); DU structure add-doc
//	    {docLines joined '\n', ranges:{}, historyRangesSupport:false,
//	    createdBlob:true} } → 200 {"name":"…","_id":"…"}
//	  file: NEW File {name, created:now, rev:0, linkedFileData, hash} →
//	    lock{ history id required (else 500 page; Node
//	    'project does not have a history id') → $push dest.fileRefs
//	    {name,created,rev,linkedFileData,hash,_id} + $inc version + $set
//	    lastUpdated(,By) (0 matched → 500); DU structure add-file
//	    {historyRangesSupport:false, hash, createdBlob:false} } →
//	    redis PUBLISH editor-events {"room_id":<pid>,
//	    "message":"reciveNewFile","payload":[<folderId>,<fileObj>,
//	    "editor",<linkedFileData|null>,<uid>],"_id":"web:<host>:<rnd4>-<n>"
//	    } → 200 {"name","rev","linkedFileData","hash","_id","created"}
//
//	Tpds* paths (apis.thirdPartyDataStore unset in CE → Node
//	`enqueue` early-returns) and node-side logs are NO-OPS in this
//	build — NOT replicated (consistent with P4.11b / P4.13a notes).

package projectlist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

var (
	entRenPat = regexp.MustCompile(`^/project/([^/]+)/([^/]+)/([^/]+)/rename$`)
	entMovPat = regexp.MustCompile(`^/project/([^/]+)/([^/]+)/([^/]+)/move$`)
	entDupPat = regexp.MustCompile(`^/project/([^/]+)/([^/]+)/([^/]+)/duplicate$`)
)

// ---------- small primitives ----------

func primitiveObjectID(hex string) (primitive.ObjectID, bool) {
	oid, err := primitive.ObjectIDFromHex(strings.ToLower(hex))
	return oid, err == nil
}

func primitiveObjectIDOr(hex string) primitive.ObjectID {
	oid, _ := primitive.ObjectIDFromHex(strings.ToLower(hex))
	return oid
}

// entUTF16Len — JS `String.length` (UTF-16 code units): BMP runes count 1
// each, astral runes 2 each.
func entUTF16Len(s string) int {
	n := 0
	for _, r := range s {
		if r < 0x10000 {
			n++
		} else {
			n += 2
		}
	}
	return n
}

// entCleanNameU16 — SafePath.isCleanFilename with JS UTF-16 lengths
// (isAllowedLength && !BADCHAR && !BADFILE).
func entCleanNameU16(name string) bool {
	if name == "" || entUTF16Len(name) > entMaxPath {
		return false
	}
	if entBadChar.MatchString(name) {
		return false
	}
	return !entBadFile.MatchString(name)
}

// ---------- tree walk (Node ProjectLocator.findElement /
// duplicateEntity _findEntityInProject — same DFS shape) ----------

// entLoc — one entity found in the tree (raw bson for $push fidelity).
type entLoc struct {
	mongo  string       // e.g. "rootFolder.0.docs.0" / "...folders.1"
	fs     string       // LEADING slash per Node findElement: "/sub/a.txt"
	elem   bson.D       // the raw stored element (or root folder doc)
	folder *primitive.D // parent folder (nil: the root-folder element)
}

func entFindEnt(pj *primitive.D, idHex, seg string) (*entLoc, bool) {
	root := dgetArr(*pj, "rootFolder")
	if len(root) == 0 {
		return nil, false
	}
	rf, ok := root[0].(primitive.D)
	if !ok {
		return nil, false
	}
	// Node findElement startSearch special case.
	if idHex == oidHex(entFld(rf, "_id")) && seg == "folders" {
		return &entLoc{mongo: "rootFolder.0", fs: "", elem: rf, folder: nil}, true
	}
	var walk func(f primitive.D, mongo, fs string) (*entLoc, bool)
	walk = func(f primitive.D, mongo, fs string) (*entLoc, bool) {
		for i, x := range dgetArr(f, seg) {
			xm, ok := x.(primitive.D)
			if !ok {
				continue
			}
			if idHex == oidHex(entFld(xm, "_id")) {
				return &entLoc{
					mongo:  fmt.Sprintf("%s.%s.%d", mongo, seg, i),
					fs:     fs + "/" + asStr(entFld(xm, "name")),
					elem:   xm,
					folder: &f,
				}, true
			}
		}
		for j, s := range dgetArr(f, "folders") {
			sm, ok := s.(primitive.D)
			if !ok {
				continue
			}
			nm := asStr(entFld(sm, "name"))
			if l, ok := walk(sm, fmt.Sprintf("%s.folders.%d", mongo, j), fs+"/"+nm); ok {
				return l, true
			}
		}
		return nil, false
	}
	if l, ok := walk(rf, "rootFolder.0", ""); ok {
		if l.fs != "" {
			l.fs = "/" + l.fs
		}
		return l, true
	}
	return nil, false
}

// entFindEntKinded — duplicate route: docs first, then fileRefs (the
// controller's _findEntityInProject + kind check).
func entFindEntKinded(pj *primitive.D, idHex, wantKind string) (*entLoc, string, bool) {
	if l, ok := entFindEnt(pj, idHex, "docs"); ok {
		return l, "doc", true
	}
	if l, ok := entFindEnt(pj, idHex, "fileRefs"); ok {
		return l, "file", true
	}
	return nil, "", false
}

// entDupName — Node ProjectDuplicator.generateDuplicateName.
func entDupName(name string, existing []string) string {
	set := map[string]bool{}
	for _, e := range existing {
		set[e] = true
	}
	i := strings.LastIndex(name, ".")
	stem, ext := name, ""
	if i > 0 {
		stem, ext = name[:i], name[i:]
	}
	c := stem + "_copy" + ext
	n := 0
	for set[c] {
		n++
		c = fmt.Sprintf("%s_copy(%d)%s", stem, n, ext)
	}
	return c
}

func entFolderNames(f *primitive.D, seg string) []string {
	var out []string
	for _, x := range dgetArr(*f, seg) {
		if xm, ok := x.(primitive.D); ok {
			out = append(out, asStr(entFld(xm, "name")))
		}
	}
	return out
}

// entBlockedTop — Node _blockedFilename: top-level (dirname === '/')
// doc|file with a blocked name; folders exempt everywhere. `fs` is the
// leading-slash path; `name` the basename.
func entBlockedTop(fs, kind, name string) bool {
	if kind == "folder" {
		return false
	}
	idx := strings.LastIndex(fs, "/")
	if idx < 0 {
		return false
	}
	dir := fs[:idx]
	if dir == "" {
		dir = "/"
	}
	if dir != "/" {
		return false
	}
	return entBlocked[name]
}

// ---------- gates ----------

// entParamsVA — {Project_id:oid, entity_id:oid, entity_type:enum} in
// Node parseReq order → 404 JSON.
func entParamsVA(cxt *core.Cxt, res *core.Res) (pidHex, entityHex, kind string, ok bool) {
	pidHex = cxt.Params["1"]
	if !entHex24(pidHex) {
		res.JSON(404, []byte(`{"error":"Validation error: Invalid Mongo ObjectId at \"params.Project_id\"","statusCode":404}`))
		return
	}
	entityHex = cxt.Params["3"]
	if !entHex24(entityHex) {
		res.JSON(404, []byte(`{"error":"Validation error: Invalid Mongo ObjectId at \"params.entity_id\"","statusCode":404}`))
		return
	}
	switch cxt.Params["2"] {
	case "doc", "file", "folder":
		kind = cxt.Params["2"]
	default:
		res.JSON(404, []byte(`{"error":"Validation error: Invalid option: expected one of \"doc\"|\"file\"|\"folder\" at \"params.entity_type\"","statusCode":404}`))
		return
	}
	return strings.ToLower(pidHex), strings.ToLower(entityHex), kind, true
}

// entVA — strict body → (values, ok). keys = declared keys in Node schema
// order; required marks the non-optional ones (folder_id for move).
// Writes the 400 (multi-issue "; " join + unrecognized group last, raw key
// order) and returns ok=false on any issue.
func entVA(cxt *core.Cxt, res *core.Res, keys []string, required map[string]bool) (map[string]any, bool) {
	raw, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
	raw = bytes.TrimSpace(raw)
	var bm map[string]any
	if len(raw) == 0 {
		bm = map[string]any{}
	} else {
		first := raw[0]
		if first == '[' {
			res.JSON(400, []byte(`{"error":"Validation error: Invalid input: expected object, received array at \"body\"","statusCode":400}`))
			return nil, false
		}
		if err := json.Unmarshal(raw, &bm); err != nil || bm == nil {
			what := "number"
			switch {
			case first == 'n':
				what = "null"
			case first == 't':
				what = "boolean"
			case first == '"':
				what = "string"
			}
			res.JSON(400, []byte(`{"error":"Validation error: Invalid input: expected object, received `+what+` at \"body\"","statusCode":400}`))
			return nil, false
		}
	}
	var segs []string
	for _, k := range keys {
		v, has := bm[k]
		if !has {
			if required[k] {
				segs = append(segs, `Invalid input: expected string, received undefined at "body.`+k+`"`)
			}
			continue
		}
		if v == nil {
			segs = append(segs, `Invalid input: expected string, received null at "body.`+k+`"`)
			continue
		}
		s, isStr := v.(string)
		if !isStr {
			segs = append(segs, `Invalid input: expected string, received `+zodReceived(v, true)+` at "body.`+k+`"`)
			continue
		}
		if k == "folder_id" && !entHex24(s) {
			segs = append(segs, `Invalid Mongo ObjectId at "body.`+k+`"`)
			continue
		}
		bm[k] = s
	}
	segs = appendUnknown(segs, bodyKeyOrder(raw), keys...)
	if len(segs) > 0 {
		msg := "Validation error: " + strings.Join(segs, "; ")
		esc := strings.ReplaceAll(msg, `"`, `\"`)
		res.JSON(400, []byte(`{"error":"`+esc+`","statusCode":400}`))
		return nil, false
	}
	return bm, true
}

// ---------- auth gate (Node ensureUserCanWriteProjectContent order) -----

func entGateAuth(a *core.App, cxt *core.Cxt, res *core.Res, projectId string) (string, *primitive.D, bool) {
	uid := ""
	if cxt.Sess != nil {
		uid = cxt.Sess.UserIDHex()
	}
	if uid == "" {
		res.SendStatus(401)
		return "", nil, false
	}
	oid, ok := paramProject(projectId, res)
	if !ok {
		return "", nil, false
	}
	doc, lerr := loadProjectFull(a, cxt, oid)
	if lerr != nil {
		res.JSON(500, []byte("internal error"))
		return "", nil, false
	}
	if doc == nil {
		views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
		return "", nil, false
	}
	if !entCanWrite(uid, *doc) {
		if core.AcceptsJSON(cxt.Req) {
			res.JSON(403, []byte(colRestricted))
		} else {
			views.Restricted403(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
		}
		return "", nil, false
	}
	return uid, doc, true
}

// ---------- service calls (DocUpdater / docstore) ----------

func entFlushDU(cxt *core.Cxt, pidHex string) {
	// Best-effort (Node fetchNothing-and-warn; delete flow precedent).
	if cxt == nil {
		return
	}
	fireHTTP(cxt, "POST", cduBase()+"/project/"+pidHex+"/flush", nil)
}

func entSendStructure(pidHex, historyID, uid string, updates []json.RawMessage, version int64, source string) bool {
	if len(updates) == 0 {
		return true // Node: `if (updates.length < 1) return` — NO DU call.
	}
	// Node body {updates, userId, version, projectHistoryId, source} —
	// projectHistoryId undefined (no-history project) → key dropped.
	var histPart string
	if historyID != "" {
		histPart = `"projectHistoryId":"` + historyID + `",`
	}
	b := "{" +
		`"updates":[` + strings.Join(rawList(updates), ",") + `],` +
		`"userId":"` + entJSONEsc(uid) + `","version":` + itoa(version) + `,` +
		histPart +
		`"source":"` + entJSONEsc(source) + `"}`
	resp, err := crHTTP.Post(upDUBase()+"/project/"+pidHex, "application/json", strings.NewReader(b))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func rawList(rs []json.RawMessage) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, string(r))
	}
	return out
}

func itoa(n int64) string { return fmt.Sprintf("%d", n) }

func entJSONEsc(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}

// _getUpdates op shapes (key order pinned; undefined keys dropped — Node
// JSON.stringify of {key: undefined} omits the key).
func entRenOp(kind, id, oldPath, newPath string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(
		`{"type":"rename-%s","id":"%s","pathname":"%s","newPathname":"%s"}`,
		kind, id, entJSONEsc(oldPath), entJSONEsc(newPath)))
}

func entAddDocOp(id, pth, docLines string, hRS bool) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(
		`{"type":"add-doc","id":"%s","pathname":"%s","docLines":"%s","ranges":{},"historyRangesSupport":%v,"createdBlob":true}`,
		id, entJSONEsc(pth), entJSONEsc(docLines), hRS))
}

func entAddFileOp(id, pth, hash, metadataJSON string, hRS bool) json.RawMessage {
	// {type, id, pathname, docLines?, ranges?, historyRangesSupport, hash,
	// metadata?, createdBlob:true} — duplicateFile entity has no
	// docLines/ranges; metadata only for linked files.
	metaPart := ""
	if metadataJSON != "" {
		metaPart = `,"metadata":` + metadataJSON
	}
	return json.RawMessage(fmt.Sprintf(
		`{"type":"add-file","id":"%s","pathname":"%s","historyRangesSupport":%v,"hash":"%s"%s,"createdBlob":true}`,
		id, entJSONEsc(pth), hRS, entJSONEsc(hash), metaPart))
}

// entHistoryRangesSupport — Node `_.get(project, 'overleaf.history.
// rangesSupportEnabled', false)`.
func entHistoryRangesSupport(pj *primitive.D) bool {
	dn, ok := pjEntSub(*pj, "overleaf")
	if !ok {
		return false
	}
	hd, ok := pjEntSub(dn, "history")
	if !ok {
		return false
	}
	if b, ok := dget(hd, "rangesSupportEnabled").(bool); ok {
		return b
	}
	return false
}

// pjEntSub navigates two levels of possibly-array-wrapped subdocs
// (the go driver materializes BSON subdocs as primitive.D and
// subdoc-arrays as primitive.A; be tolerant of both shapes).
func pjEntSub(d primitive.D, key string) (primitive.D, bool) {
	v := dget(d, key)
	if a, ok := v.(primitive.A); ok && len(a) > 0 {
		v = a[0]
	}
	dn, ok := v.(primitive.D)
	return dn, ok
}

func entHistoryID(pj *primitive.D) string {
	dn, ok := pjEntSub(*pj, "overleaf")
	if !ok {
		return ""
	}
	hd, ok := pjEntSub(dn, "history")
	if !ok {
		return ""
	}
	id := dget(hd, "id")
	if s := asStr(id); s != "" {
		return s
	}
	if o := dget2(id); o != nil {
		return o.Hex()
	}
	return ""
}

func entVersion(pj *primitive.D) int64 {
	switch t := dget(*pj, "version").(type) {
	case int32:
		return int64(t)
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	}
	return 0
}

// ---------- web locker (Node RedisWebLocker) ----------

var (
	entLockRand  uint32 = uint32(time.Now().UnixNano() & 0xffffffffff)
	entLockCount int64
)

// entLockRun — runWithLock('sequentialProjectStructureUpdateLock',
// projectId): SET-style signed value, EXPIRE 30, DEL on exit. Busy waits
// one retry tick (Node FIFO queue interval 50ms) then fails like a wait
// timeout (500). Fail-open when redis is absent (gate compares healthy
// stacks; Node would hard-reject on redis loss).
func entLockRun(a *core.App, pidHex string, res *core.Res, cxt *core.Cxt, fn func() bool) bool {
	rdb := a.Redis
	if rdb == nil {
		return fn()
	}
	key := "lock:web:sequentialProjectStructureUpdateLock:" + pidHex
	host, _ := os.Hostname()
	attempt := func() bool {
		val := fmt.Sprintf("locked:host=%s:random=%08x:time=%d:count=%d",
			host, entLockRand, time.Now().UnixNano(), entLockCount)
		_ = val
		n, err := rdb.INCR(key)
		if err != nil {
			return fn() // redis outage: fail open
		}
		if n != 1 {
			return false
		}
		_ = rdb.EXPIRE(key, 30)
		defer rdb.DEL(key)
		return fn()
	}
	if attempt() {
		return true
	}
	time.Sleep(50 * time.Millisecond) // Node LOCK_TEST_INTERVAL tick
	if attempt() {
		return true
	}
	// Wait-timeout shape (Node LOCK_WAITED → 500 page).
	views.Error500Page(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
	return false
}

// ---------- editor-events emit (EditorRealTimeController.emitToRoom) ----

var entEmitCount int64

func entEmitEvent(a *core.App, pidHex, message, payloadJSON string) {
	rdb := a.Redis
	if rdb == nil {
		return
	}
	host, _ := os.Hostname()
	blob := fmt.Sprintf(`{"room_id":"%s","message":"%s","payload":[%s],"_id":"web:%s:%08x-%d"}`,
		pidHex, message, payloadJSON, host, entLockRand, entEmitCount)
	entEmitCount++
	_ = rdb.Publish("editor-events", blob)
}

// ---------- docstore (Node DocstoreManager) ----------

// entDocstoreGet — getDoc; (nil, false) = 404 / error (controller → lines=[]).
func entDocstoreGet(pidHex, didHex string) []string {
	resp, err := upHTTP.Get(entDocstoreURL() + "/project/" + pidHex + "/doc/" + didHex)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode != 200 {
		return nil
	}
	var dj struct {
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal(body, &dj); err != nil {
		return nil
	}
	return dj.Lines
}

func entDocstoreUpdate(pidHex, didHex string, lines []string) bool {
	// Node DocstoreManager.updateDoc POST {lines, version, ranges} with
	// ranges {} for addDoc (docstore requires the key — pinned: no-ranges
	// POST → 400 validation).
	b, _ := json.Marshal(struct {
		Lines   []string        `json:"lines"`
		Version int             `json:"version"`
		Ranges  json.RawMessage `json:"ranges"`
	}{lines, 0, json.RawMessage("{}")})
	resp, err := upHTTP.Post(entDocstoreURL()+"/project/"+pidHex+"/doc/"+didHex, "application/json", bytes.NewReader(b))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// ---------- mongo writes ----------

func entMongoRename(cxt *core.Cxt, a *core.App, pid primitive.ObjectID, mongoPath, name, uid string) bool {
	if a.Mongo == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	ure, err := db.Collection("projects").UpdateOne(ctx,
		bson.D{
			{Key: "_id", Value: pid},
			{Key: mongoPath, Value: bson.D{{Key: "$exists", Value: true}}},
		},
		bson.D{
			{Key: "$set", Value: bson.D{
				{Key: mongoPath + ".name", Value: name},
				{Key: "lastUpdated", Value: time.Now()},
				{Key: "lastUpdatedBy", Value: mustObjectID(uid)},
			}},
			{Key: "$inc", Value: bson.D{{Key: "version", Value: 1}}},
		})
	_ = cxt
	return err == nil && ure.MatchedCount == 1
}

func entMongoPushParent(cxt *core.Cxt, a *core.App, pid primitive.ObjectID, parentMongo, seg string, elem bson.D, uid string) bool {
	if a.Mongo == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	ure, err := db.Collection("projects").UpdateOne(ctx,
		bson.D{
			{Key: "_id", Value: pid},
			{Key: parentMongo, Value: bson.D{{Key: "$exists", Value: true}}},
		},
		bson.D{
			{Key: "$push", Value: bson.D{{Key: parentMongo + "." + seg, Value: elem}}},
			{Key: "$inc", Value: bson.D{{Key: "version", Value: 1}}},
			{Key: "$set", Value: bson.D{
				{Key: "lastUpdated", Value: time.Now()},
				{Key: "lastUpdatedBy", Value: mustObjectID(uid)},
			}},
		})
	return err == nil && ure.MatchedCount == 1
}

func entMongoPull(cxt *core.Cxt, a *core.App, pid primitive.ObjectID, mongoPath, seg, elemHex, uid string) bool {
	if a.Mongo == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	eid := primitiveObjectIDOr(elemHex)
	ure, err := db.Collection("projects").UpdateOne(ctx,
		bson.D{{Key: "_id", Value: pid}},
		bson.D{
			{Key: "$pull", Value: bson.D{{Key: mongoPath + "." + seg, Value: bson.D{{Key: "_id", Value: eid}}}}},
			{Key: "$inc", Value: bson.D{{Key: "version", Value: 1}}},
			{Key: "$set", Value: bson.D{
				{Key: "lastUpdated", Value: time.Now()},
				{Key: "lastUpdatedBy", Value: mustObjectID(uid)},
			}},
		})
	return err == nil && ure.MatchedCount == 1
}

// ---------- RENAME ----------

func entRenameHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		uid, pj, ok := entGateAuth(a, cxt, res, cxt.Params["1"])
		if !ok {
			return
		}
		pidHex, entityHex, kind, ok := entParamsVA(cxt, res)
		if !ok {
			return
		}
		bm, ok := entVA(cxt, res, []string{"name", "source"}, nil)
		if !ok {
			return
		}
		name, nameSet := bm["name"].(string)
		if !nameSet || name == "" || entUTF16Len(name) >= 150 {
			res.SendStatus(400)
			return
		}
		if !entCleanNameU16(name) {
			// Node outer handler: isCleanFilename BEFORE the flush/lock.
			res.PlainText(400, "invalid element name")
			return
		}
		oid, _ := primitiveObjectID(pidHex)
		page := func() { views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/"))) }
		page500 := func() { views.Error500Page(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/"))) }

		done := entLockRun(a, pidHex, res, cxt, func() bool {
			entFlushDU(cxt, pidHex)
			loc, found := entFindEnt(pj, entityHex, segOf(kind))
			if !found {
				page()
				return false
			}
			endPath := parentFS(loc) + "/" + name
			if entBlockedTop(endPath, kind, name) {
				res.PlainText(400, "blocked element name")
				return false
			}
			if loc.folder != nil {
				for _, s := range []string{"docs", "fileRefs", "folders"} {
					for _, n := range entFolderNames(loc.folder, s) {
						if n == name {
							res.PlainText(400, "file already exists")
							return false
						}
					}
				}
			}
			if !entMongoRename(cxt, a, oid, loc.mongo, name, uid) {
				page500()
				return false
			}
			if kind == "doc" || kind == "file" {
				histID := entHistoryID(pj)
				version := entVersion(pj) + 1 // post-write (Node newProject)
				_ = entSendStructure(pidHex, histID, uid, []json.RawMessage{
					entRenOp(kind, entityHex, loc.fs, endPath),
				}, version, entBodySource(bm))
			}
			// Node EditorController.renameEntity: emitToRoom(projectId,
			// 'reciveEntityRename', entityId, newName) on success (all kinds).
			if a != nil {
				entEmitEvent(a, pidHex, "reciveEntityRename", `"`+entJSONEsc(entityHex)+`","`+entJSONEsc(name)+`"`)
			}
			return true
		})
		if !done {
			return
		}
		res.SendStatus204()
	}
}

func segOf(kind string) string {
	if kind == "file" {
		return "fileRefs"
	}
	if kind == "folder" {
		return "folders"
	}
	return "docs"
}

// entCountAll — total docs+fileRefs+folders under the root (Node
// _countElements for the maxEntitiesPerProject gate).
func entCountAll(pj *primitive.D) int {
	root := dgetArr(*pj, "rootFolder")
	if len(root) == 0 {
		return 0
	}
	rf, ok := root[0].(primitive.D)
	if !ok {
		return 0
	}
	var walk func(f primitive.D) int
	walk = func(f primitive.D) int {
		n := 1 + len(dgetArr(f, "docs")) + len(dgetArr(f, "fileRefs"))
		for _, s := range dgetArr(f, "folders") {
			if sm, ok := s.(primitive.D); ok {
				n += walk(sm)
			}
		}
		return n
	}
	return walk(rf)
}

func entBodySource(bm map[string]any) string {
	if s, ok := bm["source"].(string); ok {
		return s
	}
	return "editor"
}

func parentFS(loc *entLoc) string {
	idx := strings.LastIndex(loc.fs, "/")
	return loc.fs[:idx]
}

// ---------- MOVE ----------

func entMoveHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		uid, pj, ok := entGateAuth(a, cxt, res, cxt.Params["1"])
		if !ok {
			return
		}
		pidHex, entityHex, kind, ok := entParamsVA(cxt, res)
		if !ok {
			return
		}
		bm, ok := entVA(cxt, res, []string{"folder_id", "source"}, map[string]bool{"folder_id": true})
		if !ok {
			return
		}
		folderStr := strings.ToLower(bm["folder_id"].(string))
		oid, _ := primitiveObjectID(pidHex)
		page := func() { views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/"))) }
		page500 := func() { views.Error500Page(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/"))) }

		done := entLockRun(a, pidHex, res, cxt, func() bool {
			entFlushDU(cxt, pidHex)
			loc, found := entFindEnt(pj, entityHex, segOf(kind))
			if !found {
				page()
				return false
			}
			name := asStr(entFld(loc.elem, "name"))
			if entBlockedTop(loc.fs, kind, name) {
				res.PlainText(400, "blocked element name")
				return false
			}
			dest, destFound := entFindEnt(pj, folderStr, "folders")
			if !destFound {
				page()
				return false
			}
			// _checkValidElementName(destEntity, name) — including self
			// when dest == source folder.
			for _, s := range []string{"docs", "fileRefs", "folders"} {
				for _, n := range entFolderNames(&dest.elem, s) {
					if n == name {
						res.PlainText(400, "file already exists")
						return false
					}
				}
			}
			// _checkValidFolderPath (folders only).
			if kind == "folder" {
				selfS := loc.fs
				if selfS == "" || selfS == "/" {
					selfS = "/"
				} else {
					selfS += "/"
				}
				destS := dest.fs
				if destS == "" || destS == "/" {
					destS = "/"
				} else {
					destS += "/"
				}
				if destS == selfS {
					res.PlainText(400, "destination folder is the same as me")
					return false
				}
				if strings.HasPrefix(destS, selfS) {
					res.PlainText(400, "destination folder is a child folder of me")
					return false
				}
			}
			// _putElement re-checks (Node order: clean, count, length,
			// blocked, dup) — dest dup already verified above; kept for
			// order fidelity.
			if !entCleanNameU16(name) {
				res.PlainText(400, "invalid element name")
				return false
			}
			if entCountAll(pj) > entMaxEnt {
				res.PlainText(400, "project has too many files")
				return false
			}
			newFS := dest.fs + "/" + name
			if entUTF16Len(newFS) > entMaxPath {
				res.PlainText(400, "path too long")
				return false
			}
			if entBlockedTop(newFS, kind, name) {
				res.PlainText(400, "blocked element name")
				return false
			}
			// Insert first (Node safety order), then remove.
			if !entMongoPushParent(cxt, a, oid, dest.mongo, segOf(kind), loc.elem, uid) {
				page500()
				return false
			}
			if !entMongoPull(cxt, a, oid, parentMongoOf(loc), segOf(kind), entityHex, uid) {
				page500()
				return false
			}
			// DU: one rename op (id unchanged) for doc|file moves.
			if kind == "doc" || kind == "file" {
				histID := entHistoryID(pj)
				version := entVersion(pj) + 2 // push + pull
				_ = entSendStructure(pidHex, histID, uid, []json.RawMessage{
					entRenOp(kind, entityHex, loc.fs, newFS),
				}, version, entBodySource(bm))
			}
			// Node EditorController.moveEntity: emitToRoom(projectId,
			// 'reciveEntityMove', entityId, folderId) on success.
			if a != nil {
				entEmitEvent(a, pidHex, "reciveEntityMove", `"`+entJSONEsc(entityHex)+`","`+entJSONEsc(folderStr)+`"`)
			}
			return true
		})
		if !done {
			return
		}
		res.SendStatus204()
	}
}

// ---------- DUPLICATE ----------

func entDuplicateHandler(a *core.App) func(*core.Cxt, *core.Res) {
	var dupLim *core.RateLimiter
	if a != nil && a.Redis != nil {
		dupLim = core.NewRateLimiter(a.Redis, "add-folder-to-project", 60, 60)
	}
	return func(cxt *core.Cxt, res *core.Res) {
		uid, pj, ok := entGateAuth(a, cxt, res, cxt.Params["1"])
		if !ok {
			return
		}
		kind := cxt.Params["2"]
		if kind != "doc" && kind != "file" {
			res.SendStatus(400)
			return
		}
		pidHex := cxt.Params["1"]
		if dupLim != nil {
			if !dupLim.Consume(pidHex + ":" + uid) {
				core.Send429(res, "Rate limit reached, please try again later")
				return
			}
		}
		entityHex := cxt.Params["3"]
		// Node: `_findEntityInProject` (docs then fileRefs) + KIND check —
		// both missing/mismatch → bare 404.
		loc, foundKind, found := entFindEntKinded(pj, entityHex, kind)
		if !found || foundKind != kind {
			res.SendStatus(404)
			return
		}
		oid, _ := primitiveObjectID(pidHex)
		page500 := func() { views.Error500Page(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/"))) }
		refName := asStr(entFld(loc.elem, "name"))

		var siblings []string
		siblings = append(siblings, entFolderNames(loc.folder, "docs")...)
		siblings = append(siblings, entFolderNames(loc.folder, "fileRefs")...)
		newName := entDupName(refName, siblings)

		if kind == "doc" {
			if !entCleanNameU16(newName) {
				res.PlainText(400, "invalid element name")
				return
			}
			lines := entDocstoreGet(pidHex, entityHex) // 404 → nil → []
			if lines == nil {
				lines = []string{}
			}
			newDocID := primitive.NewObjectID()
			// docstore updateDoc BEFORE the lock (Node beforeLock).
			if !entDocstoreUpdate(pidHex, newDocID.Hex(), lines) {
				page500()
				return
			}
			done := entLockRun(a, pidHex, res, cxt, func() bool {
				parentMongo := parentMongoOf(loc)
				if !entMongoPushParent(cxt, a, oid, parentMongo, "docs", bson.D{
					{Key: "name", Value: newName},
					{Key: "_id", Value: newDocID},
				}, uid) {
					page500()
					return false
				}
				histID := entHistoryID(pj)
				_ = entSendStructure(pidHex, histID, uid, []json.RawMessage{
					entAddDocOp(newDocID.Hex(), parentFS(loc)+"/"+newName, strings.Join(lines, "\n"), entHistoryRangesSupport(pj)),
				}, entVersion(pj)+1, "editor")
				// Node EditorController.addDoc (via addDoc promise):
				// emitToRoom(projectId, 'reciveNewDoc', folderId, doc,
				// source, userId) — payload [folderId, {name,_id},
				// "editor", userId].
				if a != nil {
					folderHex := oidHex(entFld(*loc.folder, "_id"))
					entEmitEvent(a, pidHex, "reciveNewDoc",
						`"`+entJSONEsc(folderHex)+`",{"name":"`+entJSONEsc(newName)+`","_id":"`+newDocID.Hex()+`"},"editor","`+entJSONEsc(uid)+`"`)
				}
				return true
			})
			if !done {
				return
			}
			res.JSON(200, []byte(fmt.Sprintf(`{"name":"%s","_id":"%s"}`, entJSONEsc(newName), newDocID.Hex())))
			return
		}
		// file duplicate.
		newFileID := primitive.NewObjectID()
		now := time.Now()
		hash := asStr(entFld(loc.elem, "hash"))
		linkedJSON, hasLinked := entLinkedFileJSON(entFld(loc.elem, "linkedFileData"))
		// DU op metadata for linked files (Node
		// buildFileMetadataForHistory): {importedAt: created, ...
		// linkedFileData}, dropping build_id/clsiServerId for
		// project_output_file. Gate fixtures use unlinked (null) files →
		// "" (key dropped on the wire).
		fileMeta := entLinkedMetadata(hasLinked, linkedJSON, now)
		done := entLockRun(a, pidHex, res, cxt, func() bool {
			parentMongo := parentMongoOf(loc)
			if !entMongoPushParent(cxt, a, oid, parentMongo, "fileRefs", bson.D{
				{Key: "name", Value: newName},
				{Key: "created", Value: now},
				{Key: "rev", Value: int32(0)},
				{Key: "linkedFileData", Value: entLinkedBSON(hasLinked, linkedJSON)},
				{Key: "hash", Value: hash},
				{Key: "_id", Value: newFileID},
			}, uid) {
				page500()
				return false
			}
			// Node duplicateFile: the history-id check comes AFTER the
			// mongo push (a no-history project ends with the tree mutated
			// and a 500 — Node 'project does not have a history id').
			if entHistoryID(pj) == "" {
				page500()
				return false
			}
			_ = entSendStructure(pidHex, entHistoryID(pj), uid, []json.RawMessage{
				entAddFileOp(newFileID.Hex(), parentFS(loc)+"/"+newName, hash, fileMeta, entHistoryRangesSupport(pj)),
			}, entVersion(pj)+1, "editor")
			return true
		})
		if !done {
			return
		}
		createdJSON := fmt.Sprintf(`{"name":"%s","rev":0,"linkedFileData":%s,"hash":"%s","_id":"%s","created":"%s"}`,
			entJSONEsc(newName), linkedJSONWire(hasLinked, linkedJSON),
			entJSONEsc(hash), newFileID.Hex(),
			now.UTC().Format("2006-01-02T15:04:05.000Z"))
		folderJSON := fmt.Sprintf(`"%s"`, entJSONEsc(oidHex(entFld(*loc.folder, "_id"))))
		entEmitEvent(a, pidHex, "reciveNewFile",
			strings.Join([]string{folderJSON, createdJSON, `"editor"`, linkedJSONWire(hasLinked, linkedJSON), fmt.Sprintf("%q", uid)}, ","))
		res.JSON(200, []byte(createdJSON))
	}
}

// parentMongoOf — the parent folder's stable mongo path ("rootFolder.0",
// "rootFolder.0.folders.0", ...) from the element's path — the element's
// mongo path is <parent>.<seg>.<index>, so strip the last two segments.
func parentMongoOf(loc *entLoc) string {
	i1 := strings.LastIndex(loc.mongo, ".")
	if i1 < 0 {
		return loc.mongo
	}
	i0 := strings.LastIndex(loc.mongo[:i1], ".")
	if i0 < 0 {
		return loc.mongo
	}
	return loc.mongo[:i0]
}

// ---------- linkedFileData JSON/BSON ----------

func entLinkedFileJSON(v any) (string, bool) {
	switch t := v.(type) {
	case nil:
		return "null", false
	case string:
		return t, true
	case primitive.D:
		b, _ := bson.MarshalExtJSON(t, false, false)
		return string(b), true
	case map[string]any:
		b, _ := json.Marshal(t)
		return string(b), true
	default:
		b, _ := json.Marshal(t)
		return string(b), true
	}
}

func linkedJSONWire(has bool, s string) string {
	if !has {
		return "null"
	}
	return s
}

// entLinkedMetadata — Node buildFileMetadataForHistory: {importedAt:
// file.created, ...linkedFileData} (project_output_file: drop
// build_id/clsiServerId). Returns "" (drop the key) when there is no
// linked data.
func entLinkedMetadata(has bool, linkedJSON string, created time.Time) string {
	if !has {
		return ""
	}
	var lfd map[string]any
	if json.Unmarshal([]byte(linkedJSON), &lfd) != nil {
		return ""
	}
	if lfd["provider"] == "project_output_file" {
		delete(lfd, "build_id")
		delete(lfd, "clsiServerId")
	}
	lfd["importedAt"] = created.UTC().Format("2006-01-02T15:04:05.000Z")
	out, err := json.Marshal(lfd)
	if err != nil {
		return ""
	}
	return string(out)
}

func entLinkedBSON(has bool, s string) any {
	if !has || s == "null" {
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		var d primitive.D
		if err2 := bson.UnmarshalExtJSON([]byte(s), false, &d); err2 == nil {
			return d
		}
		return nil
	}
	return v
}
