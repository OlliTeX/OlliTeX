package otc

// Operation constructors (Node: `Operation.addFile`/`editFile`/`moveFile`/
// `removeFile`/`setFileMetadata`).
func OperationAddFile(pathname string, file *File) Operation {
	return &AddFileOperation{Pathname: pathname, File: file}
}
func OperationEditFile(pathname string, op EditOperation) Operation {
	return &EditFileOperation{Pathname: pathname, Operation: op}
}
func OperationMoveFile(pathname, newPathname string) Operation {
	return &MoveFileOperation{Pathname: pathname, NewPathname: newPathname}
}
func OperationRemoveFile(pathname string) Operation {
	return &MoveFileOperation{Pathname: pathname, NewPathname: ""}
}
func OperationSetFileMetadata(pathname string, metadata map[string]any) Operation {
	return &SetFileMetadataOperation{Pathname: pathname, Metadata: metadata}
}

// OperationFromRaw deserialises an Operation (Node: `Operation.fromRaw`).
func OperationFromRaw(raw map[string]any) (Operation, error) {
	if rawHas(raw, "file") {
		return AddFileOperationFromRaw(raw)
	}
	if rawHas(raw, "textOperation") || rawHas(raw, "commentId") ||
		rawHas(raw, "deleteComment") || rawHas(raw, "noOp") {
		return EditFileOperationFromRaw(raw)
	}
	if rawHas(raw, "newPathname") {
		pathname, _ := raw["pathname"].(string)
		newPathname, _ := raw["newPathname"].(string)
		return &MoveFileOperation{Pathname: pathname, NewPathname: newPathname}, nil
	}
	if rawHas(raw, "metadata") {
		pathname, _ := raw["pathname"].(string)
		meta, _ := raw["metadata"].(map[string]any)
		return &SetFileMetadataOperation{Pathname: pathname, Metadata: meta}, nil
	}
	if len(raw) == 0 {
		return &NoOperation{}, nil
	}
	return nil, gop("invalid raw operation " + rawJSONStr(raw))
}

// OperationTransform implements OT concurrency transform for file-tree ops
// (Node: `Operation.transform`), returning [a', b'] such that
// apply(apply(S, A), B') == apply(apply(S, B), A').
func OperationTransform(a, b Operation) [2]Operation {
	if a.IsNoOp() || b.IsNoOp() {
		return [2]Operation{a, b}
	}
	reverse := func(pr [2]Operation) [2]Operation { return [2]Operation{pr[1], pr[0]} }
	switch at := a.(type) {
	case *AddFileOperation:
		switch bt := b.(type) {
		case *AddFileOperation:
			return transformAddAdd(at, bt)
		case *MoveFileOperation:
			return transformAddMove(at, bt)
		case *EditFileOperation:
			return transformAddEdit(at, bt)
		case *SetFileMetadataOperation:
			return transformAddSet(at, bt)
		}
	case *MoveFileOperation:
		switch bt := b.(type) {
		case *AddFileOperation:
			return reverse(transformAddMove(bt, at))
		case *MoveFileOperation:
			return transformMoveMove(at, bt)
		case *EditFileOperation:
			return transformMoveEdit(at, bt)
		case *SetFileMetadataOperation:
			return transformMoveSet(at, bt)
		}
	case *EditFileOperation:
		switch bt := b.(type) {
		case *AddFileOperation:
			return reverse(transformAddEdit(bt, at))
		case *MoveFileOperation:
			return reverse(transformMoveEdit(bt, at))
		case *EditFileOperation:
			return transformEditEdit(at, bt)
		case *SetFileMetadataOperation:
			return transformEditSet(at, bt)
		}
	case *SetFileMetadataOperation:
		switch bt := b.(type) {
		case *AddFileOperation:
			return reverse(transformAddSet(bt, at))
		case *MoveFileOperation:
			return reverse(transformMoveSet(bt, at))
		case *EditFileOperation:
			return reverse(transformEditSet(bt, at))
		case *SetFileMetadataOperation:
			return transformSetSet(at, bt)
		}
	}
	panic(newTypeError("bad op"))
}

// OperationTransformMultiple transforms each op in as against each in bs,
// saving the primes in place (Node: `Operation.transformMultiple`).
func OperationTransformMultiple(as, bs []Operation) {
	for i := range as {
		for j := range bs {
			primes := OperationTransform(as[i], bs[j])
			as[i] = primes[0]
			bs[j] = primes[1]
		}
	}
}

func transformAddAdd(add1, add2 *AddFileOperation) [2]Operation {
	if add1.Pathname == add2.Pathname {
		return [2]Operation{OperationNoOp, add2}
	}
	return [2]Operation{add1, add2}
}

