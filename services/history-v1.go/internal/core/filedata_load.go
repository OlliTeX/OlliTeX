package core

import (
	"encoding/json"
	"fmt"
)

// --- File.Load (ports File.load / FileData.load dispatch) ---

// Load ports File#load: converts this file's data to `kind` using the blob
// store.
//
//	kind "eager" — full content + comments + trackedChanges (+buffered ops)
//	kind "lazy" — {hash, rangesHash?, stringLength}
//	kind "hollow" — {byteLength} | {stringLength}
//
// Node: data.load(kind, blobStore) with toEager/toLazy/toHollow per kind.
func (f *File) Load(kind string, bs BlobStoreI) (*File, error) {
	switch kind {
	case "eager":
		return f.toEager(bs)
	case "lazy":
		return f.toLazy(bs)
	case "hollow":
		return f.toHollow(bs)
	default:
		return nil, &BadRawError{Msg: "bad file data load kind: " + kind}
	}
}

// toEager ports FileData.toEager per kind:
//
//	string — this
//	lazy   — getString(hash) + getObject(rangesHash) → string data;
//	         apply buffered ops (tagged error keeps
//	         {operationIndex,totalOperations,currentContentLength})
//	hash   — fetch string blob (+ranges blob) → lazy → toEager
//	binary — this
//	hollow* — error (toEager not implemented in Node)
func (f *File) toEager(bs BlobStoreI) (*File, error) {
	switch f.Kind {
	case "string":
		return f, nil
	case "lazy":
		return f.lazyToEager(bs)
	case "hash":
		// Node HashFileData.toEager: toLazy(blobStore) then lazy.toEager.
		lazy, err := f.toLazy(bs)
		if err != nil {
			return nil, err
		}
		return lazy.toEager(bs)
	case "binary":
		return f, nil
	case "hollowStr", "hollowBin":
		return nil, &NotEditableError{}
	default:
		return nil, &BadRawError{Msg: "toEager not implemented for kind " + f.Kind}
	}
}

// lazyToEager ports LazyStringFileData.toEager verbatim contract:
// fetch string + optional ranges object, build eager StringFileData,
// applyOperations with the tagged error envelope.
func (f *File) lazyToEager(bs BlobStoreI) (*File, error) {
	content, err := bs.GetHashBlob(f.Hash)
	if err != nil {
		return nil, err
	}
	out := &File{
		Kind:     "string",
		Content:  content,
		Metadata: f.Metadata,
	}
	if f.RangesHash != "" {
		obj, err := bs.GetRangesBlob(f.RangesHash)
		if err != nil {
			return nil, err
		}
		var r struct {
			Comments       []json.RawMessage `json:"comments"`
			TrackedChanges []json.RawMessage `json:"trackedChanges"`
		}
		if err := json.Unmarshal(obj, &r); err != nil {
			return nil, &BadRawError{Msg: "bad ranges blob: " + err.Error()}
		}
		out.Comments = CommentListFromRaw(r.Comments)
		out.TrackedChanges = TrackedChangeListFromRaw(r.TrackedChanges)
	}

	total := len(f.LazyOps)
	for i, opRaw := range f.LazyOps {
		op, err := EditOpFromRaw(opRaw)
		if err != nil {
			return nil, &UnprocessableError{Msg: fmt.Sprintf(
				"operation failed during applyOperations %d/%d contentLength %d: %s",
				i, total, out.GetStringLength(), err.Error())}
		}
		if err := op.ApplyFile(out); err != nil {
			return nil, &UnprocessableError{Msg: fmt.Sprintf(
				"operation failed during applyOperations %d/%d contentLength %d: %s",
				i, total, out.GetStringLength(), err.Error())}
		}
	}
	return out, nil
}

// toLazy ports FileData.toLazy:
//
//	string — createLazyFromBlobs: string blob hash + stringLength
//	lazy   — this
//	hash   — fetch blobs → lazy (stringLength from the string blob)
//	binary — this
//	hollow* — this
func (f *File) toLazy(bs BlobStoreI) (*File, error) {
	switch f.Kind {
	case "lazy", "binary", "hollowStr", "hollowBin":
		return f, nil
	case "string":
		s, err := bs.PutString(f.Content) // content hash == hash of content
		if err != nil {
			return nil, err
		}
		l := &File{
			Kind:         "lazy",
			Hash:         s,
			StringLength: utf16Length(f.Content),
			Metadata:     f.Metadata,
		}
		if len(f.Comments) > 0 || len(f.TrackedChanges) > 0 {
			r, err := bs.PutObject(encodeRanges(f))
			if err != nil {
				return nil, err
			}
			l.RangesHash = r
		}
		return l, nil
	case "hash":
		// Node HashFileData.toLazy: getBlob(hash) → createLazyFromBlobs —
		// binary when the blob has no stringLength (stringLength == null),
		// lazy otherwise.
		byteLength, stringLength, found := bs.StringBlob(f.Hash)
		if !found {
			_, err := bs.GetHashBlob(f.Hash)
			return nil, err
		}
		if stringLength < 0 {
			return &File{Kind: "binary", Hash: f.Hash, ByteLength: int(byteLength), Metadata: f.Metadata}, nil
		}
		// LazyStringFileData: {hash, rangesHash?, stringLength}.
		return &File{
			Kind:         "lazy",
			Hash:         f.Hash,
			RangesHash:   f.RangesHash,
			StringLength: int(stringLength),
			Metadata:     f.Metadata,
			LazyOps:      f.LazyOps,
		}, nil
	default:
		return nil, &BadRawError{Msg: "toLazy not implemented for kind " + f.Kind}
	}
}

func encodeRanges(f *File) []byte {
	if len(f.Comments) == 0 && len(f.TrackedChanges) == 0 {
		return []byte(`{"comments":[],"trackedChanges":[]}`)
	}
	obj, _ := json.Marshal(struct {
		Comments       json.RawMessage `json:"comments"`
		TrackedChanges json.RawMessage `json:"trackedChanges"`
	}{commentsToRaw(f.Comments), f.TrackedChanges.ToRaw()})
	return obj
}

// toHollow ports FileData.toHollow:
//
//	string — hollowStr(stringLength)
//	lazy   — hollowStr(stringLength)
//	hash   — fetch blob → createHollow(byteLength, stringLength)
//	binary — hollowBin(byteLength)
//	hollow* — this
func (f *File) toHollow(bs BlobStoreI) (*File, error) {
	switch f.Kind {
	case "string":
		return &File{Kind: "hollowStr", StringLength: utf16Length(f.Content), Metadata: f.Metadata}, nil
	case "lazy":
		return &File{Kind: "hollowStr", StringLength: f.StringLength, Metadata: f.Metadata}, nil
	case "hash":
		// Node HashFileData.toHollow: createHollow(blob.byteLength,
		// blob.stringLength) — binary when the blob has no stringLength.
		byteLength, stringLength, found := bs.StringBlob(f.Hash)
		if !found {
			_, err := bs.GetHashBlob(f.Hash)
			return nil, err
		}
		if stringLength < 0 {
			return &File{Kind: "hollowBin", ByteLength: int(byteLength), Metadata: f.Metadata}, nil
		}
		return &File{Kind: "hollowStr", StringLength: int(stringLength), Metadata: f.Metadata}, nil
	case "binary":
		return &File{Kind: "hollowBin", ByteLength: f.ByteLength, Metadata: f.Metadata}, nil
	case "hollowStr", "hollowBin":
		return f, nil
	default:
		return nil, &BadRawError{Msg: "toHollow not implemented for kind " + f.Kind}
	}
}
