package core

import (
	"errors"
)

// Ports the Node oracle lib/operation/index.js transform section
// (Operation.transform + the nine file-level transformers +
// Operation.transformMultiple).

// IsNoOp (Node Operation.isNoOp; only NoOperation overrides it to true).
func (o *Operation) IsNoOp() bool { return o.Kind == "noOp" }

// OperationTransform ports Operation.transform: transforms two file-level
// operations against each other; returns [a', b'] such that
//
//	apply(apply(S, a), b') = apply(apply(S, b), a')
//
// The oracle's reading convention (preserved here):
//
//	return_value[0] is the op to be applied after arguments[1] (i.e. after b),
//	return_value[1] is the op to be applied after arguments[0] (i.e. after a).
func OperationTransform(a, b *Operation) ([]*Operation, error) {
	if a.IsNoOp() || b.IsNoOp() {
		return []*Operation{a, b}, nil
	}

	switch a.Kind {
	case "addFile":
		switch b.Kind {
		case "addFile":
			return transformAddFileAddFile(a, b), nil
		case "moveFile":
			return transformAddFileMoveFile(a, b), nil
		case "editFile":
			return transformAddFileEditFile(a, b), nil
		case "setFileMetadata":
			return transformAddFileSetFileMetadata(a, b), nil
		default:
			return nil, errors.New("bad op b")
		}
	case "moveFile":
		switch b.Kind {
		case "addFile":
			return transpose(transformAddFileMoveFile, a, b), nil
		case "moveFile":
			return transformMoveFileMoveFile(a, b), nil
		case "editFile":
			return transformMoveFileEditFile(a, b), nil
		case "setFileMetadata":
			return transformMoveFileSetFileMetadata(a, b), nil
		default:
			return nil, errors.New("bad op b")
		}
	case "editFile":
		switch b.Kind {
		case "addFile":
			return transpose(transformAddFileEditFile, a, b), nil
		case "moveFile":
			return transpose(transformMoveFileEditFile, a, b), nil
		case "editFile":
			return transformEditFileEditFile(a, b)
		case "setFileMetadata":
			return transformEditFileSetFileMetadata(a, b), nil
		default:
			return nil, errors.New("bad op b")
		}
	case "setFileMetadata":
		switch b.Kind {
		case "addFile":
			return transpose(transformAddFileSetFileMetadata, a, b), nil
		case "moveFile":
			return transpose(transformMoveFileSetFileMetadata, a, b), nil
		case "editFile":
			return transpose(transformEditFileSetFileMetadata, a, b), nil
		case "setFileMetadata":
			return transformSetFileMetadatas(a, b), nil
		default:
			return nil, errors.New("bad op b")
		}
	default:
		return nil, errors.New("bad op a")
	}
}

// transpose ports Node's `transpose(transformer)`: run the transformer on
// swapped arguments (b, a) and reverse the two-element result.
func transpose(
	f func(x, y *Operation) []*Operation,
	a, b *Operation,
) []*Operation {
	p := f(b, a)
	return []*Operation{p[1], p[0]}
}

// OperationTransformMultiple ports Operation.transformMultiple: transforms
// each operation in as against each operation in bs, priming both lists in
// place.
func OperationTransformMultiple(as, bs []*Operation) error {
	for i := range as {
		for j := range bs {
			primes, err := OperationTransform(as[i], bs[j])
			if err != nil {
				return err
			}
			as[i] = primes[0]
			bs[j] = primes[1]
		}
	}
	return nil
}

// transformAddFileAddFile — same pathname: b wins.
func transformAddFileAddFile(add1, add2 *Operation) []*Operation {
	if add1.Pathname == add2.Pathname {
		return []*Operation{NoOp(), add2} // add2 wins
	}
	return []*Operation{add1, add2}
}

// transformAddFileMoveFile (a = add, b = move).
func transformAddFileMoveFile(add, move *Operation) []*Operation {
	relocateAddFile := func() *Operation {
		return AddFile(move.NewPathname, add.AddFile.Clone())
	}

	if add.Pathname == move.Pathname {
		if move.NewPathname == "" {
			return []*Operation{add, NoOp()}
		}
		return []*Operation{
			relocateAddFile(),
			MoveFile(add.Pathname, move.NewPathname),
		}
	}

	if add.Pathname == move.NewPathname {
		return []*Operation{relocateAddFile(), RemoveFile(move.Pathname)}
	}

	return []*Operation{add, move}
}

// transformAddFileEditFile — the add wins.
func transformAddFileEditFile(add, edit *Operation) []*Operation {
	if add.Pathname == edit.Pathname {
		return []*Operation{add, NoOp()} // the add wins
	}
	return []*Operation{add, edit}
}

