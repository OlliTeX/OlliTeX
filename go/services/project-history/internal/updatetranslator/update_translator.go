// Package updatetranslator ports vendor app/js/UpdateTranslator.js:
// translation of raw sync updates (doc/file adds, renames, raw historyot
// edit ops, text ops, comment/metadata ops) into history changes, plus the
// compression of the resulting operation list.
//
// Vendor pipeline (per update):
//
//	_convertToChange: discriminator cascade on the raw update map
//	  -> raw operations list (map[string]any)
//	  -> rawChange {operations, v2Authors, timestamp, projectVersion?,
//	               v2DocVersions?, origin?}
//	  -> Core.Change.fromRaw(rawChange)
//	  -> OperationsCompressor.compressOperations
//
// The Go port mirrors that flow with historyot.Change +
// operationscompressor, so wire output is byte-identical to vendor
// (`change.toRaw()`), including scan-op coalescing during
// `TextOperation.fromJSON` (opmodel) and compose-driven merging of
// consecutive text ops (B3 opmodel + B8b operationscompressor).
//
// Vendor source: services/project-history/app/js/UpdateTranslator.js.
package updatetranslator

import (
	"fmt"
	"time"

	"ollitex/go/services/project-history/internal/errors"
	"ollitex/go/services/project-history/internal/historyot"
	"ollitex/go/services/project-history/internal/operationscompressor"
)

// UpdateWithBlob — vendor `UpdateWithBlob`: { update, blobHashes }.
type UpdateWithBlob struct {
	Update     map[string]any // the raw update map
	BlobHashes map[string]any // raw blob hashes (file / ranges)
}

// ConvertToChanges — vendor `convertToChanges(projectId, updatesWithBlobs)`.
func ConvertToChanges(projectID string, updates []UpdateWithBlob) ([]*historyot.Change, error) {
	out := make([]*historyot.Change, 0, len(updates))
	for _, uwb := range updates {
		change, err := convertToChange(projectID, uwb)
		if err != nil {
			return nil, err
		}
		out = append(out, change)
	}
	return out, nil
}

func vStr(v any) string {
	s, _ := v.(string)
	return s
}

func vNum(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case int32:
		return float64(n)
	}
	return 0
}

// jsTruthy — vendor JS truthiness (B8 idiom: nil is falsy; Go numeric 0 and
// "" are falsy; objects/slices are truthy).
func jsTruthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return x != ""
	case float64:
		return x != 0
	case int:
		return x != 0
	case bool:
		return x
	default:
		return true
	}
}

// --- discriminators (vendor order, first match wins) ---

// isRenameUpdate — vendor `_isRenameUpdate`: `new_pathname != null`.
func isRenameUpdate(update map[string]any) bool {
	_, ok := update["new_pathname"]
	return ok && update["new_pathname"] != nil
}

// isAddDocUpdate — `doc != null && docLines != null`.
func isAddDocUpdate(update map[string]any) bool {
	doc, docOK := update["doc"]
	if !docOK || doc == nil {
		return false
	}
	dl, dlOK := update["docLines"]
	return dlOK && dl != nil
}

// isAddFileUpdate — `file != null && (createdBlob || url != null)`.
func isAddFileUpdate(update map[string]any) bool {
	file, fileOK := update["file"]
	if !fileOK || file == nil {
		return false
	}
	cb, hasCB := update["createdBlob"]
	url, hasURL := update["url"]
	return (hasCB && jsTruthy(cb)) || (hasURL && url != nil)
}

// isAddUpdate — `isAddDocUpdate || isAddFileUpdate`.
func isAddUpdate(update map[string]any) bool {
	return isAddDocUpdate(update) || isAddFileUpdate(update)
}

// IsAddUpdate — exported alias for cross-package use (the BlobManager
// port mirrors vendor `UpdateTranslator.isAddUpdate(update)`).
func IsAddUpdate(update map[string]any) bool { return isAddUpdate(update) }

// isHistoryOTEditOperationUpdate — vendor: doc/op non-null, meta.pathname
// non-null, and `Core.EditOperationBuilder.isValid(update.op[0])`.
func isHistoryOTEditOperationUpdate(update map[string]any) bool {
	if v := update["doc"]; v == nil {
		return false
	}
	if v := update["op"]; v == nil {
		return false
	}
	meta, ok := update["meta"].(map[string]any)
	if !ok || meta["pathname"] == nil {
		return false
	}
	ops, ok := update["op"].([]any)
	if !ok || len(ops) == 0 {
		return false
	}
	return historyot.Builder.IsValid(ops[0])
}

// isTextUpdate — vendor: doc/op non-null,
// meta.pathname/meta.doc_length non-null.
func isTextUpdate(update map[string]any) bool {
	doc, docOK := update["doc"]
	op, opOK := update["op"]
	if !docOK || doc == nil || !opOK || op == nil {
		return false
	}
	meta, ok := update["meta"].(map[string]any)
	if !ok || meta["pathname"] == nil || meta["doc_length"] == nil {
		return false
	}
	return true
}

