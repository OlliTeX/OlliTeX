package projectlist

// POST /api/project (P7 completion unit U1) — the JSON project list the hub
// and legacy dashboard use.
//
// Node sources (oracle):
//
//	services/web/app/src/router.mjs                              (requireLogin →
//	                                                               getProjects 30/60s)
//	services/web/app/src/Features/Project/ProjectListController.mjs
//		· getProjectsJsonSchema     (z.strictObject body: filters/sort/page)
//		· _getProjects              (buckets → format → filter → sort → inject)
//		· _formatProjects           (bucket order + token dedup + level strings)
//		· _applyFilters/_matchesFilters/_hasActiveFilter
//		· _sortAndPaginate          (orderBy by lastUpdated | title | owner;
//	                                                                         page TODO'd away)
//		· _formatProjectInfo        (archived/trashed per user; readOnly-token
//	                                                                         nulls owner + lastUpdatedBy)
//		· _injectProjectUsers       (owner / lastUpdatedBy user refs)
//
// Pinned wire (live Node oracle 2026-09-22):
//
//	200 {"totalSize":N,"projects":[
//	  {"id","name","archived","trashed","accessLevel","source",   ← key order
//	   "lastUpdated":"…Z","lastUpdatedBy":{…}|null,"owner":{…}}]}
//
//	  · user ref: {"id","email","firstName","lastName"}
//	  · owner OMITTED when owner_ref is null or the user is not found;
//	    lastUpdatedBy is null (never omitted) when absent.
//	  · bucket levels: owner/owner, readWrite/invite, review/invite,
//	    readOnly/invite, readAndWrite/token, readOnly/token.
//	  · sort.lastUpdated asc|desc (STABLE — lodash orderBy); sort.title and
//	    sort.owner are no-ops (orderBy on a missing key → input order kept).
//	  · page is IGNORED (Node TODO) — projects[] is the full filtered list;
//	    totalSize = filtered length.
//
// VA (enforced; exact strings pinned from the live 400s), wire:
//	{"error":"Validation error: <issue>; <issue>…","statusCode":400}
//
//	  Unrecognized key: "zzz" at "body"
//	  Unrecognized key: "zzz" at "body.filters"
//	  Invalid option: expected one of "lastUpdated"|"title"|"owner" at "body.sort.by"
//	  Invalid option: expected one of "asc"|"desc" at "body.sort.order"
//	  Too small: expected number to be >0 at "body.page.size"
//	  Invalid Mongo ObjectId at "body.page.lastId"
//	  Invalid input: expected <t>, received <t2> at "body.<path>"
//
// anon: 403 "Forbidden" (core csrf before requireLogin — pinned).
// rate: redis key rate-limit:get-projects:<uid> (shared with Node counters);
//       429 "Rate limit reached, please try again later" (no content-type).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

// ---------- ordered JSON decode (zod reports unknown keys in doc order) ----------

type apOOBj struct {
	M     map[string]any
	Order []string
}

func apObject(v any) (apOOBj, bool) {
	o, ok := v.(apOOBj)
	return o, ok
}

// apOrderedValue — stream-decoded JSON preserving object key order at every
// level (scalars: float64 for numbers, string, bool, nil).
func apOrderedValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		if string(t) == "{" {
			m := map[string]any{}
			var order []string
			for dec.More() {
				kt, kerr := dec.Token()
				if kerr != nil {
					return nil, kerr
				}
				v, verr := apOrderedValue(dec)
				if verr != nil {
					return nil, verr
				}
				ks := kt.(string)
				m[ks] = v
				order = append(order, ks)
			}
			if _, cerr := dec.Token(); cerr != nil {
				return nil, cerr
			}
			return apOOBj{M: m, Order: order}, nil
		}
		if string(t) == "[" {
			var arr []any
			for dec.More() {
				v, verr := apOrderedValue(dec)
				if verr != nil {
					return nil, verr
				}
				arr = append(arr, v)
			}
			if _, cerr := dec.Token(); cerr != nil {
				return nil, cerr
			}
			return arr, nil
		}
	case nil:
		return nil, nil
	case bool:
		return t, nil
	case float64:
		return t, nil
	case string:
		return t, nil
	}
	return nil, fmt.Errorf("unexpected JSON token")
}

