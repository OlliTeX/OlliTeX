// Package collabhistory — S3 (D19): the Yjs version-history REST surface on
// Go web.
//
// The collab room (ygo/MongoStore version index, package go/services/collab)
// IS the project's version history from now on: every edit/seed/restore is
// one versioned update. These routes give the history UI its data:
//
//	GET  /project/:pid/collab/history              (>= read)  versions list, newest first
//	GET  /project/:pid/collab/history/:v           (>= read)  {version, at, content}
//	POST /project/:pid/collab/history/:v/restore   (write)    restore v → new head
//	GET  /project/:pid/collab/doc                  (>= read)  current head {version, content}
//
// Authorization: the logged-in session user (passport.user._id) must hold a
// project role — owner/collaborator = read-write, readOnly = read-only,
// anyone else gets 404 (existence of the project is not leaked; same
// convention as the OT editor APIs).
//
// The CRDT mechanics live in go/services/collab (SeedTextContent / TextAt /
// HeadText / RestoreToVersion / TextType). This package is HTTP + policy
// only — no Yjs encoding here, so the history surface and the WS surface
// can never drift apart on document semantics.
//
// The yhub REST API shape (history / changeset / restore) is the spec these
// routes follow; yhub itself is NOT a runtime dependency (D19/D5).

package collabhistory

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"ollitex/go/services/collab"
	"ollitex/go/services/web/core"

	"github.com/reearth/ygo/persistence"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// RoleFor resolves (user, project) → collab role. Production wiring reuses
// the exact collab policy (owner/collab = RW, readOnly = RO, else Deny);
// tests inject a fake.
type RoleFor func(ctx context.Context, uid, projectID string) collab.Role

// Handlers is the feature state. Zero App + a set Store = hermetic (tests);
// Feature(a) wires the production Mongo-backed store + role.
type Handlers struct {
	App     *core.App
	Store   persistence.VersionedPersistence // optional; wins over App.Mongo
	RoleFor RoleFor
	DocReader DocReader // optional; room-resolver project-doc access (tests)
	// VLog — d5dd23dd S1: version-metadata side log (nil = today's behavior).
	// Test injection point; the App.Mongo path resolves a lazily-cached
	// Mongo-backed log (versionLog).
	VLog     collab.Log
	vlogMu   sync.Mutex
	vlogMemo collab.Log
}

// versionLog — test-injected (VLog) or lazily built over App.Mongo (nil
// best-effort on any failure — metadata disabled, store behavior unchanged).
func (h *Handlers) versionLog(ctx context.Context) collab.Log {
	if h.VLog != nil {
		return h.VLog
	}
	if h.App == nil || h.App.Mongo == nil {
		return nil
	}
	h.vlogMu.Lock()
	defer h.vlogMu.Unlock()
	if h.vlogMemo != nil {
		return h.vlogMemo
	}
	db, err := h.App.Mongo.DB(ctx)
	if err != nil {
		return nil
	}
	lg, err := collab.NewMongoVersionLog(ctx, db)
	if err != nil {
		return nil
	}
	h.vlogMemo = lg
	return lg
}

var (
	histPattern    = regexp.MustCompile(`^/project/([a-fA-F0-9]{24})/collab/history$`)
	histVPattern   = regexp.MustCompile(`^/project/([a-fA-F0-9]{24})/collab/history/([1-9][0-9]{0,9})$`)
	restorePattern = regexp.MustCompile(`^/project/([a-fA-F0-9]{24})/collab/history/([1-9][0-9]{0,9})/restore$`)
	docPattern     = regexp.MustCompile(`^/project/([a-fA-F0-9]{24})/collab/doc$`)
	roomPattern    = regexp.MustCompile(`^/project/([a-fA-F0-9]{24})/collab/room$`)
)

// Feature — production wiring over the app's Mongo (lazy client).
func Feature(a *core.App) core.Feature {
	h := &Handlers{App: a}
	if a != nil && a.Mongo != nil {
		h.RoleFor = func(ctx context.Context, uid, pid string) collab.Role {
			db, err := a.Mongo.DB(ctx)
			if err != nil {
				return collab.Deny
			}
			auth := &collab.SessionAuth{Ctx: ctx, M: webMongo{db: db}}
			role, _ := auth.ProjectRole(uid, pid)
			return role
		}
	}
	return core.Feature{Name: "collabhistory", Routes: []core.Route{
		{Method: "GET", Pattern: histPattern, Handler: h.list},
		{Method: "GET", Pattern: histVPattern, Handler: h.at},
		{Method: "POST", Pattern: restorePattern, Handler: h.restore},
		{Method: "GET", Pattern: docPattern, Handler: h.doc},
		{Method: "GET", Pattern: roomPattern, Handler: h.room}, // 024 Option B: doc → room resolver
	}}
}

