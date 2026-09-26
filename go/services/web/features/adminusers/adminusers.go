// P6.3a — admin-tools (services/web/modules/admin-tools) user surface,
// read side. Node oracle pins (/tmp/p63a_node.json) are the authority;
// Node router: modules/admin-tools/app/src/AdminToolsRouter.mjs; logic in
// modules/admin-tools/app/src/UserListController.mjs.
//
//	POST   /admin/users               -> _getUsers (active + deleted rows,
//	                                   filters, sort; `page` is a Node
//	                                   no-op — the full filtered list is
//	                                   always returned)
//	GET    /admin/user/:userId/info   -> {activationLink, canManageTemplates}
//
// Node contracts reproduced exactly (each pinned):
//   - gate: global chain (anon GET -> 302 /login; anon non-GET -> 403
//     'Forbidden'; member non-GET token-less -> 403) then
//     ensureUserIsSiteAdmin (member -> 302 /restricted?from=...,
//     Accept-negotiated body via core.Res.Redirect).
//   - row field order + JSON.stringify undefined-omit semantics (a user
//     with a missing last_name / isAdmin / signUpDate drops the key,
//     while canManageTemplates/inactive/deleted/authMethods/allow* are
//     always present); dates are ISO-ms UTC.
//   - search filter short-circuit quirk: exclusion requires
//     email-no-match AND firstName-no-match AND lastName-no-match; a
//     MISSING first/last name evaluates the clause to undefined (not -1)
//     and therefore CANNOT be excluded (pinned: the 28 no-lastName users
//     pass ANY search; a NULL name throws -> 500 page).
//   - sort: by 'name' -> stable (lastName,firstName,email) case-insensitive
//     with missing->'\uffff', ALWAYS ascending (Node ignores sort.order);
//     else lodash orderBy on the key with undefined values LAST in asc /
//     FIRST in desc (pinned sequences), strings lowercased; bad by/order
//     -> OError 500 HTML page.
//   - inactive = !lastActive || lastActive < (now - 1 calendar year).
//   - info: activationLink from a live password token (use/password,
//     data.user_id, expiresAt > now, usedAt absent, peekCount < MAX_PEEKS=4)
//     else null; canManageTemplates = user flags (missing user / bad id ->
//     false, both pinned 200).
//   - urlencoded bodies: qs bracket nesting (filters[admin]=true) + string
//     truthiness.
package adminusers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

var hex24 = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// ---------- pages ----------

