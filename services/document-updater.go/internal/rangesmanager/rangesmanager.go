// Package rangesmanager — 1:1 Go port of `app/js/RangesManager.js`
// (overleaf/document-updater).
//
// RangesManager keeps a document's comments and tracked changes in sync with
// the ShareJS-style ops applied by the editor, and emits the history ops that
// the history system needs so it stays consistent with the editor's view.
//
// Node module shape (1:1):
//
//	RangesManager = { MAX_COMMENTS, MAX_CHANGES, applyUpdate, acceptChanges,
//	  deleteComment, getHistoryUpdatesForAcceptedChanges, _getRanges,
//	  _emptyRangesCount }
//
// Go: a struct with the same methods. The Node-level helpers
// (getHistoryOp / getHistoryOpForInsert / getHistoryOpForDelete /
// getHistoryOpForComment / getCroppedCommentOps) become methods on
// *RangesManager.
package rangesmanager

import (
	"errors"
	"sort"
	"strings"
	"time"

	"ollitex/go/libraries/rangestracker"

	"document-updater/internal/utils"
)

// --- errors (Node: throw new Error('...') / throw new OError('...')) --------

// ErrTooManyRanges mirrors `throw new Error('too many comments or tracked
// changes')`.
var ErrTooManyRanges = errors.New("too many comments or tracked changes")

// ErrUnrecognizedOp mirrors `throw new OError('Unrecognized op', {op})` — an
// op that is none of insert/delete/comment (unreachable for schema-valid
// wire ops).
var ErrUnrecognizedOp = errors.New("Unrecognized op")

// --- input types (mirror Node wire types) ----------------------------------

// Ranges mirrors Node `Ranges` — { changes?: TrackedChange[], comments?:
// Comment[] }. Both input (the doc's current comments/changes) and output
// (the new ranges after an update) use this shape; an absent (empty) list is
// omitted from the response.
type Ranges struct {
	Changes  []rangestracker.Change
	Comments []rangestracker.CommentItem
}

// Update mirrors Node `Update` — one editor update {doc, v, op, meta?}.
// Meta is a plain object: { tc?: boolean|string, user_id?, ts? }.
type Update struct {
	Doc              string
	V                *int
	Op               []rangestracker.Op
	Meta             map[string]any
	ProjectHistoryID *string
}

// Opts mirrors Node `opts = {}` ({historyRangesSupport?}).
type Opts struct {
	HistoryRangesSupport bool
}

// AcceptedChangesArgs mirrors the arg object of
// getHistoryUpdatesForAcceptedChanges.
type AcceptedChangesArgs struct {
	DocID             string
	AcceptedChangeIDs []string
	Changes           []rangestracker.Change
	Pathname          string
	ProjectHistoryID  string
	Lines             []string
}

// --- history op wire shape (Node HistoryOp union) --------------------------

// HistoryDeleteTrackedChange mirrors Node `HistoryDeleteTrackedChange`.
type HistoryDeleteTrackedChange struct {
	Type   string // "insert" | "delete"
	Offset int
	Length int
}

// HistoryTracking mirrors `tracking: {type: 'none'}` on history retain ops.
type HistoryTracking struct {
	Type string
}

// HistoryOp mirrors the Node `HistoryOp` union
// (HistoryInsertOp | HistoryDeleteOp | HistoryCommentOp | HistoryRetainOp)
// as a single flat struct; presence is encoded by nil pointer fields (Node:
// key absent). Only the kind's fields are meaningful:
//   - insert:    I, P, U, Hpos, CommentIds, TrackedDeleteRejection
//   - delete:    D, P, U, Hpos, TrackedChanges
//   - comment:   C, P, T, Resolved, Hpos, Hlen
//   - retain:    R, P, Hpos, Tracking
type HistoryOp struct {
	I *string
	D *string
	C *string
	R *string
	T *string
	P int
	U *bool

	Hpos                   *int
	Hlen                   *int
	CommentIds             []string
	TrackedDeleteRejection bool
	TrackedChanges         []HistoryDeleteTrackedChange
	Tracking               *HistoryTracking
	Resolved               bool
}

