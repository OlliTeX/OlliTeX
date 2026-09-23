// P4.12c — private API doc trio (router.mjs:1010-1023):
//
//	GET  /project/:Project_id/doc/:doc_id           (DocumentController.getDocument)
//	POST /project/:Project_id/doc/:doc_id           (DocumentController.setDocument)
//	POST /project/:Project_id/doc/:doc_id/changes/reject
//	                                                 (DocumentController.trackChangesRejected)
//
// Auth: `requirePrivateApiAuth` = basic auth against WEB_API_USER /
// WEB_API_PASSWORD. NO membership check — service-to-service.
//
// Node oracle (re-pinned for the WEB profile 2026-09-23; U10.3r):
//
//	no auth  + GET + explicit-json Accept    → 401 'Unauthorized' +
//	                                              WWW-Authenticate: OverleafLogin
//	no auth  + GET + html/none Accept        → 302 Location /login
//	                                              (Accept-negotiated body, Vary: Accept,
//	                                              query stripped)
//	wrong auth + GET (any Accept)            → 401 challenge
//	any state  + POST (both routes)          → 403 'Forbidden' (Node's
//	                                              session+csrf chain blocks the
//	                                              POSTs before basic auth)
//	auth OK  + GET: bad/ghost ids            → WEB 404 HTML page
//	right auth + GET (valid doc)             → handler runs
//
// NEW overleaf.sid cookie: the Node web stack runs the session middleware
// on these routes too (pinned live: fresh sess:<sid> in redis with
// csrfSecret + validationToken, cookie issued on 401/403/302).
//
// GET: project load (bad/ghost → 404 'Not Found') → findElement doc
// (missing → NotFoundError → 404 'Not Found') →
//
//	docstore GET {docstore}/project/{pid}/doc/{did} (fail → 500) →
//	chat GET {chat}/project/{pid}/resolved-thread-ids (fail → 500) →
//	?plain(true) → 200 text/plain joined lines
//	else → 200 JSON in Node key order:
//	  {lines, version, ranges, pathname, projectHistoryId?,
//	   projectHistoryType:"project-history", historyRangesSupport,
//	   otMigrationStage, resolvedCommentIds}
//
// POST (setDocument): {lines:[string], version:int, ranges, lastUpdatedAt?,
// lastUpdatedBy?} → tree check (404 'Not Found' when absent) → docstore POST
// ({lines,version,ranges} → {modified,rev}) → if modified: markAsUpdated
// ($set lastUpdated ms|now + lastUpdatedBy, cond {lastUpdated {$lt ts}}) +
// TPDS addDoc (SaaS — CE no-op) → 200 {"rev":N} or {"rev":N,"modified":true}
//
// POST (changes/reject): CE has no 'trackChangesRejected' hook listeners →
// always 204 No Content (both the !userId short-circuit and the hook branch).
package projectlist

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

var (
	docapiDlPat  = regexp.MustCompile(`^/project/([^/]+)/doc/([^/]+)$`)
	docapiRejPat = regexp.MustCompile(`^/project/([^/]+)/doc/([^/]+)/changes/reject$`)
)

// atomic counter: keeps the "unused helper" lint quiet only if needed.
func crChatBase() string { return crEnvOr("WEB_CHAT_URL", "http://127.0.0.1:3010") }

// dpath walks a document by nested keys (mongo-driver decodes nested docs
// to primitive.M / maps).
// apiText mirrors express res.sendStatus text (404/500) on the api process:
// Content-Type text/plain + exact Content-Length, no nosniff (web baseline
// never applies to the api profile) and no ETag (sendStatus omits it).
func apiText(res *core.Res, code int, body string) {
	res.W.Header().Set("Content-Type", "text/plain; charset=utf-8")
	res.W.Header().Set("ETag", core.EtagWeakBody(body))
	res.W.Header().Set("Content-Length", strconv.Itoa(len(body)))
	res.W.WriteHeader(code)
	_, _ = res.W.Write([]byte(body))
}
func dpath(d primitive.D, path ...string) (any, bool) {
	v := any(d)
	for _, p := range path {
		switch x := v.(type) {
		case primitive.D:
			v = nil
			for _, e := range x {
				if e.Key == p {
					v = e.Value
					break
				}
			}
		case primitive.M:
			v = x[p]
		case map[string]any:
			v = x[p]
		default:
			return nil, false
		}
	}
	return v, true
}

