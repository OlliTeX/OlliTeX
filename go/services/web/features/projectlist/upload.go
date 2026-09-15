package projectlist

// P4.13a — file upload: POST /Project/:Project_id/upload
//
// Node oracle (UploadsRouter.mjs + ProjectUploadController.uploadFile +
// ProjectEntityUpdateHandler upsertDoc/upsertFile + FileTypeManager.getType
// + ProjectEntityMongoUpdateHandler + DocumentUpdaterHandler), pinned live
// 2026-09-15 against the e2e stack (Node web :4000 behind :7420):
//
//   200 {"success":true,"entity_id":..,"entity_type":"doc"}
//   200 {"success":true,"entity_id":..,"entity_type":"file","hash":..}
//   404 VA "Invalid Mongo ObjectId at \"params.Project_id\"" (statusCode:404)
//   400 VA "Invalid Mongo ObjectId at \"query.folder_id\""  (statusCode:400)
//   400 {"success":false,"error":"invalid_upload_request"}  (no file part;
//       parseReq checks it before the controller's name length check)
//   422 {"success":false,"error":"invalid_filename"} (name empty/len>150,
//       SafePath dirty name, or 'path too long')
//   422 {"success":false,"error":"folder_not_found"} (incl. folder_id ABSENT:
//       upsert's findElement(undefined) → NotFoundError — pinned)
//   403 restricted (accepts html→render / json→{"message":"restricted"} /
//       default→text "restricted") — HttpErrorHandler.forbidden
//   500 generic HTML error page (multer LIMIT_UNEXPECTED_FILE on a file part
//       whose field name is not 'qqfile' — pinned; internal failures)
//
// Node request order (pinned): multer(single 'qqfile') → parseReq VA
// (query.folder_id, body.file) → name length (422) → project + write access
// → folder lookup (422) → relativePath? mkdirp (case-INSENSITIVE child
// matching; creates {name,_id,docs:[],fileRefs:[],folders:[]}) → upsert
// beforeLock (SafePath 422; upsertFile uploads the blob before the write —
// git-blob hash sha1("blob <n>\0"+bytes), PUT {V1H}/projects/{hid}/blobs/{h}
// basic auth) → withLock branches:
//
//   doc new:        docstore POST {lines,version:0,ranges:{}} → rev;
//                   $push <fp>.docs {name,_id,rev}; $inc version; lastUpdated.
//   doc re-upload:  DU POST /project/:p/doc/:d
//                   {lines,source:'upload',user_id,trackChanges} — the DU
//                   service itself flushes docstore + project; web writes
//                   nothing else (pinned comment in upsertDoc).
//   file→doc:       docstore new id → rev; $pull <fp>.fileRefs{_id:old} +
//                   $push <fp>.docs{name,newId,rev}; DU [rename-file,add-doc].
//   file new:       v1H blob PUT; $push <fp>.fileRefs{name,created,rev:0,
//                   linkedFileData:null,hash,_id}; DU add-file.
//   file re-upload: v1H blob PUT; $set <fp>._id(NEW oid)/created/
//                   linkedFileData/hash + lastUpdated(/By); $inc version +
//                   <fp>.rev; DU [rename-file(old), add-file(new)] (ids differ
//                   so DU emits delete+add, not rename — pinned _getUpdates).
//   doc→file:       v1H blob PUT; $pull <fp>.docs{_id:old} +
//                   $push <fp>.fileRefs; DU [rename-doc, add-file].
//   DU: POST {DU}/project/:p {updates,userId,version(=post-write),
//       projectHistoryId,source:'upload'}; Node drops undefined op keys, so
//       add-doc has no hash/metadata and add-file has no docLines/ranges/
//       metadata (key order pinned from _getUpdates).
//
// Skipped (no-op in this build / out of scope): tpds enqueue
// (apis.thirdPartyDataStore unset → Node early-returns), socket emits
// (reciveNewDoc / removeEntity convert* → P5 editor unit), analytics.
// Rate limiter 'project-upload' (20pts/60s, redis) — gate battery stays under.

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

var upPat = regexp.MustCompile(`^/Project/([^/]+)/upload$`)
var upHexRX = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)
var upSplitRX = regexp.MustCompile(`\r\n|\n|\r`)

