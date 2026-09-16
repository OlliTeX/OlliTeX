// P6.2 — admin-tools (services/web/modules/admin-tools) project surface.
// Node oracle pins (/tmp/p62_oracle.json) are the authority; Node router:
// modules/admin-tools/app/src/AdminToolsRouter.mjs.
//
//	GET    /admin/user                     -> 301 /hub#/site.general.users.all
//	GET    /admin/project                  -> 301 /hub#/site.general.projects.all
//	GET    /admin/active-projects          -> RT /clients projection
//	POST   /admin/user/:userId/projects    -> _getProjects (active+deleted)
//	POST   /admin/project/:project_id/trash|untrash
//	DELETE /admin/project/:project_id      -> soft delete -> 200 {deletedAt,deleterId}
//	POST   /admin/project/:project_id/undelete  -> 200 {name}
//	DELETE /admin/project/:project_id/purge     -> 200 OK
//	GET    /admin/project/:Project_id/members   -> {owner,members}
//	GET    /admin/project/:Project_id/invites   (P4 core, admin gate)
//	POST   /admin/project/:Project_id/invite    (P4 core + admin pre-validation)
//	PUT    /admin/project/:Project_id/users/:user_id  (P4 core + admin pre-validation)
//	DELETE /admin/project/:Project_id/users/:user_id  (P4 core)
//	DELETE /admin/project/:Project_id/invite/:invite_id (P4 core)
//	POST   /admin/project/:Project_id/invite/:invite_id/resend (P4 core)
//	GET    /admin/project/:Project_id/sharing-link
//	POST   /admin/project/:Project_id/sharing-link
//
// Gate splits (pinned):
//	- non-parseReq controllers (members, delete, undelete, trash, untrash,
//	  purge): malformed :Project_id -> 500 HTML page (Node `new ObjectId`
//	  BSONError) -> aGate500.
//	- parseReq controllers (invite/put/del user, revoke, resend, sharing-link):
//	  malformed :Project_id -> 404 {"error":"...Invalid Mongo ObjectId at
//	  \"params.Project_id\"","statusCode":404} -> aGateParam (the P4-core
//	  member-surface gate shape, pinned in P4).
// Login + site-admin run first on every route (302 /login for anon via the
// core global gate; 302 /restricted?from=... via RequireSiteAdmin). CSRF:
// token-less non-GET -> 403 (core chain, before authz).
package projectlist

import (
	"bytes"
	"golang.org/x/crypto/hkdf"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
	"go.mongodb.org/mongo-driver/bson"
		"go.mongodb.org/mongo-driver/bson/primitive"
)

// ---------- gates ----------

func aLoginAdmin(a *core.App, cxt *core.Cxt, res *core.Res) (string, bool) {
	uid := ""
	if cxt.Sess != nil {
		uid = cxt.Sess.UserIDHex()
	}
	if uid == "" {
		if core.AcceptsJSON(cxt.Req) {
			res.SendStatus(401)
		} else {
			res.Redirect(cxt.Req, 302, "/login")
		}
		return "", false
	}
	if !a.RequireSiteAdmin(cxt, res) {
		return "", false
	}
	return uid, true
}

// aGate500 — admin gate whose controllers throw BSONError on a malformed id:
// 500 HTML page (badpid_members/badpid_delete pins).
func aGate500(a *core.App, cxt *core.Cxt, res *core.Res) (string, *primitive.D, bool) {
	uid, ok := aLoginAdmin(a, cxt, res)
	if !ok {
		return "", nil, false
	}
	if !validOID.MatchString(cxt.Params["1"]) {
		aPage500(cxt, res)
		return "", nil, false
	}
	oid, _ := primitive.ObjectIDFromHex(strings.ToLower(cxt.Params["1"]))
	doc, lerr := loadProjectFull(a, cxt, oid)
	if lerr != nil {
		res.JSON(500, []byte("internal error"))
		return "", nil, false
	}
	if doc == nil {
		aNotFound(cxt, res)
		return "", nil, false
	}
	return uid, doc, true
}

// aGateParam — admin gate for the parseReq controllers: malformed id ->
// 404 Validation JSON (same shape as the P4 member-surface gate, P4 pin).
func aGateParam(a *core.App, cxt *core.Cxt, res *core.Res) (string, *primitive.D, bool) {
	uid, ok := aLoginAdmin(a, cxt, res)
	if !ok {
		return "", nil, false
	}
	if !validOID.MatchString(cxt.Params["1"]) {
		res.JSON(404, []byte(malformed404))
		return "", nil, false
	}
	oid, _ := primitive.ObjectIDFromHex(strings.ToLower(cxt.Params["1"]))
	doc, lerr := loadProjectFull(a, cxt, oid)
	if lerr != nil {
		res.JSON(500, []byte("internal error"))
		return "", nil, false
	}
	if doc == nil {
		aNotFound(cxt, res)
		return "", nil, false
	}
	return uid, doc, true
}

func aPage500(cxt *core.Cxt, res *core.Res) {
	views.Error500Page(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
}

func aNotFound(cxt *core.Cxt, res *core.Res) {
	if core.AcceptsJSON(cxt.Req) {
		res.JSON(404, []byte(colNotFound))
		return
	}
	accept := strings.ToLower(cxt.Req.Header.Get("Accept"))
	if accept == "" || strings.Contains(accept, "*/*") || strings.Contains(accept, "text/html") {
		views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
		return
	}
	res.PlainText(404, "not found")
}

// appendUnknown — Node strict-unknown-keys segment (insertion order,
// singular/plural).
func appendUnknown(segs []string, keyOrder []string, allowed ...string) []string {
	var unknown []string
	allow := map[string]bool{}
	for _, a := range allowed {
		allow[a] = true
	}
	for _, k := range keyOrder {
		if !allow[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		plural := "key"
		if len(unknown) > 1 {
			plural = "keys"
		}
		qs := make([]string, 0, len(unknown))
		for _, k := range unknown {
			qs = append(qs, `"`+k+`"`)
		}
		segs = append(segs, "Unrecognized "+plural+": "+strings.Join(qs, ", ")+` at "body"`)
	}
	return segs
}

// ---------- patterns ----------

var (
	adUserRedirPat   = regexp.MustCompile(`^/admin/user$`)
	adProjectRedPat  = regexp.MustCompile(`^/admin/project$`)
	activeProjsPat   = regexp.MustCompile(`^/admin/active-projects$`)
	adUserListPat    = regexp.MustCompile(`^/admin/user/([^/]+)/projects$`)
	adTrashPat       = regexp.MustCompile(`^/admin/project/([^/]+)/trash$`)
	adUntrashPat     = regexp.MustCompile(`^/admin/project/([^/]+)/untrash$`)
	adPurgePat       = regexp.MustCompile(`^/admin/project/([^/]+)/purge$`)
	adUndeletePat    = regexp.MustCompile(`^/admin/project/([^/]+)/undelete$`)
	adDeletePat      = regexp.MustCompile(`^/admin/project/([^/]+)$`)
	adMembersPat     = regexp.MustCompile(`^/admin/project/([^/]+)/members$`)
	adInvitePat      = regexp.MustCompile(`^/admin/project/([^/]+)/invite$`)
	adInvitesPat     = regexp.MustCompile(`^/admin/project/([^/]+)/invites$`)
	adUsersPat       = regexp.MustCompile(`^/admin/project/([^/]+)/users/([^/]+)$`)
	adInvRevokePat   = regexp.MustCompile(`^/admin/project/([^/]+)/invite/([0-9a-fA-F]{24})$`)
	adInvResendPat   = regexp.MustCompile(`^/admin/project/([^/]+)/invite/([0-9a-fA-F]{24})/resend$`)
	adShareLinkPat   = regexp.MustCompile(`^/admin/project/([^/]+)/sharing-link$`)
)

// ---------- legacy redirects ----------

func adminRedir(a *core.App, loc string) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, ok := aLoginAdmin(a, cxt, res); !ok {
			return
		}
		res.Redirect(cxt.Req, 301, loc)
	}
}

