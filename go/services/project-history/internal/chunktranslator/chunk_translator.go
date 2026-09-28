// Package chunktranslator ports vendor app/js/ChunkTranslator.js (647 L):
//
//   - ConvertToSummarizedUpdates(chunk) — fold a chunk's changes into a
//     per-change summary (pathnames touched + project ops + author users).
//   - ConvertToDiffUpdates(deps, chunk, pathname, from, to) — replay text
//     ops over the file's snapshot content (fetched via the injected
//     HistoryStoreManager/WebApi seams), producing
//     {initialContent, updates[]} with tracking-aware i/d ops.
//
// Port decisions (fidelity notes for review):
//
//   - Vendor is callback-style with module-level HistoryStoreManager and
//     WebApiManager dependencies; the Go port injects those as `Deps` so the
//     C-phase (D2/D3 managers) can wire the real implementations.
//   - Timestamps: vendor stores `change.timestamp` (ISO string) and re-
//     parses with `new Date(...).getTime()` per update meta. The Go port
//     takes `Change.Timestamp` as epoch millis (the B9 wire form the live
//     pipeline already emits) and surfaces it directly.
//   - TextUpdateBuilder runs over opmodel.ScanOp values on UTF-16 code-
//     unit slices (opmodel.UnitSlice/Units), matching vendor `length`/`slice`.
//
// Vendor source: services/project-history/app/js/ChunkTranslator.js.
// Oracle rig: /tmp/oracle_ct (real vendor file + tests + editor-core,
// 47/47 — source and tests internally consistent).
package chunktranslator

import (
	"encoding/json"
	"fmt"
	"time"

	"ollitex/go/services/project-history/internal/errors"
	"ollitex/go/services/project-history/internal/opmodel"
)

// Deps — the two injected vendor module-level dependencies.
type Deps struct {
	// GetHistoryID — vendor `WebApiManager.getHistoryId(projectId)`.
	GetHistoryID func(projectID string) (int, error)
	// GetProjectBlob — vendor `HistoryStoreManager.getProjectBlob(historyId,
	// hash)`. Returns raw text (the ranges case is JSON-decoded).
	GetProjectBlob func(historyID int, hash string) (string, error)
}

// Chunk — vendor `chunk.chunk` (the outer `{project_id, chunk: {...}}` is
// the caller's raw mongo shape; this carries what the module reads).
type Chunk struct {
	StartVersion  int
	SnapshotFiles map[string]map[string]any
	Changes       []Change
}

// Change — vendor `history.changes[i]` (B9 wire shape), raw maps so the
// opmodel round-trip in TextOp conversion reuses the vendor idiom.
type Change struct {
	Operations []map[string]any
	Timestamp  int64 // epoch millis
	Authors    []any
	V2Authors  []any
	Origin     map[string]any
}

// RawChunk — decode the vendor `convert*(chunk, cb)`'s chunk argument
// ({project_id, chunk: {startVersion, history: {snapshot, changes}}}).
func RawChunk(raw map[string]any) (*Chunk, error) {
	cs, ok := raw["chunk"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("chunk missing")
	}
	hist, ok := cs["history"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("chunk.history missing")
	}
	snap, ok := hist["snapshot"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("chunk.history.snapshot missing")
	}
	rawFiles, ok := snap["files"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("chunk.history.snapshot.files missing")
	}
	chunk := &Chunk{SnapshotFiles: make(map[string]map[string]any)}
	for pname, f := range rawFiles {
		m, ok := f.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("snapshot file %q must be an object", pname)
		}
		chunk.SnapshotFiles[pname] = m
	}
	if sv, ok := toInt(cs["startVersion"]); ok {
		chunk.StartVersion = sv
	}
	rawChanges, _ := hist["changes"].([]any) // absent for an empty chunk
	for _, rc := range rawChanges {
		cmap, ok := rc.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("change must be an object")
		}
		change, err := decodeChange(cmap)
		if err != nil {
			return nil, fmt.Errorf("invalid change: %w", err)
		}
		chunk.Changes = append(chunk.Changes, change)
	}
	return chunk, nil
}

