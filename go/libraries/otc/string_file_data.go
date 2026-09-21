package otc

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// StringFileData mirrors file_data/string_file_data.js: fully-loaded string
// content + comments + tracked changes. It satisfies the FileData interface
// (embedding the base defaults for the methods it does not override: GetHash,
// GetRangesHash, and ToLazy which is "not implemented" for eager data).
type StringFileData struct {
	fileDataDefaults
	Content        string
	Comments       *CommentList
	TrackedChanges *TrackedChangeList
}

// NewStringFileData builds from content + raw collections
// (Node: `new StringFileData(content, rawComments, rawTrackedChanges)`).
func NewStringFileData(content string, rawComments []map[string]any, rawTracked []map[string]any) (*StringFileData, error) {
	comments, err := FromRawCommentList(rawComments)
	if err != nil {
		return nil, err
	}
	tracked, err := FromRawTrackedChangeList(rawTracked)
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

// IsEditable is always true for eager string data (Node: `isEditable`).
func (f *StringFileData) IsEditable() *bool {
	t := true
	return &t
}

// GetByteLength returns the UTF-8 byte length (Node: `Buffer.byteLength`).
func (f *StringFileData) GetByteLength() *int64 {
	n := int64(len(f.Content))
	return &n
}

// GetStringLength returns the UTF-16 code-unit length (Node: `content.length`).
func (f *StringFileData) GetStringLength() *int64 {
	n := int64(utf16Units(f.Content))
	return &n
}

// GetContent returns the content, optionally filtering tracked deletions
// (Node: `getContent`).
func (f *StringFileData) GetContent(filterTrackedDeletes bool) *string {
	out := f.contentString(filterTrackedDeletes)
	return &out
}

func (f *StringFileData) contentString(filterTrackedDeletes bool) string {
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
	return strings.Split(f.contentString(true), "\n")
}

// ToStats returns size statistics (Node: `toStats`).
func (f *StringFileData) ToStats() map[string]any {
	nComments := 0
	if f.Comments != nil {
		nComments = f.Comments.Len()
	}
	nTracked := 0
	if f.TrackedChanges != nil {
		nTracked = f.TrackedChanges.Len()
	}
	stats := map[string]any{
		"nContent":           1,
		"contentSize":        len(f.Content),
		"nComments":          nComments,
		"commentsSize":       0,
		"nTrackedChanges":    nTracked,
		"trackedChangesSize": 0,
	}
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

// GetComments returns the comment list (Node: `getComments`).
func (f *StringFileData) GetComments() *CommentList { return f.Comments }

// GetTrackedChanges returns the tracked-change list (Node: `getTrackedChanges`).
func (f *StringFileData) GetTrackedChanges() *TrackedChangeList {
	return f.TrackedChanges
}

// ToEager returns this data (it is already eager) (Node: `toEager`).
func (f *StringFileData) ToEager(context.Context, BlobStore) (FileData, error) {
	return f, nil
}

// Edit applies an edit operation in place (Node: `edit`).
func (f *StringFileData) Edit(op EditOperation) error {
	return op.Apply(f)
}

// ToHollow collapses to a HollowStringFileData of the same lengths
// (Node: `toHollow`).
func (f *StringFileData) ToHollow(context.Context, BlobStore) (FileData, error) {
	byteLength := int64(len(f.Content))
	stringLength := int64(utf16Units(f.Content))
	return CreateHollow(byteLength, &stringLength), nil
}

// Store writes the content (and the ranges object when it has comments or
// tracked changes) and returns {hash, rangesHash?} (Node: `store`).
func (f *StringFileData) Store(ctx context.Context, bs BlobStore) (map[string]any, error) {
	blob, err := bs.PutString(ctx, f.Content)
	if err != nil {
		return nil, err
	}
	hasRanges := (f.Comments != nil && f.Comments.Len() > 0) ||
		(f.TrackedChanges != nil && f.TrackedChanges.Len() > 0)
	if hasRanges {
		ranges := map[string]any{
			"comments":       f.GetComments().ToRaw(),
			"trackedChanges": f.GetTrackedChanges().ToRaw(),
		}
		rangesBlob, err := bs.PutObject(ctx, ranges)
		if err != nil {
			return nil, err
		}
		return map[string]any{"hash": blob.Hash, "rangesHash": rangesBlob.Hash}, nil
	}
	return map[string]any{"hash": blob.Hash}, nil
}

// runeCountInString is re-exported so callers depending on the old rune-based
// string length keep a stable helper.
func runeCountInString(s string) int { return utf8.RuneCountInString(s) }
