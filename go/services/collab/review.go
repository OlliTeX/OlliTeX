// review.go — D40 (Y.Doc-native model) P1 foundations: comments + tracked
// changes as FIRST-CLASS shared types in the room doc, alongside
// "content" (Y.Text):
//
//	comments       Y.Array of Y.Map records
//	trackedChanges Y.Array of Y.Map records
//
// The operations follow the exact roomdoc.go seam (any
// persistence.VersionedPersistence; load head → one transaction → append
// the delta as a new version). Consequences:
//
//   - persistence is ZERO new storage — the ygo update log stores the whole
//     doc (content + reviews) in the versions already used by the history
//     REST (D41: history naturally includes comment state);
//   - live WS clients converge on new/changed records through ordinary ygo
//     update delivery (same mechanism as text edits);
//   - concurrent operations on independent records/types merge by CRDT
//     semantics (review_test.go proves order-independence).
//
// Server-authoritative lifecycle (D40 d2): the web surface proposes; these
// ops apply exactly once, IDEMPOTENT on record id (already-applied
// transitions return the current record, applied=false, and append nothing).
//
// Wire shape (Y.Map record keys; nested structures stored as compact JSON
// strings — deterministic round-trip, and the wire contract is JSON anyway;
// record-level concurrency is CRDT, field-level writes are
// last-writer-wins, the same semantics as a Y.Map in Yjs):
//
//	comment: id, file, text, state, author(JSON), created(ms), edited(ms),
//	    ranges(JSON), replies(JSON)
//	change:  id, kind(insert|delete), file, start, end, content,
//	    author(JSON), created(ms), state(pending|accepted|rejected)

package collab

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/reearth/ygo/crdt"
	"github.com/reearth/ygo/persistence"
)

// Top-level Y.Doc type names (client + server agree; a mismatch would
// silently yield empty lists — same warning as roomdoc TextType).
const (
	CommentsType = "comments"
	ChangesType  = "trackedChanges"
)

const (
	ChangeKindInsert = "insert"
	ChangeKindDelete = "delete"

	ChangeStatePending  = "pending"
	ChangeStateAccepted = "accepted"
	ChangeStateRejected = "rejected"
)

var (
	// ErrCommentNotFound — no comment with that id in the room.
	ErrCommentNotFound = errors.New("collab: comment not found")
	// ErrChangeNotFound — no tracked change with that id in the room.
	ErrChangeNotFound = errors.New("collab: change not found")
	// ErrChangeRange — the change's range is outside the current content
	// (drift); P1 is honest about drift (no silent clamping): the caller
	// resolves it (re-anchor / reject-by-id with current coords). P3
	// (relative-position anchors) removes the drift class itself.
	ErrChangeRange = errors.New("collab: change range is outside the current document")
	// ErrChangeKind — mutations are only expressible for the two kinds.
	ErrChangeKind = errors.New("collab: change kind must be insert or delete")
)

// NewReviewID — hex24 record id (unix-seconds big-endian + 8 random bytes),
// matching the Node hex24 comment/change id shape (mongo-style without
// requiring MongoDB objectids — D5).
func NewReviewID(now time.Time) string {
	b := make([]byte, 12)
	t := uint32(now.Unix())
	b[0] = byte(t >> 24)
	b[1] = byte(t >> 16)
	b[2] = byte(t >> 8)
	b[3] = byte(t)
	_, _ = rand.Read(b[4:])
	return hex.EncodeToString(b)
}

// Comment — D40 comment record (P1: Node-contract-compatible shape; the
// exact state values and REST envelope are pinned to the V1 panel contract
// in P2). Ranges are {start,end} pairs (d1: plain; relative-position
// anchors land in P3 once the wire carries them).
type Comment struct {
	ID      string
	File    string // doc path (e.g. "main.tex")
	Text    string
	State   string
	Author  map[string]any // user id / email / name / image (panel shape in P2)
	Ranges  []map[string]any
	Created int64 // ms since epoch
	Edited  int64 // ms since epoch
	Replies []map[string]any
}

// TrackedChange — D40 tracked-change record. Kind/Start/End/Content describe
// the change relative to the CONTENT (Y.Text) — in P1 all tracked changes
// bind to the room's single content doc (d4: text files only).
type TrackedChange struct {
	ID      string
	Kind    string // insert | delete
	File    string
	Start   int
	End     int
	Content string
	Author  map[string]any
	Created int64
	State   string // pending | accepted | rejected
}