func decodeChange(m map[string]any) (Change, error) {
	out := Change{Authors: []any{}}
	if ops, ok := m["operations"].([]any); ok {
		for _, rop := range ops {
			op, ok := rop.(map[string]any)
			if !ok {
				return Change{}, fmt.Errorf("op must be an object")
			}
			out.Operations = append(out.Operations, op)
		}
	}
	if ts, has := m["timestamp"]; has {
		ms, err := toMillis(ts)
		if err != nil {
			return Change{}, fmt.Errorf("invalid change timestamp: %w", err)
		}
		out.Timestamp = ms
	}
	if arr, ok := m["authors"].([]any); ok {
		out.Authors = arr
	}
	if arr, ok := m["v2Authors"].([]any); ok {
		out.V2Authors = arr
	}
	if o, ok := m["origin"].(map[string]any); ok {
		out.Origin = o
	}
	return out, nil
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	}
	return 0, false
}

// toMillis — vendor `new Date(v).getTime()`: numbers are epoch millis;
// ISO strings are parsed.
func toMillis(v any) (int64, error) {
	switch t := v.(type) {
	case int:
		return int64(t), nil
	case float64:
		return int64(t), nil
	case string:
		parsed, err := time.Parse(time.RFC3339, t)
		if err != nil {
			return 0, fmt.Errorf("invalid timestamp %q: %w", t, err)
		}
		return parsed.UnixMilli(), nil
	}
	return 0, fmt.Errorf("timestamp must be a number or ISO string, got %T", v)
}

// --- op discriminators (vendor `UpdateSetBuilder._is*Operation`) — raw-map
// based (B7/B9 idiom).

func isTextOperation(op map[string]any) bool {
	_, has := op["textOperation"]
	return has
}

func isRenameOperation(op map[string]any) bool {
	v, has := op["newPathname"]
	return has && strOf(v) != ""
}

func isRemoveFileOperation(op map[string]any) bool {
	v, has := op["newPathname"]
	return has && strOf(v) == ""
}

func isAddFileOperation(op map[string]any) bool {
	_, has := op["file"]
	return has
}

func strOf(v any) string {
	s, _ := v.(string)
	return s
}

// --- UpdateSetBuilder (vendor class UpdateSetBuilder) ---

type updateSetBuilder struct {
	version           int
	summarizedUpdates []map[string]any
	// files: pathname -> *file. A nil value is the vendor "seen but missing"
	// marker (`this.files[pathname] = null`), distinct from an absent key.
	files map[string]*file
}

func newUpdateSetBuilder(startVersion int, snapshotFiles map[string]map[string]any) *updateSetBuilder {
	b := &updateSetBuilder{
		version:           startVersion,
		summarizedUpdates: []map[string]any{},
		files:             make(map[string]*file),
	}
	// Vendor iterates `for (const pathname in files)` (JS insertion order);
	// Go map order is random but irrelevant (files are looked up by key).
	for pathname, snap := range snapshotFiles {
		b.files[pathname] = &file{
			pathname:       pathname,
			snapshot:       snap,
			initialVersion: startVersion,
		}
	}
	return b
}

func (b *updateSetBuilder) get(pathname string) (*file, bool) {
	f, has := b.files[pathname]
	return f, has
}

