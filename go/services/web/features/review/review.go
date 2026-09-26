// Package review — D40 P2: the V1 threads / track-changes REST surface on Go
// web, over the room-doc domain ops (go/services/collab/review.go).
//
// Contract = the in-git review panel (frontend/js/features/review-panel,
// pinned from its calls + services/web/types/review-panel/*):
//
//	GET    /project/:pid/threads                                  (read>=)
//	POST   /project/:pid/threads                                  {content, doc?/doc_id?, ranges?}
//	POST   /project/:pid/thread/:threadId/messages                {content}
//	POST   /project/:pid/thread/:threadId/messages/:cid/edit      {content}
//	DELETE /project/:pid/thread/:threadId/messages/:cid
//	DELETE /project/:pid/thread/:threadId/own-messages/:cid       (actor must own the message)
//	POST   /project/:pid/doc/:doc/thread/:threadId/resolve
//	POST   /project/:pid/doc/:doc/thread/:threadId/reopen
//	DELETE /project/:pid/doc/:doc/thread/:threadId                (cascades to messages)
//	POST   /project/:pid/doc/:doc/changes/accept                  {change_ids: [...]}
//	POST   /project/:pid/track_changes                            {enabled: bool}
//
// Wire shapes (pinned from services/web/types/review-panel): message =
// {content, id, timestamp, user, user_id}; thread = {id, doc, state, author,
// created, messages, [resolved, resolved_at, resolved_by_user_id,
// resolved_by_user]}. Dates = millisecond-precision ISO strings.
//
// Auth = core CSRF (anonymous POST → 403 upstream) + session user + project
// role (collabhistory convention: 404 below the required role — project
// existence not leaked). Room = project id (same namespace as collabhistory).

package review

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"ollitex/go/services/collab"
	"ollitex/go/services/web/core"

	"github.com/reearth/ygo/persistence"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

const hex24 = `[a-fA-F0-9]{24}`

var (
	idRe = `[0-9a-zA-Z]{6,64}`

	threadsPattern   = regexp.MustCompile(`^/project/(` + hex24 + `)/threads$`)
	threadActionPat  = regexp.MustCompile(`^/project/(` + hex24 + `)/doc/([A-Za-z0-9._\-/]+)/thread/(` + idRe + `)/(resolve|reopen)$`)
	threadDelPattern = regexp.MustCompile(`^/project/(` + hex24 + `)/doc/([A-Za-z0-9._\-/]+)/thread/(` + idRe + `)$`)
	msgAddPattern    = regexp.MustCompile(`^/project/(` + hex24 + `)/thread/(` + idRe + `)/messages$`)
	msgEditPattern   = regexp.MustCompile(`^/project/(` + hex24 + `)/thread/(` + idRe + `)/messages/(` + idRe + `)/edit$`)
	msgDelPattern    = regexp.MustCompile(`^/project/(` + hex24 + `)/thread/(` + idRe + `)/messages/(` + idRe + `)$`)
	msgOwnDelPattern = regexp.MustCompile(`^/project/(` + hex24 + `)/thread/(` + idRe + `)/own-messages/(` + idRe + `)$`)
	changesAcceptPat = regexp.MustCompile(`^/project/(` + hex24 + `)/doc/([A-Za-z0-9._\-/]+)/changes/accept$`)
	trackChangesPat  = regexp.MustCompile(`^/project/(` + hex24 + `)/track_changes$`)
)

// RoleFor — (user, project) → collab role (collab policy; collabhistory parity).
type RoleFor func(ctx context.Context, uid, projectID string) collab.Role

// UserFor — (uid) → user doc fields (name/email/profile_picture_url/...).
type UserFor func(ctx context.Context, uid string) map[string]any

// TrackFor — (uid) → the per-user track-changes preference (default ON;
// P2 assumption recorded in WEB_GO_STATE.md D40 — owner-confirm the key).
type TrackFor func(ctx context.Context, uid string) (bool, error)

// SetTrackFor — (uid, enabled) error.
type SetTrackFor func(ctx context.Context, uid string, enabled bool) error