// HistoryUpdate mirrors Node `HistoryUpdate` — {doc, v?, op:
// HistoryOp[], meta?, projectHistoryId?}. In applyUpdate the meta is a copy
// of the source update meta; in getHistoryUpdatesForAcceptedChanges it is
// rebuilt as {user_id, ts, doc_length, pathname, history_doc_length?} (v absent).
type HistoryUpdate struct {
	Doc              string
	Op               []HistoryOp
	V                *int
	Meta             map[string]any
	ProjectHistoryID *string
}

// ApplyUpdateResult mirrors applyUpdate's return object.
type ApplyUpdateResult struct {
	NewRanges           Ranges
	RangesWereCollapsed bool
	HistoryUpdates      []HistoryUpdate
	RemovedChangeIDs    []string
}

// --- metrics (Node: an injected @overleaf/metrics) --------------------------

// Metrics is the subset of @overleaf/metrics used here.
type Metrics interface {
	Histogram(name string, value int, buckets []int, opts map[string]string)
}

type nullMetrics struct{}

func (nullMetrics) Histogram(string, int, []int, map[string]string) {}

// RangeDeltaBuckets mirrors RANGE_DELTA_BUCKETS.
var RangeDeltaBuckets = []int{0, 1, 2, 3, 4, 5, 10, 20, 50}

// --- the manager ------------------------------------------------------------

// RangesManager is the 1:1 port of the Node RangesManager module object.
type RangesManager struct {
	MaxComments int
	MaxChanges  int
	Metrics     Metrics
	Now         func() int64
}

// New returns a RangesManager with the Node defaults (MAX_COMMENTS=500,
// MAX_CHANGES=2000, no-op metrics, real clock).
func New() *RangesManager {
	return &RangesManager{
		MaxComments: 500,
		MaxChanges:  2000,
		Metrics:     nullMetrics{},
		Now:         func() int64 { return time.Now().UnixMilli() },
	}
}

func (m *RangesManager) now() int64 {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now().UnixMilli()
}

// --- applyUpdate ----------------------------------------------------------

// ApplyUpdate mirrors Node `applyUpdate(projectId, docId, ranges, updates,
// newDocLines, opts)`.
//
// Key snapshot timing (must be preserved): the history op for each op is
// computed from the LIVE tracker state BEFORE that op's applyOp. Cropped
// comment ops (HRS + tracking delete) are computed from the PRE-delete
// comment, but enriched with the LIVE (post-delete) changes.
func (m *RangesManager) ApplyUpdate(projectID, docID string, ranges *Ranges, updates []Update, newDocLines []string, opts Opts) (ApplyUpdateResult, error) {
	_ = projectID
	_ = docID
	if ranges == nil {
		ranges = &Ranges{}
	}
	changes := ranges.Changes
	comments := ranges.Comments
	rangesTracker := rangestracker.New(ensureChanges(changes), ensureComments(comments))
	emptyBefore, totalBefore := emptyRangesCount(rangesTracker)

	var historyUpdates []HistoryUpdate
	for i := range updates {
		update := &updates[i]
		trackingChanges, seed := trackingFlags(update)
		rangesTracker.TrackChanges = trackingChanges
		if seed != nil {
			rangesTracker.SetIdSeed(*seed)
		}
		var historyOps []HistoryOp
		for j := range update.Op {
			op := update.Op[j]
			var cropped []rangestracker.Op
			if opts.HistoryRangesSupport {
				ho, err := m.getHistoryOp(op, rangesTracker.Comments, rangesTracker.Changes)
				if err != nil {
					return ApplyUpdateResult{}, err
				}
				historyOps = append(historyOps, ho)
				if isDeleteOp(op) && trackingChanges {
					// Cropping extends is computed from the PRE-delete comment,
					// but applied (enriched) after the delete is applied.
					cropped = getCroppedCommentOps(op, rangesTracker.Comments)
				}
			} else if isInsertOp(op) || isDeleteOp(op) {
				historyOps = append(historyOps, rawOpToHistoryOp(op))
			}
			if err := rangesTracker.ApplyOp(&update.Op[j], updateApplyMeta(update)); err != nil {
				return ApplyUpdateResult{}, err
			}
			for k := range cropped {
				historyOps = append(historyOps, m.getHistoryOpForComment(cropped[k], rangesTracker.Changes))
			}
		}
		if len(historyOps) > 0 {
			hu := HistoryUpdate{
				Doc:              update.Doc,
				Op:               historyOps,
				V:                update.V,
				Meta:             copyMap(update.Meta),
				ProjectHistoryID: update.ProjectHistoryID,
			}
			historyUpdates = append(historyUpdates, hu)
		}
	}

	if len(rangesTracker.Changes) > m.MaxChanges || len(rangesTracker.Comments) > m.MaxComments {
		return ApplyUpdateResult{}, ErrTooManyRanges
	}

	// Consistency check: all ranges/comments still match the corresponding text.
	if err := rangesTracker.Validate(strings.Join(newDocLines, "\n")); err != nil {
		return ApplyUpdateResult{}, err
	}

	emptyAfter, totalAfter := emptyRangesCount(rangesTracker)
	rangesWereCollapsed := emptyAfter > emptyBefore || totalAfter+1 < totalBefore
	if totalAfter < totalBefore {
		status := "unsaved"
		if rangesWereCollapsed {
			status = "saved"
		}
		m.Metrics.Histogram("range-delta", totalBefore-totalAfter, RangeDeltaBuckets, map[string]string{"status_code": status})
	}
	newRanges := getRanges(rangesTracker)
	removedIDs := objectKeys(rangesTracker.GetDirtyState().Change.Removed)

	return ApplyUpdateResult{
		NewRanges:           newRanges,
		RangesWereCollapsed: rangesWereCollapsed,
		HistoryUpdates:      historyUpdates,
		RemovedChangeIDs:    removedIDs,
	}, nil
}

