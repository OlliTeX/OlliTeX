package templates

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

// ---- filestore base (Node settings.apis.filestore.url) --------------------

func tplFilestoreBase() string {
	if v := os.Getenv("WEB_FILESTORE_URL"); v != "" {
		return v
	}
	h := os.Getenv("FILESTORE_HOST")
	if h == "" {
		h = "127.0.0.1"
	}
	return "http://" + h + ":3009"
}

// tplFSGet — one GET against the filestore. Node: fetchStreamWithResponse
// (30s timeout) throws RequestFailedError on any non-2xx; the body is
// carried into error.info ONLY for 400/409/413/422 (pinned live).
// ok=false ⇔ Node's thrown error (transport failure included).
func tplFSGet(ctx context.Context, url string) (body []byte, status int, bodyCarried bool, ok bool) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, 0, false, false
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, false, false
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		carried := resp.StatusCode == 400 || resp.StatusCode == 409 ||
			resp.StatusCode == 413 || resp.StatusCode == 422
		return b, resp.StatusCode, carried, false
	}
	return b, resp.StatusCode, false, true
}

// ---- tiny JSON writer (Node JSON.stringify parity, ordered) ---------------

// nodeJSONString — one JSON string value exactly as JSON.stringify writes
// it: escape " \ and control chars (\b \t \n \f \r + \u00XX); HTML
// characters are NOT escaped (no \u0026 / \u003c etc.).
func nodeJSONString(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\b':
			sb.WriteString(`\b`)
		case '\t':
			sb.WriteString(`\t`)
		case '\n':
			sb.WriteString(`\n`)
		case '\f':
			sb.WriteString(`\f`)
		case '\r':
			sb.WriteString(`\r`)
		default:
			if r < 0x20 {
				sb.WriteString(fmt.Sprintf(`\u%04x`, r))
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

// ---- mongo read helpers ----------------------------------------------------

func tplFindTemplate(ctx context.Context, a *core.App, filter bson.D) (*bson.D, bool) {
	if a == nil || a.Mongo == nil {
		return nil, false
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, false
	}
	var d bson.D
	err = db.Collection("templates").FindOne(ctx, filter).Decode(&d)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, true
		}
		return nil, false
	}
	return &d, true
}

func tplFindTemplates(ctx context.Context, a *core.App, filter bson.D) ([]bson.D, bool) {
	if a == nil || a.Mongo == nil {
		return nil, false
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, false
	}
	proj := bson.D{
		{Key: "_id", Value: 1}, {Key: "version", Value: 1},
		{Key: "name", Value: 1}, {Key: "author", Value: 1},
		{Key: "description", Value: 1}, {Key: "category", Value: 1},
		{Key: "lastUpdated", Value: 1},
	}
	flt := any(filter)
	if len(flt.(bson.D)) == 0 {
		flt = bson.M{}
	}
	cur, err := db.Collection("templates").Find(ctx, flt, options.Find().SetProjection(proj))
	if err != nil {
		return nil, false
	}
	defer cur.Close(ctx)
	var out []bson.D
	if err := cur.All(ctx, &out); err != nil {
		return nil, false
	}
	return out, true
}

// tplDocVal returns the value of a key in a bson.D doc.
func tplDocVal(d *bson.D, key string) (any, bool) {
	doc := *d
	for i := range doc {
		if doc[i].Key == key {
			return doc[i].Value, true
		}
	}
	return nil, false
}

// tplOwnerHex — owner is stored as ObjectId; the page formatter outputs
// the hex string (pinned: "6aa4b8a8…").
func tplOwnerHex(v any) string {
	switch t := v.(type) {
	case primitive.ObjectID:
		return t.Hex()
	case string:
		return t
	default:
		return ""
	}
}

// tplISODate — Date → the exact Node JSON form (milliseconds → toISOString).
func tplISODate(v any) (string, bool) {
	switch t := v.(type) {
	case primitive.DateTime:
		return t.Time().UTC().Format("2006-01-02T15:04:05.000Z"), true
	case time.Time:
		return t.UTC().Format("2006-01-02T15:04:05.000Z"), true
	case string:
		return t, true
	default:
		return "", false
	}
}

