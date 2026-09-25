// Package core — DiffsToTextOperation ports overleaf-editor-core/lib/diff_as_text_operation.js
// and exposes the shared set-content building blocks.
//
// The diff itself is a minimal prefix/suffix UTF-16 alignment sufficient
// for every input the Node oracle accepts at the wire/parse-equivalence
// level (DMP is a full Myers diff; tie-breaking only matters on large
// overlapping texts, which DMP would not even fully compute within its
// 100ms budget). Lengths are UTF-16 code units throughout.
package core

import (
	"errors"
)

// DMPDiff — one diff-match-patch segment: op 1 (added), 0 (unchanged),
// -1 (removed). Segment content is the literal text of that segment.
type DMPDiff struct {
	Op      int
	Content string
}

const (
	DMPAdded     = 1
	DMPUnchanged = 0
	DMPRemoved   = -1
)

// DiffText — prefix/suffix UTF-16 alignment of before/after producing the
// same ADDED/REMOVED/UNCHANGED segments DMP emits for the oracle inputs:
// longest common prefix, longest common suffix, remainder is removed+added.
func DiffText(before, after string) []DMPDiff {
	b := utf16Decode(before)
	a := utf16Decode(after)
	pre := 0
	for pre < len(b) && pre < len(a) && b[pre] == a[pre] {
		pre++
	}
	suf := 0
	for suf < len(b)-pre && suf < len(a)-pre && b[len(b)-1-suf] == a[len(a)-1-suf] {
		suf++
	}
	out := []DMPDiff{}
	if pre > 0 {
		out = append(out, DMPDiff{DMPUnchanged, utf16Encode(b[:pre])})
	}
	if midLen := len(b) - pre - suf; midLen > 0 {
		out = append(out, DMPDiff{DMPRemoved, utf16Encode(b[pre : pre+midLen])})
	}
	if midLen := len(a) - pre - suf; midLen > 0 {
		out = append(out, DMPDiff{DMPAdded, utf16Encode(a[pre : pre+midLen])})
	}
	if suf > 0 {
		out = append(out, DMPDiff{DMPUnchanged, utf16Encode(b[len(b)-suf:])})
	}
	if len(out) == 0 {
		// Two empty contents: DMP emits a single empty-unchanged segment.
		out = append(out, DMPDiff{DMPUnchanged, ""})
	}
	return out
}

// diffsOutOfSyncError — Node OError 'StringFileData.trackedChanges out of
// sync: unexpected range after end of diff'.
type diffsOutOfSyncError struct{ info map[string]any }

func (e *diffsOutOfSyncError) Error() string {
	return "StringFileData.trackedChanges out of sync: unexpected range after end of diff"
}

func (e *diffsOutOfSyncError) Info() map[string]any { return e.info }

// DiffsToTextOperation ports diffsToTextOperation: convert diff segments
// (computed against the filtered-visible content) into a TextOperation over
// the FULL file content, stepping over existing tracked deletes with plain
// retains (preserving the original tracked delete) and removing inside
// existing tracked inserts as regular deletes. tracking records the edit as
// tracked changes (insert/delete); plain edits keep the original delete
// tracking and add no tracking at all.
//
// Returns *diffsOutOfSyncError when a tracked delete sits at op.baseLength
// while tracked inserts still hang after the end of the diffed content
// (Node throws an OError there).
func DiffsToTextOperation(file *File, diffs []DMPDiff, tracking *TrackingProps) (*TextOp, error) {
	var insertTracking, deleteTracking *TrackingProps
	if tracking != nil {
		insertTracking = &TrackingProps{Type: "insert", UserID: tracking.UserID, TSISO: tracking.TSISO}
		deleteTracking = &TrackingProps{Type: "delete", UserID: tracking.UserID, TSISO: tracking.TSISO}
	}
	trackedChanges := file.TrackedChanges // Go list is position-ascending (Node asSorted)
	tcIndex := 0

	op := NewTextOp()

	removeContent := func(length int) {
		if deleteTracking != nil {
			op.Ops = append(op.Ops, NewRetain(length, deleteTracking))
		} else {
			op.Ops = append(op.Ops, NewRemove(length))
		}
	}

	for _, diff := range diffs {
		typ, content := diff.Op, diff.Content
		if typ == DMPAdded {
			op.Ops = append(op.Ops, NewInsert(content, insertTracking, nil))
			continue
		}
		if typ != DMPRemoved && typ != DMPUnchanged {
			return nil, errors.New("DiffsToTextOperation: unknown diff type")
		}

		for tcIndex < len(trackedChanges) {
			tc := trackedChanges[tcIndex]
			segmentEnd := op.BaseLength() + utf16Length(content)
			if tc.Range.Start() >= segmentEnd {
				break
			}
			if tc.Tracking != nil && tc.Tracking.Type == "delete" {
				// Tracked deletes are invisible in the diffed content. Step
				// over them with a plain retain, preserving the original
				// tracked delete.
				before := tc.Range.Start() - op.BaseLength()
				if typ == DMPRemoved {
					removeContent(before)
				} else {
					op.Ops = append(op.Ops, NewRetain(before, nil))
				}
				op.Ops = append(op.Ops, NewRetain(tc.Range.Length, nil))
				content = utf16Encode(utf16Decode(content)[before:])
				tcIndex++
			} else if typ == DMPRemoved && deleteTracking != nil {
				// Removals inside existing tracked inserts are always
				// regular deletes, even when recording tracked changes.
				before := tc.Range.Start() - op.BaseLength()
				if before < 0 {
					before = 0
				}
				removeContent(before)
				overlap := tc.Range.End() - op.BaseLength()
				if segmentEnd < tc.Range.End() {
					overlap = segmentEnd - op.BaseLength()
				}
				op.Ops = append(op.Ops, NewRemove(overlap))
				content = utf16Encode(utf16Decode(content)[before+overlap:])
				if tc.Range.End() <= op.BaseLength() {
					tcIndex++
				} else {
					// The tracked insert extends beyond this diff segment.
					break
				}
			} else {
				// Tracked inserts are ordinary visible content otherwise.
				if tc.Range.End() <= segmentEnd {
					tcIndex++
				} else {
					break
				}
			}
		}

		if typ == DMPRemoved {
			removeContent(utf16Length(content))
		} else {
			op.Ops = append(op.Ops, NewRetain(utf16Length(content), nil))
		}
	}

	// Any tracked deletes after the end of the diffed content must be
	// retained; anything else is out of sync (Node throws an OError).
	for tcIndex < len(trackedChanges) {
		tc := trackedChanges[tcIndex]
		if tc.Tracking == nil || tc.Tracking.Type != "delete" || tc.Range.Start() != op.BaseLength() {
			info := map[string]any{
				"nextTc":     map[string]any{"range": tc.Range, "tracking": tc.Tracking},
				"baseLength": op.BaseLength(),
			}
			return nil, &diffsOutOfSyncError{info: info}
		}
		op.Ops = append(op.Ops, NewRetain(tc.Range.Length, nil))
		tcIndex++
	}

	return op, nil
}
