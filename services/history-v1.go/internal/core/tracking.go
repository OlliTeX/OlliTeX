package core

import (
	"encoding/json"
	"time"
)

// TrackingProps — unified model for the Node union TrackingProps | ClearTrackingProps.
// Wire: {"type":"insert"|"delete","userId":..,"ts":ISO} | {"type":"none"} (Clear).
// Clear lives in the Clear==true state (the Node ClearTrackingProps class).
type TrackingProps struct {
	Type   string // "insert" | "delete" | "none"
	UserID string
	TSISO  string
	Clear  bool
}

// ToRaw ports TrackingProps.toRaw / ClearTrackingProps.toRaw.
func (t *TrackingProps) ToRaw() json.RawMessage {
	if t.Clear {
		return json.RawMessage(`{"type":"none"}`)
	}
	b, _ := json.Marshal(struct {
		Type   string `json:"type"`
		UserID string `json:"userId"`
		TS     string `json:"ts"`
	}{t.Type, t.UserID, t.TSISO})
	return b
}

// CanMergeWith — Node:
//
//	ClearTrackingProps.canMergeWith: other instanceof ClearTrackingProps.
//	TrackingProps.canMergeWith: other instanceof TrackingProps (never Clear)
//	&& type + userId equal.
//
// So: Clear merges only with Clear; live props only with equal live props.
func (t *TrackingProps) CanMergeWith(o *TrackingProps) bool {
	if t.Clear || o.Clear {
		return t.Clear && o.Clear
	}
	return t.Type == o.Type && t.UserID == o.UserID
}

// MergeWith — Node:
//
//	ClearTrackingProps.mergeWith: returns the Clear.
//	TrackingProps.mergeWith: requires canMergeWith, keeps min timestamp.
func (t *TrackingProps) MergeWith(o *TrackingProps) *TrackingProps {
	if !t.CanMergeWith(o) {
		panic("Cannot merge with incompatible tracking props")
	}
	if t.Clear {
		return &TrackingProps{Type: "none", Clear: true}
	}
	ts := t.TSISO
	if o.TSISO != "" && isoMillis(o.TSISO) < isoMillis(ts) {
		ts = o.TSISO
	}
	return &TrackingProps{Type: t.Type, UserID: t.UserID, TSISO: ts}
}

func isoMillis(s string) int64 {
	if s == "" {
		return 0
	}
	for _, layout := range []string{"2006-01-02T15:04:05.000Z07:00", time.RFC3339Nano} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}

// TrackingPropsFromRaw ports TrackingProps.fromRaw / the Clear dispatch:
// {"type":"none"} => Clear; else a live tracking prop with wire ts preserved.
func TrackingPropsFromRaw(b json.RawMessage) (*TrackingProps, error) {
	var m struct {
		Type   *string `json:"type"`
		UserID *string `json:"userId"`
		TS     *string `json:"ts"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, &BadRawError{Msg: "bad tracking raw " + string(b)}
	}
	if m.Type == nil {
		return &TrackingProps{}, nil
	}
	if *m.Type == "none" {
		return &TrackingProps{Type: "none", Clear: true}, nil
	}
	tp := &TrackingProps{Type: *m.Type}
	if m.UserID != nil {
		tp.UserID = *m.UserID
	}
	if m.TS != nil {
		tp.TSISO = *m.TS
	}
	return tp, nil
}