func apDecodeOrdered(b []byte) (any, error) {
	dec := json.NewDecoder(strings.NewReader(string(b)))
	return apOrderedValue(dec)
}

// apReadBody — the pinned express.json + zod mapping:
//
//	""           → {} (empty object, valid)
//	object       → apOOBj
//	null         → kind "null"   → zod "received null at body" 400
//	array        → kind "array"  → zod "received array at body" 400
//	scalar       → kind "scalar" → express.json strict → 400 "{}"
//	unparseable  → kind "scalar" → body-parser → 400 "{}"
func apReadBody(r *http.Request) (apOOBj, string, bool) {
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if strings.TrimSpace(string(raw)) == "" {
		return apOOBj{M: map[string]any{}}, "", true
	}
	v, err := apDecodeOrdered(raw)
	if err != nil {
		return apOOBj{}, "scalar", false
	}
	switch x := v.(type) {
	case apOOBj:
		return x, "", true
	case nil:
		return apOOBj{}, "null", false
	case []any:
		return apOOBj{}, "array", false
	default:
		return apOOBj{}, "scalar", false
	}
}

// ---------- VA (hand-written zod messages, declaration order) ----------

func apType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "array"
	case apOOBj:
		return "object"
	}
	return "unknown"
}

func apUnknown(o apOOBj, path string, declared ...string) (string, bool) {
	d := map[string]bool{}
	for _, k := range declared {
		d[k] = true
	}
	var unknown []string
	for _, k := range o.Order {
		if !d[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return "", true
	}
	if len(unknown) == 1 {
		return `Unrecognized key: \"` + unknown[0] + `\" at \"` + path + `\"`, false
	}
	q := make([]string, len(unknown))
	for i, k := range unknown {
		q[i] = `\"` + k + `\"`
	}
	return `Unrecognized keys: ` + strings.Join(q, ", ") + ` at \"` + path + `\"`, false
}

// ---- parsed request shapes ----

type apFilters struct {
	ownedByUser    bool
	sharedWithUser bool
	archived       bool
	trashed        bool
	hasTag         bool
	tag            string // valid when hasTag
	tagIsNull      bool
	hasSearch      bool
	search         string
	anyFilter      bool
}

type apSortReq struct {
	by    string // "lastUpdated" | "title" | "owner" ("" = unset)
	order string // "asc" | "desc" ("" = unset)
}

type apPageReq struct {
	size   float64 // >0
	lastID string  // hex
}

type apParsed struct {
	hasFilters bool
	filters    apFilters
	hasSort    bool
	sort       apSortReq
	hasPage    bool
	page       apPageReq
}

// apValidate — z.strictObject({filters?, sort?, page?}); returns
// (issuesText, parsed, ok).
func apValidate(body apOOBj) (string, *apParsed, bool) {
	var issues []string
	p := &apParsed{}

	if v, has := body.M["filters"]; has {
		if o, ok := apObject(v); !ok {
			issues = append(issues, `Invalid input: expected object, received `+apType(v)+` at \"body.filters\"`)
		} else {
			f, fi, fok := apValidateFilters(o)
			issues = append(issues, fi...)
			if fok {
				p.filters, p.hasFilters = f, true
			}
		}
	}
	if v, has := body.M["sort"]; has {
		if o, ok := apObject(v); !ok {
			issues = append(issues, `Invalid input: expected object, received `+apType(v)+` at \"body.sort\"`)
		} else {
			s, si, sok := apValidateSort(o)
			issues = append(issues, si...)
			if sok {
				p.sort, p.hasSort = s, true
			}
		}
	}
	if v, has := body.M["page"]; has {
		if o, ok := apObject(v); !ok {
			issues = append(issues, `Invalid input: expected object, received `+apType(v)+` at \"body.page\"`)
		} else {
			pg, pi, pok := apValidatePage(o)
			issues = append(issues, pi...)
			if pok {
				p.page, p.hasPage = pg, true
			}
		}
	}
	if u, ok := apUnknown(body, "body", "filters", "sort", "page"); !ok {
		issues = append(issues, u)
	}
	if len(issues) > 0 {
		return strings.Join(issues, "; "), nil, false
	}
	return "", p, true
}

func apValidateFilters(o apOOBj) (apFilters, []string, bool) {
	var issues []string
	var f apFilters
	if v, has := o.M["ownedByUser"]; has {
		if b, ok := v.(bool); ok {
			f.ownedByUser = b
		} else {
			issues = append(issues, `Invalid input: expected boolean, received `+apType(v)+` at \"body.filters.ownedByUser\"`)
		}
	}
	if v, has := o.M["sharedWithUser"]; has {
		if b, ok := v.(bool); ok {
			f.sharedWithUser = b
		} else {
			issues = append(issues, `Invalid input: expected boolean, received `+apType(v)+` at \"body.filters.sharedWithUser\"`)
		}
	}
	if v, has := o.M["archived"]; has {
		if b, ok := v.(bool); ok {
			f.archived = b
		} else {
			issues = append(issues, `Invalid input: expected boolean, received `+apType(v)+` at \"body.filters.archived\"`)
		}
	}
	if v, has := o.M["trashed"]; has {
		if b, ok := v.(bool); ok {
			f.trashed = b
		} else {
			issues = append(issues, `Invalid input: expected boolean, received `+apType(v)+` at \"body.filters.trashed\"`)
		}
	}
	if v, has := o.M["tag"]; has {
		switch tv := v.(type) {
		case string:
			f.tag, f.hasTag = tv, true
		case nil:
			f.tagIsNull = true
		default:
			issues = append(issues, `Invalid input: expected string, received `+apType(v)+` at \"body.filters.tag\"`)
		}
	}
	if v, has := o.M["search"]; has {
		if s, ok := v.(string); ok {
			f.search, f.hasSearch = s, true
		} else {
			issues = append(issues, `Invalid input: expected string, received `+apType(v)+` at \"body.filters.search\"`)
		}
	}
	if u, ok := apUnknown(o, "body.filters", "ownedByUser", "sharedWithUser", "archived", "trashed", "tag", "search"); !ok {
		issues = append(issues, u)
	}
	if len(issues) > 0 {
		return f, issues, false
	}
	// Node _hasActiveFilter: any non-zero check (tag === null counts!)
	f.anyFilter = f.ownedByUser || f.sharedWithUser || f.archived || f.trashed ||
		f.tagIsNull || (f.hasTag && f.tag != "") || (f.hasSearch && f.search != "")
	return f, nil, true
}

func apValidateSort(o apOOBj) (apSortReq, []string, bool) {
	var issues []string
	var s apSortReq
	if v, has := o.M["by"]; has {
		ts, ok := v.(string)
		if !ok {
			issues = append(issues, `Invalid input: expected string, received `+apType(v)+` at \"body.sort.by\"`)
		} else if ts != "lastUpdated" && ts != "title" && ts != "owner" {
			issues = append(issues, `Invalid option: expected one of \"lastUpdated\"|\"title\"|\"owner\" at \"body.sort.by\"`)
		} else {
			s.by = ts
		}
	}
	if v, has := o.M["order"]; has {
		ts, ok := v.(string)
		if !ok {
			issues = append(issues, `Invalid input: expected string, received `+apType(v)+` at \"body.sort.order\"`)
		} else if ts != "asc" && ts != "desc" {
			issues = append(issues, `Invalid option: expected one of \"asc\"|\"desc\" at \"body.sort.order\"`)
		} else {
			s.order = ts
		}
	}
	if u, ok := apUnknown(o, "body.sort", "by", "order"); !ok {
		issues = append(issues, u)
	}
	if len(issues) > 0 {
		return s, issues, false
	}
	return s, nil, true
}

func apValidatePage(o apOOBj) (apPageReq, []string, bool) {
	var issues []string
	var p apPageReq
	if v, has := o.M["size"]; has {
		switch tv := v.(type) {
		case float64:
			if tv != float64(int64(tv)) {
				issues = append(issues, `Invalid input: expected integer, received float at \"body.page.size\"`)
			} else if tv <= 0 {
				issues = append(issues, `Too small: expected number to be >0 at \"body.page.size\"`)
			} else {
				p.size = tv
			}
		default:
			issues = append(issues, `Invalid input: expected number, received `+apType(v)+` at \"body.page.size\"`)
		}
	}
	if v, has := o.M["lastId"]; has {
		ts, ok := v.(string)
		if !ok || !validOID.MatchString(ts) {
			issues = append(issues, `Invalid Mongo ObjectId at \"body.page.lastId\"`)
		} else {
			p.lastID = strings.ToLower(ts)
		}
	}
	if u, ok := apUnknown(o, "body.page", "size", "lastId"); !ok {
		issues = append(issues, u)
	}
	if len(issues) > 0 {
		return p, issues, false
	}
	return p, nil, true
}

// ---------- format / filter / sort (pure — table-testable) ----------

type apItem struct {
	id            string
	name          string
	ownerRef      string // hex; "" = none (null)
	lastUpdated   time.Time
	hasLastUpd    bool
	lastUpdatedBy string // hex; "" = null
	accessLevel   string
	source        string
	archived      bool
	trashed       bool
}

// apFormat — Node _formatProjects: bucket order (owned, readWrite, review,
// readOnly, tokenRW dedup, tokenRO dedup) + level/source strings +
// per-user archived/trashed + readOnly-token owner/lastUpdatedBy nulls.
func apFormat(b *buckets, uid string) []apItem {
	items := []apItem{}
	seen := map[string]bool{}
	add := func(docs []docRef, level, source string) {
		for _, d := range docs {
			if seen[d.id] {
				continue
			}
			seen[d.id] = true
			archived := apContainsHex(d.archived, uid)
			trashed := apContainsHex(d.trashed, uid) && !archived
			own, lub := d.ownerRefHex, d.lastUpdatedByHex
			if level == "readOnly" && source == "token" {
				own, lub = "", "" // Node: readOnlyTokenAccess nulls both
			}
			items = append(items, apItem{
				id: d.id, name: d.name,
				ownerRef:    own,
				lastUpdated: d.lastUpdated, hasLastUpd: d.hasLastUpdated,
				lastUpdatedBy: lub,
				accessLevel:   level, source: source,
				archived: archived, trashed: trashed,
			})
		}
	}
	add(b.owned, "owner", "owner")
	add(b.readWrite, "readWrite", "invite")
	add(b.review, "review", "invite")
	add(b.readOnly, "readOnly", "invite")
	add(b.tokenReadAndWrite, "readAndWrite", "token")
	add(b.tokenReadOnly, "readOnly", "token")
	return items
}

func apContainsHex(list []string, hex string) bool {
	for _, e := range list {
		if e == hex {
			return true
		}
	}
	return false
}

type apTagRow struct {
	name       string
	projectIDs []string // hex strings
}

// apFilter — Node _matchesFilters (AND of the active checks, Node order).
func apFilter(items []apItem, tags []apTagRow, f *apFilters) []apItem {
	if f == nil || !f.anyFilter {
		return items
	}
	out := make([]apItem, 0, len(items))
	for _, it := range items {
		if f.ownedByUser && it.accessLevel != "owner" {
			continue
		}
		if f.sharedWithUser && it.accessLevel == "owner" {
			continue
		}
		if f.archived && !it.archived {
			continue
		}
		if f.trashed && !it.trashed {
			continue
		}
		if f.hasTag && f.tag != "" {
			matched := false
			for _, t := range tags {
				if t.name == f.tag && apContainsHex(t.projectIDs, it.id) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		if f.hasSearch && f.search != "" {
			if !strings.Contains(strings.ToLower(it.name), strings.ToLower(f.search)) {
				continue
			}
		}
		out = append(out, it)
	}
	return out
}

// apSort — Node _sortAndPaginate: _.orderBy(items, [by||lastUpdated],
// [order||desc]). by=title|owner sorts a key that does not exist on the
// formatted object → every compare equal → lodash's stable sort keeps the
// input order (pinned live: first row = the natural-order project).
func apSort(items []apItem, s *apSortReq) []apItem {
	by, order := "lastUpdated", "desc"
	if s != nil {
		if s.by != "" {
			by = s.by
		}
		if s.order != "" {
			order = s.order
		}
	}
	if by != "lastUpdated" {
		return items
	}
	out := make([]apItem, len(items))
	copy(out, items)
	if order == "desc" {
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].lastUpdated.After(out[j].lastUpdated)
		})
	} else {
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].lastUpdated.Before(out[j].lastUpdated)
		})
	}
	return out
}

