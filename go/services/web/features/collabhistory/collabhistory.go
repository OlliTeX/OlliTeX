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
	"time"

	"ollitex/go/services/collab"
	"ollitex/go/services/web/core"

	"github.com/reearth/ygo/persistence"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
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
}

var (
	histPattern    = regexp.MustCompile(`^/project/([a-fA-F0-9]{24})/collab/history$`)
	histVPattern   = regexp.MustCompile(`^/project/([a-fA-F0-9]{24})/collab/history/([1-9][0-9]{0,9})$`)
	restorePattern = regexp.MustCompile(`^/project/([a-fA-F0-9]{24})/collab/history/([1-9][0-9]{0,9})/restore$`)
	docPattern     = regexp.MustCompile(`^/project/([a-fA-F0-9]{24})/collab/doc$`)
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

func oid(id string) primitive.ObjectID {
	if o, err := primitive.ObjectIDFromHex(id); err == nil {
		return o
	}
	return primitive.NilObjectID
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

// run executes fn with the versioned store (injected or app Mongo-backed).
func (h *Handlers) run(cxt *core.Cxt, fn func(context.Context, persistence.VersionedPersistence) error) error {
	if h.Store != nil {
		return fn(cxt.Req.Context(), h.Store)
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
	return fn(ctx, st)
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
	if err := h.run(cxt, func(ctx context.Context, st persistence.VersionedPersistence) error {
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
	if err := h.run(cxt, func(ctx context.Context, st persistence.VersionedPersistence) error {
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
	if err := h.run(cxt, func(ctx context.Context, st persistence.VersionedPersistence) error {
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
	if err := h.run(cxt, func(ctx context.Context, st persistence.VersionedPersistence) error {
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
