// Package rangestracker is the 1:1 drop-in Go port of `libraries/ranges-tracker`
// (npm `@overleaf/ranges-tracker`): the ShareJS-style op bookkeeping that keeps
// comments and tracked changes in sync with the operations applied to a
// document.
//
// Node wire shape (1:1):
//
//	op:        {i, p} | {d, p} | {c, p, t}   (insert | delete | comment)
//	change:    {id, op, metadata}
//	comment:   {id, op, metadata}
//	ranges:    {changes: [...], comments: [...]}
//
// Go conventions (documented deltas):
//
//   - Node duck-types ops by the present key (`op.i`/`op.d`/`opt.c`). Go keeps
//     one *Op per kind via pointer fields: applyOp dispatches on I, then D,
//     then C (Node's order); the first non-nil field wins.
//   - Node stores `changes`/`comments` as arrays of OBJECTS and hands those
//     references out (mutating them in place). Go stores live *Change/*Comment
//     pointers internally — `Changes`/`Comments` slices are exported as pointer
//     slices and dirty-state entries are live pointers, so mutation is
//     observable exactly as on Node. Value-based boundaries (New, GetChanges)
//     copy on the way in/out.
//   - Node `undefined` user_id (absent key) === `undefined` (JS strict) is
//     TRUE for the same-user merge checks; Go's (present, nil) vs absent is
//     replicated by sameUser() below (absent≅absent → true, absent-vs-null →
//     false, matching JS ===).
//   - `metadata.ts` accepts time.Time, *time.Time, RFC3339 strings (Node:
//     Date | ISO string) for the pickTimestamp comparisons.
//   - Node throws on a comment op with `c: undefined` (TypeError on
//     `.length`); Go treats a nil C as "" (unreachable for schema-valid data).
//   - Seed/increment id generation is non-deterministic on both sides (18-char
//     hex seed + 6-char hex increment), byte-shape 1:1.
package rangestracker

