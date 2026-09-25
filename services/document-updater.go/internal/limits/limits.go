// Package limits — 1:1 port of `app/js/Limits.js`.
// Character-size guards for documents and string file data.
package limits

// GetTotalSizeOfLines computes the total size of a document in characters,
// including newlines, from its lines.
//
//	Node: `getTotalSizeOfLines(lines)` — sum of (line.length + 1).
func GetTotalSizeOfLines(lines []string) int {
	size := 0
	for _, line := range lines {
		size += len(line) + 1
	}
	return size
}

// DocIsTooLarge reports whether a document is above `maxDocLength` characters.
//
// The estimated size should be an upper bound on the true size (typically the
// size of the JSON-stringified array of lines). If the estimate is under the
// limit we know the document is under it without summing.
//
//	Node: `docIsTooLarge(estimatedSize, lines, maxDocLength)`.
func DocIsTooLarge(estimatedSize int, lines []string, maxDocLength int) bool {
	if estimatedSize <= maxDocLength {
		return false
	}
	size := 0
	for _, line := range lines {
		size += len(line) + 1
		if size > maxDocLength {
			return true
		}
	}
	return false
}

// TrackedChange is the subset of a tracked-change record this package uses.
type TrackedChange struct {
	Type   string `json:"type"` // "insert" | "delete"
	Length int    `json:"length"`
}

// TrackedChangeRange is the nested `range`/`tracking` shape Node uses:
//
//	{ range: { pos, length }, tracking: { type, ts, userId } }
//
// The flat TrackedChange shape `{op, metadata}` is what the preview package
// uses. Both mirror their Node callers.
type TrackedChangeRange struct {
	Range    Range    `json:"range"`
	Tracking Tracking `json:"tracking"`
}

// Range mirrors `range: { pos, length }`.
type Range struct {
	Pos    int `json:"pos"`
	Length int `json:"length"`
}

// Tracking mirrors `tracking: { type, ts, userId }`.
type Tracking struct {
	Type   string `json:"type"` // "insert" | "delete"
	TS     string `json:"ts"`
	UserID string `json:"userId"`
}

// StringFileRawData mirrors the raw string file data: { content, trackedChanges? }.
type StringFileRawData struct {
	Content        string               `json:"content"`
	TrackedChanges []TrackedChangeRange `json:"trackedChanges,omitempty"`
}

// StringFileDataContentIsTooLarge reports whether raw string file data exceeds
// `maxDocLength` characters, after subtracting the lengths of tracked deletes.
//
// Tracked inserts do NOT count (their text is already in `content`) — only
// tracked deletes are removed before measuring.
//
//	Node: `stringFileDataContentIsTooLarge(raw, maxDocLength)`.
func StringFileDataContentIsTooLarge(raw *StringFileRawData, maxDocLength int) bool {
	n := len(raw.Content)
	if n <= maxDocLength {
		return false
	}
	for _, tc := range raw.TrackedChanges {
		if tc.Tracking.Type != "delete" {
			continue
		}
		n -= tc.Range.Length
		if n <= maxDocLength {
			return false
		}
	}
	return true
}
