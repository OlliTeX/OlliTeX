package history

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

// ---------- shared http client ----------

var httpClient = &http.Client{
	// Node: AbortSignal.timeout(120000) on proxy calls; blobs 10min.
	Timeout: 8 * time.Minute,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func reqURL(cxt *core.Cxt) string {
	if q := cxt.Req.URL.RawQuery; q != "" {
		return cxt.Req.URL.Path + "?" + q
	}
	return cxt.Req.URL.Path
}

func bodyBytes(cxt *core.Cxt) []byte {
	if cxt.Req.Body == nil {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 16<<20))
	return b
}

// ---------- V2 (project-history at 127.0.0.1:3054) plumbing ----------

// v2Do issues a V2 request (Node: fetch-utils fetchStreamWithResponse /
// fetchJson). Node treats non-2xx as RequestFailedError (thrown) — returned
// here as upstreamErr for the caller's 500-page mapping. 2xx -> *Response.
func v2Do(cxt *core.Cxt, uid string, rawurl string, method string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest(method, rawurl, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-User-Id", uid)
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 140*time.Second)
	defer cancel()
	resp, err := httpClient.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err // connect error -> Node 500 page (caller)
	}
	if c := resp.StatusCode; c < 200 || c >= 300 {
		_ = resp.Body.Close()
		return nil, upstreamErr(c)
	}
	return resp, nil
}

type upstreamErr int

func (e upstreamErr) Error() string { return "upstream " + strconv.Itoa(int(e)) }

// asUpstream type-asserts an upstreamErr.
func asUpstream(err error) (upstreamErr, bool) {
	ue, ok := err.(upstreamErr)
	return ue, ok
}

// s500 renders Node's global error page (681-byte "Something went wrong").
func (h *svc) s500(cxt *core.Cxt, res *core.Res, path string) {
	views.Error500Page(res.W, page(cxt, path))
}

// passV2 = Node proxyToHistoryApi success path:
//
//	res.status(response.status).set(upstream Content-Type).
//	  set('X-Accel-Buffering','no').pipe(stream)
func (h *svc) passV2(cxt *core.Cxt, res *core.Res, uid, rawurl, method string, body []byte, path string) {
	resp, err := v2Do(cxt, uid, rawurl, method, body)
	if err != nil {
		h.s500(cxt, res, path)
		return
	}
	defer resp.Body.Close()
	// Node: res.set is called only when the upstream header exists; status
	// is 200 (res.status is never called).
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		res.W.Header().Set("Content-Type", ct)
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		res.W.Header().Set("Content-Length", cl)
	} else {
		// Upstream has no CL (204/empty flush, updates stream): Node never
		// adds one — suppress Go's auto CL with explicit chunking.
		res.W.Header().Set("Transfer-Encoding", "chunked")
	}
	res.W.WriteHeader(200)
	_, _ = io.Copy(res.W, resp.Body)
}

// ---------- handlers: V2 passthrough family ----------

// updates (proxyToHistoryApiAndInjectUserDetails).
func (h *svc) updates(cxt *core.Cxt, res *core.Res) {
	uid, _, ok := h.gate(cxt, res, "read")
	if !ok {
		return
	}
	resp, err := v2Do(cxt, uid, v2Base()+reqURL(cxt), "GET", nil)
	if err != nil {
		h.s500(cxt, res, cxt.Req.URL.Path)
		return
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		h.s500(cxt, res, cxt.Req.URL.Path)
		return
	}
	out, err := injectUserDetails(h.a, cxt, raw)
	if err != nil {
		h.s500(cxt, res, cxt.Req.URL.Path)
		return
	}
	res.JSON(200, out)
}

// docDiff adds the doc_id objectId validation (Node docDiffSchema:
// params {Project_id, doc_id} — bad doc_id -> 404 JSON Validation error).
func (h *svc) docDiff(cxt *core.Cxt, res *core.Res) {
	if did := cxt.Params["2"]; did != "" && !validOID.MatchString(did) {
		res.JSON(404, []byte(`{"error":"Validation error: Invalid Mongo ObjectId at \"params.doc_id\"","statusCode":404}`))
		return
	}
	h.proxy(cxt, res)
}