// Handlers — zero App + Store/RoleFor/UserFor = hermetic tests (same doctrine
// as collabhistory).
type Handlers struct {
	App         *core.App
	Store       persistence.VersionedPersistence
	RoleFor     RoleFor
	UserFor     UserFor
	TrackFor    TrackFor
	SetTrackFor SetTrackFor
	// Now — injectable clock (unix-ms at call time; default time.Now).
	Now func() int64
}

func (h *Handlers) nowMS() int64 {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now().UnixMilli()
}

// Feature — production wiring (Mongo role/user/track lookups; room store =
// collab.NewMongoStore on the app db, room = project id).
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
		h.UserFor = func(ctx context.Context, uid string) map[string]any {
			db, err := a.Mongo.DB(ctx)
			if err != nil {
				return nil
			}
			var d bson.D
			if err := db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: oid(uid)}}).Decode(&d); err != nil {
				if errors.Is(err, mongo.ErrNoDocuments) {
					return nil
				}
				return nil
			}
			m := map[string]any{}
			for _, e := range d {
				m[e.Key] = e.Value
			}
			return m
		}
		h.TrackFor, h.SetTrackFor = prodTrack(a)
	}
	return core.Feature{Name: "review", Routes: []core.Route{
		{Method: http.MethodGet, Pattern: threadsPattern, Handler: h.threadsList},
		{Method: http.MethodPost, Pattern: threadsPattern, Handler: h.threadCreate},
		{Method: http.MethodPost, Pattern: threadActionPat, Handler: h.threadResolve},
		{Method: http.MethodDelete, Pattern: threadDelPattern, Handler: h.threadDelete},
		{Method: http.MethodPost, Pattern: msgAddPattern, Handler: h.messageAdd},
		{Method: http.MethodPost, Pattern: msgEditPattern, Handler: h.messageEdit},
		{Method: http.MethodDelete, Pattern: msgDelPattern, Handler: h.messageDelete},
		{Method: http.MethodDelete, Pattern: msgOwnDelPattern, Handler: h.ownMessageDelete},
		{Method: http.MethodPost, Pattern: changesAcceptPat, Handler: h.changesAccept},
		{Method: http.MethodPost, Pattern: trackChangesPat, Handler: h.trackChanges},
	}}
}

// ---------- wire shapes (pinned from services/web/types/review-panel) ----------

type reviewUser struct {
	AvatarText string `json:"avatar_text"`
	Email      string `json:"email"`
	Hue        int    `json:"hue"`
	ID         string `json:"id"`
	IsSelf     bool   `json:"isSelf"`
	Name       string `json:"name"`
}

type messageOut struct {
	Content   string     `json:"content"`
	ID        string     `json:"id"`
	Timestamp string     `json:"timestamp"`
	User      reviewUser `json:"user"`
	UserID    string     `json:"user_id"`
}

type resolvedBy struct {
	ResolvedAt     string     `json:"resolved_at"`
	ResolvedByUID  string     `json:"resolved_by_user_id"`
	ResolvedByUser reviewUser `json:"resolved_by_user"`
}

type threadOut struct {
	ID       string       `json:"id"`
	Doc      string       `json:"doc"`
	State    string       `json:"state"`
	Author   reviewUser   `json:"author"`
	Created  string       `json:"created"`
	Messages []messageOut `json:"messages"`
	Resolved bool         `json:"resolved,omitempty"`
	ResInfo  *resolvedBy  `json:"resolvedInfo,omitempty"`
}

// ---------- helpers ----------

func oid(id string) primitive.ObjectID {
	if o, err := primitive.ObjectIDFromHex(id); err == nil {
		return o
	}
	return primitive.NilObjectID
}

// isoMS — millisecond-precision ISO (the vendor `ms` Date string shape).
func isoMS(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z")
}

// hue — stable panel avatar colour from the uid.
func hue(uid string) int {
	sum := 0
	for i := 0; i < len(uid); i++ {
		sum = (sum*31 + int(uid[i])) % 360
	}
	return sum
}

