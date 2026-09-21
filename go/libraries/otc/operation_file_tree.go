package otc

import "context"

// Operation is a file-tree operation that changes a Snapshot when applied
// (Node: lib/operation/index.js `class Operation`). Distinct from the
// phase-B3 EditOperation (text/comment) — Node calls that `EditOperation`.
type Operation interface {
	ToRaw() map[string]any
	IsNoOp() bool
	FindBlobHashes(hashes map[string]bool)
	Store(bs BlobStore) (map[string]any, error)
	ApplyTo(s *Snapshot) error
	CanBeComposedWithForUndo(other Operation) bool
	CanBeComposedWith(other Operation) bool
	Compose(other Operation) (Operation, error)
}

// OperationNoOp is the shared no-op sentinel (Node: `Operation.NO_OP`).
var OperationNoOp = &NoOperation{}

// NoOperation mirrors no_operation.js.
type NoOperation struct{}

func (*NoOperation) ToRaw() map[string]any          { return map[string]any{} }
func (*NoOperation) IsNoOp() bool                   { return true }
func (*NoOperation) FindBlobHashes(map[string]bool) {}
func (*NoOperation) Store(BlobStore) (map[string]any, error) {
	return map[string]any{}, nil
}
func (*NoOperation) ApplyTo(*Snapshot) error                 { return nil }
func (*NoOperation) CanBeComposedWithForUndo(Operation) bool { return false }
func (*NoOperation) CanBeComposedWith(Operation) bool        { return false }
func (*NoOperation) Compose(Operation) (Operation, error) {
	return nil, gop("not implemented")
}

// --- AddFileOperation ------------------------------------------------------

// AddFileOperation adds a new file (Node: add_file_operation.js).
type AddFileOperation struct {
	Pathname string
	File     *File
}

func NewAddFileOperation(pathname string, file *File) (*AddFileOperation, error) {
	if pathname == "" {
		return nil, gop("AddFileOperation: bad pathname")
	}
	if file == nil {
		return nil, gop("AddFileOperation: bad file")
	}
	return &AddFileOperation{Pathname: pathname, File: file}, nil
}

func AddFileOperationFromRaw(raw map[string]any) (*AddFileOperation, error) {
	pathname, _ := raw["pathname"].(string)
	fm, ok := raw["file"].(map[string]any)
	if !ok {
		return nil, gop("AddFileOperation: bad raw.file")
	}
	file, err := FileFromRaw(fm)
	if err != nil {
		return nil, err
	}
	return &AddFileOperation{Pathname: pathname, File: file}, nil
}

func (a *AddFileOperation) ToRaw() map[string]any {
	return map[string]any{"pathname": a.Pathname, "file": a.File.ToRaw()}
}
func (a *AddFileOperation) IsNoOp() bool { return false }
func (a *AddFileOperation) FindBlobHashes(h map[string]bool) {
	if hsh := a.File.GetHash(); hsh != nil {
		h[*hsh] = true
	}
	if r := a.File.GetRangesHash(); r != nil {
		h[*r] = true
	}
}
func (a *AddFileOperation) Store(bs BlobStore) (map[string]any, error) {
	rawFile, err := a.File.Store(context.Background(), bs)
	if err != nil {
		return nil, err
	}
	return map[string]any{"pathname": a.Pathname, "file": rawFile}, nil
}
func (a *AddFileOperation) ApplyTo(s *Snapshot) error {
	return s.AddFile(a.Pathname, a.File.Clone())
}
func (a *AddFileOperation) GetPathname() string { return a.Pathname }
func (a *AddFileOperation) GetFile() *File      { return a.File }

func (a *AddFileOperation) CanBeComposedWithForUndo(Operation) bool { return false }
func (a *AddFileOperation) CanBeComposedWith(Operation) bool        { return false }
func (a *AddFileOperation) Compose(Operation) (Operation, error) {
	return nil, gop("not implemented")
}

// --- MoveFileOperation -----------------------------------------------------

// MoveFileOperation moves or removes a file (Node: move_file_operation.js).
type MoveFileOperation struct {
	Pathname    string
	NewPathname string
}

func (m *MoveFileOperation) ToRaw() map[string]any {
	return map[string]any{"pathname": m.Pathname, "newPathname": m.NewPathname}
}
func (m *MoveFileOperation) IsNoOp() bool                   { return false }
func (m *MoveFileOperation) FindBlobHashes(map[string]bool) {}
func (m *MoveFileOperation) Store(BlobStore) (map[string]any, error) {
	return m.ToRaw(), nil
}
func (m *MoveFileOperation) ApplyTo(s *Snapshot) error {
	return s.MoveFile(m.Pathname, m.NewPathname)
}
func (m *MoveFileOperation) GetPathname() string    { return m.Pathname }
func (m *MoveFileOperation) GetNewPathname() string { return m.NewPathname }
func (m *MoveFileOperation) IsRemoveFile() bool     { return m.NewPathname == "" }