// isSetCommentStateUpdate — `'commentId' in update && 'resolved' in update`.
func isSetCommentStateUpdate(update map[string]any) bool {
	_, hasID := update["commentId"]
	_, hasResolved := update["resolved"]
	return hasID && hasResolved
}

// isSetFileMetadataOperation — `'metadata' in update`.
func isSetFileMetadataOperation(update map[string]any) bool {
	_, ok := update["metadata"]
	return ok
}

// isDeleteCommentUpdate — `'deleteComment' in update`.
func isDeleteCommentUpdate(update map[string]any) bool {
	_, ok := update["deleteComment"]
	return ok
}

func metaOf(update map[string]any) map[string]any {
	m, _ := update["meta"].(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	return m
}

// docKey — vendor `v2DocVersions[update.doc] = ...`: JS coerces the doc id
// (a string in practice) to the map key.
func docKey(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	// Numeric doc ids (vendor JS String(n)) — shortest representation.
	return fmt.Sprintf("%v", vNum(v))
}

func wireISO(ts time.Time) string {
	// Identical to vendor `toISOString()`: ms precision, always Z.
	return ts.UTC().Format("2006-01-02T15:04:05.000Z")
}

func normalizeTs(ts any) (time.Time, error) {
	switch v := ts.(type) {
	case float64:
		if v == 0 {
			return time.Time{}, errors.BadRequest("bad meta.ts")
		}
		return time.UnixMilli(int64(v)).UTC(), nil
	case int:
		if v == 0 {
			return time.Time{}, errors.BadRequest("bad meta.ts")
		}
		return time.UnixMilli(int64(v)).UTC(), nil
	case int64:
		if v == 0 {
			return time.Time{}, errors.BadRequest("bad meta.ts")
		}
		return time.UnixMilli(v).UTC(), nil
	case string:
		for _, layout := range []string{"2006-01-02T15:04:05.000Z", time.RFC3339Nano, time.RFC3339} {
			if t, err := time.Parse(layout, v); err == nil {
				return t, nil
			}
		}
		return time.Time{}, errors.BadRequest("bad meta.ts")
	default:
		return time.Time{}, errors.BadRequest("bad meta.ts")
	}
}

// convertToChange — vendor `_convertToChange(projectId, updateWithBlob)`.
func convertToChange(projectID string, uwb UpdateWithBlob) (*historyot.Change, error) {
	update := uwb.Update
	blobHashes := uwb.BlobHashes

	var operations []any
	var projectVersion any
	v2DocVersions := map[string]any{}

	if isRenameUpdate(update) {
		operations = []any{
			map[string]any{
				"pathname":    convertPathname(vStr(update["pathname"])),
				"newPathname": convertPathname(vStr(update["new_pathname"])),
			},
		}
		projectVersion = update["version"]
	} else if isAddUpdate(update) {
		file := map[string]any{"hash": blobHashes["file"]}
		// Vendor: `op.file.rangesHash = blobHashes.ranges` (may be absent).
		if isAddDocUpdate(update) {
			if r := blobHashes["ranges"]; r != nil {
				file["rangesHash"] = r
			}
		}
		// Vendor: `op.file.metadata = update.metadata` (may be absent).
		if isAddFileUpdate(update) {
			if m, ok := update["metadata"].(map[string]any); ok && len(m) > 0 {
				file["metadata"] = m
			}
		}
		operations = []any{
			map[string]any{
				"pathname": convertPathname(vStr(update["pathname"])),
				"file":     file,
			},
		}
		projectVersion = update["version"]
	} else if isHistoryOTEditOperationUpdate(update) {
		meta := metaOf(update)
		pathname := convertPathname(vStr(meta["pathname"]))
		if v, ok := update["v"]; ok && v != nil {
			v2DocVersions[docKey(update["doc"])] = map[string]any{"pathname": pathname, "v": v}
		}
		// Vendor: each raw op becomes `{ pathname, ...op }` (pathname
		// prepended so the update's op wins on shared keys — none collide
		// in practice; prepend-then-merge mirrors the vendor spread).
		opList, _ := update["op"].([]any)
		operations = make([]any, 0, len(opList))
		for _, oRaw := range opList {
			m := map[string]any{"pathname": pathname}
			if op, ok := oRaw.(map[string]any); ok {
				for k, val := range op {
					m[k] = val
				}
			}
			operations = append(operations, m)
		}
	} else if isTextUpdate(update) {
		meta := metaOf(update)
		pathname := convertPathname(vStr(meta["pathname"]))
		builder := newOperationsBuilder(docLengthOf(meta), pathname, meta)
		opList, _ := update["op"].([]any)
		for _, oRaw := range opList {
			op, _ := oRaw.(map[string]any)
			if err := builder.addOp(op); err != nil {
				return nil, err
			}
		}
		// Vendor: commit the final text op with contentHash when present.
		if h, has := meta["doc_hash"]; has && h != nil {
			builder.commitTextOperation(vStr(h))
		}
		operations = builder.finish()
		if v, ok := update["v"]; ok && v != nil {
			v2DocVersions[docKey(update["doc"])] = map[string]any{"pathname": pathname, "v": v}
		}
	} else if isSetCommentStateUpdate(update) {
		operations = []any{
			map[string]any{
				"pathname":  convertPathname(vStr(update["pathname"])),
				"commentId": vStr(update["commentId"]),
				"resolved":  update["resolved"],
			},
		}
	} else if isSetFileMetadataOperation(update) {
		operations = []any{
			map[string]any{
				"pathname": convertPathname(vStr(update["pathname"])),
				"metadata": update["metadata"],
			},
		}
	} else if isDeleteCommentUpdate(update) {
		operations = []any{
			map[string]any{
				"pathname":      convertPathname(vStr(update["pathname"])),
				"deleteComment": vStr(update["deleteComment"]),
			},
		}
	} else {
		return nil, errors.UpdateWithUnknownFormat("update with unknown format")
	}

	meta := metaOf(update)

	// v2Authors — vendor: `anonymous-user` -> [null], else
	// `_.compact([user_id])` ([] when the value is falsy).
	var v2Authors []any
	switch {
	case vStr(meta["user_id"]) == "anonymous-user":
		v2Authors = []any{nil}
	case jsTruthy(meta["user_id"]):
		v2Authors = []any{meta["user_id"]}
	default:
		v2Authors = []any{}
	}

	// origin — vendor: `metadata.origin` passthrough, else
	// `metadata.type === 'external' && metadata.source` -> { kind: source }.
	var origin any
	if o, has := meta["origin"]; has && o != nil {
		origin = o
	} else if vStr(meta["type"]) == "external" && jsTruthy(meta["source"]) {
		origin = map[string]any{"kind": vStr(meta["source"])}
	}

	// timestamp — vendor `new Date(update.meta.ts).toISOString()`; meta.ts is
	// epoch-ms (number) or an ISO wire string, both normalizing to the
	// ISO-ms wire form.
	ts, err := normalizeTs(meta["ts"])
	if err != nil {
		return nil, err
	}

	rawChange := map[string]any{
		"operations": operations,
		"v2Authors":  v2Authors,
		"timestamp":  wireISO(ts),
	}
	// Vendor: `projectVersion` is set only in the rename/add branches and is
	// dropped by `Change.toRaw` (`if (this.projectVersion)`) when falsy;
	// mirror by omitting from the raw input when falsy.
	if projectVersion != nil && jsTruthy(projectVersion) {
		rawChange["projectVersion"] = projectVersion
	}
	if len(v2DocVersions) > 0 {
		rawChange["v2DocVersions"] = v2DocVersions
	}
	if origin != nil {
		rawChange["origin"] = origin
	}

	change, err := historyot.ChangeFromRaw(rawChange)
	if err != nil {
		return nil, err
	}
	change.Operations = operationscompressor.CompressOperations(change.Operations)
	return change, nil
}

// docLengthOf — vendor `meta.history_doc_length ?? meta.doc_length`.
func docLengthOf(meta map[string]any) int {
	h, hOK := meta["history_doc_length"]
	if hOK && h != nil {
		return int(vNum(h))
	}
	return int(vNum(meta["doc_length"]))
}

// convertPathname — vendor `_convertPathname`:
//
//	strip the leading `/`;
//	every backslash -> `_`;
//	every `*` -> `__ASTERISK__`;
//	a leading space -> `__SPACE__` (top-level file);
//	every `/ ` (folder space) -> `/__SPACE__`.
//
// Ported as an ordered single-shot transformation (no regexp), matching
// the vendor regex chain.
func convertPathname(pathname string) string {
	// Strip leading `/`.
	if len(pathname) > 0 && pathname[0] == '/' {
		pathname = pathname[1:]
	}
	// `\` -> `_`, `*` -> `__ASTERISK__` (every occurrence, left to right —
	// both substitutions are disjoint so they may be single-pass).
	{
		out := make([]byte, 0, len(pathname))
		for i := 0; i < len(pathname); i++ {
			c := pathname[i]
			switch c {
			case '\\':
				out = append(out, '_')
			case '*':
				out = append(out, []byte("__ASTERISK__")...)
			default:
				out = append(out, c)
			}
		}
		pathname = string(out)
	}
	// Leading space -> `__SPACE__` (top-level file).
	if len(pathname) > 0 && pathname[0] == ' ' {
		pathname = "__SPACE__" + pathname[1:]
	}
	// `/ ` -> `/__SPACE__` (folder spaces, left to right).
	{
		out := make([]byte, 0, 2*len(pathname))
		for i := 0; i < len(pathname); i++ {
			if i+1 < len(pathname) && pathname[i] == '/' && pathname[i+1] == ' ' {
				out = append(out, []byte("/__SPACE__")...)
				i++ // consume the space
			} else {
				out = append(out, pathname[i])
			}
		}
		pathname = string(out)
	}
	return pathname
}