func transformAddMove(add *AddFileOperation, move *MoveFileOperation) [2]Operation {
	relocate := func() Operation {
		return OperationAddFile(move.NewPathname, add.File.Clone())
	}
	if add.Pathname == move.Pathname {
		if move.IsRemoveFile() {
			return [2]Operation{add, OperationNoOp}
		}
		return [2]Operation{relocate(), OperationMoveFile(add.Pathname, move.NewPathname)}
	}
	if add.Pathname == move.NewPathname {
		return [2]Operation{relocate(), OperationMoveFile(move.Pathname, "")}
	}
	return [2]Operation{add, move}
}

func transformAddEdit(add *AddFileOperation, edit *EditFileOperation) [2]Operation {
	if add.Pathname == edit.Pathname {
		return [2]Operation{add, OperationNoOp}
	}
	return [2]Operation{add, edit}
}

func transformAddSet(add *AddFileOperation, set *SetFileMetadataOperation) [2]Operation {
	if add.Pathname == set.Pathname {
		newFile := add.File.Clone()
		newFile.SetMetadata(set.Metadata)
		return [2]Operation{OperationAddFile(add.Pathname, newFile), set}
	}
	return [2]Operation{add, set}
}

func transformMoveMove(move1, move2 *MoveFileOperation) [2]Operation {
	path1, path2 := move1.Pathname, move2.Pathname
	newPath1, newPath2 := move1.NewPathname, move2.NewPathname

	if path1 == path2 && newPath1 == newPath2 {
		return [2]Operation{OperationNoOp, OperationNoOp}
	}
	if path1 == newPath1 && path2 == newPath2 {
		return [2]Operation{OperationNoOp, OperationNoOp}
	}
	if path1 == newPath1 {
		return [2]Operation{OperationNoOp, move2}
	}
	if path2 == newPath2 {
		return [2]Operation{move1, OperationNoOp}
	}
	if path1 == newPath2 && path2 == newPath1 {
		return [2]Operation{OperationRemoveFile(path1), OperationRemoveFile(path2)}
	}
	if path1 == path2 && newPath1 != newPath2 {
		return [2]Operation{OperationNoOp, OperationMoveFile(newPath1, newPath2)}
	}
	if newPath1 == newPath2 && path1 != path2 {
		return [2]Operation{OperationRemoveFile(path1), move2}
	}
	if path1 == newPath2 && newPath1 != path2 {
		return [2]Operation{OperationMoveFile(newPath2, newPath1), OperationMoveFile(path2, newPath1)}
	}
	if newPath1 == path2 && path1 != newPath2 {
		return [2]Operation{OperationMoveFile(path1, newPath2), OperationMoveFile(newPath1, newPath2)}
	}
	return [2]Operation{move1, move2}
}

func transformMoveEdit(move *MoveFileOperation, edit *EditFileOperation) [2]Operation {
	if move.Pathname == edit.Pathname {
		if move.IsRemoveFile() {
			return [2]Operation{move, OperationNoOp}
		}
		return [2]Operation{move, OperationEditFile(move.NewPathname, edit.Operation)}
	}
	if move.NewPathname == edit.Pathname {
		return [2]Operation{move, OperationNoOp}
	}
	return [2]Operation{move, edit}
}

func transformMoveSet(move *MoveFileOperation, set *SetFileMetadataOperation) [2]Operation {
	if move.Pathname == set.Pathname {
		return [2]Operation{move, OperationSetFileMetadata(move.NewPathname, set.Metadata)}
	}
	if move.NewPathname == set.Pathname {
		return [2]Operation{move, OperationNoOp}
	}
	return [2]Operation{move, set}
}

func transformEditEdit(edit1, edit2 *EditFileOperation) [2]Operation {
	if edit1.Pathname == edit2.Pathname {
		o1, o2, err := TransformEditOps(edit1.Operation, edit2.Operation)
		if err != nil {
			panic(newTypeError("EditFile-EditFile transform: " + err.Error()))
		}
		return [2]Operation{OperationEditFile(edit1.Pathname, o1), OperationEditFile(edit2.Pathname, o2)}
	}
	return [2]Operation{edit1, edit2}
}

func transformEditSet(edit *EditFileOperation, set *SetFileMetadataOperation) [2]Operation {
	return [2]Operation{edit, set}
}

func transformSetSet(set1, set2 *SetFileMetadataOperation) [2]Operation {
	if set1.Pathname == set2.Pathname {
		return [2]Operation{OperationNoOp, set2}
	}
	return [2]Operation{set1, set2}
}