func tplStringVal(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case int32:
		return strconv.Itoa(int(t))
	case int64:
		return strconv.FormatInt(t, 10)
	case int:
		return strconv.Itoa(t)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	case bool:
		if t {
			return "true"
		}
		return "false"
	case nil:
		return "null"
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

// ---- gallery enablement (Node ensureGalleryEnabled) ------------------------

func tplSecField(d *bson.D, key string) (*bson.D, bool) {
	v, ok := tplDocVal(d, key)
	if !ok {
		return nil, false
	}
	if sub, ok := v.(*bson.D); ok {
		return sub, true
	}
	return nil, false
}

// tplGalleryDisabled mirrors: section = getSection('templates') — the
// stored site_settings.templates.enabled beats the env seed
// (OVERLEAF_TEMPLATE_GALLERY 'true'|'false'); disabled ⇔ enabled === false.
// Node boolFromEnv: 'true'→true, 'false'→false, anything else→undefined
// → the `?? coreSettings?.templates?.enabled === true` seed (false here).
func tplGalleryDisabled(ctx context.Context, a *core.App) bool {
	if a != nil && a.Mongo != nil {
		if db, err := a.Mongo.DB(ctx); err == nil {
			var d bson.D
			err = db.Collection("site_settings").FindOne(ctx,
				bson.D{{Key: "_id", Value: "global"}}).Decode(&d)
			if err == nil {
				if sec, ok := tplSecField(&d, "templates"); ok {
					if en, ok2 := tplDocVal(sec, "enabled"); ok2 {
						if b, ok3 := en.(bool); ok3 {
							return !b
						}
					}
					return false
				}
			}
		}
	}
	switch os.Getenv("OVERLEAF_TEMPLATE_GALLERY") {
	case "true":
		return false
	case "false":
		return true
	default:
		return true
	}
}

// ---- page data + error helpers (per-feature duplication) -------------------

// SessionMenuGrant: ExposedSettings.canManageTemplatesMenu (Node ExpressLocals
// re-computes it per request via TemplateAuthorizationHelper). Session-level
// best effort here (session user.isAdmin or OVERLEAF_TEMPLATES_USER_ID match);
// the DB-only paths (user flags / allUsersCanManageTemplates) are evaluated by
// the full ladder in mgmt.go's tplPrivileged where a mongo handle is in hand.
func SessionMenuGrant(sess *core.Session) bool {
	if sess == nil {
		return false
	}
	// Node ExpressLocals: SessionManager.getSessionUser = session.user ||
	// session.passport.user, then AdminAuthorizationHelper.hasAdminAccess =
	// Boolean(user.isAdmin) (+ legacy Settings.templates.user_id match).
	// OlliTeX CE sessions carry the user under `user` (passport is the
	// fallback shape only). Both are honored, in that order.
	grant := func(raw json.RawMessage) bool {
		var u struct {
			IsAdmin bool   `json:"isAdmin"`
			ID      string `json:"_id"`
		}
		if json.Unmarshal(raw, &u) != nil {
			return false
		}
		if u.IsAdmin {
			return true
		}
		env := os.Getenv("OVERLEAF_TEMPLATES_USER_ID")
		if env != "" && u.ID == env {
			return true
		}
		return false
	}
	if raw, ok := sess.GetRaw("user"); ok && grant(raw) {
		return true
	}
	if raw, ok := sess.GetRaw("passport"); ok {
		var pp struct {
			User json.RawMessage `json:"user"`
		}
		if json.Unmarshal(raw, &pp) == nil && len(pp.User) > 0 && grant(pp.User) {
			return true
		}
	}
	return false
}

// MenuGrant evaluates the full Node ladder (hasTemplateAdminAccess) for the
// request: session isAdmin → OVERLEAF_TEMPLATES_USER_ID → DB user isAdmin →
// DB flags.canManageTemplates → site section allUsersCanManageTemplates.
// Node re-computes this per-request for every page render (ExpressLocals),
// so all Go page data must use it, not the session-only SessionMenuGrant.
func MenuGrant(ctx context.Context, cxt *core.Cxt) bool {
	if cxt == nil {
		return false
	}
	return tplPrivileged(ctx, cxt.A, cxt)
}

// SessionIsAdmin: session passport user.isAdmin (Node shows the Admin
// navbar dropdown to site admins on every page render).
func SessionIsAdmin(sess *core.Session) bool {
	if sess == nil {
		return false
	}
	if raw, ok := sess.GetRaw("passport"); ok {
		var pp struct {
			User struct {
				IsAdmin bool `json:"isAdmin"`
			} `json:"user"`
		}
		if json.Unmarshal(raw, &pp) == nil {
			return pp.User.IsAdmin
		}
	}
	return false
}

func tplPageData(cxt *core.Cxt) views.PageData {
	email, uid := core.PageUserSlots(cxt.Sess)
	d := views.PageData{
		Path:      strings.TrimPrefix(cxt.Req.URL.Path, "/"), // pageBase convention: trimmed
		Origin:    cxt.SiteURL,
		UserEmail: email,
		UserID:    uid,
	}
	if cxt.Sess != nil {
		d.CanManageTemplateMenu = MenuGrant(cxt.Req.Context(), cxt)
		if SessionIsAdmin(cxt.Sess) {
			d.NavAdmin = views.AdminNavFragment
		}
	}
	if tok := cxt.Sess.CsrfToken(); tok != "" {
		d.CSRFToken = tok
	}
	d.Nonce = views.NewNonce()
	return d
}

func tplErr500(cxt *core.Cxt, res *core.Res) {
	// Error500Page sets the 500 status + headers itself (an early WriteHeader
	// here would finalize headers and drop ETag/CSP/PP).
	views.Error500Page(res.W, tplPageData(cxt))
}

func tplRestricted(cxt *core.Cxt, res *core.Res) {
	if core.AcceptsJSON(cxt.Req) {
		res.JSON(403, []byte(`{"message":"restricted"}`))
		return
	}
	views.Restricted403(res.W, tplPageData(cxt))
}

// ---- simple routes ---------------------------------------------------------

func hRedirect(target string) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		res.Redirect(cxt.Req, 301, target)
	}
}