func pageBase(cxt *core.Cxt, reqPath string) views.PageData {
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

// ---------- gate ----------

func gate(a *core.App, cxt *core.Cxt, res *core.Res) (string, bool) {
	uid := ""
	if cxt.Sess != nil {
		uid = cxt.Sess.UserIDHex()
	}
	if uid == "" {
		// the core global login gate normally bounces anonymous callers
		// first; this keeps the feature safe in isolation (P6.2 shape).
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

// ---------- row model ----------

type fkind int

const (
	fStr fkind = iota
	fBool
	fInt
	fDate
)

// fld — a decoded user field with Node JSON.stringify presence semantics:
// missing = key omitted; null = "key":null.
type fld struct {
	missing bool
	null    bool
	kind    fkind
	s       string
	b       bool
	n       int64
}

// urow — one formatted user (Node `_formatUserInfo` order).
type urow struct {
	id           string
	email        fld
	firstName    fld
	lastName     fld
	isAdmin      fld
	canMgmtTmpl  bool
	loginCount   fld
	signUpDate   fld
	lastActive   fld
	lastLoggedIn fld
	authMethods  []string
	allowUpdDet  bool
	allowUpdAdm  bool
	suspended    any
	inactive     bool
	deletedAt    fld
	deleted      bool
}

func fstr(s string) fld { return fld{kind: fStr, s: s} }
func fnull() fld        { return fld{null: true} }
func fmiss() fld        { return fld{missing: true} }
func fbool(b bool) fld  { return fld{kind: fBool, b: b} }
func fint(n int64) fld  { return fld{kind: fInt, n: n} }
func fdate(ms int64) fld {
	return fld{kind: fDate, n: ms}
}

func asInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case int64:
		return t, true
	case int32:
		return int64(t), true
	case int:
		return int64(t), true
	case float64:
		return int64(t), true
	}
	return 0, false
}

func asDateMs(v any) (int64, bool) {
	switch t := v.(type) {
	case time.Time:
		return t.UnixMilli(), true
	case primitive.DateTime:
		return int64(t), true
	case int64:
		return t, true
	case int32:
		return int64(t), true
	}
	return 0, false
}

func isoDate(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z")
}

// esc — Node JSON.stringify string escaping (quotes, backslashes and
// control characters only — no HTML escaping, unlike encoding/json).
func esc(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20:
			switch r {
			case '\b':
				b.WriteString(`\b`)
			case '\f':
				b.WriteString(`\f`)
			case '\n':
				b.WriteString(`\n`)
			case '\r':
				b.WriteString(`\r`)
			case '\t':
				b.WriteString(`\t`)
			default:
				fmt.Fprintf(&b, `\u%04x`, r)
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func jsonStr(s string) string { return `"` + esc(s) + `"` }
func jsonBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
func jsonInt(n int64) string { return strconv.FormatInt(n, 10) }
func jsonArr(ss []string) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, s := range ss {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(jsonStr(s))
	}
	b.WriteByte(']')
	return b.String()
}

func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case float64:
		return t != 0
	case int:
		return t != 0
	case int64:
		return t != 0
	}
	return true
}

// buildRow — Node `_formatUserInfo` for one user doc (active doc, or a
// deletedUsers snapshot doc), plus auth-method / allow-update / inactive
// derivations. yearAgo = now - 1 calendar year (Node setFullYear(y-1)).
func buildRow(d primitive.D, deleted bool, recDeletedAt any) *urow {
	now := time.Now().UTC()
	yearAgo := now.AddDate(-1, 0, 0).UnixMilli()

	get := func(k string) (any, bool) {
		for _, e := range d {
			if e.Key == k {
				return e.Value, true
			}
		}
		return nil, false
	}

	idHex := ""
	if v, ok := get("_id"); ok {
		if oc, ok := v.(primitive.ObjectID); ok {
			idHex = oc.Hex()
		} else if s, ok := v.(string); ok && hex24.MatchString(s) {
			idHex = strings.ToLower(s)
		}
	}
	strF := func(k string) fld {
		v, ok := get(k)
		if !ok {
			return fmiss()
		}
		if v == nil {
			return fnull()
		}
		if s, ok := v.(string); ok {
			return fstr(s)
		}
		js, _ := json.Marshal(v) // non-string stored (data-clean; defensive)
		return fstr(string(js))
	}
	boolF := func(k string) fld {
		v, ok := get(k)
		if !ok || v == nil {
			return fmiss()
		}
		if b, ok := v.(bool); ok {
			return fbool(b)
		}
		return fmiss()
	}
	intF := func(k string) fld {
		v, ok := get(k)
		if !ok || v == nil {
			return fmiss()
		}
		if n, ok := asInt64(v); ok {
			return fint(n)
		}
		return fmiss()
	}
	dateF := func(k string) fld {
		v, ok := get(k)
		if !ok || v == nil {
			return fmiss()
		}
		if ms, ok := asDateMs(v); ok {
			return fdate(ms)
		}
		return fmiss()
	}

	r := &urow{
		id:           idHex,
		email:        strF("email"),
		firstName:    strF("first_name"),
		lastName:     strF("last_name"),
		isAdmin:      boolF("isAdmin"),
		loginCount:   intF("loginCount"),
		signUpDate:   dateF("signUpDate"),
		lastActive:   dateF("lastActive"),
		lastLoggedIn: dateF("lastLoggedIn"),
	}
	if v, ok := get("flags"); ok {
		if fd, ok := v.(primitive.D); ok {
			for _, e := range fd {
				if e.Key == "canManageTemplates" {
					if b, ok := e.Value.(bool); ok {
						r.canMgmtTmpl = b
					}
					break
				}
			}
		}
	}
	if v, ok := get("suspended"); ok {
		r.suspended = v
	}
	if v, ok := get("hashedPassword"); ok && truthy(v) {
		r.authMethods = []string{"local"}
	}
	// availableAuthMethods here is ['local'] (EXTERNAL_AUTH unset) and the
	// local auth block sets neither updateUserDetailsOnLogin nor
	// attAdmin/valAdmin — both allow* flags reduce to true for every row
	// (pinned across the oracle battery).
	r.allowUpdDet = true
	r.allowUpdAdm = true
	// Node: inactive = !user.lastActive || user.lastActive < yearAgo
	if r.lastActive.missing || r.lastActive.n < yearAgo {
		r.inactive = true
	}
	if deleted {
		if ms, ok := asDateMs(recDeletedAt); ok {
			r.deletedAt = fdate(ms)
			r.deleted = true
		}
	}
	return r
}

