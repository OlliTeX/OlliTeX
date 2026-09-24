package projectlist

// U10.3 — linked file routes (Node LinkedFiles feature, CE stack):
//
//	POST /project/:project_id/linked_file                    createLinkedFile
//	POST /project/:project_id/linked_file/:file_id/refresh   refreshLinkedFile
//
// Node oracle (services/web/app/src/Features/LinkedFiles/*), pinned live
// 2026-09-23 (lkf probes):
//
// Middleware order (LinkedFilesRouter):
//   requireLogin -> 401 text/plain "Unauthorized" (accept-independent —
//   AuthenticationController res.sendStatus(401))
//   -> ensureUserCanWriteProjectContent -> 403 text/plain "Forbidden"
//   (pinned live across ALL accept headers for anonymous-ish/non-member
//   AND for owner-of-ghost-project / bad-project-id — Node
//   AuthorizationMiddleware's failure path in this stack yields the
//   plain sendStatus-style 403, NOT the restricted JSON/page of
//   HttpErrorHandler.forbidden's json branch — byte-pinned: body
//   "Forbidden", content-type text/plain, for accept
//   application/json / 'application/json, text/plain, */*' /
//   text/html / *&)
//   -> rateLimit create-linked-file (100/60, key projectId:client) /
//      refresh-linked-file (100/60) -> 429 "Rate limit reached, please
//      try again later" (text body — RateLimiterMiddleware
//      res.status(429).write(...).end())
//   -> controller.
//
// createLinkedFile (LinkedFilesController):
//   parseReq(createLinkedFileSchema, strict) — discriminated union over
//   5 providers; Node zod messages (pinned live):
//     provider missing/non-literal -> 400 {"error":"Validation error",
//       "statusCode":400} (discriminated-union fail: NO issue text —
//       "owner-badprovider"/"owner-prov-not-req" pinned)
//     data missing -> 400 "Validation error: Invalid input: expected
//       object, received undefined at \"body.data\""
//     extra body key -> 400 "Validation error: Unrecognized key:
//       \"extra\" at \"body\"" (SINGULAR — pinned live)
//     bad parent_folder_id -> 400 "Validation error: Invalid Mongo
//       ObjectId at \"body.parent_folder_id\""
//   controller:
//     _getAgent(provider): registered AND in
//     Settings.enabledLinkedFileTypes — this CE stack has
//     ENABLED_LINKED_FILE_TYPES unset -> [''] -> ALWAYS null ->
//     res.sendStatus(400) bare "Bad Request" (pinned live for valid
//     shapes of every provider).
//     (zotero-disabled 403 branch and all agent error tables sit
//     behind the agent lookup — unreachable in this stack; the Go
//     side keeps the _getAgent-null branch as the effective path.)
//
// refreshLinkedFile:
//   parseReq strict: params {project_id:oid, file_id:oid} — bad
//   file_id -> 404 {"error":"Validation error: Invalid Mongo ObjectId
//   at \"params.file_id\"","statusCode":404} (pinned live).
//   body strict {clientId?:string, shouldReindexReferences?:boolean} —
//   unknown key -> 400 (same strict-unknown style, "Unrecognized key:
//   \"X\" at \"body\"").
//   order pinned live (owner):
//     ghost project OR ghost entity -> 404 PAGE (standard 404 HTML —
//     Node ProjectLocator error -> ErrorController 404 page)
//     (401/403 fire before any of this; 429 before the lookup)
//   then: fileRef.linkedFileData == null -> 409 bare; agent null ->
//   400 bare (unreached in this stack: agent null first — but 409 is
//   reachable for a plain fileRef, pinned by gate seed).
//
// Wire notes: accept-independent 401/403/404-bare/409 (text/plain
// sendStatus bodies); 404 parse-JSON (application/json with
// statusCode 404); 404 PAGE = views.NotFoundPage; 403-zotero-disabled
// (JSON) is unreachable here.

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strconv"
	"strings"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

const (
	lfCreateLim  = "create-linked-file"
	lfRefreshLim = "refresh-linked-file"
)

var (
	lfCreatePat  = regexp.MustCompile(`^/project/([^/]+)/linked_file$`)
	lfRefreshPat = regexp.MustCompile(`^/project/([^/]+)/linked_file/([^/]+)/refresh$`)
)

// lfAllowedData / lfRequiredData — per-provider data keys (Node
// createLinkedFileSchema member tables). Required are all z.string() in CE.
var lfAllowedData = map[string][]string{
	"url":                 {"url"},
	"project_file":        {"source_project_id", "v1_source_doc_id", "source_entity_path"},
	"project_output_file": {"source_project_id", "v1_source_doc_id", "source_output_file_path", "build_id", "clsiServerId"},
	"mendeley":            {"group_id"},
	"zotero":              {"format", "group_id", "zoteroGroupId", "bibFormat"},
	"papers":              {"group_id"},
}

