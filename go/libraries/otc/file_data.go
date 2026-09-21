package otc

import (
	"context"
	"encoding/json"
)

// FileData is the Node `FileData` base (file_data/index.js) and the interface
// shared by all of its variants. It is meant to be used only through File.
//
// Node accessors that can be unknown return nullable values (a nil *string /
// *int64 / *bool = Node undefined/null). Methods that Node `throws` are
// returned as `error` (the "not implemented" defaults). `load` (which in Node
// dispatches to toEager/toLazy/toHollow) is the free function loadFileData,
// so it always reaches the CONCRETE variant's overridden methods.
type FileData interface {
	ToRaw() map[string]any
	ToStats() map[string]any
	GetHash() *string
	GetRangesHash() *string
	GetContent(filterTrackedDeletes bool) *string
	IsEditable() *bool
	GetByteLength() *int64
	GetStringLength() *int64
	Edit(op EditOperation) error
	ToEager(ctx context.Context, bs BlobStore) (FileData, error)
	ToLazy(ctx context.Context, bs BlobStore) (FileData, error)
	ToHollow(ctx context.Context, bs BlobStore) (FileData, error)
	Store(ctx context.Context, bs BlobStore) (map[string]any, error)
	GetComments() *CommentList
	GetTrackedChanges() *TrackedChangeList
}

// fileDataDefaults supplies the Node base-class defaults (the abstract
// "not implemented" throws and the null accessors). Variants embed this and
// override what they need.
type fileDataDefaults struct{}

func (fileDataDefaults) ToRaw() map[string]any { return nil }
func (fileDataDefaults) ToStats() map[string]any {
	return nil
}
func (fileDataDefaults) GetHash() *string          { return nil }
func (fileDataDefaults) GetRangesHash() *string    { return nil }
func (fileDataDefaults) GetContent(bool) *string   { return nil }
func (fileDataDefaults) IsEditable() *bool         { return nil }
func (fileDataDefaults) GetByteLength() *int64     { return nil }
func (fileDataDefaults) GetStringLength() *int64   { return nil }
func (fileDataDefaults) GetComments() *CommentList { return nil }
func (fileDataDefaults) GetTrackedChanges() *TrackedChangeList {
	return nil
}

func (fileDataDefaults) Edit(EditOperation) error {
	return gop("FileData: edit not implemented")
}
func (fileDataDefaults) ToEager(context.Context, BlobStore) (FileData, error) {
	return nil, gop("FileData: toEager not implemented")
}
func (fileDataDefaults) ToLazy(context.Context, BlobStore) (FileData, error) {
	return nil, gop("FileData: toLazy not implemented")
}
func (fileDataDefaults) ToHollow(context.Context, BlobStore) (FileData, error) {
	return nil, gop("FileData: toHollow not implemented")
}
func (fileDataDefaults) Store(context.Context, BlobStore) (map[string]any, error) {
	return nil, gop("FileData: store not implemented")
}

// loadFileData mirrors the Node base `load(kind, blobStore)` dispatch. It is a
// free function taking the FileData interface so that fd.ToEager/ToLazy/
// ToHollow reach the CONCRETE variant's overrides (Go embedding cannot re-dispatch).
func loadFileData(fd FileData, ctx context.Context, kind string, bs BlobStore) (FileData, error) {
	switch kind {
	case "eager":
		return fd.ToEager(ctx, bs)
	case "lazy":
		return fd.ToLazy(ctx, bs)
	case "hollow":
		return fd.ToHollow(ctx, bs)
	}
	return nil, gop("bad file data load kind: " + kind)
}

// --- registry (FileData.fromRaw / createHollow / createLazyFromBlobs) ------

func rawHas(raw map[string]any, key string) bool {
	_, ok := raw[key]
	return ok
}

// FromRawFileData mirrors FileData.fromRaw — dispatch by the first present
// key in Node's order (hash>byteLength>stringLength / byteLength / stringLength
// / content).
func FromRawFileData(raw map[string]any) (FileData, error) {
	if rawHas(raw, "hash") {
		if rawHas(raw, "byteLength") {
			return binaryFileDataFromRaw(raw)
		}
		if rawHas(raw, "stringLength") {
			return lazyStringFileDataFromRaw(raw)
		}
		return hashFileDataFromRaw(raw)
	}
	if rawHas(raw, "byteLength") {
		return hollowBinaryFileDataFromRaw(raw)
	}
	if rawHas(raw, "stringLength") {
		return hollowStringFileDataFromRaw(raw)
	}
	if rawHas(raw, "content") {
		return FromRawStringFileData(raw)
	}
	return nil, gop("FileData: bad raw object " + rawJSONStr(raw))
}

// CreateHollow mirrors FileData.createHollow(byteLength, stringLength).
func CreateHollow(byteLength int64, stringLength *int64) FileData {
	if stringLength == nil {
		return &HollowBinaryFileData{ByteLength: byteLength}
	}
	return &HollowStringFileData{StringLength: *stringLength}
}

// CreateLazyFromBlobs mirrors FileData.createLazyFromBlobs(blob, rangesBlob).
func CreateLazyFromBlobs(blob *Blob, rangesBlob *Blob) (FileData, error) {
	if blob == nil {
		return nil, gop("FileData: bad blob")
	}
	if sl := blob.GetStringLength(); sl == nil {
		return &BinaryFileData{Hash: blob.Hash, ByteLength: blob.ByteLength}, nil
	} else {
		var ranges *string
		if rangesBlob != nil {
			ranges = &rangesBlob.Hash
		}
		return NewLazyStringFileData(blob.Hash, ranges, *sl, nil)
	}
}

func rawJSONStr(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// deepClone mirrors lodash _.cloneDeep on the (JSON-like) metadata value.
func deepClone(v map[string]any) map[string]any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		out := make(map[string]any, len(v))
		for k, x := range v {
			out[k] = x
		}
		return out
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}