// aGateAdmin — the admin-surface gate for the parseReq controllers (invite
// core, PUT/DELETE users, revoke, resend, sharing-link): login + site-admin
// + malformed :Project_id -> 404 Validation JSON (pinned P4 member shape) +
// project load (404 matrix). NO per-project ownership check (Node admin
// routes carry only ensureUserIsSiteAdmin; the core membership checks live
// in the /project router middleware, not these handlers).
func aGateAdmin(a *core.App, cxt *core.Cxt, res *core.Res) (string, *primitive.D, bool) {
	uid, ok := aLoginAdmin(a, cxt, res)
	if !ok {
		return "", nil, false
	}
	if !validOID.MatchString(cxt.Params["1"]) {
		res.JSON(404, []byte(malformed404))
		return "", nil, false
	}
	oid, _ := primitive.ObjectIDFromHex(strings.ToLower(cxt.Params["1"]))
	doc, lerr := loadProjectFull(a, cxt, oid)
	if lerr != nil {
		res.JSON(500, []byte("internal error"))
		return "", nil, false
	}
	if doc == nil {
		aNotFound(cxt, res)
		return "", nil, false
	}
	return uid, doc, true
}

// adGateValidate — Node order: route middleware (site-admin) -> parseReq
// (body 400) -> core handler. Gates first, then body pre-validation
// (aggregated zod 400s), then the P4 core (re-validation is a no-op on a
// body that passed).
func adGateValidate(a *core.App, validate func(*core.Cxt, *core.Res) bool, next func(*core.Cxt, *core.Res)) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, _, ok := aGateAdmin(a, cxt, res); !ok {
			return
		}
		if !validate(cxt, res) {
			return
		}
		next(cxt, res)
	}
}

// adValidateInvite — Node inviteToProjectSchema (strict {email, privileges}).
func adValidateInvite(cxt *core.Cxt, res *core.Res) bool {
	body, _ := io.ReadAll(cxt.Req.Body)
	func() { cxt.Req.Body.Close() }()
	cxt.Req.Body = io.NopCloser(bytes.NewReader(body))
	var bm map[string]any
	var keyOrder []string
	if len(bytes.TrimSpace(body)) > 0 {
		if e := json.Unmarshal(body, &bm); e != nil || bm == nil {
			res.JSON(400, []byte("{}"))
			return false
		}
		keyOrder = bodyKeyOrder(body)
	} else {
		bm = map[string]any{}
	}
	var segs []string
	if v, hasE := bm["email"]; !hasE {
		segs = append(segs, `Invalid input: expected string, received undefined at "body.email"`)
	} else if _, isS := v.(string); !isS {
		segs = append(segs, `Invalid input: expected string, received `+zodRec(v)+` at "body.email"`)
	}
	if v, hasP := bm["privileges"]; !hasP {
		segs = append(segs, `Invalid option: expected one of "readOnly"|"readAndWrite"|"review" at "body.privileges"`)
	} else if s, isS := v.(string); !isS || (s != "readOnly" && s != "readAndWrite" && s != "review") {
		segs = append(segs, `Invalid option: expected one of "readOnly"|"readAndWrite"|"review" at "body.privileges"`)
	}
	segs = appendUnknown(segs, keyOrder, "email", "privileges")
	if len(segs) > 0 {
		res.JSON(400, []byte(`{"error":"Validation error: `+strings.Join(segs, "; ")+`","statusCode":400}`))
		return false
	}
	return true
}

// adValidateUser — Node setCollaboratorInfoSchema (strict {privilegeLevel}).
func adValidateUser(cxt *core.Cxt, res *core.Res) bool {
	body, _ := io.ReadAll(cxt.Req.Body)
	func() { cxt.Req.Body.Close() }()
	cxt.Req.Body = io.NopCloser(bytes.NewReader(body))
	var bm map[string]any
	var keyOrder []string
	if len(bytes.TrimSpace(body)) > 0 {
		if e := json.Unmarshal(body, &bm); e != nil || bm == nil {
			res.JSON(400, []byte("{}"))
			return false
		}
		keyOrder = bodyKeyOrder(body)
	} else {
		bm = map[string]any{}
	}
	var segs []string
	if v, hasL := bm["privilegeLevel"]; !hasL {
		segs = append(segs, `Invalid option: expected one of "readOnly"|"readAndWrite"|"review" at "body.privilegeLevel"`)
	} else if s, isS := v.(string); !isS || (s != "readOnly" && s != "readAndWrite" && s != "review") {
		segs = append(segs, `Invalid option: expected one of "readOnly"|"readAndWrite"|"review" at "body.privilegeLevel"`)
	}
	segs = appendUnknown(segs, keyOrder, "privilegeLevel")
	if len(segs) > 0 {
		res.JSON(400, []byte(`{"error":"Validation error: `+strings.Join(segs, "; ")+`","statusCode":400}`))
		return false
	}
	return true
}

// ---------- active projects ----------