const (
	upNoFile400  = `{"success":false,"error":"invalid_upload_request"}`
	upInvName422 = `{"success":false,"error":"invalid_filename"}`
	upFolder422  = `{"success":false,"error":"folder_not_found"}`
	upOid400     = `{"error":"Validation error: Invalid Mongo ObjectId at \"query.folder_id\"","statusCode":400}`

	upNameMax    = 150
	upMaxDocLen  = 2 * 1024 * 1024
	upMaxTextLen = 3 * upMaxDocLen
	upMultipartCap = 32 * 1024 * 1024
)

// ---------- text classification (FileTypeManager pin) -------------------------

var upTextExt = func() map[string]bool {
	exts := []string{
		"tex", "latex", "sty", "cls", "bst", "bib", "bibtex", "txt", "tikz",
		"mtx", "rtex", "md", "asy", "lbx", "bbx", "cbx", "m", "lco", "dtx",
		"ins", "ist", "def", "clo", "ldf", "rmd", "qmd", "lua", "py", "gv",
		"mf", "yml", "yaml", "lhs", "lean", "lean4", "hs", "mk", "xmpdata",
		"cfg", "rnw", "ltx", "inc",
		"svg", "typ", "drawio",
	}
	if env := os.Getenv("ADDITIONAL_TEXT_EXTENSIONS"); env != "" {
		for _, e := range strings.Split(env, ",") {
			if e = strings.TrimSpace(e); e != "" {
				exts = append(exts, e)
			}
		}
	}
	m := map[string]bool{}
	for _, e := range exts {
		m["." + strings.ToLower(e)] = true
	}
	return m
}()

var upEditableNames = map[string]bool{
	"latexmkrc": true, ".latexmkrc": true, "makefile": true, "gnumakefile": true,
}

func upIsTextFile(name string) bool {
	base := name
	if i := strings.LastIndex(name, "/"); i >= 0 {
		base = name[i+1:]
	}
	lb := strings.ToLower(base)
	if upEditableNames[lb] {
		return true
	}
	if i := strings.LastIndex(lb, "."); i >= 0 {
		return upTextExt[lb[i:]]
	}
	return false
}

func upJSUnits(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func upUtf16leDecode(b []byte) (string, bool) {
	var out []rune
	n := len(b)
	i := 0
	for i < n {
		var u uint32
		if i+1 < n {
			u = uint32(b[i]) | uint32(b[i+1])<<8
		} else {
			u = uint32(b[i]) // odd tail byte: single unit (Node parity)
		}
		if i+1 < n && u >= 0xD800 && u <= 0xDBFF {
			if i+3 >= n {
				return "", false // lone high surrogate
			}
			low := uint32(b[i+2]) | uint32(b[i+3])<<8
			if low < 0xDC00 || low > 0xDFFF {
				return "", false // high not followed by a low
			}
			out = append(out, rune(0x10000+(u-0xD800)<<10+(low-0xDC00)))
			i += 4
			continue
		}
		if u >= 0xD800 && u <= 0xDFFF {
			return "", false // lone surrogate
		}
		out = append(out, rune(u))
		i += 2
	}
	return string(out), true
}

func upDecodeText(data []byte) (string, bool) {
	if len(data) >= 2 && data[0] == 0xff && data[1] == 0xfe {
		// Node: Buffer.toString('utf16le') keeps the BOM unit (U+FEFF) as a
		// character — decode the full buffer including the BOM bytes.
		return upUtf16leDecode(data)
	}
	if utf8valid(data) {
		return string(data), true
	}
	// latin1 fallback (Node default decoder): byte → code point.
	out := make([]rune, len(data))
	for i, x := range data {
		out[i] = rune(x)
	}
	return string(out), true
}

func utf8valid(b []byte) bool {
	i := 0
	for i < len(b) {
		c := b[i]
		if c < 0x80 {
			i++
			continue
		}
		var n int
		switch {
		case c >= 0xC2 && c <= 0xDF:
			n = 2
		case c >= 0xE0 && c <= 0xEF:
			n = 3
		case c >= 0xF0 && c <= 0xF4:
			n = 4
		default:
			return false
		}
		if i+n > len(b) {
			return false
		}
		for j := 1; j < n; j++ {
			if b[i+j]&0xC0 != 0x80 {
				return false
			}
		}
		i += n
	}
	return true
}

// upClassify → ("doc", lines) | ("file", nil). Mirrors FileTypeManager.getType
// + isEditable (zip import AND single-file upload reuse this).
func upClassify(data []byte, name string, existingDoc bool) (string, []string) {
	if !existingDoc && !upIsTextFile(name) {
		return "file", nil
	}
	if len(data) > upMaxTextLen {
		return "file", nil
	}
	text, ok := upDecodeText(data)
	if !ok {
		return "file", nil
	}
	if upJSUnits(text) >= upMaxDocLen {
		return "file", nil
	}
	for i := 0; i < len(text); i++ {
		if text[i] == 0 {
			return "file", nil
		}
	}
	for _, r := range text {
		if (r >= 0xD800 && r <= 0xDFFF) || r > 0xFFFF {
			return "file", nil
		}
	}
	lines := upSplitRX.Split(text, -1)
	if lines == nil {
		lines = []string{}
	}
	return "doc", lines
}

func upGitBlobHash(b []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(b))
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// ---------- DU / v1-history / docstore clients --------------------------------

func upDUBase() string {
	if v := os.Getenv("WEB_DOCUMENT_UPDATER_URL"); v != "" {
		return v
	}
	return "http://127.0.0.1:3003"
}

func upV1HBase() string {
	if v := os.Getenv("WEB_V1_HISTORY_URL"); v != "" {
		return v
	}
	return "http://127.0.0.1:3100/api"
}

func upV1HBasic() string {
	u := os.Getenv("V1_HISTORY_USER")
	if u == "" {
		u = "staging"
	}
	p := os.Getenv("V1_HISTORY_PASSWORD")
	if p == "" {
		p = "fd346a58aa441966ea6233cc8c3aa9f657920726204cbe101d69ca22762ba6f8"
	}
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(u+":"+p))
}

