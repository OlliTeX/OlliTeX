package ot

import (
	"fmt"
	"time"
)

// TrackingProps is the tracking directive for an inserted or deleted range:
// who made the change and when.
//
// JS wire model for a raw tracking directive is {type, userId, ts} where
// ts is an ISO timestamp. CLSI changes never carry tracking directives, so
// this is a faithfulness shim needed to parse raw file data that does.
type TrackingProps struct {
	Type   string
	UserId string
	Ts     time.Time
}

// NewTrackingPropsFromRaw builds TrackingProps from raw {type, userId, ts}
// data. Mirrors TrackingProps.fromRaw.
func NewTrackingPropsFromRaw(t string, userId string, tsRaw *string) (*TrackingProps, error) {
	ts := time.Time{}
	if tsRaw != nil {
		parsed, err := time.Parse(time.RFC3339, *tsRaw)
		if err != nil {
			// new Date(raw.ts) would throw; CLSI raw always carries a valid
			// ISO string, so this is a faithfulness fallback, not a live path.
			return nil, fmt.Errorf("invalid tracking timestamp %q: %v", *tsRaw, err)
		}
		ts = parsed
	}
	return &TrackingProps{Type: t, UserId: userId, Ts: ts}, nil
}

// NewClearTrackingProps builds the marker for a cleared range: {type: 'none'}.
func NewClearTrackingProps() *ClearTrackingProps { return &ClearTrackingProps{} }

// ClearTrackingProps is the tracking directive for a range whose tracking
// was cleared (type "none").
type ClearTrackingProps struct{}

// ToRaw mirrors ClearTrackingProps.toRaw().
func (c *ClearTrackingProps) ToRaw() map[string]any { return map[string]any{"type": "none"} }

// --- TrackingProps methods — mirror the JS methods ---

// Equals mirrors equals().
func (t *TrackingProps) Equals(other *TrackingProps) bool {
	return other != nil && t.Type == other.Type && t.UserId == other.UserId && t.Ts.Equal(other.Ts)
}

// CanMergeWith mirrors canMergeWith(): type and userId must match (ts is
// ignored).
func (t *TrackingProps) CanMergeWith(other *TrackingProps) bool {
	return other != nil && t.Type == other.Type && t.UserId == other.UserId
}

// MergeWith mirrors mergeWith(): the earlier timestamp wins.
func (t *TrackingProps) MergeWith(other *TrackingProps) (*TrackingProps, error) {
	if !t.CanMergeWith(other) {
		return nil, fmt.Errorf("Cannot merge with incompatible tracking props")
	}
	ts := t.Ts
	if other.Ts.Before(t.Ts) {
		ts = other.Ts
	}
	return &TrackingProps{Type: t.Type, UserId: t.UserId, Ts: ts}, nil
}

// ToRaw mirrors toRaw().
func (t *TrackingProps) ToRaw() map[string]any {
	return map[string]any{"type": t.Type, "userId": t.UserId, "ts": t.Ts.UTC().Format(time.RFC3339)}
}

// trackingFromRaw parses a raw tracking directive map into either a
// TrackingProps or a ClearTrackingProps (the two runtime shapes).
func trackingFromRaw(raw map[string]any) (tp *TrackingProps, clear bool) {
	if raw == nil {
		return nil, false
	}
	if t, ok := raw["type"].(string); ok && t == "none" {
		return nil, true
	}
	t, _ := raw["type"].(string)
	userId, _ := raw["userId"].(string)
	tsRaw, _ := raw["ts"].(string)
	var tsPtr *string
	if raw["ts"] != nil {
		s := tsRaw
		tsPtr = &s
	}
	var err error
	tp, err = NewTrackingPropsFromRaw(t, userId, tsPtr)
	if err != nil {
		panic(err) // faithful: fromRaw throws
	}
	return tp, false
}
