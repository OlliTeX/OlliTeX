package core

// Ports the Node oracle lib/rebase.js.

// RebaseChanges rebases changes onto the changes another writer got in first.
//
// OperationTransformMultiple rewrites *both* operation lists in place, which
// is what makes a sequence of our changes come out right: each of ours is
// transformed against their change as it stands after our earlier ones,
// rather than against the original.
//
// Operations that transform down to a no-op are pruned, and a change left
// with none is dropped, so the result contains only changes that still carry
// an operation. An empty result means everything we had was already accounted
// for by theirs.
//
// Only the operations are touched. A change's origin, authors, v2Authors and
// timestamp travel through untouched, which is what lets a caller recognise
// one of its own changes read back from history after it has been rebased.
//
//   - ours:   modified in place
//
//   - theirs: the intervening changes, in version order
//
//   - return: the subset of ours that still carries an operation
func RebaseChanges(ours, theirs []*Change) []*Change {
	for _, change := range theirs {
		theirOperations := change.Operations
		for _, ourChange := range ours {
			OperationTransformMultiple(ourChange.Operations, theirOperations)
		}
	}

	rebased := make([]*Change, 0, len(ours))
	for _, change := range ours {
		operations := make([]*Operation, 0, len(change.Operations))
		for _, operation := range change.Operations {
			if !operation.IsNoOp() {
				operations = append(operations, operation)
			}
		}
		if len(operations) == 0 {
			// a change left with no operations is dropped
			continue
		}
		change.Operations = operations
		rebased = append(rebased, change)
	}
	return rebased
}
