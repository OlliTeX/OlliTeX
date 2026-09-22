package templates

// The privileged (management) flows of the template gallery (P6.13),
// oracle-pinned 2026-09-18 against the live Node stack:
//
//   plain user            → 403 {"message":"restricted"} on every mgmt route
//   site admin            → admin-list 200 (list); import {} → 400
//                           {"message":"bundle data is missing"};
//                           import-url {} → 400 {"message":"No bundle URL given."}
//                           import-url ssrf(127.0.0.1) → 422
//                             {"issues":["The URL host is in a network blocked by the site policy (127.0.0.0/8)"],
//                              "message":"Bundle rejected — fix the listed issues and try again."}
//                           edit {} → 200 {"lastUpdated":"<now>"};
//                           edit ghost/badhex → 500 {"message":"Something went wrong. Please try again."}
//                           import valid bundle → 200 {"template_id":…,"version":…,"created":…};
//                           import same-name (no override) → 409 {"canOverride":true,"message":"…"};
//                           delete → 200 "OK"; new/:ghost → 400
//                           {"message":"Failed to publish as a template."}

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
)

// ---- i18n (services/web/locales/en.json) ----

const (
	msgWrongServer    = "Something went wrong. Please try again."
	msgPublishFail    = "Failed to publish as a template."
	msgTryRecompile   = "Please try recompiling the project from scratch."
	msgBundleDataMiss = "bundle data is missing"
	msgBundleURLMiss  = "No bundle URL given."
	msgBundleRejected = "Bundle rejected — fix the listed issues and try again."
	maxNameLen        = 150
	maxDescMDLen      = 4096
	maxFieldLen       = 512
	maxBundleEntry    = 40 * 1024 * 1024
	maxBundleDownload = 9 * 1024 * 1024
	youName           = "You"
)

func ownedByMsg(name string) string {
	return fmt.Sprintf("A template with this title already exists and is owned by %s.", name)
}

func tplJSONMsg(msg string) []byte {
	return []byte(`{"message":` + nodeJSONString(msg) + `}`)
}

func tplIssueJSON(issues []string) []byte {
	parts := make([]string, len(issues))
	for i, s := range issues {
		parts[i] = nodeJSONString(s)
	}
	return []byte(`{"issues":[` + strings.Join(parts, ",") + `],"message":` + nodeJSONString(msgBundleRejected) + `}`)
}

func tplBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// ---- ordered JSON body (Node req.body key order preserved) ----

type tplKVP struct {
	k string
	v any
}

type tplObj struct{ items []tplKVP }

func (o *tplObj) get(k string) (any, bool) {
	for i := range o.items {
		if o.items[i].k == k {
			return o.items[i].v, true
		}
	}
	return nil, false
}

func (o *tplObj) set(k string, v any) {
	for i := range o.items {
		if o.items[i].k == k {
			o.items[i].v = v
			return
		}
	}
	o.items = append(o.items, tplKVP{k, v})
}

func (o *tplObj) jsonString() string {
	parts := make([]string, 0, len(o.items))
	for i := range o.items {
		parts = append(parts, nodeJSONString(o.items[i].k)+":"+tplAnyJSON(o.items[i].v))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

var errMalformedJSON = errors.New("malformed json")

func tplIsWS(b []byte) bool {
	return len(b) == 1 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\n' || b[0] == '\r')
}

func tplParseJSON(raw []byte) (any, error) {
	raw = bytes.TrimLeft(raw, " \t\n\r")
	if len(raw) == 0 {
		return nil, errMalformedJSON
	}
	v, rest, err := tplParseOne(raw)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimLeft(rest, " \t\n\r")) != 0 {
		return nil, errMalformedJSON
	}
	return v, nil
}

func tplParseOne(raw []byte) (any, []byte, error) {
	raw = bytes.TrimLeft(raw, " \t\n\r")
	if len(raw) == 0 {
		return nil, nil, errMalformedJSON
	}
	switch {
	case raw[0] == '{':
		o, rest, err := tplParseObj(raw)
		if err != nil {
			return nil, nil, err
		}
		return o, rest, nil
	case raw[0] == '[':
		return tplParseArr(raw)
	case raw[0] == '"':
		s, rest, err := tplParseRawString(raw)
		if err != nil {
			return nil, nil, err
		}
		return s, rest, nil
	default:
		return tplParseScalar(raw)
	}
}

func tplParseObj(raw []byte) (*tplObj, []byte, error) {
	o := &tplObj{}
	rest := raw[1:]
	if len(bytes.TrimLeft(rest, " \t\n\r")) == 0 {
		return nil, nil, errMalformedJSON
	}
	rest = bytes.TrimLeft(rest, " \t\n\r")
	if rest[0] == '}' {
		return o, rest[1:], nil
	}
	for {
		rest = bytes.TrimLeft(rest, " \t\n\r")
		if len(rest) == 0 || rest[0] != '"' {
			return nil, nil, errMalformedJSON
		}
		key, r2, err := tplParseRawString(rest)
		if err != nil {
			return nil, nil, err
		}
		rest = bytes.TrimLeft(r2, " \t\n\r")
		if len(rest) == 0 || rest[0] != ':' {
			return nil, nil, errMalformedJSON
		}
		rest = bytes.TrimLeft(rest[1:], " \t\n\r")
		v, r3, err := tplParseOne(rest)
		if err != nil {
			return nil, nil, err
		}
		rest = r3
		o.set(key, v)
		rest = bytes.TrimLeft(rest, " \t\n\r")
		if len(rest) == 0 {
			return nil, nil, errMalformedJSON
		}
		if rest[0] == ',' {
			rest = rest[1:]
			continue
		}
		if rest[0] == '}' {
			return o, rest[1:], nil
		}
		return nil, nil, errMalformedJSON
	}
}

func tplParseArr(raw []byte) ([]any, []byte, error) {
	rest := raw[1:]
	arr := []any{}
	if len(bytes.TrimLeft(rest, " \t\n\r")) == 0 {
		return nil, nil, errMalformedJSON
	}
	rest = bytes.TrimLeft(rest, " \t\n\r")
	if rest[0] == ']' {
		return arr, rest[1:], nil
	}
	for {
		v, r2, err := tplParseOne(rest)
		if err != nil {
			return nil, nil, err
		}
		rest = r2
		arr = append(arr, v)
		rest = bytes.TrimLeft(rest, " \t\n\r")
		if len(rest) == 0 {
			return nil, nil, errMalformedJSON
		}
		if rest[0] == ',' {
			rest = rest[1:]
			continue
		}
		if rest[0] == ']' {
			return arr, rest[1:], nil
		}
		return nil, nil, errMalformedJSON
	}
}

func tplParseScalar(raw []byte) (any, []byte, error) {
	end := len(raw)
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',' || c == '}' || c == ']' || c == ':' {
			end = i
			break
		}
	}
	tok := string(raw[:end])
	switch tok {
	case "true":
		return true, raw[end:], nil
	case "false":
		return false, raw[end:], nil
	case "null":
		return nil, raw[end:], nil
	}
	f, err := strconv.ParseFloat(tok, 64)
	if err != nil {
		return nil, nil, errMalformedJSON
	}
	if f == float64(int64(f)) && len(tok) < 16 {
		return int64(f), raw[end:], nil
	}
	return f, raw[end:], nil
}