// rowJSON — the exact Node `_formatUserInfo` key order.
func (r *urow) json() string {
	var b strings.Builder
	b.WriteByte('{')
	first := true
	key := func(k string, present bool, val string) {
		if !present {
			return
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.WriteString(`"` + k + `":`)
		b.WriteString(val)
	}
	val := func(f fld) string {
		if f.null {
			return "null"
		}
		switch f.kind {
		case fStr:
			return jsonStr(f.s)
		case fBool:
			return jsonBool(f.b)
		case fInt:
			return jsonInt(f.n)
		case fDate:
			return jsonStr(isoDate(f.n))
		}
		return "null"
	}
	key("id", true, jsonStr(r.id))
	key("email", !r.email.missing, val(r.email))
	key("firstName", !r.firstName.missing, val(r.firstName))
	key("lastName", !r.lastName.missing, val(r.lastName))
	key("isAdmin", !r.isAdmin.missing, val(r.isAdmin))
	key("canManageTemplates", true, jsonBool(r.canMgmtTmpl))
	key("loginCount", !r.loginCount.missing, val(r.loginCount))
	key("signUpDate", !r.signUpDate.missing, val(r.signUpDate))
	key("lastActive", !r.lastActive.missing, val(r.lastActive))
	key("lastLoggedIn", !r.lastLoggedIn.missing, val(r.lastLoggedIn))
	key("authMethods", true, jsonArr(r.authMethods))
	key("allowUpdateDetails", true, jsonBool(r.allowUpdDet))
	key("allowUpdateIsAdmin", true, jsonBool(r.allowUpdAdm))
	if truthy(r.suspended) {
		if sb, ok := r.suspended.(bool); ok {
			key("suspended", true, jsonBool(sb))
		} else if ss, ok := r.suspended.(string); ok {
			key("suspended", true, jsonStr(ss))
		}
	}
	key("inactive", true, jsonBool(r.inactive))
	key("deletedAt", !r.deletedAt.missing && !r.deletedAt.null, val(r.deletedAt))
	key("deleted", true, jsonBool(r.deleted))
	b.WriteByte('}')
	return b.String()
}

// ---------- filters ----------

type filters struct {
	deleted, all, admin, inactive, suspended, local, saml, oidc, ldap any
	search                                                            any
}

func (f *filters) anyActive() bool {
	return truthy(f.deleted) || truthy(f.all) || truthy(f.admin) ||
		truthy(f.inactive) || truthy(f.suspended) || truthy(f.local) ||
		truthy(f.saml) || truthy(f.oidc) || truthy(f.ldap) ||
		(isString(f.search) && f.search.(string) != "")
}

func isString(v any) bool { _, ok := v.(string); return ok }

// matchFilters — Node `_matchesFilters` with the exact short-circuit
// search semantics. Second return: true = Node would have thrown
// (TypeError on a null name/email) -> 500 page.
func (r *urow) matchFilters(f *filters) (bool, bool) {
	if !f.anyActive() {
		return true, false
	}
	if s, ok := f.search.(string); ok && s != "" {
		need := strings.ToLower(s)
		// user.email.toLowerCase() — missing/null email -> TypeError.
		if r.email.missing || r.email.null {
			return false, true
		}
		em := strings.Contains(strings.ToLower(r.email.s), need)
		if !em {
			fm, ferr := searchPart(r.firstName, need)
			if ferr {
				return false, true
			}
			if !fm {
				ln, lerr := searchPart(r.lastName, need)
				if lerr {
					return false, true
				}
				if !ln { // email, first, last all no-match -> excluded
					return false, false
				}
			}
		}
	}
	if r.deleted {
		return truthy(f.deleted), false
	}
	if truthy(f.all) {
		return true, false
	}
	if truthy(f.admin) {
		if r.isAdmin.missing || r.isAdmin.null {
			return false, false
		}
		return r.isAdmin.b, false
	}
	if truthy(f.inactive) && !r.inactive {
		return false, false
	}
	if truthy(f.suspended) && !truthy(r.suspended) {
		return false, false
	}
	if truthy(f.local) && !hasAuth(r.authMethods, "local") {
		return false, false
	}
	// saml/oidc/ldap filters are inert here (availableAuthMethods = the
	// ['local'] loop only), exactly like Node with EXTERNAL_AUTH unset.
	return true, false
}

// searchPart — `user.first?.toLowerCase().indexOf(x) === -1`: a missing
// name makes the whole clause undefined (NOT -1), so the user CANNOT be
// excluded (matched=true); a NULL name throws (err=true); present ->
// case-insensitive contains.
func searchPart(f fld, need string) (matched bool, err bool) {
	if f.missing {
		return true, false
	}
	if f.null {
		return false, true
	}
	return strings.Contains(strings.ToLower(f.s), need), false
}

func hasAuth(am []string, m string) bool {
	for _, v := range am {
		if v == m {
			return true
		}
	}
	return false
}

// ---------- sort ----------

const LAST = "\uffff"

// sortSpec — {by, order} from the body (absent = Node defaults).
type sortSpec struct {
	by    *string
	order *string
}

// invalid — Node `_sortAndPaginate` guard (OError -> 500 page).
func (s *sortSpec) invalid() bool {
	if s.by != nil && *s.by != "" {
		v := *s.by
		if v != "lastActive" && v != "signUpDate" && v != "email" && v != "name" && v != "deletedAt" {
			return true
		}
	}
	if s.order != nil && (*s.order != "asc" && *s.order != "desc") {
		return true
	}
	return false
}

func (s *sortSpec) isName() bool { return s.by != nil && *s.by == "name" }

// nameCmp — (lastName,firstName,email) with missing->'\uffff',
// case-insensitive (localeCompare sensitivity:'base' on this ASCII data).
func nameCmp(a, b *urow) int {
	cmpF := func(av, bv fld) int {
		avv, bvv := LAST, LAST
		if !av.missing && !av.null {
			avv = av.s
		}
		if !bv.missing && !bv.null {
			bvv = bv.s
		}
		x, y := strings.ToLower(avv), strings.ToLower(bvv)
		if x == y {
			return 0
		}
		if x < y {
			return -1
		}
		return 1
	}
	if c := cmpF(a.lastName, b.lastName); c != 0 {
		return c
	}
	if c := cmpF(a.firstName, b.firstName); c != 0 {
		return c
	}
	return cmpF(a.email, b.email)
}

// orderByKey — lodash iteratee: value or missing.
func orderByKey(key string, r *urow) (v any, missing bool) {
	switch key {
	case "email":
		if r.email.missing || r.email.null {
			return nil, true
		}
		return strings.ToLower(r.email.s), false
	case "signUpDate":
		if r.signUpDate.missing || r.signUpDate.null {
			return nil, true
		}
		return r.signUpDate.n, false
	case "lastActive":
		if r.lastActive.missing || r.lastActive.null {
			return nil, true
		}
		return r.lastActive.n, false
	case "deletedAt":
		if r.deletedAt.missing || r.deletedAt.null {
			return nil, true
		}
		return r.deletedAt.n, false
	}
	return nil, true
}

// orderBy — lodash orderBy: undefined values last (asc) / first (desc),
// stable for ties (pinned sequences).
func orderBy(rows []*urow, key string, desc bool) {
	sort.SliceStable(rows, func(i, j int) bool {
		vi, mi := orderByKey(key, rows[i])
		vj, mj := orderByKey(key, rows[j])
		switch {
		case mi && mj:
			return false
		case mi:
			return !desc
		case mj:
			return desc
		default:
			var c int
			if ai, ok := vi.(int64); ok {
				bi := vj.(int64)
				switch {
				case ai < bi:
					c = -1
				case ai > bi:
					c = 1
				}
			} else {
				sa, sb := vi.(string), vj.(string)
				switch {
				case sa < sb:
					c = -1
				case sa > sb:
					c = 1
				}
			}
			if c == 0 {
				return false
			}
			if desc {
				return c > 0
			}
			return c < 0
		}
	})
}

// ---------- body parsing ----------

// jsonBodyFilters — express.json() {filters, sort}; malformed -> error
// (Node 400 {}). Node truthiness preserved (nulls stay absent-equivalent).
func jsonBodyFilters(body []byte) (filters, sortSpec, error) {
	trimmed := strings.TrimSpace(string(body))
	var f filters
	var s sortSpec
	if trimmed == "" {
		return f, s, nil // {}
	}
	var anyv any
	if err := json.Unmarshal([]byte(trimmed), &anyv); err != nil {
		return f, s, err // malformed JSON -> 400 {} (pinned)
	}
	obj, _ := anyv.(map[string]any) // arrays/numbers/strings -> no filters (pinned)
	if fl, ok := obj["filters"].(map[string]any); ok && obj != nil {
		for k, v := range fl {
			switch k {
			case "deleted":
				f.deleted = v
			case "all":
				f.all = v
			case "admin":
				f.admin = v
			case "inactive":
				f.inactive = v
			case "suspended":
				f.suspended = v
			case "local":
				f.local = v
			case "saml":
				f.saml = v
			case "oidc":
				f.oidc = v
			case "ldap":
				f.ldap = v
			case "search":
				f.search = v
			}
		}
	}
	if so, ok := obj["sort"].(map[string]any); ok {
		if by, ok := so["by"]; ok && by != nil {
			if bs, ok := by.(string); ok {
				s.by = &bs
			} else if !isString(by) {
				b := "!" // non-string truthy by -> Node 500
				s.by = &b
			}
		}
		if od, ok := so["order"]; ok && od != nil {
			if os, ok := od.(string); ok {
				s.order = &os
			} else if !isString(od) {
				b := "!"
				s.order = &b
			}
		}
	}
	return f, s, nil
}

// formBodyFilters — qs bracket nesting (filters[admin]=true); values are
// STRINGS and keep Node truthiness ("true"/"0"/""…).
func formBodyFilters(body []byte) (filters, sortSpec, error) {
	qv, err := url.ParseQuery(string(body))
	if err != nil {
		return filters{}, sortSpec{}, err
	}
	var f filters
	for _, k := range []string{"deleted", "all", "admin", "inactive", "suspended", "local", "saml", "oidc", "ldap", "search"} {
		vs, ok := qv["filters["+k+"]"]
		if !ok || len(vs) == 0 {
			continue
		}
		val := vs[0]
		switch k {
		case "deleted":
			f.deleted = val
		case "all":
			f.all = val
		case "admin":
			f.admin = val
		case "inactive":
			f.inactive = val
		case "suspended":
			f.suspended = val
		case "local":
			f.local = val
		case "saml":
			f.saml = val
		case "oidc":
			f.oidc = val
		case "ldap":
			f.ldap = val
		case "search":
			f.search = val
		}
	}
	return f, sortSpec{}, nil
}

// ---------- list handler ----------

// bodyKind — Node body-parser routing: express.json() handles the JSON
// types, express.urlencoded() the form type, and ANY other content type
// leaves req.body undefined so `const { … } = req.body` throws a TypeError
// (500 page) — pinned behaviour, not an oversight.
func bodyKind(ct string) string {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	switch ct {
	case "application/json", "application/*+json":
		return "json"
	case "application/x-www-form-urlencoded":
		return "form"
	}
	return "none"
}

func listHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, ok := gate(a, cxt, res); !ok {
			return
		}
		kind := bodyKind(cxt.Req.Header.Get("Content-Type"))
		body, _ := io.ReadAll(cxt.Req.Body)
		var f filters
		var s sortSpec
		var err error
		switch kind {
		case "json":
			f, s, err = jsonBodyFilters(body)
		case "form":
			f, s, err = formBodyFilters(body)
		default:
			// Node: req.body stays {} for non-matching content types ->
			// default filters (pinned: text/plain/no-CT -> 200 full list)
		}
		if err != nil {
			// express.json() bad request (Node core error path): 400 {}
			res.JSON(400, []byte("{}"))
			return
		}
		if s.invalid() {
			// OError 'Invalid sorting criteria' -> 500 page (pinned)
			views.Error500Page(res.W, pageBase(cxt, cxt.Req.URL.Path))
			return
		}
		listAndRespond(a, cxt, res, f, s)
	}
}