func adminActiveProjects(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, ok := aLoginAdmin(a, cxt, res); !ok {
			return
		}
		base := os.Getenv("REALTIME_URL")
		if base == "" {
			base = "http://127.0.0.1:3026"
		}
		user := os.Getenv("REALTIME_USER")
		if user == "" {
			user = os.Getenv("WEB_API_USER")
		}
		if user == "" {
			user = "overleaf"
		}
		pass := os.Getenv("REALTIME_PASS")
		if pass == "" {
			pass = os.Getenv("WEB_API_PASSWORD")
		}
		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 10*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimSuffix(base, "/")+"/clients", nil)
		if err != nil {
			aPage500(cxt, res)
			return
		}
		req.SetBasicAuth(user, pass)
		rsp, err := http.DefaultClient.Do(req)
		if err != nil {
			aPage500(cxt, res)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(rsp.Body, 4<<20))
		rsp.Body.Close()
		var clients []map[string]any
		if json.Unmarshal(body, &clients) != nil {
			aPage500(cxt, res)
			return
		}
		type slot struct{ clients []map[string]any }
		order := []string{}
		ids := map[string]*slot{}
		for _, cl := range clients {
			pid := asStr(cl["project_id"])
			if pid == "" {
				pid = oidHex(cl["project_id"])
			}
			if _, has := ids[pid]; !has {
				order = append(order, pid)
				ids[pid] = &slot{}
			}
			ids[pid].clients = append(ids[pid].clients, cl)
		}
		db, dbErr := a.Mongo.DB(ctx)
		parts := make([]string, 0, len(order))
		for _, pid := range order {
			if !validOID.MatchString(pid) {
				aPage500(cxt, res)
				return
			}
			oid, _ := primitive.ObjectIDFromHex(pid)
			var name *string
			if dbErr == nil {
				var proj struct {
					Name *string `bson:"name"`
				}
				if dbErr = db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&proj); dbErr == nil {
					name = proj.Name
				}
			}
			if name == nil {
				// Node: getProject -> project null -> OError 500.
				aPage500(cxt, res)
				return
			}
			var au strings.Builder
			au.WriteByte('[')
			for i, cl := range ids[pid].clients {
				if i > 0 {
					au.WriteByte(',')
				}
				nm := strings.TrimSpace(asStr(cl["first_name"]) + " " + asStr(cl["last_name"]))
				em := ""
				if v, okm := cl["email"]; okm && v != nil {
					em = asStr(v)
				}
				if nm == "" {
					nm = em
				}
				if nm == "" {
					nm = "Unknown"
				}
				au.WriteString(`{"name":` + jstr(nm) + `,"email":`)
				if v, okm := cl["email"]; okm && v != nil {
					au.WriteString(jstr(asStr(v)))
				} else {
					au.WriteString(`null`)
				}
				au.WriteByte('}')
			}
			au.WriteByte(']')
			nmJSON := `null`
			if name != nil {
				nmJSON = jstr(*name)
			}
			parts = append(parts, `{"id":`+jstr(pid)+`,"name":`+nmJSON+`,"activeUsers":`+au.String()+`,"connectionCount":`+fmt.Sprint(len(ids[pid].clients))+`}`)
		}
		res.JSON(200, []byte("["+strings.Join(parts, ",")+"]"))
	}
}

// ---------- listing ----------

func nzBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t != ""
	case float64:
		return t != 0
	case int:
		return t != 0
	}
	return v != nil
}

func adNameSet(a *core.App, cxt *core.Cxt, ownerHex string) map[string]bool {
	set := map[string]bool{}
	if ownerHex == "" {
		return set
	}
	ctx := cxt.Req.Context()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return set
	}
	oid, _ := primitive.ObjectIDFromHex(ownerHex)
	add := func(filter bson.D) {
		cr, err := db.Collection("projects").Find(ctx, filter)
		if err != nil {
			return
		}
		for cr.Next(ctx) {
			var p struct {
				Name string `bson:"name"`
			}
			if cr.Decode(&p) == nil {
				set[p.Name] = true
			}
		}
	}
	add(bson.D{{Key: "owner_ref", Value: oid}})
	add(bson.D{{Key: "collablator_refs", Value: oid}})
	add(bson.D{{Key: "reviewer_refs", Value: oid}})
	add(bson.D{{Key: "readOnly_refs", Value: oid}})
	add(bson.D{{Key: "tokenAccessReadAndWrite_refs", Value: oid}, {Key: "publicAccesLevel", Value: "tokenBased"}})
	add(bson.D{{Key: "tokenAccessReadOnly_refs", Value: oid}, {Key: "publicAccesLevel", Value: "tokenBased"}})
	return set
}

// adEnsureUnique mirrors ProjectHelper.ensureNameIsUnique +
// _addNumericSuffixToProjectName (suffixes = []).
func adEnsureUnique(set map[string]bool, name string) (string, bool) {
	const maxLength = 4096
	if _, has := set[name]; !has {
		return name, true
	}
	numRe := regexp.MustCompile(` \((\d+)\)$`)
	basename, n := name, 1
	if m := numRe.FindStringSubmatch(name); m != nil {
		basename = numRe.ReplaceAllString(name, "")
		fmt.Sscanf(m[1], "%d", &n)
	}
	prefixRe := regexp.MustCompile(`^` + regexp.QuoteMeta(basename) + ` \(\d+\)$`)
	samePrefix := 0
	for nm := range set {
		if prefixRe.MatchString(nm) {
			samePrefix++
		}
	}
	last := len(set) + n
	if n > 1000 && samePrefix < n/2 {
		basename, n = name, 1
	}
	for n <= last {
		suffix := fmt.Sprintf(" (%d)", n)
		cand := basename
		if len(cand)+len(suffix) > maxLength {
			cand = cand[:maxLength-len(suffix)]
		}
		cand += suffix
		if _, has := set[cand]; !has {
			return cand, true
		}
		n++
	}
	return "", false // Node: OError 500
}

