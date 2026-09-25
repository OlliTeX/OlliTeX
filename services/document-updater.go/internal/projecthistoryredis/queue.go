package projecthistoryredis

// queue.go — the five vendored queue* methods over the hermetic Client
// seam, each byte-pinning JSON.stringify(projectUpdate) onto the
// project-history Redis ops list, with a SETNX oldest-op timestamp and
// (resync doc content) a doc-size guard.

import (
	"fmt"
	"strings"
	"time"

	"document-updater/internal/historyconversions"
	"document-updater/internal/limits"
	"document-updater/internal/utils"
	"ollitex/go/libraries/oerror"
)

// RawRenameEntity mirrors the vendored projectUpdate input to
// queueRenameEntity: { pathname, newPathname, version }.
type RawRenameEntity struct {
	// Pathname — the entity's old pathname.
	Pathname string
	// NewPathname — the entity's new pathname (wire key new_pathname).
	NewPathname string
	// Version — the projectVersion of the rename.
	Version int
}

// RawAddEntity mirrors the vendored projectUpdate input to queueAddEntity:
// { pathname, docLines?, url?, version, hash?, metadata?, createdBlob?,
// historyRangesSupport, ranges? }. Absent optionals are nil — the vendor
// omits undefined keys on JSON.stringify.
type RawAddEntity struct {
	// Pathname — the entity's pathname.
	Pathname string
	// DocLines — the content string (absent for files).
	DocLines *string
	// URL — the file URL (absent for plain docs).
	URL *string
	// Version — the projectVersion of the add.
	Version int
	// Hash — the content hash (absent when not set).
	Hash *string
	// Metadata — the file metadata (e.g. {importedAt, provider}), passed
	// through verbatim.
	Metadata map[string]any
	// CreatedBlob — the vendor `createdBlob ?? false` (nil → false).
	CreatedBlob *bool
	// HistoryRangesSupport — the vendor historyRangesSupport flag.
	HistoryRangesSupport bool
	// Ranges — the editor-side ranges, present when HRS is enabled.
	Ranges *historyconversions.Ranges
}

// RawRange mirrors the raw wire { pos, length } range.
type RawRange struct {
	// Pos — the range start.
	Pos int
	// Length — the range length.
	Length int
}

// RawTracking mirrors the raw wire { type, userId, ts } tracking.
type RawTracking struct {
	// Type — "insert" | "delete".
	Type string
	// UserID — the tracking user.
	UserID string
	// TS — the tracking timestamp.
	TS string
}

// RawLineComment mirrors one raw history-OT comment wire item:
// { id, ranges: [{pos, length}] } (verbatim passthrough).
type RawLineComment struct {
	// ID — the comment ID.
	ID string
	// Ranges — the raw comment ranges.
	Ranges []RawRange
}

// RawTrackedChange mirrors one raw history-OT tracked change wire item:
// { range: {pos, length}, tracking: {type, userId, ts} } (verbatim
// passthrough).
type RawTrackedChange struct {
	// Range — the raw range.
	Range RawRange
	// Tracking — { type, userId, ts }.
	Tracking RawTracking
}

// RawLines mirrors the vendored StringFileData (history-OT) resync `lines`
// input: { content, comments?, trackedChanges? } (absent optionals are
// nil slices — the vendor omits undefined keys).
type RawLines struct {
	// Content — the raw content string.
	Content string
	// Comments — the raw comments (verbatim wire).
	Comments []RawLineComment
	// TrackedChanges — the raw tracked changes (verbatim wire).
	TrackedChanges []RawTrackedChange
}

// ResyncOpts mirrors the vendor `opts` ({resyncProjectStructureOnly?}) for
// queueResyncProjectStructure.
type ResyncOpts struct {
	// ResyncProjectStructureOnly — the structure-only resync flag (the
	// vendor emits the key only when the value is truthy).
	ResyncProjectStructureOnly *bool
}

// QueueOps mirrors the vendor queueOps(projectId, ...ops): per raw op
// metrics.summary(redis.projectHistoryOps, op.length, {status: push}),
// then one MULTI that rpushes the ops and SET-NXs the first-op
// timestamp.
func (m *Manager) QueueOps(projectID string, ops ...string) error {
	if m.QueueOpsSeam != nil {
		return m.QueueOpsSeam(projectID, ops...)
	}
	for _, op := range ops {
		m.Summary("redis.projectHistoryOps", len(op), map[string]any{"status": "push"})
	}
	// MULTI: rpush ...ops + setnx <firstOpTs> Date.now() (both keys
	// project-scoped — one hash slot, vendor cluster note).
	multi := m.C.Multi()
	multi.RPush(m.Keys.ProjectHistoryOps(projectID), ops...)
	multi.SetNx(m.Keys.ProjectHistoryFirstOpTimestamp(projectID), m.Now())
	multi.Exec()
	return nil
}