func (m *MoveFileOperation) CanBeComposedWithForUndo(Operation) bool { return false }
func (m *MoveFileOperation) CanBeComposedWith(Operation) bool        { return false }
func (m *MoveFileOperation) Compose(Operation) (Operation, error) {
	return nil, gop("not implemented")
}

// --- EditFileOperation -----------------------------------------------------

// EditFileOperation edits a file in place (Node: edit_file_operation.js).
type EditFileOperation struct {
	Pathname  string
	Operation EditOperation
}

func (e *EditFileOperation) ToRaw() map[string]any {
	raw := map[string]any{"pathname": e.Pathname}
	for k, v := range e.Operation.ToJSON() {
		raw[k] = v
	}
	return raw
}
func EditFileOperationFromRaw(raw map[string]any) (*EditFileOperation, error) {
	pathname, _ := raw["pathname"].(string)
	op, err := FromJSONEditOperation(raw)
	if err != nil {
		return nil, err
	}
	return &EditFileOperation{Pathname: pathname, Operation: op}, nil
}
func (e *EditFileOperation) IsNoOp() bool                   { return false }
func (e *EditFileOperation) FindBlobHashes(map[string]bool) {}
func (e *EditFileOperation) Store(BlobStore) (map[string]any, error) {
	return e.ToRaw(), nil
}
func (e *EditFileOperation) ApplyTo(s *Snapshot) error {
	return s.EditFile(e.Pathname, e.Operation)
}
func (e *EditFileOperation) GetPathname() string         { return e.Pathname }
func (e *EditFileOperation) GetOperation() EditOperation { return e.Operation }

func (e *EditFileOperation) CanBeComposedWithForUndo(other Operation) bool {
	oe, ok := other.(*EditFileOperation)
	if !ok {
		return false
	}
	return e.CanBeComposedWith(other) && e.Operation.CanBeComposedWithForUndo(oe.Operation)
}
func (e *EditFileOperation) CanBeComposedWith(other Operation) bool {
	oe, ok := other.(*EditFileOperation)
	if !ok {
		return false
	}
	if e.Pathname != oe.Pathname {
		return false
	}
	return e.Operation.CanBeComposedWith(oe.Operation)
}
func (e *EditFileOperation) Compose(other Operation) (Operation, error) {
	oe, ok := other.(*EditFileOperation)
	if !ok {
		return nil, gop("not implemented")
	}
	composed, err := e.Operation.Compose(oe.Operation)
	if err != nil {
		return nil, err
	}
	return &EditFileOperation{Pathname: e.Pathname, Operation: composed}, nil
}

// --- SetFileMetadataOperation ----------------------------------------------

// SetFileMetadataOperation sets a file's metadata (Node: set_file_metadata_operation.js).
type SetFileMetadataOperation struct {
	Pathname string
	Metadata map[string]any
}

func NewSetFileMetadataOperation(pathname string, metadata map[string]any) (*SetFileMetadataOperation, error) {
	if pathname == "" {
		return nil, gop("SetFileMetadataOperation: bad pathname")
	}
	if metadata == nil {
		return nil, gop("SetFileMetadataOperation: bad metadata")
	}
	return &SetFileMetadataOperation{Pathname: pathname, Metadata: metadata}, nil
}
func (s *SetFileMetadataOperation) ToRaw() map[string]any {
	return map[string]any{"pathname": s.Pathname, "metadata": deepClone(s.Metadata)}
}
func (s *SetFileMetadataOperation) IsNoOp() bool                   { return false }
func (s *SetFileMetadataOperation) FindBlobHashes(map[string]bool) {}
func (s *SetFileMetadataOperation) Store(BlobStore) (map[string]any, error) {
	return s.ToRaw(), nil
}
func (s *SetFileMetadataOperation) ApplyTo(snap *Snapshot) error {
	file := snap.GetFile(s.Pathname)
	if file == nil {
		return nil
	}
	file.SetMetadata(s.Metadata)
	return nil
}
func (s *SetFileMetadataOperation) GetPathname() string         { return s.Pathname }
func (s *SetFileMetadataOperation) GetMetadata() map[string]any { return s.Metadata }

func (s *SetFileMetadataOperation) CanBeComposedWithForUndo(Operation) bool { return false }
func (s *SetFileMetadataOperation) CanBeComposedWith(Operation) bool        { return false }
func (s *SetFileMetadataOperation) Compose(Operation) (Operation, error) {
	return nil, gop("not implemented")
}
