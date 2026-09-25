package core

import (
	"encoding/json"
)

// Operation (file-level op sum). Ports Operation.fromRaw dispatch by raw key
// presence (first match wins), golden-locked against the Node oracle:
//
//	"file"          -> addFile
//	"textOperation" | "commentId" | "deleteComment" | "noOp" -> editFile
//	"newPathname"   -> moveFile (NewPathname=="" is remove)
//	"metadata"      -> setFileMetadata
//	{} (empty)      -> noOp
//	anything else   -> BadRawError "invalid raw operation " + <json>
type Operation struct {
	Kind     string // "addFile" | "editFile" | "moveFile" | "setFileMetadata" | "noOp"
	Pathname string

	AddFile     *File
	EditOp      *EditOp
	NewPathname string
	SetMeta     json.RawMessage // nil -> {}
}

type opProbe struct {
	File      json.RawMessage `json:"file"`
	NoOp      json.RawMessage `json:"noOp"`
	TextOp    json.RawMessage `json:"textOperation"`
	CommentId string          `json:"commentId"`
	DeleteCmt string          `json:"deleteComment"`
	NewPath   json.RawMessage `json:"newPathname"`
	Metadata  json.RawMessage `json:"metadata"`
}

type opPathname struct {
	Pathname    string          `json:"pathname"`
	NewPathname string          `json:"newPathname"`
	Metadata    json.RawMessage `json:"metadata"`
}

// OperationFromRaw ports Operation.fromRaw.
func OperationFromRaw(raw json.RawMessage) (*Operation, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, &BadRawError{Msg: "bad operation raw (empty)"}
	}
	var p opProbe
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &BadRawError{Msg: "bad operation raw: " + err.Error()}
	}
	var pp opPathname
	_ = json.Unmarshal(raw, &pp)

	switch {
	case len(p.File) != 0:
		f, err := FileFromRaw(p.File)
		if err != nil {
			return nil, err
		}
		return &Operation{Kind: "addFile", Pathname: pp.Pathname, AddFile: f}, nil
	case len(p.TextOp) != 0 || p.CommentId != "" || p.DeleteCmt != "" || len(p.NoOp) != 0:
		e, err := EditOpFromRaw(raw)
		if err != nil {
			return nil, err
		}
		return &Operation{Kind: "editFile", Pathname: pp.Pathname, EditOp: e}, nil
	case rawKeyPresent(raw, "newPathname"):
		return &Operation{Kind: "moveFile", Pathname: pp.Pathname, NewPathname: pp.NewPathname}, nil
	case rawKeyPresent(raw, "metadata"):
		return &Operation{Kind: "setFileMetadata", Pathname: pp.Pathname, SetMeta: pp.Metadata}, nil
	default:
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, &BadRawError{Msg: "bad operation raw: " + err.Error()}
		}
		if len(m) == 0 {
			return &Operation{Kind: "noOp"}, nil
		}
		b, _ := json.Marshal(raw)
		return nil, &BadRawError{Msg: "invalid raw operation " + string(b)}
	}
}

// rawKeyPresent — whether key is a key of the JSON object raw.
func rawKeyPresent(raw json.RawMessage, key string) bool {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false
	}
	_, ok := obj[key]
	return ok
}

// Factories (Node Operation.addFile/removeFile/moveFile/editFile/setFileMetadata/noOp).

// AddFile (Node AddFileOperation).
func AddFile(pathname string, file *File) *Operation {
	return &Operation{Kind: "addFile", Pathname: pathname, AddFile: file}
}

// MoveFile (Node MoveFileOperation).
func MoveFile(pathname, newPathname string) *Operation {
	return &Operation{Kind: "moveFile", Pathname: pathname, NewPathname: newPathname}
}

// RemoveFile (Node MoveFileOperation with newPathname = "").
func RemoveFile(pathname string) *Operation { return MoveFile(pathname, "") }

// EditFile (Node EditFileOperation) wrapping an EditOp.
func EditFile(pathname string, op *EditOp) *Operation {
	return &Operation{Kind: "editFile", Pathname: pathname, EditOp: op}
}

// SetFileMetadata (Node SetFileMetadataOperation).
func SetFileMetadata(pathname string, metadata json.RawMessage) *Operation {
	return &Operation{Kind: "setFileMetadata", Pathname: pathname, SetMeta: metadata}
}

// NoOp (Node NoOperation).
func NoOp() *Operation { return &Operation{Kind: "noOp"} }