// applyChange — vendor `UpdateSetBuilder.applyChange(change)`.
func (b *updateSetBuilder) applyChange(change Change) error {
	// vendor: `let authors = _.map(change.authors, id => id == null ? null : id)`
	authors := make([]any, 0, len(change.Authors)+len(change.V2Authors))
	for _, id := range change.Authors {
		if id == nil {
			authors = append(authors, nil)
		} else {
			authors = append(authors, id)
		}
	}
	authors = append(authors, change.V2Authors...)

	pathnames := []string{}
	projectOps := []any{}
	set := &changeSet{pathnames: &pathnames, projectOps: &projectOps}
	for _, op := range change.Operations {
		if err := b.applyOp(op, change.Timestamp, authors, change.Origin, set); err != nil {
			return err
		}
	}
	meta := map[string]any{
		"users":    authors,
		"start_ts": change.Timestamp,
		"end_ts":   change.Timestamp,
	}
	if change.Origin != nil {
		meta["origin"] = change.Origin
	}
	b.summarizedUpdates = append(b.summarizedUpdates, map[string]any{
		"meta":        meta,
		"v":           b.version, // vendor: `v: this.version` BEFORE the ++
		"pathnames":   toAny(pathnames),
		"project_ops": projectOps,
	})
	b.version++
	return nil
}

type changeSet struct {
	pathnames  *[]string
	projectOps *[]any
}

func (s *changeSet) addPathname(p string) {
	for _, existing := range *s.pathnames {
		if existing == p {
			return // vendor pathnames is a Set
		}
	}
	*s.pathnames = append(*s.pathnames, p)
}