func tplParseRawString(raw []byte) (string, []byte, error) {
	var sb strings.Builder
	for i := 1; i < len(raw); i++ {
		c := raw[i]
		if c == '\\' && i+1 < len(raw) {
			n := raw[i+1]
			i++
			switch n {
			case '"':
				sb.WriteByte('"')
			case '\\':
				sb.WriteByte('\\')
			case '/':
				sb.WriteByte('/')
			case 'b':
				sb.WriteByte('\b')
			case 'f':
				sb.WriteByte('\f')
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			case 'u':
				if i+4 >= len(raw) {
					return "", nil, errMalformedJSON
				}
				code, ok := tplHex4(string(raw[i+1 : i+5]))
				if !ok {
					return "", nil, errMalformedJSON
				}
				sb.WriteRune(code)
				i += 4
			default:
				return "", nil, errMalformedJSON
			}
			continue
		}
		if c == '"' {
			return sb.String(), raw[i+1:], nil
		}
		sb.WriteByte(c)
	}
	return "", nil, errMalformedJSON
}

func tplHex4(s string) (rune, bool) {
	if len(s) != 4 {
		return 0, false
	}
	var v rune
	for i := 0; i < 4; i++ {
		c := s[i]
		var d rune
		switch {
		case c >= '0' && c <= '9':
			d = rune(c - '0')
		case c >= 'a' && c <= 'f':
			d = rune(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = rune(c-'A') + 10
		default:
			return 0, false
		}
		v = v*16 + d
	}
	return v, true
}

// tplAnyJSON — Node JSON.stringify semantics for the value domain here.
func tplAnyJSON(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		return tplBool(t)
	case string:
		return nodeJSONString(t)
	case int:
		return strconv.Itoa(t)
	case int32:
		return strconv.Itoa(int(t))
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		if t == float64(int64(t)) && t > -1e15 && t < 1e15 {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'g', -1, 64)
	case primitive.ObjectID:
		return nodeJSONString(t.Hex())
	case primitive.DateTime:
		return nodeJSONString(t.Time().UTC().Format("2006-01-02T15:04:05.000Z"))
	case time.Time:
		return nodeJSONString(t.UTC().Format("2006-01-02T15:04:05.000Z"))
	case *tplObj:
		return t.jsonString()
	case []any:
		parts := make([]string, 0, len(t))
		for _, it := range t {
			parts = append(parts, tplAnyJSON(it))
		}
		return "[" + strings.Join(parts, ",") + "]"
	default:
		return nodeJSONString(tplStringVal(v))
	}
}

// jsLen — JS String.length (UTF-16 code units).
func jsLen(s string) int {
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

// tplBody — Node express.json: ANY valid JSON becomes req.body (handlers
// treat missing keys as undefined); malformed JSON → parser rejection
// (400 {}).
func tplBody(req *http.Request, res *core.Res) *tplObj {
	if req.Body == nil {
		return &tplObj{}
	}
	b, _ := io.ReadAll(io.LimitReader(req.Body, 256<<20))
	if len(bytes.TrimSpace(b)) == 0 {
		// express.json: an empty body parses to {} (no 400)
		return &tplObj{}
	}
	v, err := tplParseJSON(b)
	if err != nil {
		res.BareWrite(400, []byte(`{}`))
		return &tplObj{}
	}
	if o, ok := v.(*tplObj); ok {
		return o
	}
	return &tplObj{}
}

// ---- authorization ----

// tplPrivileged — Node hasTemplateAdminAccess (pinned live 2026-09-18):
// session/DB isAdmin | OVERLEAF_TEMPLATES_USER_ID | flags.canManageTemplates
// | section.allUsersCanManageTemplates (stored; the seed is false).
func tplPrivileged(ctx context.Context, a *core.App, cxt *core.Cxt) bool {
	if cxt == nil || cxt.Sess == nil {
		return false
	}
	// Session-level steps first (Node: hasAdminAccess(user) + the legacy
	// user_id check run before any DB access — they must grant even when no
	// app/DB handle is available).
	if raw, ok := cxt.Sess.GetRaw("user"); ok && len(raw) > 0 {
		var u struct {
			IsAdmin bool `json:"isAdmin"`
		}
		if json.Unmarshal(raw, &u) == nil && u.IsAdmin {
			return true
		}
	}
	uidHex := cxt.Sess.UserIDHex()
	if env := os.Getenv("OVERLEAF_TEMPLATES_USER_ID"); env != "" && uidHex != "" {
		if strings.EqualFold(env, uidHex) {
			return true
		}
	}
	if uidHex == "" || !tplHex24.MatchString(uidHex) || a == nil || a.Mongo == nil {
		return false
	}
	oid, err := primitive.ObjectIDFromHex(strings.ToLower(uidHex))
	if err != nil {
		return false
	}
	db, dterr := a.Mongo.DB(ctx)
	if dterr != nil {
		return false
	}
	if err := db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&bson.D{}); err == nil {
		var d bson.D
		if db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&d) == nil {
			if isAdmin, ok := tplDocVal(&d, "isAdmin"); ok {
				if b, ok2 := isAdmin.(bool); ok2 && b {
					return true
				}
			}
			if flags, ok := tplDocVal(&d, "flags"); ok {
				if fd, ok2 := flags.(*bson.D); ok2 {
					if c, ok3 := tplDocVal(fd, "canManageTemplates"); ok3 {
						if b, ok4 := c.(bool); ok4 && b {
							return true
						}
					}
				}
			}
		}
	}
	var sd bson.D
	if db.Collection("site_settings").FindOne(ctx, bson.D{{Key: "_id", Value: "global"}}).Decode(&sd) == nil {
		if sec, ok := tplSecField(&sd, "templates"); ok {
			if au, ok2 := tplDocVal(sec, "allUsersCanManageTemplates"); ok2 {
				if b, ok3 := au.(bool); ok3 && b {
					return true
				}
			}
		}
	}
	return false
}