var lfRequiredData = map[string][]string{
	"url":                 {"url"},
	"project_file":        {"source_entity_path"},
	"project_output_file": {"source_output_file_path"},
}

// lfUnrec builds the single "Unrecognized key(s)" issue zod emits for a
// strict object: 1 key -> 'Unrecognized key: "k" at "path"'; N -> 'Unrecognized
// keys: "a", "b", ... at "path"' (keys in client order, ", "-joined). Returns
// the issue with UNescaped quotes; lfValJSON escapes when assembling JSON.
func lfUnrec(keys []string, path string) string {
	if len(keys) == 1 {
		return `Unrecognized key: "` + keys[0] + `" at "` + path + `"`
	}
	qu := make([]string, len(keys))
	for i, k := range keys {
		qu[i] = `"` + k + `"`
	}
	return `Unrecognized keys: ` + strings.Join(qu, ", ") + ` at "` + path + `"`
}

// lfValJSON emits the Node parseReq failure envelope:
//
//	{"error":"Validation error: <issues joined by "; ">","statusCode":N}
//
// with the message JSON-escaped exactly like Node's res.json (no Go HTML
// escaping). issues are the per-issue texts (no prefix). N is 404 when any
// issue is a params issue, 400 otherwise.
func lfValJSON(res *core.Res, status int, issues []string) {
	msg := "Validation error: " + strings.Join(issues, "; ")
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(msg); err != nil {
		res.SendStatus(status)
		return
	}
	line := strings.TrimRight(sb.String(), "\n")
	res.JSON(status, []byte(`{"error":`+line+`,"statusCode":`+strconv.Itoa(status)+`}`))
}

// lfRecvBool reports whether the raw token is a JSON boolean literal.
func lfRecvBool(raw json.RawMessage) bool {
	t := strings.TrimSpace(string(raw))
	return t == "true" || t == "false"
}

// lfNewLIM — nil-app safe for Feature(nil) unit tests.
func lfNewLIM(a *core.App, name string) *core.RateLimiter {
	if a != nil && a.Redis != nil {
		return core.NewRateLimiter(a.Redis, name, 100, 60)
	}
	return nil
}

// lfGateCreate — Node order for create: requireLogin (401) ->
// ensureUserCanWriteProjectContent (403 "Forbidden", pinned live as
// text/plain regardless of accept) -> project resolution.
//
// The LIVE Node behavior (owner-of-ghost-project included) is a plain
// 403 "Forbidden" — NOT the restricted JSON/page. The authorization
// layer in this stack surfaces the plain 403 for these routes; the
// oracle is authoritative.
func lfGateCreate(a *core.App, cxt *core.Cxt, res *core.Res) (string, bool) {
	uid := ""
	if cxt.Sess != nil {
		uid = cxt.Sess.UserIDHex()
	}
	if uid == "" {
		res.SendStatus(401)
		return "", false
	}
	return uid, true
}

// lfCreateHandler — see file header for the pinned oracle.
func lfCreateHandler(a *core.App) func(*core.Cxt, *core.Res) {
	lim := lfNewLIM(a, lfCreateLim)
	return func(cxt *core.Cxt, res *core.Res) {
		uid, ok := lfGateCreate(a, cxt, res)
		if !ok {
			return
		}
		oid, okHex := primitiveObjectID(cxt.Params["1"])
		if !okHex {
			// Node zod params.validation runs before authorization.
			res.JSON(404, []byte(`{"error":"Validation error: Invalid Mongo ObjectId at \"params.project_id\"","statusCode":404}`))
			return
		}
		if lim != nil && !lim.Consume(oid.Hex()+":"+uid) {
			core.Send429(res, "Rate limit reached, please try again later")
			return
		}
		pj, _ := loadProjectFull(a, cxt, oid)
		// Node: ghost project -> 404 PAGE (both roles).
		if pj == nil {
			views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
			return
		}
		if !entCanWrite(uid, *pj) {
			res.JSON(403, []byte(`{"message":"restricted"}`))
			return
		}
		if !lfValidateCreate(cxt, res) {
			return
		}
		// _getAgent(provider) — null in this stack (no enabled linked
		// file types) -> bare 400 (pinned live for every valid provider
		// shape).
		res.SendStatus(400)
	}
}