func listAndRespond(a *core.App, cxt *core.Cxt, res *core.Res, f filters, s sortSpec) {
	boom := func() {
		views.Error500Page(res.W, pageBase(cxt, cxt.Req.URL.Path))
	}

	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 15*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		boom()
		return
	}

	rows := []*urow{}
	cur, err := db.Collection("users").Find(ctx, bson.D{})
	if err != nil {
		boom()
		return
	}
	var active []primitive.D
	if err := cur.All(ctx, &active); err != nil {
		boom()
		return
	}
	for _, d := range active {
		rows = append(rows, buildRow(d, false, nil))
	}
	dcur, err := db.Collection("deletedUsers").Find(ctx,
		bson.D{{Key: "user", Value: bson.D{{Key: "$type", Value: "object"}}}})
	if err != nil {
		boom()
		return
	}
	var dels []primitive.D
	if err := dcur.All(ctx, &dels); err != nil {
		boom()
		return
	}
	for _, rec := range dels {
		var u *primitive.D
		var delAt any
		for _, e := range rec {
			switch e.Key {
			case "user":
				if pd, ok := e.Value.(primitive.D); ok {
					u = &pd
				} else if pds, ok := e.Value.(*primitive.D); ok {
					u = pds
				}
			case "deleterData":
				if dd, ok := e.Value.(primitive.D); ok {
					for _, de := range dd {
						if de.Key == "deletedAt" {
							delAt = de.Value
						}
					}
				}
			}
		}
		if u == nil {
			continue
		}
		rows = append(rows, buildRow(*u, true, delAt))
	}

	filtered := []*urow{}
	for _, r := range rows {
		ok, e500 := r.matchFilters(&f)
		if e500 {
			boom() // Node TypeError -> 500 page
			return
		}
		if ok {
			filtered = append(filtered, r)
		}
	}

	if s.isName() {
		sort.SliceStable(filtered, func(i, j int) bool { return nameCmp(filtered[i], filtered[j]) < 0 })
	} else {
		key := "signUpDate"
		if s.by != nil && *s.by != "" {
			key = *s.by
		}
		desc := true
		if s.order != nil {
			desc = *s.order == "desc"
		}
		orderBy(filtered, key, desc)
	}

	var b strings.Builder
	b.WriteString(`{"totalSize":`)
	b.WriteString(strconv.Itoa(len(filtered)))
	b.WriteString(`,"users":[`)
	for i, r := range filtered {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(r.json())
	}
	b.WriteString(`]}`)
	res.JSON(200, []byte(b.String()))
}