func ensureChanges(changes []rangestracker.Change) []rangestracker.Change {
	if changes == nil {
		return make([]rangestracker.Change, 0)
	}
	return changes
}

func ensureComments(comments []rangestracker.CommentItem) []rangestracker.CommentItem {
	if comments == nil {
		return make([]rangestracker.CommentItem, 0)
	}
	return comments
}

func copyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// trackingFlags mirrors `Boolean(tc)` / `if (tc) setIdSeed(tc)`. tc may be a
// boolean or (per wire reality) an id-seed string; falsy values disable
// tracking. Mirrors Node JS truthiness ("" / null / undefined / false are
// falsy; anything else is truthy).
func trackingFlags(update *Update) (bool, *string) {
	tc, ok := lookupValue(update.Meta, "tc")
	if !ok || jsFalsy(tc) {
		return false, nil
	}
	seed, isStr := tc.(string)
	if !isStr {
		return true, nil
	}
	return true, &seed
}

// lookupValue mirrors `meta?.k` over Go maps: absent key or nil value is
// falsy either way; present non-nil value is truthy unless "" / false.
func lookupValue(meta map[string]any, key string) (any, bool) {
	v, ok := meta[key]
	if !ok || v == nil {
		return nil, false
	}
	return v, true
}

// jsFalsy mirrors Node falsiness for `any` wire values: null / "" / false / 0.
func jsFalsy(v any) bool {
	switch t := v.(type) {
	case string:
		return t == ""
	case bool:
		return !t
	case nil:
		return true
	default:
		return false
	}
}

// updateApplyMeta mirrors `{user_id: update.meta?.user_id}` — the key is
// ALWAYS present (value nil when absent), matching Node's `undefined` value
// so the tracker's sameUser merge check sees (present, nil) vs (absent, nil)
// exactly like JS ===.
func updateApplyMeta(update *Update) rangestracker.Metadata {
	uid, _ := lookupValue(update.Meta, "user_id")
	return rangestracker.Metadata{"user_id": uid}
}

func isInsertOp(op rangestracker.Op) bool  { return op.I != nil }
func isDeleteOp(op rangestracker.Op) bool  { return op.D != nil }
func isCommentOp(op rangestracker.Op) bool { return op.C != nil }

func commentText(c *rangestracker.CommentItem) string {
	if c.Op.C != nil {
		return *c.Op.C
	}
	return ""
}

