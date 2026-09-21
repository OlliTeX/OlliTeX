package otc

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// StringFileData mirrors file_data/string_file_data.js (Phase A surface).
type StringFileData struct {
	Content        string
	Comments       *CommentList
	TrackedChanges *TrackedChangeList
}

// NewStringFileData builds from content + raw collections
// (Node: `new StringFileData(content, rawComments, rawTrackedChanges)`).
func NewStringFileData(content string, rawComments []map[string]any, rawTrackedChanges []map[string]any) (*StringFileData, error) {
	comments, err := FromRawCommentList(rawComments)
	if err != nil {
		return nil, err
	}
	tracked, err := FromRawTrackedChangeList(rawTrackedChanges)
	if err != nil {
		return nil, err
	}
	return &StringFileData{Content: content, Comments: comments, TrackedChanges: tracked}, nil
}

// FromRawStringFileData builds from raw (Node: `StringFileData.fromRaw`).
func FromRawStringFileData(raw map[string]any) (*StringFileData, error) {
	content, _ := raw["content"].(string)
	var rawComments, rawTracked []map[string]any
	if v, ok := raw["comments"]; ok && v != nil {
		rawComments = asRawObjects(v)
	}
	if v, ok := raw["trackedChanges"]; ok && v != nil {
		rawTracked = asRawObjects(v)
	}
	return NewStringFileData(content, rawComments, rawTracked)
}

// ToRaw serialises {content, comments?, trackedChanges?} (Node: `toRaw`).
func (f *StringFileData) ToRaw() map[string]any {
	raw := map[string]any{"content": f.Content}
	if f.Comments != nil && f.Comments.Len() > 0 {
		raw["comments"] = f.Comments.ToRaw()
	}
	if f.TrackedChanges != nil && f.TrackedChanges.Len() > 0 {
		raw["trackedChanges"] = f.TrackedChanges.ToRaw()
	}
	return raw
}

// IsEditable reports editability (Node: `isEditable`).
func (f *StringFileData) IsEditable() bool { return true }

// GetByteLength returns the UTF-8 byte length (Node: `getByteLength`).
func (f *StringFileData) GetByteLength() int { return len(f.Content) }

// GetStringLength returns the string length (Node: `getStringLength`).
func (f *StringFileData) GetStringLength() int { return utf8.RuneCountInString(f.Content) }

// GetContent returns the content, optionally filtering tracked deletions
// (Node: `getContent`).
func (f *StringFileData) GetContent(filterTrackedDeletes bool) string {
	if !filterTrackedDeletes {
		return f.Content
	}
	var sb strings.Builder
	cursor := 0
	if f.TrackedChanges != nil {
		for _, tc := range f.TrackedChanges.AsSorted() {
			if tp, ok := tpType(tc.Tracking); !ok || tp != "delete" {
				continue
			}
			if cursor < tc.Range.Start() {
				sb.WriteString(f.Content[cursor:tc.Range.Start()])
			}
			cursor = tc.Range.End()
		}
	}
	if cursor < len(f.Content) {
		sb.WriteString(f.Content[cursor:])
	}
	return sb.String()
}

func tpType(d any) (string, bool) {
	if dir, ok := d.(TrackingDirective); ok {
		if tp, okTP := dir.(TrackingProps); okTP {
			return tp.Type, true
		}
	}
	return "", false
}

// GetLines splits the filtered content on newlines (Node: `getLines`).
func (f *StringFileData) GetLines() []string {
	return strings.Split(f.GetContent(true), "\n")
}

// ToStats returns size statistics (Node: `toStats`).
func (f *StringFileData) ToStats() map[string]int {
	stats := map[string]int{"nContent": 1, "contentSize": len(f.Content)}
	nComments := 0
	if f.Comments != nil {
		nComments = f.Comments.Len()
	}
	nTracked := 0
	if f.TrackedChanges != nil {
		nTracked = f.TrackedChanges.Len()
	}
	stats["nComments"] = nComments
	stats["nTrackedChanges"] = nTracked
	if nComments > 0 {
		b, _ := json.Marshal(f.Comments.ToRaw())
		stats["commentsSize"] = len(b)
	}
	if nTracked > 0 {
		b, _ := json.Marshal(f.TrackedChanges.ToRaw())
		stats["trackedChangesSize"] = len(b)
	}
	return stats
}