// ---------- response shape ----------

type apUserRef struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	First string `json:"firstName"`
	Last  string `json:"lastName"`
}

type apWire struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Archived      bool       `json:"archived"`
	Trashed       bool       `json:"trashed"`
	AccessLevel   string     `json:"accessLevel"`
	Source        string     `json:"source"`
	LastUpdated   string     `json:"lastUpdated"`
	LastUpdatedBy *apUserRef `json:"lastUpdatedBy"`
	Owner         *apUserRef `json:"owner,omitempty"`
}

// ---------- handler ----------

// apProjectHandler — POST /api/project (Node: requireLogin → limiter → VA →
// buckets → format → filter → sort → inject → res.json).
func apProjectHandler(a *core.App) func(*core.Cxt, *core.Res) {
	lim := limOfAP(a, "get-projects", 30, 60)
	return func(cxt *core.Cxt, res *core.Res) {
		if cxt.Sess == nil || cxt.Sess.UserIDHex() == "" {
			// core csrf bounces anon POSTs to 403 first; defensive fallback
			if core.AcceptsJSON(cxt.Req) {
				res.SendStatus(401)
			} else {
				res.Redirect(cxt.Req, 302, "/login")
			}
			return
		}
		uid := cxt.Sess.UserIDHex()
		if !lim.Consume(uid) {
			core.Send429(res, "Rate limit reached, please try again later")
			return
		}
		body, kind, ok := apReadBody(cxt.Req)
		if !ok {
			if kind == "scalar" {
				res.BareWrite(400, []byte("{}"))
				return
			}
			res.JSON(400, []byte(`{"error":"Validation error: Invalid input: expected object, received `+kind+` at \"body\"","statusCode":400}`))
			return
		}
		issues, parsed, ok := apValidate(body)
		if !ok {
			res.JSON(400, []byte(`{"error":"Validation error: `+issues+`","statusCode":400}`))
			return
		}
		b, err := loadProjectBuckets(a, cxt, uid)
		if err != nil {
			apServe500(cxt, "loadProjectBuckets: "+err.Error(), res)
			return
		}
		items := apFormat(b, uid)

		var tags []apTagRow
		if parsed.hasFilters && parsed.filters.anyFilter {
			tags, err = apLoadTags(a, cxt, uid)
			if err != nil {
				apServe500(cxt, "apLoadTags: "+err.Error(), res)
				return
			}
		}
		items = apFilter(items, tags, apIfHas(parsed))
		items = apFillDefaults(items, time.Now())
		items = apSort(items, apIfSort(parsed))

		// user injection set (owner_ref + lastUpdatedBy that survive)
		need := map[string]bool{}
		for _, it := range items {
			if it.ownerRef != "" {
				need[it.ownerRef] = true
			}
			if it.lastUpdatedBy != "" {
				need[it.lastUpdatedBy] = true
			}
		}
		users := map[string]apUserRef{}
		if len(need) > 0 {
			um, uerr := apLoadUsers(a, cxt, need)
			if uerr != nil {
				apServe500(cxt, "apLoadUsers: "+uerr.Error(), res)
				return
			}
			users = um
		}

		proj := make([]apWire, 0, len(items))
		for _, it := range items {
			w := apWire{
				ID: it.id, Name: it.name,
				Archived: it.archived, Trashed: it.trashed,
				AccessLevel: it.accessLevel, Source: it.source,
				LastUpdated: it.lastUpdated.UTC().Format("2006-01-02T15:04:05.000Z"),
			}
			if it.lastUpdatedBy != "" {
				if u, ok := users[it.lastUpdatedBy]; ok {
					w.LastUpdatedBy = &u
				}
			}
			if it.ownerRef != "" {
				if u, ok := users[it.ownerRef]; ok {
					w.Owner = &u // else the key is OMITTED (Node: undefined)
				}
			}
			proj = append(proj, w)
		}
		res.JSON(200, core.JSON(apResp{Total: len(items), Projects: proj}))
	}
}