// webMongo adapts the web app's database to collab.Mongo (role lookup only
// uses ProjectByID).
type webMongo struct{ db *mongo.Database }

func (w webMongo) UserByID(ctx context.Context, id string) (bson.D, error) {
	var d bson.D
	err := w.db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: oid(id)}}).Decode(&d)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return d, nil
}

func (w webMongo) ProjectByID(ctx context.Context, id string) (bson.D, error) {
	var d bson.D
	err := w.db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: oid(id)}}).Decode(&d)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return d, nil
}

func oid(id string) bson.ObjectID {
	if o, err := bson.ObjectIDFromHex(id); err == nil {
		return o
	}
	return bson.NilObjectID
}

// ---------- response bodies (stable shapes; the client parses these) ----------

const (
	bodyInvalidID    = `{"message":"invalid id"}`
	bodyNotFound     = `{"message":"not found"}`
	bodyUnauthorized = `{"message":"role required"}`
)

// ---------- request plumbing ----------

// pid — the 24-hex project id param (already matched by route pattern).
func pidParam(cxt *core.Cxt) string {
	return strings.ToLower(cxt.Params["1"])
}

// vParam — the version param (group 2) as persistence.Version.
func vParam(cxt *core.Cxt) (persistence.Version, bool) {
	v, err := strconv.ParseUint(cxt.Params["2"], 10, 64)
	if err != nil || v < 1 {
		return 0, false
	}
	return persistence.Version(v), true
}

// run executes fn with the versioned store (injected or app-Mongo-backed)
// + the S1 actor/origin ctx enrichment (nil = plain).
func (h *Handlers) run(cxt *core.Cxt, enrich func(context.Context) context.Context, fn func(context.Context, persistence.VersionedPersistence) error) error {
	if enrich == nil {
		enrich = func(ctx context.Context) context.Context { return ctx }
	}
	lg := h.versionLog(cxt.Req.Context())
	if h.Store != nil {
		return fn(enrich(cxt.Req.Context()), collab.Wrap(h.Store, lg))
	}
	if h.App == nil || h.App.Mongo == nil {
		return errors.New("collabhistory: no mongo available")
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 10*time.Second)
	defer cancel()
	db, err := h.App.Mongo.DB(ctx)
	if err != nil {
		return err
	}
	st, err := collab.NewMongoStore(ctx, db)
	if err != nil {
		return err
	}
	return fn(enrich(ctx), collab.Wrap(st, lg))
}

// ---------- handlers ----------

type versionOut struct {
	Version persistence.Version `json:"version"`
	At      string              `json:"at"`
}

func (h *Handlers) list(cxt *core.Cxt, res *core.Res) {
	pid := pidParam(cxt)
	if !h.gate(cxt, res, pid, collab.ReadOnly) {
		return
	}
	var metas []persistence.VersionMeta
	if err := h.run(cxt, nil, func(ctx context.Context, st persistence.VersionedPersistence) error {
		var e error
		metas, e = st.ListVersions(ctx, pid)
		return e
	}); err != nil {
		res.JSON(500, []byte(`{"message":"history unavailable"}`))
		return
	}
	out := make([]versionOut, 0, len(metas))
	for _, m := range metas {
		out = append(out, versionOut{Version: m.Version, At: m.UpdatedAt.UTC().Format(time.RFC3339)})
	}
	b, _ := json.Marshal(struct {
		ProjectID string       `json:"project_id"`
		Versions  []versionOut `json:"versions"`
	}{pid, out})
	res.JSON(200, b)
}