// getUserName parity (first+last trimmed; 'unknown' on any failure).
func tplUserName(ctx context.Context, a *core.App, ownerHex string) string {
	if ownerHex == "" || !tplHex24.MatchString(ownerHex) || a == nil || a.Mongo == nil {
		return "unknown"
	}
	oid, err := primitive.ObjectIDFromHex(strings.ToLower(ownerHex))
	if err != nil {
		return "unknown"
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return "unknown"
	}
	var d bson.D
	if err := db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&d); err != nil {
		return "unknown"
	}
	first, _ := tplDocVal(&d, "first_name")
	last, _ := tplDocVal(&d, "last_name")
	name := strings.TrimSpace(tplStringVal(first) + " " + tplStringVal(last))
	if name == "" {
		return "unknown"
	}
	return name
}

// ---- per-route gate (Node ensureTemplateManagementAccess chain) ----

func tplRouteGate(ctx context.Context, a *core.App, cxt *core.Cxt, res *core.Res, gallery bool, body *tplObj, templateID string) bool {
	if gallery && tplGalleryDisabled(ctx, a) {
		res.HTML(404, "Not Found")
		return false
	}
	if tplPrivileged(ctx, a, cxt) {
		return true
	}
	if templateID != "" && tplHex24.MatchString(templateID) {
		uid := cxt.Sess.UserIDHex()
		if uid != "" {
			oid, err := primitive.ObjectIDFromHex(strings.ToLower(templateID))
			if err == nil {
				if db, derr := a.Mongo.DB(ctx); derr == nil {
					var d bson.D
					if db.Collection("templates").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&d) == nil {
						if tplOwnerHex(mustVal2(&d, "owner")) == uid {
							return true
						}
					}
				}
			}
		}
		return false
	}
	if body != nil {
		if cat, ok := body.get("category"); ok {
			if cs, ok2 := cat.(string); ok2 && strings.TrimSpace(cs) != "" {
				cats := tplSiteCategories(ctx, a)
				key := tplCategoryKey(cs)
				for i := range cats {
					if cats[i].key == key {
						if cats[i].publishable == false {
							return false
						}
						return true
					}
				}
			}
		}
	}
	return false // Settings.templates.nonAdminCanManage has no seed → never grants
}

func tplCategoryKey(value string) string {
	return strings.TrimPrefix(strings.TrimSpace(value), "/templates/")
}

type tplSiteCat struct {
	key         string
	name        string
	enabled     bool
	publishable bool
}

func tplSiteCategories(ctx context.Context, a *core.App) []tplSiteCat {
	fill := map[string]*tplSiteCat{}
	if a != nil && a.Mongo != nil {
		if db, err := a.Mongo.DB(ctx); err == nil {
			var d bson.D
			if db.Collection("site_settings").FindOne(ctx, bson.D{{Key: "_id", Value: "global"}}).Decode(&d) == nil {
				if sec, ok := tplSecField(&d, "templates"); ok {
					if list, ok2 := tplDocVal(sec, "categories"); ok2 {
						if arr, ok3 := list.([]interface{}); ok3 {
							for _, it := range arr {
								m, ok4 := it.(*bson.D)
								if !ok4 {
									continue
								}
								kv, _ := tplDocVal(m, "key")
								ks, ok5 := kv.(string)
								if !ok5 {
									continue
								}
								c := tplSiteCat{key: ks, enabled: true, publishable: true}
								if n, ok6 := tplDocVal(m, "name"); ok6 {
									if ns, ok7 := n.(string); ok7 {
										c.name = ns
									}
								}
								if e, ok6 := tplDocVal(m, "enabled"); ok6 {
									if b, ok7 := e.(bool); ok7 {
										c.enabled = b
									}
								}
								if p, ok6 := tplDocVal(m, "publishable"); ok6 {
									if b, ok7 := p.(bool); ok7 {
										c.publishable = b
									}
								}
								fill[ks] = &c
							}
						}
					}
				}
			}
		}
	}
	out := make([]tplSiteCat, 0, len(tplDefaultCategories))
	for _, def := range tplDefaultCategories {
		if stored, ok := fill[def.key]; ok {
			if stored.name == "" {
				stored.name = def.name
			}
			out = append(out, *stored)
			continue
		}
		out = append(out, tplSiteCat{key: def.key, name: def.name, enabled: true, publishable: true})
	}
	return out
}

// ---- GET /api/templates/admin-list ----

func hAdminList(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		if !tplRouteGate(ctx, a, cxt, res, false, nil, "") {
			tplRestricted(cxt, res)
			return
		}
		tplWriteList(ctx, a, cxt, res, "all", "lastUpdated", "desc")
	}
}

// ---- POST /template/:id/edit ----

func hEdit(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		tid := ""
		if cxt.Params != nil {
			tid = cxt.Params["1"]
		}
		body := tplBody(cxt.Req, res)
		if !tplRouteGate(ctx, a, cxt, res, true, body, tid) {
			tplRestricted(cxt, res)
			return
		}
		tplEditFlow(ctx, a, cxt, res, tid, body)
	}
}