// QueueRenameEntity mirrors the vendor queueRenameEntity(projectId,
// projectHistoryId, entityType, entityId, userId, projectUpdate,
// originOrSource).
func (m *Manager) QueueRenameEntity(projectID, projectHistoryID, entityType string, entityID any, userID string, raw RawRenameEntity, originOrSource any) error {
	pu := []pair{
		{"pathname", raw.Pathname},
		{"new_pathname", raw.NewPathname},
		{"meta", m.metaWire(userID, originOrSource)},
		{"version", raw.Version},
		{"projectHistoryId", projectHistoryID},
	}
	// The dynamic last key (vendor: projectUpdate[entityType] = entityId).
	pu = append(pu, pair{key: entityType, val: entityID})
	wire, err := encodeOrdered(pu)
	if err != nil {
		return fmt.Errorf("projecthistoryredis: wire rename: %w", err)
	}
	return m.QueueOps(projectID, wire)
}

// QueueAddEntity mirrors the vendor queueAddEntity(projectID,
// projectHistoryId, entityType, entityId, userId, projectUpdate,
// originOrSource).
func (m *Manager) QueueAddEntity(projectID, projectHistoryID, entityType string, entityID any, userID string, raw RawAddEntity, originOrSource any) error {
	docLines := raw.DocLines
	var rangesWire any
	if raw.HistoryRangesSupport && raw.Ranges != nil {
		// Vendor: addTrackedDeletesToContent(docLines,
		// ranges.changes ?? []) + toHistoryRanges(ranges) — the
		// `ranges` key is emitted only then.
		if docLines != nil {
			newLines := m.AddTrackedDeletesToContent(*docLines, raw.Ranges.Changes)
			docLines = &newLines
		}
		rangesWire = historyRangesWire(m.ToHistoryRanges(*raw.Ranges))
	}
	pu := []pair{{"pathname", raw.Pathname}}
	if docLines != nil {
		pu = append(pu, pair{"docLines", *docLines})
	}
	if raw.URL != nil {
		pu = append(pu, pair{"url", *raw.URL})
	}
	pu = append(pu,
		pair{"meta", m.metaWire(userID, originOrSource)},
		pair{"version", raw.Version})
	if raw.Hash != nil {
		pu = append(pu, pair{"hash", *raw.Hash})
	}
	if raw.Metadata != nil {
		pu = append(pu, pair{"metadata", raw.Metadata})
	}
	pu = append(pu, pair{"projectHistoryId", projectHistoryID})
	createdBlob := false
	if raw.CreatedBlob != nil {
		createdBlob = *raw.CreatedBlob
	}
	pu = append(pu, pair{"createdBlob", createdBlob})
	if rangesWire != nil {
		pu = append(pu, pair{"ranges", rangesWire})
	}
	pu = append(pu, pair{key: entityType, val: entityID})
	wire, err := encodeOrdered(pu)
	if err != nil {
		return fmt.Errorf("projecthistoryredis: wire add: %w", err)
	}
	return m.QueueOps(projectID, wire)
}

// QueueResyncProjectStructure mirrors the vendor
// queueResyncProjectStructure(projectId, projectHistoryId, docs, files,
// opts).
func (m *Manager) QueueResyncProjectStructure(projectID, projectHistoryID string, docs, files []string, opts *ResyncOpts) error {
	pu := []pair{
		{"resyncProjectStructure", obj("docs", docs, "files", files)},
		{"projectHistoryId", projectHistoryID},
		{"meta", m.tsOnlyMeta()},
	}
	// Vendor: `if (opts.resyncProjectStructureOnly)` — emit only when
	// truthy (the falsy flag is not emitted).
	if opts != nil && opts.ResyncProjectStructureOnly != nil && *opts.ResyncProjectStructureOnly {
		pu = append(pu, pair{"resyncProjectStructureOnly", *opts.ResyncProjectStructureOnly})
	}
	wire, err := encodeOrdered(pu)
	if err != nil {
		return fmt.Errorf("projecthistoryredis: wire resync-structure: %w", err)
	}
	return m.QueueOps(projectID, wire)
}

