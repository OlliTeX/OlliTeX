package sitesettings

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	mongooptions "go.mongodb.org/mongo-driver/mongo/options"

	"ollitex/go/services/web/core"
)

var (
	cipherOnce    sync.Once
	cipherInst    *cipher
	cipherLoadErr error
)

func getCipher() (*cipher, error) {
	cipherOnce.Do(func() {
		cipherInst, cipherLoadErr = loadCipher()
	})
	return cipherInst, cipherLoadErr
}

// Feature wires the five Manage-Site SiteSettings routes (P3.6).
func Feature(a *core.App) core.Feature {
	return core.Feature{
		Name: "sitesettings",
		Routes: []core.Route{
			{Method: "GET", Path: "/admin/site", Handler: siteIndex(a)},
			{Method: "GET", Path: "/admin/site-settings", Handler: getSiteSettings(a)},
			{
				Method:  "PUT",
				Pattern: regexp.MustCompile(`^/admin/site-settings/(?P<section>[^/]+)$`),
				Handler: updateSiteSettings(a),
			},
			{Method: "POST", Path: "/admin/site-settings/email/test", Handler: emailTest(a)},
			{Method: "GET", Path: "/admin/site/template-admins", Handler: templateAdmins(a)},
		},
	}
}

func ctxWith(cxt *core.Cxt) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 10*time.Second)
	return ctx, cancel
}

func serve500(a *core.App, cxt *core.Cxt, res *core.Res) {
	if a.Render500 != nil {
		a.Render500(cxt, res)
		return
	}
	res.SendStatus(500)
}

// JSON error bodies (Node HttpErrorHandler / res.status(code).json shape).
func errBody(msg string) []byte {
	return []byte(`{"message":"` + jsEscapeQuote(msg) + `"}`)
}

func jsEscapeQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// ---------- GET /admin/site → 302 /hub#/site ----------

func siteIndex(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		res.Redirect(cxt.Req, 302, "/hub#/site")
	}
}

// ---------- GET /admin/site-settings → all sections ----------

func getSiteSettings(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		c, cerr := getCipher()
		if cerr != nil || a.Mongo == nil {
			serve500(a, cxt, res)
			return
		}
		ctx, cancel := ctxWith(cxt)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			serve500(a, cxt, res)
			return
		}
		sections := loadAllSections(ctx, db)
		body := Obj{}
		for _, name := range GET_SECTION_ORDER {
			if name == "templates" {
				sec := getSectionMasked("templates", sections, c)
				sec = ObjSet(sec, "counts", buildCounts(ctx, db, sections, c))
				body = append(body, KV{"templates", sec})
			} else if name == "storage" {
				body = append(body, KV{"storage", storageSectionOut("storage", sections, c)})
			} else {
				body = append(body, KV{name, getSectionMasked(name, sections, c)})
			}
		}
		res.JSON(200, Encode(body))
	}
}

// buildCounts — per-category template counts (Node: cat 'all' → {}, else
// {category:"/templates/<key>"}), in the category order (gate normalises
// key order — Node's is completion-order/non-deterministic).
func buildCounts(ctx context.Context, db *mongo.Database, sections map[string]Obj, c *cipher) Obj {
	tmpl := getSection("templates", sections, c)
	cats, _ := ObjGetD(tmpl, "categories").([]any)
	out := Obj{}
	for _, e := range cats {
		cat, _ := e.(Obj)
		key := asStr(ObjGetD(cat, "key"))
		var filter bson.D
		if key == "all" {
			filter = bson.D{}
		} else {
			filter = bson.D{{Key: "category", Value: "/templates/" + key}}
		}
		n, err := db.Collection("templates").CountDocuments(ctx, filter)
		if err != nil {
			out = append(out, KV{key, nil}) // Node: counts[key]=null on failure
			continue
		}
		out = append(out, KV{key, int64(n)})
	}
	return out
}

// readBodyOrdered decodes the JSON body preserving object key order.
// Returns (value, kind): kind ∈ {"object","array","empty","scalar"}.
func readBodyOrdered(r *http.Request) (any, string) {
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return map[string]any{}, "empty"
	}
	if strings.HasPrefix(trimmed, "[") {
		if v, ok := parseOrderedJSON(trimmed); ok {
			return v, "array"
		}
		return nil, "array"
	}
	if strings.HasPrefix(trimmed, "{") {
		if v, ok := parseOrderedJSON(trimmed); ok {
			return v, "object"
		}
		return nil, "scalar"
	}
	return nil, "scalar"
}

