package otc

// RebaseChanges rebases `ours` against `theirs`, transforming our ops over the
// theirs' ops, pruning no-ops, and dropping emptied changes (Node: rebase.js).
func RebaseChanges(ours []*Change, theirs []*Change) []*Change {
	for _, theirChange := range theirs {
		theirOperations := theirChange.Operations
		for _, ourChange := range ours {
			OperationTransformMultiple(ourChange.Operations, theirOperations)
		}
	}
	rebased := []*Change{}
	for _, ourChange := range ours {
		operations := []Operation{}
		for _, operation := range ourChange.Operations {
			if !operation.IsNoOp() {
				operations = append(operations, operation)
			}
		}
		if len(operations) == 0 {
			continue
		}
		ourChange.Operations = operations
		rebased = append(rebased, ourChange)
	}
	return rebased
}