var upHTTP = &http.Client{Timeout: 60 * time.Second}

func upPutBlob(historyID, hash string, data []byte) bool {
	req, err := http.NewRequest("PUT", upV1HBase()+"/projects/"+historyID+"/blobs/"+hash, bytes.NewReader(data))
	if err != nil {
		return false
	}
	req.ContentLength = int64(len(data))
	req.Header.Set("Authorization", upV1HBasic())
	resp, err := upHTTP.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func upDUSetDoc(pj, docID string, lines []string, uid string, track bool) bool {
	body, _ := json.Marshal(map[string]any{
		"lines": lines, "source": "upload", "user_id": uid, "trackChanges": track,
	})
	resp, err := upHTTP.Post(upDUBase()+"/project/"+pj+"/doc/"+docID, "application/json", bytes.NewReader(body))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func upDocstorePut(pj, docID string, lines []string) (int64, bool) {
	body, _ := json.Marshal(map[string]any{
		"lines": lines, "version": 0, "ranges": map[string]any{},
	})
	u := entDocstoreURL() + "/project/" + pj + "/doc/" + docID
	resp, err := upHTTP.Post(u, "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, false
	}
	var out struct {
		Rev int64 `json:"rev"`
	}
	if err := json.Unmarshal(rb, &out); err != nil {
		return 0, true
	}
	return out.Rev, true
}

// ---------- DU structure ops (key order + key presence pinned) ----------------

func upAddOpDoc(id, path, lines string, hrs bool) bson.D {
	return bson.D{
		{Key: "type", Value: "add-doc"},
		{Key: "id", Value: id},
		{Key: "pathname", Value: path},
		{Key: "docLines", Value: lines},
		{Key: "ranges", Value: bson.D{}},
		{Key: "historyRangesSupport", Value: hrs},
		{Key: "createdBlob", Value: true},
	}
}

func upAddOpFile(id, path, hash string, hrs bool) bson.D {
	return bson.D{
		{Key: "type", Value: "add-file"},
		{Key: "id", Value: id},
		{Key: "pathname", Value: path},
		{Key: "historyRangesSupport", Value: hrs},
		{Key: "hash", Value: hash},
		{Key: "createdBlob", Value: true},
	}
}

func upDelOp(kind, id, path string) bson.D {
	return bson.D{
		{Key: "type", Value: "rename-" + kind},
		{Key: "id", Value: id},
		{Key: "pathname", Value: path},
		{Key: "newPathname", Value: ""},
	}
}


// upJSONObj — bson.D (ordered) → map for encoding/json (json.Marshal of
// bson.D serializes as an ARRAY — driver D has no encoding/json shape).
// Recurses into nested D/A so inner objects (e.g. "ranges": {}) do not
// degrade to arrays.
func upJSONObj(d bson.D) map[string]any {
	m := map[string]any{}
	for _, e := range d {
		m[e.Key] = upJSONVal(e.Value)
	}
	return m
}

func upJSONVal(v any) any {
	switch t := v.(type) {
	case bson.D:
		return upJSONObj(t)
	case bson.A:
		out := make([]any, 0, len(t))
		for _, x := range t {
			out = append(out, upJSONVal(x))
		}
		return out
	case map[string]any:
		for k, x := range t {
			t[k] = upJSONVal(x)
		}
		return t
	default:
		return v
	}
}

func upJSONArray(a bson.A) []map[string]any {
	out := make([]map[string]any, 0, len(a))
	for _, v := range a {
		if d, ok := v.(bson.D); ok {
			out = append(out, upJSONObj(d))
			continue
		}
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, m)
	}
	return out
}



func upUpdateStructure(pj, uid string, version int64, historyID string, updates bson.A) bool {
	if len(updates) < 1 {
		return true
	}
	body, _ := json.Marshal(map[string]any{
		"updates": upJSONArray(updates),
		"userId": uid,
		"version": version,
		"projectHistoryId": historyID,
		"source": "upload",
	})
	resp, err := upHTTP.Post(upDUBase()+"/project/"+pj, "application/json", bytes.NewReader(body))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// ---------- project-doc helpers ------------------------------------------------

func upTrackChanges(d any, uid string) bool {
	switch v := d.(type) {
	case bool:
		return v
	case primitive.D:
		for _, e := range v {
			if e.Key == uid {
				return e.Value == true
			}
		}
	}
	return false
}

func upRangsup(d primitive.D) bool {
	if ov, ok := entFld(d, "overleaf").(primitive.D); ok {
		if h, ok := entFld(ov, "history").(primitive.D); ok {
			if v := entFld(h, "rangesSupportEnabled"); v != nil {
				return v == true
			}
		}
	}
	return false
}

func upHistoryID(d primitive.D) string {
	if ov, ok := entFld(d, "overleaf").(primitive.D); ok {
		if h, ok := entFld(ov, "history").(primitive.D); ok {
			if hx := oidHex(entFld(h, "id")); hx != "" {
				return hx
			}
			return asStr(entFld(h, "id"))
		}
	}
	return ""
}

func upVersion(d primitive.D) int64 {
	switch v := entFld(d, "version").(type) {
	case int32:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}

// ---------- folder target -------------------------------------------------------

type upTarget struct {
	mongoPath    string
	fsPath       string
	folderID     string
	existingDoc  *entElement
	existingFile *entElement
	fileIdx      int
	docIdx       int
}

func upResolveFolder(root []entFolder, folderID, name string) *upTarget {
	mongoPath, fs, f, ok := entFindLoc(root, folderID)
	if !ok {
		return nil
	}
	t := &upTarget{mongoPath: mongoPath, fsPath: fs, folderID: folderID}
	for i := range f.docs {
		if f.docs[i].name == name {
			el := &f.docs[i]
			t.existingDoc = el
			t.docIdx = i
			break
		}
	}
	for i := range f.files {
		if f.files[i].name == name {
			el := &f.files[i]
			t.existingFile = el
			t.fileIdx = i
			break
		}
	}
	return t
}

// upMkdirp — Node ProjectEntityMongoUpdateHandler.mkdirp (default
// case-INSENSITIVE per-segment child matching; creates missing folders).
func upMkdirp(a *core.App, uid string, doc *primitive.D, tgt *upTarget, rel string) (*primitive.D, *upTarget, bool) {
	cur := tgt
	curDoc := doc
	for _, name := range upSegments(rel) {
		root := entParseTree(entFld(*curDoc, "rootFolder"))
		child := ""
		if _, _, pf, ok := entFindLoc(root, cur.folderID); ok {
			for i := range pf.fold {
				if strings.EqualFold(pf.fold[i].name, name) {
					child = pf.fold[i].idHex
					break
				}
			}
		}
		if child != "" {
			mp, fs, _, ok := entFindLoc(root, child)
			if !ok {
				return nil, nil, false
			}
			cur = &upTarget{mongoPath: mp, fsPath: fs, folderID: child}
			continue
		}
		newID := primitive.NewObjectID()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		db, derr := a.Mongo.DB(ctx)
		cancel()
		if derr != nil {
			return nil, nil, false
		}
		ure, err := db.Collection("projects").UpdateOne(ctx,
			bson.D{
				{Key: "_id", Value: mustObjectID(cur.folderID)},
				{Key: cur.mongoPath + ".folders", Value: bson.D{{Key: "$exists", Value: true}}},
			},
			bson.D{
				{Key: "$push", Value: bson.D{{Key: cur.mongoPath + ".folders", Value: bson.D{
					{Key: "name", Value: name},
					{Key: "_id", Value: newID},
					{Key: "docs", Value: bson.A{}},
					{Key: "fileRefs", Value: bson.A{}},
					{Key: "folders", Value: bson.A{}},
				}}}},
				{Key: "$inc", Value: bson.D{{Key: "version", Value: 1}}},
				{Key: "$set", Value: bson.D{
					{Key: "lastUpdated", Value: time.Now()},
					{Key: "lastUpdatedBy", Value: mustObjectID(uid)},
				}},
			})
		if err != nil || ure.MatchedCount == 0 {
			return nil, nil, false
		}
		var ndoc primitive.D
		rctx, rcancel := context.WithTimeout(context.Background(), 10*time.Second)
		ferr := db.Collection("projects").FindOne(rctx, bson.D{{Key: "_id", Value: mustObjectID(cur.folderID)}}).Decode(&ndoc)
		rcancel()
		if ferr != nil {
			return nil, nil, false
		}
		curDoc = &ndoc
		root = entParseTree(entFld(ndoc, "rootFolder"))
		mp, fs, _, ok := entFindLoc(root, newID.Hex())
		if !ok {
			return nil, nil, false
		}
		cur = &upTarget{mongoPath: mp, fsPath: fs, folderID: newID.Hex()}
	}
	return curDoc, cur, true
}

func upSegments(s string) []string {
	out := []string{}
	cur := ""
	for _, r := range s {
		if r == '/' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// ---------- mongo writes (pinned _putElement / replace* shapes) ----------------

func upPushDoc(a *core.App, pj, fp, name, docID string, rev int64, uid string, now time.Time) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	ure, err := db.Collection("projects").UpdateOne(ctx,
		bson.D{
			{Key: "_id", Value: mustObjectID(pj)},
			{Key: fp, Value: bson.D{{Key: "$exists", Value: true}}},
		},
		bson.D{
			{Key: "$push", Value: bson.D{{Key: fp + ".docs", Value: bson.D{
				{Key: "name", Value: name},
				{Key: "_id", Value: mustObjectID(docID)},
			}}}},
			{Key: "$inc", Value: bson.D{{Key: "version", Value: 1}}},
			{Key: "$set", Value: bson.D{
				{Key: "lastUpdated", Value: now},
				{Key: "lastUpdatedBy", Value: mustObjectID(uid)},
			}},
		})
	return err == nil && ure.MatchedCount == 1
}

func upPushFile(a *core.App, pj, fp, name, fileID, hash string, uid string, now time.Time) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	ure, err := db.Collection("projects").UpdateOne(ctx,
		bson.D{
			{Key: "_id", Value: mustObjectID(pj)},
			{Key: fp, Value: bson.D{{Key: "$exists", Value: true}}},
		},
		bson.D{
			{Key: "$push", Value: bson.D{{Key: fp + ".fileRefs", Value: bson.D{
				{Key: "name", Value: name},
				{Key: "created", Value: now},
				{Key: "rev", Value: 0},
				{Key: "linkedFileData", Value: nil},
				{Key: "hash", Value: hash},
				{Key: "_id", Value: mustObjectID(fileID)},
			}}}},
			{Key: "$inc", Value: bson.D{{Key: "version", Value: 1}}},
			{Key: "$set", Value: bson.D{
				{Key: "lastUpdated", Value: now},
				{Key: "lastUpdatedBy", Value: mustObjectID(uid)},
			}},
		})
	return err == nil && ure.MatchedCount == 1
}

// upReplaceFile — Node replaceFileWithNew: same mongo path, new _id,
// $inc [path].rev + version.
func upReplaceFile(a *core.App, pj, fp, newFileID, hash string, uid string, now time.Time, idx int) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	ure, err := db.Collection("projects").UpdateOne(ctx,
		bson.D{
			{Key: "_id", Value: mustObjectID(pj)},
			{Key: fp, Value: bson.D{{Key: "$exists", Value: true}}},
		},
		bson.D{
			{Key: "$set", Value: bson.D{
				{Key: fp + ".fileRefs." + itoaStr(idx) + "._id", Value: mustObjectID(newFileID)},
				{Key: fp + ".fileRefs." + itoaStr(idx) + ".created", Value: now},
				{Key: fp + ".fileRefs." + itoaStr(idx) + ".linkedFileData", Value: nil},
				{Key: fp + ".fileRefs." + itoaStr(idx) + ".hash", Value: hash},
				{Key: "lastUpdated", Value: now},
				{Key: "lastUpdatedBy", Value: mustObjectID(uid)},
			}},
			{Key: "$inc", Value: bson.D{
				{Key: "version", Value: 1},
				{Key: fp + ".fileRefs." + itoaStr(idx) + ".rev", Value: 1},
			}},
		})
	return err == nil && ure.MatchedCount == 1
}

// upSwapFileToDoc — Node replaceFileWithDoc: $pull fileRefs + $push docs.
func upSwapFileToDoc(a *core.App, pj, fp, oldFileID, newDocID, name string, rev int64, uid string, now time.Time) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	ure, err := db.Collection("projects").UpdateOne(ctx,
		bson.D{
			{Key: "_id", Value: mustObjectID(pj)},
			{Key: fp, Value: bson.D{{Key: "$exists", Value: true}}},
		},
		bson.D{
			{Key: "$pull", Value: bson.D{{Key: fp + ".fileRefs", Value: bson.D{{Key: "_id", Value: mustObjectID(oldFileID)}}}}},
			{Key: "$push", Value: bson.D{{Key: fp + ".docs", Value: bson.D{
				{Key: "name", Value: name},
				{Key: "_id", Value: mustObjectID(newDocID)},
			}}}},
			{Key: "$inc", Value: bson.D{{Key: "version", Value: 1}}},
			{Key: "$set", Value: bson.D{
				{Key: "lastUpdated", Value: now},
				{Key: "lastUpdatedBy", Value: mustObjectID(uid)},
			}},
		})
	return err == nil && ure.MatchedCount == 1
}

// upSwapDocToFile — Node replaceDocWithFile: $pull docs + $push fileRefs.
func upSwapDocToFile(a *core.App, pj, fp, oldDocID string, newFile bson.D, uid string, now time.Time) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	ure, err := db.Collection("projects").UpdateOne(ctx,
		bson.D{
			{Key: "_id", Value: mustObjectID(pj)},
			{Key: fp, Value: bson.D{{Key: "$exists", Value: true}}},
		},
		bson.D{
			{Key: "$pull", Value: bson.D{{Key: fp + ".docs", Value: bson.D{{Key: "_id", Value: mustObjectID(oldDocID)}}}}},
			{Key: "$push", Value: bson.D{{Key: fp + ".fileRefs", Value: newFile}}},
			{Key: "$inc", Value: bson.D{{Key: "version", Value: 1}}},
			{Key: "$set", Value: bson.D{
				{Key: "lastUpdated", Value: now},
				{Key: "lastUpdatedBy", Value: mustObjectID(uid)},
			}},
		})
	return err == nil && ure.MatchedCount == 1
}