func (h *Handlers) at(cxt *core.Cxt, res *core.Res) {
	pid := pidParam(cxt)
	if !h.gate(cxt, res, pid, collab.ReadOnly) {
		return
	}
	v, ok := vParam(cxt)
	if !ok {
		res.JSON(404, []byte(bodyInvalidID))
		return
	}
	var (
		meta    persistence.VersionMeta
		content string
		found   bool
	)
	if err := h.run(cxt, nil, func(ctx context.Context, st persistence.VersionedPersistence) error {
		_, m, f, e := st.GetUpdate(ctx, pid, v)
		if e != nil {
			return e
		}
		if !f {
			return nil
		}
		found, meta = true, m
		var ce error
		content, ce = collab.TextAt(ctx, st, pid, v)
		return ce
	}); err != nil {
		if errors.Is(err, collab.ErrUnknownVersion) {
			res.JSON(404, []byte(bodyNotFound))
			return
		}
		res.JSON(500, []byte(`{"message":"history unavailable"}`))
		return
	}
	if !found {
		res.JSON(404, []byte(bodyNotFound))
		return
	}
	b, _ := json.Marshal(struct {
		Version persistence.Version `json:"version"`
		At      string              `json:"at"`
		Content string              `json:"content"`
	}{v, meta.UpdatedAt.UTC().Format(time.RFC3339), content})
	res.JSON(200, b)
}

func (h *Handlers) restore(cxt *core.Cxt, res *core.Res) {
	pid := pidParam(cxt)
	if !h.gate(cxt, res, pid, collab.ReadWrite) {
		return
	}
	v, ok := vParam(cxt)
	if !ok {
		res.JSON(404, []byte(bodyInvalidID))
		return
	}
	var newHead persistence.Version
	if err := h.run(cxt, func(ctx context.Context) context.Context {
		// d5dd23dd S1: attribute the restore version to the session user
		// with the Node-parity origin kind (shared.ts: file-restore).
		if cxt.Sess != nil {
			ctx = collab.WithActor(ctx, cxt.Sess.UserIDHex())
		}
		return collab.WithOrigin(ctx, "file-restore")
	}, func(ctx context.Context, st persistence.VersionedPersistence) error {
		var e error
		newHead, e = collab.RestoreToVersion(ctx, st, pid, v)
		return e
	}); err != nil {
		if errors.Is(err, collab.ErrEmptyRoom) || errors.Is(err, collab.ErrUnknownVersion) {
			res.JSON(404, []byte(bodyNotFound))
			return
		}
		res.JSON(500, []byte(`{"message":"restore failed"}`))
		return
	}
	b, _ := json.Marshal(struct {
		Version persistence.Version `json:"version"`
	}{newHead})
	res.JSON(200, b)
}

func (h *Handlers) doc(cxt *core.Cxt, res *core.Res) {
	pid := pidParam(cxt)
	if !h.gate(cxt, res, pid, collab.ReadOnly) {
		return
	}
	var (
		content string
		head    persistence.Version
	)
	if err := h.run(cxt, nil, func(ctx context.Context, st persistence.VersionedPersistence) error {
		var e error
		content, head, e = collab.HeadText(ctx, st, pid)
		return e
	}); err != nil {
		res.JSON(500, []byte(`{"message":"doc unavailable"}`))
		return
	}
	b, _ := json.Marshal(struct {
		Version persistence.Version `json:"version"`
		Content string              `json:"content"`
	}{head, content})
	res.JSON(200, b)
}

// room — 024 Option B: the authoritative (project, document) → room name
// resolver for the editor client:
//
//	GET /project/:id/collab/room?doc={docID}  → {"room":"…"}
//	GET /project/:id/collab/room              → {"room":"{id}"}  (root room)
//
// The room identity contract (go/services/collab roomkey.go): the project's
// ROOT document keeps the D19 room "{projectID}" (existing live history,
// review records, IndexedDB "ollitex-collab-{pid}" all stay valid); every
// other document gets its own room "{projectID}-{docID}". The client gets
// EXACTLY the room the collab service will serve (seed + role both resolve
// the same way server-side) — no client-side policy, no meta change.
func (h *Handlers) room(cxt *core.Cxt, res *core.Res) {
	pid := pidParam(cxt)
	if !h.gate(cxt, res, pid, collab.ReadOnly) {
		return
	}
	doc := strings.ToLower(cxt.Req.URL.Query().Get("doc"))
	rootDoc := h.projectRootDoc(cxt, pid)
	// Root room (D19 contract — unchanged identity: history, review records
	// and IndexedDB "ollitex-collab-{pid}" all belong to this room):
	//  - no doc param, or
	//  - the doc IS the project's root document.
	if doc == "" || (rootDoc != "" && doc == rootDoc) {
		b, _ := json.Marshal(struct {
			Room string `json:"room"`
			Root bool   `json:"root"`
		}{pid, true})
		res.JSON(200, b)
		return
	}
	if !regexp.MustCompile(`^[0-9a-f]{24}$`).MatchString(doc) {
		res.JSON(400, []byte(bodyInvalidID))
		return
	}
	// The per-doc room is legitimate only for a doc INSIDE this project's
	// tree — refuse unknown/foreign doc ids (same contract as the seed's
	// tree check; the 404 hides existence, per the panel convention).
	if !h.rootOfContains(cxt, pid, doc) {
		res.JSON(404, []byte(bodyNotFound))
		return
	}
	b, _ := json.Marshal(struct {
		Room string `json:"room"`
		Root bool   `json:"root"`
	}{pid + "-" + doc, false})
	res.JSON(200, b)
}

