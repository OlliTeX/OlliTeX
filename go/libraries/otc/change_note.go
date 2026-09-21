package otc

import oerror "ollitex/go/libraries/oerror"

// ChangeNote mirrors lib/change_note.js: returned by the server after it has
// applied a Change.
type ChangeNote struct {
	BaseVersion int
	Change      *Change
}

// NewChangeNote mirrors `new ChangeNote(baseVersion, change)`.
func NewChangeNote(baseVersion int, change *Change) *ChangeNote {
	return &ChangeNote{BaseVersion: baseVersion, Change: change}
}

// ToRaw (Node: toRaw). change is always present on a server-produced note; the
// Go build is defensive about a nil change (Node would throw on `change.toRaw()`
// for an undefined change).
func (n *ChangeNote) ToRaw() map[string]any {
	change := any(nil)
	if n.Change != nil {
		change = n.Change.ToRaw()
	}
	return map[string]any{"baseVersion": n.BaseVersion, "change": change}
}

// ToRawWithoutChange (Node: toRawWithoutChange).
func (n *ChangeNote) ToRawWithoutChange() map[string]any {
	return map[string]any{"baseVersion": n.BaseVersion}
}

// ChangeNoteFromRaw mirrors `ChangeNote.fromRaw`.
//
//	Node:
//	  assert.integer(raw.baseVersion, 'bad raw.baseVersion')
//	  assert.maybe.object(raw.change, 'bad raw.changes')
//	  return new ChangeNote(raw.baseVersion, Change.fromRaw(raw.change))
//
// change is optional (assert.maybe); when absent the Go note carries a nil
// *Change (documented Go leniency — Node would attempt Change.fromRaw(undefined)).
func ChangeNoteFromRaw(raw map[string]any) (*ChangeNote, error) {
	bv, ok := raw["baseVersion"].(int)
	if !ok {
		return nil, oerror.New("bad raw.baseVersion", nil)
	}
	var change *Change
	if rc, present := raw["change"]; present && rc != nil {
		rm, ok := rc.(map[string]any)
		if !ok {
			return nil, oerror.New("bad raw.changes", nil)
		}
		c, err := ChangeFromRaw(rm)
		if err != nil {
			return nil, err
		}
		change = c
	}
	return NewChangeNote(bv, change), nil
}

// GetBaseVersion (Node: getBaseVersion).
func (n *ChangeNote) GetBaseVersion() int { return n.BaseVersion }

// GetResultVersion (Node: getResultVersion) — baseVersion + 1.
func (n *ChangeNote) GetResultVersion() int { return n.BaseVersion + 1 }

// GetChange (Node: getChange).
func (n *ChangeNote) GetChange() *Change { return n.Change }
