package toolkit

// Hub data — the hub admin views (site.general.projects.* + site.general.users.*)
// inside the toolkit TUI. Owner directive 2026-10-05/06 (TODO-65c003a6).
//
// Semantics are PARITY with the Go web truths (the exact predicates the /hub
// admin leaves rely on) AND with the LIVE document shapes of the shared
// content store (verified 2026-10-06 against overleafmongo `ollitex`):
//
//   - projects (go/services/web/features/projectlist/admin.go):
//       trashed   = the `trashed` field is NON-EMPTY (legacy model: an array /
//     a user id; live docs carry `trashed: []` when NOT trashed)
//       deleted   = a row appears in the `deletedProjects` collection
//       inactive  = lastUpdated < (now - 1 calendar year)
//     the leaf views are INDEPENDENT filters (a project can be both
//     trashed and inactive), so the counters are per-leaf, not a partition.
//
//   - users (go/services/web/features/adminusers/adminusers.go):
//       admin     = isAdmin == true            (live: `role` may also exist)
//       suspended = `suspended` truthy (Node truthy): bool, non-empty string,
//       array length, ObjectID (live docs: usually absent = not suspended)
//       inactive  = lastActive missing OR lastActive < (now - 1 calendar year)
//       deleted   = a row in the `deletedUsers` collection
//     also per-leaf independent filters.
//
//   - owner: live projects carry `owner_ref` = a user id (string). The hub
//     frontend resolves owner id -> user email; we do the same join (a
//     users _id -> email map), falling back to `owner` shapes and finally
//     to the raw id (honest, never silently empty when resolvable).
//   - name:  live projects use `name` (the newer docs use `title` — both
//     accepted, `name` first).

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// HubProjectRow is one project with its per-leaf flags.
type HubProjectRow struct {
	ID          string
	Name        string
	Owner       string // resolved email (or the raw id when unresolvable)
	LastUpdated int64  // unix ms (0 = missing)
	Trashed     bool   // the leaf `trashed` matches this row
	Inactive    bool   // the leaf `inactive` matches this row
	Deleted     bool   // the leaf `deleted` matches this row
}

// HubUserRow is one user with its per-leaf flags.
type HubUserRow struct {
	ID         string
	Email      string
	IsAdmin    bool  // the leaf `admins` matches this row
	Suspended  bool  // the leaf `suspended` matches this row
	Inactive   bool  // the leaf `inactive` matches this row
	Deleted    bool  // the leaf `deleted` matches this row
	SignUp     int64 // unix ms
	LastActive int64 // unix ms (0 = missing => inactive)
}

// HubStats is one collected snapshot of the hub views.
type HubStats struct {
	DB        string
	Now       time.Time
	MongoHost string
	Projects  struct {
		All      int // the whole hub list (collection + deleted rows)
		Inactive int
		Trashed  int
		Deleted  int
		Sample   []HubProjectRow
	}
	Users struct {
		All       int
		Admins    int
		Suspended int
		Inactive  int
		Deleted   int
		Sample    []HubUserRow
	}
}

// truthy mirrors Node truthy() (adminusers suspended; generic).
func truthyHub(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t != ""
	case bson.ObjectID:
		return true
	case bson.A:
		return len(t) > 0
	case []any:
		return len(t) > 0
	case nil:
		return false
	default:
		return true
	}
}

// trashedFlag mirrors the live legacy model: an empty array/missing = NOT
// trashed; a non-empty array, a user id string, true, or an ObjectID =
// trashed. (admin.go: `(trashed||[]).some(...)` — same intent.)
func trashedFlag(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t != "" && t != "false"
	case bson.ObjectID:
		return true
	case bson.A:
		return len(t) > 0
	case []any:
		return len(t) > 0
	case nil:
		return false
	default:
		s := fmt.Sprintf("%v", t)
		return s != "" && s != "[]" && s != "null"
	}
}

// asDocTime is ms-if-time-ish, else 0.
func asDocTime(v any) int64 {
	switch t := v.(type) {
	case time.Time:
		return t.UnixMilli()
	case bson.DateTime: // driver v2 decodes BSON dates as its own ms type
		return int64(t)
	case bson.Timestamp:
		return int64(t.T) * 1000
	case int64:
		return t
	case int32:
		return int64(t)
	case float64:
		return int64(t)
	case string:
		if ts, err := time.Parse(time.RFC3339, t); err == nil {
			return ts.UnixMilli()
		}
		if ts, err := time.Parse("2006-01-02T15:04:05.000Z", t); err == nil {
			return ts.UnixMilli()
		}
	case bson.ObjectID:
		// ObjectID's first 4 bytes are a unix timestamp (legacy ids)
		b := t_bytes(t)
		if len(b) >= 4 {
			sec := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
			return int64(sec) * 1000
		}
	default:
		return 0
	}
	return 0
}

