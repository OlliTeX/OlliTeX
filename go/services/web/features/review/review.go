// Package review — D40 P2: the review-panel REST surface on Go web, over the
// room-doc domain ops (go/services/collab/review.go).
//
// Contract = the IN-GIT review panel (frontend/js/features/review-panel),
// pinned 1:1 from its calls + services/web/types/review-panel (D40-d5: the
// Node fork never served these routes — the panel's OT-op paths are dead on
// the Yjs engine, document-container.ts throws for historyOTShareDoc — so
// the REST surface IS the contract; the shapes below are what the panel
// consumes):
//
//	REST URL (panel pins)                                   Go handler
//	GET    /project/:pid/threads                     → Record<threadId, Thread>
//	POST   /project/:pid/thread/:threadId/messages   → thread record (201; the
//	       body {content, id?, doc?}                 first message creates the
//	POST   /project/:pid/thread/:threadId/messages/:cid/edit
//	DELETE /project/:pid/thread/:threadId/messages/:cid
//	DELETE /project/:pid/thread/:threadId/own-messages/:cid
//	POST   /project/:pid/doc/:doc/thread/:threadId/resolve
//	POST   /project/:pid/doc/:doc/thread/:threadId/reopen
//	DELETE /project/:pid/doc/:doc/thread/:threadId
//	POST   /project/:pid/doc/:doc/changes            → create change (server-
//	POST   /project/:pid/doc/:doc/changes/accept     → assisted; editor-side
//	POST   /project/:pid/track_changes               → creation pending)
//
// Thread record (comment-thread.ts, keyed by thread id):
//	{ messages: [{content,id,timestamp,user{avatar_text,email,hue,id,isSelf,
//	   name},user_id}], resolved?, resolved_at?, resolved_by_user_id?,
//	   resolved_by_user? } — `doc`/`author`/`created` ride along as a
//	documented superset (the overview + e2e need the doc association).
//
// track_changes (track-changes-state-context.ts): body {on_for?, on_for_guests?}
// → persisted as the project's explicit `track_changes` map (Node parity:
// project.track_changes → editor trackChangesState).
//
// Auth = core CSRF (anonymous POST → 403 upstream) + session + project role
// (collabhistory convention: 404 below the required role — project existence
// not leaked). Room = project id (same namespace as collabhistory).

package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
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
	changesPat       = regexp.MustCompile(`^/project/(` + hex24 + `)/doc/([A-Za-z0-9._\-/]+)/changes$`)
	changesAcceptPat = regexp.MustCompile(`^/project/(` + hex24 + `)/doc/([A-Za-z0-9._\-/]+)/changes/accept$`)
	trackChangesPat  = regexp.MustCompile(`^/project/(` + hex24 + `)/track_changes$`)
)

// RoleFor — (user, project) → collab role (collab policy; collabhistory parity).
type RoleFor func(ctx context.Context, uid, projectID string) collab.Role

// UserFor — (uid) → user doc fields (name/email/profile_picture_url/...).
type UserFor func(ctx context.Context, uid string) map[string]any

// TrackStateFor — (projectID) → the project's explicit track-changes map
// (uid → bool, `__guests__` included); empty = off.
type TrackStateFor func(ctx context.Context, projectID string) (map[string]bool, error)

// TrackStateSet — (projectID, map) error (the whole explicit map).
type TrackStateSet func(ctx context.Context, projectID string, m map[string]bool) error

// Emit — relay a project-room event to the realtime bus (panel socket
// listeners update local state from these; pinned listener signatures in
// threads-context.tsx / ranges-context.tsx / track-changes-state-context.tsx).
// nil = no broadcast (hermetic tests). Best-effort: a failed relay is
// logged, not surfaced (Node parity: socket emit failures don't fail the
// request).
type Emit func(ctx context.Context, pid, name string, args []any) error

