// Package webdav implements the Node services/web/modules/webdav
// (WebdavRouter.mjs + WebdavController.mjs + WebdavHandler.mjs +
// WebdavCredentials.mjs) route surface on the Go web service.
//
// Routes (Node registration order; all requireLogin — anonymous is bounced
// by the core global chain, pinned P1: GET+json 401 / bare GET 302 → /login
// / non-GET 403):
//
//	GET    /user/webdav/status                  → 200 {"connected":false}
//	                                                                 |
//	         200 {"connected":true,"baseUrl":…,"rootPath":…,
//	                "lastSyncAt":…,"lastSyncError":…,"lastConflict":…}
//	POST   /user/webdav/connect                 → 200 {"success":true}; 500
//	POST   /user/webdav/disconnect              → 200 {"success":true}; 500
//	GET    /project/:project_id/webdav/state    → 200 {"connected":false}  (unlinked)
//	GET    /project/:project_id/webdav/files    → 404 {"message":"Project not linked to WebDAV"}
//	POST   /project/:project_id/webdav/pull     → 409 {"message":"WebDAV is not connected"} (unlinked)
//	POST   /project/:project_id/webdav/push     → 500 {"message":"WebDAV is not connected"} (unlinked)
//	POST   /project/:project_id/webdav/conflict/resolve
//	         missing params  → 400 {"message":"Missing required parameters: path and choice are required"}
//	         bad choice      → 400 {"message":"Invalid choice. Must be 'local' or 'remote'"}
//	         no conflict     → 404 {"message":"No active conflict for this file","errorCode":"CONFLICT_NOT_FOUND"}
//	POST   /project/:project_id/webdav/link     → 400 {"message":"WebDAV credentials are not configured"}
//	                                                                 |
//	         400 {"message":"WebDAV credentials are incomplete"}
//	         200 {"success":true,"message":"Project linked to WebDAV successfully","state":{…}}
//	GET    /project/:project_id/webdav/project-name
//	         → 200 {"projectName":"…"}
//	DELETE /project/:project_id/webdav/state    → 404 {"message":"Project is not linked to WebDAV","errorCode":"PROJECT_NOT_LINKED"}
//	                                                                 |
//	         200 {"success":true,"message":"Project unlinked from WebDAV"}
//	POST   /project/new/webdav                  → 400 {"error":"projectName is required"}
//	                                                                 |
//	         500 {"error":"WebDAV is not connected"} (unlinked)
//
// Project params (pinned oracle 2026-09-17, Node container):
//
//	invalid ObjectId  → 404 {"error":"Validation error: Invalid Mongo ObjectId at \"params.project_id\"","statusCode":404}
//	unknown project   → 404 HTML (general/404 page)
//	other user's proj → 403 {"message":"restricted"} (json accept) | restricted view
//
// PINS captured 2026-09-17 (e2e user webdav-UNLINKED, fresh instance — both
// stacks byte-identical; gate asserts every row):
//
//	status(unlinked)            → 200 {"connected":false}
//	status(corrupted token)     → 200 {"connected":false}  (Node live: degrade)
//	connect{} → status          → 200 {"connected":true,"lastSyncAt":null,"lastSyncError":null,"lastConflict":null}
//	connect(full) → status      → 200 {"connected":true,"baseUrl":"https://dav.e2e.invalid","rootPath":"/Overleaf","lastSyncAt":null,"lastSyncError":null,"lastConflict":null}
//	disconnect → status         → 200 {"connected":false}
//	state(own, unlinked)        → 200 {"connected":false}
//	files(own, unlinked)        → 404 {"message":"Project not linked to WebDAV"}
//	link(creds absent)          → 400 {"message":"WebDAV credentials are not configured"}
//	link(creds, no baseUrl)     → 400 {"message":"WebDAV credentials are not configured"}
//	link(creds, no user/pass)   → 400 {"message":"WebDAV credentials are incomplete"}
//	pull(creds absent)          → 409 {"message":"WebDAV is not connected"}
//	push(creds absent)          → 500 {"message":"WebDAV is not connected"}
//	conflict/resolve {}         → 400 {"message":"Missing required parameters: path and choice are required"}
//	conflict/resolve bogus      → 400 {"message":"Invalid choice. Must be 'local' or 'remote'"}
//	conflict/resolve valid      → 404 {"message":"No active conflict for this file","errorCode":"CONFLICT_NOT_FOUND"}
//	unlink(unlinked)            → 404 {"message":"Project is not linked to WebDAV","errorCode":"PROJECT_NOT_LINKED"}
//	project-name(own)           → 200 {"projectName":"webdav-p69-gate"}
//	project-new/webdav {}       → 400 {"error":"projectName is required"}
//	project-new/webdav {name}   → 500 {"error":"WebDAV is not connected"}
//
// Live WebDAV traffic (PROPFIND/PUT/GET against a real server) is implemented
// via client.go (check/list/get/put/mkcol/remove) but NOT e2e-pinned — the
// sandbox has no live WebDAV endpoint and the e2e user is unlinked; only the
// deterministic, offline-reachable surface is pinned byte-exact.
package webdav

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