func adminUserProjects(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, ok := aLoginAdmin(a, cxt, res); !ok {
			return
		}
		raw := cxt.Params["1"]
		allUsers := raw == "" || raw == "null"
		var ownerHex string
		if !allUsers {
			if !validOID.MatchString(raw) {
				aPage500(cxt, res) // mongoose CastError
				return
			}
			ownerHex = strings.ToLower(raw)
		}

		body, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
		var bodyM map[string]any
		if len(bytes.TrimSpace(body)) > 0 {
			if e := json.Unmarshal(body, &bodyM); e != nil || bodyM == nil {
				bodyM = map[string]any{}
			}
		} else {
			bodyM = map[string]any{}
		}
		filters, _ := bodyM["filters"].(map[string]any)
		if filters == nil {
			filters = map[string]any{}
		}
		sortBy, sortOrder := "", ""
		if sp, okm := bodyM["sort"].(map[string]any); okm {
			sortBy, _ = sp["by"].(string)
			sortOrder, _ = sp["order"].(string)
		}
		if (sortBy != "" && sortBy != "lastUpdated" && sortBy != "title" && sortBy != "deletedAt" && sortBy != "owner") ||
			(sortOrder != "" && sortOrder != "asc" && sortOrder != "desc") {
			aPage500(cxt, res) // OError 'Invalid sorting criteria'
			return
		}
		if sortBy == "" {
			sortBy = "lastUpdated"
		}
		if sortOrder == "" {
			sortOrder = "desc"
		}

		ctx := cxt.Req.Context()
		db, dbErr := a.Mongo.DB(ctx)
		if dbErr != nil {
			aPage500(cxt, res)
			return
		}
		pFilter := bson.D{}
		if !allUsers {
			oid, _ := primitive.ObjectIDFromHex(ownerHex)
			pFilter = bson.D{{Key: "owner_ref", Value: oid}}
		}
		pcr, err := db.Collection("projects").Find(ctx, pFilter)
		if err != nil {
			aPage500(cxt, res)
			return
		}
		var activeD []primitive.D
		for pcr.Next(ctx) {
			var d primitive.D
			if pcr.Decode(&d) != nil {
				aPage500(cxt, res)
				return
			}
			activeD = append(activeD, d)
		}
		dFilter := bson.D{}
		if allUsers {
			dFilter = bson.D{{Key: "project", Value: bson.D{{Key: "$type", Value: "object"}}}}
		} else {
			oid, _ := primitive.ObjectIDFromHex(ownerHex)
			dFilter = bson.D{{Key: "project.owner_ref", Value: oid}}
		}
		dcr, err := db.Collection("deletedProjects").Find(ctx, dFilter)
		if err != nil {
			aPage500(cxt, res)
			return
		}
		var delD []primitive.D
		for dcr.Next(ctx) {
			var d primitive.D
			if dcr.Decode(&d) != nil {
				aPage500(cxt, res)
				return
			}
			delD = append(delD, d)
		}

		yearAgo := time.Now().AddDate(-1, 0, 0)
		type row struct {
			id, name, owner, lu, lub, dat, did string
			nameHas, nameNil, ownerHas, ownerNull, luHas, lubHas, lubNil, datHas, didHas, delHas bool
			inactive, trashed bool
		}
		mkRow := func(p any, out func(row)) bool {
			// returns false -> caller 500s.
			var r row
			doc := *dcast(p)
			idv, ok := doc[0].Value.(primitive.ObjectID)
			if !ok {
				return false
			}
			r.id = idv.Hex()
			if v, okm := dg(doc, "name"); okm {
				if s, isS := v.(string); isS {
					r.name, r.nameHas = s, true
				} else if v == nil {
					r.nameNil = true // explicit null -> "name":null
				}
			}
			if v, okm := dg(doc, "owner_ref"); okm {
				switch o := v.(type) {
				case primitive.ObjectID:
					r.owner, r.ownerHas = o.Hex(), true
				case string:
					r.owner, r.ownerHas = o, true
				case nil:
					r.ownerNull = true
				}
			}
			if v, okm := dg(doc, "lastUpdated"); okm && v != nil {
				t, okT := asTime(v)
				if !okT {
					return false // Node toISOString() throws -> 500
				}
				r.lu, r.luHas = t.UTC().Format("2006-01-02T15:04:05.000Z"), true
			}
			if v, okm := dg(doc, "lastUpdatedBy"); okm {
				if o, isO := v.(primitive.ObjectID); isO {
					r.lub, r.lubHas = o.Hex(), true
				}
				} else if v == nil {
					r.lubNil = true // explicit null -> "lastUpdatedBy":null
			}
			if v, okm := dg(doc, "lastOpened"); okm && v != nil {
				if t, okT := asTime(v); okT {
					r.inactive = t.Before(yearAgo)
				}
			}
			if r.ownerHas && r.owner != "" {
				if !validOID.MatchString(r.owner) {
					return false // isTrashed: new ObjectId throws -> 500
				}
				oo, _ := primitive.ObjectIDFromHex(strings.ToLower(r.owner))
				if tvRaw, okTr := dg(doc, "trashed"); okTr {
					arr, isA := tvRaw.(primitive.A)
					if !isA {
						return false // Node: (trashed||[]).some TypeError -> 500
					}
					for _, tv := range arr {
						to, isO := tv.(primitive.ObjectID)
						if !isO {
							return false // Node: id.equals TypeError -> 500
						}
						if to == oo {
							r.trashed = true
						}
					}
				}
			}
			out(r)
			return true
		}
		isoTS := func(v any) (string, bool) {
			t, okT := asTime(v)
			if !okT {
				return "", false
			}
			return t.UTC().Format("2006-01-02T15:04:05.000Z"), true
		}
		rows := make([]row, 0, len(activeD)+len(delD))
		for _, d := range activeD {
			if !mkRow(&d, func(r row) { rows = append(rows, r) }) {
				aPage500(cxt, res)
				return
			}
		}
		for _, d := range delD {
			pv, okp := dg(d, "project")
			if !okp || pv == nil {
				// Node: _formatDeletedProjectInfo on missing .project ->
				// TypeError -> 500 (the query excludes nulls; defensive parity).
				aPage500(cxt, res)
				return
			}
			var recDat, recDid string
			var recDatHas, recDidHas bool
			if ddv, okm := dg(d, "deleterData"); okm && ddv != nil {
				ddd := *dcast(ddv)
				if v, okv := dg(ddd, "deletedAt"); okv && v != nil {
					v2, okT := isoTS(v)
					if !okT {
						aPage500(cxt, res) // Node: toISOString TypeError -> 500
						return
					}
					recDat, recDatHas = v2, true
				}
				if v, okv := dg(ddd, "deleterId"); okv {
					if o, isO := v.(primitive.ObjectID); isO {
						recDid, recDidHas = o.Hex(), true
					}
				}
			}
			if !mkRow(pv, func(r row) {
				r.delHas = true
				r.dat, r.datHas = recDat, recDatHas
				r.did, r.didHas = recDid, recDidHas
				rows = append(rows, r)
			}) {
				aPage500(cxt, res)
				return
			}
		}

		// _applyFilters
		hasActive := nzBool(filters["owned"]) || nzBool(filters["inactive"]) || nzBool(filters["trashed"]) || nzBool(filters["deleted"])
		search, _ := filters["search"].(string)
		if hasActive || search != "" {
			kept := rows[:0]
			for _, r := range rows {
				if nzBool(filters["owned"]) && (r.trashed || r.delHas) {
					continue
				}
				if nzBool(filters["trashed"]) && (!r.trashed || r.delHas) {
					continue
				}
				if nzBool(filters["deleted"]) && !r.delHas {
					continue
				}
				if nzBool(filters["inactive"]) && (r.trashed || r.delHas || !r.inactive) {
					continue
				}
				if search != "" {
					if !r.nameHas {
						aPage500(cxt, res) // project.name.toLowerCase() on undefined
						return
					}
					if !strings.Contains(strings.ToLower(r.name), strings.ToLower(search)) {
						continue
					}
				}
				kept = append(kept, r)
			}
			rows = kept
		}

		// _sortAndPaginate (lodash orderBy: missing values last, both orders;
		// stable) — title uses localeCompare approx.
		if sortBy == "title" {
			sort.SliceStable(rows, func(i, j int) bool {
				ni, nj := rows[i].name, rows[j].name
				if !rows[i].nameHas {
					ni = "\uffff"
				}
				if !rows[j].nameHas {
					nj = "\uffff"
				}
				if c := localeLikeCmp(ni, nj); c != 0 {
					return c < 0 // title is always ascending in Node
				}
				return false
			})
		} else {
			asc := sortOrder == "asc"
			val := func(r row) (string, bool) {
				switch sortBy {
				case "lastUpdated":
					return r.lu, r.luHas
				case "deletedAt":
					return r.dat, r.datHas
				case "owner":
					return r.owner, r.ownerHas
				}
				return "", false
			}
			sort.SliceStable(rows, func(i, j int) bool {
				vi, okI := val(rows[i])
				vj, okJ := val(rows[j])
				if okI != okJ {
					return okI // missing last in both directions
				}
				if !okI {
					return false
				}
				c := strings.Compare(vi, vj)
				if c < 0 {
					return asc
				}
				if c > 0 {
					return !asc
				}
				return false
			})
		}

		var sb strings.Builder
		sb.WriteString(`{"totalSize":` + fmt.Sprint(len(rows)) + `,"projects":[`)
		for i, r := range rows {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(`{"id":` + jstr(r.id) + ``)
			if r.nameHas {
				sb.WriteString(`,"name":` + jstr(r.name))
			} else if r.nameNil {
				sb.WriteString(`,"name":null`)
			}
			if r.ownerHas {
				sb.WriteString(`,"owner":` + jstr(r.owner))
			} else if r.ownerNull {
				sb.WriteString(`,"owner":null`)
			}
			if r.luHas {
				sb.WriteString(`,"lastUpdated":` + jstr(r.lu))
			}
			if r.lubHas {
				sb.WriteString(`,"lastUpdatedBy":` + jstr(r.lub))
			} else if r.lubNil {
				sb.WriteString(`,"lastUpdatedBy":null`)
			}
			sb.WriteString(`,"inactive":` + fmt.Sprint(r.inactive) +
				`,"trashed":` + fmt.Sprint(r.trashed) +
				`,"deleted":` + fmt.Sprint(r.delHas))
			if r.delHas {
				if r.datHas {
					sb.WriteString(`,"deletedAt":` + jstr(r.dat))
				}
				if r.didHas {
					sb.WriteString(`,"deleterId":` + jstr(r.did))
				}
			}
			sb.WriteByte('}')
		}
		sb.WriteString(`]}`)
		res.JSON(200, []byte(sb.String()))
	}
}

