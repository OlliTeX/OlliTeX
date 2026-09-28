package historyot

import (
	"errors"
	"ollitex/go/services/project-history/internal/opmodel"
)

var ErrOperationFromRawInvalid = errors.New("invalid raw operation")

// Operation — vendor abstract `Operation` (lib/operation/index.js), the base
// class of file-level operations. In Go the concrete types (AddFileOperation,
// EditFileOperation, MoveFileOperation, SetFileMetadataOperation, NoOperation)
// all implement this interface; the Go replacement for vendor `instanceof`
// dispatch is type switches.
type Operation interface {
	// ToRaw — vendor `toRaw()`.
	ToRaw() map[string]any
	// IsNoOp — vendor `isNoOp()`.
	IsNoOp() bool
	// ClassName — vendor `constructor.name`, used for error strings.
	ClassName() string
	// CanBeComposedWith — vendor `canBeComposedWith(other)`.
	CanBeComposedWith(other Operation) bool
	// Compose — vendor `compose(other)`.
	Compose(other Operation) (Operation, error)
	// CanBeComposedWithForUndo — vendor `canBeComposedWithForUndo(other)`.
	CanBeComposedWithForUndo(other Operation) bool
	// FindBlobHashes — vendor `findBlobHashes(blobHashes)`: append any blob
	// hashes to `out`.
	FindBlobHashes(out map[string]struct{})
}

// AddFileOperation — vendor `AddFileOperation`
// (lib/operation/add_file_operation.js). Wraps a File.
type AddFileOperation struct {
	FilePathname string
	file         *File
}

// AddFileOperationFromRaw — vendor `AddFileOperation.fromRaw(raw)`.
func AddFileOperationFromRaw(raw map[string]any) (*AddFileOperation, error) {
	p, _ := raw["pathname"].(string)
	fileRaw, ok := raw["file"].(map[string]any)
	if !ok {
		return nil, errOperationFromRaw("AddFileOperation.fromRaw: bad raw.file")
	}
	file, err := FileFromRaw(fileRaw)
	if err != nil {
		return nil, err
	}
	return &AddFileOperation{FilePathname: p, file: file}, nil
}

// NewAddFileOperation — vendor `Operation.addFile(pathname, file)`.
func NewAddFileOperation(pathname string, file *File) *AddFileOperation {
	return &AddFileOperation{FilePathname: pathname, file: file}
}

func (o *AddFileOperation) ClassName() string { return "AddFileOperation" }
func (o *AddFileOperation) ToRaw() map[string]any {
	return map[string]any{"pathname": o.FilePathname, "file": o.file.ToRaw()}
}
func (o *AddFileOperation) IsNoOp() bool                            { return false }
func (o *AddFileOperation) CanBeComposedWithForUndo(Operation) bool { return false }
func (o *AddFileOperation) CanBeComposedWith(Operation) bool        { return false }

func (o *AddFileOperation) Compose(Operation) (Operation, error) {
	return nil, errors.New("not implemented")
}

func (o *AddFileOperation) FindBlobHashes(out map[string]struct{}) {
	if h := o.file.GetHash(); h != "" {
		out[h] = struct{}{}
	}
	if h := o.file.GetRangesHash(); h != "" {
		out[h] = struct{}{}
	}
}

// EditFileOperation — vendor `EditFileOperation`
// (lib/operation/edit_file_operation.js). Wraps a single EditOp.
type EditFileOperation struct {
	FilePathname string
	editOp       EditOp
}

// EditFileOperationFromRaw — vendor `EditFileOperation.fromRaw(raw)`.
func EditFileOperationFromRaw(raw map[string]any) (*EditFileOperation, error) {
	p, _ := raw["pathname"].(string)
	editOp, err := Builder.BuildFromRaw(raw)
	if err != nil {
		return nil, err
	}
	return &EditFileOperation{FilePathname: p, editOp: editOp}, nil
}

// NewEditFileOperation — vendor `Operation.editFile(pathname, editOperation)`.
func NewEditFileOperation(pathname string, editOp EditOp) *EditFileOperation {
	return &EditFileOperation{FilePathname: pathname, editOp: editOp}
}