import (
	"errors"
	"math/rand/v2"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Metadata is the change/comment metadata object (Node: plain object,
// {user_id, ts, ...}).
type Metadata map[string]any

// Op is one ShareJS-style op. Pointer fields mark presence (Node duck-types
// on present keys). U is the undo flag; FixedRemoveChange / OrderedRejections
// are legacy wire-only flags carried by old tracked change ops (schemas.js);
// Resolved is the history-restore comment flag.
type Op struct {
	I                 *string
	D                 *string
	C                 *string
	T                 *string `json:"t,omitempty"`
	P                 int
	U                 *bool
	FixedRemoveChange *bool
	OrderedRejections *bool
	Resolved          *bool
}

// InsertOp builds {i, p} (optionally {u}).
func InsertOp(i string, p int, u *bool) *Op {
	return &Op{I: &i, P: p, U: u}
}

// DeleteOp builds {d, p} (optionally {u}).
func DeleteOp(d string, p int, u *bool) *Op {
	return &Op{D: &d, P: p, U: u}
}

// CommentOp builds {c, p, t}.
func CommentOp(c string, p int, t string) *Op {
	return &Op{C: &c, P: p, T: &t}
}

// Change is one tracked change record {id, op, metadata}.
type Change struct {
	ID       string
	Op       Op
	Metadata Metadata
}

// CommentItem is one comment record {id, op, metadata}.
// (Named CommentItem to keep the package importable without a `Comment`
// clash with the wire `comment.op` vocabulary.)
type CommentItem struct {
	ID       string
	Op       Op
	Metadata Metadata
}

// RangeRef is one dirty-state entry: a live reference to the tracked change
// or comment that was moved/removed/added (Node: the object reference,
// keyed by object.id).
type RangeRef struct {
	ID      string
	Change  *Change
	Comment *CommentItem
}

// Dirty is the per-type dirty bucket {moved, removed, added}, keyed by id.
type Dirty struct {
	Moved   map[string]*RangeRef
	Removed map[string]*RangeRef
	Added   map[string]*RangeRef
}

// DirtyState is the RangesTracker._dirtyState
// ({comment:{moved,removed,added}, change:{moved,removed,added}}).
type DirtyState struct {
	Comment Dirty
	Change  Dirty
}

func newDirtyState() *DirtyState {
	return &DirtyState{
		Comment: Dirty{Moved: map[string]*RangeRef{}, Removed: map[string]*RangeRef{}, Added: map[string]*RangeRef{}},
		Change:  Dirty{Moved: map[string]*RangeRef{}, Removed: map[string]*RangeRef{}, Added: map[string]*RangeRef{}},
	}
}

// RangesTracker is the 1:1 port of the Node RangesTracker class.
type RangesTracker struct { //nolint:golint // named API parity
	// Changes and Comments are exported (Node: .changes / .comments, mutated
	// in place; Go: live pointer elements so mutations stay observable).
	Changes      []*Change
	Comments     []*CommentItem
	TrackChanges bool // Node: .track_changes

	idSeed      string
	idIncrement int
	dirtyState  *DirtyState
}

// New mirrors `new RangesTracker(changes, comments)` (both nil → empty).
func New(changes []Change, comments []CommentItem) *RangesTracker {
	rt := &RangesTracker{
		Changes:      make([]*Change, 0, len(changes)),
		Comments:     make([]*CommentItem, 0, len(comments)),
		dirtyState:   newDirtyState(),
		idSeed:       GenerateIdSeed(),
		idIncrement:  0,
		TrackChanges: false,
	}
	for i := range changes {
		rt.Changes = append(rt.Changes, &changes[i])
	}
	for i := range comments {
		rt.Comments = append(rt.Comments, &comments[i])
	}
	return rt
}

// --- id generation (Node: generateIdSeed / generateId / newId) ------------

// GenerateIdSeed generates the first 18 characters of a Mongo ObjectId style
// seed: 8 hex timestamp (seconds, 32-bit) + 6 hex machine (24-bit) + 4 hex
// pid (14-bit). Non-deterministic (Node: Math.random + Date.now()).
func GenerateIdSeed() string {
	timestamp := uint32(time.Now().Unix())
	machine := rand.Uint32() & 0xFFFFFF // floor(random * 16777216)
	pid := rand.Uint32() & 0x7FFF       // floor(random * 32767)
	return hexPadded(uint64(timestamp), 8) + hexPadded(uint64(machine), 6) + hexPadded(uint64(pid), 4)
}

func hexPadded(v uint64, width int) string {
	s := strconv.FormatUint(v, 16)
	if len(s) >= width {
		return s
	}
	return strings.Repeat("0", width-len(s)) + s
}

// GenerateId mirrors RangesTracker.generateId(): seed + '000001'.
func GenerateId() string { return GenerateIdSeed() + "000001" }

// GetIdSeed mirrors getIdSeed().
func (rt *RangesTracker) GetIdSeed() string { return rt.idSeed }

// SetIdSeed mirrors setIdSeed(seed): sets the seed and RESETS the increment.
func (rt *RangesTracker) SetIdSeed(seed string) {
	rt.idSeed = seed
	rt.idIncrement = 0
}

// NewId mirrors newId(): seed + zero-padded 6-char hex increment (the Node
// substr(0, 6-n) pad collapses to ” when the increment exceeds 6 hex chars —
// replicated: no truncation of the increment itself).
func (rt *RangesTracker) NewId() string {
	rt.idIncrement++
	inc := strconv.FormatUint(uint64(rt.idIncrement), 16)
	pad := 6 - len(inc)
	if pad < 0 {
		pad = 0
	}
	return rt.idSeed + strings.Repeat("0", pad) + inc
}

// --- comment / change lookups + removals (Node: 1:1) ----------------------

// GetComment mirrors getComment(): first id match, nil if none.
func (rt *RangesTracker) GetComment(commentID string) *CommentItem {
	for _, c := range rt.Comments {
		if c.ID == commentID {
			return c
		}
	}
	return nil
}

// RemoveCommentId mirrors removeCommentId(): drops every comment with the id
// and marks the FIRST match as removed (Node marks the matched comment).
func (rt *RangesTracker) RemoveCommentId(commentID string) {
	comment := rt.GetComment(commentID)
	if comment == nil {
		return
	}
	kept := rt.Comments[:0]
	for _, c := range rt.Comments {
		if c.ID != commentID {
			kept = append(kept, c)
		}
	}
	rt.Comments = kept
	rt.markAsDirty(comment, "comment", "removed")
}

// MoveCommentId mirrors moveCommentId(): updates EVERY comment with the id
// (Node: no break) and marks each as moved.
func (rt *RangesTracker) MoveCommentId(commentID string, position int, text string) {
	for _, comment := range rt.Comments {
		if comment.ID == commentID {
			comment.Op.P = position
			comment.Op.C = &text
			rt.markAsDirty(comment, "comment", "moved")
		}
	}
}

// GetChange mirrors getChange(): first id match, nil if none.
func (rt *RangesTracker) GetChange(changeID string) *Change {
	for _, c := range rt.Changes {
		if c.ID == changeID {
			return c
		}
	}
	return nil
}

// GetChanges mirrors getChanges(ids): all changes whose id is in ids
// (order = current changes order).
func (rt *RangesTracker) GetChanges(ids []string) []Change {
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	out := make([]Change, 0)
	for _, change := range rt.Changes {
		if idSet[change.ID] {
			out = append(out, *change)
		}
	}
	return out
}

// RemoveChangeId mirrors removeChangeId().
func (rt *RangesTracker) RemoveChangeId(changeID string) { rt.RemoveChangeIds([]string{changeID}) }

// RemoveChangeIds mirrors removeChangeIds(): drops every change with any of
// the ids (early return when ids is empty) and marks each removed.
func (rt *RangesTracker) RemoveChangeIds(ids []string) {
	if len(ids) == 0 {
		return
	}
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	kept := rt.Changes[:0]
	for _, change := range rt.Changes {
		if idSet[change.ID] {
			rt.markAsDirty(change, "change", "removed")
		} else {
			kept = append(kept, change)
		}
	}
	rt.Changes = kept
}

// Validate mirrors validate(text): every tracked insert's text must appear at
// its position, every comment's content at its position.
func (rt *RangesTracker) Validate(text string) error {
	for _, change := range rt.Changes {
		if change.Op.I != nil {
			i := *change.Op.I
			if !validSlice(text, change.Op.P, i) {
				return errors.New("insertion does not match text in document")
			}
		}
	}
	for _, comment := range rt.Comments {
		c := commentText(comment)
		if !validSlice(text, comment.Op.P, c) {
			return errors.New("comment does not match text in document")
		}
	}
	return nil
}

func validSlice(text string, p int, content string) bool {
	if p < 0 || p+len(content) > len(text) {
		// Node slice() truncates out-of-range; the content could not match.
		return false
	}
	return text[p:p+len(content)] == content
}

func commentText(comment *CommentItem) string {
	if comment.Op.C != nil {
		return *comment.Op.C
	}
	return ""
}

// --- applyOp (Node: applyOp / applyOps / addComment) ----------------------

// ErrUnknownOp mirrors `throw new Error('unknown op type')`.
var ErrUnknownOp = errors.New("unknown op type")

// ApplyOp mirrors applyOp(op, metadata): dispatches insert / delete / comment
// ops. Node defaults metadata.ts to `new Date()` when absent — replicated.
// metadata may be nil (Node: null → {}).
func (rt *RangesTracker) ApplyOp(op *Op, metadata Metadata) error {
	if metadata == nil {
		metadata = Metadata{}
	}
	if metadata["ts"] == nil {
		metadata["ts"] = time.Now()
	}
	if op.I != nil {
		rt.applyInsertToChanges(op, metadata)
		rt.ApplyInsertToComments(op)
	} else if op.D != nil {
		if err := rt.applyDeleteToChanges(op, metadata); err != nil {
			return err
		}
		return rt.ApplyDeleteToComments(op)
	} else if op.C != nil {
		rt.AddComment(op, metadata)
	} else {
		return ErrUnknownOp
	}
	return nil
}

// ApplyOps mirrors applyOps(ops, metadata): same shared metadata object for
// every op (Node hands the same reference to each applyOp).
func (rt *RangesTracker) ApplyOps(ops []*Op, metadata Metadata) error {
	if metadata == nil {
		metadata = Metadata{}
	}
	for _, op := range ops {
		if err := rt.ApplyOp(op, metadata); err != nil {
			return err
		}
	}
	return nil
}

// AddComment mirrors addComment(op, metadata): an existing comment with the
// op's thread id is MOVED; otherwise a new comment is added (id = op.t or a
// fresh NewId()).
func (rt *RangesTracker) AddComment(op *Op, metadata Metadata) {
	if op.T != nil {
		if existing := rt.GetComment(*op.T); existing != nil {
			rt.MoveCommentId(*op.T, op.P, commentTextFromOp(op))
			return
		}
	}
	id := rt.NewId()
	if op.T != nil && *op.T != "" {
		// Node: op.t || this.newId() — empty string is falsy → fresh id
		id = *op.T
	}
	comment := &CommentItem{
		ID: id,
		// Copy because we'll modify in place (Node: {c, p, t} reference copy).
		Op: Op{C: op.C, P: op.P, T: op.T},
		// Node stores the metadata reference as-is (possibly {}).
		Metadata: metadata,
	}
	rt.Comments = append(rt.Comments, comment)
	rt.markAsDirty(comment, "comment", "added")
}

func commentTextFromOp(op *Op) string {
	if op.C != nil {
		return *op.C
	}
	return ""
}

// --- comment op bookkeeping (Node: applyInsertToComments / applyDeleteToComments) —

// ApplyInsertToComments mirrors applyInsertToComments(op): insert at/before a
// comment shifts it; insert inside extends its content at the offset (Node's
// quirky `slice(0, +(offset-1)+1 || undefined)` reduces to the plain
// at-offset splice).
func (rt *RangesTracker) ApplyInsertToComments(op *Op) {
	for _, comment := range rt.Comments {
		c := commentText(comment)
		if op.P <= comment.Op.P {
			comment.Op.P += len(*op.I)
			rt.markAsDirty(comment, "comment", "moved")
		} else if op.P < comment.Op.P+len(c) {
			offset := op.P - comment.Op.P
			comment.Op.C = ptrTo(c[:offset] + *op.I + c[offset:])
			rt.markAsDirty(comment, "comment", "moved")
		}
	}
}

// ErrDeletedCommentMismatch mirrors `throw new Error('deleted content does
// not match comment content')`.
var ErrDeletedCommentMismatch = errors.New("deleted content does not match comment content")

// ApplyDeleteToComments mirrors applyDeleteToComments(op).
func (rt *RangesTracker) ApplyDeleteToComments(op *Op) error {
	opStart := op.P
	opLength := len(*op.D)
	opEnd := opStart + opLength
	for _, comment := range rt.Comments {
		c := commentText(comment)
		commentStart := comment.Op.P
		commentEnd := commentStart + len(c)
		commentLength := len(c)
		if opEnd <= commentStart {
			// delete is fully before comment
			comment.Op.P -= opLength
			rt.markAsDirty(comment, "comment", "moved")
		} else if opStart >= commentEnd {
			// delete is fully after comment, nothing to do
		} else {
			// delete and comment overlap
			var remainingBefore, remainingAfter string
			if opStart <= commentStart {
				remainingBefore = ""
			} else {
				remainingBefore = c[:opStart-commentStart]
			}
			if opEnd >= commentEnd {
				remainingAfter = ""
			} else {
				remainingAfter = c[opEnd-commentStart:]
			}

			// Check deleted content matches delete op
			deletedComment := c[len(remainingBefore) : commentLength-len(remainingAfter)]
			offset := commentStart - opStart
			if offset < 0 {
				offset = 0
			}
			deletedOpContent := (*op.D)[offset : offset+len(deletedComment)]
			if deletedComment != deletedOpContent {
				return ErrDeletedCommentMismatch
			}

			comment.Op.P = minInt(commentStart, opStart)
			comment.Op.C = ptrTo(remainingBefore + remainingAfter)
			rt.markAsDirty(comment, "comment", "moved")
		}
	}
	return nil
}

// --- dirty state (Node: _markAsDirty / resetDirtyState / getDirtyState) ---

func (rt *RangesTracker) markAsDirty(object any, class, action string) {
	var bucket *Dirty
	var ref *RangeRef
	switch o := object.(type) {
	case *Change:
		bucket = &rt.dirtyState.Change
		ref = &RangeRef{ID: o.ID, Change: o}
	case *CommentItem:
		bucket = &rt.dirtyState.Comment
		ref = &RangeRef{ID: o.ID, Comment: o}
	default:
		panic("rangestracker: markAsDirty: " + class)
	}
	switch action {
	case "moved":
		bucket.Moved[ref.ID] = ref
	case "removed":
		bucket.Removed[ref.ID] = ref
	case "added":
		bucket.Added[ref.ID] = ref
	}
}

// ResetDirtyState mirrors resetDirtyState().
func (rt *RangesTracker) ResetDirtyState() { rt.dirtyState = newDirtyState() }

// GetDirtyState mirrors getDirtyState() — the LIVE state (Node returns the
// internal object reference).
func (rt *RangesTracker) GetDirtyState() *DirtyState { return rt.dirtyState }

// GetTrackedDeletesLength mirrors getTrackedDeletesLength().
func (rt *RangesTracker) GetTrackedDeletesLength() int {
	length := 0
	for _, change := range rt.Changes {
		if change.Op.D != nil {
			length += len(*change.Op.D)
		}
	}
	return length
}

// --- pickTimestamp (Node module-level function, 1:1) ----------------------

// pickTimestamp mirrors the Node pickTimestamp(oldMetadata, newMetadata):
// null/undefined on either side → the other side's ts; otherwise the EARLIER
// date (strict <: a tie keeps the NEW ts — Node `old < new ? old : new`).
//
// ts values: time.Time / *time.Time / RFC3339 strings (Node: Date | string).
// Unparseable values compare as Invalid Date in Node (comparison false →
// the new side wins) — replicated.
func pickTimestamp(oldMetadata, newMetadata Metadata) any {
	oldTS, oldOK := metadataTS(oldMetadata)
	newTS, newOK := metadataTS(newMetadata)
	if !oldOK || oldTS == nil {
		return newTS
	}
	if !newOK || newTS == nil {
		return oldTS
	}
	oldTime, oldValid := tsToTime(oldTS)
	newTime, newValid := tsToTime(newTS)
	if oldValid && newValid {
		if oldTime.Before(newTime) {
			return oldTS
		}
		return newTS
	}
	// Node: new Date(garbage) → Invalid Date; any comparison is false, so
	// `old < new ? old : new` returns the new ts.
	return newTS
}

func metadataTS(m Metadata) (any, bool) {
	if m == nil {
		return nil, false
	}
	v, ok := m["ts"]
	return v, ok
}

func tsToTime(v any) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t, true
	case *time.Time:
		if t == nil {
			return time.Time{}, false
		}
		return *t, true
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, t)
		if err != nil {
			parsed, err = time.Parse(time.RFC3339, t)
			if err != nil {
				return time.Time{}, false
			}
		}
		return parsed, true
	}
	return time.Time{}, false
}