func isInsertChange(change *rangestracker.Change) bool { return change.Op.I != nil }

// rawOpToHistoryOp mirrors Node `{...op}` for the non-HRS push (the raw op,
// keys i/d/c/p/u preserved).
func rawOpToHistoryOp(op rangestracker.Op) HistoryOp {
	return HistoryOp{I: op.I, D: op.D, C: op.C, T: op.T, P: op.P, U: op.U}
}

// getHistoryOp mirrors Node getHistoryOp(op, comments, changes).
func (m *RangesManager) getHistoryOp(op rangestracker.Op, comments []*rangestracker.CommentItem, changes []*rangestracker.Change) (HistoryOp, error) {
	if isInsertOp(op) {
		return m.getHistoryOpForInsert(op, comments, changes), nil
	}
	if isDeleteOp(op) {
		return m.getHistoryOpForDelete(op, changes), nil
	}
	if isCommentOp(op) {
		return m.getHistoryOpForComment(op, changes), nil
	}
	return HistoryOp{}, ErrUnrecognizedOp
}

// getHistoryOpForInsert mirrors Node getHistoryOpForInsert.
func (m *RangesManager) getHistoryOpForInsert(op rangestracker.Op, comments []*rangestracker.CommentItem, changes []*rangestracker.Change) HistoryOp {
	hpos := op.P
	tdr := false
	commentIDs := []string{}
	seen := map[string]bool{}
	for _, c := range comments {
		if c.Op.P < op.P && op.P < c.Op.P+len(commentText(c)) {
			t := ""
			if c.Op.T != nil {
				t = *c.Op.T
			}
			if !seen[t] {
				seen[t] = true
				commentIDs = append(commentIDs, t)
			}
		}
	}
	tdrOffset := 0
	for _, change := range changes {
		if change.Op.D == nil {
			continue
		}
		if change.Op.P < op.P {
			hpos += len(*change.Op.D)
		} else if change.Op.P == op.P {
			if op.U != nil && *op.U && hasPrefix(*change.Op.D, *op.I) {
				tdr = true
				hpos += tdrOffset
				break
			}
			tdrOffset += len(*change.Op.D)
		} else {
			break
		}
	}
	ho := HistoryOp{I: op.I, P: op.P, U: op.U}
	if len(commentIDs) > 0 {
		ho.CommentIds = commentIDs
	}
	if hpos != op.P {
		ho.Hpos = intPtr(hpos)
	}
	if tdr {
		ho.TrackedDeleteRejection = true
	}
	return ho
}

func hasPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return s[:len(prefix)] == prefix
}

// getHistoryOpForDelete mirrors Node getHistoryOpForDelete.
func (m *RangesManager) getHistoryOpForDelete(op rangestracker.Op, changes []*rangestracker.Change) HistoryOp {
	hpos := op.P
	deleteLen := len(*op.D)
	opEnd := op.P + deleteLen
	var inside []HistoryDeleteTrackedChange
	for _, change := range changes {
		if change.Op.P <= op.P {
			if change.Op.D != nil {
				hpos += len(*change.Op.D)
			} else if change.Op.I != nil {
				changeEnd := change.Op.P + len(*change.Op.I)
				endPos := min(changeEnd, opEnd)
				if endPos > op.P {
					inside = append(inside, HistoryDeleteTrackedChange{Type: "insert", Offset: 0, Length: endPos - op.P})
				}
			}
		} else if change.Op.P < op.P+deleteLen {
			if change.Op.D != nil {
				inside = append(inside, HistoryDeleteTrackedChange{Type: "delete", Offset: change.Op.P - op.P, Length: len(*change.Op.D)})
			} else if change.Op.I != nil {
				inside = append(inside, HistoryDeleteTrackedChange{Type: "insert", Offset: change.Op.P - op.P, Length: min(len(*change.Op.I), opEnd-change.Op.P)})
			}
		} else {
			break
		}
	}
	ho := HistoryOp{D: op.D, P: op.P, U: op.U}
	if hpos != op.P {
		ho.Hpos = intPtr(hpos)
	}
	if len(inside) > 0 {
		ho.TrackedChanges = inside
	}
	return ho
}