func dcast(v any) *primitive.D {
	switch t := v.(type) {
	case *primitive.D:
		return t
	case bson.D:
		d := primitive.D(t)
		return &d
	case bson.M:
		var d primitive.D
		for k, val := range t {
			d = append(d, primitive.E{Key: k, Value: val})
		}
		return &d
	case map[string]any:
		var d primitive.D
		for k, val := range t {
			d = append(d, primitive.E{Key: k, Value: val})
		}
		return &d
	}
	return nil
}

// dg — dget with presence bool.
func dg(d primitive.D, key string) (any, bool) {
	for _, e := range d {
		if e.Key == key {
			return e.Value, true
		}
	}
	return nil, false
}

func asTime(v any) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t, true
	case primitive.DateTime:
		return time.UnixMilli(int64(t)), true
	case int64:
		return time.UnixMilli(t), true
	case int32:
		return time.UnixMilli(int64(t)), true
	case float64:
		return time.UnixMilli(int64(t)), true
	}
	return time.Time{}, false
}

func localeLikeCmp(a, b string) int {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if la != lb {
		if la < lb {
			return -1
		}
		return 1
	}
	// case tie: lowercase before uppercase (CLDR primary weight)
	ra, rb := []rune(a), []rune(b)
	n := len(ra)
	if len(rb) < n {
		n = len(rb)
	}
	for i := 0; i < n; i++ {
		ca, cb := ra[i], rb[i]
		if ca == cb {
			continue
		}
		aLow := ca >= 'a' && ca <= 'z'
		bLow := cb >= 'a' && cb <= 'z'
		if aLow != bLow {
			if aLow {
				return -1
			}
			return 1
		}
		if ca < cb {
			return -1
		}
		return 1
	}
	if len(ra) < len(rb) {
		return -1
	}
	if len(ra) > len(rb) {
		return 1
	}
	return 0
}

// ---------- trash / untrash ----------

func adminTrashOrUntrash(a *core.App, trash bool) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, ok := aLoginAdmin(a, cxt, res); !ok {
			return
		}
		pidP := cxt.Params["1"]
		if !validOID.MatchString(pidP) {
			aPage500(cxt, res)
			return
		}
		pid, _ := primitive.ObjectIDFromHex(strings.ToLower(pidP))

		body, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
		uidHex := ""
		if len(bytes.TrimSpace(body)) > 0 {
			var bm map[string]any
			if json.Unmarshal(body, &bm) == nil {
				if s, okm := bm["userId"].(string); okm && s != "" && validOID.MatchString(s) {
					uidHex = strings.ToLower(s)
				}
			}
		}
		if uidHex == "" {
			// resolveProjectUserId fallback: the project's own owner.
			db, dbErr := a.Mongo.DB(cxt.Req.Context())
			var doc primitive.D
			okDoc := false
			if dbErr == nil {
				var d primitive.D
				if db.Collection("projects").FindOne(cxt.Req.Context(), bson.D{{Key: "_id", Value: pid}}).Decode(&d) == nil {
					doc = d
					okDoc = true
				}
			}
			if okDoc {
				if v, okv := dg(doc, "owner_ref"); okv && v != nil {
					if o, isO := v.(primitive.ObjectID); isO {
						uidHex = o.Hex()
					} else if s, isS := v.(string); isS {
						uidHex = s
					}
				}
				if uidHex == "" {
					if ov, okv := dg(doc, "owner"); okv && ov != nil {
						if dd2 := dcast(ov); dd2 != nil {
							if id, oki := dg(*dd2, "_id"); oki {
								if o, isO := id.(primitive.ObjectID); isO {
									uidHex = o.Hex()
								}
							}
						}
					}
				}
			}
		}
		if !validOID.MatchString(uidHex) {
			aPage500(cxt, res) // OError / BSONError
			return
		}
		uid, _ := primitive.ObjectIDFromHex(strings.ToLower(uidHex))
		db, dbErr := a.Mongo.DB(cxt.Req.Context())
		if dbErr != nil {
			aPage500(cxt, res)
			return
		}
		if trash {
			_, _ = db.Collection("projects").UpdateOne(cxt.Req.Context(),
				bson.D{{Key: "_id", Value: pid}},
				bson.D{{Key: "$addToSet", Value: bson.D{{Key: "trashed", Value: uid}}},
					{Key: "$pull", Value: bson.D{{Key: "archived", Value: uid}}}})
		} else {
			_, _ = db.Collection("projects").UpdateOne(cxt.Req.Context(),
				bson.D{{Key: "_id", Value: pid}},
				bson.D{{Key: "$pull", Value: bson.D{{Key: "trashed", Value: uid}}}})
		}
		res.PlainText(200, "OK")
	}
}

// ---------- soft delete ----------

func adminDelete(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		uid, doc, ok := aGate500(a, cxt, res)
		if !ok {
			return
		}
		oid := mustObjectID(strings.ToLower(cxt.Params["1"]))
		// Node admin options = {deleterUser} only: no deleterIpAddress, no
		// deletedReason — pass empty strings to omit both from deleterData.
		deleteProjectExec(a, cxt, oid, *doc, uid, "", "")
		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 5*time.Second)
		defer cancel()
		db, dbErr := a.Mongo.DB(ctx)
		if dbErr != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		var dpDoc primitive.D
		if db.Collection("deletedProjects").FindOne(ctx,
			bson.D{{Key: "deleterData.deletedProjectId", Value: oid}}).Decode(&dpDoc) != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		dat, did := "null", "null"
		if v, okv := dget2get(dpDoc, "deleterData"); okv && v != nil {
			if dd := dcast(v); dd != nil {
				if t, okT := asTime(dget2raw(*dd, "deletedAt")); okT {
					dat = jstr(t.UTC().Format("2006-01-02T15:04:05.000Z"))
				}
				if o, isO := dget2raw(*dd, "deleterId").(primitive.ObjectID); isO {
					did = jstr(o.Hex())
				}
			}
		}
		res.JSON(200, []byte(`{"deletedAt":`+dat+`,"deleterId":`+did+`}`))
	}
}

func dget2(v any) *primitive.ObjectID {
	if o, ok := v.(primitive.ObjectID); ok {
		return &o
	}
	return nil
}

func dget2get(d primitive.D, key string) (any, bool) {
	return dg(d, key)
}

func dget2raw(d primitive.D, key string) any {
	return dget(d, key)
}

// ---------- undelete ----------

