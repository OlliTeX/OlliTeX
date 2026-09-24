package core

import (
	"encoding/json"
	"os"
	"strings"
)

// NavSiteAdmin — the layout-react navbar's admin-driven flags for THIS
// request: Node's canDisplayAdminMenu = hasAdminAccess(sessionUser) and
// canDisplayProjectUrlLookup = adminPrivilegeAvailable && that &&
// hasAdminCapability('view-project-setting', false); hasAdminAccess =
// Settings.adminPrivilegeAvailable && session.passport.user.isAdmin.
// In this stack (no adminCapabilities configured → the capability check
// assumes all for admins) both collapse to the same boolean, verified by
// the U9 gate (admin true / member false).
func NavSiteAdmin(s *Session) bool {
	if os.Getenv("ADMIN_PRIVILEGE_AVAILABLE") != "true" {
		return false
	}
	if s == nil {
		return false
	}
	raw, ok := s.GetRaw("passport")
	if !ok {
		return false
	}
	var pp struct {
		User struct {
			IsAdmin bool `json:"isAdmin"`
		} `json:"user"`
	}
	if json.Unmarshal(raw, &pp) != nil {
		return false
	}
	return pp.User.IsAdmin
}

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
