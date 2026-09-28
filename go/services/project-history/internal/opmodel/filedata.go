package opmodel

// StringFileData — the vendor lib/file_data/string_file_data.js reduced to
// the surface the opmodel needs for apply/invert (the blob/snapshot layer is
// deliberately NOT ported here: no BlobStore, no HashFileData, no toEager/
// toHollow/store/getByteLength). This is a minimal 1:1 mirror of the String
// file data class.
//
// Vendor class:
//
//	class StringFileData extends FileData {
//	  constructor(content, rawComments = [], rawTrackedChanges = [])
//	  getContent(opts = {})          // default returns this.content
//	  getComments() / getTrackedChanges()
//	  edit(operation)                // operation.apply(this)
//	}
//
// Lengths are UTF-16 units.
type StringFileData struct {
	Content        string
	Comments       *CommentList
	TrackedChanges *TrackedChangeList
}

// StringFileDataFromRaw — vendor `StringFileData.fromRaw(raw)`:
//
//	new StringFileData(raw.content, raw.comments || [], raw.trackedChanges || [])
func StringFileDataFromRaw(raw map[string]any) (*StringFileData, error) {
	content, _ := raw["content"].(string)
	var rawComments, rawTracked []any
	if v, ok := raw["comments"].([]any); ok {
		rawComments = v
	}
	if v, ok := raw["trackedChanges"].([]any); ok {
		rawTracked = v
	}
	cl, err := CommentListFromRaw(rawComments)
	if err != nil {
		return nil, err
	}
	tcl, err := TrackedChangeListFromRaw(rawTracked)
	if err != nil {
		return nil, err
	}
	return &StringFileData{
		Content:        content,
		Comments:       cl,
		TrackedChanges: tcl,
	}, nil
}

// GetContent — vendor `getContent({})` (no filterTrackedDeletes). Mirrors the
// default path that TextOperation.apply/invert use.
func (s *StringFileData) GetContent() string { return s.Content }

// Edit — vendor `edit(operation)`: apply the operation in place (mutates the
// file). Vendor calls `operation.apply(this)`.
func (s *StringFileData) Edit(op EditOperation) error { return op.Apply(s) }

// GetComments — vendor `getComments()`.
func (s *StringFileData) GetComments() *CommentList { return s.Comments }

// GetTrackedChanges — vendor `getTrackedChanges()`.
func (s *StringFileData) GetTrackedChanges() *TrackedChangeList {
	return s.TrackedChanges
}