// --- record encode/decode ---

func jsonKey(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func mapToComment(m *crdt.YMap) (Comment, error) {
	var c Comment
	c.ID = strVal(m, "id")
	c.File = strVal(m, "file")
	c.Text = strVal(m, "text")
	c.State = strVal(m, "state")
	if raw, ok := m.Get("author"); ok {
		if err := parseJSONVal(raw, &c.Author); err != nil {
			return c, err
		}
	}
	c.Created = int64Val(m, "created")
	c.Edited = int64Val(m, "edited")
	if raw, ok := m.Get("ranges"); ok {
		if err := parseJSONVal(raw, &c.Ranges); err != nil {
			return c, err
		}
	}
	if raw, ok := m.Get("replies"); ok {
		if err := parseJSONVal(raw, &c.Replies); err != nil {
			return c, err
		}
	}
	return c, nil
}

func mapToChange(m *crdt.YMap) (TrackedChange, error) {
	var ch TrackedChange
	ch.ID = strVal(m, "id")
	ch.Kind = strVal(m, "kind")
	ch.File = strVal(m, "file")
	ch.Start = int(int64Val(m, "start"))
	ch.End = int(int64Val(m, "end"))
	ch.Content = strVal(m, "content")
	if raw, ok := m.Get("author"); ok {
		if err := parseJSONVal(raw, &ch.Author); err != nil {
			return ch, err
		}
	}
	ch.Created = int64Val(m, "created")
	ch.State = strVal(m, "state")
	return ch, nil
}

func parseJSONVal(v any, dst any) error {
	if v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	return json.Unmarshal([]byte(s), dst)
}

// --- small ygo value conveniences ---

func int64Val(m *crdt.YMap, key string) int64 {
	v, ok := m.Get(key)
	if !ok || v == nil {
		return 0
	}
	switch x := v.(type) {
	case int:
		return int64(x)
	case int64:
		return x
	case int32:
		return int64(x)
	case float64:
		return int64(x)
	}
	return 0
}

func strVal(m *crdt.YMap, key string) string {
	v, ok := m.Get(key)
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// --- doc load / delta append (roomdoc.go pattern) ---

func loadReviewDoc(ctx context.Context, store persistence.VersionedPersistence, room string) (*crdt.Doc, error) {
	lr, err := store.Load(ctx, room)
	if err != nil {
		return nil, err
	}
	d := newServerDoc()
	if lr.Version > 0 {
		if err := crdt.ApplyUpdateV1(d, lr.Update, nil); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// appendReviewDelta — encode the delta since svBefore and append it. A zero
// delta (idempotent no-op op) appends nothing and returns the current head
// — NO history pollution for already-applied operations.
func appendReviewDelta(ctx context.Context, store persistence.VersionedPersistence, room string, d *crdt.Doc, svBefore crdt.StateVector) (persistence.Version, error) {
	upd := crdt.EncodeStateAsUpdateV1(d, svBefore)
	if len(upd) == 0 {
		lr, err := store.Load(ctx, room)
		if err != nil {
			return 0, err
		}
		return lr.Version, nil
	}
	return store.AppendUpdate(ctx, room, upd)
}

func findRecord(d *crdt.Doc, typeName, id string) (*crdt.YMap, int, bool) {
	a := d.GetArray(typeName)
	for i := 0; i < a.Len(); i++ {
		if m, ok := a.Get(i).(*crdt.YMap); ok && strVal(m, "id") == id {
			return m, i, true
		}
	}
	return nil, -1, false
}

// --- comments ---

// AddComment — append a comment record (server-applied). Idempotent: if a
// record with the same id exists it is returned unchanged, applied=false,
// and nothing is appended. Empty id ⇒ generated (NewReviewID).
func AddComment(ctx context.Context, store persistence.VersionedPersistence, room string, c Comment) (Comment, bool, persistence.Version, error) {
	if c.ID == "" {
		c.ID = NewReviewID(time.Now())
	}
	d, err := loadReviewDoc(ctx, store, room)
	if err != nil {
		return Comment{}, false, 0, err
	}
	if m, _, ok := findRecord(d, CommentsType, c.ID); ok {
		ex, err := mapToComment(m)
		if err != nil {
			return Comment{}, false, 0, err
		}
		lr, _ := store.Load(ctx, room)
		return ex, false, lr.Version, nil
	}
	svBefore := d.StateVector().Clone()
	m := crdt.NewMapPrelim()
	d.Transact(func(txn *crdt.Transaction) {
		commentToMap(c, m, txn)
		txn.GetArray(CommentsType).PushType(txn, m)
	})
	v, err := appendReviewDelta(ctx, store, room, d, svBefore)
	if err != nil {
		return Comment{}, false, 0, err
	}
	return c, true, v, nil
}

// SetCommentState — transition a comment's state (opened/resolved — values
// pinned to the panel contract in P2). Idempotent on the target state.
func SetCommentState(ctx context.Context, store persistence.VersionedPersistence, room, id, state string) (Comment, bool, persistence.Version, error) {
	d, err := loadReviewDoc(ctx, store, room)
	if err != nil {
		return Comment{}, false, 0, err
	}
	m, _, ok := findRecord(d, CommentsType, id)
	if !ok {
		return Comment{}, false, 0, ErrCommentNotFound
	}
	cur, err := mapToComment(m)
	if err != nil {
		return Comment{}, false, 0, err
	}
	if cur.State == state {
		lr, _ := store.Load(ctx, room)
		return cur, false, lr.Version, nil
	}
	svBefore := d.StateVector().Clone()
	cur.State = state
	cur.Edited = max(cur.Edited, time.Now().UnixMilli())
	d.Transact(func(txn *crdt.Transaction) {
		commentToMap(cur, m, txn)
	})
	v, err := appendReviewDelta(ctx, store, room, d, svBefore)
	if err != nil {
		return Comment{}, false, 0, err
	}
	return cur, true, v, nil
}

// EditCommentText — set the comment text (edited=now). Idempotent on equal
// text.
func EditCommentText(ctx context.Context, store persistence.VersionedPersistence, room, id, text string) (Comment, bool, persistence.Version, error) {
	d, err := loadReviewDoc(ctx, store, room)
	if err != nil {
		return Comment{}, false, 0, err
	}
	m, _, ok := findRecord(d, CommentsType, id)
	if !ok {
		return Comment{}, false, 0, ErrCommentNotFound
	}
	cur, err := mapToComment(m)
	if err != nil {
		return Comment{}, false, 0, err
	}
	if cur.Text == text {
		lr, _ := store.Load(ctx, room)
		return cur, false, lr.Version, nil
	}
	svBefore := d.StateVector().Clone()
	cur.Text = text
	cur.Edited = time.Now().UnixMilli()
	d.Transact(func(txn *crdt.Transaction) {
		commentToMap(cur, m, txn)
	})
	v, err := appendReviewDelta(ctx, store, room, d, svBefore)
	if err != nil {
		return Comment{}, false, 0, err
	}
	return cur, true, v, nil
}

// AddCommentReply — append one reply record to a comment (panel shape in P2;
// P1 stores the caller's JSON-serialisable reply map verbatim).
func AddCommentReply(ctx context.Context, store persistence.VersionedPersistence, room, id string, reply map[string]any) (Comment, bool, persistence.Version, error) {
	d, err := loadReviewDoc(ctx, store, room)
	if err != nil {
		return Comment{}, false, 0, err
	}
	m, _, ok := findRecord(d, CommentsType, id)
	if !ok {
		return Comment{}, false, 0, ErrCommentNotFound
	}
	cur, err := mapToComment(m)
	if err != nil {
		return Comment{}, false, 0, err
	}
	svBefore := d.StateVector().Clone()
	cur.Replies = append(cur.Replies, reply)
	cur.Edited = time.Now().UnixMilli()
	d.Transact(func(txn *crdt.Transaction) {
		commentToMap(cur, m, txn)
	})
	v, err := appendReviewDelta(ctx, store, room, d, svBefore)
	if err != nil {
		return Comment{}, false, 0, err
	}
	return cur, true, v, nil
}

// DeleteComment — remove a comment record. Idempotent (absent ⇒ no-op).
func DeleteComment(ctx context.Context, store persistence.VersionedPersistence, room, id string) (bool, persistence.Version, error) {
	d, err := loadReviewDoc(ctx, store, room)
	if err != nil {
		return false, 0, err
	}
	_, idx, ok := findRecord(d, CommentsType, id)
	if !ok {
		lr, _ := store.Load(ctx, room)
		return false, lr.Version, nil
	}
	svBefore := d.StateVector().Clone()
	d.Transact(func(txn *crdt.Transaction) {
		txn.GetArray(CommentsType).Delete(txn, idx, 1)
	})
	v, err := appendReviewDelta(ctx, store, room, d, svBefore)
	if err != nil {
		return false, 0, err
	}
	return true, v, nil
}

// ListComments — the room's comments in array (panel) order.
func ListComments(ctx context.Context, store persistence.VersionedPersistence, room string) ([]Comment, error) {
	d, err := loadReviewDoc(ctx, store, room)
	if err != nil {
		return nil, err
	}
	a := d.GetArray(CommentsType)
	out := make([]Comment, 0, a.Len())
	for i := 0; i < a.Len(); i++ {
		m, ok := a.Get(i).(*crdt.YMap)
		if !ok {
			continue
		}
		c, err := mapToComment(m)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// --- tracked changes ---

func commentToMap(c Comment, m *crdt.YMap, txn *crdt.Transaction) {
	m.Set(txn, "id", c.ID)
	m.Set(txn, "file", c.File)
	m.Set(txn, "text", c.Text)
	m.Set(txn, "state", c.State)
	if c.Author != nil {
		if s, err := jsonKey(c.Author); err == nil {
			m.Set(txn, "author", s)
		}
	}
	m.Set(txn, "created", c.Created)
	m.Set(txn, "edited", c.Edited)
	if s, err := jsonKey(c.Ranges); err == nil && s != "null" {
		m.Set(txn, "ranges", s)
	}
	if s, err := jsonKey(c.Replies); err == nil && s != "null" {
		m.Set(txn, "replies", s)
	}
}

// AddChange — register a pending tracked change (the doc text ALREADY reflects
// it — Yjs reality: the edit is in the shared text; the change record is the
// pending marker). Idempotent on id.
func AddChange(ctx context.Context, store persistence.VersionedPersistence, room string, ch TrackedChange) (TrackedChange, bool, persistence.Version, error) {
	if ch.ID == "" {
		ch.ID = NewReviewID(time.Now())
	}
	if ch.Kind != ChangeKindInsert && ch.Kind != ChangeKindDelete {
		return TrackedChange{}, false, 0, ErrChangeKind
	}
	if ch.State == "" {
		ch.State = ChangeStatePending
	}
	d, err := loadReviewDoc(ctx, store, room)
	if err != nil {
		return TrackedChange{}, false, 0, err
	}
	if m, _, ok := findRecord(d, ChangesType, ch.ID); ok {
		ex, err := mapToChange(m)
		if err != nil {
			return TrackedChange{}, false, 0, err
		}
		lr, _ := store.Load(ctx, room)
		return ex, false, lr.Version, nil
	}
	svBefore := d.StateVector().Clone()
	m := crdt.NewMapPrelim()
	d.Transact(func(txn *crdt.Transaction) {
		changeToMap(ch, m, txn)
		txn.GetArray(ChangesType).PushType(txn, m)
	})
	v, err := appendReviewDelta(ctx, store, room, d, svBefore)
	if err != nil {
		return TrackedChange{}, false, 0, err
	}
	return ch, true, v, nil
}

func changeToMap(ch TrackedChange, m *crdt.YMap, txn *crdt.Transaction) {
	m.Set(txn, "id", ch.ID)
	m.Set(txn, "kind", ch.Kind)
	m.Set(txn, "file", ch.File)
	m.Set(txn, "start", ch.Start)
	m.Set(txn, "end", ch.End)
	m.Set(txn, "content", ch.Content)
	if ch.Author != nil {
		if s, err := jsonKey(ch.Author); err == nil {
			m.Set(txn, "author", s)
		}
	}
	m.Set(txn, "created", ch.Created)
	m.Set(txn, "state", ch.State)
}

// setContent — the P1 single-content mutation point (d4: text only).
func setContent(d *crdt.Doc, fn func(t *crdt.YText, txn *crdt.Transaction)) {
	d.Transact(func(txn *crdt.Transaction) {
		fn(txn.GetText(TextType), txn)
	})
}

func changeRangeOK(t *crdt.YText, ch TrackedChange) bool {
	n := int(t.Len())
	return ch.Start >= 0 && ch.End >= ch.Start && ch.End <= n
}

// AcceptChange — pending → accepted. NO content mutation (the edit stands).
// Idempotent: an already-accepted/rejected change returns unchanged.
func AcceptChange(ctx context.Context, store persistence.VersionedPersistence, room, id string) (TrackedChange, bool, persistence.Version, error) {
	d, err := loadReviewDoc(ctx, store, room)
	if err != nil {
		return TrackedChange{}, false, 0, err
	}
	m, _, ok := findRecord(d, ChangesType, id)
	if !ok {
		return TrackedChange{}, false, 0, ErrChangeNotFound
	}
	cur, err := mapToChange(m)
	if err != nil {
		return TrackedChange{}, false, 0, err
	}
	if cur.State != ChangeStatePending {
		lr, _ := store.Load(ctx, room)
		return cur, false, lr.Version, nil
	}
	svBefore := d.StateVector().Clone()
	cur.State = ChangeStateAccepted
	d.Transact(func(txn *crdt.Transaction) {
		changeToMap(cur, m, txn)
	})
	v, err := appendReviewDelta(ctx, store, room, d, svBefore)
	if err != nil {
		return TrackedChange{}, false, 0, err
	}
	return cur, true, v, nil
}

// RejectChange — pending → rejected + CONTENT MUTATION (the only place D40
// mutates the shared text, server-side, exactly once):
//
//	kind=insert → delete [Start,End)   (the inserted text is removed)
//	kind=delete → insert Content at Start (the deleted text is restored)
//
// Idempotent: a second reject mutates NOTHING (applied=false), so racing
// rejects cannot double-restore / double-remove. Drifted ranges (outside the
// current text) fail with ErrChangeRange and leave the change PENDING —
// the caller re-resolves it (P3 anchors remove the drift class).
func RejectChange(ctx context.Context, store persistence.VersionedPersistence, room, id string) (TrackedChange, bool, persistence.Version, error) {
	d, err := loadReviewDoc(ctx, store, room)
	if err != nil {
		return TrackedChange{}, false, 0, err
	}
	m, _, ok := findRecord(d, ChangesType, id)
	if !ok {
		return TrackedChange{}, false, 0, ErrChangeNotFound
	}
	cur, err := mapToChange(m)
	if err != nil {
		return TrackedChange{}, false, 0, err
	}
	if cur.State != ChangeStatePending {
		lr, _ := store.Load(ctx, room)
		return cur, false, lr.Version, nil
	}
	t := d.GetText(TextType)
	if !changeRangeOK(t, cur) {
		return cur, false, 0, ErrChangeRange
	}
	svBefore := d.StateVector().Clone()
	setContent(d, func(txt *crdt.YText, txn *crdt.Transaction) {
		switch cur.Kind {
		case ChangeKindInsert:
			txt.Delete(txn, cur.Start, cur.End-cur.Start)
		case ChangeKindDelete:
			txt.Insert(txn, cur.Start, cur.Content, nil)
		}
	})
	cur.State = ChangeStateRejected
	d.Transact(func(txn *crdt.Transaction) {
		changeToMap(cur, m, txn)
	})
	v, err := appendReviewDelta(ctx, store, room, d, svBefore)
	if err != nil {
		return TrackedChange{}, false, 0, err
	}
	return cur, true, v, nil
}

// ListChanges — the room's tracked changes in array (panel) order.
func ListChanges(ctx context.Context, store persistence.VersionedPersistence, room string) ([]TrackedChange, error) {
	d, err := loadReviewDoc(ctx, store, room)
	if err != nil {
		return nil, err
	}
	a := d.GetArray(ChangesType)
	out := make([]TrackedChange, 0, a.Len())
	for i := 0; i < a.Len(); i++ {
		m, ok := a.Get(i).(*crdt.YMap)
		if !ok {
			continue
		}
		ch, err := mapToChange(m)
		if err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	return out, nil
}
