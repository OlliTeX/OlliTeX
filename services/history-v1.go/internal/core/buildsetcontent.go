package core

import (
	"encoding/json"
)

// --- BuildSetContentOperations ports overleaf-editor-core/lib/build_set_content_operations.js: a
// setDoc-like upsert. Exactly one of Content (editable doc) and Binary
// (binary file) must be provided. An existing editable doc is edited in
// place with a minimal diff; any other case replaces or creates the file
// at the pathname. New content is stored in the blob store and referenced
// by hash, so the returned operations are cheap to serialize.
//
// The Node `blob` argument is a Blob value (hash + byteLength [+
// stringLength]); Go mirrors that with BinaryRef.

// SetContentArgs — Node args.
type SetContentArgs struct {
	File     *File // existing file at the pathname (nil when absent)
	Pathname string
	Content  *string // exactly one of Content or Binary set
	Binary   *BinaryRef
	// Metadata — the file's metadata is always replaced with this value
	// (nil => {} "no metadata key").
	Metadata json.RawMessage
	// Tracking — record the edit as tracked changes (ignored for binary).
	Tracking *TrackingProps
	// BlobStore — Node blobStore.
	BlobStore BlobStoreI
}

// BinaryRef — the Node Blob argument, reduced to the two halves the engine
// needs: the binary hash and its byteLength.
type BinaryRef struct {
	Hash       string
	ByteLength int
}

// SetContentResult — Node { operations, status: 'applied' | 'noop' }.
type SetContentResult struct {
	Operations []*Operation
	Status     string // "applied" | "noop"
}

// SetContentError — OError for the "exactly one of content and blob must be
// given" guard and the follow-on "no blob" error.
type SetContentError struct {
	Msg      string
	Pathname string
}

func (e *SetContentError) Error() string { return e.Msg + " (pathname=" + e.Pathname + ")" }

// BuildSetContentOperations ports buildSetContentOperations.
func BuildSetContentOperations(args SetContentArgs) (SetContentResult, error) {
	file := args.File
	pathname := args.Pathname
	tracking := args.Tracking
	bs := args.BlobStore
	metadata := args.Metadata

	var blobHash string

	if (args.Content == nil) == (args.Binary == nil) {
		return SetContentResult{}, &SetContentError{Msg: "exactly one of content and blob must be given", Pathname: pathname}
	}

	if args.Binary != nil {
		blobHash = args.Binary.Hash
	}

	if args.Content != nil {
		content := *args.Content
		if file != nil {
			// Eager loading resolves both the editability of files whose
			// data is a bare hash and the content to diff against. Go
			// File.Load returns a NEW *File for lazy/hash kinds.
			loaded, err := file.Load("eager", bs)
			if err != nil {
				return SetContentResult{}, &SetContentError{Msg: "eager load failed: " + err.Error(), Pathname: pathname}
			}
			if loaded.IsEditable() {
				return buildSetContentEditOps(loaded, pathname, content, metadata, tracking)
			}
		}
		h, err := bs.PutString(content)
		if err != nil {
			return SetContentResult{}, err
		}
		blobHash = h
	}
	if file != nil && file.GetHash() == blobHash {
		if MetadataEqual(file.Metadata, metadata) {
			return SetContentResult{Status: "noop"}, nil
		}
		return SetContentResult{
			Operations: []*Operation{SetFileMetadata(pathname, metadata)},
			Status:     "applied",
		}, nil
	}

	var rangesHash string
	if args.Content != nil && len(*args.Content) > 0 && tracking != nil {
		// Record the whole new doc as a tracked insert.
		h, err := putTrackedInsertRanges(*args.Content, tracking, bs)
		if err != nil {
			return SetContentResult{}, err
		}
		rangesHash = h
	}

	var newFile *File
	if args.Content != nil {
		// File.createLazyFromBlobs(blob, undefined, metadata) where blob is
		// from putString (stringLength = content.length) yields a lazy.
		newFile = &File{Kind: "lazy", Hash: blobHash, StringLength: utf16Length(*args.Content)}
		if rangesHash != "" {
			newFile.RangesHash = rangesHash
		}
	} else {
		// File.createLazyFromBlobs(blob, undefined, metadata) with a binary
		// blob (stringLength undefined) yields a binary file.
		newFile = &File{Kind: "binary", Hash: blobHash, ByteLength: args.Binary.ByteLength}
	}
	// File.createLazyFromBlobs(blob, rangesBlob, metadata) (Node createLazyFromBlobs passes
	// metadata through to the File constructor).
	newFile.Metadata = metadata

	ops := make([]*Operation, 0, 2)
	if file != nil {
		ops = append(ops, RemoveFile(pathname))
	}
	ops = append(ops, AddFile(pathname, newFile))
	return SetContentResult{Operations: ops, Status: "applied"}, nil
}