func t_bytes(o bson.ObjectID) []byte {
	b := [12]byte(o)
	return b[:]
}

// projectDoc pulls the fields we classify (live + newer shapes).
func projectDoc(d bson.M) HubProjectRow {
	r := HubProjectRow{}
	if id, ok := d["_id"].(bson.ObjectID); ok {
		r.ID = id.Hex()
	} else if s, ok := d["_id"].(string); ok {
		r.ID = s
	}
	if n, ok := d["name"].(string); ok && n != "" {
		r.Name = n
	} else if n, ok := d["title"].(string); ok {
		r.Name = n
	}
	r.LastUpdated = asDocTime(d["lastUpdated"])
	r.Trashed = trashedFlag(d["trashed"])
	return r
}

// hubUserEmail reads a user doc's email: `email` first, then the legacy
// `emails` fallback with ALL accepted shapes (bson.A / []any; bson.D /
// bson.M / map elements), keeping the original string-entry fallback last.
//
// driver v2 quirk (the Q/C bug class, now in toolkit): decoding into bson.M
// keeps nested arrays the nominal type bson.A and nested objects bson.D —
// a `.([]any)` / `. (bson.M)` chain silently fails on both.
func hubUserEmail(d bson.M) string {
	if se, ok := d["email"].(string); ok && se != "" {
		return se
	}
	var emails []any
	switch v := d["emails"].(type) {
	case bson.A:
		emails = v
	case []any:
		emails = v
	}
	for _, m := range emails {
		switch e2 := m.(type) {
		case bson.D:
			for _, kv := range e2 {
				if kv.Key == "emailAddress" {
					if s, ok := kv.Value.(string); ok && s != "" {
						return s
					}
				}
			}
		case bson.M:
			if s, ok := e2["emailAddress"].(string); ok && s != "" {
				return s
			}
		case map[string]any:
			if s, ok := e2["emailAddress"].(string); ok && s != "" {
				return s
			}
		}
	}
	for _, m := range emails {
		if s, ok := m.(string); ok && strings.Contains(s, "@") {
			return s
		}
	}
	if e, ok := d["email"].(string); ok {
		return e
	}
	return ""
}

// usersDoc pulls the user fields (Node-parity flags).
func usersDoc(d bson.M, now time.Time) HubUserRow {
	r := HubUserRow{}
	if id, ok := d["_id"].(bson.ObjectID); ok {
		r.ID = id.Hex()
	} else if s, ok := d["_id"].(string); ok {
		r.ID = s
	}
	r.Email = hubUserEmail(d)
	if ab, ok := d["isAdmin"].(bool); ok {
		r.IsAdmin = ab
	}
	r.Suspended = truthyHub(d["suspended"])
	r.SignUp = asDocTime(d["signUpDate"])
	r.LastActive = asDocTime(d["lastActive"])
	// Node: inactive = !user.lastActive || user.lastActive < yearAgo
	yearAgo := now.AddDate(-1, 0, 0)
	if r.LastActive == 0 || time.UnixMilli(r.LastActive).Before(yearAgo) {
		r.Inactive = true
	}
	return r
}

// HubResolve finds a usable content-DB DSN + database name.
//
// Order: config-store MONGO_URL → OLLITEX_HUB_MONGO_DSN → the stack's known
// mongo names (the shared store `overleafmongo` first — that is where the
// canonical app and the imported `ollitex` DB live — then the canonical
// service `ollitex-mongo`, then the e2e `mongo`). The FIRST candidate that
// pings AND has the expected DB with collections wins (there are several
// mongods on this box and the name `overleafmongo` collides across bridges
// — the data check disambiguates).
func (t *Toolkit) HubResolve(ctx context.Context) (dsn, db, host string, err error) {
	db = t.val("MONGO_DB")
	if db == "" {
		db = os.Getenv("OLLITEX_HUB_MONGO_DB")
	}
	if db == "" {
		db = "ollitex"
	}
	if v := t.val("MONGO_URL"); v != "" {
		dsn = v
	}
	if v := os.Getenv("OLLITEX_HUB_MONGO_DSN"); v != "" {
		dsn = v
	}
	// directConnection=true: the shared store is a single-node replica set
	// advertising 127.0.0.1 (the legacy cep pattern) — topology discovery
	// from OTHER networks would follow that advertised address and fail
	// (no primary). Direct connection reads the primary straight.
	direct := "?directConnection=true"
	cands := []string{
		dsn,
		"mongodb://overleafmongo:27017/" + direct,
		"mongodb://contentstore:27017/" + direct,
		"mongodb://ollitex-mongo:27017/" + direct,
		"mongodb://mongo:27017/" + direct,
	}
	var firstPing string
	for _, c := range cands {
		if c == "" {
			continue
		}
		cl, e := mongo.Connect(options.Client().ApplyURI(c).SetServerSelectionTimeout(2500 * time.Millisecond))
		if e != nil {
			continue
		}
		pctx, pcancel := context.WithTimeout(ctx, 4*time.Second)
		ok := cl.Ping(pctx, nil) == nil
		names, _ := cl.Database(db).ListCollectionNames(pctx, bson.M{})
		pcancel()
		hasData := ok && len(names) > 0
		if ok {
			if firstPing == "" {
				firstPing = c
			}
		}
		has := e == nil && ok && hasData
		_ = cl.Disconnect(context.Background())
		if has {
			host = hostOf(c)
			return c, db, host, nil
		}
	}
	if firstPing != "" {
		return firstPing, db, hostOf(firstPing), nil
	}
	return "", db, "", fmt.Errorf("no reachable content DB (content DB must be running — start the stack or set OLLITEX_HUB_MONGO_DSN)")
}