// ---------- info handler ----------

func infoHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, ok := gate(a, cxt, res); !ok {
			return
		}
		userId := cxt.Params["1"]

		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 10*time.Second)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			views.Error500Page(res.W, pageBase(cxt, cxt.Req.URL.Path))
			return
		}

		// activationLink — Node _getActivationLink (one live password
		// token, first match in natural order).
		link := "null"
		now := time.Now()
		tokenFilter := bson.D{
			{Key: "use", Value: "password"},
			{Key: "data.user_id", Value: userId},
			{Key: "expiresAt", Value: bson.D{{Key: "$gt", Value: now}}},
			{Key: "usedAt", Value: bson.D{{Key: "$exists", Value: false}}},
			{Key: "peekCount", Value: bson.D{{Key: "$not", Value: bson.D{{Key: "$gte", Value: int32(4)}}}}},
		}
		var tok struct {
			Token string `bson:"token"`
		}
		if terr := db.Collection("tokens").FindOne(ctx, tokenFilter).Decode(&tok); terr == nil {
			link = jsonStr(cxt.SiteURL + "/user/activate?token=" + tok.Token + "&user_id=" + userId)
		}

		// canManageTemplates — User.findById(...).select flags (bad id /
		// missing user -> caught -> false; both pinned 200).
		canMgmt := false
		if hex24.MatchString(userId) {
			oid, _ := primitive.ObjectIDFromHex(userId)
			var u struct {
				Flags struct {
					CanMgmt *bool `bson:"canManageTemplates"`
				} `bson:"flags"`
			}
			if e2 := db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: oid}},
				options.FindOne().SetProjection(bson.D{{Key: "flags.canManageTemplates", Value: 1}})).Decode(&u); e2 == nil &&
				u.Flags.CanMgmt != nil && *u.Flags.CanMgmt {
				canMgmt = true
			}
		}

		res.JSON(200, []byte(`{"activationLink":`+link+`,"canManageTemplates:`+jsonBool(canMgmt)+`}`))
	}
}