// rootOfContains — true when docID occurs in the project's rootFolder tree.
func (h *Handlers) projectRootDoc(cxt *core.Cxt, pid string) string {
	doc, err := h.fetchProjectDoc(cxt, pid)
	if err != nil || doc == nil {
		return ""
	}
	return docIDHexOf(fldOrZero(doc, "rootDoc_id"))
}

func (h *Handlers) rootOfContains(cxt *core.Cxt, pid, docID string) bool {
	doc, err := h.fetchProjectDoc(cxt, pid)
	if err != nil || doc == nil {
		return false
	}
	return docInRootFolder(doc, docID)
}

// DocReader — injectable (project, doc) access for the room resolver
// (hermetic tests); nil → the App.Mongo path.
type DocReader func(cxt *core.Cxt, projectID string) (bson.D, error)

func (h *Handlers) fetchProjectDoc(cxt *core.Cxt, pid string) (bson.D, error) {
	if h.DocReader != nil {
		return h.DocReader(cxt, pid)
	}
	if h.App == nil || h.App.Mongo == nil {
		return nil, nil
	}
	ctx := cxt.Req.Context()
	db, err := h.App.Mongo.DB(ctx)
	if err != nil {
		return nil, err
	}
	var d bson.D
	err = db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: oid(pid)}}).Decode(&d)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return d, nil
}

// docInRootFolder — docID anywhere in the rootFolder tree (docs[] at any
// depth; live shape = array-of-folder wrappers). Mirrors the collab seed's
// tree check so client-visible rooms and service-seeded rooms agree.
// toAnySlice — normalize an array value decoded from mongo ([]any in the
// JSON-decode world, bson.A in the bson.D-decode world) to []any. Walkers
// that only match []any silently miss every real mongo array (the 024 room
// resolver 404 class): both encodings carry the same elements.
func toAnySlice(v any) []any {
	switch s := v.(type) {
	case []any:
		return s
	case bson.A:
		return []any(s)
	}
	return nil
}

func docInRootFolder(doc bson.D, docID string) bool {
	rf, ok := fld(doc, "rootFolder")
	if !ok || rf == nil {
		return false
	}
	return docInValues(rf, docID)
}

func docInValues(v any, docID string) bool {
	switch t := v.(type) {
	case []any, bson.A:
		for _, e := range toAnySlice(t) {
			if docInValues(e, docID) {
				return true
			}
		}
		return false
	case bson.D:
		id := docIDHexOf(fldOrZero(t, "_id"))
		if id == docID {
			return true
		}
		for _, k := range []string{"docs", "folders", "rootFolder", "children"} {
			if sub, ok := fld(t, k); ok && sub != nil && docInValues(sub, docID) {
				return true
			}
		}
		return false
	}
	return false
}

// fld — first value for key (found, zeroValue) in a bson.D.
func fld(d bson.D, key string) (any, bool) {
	for i := 0; i < len(d); i++ {
		if d[i].Key == key {
			return d[i].Value, true
		}
	}
	return nil, false
}

func fldOrZero(d bson.D, key string) any { v, _ := fld(d, key); return v }

func docIDHexOf(v any) string {
	if s, ok := v.(string); ok {
		return strings.ToLower(s)
	}
	if o, ok := v.(bson.ObjectID); ok {
		return o.Hex()
	}
	return ""
}

// gate — session user + role check (fail closed: no session/role → 404).
func (h *Handlers) gate(cxt *core.Cxt, res *core.Res, pid string, want collab.Role) bool {
	uid := ""
	if cxt.Sess != nil {
		uid = cxt.Sess.UserIDHex()
	}
	if uid == "" || h.RoleFor == nil {
		res.JSON(404, []byte(bodyNotFound))
		return false
	}
	role := h.RoleFor(cxt.Req.Context(), uid, pid)
	if role < want {
		// Deny for everything (including read): don't reveal existence.
		res.JSON(404, []byte(bodyNotFound))
		return false
	}
	return true
}
