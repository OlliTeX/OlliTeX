package otc

import (
	"context"
)

// --- HashFileData ----------------------------------------------------------

// HashFileData mirrors file_data/hash_file_data.js: only hash (+ rangesHash) known.
type HashFileData struct {
	fileDataDefaults
	Hash       string
	RangesHash *string // nil = no ranges hash
}

func newHashFileData(hash string, rangesHash *string) (*HashFileData, error) {
	if !isHexHash40(hash) {
		return nil, gop("HashFileData: bad hash")
	}
	if rangesHash != nil && !isHexHash40(*rangesHash) {
		return nil, gop("HashFileData: bad ranges hash")
	}
	return &HashFileData{Hash: hash, RangesHash: rangesHash}, nil
}

func hashFileDataFromRaw(raw map[string]any) (FileData, error) {
	hash, _ := raw["hash"].(string)
	out := &HashFileData{Hash: hash}
	if rh, ok := raw["rangesHash"].(string); ok && rh != "" {
		out.RangesHash = &rh
	}
	if !isHexHash40(hash) {
		return nil, gop("HashFileData: bad hash")
	}
	if out.RangesHash != nil && !isHexHash40(*out.RangesHash) {
		return nil, gop("HashFileData: bad ranges hash")
	}
	return out, nil
}

func (h *HashFileData) ToRaw() map[string]any {
	raw := map[string]any{"hash": h.Hash}
	if h.RangesHash != nil {
		raw["rangesHash"] = *h.RangesHash
	}
	return raw
}

func (h *HashFileData) ToStats() map[string]any {
	return map[string]any{"hashes": 1 + hashCount(h.RangesHash)}
}

func (h *HashFileData) GetHash() *string       { return &h.Hash }
func (h *HashFileData) GetRangesHash() *string { return h.RangesHash }

func (h *HashFileData) ToEager(ctx context.Context, bs BlobStore) (FileData, error) {
	lazy, err := h.ToLazy(ctx, bs)
	if err != nil {
		return nil, err
	}
	return lazy.ToEager(ctx, bs)
}

func (h *HashFileData) ToLazy(ctx context.Context, bs BlobStore) (FileData, error) {
	blob, err := bs.GetBlob(ctx, h.Hash)
	if err != nil {
		return nil, err
	}
	var rangesBlob *Blob
	if h.RangesHash != nil {
		rangesBlob, err = bs.GetBlob(ctx, *h.RangesHash)
		if err != nil {
			return nil, err
		}
		if rangesBlob == nil {
			return nil, gop("Failed to look up rangesHash in blobStore")
		}
	}
	if blob == nil {
		return nil, gop("blob not found: " + h.Hash)
	}
	return CreateLazyFromBlobs(blob, rangesBlob)
}

func (h *HashFileData) ToHollow(ctx context.Context, bs BlobStore) (FileData, error) {
	blob, err := bs.GetBlob(ctx, h.Hash)
	if err != nil {
		return nil, err
	}
	if blob == nil {
		return nil, gop("Failed to look up hash in blobStore")
	}
	return CreateHollow(blob.GetByteLength(), blob.GetStringLength()), nil
}

func (h *HashFileData) Store(context.Context, BlobStore) (map[string]any, error) {
	return h.ToRaw(), nil
}

func hashCount(s *string) int {
	if s == nil {
		return 0
	}
	return 1
}

// --- BinaryFileData --------------------------------------------------------

// BinaryFileData mirrors file_data/binary_file_data.js.
type BinaryFileData struct {
	fileDataDefaults
	Hash       string
	ByteLength int64
}

func newBinaryFileData(hash string, byteLength int64) (*BinaryFileData, error) {
	if !isHexHash40(hash) {
		return nil, gop("BinaryFileData: bad hash")
	}
	if byteLength < 0 {
		return nil, gop("BinaryFileData: low byteLength")
	}
	return &BinaryFileData{Hash: hash, ByteLength: byteLength}, nil
}

func binaryFileDataFromRaw(raw map[string]any) (FileData, error) {
	hash, _ := raw["hash"].(string)
	byteLength, ok := raw["byteLength"].(int64)
	if !ok {
		if f, okF := raw["byteLength"].(float64); okF {
			byteLength = int64(f)
		} else {
			return nil, gop("BinaryFileData: bad byteLength")
		}
	}
	return newBinaryFileData(hash, byteLength)
}

