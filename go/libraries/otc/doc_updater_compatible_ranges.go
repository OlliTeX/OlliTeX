package otc

import (
	"errors"
)

// doc_updater_compatible_ranges.go — 1:1 port of
// `lib/doc_updater_compatible_ranges.js`. Translates a file's tracked changes
// and comments into the "doc-updater compatible" representation: positions are
// RELATIVE to a document where tracked deletes have been removed, so a comment
// overlapping a tracked deletion is truncated.

// DocUpdaterChangeOp is one tracked-change op (`{p, i|d}`). Exactly one of
// Insert/Delete is non-nil (insert vs delete).
type DocUpdaterChangeOp struct {
	P      int
	Insert *string
	Delete *string
}

// DocUpdaterMetadata is the `{ts, user_id}` metadata.
type DocUpdaterMetadata struct {
	TS     string
	UserID string
}

// DocUpdaterChange is one entry of the `changes` array.
type DocUpdaterChange struct {
	Op       DocUpdaterChangeOp
	Metadata DocUpdaterMetadata
}

// DocUpdaterCommentOp is one comment op (`{p, c, t, resolved}`).
type DocUpdaterCommentOp struct {
	P        int
	C        string
	T        string
	Resolved bool
}

// DocUpdaterComment is one entry of the `comments` array.
type DocUpdaterComment struct {
	Op DocUpdaterCommentOp
	ID string
}

// DocUpdaterCompatibleRanges is the result of GetDocUpdaterCompatibleRanges.
type DocUpdaterCompatibleRanges struct {
	Changes  []DocUpdaterChange
	Comments []DocUpdaterComment
}

// trackingTypeOf extracts a TrackingProps' type, ok=false for a
// ClearTrackingProps directive (Node `trackedChange.tracking.type` undefined).
func trackingTypeOf(d TrackingDirective) (string, bool) {
	tp, ok := d.(TrackingProps)
	return tp.Type, ok
}

// GetDocUpdaterCompatibleRanges — mirroring Node `getDocUpdaterCompatibleRanges(file)`.
// Returns the empty shape for a non-editable (binary) file, or an error when an
// editable file has no readable content (Node throws `Unable to read file contents`).
func GetDocUpdaterCompatibleRanges(file *File) (DocUpdaterCompatibleRanges, error) {
	empty := DocUpdaterCompatibleRanges{Changes: []DocUpdaterChange{}, Comments: []DocUpdaterComment{}}
	editable := file.IsEditable()
	if editable == nil || !*editable {
		// A binary file has no tracked changes or comments.
		return empty, nil
	}
	content := file.GetContent(false)
	if content == nil {
		return empty, errors.New("Unable to read file contents")
	}
	full := *content

	trackedChanges := file.GetTrackedChanges().AsSorted()
	comments := file.GetComments().ToArray()

	changes := []DocUpdaterChange{}
	trackedDeletionOffset := 0
	for _, tc := range trackedChanges {
		isDel := false
		var tp TrackingProps
		if t, ok := tc.Tracking.(TrackingProps); ok {
			tp = t
			isDel = tp.Type == "delete"
		}
		changeContent := full[tc.Range.Start():tc.Range.End()]
		op := DocUpdaterChangeOp{P: tc.Range.Start() - trackedDeletionOffset}
		if isDel {
			d := changeContent
			op.Delete = &d
		} else {
			i := changeContent
			op.Insert = &i
		}
		meta := DocUpdaterMetadata{TS: toISOString(tp.TS), UserID: tp.UserID}
		changes = append(changes, DocUpdaterChange{Op: op, Metadata: meta})
		if isDel {
			trackedDeletionOffset += tc.Range.Length
		}
	}

	// Comments are shifted left by the length of any previous tracked deletions;
	// if they overlap a tracked deletion they are truncated.
	trackedDeletions := []TrackedChange{}
	for _, tc := range trackedChanges {
		if t, ok := tc.Tracking.(TrackingProps); ok && t.Type == "delete" {
			trackedDeletions = append(trackedDeletions, tc)
		}
	}

	outComments := []DocUpdaterComment{}
	for _, comment := range comments {
		trackedDeletionIndex := 0
		if len(comment.Ranges) == 0 {
			// Detached comment → zero-length comment at position 0.
			outComments = append(outComments, DocUpdaterComment{
				Op: DocUpdaterCommentOp{P: 0, C: "", T: comment.ID, Resolved: comment.Resolved},
				ID: comment.ID,
			})
			continue
		}
		// A multi-range comment is treated as one joining all its ranges.
		commentStart := comment.Ranges[0].Start()
		commentEnd := comment.Ranges[len(comment.Ranges)-1].End()

		var commentContent string
		position := commentStart
		for trackedDeletionIndex < len(trackedDeletions) && trackedDeletions[trackedDeletionIndex].Range.End() <= commentStart {
			position -= trackedDeletions[trackedDeletionIndex].Range.Length
			trackedDeletionIndex++
		}
		if trackedDeletionIndex < len(trackedDeletions) && trackedDeletions[trackedDeletionIndex].Range.Start() < commentStart {
			position -= commentStart - trackedDeletions[trackedDeletionIndex].Range.Start()
		}

		cursor := commentStart
		for cursor < commentEnd {
			if trackedDeletionIndex >= len(trackedDeletions) || trackedDeletions[trackedDeletionIndex].Range.Start() >= commentEnd {
				commentContent += full[cursor:commentEnd]
				break
			}
			td := trackedDeletions[trackedDeletionIndex]
			if td.Range.Start() > cursor {
				commentContent += full[cursor:td.Range.Start()]
			}
			if td.Range.End() <= commentEnd {
				cursor = td.Range.End()
				trackedDeletionIndex++
			} else {
				break
			}
		}
		outComments = append(outComments, DocUpdaterComment{
			Op: DocUpdaterCommentOp{P: position, C: commentContent, T: comment.ID, Resolved: comment.Resolved},
			ID: comment.ID,
		})
	}

	return DocUpdaterCompatibleRanges{Changes: changes, Comments: outComments}, nil
}
