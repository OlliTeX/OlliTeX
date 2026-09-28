package updatetranslator

import (
	"sort"
	"time"

	"ollitex/go/services/project-history/internal/errors"
	"ollitex/go/services/project-history/internal/opmodel"
	"ollitex/go/services/project-history/internal/utils"
)

// OperationsBuilder — vendor `class OperationsBuilder`: builds the raw
// text-operation scan-op list + inline comment operations from the update's
// raw op list, in `meta.doc_length`/`meta.history_doc_length` coordinate
// space. Commits (splits) the text op stream on back-references, comment
// ops, and the trailing retain.
//
// The builder emits raw wire scan-op values (number / string / object),
// exactly what vendor's `textOperation.push` produces; `ChangeFromRaw`
// (opmodel `TextOperation.fromJSON`) then reconstructs typed scan ops and
// coalesces plain retains, matching vendor.
type OperationsBuilder struct {
	operations []any

	// Current text op buffer (raw wire scan ops) + cursor state.
	textOperation []any
	cursor        int
	docLength     int
	pathname      string

	// Tracking context from `meta` (read once; vendor reads `update.meta`
	// inside `addOp`).
	userId string
	tsWire string // `new Date(meta.ts).toISOString()`
	tcSet  bool
}

// newOperationsBuilder — vendor `new OperationsBuilder(docLength, pathname)`
// plus the `meta` values threaded for tracking directives.
func newOperationsBuilder(docLength int, pathname string, meta map[string]any) *OperationsBuilder {
	uid, _ := meta["user_id"].(string)
	ts, err := normalizeTs(meta["ts"])
	if err != nil {
		// meta.ts is normalized again in convertToChange; an invalid ts
		// there surfaces its error first, so this fallback is degraded-only.
		ts = time.Time{}
	}
	tcSet := hasKeyNonNil(meta, "tc")
	_ = uid
	return &OperationsBuilder{
		userId:    uid,
		tsWire:    wireISO(ts),
		tcSet:     tcSet,
		cursor:    0,
		docLength: docLength,
		pathname:  pathname,
	}
}

func hasKeyNonNil(m map[string]any, k string) bool {
	if m == nil {
		return false
	}
	_, ok := m[k]
	return ok && m[k] != nil
}

// finish — vendor `finish()`.
func (b *OperationsBuilder) finish() []any {
	b.commitTextOperation("")
	return b.operations
}

// commitTextOperation — vendor `commitTextOperation(opts)`:
//
//	if the buffer is non-empty, back-fill remains (retain up to
//	docLength); push `{pathname, textOperation[, contentHash]}`; reset the
//	buffer AND the cursor (even when the buffer was empty — a back-reference
//	commits the stream).
func (b *OperationsBuilder) commitTextOperation(contentHash string) {
	if len(b.textOperation) > 0 && b.cursor < b.docLength {
		b.retain(b.docLength-b.cursor, nil)
	}
	if len(b.textOperation) > 0 {
		opAny := map[string]any{
			"pathname":      b.pathname,
			"textOperation": b.textOperation,
		}
		if contentHash != "" {
			opAny["contentHash"] = contentHash
		}
		b.operations = append(b.operations, opAny)
		b.textOperation = nil
	}
	b.cursor = 0
}

// addOp — vendor `addOp(op, update)`.
func (b *OperationsBuilder) addOp(op map[string]any) error {
	if op == nil {
		return errors.UnexpectedOpType("unexpected op type")
	}
	pos := opPos(op, b.docLength)
	if utils.IsComment(op) {
		b.commitTextOperation("")
		var commentLength int
		if h, has := op["hlen"]; has && h != nil {
			commentLength = int(vNum(h))
		} else if c, _ := op["c"].(string); c != "" {
			commentLength = opmodel.Units(c)
		}
		commentId, _ := op["t"].(string)
		commentOp := map[string]any{
			"pathname":  b.pathname,
			"commentId": commentId,
			"ranges":    []any{},
		}
		if commentLength > 0 {
			commentOp["ranges"] = []any{map[string]any{"pos": float64(pos), "length": float64(commentLength)}}
		}
		if _, has := op["resolved"]; has && op["resolved"] != nil {
			commentOp["resolved"] = op["resolved"]
		}
		b.operations = append(b.operations, commentOp)
		return nil
	}
	if !utils.IsInsert(op) && !utils.IsDelete(op) && !utils.IsRetain(op) {
		return errors.UnexpectedOpType("unexpected op type")
	}
	if pos < b.cursor {
		b.commitTextOperation("")
	}
	if pos > b.cursor {
		b.retain(pos-b.cursor, nil)
	}
	if utils.IsInsert(op) {
		if tdr, _ := op["trackedDeleteRejection"].(bool); tdr {
			// Vendor pushes the RAW wire directive (a map), e.g.
			// {r, tracking: {type:'none'}}; opmodel.DecodeDirective
			// expects the raw wire shape, not a *Tracking.
			b.retain(opmodel.Units(opStr(op, "i")), opmodel.Clear().ToRaw())
		} else {
			opts := map[string]any{}
			if b.tcSet {
				opts["tracking"] = trackedDirective("insert", b.userId, b.tsWire)
			}
			if c, has := op["commentIds"]; has && c != nil {
				opts["commentIds"] = c
			}
			b.insert(opStr(op, "i"), opts)
		}
	}
	if r, has := op["r"]; has && r != nil {
		if tr, has := op["tracking"]; has && tr != nil {
			b.retain(opmodel.Units(opStr(op, "r")), tr)
		} else {
			b.retain(opmodel.Units(opStr(op, "r")), nil)
		}
	}
	if d, has := op["d"]; has && d != nil {
		b.deleteOp(op, opStr(op, "d"))
	}
	return nil
}

