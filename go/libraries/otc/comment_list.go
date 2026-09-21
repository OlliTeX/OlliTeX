package otc

import "fmt"

// CommentList mirrors file_data/comment_list.js: an ordered id→Comment map.
type CommentList struct {
	order    []string
	comments map[string]*Comment
}

// NewCommentList builds a list from comments (Node: constructor).
func NewCommentList(comments []*Comment) *CommentList {
	l := &CommentList{comments: map[string]*Comment{}}
	for _, c := range comments {
		l.Add(c)
	}
	return l
}

// FromRawCommentList builds a list from raw (Node: `CommentList.fromRaw`).
func FromRawCommentList(raw []map[string]any) (*CommentList, error) {
	list := &CommentList{comments: map[string]*Comment{}}
	for _, r := range raw {
		c, err := FromRawComment(r)
		if err != nil {
			return nil, err
		}
		list.Add(c)
	}
	return list, nil
}

// FromRawCommentListAny builds a list from a raw slice typed []any.
func FromRawCommentListAny(raw any) (*CommentList, error) {
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("invalid comment list")
	}
	items := make([]map[string]any, 0, len(arr))
	for _, v := range arr {
		m, okM := v.(map[string]any)
		if !okM {
			return nil, fmt.Errorf("invalid comment")
		}
		items = append(items, m)
	}
	return FromRawCommentList(items)
}

// ToArray returns the comments in insertion order (Node: `toArray`).
func (l *CommentList) ToArray() []*Comment {
	out := make([]*Comment, 0, len(l.order))
	for _, id := range l.order {
		out = append(out, l.comments[id])
	}
	return out
}

// Len returns the number of comments (Node: `get length()`).
func (l *CommentList) Len() int { return len(l.comments) }

// ToRaw serialises the list (Node: `toRaw`).
func (l *CommentList) ToRaw() []map[string]any {
	out := make([]map[string]any, 0, len(l.order))
	for _, id := range l.order {
		out = append(out, l.comments[id].ToRaw())
	}
	return out
}

// GetComment returns the comment with id, or nil (Node: `getComment`).
func (l *CommentList) GetComment(id string) *Comment {
	return l.comments[id]
}

// Add inserts a comment (Node: `add`).
func (l *CommentList) Add(c *Comment) {
	if _, present := l.comments[c.ID]; !present {
		l.order = append(l.order, c.ID)
	}
	l.comments[c.ID] = c
}

// Delete removes a comment, returning whether it was present (Node: `delete`).
func (l *CommentList) Delete(id string) bool {
	if _, present := l.comments[id]; !present {
		return false
	}
	delete(l.comments, id)
	for i, o := range l.order {
		if o == id {
			l.order = append(l.order[:i], l.order[i+1:]...)
			break
		}
	}
	return true
}

// IDs returns the comment ids in insertion order (inspection helper).
func (l *CommentList) IDs() []string {
	out := make([]string, len(l.order))
	copy(out, l.order)
	return out
}

// ApplyInsert applies an insert to every comment (Node: `applyInsert`).
func (l *CommentList) ApplyInsert(r Range, commentIDs []string) {
	for _, id := range l.order {
		extend := false
		for _, cid := range commentIDs {
			if cid == id {
				extend = true
			}
		}
		l.comments[id] = l.comments[id].ApplyInsert(r.Pos, r.Length, extend)
	}
}

// ApplyDelete applies a delete to every comment (Node: `applyDelete`).
func (l *CommentList) ApplyDelete(r Range) {
	for _, id := range l.order {
		l.comments[id] = l.comments[id].ApplyDelete(r)
	}
}

// IDsCoveringRange returns ids of comments covering range (Node: `idsCoveringRange`).
func (l *CommentList) IDsCoveringRange(r Range) []string {
	out := []string{}
	for _, id := range l.order {
		c := l.comments[id]
		for _, cr := range c.Ranges {
			if cr.Contains(r) {
				out = append(out, id)
				break
			}
		}
	}
	return out
}
