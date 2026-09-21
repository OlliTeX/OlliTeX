package otc

// Author mirrors libraries/overleaf-editor-core/lib/author.js.
//
// An author of a Change. We want to store user IDs, and then fill in the other
// properties (which the user can change over time) when changes are loaded. At
// present we assume that all authors have a user ID.
type Author struct {
	ID    int64
	Email string
	Name  string
}

// NewAuthor mirrors the Author constructor with its check-types assertions:
// id must be a number, email and name must be strings.
func NewAuthor(id int64, email, name string) *Author {
	_ = email // string-typed in Go; the Node assert.string guards are enforced by the type system
	_ = name
	return &Author{ID: id, Email: email, Name: name}
}

// AuthorFromRaw mirrors Author.fromRaw: nullish raw becomes a nil Author,
// otherwise the three fields are read.
func AuthorFromRaw(raw *Author) *Author {
	if raw == nil {
		return nil
	}
	return NewAuthor(raw.ID, raw.Email, raw.Name)
}

// ToRaw mirrors Author.toRaw.
func (a *Author) ToRaw() map[string]any {
	return map[string]any{
		"id":    a.ID,
		"email": a.Email,
		"name":  a.Name,
	}
}

// GetID mirrors Author#getId.
func (a *Author) GetID() int64 { return a.ID }

// GetEmail mirrors Author#getEmail.
func (a *Author) GetEmail() string { return a.Email }

// GetName mirrors Author#getName.
func (a *Author) GetName() string { return a.Name }