// ApplyTo (Node Operation.applyTo) — dispatch per kind onto a snapshot.
func (o *Operation) ApplyTo(snap *Snapshot) error { //nolint:contextcheck — no context needed
	switch o.Kind {
	case "noOp":
		return nil
	case "addFile":
		// Node: snapshot.addFile(this.pathname, this.file.clone())
		return snap.AddFile(o.Pathname, o.AddFile.Clone())
	case "editFile":
		f := snap.GetFile(o.Pathname)
		if f == nil {
			return &EditMissingFileError{Pathname: o.Pathname}
		}
		return o.EditOp.ApplyFile(f)
	case "moveFile":
		return snap.MoveFile(o.Pathname, o.NewPathname)
	case "setFileMetadata":
		f := snap.GetFile(o.Pathname)
		if f == nil {
			return nil
		}
		f.Metadata = cloneMeta(o.SetMeta)
		return nil
	default:
		return nil
	}
}

func cloneMeta(b []byte) json.RawMessage {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	return b
}

// (mustStringJSON helper is at bottom of this file)

// ToRaw ports Operation.toRaw per kind. Wire key *presence* is what dispatch
// depends on; object key *order* is not significant. For addFile / moveFile we
// hand-build to preserve Node insertion order (pathname first), which parse-
// equivalence does care about for arrays inside the object.
func (o *Operation) ToRaw() json.RawMessage {
	switch o.Kind {
	case "addFile":
		return json.RawMessage(`{"pathname":` + string(mustStringJSON(o.Pathname)) + `,"file":` + string(o.AddFile.ToRaw()) + `}`)
	case "editFile":
		pb := string(mustStringJSON(o.Pathname))
		editRaw := o.EditOp.ToRaw()
		var editObj map[string]json.RawMessage
		_ = json.Unmarshal(editRaw, &editObj)
		out := make(map[string]json.RawMessage, len(editObj)+1)
		out["pathname"] = json.RawMessage(pb)
		for k, v := range editObj {
			out[k] = v
		}
		b, _ := json.Marshal(out)
		return b
	case "moveFile":
		return json.RawMessage(`{"pathname":` + string(mustStringJSON(o.Pathname)) + `,"newPathname":` + string(mustStringJSON(o.NewPathname)) + `}`)
	case "setFileMetadata":
		meta := json.RawMessage("{}")
		if o.SetMeta != nil && string(o.SetMeta) != "null" {
			meta = o.SetMeta
		}
		return json.RawMessage(`{"pathname":` + string(mustStringJSON(o.Pathname)) + `,"metadata":` + string(meta) + `}`)
	default:
		return json.RawMessage(`{}`)
	}
}

// --- Persistence (ports Operation.store / loadFiles / findBlobHashes) ---

// Store (Node Operation.store): base = toRaw (no blob write); AddFileOperation
// overrides: return {pathname, file: await file.store(blobStore)}.
//
//	editFile / moveFile / setFileMetadata / noOp -> toRaw
//	addFile  -> {pathname, file: <stored raw file>}
func (o *Operation) Store(bs BlobStoreI) (json.RawMessage, error) {
	if o.Kind == "addFile" {
		if o.AddFile == nil {
			return nil, &BadRawError{Msg: "addFile operation missing file"}
		}
		rawFile, err := o.AddFile.Store(bs)
		if err != nil {
			return nil, err
		}
		w, _ := json.Marshal(map[string]any{
			"pathname": o.Pathname,
			"file":     rawFile,
		})
		return w, nil
	}
	return o.ToRaw(), nil
}

// LoadFiles (Node Operation.loadFiles): base is a no-op; addFile loads its
// file (Node: this.file.load(kind, blobStore) replaces the data in place).
func (o *Operation) LoadFiles(kind string, bs BlobStoreI) error {
	if o.Kind != "addFile" {
		return nil
	}
	if o.AddFile == nil {
		return &BadRawError{Msg: "addFile operation missing file"}
	}
	loaded, err := o.AddFile.Load(kind, bs)
	if err != nil {
		return err
	}
	o.AddFile = loaded
	return nil
}

// FindBlobHashes (Node Operation.findBlobHashes): base is a no-op; addFile
// contributes file.getHash() and file.getRangesHash() (when present).
func (o *Operation) FindBlobHashes(blobHashes *map[string]struct{}) {
	if o.Kind != "addFile" || o.AddFile == nil || blobHashes == nil {
		return
	}
	if h := o.AddFile.GetHash(); h != "" {
		(*blobHashes)[h] = struct{}{}
	}
	if rh := o.AddFile.GetRangesHash(); rh != "" {
		(*blobHashes)[rh] = struct{}{}
	}
}

func mustStringJSON(s string) []byte {
	b, _ := json.Marshal(s)
	return b
}