func (s *changeSet) addProjectOp(k string, m map[string]any) {
	*s.projectOps = append(*s.projectOps, map[string]any{k: m})
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func (b *updateSetBuilder) applyOp(
	op map[string]any,
	tsMs int64,
	authors []any,
	origin map[string]any,
	set *changeSet,
) error {
	if isTextOperation(op) {
		return b.applyTextOp(op, tsMs, authors, origin, set)
	}
	if isRenameOperation(op) {
		return b.applyRenameOp(op, set)
	}
	if isRemoveFileOperation(op) {
		return b.applyRemoveFileOp(op, set)
	}
	if isAddFileOperation(op) {
		return b.applyAddFileOp(op, set)
	}
	// vendor: unknown op — silently skipped.
	return nil
}

func (b *updateSetBuilder) applyTextOp(
	op map[string]any,
	tsMs int64,
	authors []any,
	origin map[string]any,
	set *changeSet,
) error {
	pathname := strOf(getAny(op, "pathname"))
	if pathname == "" {
		// vendor: logger.warn + return (history renders, op is recorded).
		return nil
	}
	f, has := b.files[pathname]
	if !has || f == nil {
		// vendor: logger.warn + missing-file marker.
		b.files[pathname] = nil
		return nil
	}
	f.operations = append(f.operations, storedOp{
		authors:  authors,
		tsMillis: tsMs,
		version:  b.version,
		op:       op,
		origin:   origin,
	})
	set.addPathname(pathname)
	return nil
}

func (b *updateSetBuilder) applyRenameOp(op map[string]any, set *changeSet) error {
	pathname := strOf(getAny(op, "pathname"))
	newPathname := strOf(getAny(op, "newPathname"))
	f, has := b.files[pathname]
	if !has || f == nil {
		b.files[pathname] = nil
		return nil
	}
	set.addProjectOp("rename", map[string]any{"pathname": pathname, "newPathname": newPathname})
	f.rename(newPathname)
	delete(b.files, pathname)
	b.files[newPathname] = f
	return nil
}

func (b *updateSetBuilder) applyAddFileOp(op map[string]any, set *changeSet) error {
	pathname := strOf(getAny(op, "pathname"))
	m, _ := getAny(op, "file").(map[string]any)
	set.addProjectOp("add", map[string]any{"pathname": pathname})
	b.files[pathname] = &file{
		pathname:       pathname,
		snapshot:       m,
		initialVersion: b.version,
	}
	return nil
}

func (b *updateSetBuilder) applyRemoveFileOp(op map[string]any, set *changeSet) error {
	pathname := strOf(getAny(op, "pathname"))
	f, has := b.files[pathname]
	if !has || f == nil {
		b.files[pathname] = nil
		return nil
	}
	set.addProjectOp("remove", map[string]any{"pathname": pathname})
	delete(b.files, pathname)
	return nil
}

func getAny(m map[string]any, k string) any {
	if m == nil {
		return nil
	}
	if v, has := m[k]; has {
		return v
	}
	return nil
}

// --- File (vendor class File) ---

type file struct {
	pathname       string
	snapshot       map[string]any
	initialVersion int
	operations     []storedOp
}

type storedOp struct {
	authors  []any
	tsMillis int64
	version  int
	op       map[string]any
	origin   map[string]any
}

func (f *file) rename(pathname string) { f.pathname = pathname }

// getDiffUpdates — vendor `File.getDiffUpdates(historyId, fromVersion,
// toVersion, cb)`.
func (f *file) getDiffUpdates(
	deps Deps,
	historyID int,
	fromVersion, toVersion int,
) (map[string]any, error) {
	if v, has := f.snapshot["stringLength"]; !has || isNilValue(v) {
		// vendor: `if (this.snapshot.stringLength == null)` → binary file
		return map[string]any{"binary": true}, nil
	}
	content, ranges, err := f.loadContentAndRanges(deps, historyID)
	if err != nil {
		return nil, err
	}
	var tcl *opmodel.TrackedChangeList
	if ranges != nil {
		rawTCs, _ := ranges["trackedChanges"].([]any)
		tcl, err = opmodel.TrackedChangeListFromRaw(rawTCs)
		if err != nil {
			return nil, err
		}
	} else {
		tcl = opmodel.NewTrackedChangeList(nil)
	}
	updates := []any{}
	seen, seenInitial := false, ""
	for _, opInfo := range f.operations {
		if !isTextOperation(opInfo.op) {
			// vendor: "We only care about text operations"
			continue
		}
		// vendor: "Set the initialContent to the latest version we have
		// before the diff begins... we store the content *before* applying
		// the updates." — the snapshot runs BEFORE this iteration's
		// convertTextOperation (the shared tracked list is mutated in place
		// per op, so the order is semantic).
		if opInfo.version >= fromVersion && !seen {
			seenInitial = removeTrackedDeletesFromString(content, tcl)
			seen = true
		}
		converted, ops, err := convertTextOperation(content, opInfo.op, tcl)
		if err != nil {
			return nil, err
		}
		if opInfo.version >= fromVersion && opInfo.version < toVersion {
			m := map[string]any{
				"users":    opInfo.authors,
				"start_ts": opInfo.tsMillis,
				"end_ts":   opInfo.tsMillis,
			}
			if opInfo.origin != nil {
				m["origin"] = opInfo.origin
			}
			updates = append(updates, map[string]any{
				"meta": m,
				"v":    opInfo.version,
				"op":   toAnyOps(ops),
			})
		}
		content = converted
	}
	if !seen {
		seenInitial = removeTrackedDeletesFromString(content, tcl)
	}
	return map[string]any{"initialContent": seenInitial, "updates": updates}, nil
}

func isNilValue(v any) bool {
	// vendor `== null` for the snapshot.stringLength key: absent or nil.
	// (Numbers — including 0 — are a valid string length.)
	if v == nil {
		return true
	}
	switch v.(type) {
	case int, int64, float64:
		return false
	}
	return true
}

func toAnyOps(ops []map[string]any) []any {
	out := make([]any, len(ops))
	for i, m := range ops {
		out[i] = m
	}
	return out
}

func convertTextOperation(source string, op map[string]any, tcl *opmodel.TrackedChangeList) (string, []map[string]any, error) {
	textOp, err := opmodel.TextOperationFromJSON(op)
	if err != nil {
		return "", nil, err
	}
	builder := newTextUpdateBuilder(source, tcl)
	for _, rop := range textOp.Ops {
		builder.applyOp(rop)
	}
	builder.finish()
	return builder.result, builder.changes, nil
}

func (f *file) loadContentAndRanges(deps Deps, historyID int) (string, map[string]any, error) {
	hash := strOf(getAny(f.snapshot, "hash"))
	content, err := deps.GetProjectBlob(historyID, hash)
	if err != nil {
		return "", nil, err
	}
	if rh := strOf(getAny(f.snapshot, "rangesHash")); rh != "" {
		raw, err := deps.GetProjectBlob(historyID, rh)
		if err != nil {
			return "", nil, err
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			return "", nil, fmt.Errorf("invalid ranges blob: %w", err)
		}
		return content, m, nil
	}
	return content, nil, nil
}

// --- TextUpdateBuilder (vendor class TextUpdateBuilder) ---

type textUpdateBuilder struct {
	trackedChanges *opmodel.TrackedChangeList
	source         string
	sourceCursor   int
	result         string
	changes        []map[string]any
}

func newTextUpdateBuilder(source string, ranges *opmodel.TrackedChangeList) *textUpdateBuilder {
	return &textUpdateBuilder{trackedChanges: ranges, source: source, changes: []map[string]any{}}
}

func (b *textUpdateBuilder) applyOp(op opmodel.ScanOp) {
	switch rop := op.(type) {
	case opmodel.RetainOp:
		length := opmodel.Units(b.result)
		b.applyRetain(rop)
		b.trackedChanges.ApplyRetain(length, rop.Length, rop.Tracking)
	case opmodel.InsertOp:
		length := opmodel.Units(b.result)
		b.applyInsert(rop)
		b.trackedChanges.ApplyInsert(length, rop.Insertion, rop.Tracking)
	case opmodel.RemoveOp:
		length := opmodel.Units(b.result)
		b.applyDelete(rop)
		b.trackedChanges.ApplyDelete(length, rop.Length)
	}
}

// applyRetain — vendor `TextUpdateBuilder.applyRetain`.
func (b *textUpdateBuilder) applyRetain(retain opmodel.RetainOp) {
	resultRetentionRange := opmodel.Range{Pos: b.resultUnits(), Length: retain.Length}
	sourceRetentionRange := opmodel.Range{Pos: b.sourceCursor, Length: retain.Length}

	scanCursor := b.resultUnits()
	if retain.Tracking != nil {
		// vendor: "modifying existing tracked deletes" — removal (type
		// insert/none) of a tracked delete is an insertion; any range we
		// introduce as a tracked deletion is a deletion.
		var trackedDeletes []opmodel.TrackedChange
		for _, tc := range b.trackedChanges.AsSorted() {
			if tc.Tracking.Type == "delete" && tc.Range.Overlaps(resultRetentionRange) {
				trackedDeletes = append(trackedDeletes, tc)
			}
		}
		sourceOffset := b.sourceCursor - b.resultUnits()
		for _, trackedDelete := range trackedDeletes {
			// vendor: clamp to the retention range start (a prior insert
			// tracked as delete can extend before the range).
			clampedStart := trackedDelete.Range.Start()
			if resultRetentionRange.Start() > clampedStart {
				clampedStart = resultRetentionRange.Start()
			}
			resultTD := opmodel.Range{Pos: clampedStart, Length: trackedDelete.Range.End() - clampedStart}
			sourceTD := resultTD.MoveBy(sourceOffset)

			if scanCursor < resultTD.Start() {
				if retain.Tracking.Type == "delete" {
					b.changes = append(b.changes, map[string]any{
						"d": opmodel.UnitSlice(b.source, b.sourceCursor, sourceTD.Start()),
						"p": b.resultUnits(),
					})
				}
				b.result += opmodel.UnitSlice(b.source, b.sourceCursor, sourceTD.Start())
				scanCursor = resultTD.Start()
				b.sourceCursor = sourceTD.Start()
			}
			endOfInsertionResult := resultTD.End()
			if resultRetentionRange.End() < endOfInsertionResult {
				endOfInsertionResult = resultRetentionRange.End()
			}
			endOfInsertionSource := sourceTD.End()
			if sourceRetentionRange.End() < endOfInsertionSource {
				endOfInsertionSource = sourceRetentionRange.End()
			}
			text := opmodel.UnitSlice(b.source, b.sourceCursor, endOfInsertionSource)
			if retain.Tracking.Type == "none" || retain.Tracking.Type == "insert" {
				b.changes = append(b.changes, map[string]any{
					"i": text,
					"p": b.resultUnits(),
				})
			}
			b.result += text
			// skip the tracked delete itself
			scanCursor = endOfInsertionResult
			b.sourceCursor = endOfInsertionSource

			if scanCursor >= resultRetentionRange.End() {
				break
			}
		}
	}
	if scanCursor < resultRetentionRange.End() {
		// vendor: the last region is not a tracked delete, but we still
		// report a new tracked deletion as a deletion.
		text := opmodel.UnitSlice(b.source, b.sourceCursor, sourceRetentionRange.End())
		if retain.Tracking != nil && retain.Tracking.Type == "delete" {
			b.changes = append(b.changes, map[string]any{
				"d": text,
				"p": b.resultUnits(),
			})
		}
		b.result += text
	}
	b.sourceCursor = sourceRetentionRange.End()
}

func (b *textUpdateBuilder) resultUnits() int { return opmodel.Units(b.result) }

// applyInsert — vendor `TextUpdateBuilder.applyInsert`.
func (b *textUpdateBuilder) applyInsert(insert opmodel.InsertOp) {
	if insert.Tracking == nil || insert.Tracking.Type != "delete" {
		b.changes = append(b.changes, map[string]any{
			"i": insert.Insertion,
			"p": b.resultUnits(),
		})
	}
	b.result += insert.Insertion
	// vendor: the source cursor does NOT advance
}

// applyDelete — vendor `TextUpdateBuilder.applyDelete`.
func (b *textUpdateBuilder) applyDelete(deletion opmodel.RemoveOp) {
	sourceDeletionRange := opmodel.Range{Pos: b.sourceCursor, Length: deletion.Length}
	resultDeletionRange := opmodel.Range{Pos: b.resultUnits(), Length: deletion.Length}

	var trackedDeletes []opmodel.TrackedChange
	for _, tc := range b.trackedChanges.AsSorted() {
		if tc.Tracking.Type == "delete" && tc.Range.Overlaps(resultDeletionRange) {
			trackedDeletes = append(trackedDeletes, tc)
		}
	}
	scanCursor := b.resultUnits()
	sourceOffset := b.sourceCursor - b.resultUnits()

	for _, trackedDelete := range trackedDeletes {
		// vendor: clamp to the deletion range start (see applyRetain).
		clampedStart := trackedDelete.Range.Start()
		if resultDeletionRange.Start() > clampedStart {
			clampedStart = resultDeletionRange.Start()
		}
		resultTD := opmodel.Range{Pos: clampedStart, Length: trackedDelete.Range.End() - clampedStart}
		sourceTD := resultTD.MoveBy(sourceOffset)

		if scanCursor < resultTD.Start() {
			b.changes = append(b.changes, map[string]any{
				"d": opmodel.UnitSlice(b.source, b.sourceCursor, sourceTD.Start()),
				"p": b.resultUnits(),
			})
		}
		// skip the tracked delete itself
		scanCursor = resultTD.End()
		if resultDeletionRange.End() < scanCursor {
			scanCursor = resultDeletionRange.End()
		}
		b.sourceCursor = sourceTD.End()
		if sourceDeletionRange.End() < b.sourceCursor {
			b.sourceCursor = sourceDeletionRange.End()
		}

		if scanCursor >= resultDeletionRange.End() {
			break
		}
	}
	if scanCursor < resultDeletionRange.End() {
		b.changes = append(b.changes, map[string]any{
			"d": opmodel.UnitSlice(b.source, b.sourceCursor, sourceDeletionRange.End()),
			"p": b.resultUnits(),
		})
	}
	b.sourceCursor = sourceDeletionRange.End()
}

// finish — vendor `TextUpdateBuilder.finish`: append the remaining source,
// then shift each `d` op's `p` back by the tracked-deletes hidden before it.
func (b *textUpdateBuilder) finish() {
	if b.sourceCursor < opmodel.Units(b.source) {
		b.result += opmodel.UnitSlice(b.source, b.sourceCursor, opmodel.Units(b.source))
	}
	for _, op := range b.changes {
		pi, ok := op["p"].(int)
		if !ok {
			continue
		}
		shift := 0
		for _, tc := range b.trackedChanges.AsSorted() {
			if tc.Tracking.Type != "delete" || tc.Range.Start() >= pi {
				continue
			}
			if tc.Range.End() < pi {
				shift += tc.Range.Length
			} else {
				shift += pi - tc.Range.Start()
			}
		}
		op["p"] = pi - shift
	}
}

// removeTrackedDeletesFromString — vendor top-level function.
func removeTrackedDeletesFromString(content string, tcl *opmodel.TrackedChangeList) string {
	var buf []byte
	cursor := 0
	for _, tc := range tcl.AsSorted() {
		if tc.Tracking.Type != "delete" {
			continue
		}
		if cursor < tc.Range.Start() {
			buf = append(buf, opmodel.UnitSlice(content, cursor, tc.Range.Start())...)
		}
		// skip the tracked change itself
		cursor = tc.Range.End()
	}
	buf = append(buf, opmodel.UnitSlice(content, cursor, opmodel.Units(content))...)
	return string(buf)
}

// --- top-level entry points (vendor exports) ---

// ConvertToSummarizedUpdates — vendor `convertToSummarizedUpdates(chunk, cb)`.
func ConvertToSummarizedUpdates(chunk *Chunk) ([]map[string]any, error) {
	builder := newUpdateSetBuilder(chunk.StartVersion, chunk.SnapshotFiles)
	for _, change := range chunk.Changes {
		if err := builder.applyChange(change); err != nil {
			return nil, err
		}
	}
	return builder.summarizedUpdates, nil
}

// ConvertToDiffUpdates — vendor `convertToDiffUpdates(projectId, chunk,
// pathname, fromVersion, toVersion, cb)`.
func ConvertToDiffUpdates(
	deps Deps,
	projectID string,
	chunk *Chunk,
	pathname string,
	fromVersion, toVersion int,
) (map[string]any, error) {
	builder := newUpdateSetBuilder(chunk.StartVersion, chunk.SnapshotFiles)

	// vendor: "Because we're referencing by pathname, which can change, we
	// want to get the last file in the range fromVersion:toVersion that has
	// the pathname we want."
	file := (*file)(nil)
	version := builder.version
	for _, change := range chunk.Changes {
		if fromVersion <= version && version <= toVersion {
			if cur, _ := builder.get(pathname); cur != nil {
				file = cur
			}
		}
		if err := builder.applyChange(change); err != nil {
			return nil, err
		}
		version++
	}
	if fromVersion <= version && version <= toVersion {
		if cur, _ := builder.get(pathname); cur != nil {
			file = cur
		}
	}

	// vendor: `builder.getFile(pathname) === null` — an explicit missing-file
	// marker returns an empty diff rather than an error.
	if marker, exists := builder.get(pathname); exists && marker == nil {
		return map[string]any{"initialContent": "", "updates": []any{}}, nil
	}
	if file == nil {
		return nil, errors.NotFound(fmt.Sprintf("pathname '%s' not found in range", pathname))
	}
	histID, err := deps.GetHistoryID(projectID)
	if err != nil {
		return nil, err
	}
	return file.getDiffUpdates(deps, histID, fromVersion, toVersion)
}
