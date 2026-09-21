package otc

import (
	"fmt"
	"time"
)

// TrackingDirective is the union of TrackingProps and ClearTrackingProps
// (Node's `TrackingDirective` type: the two duck-typed tracking classes).
type TrackingDirective interface {
	isTrackingDirective()
	Equals(other TrackingDirective) bool
	CanMergeWith(other TrackingDirective) bool
	MergeWith(other TrackingDirective) (TrackingDirective, error)
	ToRaw() any
}

type (
	// TrackingPropsMarker ensures interface satisfaction.
	TrackingPropsMarker = TrackingDirective
)

// isoDate renders a UTC time as an ISO-8601 millisecond string (Node:
// `Date.toISOString()`), e.g. "2024-01-01T00:00:00.000Z".
func isoDate(t time.Time) string {
	t = t.UTC()
	ms := int(t.Nanosecond()/1e6) % 1000
	return t.Format("2006-01-02T15:04:05") + fmt.Sprintf(".%03dZ", ms)
}

// parseISODate parses an ISO-8601 string (millisecond precision) to a time.Time.
func parseISODate(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}

// === TrackingProps (Node: file_data/tracking_props.js) =====================

// TrackingProps is a tracked-change directive: {type: 'insert'|'delete',
// userId, ts} (Node: TrackingProps).
type TrackingProps struct {
	Type   string
	UserID string
	TS     time.Time
}

func (TrackingProps) isTrackingDirective() {}

// NewTrackingProps builds TrackingProps (Node: `new TrackingProps(type, userId, ts)`).
func NewTrackingProps(trackingType, userID string, ts time.Time) TrackingProps {
	return TrackingProps{Type: trackingType, UserID: userID, TS: ts}
}

// FromRawTrackingProps builds TrackingProps from a raw {type,userId,ts}
// (Node: `TrackingProps.fromRaw`).
func FromRawTrackingProps(raw map[string]any) (TrackingProps, error) {
	trackingType, _ := raw["type"].(string)
	userID, _ := raw["userId"].(string)
	tsStr, _ := raw["ts"].(string)
	ts, err := parseISODate(tsStr)
	if err != nil {
		return TrackingProps{}, err
	}
	return TrackingProps{Type: trackingType, UserID: userID, TS: ts}, nil
}

// ToRaw serialises {type, userId, ts} (Node: `toRaw`).
func (t TrackingProps) ToRaw() any {
	return map[string]any{"type": t.Type, "userId": t.UserID, "ts": isoDate(t.TS)}
}

// Equals compares type+userId+ts (Node: `equals`).
func (t TrackingProps) Equals(other TrackingDirective) bool {
	o, ok := asTrackingProps(other)
	if !ok {
		return false
	}
	return t.Type == o.Type && t.UserID == o.UserID && t.TS.Equal(o.TS)
}

// CanMergeWith reports compatible type+userId (Node: `canMergeWith`).
func (t TrackingProps) CanMergeWith(other TrackingDirective) bool {
	o, ok := asTrackingProps(other)
	if !ok {
		return false
	}
	return t.Type == o.Type && t.UserID == o.UserID
}

// MergeWith merges, keeping the lower timestamp (Node: `mergeWith`).
func (t TrackingProps) MergeWith(other TrackingDirective) (TrackingDirective, error) {
	o, ok := asTrackingProps(other)
	if !ok {
		return nil, fmt.Errorf("Cannot merge with incompatible tracking props")
	}
	if !t.CanMergeWith(other) {
		return nil, fmt.Errorf("Cannot merge with incompatible tracking props")
	}
	ts := t.TS
	if o.TS.Before(t.TS) {
		ts = o.TS
	}
	return TrackingProps{Type: t.Type, UserID: t.UserID, TS: ts}, nil
}

func asTrackingProps(d TrackingDirective) (TrackingProps, bool) {
	if p, ok := d.(TrackingProps); ok {
		return p, true
	}
	if p, ok := d.(*TrackingProps); ok {
		return *p, true
	}
	return TrackingProps{}, false
}

// === ClearTrackingProps (Node: file_data/clear_tracking_props.js) ===========

// ClearTrackingProps removes tracking (Node: ClearTrackingProps, `type:'none'`).
type ClearTrackingProps struct{}

func (ClearTrackingProps) isTrackingDirective() {}

// Equals is true when other is a ClearTrackingProps (Node: `equals`).
func (ClearTrackingProps) Equals(other TrackingDirective) bool {
	_, ok := other.(ClearTrackingProps)
	if !ok {
		_, ok = other.(*ClearTrackingProps)
	}
	return ok
}

// CanMergeWith is true when other is a ClearTrackingProps (Node: `canMergeWith`).
func (ClearTrackingProps) CanMergeWith(other TrackingDirective) bool {
	_, ok := other.(ClearTrackingProps)
	if !ok {
		_, ok = other.(*ClearTrackingProps)
	}
	return ok
}

// MergeWith returns this (Node: `mergeWith`).
func (c ClearTrackingProps) MergeWith(TrackingDirective) (TrackingDirective, error) {
	return c, nil
}

// ToRaw returns {type:'none'} (Node: `toRaw`).
func (ClearTrackingProps) ToRaw() any { return map[string]any{"type": "none"} }

// AsClearTrackingProps asserts a directive is a ClearTrackingProps.
func AsClearTrackingProps(d TrackingDirective) (ClearTrackingProps, bool) {
	if p, ok := d.(ClearTrackingProps); ok {
		return p, true
	}
	return ClearTrackingProps{}, false
}

// IsClearTrackingProps reports whether d is a ClearTrackingProps.
func IsClearTrackingProps(d any) bool {
	if d == nil {
		return false
	}
	if dir, ok := d.(TrackingDirective); ok {
		_, is := dir.(ClearTrackingProps)
		if is {
			return true
		}
		_, is = dir.(*ClearTrackingProps)
		return is
	}
	if _, ok := d.(ClearTrackingProps); ok {
		return true
	}
	_, ok := d.(*ClearTrackingProps)
	return ok
}