// ---- /api/template ---------------------------------------------------------

// singleQuery — Node req.query: a single value; duplicates → array → the
// handlers treat it as the invalid/undefined paths (pinned).
func singleQuery(q map[string][]string, key string) (string, bool) {
	if vs, ok := q[key]; ok && len(vs) == 1 {
		return vs[0], true
	}
	return "", false
}

// hGetTemplate — GET /api/template?key=_id|name&val=… (Node getTemplate):
//
//	key=_id   : isValid(val) ? findById : null
//	key=name  : findOne({name})
//	key=other / missing : null
//
// found → _formatTemplateForPage (pinned key order; cleanHtml author
// linksOnly / description reachText; absent document fields DROPPED).
func hGetTemplate(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		key, _ := singleQuery(cxt.Req.URL.Query(), "key")
		val, hasVal := singleQuery(cxt.Req.URL.Query(), "val")
		var d *bson.D
		switch key {
		case "_id":
			if hasVal && tplOidValid(val) {
				oid, _ := primitive.ObjectIDFromHex(strings.ToLower(val))
				tmp, ok := tplFindTemplate(ctx, a, bson.D{{Key: "_id", Value: oid}})
				if !ok {
					tplErr500(cxt, res)
					return
				}
				d = tmp
			}
		case "name":
			if hasVal {
				tmp, ok := tplFindTemplate(ctx, a, bson.D{{Key: "name", Value: val}})
				if !ok {
					tplErr500(cxt, res)
					return
				}
				d = tmp
			}
		}
		body := "null"
		if d != nil {
			body = tplFormatForPage(d)
		}
		res.JSON(200, []byte(body))
	}
}

// tplOidValid — mongoose ObjectId.isValid: 24-char hex OR any 12-char
// string. The 12-char case can never match a stored 24-hex _id, so both
// paths observably end at null (pinned: bad _id → null).
func tplOidValid(s string) bool {
	return tplHex24.MatchString(s) || len(s) == 12
}