// opPos — vendor `Math.min(op.hpos ?? op.p, this.docLength)`.
func opPos(op map[string]any, docLength int) int {
	pos := vNum(op["p"])
	if h, has := op["hpos"]; has && h != nil {
		pos = vNum(h)
	}
	if pos > float64(docLength) {
		pos = float64(docLength)
	}
	return int(pos)
}

func opStr(op map[string]any, k string) string {
	s, _ := op[k].(string)
	return s
}

// retain — vendor `retain(length, {tracking?})`: pushes a wire scan op and
// advances the cursor. `tracking` nil => plain; a raw map => `{r, tracking}`
// wire.
func (b *OperationsBuilder) retain(n int, tracking any) {
	if n == 0 {
		return
	}
	if tracking != nil {
		b.textOperation = append(b.textOperation, map[string]any{"r": n, "tracking": tracking})
	} else {
		b.textOperation = append(b.textOperation, n)
	}
	b.cursor += n
}

func trackedDirective(typ, userId, tsWire string) map[string]any {
	return map[string]any{"type": typ, "userId": userId, "ts": tsWire}
}

// insert — vendor `insert(str, opts)`: pushes a wire scan op and advances
// the cursor and docLength.
func (b *OperationsBuilder) insert(str string, opts map[string]any) {
	if hasOpts(opts) {
		opAny := map[string]any{"i": str}
		if tr, has := opts["tracking"]; has && tr != nil {
			opAny["tracking"] = tr
		}
		if c, has := opts["commentIds"]; has && c != nil {
			opAny["commentIds"] = c
		}
		b.textOperation = append(b.textOperation, opAny)
	} else {
		b.textOperation = append(b.textOperation, str)
	}
	u := opmodel.Units(str)
	b.cursor += u
	b.docLength += u
}

func hasOpts(m map[string]any) bool {
	if tr, has := m["tracking"]; has && tr != nil {
		return true
	}
	if c, has := m["commentIds"]; has && c != nil {
		return true
	}
	return false
}

// delete — vendor `delete(length)`: pushes `-length`; the cursor is NOT
// advanced and docLength shrinks.
func (b *OperationsBuilder) delete(n int) {
	b.textOperation = append(b.textOperation, -n)
	b.docLength -= n
}

// deleteOp — vendor's `isDelete` branch:
//
//	changes = op.trackedChanges ?? []  (offset-ordered, delete before
//	insert at equal offset)
//	offset := each change:
//	  skip plain (or tracked-delete, when meta.tc is set) to change.offset
//	  delete: plain retain(change.length)
//	  insert: delete(change.length); offset += change.length
//	trailing: delete/repeat-to-plain(op.d.length - offset)
func (b *OperationsBuilder) deleteOp(op map[string]any, d string) {
	var changes []map[string]any
	if raw, has := op["trackedChanges"]; has && raw != nil {
		if list, ok := raw.([]any); ok {
			for _, c := range list {
				if m, ok := c.(map[string]any); ok {
					changes = append(changes, m)
				}
			}
		}
	}
	// Vendor `Array.prototype.sort` comparator: offset ascending; at equal
	// offset, deletes before inserts. Stable for same-offset same-type.
	sort.SliceStable(changes, func(i, j int) bool {
		a, c := changes[i], changes[j]
		ao, co := vNum(a["offset"]), vNum(c["offset"])
		if ao != co {
			return ao < co
		}
		at, ct := opStr(a, "type"), opStr(c, "type")
		if at == "delete" && ct == "insert" {
			return true
		}
		if at == "insert" && ct == "delete" {
			return false
		}
		return false
	})
	length := opmodel.Units(d)
	offset := 0
	for _, ch := range changes {
		changeOffset := int(vNum(ch["offset"]))
		changeLen := int(vNum(ch["length"]))
		changeType := opStr(ch, "type")
		if changeOffset > offset {
			if b.tcSet {
				b.retainTrackedDelete(changeOffset - offset)
			} else {
				b.delete(changeOffset - offset)
			}
			offset = changeOffset
		}
		if changeType == "delete" {
			b.retain(changeLen, nil)
		} else if changeType == "insert" {
			b.delete(changeLen)
			offset += changeLen
		}
	}
	if offset < length {
		if b.tcSet {
			b.retainTrackedDelete(length - offset)
		} else {
			b.delete(length - offset)
		}
	}
}

func (b *OperationsBuilder) retainTrackedDelete(n int) {
	b.retain(n, trackedDirective("delete", b.userId, b.tsWire))
}
