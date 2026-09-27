package collab

// anchors.go — D40-P3: relative-position anchoring for review ranges.
//
// P1 stores review ranges (thread comment ranges, tracked-change
// start/end) as PLAIN coordinates. Those drift as the content edits: a
// comment anchored at [10,30) silently means "the 10..30 of the text AT
// CREATION TIME" once text is inserted before it.
//
// P3 adds STABLE anchors at creation time using ygo's RelativePosition
// API — the same construct as Yjs `Y.RelativePosition` (the wire format
// is Yjs-compatible, so a future client-side selection-anchoring slice
// can share the codec verbatim):
//
//   - start anchor: assoc >= 0 — anchored AFTER the item at `start`.
//     Insertions before the range shift it along with the text.
//   - end anchor:   assoc <  0 — anchored BEFORE the item at `end`.
//     Insertions inside the range push the end out (the range stays over
//     the originally-anchored text).
//
// Anchors are stored ADDITIVELY next to the P1 plain coordinates
// (old records keep working; live resolution falls back to the plain
// coords when no anchors exist — the documented P1 drift). Resolution is
// batched (one doc load for many ranges) because read paths (d10/d12)
// resolve per-entry.
//
// Deletion semantics (Yjs toAbsolutePosition parity): an anchor on a
// deleted item resolves to the nearest surviving boundary — ranges
// shrink around surviving text rather than vanishing (honest-oracle:
// we do not invent a position the client could not predict).

import (
	"context"
	"encoding/base64"
	"errors"

	"github.com/reearth/ygo/crdt"
	"github.com/reearth/ygo/persistence"
)

// AnchorRange — stable anchors for one (start, end) range. Both fields
// are Yjs-wire-relative-positions, url-safe base64 (Y.Map values are
// JSON-ish; base64-URL keeps them portable through both the Y doc and
// any REST surface that echoes them).
type AnchorRange struct {
	Start string // "" = no start anchor
	End   string // "" = no end anchor
}

// ErrAnchorResolve — the stored position is undecodable or unresolvable
// against the room (e.g. room content type missing).
var ErrAnchorResolve = errors.New("collab: cannot resolve stored anchors")

func encodeAnchor(rp crdt.RelativePosition) string {
	return base64.URLEncoding.EncodeToString(crdt.EncodeRelativePosition(rp))
}

func decodeAnchor(s string) (crdt.RelativePosition, error) {
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return crdt.RelativePosition{}, ErrAnchorResolve
	}
	rp, err := crdt.DecodeRelativePosition(b)
	if err != nil {
		return crdt.RelativePosition{}, ErrAnchorResolve
	}
	return rp, nil
}

// headDoc — the room's Y.Doc at head (the anchor operations are read-only
// against the CURRENT content; they never append a version).
func headDoc(ctx context.Context, store persistence.VersionedPersistence, room string) (*crdt.Doc, error) {
	lr, err := store.Load(ctx, room)
	if err != nil {
		return nil, err
	}
	d := crdt.New()
	if err := crdt.ApplyUpdateV1(d, lr.Update, nil); err != nil {
		return nil, err
	}
	return d, nil
}

func intOrZero(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// MakeRangeAnchors — anchor a plain (start, end) against the room's head
// content. Coordinates clamp to [0, len] (honest pin: out-of-range
// anchors resolve to the boundary, never error).
func MakeRangeAnchors(ctx context.Context, store persistence.VersionedPersistence, room string, start, end int) (AnchorRange, error) {
	d, err := headDoc(ctx, store, room)
	if err != nil {
		return AnchorRange{}, err
	}
	text := d.GetText(TextType)
	if text == nil {
		return AnchorRange{}, ErrAnchorResolve
	}
	n := text.Len()
	if start < 0 {
		start = 0
	}
	if end < 0 {
		end = 0
	}
	if start > n {
		start = n
	}
	if end > n {
		end = n
	}
	srp := crdt.CreateRelativePositionFromIndex(text, start, 0)
	erp := crdt.CreateRelativePositionFromIndex(text, end, -1)
	return AnchorRange{
		Start: encodeAnchor(srp),
		End:   encodeAnchor(erp),
	}, nil
}

// ResolveRangeAnchors — the LIVE (start, end) of stored anchors against
// the room's head. Empty anchor fields keep the plain value (P1 record
// or partial anchor).
func ResolveRangeAnchors(ctx context.Context, store persistence.VersionedPersistence, room string, ar AnchorRange, plainStart, plainEnd int) (int, int, error) {
	if ar.Start == "" && ar.End == "" {
		return plainStart, plainEnd, nil
	}
	d, err := headDoc(ctx, store, room)
	if err != nil {
		return plainStart, plainEnd, err
	}
	start, end := plainStart, plainEnd
	if ar.Start != "" {
		rp, err := decodeAnchor(ar.Start)
		if err != nil {
			return 0, 0, err
		}
		abs, ok := crdt.ToAbsolutePosition(d, rp)
		if !ok {
			return 0, 0, ErrAnchorResolve
		}
		start = abs.Index
	}
	if ar.End != "" {
		rp, err := decodeAnchor(ar.End)
		if err != nil {
			return 0, 0, err
		}
		abs, ok := crdt.ToAbsolutePosition(d, rp)
		if !ok {
			return 0, 0, ErrAnchorResolve
		}
		end = abs.Index
	}
	if end < start {
		end = start
	}
	return start, end, nil
}

// ResolveLiveRanges — batch resolution of PLAIN-range records
// ({start, end, a:{s,e} optional}) into LIVE records: start/end are
// rewritten from the stored anchors when present (single doc load);
// `a` is echoed back so clients can adopt the same anchors. Records
// without anchors pass through (P1 drift — documented). Error surfaces
// only for records that HAVE anchors which fail to resolve (a record
// carrying broken anchors is a data bug worth a 500 at the read path).
func ResolveLiveRanges(ctx context.Context, store persistence.VersionedPersistence, room string, in []map[string]any) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(in))
	d := (*crdt.Doc)(nil)
	for _, r := range in {
		nr := map[string]any{}
		for k, v := range r {
			nr[k] = v
		}
		a, _ := r["a"].(map[string]any)
		ar := AnchorRange{
			Start: stringOf(a, "s"),
			End:   stringOf(a, "e"),
		}
		plainStart := intOrZero(r["start"])
		plainEnd := intOrZero(r["end"])
		if ar.Start != "" || ar.End != "" {
			if d == nil {
				var err error
				d, err = headDoc(ctx, store, room)
				if err != nil {
					return nil, err
				}
			}
			start, end := plainStart, plainEnd
			if ar.Start != "" {
				rp, err := decodeAnchor(ar.Start)
				if err != nil {
					return nil, err
				}
				abs, ok := crdt.ToAbsolutePosition(d, rp)
				if !ok {
					return nil, ErrAnchorResolve
				}
				start = abs.Index
			}
			if ar.End != "" {
				rp, err := decodeAnchor(ar.End)
				if err != nil {
					return nil, err
				}
				abs, ok := crdt.ToAbsolutePosition(d, rp)
				if !ok {
					return nil, ErrAnchorResolve
				}
				end = abs.Index
			}
			if end < start {
				end = start
			}
			nr["start"] = start
			nr["end"] = end
		}
		out = append(out, nr)
	}
	return out, nil
}

func stringOf(a map[string]any, k string) string {
	if a == nil {
		return ""
	}
	if v, ok := a[k].(string); ok {
		return v
	}
	return ""
}