// --- helpers ---------------------------------------------------------------

func ptrTo(s string) *string { return &s }

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// sameUser mirrors Node `metadata.user_id === change.metadata.user_id` with
// JS strict-equality semantics on (absent | null | value): absent===absent
// true, null===null true, absent===null false, values === by identity/equality.
func sameUser(a, b Metadata) bool {
	va, aOK := a["user_id"]
	vb, bOK := b["user_id"]
	if !aOK && !bOK {
		return true
	}
	if aOK != bOK {
		return false // JS: undefined !== null
	}
	if va == nil && vb == nil {
		return true
	}
	return reflect.DeepEqual(va, vb)
}

// sortChangesStable is the _addOp sort: offset ascending, deletes before
// inserts at the same offset, otherwise stable (Node Array.sort is stable in
// V8; sort.SliceStable keeps the same guarantee for equal keys).
func sortChangesStable(changes []*Change) {
	sort.SliceStable(changes, func(ix, iy int) bool {
		a, b := changes[ix], changes[iy]
		if a.Op.P != b.Op.P {
			return a.Op.P < b.Op.P
		}
		switch {
		case a.Op.I != nil && b.Op.D != nil:
			return false // insert after delete
		case a.Op.D != nil && b.Op.I != nil:
			return true // delete before insert
		}
		return false
	})
}