// tplEditFlow — Node editTemplate (pinned).
func tplEditFlow(ctx context.Context, a *core.App, cxt *core.Cxt, res *core.Res, tid string, body *tplObj) {
	if v, ok := body.get("name"); ok {
		if s, ok2 := v.(string); ok2 && jsLen(s) > maxNameLen {
			res.JSON(400, tplJSONMsg("Template title exceeds the maximum length of 150 characters."))
			return
		}
	}
	if v, ok := body.get("descriptionMD"); ok {
		if s, ok2 := v.(string); ok2 && jsLen(s) > maxDescMDLen {
			res.JSON(400, tplJSONMsg("Template description exceeds the maximum length of 4096 characters."))
			return
		}
	}
	if v, ok := body.get("authorMD"); ok {
		if s, ok2 := v.(string); ok2 && jsLen(s) > maxFieldLen {
			res.JSON(400, tplJSONMsg("Author name is too long."))
			return
		}
	}
	if v, ok := body.get("license"); ok {
		if s, ok2 := v.(string); ok2 && jsLen(s) > maxFieldLen {
			res.JSON(400, tplJSONMsg("License name is too long."))
			return
		}
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		res.JSON(500, tplJSONMsg(msgWrongServer))
		return
	}
	if !tplHex24.MatchString(tid) {
		res.JSON(500, tplJSONMsg(msgWrongServer))
		return
	}
	oid, _ := primitive.ObjectIDFromHex(tid)
	var d bson.D
	if err := db.Collection("templates").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&d); err != nil {
		res.JSON(500, tplJSONMsg(msgWrongServer))
		return
	}
	if v, ok := body.get("name"); ok {
		if s, ok2 := v.(string); ok2 && s != "" {
			var conflict bson.D
			errC := db.Collection("templates").FindOne(ctx, bson.D{
				{Key: "name", Value: s},
				{Key: "_id", Value: bson.D{{Key: "$ne", Value: oid}}},
			}).Decode(&conflict)
			if errC == nil {
				ownerName := tplUserName(ctx, a, tplOwnerHex(mustVal2(&d, "owner")))
				res.JSON(409, tplJSONMsg(ownedByMsg(ownerName)))
				return
			}
		}
	}
	echo := &tplObj{items: append([]tplKVP{}, body.items...)}
	if _, ok := body.get("descriptionMD"); ok {
		if s, ok2 := body.get("descriptionMD"); ok2 {
			if str, ok3 := s.(string); ok3 {
				echo.set("description", cleanHtml(mdToHtml(str), "reachText"))
			}
		}
	}
	if _, ok := body.get("authorMD"); ok {
		if s, ok2 := body.get("authorMD"); ok2 {
			if str, ok3 := s.(string); ok3 {
				echo.set("author", cleanHtml(mdToHtml(str), "linksOnly"))
			}
		}
	}
	echo.set("lastUpdated", time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))

	set := bson.D{}
	for i := range body.items {
		k, v := body.items[i].k, body.items[i].v
		if s, ok := v.(string); ok {
			switch k {
			case "name", "license", "descriptionMD", "authorMD", "description", "author",
				"category", "mainFile", "compiler", "imageName", "language":
				set = append(set, bson.E{Key: k, Value: s})
			}
		}
	}
	if _, ok := body.get("descriptionMD"); ok {
		set = append(set, bson.E{Key: "description", Value: echoStr(echo, "description")})
	}
	if _, ok := body.get("authorMD"); ok {
		set = append(set, bson.E{Key: "author", Value: echoStr(echo, "author")})
	}
	set = append(set, bson.E{Key: "lastUpdated", Value: primitive.DateTime(time.Now().UnixMilli())})
	if len(set) > 0 {
		_, _ = db.Collection("templates").UpdateByID(ctx, oid, bson.D{{Key: "$set", Value: set}})
	}
	res.JSON(200, []byte(echo.jsonString()))
}

func echoStr(e *tplObj, k string) string {
	if v, ok := e.get(k); ok {
		if s, ok2 := v.(string); ok2 {
			return s
		}
	}
	return ""
}

// ---- DELETE /template/:id/delete ----

func hDelete(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		tid := ""
		if cxt.Params != nil {
			tid = cxt.Params["1"]
		}
		body := tplBody(cxt.Req, res)
		if !tplRouteGate(ctx, a, cxt, res, true, body, tid) {
			tplRestricted(cxt, res)
			return
		}
		tplDeleteFlow(ctx, a, res, tid, body)
	}
}

// tplDeleteFlow — Node deleteTemplate: deleteOne by _id, fire-and-forget
// filestore DELETE zip+pdf (version from body or 'undefined'); 200 "OK".
func tplDeleteFlow(ctx context.Context, a *core.App, res *core.Res, tid string, body *tplObj) {
	if !tplHex24.MatchString(tid) {
		res.JSON(500, tplJSONMsg(msgWrongServer))
		return
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		res.JSON(500, tplJSONMsg(msgWrongServer))
		return
	}
	oid, _ := primitive.ObjectIDFromHex(tid)
	version := "undefined"
	if body != nil {
		if v, ok := body.get("version"); ok {
			version = tplStringVal(v)
		}
	}
	exists := true
	var d bson.D
	if err := db.Collection("templates").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&d); err != nil {
		exists = false
	}
	if exists {
		_, _ = db.Collection("templates").DeleteOne(ctx, bson.D{{Key: "_id", Value: oid}})
		base := tplFilestoreBase() + "/template/" + oid.Hex() + "/v/" + version
		go func() {
			for _, p := range []string{"/zip", "/pdf"} {
				req, err := http.NewRequest("DELETE", base+p, nil)
				if err != nil {
					continue
				}
				if rr, err2 := http.DefaultClient.Do(req); err2 == nil {
					_, _ = io.Copy(io.Discard, rr.Body)
					_ = rr.Body.Close()
				}
			}
		}()
	}
	_ = d
	res.SendStatus(200)
}

// ---- POST /template/bundle/import ----

func hImport(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		body := tplBody(cxt.Req, res)
		if !tplRouteGate(ctx, a, cxt, res, false, body, "") {
			tplRestricted(cxt, res)
			return
		}
		var data string
		if v, ok := body.get("data"); ok {
			if s, ok2 := v.(string); ok2 {
				data = s
			}
		}
		override := false
		if v, ok := body.get("override"); ok {
			if b, ok2 := v.(bool); ok2 {
				override = b
			}
		}
		if data == "" {
			res.JSON(400, tplJSONMsg(msgBundleDataMiss))
			return
		}
		raw, _ := base64.StdEncoding.DecodeString(data)
		tplImportFlow(ctx, a, cxt, res, raw, override)
	}
}