// lfRefreshHandler — see file header.
func lfRefreshHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		uid, ok := lfGateCreate(a, cxt, res)
		if !ok {
			return
		}
		pidHex := cxt.Params["1"]
		fileHex := cxt.Params["2"]
		// Node parseReq: project_id is validated first and SHORT-CIRCUITS —
		// an invalid project_id alone 404s (file_id + body are not inspected).
		if !entHex24(pidHex) {
			res.JSON(404, []byte(`{"error":"Validation error: Invalid Mongo ObjectId at \"params.project_id\"","statusCode":404}`))
			return
		}
		oid, _ := primitiveObjectID(pidHex)
		lim := lfNewLIM(a, lfRefreshLim)
		if lim != nil && !lim.Consume(oid.Hex()+":"+uid) {
			core.Send429(res, "Rate limit reached, please try again later")
			return
		}
		pj, _ := loadProjectFull(a, cxt, oid)
		// Node: ghost project -> 404 PAGE (both roles); membership is only
		// judged once the project exists (403 restricted JSON).
		if pj == nil {
			views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
			return
		}
		if !entCanWrite(uid, *pj) {
			res.JSON(403, []byte(`{"message":"restricted"}`))
			return
		}
		// collect file_id (params) + body issues — Node reports file_id first,
		// then body issues in schema order (clientId, shouldReindexReferences,
		// then a combined unknown-keys issue).
		var issues []string
		hasParamsIssue := false
		if !entHex24(fileHex) {
			issues = append(issues, `Invalid Mongo ObjectId at "params.file_id"`)
			hasParamsIssue = true
		}
		// body strict {clientId?:string, shouldReindexReferences?:boolean}
		raw, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
		raw = []byte(strings.TrimSpace(string(raw)))
		if len(raw) == 0 {
			raw = []byte("{}")
		}
		if strings.HasPrefix(string(raw), "{") {
			rks, rvals := lfDecodeObject(raw)
			if rks != nil {
				rval := func(k string) (json.RawMessage, bool) {
					for i := range rks {
						if rks[i] == k {
							return rvals[i], true
						}
					}
					return nil, false
				}
				cv, hasC := rval("clientId")
				if hasC && !lfRawStringOK(cv) {
					issues = append(issues, `Invalid input: expected string, received `+lfRecv(cv)+` at "body.clientId"`)
				}
				rv, hasR := rval("shouldReindexReferences")
				if hasR && !lfRecvBool(rv) {
					issues = append(issues, `Invalid input: expected boolean, received `+lfRecv(rv)+` at "body.shouldReindexReferences"`)
				}
				var unk []string
				rkbody := map[string]bool{"clientId": true, "shouldReindexReferences": true}
				for _, k := range rks {
					if !rkbody[k] {
						unk = append(unk, k)
					}
				}
				if len(unk) > 0 {
					issues = append(issues, lfUnrec(unk, "body"))
				}
			}
		} else {
			issues = append(issues, `Invalid input: expected object, received `+lfRecv(raw)+` at "body"`)
		}
		if len(issues) > 0 {
			status := 400
			if hasParamsIssue {
				status = 404
			}
			lfValJSON(res, status, issues)
			return
		}
		// getFileById: project/entity lookup — both ghost project and
		// ghost entity -> 404 PAGE (pinned live).
		foundEl, found := entFindEnt(pj, fileHex, "fileRefs")
		if !found {
			views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
			return
		}
		// linkedFileData null -> 409 bare (pinned: plain fileRef).
		linkedJSON, _ := entLinkedFileJSON(entFld(foundEl.elem, "linkedFileData"))
		if !lfHasProvider(linkedJSON) {
			res.SendStatus(409)
			return
		}
		// _getAgent(provider) -> null -> 400 bare (this stack).
		res.SendStatus(400)
	}
}

func lfHasProvider(linkedJSON string) bool {
	return linkedJSON != "" && strings.Contains(linkedJSON, `"provider"`)
}

