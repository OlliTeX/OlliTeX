package core

import (
	"encoding/json"
)

func isRetain(op *ScanOp) bool { return op.r.isRetain }
func isInsert(op *ScanOp) bool { return op.r.isInsert }
func isRemove(op *ScanOp) bool { return op.r.isRemove }

// ApplyFile ports TextOperation.apply (Node order, stringLength in UTF-16
// units). baseLength check → comments ApplyDelete/ApplyInsert accumulate →
// inputCursor check → trackedChanges.applyTextOperation (Node does this
// BEFORE file.content is assigned) → file.content = result → TooLong thrown
// AFTER the mutation (Node: for persist purposes too-long = validation
// failure on the applied file).
func (o *TextOp) ApplyFile(file *File) error {
	baseUnits := utf16Decode(file.Content)
	baseLength := len(baseUnits)
	if baseLength != o.BaseLength() {
		return &ApplyError{Msg: "The operation's base length must be equal to the string's length.", Op: o.ToRaw(), Content: file.Content}
	}
	newUnits := make([]uint16, 0, baseLength+64)
	inputCursor := 0
	for _, op := range o.Ops {
		if isRetain(op) {
			if inputCursor+op.r.length > baseLength {
				return &ApplyError{Msg: "Operation can't retain more chars than are left in the string.", Op: op.ToRaw(), Content: file.Content}
			}
			newUnits = append(newUnits, baseUnits[inputCursor:inputCursor+op.r.length]...)
			inputCursor += op.r.length
			continue
		}
		if isInsert(op) {
			insertedUnits := utf16Decode(op.r.insertion)
			file.Comments = file.Comments.ApplyInsert(len(newUnits), len(insertedUnits), op.r.commentIDs)
			newUnits = append(newUnits, insertedUnits...)
			continue
		}
		if isRemove(op) {
			file.Comments = file.Comments.ApplyDelete(Range{Pos: len(newUnits), Length: op.r.length})
			inputCursor += op.r.length
			continue
		}
		return &UnprocessableError{Msg: "Unknown ScanOp type during apply"}
	}
	if inputCursor != baseLength {
		return &ApplyError{Msg: "The operation didn't operate on the whole string.", Op: o.ToRaw(), Content: file.Content}
	}
	newStr := utf16Encode(newUnits)
	if utf16Length(newStr) > MaxStringLength {
		return &TooLongError{NewLength: utf16Length(newStr)}
	}
	file.TrackedChanges.ApplyTextOperation(o.Ops)
	file.Content = newStr
	return nil
}

// ApplyToLength ports TextOperation.applyToLength. In Node str.length counts
// UTF-16 code units (supplementary chars = 2). For byte-level fidelity we
// keep the unit count here as-is; the caller passes UTF-16 lengths.
func (o *TextOp) ApplyToLength(baseLength int) (int, error) {
	if baseLength != o.BaseLength() {
		return 0, &ApplyError{Msg: "The operation's base length must be equal to the string's length.", Op: o.ToRaw(), Content: ""}
	}
	length := 0
	inputCursor := 0
	for _, op := range o.Ops {
		if isRetain(op) {
			if inputCursor+op.r.length > baseLength {
				return 0, &ApplyError{Msg: "Operation can't retain more chars than are left in the string.", Op: op.ToRaw(), Content: ""}
			}
			length += op.r.length
			inputCursor += op.r.length
		} else if isInsert(op) {
			length += len(op.r.insertion)
		} else if isRemove(op) {
			inputCursor += op.r.length
		}
	}
	if inputCursor != baseLength {
		return 0, &ApplyError{Msg: "The operation didn't operate on the whole string.", Op: o.ToRaw(), Content: ""}
	}
	if length > MaxStringLength {
		return 0, &TooLongError{NewLength: length}
	}
	return length, nil
}

// TextOpFromRaw ports TextOperation.fromJSON: {textOperation:[...],
// contentHash?}.
func TextOpFromRaw(raw json.RawMessage) (*TextOp, error) {
	var m struct {
		Ops         []json.RawMessage `json:"textOperation"`
		ContentHash *string           `json:"contentHash"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, &UnprocessableError{Msg: "bad TextOperation: " + err.Error()}
	}
	o := &TextOp{Ops: make([]*ScanOp, 0, len(m.Ops))}
	for _, ob := range m.Ops {
		op, err := ScanOpFromRaw(ob)
		if err != nil {
			return nil, err
		}
		o.Ops = append(o.Ops, op)
	}
	if m.ContentHash != nil {
		o.ContentHash = *m.ContentHash
	}
	return o, nil
}

// NewTextOp — helper (build_set_content minimal diff + tests).
func NewTextOp(ops ...*ScanOp) *TextOp { return &TextOp{Ops: ops} }