// getHistoryOpForComment mirrors Node getHistoryOpForComment.
func (m *RangesManager) getHistoryOpForComment(op rangestracker.Op, changes []*rangestracker.Change) HistoryOp {
	hpos := op.P
	cLen := 0
	if op.C != nil {
		cLen = len(*op.C)
	}
	hlen := cLen
	for _, change := range changes {
		if change.Op.D == nil {
			continue
		}
		if change.Op.P <= op.P {
			hpos += len(*change.Op.D)
		} else if change.Op.P < op.P+cLen {
			hlen += len(*change.Op.D)
		} else {
			break
		}
	}
	ho := HistoryOp{C: op.C, P: op.P, T: op.T}
	if op.Resolved != nil && *op.Resolved {
		ho.Resolved = true
	}
	if hpos != op.P {
		ho.Hpos = intPtr(hpos)
	}
	if hlen != cLen {
		ho.Hlen = intPtr(hlen)
	}
	return ho
}

// getCroppedCommentOps mirrors Node getCroppedCommentOps(op, comments) — the
// comment-crop ops to send after a tracked delete overlaps a comment.
func getCroppedCommentOps(op rangestracker.Op, comments []*rangestracker.CommentItem) []rangestracker.Op {
	deleteStart := op.P
	deleteLength := len(*op.D)
	deleteEnd := deleteStart + deleteLength

	var out []rangestracker.Op
	for _, comment := range comments {
		commentStart := comment.Op.P
		cLen := len(commentText(comment))
		commentEnd := commentStart + cLen

		if deleteStart <= commentStart && deleteEnd > commentStart {
			overlapLength := min(deleteEnd, commentEnd) - commentStart
			cropC := commentText(comment)[overlapLength:]
			out = append(out, rangestracker.Op{C: &cropC, P: deleteStart, T: comment.Op.T, Resolved: comment.Op.Resolved})
		} else if deleteStart > commentStart && deleteStart < commentEnd && deleteEnd >= commentEnd {
			overlapLength := commentEnd - deleteStart
			cropC := commentText(comment)[:cLen-overlapLength]
			out = append(out, rangestracker.Op{C: &cropC, P: commentStart, T: comment.Op.T, Resolved: comment.Op.Resolved})
		}
	}
	return out
}

// --- acceptChanges / deleteComment ----------------------------------------

// AcceptChanges mirrors Node `acceptChanges(projectId, docId, changeIds,
// ranges, lines)`.
func (m *RangesManager) AcceptChanges(projectID, docID string, changeIDs []string, ranges *Ranges, lines []string) Ranges {
	_ = projectID
	_ = docID
	_ = lines
	if ranges == nil {
		ranges = &Ranges{}
	}
	rangesTracker := rangestracker.New(ensureChanges(ranges.Changes), ensureComments(ranges.Comments))
	rangesTracker.RemoveChangeIds(changeIDs)
	return getRanges(rangesTracker)
}

// DeleteComment mirrors Node `deleteComment(commentId, ranges)`.
func (m *RangesManager) DeleteComment(commentID string, ranges *Ranges) Ranges {
	if ranges == nil {
		ranges = &Ranges{}
	}
	rangesTracker := rangestracker.New(ensureChanges(ranges.Changes), ensureComments(ranges.Comments))
	rangesTracker.RemoveCommentId(commentID)
	return getRanges(rangesTracker)
}

// --- getHistoryUpdatesForAcceptedChanges -----------------------------------

