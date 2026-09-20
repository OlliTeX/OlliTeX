package ot

// Port of file_data/comment_list.js.
//
// Iteration order matters (JSON output), so the list preserves the JS Map
// insertion order: `order` holds the IDs in their first-insertion
// sequence, updated on Add.

// CommentList is an ordered map of comment id -> Comment.
type CommentList struct {
	m     map[string]*Comment
	order []string
}

// NewCommentList mirrors the CommentList constructor.
func NewCommentList(comments []*Comment) *CommentList {
	l := &CommentList{m: map[string]*Comment{}, order: []string{}}
	for _, c := range comments {
		l.add(c)
	}
	return l
}

func (l *CommentList) add(c *Comment) {
	if _, ok := l.m[c.ID()]; !ok {
		l.order = append(l.order, c.ID())
	}
	l.m[c.ID()] = c
}

// Add mirrors comments.set(id, comment) (Map.set: updates value, first-insert
// position preserved).
func (l *CommentList) Add(c *Comment) { l.add(c) }

// GetComment mirrors comments.get(id).
func (l *CommentList) GetComment(id string) *Comment { return l.m[id] }

// Delete mirrors comments.delete(id). Reports whether a value was removed.
func (l *CommentList) Delete(id string) bool {
	if _, ok := l.m[id]; !ok {
		return false
	}
	delete(l.m, id)
	for i, k := range l.order {
		if k == id {
			l.order = append(l.order[:i], l.order[i+1:]...)
			break
		}
	}
	return true
}

// Length mirrors get length().
func (l *CommentList) Length() int { return len(l.m) }

// ToArray mirrors toArray(): insertion order.
func (l *CommentList) ToArray() []*Comment {
	out := make([]*Comment, 0, len(l.m))
	for _, id := range l.order {
		out = append(out, l.m[id])
	}
	return out
}

// ToRaw mirrors toRaw(): insertion order.
func (l *CommentList) ToRaw() []any {
	out := make([]any, len(l.m))
	for i, id := range l.order {
		out[i] = l.m[id].ToRaw()
	}
	return out
}

// ApplyInsert mirrors applyInsert(range, {commentIds}).
func (l *CommentList) ApplyInsert(rng *Range, commentIDs []string) {
	cursor := rng.Pos
	for _, id := range l.order {
		c := l.m[id]
		l.m[id] = c.ApplyInsert(cursor, rng.Length, containsInStrings(commentIDs, id))
	}
}

// ApplyDelete mirrors applyDelete(range).
func (l *CommentList) ApplyDelete(rng *Range) {
	for _, id := range l.order {
		l.m[id] = l.m[id].ApplyDelete(rng)
	}
}

// IdsCoveringRange mirrors idsCoveringRange(range): ids of comments with a
// range containing `range`, in map insertion order.
func (l *CommentList) IdsCoveringRange(rng *Range) []string {
	out := []string{}
	for _, id := range l.order {
		c := l.m[id]
		for _, r := range c.Ranges() {
			if r.Contains(rng) {
				out = append(out, id)
				break
			}
		}
	}
	return out
}

// CommentListFromRaw mirrors CommentList.fromRaw.
func CommentListFromRaw(id string, rawRanges []map[string]any, rawResolved bool) (*CommentList, error) {
	c, err := CommentFromRaw(id, rawRanges, rawResolved)
	if err != nil {
		return nil, err
	}
	return NewCommentList([]*Comment{c}), nil
}

func containsInStrings(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