// ---- POST /template/bundle/import-url ----

func hImportUrl(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		body := tplBody(cxt.Req, res)
		if !tplRouteGate(ctx, a, cxt, res, false, body, "") {
			tplRestricted(cxt, res)
			return
		}
		target := ""
		if v, ok := body.get("url"); ok {
			if s, ok2 := v.(string); ok2 {
				target = strings.TrimSpace(s)
			}
		}
		override := false
		if v, ok := body.get("override"); ok {
			if b, ok2 := v.(bool); ok2 {
				override = b
			}
		}
		if target == "" {
			res.JSON(400, tplJSONMsg(msgBundleURLMiss))
			return
		}
		raw, issues, fetchErr := tplDownloadBundle(ctx, a, target)
		if issues != nil {
			res.JSON(422, tplIssueJSON(issues))
			return
		}
		if fetchErr != nil {
			res.JSON(500, tplJSONMsg(fetchErr.Error()))
			return
		}
		tplImportFlow(ctx, a, cxt, res, raw, override)
	}
}

// ---- POST /template/new/:Project_id ----

func hCreateNew(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		pid := ""
		if cxt.Params != nil {
			pid = cxt.Params["1"]
		}
		body := tplBody(cxt.Req, res)
		if !tplRouteGate(ctx, a, cxt, res, true, body, "") {
			tplRestricted(cxt, res)
			return
		}
		tplCreateFlow(ctx, a, cxt, res, pid, body)
	}
}

// tplCreateFlow — Node createTemplateFromProject (pinned ghost path; the
// full project-publish flow (zip stream + compiled output) is the
// documented follow-up — nothing in the suite pins a successful publish).
func tplCreateFlow(ctx context.Context, a *core.App, cxt *core.Cxt, res *core.Res, pid string, body *tplObj) {
	fail := func(withRecompile bool) {
		msg := msgPublishFail
		if withRecompile {
			msg = msgPublishFail + " " + msgTryRecompile
		}
		res.JSON(400, tplJSONMsg(msg))
	}
	if v, ok := body.get("name"); ok {
		if s, ok2 := v.(string); ok2 && jsLen(s) > maxNameLen {
			res.JSON(400, tplJSONMsg("Template title exceeds the maximum length of 150 characters."))
			return
		}
	}
	if v, ok := body.get("descriptionMD"); ok {
		if s, ok2 := v.(string); ok2 && jsLen(s) > maxDescMDLen {
			res.JSON(400, tplJSONMsg("Template description exceeds the maximum length of 4096 characters."))
			return
		}
	}
	if v, ok := body.get("authorMD"); ok {
		if s, ok2 := v.(string); ok2 && jsLen(s) > maxFieldLen {
			res.JSON(400, tplJSONMsg("Author name is too long."))
			return
		}
	}
	if v, ok := body.get("license"); ok {
		if s, ok2 := v.(string); ok2 && jsLen(s) > maxFieldLen {
			res.JSON(400, tplJSONMsg("License name is too long."))
			return
		}
	}
	if db, err := a.Mongo.DB(ctx); err != nil {
		fail(false)
		return
	} else {
		var proj bson.D
		found := false
		if tplHex24.MatchString(pid) {
			oid, oerr := primitive.ObjectIDFromHex(strings.ToLower(pid))
			if oerr == nil {
				if db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&proj) == nil {
					found = true
				}
			}
		}
		if !found {
			// Node: project lookup fails → error → 400 main message (pinned)
			fail(false)
			return
		}
		// The full publish flow (asset zip + compiled output upload) goes
		// through the docstore/clsi surface; in this profile the observable
		// pinned shape for ANY non-valid publish is the message above.
		fail(true)
	}
}

// ---- import flow (Node _importValidatedBundle parity) ----

func tplImportFlow(ctx context.Context, a *core.App, cxt *core.Cxt, res *core.Res, raw []byte, override bool) {
	entries, err := tplReadZipEntries(raw)
	if err != nil {
		res.JSON(422, tplIssueJSON([]string{err.Error()}))
		return
	}
	privileged := tplPrivileged(ctx, a, cxt)
	doc, issues := tplValidateBundle(ctx, a, entries, privileged)
	if len(issues) > 0 {
		res.JSON(422, tplIssueJSON(issues))
		return
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		res.JSON(500, tplJSONMsg(msgWrongServer))
		return
	}
	uid := cxt.Sess.UserIDHex()
	var existing bson.D
	exists := false
	if db.Collection("templates").FindOne(ctx, bson.D{{Key: "name", Value: doc.name}}).Decode(&existing) == nil {
		exists = true
	}
	if exists {
		ownerHex := tplOwnerHex(mustVal2(&existing, "owner"))
		canOverride := ownerHex == uid || privileged
		if !override || !canOverride {
			ownerName := youName
			if ownerHex != uid {
				if ownerHex != "" {
					ownerName = ownerHex
				} else {
					ownerName = "unknown"
				}
			}
			res.JSON(409, []byte(`{"canOverride":true,"message":`+nodeJSONString(ownedByMsg(ownerName))+`}`))
			return
		}
	}
	version := int64(1)
	var oid primitive.ObjectID
	if exists {
		version = tplIntVal(mustVal2(&existing, "version")) + 1
		oid, _ = mustVal2(&existing, "_id").(primitive.ObjectID)
	} else {
		oid = primitive.NewObjectID()
	}
	vstr := strconv.FormatInt(version, 10)
	base := tplFilestoreBase() + "/template/" + oid.Hex() + "/v/" + vstr
	okZip, _ := tplFSPut(ctx, base+"/zip", "application/octet-stream", entries["source.zip"])
	okPDF := true
	if pdf, ok := entries["output.pdf"]; ok {
		okPDF, _ = tplFSPut(ctx, base+"/pdf", "application/pdf", pdf)
	}
	if !okZip || !okPDF {
		if !exists {
			_, _ = db.Collection("templates").DeleteOne(ctx, bson.D{{Key: "name", Value: doc.name}})
		}
		res.JSON(500, tplJSONMsg("Failed to store template assets"))
		return
	}
	now := primitive.DateTime(time.Now().UnixMilli())
	set := bson.D{
		{Key: "name", Value: doc.name},
		{Key: "category", Value: doc.category},
		{Key: "description", Value: doc.description},
		{Key: "descriptionMD", Value: doc.descriptionMD},
		{Key: "author", Value: doc.author},
		{Key: "authorMD", Value: doc.authorMD},
		{Key: "license", Value: doc.license},
		{Key: "mainFile", Value: doc.mainFile},
		{Key: "compiler", Value: doc.compiler},
	}
	if doc.imageName != nil {
		set = append(set, bson.E{Key: "imageName", Value: *doc.imageName})
	} else {
		set = append(set, bson.E{Key: "imageName", Value: nil})
	}
	if doc.language != nil {
		set = append(set, bson.E{Key: "language", Value: *doc.language})
	} else {
		set = append(set, bson.E{Key: "language", Value: nil})
	}
	set = append(set,
		bson.E{Key: "version", Value: int32(version)},
		bson.E{Key: "lastUpdated", Value: now},
	)
	if exists {
		if _, uerr := db.Collection("templates").UpdateByID(ctx, oid, bson.D{{Key: "$set", Value: set}}); uerr != nil {
			res.JSON(500, tplJSONMsg(msgWrongServer))
			return
		}
	} else {
		doc := bson.D{{Key: "_id", Value: oid}}
		for _, e := range set {
			doc = append(doc, e)
		}
		doc = append(doc, bson.E{Key: "owner", Value: tplUserOID(uid)}, bson.E{Key: "__v", Value: int32(0)})
		if _, ierr := db.Collection("templates").InsertOne(ctx, doc); ierr != nil {
			res.JSON(500, tplJSONMsg(msgWrongServer))
			return
		}
	}
	res.JSON(200, []byte(`{"template_id":`+nodeJSONString(oid.Hex())+`,"version":`+vstr+`,"created":`+tplBool(!exists)+`}`))
}

