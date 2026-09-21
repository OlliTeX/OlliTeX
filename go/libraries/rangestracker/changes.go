package rangestracker

import (
	"errors"
	"sort"
)

// changes.go — Node applyInsertToChanges / applyDeleteToChanges / _addOp /
// _removeChange / _applyOpModifications / _scanAndMergeAdjacentUpdates, 1:1.

// ErrDeleteMismatch mirrors `throw new Error('deletion does not match text in document')`.
var ErrDeleteMismatch = errors.New("deletion does not match text in document")

// opModRef is one op modification {i|d, p} (Node: opModifications entries).
type opModRef struct {
	i *string // insert text at p (absent here — see applyOpModifications)
	d *string // delete text at p
	p int
}

// applyOpModifications — Node _applyOpModifications(content, opModifications):
// descending position order, deletes before inserts at the same offset.
func applyOpModifications(content string, modifications []opModRef) (string, error) {
	sort.SliceStable(modifications, func(a, b int) bool {
		m1, m2 := modifications[a], modifications[b]
		if m1.p != m2.p {
			return m1.p > m2.p // Node: b.p - a.p (descending)
		}
		switch {
		case m1.i != nil && m2.d != nil:
			return false // insert after delete
		case m1.d != nil && m2.i != nil:
			return true // delete before insert
		}
		return false
	})
	for _, m := range modifications {
		if m.i != nil {
			content = content[:m.p] + *m.i + content[m.p:]
		} else if m.d != nil {
			if content[m.p:m.p+len(*m.d)] != *m.d {
				return "", ErrDeleteMismatch
			}
			content = content[:m.p] + content[m.p+len(*m.d):]
		}
	}
	return content, nil
}

// applyInsertToChanges — 1:1 port including the undoing/reject-delete quirk
// (trackedDeletesAtOpPosition rollback) and the split-another-user insert.
func (rt *RangesTracker) applyInsertToChanges(op *Op, metadata Metadata) { //nolint:gocognit // 1:1 port
	opStart := op.P
	opLength := len(*op.I)
	opEnd := opStart + opLength
	undoing := op.U != nil && *op.U

	alreadyMerged := false
	previousChange := (*Change)(nil)
	movedChanges := []*Change{}
	removeChanges := []*Change{}
	type newChangeEntry struct {
		op       Op
		metadata Metadata
	}
	newChanges := []newChangeEntry{}
	trackedDeletesAtOpPosition := []*Change{}
	for i := range rt.Changes {
		change := rt.Changes[i]
		changeStart := change.Op.P

		if change.Op.D != nil {
			if opStart < changeStart {
				// Shift any deletes after this along by the length of this insert
				change.Op.P += opLength
				movedChanges = append(movedChanges, change)
			} else if opStart == changeStart {
				if !alreadyMerged &&
					undoing &&
					len(*change.Op.D) >= opLength &&
					equalPrefix(*change.Op.D, *op.I) {
					// We are undoing: reject the tracked delete — cut the
					// re-inserted text out of it instead of appending.
					rest := (*change.Op.D)[opLength:]
					change.Op.D = &rest
					change.Op.P += opLength
					if rest == "" {
						removeChanges = append(removeChanges, change)
					} else {
						movedChanges = append(movedChanges, change)
					}
					alreadyMerged = true

					// Any tracked delete that came before this rejection was
					// moved after the incoming insert — move them back so they
					// appear before the tracked delete rejection.
					for _, trackedDelete := range trackedDeletesAtOpPosition {
						trackedDelete.Op.P -= opLength
					}
				} else {
					change.Op.P += opLength
					movedChanges = append(movedChanges, change)

					if !alreadyMerged {
						trackedDeletesAtOpPosition = append(trackedDeletesAtOpPosition, change)
					}
				}
			}
		} else if change.Op.I != nil {
			changeEnd := changeStart + len(*change.Op.I)
			isChangeOverlapping := opStart >= changeStart && opStart <= changeEnd

			// Only merge inserts if they are from the same user
			isSameUser := sameUser(metadata, change.Metadata)

			// If we are undoing and a delete op sits just after the existing
			// insert, this insert may only cancel that delete — not append.
			var nextChange *Change
			if i+1 < len(rt.Changes) {
				nextChange = rt.Changes[i+1]
			}
			isOpAdjacentToNextDelete := nextChange != nil &&
				nextChange.Op.D != nil &&
				opStart == changeEnd &&
				nextChange.Op.P == opStart
			willOpCancelNextDelete := undoing && isOpAdjacentToNextDelete &&
				equalPrefix(*nextChange.Op.D, *op.I)

			// A delete at the start of the existing insert is a partition:
			// don't merge across it.
			isInsertBlockedByDelete := previousChange != nil &&
				previousChange.Op.D != nil &&
				previousChange.Op.P == opEnd

			if rt.TrackChanges &&
				isChangeOverlapping &&
				!isInsertBlockedByDelete &&
				!alreadyMerged &&
				!willOpCancelNextDelete &&
				isSameUser {
				offset := opStart - changeStart
				old := *change.Op.I
				change.Op.I = ptrTo(old[:offset] + *op.I + old[offset:])
				change.Metadata["ts"] = pickTimestamp(change.Metadata, metadata)
				alreadyMerged = true
				movedChanges = append(movedChanges, change)
			} else if opStart <= changeStart {
				// Fully before the other insert — shift it along.
				change.Op.P += opLength
				movedChanges = append(movedChanges, change)
			} else if (!isSameUser || !rt.TrackChanges) && changeStart < opStart && opStart < changeEnd {
				// This user is inserting inside a change by another user:
				// split the existing change into one before and one after.
				offset := opStart - changeStart
				old := *change.Op.I
				beforeContent := old[:offset]
				afterContent := old[offset:]
				change.Op.I = &beforeContent
				movedChanges = append(movedChanges, change)

				afterMetadata := make(Metadata, len(change.Metadata))
				for k, v := range change.Metadata {
					afterMetadata[k] = v
				}
				newChanges = append(newChanges, newChangeEntry{
					op:       Op{I: ptrTo(afterContent), P: changeStart + offset + opLength},
					metadata: afterMetadata,
				})
			}
		}

		previousChange = change
	}

	if rt.TrackChanges && !alreadyMerged {
		rt.addOp(*op, metadata)
	}
	for _, nc := range newChanges {
		rt.addOp(nc.op, nc.metadata)
	}

	for _, change := range removeChanges {
		rt.removeChange(change)
	}

	for _, change := range movedChanges {
		rt.markAsDirty(change, "change", "moved")
	}
}

func equalPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return s[:len(prefix)] == prefix
}

// applyDeleteToChanges — 1:1 port. Delete-vs-insert cancellation is computed
// as opModifications and applied AFTER the sweep so offsets stay valid; the
// caller's op is NOT modified (Node copies it: op = {p, d: _applyOpModifications}).
func (rt *RangesTracker) applyDeleteToChanges(op *Op, metadata Metadata) error { //nolint:gocognit // 1:1 port
	opStart := op.P
	opLength := len(*op.D)
	opEnd := opStart + opLength
	removeChanges := []*Change{}
	movedChanges := []*Change{}
	opModifications := []opModRef{}

	for _, change := range rt.Changes {
		if change.Op.I != nil {
			changeStart := change.Op.P
			changeEnd := changeStart + len(*change.Op.I)
			if opEnd <= changeStart {
				// Shift ops after us back by our length
				change.Op.P -= opLength
				movedChanges = append(movedChanges, change)
			} else if opStart >= changeEnd {
				// Delete is after the insert, nothing to do
			} else {
				// Overlap: the two cancel out where they overlap.
				var deleteRemainingBefore, insertRemainingBefore string
				var deleteRemainingAfter, insertRemainingAfter string
				if opStart >= changeStart {
					deleteRemainingBefore = ""
					insertRemainingBefore = (*change.Op.I)[:opStart-changeStart]
				} else {
					deleteRemainingBefore = (*op.D)[:changeStart-opStart]
					insertRemainingBefore = ""
				}
				if opEnd <= changeEnd {
					deleteRemainingAfter = ""
					insertRemainingAfter = (*change.Op.I)[opEnd-changeStart:]
				} else {
					deleteRemainingAfter = (*op.D)[changeEnd-opStart:]
					insertRemainingAfter = ""
				}

				insertRemaining := insertRemainingBefore + insertRemainingAfter
				if len(insertRemaining) > 0 {
					change.Op.I = &insertRemaining
					change.Op.P = minInt(changeStart, opStart)
					movedChanges = append(movedChanges, change)
				} else {
					removeChanges = append(removeChanges, change)
				}

				// The middle chunk of OUR delete covered by the insert.
				deleteRemovedLength := opLength - len(deleteRemainingBefore) - len(deleteRemainingAfter)
				deleteRemovedStart := len(deleteRemainingBefore)
				d := (*op.D)[deleteRemovedStart : deleteRemovedStart+deleteRemovedLength]
				if len(d) > 0 {
					opModifications = append(opModifications, opModRef{d: &d, p: deleteRemovedStart})
				}
			}
		} else if change.Op.D != nil {
			changeStart := change.Op.P
			if opEnd < changeStart || (!rt.TrackChanges && opEnd == changeStart) {
				// Shift ops after us back. Tracking: strictly before (touching
				// merges below); not tracking: touching is fine.
				change.Op.P -= opLength
				movedChanges = append(movedChanges, change)
			} else if opStart <= changeStart && changeStart <= opEnd {
				if rt.TrackChanges {
					// Overlap a tracked delete: absorb its content into this
					// op (at the offset) and delete the existing change.
					opModifications = append(opModifications, opModRef{i: change.Op.D, p: changeStart - opStart})
					removeChanges = append(removeChanges, change)
				} else {
					change.Op.P = opStart
					movedChanges = append(movedChanges, change)
				}
			}
		}
	}

	// Copy rather than modify (Node: op = {p: op.p, d: ...}).
	// The ORIGINAL op still applies to comments.
	opD, err := applyOpModifications(*op.D, opModifications)
	if err != nil {
		return err
	}
	newOpP, newOpD := op.P, opD

	for _, change := range removeChanges {
		// Hack (Node): avoid removing one delete and replacing it with
		// another — it causes the UI to flicker.
		if newOpD != "" &&
			change.Op.D != nil &&
			newOpP <= change.Op.P && change.Op.P <= newOpP+len(newOpD) {
			change.Op.P = newOpP
			keptD := newOpD
			change.Op.D = &keptD // value copy (Node: change.op.d = op.d string copy)
			change.Metadata = metadata
			movedChanges = append(movedChanges, change)
			newOpD = "" // stop it being added
		} else {
			rt.removeChange(change)
		}
	}

	if rt.TrackChanges && newOpD != "" {
		addD := newOpD
		rt.addOp(Op{D: &addD, P: newOpP}, metadata)
	} else {
		// We deleted an insert between two other inserts — merge them again.
		moved, removed := rt.scanAndMergeAdjacentUpdates()
		movedChanges = append(movedChanges, moved...)
		for _, change := range removed {
			rt.removeChange(change)
			movedChanges = withoutChange(movedChanges, change)
		}
	}

	for _, change := range movedChanges {
		rt.markAsDirty(change, "change", "moved")
	}
	return nil
}