// Handlers — zero App + Store/RoleFor/... = hermetic tests (same doctrine as
// collabhistory).
type Handlers struct {
	App           *core.App
	Store         persistence.VersionedPersistence
	RoleFor       RoleFor
	UserFor       UserFor
	TrackStateFor TrackStateFor
	TrackStateSet TrackStateSet
	Emit          Emit
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
			role, _ := auth.ProjectRole(uid, strings.ToLower(pid))
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
			return bsonDToAny(d)
		}
		h.TrackStateFor, h.TrackStateSet = prodTrack(a)
		h.Emit = prodEmit(a.Cfg.RealtimeURL)
	}
	return core.Feature{Name: "review", Routes: []core.Route{
		{Method: http.MethodGet, Pattern: threadsPattern, Handler: h.threadsList},
		{Method: http.MethodPost, Pattern: threadActionPat, Handler: h.threadResolve},
		{Method: http.MethodDelete, Pattern: threadDelPattern, Handler: h.threadDelete},
		{Method: http.MethodPost, Pattern: msgAddPattern, Handler: h.messageAdd},
		{Method: http.MethodPost, Pattern: msgEditPattern, Handler: h.messageEdit},
		{Method: http.MethodDelete, Pattern: msgDelPattern, Handler: h.messageDelete},
		{Method: http.MethodDelete, Pattern: msgOwnDelPattern, Handler: h.ownMessageDelete},
		{Method: http.MethodPost, Pattern: changesPat, Handler: h.changesCreate},
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

// threadRecord — pinned Thread shape + documented superset (doc/author/created).
type threadRecord struct {
	Doc            string       `json:"doc"`
	Author         reviewUser   `json:"author,omitempty"`
	Created        string       `json:"created,omitempty"`
	Messages       []messageOut `json:"messages"`
	Resolved       bool         `json:"resolved,omitempty"`
	ResolvedAt     string       `json:"resolved_at,omitempty"`
	ResolvedByUID  string       `json:"resolved_by_user_id,omitempty"`
	ResolvedByUser reviewUser   `json:"resolved_by_user,omitempty"`
}

// ---------- helpers ----------

func oid(id string) primitive.ObjectID {
	if o, err := primitive.ObjectIDFromHex(id); err == nil {
		return o
	}
	return primitive.NilObjectID
}

func bsonDToAny(d bson.D) map[string]any {
	m := make(map[string]any, len(d))
	for _, e := range d {
		m[e.Key] = e.Value
	}
	return m
}

// isoMS — millisecond-precision ISO (JS Date JSON shape).
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
	if v, ok := m["author"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		if m2, ok := v.(map[string]any); ok {
			return uidOf(m2)
		}
	}
	return ""
}

func anyField(m map[string]any, key string) (string, bool) {
	s, ok := m[key].(string)
	return s, ok
}

// relay — best-effort room event (logs on failure; Node parity: a socket
// emit failure never 5xx's the request).
func (h *Handlers) relay(ctx context.Context, pid, name string, args []any) {
	if h.Emit == nil {
		return
	}
	if err := h.Emit(ctx, pid, name, args); err != nil {
		fmt.Fprintf(os.Stderr, "review: relay %s failed: %v\n", name, err)
	}
}

// userName / userEmail — flat user fields for relay payloads (resolve-thread
// pinned shape {email, first_name, id}).
func (h *Handlers) userName(ctx context.Context, uid string) string {
	if h.UserFor == nil {
		return ""
	}
	if raw := h.UserFor(ctx, uid); raw != nil {
		if v, ok := anyField(raw, "name"); ok {
			return v
		}
	}
	return ""
}

func (h *Handlers) userEmail(ctx context.Context, uid string) string {
	if h.UserFor == nil {
		return ""
	}
	if raw := h.UserFor(ctx, uid); raw != nil {
		if v, ok := anyField(raw, "email"); ok {
			return v
		}
	}
	return ""
}

func (h *Handlers) userShape(ctx context.Context, uid, self string) reviewUser {
	u := reviewUser{ID: uid, IsSelf: uid == self, Hue: hue(uid)}
	if h.UserFor != nil {
		if raw := h.UserFor(ctx, uid); raw != nil {
			if v, ok := anyField(raw, "name"); ok {
				u.Name = v
			}
			if v, ok := anyField(raw, "email"); ok {
				u.Email = v
			}
			if v, ok := anyField(raw, "profile_picture_url"); ok {
				u.AvatarText = v
			}
		}
	}
	return u
}

func (h *Handlers) authorOf(cxt *core.Cxt, uid string) map[string]any {
	m := map[string]any{"user_id": uid}
	if h.UserFor != nil {
		if raw := h.UserFor(cxt.Req.Context(), uid); raw != nil {
			for _, k := range []string{"name", "email"} {
				if v, ok := anyField(raw, k); ok {
					m[k] = v
				}
			}
		}
	}
	return m
}

// ---------- store seam (collabhistory pattern) ----------

func (h *Handlers) run(cxt *core.Cxt, fn func(context.Context, persistence.VersionedPersistence) error) error {
	if h.Store != nil {
		return fn(cxt.Req.Context(), h.Store)
	}
	if h.App == nil || h.App.Mongo == nil {
		return errors.New("review: no store configured")
	}
	ctx := cxt.Req.Context()
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
func okJSON(res *core.Res, code int, v any) {
	b, _ := json.Marshal(v)
	res.JSON(code, b)
}

func decodeBody(r io.Reader, dst any) error {
	return json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(dst)
}

// ---------- threads ----------

func (h *Handlers) threadRecord(ctx context.Context, pid, self string, st persistence.VersionedPersistence, th collab.Thread) threadRecord {
	msgs, _ := collab.MessagesOfThread(ctx, st, pid, th.ID)
	o := threadRecord{
		Doc:      th.File,
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
		o.ResolvedAt = isoMS(th.Resolved)
		uidBy := uidOf(by)
		o.ResolvedByUID = uidBy
		o.ResolvedByUser = h.userShape(ctx, uidBy, self)
	}
	return o
}

// threadsList — GET /project/:pid/threads → Record<threadId, Thread> (the
// panel `setData(data)` shape; threads-context.tsx `type Threads`).
func (h *Handlers) threadsList(cxt *core.Cxt, res *core.Res) {
	pid := strings.ToLower(cxt.Params["1"])
	uid, ok := h.gate(cxt, res, pid, collab.ReadOnly)
	if !ok {
		return
	}
	out := map[string]threadRecord{}
	err := h.run(cxt, func(ctx context.Context, st persistence.VersionedPersistence) error {
		ths, err := collab.ListThreads(ctx, st, pid)
		if err != nil {
			return err
		}
		for _, th := range ths {
			if th.ID == "" {
				continue
			}
			out[th.ID] = h.threadRecord(ctx, pid, uid, st, th)
		}
		return nil
	})
	if err != nil {
		internal(res)
		return
	}
	okJSON(res, http.StatusOK, out)
}

// messageAdd — POST /project/:pid/thread/:threadId/messages.
// Panel addComment/addMessage: body {content, id?, doc?}. The FIRST message
// for a thread creates the thread (the panel generates the thread id via
// RangesTracker.generateId and posts the message directly).
func (h *Handlers) messageAdd(cxt *core.Cxt, res *core.Res) {
	pid := strings.ToLower(cxt.Params["1"])
	threadID := cxt.Params["2"]
	uid, ok := h.gate(cxt, res, pid, collab.ReadWrite)
	if !ok {
		return
	}
	var body struct {
		Content string `json:"content"`
		ID      string `json:"id"`
		Doc     string `json:"doc"`
	}
	if err := decodeBody(cxt.Req.Body, &body); err != nil || strings.TrimSpace(body.Content) == "" {
		badBody(res)
		return
	}
	now := h.nowMS()
	author := h.authorOf(cxt, uid)
	var rec threadRecord
	var created bool
	err := h.run(cxt, func(ctx context.Context, st persistence.VersionedPersistence) error {
		ths, lerr := collab.ListThreads(ctx, st, pid)
		if lerr != nil {
			return lerr
		}
		var th *collab.Thread
		for i := range ths {
			if ths[i].ID == threadID {
				th = &ths[i]
				break
			}
		}
		if th == nil {
			nt, _, _, aerr := collab.AddThread(ctx, st, pid, collab.Thread{
				ID: threadID, File: body.Doc, State: collab.ThreadStateOpened,
				Author: author, Created: now,
			})
			if aerr != nil {
				return aerr
			}
			th = &nt
			created = true
		}
		file := th.File
		if file == "" {
			file = body.Doc
		}
		msg, _, _, merr := collab.AddComment(ctx, st, pid, collab.Comment{
			ID: body.ID, ThreadID: threadID, File: file, Text: body.Content,
			State: "opened", Author: author, Created: now, Edited: now,
		})
		if merr != nil {
			if created {
				return errors.New("message failed after thread creation: " + merr.Error())
			}
			return merr
		}
		rec = h.threadRecord(ctx, pid, uid, st, *th)
		// pinned listener (threads-context.tsx 'new-comment'):
		// (threadId, {content, id, timestamp:ms, user})
		h.relay(ctx, pid, "new-comment", []any{threadID, map[string]any{
			"content":   body.Content,
			"id":        msg.ID,
			"timestamp": now,
			"user": map[string]any{
				"id": uid, "name": h.userName(ctx, uid), "email": h.userEmail(ctx, uid),
			},
		}})
		return nil
	})
	if err != nil {
		internal(res)
		return
	}
	okJSON(res, http.StatusCreated, rec)
}

// messageEdit — POST /project/:pid/thread/:threadId/messages/:cid/edit.
func (h *Handlers) messageEdit(cxt *core.Cxt, res *core.Res) {
	pid := strings.ToLower(cxt.Params["1"])
	threadID, cid := cxt.Params["2"], cxt.Params["3"]
	uid, ok := h.gate(cxt, res, pid, collab.ReadWrite)
	if !ok {
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := decodeBody(cxt.Req.Body, &body); err != nil || strings.TrimSpace(body.Content) == "" {
		badBody(res)
		return
	}
	var out collab.Comment
	err := h.run(cxt, func(ctx context.Context, st persistence.VersionedPersistence) error {
		msgs, err := collab.MessagesOfThread(ctx, st, pid, threadID)
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
		m, _, _, err := collab.EditCommentText(ctx, st, pid, cid, body.Content)
		if err != nil {
			return err
		}
		out = m
		return nil
	})
	if err != nil {
		if errors.Is(err, collab.ErrCommentNotFound) || errors.Is(err, collab.ErrThreadNotFound) {
			notFound(res)
			return
		}
		internal(res)
		return
	}
	// pinned listener 'edit-message': (threadId, commentId, content)
	h.relay(cxt.Req.Context(), pid, "edit-message", []any{threadID, cid, body.Content})
	okJSON(res, http.StatusOK, messageOut{
		Content: body.Content, ID: out.ID, Timestamp: isoMS(out.Edited),
		User: h.userShape(cxt.Req.Context(), uidOf(out.Author), uid), UserID: uidOf(out.Author),
	})
}

func (h *Handlers) messageDelete(cxt *core.Cxt, res *core.Res) {
	pid := strings.ToLower(cxt.Params["1"])
	_ = cxt.Params["2"]
	cid := cxt.Params["3"]
	if _, ok := h.gate(cxt, res, pid, collab.ReadWrite); !ok {
		return
	}
	if err := h.run(cxt, func(ctx context.Context, st persistence.VersionedPersistence) error {
		_, _, err := collab.DeleteComment(ctx, st, pid, cid)
		return err
	}); err != nil {
		internal(res)
		return
	}
	h.relay(cxt.Req.Context(), pid, "delete-message", []any{cxt.Params["2"], cid})
	okJSON(res, http.StatusOK, map[string]any{})
}

// ownMessageDelete — panel's own-message rule: only the author may use the
// own-message route (others use the plain messages route at write role).
func (h *Handlers) ownMessageDelete(cxt *core.Cxt, res *core.Res) {
	pid := strings.ToLower(cxt.Params["1"])
	threadID, cid := cxt.Params["2"], cxt.Params["3"]
	uid, ok := h.gate(cxt, res, pid, collab.ReadWrite)
	if !ok {
		return
	}
	owner := ""
	err := h.run(cxt, func(ctx context.Context, st persistence.VersionedPersistence) error {
		msgs, err := collab.MessagesOfThread(ctx, st, pid, threadID)
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
	})
	if err != nil {
		if errors.Is(err, collab.ErrCommentNotFound) || errors.Is(err, collab.ErrThreadNotFound) {
			notFound(res)
			return
		}
		internal(res)
		return
	}
	if owner != "" && owner != uid {
		forbidden(res)
		return
	}
	if err := h.run(cxt, func(ctx context.Context, st persistence.VersionedPersistence) error {
		_, _, err := collab.DeleteComment(ctx, st, pid, cid)
		return err
	}); err != nil {
		internal(res)
		return
	}
	h.relay(cxt.Req.Context(), pid, "delete-message", []any{threadID, cid})
	okJSON(res, http.StatusOK, map[string]any{})
}

// threadResolve — POST .../doc/:doc/thread/:id/resolve | /reopen (200 = the
// updated thread record, the pinned shape).
func (h *Handlers) threadResolve(cxt *core.Cxt, res *core.Res) {
	pid := strings.ToLower(cxt.Params["1"])
	threadID := cxt.Params["3"]
	action := cxt.Params["4"]
	uid, ok := h.gate(cxt, res, pid, collab.ReadWrite)
	if !ok {
		return
	}
	state := collab.ThreadStateResolved
	if action == "reopen" {
		state = collab.ThreadStateOpened
	}
	var rec threadRecord
	err := h.run(cxt, func(ctx context.Context, st persistence.VersionedPersistence) error {
		th, _, _, err := collab.SetThreadStateBy(ctx, st, pid, threadID, state, h.authorOf(cxt, uid))
		if err != nil {
			return err
		}
		rec = h.threadRecord(ctx, pid, uid, st, th)
		return nil
	})
	if err != nil {
		if errors.Is(err, collab.ErrThreadNotFound) {
			notFound(res)
			return
		}
		internal(res)
		return
	}
	// pinned listeners: 'resolve-thread' (threadId, {email, first_name, id})
	// and 'reopen-thread' (threadId).
	if action == "resolve" {
		h.relay(cxt.Req.Context(), pid, "resolve-thread", []any{threadID, map[string]any{
			"email":      h.userEmail(cxt.Req.Context(), uid),
			"first_name": firstName(h.userName(cxt.Req.Context(), uid)),
			"id":         uid,
		}})
	} else {
		h.relay(cxt.Req.Context(), pid, "reopen-thread", []any{threadID})
	}
	okJSON(res, http.StatusOK, rec)
}

func (h *Handlers) threadDelete(cxt *core.Cxt, res *core.Res) {
	pid := strings.ToLower(cxt.Params["1"])
	threadID := cxt.Params["3"]
	if _, ok := h.gate(cxt, res, pid, collab.ReadWrite); !ok {
		return
	}
	if err := h.run(cxt, func(ctx context.Context, st persistence.VersionedPersistence) error {
		_, _, err := collab.DeleteThread(ctx, st, pid, threadID)
		return err
	}); err != nil {
		internal(res)
		return
	}
	// pinned listener 'delete-thread': (threadId)
	h.relay(cxt.Req.Context(), pid, "delete-thread", []any{threadID})
	okJSON(res, http.StatusOK, map[string]any{})
}

// ---------- track changes ----------

// trackChanges — POST /project/:pid/track_changes with the panel body
// {on_for?, on_for_guests?} (track-changes-state-context.ts). Normalizes to
// the explicit map (Node parity: project.track_changes → editor
// trackChangesState) and persists the whole map on the project.
func (h *Handlers) trackChanges(cxt *core.Cxt, res *core.Res) {
	pid := strings.ToLower(cxt.Params["1"])
	if _, ok := h.gate(cxt, res, pid, collab.ReadOnly); !ok {
		return
	}
	var body struct {
		OnFor       map[string]json.RawMessage `json:"on_for"`
		OnForGuests *bool                      `json:"on_for_guests"`
	}
	ctx := cxt.Req.Context()
	m := map[string]bool{}
	if h.TrackStateFor != nil {
		if cur, err := h.TrackStateFor(ctx, pid); err == nil {
			m = cur
		}
	}
	touched := false
	if err := decodeBody(cxt.Req.Body, &body); err == nil {
		for uid, raw := range body.OnFor {
			var b bool
			if json.Unmarshal(raw, &b) == nil {
				m[uid] = b
				touched = true
			}
		}
		if body.OnForGuests != nil {
			m["__guests__"] = *body.OnForGuests
			touched = true
		}
	}
	if touched && h.TrackStateSet != nil {
		if err := h.TrackStateSet(ctx, pid, m); err != nil {
			internal(res)
			return
		}
	}
	// pinned listener 'toggle-track-changes': (false | TrackChangesStateData)
	var state any
	if len(m) > 0 {
		state = m
	} else {
		state = false
	}
	h.relay(cxt.Req.Context(), pid, "toggle-track-changes", []any{state})
	okJSON(res, http.StatusOK, struct {
		ProjectID    string          `json:"project_id"`
		TrackChanges map[string]bool `json:"track_changes"`
	}{pid, m})
}

// ---------- changes ----------

// changesCreate — POST /project/:pid/doc/:doc/changes. Server-assisted
// change creation (D40-d5: editor-side creation is pending; the panel flow
// for CURRENT-doc changes is client-transaction + REST state-sync per the
// D40 block note — this route serves the deterministic server path, e2e, and
// the pre-editor-integration flow). Body {content?, start, end?, kind?,
// change_id?}: non-empty content → insert; else delete.
func (h *Handlers) changesCreate(cxt *core.Cxt, res *core.Res) {
	pid := strings.ToLower(cxt.Params["1"])
	doc := cxt.Params["2"]
	uid, ok := h.gate(cxt, res, pid, collab.ReadWrite)
	if !ok {
		return
	}
	var body struct {
		Content  string `json:"content"`
		Start    int    `json:"start"`
		End      int    `json:"end"`
		Kind     string `json:"kind"`
		ChangeID string `json:"change_id"`
	}
	if err := decodeBody(cxt.Req.Body, &body); err != nil {
		badBody(res)
		return
	}
	if body.End < body.Start {
		body.End = body.Start
	}
	kind := body.Kind
	if kind == "" {
		if strings.TrimSpace(body.Content) != "" {
			kind = collab.ChangeKindInsert
		} else {
			kind = collab.ChangeKindDelete
		}
	}
	now := h.nowMS()
	var out collab.TrackedChange
	err := h.run(cxt, func(ctx context.Context, st persistence.VersionedPersistence) error {
		ch, _, _, err := collab.AddChange(ctx, st, pid, collab.TrackedChange{
			ID: body.ChangeID, Kind: kind, File: doc,
			Start: body.Start, End: body.End, Content: body.Content,
			Author: h.authorOf(cxt, uid), Created: now, State: collab.ChangeStatePending,
		})
		if err != nil {
			return err
		}
		out = ch
		return nil
	})
	if err != nil {
		internal(res)
		return
	}
	okJSON(res, http.StatusCreated, map[string]any{
		"change_id": out.ID,
		"kind":      out.Kind,
		"start":     out.Start,
		"end":       out.End,
		"content":   out.Content,
		"state":     out.State,
	})
}

func (h *Handlers) changesAccept(cxt *core.Cxt, res *core.Res) {
	pid := strings.ToLower(cxt.Params["1"])
	if _, ok := h.gate(cxt, res, pid, collab.ReadWrite); !ok {
		return
	}
	var body struct {
		ChangeIDs []string `json:"change_ids"`
	}
	if err := decodeBody(cxt.Req.Body, &body); err != nil || len(body.ChangeIDs) == 0 {
		badBody(res)
		return
	}
	accepted := 0
	if err := h.run(cxt, func(ctx context.Context, st persistence.VersionedPersistence) error {
		for _, id := range body.ChangeIDs {
			if _, applied, _, err := collab.AcceptChange(ctx, st, pid, id); err == nil && applied {
				accepted++
			}
		}
		return nil
	}); err != nil {
		internal(res)
		return
	}
	// pinned listener 'accept-changes' (ranges-context.tsx): (docId, entryIds)
	h.relay(cxt.Req.Context(), pid, "accept-changes", []any{cxt.Params["2"], body.ChangeIDs})
	okJSON(res, http.StatusOK, struct {
		ProjectID string `json:"project_id"`
		Accepted  int    `json:"accepted"`
	}{pid, accepted})
}

// firstName — resolve-thread pinned user shape uses first_name only.
func firstName(full string) string {
	sp := strings.Fields(full)
	if len(sp) == 0 {
		return ""
	}
	return sp[0]
}

// ---------- production seam: room-event relay (realtime bus :3026) ----------

// prodEmit — Node web → real-time HttpApiController.sendMessage → LB
// emitToRoom, pinned as bus SendRoomMessage (POST /project/:pid/message/:name,
// body = args array).
func prodEmit(baseURL string) Emit {
	if baseURL == "" {
		baseURL = "http://127.0.0.1:3026"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	return func(ctx context.Context, pid, name string, args []any) error {
		b, err := json.Marshal(args)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			strings.TrimSuffix(baseURL, "/")+"/project/"+pid+"/message/"+name,
			strings.NewReader(string(b)))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		if resp.StatusCode >= 300 {
			return fmt.Errorf("realtime bus %s: %s", req.URL.Path, resp.Status)
		}
		return nil
	}
}

// ---------- production seam: track-changes map on the projects doc ----------

func prodTrack(a *core.App) (TrackStateFor, TrackStateSet) {
	trFn := func(ctx context.Context, pid string) (map[string]bool, error) {
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			return nil, err
		}
		var doc struct {
			TrackChanges any `bson:"track_changes"`
		}
		err = db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: oid(pid)}}).Decode(&doc)
		if err != nil {
			if errors.Is(err, mongo.ErrNoDocuments) {
				return map[string]bool{}, nil
			}
			return nil, err
		}
		m := map[string]bool{}
		switch v := doc.TrackChanges.(type) {
		case map[string]any:
			for k, av := range v {
				if b, ok := av.(bool); ok {
					m[k] = b
				}
			}
		case bool:
			if v {
				m["__all__"] = true
			}
		}
		return m, nil
	}
	setFn := func(ctx context.Context, pid string, m map[string]bool) error {
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			return err
		}
		bm := map[string]bool(m)
		_, err = db.Collection("projects").UpdateOne(ctx,
			bson.D{{Key: "_id", Value: oid(pid)}},
			bson.D{{Key: "$set", Value: bson.D{{Key: "track_changes", Value: bm}}}})
		return err
	}
	return trFn, setFn
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
		return nil, err
	}
	return d, nil
}