type apResp struct {
	Total    int      `json:"totalSize"`
	Projects []apWire `json:"projects"`
}

// apFillDefaults — Node's mongoose default getter for lastUpdated
// (`default: () => new Date()`): the value is evaluated at property access,
// so it is filled BEFORE _sortAndPaginate reads it — the filled value equals
// the request time ("now"), i.e. the NEWEST in the set, and PINS the sort:
// head of lastUpdated-desc, tail of asc. Pinned A/B: the four stored-ABSENT
// webgo-p4c/na projects render at the request timestamp and lead the
// lastUpdated-desc list (their mutual sub-ms order is Node jitter and not
// reproducible — the gate compares them as a set).
func apFillDefaults(items []apItem, now time.Time) []apItem {
	for i := range items {
		if !items[i].hasLastUpd {
			items[i].lastUpdated = now
			items[i].hasLastUpd = true
		}
	}
	return items
}

func apIfHas(p *apParsed) *apFilters {
	if p.hasFilters {
		return &p.filters
	}
	return nil
}

func apIfSort(p *apParsed) *apSortReq {
	if p.hasSort {
		return &p.sort
	}
	return nil
}

// apServe500 — the pinned 500 HTML page.
func apServe500(cxt *core.Cxt, what string, res *core.Res) {
	log.Printf("webgo: POST /api/project 500 (%s)", what)
	views.Error500Page(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
}

// ---------- mongo loads (tags for the filter, users for injection) ----------

func apMongoCtx(cxt *core.Cxt) (context.Context, context.CancelFunc) {
	return context.WithTimeout(cxt.Req.Context(), 10*time.Second)
}

// apLoadTags — TagsHandler.getAllTags (find {user_id}); name + project_ids.
func apLoadTags(a *core.App, cxt *core.Cxt, uid string) ([]apTagRow, error) {
	if a.Mongo == nil {
		return nil, fmt.Errorf("mongo not available")
	}
	ctx, cancel := apMongoCtx(cxt)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, err
	}
	cur, err := db.Collection("tags").Find(ctx, bson.D{{Key: "user_id", Value: uid}})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := []apTagRow{}
	for cur.Next(ctx) {
		var d primitive.D
		if cur.Decode(&d) != nil {
			continue
		}
		row := apTagRow{}
		for _, e := range d {
			switch e.Key {
			case "name":
				if s, ok := e.Value.(string); ok {
					row.name = s
				}
			case "project_ids":
				if arr, ok := e.Value.(primitive.A); ok {
					row.projectIDs = make([]string, 0, len(arr))
					for _, pv := range arr {
						if s, ok := pv.(string); ok {
							row.projectIDs = append(row.projectIDs, s)
						}
					}
				}
			}
		}
		out = append(out, row)
	}
	return out, cur.Err()
}