// ---- shared helpers --------------------------------------------------------

var wdHex24 = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// wdBadOID404 — Node zod param schema (params.project_id) 404 body.
const wdBadOID404 = `{"error":"Validation error: Invalid Mongo ObjectId at \"params.project_id\"","statusCode":404}`

func wdUID(cxt *core.Cxt) string {
	if cxt.Sess == nil {
		return ""
	}
	return cxt.Sess.UserIDHex()
}

// wdPageBase — the views.PageData for 403/404 renders (mirrors
// projectlist.pageBase).
func wdPageBase(cxt *core.Cxt, reqPath string) views.PageData {
	d := views.PageData{Nonce: views.NewNonce(), Path: reqPath}
	origin := cxt.SiteURL
	if origin == "" {
		origin = "http://" + cxt.Req.Host
	}
	d.Origin = origin
	if cxt.Sess != nil {
		d.CSRFToken = cxt.Sess.CsrfToken()
		d.UserEmail, d.UserID = core.PageUserSlots(cxt.Sess)
	}
	return d
}

// wdLoadProject — minimal project fetch (nil when absent).
func wdLoadProject(a *core.App, ctx context.Context, oid primitive.ObjectID) (*bson.D, error) {
	if a.Mongo == nil {
		return nil, nil
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, err
	}
	var d bson.D
	if err := db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&d); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

// wdCanWrite — Node canUserWriteProjectContent (owner OR read-and-write
// collaborator; site-admin capability is unreachable for the e2e accounts).
func wdCanWrite(uidHex string, doc bson.D) bool {
	own, _ := wdDocStr(doc, "owner_ref")
	if own == "" {
		if o, ok := wdDocVal(doc, "owner"); ok {
			if dm, ok := o.(bson.D); ok {
				for _, k := range []string{"userId", "user_id", "_id"} {
					for _, e := range dm {
						if e.Key == k {
							if s, ok := oidHex(e.Value); ok && s != "" {
								own = s
							}
						}
					}
				}
			}
		}
	}
	if own != "" && strings.EqualFold(own, uidHex) {
		return true
	}
	for _, key := range []string{"collab_refs", "collaberator_refs", "tokenAccessReadAndWrite_refs"} {
		if arr, ok := wdDocVal(doc, key); ok {
			if items, ok := arr.([]interface{}); ok {
				for _, it := range items {
					if s, ok := oidHex(it); ok && s != "" && strings.EqualFold(s, uidHex) {
						return true
					}
				}
			}
		}
	}
	return false
}

func wdDocStr(doc bson.D, key string) (string, bool) {
	v, ok := wdDocVal(doc, key)
	if !ok {
		return "", false
	}
	if s, ok := v.(string); ok {
		return s, true
	}
	if o, ok := v.(primitive.ObjectID); ok {
		return o.Hex(), true
	}
	return "", false
}

func wdDocVal(doc bson.D, key string) (interface{}, bool) {
	for _, e := range doc {
		if e.Key == key {
			return e.Value, true
		}
	}
	return nil, false
}

