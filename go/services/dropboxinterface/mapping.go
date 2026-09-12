package dropboxinterface

import (
	"strings"
	"time"
)

// --- Dropbox error mapping (1:1 with _mapDropboxError) ----------------------

type dropboxErrorDetails struct {
	AccessToken *string                   `json:"access_token"`
	Path        *dropboxPathError         `json:"path"`
	RateLimit   *struct{ Summary string } `json:"rate_limit"`
	Conflict    *struct{ Summary string } `json:"conflict"`
	Folder      *struct{ Summary string } `json:"folder"`
	Summary     *string                   `json:"summary"`
}

type dropboxPathError struct {
	Tag     string  `json:".tag"`
	Summary *string `json:"summary"`
}

func mapDropboxError(status int, d *dropboxErrorDetails) (int, string, string) {
	code, msg := 500, "Unknown Dropbox error"
	var summary string
	if d != nil && d.Summary != nil {
		summary = *d.Summary
	}
	pathNotFound := d != nil && d.Path != nil && d.Path.Tag == "not_found"

	invalidToken := d != nil && d.AccessToken != nil && strings.Contains(*d.AccessToken, "invalid_access_token")
	if status == 401 || invalidToken {
		code, msg = 401, "Invalid or expired access token"
	} else if status == 429 || strings.Contains(summary, "rate_limit_exceeded") {
		code, msg = 429, "Rate limit exceeded. Please wait and try again."
	} else if status == 403 {
		code, msg = 403, "Permission denied"
	} else if status == 404 || pathNotFound {
		code, msg = 404, "File or folder not found"
	} else if status == 409 {
		code = 409
		conflict := ""
		if d != nil && d.Conflict != nil {
			conflict = d.Conflict.Summary
		}
		if strings.Contains(conflict, "different_file") || strings.Contains(conflict, "same_file") {
			msg = "File conflict detected"
		} else {
			msg = "Conflict: " + (summaryIf(summary, "Unknown Dropbox error"))
		}
	} else if status >= 500 {
		code, msg = 503, "Dropbox service temporarily unavailable"
	}
	return code, msg, summary
}

func summaryIf(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// parseISO8601 parses an RFC3339/ISO-8601 timestamp (Dropbox server_modified).
func parseISO8601(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}

// dbxOrDefault returns s if non-empty, else def (Node's `x || 'default'`).
func dbxOrDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
