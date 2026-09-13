package core

import (
	"encoding/json"
	"strings"
)

// PassportUser returns the logged-in session user's email + id hex (the
// page layout slots ol-usersEmail / ol-user_id). Anonymous → empty.
func PassportUser(s *Session) (email, id string) {
	if s == nil {
		return "", ""
	}
	raw, ok := s.GetRaw("passport")
	if !ok {
		return email, id
	}
	var pp struct {
		User struct {
			Email string `json:"email"`
			ID    string `json:"_id"`
		} `json:"user"`
	}
	if json.Unmarshal(raw, &pp) != nil {
		return "", ""
	}
	return pp.User.Email, pp.User.ID
}

// PageUserSlots builds the PageData user fields from the session.
func PageUserSlots(s *Session) (email, id string) {
	pe, uid := PassportUser(s)
	if pe == "" {
		if raw, ok := s.GetRaw("user"); ok {
			var u struct {
				Email string `json:"email"`
				ID    string `json:"_id"`
			}
			if json.Unmarshal(raw, &u) == nil {
				pe, uid = u.Email, u.ID
			}
		}
	}
	return strings.TrimSpace(pe), uid
}
