package core

import (
	"encoding/json"
)

// NewTextEditOp — constructor wrapping a typed *TextOp as a text EditOp (Node
// EditOperationBuilder text branch / TextOperation constructor). The persist
// and set-content paths build text edits in Go (no JSON round-trip).
func NewTextEditOp(to *TextOp) *EditOp {
	return &EditOp{kind: "text", textOp: to}
}

// EditOp (ports EditOperationBuilder.fromJSON / EditOperation dispatch) —
// the edit-level operation sum, discovered by which JSON key is present:
//
//	"textOperation"            -> text (TextOp)
//	"commentId" + "ranges"[]   -> addComment
//	"deleteComment"            -> deleteComment
//	"commentId" + "resolved"   -> setCommentState
//	"noOp"                     -> noOp
//
// Node precedence (first match wins): textOperation, addComment, deleteComment,
// setCommentState, noOp. A raw carrying none of those (e.g. bare commentId)
// errors "Unsupported operation in EditOperationBuilder.fromJSON".
type EditOp struct {
	kind      string // "text" | "addComment" | "deleteComment" | "setCommentState" | "noOp"
	textOp    *TextOp
	commentID string
	ranges    []Range
	resolved  bool
}

// EditOpFromRaw ports EditOperationBuilder.fromJSON.
func EditOpFromRaw(raw json.RawMessage) (*EditOp, error) {
	var probe struct {
		HasText     bool              `json:"-"`
		CommentID   string            `json:"commentId"`
		HasRanges   bool              `json:"-"`
		RawRanges   []json.RawMessage `json:"ranges"`
		DeleteCmt   string            `json:"deleteComment"`
		ResolvedPtr *bool             `json:"resolved"`
		NoOp        bool              `json:"noOp"`
	}
	// Detect presence of "textOperation" and "ranges" via a nested probe.
	var presence struct {
		TextOp json.RawMessage   `json:"textOperation"`
		Ranges []json.RawMessage `json:"ranges"`
	}
	if err := json.Unmarshal(raw, &presence); err != nil {
		return nil, &BadRawError{Msg: "bad edit op raw: " + err.Error()}
	}
	probe.HasText = len(presence.TextOp) != 0
	probe.RawRanges = presence.Ranges
	probe.HasRanges = len(presence.Ranges) != 0
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, &BadRawError{Msg: "bad edit op raw: " + err.Error()}
	}

	e := &EditOp{}
	switch {
	case probe.HasText:
		op, err := TextOpFromRaw(raw)
		if err != nil {
			return nil, err
		}
		e.kind = "text"
		e.textOp = op
	case probe.CommentID != "" && probe.HasRanges:
		// addComment: has "commentId" and a "ranges" array.
		e.kind = "addComment"
		e.commentID = probe.CommentID
		e.ranges = make([]Range, 0, len(probe.RawRanges))
		for i := range probe.RawRanges {
			r, err := RangeFromRaw(probe.RawRanges[i])
			if err != nil {
				return nil, err
			}
			e.ranges = append(e.ranges, r)
		}
		if probe.ResolvedPtr != nil {
			e.resolved = *probe.ResolvedPtr
		}
	case probe.DeleteCmt != "":
		e.kind = "deleteComment"
		e.commentID = probe.DeleteCmt
	case probe.CommentID != "" && probe.ResolvedPtr != nil:
		e.kind = "setCommentState"
		e.commentID = probe.CommentID
		e.resolved = *probe.ResolvedPtr
	case probe.NoOp:
		e.kind = "noOp"
	default:
		return nil, &BadRawError{Msg: "Unsupported operation in EditOperationBuilder.fromJSON"}
	}
	return e, nil
}

// ApplyFile ports EditOperation.apply(fileData) for a File (string kind).
// Node applies to StringFileData (content+comments); comment ops only touch
// comments, text ops touch content+comments+trackedChanges.
func (e *EditOp) ApplyFile(file *File) error {
	switch e.kind {
	case "text":
		return file.Edit(e.textOp)
	case "addComment":
		if file.Kind != "string" {
			return nil
		}
		file.Comments = file.Comments.AddComment(e.commentID, e.ranges, e.resolved)
		return nil
	case "deleteComment":
		if file.Kind != "string" {
			return nil
		}
		file.Comments = file.Comments.DeleteComment(e.commentID)
		return nil
	case "setCommentState":
		if file.Kind != "string" {
			return nil
		}
		file.Comments = file.Comments.SetCommentState(e.commentID, e.resolved)
		return nil
	case "noOp":
		return nil
	default:
		return &BadRawError{Msg: "unknown EditOp kind " + e.kind}
	}
}

// ApplyToLength ports EditOperation.applyToLength (Node base impl returns
// length unchanged for non-text ops).
func (e *EditOp) ApplyToLength(length int) (int, error) {
	if e.kind == "text" {
		return e.textOp.ApplyToLength(length)
	}
	return length, nil
}

// Kind returns the EditOp kind (text, addComment, deleteComment,
// setCommentState, noOp).
func (e *EditOp) Kind() string { return e.kind }

// TextOpForEdit — the underlying *TextOp when kind == "text", else nil.
func (e *EditOp) TextOpForEdit() *TextOp {
	if e.kind != "text" {
		return nil
	}
	return e.textOp
}

// ToRaw ports EditOperation.toJSON (wire key by kind).
func (e *EditOp) ToRaw() json.RawMessage {
	switch e.kind {
	case "text":
		return e.textOp.ToRaw()
	case "addComment":
		ranges := make([]json.RawMessage, 0, len(e.ranges))
		for _, r := range e.ranges {
			ranges = append(ranges, r.ToRaw())
		}
		b, _ := json.Marshal(struct {
			CommentID string            `json:"commentId"`
			Ranges    []json.RawMessage `json:"ranges"`
			Resolved  bool              `json:"resolved,omitempty"`
		}{e.commentID, ranges, e.resolved})
		return b
	case "deleteComment":
		b, _ := json.Marshal(struct {
			Delete string `json:"deleteComment"`
		}{e.commentID})
		return b
	case "setCommentState":
		b, _ := json.Marshal(struct {
			Resolved  bool   `json:"resolved"`
			CommentID string `json:"commentId"`
		}{e.resolved, e.commentID})
		return b
	default: // noOp
		return []byte(`{"noOp":true}`)
	}
}