func oidHex(v interface{}) (string, bool) {
	switch t := v.(type) {
	case primitive.ObjectID:
		return t.Hex(), true
	case string:
		if wdHex24.MatchString(t) {
			return strings.ToLower(t), true
		}
	}
	return "", false
}

// wdAuthzProject — the /project/:project_id/webdav/* gate:
//
//  1. param must be a valid ObjectId → else 404 zod body (wdBadOID404)
//  2. project must exist            → else 404 HTML page
//  3. user must be able to write    → else 403 restricted (json) / view
//
// Returns the (lowercase-hex) project id on success.
func wdAuthzProject(a *core.App, cxt *core.Cxt, res *core.Res) (string, bool) {
	seg := cxt.Params["1"]
	if !wdHex24.MatchString(seg) {
		res.JSON(404, []byte(wdBadOID404))
		return "", false
	}
	oid, oerr := primitive.ObjectIDFromHex(strings.ToLower(seg))
	if oerr != nil {
		res.JSON(404, []byte(wdBadOID404))
		return "", false
	}
	uid := wdUID(cxt)
	doc, lerr := wdLoadProject(a, cxt.Req.Context(), oid)
	if lerr != nil {
		res.JSON(500, []byte(`{"message":"internal error"}`))
		return "", false
	}
	if doc == nil {
		views.NotFoundPage(res.W, wdPageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
		return "", false
	}
	if uid == "" || !wdCanWrite(uid, *doc) {
		if core.AcceptsJSON(cxt.Req) {
			res.JSON(403, []byte(`{"message":"restricted"}`))
		} else {
			views.Restricted403(res.W, wdPageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
		}
		return "", false
	}
	return seg, true
}

// ---- body helpers ----------------------------------------------------------

// wdStrField — a JSON body field that can be absent, null, or a string.
// Node's controller destructures each key and JSON.stringify drops UNDEFINED
// keys but KEEPS explicit nulls — mirror that.
func wdStrField(raw map[string]json.RawMessage, key string) (string, bool, bool) {
	// returns (value, present, isNull)
	r, ok := raw[key]
	if !ok {
		return "", false, false
	}
	var s *string
	if err := json.Unmarshal(r, &s); err != nil {
		return "", true, false // non-string (number…) — treat as present-non-null
	}
	if s == nil {
		return "", true, true
	}
	return *s, true, false
}

// wdJSString — JSON.stringify string escaping (quotes + backslash are all the
// cases that occur in the pinned bodies).
func wdJSString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// wdReadBodyJSON — read + parse the JSON body. On malformed JSON the core
// pre-handler has already answered (BadJSON400), so error here means the
// caller should 400 as Node's parseReq would.
func wdReadBodyJSON(cxt *core.Cxt) (map[string]json.RawMessage, error) {
	raw, err := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]json.RawMessage{}
	}
	return m, nil
}

// wdCredsPlain — rebuild the credentials plaintext JSON exactly as Node's
// connect does: only the four keys, in Node's order (baseUrl, username,
// password, rootPath), absent keys dropped, nulls kept.
func wdCredsPlain(body map[string]json.RawMessage) string {
	var b strings.Builder
	first := true
	for _, key := range []string{"baseUrl", "username", "password", "rootPath"} {
		v, present, isNull := wdStrField(body, key)
		if !present {
			continue
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.WriteString(`"` + key + `":`)
		if isNull {
			b.WriteString(`null`)
		} else {
			b.WriteString(wdJSString(v))
		}
	}
	return "{" + b.String() + "}"
}

// ---- user credential JSON --------------------------------------------------

// wdCredField — extract a string field out of the decrypted credentials
// JSON (map: absent / null / string).
func wdCredField(plainJSON string, key string) (*string, bool) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(plainJSON), &m); err != nil {
		return nil, false
	}
	raw, ok := m[key]
	if !ok {
		return nil, false
	}
	val := raw
	var s *string
	if string(bytes.TrimSpace(val)) == "null" {
		return nil, true // present-null
	}
	if err := json.Unmarshal(val, &s); s != nil && err == nil {
		return s, true
	}
	return nil, false
}

// ---- handlers ---------------------------------------------------------------