func adminUndelete(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, ok := aLoginAdmin(a, cxt, res); !ok {
			return
		}
		pidP := cxt.Params["1"]
		if !validOID.MatchString(pidP) {
			aPage500(cxt, res)
			return
		}
		pid, _ := primitive.ObjectIDFromHex(strings.ToLower(pidP))

		body, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
		uidHex := ""
		if len(bytes.TrimSpace(body)) > 0 {
			var bm map[string]any
			if json.Unmarshal(body, &bm) == nil {
				if s, okm := bm["userId"].(string); okm && s != "" && validOID.MatchString(s) {
					uidHex = strings.ToLower(s)
				}
			}
		}
		ctx := cxt.Req.Context()
		db, dbErr := a.Mongo.DB(ctx)
		if dbErr != nil {
			aPage500(cxt, res)
			return
		}
		var dpDoc primitive.D
		if db.Collection("deletedProjects").FindOne(ctx,
			bson.D{{Key: "deleterData.deletedProjectId", Value: pid}}).Decode(&dpDoc) != nil {
			aNotFound(cxt, res)
			return
		}
		pv, okP := dg(dpDoc, "project")
		if !okP || pv == nil {
			aNotFound(cxt, res)
			return
		}
		if uidHex == "" {
			aPage500(cxt, res) // resolveProjectUserId -> OError (project gone)
			return
		}
		uid, _ := primitive.ObjectIDFromHex(uidHex)

		origOwnerHex := ""
		if dv, okv := dg(dpDoc, "deleterData"); okv && dv != nil {
			if dd := dcast(dv); dd != nil {
				if o, isO := dget(*dd, "deletedProjectOwnerId").(primitive.ObjectID); isO {
					origOwnerHex = o.Hex()
				}
			}
		}
		saved := *dcast(pv)
		savedName := ""
		if v, okv := dg(saved, "name"); okv {
			if s, isS := v.(string); isS {
				savedName = s
			}
		}
		target := savedName + " (Restored)"
		nameSet := adNameSet(a, cxt, origOwnerHex)
		finalName, okN := adEnsureUnique(nameSet, target)
		if !okN || finalName == "" {
			aPage500(cxt, res)
			return
		}

		// restored = {…saved} minus archived; owner_ref = uid; name = finalName;
		// deletedDocs = [] (Node resets; docstore deleteDoc side-effects).
		restored := bson.D{{Key: "_id", Value: saved[0].Value}}
		deletedDocs := []any{}
		for i := 1; i < len(saved); i++ {
			el := saved[i]
			switch el.Key {
			case "archived":
				continue
			case "owner_ref":
				restored = append(restored, bson.E{Key: "owner_ref", Value: uid})
			case "name":
				restored = append(restored, bson.E{Key: "name", Value: finalName})
			case "deletedDocs":
				if arr, isA := el.Value.(primitive.A); isA {
					deletedDocs = arr
				}
				restored = append(restored, bson.E{Key: "deletedDocs", Value: primitive.A{}})
			default:
				restored = append(restored, el)
			}
		}
		if _, insErr := db.Collection("projects").InsertOne(ctx, restored); insErr != nil {
			aPage500(cxt, res)
			return
		}
		// Node side effect: docstore deleteDoc for each previously-deleted doc
		// (best-effort service call; Node awaits, but failure -> 500).
		for _, ddv := range deletedDocs {
			if dd := dcast(ddv); dd != nil {
				did := dget2raw(*dd, "_id")
				dnm := dget2raw(*dd, "name")
				f := strings.TrimSuffix(crDocstoreBase(), "/")
				fireHTTP(cxt, "DELETE", f+"/project/"+pid.Hex()+"/doc/"+asStr(did)+"/name/"+asStr(dnm), nil)
			}
		}
		dpID, _ := dpDoc[0].Value.(primitive.ObjectID)
		_, _ = db.Collection("deletedProjects").DeleteOne(ctx, bson.D{{Key: "_id", Value: dpID}})
		_, _ = db.Collection("projects").UpdateOne(ctx,
			bson.D{{Key: "_id", Value: pid}},
			bson.D{{Key: "$pull", Value: bson.D{{Key: "trashed", Value: uid}}}})
		res.JSON(200, []byte(`{"name":`+jstr(finalName)+`}`))
	}
}

// ---------- purge ----------

func adminPurge(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, ok := aLoginAdmin(a, cxt, res); !ok {
			return
		}
		pidP := cxt.Params["1"]
		if !validOID.MatchString(pidP) {
			aPage500(cxt, res)
			return
		}
		pid, _ := primitive.ObjectIDFromHex(strings.ToLower(pidP))
		ctx := cxt.Req.Context()
		db, dbErr := a.Mongo.DB(ctx)
		if dbErr != nil {
			aPage500(cxt, res)
			return
		}
		var act primitive.D
		if db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: pid}}).Decode(&act) == nil {
			_, _ = db.Collection("deletedProjects").DeleteOne(ctx,
				bson.D{{Key: "deleterData.deletedProjectId", Value: pid}})
			res.PlainText(200, "OK")
			return
		}
		var dpDoc primitive.D
		if db.Collection("deletedProjects").FindOne(ctx,
			bson.D{{Key: "deleterData.deletedProjectId", Value: pid}}).Decode(&dpDoc) != nil {
			aNotFound(cxt, res)
			return
		}
		pv, okP := dg(dpDoc, "project")
		if !okP || pv == nil {
			res.PlainText(200, "OK") // already expired
			return
		}
		saved := *dcast(pv)
		pidHex := pid.Hex()

		if !fireHTTP(cxt, "POST", strings.TrimSuffix(crDocstoreBase(), "/")+"/project/"+pidHex+"/destroy", nil) {
			aPage500(cxt, res)
			return
		}
		if !fireHTTP(cxt, "DELETE", strings.TrimSuffix(crHistoryBase(), "/")+"/project/"+pidHex, nil) {
			aPage500(cxt, res)
			return
		}
		if ov, ovOK := dg(saved, "overleaf"); ovOK && ov != nil {
			if dd := dcast(ov); dd != nil {
				if hv, hvOK := dg(*dd, "history"); hvOK && hv != nil {
					if hdd := dcast(hv); hdd != nil {
						if hid, isS := dget(*hdd, "id").(string); isS && hid != "" {
							u, p := "staging", os.Getenv("STAGING_PASSWORD")
							if !fireBasic(cxt, "DELETE", strings.TrimSuffix(os.Getenv("V1_HISTORY_URL"), "/")+"/projects/"+hid, "staging", p) {
								aPage500(cxt, res)
								return
							}
							_ = u
						}
					}
				}
			}
		}
		chatBase := os.Getenv("WEB_CHAT_URL")
		if chatBase == "" {
			chatBase = "http://127.0.0.1:3010"
		}
		if !fireHTTP(cxt, "DELETE", strings.TrimSuffix(chatBase, "/")+"/project/"+pidHex, nil) {
			aPage500(cxt, res)
			return
		}
		_, _ = db.Collection("projectAuditLogEntries").DeleteMany(ctx, bson.D{{Key: "projectId", Value: pid}})

		dpID, _ := dpDoc[0].Value.(primitive.ObjectID)
		if _, uerr := db.Collection("deletedProjects").UpdateOne(ctx,
			bson.D{{Key: "_id", Value: dpID}},
			bson.D{{Key: "$set", Value: bson.D{
				{Key: "deleterData.deleterIpAddress", Value: nil},
				{Key: "project", Value: nil},
			}}}); uerr != nil {
			aPage500(cxt, res)
			return
		}
		res.PlainText(200, "OK")
	}
}

