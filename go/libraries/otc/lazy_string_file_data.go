package otc

import (
	"context"
	"encoding/json"
)

// LazyStringFileData mirrors file_data/lazy_string_file_data.js: a cached set of
// edit operations to apply over a known blob, plus its string length.
type LazyStringFileData struct {
	fileDataDefaults
	Hash         string
	RangesHash   *string
	StringLength int64
	Operations   []EditOperation
}

// NewLazyStringFileData builds one (Node constructor; validates the hashes and
// non-negative stringLength).
func NewLazyStringFileData(hash string, rangesHash *string, stringLength int64, operations []EditOperation) (*LazyStringFileData, error) {
	if !isHexHash40(hash) {
		return nil, gop("LazyStringFileData: bad hash")
	}
	if rangesHash != nil && !isHexHash40(*rangesHash) {
		return nil, gop("LazyStringFileData: bad ranges hash")
	}
	if stringLength < 0 {
		return nil, gop("LazyStringFileData: low stringLength")
	}
	if operations == nil {
		operations = []EditOperation{}
	}
	return &LazyStringFileData{Hash: hash, RangesHash: rangesHash, StringLength: stringLength, Operations: operations}, nil
}

func lazyStringFileDataFromRaw(raw map[string]any) (FileData, error) {
	hash, _ := raw["hash"].(string)
	var ranges *string
	if rh, ok := raw["rangesHash"].(string); ok && rh != "" {
		ranges = &rh
	}
	sl, ok := raw["stringLength"].(int64)
	if !ok {
		if f, okF := raw["stringLength"].(float64); okF {
			sl = int64(f)
		} else {
			return nil, gop("LazyStringFileData: bad stringLength")
		}
	}
	var ops []EditOperation
	if v := raw["operations"]; v != nil {
		switch arr := v.(type) {
		case []any:
			for _, o := range arr {
				om, okM := o.(map[string]any)
				if !okM {
					return nil, gop("LazyStringFileData: bad operation in raw")
				}
				op, err := FromJSONEditOperation(om)
				if err != nil {
					return nil, err
				}
				ops = append(ops, op)
			}
		case []map[string]any:
			for _, m := range arr {
				op, err := FromJSONEditOperation(m)
				if err != nil {
					return nil, err
				}
				ops = append(ops, op)
			}
		default:
			return nil, gop("LazyStringFileData: bad operations in raw")
		}
	}
	return NewLazyStringFileData(hash, ranges, sl, ops)
}

func (l *LazyStringFileData) ToRaw() map[string]any {
	raw := map[string]any{"hash": l.Hash, "stringLength": l.StringLength}
	if l.RangesHash != nil {
		raw["rangesHash"] = *l.RangesHash
	}
	if len(l.Operations) > 0 {
		opsRaw := make([]map[string]any, 0, len(l.Operations))
		for _, op := range l.Operations {
			opsRaw = append(opsRaw, op.ToJSON())
		}
		raw["operations"] = opsRaw
	}
	return raw
}

func (l *LazyStringFileData) ToStats() map[string]any {
	size := 0
	for _, op := range l.Operations {
		b, _ := json.Marshal(op.ToJSON())
		size += len(b)
	}
	return map[string]any{
		"hashes":         1 + hashCount(l.RangesHash),
		"stringLength":   l.StringLength,
		"nOperations":    len(l.Operations),
		"operationsSize": size,
	}
}

func (l *LazyStringFileData) GetHash() *string {
	if len(l.Operations) > 0 {
		return nil
	}
	return &l.Hash
}

func (l *LazyStringFileData) GetRangesHash() *string {
	if len(l.Operations) > 0 {
		return nil
	}
	return l.RangesHash
}

func (l *LazyStringFileData) IsEditable() *bool {
	t := true
	return &t
}

func (l *LazyStringFileData) GetByteLength() *int64   { return &l.StringLength } // approx: UTF-16 length
func (l *LazyStringFileData) GetStringLength() *int64 { return &l.StringLength }
func (l *LazyStringFileData) GetOperations() []EditOperation {
	return l.Operations
}