// docapiFindDoc returns findElement's path.fileSystem for a doc id
// (folder-name chain + doc name, '/' joined).
func docapiFindDoc(root any, didHex string) (p string, ok bool) {
	type hit struct{ prefix, name string }
	var walk func(v any, prefix string) *hit
	walk = func(v any, prefix string) *hit {
		if !entIsDocObj(v) {
			return nil
		}
		for _, d := range entArr(entFld(v, "docs")) {
			if entIsDocObj(d) && oidHex(entFld(d, "_id")) == didHex {
				return &hit{prefix: prefix, name: asStr(entFld(d, "name"))}
			}
		}
		for _, f := range entArr(entFld(v, "folders")) {
			if r := walk(f, prefix+asStr(entFld(f, "name"))+"/"); r != nil {
				return r
			}
		}
		return nil
	}
	for _, v := range entArr(root) {
		if r := walk(v, ""); r != nil {
			return "/" + r.prefix + r.name, true
		}
	}
	return "", false
}
func docapiFetch(cxt *core.Cxt, method, url string, body []byte) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(cxt.Req.Context(), method, url, rd)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := crHTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	buf, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	resp.Body.Close()
	return resp.StatusCode, buf, nil
}

// apiXPB wraps a private-API handler with the express default
// X-Powered-By: Express (Node api process sets it on every response —
// pinned on the 200 GET, the 401 and the 404 alike).
func apiXPB(h func(cxt *core.Cxt, res *core.Res)) func(cxt *core.Cxt, res *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		res.W.Header().Set("X-Powered-By", "Express")
		h(cxt, res)
	}
}

// apiCSRF403 — U10.3r (route audit 2026-09-23, pinned live): on the Node WEB
// stack the session+csrf chain blocks the doc trio's two POST routes before
// basic auth — every POST, any auth state / Accept → 403 text/plain
// "Forbidden" + fresh overleaf.sid (see core.APISend403). The Go
// setDocument/reject handlers stay for parity with the :3000 api profile
// but are unreachable via the web entry.
func apiCSRF403(cxt *core.Cxt, res *core.Res) { cxt.A.APISend403(cxt, res) }

// apiDoc404Page — U10.3r (pinned live 2026-09-23): Node's GET doc route on
// the web stack renders the standard 404 HTML page for missing/invalid ids
// (VA and NotFound both land in the web 404 handler — bad-oid AND ghost
// valid-oid both → 404 HTML page). The rendered page rides the web-baseline
// helmet set (post-helmet render) but carries NO X-Powered-By (page render).
func apiDoc404Page(cxt *core.Cxt, res *core.Res) {
	views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
}

