package library

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"go.mongodb.org/mongo-driver/bson"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

// ---------- LibraryController.mjs ports ----------
//
// sendError — Node's exact mapping:
//   validationReason → 400 {"message": VALIDATION_MESSAGES[reason] || fallback}
//   notFound         → 404 {"message":"Reference not found."}
//   duplicateKey     → 409 {"message":"The citation key \u201cK\u201d is already used
//                       by another reference.","duplicateKey":"K"}
//   anything else    → 500 {"message": fallback}

// VALIDATION_MESSAGES — LibraryController.mjs table (verbatim).
var validationMessages = map[string]string{
	"entry-not-object":       "Each reference must be an object.",
	"entries-missing":        "At least one reference is required.",
	"entries-too-many":       "At most 200 references can be added at once.",
	"key-missing":            "A citation key is required.",
	"key-too-long":           "The citation key is too long.",
	"key-invalid":            "The citation key is invalid (letters, numbers, dot, underscore and dash only).",
	"type-invalid":           "A valid entry type is required.",
	"type-unknown":           "Unknown entry type.",
	"fields-not-array":       "Fields must be a list.",
	"fields-too-many":        "Too many fields in one reference.",
	"field-not-object":       "Each field must be an object with name and value.",
	"field-name-invalid":     "Field names must be lowercase words.",
	"field-value-not-string": "Field values must be strings.",
	"field-value-too-long":   "A field value is too long.",
	"nothing-to-delete":      "Provide ids or a search term to delete.",
}

func validationMessage(reason, fallback string) string {
	if m, ok := validationMessages[reason]; ok {
		return m
	}
	return fallback
}

// patchPat — PATCH /library/references/:key (declared LAST in Node).
var patchPat = regexp.MustCompile(`^/library/references/([^/]+)$`)

// Feature — core.Feature (P6.5). Limiter names/points per LibraryRoutes.mjs:
// page 60/60, api 120/60, writes 60/60 (identifier = client ip, Node
// `identifier: req.ip`).
func Feature(a *core.App) core.Feature {
	pageLim := core.NewRateLimiter(a.Redis, "bib-library-page", 60, 60)
	apiLim := core.NewRateLimiter(a.Redis, "bib-library-api", 120, 60)
	writesLim := core.NewRateLimiter(a.Redis, "bib-library-writes", 60, 60)

	limit := func(l *core.RateLimiter, fn func(cxt *core.Cxt, res *core.Res)) func(*core.Cxt, *core.Res) {
		return func(cxt *core.Cxt, res *core.Res) {
			if l != nil && !l.Consume(core.ClientIP(cxt.Req)) {
				core.Send429(res, "Rate limit reached, please try again later")
				return
			}
			fn(cxt, res)
		}
	}

	f := newFeature(a)
	return core.Feature{
		Name: "library",
		Routes: []core.Route{
			{Method: "GET", Path: "/library", Handler: limit(pageLim, f.page(false))},
			{Method: "GET", Path: "/library/trashed", Handler: limit(pageLim, f.page(true))},
			{Method: "GET", Path: "/library/references", Handler: limit(apiLim, f.list)},
			{Method: "POST", Path: "/library/references", Handler: limit(writesLim, f.create)},
			{Method: "POST", Path: "/library/references/match", Handler: limit(apiLim, f.match)},
			{Method: "GET", Path: "/library/references/count", Handler: limit(apiLim, f.count)},
			{Method: "GET", Path: "/library/references/download", Handler: limit(apiLim, f.download)},
			{Method: "GET", Path: "/library/references/citation-key-suggestions", Handler: limit(apiLim, f.suggestions)},
			{Method: "POST", Path: "/library/references/delete", Handler: limit(writesLim, f.delete)},
			{Method: "POST", Path: "/library/references/restore", Handler: limit(writesLim, f.restore)},
			// LAST (Node registration order): the :key PATCH.
			{Method: "PATCH", Pattern: patchPat, Handler: limit(apiLim, f.update)},
		},
	}
}

// feature — per-app library handlers (one instance per process).
type feature struct {
	app *core.App
	mgr *manager
}

func newFeature(a *core.App) *feature {
	return &feature{app: a, mgr: newManager(a)}
}

// ---------- body / param helpers ----------