// ---------- response shapes (exact key order pinned) ----------------------------

func upJSONDoc(entityID string) []byte {
	return []byte(`{"success":true,"entity_id":"` + entityID + `","entity_type":"doc"}`)
}

func upJSONFile(entityID, hash string) []byte {
	return []byte(`{"success":true,"entity_id":"` + entityID + `","entity_type":"file","hash":"` + hash + `"}`)
}

// ---------- handler --------------------------------------------------------------

func uploadHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		reqPath := strings.TrimPrefix(cxt.Req.URL.Path, "/")
		pbase := func() views.PageData { return pageBase(cxt, reqPath) }
		fail500 := func() { views.Error500Page(res.W, pbase()) }

		uid := ""
		if cxt.Sess != nil {
			uid = cxt.Sess.UserIDHex()
		}
		if uid == "" {
			res.SendStatus(401)
			return
		}
		oid, ok := paramProject(cxt.Params["1"], res)
		if !ok {
			return
		}
		pj := oid.Hex()

		// parseReq VA: query.folder_id (before body file presence). Node's
		// parseReq validates the PRESENT param — empty value → 400 objectId
		// validation; absent param is legal (controller falls back to root).
		folderID := cxt.Req.URL.Query().Get("folder_id")
		if len(cxt.Req.URL.Query()["folder_id"]) > 0 && folderID == "" && !upHexRX.MatchString(folderID) {
			res.JSON(400, []byte(upOid400))
			return
		}
		if folderID != "" && !upHexRX.MatchString(folderID) {
			res.JSON(400, []byte(upOid400))
			return
		}

		// multer (single 'qqfile').
		if err := cxt.Req.ParseMultipartForm(upMultipartCap); err != nil {
			res.JSON(400, []byte(upNoFile400))
			return
		}
		mf := cxt.Req.MultipartForm
		if mf == nil || len(mf.File) == 0 {
			res.JSON(400, []byte(upNoFile400))
			return
		}
		var fileHdr *multipart.FileHeader
		for fname, hdrs := range mf.File {
			if len(hdrs) != 1 || fname != "qqfile" {
				// MulterError LIMIT_UNEXPECTED_FILE / single() → 500 page.
				fail500()
				return
			}
			fileHdr = hdrs[0]
		}
		field := func(k string) string {
			if mf.Value != nil && len(mf.Value[k]) > 0 {
				return mf.Value[k][0]
			}
			return ""
		}
		name := field("name")
		ff, oerr := fileHdr.Open()
		if oerr != nil {
			fail500()
			return
		}
		data, rerr := io.ReadAll(ff)
		ff.Close()
		if rerr != nil {
			fail500()
			return
		}

		// controller name length (422, checked before folder resolution).
		if name == "" || upJSUnits(name) > upNameMax {
			res.JSON(422, []byte(upInvName422))
			return
		}

		// project + write access (ensureUserCanWriteProjectContent →
		// HttpErrorHandler.forbidden: accepts html/json/other).
		doc, lerr := loadProjectFull(a, cxt, oid)
		if lerr != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		if doc == nil {
			views.NotFoundPage(res.W, pbase())
			return
		}
		if !entCanWrite(uid, *doc) {
			accept := cxt.Req.Header.Get("Accept")
			if strings.Contains(accept, "application/json") {
				res.JSON(403, []byte(colRestricted))
			} else if strings.Contains(accept, "html") {
				views.Restricted403(res.W, pbase())
			} else {
				res.PlainText(403, "restricted")
			}
			return
		}

		// upsert beforeLock: SafePath runs BEFORE the folder lookup (pinned).
		if !entCleanName(name) {
			res.JSON(422, []byte(upInvName422))
			return
		}

		// folder lookup (upsert findElement) → 422 folder_not_found
		// (including the ABSENT folder_id case — pinned Node quirk).
		root := entParseTree(entFld(*doc, "rootFolder"))
		tgt := upResolveFolder(root, folderID, name)
		if tgt == nil {
			res.JSON(422, []byte(upFolder422))
			return
		}

		// relativePath: Node stores the file's FULL relative path (incl. file
		// name); the target folder is Path.dirname of it (Uppy semantics).
		if rel := field("relativePath"); rel != "" && rel != "null" {
			relDir := rel
			if i := strings.LastIndex(rel, "/"); i >= 0 {
				relDir = rel[:i]
			} else {
				relDir = ""
			}
			if relDir == "" {
				// dirname empty → file lands in the resolved folder (no mkdirp).
			} else {
			nd, nt, ok := upMkdirp(a, uid, doc, tgt, relDir)
			if !ok {
				fail500()
				return
			}
			doc = nd
			root = entParseTree(entFld(*doc, "rootFolder"))
			tgt = upResolveFolder(root, nt.folderID, name)
			if tgt == nil {
				fail500()
				return
			}
			}
		}

		kind, lines := upClassify(data, name, tgt.existingDoc != nil)
		if kind == "doc" {
			upDoDoc(a, cxt, res, fail500, pj, tgt, name, lines, uid, *doc, upTrackChanges(entFld(*doc, "track_changes"), uid))
			return
		}
		upDoFile(a, cxt, res, fail500, pj, tgt, name, data, uid, *doc)
	}
}

