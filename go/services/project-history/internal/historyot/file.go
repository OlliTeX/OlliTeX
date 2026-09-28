package historyot

import (
	"ollitex/go/services/project-history/internal/opmodel"
)

// EmptyFileHash — vendor `File.EMPTY_FILE_HASH` (lib/file.js): the blob hash
// of an empty file.
const EmptyFileHash = "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"

// File — vendor `File` (lib/file.js), scoped to the wire representation the
// B-phase layers need (AddFileOperation.toRaw, blob-hash lookup).
//
// Vendor File composes a FileData (hash-only / lazy / eager / hollow) with a
// metadata object and serializes to:
//
//	hash-only: {hash[, rangesHash]}[ + metadata]
//	eager:     {content[, comments][, trackedChanges]}[ + metadata]
//
// The Go port keeps the wire file-data raw map plus metadata, mirroring
// storeRawMetadata (metadata rides inside the file-data object on the wire,
// only when non-empty).
type File struct {
	raw      map[string]any
	metadata map[string]any
}

// FileFromRaw — vendor `File.fromRaw(raw)`:
// `new File(FileData.fromRaw(raw), raw.metadata)`.
//
// Vendor FileData.fromRaw dispatches on the raw keys ('hash' | 'byteLength'
// | 'stringLength' | 'content'); the Go port validates the same set (the
// hollow/lazy variants are C-phase Blob/Store territory).
func FileFromRaw(raw map[string]any) (*File, error) {
	has := func(k string) bool {
		_, ok := raw[k]
		return ok
	}
	if !(has("hash") || has("byteLength") || has("stringLength") || has("content")) {
		return nil, opmodel.NewUnprocessableError("File.fromRaw: bad raw object", nil)
	}
	f := &File{raw: make(map[string]any, len(raw))}
	for k, v := range raw {
		if k == "metadata" {
			if m, ok := v.(map[string]any); ok {
				f.metadata = m
			}
			continue
		}
		f.raw[k] = v
	}
	return f, nil
}

// FileFromHash — vendor `File.fromHash(hash, rangesHash, metadata)`.
// Vendor asserts Blob.HEX_HASH_RX (40 hex chars); mirrored.
func FileFromHash(hash string, rangesHash string, metadata map[string]any) (*File, error) {
	if !isHex40(hash) {
		return nil, opmodel.NewUnprocessableError("File.fromHash: bad hash "+hash, nil)
	}
	if rangesHash != "" && !isHex40(rangesHash) {
		return nil, opmodel.NewUnprocessableError("File.fromHash: bad ranges hash "+rangesHash, nil)
	}
	f := &File{raw: map[string]any{"hash": hash}, metadata: metadata}
	if rangesHash != "" {
		f.raw["rangesHash"] = rangesHash
	}
	return f, nil
}

// FileFromString — vendor `File.fromString(string, metadata)` (eager
// string file data).
func FileFromString(content string, metadata map[string]any) *File {
	sd := &opmodel.StringFileData{Content: content}
	raw := map[string]any{"content": sd.Content}
	if sd.Comments != nil && sd.Comments.Len() > 0 {
		raw["comments"] = sd.Comments.ToRaw()
	}
	if sd.TrackedChanges != nil && sd.TrackedChanges.Len() > 0 {
		raw["trackedChanges"] = sd.TrackedChanges.ToRaw()
	}
	return &File{raw: raw, metadata: metadata}
}

// NewStringFileData — for tests/ports building eager file content.
func NewStringFileData(content string) *opmodel.StringFileData {
	return &opmodel.StringFileData{
		Content:        content,
		Comments:       opmodel.NewCommentList(nil),
		TrackedChanges: opmodel.NewTrackedChangeList(nil),
	}
}

// ToRaw — vendor `File.toRaw()`: `data.toRaw()` + storeRawMetadata.
func (f *File) ToRaw() map[string]any {
	out := make(map[string]any, len(f.raw)+1)
	for k, v := range f.raw {
		out[k] = v
	}
	if len(f.metadata) > 0 {
		out["metadata"] = shallowCopyMap(f.metadata)
	}
	return out
}

// GetHash — vendor `getHash()`.
func (f *File) GetHash() string {
	h, _ := f.raw["hash"].(string)
	return h
}

// GetRangesHash — vendor `getRangesHash()` ("" when absent).
func (f *File) GetRangesHash() string {
	h, _ := f.raw["rangesHash"].(string)
	return h
}

// GetContent — eager content ("" when not eager).
func (f *File) GetContent() (string, bool) {
	c, ok := f.raw["content"].(string)
	return c, ok
}

// GetMetadata — vendor `getMetadata()`.
func (f *File) GetMetadata() map[string]any { return f.metadata }

// SetMetadata — vendor `setMetadata`: replace; empty when nil.
func (f *File) SetMetadata(metadata map[string]any) {
	if metadata == nil {
		f.metadata = nil
		return
	}
	f.metadata = shallowCopyMap(metadata)
}

// IsEager — vendor instanceof checks (string file data presence).
func (f *File) IsEager() bool {
	_, ok := f.raw["content"]
	return ok
}

func isHex40(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			continue
		}
		return false
	}
	return true
}

// shallowCopyMap — the Go mirror of vendor `_.cloneDeep` for the wire maps
// produced here (string keys, scalar/array values only), which keeps ToRaw
// output stable against mutation of the in-memory metadata.
func shallowCopyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