// proxy (diff / filetree/diff — raw proxyToHistoryApi).
func (h *svc) proxy(cxt *core.Cxt, res *core.Res) {
	uid, _, ok := h.gate(cxt, res, "read")
	if !ok {
		return
	}
	h.passV2(cxt, res, uid, v2Base()+reqURL(cxt), cxt.Req.Method, bodyBytes(cxt), cxt.Req.URL.Path)
}

// flush (proxyToHistoryApi; Node: 200 empty body in this stack).
func (h *svc) flush(cxt *core.Cxt, res *core.Res) {
	uid, _, ok := h.gate(cxt, res, "read")
	if !ok {
		return
	}
	h.passV2(cxt, res, uid, v2Base()+reqURL(cxt), "POST", bodyBytes(cxt), cxt.Req.URL.Path)
}

// ---------- V1 (history-v1 at 127.0.0.1:3100/api, basic auth) ----------

type upBody struct {
	ct   string
	body []byte
}

func v1Call(cxt *core.Cxt, rawurl, method string, body []byte) (*upBody, error) {
	req, err := http.NewRequest(method, rawurl, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(v1User(), v1Pass())
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 140*time.Second)
	defer cancel()
	resp, err := httpClient.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 404 {
		return nil, upstreamErr(404)
	}
	if c := resp.StatusCode; c < 200 || c >= 300 {
		return nil, upstreamErr(c)
	}
	return &upBody{ct: resp.Header.Get("Content-Type"), body: b}, nil
}

// latestHistory -> V1 /projects/:historyId/latest/history -> res.json.
func (h *svc) latestHistory(cxt *core.Cxt, res *core.Res) {
	uid, p, ok := h.gate(cxt, res, "read")
	if !ok {
		return
	}
	if p.HistID == "" { // getHistoryId OError -> Node 500 page
		h.s500(cxt, res, cxt.Req.URL.Path)
		return
	}
	r, err := v1Call(cxt, v1Base()+"/projects/"+p.HistID+"/latest/history", "GET", nil)
	if err != nil {
		h.s500(cxt, res, cxt.Req.URL.Path)
		return
	}
	res.JSON(200, r.body)
	_ = uid
}

// changes -> V1 /projects/:historyId/changes?since=N.
func (h *svc) changes(cxt *core.Cxt, res *core.Res) {
	uid, p, ok := h.gate(cxt, res, "read")
	if !ok {
		return
	}
	sq := cxt.Req.URL.Query()
	paginated := sq.Get("paginated") == "true"
	since, sErr := parseSinceParam(sq.Get("since"))
	if sErr != "" {
		res.JSON(400, []byte(`{"error":"Validation error: `+sErr+` at \"query.since\"","statusCode":400}`))
		return
	}
	if p.HistID == "" {
		h.s500(cxt, res, cxt.Req.URL.Path)
		return
	}
	base := v1Base() + "/projects/" + p.HistID + "/changes?since=" + since
	if paginated {
		r, err := v1Call(cxt, base, "GET", nil)
		if err != nil {
			h.s500(cxt, res, cxt.Req.URL.Path)
			return
		}
		res.JSON(200, r.body)
		return
	}
	// Node loop: while (hasMore) { since += changes.length } then
	// res.json(allChanges). One round in practice.
	all := "[]"
	for i := 0; i < 1000; i++ {
		r, err := v1Call(cxt, base, "GET", nil)
		if err != nil {
			h.s500(cxt, res, cxt.Req.URL.Path)
			return
		}
		var obj struct {
			Changes []json.RawMessage `json:"changes"`
			HasMore bool              `json:"hasMore"`
		}
		if json.Unmarshal(r.body, &obj) != nil {
			h.s500(cxt, res, cxt.Req.URL.Path)
			return
		}
		var prev []json.RawMessage
		_ = json.Unmarshal([]byte(all), &prev)
		merged := append(prev, obj.Changes...)
		mb, _ := json.Marshal(merged)
		all = string(mb)
		if !obj.HasMore || len(obj.Changes) == 0 {
			break
		}
		n, _ := strconv.Atoi(since)
		since = strconv.Itoa(n + len(obj.Changes))
		base = v1Base() + "/projects/" + p.HistID + "/changes?since=" + since
	}
	res.JSON(200, []byte(all))
	_ = uid
}

