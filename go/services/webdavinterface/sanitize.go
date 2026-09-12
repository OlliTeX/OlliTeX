package webdavinterface

import (
	"errors"
	"net/url"
	"regexp"
)

// --- error sanitization helpers (1:1 with server.mjs H8/M9) ------------------

var credentialURLRe = regexp.MustCompile(`(?i)(https?|webdav)://[^@\s]+@`)

// safeError redacts credential-bearing URLs for LOGS (1:1 with Node safeError).
func safeError(err error) string {
	msg := "unknown error"
	if err != nil {
		msg = err.Error()
	}
	if len(msg) > 1000 {
		msg = msg[:1000]
	}
	return credentialURLRe.ReplaceAllString(msg, "$1://<redacted>@")
}

var (
	reAuth     = regexp.MustCompile(`(?i)unauthorized|authentication|invalid (token|credential)`)
	reNotFound = regexp.MustCompile(`(?i)not found|no such`)
	reConflict = regexp.MustCompile(`(?i)conflict|precondition`)
)

// providerStatusError maps a provider error to a generic {status,error} for
// RESPONSES (1:1 with Node providerStatusError).
func providerStatusError(status int, msg string) (int, string) {
	if status == 401 || reAuth.MatchString(msg) {
		return 401, "authentication failed"
	}
	if status == 404 || reNotFound.MatchString(msg) {
		return 404, "not found"
	}
	if status == 409 || status == 412 || reConflict.MatchString(msg) {
		return 409, "modified since last sync"
	}
	return 502, "provider request failed"
}

// sanitizeUrlForLogging strips embedded credentials from a URL for logs (1:1).
func sanitizeUrlForLogging(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return u.Scheme + "://" + u.Host + u.Path
}

// webdavStatusOf extracts a provider status from *WebDAVError (else 0).
func webdavStatusOf(err error) int {
	var we *WebDAVError
	if errors.As(err, &we) {
		return we.Status
	}
	return 0
}

// isRetryable reports whether a WebDAV status should be retried (1:1: 423 /
// 502 / 503 / 504).
func isRetryable(status int) bool {
	switch status {
	case 423, 502, 503, 504:
		return true
	}
	return false
}