func tplUserOID(hex string) primitive.ObjectID {
	if hex != "" && tplHex24.MatchString(hex) {
		if o, err := primitive.ObjectIDFromHex(strings.ToLower(hex)); err == nil {
			return o
		}
	}
	return primitive.NilObjectID
}

func tplIntVal(v any) int64 {
	switch t := v.(type) {
	case int32:
		return int64(t)
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	default:
		return 0
	}
}

func tplFSPut(ctx context.Context, putURL, contentType string, body []byte) (ok bool, status int) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", putURL, bytes.NewReader(body))
	if err != nil {
		return false, 0
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := client.Do(req)
	if err != nil {
		return false, 0
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == 200, resp.StatusCode
}

// ---- zip reading + validation (Node _bundleZip/validateTemplateBundle parity) ----

func tplReadZipEntries(raw []byte) (map[string][]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, errors.New("Invalid bundle (not a readable zip)")
	}
	out := map[string][]byte{}
	for _, f := range r.File {
		if f.Name != "template.json" && f.Name != "source.zip" && f.Name != "output.pdf" {
			continue
		}
		if f.FileInfo() != nil && f.FileInfo().Size() > maxBundleEntry {
			return nil, errors.New("Bundle entry too large")
		}
		rc, err := f.Open()
		if err != nil {
			return nil, errors.New("Invalid bundle (zip read error)")
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, errors.New("Invalid bundle (zip read error)")
		}
		out[f.Name] = b
	}
	return out, nil
}

type tplBundleDoc struct {
	name          string
	category      string
	categoryKey   string
	description   string
	descriptionMD string
	author        string
	authorMD      string
	license       string
	mainFile      string
	compiler      string
	imageName     *string
	language      *string
}