// ---------- feature ----------

var usersPat = regexp.MustCompile(`^/admin/users$`)
var infoPat = regexp.MustCompile(`^/admin/user/([^/]+)/info$`)

func Feature(a *core.App) core.Feature {
	fm := newMailBox63b(a)
	return core.Feature{
		Name: "p63-admin-users",
		Routes: []core.Route{
			{Method: "POST", Pattern: usersPat, Handler: listHandler(a)},
			{Method: "GET", Pattern: infoPat, Handler: infoHandler(a)},
			// P6.3b mutation surface (admin-tools UserListController).
			{Method: "POST", Path: "/admin/user/create", Handler: createHandler63b(fm, a)},
			{Method: "POST", Pattern: regexp.MustCompile(`^/admin/user/([0-9a-fA-F]{24})/send-activation$`),
				Handler: sendActivationHandler63b(fm, a)},
			{Method: "POST", Pattern: regexp.MustCompile(`^/admin/user/([0-9a-fA-F]{24})/update$`),
				Handler: updateHandler63b(fm, a)},
			{Method: "POST", Pattern: regexp.MustCompile(`^/admin/user/([0-9a-fA-F]{24})/delete$`),
				Handler: deleteHandler63b(a)},
			{Method: "POST", Pattern: regexp.MustCompile(`^/admin/user/([0-9a-fA-F]{24})/restore$`),
				Handler: restoreHandler63b(a)},
			{Method: "DELETE", Pattern: regexp.MustCompile(`^/admin/user/([0-9a-fA-F]{24})$`),
				Handler: purgeHandler63b(a)},
		},
	}
}
