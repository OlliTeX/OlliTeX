package otc

// diff_as_text_operation.go — 1:1 port of `lib/diff_as_text_operation.js`.
// Diffs a file's content (with tracked deletes filtered) against `after` and
// returns a minimal TextOperation turning the file into it, preserving the
// file's tracked deletes. The DMP diff comes from the go-diff dependency
// (github.com/sergi/go-diff), pinned to reproduce overleaf's diff-match-patch@1.0.5
// outputs (see dmp_test.go).

import (
	"errors"
	"time"

	diffmatchpatch "github.com/sergi/go-diff/diffmatchpatch"
	oerror "ollitex/go/libraries/oerror"
)

// Diff operation constants, mirroring Node's ADDED/REMOVED/UNCHANGED.
const (
	ADDED     = 1
	REMOVED   = -1
	UNCHANGED = 0
)

// otcDMP is the shared diff-match-patch instance (Node: `const dmp = new DMP()`
// with `dmp.Diff_Timeout = 0.1`).
var otcDMP = func() *diffmatchpatch.DiffMatchPatch {
	d := diffmatchpatch.New()
	d.DiffTimeout = 100 * time.Millisecond
	return d
}()

// DiffTracking carries the tracking identity to record an edit as (Node opts.tracking).
type DiffTracking struct {
	UserID string
	TS     time.Time
}

// DiffOpts are the options for DiffAsTextOperation / DiffsToTextOperation.
type DiffOpts struct {
	// Tracking, when set, records the edit as tracked changes.
	Tracking *DiffTracking
}

// DiffAsTextOperation — mirroring Node `diffAsTextOperation(file, after, opts)`.
// Diffs the file's content (tracked deletes filtered) against `after` and returns
// a minimal TextOperation turning the file into it. Returns an error for
// unreachable diff types or out-of-sync tracked changes (Node throws).
func DiffAsTextOperation(file *StringFileData, after string, opts DiffOpts) (*TextOperation, error) {
	before := file.GetContent(true) // filterTrackedDeletes: the content external sources see
	beforeStr := ""
	if before != nil {
		beforeStr = *before
	}
	diffs := otcDMP.DiffMain(beforeStr, after, true)
	diffs = otcDMP.DiffCleanupSemantic(diffs)
	return DiffsToTextOperation(file, diffs, opts)
}

// DiffsToTextOperation — mirroring Node `diffsToTextOperation(file, diffs, opts)`.
// Converts DMP diffs (computed against the file content with tracked deletes
// filtered out) into a TextOperation applying to the full content: positions step
// over tracked-delete spans with plain retains, preserving them. When
// opts.Tracking is set, insertions become tracked inserts and removals become
// tracked deletes — except inside existing tracked inserts (regular deletes) and
// over existing tracked deletes (left untouched, keeping the original author).
//
// Exported for tests; use DiffAsTextOperation instead.
func DiffsToTextOperation(file *StringFileData, diffs []diffmatchpatch.Diff, opts DiffOpts) (*TextOperation, error) {
	var insertTracking, deleteTracking TrackingDirective
	if opts.Tracking != nil {
		insertTracking = TrackingProps{Type: "insert", UserID: opts.Tracking.UserID, TS: opts.Tracking.TS}
		deleteTracking = TrackingProps{Type: "delete", UserID: opts.Tracking.UserID, TS: opts.Tracking.TS}
	}

	trackedChanges := file.GetTrackedChanges().AsSorted()
	tcIndex := 0

	op := NewTextOperation()

	// removeContent consumes removed content: a plain remove, or a tracked delete
	// when tracking is being recorded.
	removeContent := func(length int) error {
		if deleteTracking != nil {
			return op.Retain(length, RetainBuilderOpts{Tracking: deleteTracking})
		}
		return op.Remove(length)
	}
	// tcType returns the tracked change's type ('insert'/'delete') or '' for a
	// non-TrackingProps directive (Node `tc.tracking.type` undefined).
	tcType := func(tc TrackedChange) string {
		if tp, ok := tc.AsTrackingProps(); ok {
			return tp.Type
		}
		return ""
	}

	for _, diff := range diffs {
		typ := int(diff.Type)
		content := diff.Text
		if typ == ADDED {
			opts2 := InsertBuilderOpts{}
			if insertTracking != nil {
				opts2.Tracking = insertTracking
			}
			if err := op.Insert(content, opts2); err != nil {
				return nil, err
			}
			continue
		}
		if typ != REMOVED && typ != UNCHANGED {
			return nil, errors.New("Unknown type")
		}

	tcLoop:
		for tcIndex < len(trackedChanges) {
			tc := trackedChanges[tcIndex]
			segmentEnd := op.BaseLength + len(content)
			if tc.Range.Start() >= segmentEnd {
				break tcLoop
			}
			switch {
			case tcType(tc) == "delete":
				// Tracked deletes are invisible in the diffed content. Step over
				// them with a plain retain, preserving the original tracked delete.
				before := tc.Range.Start() - op.BaseLength
				if typ == REMOVED {
					if err := removeContent(before); err != nil {
						return nil, err
					}
				} else if err := op.Retain(before, RetainBuilderOpts{}); err != nil {
					return nil, err
				}
				if err := op.Retain(tc.Range.Length, RetainBuilderOpts{}); err != nil {
					return nil, err
				}
				if before > 0 {
					content = content[before:]
				}
				tcIndex++
			case typ == REMOVED && deleteTracking != nil:
				// Removals inside existing tracked inserts are always regular
				// deletes, even when recording tracked changes.
				before := tc.Range.Start() - op.BaseLength
				if before < 0 {
					before = 0
				}
				if err := removeContent(before); err != nil {
					return nil, err
				}
				overlap := min(tc.Range.End(), segmentEnd) - op.BaseLength
				if overlap < 0 {
					overlap = 0
				}
				if err := op.Remove(overlap); err != nil {
					return nil, err
				}
				if off := before + overlap; off > 0 {
					content = content[off:]
				}
				if tc.Range.End() <= op.BaseLength {
					tcIndex++
				} else {
					// The tracked insert extends beyond this diff segment.
					break tcLoop
				}
			default:
				// Tracked inserts are ordinary visible content otherwise. Only
				// move past them once the whole range has been covered by diff
				// segments.
				if tc.Range.End() <= segmentEnd {
					tcIndex++
				} else {
					break tcLoop
				}
			}
		}

		if typ == REMOVED {
			if err := removeContent(len(content)); err != nil {
				return nil, err
			}
		} else if err := op.Retain(len(content), RetainBuilderOpts{}); err != nil {
			return nil, err
		}
	}

	// Any tracked deletes after the end of the diffed content must be retained.
	for tcIndex < len(trackedChanges) {
		tc := trackedChanges[tcIndex]
		if !(tcType(tc) == "delete" && tc.Range.Start() == op.BaseLength) {
			return nil, oerror.New(
				"StringFileData.trackedChanges out of sync: unexpected range after end of diff",
				map[string]any{"nextTc": rawJSONStr(tc.ToRaw()), "baseLength": op.BaseLength},
			)
		}
		if err := op.Retain(tc.Range.Length, RetainBuilderOpts{}); err != nil {
			return nil, err
		}
		tcIndex++
	}

	return op, nil
}