func uidOf(m map[string]any) string {
	if v, ok := m["user_id"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	if v, ok := m["id"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func (h *Handlers) userShape(ctx context.Context, uid, self string) reviewUser {
	u := reviewUser{ID: uid, IsSelf: uid == self, Hue: hue(uid)}
	if h.UserFor != nil {
		if raw := h.UserFor(ctx, uid); raw != nil {
			if v, ok := raw["name"].(string); ok {
				u.Name = v
			}
			if v, ok := raw["email"].(string); ok {
				u.Email = v
			}
			if v, ok := raw["profile_picture_url"].(string); ok {
				u.AvatarText = v
			}
		}
	}
	return u
}

// ---------- store seam (collabhistory pattern) ----------

func (h *Handlers) run(ctx context.Context, fn func(persistence.VersionedPersistence) error) error {
	if h.Store != nil {
		return fn(h.Store)
	}
	if h.App == nil || h.App.Mongo == nil {
		return errors.New("review: no store configured")
	}
	db, err := h.App.Mongo.DB(ctx)
	if err != nil {
		return err
	}
	st, err := collab.NewMongoStore(ctx, db)
	if err != nil {
		return err
	}
	return fn(st)
}

// ---------- gate (collabhistory convention) ----------

func (h *Handlers) gate(cxt *core.Cxt, res *core.Res, pid string, want collab.Role) (string, bool) {
	uid := ""
	if cxt.Sess != nil {
		uid = cxt.Sess.UserIDHex()
	}
	if uid == "" || h.RoleFor == nil {
		res.JSON(http.StatusNotFound, []byte(`{"message":"not found"}`))
		return "", false
	}
	if h.RoleFor(cxt.Req.Context(), uid, pid) < want {
		res.JSON(http.StatusNotFound, []byte(`{"message":"not found"}`))
		return "", false
	}
	return uid, true
}

func notFound(res *core.Res) { res.JSON(http.StatusNotFound, []byte(`{"message":"not found"}`)) }
func badBody(res *core.Res)  { res.JSON(http.StatusBadRequest, []byte(`{"message":"invalid body"}`)) }
func internal(res *core.Res) {
	res.JSON(http.StatusInternalServerError, []byte(`{"message":"internal error"}`))
}
func forbidden(res *core.Res) { res.JSON(http.StatusForbidden, []byte(`{"message":"forbidden"}`)) }

func decodeJSON(r io.Reader, dst any) error {
	return json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(dst)
}

// ---------- threads ----------

func (h *Handlers) threadEnvelope(ctx context.Context, pid, self string, st persistence.VersionedPersistence, th collab.Thread) threadOut {
	msgs, _ := collab.MessagesOfThread(ctx, st, pid, th.ID)
	o := threadOut{
		ID:       th.ID,
		Doc:      th.File,
		State:    th.State,
		Author:   h.userShape(ctx, uidOf(th.Author), self),
		Created:  isoMS(th.Created),
		Messages: make([]messageOut, 0, len(msgs)),
	}
	for _, m := range msgs {
		uid := uidOf(m.Author)
		o.Messages = append(o.Messages, messageOut{
			Content:   m.Text,
			ID:        m.ID,
			Timestamp: isoMS(m.Created),
			User:      h.userShape(ctx, uid, self),
			UserID:    uid,
		})
	}
	if th.State == collab.ThreadStateResolved {
		o.Resolved = true
		by := th.ResolvedBy
		if by == nil {
			by = th.Author
		}
		o.ResInfo = &resolvedBy{
			ResolvedAt:     isoMS(th.Resolved),
			ResolvedByUID:  uidOf(by),
			ResolvedByUser: h.userShape(ctx, uidOf(by), self),
		}
	}
	return o
}

func (h *Handlers) threadsList(cxt *core.Cxt, res *core.Res) {
	pid := cxt.Params["1"]
	uid, ok := h.gate(cxt, res, pid, collab.ReadOnly)
	if !ok {
		return
	}
	var out []threadOut
	if err := h.run(cxt.Req.Context(), func(st persistence.VersionedPersistence) error {
		ths, err := collab.ListThreads(cxt.Req.Context(), st, pid)
		if err != nil {
			return err
		}
		out = make([]threadOut, 0, len(ths))
		for _, th := range ths {
			out = append(out, h.threadEnvelope(cxt.Req.Context(), pid, uid, st, th))
		}
		return nil
	}); err != nil {
		internal(res)
		return
	}
	b, _ := json.Marshal(struct {
		Threads []threadOut `json:"threads"`
	}{out})
	res.JSON(http.StatusOK, b)
}

func (h *Handlers) authorFrom(cxt *core.Cxt, uid string) map[string]any {
	m := map[string]any{"user_id": uid}
	if h.UserFor != nil {
		if raw := h.UserFor(cxt.Req.Context(), uid); raw != nil {
			for _, k := range []string{"name", "email"} {
				if v, ok := raw[k].(string); ok {
					m[k] = v
				}
			}
		}
	}
	return m
}

func (h *Handlers) threadCreate(cxt *core.Cxt, res *core.Res) {
	pid := cxt.Params["1"]
	uid, ok := h.gate(cxt, res, pid, collab.ReadWrite)
	if !ok {
		return
	}
	var body struct {
		Content string           `json:"content"`
		Doc     string           `json:"doc"`
		DocID   string           `json:"doc_id"`
		Ranges  []map[string]any `json:"ranges"`
		Thread  string           `json:"thread_id"`
	}
	if err := decodeJSON(cxt.Req.Body, &body); err != nil {
		badBody(res)
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		badBody(res)
		return
	}
	doc := body.Doc
	if doc == "" {
		doc = body.DocID
	}
	now := h.nowMS()
	author := h.authorFrom(cxt, uid)
	var created collab.Thread
	var firstMessage collab.Comment
	if err := h.run(cxt.Req.Context(), func(st persistence.VersionedPersistence) error {
		th, applied, _, err := collab.AddThread(cxt.Req.Context(), st, pid, collab.Thread{
			ID: body.Thread, File: doc, State: collab.ThreadStateOpened, Author: author, Created: now,
		})
		if err != nil {
			return err
		}
		created = th
		if applied {
			m, applied2, _, err := collab.AddComment(cxt.Req.Context(), st, pid, collab.Comment{
				ThreadID: th.ID, File: doc, Text: body.Content, State: "opened",
				Author: author, Ranges: body.Ranges, Created: now, Edited: now,
			})
			if err != nil {
				return err
			}
			if applied2 {
				firstMessage = m
			}
		}
		return nil
	}); err != nil {
		internal(res)
		return
	}
	b, _ := json.Marshal(h.threadCreateOut(uid, created.ID, doc, now, created.State, firstMessage, author))
	res.JSON(http.StatusCreated, b)
}

// threadCreateOut — the created-thread envelope (first message included; no
// DB round-trip needed for the 201 body).
func (h *Handlers) threadCreateOut(uid string, threadID, doc string, now int64, state string, first collab.Comment, author map[string]any) threadOut {
	o := threadOut{
		ID:       threadID,
		Doc:      doc,
		State:    state,
		Author:   h.userShape(context.Background(), uidOf(author), uid),
		Created:  isoMS(now),
		Messages: make([]messageOut, 0, 1),
	}
	if first.ID != "" {
		o.Messages = append(o.Messages, messageOut{
			Content:   first.Text,
			ID:        first.ID,
			Timestamp: isoMS(first.Created),
			User:      h.userShape(context.Background(), uidOf(author), uid),
			UserID:    uidOf(author),
		})
	}
	return o
}

// ---------- thread actions ----------

func (h *Handlers) threadResolve(cxt *core.Cxt, res *core.Res) {
	pid, threadID, action := cxt.Params["1"], cxt.Params["3"], cxt.Params["4"]
	uid, ok := h.gate(cxt, res, pid, collab.ReadWrite)
	if !ok {
		return
	}
	state := collab.ThreadStateResolved
	if action == "reopen" {
		state = collab.ThreadStateOpened
	}
	var out threadOut
	if err := h.run(cxt.Req.Context(), func(st persistence.VersionedPersistence) error {
		th, _, _, err := collab.SetThreadStateBy(cxt.Req.Context(), st, pid, threadID, state, h.authorFrom(cxt, uid))
		if err != nil {
			return err
		}
		out = h.threadEnvelope(cxt.Req.Context(), pid, uid, st, th)
		return nil
	}); err != nil {
		if errors.Is(err, collab.ErrThreadNotFound) {
			notFound(res)
			return
		}
		internal(res)
		return
	}
	b, _ := json.Marshal(out)
	res.JSON(http.StatusOK, b)
}

func (h *Handlers) threadDelete(cxt *core.Cxt, res *core.Res) {
	pid, threadID := cxt.Params["1"], cxt.Params["3"]
	if _, ok := h.gate(cxt, res, pid, collab.ReadWrite); !ok {
		return
	}
	if err := h.run(cxt.Req.Context(), func(st persistence.VersionedPersistence) error {
		_, _, err := collab.DeleteThread(cxt.Req.Context(), st, pid, threadID)
		return err
	}); err != nil {
		internal(res)
		return
	}
	res.JSON(http.StatusOK, []byte(`{}`))
}

// ---------- messages ----------

func (h *Handlers) messageAdd(cxt *core.Cxt, res *core.Res) {
	pid, threadID := cxt.Params["1"], cxt.Params["2"]
	uid, ok := h.gate(cxt, res, pid, collab.ReadWrite)
	if !ok {
		return
	}
	var body struct {
		Content string `json:"content"`
		ID      string `json:"comment_id"`
	}
	if err := decodeJSON(cxt.Req.Body, &body); err != nil || strings.TrimSpace(body.Content) == "" {
		badBody(res)
		return
	}
	now := h.nowMS()
	author := h.authorFrom(cxt, uid)
	var out collab.Comment
	if err := h.run(cxt.Req.Context(), func(st persistence.VersionedPersistence) error {
		m, applied, _, err := collab.AddComment(cxt.Req.Context(), st, pid, collab.Comment{
			ID: body.ID, ThreadID: threadID, Text: body.Content, Author: author, Created: now, Edited: now,
		})
		if err != nil {
			return err
		}
		if !applied {
			return errors.New("review: message already exists")
		}
		out = m
		return nil
	}); err != nil {
		internal(res)
		return
	}
	b, _ := json.Marshal(messageOut{
		Content: body.Content, ID: out.ID, Timestamp: isoMS(now),
		User: h.userShape(cxt.Req.Context(), uid, uid), UserID: uid,
	})
	res.JSON(http.StatusCreated, b)
}

func (h *Handlers) messageEdit(cxt *core.Cxt, res *core.Res) {
	pid, threadID, cid := cxt.Params["1"], cxt.Params["2"], cxt.Params["3"]
	uid, ok := h.gate(cxt, res, pid, collab.ReadWrite)
	if !ok {
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := decodeJSON(cxt.Req.Body, &body); err != nil || strings.TrimSpace(body.Content) == "" {
		badBody(res)
		return
	}
	var out collab.Comment
	if err := h.run(cxt.Req.Context(), func(st persistence.VersionedPersistence) error {
		msgs, err := collab.MessagesOfThread(cxt.Req.Context(), st, pid, threadID)
		if err != nil {
			return err
		}
		found := false
		for _, m := range msgs {
			if m.ID == cid {
				found = true
				break
			}
		}
		if !found {
			return collab.ErrCommentNotFound
		}
		m, _, _, err := collab.EditCommentText(cxt.Req.Context(), st, pid, cid, body.Content)
		if err != nil {
			return err
		}
		out = m
		return nil
	}); err != nil {
		if errors.Is(err, collab.ErrCommentNotFound) {
			notFound(res)
			return
		}
		internal(res)
		return
	}
	b, _ := json.Marshal(messageOut{
		Content: body.Content, ID: out.ID, Timestamp: isoMS(out.Edited),
		User: h.userShape(cxt.Req.Context(), uidOf(out.Author), uid), UserID: uidOf(out.Author),
	})
	res.JSON(http.StatusOK, b)
}

func (h *Handlers) messageDelete(cxt *core.Cxt, res *core.Res) {
	pid, cid := cxt.Params["1"], cxt.Params["3"]
	if _, ok := h.gate(cxt, res, pid, collab.ReadWrite); !ok {
		return
	}
	if err := h.run(cxt.Req.Context(), func(st persistence.VersionedPersistence) error {
		_, _, err := collab.DeleteComment(cxt.Req.Context(), st, pid, cid)
		return err
	}); err != nil {
		internal(res)
		return
	}
	res.JSON(http.StatusOK, []byte(`{}`))
}

// ownMessageDelete — the panel's own-message rule: only the message author
// (or a write-role user) may delete via /own-messages/.
func (h *Handlers) ownMessageDelete(cxt *core.Cxt, res *core.Res) {
	pid, cid := cxt.Params["1"], cxt.Params["3"]
	uid, ok := h.gate(cxt, res, pid, collab.ReadWrite)
	if !ok {
		return
	}
	owner := ""
	if err := h.run(cxt.Req.Context(), func(st persistence.VersionedPersistence) error {
		msgs, err := collab.MessagesOfThread(cxt.Req.Context(), st, pid, cxt.Params["2"])
		if err != nil {
			return err
		}
		for _, m := range msgs {
			if m.ID == cid {
				owner = uidOf(m.Author)
				return nil
			}
		}
		return collab.ErrCommentNotFound
	}); err != nil {
		if errors.Is(err, collab.ErrCommentNotFound) {
			notFound(res)
			return
		}
		internal(res)
		return
	}
	if owner != uid {
		forbidden(res)
		return
	}
	if err := h.run(cxt.Req.Context(), func(st persistence.VersionedPersistence) error {
		_, _, err := collab.DeleteComment(cxt.Req.Context(), st, pid, cid)
		return err
	}); err != nil {
		internal(res)
		return
	}
	res.JSON(http.StatusOK, []byte(`{}`))
}

// ---------- track changes ----------

func (h *Handlers) changesAccept(cxt *core.Cxt, res *core.Res) {
	pid := cxt.Params["1"]
	if _, ok := h.gate(cxt, res, pid, collab.ReadWrite); !ok {
		return
	}
	var body struct {
		ChangeIDs []string `json:"change_ids"`
	}
	if err := decodeJSON(cxt.Req.Body, &body); err != nil || len(body.ChangeIDs) == 0 {
		badBody(res)
		return
	}
	accepted := 0
	if err := h.run(cxt.Req.Context(), func(st persistence.VersionedPersistence) error {
		for _, id := range body.ChangeIDs {
			if _, applied, _, err := collab.AcceptChange(cxt.Req.Context(), st, pid, id); err == nil && applied {
				accepted++
			}
		}
		return nil
	}); err != nil {
		internal(res)
		return
	}
	b, _ := json.Marshal(struct {
		ProjectID string `json:"project_id"`
		Accepted  int    `json:"accepted"`
	}{pid, accepted})
	res.JSON(http.StatusOK, b)
}

func (h *Handlers) trackChanges(cxt *core.Cxt, res *core.Res) {
	pid := cxt.Params["1"]
	uid, ok := h.gate(cxt, res, pid, collab.ReadOnly)
	if !ok {
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeJSON(cxt.Req.Body, &body); err == nil && body.Enabled != nil && h.SetTrackFor != nil {
		if err := h.SetTrackFor(cxt.Req.Context(), uid, *body.Enabled); err != nil {
			internal(res)
			return
		}
	}
	val := true
	if h.TrackFor != nil {
		v, err := h.TrackFor(cxt.Req.Context(), uid)
		if err == nil {
			val = v
		}
	}
	b, _ := json.Marshal(struct {
		Enabled bool `json:"enabled"`
	}{val})
	res.JSON(http.StatusOK, b)
}

// ---------- production track-changes persistence (users doc) ----------

func prodTrack(a *core.App) (TrackFor, SetTrackFor) {
	tr := func(ctx context.Context, uid string) (bool, error) {
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			return true, err
		}
		var doc struct {
			Settings bson.M `bson:"settings"`
		}
		err = db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: oid(uid)}}).Decode(&doc)
		if err != nil {
			if errors.Is(err, mongo.ErrNoDocuments) {
				return true, nil // default ON (panel default)
			}
			return true, err
		}
		if v, ok := doc.Settings["track_changes"]; ok {
			if b, ok := v.(bool); ok {
				return b, nil
			}
		}
		return true, nil
	}
	set := func(ctx context.Context, uid string, enabled bool) error {
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			return err
		}
		_, err = db.Collection("users").UpdateOne(ctx,
			bson.D{{Key: "_id", Value: oid(uid)}},
			bson.D{{Key: "$set", Value: bson.D{{Key: "settings.track_changes", Value: enabled}}}})
		return err
	}
	return tr, set
}

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
	}
	return d, err
}