// parseSinceParam mirrors zod z.coerce.number().int().min(0); "" -> 0.
func parseSinceParam(s string) (string, string) {
	if s == "" {
		return "0", ""
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return "", "Invalid input: expected number, received NaN"
	}
	if f != float64(int64(f)) {
		return "", "Invalid input: expected integer, received float"
	}
	if f < 0 {
		return "", "Too small: expected number to be >=0"
	}
	return strconv.FormatInt(int64(f), 10), ""
}

// ---------- labels ----------

// labels GET: V2 /project/:id/labels + _enrichLabels.
func (h *svc) labels(cxt *core.Cxt, res *core.Res) {
	uid, _, ok := h.gate(cxt, res, "read")
	if !ok {
		return
	}
	r, err := v2Body(cxt, uid, v2Base()+"/project/"+cxt.Params["1"]+"/labels", "GET", nil)
	if err != nil {
		h.s500(cxt, res, cxt.Req.URL.Path)
		return
	}
	out, err := enrichLabels(h.a, cxt, r)
	if err != nil {
		h.s500(cxt, res, cxt.Req.URL.Path)
		return
	}
	res.JSON(200, out)
	_ = uid
}

func v2Body(cxt *core.Cxt, uid, url string, method string, body []byte) ([]byte, error) {
	resp, err := v2Do(cxt, uid, url, method, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// createLabel: POST strictObject{comment, version}.
func (h *svc) createLabel(cxt *core.Cxt, res *core.Res) {
	uid, _, ok := h.gate(cxt, res, "wreview")
	if !ok {
		return
	}
	raw := bodyBytes(cxt)
	bm, ok := parseBodyStrict(raw)
	if !ok {
		json400(res, `{"error":"","statusCode":400}`) // replaced below
		return
	}
	if msg := labelIssues(bm); msg != "" {
		res.JSON(400, []byte(`{"error":"Validation error: `+msg+`","statusCode":400}`))
		return
	}
	// Node creates via V2 with {comment, version, user_id} (user_id from the
	// session) and enriches the response with user_display_name.
	vbody, err := json.Marshal(map[string]any{
		"comment": strOr(bm["comment"]),
		"version": numOr(bm["version"]),
		"user_id": uid,
	})
	if err != nil {
		h.s500(cxt, res, cxt.Req.URL.Path)
		return
	}
	r, err := v2Body(cxt, uid, v2Base()+"/project/"+cxt.Params["1"]+"/labels", "POST", vbody)
	if err != nil {
		h.s500(cxt, res, cxt.Req.URL.Path)
		return
	}
	res.JSON(200, h.enrichLabelJSON(cxt, r))
}

func (h *svc) deleteLabel(cxt *core.Cxt, res *core.Res) {
	lid := cxt.Params["2"]
	if lid == "" || !validOID.MatchString(lid) {
		res.JSON(404, []byte(`{"error":"Validation error: Invalid Mongo ObjectId at \"params.label_id\"","statusCode":404}`))
		return
	}
	uid, p, ok := h.gate(cxt, res, "wreview")
	if !ok {
		return
	}
	// Node deleteLabel: owner -> V2 /project/:id/labels/:lid; non-owner
	// (reviewer member) -> V2 /project/:id/user/:uid/labels/:lid; 204 both.
	base := v2Base() + "/project/" + cxt.Params["1"]
	if p.OwnerRef == uid {
		base = base + "/labels/" + lid
	} else {
		base = base + "/user/" + uid + "/labels/" + lid
	}
	_, err := v2Body(cxt, uid, base, "DELETE", nil)
	if err != nil {
		h.s500(cxt, res, cxt.Req.URL.Path)
		return
	}
	res.SendStatus204() // Node deleteLabel: res.sendStatus(204)
}

// ---------- zip ----------

// versionZip: V1 zip of a version with attachment headers.
func (h *svc) versionZip(cxt *core.Cxt, res *core.Res) {
	uid, p, ok := h.gate(cxt, res, "read")
	if !ok {
		return
	}
	vRaw := cxt.Params["2"]
	vErr := versionIssue(vRaw)
	if vErr != "" {
		res.JSON(404, []byte(`{"error":"Validation error: `+vErr+` at \"params.version\"","statusCode":404}`))
		return
	}
	if p.HistID == "" {
		res.SendStatus(402) // pinned: "Payment Required"
		return
	}
	url := v1Base() + "/projects/" + p.HistID + "/version/" + vRaw + "/zip"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		h.s500(cxt, res, cxt.Req.URL.Path)
		return
	}
	req.SetBasicAuth(v1User(), v1Pass())
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 140*time.Second)
	defer cancel()
	resp, err := httpClient.Do(req.WithContext(ctx))
	if err != nil {
		h.s500(cxt, res, cxt.Req.URL.Path)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		res.SendStatus(404) // pinned: "Not Found"
		return
	}
	if c := resp.StatusCode; c < 200 || c >= 300 {
		h.s500(cxt, res, cxt.Req.URL.Path)
		return
	}
	hdr := res.W.Header()
	hdr.Set("Content-Type", "application/zip") // res.attachment(name.zip) pin
	name := p.Name
	if name == "" {
		name = "project"
	}
	hdr.Set("Content-Disposition", `attachment; filename="`+name+` (Version `+vRaw+`).zip"`)
	hdr.Set("X-Content-Type-Options", "nosniff")
	if cxt.Req.Method == http.MethodGet {
		hdr.Set("X-Accel-Buffering", "no")
	}
	// Node streams the zip with NO Content-Length (both GET and HEAD —
	// pinned U10.1 wire); suppress Go's small-body auto CL with chunking.
	if cxt.Req.Method == http.MethodGet {
		hdr.Set("Transfer-Encoding", "chunked")
	}
	res.W.WriteHeader(200)
	if cxt.Req.Method != http.MethodHead {
		_, _ = io.Copy(res.W, resp.Body)
	}
	_ = uid
}
func versionIssue(s string) string {
	if _, err := strconv.ParseFloat(s, 64); err != nil {
		return "Invalid input: expected number, received NaN"
	}
	f, _ := strconv.ParseFloat(s, 64)
	if f != float64(int64(f)) {
		return "Invalid input: expected integer, received float"
	}
	if f < 0 {
		return "Too small: expected number to be >=0"
	}
	return ""
}