// fireHTTP with Basic auth (for v1 history).
func fireBasic(cxt *core.Cxt, method, url, user, pass string) bool {
	req, err := http.NewRequestWithContext(cxt.Req.Context(), method, url, nil)
	if err != nil {
		return false
	}
	req.SetBasicAuth(user, pass)
	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, rsp.Body)
	rsp.Body.Close()
	return rsp.StatusCode < 300
}

// ---------- members ----------

func adminMembers(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		_, doc, ok := aGate500(a, cxt, res)
		if !ok {
			return
		}
		ownerJSON := "null"
		if or, okm := dg(*doc, "owner_ref"); okm {
			if o, isO := or.(primitive.ObjectID); isO {
				var u primitive.D
				db, dbErr := a.Mongo.DB(cxt.Req.Context())
				if dbErr == nil && db.Collection("users").FindOne(cxt.Req.Context(),
					bson.D{{Key: "_id", Value: o}}).Decode(&u) == nil {
					su := "null"
					if t, okT := asTime(dget(u, "signUpDate")); okT {
						su = jstr(t.UTC().Format("2006-01-02T15:04:05.000Z"))
					}
					ownerJSON = `{"_id":` + jstr(o.Hex()) +
						`,"first_name":` + jstr(asStr(dget(u, "first_name"))) +
						`,"last_name":` + jstr(asStr(dget(u, "last_name"))) +
						`,"email":` + jstr(asStr(dget(u, "email"))) +
						`,"privileges":"owner","signUpDate":` + su + `}`
				}
			}
		}

		rows := invitedMemberRows(doc)
		users := loadUsers(a, cxt, rows)
		var sb strings.Builder
		sb.WriteString(`{"owner":` + ownerJSON + `,"members":[`)
		first := true
		for _, r := range rows {
			u, okm := users[r.uid]
			if !okm {
				continue
			}
			if !first {
				sb.WriteByte(',')
			}
			first = false
			sb.WriteString(`{"_id":` + jstr(r.uid) +
				`,"first_name":` + jstr(asStr(dget(u, "first_name"))) +
				`,"last_name":` + jstr(asStr(dget(u, "last_name"))) +
				`,"email":` + jstr(asStr(dget(u, "email"))) +
				`,"privileges":` + jstr(r.priv) + `,"signUpDate":`)
			if iso, oki := signUpDateISO(dget(u, "signUpDate")); oki {
				sb.WriteString(jstr(iso))
			} else {
				sb.WriteString(`null`)
			}
			if r.pendingEditor {
				sb.WriteString(`,"pendingEditor":true`)
			}
			if r.pendingReviewer {
				sb.WriteString(`,"pendingReviewer":true`)
			}
			sb.WriteByte('}')
		}
		sb.WriteString(`]}`)
		res.JSON(200, []byte(sb.String()))
	}
}

// ---------- sharing link (AccessTokenEncryptor aes-256-ctr / HKDF-SHA512) ----------

const atLabel = "2026.3-v3"

func atKey(salt []byte) ([]byte, error) {
	secret := os.Getenv("OVERLEAF_INVITE_TOKEN_SECRET")
	if secret == "" {
		return nil, fmt.Errorf("no secret")
	}
	h := hkdf.New(sha512.New, []byte(secret), salt, []byte(""))
	key := make([]byte, 32)
	if _, err := io.ReadFull(h, key); err != nil {
		return nil, err
	}
	return key, nil
}

func atEncrypt(plain []byte) (string, error) {
	salt := make([]byte, 16)
	iv := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	key, err := atKey(salt)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	out := make([]byte, len(plain))
	cipher.NewCTR(block, iv).XORKeyStream(out, plain)
	return atLabel + ":" + hex.EncodeToString(salt) + ":" +
		base64.StdEncoding.EncodeToString(out) + ":" + hex.EncodeToString(iv), nil
}

func atDecrypt(enc string) ([]byte, bool) {
	parts := strings.SplitN(enc, ":", 4)
	if len(parts) != 4 || parts[0] != atLabel {
		return nil, false
	}
	salt, err := hex.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	ct, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, false
	}
	iv, err := hex.DecodeString(parts[3])
	if err != nil {
		return nil, false
	}
	key, err := atKey(salt)
	if err != nil {
		return nil, false
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, false
	}
	out := make([]byte, len(ct))
	cipher.NewCTR(block, iv).XORKeyStream(out, ct)
	return out, true
}

func adminShareGet(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, _, ok := aGateParam(a, cxt, res); !ok {
			return
		}
		projID := mustObjectID(strings.ToLower(cxt.Params["1"]))
		db, dbErr := a.Mongo.DB(cxt.Req.Context())
		if dbErr != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		var inv primitive.D
		if db.Collection("projectInvites").FindOne(cxt.Req.Context(),
			bson.D{{Key: "projectId", Value: projID}, {Key: "reusable", Value: true}}).Decode(&inv) != nil {
			res.SendStatus(404)
			return
		}
		encV, okE := dg(inv, "encryptedToken")
		enc, isE := encV.(string)
		if !okE || !isE || enc == "" {
			res.SendStatus(404)
			return
		}
		plain, okD := atDecrypt(enc)
		if !okD {
			aPage500(cxt, res)
			return
		}
		var token string
		if json.Unmarshal(plain, &token) != nil {
			aPage500(cxt, res)
			return
		}
		res.JSON(200, shareInvJSON(inv))
	}
}

func shareInvJSON(inv primitive.D) []byte {
	var sb strings.Builder
	id, _ := inv[0].Value.(primitive.ObjectID)
	enc, _ := dget(inv, "encryptedToken").(string)
	plain, okD := atDecrypt(enc)
	token := ""
	if okD {
		json.Unmarshal(plain, &token)
	}
	sb.WriteString(`{"_id":` + jstr(id.Hex()) + `,"token":` + jstr(token) + `,"privileges":`)
	pv, okP := dg(inv, "privileges")
	switch v := pv.(type) {
	case bool:
		sb.WriteString(fmt.Sprint(v))
	case string:
		sb.WriteString(jstr(v))
	default:
		if !okP {
			sb.WriteString(`null`)
		} else {
			sb.WriteString(`null`)
		}
	}
	if sub, okS := dg(inv, "subscriptionId"); okS {
		if o, isO := sub.(primitive.ObjectID); isO {
			sb.WriteString(`,"subscriptionId":` + jstr(o.Hex()))
		} else if s, isS := sub.(string); isS {
			sb.WriteString(`,"subscriptionId":` + jstr(s))
		}
	}
	sb.WriteString(`}`)
	return []byte(sb.String())
}

