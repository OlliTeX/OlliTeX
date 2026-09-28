package opmodel

// CommentList ports vendor lib/file_data/comment_list.js.
//
// An id -> Comment map that preserves insertion order (vendor Map), plus
// the scan operations the op layer needs. applyInsert/applyDelete rewrite
// the list (fresh Comment values per call), mirroring vendor `const
// commentAfterInsert = comment.applyInsert(...); this.comments.set(...)`.

// CommentList — vendor `class CommentList`.
type CommentList struct {
	comments map[string]*Comment
	order    []string
}

// NewCommentList — vendor constructor (comments list, order preserved).
func NewCommentList(comments []*Comment) *CommentList {
	cl := &CommentList{comments: map[string]*Comment{}}
	for _, c := range comments {
		cl.Add(c)
	}
	return cl
}

// CommentListFromRaw — vendor `CommentList.fromRaw(rawComments)`.
func CommentListFromRaw(raw []any) (*CommentList, error) {
	cl := &CommentList{comments: map[string]*Comment{}}
	for _, rc := range raw {
		m, ok := rc.(map[string]any)
		if !ok {
			return nil, NewUnprocessableError("Invalid comment raw data", nil)
		}
		c, err := CommentFromRaw(m)
		if err != nil {
			return nil, err
		}
		cl.Add(c)
	}
	return cl, nil
}

// Add — vendor `add(newComment)`.
func (cl *CommentList) Add(c *Comment) {
	if cl.comments == nil {
		cl.comments = map[string]*Comment{}
	}
	if _, exists := cl.comments[c.ID]; !exists {
		cl.order = append(cl.order, c.ID)
	}
	cl.comments[c.ID] = c
}

// Delete — vendor `delete(id)`: removes the comment, reports presence.
func (cl *CommentList) Delete(id string) bool {
	if cl.comments != nil {
		if _, exists := cl.comments[id]; !exists {
			return false
		}
	} else {
		return false
	}
	delete(cl.comments, id)
	reordered := make([]string, 0, len(cl.order)-1)
	for _, oid := range cl.order {
		if oid != id {
			reordered = append(reordered, oid)
		}
	}
	cl.order = reordered
	return true
}

// GetComment — vendor `getComment(id)`: nil when absent.
func (cl *CommentList) GetComment(id string) *Comment { return cl.comments[id] }

// Len — vendor `get length()`.
func (cl *CommentList) Len() int {
	if cl.comments == nil {
		return 0
	}
	return len(cl.comments)
}

// Array — vendor `toArray()`: the comments in insertion order.
func (cl *CommentList) Array() []*Comment {
	out := make([]*Comment, 0, cl.Len())
	for _, id := range cl.order {
		out = append(out, cl.comments[id])
	}
	return out
}

// ToRaw — vendor `toRaw()`.
func (cl *CommentList) ToRaw() []map[string]any {
	out := make([]map[string]any, 0, cl.Len())
	for _, id := range cl.order {
		out = append(out, cl.comments[id].ToRaw())
	}
	return out
}

// ApplyInsert — vendor `applyInsert(range, {commentIds})`: shift/extend
// every comment against an insert at range.Pos of range.Length units.
func (cl *CommentList) ApplyInsert(rng Range, commentIDs []string) {
	for _, id := range cl.order {
		c := cl.comments[id]
		nc := c.ApplyInsert(rng.Pos, rng.Length, coversID(commentIDs, id))
		cl.comments[id] = &nc
	}
}

// ApplyDelete — vendor `applyDelete(range)`.
func (cl *CommentList) ApplyDelete(rng Range) {
	for _, id := range cl.order {
		c := cl.comments[id]
		nc := c.ApplyDelete(rng)
		cl.comments[id] = &nc
	}
}

// IDsCoveringRange — vendor `idsCoveringRange(range)`: the ids of the
// comments whose ranges fully contain `range`, in insertion order.
func (cl *CommentList) IDsCoveringRange(rng Range) []string {
	ids := []string{}
	for _, id := range cl.order {
		c := cl.comments[id]
		for _, cr := range c.Ranges {
			if cr.Contains(rng) {
				ids = append(ids, id)
				break
			}
		}
	}
	return ids
}