// QueueResyncDocContent mirrors the vendor queueResyncDocContent(projectId,
// projectHistoryId, docId, lines, ranges, resolvedCommentIds, version,
// pathname, historyRangesSupport). `lines` is the line-array resync form
// ([]string; raw == nil) or the history-OT raw form (raw != nil).
func (m *Manager) QueueResyncDocContent(projectID, projectHistoryID string, docID any, lines []string, raw *RawLines, ranges historyconversions.Ranges, resolvedCommentIDs []string, version int, pathname string, historyRangesSupport bool) error {
	resync := []pair{{"version", version}}
	var content string
	if raw != nil {
		// history-OT branch: raw StringFileData — no HRS transform.
		content = raw.Content
		resync = append(resync, pair{"historyOTRanges", m.historyOTRangesWire(raw)})
	} else {
		// line-array branch.
		content = strings.Join(lines, "\n")
		if historyRangesSupport {
			// Vendor: content = addTrackedDeletesToContent(content,
			// ranges.changes ?? []) + toHistoryRanges(ranges).
			content = m.AddTrackedDeletesToContent(content, ranges.Changes)
			resync = append(resync,
				pair{"ranges", historyRangesWire(m.ToHistoryRanges(ranges))},
				pair{"resolvedCommentIds", resolvedCommentIDs})
		}
	}
	// Vendor: content is assigned after resyncDocContent's other keys.
	resync = append(resync, pair{"content", content})
	pu := []pair{
		{"resyncDocContent", resync},
		{"projectHistoryId", projectHistoryID},
		{"path", pathname},
		{"doc", docID},
		{"meta", m.tsOnlyMeta()},
	}
	wire, err := encodeOrdered(pu)
	if err != nil {
		return fmt.Errorf("projecthistoryredis: wire resync-doc: %w", err)
	}
	// Vendor optimised size check over the serialised update length
	// (upper bound), then the branch-specific guard.
	sizeBound := len(wire)
	if raw != nil {
		if m.StringFileDataContentIsTooLarge(rawFileData(raw), m.MaxDocLength) {
			return oerror.New("blocking resync doc content insert into project history queue: doc is too large", map[string]any{
				"projectId": projectID,
				"docId":     docID,
				"docSize":   sizeBound,
			})
		}
	} else {
		if m.DocIsTooLarge(sizeBound, lines, m.MaxDocLength) {
			return oerror.New("blocking resync doc content insert into project history queue: doc is too large", map[string]any{
				"projectId": projectID,
				"docId":     docID,
				"docSize":   sizeBound,
			})
		}
	}
	return m.QueueOps(projectID, wire)
}

// rawFileData reshapes RawLines into the vendored
// limits.StringFileRawData shape the vendor
// stringFileDataContentIsTooLarge expects.
func rawFileData(raw *RawLines) *limits.StringFileRawData {
	rawData := &limits.StringFileRawData{
		Content:        raw.Content,
		TrackedChanges: make([]limits.TrackedChangeRange, 0, len(raw.TrackedChanges)),
	}
	for _, tc := range raw.TrackedChanges {
		rawData.TrackedChanges = append(rawData.TrackedChanges, limits.TrackedChangeRange{
			Range:    limits.Range{Pos: tc.Range.Pos, Length: tc.Range.Length},
			Tracking: limits.Tracking{Type: tc.Tracking.Type, UserID: tc.Tracking.UserID, TS: tc.Tracking.TS},
		})
	}
	return rawData
}

// --- wire builders -------------------------------------------------------

// tsISO renders the vendor `new Date()` wire (Date.toJSON == toISOString,
// ms precision, from the Now seam).
func tsISO(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z")
}

// metaWire builds the vendor meta object: { user_id, ts, [origin|source,
// type?='external'] }.
func (m *Manager) metaWire(userID string, originOrSource any) []pair {
	meta := []pair{
		{"user_id", userID},
		{"ts", tsISO(m.Now())},
	}
	source, origin := utils.ExtractOriginOrSource(originOrSource)
	if origin != nil {
		meta = append(meta, pair{"origin", origin})
		if kindOf(origin) != "editor" {
			meta = append(meta, pair{"type", "external"})
		}
	} else if source != "" {
		meta = append(meta, pair{"source", source})
		if source != "editor" {
			meta = append(meta, pair{"type", "external"})
		}
	}
	return meta
}

// kindOf extracts origin.kind from an origin wire object.
func kindOf(origin any) string {
	if m, ok := origin.(map[string]any); ok {
		v, _ := m["kind"].(string)
		return v
	}
	return ""
}

// tsOnlyMeta builds the resync meta object: { ts } (no user_id).
func (m *Manager) tsOnlyMeta() []pair {
	return []pair{{"ts", tsISO(m.Now())}}
}