func adminShareSet(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, _, ok := aGateParam(a, cxt, res); !ok {
			return
		}
		projID, _ := primitive.ObjectIDFromHex(strings.ToLower(cxt.Params["1"]))
		body, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
		var bm map[string]any
		var keyOrder []string
		if len(bytes.TrimSpace(body)) > 0 {
			if e := json.Unmarshal(body, &bm); e != nil || bm == nil {
				res.JSON(400, []byte("{}"))
				return
			}
			keyOrder = bodyKeyOrder(body)
		} else {
			bm = map[string]any{}
		}
		// privileges: false | readOnly | readAndWrite | review (required)
		privOK := false
		privVal := any(nil)
		if v, hasP := bm["privileges"]; hasP {
			switch t := v.(type) {
			case bool:
				if t == false {
					privOK, privVal = true, false
				}
			case string:
				if t == "readOnly" || t == "readAndWrite" || t == "review" {
					privOK, privVal = true, t
				}
			}
		}
		subVal := any(nil)
		if v, hasS := bm["subscriptionId"]; hasS {
			if s, isS := v.(string); isS && s != "" && validOID.MatchString(s) {
				subVal = strings.ToLower(s)
			}
		}
		var unknown []string
		for _, k := range keyOrder {
			if k != "privileges" && k != "subscriptionId" {
				unknown = append(unknown, k)
			}
		}
		var segs []string
		if !privOK {
			segs = append(segs, `Invalid input: expected false at "body.privileges" or `+
				`Invalid option: expected one of "readOnly"|"readAndWrite"|"review" at "body.privileges"`)
		}
		if v, hasS := bm["subscriptionId"]; hasS {
			if s, isS := v.(string); !isS {
				segs = append(segs, `Invalid input: expected string, received `+zodRec(v)+" at \"body.subscriptionId\"")
			} else if s != "" && !validOID.MatchString(s) {
				segs = append(segs, `Invalid input: invalid Mongo ObjectId at "body.subscriptionId"`)
			}
		}
		if len(unknown) > 0 {
			plural := "key"
			if len(unknown) > 1 {
				plural = "keys"
			}
			qs := make([]string, 0, len(unknown))
			for _, k := range unknown {
				qs = append(qs, `"`+k+`"`)
			}
			segs = append(segs, "Unrecognized "+plural+": "+strings.Join(qs, ", ")+` at "body"`)
		}
		if len(segs) > 0 {
			res.JSON(400, []byte(`{"error":"Validation error: `+strings.Join(segs, "; ")+`","statusCode":400}`))
			return
		}

		ctx := cxt.Req.Context()
		db, dbErr := a.Mongo.DB(ctx)
		if dbErr != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		var inv primitive.D
		err := db.Collection("projectInvites").FindOne(ctx,
			bson.D{{Key: "projectId", Value: projID}, {Key: "reusable", Value: true}}).Decode(&inv)
		if err != nil {
			token := invRandToken()
			enc, e2 := atEncrypt([]byte(`"` + token + `"`))
			if e2 != nil {
				aPage500(cxt, res)
				return
			}
			doc := bson.D{
				{Key: "encryptedToken", Value: enc},
				{Key: "tokenHmac", Value: invTokenHMAC(token)},
				{Key: "projectId", Value: projID},
				{Key: "privileges", Value: privVal},
			}
			if s, isS := subVal.(string); isS {
				sub, _ := primitive.ObjectIDFromHex(s)
				doc = append(doc, bson.E{Key: "subscriptionId", Value: sub})
			}
			doc = append(doc, bson.E{Key: "reusable", Value: true}, bson.E{Key: "expires", Value: nil})
			if _, e3 := db.Collection("projectInvites").InsertOne(ctx, doc); e3 != nil {
				aPage500(cxt, res)
				return
			}
			if e4 := db.Collection("projectInvites").FindOne(ctx,
				bson.D{{Key: "projectId", Value: projID}, {Key: "reusable", Value: true}}).Decode(&inv); e4 != nil {
				aPage500(cxt, res)
				return
			}
		} else {
			setD := bson.D{{Key: "privileges", Value: privVal}}
			switch sv := subVal.(type) {
			case string:
				sub, _ := primitive.ObjectIDFromHex(sv)
				setD = append(setD, bson.E{Key: "subscriptionId", Value: sub})
			}
			id, _ := inv[0].Value.(primitive.ObjectID)
			if _, e5 := db.Collection("projectInvites").UpdateOne(ctx,
				bson.D{{Key: "_id", Value: id}},
				bson.D{{Key: "$set", Value: setD}}); e5 != nil {
				aPage500(cxt, res)
				return
			}
			// re-read for the response
			if e6 := db.Collection("projectInvites").FindOne(ctx,
				bson.D{{Key: "projectId", Value: projID}, {Key: "reusable", Value: true}}).Decode(&inv); e6 != nil {
				aPage500(cxt, res)
				return
			}
		}
		res.JSON(200, shareInvJSON(inv))
	}
}

func zodRec(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64, int, int64:
		return "number"
	case []any:
		return "array"
	case nil:
		return "null"
	default:
		return "object"
	}
}

// bodyKeyOrder — JSON object key order (Node strict-unknowns ordering).
func bodyKeyOrder(body []byte) []string {
	dec := json.NewDecoder(bytes.NewReader(body))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil
	}
	var out []string
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			break
		}
		if ks, ok := kt.(string); ok {
			out = append(out, ks)
			// skip value
			var skip any
			if err := dec.Decode(&skip); err != nil {
				// nested value not consumed; try raw
				var raw json.RawMessage
				if err2 := dec.Decode(&raw); err2 != nil {
					break
				}
			}
		}
	}
	return out
}

// ---------- feature ----------

func AdminFeature(a *core.App) core.Feature {
	return core.Feature{
		Name: "p62-admin-tools",
		Routes: []core.Route{
			{Method: "GET", Pattern: adUserRedirPat, Handler: adminRedir(a, "/hub#/site.general.users.all")},
			{Method: "GET", Pattern: adProjectRedPat, Handler: adminRedir(a, "/hub#/site.general.projects.all")},
			{Method: "GET", Pattern: activeProjsPat, Handler: adminActiveProjects(a)},
			{Method: "POST", Pattern: adUserListPat, Handler: adminUserProjects(a)},
			{Method: "POST", Pattern: adTrashPat, Handler: adminTrashOrUntrash(a, true)},
			{Method: "POST", Pattern: adUntrashPat, Handler: adminTrashOrUntrash(a, false)},
			{Method: "DELETE", Pattern: adPurgePat, Handler: adminPurge(a)},
			{Method: "POST", Pattern: adUndeletePat, Handler: adminUndelete(a)},
			{Method: "DELETE", Pattern: adDeletePat, Handler: adminDelete(a)},
			{Method: "GET", Pattern: adMembersPat, Handler: adminMembers(a)},
			{Method: "GET", Pattern: adInvitesPat, Handler: inviteListHandler(a, aGateAdmin)},
			{Method: "POST", Pattern: adInvitePat, Handler: adGateValidate(a, adValidateInvite, inviteCreateHandler(a, aGateAdmin))},
			{Method: "PUT", Pattern: adUsersPat, Handler: adGateValidate(a, adValidateUser, setUserLevelHandler(a, aGateAdmin))},
			{Method: "DELETE", Pattern: adUsersPat, Handler: removeUserHandler(a, aGateAdmin)},
			{Method: "DELETE", Pattern: adInvRevokePat, Handler: inviteRevokeHandler(a, aGateAdmin)},
			{Method: "POST", Pattern: adInvResendPat, Handler: inviteResendHandler(a, aGateAdmin)},
			{Method: "GET", Pattern: adShareLinkPat, Handler: adminShareGet(a)},
			{Method: "POST", Pattern: adShareLinkPat, Handler: adminShareSet(a)},
		},
	}
}