// hStatus — GET /user/webdav/status (P6.9 webdav).
func hStatus(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		uid := wdUID(cxt)
		plain, ok := wdGetCreds(ctx, a, uid)
		if !ok {
			// Node (WebdavRouter, live-gated): no doc → plain not-linked;
			// doc present but undecryptable (rotated cipher / corruption) →
			// not-linked WITH the error key.
			if wdCredsDocExists(ctx, a, uid) {
				res.JSON(200, []byte(`{"connected":false,"error":"stored-credentials-invalid"}`))
				return
			}
			res.JSON(200, []byte(`{"connected":false}`))
			return
		}
		baseURL, hasBase := wdCredField(plain, "baseUrl")
		rootPath, hasRoot := wdCredField(plain, "rootPath")
		username, hasUser := wdCredField(plain, "username")

		// last-sync info: state docs owned by this user (or matching the
		// connection username), most-recent lastSyncAt first (nulls last).
		var lastSyncAt *string
		var lastSyncError *string
		var lastConflict string // raw JSON or ""
		if d, ok := wdFirstStateForUser(ctx, a, uid, username, hasUser); ok {
			lastSyncAt, lastSyncError, lastConflict = wdStateSyncFields(d)
		}

		var b strings.Builder
		b.WriteString(`{"connected":true,`)
		if hasBase {
			if baseURL == nil {
				b.WriteString(`"baseUrl":null,`)
			} else {
				b.WriteString(`"baseUrl":` + wdJSString(*baseURL) + `,`)
			}
		}
		if hasRoot {
			if rootPath == nil {
				b.WriteString(`"rootPath":null,`)
			} else {
				b.WriteString(`"rootPath":` + wdJSString(*rootPath) + `,`)
			}
		}
		b.WriteString(`"lastSyncAt":`)
		b.WriteString(orNull(lastSyncAt))
		b.WriteString(`,"lastSyncError":`)
		b.WriteString(orNull(lastSyncError))
		b.WriteString(`,"lastConflict":`)
		if lastConflict == "" {
			b.WriteString(`null`)
		} else {
			b.WriteString(lastConflict)
		}
		b.WriteString(`}`)
		res.JSON(200, []byte(b.String()))
	}
}

func orNull(s *string) string {
	if s == nil {
		return `null`
	}
	return wdJSString(*s)
}

// hConnect — POST /user/webdav/connect (P6.9 webdav).
func hConnect(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		uid := wdUID(cxt)
		body, err := wdReadBodyJSON(cxt)
		if err != nil {
			res.JSON(400, []byte(`{}`))
			return
		}
		if serr := wdSaveCreds(ctx, a, uid, wdCredsPlain(body)); serr != nil {
			res.JSON(500, []byte(`{"error":`+wdJSString(serr.Error())+`}`))
			return
		}
		res.JSON(200, []byte(`{"success":true}`))
	}
}

// hDisconnect — POST /user/webdav/disconnect (P6.9 webdav).
func hDisconnect(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		uid := wdUID(cxt)
		if err := wdRemoveCreds(ctx, a, uid); err != nil {
			res.JSON(500, []byte(`{"error":`+wdJSString(err.Error())+`}`))
			return
		}
		res.JSON(200, []byte(`{"success":true}`))
	}
}

// hProjectState — GET /project/:project_id/webdav/state (P6.9 webdav).
func hProjectState(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, ok := wdAuthzProject(a, cxt, res); !ok {
			return
		}
		ctx := cxt.Req.Context()
		d, found := wdGetState(ctx, a, cxt.Params["1"])
		if !found {
			res.JSON(200, []byte(`{"connected":false}`))
			return
		}
		// Node: getProjectState(…, {verifyConnection:false}) → state doc +
		// connected:true (appended at the end).
		b := wdOrderedJSON(d)
		if strings.HasSuffix(b, "}") {
			b = b[:len(b)-1] + `,"connected":true}`
		}
		res.JSON(200, []byte(b))
	}
}