// tplFormatForPage — _formatTemplateForPage parity (pinned live body):
// id, version(String), category, name, author(linksOnly), authorMD,
// description(reachText), descriptionMD, license, lastUpdated, owner,
// mainFile, compiler, [imageName], language.
func tplFormatForPage(d *bson.D) string {
	var parts []string
	add := func(k string, v string) { parts = append(parts, nodeJSONString(k)+":"+v) }
	if v, ok := tplDocVal(d, "_id"); ok {
		if oid, ok := v.(primitive.ObjectID); ok {
			add("id", nodeJSONString(oid.Hex()))
		}
	}
	if v, ok := tplDocVal(d, "version"); ok {
		add("version", nodeJSONString(tplStringVal(v)))
	}
	if v, ok := tplDocVal(d, "category"); ok {
		add("category", nodeJSONString(tplStringVal(v)))
	}
	if v, ok := tplDocVal(d, "name"); ok {
		add("name", nodeJSONString(tplStringVal(v)))
	}
	if v, ok := tplDocVal(d, "author"); ok {
		add("author", nodeJSONString(cleanHtml(tplStringVal(v), "linksOnly")))
	}
	if v, ok := tplDocVal(d, "authorMD"); ok {
		add("authorMD", nodeJSONString(tplStringVal(v)))
	}
	if v, ok := tplDocVal(d, "description"); ok {
		add("description", nodeJSONString(cleanHtml(tplStringVal(v), "reachText")))
	}
	if v, ok := tplDocVal(d, "descriptionMD"); ok {
		add("descriptionMD", nodeJSONString(tplStringVal(v)))
	}
	if v, ok := tplDocVal(d, "license"); ok {
		add("license", nodeJSONString(tplStringVal(v)))
	}
	if v, ok := tplDocVal(d, "lastUpdated"); ok {
		if iso, ok2 := tplISODate(v); ok2 {
			add("lastUpdated", nodeJSONString(iso))
		}
	}
	if v, ok := tplDocVal(d, "owner"); ok {
		add("owner", nodeJSONString(tplOwnerHex(v)))
	}
	if v, ok := tplDocVal(d, "mainFile"); ok {
		add("mainFile", nodeJSONString(tplStringVal(v)))
	}
	if v, ok := tplDocVal(d, "compiler"); ok {
		add("compiler", nodeJSONString(tplStringVal(v)))
	}
	if v, ok := tplDocVal(d, "imageName"); ok {
		add("imageName", nodeJSONString(tplStringVal(v)))
	}
	if v, ok := tplDocVal(d, "language"); ok {
		add("language", nodeJSONString(tplStringVal(v)))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// ---- /api/template/categories ----------------------------------------------

var tplDefaultCategories = []struct {
	key, name string
}{
	{"academic-journal", "Academic journals"},
	{"book", "Books"},
	{"presentation", "Presentations"},
	{"poster", "Posters"},
	{"cv", "CVs"},
	{"homework", "Homework"},
	{"bibliography", "Bibliographies"},
	{"calendar", "Calendars"},
	{"formal-letter", "Formal letters"},
	{"report", "Reports"},
	{"thesis", "Theses"},
	{"newsletter", "Newsletters"},
	{"all", "All templates"},
}

var _ = tplDefaultCategories

// hCategories — getEnabledCategories: section.categories (stored beats
// seed; the 13 seed entries in Node DEFAULT_TEMPLATE_CATEGORIES order),
// filter c.enabled !== false, map {key, name || key}.
func hCategories(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		if a != nil && a.Mongo != nil {
			if db, err := a.Mongo.DB(ctx); err == nil {
				var d bson.D
				if db.Collection("site_settings").FindOne(ctx,
					bson.D{{Key: "_id", Value: "global"}}).Decode(&d) == nil {
					if sec, ok := tplSecField(&d, "templates"); ok {
						if list, ok2 := tplDocVal(sec, "categories"); ok2 {
							if arr, ok3 := list.([]interface{}); ok3 {
								var parts []string
								for _, it := range arr {
									m, ok4 := it.(*bson.D)
									if !ok4 {
										continue
									}
									if en, ok5 := tplDocVal(m, "enabled"); ok5 {
										if b, ok6 := en.(bool); ok6 && !b {
											continue
										}
									}
									key, _ := tplDocVal(m, "key")
									ks, ok7 := key.(string)
									if !ok7 {
										continue
									}
									name := ks
									if n, ok8 := tplDocVal(m, "name"); ok8 {
										if ns, ok9 := n.(string); ok9 && ns != "" {
											name = ns
										}
									}
									parts = append(parts,
										`{"key":`+nodeJSONString(ks)+`,"name":`+nodeJSONString(name)+`}`)
								}
								res.JSON(200, []byte("["+strings.Join(parts, ",")+"]"))
								return
							}
						}
					}
				}
			}
		}
		parts := make([]string, 0, len(tplDefaultCategories))
		for _, c := range tplDefaultCategories {
			parts = append(parts,
				`{"key":`+nodeJSONString(c.key)+`,"name":`+nodeJSONString(c.name)+`}`)
		}
		res.JSON(200, []byte("["+strings.Join(parts, ",")+"]"))
	}
}

// ---- /api/templates ---------------------------------------------------------

// tplSortKey — the sort field for orderBy (lodash stable).
func tplSortKey(d *bson.D, by string) (time.Time, string) {
	if by == "name" {
		if v, ok := tplDocVal(d, "name"); ok {
			return time.Time{}, tplStringVal(v)
		}
		return time.Time{}, ""
	}
	if v, ok := tplDocVal(d, "lastUpdated"); ok {
		switch t := v.(type) {
		case primitive.DateTime:
			return t.Time(), ""
		case time.Time:
			return t, ""
		}
	}
	return time.Time{}, ""
}

// hList — Node getCategoryTemplates + _sortTemplates (pinned):
//
//	category=all (default) → all; else {category: '/templates/'+category}
//	by ∉ {lastUpdated,name} (non-empty invalid) → next(error) → 500 page
//	by='' → lodash no-op → natural order; asc/desc stable
//	duplicate params → ARRAY → TypeError → 500 page
//	list item: id, version(String), name, author(plainText),
//	           description(plainText), category, lastUpdated
func hList(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		q := cxt.Req.URL.Query()
		if len(q["by"]) > 1 || len(q["order"]) > 1 || len(q["category"]) > 1 {
			tplErr500(cxt, res)
			return
		}
		category, by, order := "all", "lastUpdated", "desc"
		if vs := q["category"]; len(vs) == 1 {
			category = vs[0]
		}
		if vs := q["by"]; len(vs) == 1 {
			by = vs[0]
		}
		if vs := q["order"]; len(vs) == 1 {
			order = vs[0]
		}
		if (by != "" && by != "lastUpdated" && by != "name") ||
			(order != "" && order != "asc" && order != "desc") {
			tplErr500(cxt, res)
			return
		}
		// Node: `[sort.by || 'lastUpdated']` / `[sort.order || 'desc']` —
		// EMPTY values fall back to the defaults (lodash still sorts).
		if by == "" {
			by = "lastUpdated"
		}
		if order == "" {
			order = "desc"
		}
		var filter bson.D
		if category != "all" {
			filter = bson.D{{Key: "category", Value: "/templates/" + category}}
		}
		docs, ok := tplFindTemplates(ctx, a, filter)
		if !ok {
			tplErr500(cxt, res)
			return
		}
		if by == "lastUpdated" || by == "name" {
			asc := order == "asc"
			sort.SliceStable(docs, func(i, j int) bool {
				ti, si := tplSortKey(&docs[i], by)
				tj, sj := tplSortKey(&docs[j], by)
				if by == "name" {
					if si != sj {
						return (asc && si < sj) || (!asc && si > sj)
					}
					return false
				}
				if !ti.Equal(tj) {
					return (asc && ti.Before(tj)) || (!asc && ti.After(tj))
				}
				return false
			})
		}
		parts := make([]string, 0, len(docs))
		for i := range docs {
			parts = append(parts, tplFormatForList(&docs[i]))
		}
		res.JSON(200, []byte(`{"totalSize":`+strconv.Itoa(len(docs))+`,"templates":[`+strings.Join(parts, ",")+`]}`))
	}
}