func hostOf(dsn string) string {
	if i := strings.Index(dsn, "://"); i >= 0 {
		tail := dsn[i+3:]
		for _, sep := range []string{"/", "?"} {
			if j := strings.Index(tail, sep); j >= 0 {
				tail = tail[:j]
			}
		}
		return tail
	}
	return dsn
}

// HubCollect is the single read pass behind the hub screen. Plain reads,
// bounded (the admin collections are small by design).
func (t *Toolkit) HubCollect(ctx context.Context, sample int) (*HubStats, error) {
	dsn, dbName, host, err := t.HubResolve(ctx)
	if err != nil {
		return nil, err
	}
	cl, err := mongo.Connect(options.Client().ApplyURI(dsn))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}
	defer func() { _ = cl.Disconnect(context.Background()) }()
	pctx, pcancel := context.WithTimeout(ctx, 5*time.Second)
	defer pcancel()
	if err := cl.Ping(pctx, nil); err != nil {
		return nil, fmt.Errorf("mongo ping %s: %w", host, err)
	}
	db := cl.Database(dbName)
	now := time.Now()
	if sample <= 0 {
		sample = 10
	}
	s := &HubStats{DB: dbName, Now: now, MongoHost: host}

	// ---- users' email map (owner id -> email, the hub's own join) ---------
	emailByID := map[string]string{}
	if curU, e2 := db.Collection("users").Find(pctx, bson.M{},
		options.Find().SetProjection(bson.M{"_id": 1, "email": 1, "emails": 1})); e2 == nil {
		var udocs []bson.M
		if err := curU.All(pctx, &udocs); err == nil {
			for _, ud := range udocs {
				e := hubUserEmail(ud)
				if e != "" {
					if id, ok := ud["_id"].(bson.ObjectID); ok {
						emailByID[id.Hex()] = e
					} else if id, ok := ud["_id"].(string); ok {
						emailByID[id] = e
					}
				}
			}
		}
		_ = curU.Close(pctx)
	}

	// ---- projects ----------------------------------------------------------
	fctx, fcancel := context.WithTimeout(ctx, 15*time.Second)
	defer fcancel()
	projDocs := []bson.M{}
	if cur, e3 := db.Collection("projects").Find(fctx, bson.M{},
		options.Find().SetProjection(bson.M{"_id": 1, "name": 1, "title": 1, "owner_ref": 1, "owner": 1, "lastUpdated": 1, "trashed": 1})); e3 == nil {
		_ = cur.All(fctx, &projDocs)
		_ = cur.Close(fctx)
	}
	deletedIDs := map[string]struct{}{}
	delDocs := []bson.M{}
	if c, e4 := db.Collection("deletedProjects").Find(fctx, bson.M{}); e4 == nil {
		_ = c.All(fctx, &delDocs)
		_ = c.Close(fctx)
	}

	rows := make([]HubProjectRow, 0, len(projDocs))
	for _, d := range projDocs {
		r := projectDoc(d)
		switch or := d["owner_ref"].(type) {
		case string:
			if or != "" {
				if e, ok := emailByID[or]; ok && e != "" {
					r.Owner = e
				} else {
					r.Owner = or
				}
			}
		case bson.ObjectID:
			if e, ok := emailByID[or.Hex()]; ok && e != "" {
				r.Owner = e
			} else {
				r.Owner = or.Hex()
			}
		default:
			r.Owner = ownerEmail(d["owner"], emailByID)
		}
		if r.ID != "" {
			if _, ok2 := deletedIDs[r.ID]; ok2 {
				r.Deleted = true
			}
		}
		if !r.Deleted && !r.Trashed {
			yearAgo := now.AddDate(-1, 0, 0)
			if r.LastUpdated > 0 && time.UnixMilli(r.LastUpdated).Before(yearAgo) {
				r.Inactive = true
			}
		}
		rows = append(rows, r)
	}
	// deleted rows (from the snapshot docs; classify as deleted, keep name)
	for _, dd := range delDocs {
		if snap, ok := dd["project"].(bson.M); ok {
			dr := projectDoc(snap)
			if dr.ID != "" {
				deletedIDs[dr.ID] = struct{}{}
			}
			dr.Deleted = true
			if raw, ok := snap["owner_ref"].(string); ok && emailByID[raw] != "" {
				dr.Owner = emailByID[raw]
			}
			rows = append(rows, dr)
			continue
		}
		if id, ok := dd["_id"].(bson.ObjectID); ok {
			deletedIDs[id.Hex()] = struct{}{}
			// mark the matching live row as deleted if present
			for i := range rows {
				if rows[i].ID == id.Hex() {
					rows[i].Deleted = true
				}
			}
		}
	}

	// per-leaf independent counters (the hub views are filters, not a
	// partition — a trashed project can also be inactive)
	nonDel := make([]HubProjectRow, 0, len(rows))
	for _, r := range rows {
		if r.Deleted {
			s.Projects.Deleted++
			continue
		}
		nonDel = append(nonDel, r)
		if r.Trashed {
			s.Projects.Trashed++
		}
		if r.Inactive {
			s.Projects.Inactive++
		}
	}
	s.Projects.All = len(nonDel) + s.Projects.Deleted
	sort.Slice(nonDel, func(i, j int) bool { return nonDel[i].LastUpdated > nonDel[j].LastUpdated })
	if len(nonDel) > sample {
		nonDel = nonDel[:sample]
	}
	s.Projects.Sample = nonDel

	// ---- users --------------------------------------------------------------
	uDocs := []bson.M{}
	if cur, e5 := db.Collection("users").Find(fctx, bson.M{},
		options.Find().SetProjection(bson.M{"_id": 1, "email": 1, "emails": 1, "isAdmin": 1, "suspended": 1, "lastActive": 1, "signUpDate": 1})); e5 == nil {
		_ = cur.All(fctx, &uDocs)
		_ = cur.Close(fctx)
	} else if cur2, e6 := db.Collection("user").Find(fctx, bson.M{},
		options.Find().SetProjection(bson.M{"_id": 1, "email": 1, "emails": 1, "isAdmin": 1, "suspended": 1, "lastActive": 1, "signUpDate": 1})); e6 == nil {
		_ = cur2.All(fctx, &uDocs)
		_ = cur2.Close(fctx)
	} else {
		return nil, fmt.Errorf("find users: %v (and `user`: %v)", e5, e6)
	}
	delUserDocs := []bson.M{}
	if c, e7 := db.Collection("deletedUsers").Find(fctx, bson.M{}); e7 == nil {
		_ = c.All(fctx, &delUserDocs)
		_ = c.Close(fctx)
	}

	uNonDel := make([]HubUserRow, 0, len(uDocs))
	for _, d := range uDocs {
		r := usersDoc(d, now)
		uNonDel = append(uNonDel, r)
		if r.IsAdmin {
			s.Users.Admins++
		}
		if r.Suspended {
			s.Users.Suspended++
		}
		if r.Inactive {
			s.Users.Inactive++
		}
	}
	for _, d := range delUserDocs {
		r := usersDoc(d, now)
		r.Deleted = true
		s.Users.Deleted++
	}
	s.Users.All = len(uNonDel) + s.Users.Deleted
	sort.Slice(uNonDel, func(i, j int) bool { return uNonDel[i].SignUp > uNonDel[j].SignUp })
	if len(uNonDel) > sample {
		uNonDel = uNonDel[:sample]
	}
	s.Users.Sample = uNonDel

	return s, nil
}

// ownerEmail resolves an `owner`-shaped value (string id / {email} / ObjectID
// / array) through the users map.
func ownerEmail(owner any, emailByID map[string]string) string {
	switch o := owner.(type) {
	case nil:
		return ""
	case string:
		if strings.Contains(o, "@") {
			return o
		}
		if emailByID != nil {
			if e, ok := emailByID[o]; ok && e != "" {
				return e
			}
		}
		return o
	case bson.ObjectID:
		if emailByID != nil {
			if e, ok := emailByID[o.Hex()]; ok && e != "" {
				return e
			}
		}
		return o.Hex()
	case bson.M:
		if e, ok := o["email"].(string); ok {
			return e
		}
		if id, ok := o["_id"].(bson.ObjectID); ok {
			return ownerEmail(id, emailByID)
		}
	case []any:
		if l := len(o); l > 0 {
			return ownerEmail(o[l-1], emailByID)
		}
	}
	return ""
}