// buildSetContentEditOps ports buildEditOperations.
func buildSetContentEditOps(file *File, pathname, content string, metadata json.RawMessage, tracking *TrackingProps) (SetContentResult, error) {
	textOp, err := DiffAsTextOperation(file, content, tracking)
	if err != nil {
		return SetContentResult{}, err
	}
	ops := make([]*Operation, 0, 2)
	if !textOp.IsNoop() {
		ops = append(ops, EditFile(pathname, NewTextEditOp(textOp)))
	}
	if !MetadataEqual(file.Metadata, metadata) {
		ops = append(ops, SetFileMetadata(pathname, metadata))
	}
	if len(ops) == 0 {
		return SetContentResult{Status: "noop"}, nil
	}
	return SetContentResult{Operations: ops, Status: "applied"}, nil
}

// DiffAsTextOperation ports diffAsTextOperation: diff the file's
// (filtered-visible) content against after, returning a TextOperation that
// turns the file into it and preserves the file's tracked deletes. file
// must be an eagerly loaded file; tracking records the edit as a tracked
// change (inserted content tracked as an insert, removed content as a
// delete) instead of a plain edit.
func DiffAsTextOperation(file *File, after string, tracking *TrackingProps) (*TextOp, error) {
	if file == nil || file.Kind != "string" {
		return nil, &ApplyError{Msg: "DiffAsTextOperation: file is not an eagerly loaded string file"}
	}
	before, err := file.GetContent(true)
	if err != nil {
		return nil, err
	}
	return DiffsToTextOperation(file, DiffText(before, after), tracking)
}

// MetadataEqual — Node _.isEqual(file.getMetadata(), metadata) where
// File.getMetadata() defaults to {}. Go metadata: nil or "null" counts as
// {}, matching the wire normalization.
func MetadataEqual(a, b json.RawMessage) bool {
	norm := func(m json.RawMessage) string {
		s := string(m)
		if s == "" || s == "null" {
			return "{}"
		}
		return s
	}
	return norm(a) == norm(b)
}

// putTrackedInsertRanges stores the ranges blob
// {comments: [], trackedChanges: [{range: {pos: 0, length: L}, tracking}]}
// and returns its hash. Canonical key order mirrors Node JSON.stringify:
// comments then trackedChanges; range then tracking; type, userId, ts.
func putTrackedInsertRanges(content string, tracking *TrackingProps, bs BlobStoreI) (string, error) {
	body, err := json.Marshal(struct {
		Type   string `json:"type"`
		UserID string `json:"userId"`
		TS     string `json:"ts"`
	}{tracking.Type, tracking.UserID, tracking.TSISO})
	if err != nil {
		return "", err
	}
	out := append([]byte(`{"comments":[],"trackedChanges":[{"range":{"pos":0,"length":`), jsonLength(utf16Length(content))...)
	out = append(out, `},"tracking":`...)
	out = append(out, body...)
	out = append(out, `}]}`...)
	return bs.PutObject(out)
}

// jsonLength — tiny int-to-string for the ranges blob length field.
func jsonLength(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