// ---------- branches (inline order pinned in Node upsertDoc/upsertFile) ---------

func upDoDoc(a *core.App, cxt *core.Cxt, res *core.Res, fail500 func(), pj string, tgt *upTarget, name string, lines []string, uid string, doc primitive.D, track bool) {
	now := time.Now()
	uidl := strings.ToLower(uid)
	if tgt.existingDoc != nil {
		// doc re-upload: DU setDocument (DU flushes docstore + project).
		if !upDUSetDoc(pj, tgt.existingDoc.idHex, lines, uidl, track) {
			fail500()
			return
		}
		res.JSON(200, upJSONDoc(tgt.existingDoc.idHex))
		return
	}
	if tgt.existingFile != nil {
		// file → doc: docstore new id → mongo swap → DU [rename-file, add-doc].
		newDocID := primitive.NewObjectID()
		rev, ok := upDocstorePut(pj, newDocID.Hex(), lines)
		if !ok {
			fail500()
			return
		}
		if !upSwapFileToDoc(a, pj, tgt.mongoPath, tgt.existingFile.idHex, newDocID.Hex(), name, rev, uidl, now) {
			fail500()
			return
		}
		path := tgt.fsPath + "/" + name
		updates := bson.A{
			upDelOp("file", tgt.existingFile.idHex, path),
			upAddOpDoc(newDocID.Hex(), path, strings.Join(lines, "\n"), upRangsup(doc)),
		}
		if !upUpdateStructure(pj, uidl, upVersion(doc)+1, upHistoryID(doc), updates) {
			fail500()
			return
		}
		res.JSON(200, upJSONDoc(newDocID.Hex()))
		return
	}
	// new doc: docstore → $push docs → DU add-doc.
	newDocID := primitive.NewObjectID()
	rev, ok := upDocstorePut(pj, newDocID.Hex(), lines)
	if !ok {
		fail500()
		return
	}
	if !upPushDoc(a, pj, tgt.mongoPath, name, newDocID.Hex(), rev, uidl, now) {
		fail500()
		return
	}
	path := tgt.fsPath + "/" + name
	updates := bson.A{
		upAddOpDoc(newDocID.Hex(), path, strings.Join(lines, "\n"), upRangsup(doc)),
	}
	if !upUpdateStructure(pj, uidl, upVersion(doc)+1, upHistoryID(doc), updates) {
		fail500()
		return
	}
	res.JSON(200, upJSONDoc(newDocID.Hex()))
}