// GetHistoryUpdatesForAcceptedChanges mirrors Node
// getHistoryUpdatesForAcceptedChanges(args).
func (m *RangesManager) GetHistoryUpdatesForAcceptedChanges(args AcceptedChangesArgs) []HistoryUpdate {
	acceptedSet := make(map[string]bool, len(args.AcceptedChangeIDs))
	for _, id := range args.AcceptedChangeIDs {
		acceptedSet[id] = true
	}
	isAccepted := func(change rangestracker.Change) bool {
		return change.ID != "" && acceptedSet[change.ID]
	}

	sorted := make([]rangestracker.Change, len(args.Changes))
	copy(sorted, args.Changes)
	sortChangesByOffset(sorted)

	docLength := utils.GetDocLength(args.Lines)
	historyDocLength := docLength
	for _, change := range sorted {
		if change.Op.D != nil {
			historyDocLength += len(*change.Op.D)
		}
	}

	var historyUpdates []HistoryUpdate
	unacceptedDeletes := 0
	for _, change := range sorted {
		var op HistoryOp
		haveOp := false
		if change.Op.D != nil {
			if isAccepted(change) {
				op = HistoryOp{D: change.Op.D, P: change.Op.P}
				haveOp = true
			} else {
				unacceptedDeletes += len(*change.Op.D)
			}
		} else if change.Op.I != nil {
			if isAccepted(change) {
				r := *change.Op.I
				op = HistoryOp{R: &r, P: change.Op.P, Tracking: &HistoryTracking{Type: "none"}}
				haveOp = true
			}
		}
		if !haveOp {
			continue
		}
		if unacceptedDeletes > 0 {
			op.Hpos = intPtr(op.P + unacceptedDeletes)
		}
		meta := map[string]any{}
		for k, v := range change.Metadata {
			meta[k] = v
		}
		meta["ts"] = m.now()
		meta["doc_length"] = docLength
		meta["pathname"] = args.Pathname
		if historyDocLength != docLength {
			meta["history_doc_length"] = historyDocLength
		}
		hu := HistoryUpdate{Doc: args.DocID, Op: []HistoryOp{op}, Meta: meta}
		if args.ProjectHistoryID != "" {
			ph := args.ProjectHistoryID
			hu.ProjectHistoryID = &ph
		}
		historyUpdates = append(historyUpdates, hu)
		if change.Op.D != nil && isAccepted(change) {
			historyDocLength -= len(*change.Op.D)
		}
	}
	return historyUpdates
}

// sortChangesByOffset mirrors Node's slice().sort: offset ascending, deletes
// before inserts at the same offset, stable.
func sortChangesByOffset(changes []rangestracker.Change) {
	sort.SliceStable(changes, func(i, j int) bool {
		a, b := changes[i], changes[j]
		if a.Op.P != b.Op.P {
			return a.Op.P < b.Op.P
		}
		aIns, aDel := a.Op.I != nil, a.Op.D != nil
		bIns, bDel := b.Op.I != nil, b.Op.D != nil
		if aDel && bIns {
			return true
		}
		if aIns && bDel {
			return false
		}
		return false // same kind — stable
	})
}

// --- _getRanges / _emptyRangesCount ----------------------------------------

func getRanges(tracker *rangestracker.RangesTracker) Ranges {
	res := Ranges{}
	if len(tracker.Changes) > 0 {
		res.Changes = make([]rangestracker.Change, 0, len(tracker.Changes))
		for _, c := range tracker.Changes {
			res.Changes = append(res.Changes, *c)
		}
	}
	if len(tracker.Comments) > 0 {
		res.Comments = make([]rangestracker.CommentItem, 0, len(tracker.Comments))
		for _, c := range tracker.Comments {
			res.Comments = append(res.Comments, *c)
		}
	}
	return res
}

func emptyRangesCount(tracker *rangestracker.RangesTracker) (empty, total int) {
	for _, c := range tracker.Comments {
		total++
		if commentText(c) == "" {
			empty++
		}
	}
	for _, c := range tracker.Changes {
		total++
		if c.Op.I != nil && *c.Op.I == "" {
			empty++
		}
	}
	return empty, total
}

// objectKeys mirrors Node Object.keys of the dirty "removed" map. Node orders
// integer-like keys ascending before the rest (string order); replicated so
// ids are deterministic.
func objectKeys(m map[string]*rangestracker.RangeRef) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	var intKeys, strKeys []string
	for _, k := range keys {
		if isIntegerLikeKey(k) {
			intKeys = append(intKeys, k)
		} else {
			strKeys = append(strKeys, k)
		}
	}
	sort.Slice(intKeys, func(i, j int) bool { return atoi(intKeys[i]) < atoi(intKeys[j]) })
	sort.Strings(strKeys)
	return append(intKeys, strKeys...)
}

func isIntegerLikeKey(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

func intPtr(v int) *int { return &v }