// tplFormatForList — _formatTemplateForList parity (pinned) with plainText
// author/description; absent fields dropped.
func tplFormatForList(d *bson.D) string {
	var parts []string
	add := func(k string, v string) { parts = append(parts, nodeJSONString(k)+":"+v) }
	if v, ok := tplDocVal(d, "_id"); ok {
		if oid, ok := v.(primitive.ObjectID); ok {
			add("id", nodeJSONString(oid.Hex()))
		}
	}
	if v, ok := tplDocVal(d, "version"); ok {
		add("version", nodeJSONString(tplStringVal(v)))
	}
	if v, ok := tplDocVal(d, "name"); ok {
		add("name", nodeJSONString(tplStringVal(v)))
	}
	if v, ok := tplDocVal(d, "author"); ok {
		add("author", nodeJSONString(cleanHtml(tplStringVal(v), "plainText")))
	}
	if v, ok := tplDocVal(d, "description"); ok {
		add("description", nodeJSONString(cleanHtml(tplStringVal(v), "plainText")))
	}
	if v, ok := tplDocVal(d, "category"); ok {
		add("category", nodeJSONString(tplStringVal(v)))
	}
	if v, ok := tplDocVal(d, "lastUpdated"); ok {
		if iso, ok2 := tplISODate(v); ok2 {
			add("lastUpdated", nodeJSONString(iso))
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// ---- /template/:id/preview --------------------------------------------------

// hPreview — Node fetchTemplatePreview + controller error mapping (pinned):
//   - no templateId/version        → 404 page
//   - style ∉ {preview,thumbnail}  → 404 page
//   - filestore non-2xx 404        → 404 page (ErrorController.notFound)
//   - filestore non-2xx other      → res.status(n).json({url,method,status[,body]})
//   - ok + image style             → 200 application/octet-stream (bytes)
//   - ok + no image style          → 200 application/pdf   (bytes)
//     (placeholder-SVG branch = dead code: fetchStreamWithResponse throws on
//     every non-2xx before the controller would see response.ok=false)
func hPreview(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		tid := ""
		if cxt.Params != nil {
			tid = cxt.Params["1"]
		}
		q := cxt.Req.URL.Query()
		version := ""
		if vs := q["version"]; len(vs) == 1 {
			version = vs[0]
		}
		style := ""
		if vs := q["style"]; len(vs) == 1 {
			style = vs[0]
		}
		page404 := func() { views.NotFoundPage(res.W, tplPageData(cxt)) }
		if tid == "" || version == "" {
			page404()
			return
		}
		styleParam := ""
		if style != "" {
			if style != "preview" && style != "thumbnail" {
				page404()
				return
			}
			styleParam = "style=" + style
		}
		url := tplFilestoreBase() + "/template/" + tid + "/v/" + version + "/pdf?" + styleParam
		body, status, carried, ok := tplFSGet(ctx, url)
		if !ok {
			info := `{"url":` + nodeJSONString(url) + `,"method":"GET","status":` + strconv.Itoa(status)
			if carried {
				info += `,"body":` + nodeJSONString(string(body))
			}
			if status == 404 {
				page404()
				return
			}
			code := status
			if code == 0 {
				code = 400
			}
			res.JSON(code, []byte(info+"}"))
			return
		}
		if style == "preview" || style == "thumbnail" {
			tplStream(res, "application/octet-stream", body)
			return
		}
		tplStream(res, "application/pdf", body)
	}
}

// tplStream — Node: res.setHeader('Content-Type', ct); stream.pipe(res)
// (bundle adds Content-Disposition + Content-Length) — helmet baseline
// already set by the core pre-handler; NO ETag (not res.send); chunked
// when Content-Length is absent (Node streams).
func tplStream(res *core.Res, ct string, body []byte) {
	h := res.W.Header()
	h.Del("ETag")
	h.Set("Content-Type", ct)
	res.W.WriteHeader(200)
	_, _ = res.W.Write(body)
}

// ---- /template/:id/bundle ----------------------------------------------------

// tplNameSanitize — Node: name.replace(/[/:*?"<>|\s]+/g, '_'); \s =
// space \t \n \v \f \r; consecutive matches collapse to a single '_'.
func tplNameSanitize(name string) string {
	var sb strings.Builder
	underscore := false
	for _, r := range name {
		bad := r == '/' || r == ':' || r == '*' || r == '?' || r == '"' ||
			r == '<' || r == '>' || r == '|' ||
			r == ' ' || r == '\t' || r == '\n' || r == '\v' || r == '\f' || r == '\r'
		if bad {
			underscore = true
			continue
		}
		if underscore {
			sb.WriteByte('_')
			underscore = false
		}
		sb.WriteRune(r)
	}
	if underscore {
		sb.WriteByte('_')
	}
	return sb.String()
}

type tplZipEntry struct {
	name  string
	bytes []byte
}

// tplZipBytes — the bundle zip: template.json, source.zip, [output.pdf]
// (Node archiver order; deflate on both stacks). The gate compares entry
// names + sizes + contents, not raw bytes (entry mtimes are wall-clock).
func tplZipBytes(meta string, zipBody, pdfBody []byte, pdfOK bool) []byte {
	entries := []tplZipEntry{
		{"template.json", []byte(meta)},
		{"source.zip", zipBody},
	}
	if pdfOK {
		entries = append(entries, tplZipEntry{"output.pdf", pdfBody})
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	now := time.Now()
	for _, e := range entries {
		fh, err := w.CreateHeader(&zip.FileHeader{Name: e.name, Modified: now})
		if err != nil {
			return nil
		}
		_, _ = fh.Write(e.bytes)
	}
	_ = w.Close()
	return buf.Bytes()
}

// hBundle — Node getTemplateBundle (pinned):
//   - bad-hex id → 500 {"message":"Cast to ObjectId failed for value …"}
//   - ghost id   → 500 {"message":"Template not found"}
//   - filestore zip missing → 500 {"message":"request failed"}
//   - ok → 200 application/zip, Content-Disposition
//     attachment; filename="<sanitized>_v<version>.bundle.zip",
//     Content-Length set; entries template.json + source.zip [+ output.pdf]
func hBundle(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		tid := ""
		if cxt.Params != nil {
			tid = cxt.Params["1"]
		}
		if !tplHex24.MatchString(tid) {
			msg := `Cast to ObjectId failed for value "` + tid + `" (type string) at path "_id" for model "Template"`
			res.JSON(500, []byte(`{"message":`+nodeJSONString(msg)+`}`))
			return
		}
		oid, _ := primitive.ObjectIDFromHex(tid)
		d, ok := tplFindTemplate(ctx, a, bson.D{{Key: "_id", Value: oid}})
		if !ok {
			tplErr500(cxt, res)
			return
		}
		if d == nil {
			res.JSON(500, []byte(`{"message":"Template not found"}`))
			return
		}
		var version any
		if v, ok := tplDocVal(d, "version"); ok {
			version = v
		}
		versionStr := tplStringVal(version)
		base := tplFilestoreBase() + "/template/" + oid.Hex() + "/v/" + versionStr
		zipBody, _, _, zok := tplFSGet(ctx, base+"/zip")
		if !zok {
			// RequestFailedError.message = 'request failed'; error.status is
			// undefined (status lives in .info) → res.status(500).json(...).
			res.JSON(500, []byte(`{"message":"request failed"}`))
			return
		}
		pdfBody, _, _, pok := tplFSGet(ctx, base+"/pdf")
		meta := tplBundleMeta(d)
		nameVal, _ := tplDocVal(d, "name")
		name := tplStringVal(nameVal)
		if name == "" {
			name = "template"
		}
		filename := tplNameSanitize(name) + "_v" + versionStr + ".bundle.zip"
		zipBytes := tplZipBytes(meta, zipBody, pdfBody, pok)
		h := res.W.Header()
		h.Del("ETag")
		h.Set("Content-Type", "application/zip")
		h.Set("Content-Disposition", `attachment; filename="`+filename+`"`)
		h.Set("Content-Length", strconv.Itoa(len(zipBytes)))
		res.W.WriteHeader(200)
		_, _ = res.W.Write(zipBytes)
	}
}

// tplBundleMeta — JSON.stringify(template.toJSON minus _id/__v/owner, null, 2):
// document field order, 2-space indent, Node string escaping.
func tplBundleMeta(d *bson.D) string {
	keys := make([]string, 0, len(*d))
	vals := map[string]any{}
	for i := range *d {
		k := (*d)[i].Key
		if k == "_id" || k == "__v" || k == "owner" {
			continue
		}
		keys = append(keys, k)
		vals[k] = (*d)[i].Value
	}
	lines := make([]string, 0, len(keys))
	for i, k := range keys {
		suf := ","
		if i == len(keys)-1 {
			suf = ""
		}
		lines = append(lines, "  "+nodeJSONString(k)+": "+tplMetaVal(vals[k])+suf)
	}
	return "{\n" + strings.Join(lines, "\n") + "\n}"
}

// tplMetaVal — one JSON value for the bundle meta (single-line form).
func tplMetaVal(v any) string {
	switch t := v.(type) {
	case string:
		return nodeJSONString(t)
	case int32:
		return strconv.Itoa(int(t))
	case int64:
		return strconv.FormatInt(t, 10)
	case int:
		return strconv.Itoa(t)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	case bool:
		if t {
			return "true"
		}
		return "false"
	case nil:
		return "null"
	case primitive.DateTime:
		return nodeJSONString(t.Time().UTC().Format("2006-01-02T15:04:05.000Z"))
	case time.Time:
		return nodeJSONString(t.UTC().Format("2006-01-02T15:04:05.000Z"))
	case primitive.ObjectID:
		return nodeJSONString(t.Hex())
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return "null"
		}
		return string(b)
	}
}
