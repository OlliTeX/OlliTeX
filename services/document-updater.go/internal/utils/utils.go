// Package utils — 1:1 port of `app/js/Utils.js`.
// Op shape, doc length, tracked-delete re-hydration, doc hashing, and the
// origin/source wire helper.
//
// Node `Op` is one of { i, d, c, p } — only one of i/d/c is present; p is the
// position (or, after serialization, an op-range).
package utils

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
)

// Op mirrors Node `Op` — { i?: string, d?: string, c?: Comment, p: number }.
// `P` is the UTF-16 position of the op (JSON: "p").
type Op struct {
	I *string `json:"i,omitempty"`
	D *string `json:"d,omitempty"`
	C any     `json:"c,omitempty"`
	P int     `json:"p"`
}

// Metadata mirrors the per-change metadata: { user_id?, ts?, ... }.
// Resolved mirrors the sharejs-comment metadata { resolved: bool } the
// vendor cloneDeep passes through unchanged (the vendored comment
// metadata carries `resolved`, not user_id/ts).
type Metadata struct {
	UserID   string `json:"user_id,omitempty"`
	TS       string `json:"ts,omitempty"`
	Resolved *bool  `json:"resolved,omitempty"`
}

// TrackedChange mirrors Node `TrackedChange` — { id?, op, metadata? }.
type TrackedChange struct {
	ID       *string   `json:"id,omitempty"`
	Op       Op        `json:"op"`
	Metadata *Metadata `json:"metadata,omitempty"`
}

// IsInsert reports whether the op is an insert.
func IsInsert(op Op) bool { return op.I != nil }

// IsDelete reports whether the op is a delete.
func IsDelete(op Op) bool { return op.D != nil }

// IsComment reports whether the op is a comment.
func IsComment(op Op) bool { return op.C != nil }

// GetDocLength returns the character length of a doc built from lines:
// sum of line lengths + (len-1) newlines (a nonempty list).
//
//	Node: `getDocLength(lines)`.
func GetDocLength(lines []string) int {
	length := 0
	for _, l := range lines {
		length += len(l)
	}
	if length > 0 {
		length += len(lines) - 1
	}
	return length
}

// AddTrackedDeletesToContent re-inserts tracked deletes into content so the
// result matches the history service's (hash-)content.
//
// Deletes are applied in ascending order by position; inserts are skipped.
//
//	Node: `addTrackedDeletesToContent(content, trackedChanges)`.
func AddTrackedDeletesToContent(content string, trackedChanges []TrackedChange) string {
	cursor := 0
	var result strings.Builder
	for _, change := range trackedChanges {
		if IsDelete(change.Op) {
			result.WriteString(content[cursor:change.Op.P])
			cursor = change.Op.P
			result.WriteString(*change.Op.D)
		}
	}
	result.WriteString(content[cursor:])
	return result.String()
}

// ComputeDocHash returns the SHA-1 hex hash of the document (lines joined by
// "\n", the last line with no trailing newline), sent to the history service
// to validate updates.
//
//	Node: `computeDocHash(lines)`.
func ComputeDocHash(lines []string) string {
	h := sha1.New()
	for i, line := range lines {
		if i < len(lines)-1 {
			h.Write([]byte(line + "\n"))
		} else {
			h.Write([]byte(line))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ExtractOriginOrSource mirrors Node `extractOriginOrSource(originOrSource)`:
// a string is a source, an object is an origin.
//
// Returns (source, origin) where the unused one is a zero value.
func ExtractOriginOrSource(originOrSource any) (source string, origin any) {
	switch v := originOrSource.(type) {
	case string:
		return v, nil
	case map[string]any:
		return "", v
	}
	return "", nil
}