// readBody — the core chain already rejected scalar roots; assume an
// object/array/null JSON document.
func readBody(c *core.Cxt) (map[string]any, bool) {
	raw, err := io.ReadAll(io.LimitReader(c.Req.Body, 1<<20))
	if err != nil {
		return nil, false
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil, false
	}
	if m, ok := v.(map[string]any); ok {
		return m, true
	}
	return map[string]any{}, true
}

// ---------- handlers ----------

func (f *feature) page(trash bool) func(*core.Cxt, *core.Res) {
	return func(c *core.Cxt, r *core.Res) {
		if c.Sess == nil || !c.Sess.IsLoggedIn() {
			r.Redirect(c.Req, 302, "/login")
			return
		}
		uid := c.Sess.UserIDHex()
		// Node themeLocals: user + UserSettingsHelper.buildUserSettings.
		udoc, email := f.loadUser(c, uid)
		us := legacyUserSettings(udoc)
		d := views.PageData{Nonce: views.NewNonce()}
		origin := c.SiteURL
		if origin == "" {
			origin = "http://" + c.Req.Host
		}
		d.Origin = origin
		d.CSRFToken = c.Sess.CsrfToken()
		d.UserEmail, d.UserID = core.PageUserSlots(c.Sess)
		d.LibUsersJSON = us
		_ = email
		if trash {
			views.LibraryView(r.W, d, true)
			return
		}
		views.LibraryView(r.W, d, false)
	}
}

// loadUser — hub-style user doc fetch (email + ace + signUpDate).
func (f *feature) loadUser(c *core.Cxt, uid string) (map[string]any, string) {
	if f.app.Mongo == nil || uid == "" {
		return map[string]any{}, ""
	}
	db, err := f.app.Mongo.DB(c.Req.Context())
	if err != nil {
		return map[string]any{}, ""
	}
	oid, err := primitiveObjectIDFromHex(uid)
	if err != nil {
		return map[string]any{}, ""
	}
	var udoc map[string]any
	if err := db.Collection("users").FindOne(c.Req.Context(),
		bson.M{"_id": oid}).Decode(&udoc); err != nil {
		return nil, ""
	}
	email, _ := udoc["email"].(string)
	return udoc, email
}

// list — GET /library/references (Node listReferences).
func (f *feature) list(c *core.Cxt, r *core.Res) {
	q := c.Req.URL.Query()
	search := oneQuery(q, "search")
	if strings.TrimSpace(search) == "" {
		search = ""
	}
	trashed := oneQuery(q, "trashed")
	parsedTrashed := trashed == "true" || trashed == "1" || trashed == "yes"
	cursor := oneQuery(q, "cursor")
	limitRaw := oneQuery(q, "limit")
	limit := 50
	if limitRaw != "" {
		if v, err := strconv.ParseFloat(limitRaw, 64); err == nil {
			if v == 0 {
				v = 50
			}
			n := int64(toFloor(v))
			if n < 1 {
				n = 1
			}
			if n > 200 {
				n = 200
			}
			limit = int(n)
		}
	}
	items, next, err := f.mgr.list(c.Req.Context(), c.Sess.UserIDHex(), parsedTrashed, search, cursor, limit)
	if err != nil {
		jsonErr(r, http.StatusInternalServerError, "References couldn\u2019t be loaded.")
		return
	}
	jsonEntries(r, http.StatusOK, `{"items":[`+entriesJSON(items)+`]`, func() string {
		if next != "" {
			return `,"nextCursor":"` + next + `"}`
		}
		return `,"nextCursor":null}`
	}())
}

func entriesJSON(items []map[string]any) string {
	bodies := make([]string, 0, len(items))
	for i, it := range items {
		bodies = append(bodies, toApiEntry(it, i))
	}
	return strings.Join(bodies, ",")
}

func jsonEntries(r *core.Res, code int, head, tail string) {
	r.JSON(code, []byte(head+tail))
}

func jsonErr(r *core.Res, code int, msg string) {
	r.JSON(code, []byte(`{"message":`+jstr(msg)+`}`))
}

// create — POST /library/references.
func (f *feature) create(c *core.Cxt, r *core.Res) {
	body, ok := readBody(c)
	if !ok || body == nil {
		jsonErr(r, http.StatusBadRequest, "At least one reference is required.")
		return
	}
	entries, _ := body["entries"].([]any)
	// Node: the MANAGER validates first (validateEntryBatch) and throws the
	// reason → controller 400; validate before touching Mongo.
	if reason := validationReasonFromBody(entries); reason != "ok" {
		jsonErr(r, http.StatusBadRequest, validationMessage(reason, "The reference could not be added."))
		return
	}
	items, err := f.mgr.create(c.Req.Context(), c.Sess.UserIDHex(), entryMaps(entries))
	if err != nil {
		jsonErr(r, http.StatusInternalServerError, "The reference could not be added.")
		return
	}
	r.JSON(http.StatusCreated, []byte(`{"items":[`+entriesJSON(items)+`]}`))
}