// GET /project/:pid/doc/:did
func docapiGetHandler(a *core.App) func(cxt *core.Cxt, res *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		req := cxt.Req
		mm := docapiDlPat.FindStringSubmatch(req.URL.Path)
		if mm == nil {
			views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(req.URL.Path, "/")))
			return
		}
		pidHex, didHex := mm[1], mm[2]
		if !a.APIBasicGate(cxt, res, req) {
			return
		}
		// Valid-cred path (unreachable in e2e — sandbox interceptor blocks the
		// WEB_API password; covered by Go unit test). The rendered 404 page /
		// 200 body rides the web-baseline helmet set + fresh sid; the 200
		// res.send path additionally carries X-Powered-By (pinned on 200 GET),
		// the rendered 404 page does NOT (page render) — set per-response below.
		a.NewAPISessionCookie(cxt, res)
		a.APIHelmet(res, req)

		// U10.3r (pinned live 2026-09-23 on the web stack): Node renders the
		// WEB 404 page for both bad-oid and ghost/missing on the GET route
		// (accept-header independent — the web error handler owns it).
		if !delHex24(pidHex) || !delHex24(didHex) {
			apiDoc404Page(cxt, res)
			return
		}
		oid, _ := primitive.ObjectIDFromHex(pidHex)
		doc, _ := loadProjectFull(a, cxt, oid)
		if doc == nil {
			apiDoc404Page(cxt, res)
			return
		}
		pathName, dok := docapiFindDoc(dget(*doc, "rootFolder"), didHex)
		if !dok {
			apiDoc404Page(cxt, res)
			return
		}

		code, buf, err := docapiFetch(cxt, http.MethodGet,
			crDocstoreBase()+"/project/"+pidHex+"/doc/"+didHex, nil)
		if err != nil || code != http.StatusOK {
			apiText(res, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		var ddoc struct {
			Lines   []string        `json:"lines"`
			Version *int            `json:"version"`
			Ranges  json.RawMessage `json:"ranges"`
		}
		if jerr := json.Unmarshal(buf, &ddoc); jerr != nil || ddoc.Lines == nil {
			apiText(res, http.StatusInternalServerError, "Internal Server Error")
			return
		}

		ccode, cbuf, cerr := docapiFetch(cxt, http.MethodGet,
			crChatBase()+"/project/"+pidHex+"/resolved-thread-ids", nil)
		if cerr != nil || ccode != http.StatusOK {
			apiText(res, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		var cdoc struct {
			ResolvedThreadIDs []string `json:"resolvedThreadIds"`
		}
		_ = json.Unmarshal(cbuf, &cdoc)

		plain := false
		switch strings.ToLower(req.URL.Query().Get("plain")) {
		case "1", "true", "yes", "on":
			plain = true
		}
		if plain {
			// ?plain=1 → res.send semantics: text/plain + weak ETag + helmet
			// nosniff (pinned: api plain-200 carries X-Content-Type-Options),
			// exact Content-Length, X-Powered-By (res.send path — the 200 is a
			// res.send, unlike the rendered 404 page which carries no XPB).
			pl := strings.Join(ddoc.Lines, "\n")
			res.W.Header().Set("X-Powered-By", "Express")
			res.W.Header().Set("Content-Type", "text/plain; charset=utf-8")
			res.W.Header().Set("ETag", core.EtagWeakBody(pl))
			res.W.Header().Set("X-Content-Type-Options", "nosniff")
			res.W.Header().Set("Content-Length", strconv.Itoa(len(pl)))
			res.W.WriteHeader(http.StatusOK)
			_, _ = res.W.Write([]byte(pl))
			return
		}

		// JSON — pinned Node key order; projectHistoryId omitted if absent.
		var b strings.Builder
		lb, _ := json.Marshal(ddoc.Lines)
		b.WriteString(`{"lines":`)
		b.Write(lb)
		b.WriteString(`,"version":`)
		if ddoc.Version == nil {
			b.WriteString("null")
		} else {
			b.WriteString(strconv.Itoa(*ddoc.Version))
		}
		if ddoc.Ranges == nil {
			b.WriteString(`,"ranges":null`)
		} else {
			b.WriteString(`,"ranges":`)
			b.Write(ddoc.Ranges)
		}
		pb, _ := json.Marshal(pathName)
		b.WriteString(`,"pathname":`)
		b.Write(pb)
		if hid, okk := dpath(*doc, "overleaf", "history", "id"); okk && hid != nil {
			hb, _ := json.Marshal(oidHex(hid))
			b.WriteString(`,"projectHistoryId":`)
			b.Write(hb)
		}
		b.WriteString(`,"projectHistoryType":"project-history"`)
		if v, okk := dpath(*doc, "overleaf", "history", "rangesSupportEnabled"); okk {
			if bb, okb := v.(bool); okb && bb {
				b.WriteString(`,"historyRangesSupport":true`)
			} else {
				b.WriteString(`,"historyRangesSupport":false`)
			}
		} else {
			b.WriteString(`,"historyRangesSupport":false`)
		}
		// otMigrationStage is ALWAYS present in the Node payload (literal 0
		// fallback when the doc field is absent) — presence is not conditional.
		if v, okk := dpath(*doc, "overleaf", "history", "otMigrationStage"); okk {
			if fv, okf := toInt(v); okf && fv >= 0 {
				b.WriteString(`,"otMigrationStage":`)
				b.WriteString(strconv.Itoa(fv))
				goto docapiAfterOt
			}
		}
		b.WriteString(`,"otMigrationStage":0`)
	docapiAfterOt:
		// resolvedCommentIds = resolvedThreadIds ∩ ranges.comments[].id
		commentSet := map[string]bool{}
		if ddoc.Ranges != nil {
			var rr struct {
				Comments []struct {
					ID string `json:"id"`
				} `json:"comments"`
			}
			if json.Unmarshal(ddoc.Ranges, &rr) == nil {
				for _, cmt := range rr.Comments {
					commentSet[cmt.ID] = true
				}
			}
		}
		b.WriteString(`,"resolvedCommentIds":[`)
		first := true
		seen := map[string]bool{}
		for _, rid := range cdoc.ResolvedThreadIDs {
			if !commentSet[rid] || seen[rid] {
				continue
			}
			seen[rid] = true
			if !first {
				b.WriteString(",")
			}
			first = false
			qb, _ := json.Marshal(rid)
			b.Write(qb)
		}
		b.WriteString("]}")
		res.W.Header().Set("X-Powered-By", "Express") // res.send path (200), not the 404 page
		res.JSON(200, []byte(b.String()))
	}
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

// POST /project/:pid/doc/:did (setDocument)
func docapiPostHandler(a *core.App) func(cxt *core.Cxt, res *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		req := cxt.Req
		mm := docapiDlPat.FindStringSubmatch(req.URL.Path)
		if mm == nil {
			views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(req.URL.Path, "/")))
			return
		}
		pidHex, didHex := mm[1], mm[2]
		if !a.APIBasicGate(cxt, res, req) {
			return
		}

		// params-VA first (schema order), then body-VA (Node parseReq reports
		// the first failing path: params.*, then body.*).
		if !delHex24(pidHex) {
			res.JSON(404, delParamVA("Project_id"))
			return
		}
		if !delHex24(didHex) {
			res.JSON(404, delParamVA("doc_id"))
			return
		}

		raw, _ := io.ReadAll(io.LimitReader(req.Body, 8<<20))
		type jb struct {
			Lines         *[]string `json:"lines"`
			Version       *int      `json:"version"`
			Ranges        any       `json:"ranges"`
			LastUpdatedAt *int64    `json:"lastUpdatedAt"`
			LastUpdatedBy *string   `json:"lastUpdatedBy"`
		}
		var fields map[string]json.RawMessage
		if jerr := json.Unmarshal(raw, &fields); jerr != nil || fields == nil {
			res.JSON(400, colVa("Invalid input: expected object, received string", "body", 400))
			return
		}
		missing := func(k string) bool { _, okk := fields[k]; return !okk }
		if missing("lines") {
			res.JSON(400, colVa("Invalid input: expected array, received undefined", "body.lines", 400))
			return
		}
		if missing("version") {
			res.JSON(400, colVa("Invalid input: expected number, received undefined", "body.version", 400))
			return
		}
		if missing("ranges") {
			res.JSON(400, colVa("Invalid input: expected object, received undefined", "body.ranges", 400))
			return
		}
		var body jb
		if jerr := json.Unmarshal(raw, &body); jerr != nil {
			// lines (or version) had the wrong type — report the ACTUAL
			// received type at the first malformed field (Node parity:
			// lines:7 → "received number", lines:"s" → "received string").
			if lr, okk := fields["lines"]; okk {
				var lv any
				if json.Unmarshal(lr, &lv) == nil {
					res.JSON(400, colVa("Invalid input: expected array, received "+jsonTypeName(lv), "body.lines", 400))
					return
				}
			}
			if vr, okk := fields["version"]; okk {
				var vv any
				if json.Unmarshal(vr, &vv) == nil {
					res.JSON(400, colVa("Invalid input: expected number, received "+jsonTypeName(vv), "body.version", 400))
					return
				}
			}
			res.JSON(400, colVa("Invalid input: expected object, received string", "body", 400))
			return
		}
		if body.Lines == nil || len(*body.Lines) < 0 {
			res.JSON(400, colVa("Invalid input: expected array, received string", "body.lines", 400))
			return
		}
		if body.Version == nil {
			res.JSON(400, colVa("Invalid input: expected number, received undefined", "body.version", 400))
			return
		}
		if !jsonTypeObject(body.Ranges) {
			res.JSON(400, colVa("Invalid input: expected object, received "+jsonTypeName(body.Ranges), "body.ranges", 400))
			return
		}
		if !lastUpdatedAtOK(body.LastUpdatedAt) {
			res.JSON(400, colVa("Invalid input: expected number, received undefined", "body.lastUpdatedAt", 400))
			return
		}

		oid, _ := primitive.ObjectIDFromHex(pidHex)
		doc, _ := loadProjectFull(a, cxt, oid)
		if doc == nil {
			apiText(res, http.StatusNotFound, "Not Found")
			return
		}
		if _, dok := docapiFindDoc(dget(*doc, "rootFolder"), didHex); !dok {
			apiText(res, http.StatusNotFound, "Not Found")
			return
		}

		pbody := map[string]any{
			"lines":   *body.Lines,
			"version": *body.Version,
			"ranges":  body.Ranges,
		}
		pb, _ := json.Marshal(pbody)
		code, buf, err := docapiFetch(cxt, http.MethodPost,
			crDocstoreBase()+"/project/"+pidHex+"/doc/"+didHex, pb)
		if err != nil || code != http.StatusOK {
			apiText(res, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		var dres struct {
			Modified *bool `json:"modified"`
			Rev      int64 `json:"rev"`
		}
		if jerr := json.Unmarshal(buf, &dres); jerr != nil {
			res.JSON(500, []byte("internal error"))
			return
		}

		modified := dres.Modified == nil || *dres.Modified
		if modified {
			// markAsUpdated — fire-and-forget parity (best effort).
			ts := time.Now().UnixMilli()
			if body.LastUpdatedAt != nil {
				ts = *body.LastUpdatedAt
			}
			mdb, merr := a.Mongo.DB(cxt.Req.Context())
			if merr == nil && mdb != nil {
				_, _ = mdb.Collection("projects").UpdateOne(
					cxt.Req.Context(),
					bson.D{
						{Key: "_id", Value: oid},
						{Key: "lastUpdated", Value: bson.D{{Key: "$lt", Value: ts}}},
					},
					bson.D{{Key: "$set", Value: bson.D{
						{Key: "lastUpdated", Value: ts},
					}}},
				)
			}
		}

		if modified {
			res.JSON(200, []byte(`{"rev":`+strconv.FormatInt(dres.Rev, 10)+`,"modified":true}`))
		} else {
			res.JSON(200, []byte(`{"rev":`+strconv.FormatInt(dres.Rev, 10)+`}`))
		}
	}
}

// POST /project/:pid/doc/:did/changes/reject — CE: always 204.
func docapiRejectHandler(a *core.App) func(cxt *core.Cxt, res *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		req := cxt.Req
		mm := docapiRejPat.FindStringSubmatch(req.URL.Path)
		if mm == nil {
			views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(req.URL.Path, "/")))
			return
		}
		if !a.APIBasicGate(cxt, res, req) {
			return
		}
		// Node sends `res.status(204).send("No Content")` — express computes
		// the ETag over the (stripped) 10-byte body, pinned: 204 carries
		// W/"a-bAsFyilMr4Ra1hIU5PyoyFRunpI".
		res.W.Header().Set("ETag", core.EtagWeakBody("No Content"))
		res.NoContent()
	}
}

func jsonTypeObject(v any) bool {
	switch v.(type) {
	case map[string]any:
		return true
	}
	if rm, ok := v.(json.RawMessage); ok {
		t := bytes.TrimSpace(rm)
		return len(t) > 0 && t[0] == '{'
	}
	return false
}

func jsonTypeName(v any) string {
	switch v.(type) {
	case map[string]any:
		return "object"
	case []string, []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	case nil:
		return "null"
	}
	switch n := v.(type) {
	case float64, int, int64:
		_ = n
		return "number"
	}
	if _, ok := v.(json.RawMessage); ok {
		t := bytes.TrimSpace(v.(json.RawMessage))
		switch {
		case len(t) > 0 && (t[0] == '{'):
			return "object"
		case len(t) > 0 && (t[0] == '['):
			return "array"
		case len(t) > 0 && (t[0] == '"'):
			return "string"
		case string(t) == "true" || string(t) == "false":
			return "boolean"
		case string(t) == "null":
			return "null"
		default:
			return "number"
		}
	}
	return "undefined"
}

func lastUpdatedAtOK(v *int64) bool { return v == nil || *v > 0 }