func upDoFile(a *core.App, cxt *core.Cxt, res *core.Res, fail500 func(), pj string, tgt *upTarget, name string, data []byte, uid string, doc primitive.D) {
	now := time.Now()
	uidl := strings.ToLower(uid)
	hash := upGitBlobHash(data)
	hist := upHistoryID(doc)

	// Node upsertFile beforeLock: blob upload happens BEFORE the folder write
	// (even when a same-name doc will be replaced) — pinned.
	if hist != "" && !upPutBlob(hist, hash, data) {
		fail500()
		return
	}

	if tgt.existingFile != nullElement() {
		// file re-upload: $set same path with NEW _id, $inc [path].rev+version.
		newFileID := primitive.NewObjectID()
		if !upReplaceFile(a, pj, tgt.mongoPath, newFileID.Hex(), hash, uidl, now, tgt.fileIdx) {
			fail500()
			return
		}
		path := tgt.fsPath + "/" + name
		updates := bson.A{
			upDelOp("file", tgt.existingFile.idHex, path),
			upAddOpFile(newFileID.Hex(), path, hash, upRangsup(doc)),
		}
		if !upUpdateStructure(pj, uidl, upVersion(doc)+1, hist, updates) {
			fail500()
			return
		}
		res.JSON(200, upJSONFile(newFileID.Hex(), hash))
		return
	}
	if tgt.existingDoc != nullElement() {
		// doc → file: $pull docs + $push fileRefs + DU [rename-doc, add-file].
		newFileID := primitive.NewObjectID()
		newFile := bson.D{
			{Key: "name", Value: name},
			{Key: "created", Value: now},
			{Key: "rev", Value: 0},
			{Key: "linkedFileData", Value: nil},
			{Key: "hash", Value: hash},
			{Key: "_id", Value: mustObjectID(newFileID.Hex())},
		}
		if !upSwapDocToFile(a, pj, tgt.mongoPath, tgt.existingDoc.idHex, newFile, uidl, now) {
			fail500()
			return
		}
		path := tgt.fsPath + "/" + name
		updates := bson.A{
			upDelOp("doc", tgt.existingDoc.idHex, path),
			upAddOpFile(newFileID.Hex(), path, hash, upRangsup(doc)),
		}
		if !upUpdateStructure(pj, uidl, upVersion(doc)+1, hist, updates) {
			fail500()
			return
		}
		res.JSON(200, upJSONFile(newFileID.Hex(), hash))
		return
	}
	// new file: $push fileRefs → DU add-file.
	newFileID := primitive.NewObjectID()
	if !upPushFile(a, pj, tgt.mongoPath, name, newFileID.Hex(), hash, uidl, now) {
		fail500()
		return
	}
	path := tgt.fsPath + "/" + name
	updates := bson.A{
		upAddOpFile(newFileID.Hex(), path, hash, upRangsup(doc)),
	}
	if !upUpdateStructure(pj, uidl, upVersion(doc)+1, hist, updates) {
		fail500()
		return
	}
	res.JSON(200, upJSONFile(newFileID.Hex(), hash))
}

func nullElement() *entElement { return (*entElement)(nil) }

func itoaStr(i int) string {
	if i == 0 {
		return "0"
	}
	d := ""
	for i > 0 {
		d = string(rune('0'+i%10)) + d
		i /= 10
	}
	return d
}
