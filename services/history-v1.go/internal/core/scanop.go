package core

import "encoding/json"

// --- Raw scan operations (wire) + TextOperation (typed).
//
// Wire (golden-locked):
//   retain:  number | {"r":n,"tracking":{...}}
//   insert:  string | {"i":s, "commentIds":[..], "tracking":{...}}
//   remove:  number (negative, -length)
// TextOperation wire = {"textOperation":[...], "contentHash":...?} (wrapped).

type trackingPtr struct{ v *TrackingProps }

type rawOp struct {
	isRetain   bool
	isInsert   bool
	isRemove   bool
	length     int
	insertion  string
	commentIDs []string
	tracking   *TrackingProps
}

// ScanOp — a single scan operation inside a text operation.
type ScanOp struct {
	r rawOp
}

// NewRetain, NewInsert, NewRemove constructors.
func NewRetain(length int, tracking *TrackingProps) *ScanOp {
	return &ScanOp{rawOp{isRetain: true, length: length, tracking: tracking}}
}

func NewInsert(insertion string, tracking *TrackingProps, commentIDs []string) *ScanOp {
	return &ScanOp{rawOp{isInsert: true, insertion: insertion, tracking: tracking, commentIDs: commentIDs}}
}

func NewRemove(length int) *ScanOp {
	return &ScanOp{rawOp{isRemove: true, length: length}}
}

// ToRaw exports the scan op to its wire form.
func (o *ScanOp) ToRaw() json.RawMessage {
	switch {
	case o.r.isRemove:
		b, _ := json.Marshal(-o.r.length)
		return b
	case o.r.isInsert:
		if o.r.tracking == nil && len(o.r.commentIDs) == 0 {
			b, _ := json.Marshal(o.r.insertion)
			return b
		}
		obj := struct {
			I          string            `json:"i"`
			Tracking   json.RawMessage   `json:"tracking,omitempty"`
			CommentIDs []json.RawMessage `json:"commentIds,omitempty"`
		}{}
		obj.I = o.r.insertion
		if o.r.tracking != nil {
			obj.Tracking = o.r.tracking.ToRaw()
		}
		for _, cid := range o.r.commentIDs {
			b, _ := json.Marshal(cid)
			obj.CommentIDs = append(obj.CommentIDs, b)
		}
		b, _ := json.Marshal(obj)
		return b
	default: // retain
		if o.r.tracking != nil {
			obj := struct {
				R        int             `json:"r"`
				Tracking json.RawMessage `json:"tracking"`
			}{o.r.length, o.r.tracking.ToRaw()}
			b, _ := json.Marshal(obj)
			return b
		}
		b, _ := json.Marshal(o.r.length)
		return b
	}
}

// ScanOpFromRaw ports ScanOp.fromJSON dispatch: retain/insert/remove.
func ScanOpFromRaw(b json.RawMessage) (*ScanOp, error) {
	peek := byte('z')
	if len(b) > 0 {
		peek = b[0]
	}
	switch peek {
	case '[':
		return nil, &UnprocessableError{Msg: "unknown operation: " + string(b)}
	case '"':
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return nil, &UnprocessableError{Msg: "Invalid ScanOp " + string(b)}
		}
		return NewInsert(s, nil, nil), nil
	case '{':
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, &UnprocessableError{Msg: "Invalid ScanOp " + string(b)}
		}
		if r, ok := m["r"]; ok {
			var n int
			_ = json.Unmarshal(r, &n)
			var tracking *TrackingProps
			if tr, ok := m["tracking"]; ok {
				t, err := TrackingPropsFromRaw(tr)
				if err != nil {
					return nil, err
				}
				tracking = t
			}
			return NewRetain(n, tracking), nil
		}
		if i, ok := m["i"]; ok {
			var s string
			_ = json.Unmarshal(i, &s)
			var tracking *TrackingProps
			if tr, ok := m["tracking"]; ok {
				t, _ := TrackingPropsFromRaw(tr)
				tracking = t
			}
			var commentIDs []string
			if c, ok := m["commentIds"]; ok {
				_ = json.Unmarshal(c, &commentIDs)
			}
			return NewInsert(s, tracking, commentIDs), nil
		}
		return nil, &UnprocessableError{Msg: "unknown operation: " + string(b)}
	default:
		var n int
		if err := json.Unmarshal(b, &n); err != nil {
			return nil, &UnprocessableError{Msg: "Invalid ScanOp " + string(b)}
		}
		if n < 0 {
			return NewRemove(-n), nil
		}
		return NewRetain(n, nil), nil
	}
}

// TextOp — typed text operation (scan ops + optional contentHash).
type TextOp struct {
	Ops         []*ScanOp
	ContentHash string
}

// BaseLength — Node TextOperation.baseLength: the length of every string the
// operation can be applied to. Node accumulates baseLength for BOTH retain
// and remove ops (input length consumed); insert only affects targetLength.
func (o *TextOp) BaseLength() int {
	n := 0
	for _, op := range o.Ops {
		if op.r.isRetain || op.r.isRemove {
			n += op.r.length
		}
	}
	return n
}

// IsNoop ports TextOperation.isNoop.
func (o *TextOp) IsNoop() bool {
	if len(o.Ops) == 0 {
		return true
	}
	if len(o.Ops) == 1 {
		op := o.Ops[0]
		if op.r.isRetain && op.r.tracking == nil {
			return true
		}
	}
	return false
}

// ToRaw — wrapped wire form (golden-locked shape).
func (o *TextOp) ToRaw() json.RawMessage {
	ops := make([]json.RawMessage, 0, len(o.Ops))
	for _, op := range o.Ops {
		ops = append(ops, op.ToRaw())
	}
	obj := struct {
		TextOperation []json.RawMessage `json:"textOperation"`
		ContentHash   string            `json:"contentHash,omitempty"`
	}{ops, o.ContentHash}
	b, _ := json.Marshal(obj)
	return b
}