// ---------- blob ----------

// blob GET/HEAD.
func (h *svc) blob(cxt *core.Cxt, res *core.Res) {
	uid, p, ok := h.gate(cxt, res, "read")
	if !ok {
		return
	}
	hash := cxt.Params["2"]
	if msg := hashIssue(hash); msg != "" {
		res.JSON(404, []byte(`{"error":"Validation error: `+msg+`","statusCode":404}`))
		return
	}
	if inm := cxt.Req.Header.Get("If-None-Match"); inm == hash {
		res.W.Header().Set("ETag", hash)
		res.W.Header().Set("Cache-Control", "private, max-age=86400, stale-while-revalidate=31536000")
		res.W.WriteHeader(304)
		return
	}
	if p.HistID == "" {
		h.s500(cxt, res, cxt.Req.URL.Path)
		return
	}
	req, err := http.NewRequest(cxt.Req.Method, v1Base()+"/projects/"+p.HistID+"/blobs/"+hash, nil)
	if err != nil {
		h.s500(cxt, res, cxt.Req.URL.Path)
		return
	}
	req.SetBasicAuth(v1User(), v1Pass())
	if rg := cxt.Req.Header.Get("Range"); rg != "" {
		req.Header.Set("Range", rg)
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 8*time.Minute)
	defer cancel()
	resp, err := httpClient.Do(req.WithContext(ctx))
	if err != nil {
		h.s500(cxt, res, cxt.Req.URL.Path)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		res.W.WriteHeader(404) // pinned: empty body, no Content-Type
		return
	}
	if c := resp.StatusCode; c < 200 || c >= 300 {
		h.s500(cxt, res, cxt.Req.URL.Path)
		return
	}
	hdr := res.W.Header()
	hdr.Set("Content-Type", "application/octet-stream")
	hdr.Set("ETag", hash)
	hdr.Set("Cache-Control", "private, max-age=86400, stale-while-revalidate=31536000")
	hdr.Set("X-Accel-Buffering", "no")
	// Node: CL only on HEAD / partial(206) responses — plain GET has none.
	if cl := resp.Header.Get("Content-Length"); cl != "" && (cxt.Req.Method == "HEAD" || resp.Header.Get("Content-Range") != "") {
		hdr.Set("Content-Length", cl)
	}
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		hdr.Set("Content-Range", cr)
	}
	if cxt.Req.Method != "HEAD" && resp.Header.Get("Content-Range") == "" {
		// Node's plain GET blob carries NO Content-Length; a small body makes
		// Go auto-set one — force chunked like Send429 does.
		res.W.Header().Set("Transfer-Encoding", "chunked")
	}
	res.W.WriteHeader(resp.StatusCode)
	if cxt.Req.Method != "HEAD" {
		_, _ = io.Copy(res.W, resp.Body)
	}
	_ = uid
}