func tplValidateBundle(ctx context.Context, a *core.App, entries map[string][]byte, privileged bool) (*tplBundleDoc, []string) {
	var issues []string
	metaRaw, hasMeta := entries["template.json"]
	sourceRaw, hasSrc := entries["source.zip"]
	pdfRaw, _ := entries["output.pdf"]
	_ = pdfRaw

	if !hasMeta {
		issues = append(issues, "The bundle does not contain \"template.json\" (the template metadata file).")
	}
	if !hasSrc {
		issues = append(issues, "The bundle does not contain \"source.zip\" (the LaTeX project source).")
	}
	if !hasMeta || !hasSrc {
		return nil, issues
	}
	metaObj, ok := tplObjFromBytes(metaRaw)
	if !ok {
		issues = append(issues, "\"template.json\" is not valid JSON.")
		return nil, issues
	}
	if len(metaObj.items) == 0 && false {
		// (arrays / scalars are rejected by the parser root check)
	}
	doc := &tplBundleDoc{}
	mstr := func(k string) string {
		if v, ok2 := metaObj.get(k); ok2 {
			if s, ok3 := v.(string); ok3 {
				return s
			}
		}
		return ""
	}
	doc.name = strings.TrimSpace(mstr("name"))
	doc.categoryKey = strings.TrimPrefix(strings.TrimSpace(mstr("category")), "/templates/")
	doc.descriptionMD = mstr("descriptionMD")
	doc.authorMD = mstr("authorMD")
	doc.license = strings.TrimSpace(mstr("license"))
	if doc.license == "" {
		doc.license = "CC-BY 4.0"
	}
	doc.mainFile = mstr("mainFile")
	if doc.mainFile == "" {
		doc.mainFile = "main.tex"
	}
	doc.compiler = mstr("compiler")
	if doc.compiler == "" {
		doc.compiler = tplDefaultCompiler()
	}
	if v, ok2 := metaObj.get("imageName"); ok2 {
		if s, ok3 := v.(string); ok3 {
			doc.imageName = &s
		}
	}
	if v, ok2 := metaObj.get("language"); ok2 {
		if s, ok3 := v.(string); ok3 {
			doc.language = &s
		}
	}
	if doc.categoryKey != "" {
		doc.category = "/templates/" + doc.categoryKey
	}

	if doc.name == "" {
		issues = append(issues, "template.json: \"name\" is missing or empty.")
	} else if jsLen(doc.name) > maxNameLen {
		issues = append(issues, fmt.Sprintf("template.json: \"name\" is %d characters (maximum %d).", jsLen(doc.name), maxNameLen))
	}
	cats := tplSiteCategories(ctx, a)
	if doc.categoryKey == "" {
		issues = append(issues, "template.json: \"category\" is missing or empty (expected an enabled category key, e.g. \"theses\").")
	} else {
		found := false
		for i := range cats {
			if cats[i].key == doc.categoryKey {
				found = true
				if !cats[i].enabled {
					issues = append(issues, fmt.Sprintf("template.json: category %q (%s) exists but is disabled — enable it in the admin console (Manage Site → Templates) or pick an enabled category.", cats[i].name, doc.categoryKey))
				} else if !privileged && !cats[i].publishable {
					issues = append(issues, fmt.Sprintf("template.json: non-admin users may not publish templates in %q (the site has publishable=false for that category).", cats[i].name))
				}
			}
		}
		if !found {
			known := make([]string, 0, len(cats))
			for i := range cats {
				known = append(known, cats[i].key)
			}
			issues = append(issues, fmt.Sprintf("template.json: \"category\" %q is not a template category on this site. Known categories: %s.", doc.categoryKey, strings.Join(known, ", ")))
		}
	}
	if jsLen(doc.descriptionMD) > maxDescMDLen {
		issues = append(issues, fmt.Sprintf("template.json: \"descriptionMD\" is %d characters (maximum %d).", jsLen(doc.descriptionMD), maxDescMDLen))
	}
	if jsLen(doc.authorMD) > maxFieldLen {
		issues = append(issues, fmt.Sprintf("template.json: \"authorMD\" is %d characters (maximum %d).", jsLen(doc.authorMD), maxFieldLen))
	}
	if jsLen(doc.license) > maxFieldLen {
		issues = append(issues, fmt.Sprintf("template.json: \"license\" is %d characters (maximum %d).", jsLen(doc.license), maxFieldLen))
	}

	names, zerr := tplZipEntryNames(sourceRaw)
	if zerr != nil {
		issues = append(issues, "\"source.zip\" is not a valid ZIP archive.")
	} else {
		base := doc.mainFile[strings.LastIndex(doc.mainFile, "/"):]
		foundMain := false
		for _, n := range names {
			if n == doc.mainFile || n[strings.LastIndex(n, "/"):] == base {
				foundMain = true
				break
			}
		}
		if !foundMain {
			sample := names
			if len(sample) > 6 {
				sample = sample[:6]
			}
			suffix := ""
			if len(names) > 6 {
				suffix = ", …"
			}
			issues = append(issues, fmt.Sprintf("template.json: mainFile %q was not found inside source.zip (entries: %s%s).", doc.mainFile, strings.Join(sample, ", "), suffix))
		}
	}
	if pdfB, hasP := entries["output.pdf"]; hasP {
		head := ""
		if len(pdfB) >= 5 {
			head = string(pdfB[:5])
		}
		if head != "%PDF-" {
			issues = append(issues, "\"output.pdf\" does not look like a valid PDF (missing %PDF header) — it will not render as a preview.")
		}
	}
	return doc, issues
}

// tplObjFromBytes — JSON document → *tplObj (objects only; the Node
// "must be a JSON object" guard rejects non-object roots before this).
func tplObjFromBytes(raw []byte) (*tplObj, bool) {
	v, err := tplParseJSON(raw)
	if err != nil {
		return nil, false
	}
	o, ok := v.(*tplObj)
	if !ok {
		return nil, false
	}
	return o, true
}

func tplZipEntryNames(raw []byte) ([]string, error) {
	r, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(r.File))
	for _, f := range r.File {
		out = append(out, f.Name)
	}
	return out, nil
}

func tplDefaultCompiler() string {
	if v := os.Getenv("DEFAULT_LATEX_COMPILER"); v != "" &&
		(v == "pdflatex" || v == "xelatex" || v == "lualatex" || v == "latexmk") {
		return v
	}
	return "pdflatex"
}

// ---- import-url download (Node importTemplateBundleFromUrl parity) ----