// transformAddFileSetFileMetadata (a = add, b = set).
func transformAddFileSetFileMetadata(add, set *Operation) []*Operation {
	if add.Pathname == set.Pathname {
		newFile := add.AddFile.Clone()
		newFile.Metadata = set.SetMeta
		return []*Operation{AddFile(add.Pathname, newFile), set}
	}
	return []*Operation{add, set}
}

// transformMoveFileMoveFile — the 15-case equivalence lattice from the
// oracle (same move, no-ops, opposite, divergent, transitive, convergent,
// no conflict).
func transformMoveFileMoveFile(move1, move2 *Operation) []*Operation {
	path1, path2 := move1.Pathname, move2.Pathname
	newPath1, newPath2 := move1.NewPathname, move2.NewPathname

	// the same move
	if path1 == path2 && newPath1 == newPath2 {
		return []*Operation{NoOp(), NoOp()}
	}

	// no-ops
	if path1 == newPath1 && path2 == newPath2 {
		return []*Operation{NoOp(), NoOp()}
	}
	if path1 == newPath1 {
		return []*Operation{NoOp(), move2}
	}
	if path2 == newPath2 {
		return []*Operation{move1, NoOp()}
	}

	// opposite moves (foo -> bar, bar -> foo)
	if path1 == newPath2 && path2 == newPath1 {
		// We can't handle this very well: if we wanted one to win, the
		// winner would have to be addFile with the other file's content,
		// which we can't reconstruct here. So, we just destroy both files.
		return []*Operation{RemoveFile(path1), RemoveFile(path2)}
	}

	// divergent moves (foo -> bar, foo -> baz); convention: move2 wins
	if path1 == path2 && newPath1 != newPath2 {
		return []*Operation{NoOp(), MoveFile(newPath1, newPath2)}
	}

	// convergent move (foo -> baz, bar -> baz); convention: move2 wins
	if newPath1 == newPath2 && path1 != path2 {
		return []*Operation{RemoveFile(path1), move2}
	}

	// transitive move:
	//   1: foo -> baz, 2: bar -> foo (result: bar -> baz) or
	//   1: foo -> bar, 2: bar -> baz (result: foo -> baz)
	if path1 == newPath2 && newPath1 != path2 {
		return []*Operation{
			MoveFile(newPath2, newPath1),
			MoveFile(path2, newPath1),
		}
	}
	if newPath1 == path2 && path1 != newPath2 {
		return []*Operation{
			MoveFile(path1, newPath2),
			MoveFile(newPath1, newPath2),
		}
	}

	// no conflict
	return []*Operation{move1, move2}
}

// transformMoveFileEditFile (a = move, b = edit).
func transformMoveFileEditFile(move, edit *Operation) []*Operation {
	if move.Pathname == edit.Pathname {
		if move.NewPathname == "" {
			// let the remove win
			return []*Operation{move, NoOp()}
		}
		return []*Operation{
			move,
			EditFile(move.NewPathname, edit.EditOp),
		}
	}

	if move.NewPathname == edit.Pathname {
		// let the move win
		return []*Operation{move, NoOp()}
	}

	return []*Operation{move, edit}
}

// transformMoveFileSetFileMetadata (a = move, b = set).
func transformMoveFileSetFileMetadata(move, set *Operation) []*Operation {
	if move.Pathname == set.Pathname {
		return []*Operation{
			move,
			SetFileMetadata(move.NewPathname, set.SetMeta),
		}
	}
	// A: mv foo -> bar
	// B: set bar.x
	//
	// A': mv foo -> bar
	// B': nothing
	if move.NewPathname == set.Pathname {
		return []*Operation{move, NoOp()} // let the move win
	}
	return []*Operation{move, set}
}

// transformEditFileEditFile — same pathname: transform the EditOps.
func transformEditFileEditFile(edit1, edit2 *Operation) ([]*Operation, error) {
	if edit1.Pathname == edit2.Pathname {
		primary, secondary, err := EditOpTransform(edit1.EditOp, edit2.EditOp)
		if err != nil {
			return nil, err
		}
		return []*Operation{
			EditFile(edit1.Pathname, primary),
			EditFile(edit2.Pathname, secondary),
		}, nil
	}
	return []*Operation{edit1, edit2}, nil
}

// transformEditFileSetFileMetadata — no conflict.
func transformEditFileSetFileMetadata(edit, set *Operation) []*Operation {
	return []*Operation{edit, set}
}

// transformSetFileMetadatas — same pathname: set2 wins.
func transformSetFileMetadatas(set1, set2 *Operation) []*Operation {
	if set1.Pathname == set2.Pathname {
		return []*Operation{NoOp(), set2} // set2 wins
	}
	return []*Operation{set1, set2}
}