// apLoadUsers — UserGetter.getUsers({_id: {$in}}, {first_name,last_name,email}).
func apLoadUsers(a *core.App, cxt *core.Cxt, need map[string]bool) (map[string]apUserRef, error) {
	if a.Mongo == nil {
		return nil, fmt.Errorf("mongo not available")
	}
	ids := make([]primitive.ObjectID, 0, len(need))
	for h := range need {
		o, err := primitive.ObjectIDFromHex(h)
		if err != nil {
			continue // malformed stored ids never match (Node: same)
		}
		ids = append(ids, o)
	}
	if len(ids) == 0 {
		return map[string]apUserRef{}, nil
	}
	ctx, cancel := apMongoCtx(cxt)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, err
	}
	proj := options.Find().SetProjection(bson.D{
		{Key: "first_name", Value: 1},
		{Key: "last_name", Value: 1},
		{Key: "email", Value: 1},
	})
	cur, err := db.Collection("users").Find(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: ids}}}}, proj)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := map[string]apUserRef{}
	for cur.Next(ctx) {
		var d primitive.D
		if cur.Decode(&d) != nil {
			continue
		}
		ref := apUserRef{}
		for _, e := range d {
			switch e.Key {
			case "_id":
				if o, ok := e.Value.(primitive.ObjectID); ok {
					ref.ID = o.Hex()
				}
			case "email":
				if s, ok := e.Value.(string); ok {
					ref.Email = s
				}
			case "first_name":
				if s, ok := e.Value.(string); ok {
					ref.First = s
				}
			case "last_name":
				if s, ok := e.Value.(string); ok {
					ref.Last = s
				}
			}
		}
		if ref.ID != "" {
			out[ref.ID] = ref
		}
	}
	return out, cur.Err()
}

// limOfAP — lazy limiter construction (Feature(nil) in unit tests must not
// dereference a nil *core.App; Consume is nil-safe and fails open).
func limOfAP(a *core.App, name string, points, sec int) *core.RateLimiter {
	if a == nil {
		return nil
	}
	return core.NewRateLimiter(a.Redis, name, points, sec)
}