// hashIssue mirrors zz.string().regex(/^[0-9a-f]*$/).length(40) — zod
// collects ALL failed checks in chain order (regex, min, max); every issue
// carries the ` at "params.hash"` suffix.
func hashIssue(s string) string {
	issues := []string{}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			issues = append(issues, `Invalid string: must match pattern /^[0-9a-f]*$/ at \"params.hash\"`)
			break
		}
	}
	if len(s) < 40 {
		issues = append(issues, `Too small: expected string to have >=40 characters at \"params.hash\"`)
	}
	if len(s) > 40 {
		issues = append(issues, `Too big: expected string to have <=40 characters at \"params.hash\"`)
	}
	return strings.Join(issues, "; ")
}

// ---------- restore / revert (gates + validation; success = U10.5) ----------
//
// Node success paths run RestoreManager (local fs + mongo + V2) — ported in
// U10.5 with its own oracle gate. U10.1 pins: access gates + exact 400
// body validation + 404 param validation. A fully valid body reaches the
// U10.5 marker (500 page — honest: the Go success path is pending; NOT
// claimed green).

func (h *svc) restoreFile(cxt *core.Cxt, res *core.Res)   { h.writeValidate(cxt, res, true) }
func (h *svc) revertFile(cxt *core.Cxt, res *core.Res)    { h.writeValidate(cxt, res, true) }
func (h *svc) revertProject(cxt *core.Cxt, res *core.Res) { h.writeValidate(cxt, res, false) }

func (h *svc) writeValidate(cxt *core.Cxt, res *core.Res, withPath bool) {
	uid, _, ok := h.gate(cxt, res, "write")
	if !ok {
		return
	}
	raw := bodyBytes(cxt)
	bm, okb := parseBodyStrict(raw)
	if !okb {
		res.JSON(400, []byte(`{"error":"","statusCode":400}`))
		return
	}
	if got := writeValidateIssues(withPath, bm); got != "" {
		res.JSON(400, []byte(`{"error":"Validation error: `+got+`","statusCode":400}`))
		return
	}
	// U10.5 pending: the RestoreManager success path (local fs + folder +
	// addEntity) is not ported yet. Honest marker: 500 page (Node would
	// 200 {type,id} after mutation). Tracked in WEB_GO_PLAN U10.5.
	_ = uid
	h.s500(cxt, res, cxt.Req.URL.Path)
}

// writeValidateIssues mirrors the restore/revert body schemas:
// strictObject{version: int>=0, [pathname: zz.filepath()]}; version is
// validated first in the Node oracle for the revert_file empty body
// ("...received undefined at \"body.version\"; ...body.pathname" order is
// schema order: version, pathname — the pinned oracle shows version
// first).
func writeValidateIssues(withPath bool, bm map[string]any) string {
	issues := []string{}
	if v, ok := bm["version"]; !ok || v == nil {
		issues = append(issues, `Invalid input: expected number, received undefined at \"body.version\"`)
	} else if _, isStr := v.(string); isStr {
		issues = append(issues, `Invalid input: expected number, received string at \"body.version\"`)
	} else if _, isNum := v.(float64); !isNum {
		issues = append(issues, `Invalid input: expected number, received `+jsType(v)+` at \"body.version\"`)
	} else if f := v.(float64); f != float64(int64(f)) {
		issues = append(issues, `Invalid input: expected integer, received float at \"body.version\"`)
	} else if f := v.(float64); f < 0 {
		issues = append(issues, `Too small: expected number to be >=0 at \"body.version\"`)
	}
	if withPath {
		if v, ok := bm["pathname"]; !ok || v == nil {
			issues = append(issues, `Invalid input: expected string, received undefined at \"body.pathname\"`)
		} else if str, isStr := v.(string); !isStr {
			issues = append(issues, `Invalid input: expected string, received `+jsType(v)+` at \"body.pathname\"`)
		} else if str == "" {
			issues = append(issues, `Path is empty at \"body.pathname\"`)
		} else if strings.HasPrefix(str, "/") {
			issues = append(issues, `Path is absolute at \"body.pathname\"`)
		} else if anyPart(str) == ".." {
			issues = append(issues, `Path traversal detected at \"body.pathname\"`)
		}
	}
	if ek := extraKeys(bm, "version", "pathname"); ek != nil {
		issues = append(issues, `Unrecognized key: \"`+ek[0]+`\" at \"body\"`)
	}
	return strings.Join(issues, "; ")
}