func (b *BinaryFileData) ToRaw() map[string]any {
	return map[string]any{"hash": b.Hash, "byteLength": b.ByteLength}
}
func (b *BinaryFileData) ToStats() map[string]any {
	return map[string]any{"hashes": 1, "byteLength": b.ByteLength}
}
func (b *BinaryFileData) GetHash() *string      { return &b.Hash }
func (b *BinaryFileData) GetByteLength() *int64 { return &b.ByteLength }

func (b *BinaryFileData) IsEditable() *bool {
	f := false
	return &f
}
func (b *BinaryFileData) ToEager(context.Context, BlobStore) (FileData, error) {
	return b, nil
}
func (b *BinaryFileData) ToLazy(context.Context, BlobStore) (FileData, error) {
	return b, nil
}
func (b *BinaryFileData) ToHollow(context.Context, BlobStore) (FileData, error) {
	return CreateHollow(b.ByteLength, nil), nil
}
func (b *BinaryFileData) Store(context.Context, BlobStore) (map[string]any, error) {
	return map[string]any{"hash": b.Hash}, nil
}

// --- HollowStringFileData --------------------------------------------------

// HollowStringFileData mirrors file_data/hollow_string_file_data.js.
type HollowStringFileData struct {
	fileDataDefaults
	StringLength int64
}

func newHollowStringFileData(stringLength int64) (*HollowStringFileData, error) {
	if stringLength < 0 {
		return nil, gop("HollowStringFileData: low stringLength")
	}
	return &HollowStringFileData{StringLength: stringLength}, nil
}

func hollowStringFileDataFromRaw(raw map[string]any) (FileData, error) {
	n, ok := raw["stringLength"].(int64)
	if !ok {
		if f, okF := raw["stringLength"].(float64); okF {
			n = int64(f)
		} else {
			return nil, gop("HollowStringFileData: bad stringLength")
		}
	}
	return newHollowStringFileData(n)
}

func (h *HollowStringFileData) ToRaw() map[string]any {
	return map[string]any{"stringLength": h.StringLength}
}
func (h *HollowStringFileData) ToStats() map[string]any {
	return map[string]any{"stringLength": h.StringLength}
}
func (h *HollowStringFileData) GetStringLength() *int64 { return &h.StringLength }
func (h *HollowStringFileData) IsEditable() *bool {
	t := true
	return &t
}
func (h *HollowStringFileData) ToHollow(context.Context, BlobStore) (FileData, error) {
	return h, nil
}
func (h *HollowStringFileData) Edit(op EditOperation) error {
	n, err := op.ApplyToLength(int(h.StringLength))
	if err != nil {
		return err
	}
	h.StringLength = int64(n)
	return nil
}

// --- HollowBinaryFileData --------------------------------------------------

// HollowBinaryFileData mirrors file_data/hollow_binary_file_data.js.
type HollowBinaryFileData struct {
	fileDataDefaults
	ByteLength int64
}

func newHollowBinaryFileData(byteLength int64) (*HollowBinaryFileData, error) {
	if byteLength < 0 {
		return nil, gop("HollowBinaryFileData: low byteLength")
	}
	return &HollowBinaryFileData{ByteLength: byteLength}, nil
}

func hollowBinaryFileDataFromRaw(raw map[string]any) (FileData, error) {
	n, ok := raw["byteLength"].(int64)
	if !ok {
		if f, okF := raw["byteLength"].(float64); okF {
			n = int64(f)
		} else {
			return nil, gop("HollowBinaryFileData: bad byteLength")
		}
	}
	return newHollowBinaryFileData(n)
}

func (h *HollowBinaryFileData) ToRaw() map[string]any {
	return map[string]any{"byteLength": h.ByteLength}
}
func (h *HollowBinaryFileData) ToStats() map[string]any {
	return map[string]any{"byteLength": h.ByteLength}
}
func (h *HollowBinaryFileData) GetByteLength() *int64 { return &h.ByteLength }
func (h *HollowBinaryFileData) IsEditable() *bool {
	f := false
	return &f
}
func (h *HollowBinaryFileData) ToHollow(context.Context, BlobStore) (FileData, error) {
	return h, nil
}

// isHexHash40 mirrors the Blob.HEX_HASH_RX match used by the hash variants.
func isHexHash40(hash string) bool {
	return hexHashRx.MatchString(hash)
}