// hFiles — GET /project/:project_id/webdav/files (P6.9 webdav).
func hFiles(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, ok := wdAuthzProject(a, cxt, res); !ok {
			return
		}
		ctx := cxt.Req.Context()
		uid := wdUID(cxt)
		if _, found := wdGetState(ctx, a, cxt.Params["1"]); !found {
			res.JSON(404, []byte(`{"message":"Project not linked to WebDAV"}`))
			return
		}
		plain, has := wdGetCreds(ctx, a, uid)
		if !has {
			res.JSON(500, []byte(`{"message":"WebDAV is not connected"}`))
			return
		}
		cl, cerr := wdNewCredsClient(plain)
		if cerr != nil {
			res.JSON(500, []byte(`{"message":`+wdJSString(cerr.Error())+`}`))
			return
		}
		projectName := wdProjectName(ctx, a, cxt.Params["1"])
		rootPath, _ := wdCredField(plain, "rootPath")
		root := wdRemotePath(orEmpty(rootPath), projectName)
		items, lerr := cl.list(ctx, root)
		if lerr != nil {
			res.JSON(500, []byte(`{"message":`+wdJSString(lerr.Error())+`}`))
			return
		}
		var out []string
		for _, it := range items {
			if it.isDirectory {
				continue
			}
			out = append(out, `{"path":`+wdJSString(it.path)+`,"size":`+orNullNum(it.size)+`,"lastModified":`+wdJSString(orEmpty(it.modifiedAt))+`}`)
		}
		res.JSON(200, []byte(`{"files":[`+strings.Join(out, ",")+`]}`))
	}
}

// hPull — POST /project/:project_id/webdav/pull (P6.9 webdav).
func hPull(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, ok := wdAuthzProject(a, cxt, res); !ok {
			return
		}
		ctx := cxt.Req.Context()
		uid := wdUID(cxt)
		if _, ok := wdGetCreds(ctx, a, uid); !ok {
			// Node pollProject: no credentials → throw Error{status:409}
			res.JSON(409, []byte(`{"message":"WebDAV is not connected"}`))
			return
		}
		if err := wdPollProject(ctx, a, uid, cxt.Params["1"]); err != nil {
			res.JSON(500, []byte(`{"message":`+wdJSString(err.Error())+`}`))
			return
		}
		res.JSON(200, []byte(`{"success":true,"message":"Pull completed"}`))
	}
}

// hPush — POST /project/:project_id/webdav/push (P6.9 webdav).
func hPush(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, ok := wdAuthzProject(a, cxt, res); !ok {
			return
		}
		ctx := cxt.Req.Context()
		uid := wdUID(cxt)
		if _, ok := wdGetCreds(ctx, a, uid); !ok {
			// Node syncProject: no credentials → throw Error (no status) → 500
			res.JSON(500, []byte(`{"message":"WebDAV is not connected"}`))
			return
		}
		if err := wdSyncProject(ctx, a, uid, cxt.Params["1"]); err != nil {
			res.JSON(500, []byte(`{"message":`+wdJSString(err.Error())+`}`))
			return
		}
		res.JSON(200, []byte(`{"success":true,"message":"Push completed"}`))
	}
}

// hResolveConflict — POST /project/:project_id/webdav/conflict/resolve.
func hResolveConflict(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, ok := wdAuthzProject(a, cxt, res); !ok {
			return
		}
		ctx := cxt.Req.Context()
		uid := wdUID(cxt)

		body, err := wdReadBodyJSON(cxt)
		if err != nil {
			res.JSON(400, []byte(`{"message":"Missing required parameters: path and choice are required"}`))
			return
		}
		path, pathOK, _ := wdStrField(body, "path")
		choice, choiceOK, _ := wdStrField(body, "choice")
		if !pathOK || path == "" || !choiceOK || choice == "" {
			res.JSON(400, []byte(`{"message":"Missing required parameters: path and choice are required"}`))
			return
		}
		if choice != "local" && choice != "remote" {
			res.JSON(400, []byte(`{"message":"Invalid choice. Must be 'local' or 'remote'"}`))
			return
		}

		// ConflictResolver.resolve: no recorded conflict matching (path) →
		// ConflictNotFoundError → controller 404 CONFLICT_NOT_FOUND.
		conflictPath, hasConflict := wdActiveConflictPath(ctx, a, uid, cxt.Params["1"], path)
		if !hasConflict {
			res.JSON(404, []byte(`{"message":"No active conflict for this file","errorCode":"CONFLICT_NOT_FOUND"}`))
			return
		}
		if err := wdResolveConflictWork(ctx, a, uid, cxt.Params["1"], conflictPath, choice); err != nil {
			res.JSON(500, []byte(`{"message":`+wdJSString(err.Error())+`}`))
			return
		}
		res.JSON(200, []byte(`{"success":true,"message":`+wdJSString(`Conflict resolved - keeping `+choice+` version`)+`,"path":`+wdJSString(path)+`}`))
	}
}

