package core

import (
	"encoding/json"
)

// File — engine file model. Variants (Node FileData.fromRaw precedence,
// golden-locked wire):
//
//	"string"   {content, comments?, trackedChanges?}
//	"lazy"     {stringLength, hash?, rangesHash?, operations?:[{textOperation, contentHash?}]}
//	"hash"     {hash[, rangesHash?]}  (binary; rangesHash => binary+ranges)
//	"hollow"   {byteLength} | {stringLength}
//
// File wire = data raw + "metadata" key (when non-empty; Node: !_.isEmpty).
//
// Metadata key last (Node File.toRaw: {...data.toRaw(), metadata}).
type File struct {
	Kind string // "string" | "lazy" | "hash" | "hollow"

	// string kind
	Content        string
	Comments       CommentList
	TrackedChanges TrackedChangeList

	// hash/ranges kind
	Hash       string
	RangesHash string

	// lazy kind
	StringLength int
	LazyOps      []json.RawMessage // buffered raw edit ops

	// hollow-bin kind
	ByteLength int

	// File-level metadata (raw object JSON)
	Metadata json.RawMessage
}

// --- FromRaw (ports File/FileData.fromRaw precedence) ---

func FileFromRaw(raw json.RawMessage) (*File, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var probe struct {
		Hash         *string           `json:"hash"`
		RangesHash   *string           `json:"rangesHash"`
		Content      *string           `json:"content"`
		StringLength *float64          `json:"stringLength"`
		ByteLength   *float64          `json:"byteLength"`
		Operations   []json.RawMessage `json:"operations"`
		Comments     []json.RawMessage `json:"comments"`
		Tracked      []json.RawMessage `json:"trackedChanges"`
		Metadata     json.RawMessage   `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, &BadRawError{Msg: "bad file raw: " + err.Error()}
	}
	f := &File{}
	// Node FileData.fromRaw precedence:
	//   hash + byteLength        -> BinaryFileData   {hash, byteLength}
	//   hash + stringLength      -> LazyStringFileData {hash, stringLength, ...}
	//   hash                     -> HashFileData     {hash[, rangesHash]}
	//   byteLength               -> HollowBinaryFileData {byteLength}
	//   stringLength             -> HollowStringFileData {stringLength}
	//   content                  -> StringFileData   {content, ...}
	switch {
	case probe.Hash != nil && probe.ByteLength != nil:
		f.Kind = "binary"
		f.Hash = *probe.Hash
		f.ByteLength = int(*probe.ByteLength)
		f.RangesHash = ""
	case probe.Hash != nil && probe.RangesHash != nil:
		if probe.StringLength != nil {
			f.Kind = "lazy"
			f.StringLength = int(*probe.StringLength)
			f.LazyOps = probe.Operations
		} else {
			f.Kind = "hash"
		}
		f.Hash = *probe.Hash
		f.RangesHash = *probe.RangesHash
	case probe.Hash != nil && probe.StringLength != nil:
		f.Kind = "lazy"
		f.Hash = *probe.Hash
		f.StringLength = int(*probe.StringLength)
		f.LazyOps = probe.Operations
	case probe.Hash != nil:
		f.Kind = "hash"
		f.Hash = *probe.Hash
	case probe.RangesHash != nil && probe.StringLength != nil:
		// {stringLength, rangesHash} — lazy without hash (post-serialization)
		f.Kind = "lazy"
		f.StringLength = int(*probe.StringLength)
		f.RangesHash = *probe.RangesHash
		f.LazyOps = probe.Operations
	case probe.RangesHash != nil:
		f.Kind = "hash"
		f.RangesHash = *probe.RangesHash
	case probe.StringLength != nil && probe.RangesHash == nil && probe.ByteLength == nil:
		f.Kind = "hollowStr"
		f.StringLength = int(*probe.StringLength)
	case probe.ByteLength != nil:
		f.Kind = "hollowBin"
		f.ByteLength = int(*probe.ByteLength)
	case probe.Content != nil:
		f.Kind = "string"
		f.Content = *probe.Content
		f.Comments = CommentListFromRaw(probe.Comments)
		f.TrackedChanges = TrackedChangeListFromRaw(probe.Tracked)
	default:
		return nil, &BadRawError{Msg: "unknown file data " + string(raw)}
	}
	f.Metadata = probe.Metadata
	return f, nil
}

// --- ToRaw (fresh values only; Node key order) ---

// ToRaw — data raw + "metadata" (Node: metadata key present when !_.isEmpty;
// empty {} => omitted).
func (f *File) ToRaw() json.RawMessage {
	var data []byte
	switch f.Kind {
	case "string":
		// Node StringFileData.toRaw: {content, comments? (if len>0),
		// trackedChanges? (if len>0)}, in that key order.
		buf := make([]byte, 0, len(f.Content)+64)
		buf = append(buf, `{"content":`...)
		cb, _ := json.Marshal(f.Content)
		buf = append(buf, cb...)
		if len(f.Comments) > 0 {
			buf = append(buf, `,"comments":`...)
			buf = append(buf, commentsToRaw(f.Comments)...)
		}
		if len(f.TrackedChanges) > 0 {
			buf = append(buf, `,"trackedChanges":`...)
			buf = append(buf, f.TrackedChanges.ToRaw()...)
		}
		buf = append(buf, '}')
		data = buf
	case "lazy":
		// Node LazyStringFileData.toRaw: {hash, stringLength[, rangesHash][,
		// operations]}. hash is ALWAYS present (empty string preserved).
		b, _ := json.Marshal(struct {
			Hash         string            `json:"hash"`
			StringLength int               `json:"stringLength"`
			RangesHash   string            `json:"rangesHash,omitempty"`
			Operations   []json.RawMessage `json:"operations,omitempty"`
		}{Hash: f.Hash, StringLength: f.StringLength, RangesHash: f.RangesHash, Operations: f.LazyOps})
		data = b
	case "hash":
		// Node HashFileData.toRaw: {hash[, rangesHash]}.
		if f.RangesHash != "" {
			b, _ := json.Marshal(struct {
				Hash       string `json:"hash"`
				RangesHash string `json:"rangesHash"`
			}{f.Hash, f.RangesHash})
			data = b
		} else {
			b, _ := json.Marshal(struct {
				Hash string `json:"hash"`
			}{f.Hash})
			data = b
		}
	case "hollowStr":
		b, _ := json.Marshal(struct {
			StringLength int `json:"stringLength"`
		}{f.StringLength})
		data = b
	case "hollowBin":
		b, _ := json.Marshal(struct {
			ByteLength int `json:"byteLength"`
		}{f.ByteLength})
		data = b
	case "binary":
		// Node BinaryFileData.toRaw: {hash, byteLength}.
		b, _ := json.Marshal(struct {
			Hash       string `json:"hash"`
			ByteLength int    `json:"byteLength"`
		}{f.Hash, f.ByteLength})
		data = b
	default:
		data = json.RawMessage(`{}`)
	}
	return assembleFile(data, f.Metadata)
}

func assembleFile(data, metadata json.RawMessage) json.RawMessage {
	if len(metadata) == 0 || string(metadata) == "null" || string(metadata) == "{}" {
		return data
	}
	// Node: data.slice(0, -1) + ',metadata:' + metadata + '}'
	out := make([]byte, 0, len(data)+len(metadata)+11)
	out = append(out, data[:len(data)-1]...)
	out = append(out, ",\"metadata\":"...)
	out = append(out, metadata...)
	out = append(out, '}')
	return out
}

// --- State queries ---

// IsEditable — Node FileData.isEditable(): StringFileData/LazyStringFileData/
// HollowStringFileData => true; BinaryFileData/HashFileData/HollowBinaryFileData
// => false.
func (f *File) IsEditable() bool {
	return f.Kind == "string" || f.Kind == "lazy" || f.Kind == "hollowStr"
}

// IsString: kind is string (editable text in the engine).
func (f *File) IsString() bool { return f.Kind == "string" }

// --- Edit (ports File.edit dispatch: File.NotEditableError for non-editable) ---

func (f *File) Edit(op *TextOp) error {
	if !f.IsEditable() {
		return &NotEditableError{}
	}
	switch f.Kind {
	case "string":
		return op.ApplyFile(f) // StringFileData.edit: operation.apply(fileData)
	case "lazy":
		// Node LazyStringFileData.edit: op.applyToLength(stringLength) then
		// push the raw op.
		newLength, err := op.ApplyToLength(f.StringLength)
		if err != nil {
			return err
		}
		f.StringLength = newLength
		f.LazyOps = append(f.LazyOps, op.ToRaw())
		return nil
	case "hollowStr":
		// Node HollowStringFileData.edit: only updates stringLength.
		newLength, err := op.ApplyToLength(f.StringLength)
		if err != nil {
			return err
		}
		f.StringLength = newLength
		return nil
	default:
		return &NotEditableError{}
	}
}

// --- Getters (engine) ---

func (f *File) GetHash() string {
	switch f.Kind {
	case "string", "hash", "binary":
		return f.Hash
	case "lazy":
		if len(f.LazyOps) > 0 {
			return "" // Node: null when buffered ops
		}
		return f.Hash
	default:
		return ""
	}
}
func (f *File) GetRangesHash() string {
	switch f.Kind {
	case "hash", "lazy":
		if f.Kind == "lazy" && len(f.LazyOps) > 0 {
			return ""
		}
		return f.RangesHash
	default:
		return ""
	}
}

func (f *File) GetByteLength() int {
	switch f.Kind {
	case "string":
		// Node StringFileData.getByteLength: Buffer.byteLength(content,'utf8').
		return len(f.Content)
	case "hash":
		return -1 // Node HashFileData.getByteLength → not applicable
	default:
		return f.ByteLength
	}
}

// GetStringLength — UTF-16 code units (Node stringLength counts UTF-16).
func (f *File) GetStringLength() int {
	switch f.Kind {
	case "string":
		return utf16Length(f.Content)
	default:
		return f.StringLength
	}
}

// BlobStoreI — the blob-store surface the engine needs.
//
// StringBlob exposes the blob metadata Node's Blob model carries
// (byteLength + stringLength, the latter -1 for binary blobs, Node
// stringLength undefined) that HashFileData.load consults to pick the
// binary vs lazy file kind.
type BlobStoreI interface {
	PutString(content string) (string, error)
	PutObject(data []byte) (string, error)
	GetHashBlob(hash string) (string, error)
	GetRangesBlob(rangesHash string) (json.RawMessage, error)
	StringBlob(hash string) (byteLength, stringLength int, found bool)
}

// Store ports FileData.store (per-kind dispatch):
//
//	string — putString(content); optionally putObject({comments, trackedChanges});
//	  returns {hash[, rangesHash]}
//	lazy — if operations=[] returns {hash[, rangesHash]}; else toEager() then store
//	hash — returns {hash[, rangesHash]} (no blob write)
//	binary — returns {hash}
//
// Node File.store wraps the kind raw with metadata (storeRawMetadata).
func (f *File) Store(bs BlobStoreI) (json.RawMessage, error) {
	switch f.Kind {
	case "string":
		w, err := f.storeString(bs)
		if err != nil {
			return nil, err
		}
		return assembleFile(w, f.Metadata), nil
	case "lazy":
		if len(f.LazyOps) == 0 {
			w, err := fileHashOnly(f.Hash, f.RangesHash)
			if err != nil {
				return nil, err
			}
			// Node File.store always wraps with storeRawMetadata (incl.
			// {hash} lazy-no-ops) so metadata survives the chunk wire.
			return assembleFile(w, f.Metadata), nil
		}
		eager, err := f.toEager(bs)
		if err != nil {
			return nil, err
		}
		w, err := eager.storeString(bs)
		if err != nil {
			return nil, err
		}
		return assembleFile(w, f.Metadata), nil
	case "hash":
		w, err := fileHashOnly(f.Hash, f.RangesHash)
		if err != nil {
			return nil, err
		}
		return assembleFile(w, f.Metadata), nil
	case "binary":
		w, err := fileHashOnly(f.Hash, "")
		if err != nil {
			return nil, err
		}
		return assembleFile(w, f.Metadata), nil
	default:
		return nil, &BadRawError{Msg: "store not implemented for kind " + f.Kind}
	}
}

func fileHashOnly(hash, rangesHash string) (json.RawMessage, error) {
	if rangesHash != "" {
		b, _ := json.Marshal(struct {
			Hash       string `json:"hash"`
			RangesHash string `json:"rangesHash"`
		}{hash, rangesHash})
		return b, nil
	}
	b, _ := json.Marshal(struct {
		Hash string `json:"hash"`
	}{hash})
	return b, nil
}

// storeString ports StringFileData.store.
func (f *File) storeString(bs BlobStoreI) (json.RawMessage, error) {
	hash, err := bs.PutString(f.Content)
	if err != nil {
		return nil, err
	}
	if len(f.Comments) == 0 && len(f.TrackedChanges) == 0 {
		b, _ := json.Marshal(struct {
			Hash string `json:"hash"`
		}{hash})
		return b, nil
	}
	ranges := struct {
		Comments       json.RawMessage `json:"comments"`
		TrackedChanges json.RawMessage `json:"trackedChanges"`
	}{commentsToRaw(f.Comments), f.TrackedChanges.ToRaw()}
	b, _ := json.Marshal(ranges)
	rh, err := bs.PutObject(b)
	if err != nil {
		return nil, err
	}
	return fileHashOnly(hash, rh)
}

func commentsToRaw(l CommentList) json.RawMessage {
	out := make([]json.RawMessage, 0, len(l))
	for i := range l {
		c := l[i]
		ranges := make([]json.RawMessage, 0, len(c.Ranges))
		for j := range c.Ranges {
			ranges = append(ranges, c.Ranges[j].ToRaw())
		}
		obj := struct {
			CommentID string            `json:"id"`
			Ranges    []json.RawMessage `json:"ranges"`
			Resolved  bool              `json:"resolved,omitempty"`
		}{CommentID: c.ID, Ranges: ranges, Resolved: c.Resolved}
		b, _ := json.Marshal(obj)
		out = append(out, b)
	}
	b, _ := json.Marshal(out)
	return b
}

// Clone (Node File.clone: deep clone via toRaw/fromRaw in Node; Go: fresh
// struct with copied slices. CommentList/TrackedChangeList copy their slice
// backing, since all mutators are value-semantic returning new lists).
func (f *File) Clone() *File {
	cp := *f
	cp.LazyOps = append([]json.RawMessage(nil), f.LazyOps...)
	cp.Comments = make(CommentList, len(f.Comments))
	copy(cp.Comments, f.Comments)
	cp.TrackedChanges = make(TrackedChangeList, len(f.TrackedChanges))
	copy(cp.TrackedChanges, f.TrackedChanges)
	return &cp
}
