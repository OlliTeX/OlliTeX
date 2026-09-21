package otc

import (
	"time"
)

// change_identity.go — 1:1 port of `lib/change_identity.js`. Identifies a
// change an editor client (or another writer) submitted, so that a resend of
// the same logical change is recognised as a duplicate and not applied twice.
//
// The identifying fields are the ones a rebase leaves alone: transforming a
// change rewrites its operations (so they cannot identify it), while the
// origin kind, the author and the timestamp travel through history untouched.
// The timestamp is compared as a point in time (Unix ms), not as a string, so
// two spellings of the same instant read as the same change.

// EditorChangeIdentity is the identity of a change (Node `EditorChangeIdentity`).
// Author is nil when the change is anonymous (Node `author == null`).
type EditorChangeIdentity struct {
	HistoryClientId string
	Author          *string
	Timestamp       int64 // Unix milliseconds since the epoch
}

// rawTimestampMs converts a raw wire timestamp to Unix ms, ok=false when it is
// unparseable (Node `Number.isNaN(new Date(raw.timestamp).getTime())`).
func rawTimestampMs(v any) (int64, bool) {
	switch t := v.(type) {
	case string:
		ts, err := parseRawTime(t)
		if err != nil {
			return 0, false
		}
		return ts.UnixMilli(), true
	case int64:
		return t, true
	case int:
		return int64(t), true
	case float64:
		return int64(t), true
	default:
		return 0, false
	}
}

// ChangeIdentityFromRaw — mirroring Node `editorChangeIdentity(raw)`: the
// identity of a raw change, or nil if it is not an identifiable editor change
// (another writer's, or one whose historyClientId was dropped when its chunk
// was written).
func ChangeIdentityFromRaw(raw map[string]any) *EditorChangeIdentity {
	origin, _ := raw["origin"].(map[string]any)
	if origin == nil {
		return nil
	}
	if kind, _ := origin["kind"].(string); kind != EditorOriginKind {
		return nil
	}
	// Absent on a change nothing has to recognise again, or on one whose id was
	// dropped when its chunk was written; an absent id must never read as a match.
	hcid, _ := origin["historyClientId"].(string)
	if hcid == "" {
		return nil
	}
	// Real-time stamps exactly one author on every change it forwards.
	authors, _ := raw["v2Authors"].([]any)
	if authors == nil || len(authors) != 1 {
		return nil
	}
	ms, ok := rawTimestampMs(raw["timestamp"])
	if !ok {
		return nil
	}
	var author *string
	if a, ok := authors[0].(string); ok {
		author = &a
	}
	return &EditorChangeIdentity{HistoryClientId: hcid, Author: author, Timestamp: ms}
}

// ChangeIdentityOf — mirroring Node `editorChangeIdentityOf({historyClientId,
// author, timestamp})`: build an identity from a client's own record of a
// change it submitted, for comparing against what comes back from history.
// author nil = anonymous (Node `author ?? null`).
func ChangeIdentityOf(historyClientId string, author *string, timestamp time.Time) *EditorChangeIdentity {
	return &EditorChangeIdentity{HistoryClientId: historyClientId, Author: author, Timestamp: timestamp.UnixMilli()}
}

// IsSameEditorChange — mirroring Node `isSameEditorChange(a, b)`: whether two
// identities name the same change. A nil identity matches nothing, including
// another nil.
func IsSameEditorChange(a, b *EditorChangeIdentity) bool {
	if a == nil || b == nil {
		return false
	}
	if a.HistoryClientId != b.HistoryClientId {
		return false
	}
	if (a.Author == nil) != (b.Author == nil) {
		return false
	}
	if a.Author != nil && *a.Author != *b.Author {
		return false
	}
	return a.Timestamp == b.Timestamp
}

// IsChangeFrom — mirroring Node `isChangeFrom(raw, {originKind, author,
// timestamp})`: whether a raw change is one a given (non-editor) writer
// placed. Compares the origin kind, membership of the author among v2Authors,
// and the timestamp as a point in time.
func IsChangeFrom(raw map[string]any, originKind, author string, timestamp time.Time) bool {
	if raw == nil {
		return false
	}
	origin, _ := raw["origin"].(map[string]any)
	if origin == nil {
		return false
	}
	if kind, _ := origin["kind"].(string); kind != originKind {
		return false
	}
	authors, _ := raw["v2Authors"].([]any)
	if authors == nil {
		return false
	}
	found := false
	for _, a := range authors {
		if s, ok := a.(string); ok && s == author {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	ms, ok := rawTimestampMs(raw["timestamp"])
	if !ok {
		return false
	}
	return ms == timestamp.UnixMilli()
}