func (o *EditFileOperation) ClassName() string { return "EditFileOperation" }
func (o *EditFileOperation) ToRaw() map[string]any {
	out := map[string]any{"pathname": o.FilePathname}
	for k, v := range o.editOp.ToRaw() {
		out[k] = v
	}
	return out
}
func (o *EditFileOperation) IsNoOp() bool {
	// Vendor: EditFileOperation does NOT override isNoOp() — the base
	// Operation.isNoOp returns false (even when wrapping an EditNoOperation).
	return false
}

// CanBeComposedWithForUndo — vendor:
// canBeComposedWith(other) && inner editOp.canBeComposedWithForUndo(other).
func (o *EditFileOperation) CanBeComposedWithForUndo(other Operation) bool {
	if !o.CanBeComposedWith(other) {
		return false
	}
	otherEF, ok := other.(*EditFileOperation)
	if !ok {
		return false
	}
	return o.editOp.CanBeComposedWithForUndo(otherEF.editOp)
}

func (o *EditFileOperation) CanBeComposedWith(other Operation) bool {
	otherEF, ok := other.(*EditFileOperation)
	if !ok {
		return false
	}
	if o.FilePathname != otherEF.FilePathname {
		return false
	}
	return o.editOp.CanBeComposedWith(otherEF.editOp)
}

func (o *EditFileOperation) Compose(other Operation) (Operation, error) {
	otherEF, ok := other.(*EditFileOperation)
	if !ok {
		return nil, errors.New("not implemented")
	}
	composed, err := o.editOp.Compose(otherEF.editOp)
	if err != nil {
		return nil, err
	}
	return &EditFileOperation{FilePathname: o.FilePathname, editOp: composed}, nil
}

func (o *EditFileOperation) FindBlobHashes(map[string]struct{}) {}

// MoveFileOperation — vendor `MoveFileOperation`
// (lib/operation/move_file_operation.js). Moves or removes a file.
type MoveFileOperation struct {
	FilePathname string
	NewPathname  string
}

// NewMoveFileOperation — vendor `Operation.moveFile(pathname, newPathname)`.
func NewMoveFileOperation(pathname string, newPathname string) *MoveFileOperation {
	return &MoveFileOperation{FilePathname: pathname, NewPathname: newPathname}
}

// NewRemoveFileOperation — vendor `Operation.removeFile(pathname)`:
// MoveFileOperation(pathname, ”).
func NewRemoveFileOperation(pathname string) *MoveFileOperation {
	return &MoveFileOperation{FilePathname: pathname, NewPathname: ""}
}

func (o *MoveFileOperation) ClassName() string { return "MoveFileOperation" }

func (o *MoveFileOperation) ToRaw() map[string]any {
	return map[string]any{
		"pathname":    o.FilePathname,
		"newPathname": o.NewPathname,
	}
}
func (o *MoveFileOperation) IsNoOp() bool                            { return false }
func (o *MoveFileOperation) IsRemoveFile() bool                      { return o.NewPathname == "" }
func (o *MoveFileOperation) CanBeComposedWithForUndo(Operation) bool { return false }
func (o *MoveFileOperation) CanBeComposedWith(Operation) bool        { return false }

func (o *MoveFileOperation) Compose(Operation) (Operation, error) {
	return nil, errors.New("not implemented")
}

func (o *MoveFileOperation) FindBlobHashes(map[string]struct{}) {}

// SetFileMetadataOperation — vendor `SetFileMetadataOperation`
// (lib/operation/set_file_metadata_operation.js). Metadata map rides inside.
type SetFileMetadataOperation struct {
	FilePathname string
	Metadata     map[string]any
}