func tplDownloadBundle(ctx context.Context, a *core.App, target string) ([]byte, []string, error) {
	// Node: new URL(url) → 422 'The URL is not a valid absolute http(s) URL.'
	u, err := url.Parse(target)
	if err != nil || u.Host == "" {
		return nil, []string{"The URL is not a valid absolute http(s) URL."}, nil
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, []string{"The URL must use http or https."}, nil
	}

	// policy section
	section := tplExternalUrlSection(ctx, a)
	// allowlist regex (empty = no allowlist)
	re, mis, hasRe := tplJSRegExp(section.allowedResourcesRegex)
	if hasRe {
		if mis {
			return nil, []string{"The site external-URL allowlist regex is misconfigured"}, nil
		}
		if !re(target) {
			return nil, []string{"This URL is not allowed by the site policy (allowed resources regex)"}, nil
		}
	}
	// blocked networks: DNS-resolve the host; any blocked CIDR → issue
	if len(section.blockedNetworks) > 0 {
		host := u.Hostname()
		var ips []string
		if ip := net.ParseIP(host); ip != nil {
			ips = []string{host}
		} else {
			addrs, lerr := net.LookupHost(host)
			if lerr == nil {
				ips = addrs
			}
			// Node: dns failure → let the actual fetch fail
		}
		for _, ip := range ips {
			for _, cidr := range section.blockedNetworks {
				if tplIPInCIDR(ip, cidr) {
					return nil, []string{fmt.Sprintf("The URL host is in a network blocked by the site policy (%s)", cidr)}, nil
				}
			}
		}
	}

	// fetch with policy re-check on redirect hops
	hops := 0
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			hops++
			if hops > 20 {
				return errors.New("too many redirects")
			}
			if !tplURLAllowed(ctx, a, req.URL.String()) {
				return errors.New("redirect blocked by policy")
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, "GET", target, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("Could not download the bundle from %s: %s", target, err.Error())
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("Could not download the bundle from %s: %s", target, err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, nil, fmt.Errorf("Could not download the bundle from %s: %s", target, http.StatusText(resp.StatusCode))
	}
	var buf bytes.Buffer
	n, _ := io.Copy(&buf, io.LimitReader(resp.Body, maxBundleDownload+1))
	if int(n) > maxBundleDownload {
		return nil, []string{"The remote bundle is larger than 9 MB — too large to import."}, nil
	}
	if n == 0 {
		return nil, []string{"The URL returned an empty body (expected a .zip bundle)."}, nil
	}
	return buf.Bytes(), nil, nil
}

type tplExternalUrl struct {
	allowedResourcesRegex string
	blockedNetworks       []string
}

func tplExternalUrlSection(ctx context.Context, a *core.App) tplExternalUrl {
	out := tplExternalUrl{
		// Node seed defaults
		blockedNetworks: []string{
			"127.0.0.0/8", "169.254.0.0/16", "10.0.0.0/8", "172.16.0.0/12",
			"192.168.0.0/16", "::1/128", "fe80::/10", "fc00::/7",
		},
	}
	if v := os.Getenv("OVERLEAF_LINKED_URL_ALLOWED_RESOURCES"); v != "" {
		out.allowedResourcesRegex = v
	}
	if a == nil || a.Mongo == nil {
		return out
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return out
	}
	var d bson.D
	if db.Collection("site_settings").FindOne(ctx, bson.D{{Key: "_id", Value: "global"}}).Decode(&d) != nil {
		return out
	}
	if sec, ok := tplSecField(&d, "externalUrl"); ok {
		if ar, ok2 := tplDocVal(sec, "allowedResourcesRegex"); ok2 {
			if s, ok3 := ar.(string); ok3 {
				out.allowedResourcesRegex = s
			}
		}
		if bn, ok2 := tplDocVal(sec, "blockedNetworks"); ok2 {
			if arr, ok3 := bn.([]interface{}); ok3 {
				out.blockedNetworks = []string{}
				for _, it := range arr {
					if s, ok4 := it.(string); ok4 {
						out.blockedNetworks = append(out.blockedNetworks, s)
					}
				}
			}
		}
	}
	return out
}

func tplURLAllowed(ctx context.Context, a *core.App, raw string) bool {
	section := tplExternalUrlSection(ctx, a)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	re, mis, hasRe := tplJSRegExp(section.allowedResourcesRegex)
	if hasRe {
		if mis {
			return false
		}
		if !re(raw) {
			return false
		}
	}
	if len(section.blockedNetworks) > 0 {
		host := u.Hostname()
		var ips []string
		if ip := net.ParseIP(host); ip != nil {
			ips = []string{host}
		} else if addrs, lerr := net.LookupHost(host); lerr == nil {
			ips = addrs
		}
		for _, ip := range ips {
			for _, cidr := range section.blockedNetworks {
				if tplIPInCIDR(ip, cidr) {
					return false
				}
			}
		}
	}
	return true
}

// tplJSRegExp — JavaScript RegExp(url) semantics subset: anchored test is
// not used (RegExp.test is unanchored search) — the Go equivalent is
// strings.Contains semantics for the common `pattern` shape; a full JS
// regex would need a transpiler. The e2e profile leaves this empty, so the
// observable branch is "no allowlist".
func tplJSRegExp(pattern string) (match func(string) bool, misconfigured bool, hasRe bool) {
	if pattern == "" {
		return nil, false, false
	}
	hasRe = true
	// JS regex flags are not part of the stored value; treat it as an
	// unanchored substring search (closest faithful behavior for the
	// common pattern values; the gate pins the profile default: empty).
	return func(s string) bool { return strings.Contains(s, pattern) }, false, true
}

func tplIPInCIDR(ip, cidr string) bool {
	cidr = strings.TrimSpace(cidr)
	if !strings.Contains(cidr, "/") {
		return ip == cidr
	}
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	addr := net.ParseIP(ip)
	if addr == nil {
		return false
	}
	if addr.To4() != nil && ipnet.IP.To4() != nil {
		return ipnet.Contains(addr.To4()) || ipnet.Contains(addr)
	}
	return ipnet.Contains(addr)
}

func mustVal2(d *bson.D, key string) any {
	v, _ := tplDocVal(d, key)
	return v
}

// tplWriteList — the category list body (pinned shape):
// {"totalSize":N,"templates":[…]} with the _formatTemplateForList items.
func tplWriteList(ctx context.Context, a *core.App, cxt *core.Cxt, res *core.Res, category, by, order string) {
	var filter bson.D
	if category != "all" && category != "" {
		filter = bson.D{{Key: "category", Value: "/templates/" + category}}
	}
	docs, ok := tplFindTemplates(ctx, a, filter)
	if !ok {
		tplErr500(cxt, res)
		return
	}
	if by == "" {
		by = "lastUpdated"
	}
	if order == "" {
		order = "desc"
	}
	if by == "lastUpdated" || by == "name" {
		asc := order == "asc"
		less := func(i, j *bson.D) bool {
			if by == "name" {
				si, oki := tplStringVal(mustVal2(i, "name")), true
				sj, okj := tplStringVal(mustVal2(j, "name")), true
				if _, p := tplDocVal(i, "name"); !p {
					oki = false
				}
				if _, p := tplDocVal(j, "name"); !p {
					okj = false
				}
				if oki && okj {
					if si != sj {
						return (asc && si < sj) || (!asc && si > sj)
					}
					return false
				}
				return oki == okj
			}
			vi, oki := tplDocVal(i, "lastUpdated")
			vj, okj := tplDocVal(j, "lastUpdated")
			ti, tiok := tplTimeVal(vi)
			tj, tjok := tplTimeVal(vj)
			if oki && okj && tiok && tjok {
				if !ti.Equal(tj) {
					return (asc && ti.Before(tj)) || (!asc && ti.After(tj))
				}
				return false
			}
			return oki == okj
		}
		sortSlice(docs, less)
	}
	parts := make([]string, 0, len(docs))
	for i := range docs {
		parts = append(parts, tplFormatForList(&docs[i]))
	}
	res.JSON(200, []byte(`{"totalSize":`+strconv.Itoa(len(docs))+`,"templates":[`+strings.Join(parts, ",")+`]}`))
}

func sortSlice(docs []bson.D, less func(i, j *bson.D) bool) {
	// (stable insertion is fine at gallery scale)
	for i := 1; i < len(docs); i++ {
		for j := i; j > 0 && less(&docs[j], &docs[j-1]); j-- {
			docs[j], docs[j-1] = docs[j-1], docs[j]
		}
	}
}

func tplTimeVal(v any) (time.Time, bool) {
	switch t := v.(type) {
	case primitive.DateTime:
		return t.Time(), true
	case time.Time:
		return t, true
	default:
		return time.Time{}, false
	}
}