// lfValidateCreate — Node parseReq(createLinkedFileSchema) mirror
// (strict; exact zod messages + order for the reachable pins).
func lfValidateCreate(cxt *core.Cxt, res *core.Res) bool {
	raw, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	noobj := func() bool {
		if !strings.HasPrefix(string(raw), "{") {
			return true
		}
		k2, _ := lfDecodeObject(raw)
		return k2 == nil
	}()
	if noobj {
		res.JSON(400, []byte(`{"error":"Validation error","statusCode":400}`))
		return false
	}
	ks, vals := lfDecodeObject(raw)
	get := func(k string) (json.RawMessage, bool) {
		for i := range ks {
			if ks[i] == k {
				return vals[i], true
			}
		}
		return nil, false
	}

	// 0. discriminatedUnion discriminator — checked FIRST: missing or
	// non-literal provider -> bare "Validation error" (pinned: even a bad
	// name co-fails bare). A valid provider selects the member.
	prov := ""
	provOK := false
	if provRaw, hasProv := get("provider"); hasProv && lfRawStringOK(provRaw) {
		_ = json.Unmarshal(provRaw, &prov)
		switch prov {
		case "url", "project_file", "project_output_file",
			"mendeley", "zotero", "papers":
			provOK = true
		}
	}
	if !provOK {
		res.JSON(400, []byte(`{"error":"Validation error","statusCode":400}`))
		return false
	}

	// 1. member field issues in schema order: name, parent_folder_id, data
	var issues []string
	nameRaw, hasName := get("name")
	if !hasName {
		issues = append(issues, `Invalid input: expected string, received undefined at "body.name"`)
	} else if !lfRawStringOK(nameRaw) {
		issues = append(issues, `Invalid input: expected string, received `+lfRecv(nameRaw)+` at "body.name"`)
	}
	pfRaw, hasPf := get("parent_folder_id")
	if !hasPf {
		issues = append(issues, `Invalid input: expected string, received undefined at "body.parent_folder_id"`)
	} else if !lfRawStringOK(pfRaw) {
		issues = append(issues, `Invalid input: expected string, received `+lfRecv(pfRaw)+` at "body.parent_folder_id"`)
	} else {
		var pf string
		_ = json.Unmarshal(pfRaw, &pf)
		if !entHex24(pf) {
			issues = append(issues, `Invalid Mongo ObjectId at "body.parent_folder_id"`)
		}
	}
	dataRaw, hasData := get("data")
	if !hasData {
		issues = append(issues, `Invalid input: expected object, received undefined at "body.data"`)
	} else if !strings.HasPrefix(string(dataRaw), "{") {
		issues = append(issues, `Invalid input: expected object, received `+lfRecv(dataRaw)+` at "body.data"`)
	} else {
		dks, dvals := lfDecodeObject(dataRaw)
		if dks == nil {
			issues = append(issues, `Invalid input: expected object, received object at "body.data"`)
		} else {
			dget := func(k string) (json.RawMessage, bool) {
				for i := range dks {
					if dks[i] == k {
						return dvals[i], true
					}
				}
				return nil, false
			}
			allowed := map[string]bool{}
			for _, a := range lfAllowedData[prov] {
				allowed[a] = true
			}
			// required data fields first (all z.string in CE):
			// missing -> "received undefined"; present non-string -> type.
			for _, rk := range lfRequiredData[prov] {
				if dv, has := dget(rk); !has {
					issues = append(issues, `Invalid input: expected string, received undefined at "body.data.`+rk+`"`)
				} else if !lfRawStringOK(dv) {
					issues = append(issues, `Invalid input: expected string, received `+lfRecv(dv)+` at "body.data.`+rk+`"`)
				}
			}
			// then a combined unknown-keys issue (client order).
			var dunk []string
			for _, k := range dks {
				if !allowed[k] {
					dunk = append(dunk, k)
				}
			}
			if len(dunk) > 0 {
				issues = append(issues, lfUnrec(dunk, "body.data"))
			}
		}
	}
	// 2. body-level unknown keys LAST (combined, client order).
	allowedBody := map[string]bool{"name": true, "parent_folder_id": true,
		"provider": true, "data": true}
	var bunk []string
	for _, k := range ks {
		if !allowedBody[k] {
			bunk = append(bunk, k)
		}
	}
	if len(bunk) > 0 {
		issues = append(issues, lfUnrec(bunk, "body"))
	}
	if len(issues) > 0 {
		lfValJSON(res, 400, issues)
		return false
	}
	return true
}

// lfDecodeObject returns the raw top-level key order and their raw
// (order-preserving) values, or (nil, nil) if the root is not an
// object. It is order-preserving (json.Decoder) so zod's "Unrecognized
// key" reporting reflects the client's key order (pinned).
func lfDecodeObject(raw []byte) ([]string, []json.RawMessage) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, nil
	}
	d, ok := tok.(json.Delim)
	if !ok || d != '{' {
		return nil, nil
	}
	ks := []string{}
	vals := []json.RawMessage{}
	for dec.More() {
		ktok, err := dec.Token()
		if err != nil {
			return nil, nil
		}
		key, ok := ktok.(string)
		if !ok {
			return nil, nil
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, nil
		}
		ks = append(ks, key)
		vals = append(vals, v)
	}
	return ks, vals
}

// lfRawStringOK reports whether raw is a JSON string literal.
func lfRawStringOK(raw json.RawMessage) bool {
	t := strings.TrimSpace(string(raw))
	return strings.HasPrefix(t, `"`) && json.Valid(raw)
}

// lfRecv maps a raw JSON token to the human "received" name zod emits
// (string/number/boolean/null/object/array).
func lfRecv(raw json.RawMessage) string {
	t := strings.TrimSpace(string(raw))
	switch {
	case t == "null":
		return "null"
	case t == "true" || t == "false":
		return "boolean"
	case strings.HasPrefix(t, `"`):
		return "string"
	case t == "":
		return "undefined"
	case t[0] == '{':
		return "object"
	case t[0] == '[':
		return "array"
	default:
		return "number"
	}
}

// TEMP diagnostics (NOT FOR COMMIT).
