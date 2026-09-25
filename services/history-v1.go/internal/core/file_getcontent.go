package core

import "sort"

// GetContent ports File#getContent (Node: FileData.getContent dispatch):
// string kind — with filterTrackedDeletes, drop the ranges whose
// tracking.type == "delete"; other kinds return "" (Node base returns null).
//
// Persist uses: file.load("eager"); file.getContent({filterTrackedDeletes:
// true}) — must be byte-exact against the Node result (git blob hash input).
func (f *File) GetContent(filterTrackedDeletes bool) (string, error) {
	if f.Kind != "string" {
		return "", nil
	}
	if !filterTrackedDeletes {
		return f.Content, nil
	}
	var deletes []TrackedChange
	for i := range f.TrackedChanges {
		tc := f.TrackedChanges[i]
		if tc.Tracking != nil && tc.Tracking.Type == "delete" {
			deletes = append(deletes, *tc)
		}
	}
	if len(deletes) == 0 {
		return f.Content, nil
	}
	sort.Slice(deletes, func(i, j int) bool { return deletes[i].Range.Pos < deletes[j].Range.Pos })
	// Node asSorted; stable by pos.
	// Node: this.content.slice(cursor, tc.range.start) — cursor and start are
	// UTF-16 positions. Work in UTF-16 units then re-encode.
	units := utf16Decode(f.Content)
	var out []uint16
	cursor := 0
	for _, tc := range deletes {
		start := tc.Range.Pos
		end := start + tc.Range.Length
		if cursor < start {
			out = append(out, units[cursor:start]...)
		}
		cursor = end
	}
	if cursor < len(units) {
		out = append(out, units[cursor:]...)
	}
	if out == nil {
		return "", nil
	}
	return utf16Encode(out), nil
}