func withoutChange(changes []*Change, exclude *Change) []*Change {
	kept := changes[:0]
	for _, c := range changes {
		if c != exclude {
			kept = append(kept, c)
		}
	}
	return kept
}

// addOp — Node _addOp(op, metadata): clone op+metadata, fresh id, push,
// resort (offset ascending, deletes before inserts at equal offset, stable),
// mark added.
func (rt *RangesTracker) addOp(op Op, metadata Metadata) {
	meta := make(Metadata, len(metadata))
	for k, v := range metadata {
		meta[k] = v
	}
	change := &Change{ID: rt.NewId(), Op: op, Metadata: meta}
	rt.Changes = append(rt.Changes, change)
	rt.markAsDirty(change, "change", "added")
	sortChangesStable(rt.Changes)
}

// removeChange — Node _removeChange(change): filter out by identity, mark removed.
func (rt *RangesTracker) removeChange(change *Change) {
	rt.Changes = withoutChange(rt.Changes, change)
	rt.markAsDirty(change, "change", "removed")
}

// scanAndMergeAdjacentUpdates — Node _scanAndMergeAdjacentUpdates: re-merge
// same-user adjacent inserts and same-position deletes after a middle insert
// was deleted. Quirk pinned (Node): when both are inserts but NOT merged,
// `previousChange` is NOT advanced; after a delete merge it is NOT advanced
// either.
func (rt *RangesTracker) scanAndMergeAdjacentUpdates() (moved []*Change, removed []*Change) {
	previous := (*Change)(nil)
	for _, change := range rt.Changes {
		if previous != nil && previous.Op.I != nil && change.Op.I != nil {
			previousEnd := previous.Op.P + len(*previous.Op.I)
			if previousEnd == change.Op.P && sameUser(previous.Metadata, change.Metadata) {
				merged := *previous.Op.I + *change.Op.I
				previous.Op.I = &merged
				previous.Metadata["ts"] = pickTimestamp(previous.Metadata, change.Metadata)
				removed = append(removed, change)
				moved = append(moved, previous)
			}
		} else if previous != nil &&
			previous.Op.D != nil && change.Op.D != nil &&
			previous.Op.P == change.Op.P {
			// Merge adjacent deletes
			mergedD := *previous.Op.D + *change.Op.D
			previous.Op.D = &mergedD
			removed = append(removed, change)
			moved = append(moved, previous)
		} else {
			previous = change
		}
	}
	return moved, removed
}