// match — POST /library/references/match.
func (f *feature) match(c *core.Cxt, r *core.Res) {
	body, _ := readBody(c)
	var entries []any
	if body != nil {
		entries, _ = body["entries"].([]any)
	}
	keys := []string{}
	if entries != nil {
		for _, e := range entries {
			if em, ok := e.(map[string]any); ok {
				if k, ok := em["key"].(string); ok && strings.TrimSpace(k) != "" {
					keys = append(keys, strings.TrimSpace(k))
				}
			}
		}
	}
	matches := f.mgr.matchKeys(c.Req.Context(), c.Sess.UserIDHex(), keys)
	parts := make([]string, 0, len(matches))
	for _, k := range matches {
		parts = append(parts, jstr(k))
	}
	r.JSON(http.StatusOK, []byte(`{"matches":[`+strings.Join(parts, ",")+`]}`))
}

// count — GET /library/references/count.
func (f *feature) count(c *core.Cxt, r *core.Res) {
	q := c.Req.URL.Query()
	search := oneQuery(q, "search")
	if strings.TrimSpace(search) == "" {
		search = ""
	}
	trashed := oneQuery(q, "trashed")
	parsedTrashed := trashed == "true" || trashed == "1" || trashed == "yes"
	n, err := f.mgr.count(c.Req.Context(), c.Sess.UserIDHex(), parsedTrashed, search)
	if err != nil {
		jsonErr(r, http.StatusInternalServerError, "The reference count could not be loaded.")
		return
	}
	r.JSON(http.StatusOK, []byte(`{"count":`+strconv.FormatInt(n, 10)+`}`))
}