// historyRangesWire mirrors the vendored toHistoryRanges RETURN wire
// ({ comments?, changes? } — comments FIRST; keys absent when the arrays
// are empty), each item {id?, op, metadata?}.
func historyRangesWire(r historyconversions.HistoryRanges) []pair {
	var out []pair
	if len(r.Comments) > 0 {
		cw := make([]any, 0, len(r.Comments))
		for _, c := range r.Comments {
			cw = append(cw, historyCommentWire(c))
		}
		out = append(out, pair{"comments", cw})
	}
	if len(r.Changes) > 0 {
		ow := make([]any, 0, len(r.Changes))
		for _, c := range r.Changes {
			ow = append(ow, historyChangeWire(c))
		}
		out = append(out, pair{"changes", ow})
	}
	return out
}

// historyOpWire builds the enriched op wire { p, i?, d?, c?, t?, hpos?,
// hlen? }.
func historyOpWire(op historyconversions.HistoryOp) []pair {
	p := []pair{{"p", op.P}}
	if op.I != nil {
		p = append(p, pair{"i", *op.I})
	}
	if op.D != nil {
		p = append(p, pair{"d", *op.D})
	}
	if op.C != "" {
		p = append(p, pair{"c", op.C})
	}
	if op.T != "" {
		p = append(p, pair{"t", op.T})
	}
	if op.Hpos != nil {
		p = append(p, pair{"hpos", *op.Hpos})
	}
	if op.Hlen != nil {
		p = append(p, pair{"hlen", *op.Hlen})
	}
	return p
}

// metadataWire flattens the vendored metadata to its wire
// ({ ts?, user_id?, resolved? } — Go-sorted keys wire-compatible).
func metadataWire(md *utils.Metadata) map[string]any {
	if md == nil {
		return nil
	}
	m := map[string]any{}
	if md.TS != "" {
		m["ts"] = md.TS
	}
	if md.UserID != "" {
		m["user_id"] = md.UserID
	}
	if md.Resolved != nil {
		m["resolved"] = *md.Resolved
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

func historyChangeWire(c historyconversions.HistoryTrackedChange) []pair {
	item := []pair{}
	if c.ID != nil && *c.ID != "" {
		item = append(item, pair{"id", *c.ID})
	}
	item = append(item, pair{"op", historyOpWire(c.Op)})
	if md := metadataWire(c.Metadata); md != nil {
		item = append(item, pair{"metadata", md})
	}
	return item
}

func historyCommentWire(c historyconversions.HistoryComment) []pair {
	item := []pair{}
	if c.ID != "" {
		item = append(item, pair{"id", c.ID})
	}
	item = append(item, pair{"op", historyOpWire(c.Op)})
	if md := metadataWire(c.Metadata); md != nil {
		item = append(item, pair{"metadata", md})
	}
	return item
}

// historyOTRangesWire mirrors the vendor historyOTRanges verbatim wire:
// { comments?, trackedChanges? } (keys absent when undefined).
func (m *Manager) historyOTRangesWire(raw *RawLines) []pair {
	var out []pair
	if raw.Comments != nil {
		cw := make([]any, 0, len(raw.Comments))
		for _, c := range raw.Comments {
			cw = append(cw, rawCommentWire(c))
		}
		out = append(out, pair{"comments", cw})
	}
	if raw.TrackedChanges != nil {
		tw := make([]any, 0, len(raw.TrackedChanges))
		for _, tc := range raw.TrackedChanges {
			tw = append(tw, rawTCWire(tc))
		}
		out = append(out, pair{"trackedChanges", tw})
	}
	return out
}

func rawCommentWire(c RawLineComment) []pair {
	item := []pair{}
	if c.ID != "" {
		item = append(item, pair{"id", c.ID})
	}
	if c.Ranges != nil {
		rw := make([]any, 0, len(c.Ranges))
		for _, r := range c.Ranges {
			rw = append(rw, obj("pos", r.Pos, "length", r.Length))
		}
		item = append(item, pair{"ranges", rw})
	}
	return item
}

// rawTCWire mirrors the vendor raw tracked-change wire verbatim
// { range: {pos, length}, tracking: {type, userId?, ts?} } (the fixture
// key order: type BEFORE ts).
func rawTCWire(tc RawTrackedChange) []pair {
	tracking := []pair{{"type", tc.Tracking.Type}}
	if tc.Tracking.UserID != "" {
		tracking = append(tracking, pair{"userId", tc.Tracking.UserID})
	}
	if tc.Tracking.TS != "" {
		tracking = append(tracking, pair{"ts", tc.Tracking.TS})
	}
	return []pair{
		{"range", obj("pos", tc.Range.Pos, "length", tc.Range.Length)},
		{"tracking", tracking},
	}
}