func bodyToObj(v any) Obj {
	if o, ok := v.(Obj); ok {
		return o
	}
	if m, ok := v.(map[string]any); ok {
		out := Obj{}
		for k, val := range m {
			out = append(out, KV{k, val})
		}
		return out
	}
	return Obj{}
}

// ---------- PUT /admin/site-settings/:section ----------

func updateSiteSettings(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		section := cxt.Params["section"]
		validator, known := SECTION_VALIDATORS[section]
		if !known {
			res.JSON(422, errBody("Unknown section: "+section))
			return
		}
		v, _ := readBodyOrdered(cxt.Req)
		if errs := validator(v); len(errs) > 0 {
			res.JSON(422, errBody(strings.Join(errs, "; ")))
			return
		}
		c, cerr := getCipher()
		if cerr != nil || a.Mongo == nil {
			serve500(a, cxt, res)
			return
		}
		clean, ok := cleanSectionInput(section, bodyToObj(v))
		if !ok {
			clean = Obj{}
		}

		// Storage: the managed env fragment is written FIRST; a DB failure
		// rolls it back so the two never disagree.
		var previousManaged Obj
		var hadPrevious bool
		if section == "storage" {
			if se := storageEnvSection(); se != nil {
				previousManaged, hadPrevious = se, true
			}
			if len(clean) > 0 {
				if err := writeStorageEnv(clean); err != nil {
					serve500(a, cxt, res)
					return
				}
			} else {
				removeStorageEnv()
			}
		}

		ctx, cancel := ctxWith(cxt)
		defer cancel()
		upserted, modified, err := setSection(a, ctx, section, clean, c)
		if err != nil {
			if section == "storage" {
				if hadPrevious {
					_ = writeStorageEnv(previousManaged)
				} else {
					removeStorageEnv()
				}
			}
			serve500(a, cxt, res)
			return
		}
		out := Obj{
			KV{"ok", true},
			KV{"upserted", upserted},
			KV{"modified", modified},
		}
		if section == "storage" {
			out = append(out, KV{"appliesOn", "next container restart"})
			out = append(out, KV{"envLines", buildStorageEnvLines(clean)})
		}
		res.JSON(200, Encode(out))
	}
}

// ---------- POST /admin/site-settings/email/test ----------

var (
	rateMu     sync.Mutex
	rateBuckets = map[string][]int64{}
	rateMax     = 5
	rateWindow  = int64(60 * 1000)
)

func tryConsumeLimit(userID string) bool {
	rateMu.Lock()
	defer rateMu.Unlock()
	now := time.Now().UnixNano() / 1e6
	list := make([]int64, 0, len(rateBuckets[userID]))
	for _, t := range rateBuckets[userID] {
		if now-t < rateWindow {
			list = append(list, t)
		}
	}
	if len(list) >= rateMax {
		rateBuckets[userID] = list
		return false
	}
	list = append(list, now)
	rateBuckets[userID] = list
	return true
}

var emailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func emailTest(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		v, _ := readBodyOrdered(cxt.Req)
		var to string
		if o, ok := v.(Obj); ok {
			to = asStr(ObjGetD(o, "to"))
		} else if m, ok := v.(map[string]any); ok {
			to = asStr(m["to"])
		}
		to = strings.ToLower(strings.TrimSpace(to))
		if !emailRe.MatchString(to) {
			res.JSON(422, errBody("Enter a valid e-mail address"))
			return
		}
		userID := "unknown"
		if cxt.Sess != nil && cxt.Sess.IsLoggedIn() {
			if u := cxt.Sess.UserIDHex(); u != "" {
				userID = u
			}
		}
		if !tryConsumeLimit(userID) {
			res.JSON(429, errBody("Too many test e-mails — please wait a minute"))
			return
		}
		c, cerr := getCipher()
		if cerr != nil || a.Mongo == nil {
			serve500(a, cxt, res)
			return
		}
		ctx, cancel := ctxWith(cxt)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			serve500(a, cxt, res)
			return
		}
		sections := loadAllSections(ctx, db)
		email := getSection("email", sections, c)
		driver := asStr(ObjGetD(email, "driver"))
		if driver == "" {
			driver = "smtp"
		}
		if driver != "smtp" && driver != "ses" {
			res.JSON(500, errBody("Unknown e-mail driver: "+driver))
			return
		}
		host := asStr(ObjGetD(email, "host"))
		if driver == "smtp" && host == "" {
			res.JSON(500, errBody("SMTP host is not configured yet"))
			return
		}
		port := int(fnum(ObjGetD(email, "port")))
		if port <= 0 {
			port = 465
		}
		from := asStr(ObjGetD(email, "fromAddress"))
		if from == "" {
			from = "noreply@overleaf.com"
		}
		via := "driver=" + driver
		if driver == "smtp" {
			hp := host
			if hp == "" {
				hp = "?"
			}
			via += " host=" + hp + " port=" + strconv.Itoa(port)
		}
		now := time.Now().UTC().Format(time.RFC3339)
		subject := "[Overleaf] E-mail configuration test"
		text := "This is a test e-mail sent from the Overleaf admin console " +
			"(Manage Site → E-mail).\n\nSent at: " + now + "\nVia: " + via + "."
		html := "<p>This is a test e-mail sent from the Overleaf admin console " +
			"(Manage Site → E-mail).</p>" +
			"<p>Sent at: " + now + " via <code>" + via + "</code></p>"
		m := &core.Mail{Host: host, Port: port, Secure: asBool(ObjGetD(email, "secure")), From: from, Timeout: 20}
		if serr := m.Send(to, subject, text, html); serr != nil {
			detail := asStrOrErr(serr.Error())
			res.JSON(502, errBody("Test e-mail failed to send: "+truncate(detail, 200)))
			return
		}
		res.JSON(200, []byte(`{"ok":true}`))
	}
}

func asStrOrErr(s string) string {
	if s == "" {
		return "unknown SMTP error"
	}
	return s
}

// ---------- GET /admin/site/template-admins ----------

func templateAdmins(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		if a.Mongo == nil {
			serve500(a, cxt, res)
			return
		}
		ctx, cancel := ctxWith(cxt)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			serve500(a, cxt, res)
			return
		}
		filter := bson.D{{Key: "$or", Value: bson.A{
			bson.D{{Key: "flags.canManageTemplates", Value: true}},
			bson.D{{Key: "isAdmin", Value: true}},
		}}}
		opts := mongooptions.Find().SetProjection(bson.D{
			{Key: "_id", Value: 1}, {Key: "email", Value: 1},
			{Key: "first_name", Value: 1}, {Key: "last_name", Value: 1},
			{Key: "isAdmin", Value: 1}, {Key: "flags", Value: 1},
		})
		cursor, err := db.Collection("users").Find(ctx, filter, opts)
		if err != nil {
			serve500(a, cxt, res)
			return
		}
		var docs []bson.M
		if err := cursor.All(ctx, &docs); err != nil {
			serve500(a, cxt, res)
			return
		}
		_ = cursor.Close(ctx)
		seen := map[string]bool{}
		rows := []any{}
		for _, d := range docs {
			id := asStr(d["_id"])
			if oid, ok := d["_id"].(interface{ Hex() string }); ok {
				id = oid.Hex()
			}
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			flags := d["flags"]
			hasFlag := false
			if m, ok := flags.(bson.M); ok {
				hasFlag = asBool(m["canManageTemplates"])
			} else if o, ok := flags.(Obj); ok {
				hasFlag = asBool(ObjGetD(o, "canManageTemplates"))
			}
			notFound := ""
			rows = append(rows, Obj{
				KV{"id", id},
				KV{"email", asStr(d["email"])},
				KV{"firstName", strOr(d["first_name"], notFound)},
				KV{"lastName", strOr(d["last_name"], "")},
				KV{"isAdmin", asBool(d["isAdmin"])},
				KV{"hasTemplateFlag", hasFlag},
			})
		}
		body := Obj{KV{"users", rows}}
		res.JSON(200, Encode(body))
	}
}

func strOr(v any, def string) string {
	s, ok := v.(string)
	if !ok || s == "" {
		return def
	}
	return s
}