// download — GET /library/references/download.
func (f *feature) download(c *core.Cxt, r *core.Res) {
	q := c.Req.URL.Query()
	mode := q.Get("mode")
	if mode != "exclusion" {
		mode = "inclusion"
	}
	search := oneQuery(q, "search")
	if strings.TrimSpace(search) == "" {
		search = ""
	}
	ids := q["ids"] // Node: ids string → split ','; array → as-is
	if len(ids) == 1 && ids[0] != "" {
		parts := []string{}
		for _, s := range strings.Split(ids[0], ",") {
			if s = strings.TrimSpace(s); s != "" {
				parts = append(parts, s)
			}
		}
		ids = parts
	}
	items, err := f.mgr.downloadDocs(c.Req.Context(), c.Sess.UserIDHex(), mode, search, ids)
	if err != nil {
		jsonErr(r, http.StatusInternalServerError, "The library could not be downloaded.")
		return
	}
	bibEntries := make([]bibEntry, 0, len(items))
	for _, it := range items {
		flds := []fieldOut{}
		for _, f := range asAnySlice(it["fields"]) {
			if fm, ok := f.(map[string]any); ok {
				v := ""
				if s, ok := fm["value"].(string); ok {
					v = s
				}
				flds = append(flds, fieldOut{Name: jsStr(fm["name"]), Value: v})
			}
		}
		bibEntries = append(bibEntries, bibEntry{Key: jsStr(it["key"]), Type: jsStr(it["type"]), Fields: flds})
	}
	bib := serializeBibFile(bibEntries)
	h := r.W.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="library.bib"`)
	r.W.WriteHeader(http.StatusOK)
	_, _ = r.W.Write([]byte(bib))
}

// suggestions — GET /library/references/citation-key-suggestions.
func (f *feature) suggestions(c *core.Cxt, r *core.Res) {
	q := c.Req.URL.Query()
	base := q.Get("base")
	keysRaw := q.Get("keys")
	extra := []string{}
	if keysRaw != "" {
		for _, k := range strings.Split(keysRaw, ",") {
			if k = strings.TrimSpace(k); k != "" {
				extra = append(extra, k)
			}
		}
	}
	keys := f.mgr.suggestions(c.Req.Context(), c.Sess.UserIDHex(), base, extra)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, jstr(k))
	}
	r.JSON(http.StatusOK, []byte(`{"keys":[`+strings.Join(parts, ",")+`]}`))
}

// delete — POST /library/references/delete.
func (f *feature) delete(c *core.Cxt, r *core.Res) {
	body, _ := readBody(c)
	var ids []string
	search := ""
	permanent := false
	if body != nil {
		if arr, ok := body["ids"].([]any); ok {
			for _, id := range arr {
				if s, ok := id.(string); ok && s != "" {
					ids = append(ids, s)
				}
			}
		}
		if s, ok := body["search"].(string); ok {
			search = s
		}
		permanent = body["permanent"] == true
	}
	res := f.mgr.delete(c.Req.Context(), c.Sess.UserIDHex(), ids, search, permanent)
	switch {
	case res.nothing:
		jsonErr(r, http.StatusBadRequest, "Provide ids or a search term to delete.")
	case res.err != nil:
		jsonErr(r, http.StatusInternalServerError, "The references could not be deleted.")
	default:
		r.JSON(http.StatusOK, []byte(`{"deletedCount":`+strconv.Itoa(res.count)+`}`))
	}
}

// restore — POST /library/references/restore.
func (f *feature) restore(c *core.Cxt, r *core.Res) {
	body, _ := readBody(c)
	var ids []string
	if body != nil {
		if arr, ok := body["ids"].([]any); ok {
			for _, id := range arr {
				if s, ok := id.(string); ok && s != "" {
					ids = append(ids, s)
				}
			}
		}
	}
	n, err := f.mgr.restore(c.Req.Context(), c.Sess.UserIDHex(), ids)
	if err != nil {
		jsonErr(r, http.StatusInternalServerError, "The references could not be restored.")
		return
	}
	r.JSON(http.StatusOK, []byte(`{"restoredCount":`+strconv.Itoa(n)+`}`))
}

// update — PATCH /library/references/:key.
func (f *feature) update(c *core.Cxt, r *core.Res) {
	keyParam := c.Params["1"]
	body, _ := readBody(c)
	var entry map[string]any
	if body != nil {
		if arr, ok := body["entries"].([]any); ok && len(arr) > 0 {
			if em, ok := arr[0].(map[string]any); ok {
				entry = em
			}
		}
	}
	if entry == nil {
		// Node: `req.body?.entries?.[0] ?? req.body ?? {}` — the BODY itself.
		if body != nil {
			entry = body
		} else {
			entry = map[string]any{}
		}
	}
	res := f.mgr.update(c.Req.Context(), c.Sess.UserIDHex(), keyParam, entry)
	switch res.reason {
	case "notFound":
		jsonErr(r, http.StatusNotFound, "Reference not found.")
	case "duplicateKey":
		msg := "The citation key \u201c" + res.dupKey + "\u201d is already used by another reference."
		r.JSON(http.StatusConflict, []byte(`{"message":`+jstr(msg)+`,"duplicateKey":`+jstr(res.dupKey)+`}`))
	case "":
	default:
		jsonErr(r, http.StatusBadRequest, validationMessage(res.reason, "The reference could not be saved."))
		return
	}
	if res.err != nil {
		jsonErr(r, http.StatusInternalServerError, "The reference could not be saved.")
		return
	}
	r.JSON(http.StatusOK, []byte(toApiEntry(res.doc, 0)))
}

// ---------- small helpers ----------

// oneQuery — Node req.query semantics for a SCALAR param: single value =
// the string; repeated values = an ARRAY (typeof !== 'string' → null in
// Node's checks). So multi-value params degrade to ""/[] here.
func oneQuery(q url.Values, k string) string {
	vs := q[k]
	if len(vs) != 1 {
		return ""
	}
	return vs[0]
}

func toFloor(v float64) int64 {
	// JS Math.floor
	if v >= 0 {
		return int64(v)
	}
	return int64(v) - 1
}

func entryMaps(entries []any) []map[string]any {
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// validationReasonFromBody — Node: the FIRST failing entry's reason wins;
// non-object entry → entry-not-object; missing/empty array → entries-missing.
func validationReasonFromBody(entries []any) string {
	if entries == nil {
		return "entries-missing"
	}
	if len(entries) > maxEntryCount {
		return "entries-too-many"
	}
	for _, e := range entries {
		if m, ok := e.(map[string]any); ok {
			if r := validateEntry(m); r.reason != "ok" {
				return r.reason
			}
		} else {
			return "entry-not-object"
		}
	}
	return "ok"
}