func (l *LazyStringFileData) ToLazy(context.Context, BlobStore) (FileData, error) {
	return l, nil
}

func (l *LazyStringFileData) ToHollow(context.Context, BlobStore) (FileData, error) {
	sl := l.StringLength
	return CreateHollow(0, &sl), nil
}

// ToEager loads the content (and ranges, if any) and applies the cached
// operations, tagging failures with oerror metadata (Node oracle-pinned).
func (l *LazyStringFileData) ToEager(ctx context.Context, bs BlobStore) (FileData, error) {
	content, err := bs.GetString(ctx, l.Hash)
	if err != nil {
		return nil, err
	}
	var ranges map[string]any
	if l.RangesHash != nil {
		ranges, err = bs.GetObject(ctx, *l.RangesHash)
		if err != nil {
			return nil, err
		}
	}
	var rawComments, rawTracked []map[string]any
	if ranges != nil {
		rawComments = asRawObjects(ranges["comments"])
		rawTracked = asRawObjects(ranges["trackedChanges"])
	}
	file, err := NewStringFileData(content, rawComments, rawTracked)
	if err != nil {
		return nil, err
	}
	if err := applyOperations(l.Operations, file); err != nil {
		info := map[string]any{
			"blobHash":             l.Hash,
			"blobContentLength":    utf16Units(content),
			"metadataStringLength": int(l.StringLength),
			"totalOperations":      len(l.Operations),
		}
		first := firstOpBaseLengthOf(l.Operations)
		if first != nil {
			info["firstOpBaseLength"] = *first
			info["contentMatchesFirstOp"] = (utf16Units(content) == *first)
		}
		info["contentMatchesMetadata"] = (utf16Units(content) == int(l.StringLength))
		return nil, tagErr(err, info)
	}
	return file, nil
}

// Edit applies the operation to the length and queues it, tagging a length
// mismatch with oerror metadata (Node oracle-pinned).
func (l *LazyStringFileData) Edit(op EditOperation) error {
	n, err := op.ApplyToLength(int(l.StringLength))
	if err != nil {
		info := map[string]any{
			"blobHash":                l.Hash,
			"metadataStringLength":    int(l.StringLength),
			"totalExistingOperations": len(l.Operations),
		}
		if base := opBaseLength(op); base != nil {
			info["operationBaseLength"] = *base
		}
		return tagErr(err, info)
	}
	l.StringLength = int64(n)
	l.Operations = append(l.Operations, op)
	return nil
}

func (l *LazyStringFileData) Store(ctx context.Context, bs BlobStore) (map[string]any, error) {
	if len(l.Operations) == 0 {
		raw := map[string]any{"hash": l.Hash}
		if l.RangesHash != nil {
			raw["rangesHash"] = *l.RangesHash
		}
		return raw, nil
	}
	eager, err := l.ToEager(ctx, bs)
	if err != nil {
		return nil, err
	}
	raw, err := eager.Store(ctx, bs)
	if err != nil {
		return nil, err
	}
	if h, ok := raw["hash"].(string); ok {
		l.Hash = h
	}
	if rh, ok := raw["rangesHash"].(string); ok && rh != "" {
		l.RangesHash = &rh
	} else {
		l.RangesHash = nil
	}
	l.Operations = nil
	return raw, nil
}

// applyOperations mirrors the lazy module's helper, tagging the failing index.
func applyOperations(operations []EditOperation, file *StringFileData) error {
	for i, op := range operations {
		if err := op.Apply(file); err != nil {
			return tagErr(err, map[string]any{
				"operationIndex":       i,
				"totalOperations":      len(operations),
				"currentContentLength": utf16Units(file.Content),
			})
		}
	}
	return nil
}

func opBaseLength(op EditOperation) *int {
	if to, ok := asTextOp(op); ok {
		return &to.BaseLength
	}
	return nil
}

func firstOpBaseLengthOf(ops []EditOperation) *int {
	if len(ops) == 0 {
		return nil
	}
	return opBaseLength(ops[0])
}