func anyPart(s string) string {
	for _, p := range strings.Split(s, "/") {
		if p == ".." {
			return ".."
		}
	}
	return ""
}

func jsType(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case nil:
		return "undefined"
	}
	return "number"
}

// ---------- body helpers (zod strictObject mirrors) ----------

// parseBodyStrict: Node express.json + z.strictObject. Empty body -> {} (ok:
// missing fields become "received undefined"); non-object root -> 400 {}
// (pinned U1: express.json strict "{}").
func parseBodyStrict(raw []byte) (map[string]any, bool) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" {
		return map[string]any{}, true
	}
	var bm map[string]any
	if json.Unmarshal(raw, &bm) != nil || bm == nil {
		return nil, false
	}
	return bm, true
}

func extraKeys(bm map[string]any, allowed ...string) []string {
	ok := map[string]bool{}
	for _, a := range allowed {
		ok[a] = true
	}
	out := []string{}
	for k := range bm {
		if !ok[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func json400(res *core.Res, body string) {
	res.W.Header().Set("Content-Type", "application/json; charset=utf-8")
	res.W.WriteHeader(400)
	_, _ = res.W.Write([]byte(body))
}

// labelIssues mirrors z.strictObject({comment: string, version: int>=0})
// with zod's field order (comment, version).
func labelIssues(bm map[string]any) string {
	issues := []string{}
	if v, ok := bm["comment"]; !ok || v == nil {
		issues = append(issues, `Invalid input: expected string, received undefined at \"body.comment\"`)
	} else if _, isStr := v.(string); !isStr {
		issues = append(issues, `Invalid input: expected string, received `+jsType(v)+` at \"body.comment\"`)
	}
	if v, ok := bm["version"]; !ok || v == nil {
		issues = append(issues, `Invalid input: expected number, received undefined at \"body.version\"`)
	} else if _, isStr := v.(string); isStr {
		issues = append(issues, `Invalid input: expected number, received string at \"body.version\"`)
	} else if _, isNum := v.(float64); !isNum {
		issues = append(issues, `Invalid input: expected number, received `+jsType(v)+` at \"body.version\"`)
	} else if f := v.(float64); f != float64(int64(f)) {
		issues = append(issues, `Invalid input: expected integer, received float at \"body.version\"`)
	} else if f := v.(float64); f < 0 {
		issues = append(issues, `Too small: expected number to be >=0 at \"body.version\"`)
	}
	if ek := extraKeys(bm, "comment", "version"); ek != nil {
		issues = append(issues, `Unrecognized key: \"`+ek[0]+`\" at \"body\"`)
	}
	return strings.Join(issues, "; ")
}

// strOr / numOr — JSON value helpers for the createLabel V2 body.
func strOr(v any) any {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func numOr(v any) any {
	if f, ok := v.(float64); ok {
		return f
	}
	return float64(0)
}

// enrichLabelJSON mirrors Node _enrichLabel: append user_display_name
// (user fetched by label.user_id; "Anonymous" when unresolved/absent)
// without disturbing the V2 key order.
func (h *svc) enrichLabelJSON(cxt *core.Cxt, raw []byte) []byte {
	var o opair
	if err := o.UnmarshalJSON(raw); err != nil {
		return raw
	}
	uidRaw := o.get("user_id")
	dis := "Anonymous"
	if len(uidRaw) > 0 && string(uidRaw) != "null" {
		uidStr := strings.Trim(string(uidRaw), `"`)
		users := loadUsersByIDs(cxt.A, cxt, []string{uidStr})
		if u, ok := users[uidStr]; ok {
			if d := displayNameFrom(u); d != "" {
				dis = d
			}
		}
	}
	o.set("user_display_name", json.RawMessage(`"`+dis+`"`))
	out, err := json.Marshal(o)
	if err != nil {
		return raw
	}
	return out
}