// SetFileMetadataOperationFromRaw — vendor `fromRaw` (not in vendor: it is
// constructed directly by UpdateTranslator as {pathname, metadata}).
func SetFileMetadataOperationFromRaw(raw map[string]any) (*SetFileMetadataOperation, error) {
	p, _ := raw["pathname"].(string)
	m, ok := raw["metadata"].(map[string]any)
	if !ok {
		return nil, errOperationFromRaw("SetFileMetadataOperation.fromRaw: bad raw.metadata")
	}
	return &SetFileMetadataOperation{FilePathname: p, Metadata: m}, nil
}

func (o *SetFileMetadataOperation) ClassName() string {
	return "SetFileMetadataOperation"
}
func (o *SetFileMetadataOperation) ToRaw() map[string]any {
	return map[string]any{
		"pathname": o.FilePathname,
		"metadata": shallowCopyMap(o.Metadata),
	}
}
func (o *SetFileMetadataOperation) IsNoOp() bool                            { return false }
func (o *SetFileMetadataOperation) CanBeComposedWithForUndo(Operation) bool { return false }
func (o *SetFileMetadataOperation) CanBeComposedWith(Operation) bool        { return false }

func (o *SetFileMetadataOperation) Compose(Operation) (Operation, error) {
	return nil, errors.New("not implemented")
}

func (o *SetFileMetadataOperation) FindBlobHashes(map[string]struct{}) {}

// NoOperation — vendor `NoOperation` (lib/operation/no_operation.js): an
// explicit no-op (e.g. move a file to itself).
type NoOperation struct{}

func NewNoOperation() *NoOperation { return &NoOperation{} }

func (o *NoOperation) ToRaw() map[string]any                   { return map[string]any{} }
func (o *NoOperation) IsNoOp() bool                            { return true }
func (o *NoOperation) ClassName() string                       { return "NoOperation" }
func (o *NoOperation) CanBeComposedWithForUndo(Operation) bool { return false }
func (o *NoOperation) CanBeComposedWith(Operation) bool        { return false }

func (o *NoOperation) Compose(Operation) (Operation, error) {
	return nil, errors.New("not implemented")
}

func (o *NoOperation) FindBlobHashes(map[string]struct{}) {}

// OperationFromRaw — vendor `Operation.fromRaw(raw)` dispatch:
//
//	'file' in raw           -> AddFileOperation
//	'textOperation' | 'commentId' | 'deleteComment' | 'noOp' in raw
//	                          -> EditFileOperation.fromRaw
//	'newPathname' in raw    -> MoveFileOperation
//	'metadata' in raw       -> SetFileMetadataOperation
//	isEmpty(raw)            -> NoOperation
//	else                    -> invalid
//
// Vendor uses lodash _.isEmpty: a raw map is "empty" when it has no own
// keys. The Go mirror: len(raw) == 0.
func OperationFromRaw(raw map[string]any) (Operation, error) {
	if _, has := raw["file"]; has {
		return AddFileOperationFromRaw(raw)
	}
	if hasEditOpMarker(raw) {
		return EditFileOperationFromRaw(raw)
	}
	if _, has := raw["newPathname"]; has {
		p, _ := raw["pathname"].(string)
		np, _ := raw["newPathname"].(string)
		return &MoveFileOperation{FilePathname: p, NewPathname: np}, nil
	}
	if _, has := raw["metadata"]; has {
		return SetFileMetadataOperationFromRaw(raw)
	}
	if len(raw) == 0 {
		return &NoOperation{}, nil
	}
	return nil, errOperationFromRaw("invalid raw operation " + mapString(raw))
}

func hasEditOpMarker(raw map[string]any) bool {
	if _, has := raw["textOperation"]; has {
		return true
	}
	if _, has := raw["commentId"]; has {
		return true
	}
	if _, has := raw["deleteComment"]; has {
		return true
	}
	if _, has := raw["noOp"]; has {
		return true
	}
	return false
}

// OperationEditFile — vendor `Operation.editFile(pathname, editOperation)`.
func OperationEditFile(pathname string, editOp EditOp) *EditFileOperation {
	return &EditFileOperation{FilePathname: pathname, editOp: editOp}
}

func errOperationFromRaw(msg string) *opmodel.UnprocessableError {
	return opmodel.NewUnprocessableError(msg, nil)
}
