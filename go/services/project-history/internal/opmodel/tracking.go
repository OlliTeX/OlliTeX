package opmodel

import "time"

// Tracking ports the editor-core tracking-directive union:
//
//	tracking_props.js        TrackingProps{type: 'insert'|'delete', userId, ts: Date}
//	clear_tracking_props.js  ClearTrackingProps{type: 'none'}
//
// Vendor models them as two classes distinguished by instanceof; every
// behavioural method is reproduced here by the Type discriminator:
//
//   - ToRaw:      'none' -> {type:"none"};  else {type,userId,ts} (ISO ms precision)
//   - Equals:     'none' matches only 'none'; else type+userId+ts-equality
//   - CanMerge:   'none' matches only 'none'; else type+userId equality
//   - MergeWith:  'none' returns itself; else (type, userId, min(ts))
//
// nil (absent) and &Tracking{Type:"none"} (present, clearing) remain distinct,
// mirroring undefined vs a new ClearTrackingProps().
type Tracking struct {
	Type   string // "insert" | "delete" | "none"
	UserId string
	TsMs   int64 // epoch milliseconds (JS Date only has ms precision)
}

// NewTracking builds the tracking props from wire fields.
func NewTracking(ttype, userId string, tsMs int64) *Tracking {
	return &Tracking{Type: ttype, UserId: userId, TsMs: tsMs}
}

// Clear returns the "none" directive (mirrors `new ClearTrackingProps()`).
func Clear() *Tracking { return &Tracking{Type: "none"} }

// Equals mirrors TrackingProps.equals / ClearTrackingProps.equals.
func (t *Tracking) Equals(o *Tracking) bool {
	if o == nil {
		return false
	}
	if t.Type != o.Type {
		return false
	}
	return t.Type == "none" || (t.UserId == o.UserId && t.TsMs == o.TsMs)
}

// CanMergeWith mirrors TrackingProps.canMergeWith / ClearTrackingProps.canMergeWith.
func (t *Tracking) CanMergeWith(o *Tracking) bool {
	if o == nil {
		return false
	}
	if t.Type != o.Type {
		return false
	}
	return t.Type == "none" || t.UserId == o.UserId
}

// MergeWith mirrors both props classes' mergeWith (assumes CanMergeWith).
// Clear returns itself; tracked props merge to (this.type, this.userId, min ts).
func (t *Tracking) MergeWith(o *Tracking) *Tracking {
	if t.Type == "none" {
		return t
	}
	ts := t.TsMs
	if o.TsMs < ts {
		ts = o.TsMs
	}
	return &Tracking{Type: t.Type, UserId: t.UserId, TsMs: ts}
}

// ToRaw mirrors toRaw: wire shape {type} | {type, userId, ts: toISOString()}.
func (t *Tracking) ToRaw() map[string]any {
	if t.Type == "none" {
		return map[string]any{"type": "none"}
	}
	return map[string]any{
		"type":   t.Type,
		"userId": t.UserId,
		"ts":     isoMs(t.TsMs),
	}
}

// DecodeDirective converts a raw wire value ({type, userId?, ts?}) into a
// Tracking. Mirrors RetainOp.fromJSON's tracking branch:
//
//	op.tracking.type === 'none' ? new ClearTrackingProps() : TrackingProps.fromRaw
//
// A nil raw yields nil (absent).
func DecodeDirective(raw any) *Tracking {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	typ, _ := m["type"].(string)
	switch typ {
	case "none":
		return Clear()
	case "insert", "delete":
		user, _ := m["userId"].(string)
		return &Tracking{Type: typ, UserId: user, TsMs: DecodeWireTs(m["ts"])}
	default:
		return nil
	}
}

// DecodeWireTs parses a wire timestamp (ISO "…Z" string, ms precision) to
// epoch ms. A non-string / unparseable value yields 0, mirroring `new
// Date(undefined)` -> Invalid Date being unusable; callers only compare TsMs
// values that round-tripped through the same codec.
func DecodeWireTs(s any) int64 {
	str, ok := s.(string)
	if !ok {
		return 0
	}
	for _, layout := range []string{"2006-01-02T15:04:05.000Z", time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, str); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}

// isoMs formats epoch ms exactly like JS Date.toISOString():
// "YYYY-MM-DDTHH:MM:SS.mmmZ" (always 3-digit ms, always Z).
func isoMs(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z")
}