// hLink — POST /project/:project_id/webdav/link (P6.9 webdav).
func hLink(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, ok := wdAuthzProject(a, cxt, res); !ok {
			return
		}
		ctx := cxt.Req.Context()
		uid := wdUID(cxt)

		plain, _ := wdGetCreds(ctx, a, uid)
		baseURL, hasBase := wdCredField(plain, "baseUrl")
		username, hasUser := wdCredField(plain, "username")
		password, hasPass := wdCredField(plain, "password")
		rootPath, hasRoot := wdCredField(plain, "rootPath")

		rp := ""
		if hasRoot && rootPath != nil {
			rp = *rootPath
		}
		if rp == "" {
			rp = wdSettingsRootPath(ctx, a)
		}
		if rp == "" {
			rp = "/Overleaf"
		}

		if !hasBase || baseURL == nil || *baseURL == "" {
			res.JSON(400, []byte(`{"message":"WebDAV credentials are not configured"}`))
			return
		}
		if !hasUser || username == nil || *username == "" || !hasPass || password == nil || *password == "" {
			res.JSON(400, []byte(`{"message":"WebDAV credentials are incomplete"}`))
			return
		}

		cl, cerr := wdNewCredsClient(plain)
		if cerr != nil {
			res.JSON(500, []byte(`{"message":`+wdJSString(cerr.Error())+`}`))
			return
		}
		if cherr := cl.check(ctx); cherr != nil {
			// node: err?.status || err?.response?.status || 500
			code := 500
			if he, okk := cherr.(*wdHTTPError); okk {
				code = he.status(500)
			}
			res.JSON(code, []byte(`{"message":`+wdJSString(cherr.Error())+`}`))
			return
		}

		if serr := wdCreateState(ctx, a, cxt.Params["1"], []bson.E{
			{Key: "connected", Value: true},
			{Key: "baseUrl", Value: *baseURL},
			{Key: "rootPath", Value: rp},
			{Key: "username", Value: *username},
			{Key: "ownerId", Value: uid},
			{Key: "lastSyncAt", Value: nil},
			{Key: "mergeStatus", Value: "clean"},
		}); serr != nil {
			res.JSON(500, []byte(`{"message":`+wdJSString(serr.Error())+`}`))
			return
		}
		// Initial push (best-effort in the sandbox; on failure Node removes
		// the orphan state + rethrows the push error).
		if perr := wdSyncProject(ctx, a, uid, cxt.Params["1"]); perr != nil {
			_ = wdRemoveState(ctx, a, cxt.Params["1"])
			res.JSON(500, []byte(`{"message":`+wdJSString(perr.Error())+`}`))
			return
		}

		res.JSON(200, []byte(`{"success":true,"message":"Project linked to WebDAV successfully","state":{"connected":true,"baseUrl":`+wdJSString(*baseURL)+`,"rootPath":`+wdJSString(rp)+`}}`))
	}
}

// hProjectName — GET /project/:project_id/webdav/project-name.
func hProjectName(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, ok := wdAuthzProject(a, cxt, res); !ok {
			return
		}
		ctx := cxt.Req.Context()
		name := wdProjectName(ctx, a, cxt.Params["1"])
		if name == "" {
			name = "project_" + cxt.Params["1"]
		}
		res.JSON(200, []byte(`{"projectName":`+wdJSString(name)+`}`))
	}
}

