package otc

import (
	"context"
	"encoding/json"
)

// FileEmptyHash is File.EMPTY_FILE_HASH (the git empty-blob hash, spelled out
// rather than read across a module boundary to avoid a load-order undefined).
const FileEmptyHash = EmptyHash

// File mirrors lib/file.js: a file in a Snapshot, carrying FileData + metadata.
type File struct {
	data     FileData
	Metadata map[string]any
}

// NewFile mirrors the File constructor (validates data is a FileData; the Go
// type system enforces this, so only the metadata defaulting is modelled).
func NewFile(data FileData, metadata map[string]any) *File {
	f := &File{data: data, Metadata: map[string]any{}}
	f.SetMetadata(metadata)
	return f
}

// FileFromRaw mirrors File.fromRaw (nil raw -> nil; else build data + metadata).
func FileFromRaw(raw map[string]any) (*File, error) {
	if raw == nil {
		return nil, nil
	}
	data, err := FromRawFileData(raw)
	if err != nil {
		return nil, err
	}
	var meta map[string]any
	if m, ok := raw["metadata"].(map[string]any); ok {
		meta = m
	}
	return NewFile(data, meta), nil
}

// FileFromRawOrPanic is the convenience used by Clone (Node: clone uses
// File.fromRaw which only throws on a bad raw).
func FileFromRawOrPanic(raw map[string]any) *File {
	f, err := FileFromRaw(raw)
	if err != nil {
		panic(err)
	}
	return f
}

// FileFromHash mirrors File.fromHash(hash, rangesHash, metadata).
func FileFromHash(hash string, rangesHash *string, metadata map[string]any) (*File, error) {
	data, err := newHashFileData(hash, rangesHash)
	if err != nil {
		return nil, err
	}
	return NewFile(data, metadata), nil
}

// FileFromString mirrors File.fromString(string, metadata).
func FileFromString(content string, metadata map[string]any) (*File, error) {
	data, err := NewStringFileData(content, nil, nil)
	if err != nil {
		return nil, err
	}
	return NewFile(data, metadata), nil
}

// FileCreateHollow mirrors File.createHollow(byteLength, stringLength, metadata).
func FileCreateHollow(byteLength int64, stringLength *int64, metadata map[string]any) *File {
	return NewFile(CreateHollow(byteLength, stringLength), metadata)
}

// FileCreateLazyFromBlobs mirrors File.createLazyFromBlobs(blob, rangesBlob, metadata).
func FileCreateLazyFromBlobs(blob *Blob, rangesBlob *Blob, metadata map[string]any) (*File, error) {
	data, err := CreateLazyFromBlobs(blob, rangesBlob)
	if err != nil {
		return nil, err
	}
	return NewFile(data, metadata), nil
}

// ToRaw mirrors File.toRaw (raw data + a deep-clone of non-empty metadata).
func (f *File) ToRaw() map[string]any {
	raw := f.data.ToRaw()
	storeRawMetadata(f.Metadata, raw)
	return raw
}

// ToStats mirrors File.toStats (data stats + metadata stats when non-empty).
func (f *File) ToStats() map[string]any {
	stats := f.data.ToStats()
	if len(f.Metadata) > 0 {
		stats["nMetadata"] = 1
		b, _ := json.Marshal(f.Metadata)
		stats["metadataSize"] = len(b)
	}
	return stats
}

func (f *File) GetHash() *string                      { return f.data.GetHash() }
func (f *File) GetRangesHash() *string                { return f.data.GetRangesHash() }
func (f *File) GetContent(b bool) *string             { return f.data.GetContent(b) }
func (f *File) IsEditable() *bool                     { return f.data.IsEditable() }
func (f *File) GetByteLength() *int64                 { return f.data.GetByteLength() }
func (f *File) GetStringLength() *int64               { return f.data.GetStringLength() }
func (f *File) GetComments() *CommentList             { return f.data.GetComments() }
func (f *File) GetTrackedChanges() *TrackedChangeList { return f.data.GetTrackedChanges() }

// GetMetadata returns the metadata object (Node: `getMetadata`).
func (f *File) GetMetadata() map[string]any { return f.Metadata }

// SetMetadata sets the metadata, defaulting nil to {} (Node: `setMetadata`,
// the constructor uses `metadata || {}`).
func (f *File) SetMetadata(metadata map[string]any) {
	if metadata == nil {
		metadata = map[string]any{}
	}
	f.Metadata = metadata
}

// Edit edits the file if it is editable (Node: `edit`), else NotEditableError.
func (f *File) Edit(op EditOperation) error {
	if ed := f.data.IsEditable(); ed == nil || !*ed {
		return &NotEditableError{}
	}
	return f.data.Edit(op)
}

// Load converts the file's data to the given kind (Node: `load`).
func (f *File) Load(ctx context.Context, kind string, bs BlobStore) (*File, error) {
	data, err := loadFileData(f.data, ctx, kind, bs)
	if err != nil {
		return nil, err
	}
	f.data = data
	return f, nil
}

// LoadEager loads an editable file's content and answers with the data holding
// it (Node: `loadEager`), or NotEditableError.
func (f *File) LoadEager(ctx context.Context, bs BlobStore) (*StringFileData, error) {
	if _, err := f.Load(ctx, "eager", bs); err != nil {
		return nil, err
	}
	sfd, ok := f.data.(*StringFileData)
	if !ok {
		return nil, &NotEditableError{}
	}
	return sfd, nil
}

// Store writes the file's content to the blob store and returns a raw file
// (Node: `store`).
func (f *File) Store(ctx context.Context, bs BlobStore) (map[string]any, error) {
	raw, err := f.data.Store(ctx, bs)
	if err != nil {
		return nil, err
	}
	storeRawMetadata(f.Metadata, raw)
	return raw, nil
}

// Clone returns a new File of the same data (Node: `clone`).
func (f *File) Clone() *File {
	return FileFromRawOrPanic(f.ToRaw())
}

// storeRawMetadata mirrors the file.js helper: attaches a deep-clone of the
// (non-empty) metadata to a raw file.
func storeRawMetadata(metadata map[string]any, raw map[string]any) {
	if len(metadata) > 0 {
		raw["metadata"] = deepClone(metadata)
	}
}