// hUnlink — DELETE /project/:project_id/webdav/state.
func hUnlink(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, ok := wdAuthzProject(a, cxt, res); !ok {
			return
		}
		ctx := cxt.Req.Context()
		d, found := wdGetState(ctx, a, cxt.Params["1"])
		if !found {
			res.JSON(404, []byte(`{"message":"Project is not linked to WebDAV","errorCode":"PROJECT_NOT_LINKED"}`))
			return
		}
		if err := wdRemoveState(ctx, a, cxt.Params["1"]); err != nil {
			res.JSON(500, []byte(`{"message":`+wdJSString(err.Error())+`}`))
			return
		}
		// C3: forget the project on the owning user's credentials (best-effort).
		if owner, ok := wdDocVal(d, "ownerId"); ok {
			if oh, ok := oidHex(owner); ok {
				wdOwnerForgetProject(ctx, a, oh, wdProjectName(ctx, a, cxt.Params["1"]))
			}
		}
		res.JSON(200, []byte(`{"success":true,"message":"Project unlinked from WebDAV"}`))
	}
}

// hImport — POST /project/new/webdav (NO project param; login-only).
func hImport(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		uid := wdUID(cxt)

		body, err := wdReadBodyJSON(cxt)
		if err != nil {
			res.JSON(400, []byte(`{"error":"projectName is required"}`))
			return
		}
		projectName, nameOK, nameNull := wdStrField(body, "projectName")
		rootPath, _, rootNull := wdStrField(body, "rootPath")
		if rootNull {
			rootPath = ""
		}
		if !nameOK || nameNull || projectName == "" {
			res.JSON(400, []byte(`{"error":"projectName is required"}`))
			return
		}

		plain, has := wdGetCreds(ctx, a, uid)
		if !has {
			res.JSON(500, []byte(`{"error":"WebDAV is not connected"}`))
			return
		}
		cl, cerr := wdNewCredsClient(plain)
		if cerr != nil {
			res.JSON(500, []byte(`{"error":`+wdJSString(cerr.Error())+`}`))
			return
		}
		root := wdRemotePath(rootPath, projectName)
		if _, ierr := wdImportRemote(ctx, a, uid, cl, root, projectName); ierr != nil {
			res.JSON(500, []byte(`{"error":`+wdJSString(ierr.Error())+`}`))
			return
		}
		res.JSON(200, []byte(`{"success":true,"message":"Import completed"}`))
	}
}

// ---- small utilities -------------------------------------------------------

func orEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func orNullNum(v interface{}) string {
	if v == nil {
		return `null`
	}
	switch n := v.(type) {
	case int64:
		return itoa64(n)
	case int32:
		return itoa64(int64(n))
	}
	return `null`
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// wdProjectName — project name by id ("" on lookup failure/absent).
func wdProjectName(ctx context.Context, a *core.App, projectID string) string {
	oid, err := primitive.ObjectIDFromHex(strings.ToLower(projectID))
	if err != nil {
		return ""
	}
	doc, _ := wdLoadProject(a, ctx, oid)
	if doc == nil {
		return ""
	}
	name, _ := wdDocStr(*doc, "name")
	return name
}

// wdRemotePath — Node WebdavPaths.remotePath(rootPath, projectName):
// join with '/' and normalize (strip double slashes, keep leading '/').
func wdRemotePath(rootPath, projectName string) string {
	rp := strings.TrimSpace(rootPath)
	if rp == "" {
		rp = "/"
	}
	if rp[0] != '/' {
		rp = "/" + rp
	}
	rp = strings.TrimRight(rp, "/")
	name := strings.TrimSpace(projectName)
	if name == "" {
		name = "untitled"
	}
	name = strings.TrimPrefix(name, "/")
	return rp + "/" + name
}

// wdSettingsRootPath — site_settings.webdav.rootPath when set.
func wdSettingsRootPath(ctx context.Context, a *core.App) string {
	if a.Mongo == nil {
		return ""
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return ""
	}
	var rec struct {
		Webdav *bson.D `bson:"webdav"`
	}
	if err := db.Collection("site_settings").FindOne(ctx, bson.D{{Key: "_id", Value: "webdav"}}).Decode(&rec); err != nil {
		// try the single-doc store
		var rec2 struct {
			Webdav *bson.D `bson:"webdav"`
		}
		if err2 := db.Collection("site_settings").FindOne(ctx, bson.D{}).Decode(&rec2); err2 == nil && rec2.Webdav != nil {
			for _, e := range *rec2.Webdav {
				if e.Key == "rootPath" {
					if s, ok := e.Value.(string); ok {
						return s
					}
				}
			}
			return ""
		}
		return ""
	}
	if rec.Webdav == nil {
		return ""
	}
	for _, e := range *rec.Webdav {
		if e.Key == "rootPath" {
			if s, ok := e.Value.(string); ok {
				return s
			}
		}
	}
	return ""
}

// wdOwnerForgetProject — remove the project name from the owner's
// credentials syncedProjects + remoteState (C3 unlink cleanup, best-effort;
// the state doc (authoritative link) is already removed by the caller).
func wdOwnerForgetProject(ctx context.Context, a *core.App, uid, projectName string) {
	if uid == "" || projectName == "" {
		return
	}
	plain, ok := wdGetCreds(ctx, a, uid)
	if !ok {
		return
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(plain), &m); err != nil {
		return
	}
	changed := false
	if sp, ok := m["syncedProjects"]; ok {
		var arr []json.RawMessage
		if json.Unmarshal(sp, &arr) == nil {
			var keep []string
			for _, e := range arr {
				var s string
				if json.Unmarshal(e, &s) == nil && s == projectName {
					continue
				}
				keep = append(keep, string(e))
			}
			b, _ := json.Marshal(keep)
			m["syncedProjects"] = json.RawMessage(b)
			changed = true
		}
	}
	if rs, ok := m["remoteState"]; ok {
		var rsm map[string]json.RawMessage
		if json.Unmarshal(rs, &rsm) == nil {
			if _, had := rsm[projectName]; had {
				delete(rsm, projectName)
				b, _ := json.Marshal(rsm)
				m["remoteState"] = json.RawMessage(b)
				changed = true
			}
		}
	}
	if changed {
		out, _ := json.Marshal(m)
		_ = wdSaveCreds(ctx, a, uid, string(out))
	}
}

// Feature — the core.Feature registration (13 routes; Node module order).
func Feature(a *core.App) core.Feature {
	return core.Feature{
		Name: "webdav",
		Routes: []core.Route{
			{Method: "GET", Path: "/user/webdav/status", Handler: hStatus(a)},
			{Method: "POST", Path: "/user/webdav/connect", Handler: hConnect(a)},
			{Method: "POST", Path: "/user/webdav/disconnect", Handler: hDisconnect(a)},
			{Method: "GET", Pattern: wdPatState, Handler: hProjectState(a)},
			{Method: "GET", Pattern: wdPatFiles, Handler: hFiles(a)},
			{Method: "POST", Pattern: wdPatPull, Handler: hPull(a)},
			{Method: "POST", Pattern: wdPatPush, Handler: hPush(a)},
			{Method: "POST", Pattern: wdPatConflict, Handler: hResolveConflict(a)},
			{Method: "POST", Pattern: wdPatLink, Handler: hLink(a)},
			{Method: "GET", Pattern: wdPatName, Handler: hProjectName(a)},
			{Method: "DELETE", Pattern: wdPatState, Handler: hUnlink(a)},
			{Method: "POST", Path: "/project/new/webdav", Handler: hImport(a)},
		},
	}
}

// Route patterns (core matches by full-path regex; the :project_id segment is
// group 1).
var (
	wdPatState    = compile(`^/project/([^/]+)/webdav/state$`)
	wdPatFiles    = compile(`^/project/([^/]+)/webdav/files$`)
	wdPatPull     = compile(`^/project/([^/]+)/webdav/pull$`)
	wdPatPush     = compile(`^/project/([^/]+)/webdav/push$`)
	wdPatConflict = compile(`^/project/([^/]+)/webdav/conflict/resolve$`)
	wdPatLink     = compile(`^/project/([^/]+)/webdav/link$`)
	wdPatName     = compile(`^/project/([^/]+)/webdav/project-name$`)
)

func compile(p string) *regexp.Regexp { return regexp.MustCompile(p) }
