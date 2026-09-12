package dropboxinterface

import (
	"errors"
	"regexp"
	"strings"
)

// --- token helpers (1:1 with auth.mjs) ---------------------------------------

// dbxValidateToken mirrors Node validateToken (non-empty string; warns if not
// an sl. token but still accepts).
func dbxValidateToken(token string) error {
	if token == "" {
		return errors.New("Missing or invalid access token")
	}
	return nil
}

// dbxIsTokenFormat is 1:1 with DropboxClient.isValidToken (sl. or dp. prefix).
func dbxIsTokenFormat(token string) bool {
	return strings.HasPrefix(token, "sl.") || strings.HasPrefix(token, "dp.")
}

// dbxSanitizeTokenForLogging shows first 10 + last 4 chars (1:1).
func dbxSanitizeTokenForLogging(token string) string {
	if token == "" {
		return "[none]"
	}
	if len(token) <= 14 {
		return "[hidden]"
	}
	return token[:10] + "..." + token[len(token)-4:]
}

var dbxTokenLE = regexp.MustCompile(`(?i)access[_-]?token[=:'" ]+[A-Za-z0-9._~+-]+`)

// safeProviderError redacts a token value and access_token= patterns for
// logs/responses (1:1 with server.mjs).
func safeProviderError(msg, token string) string {
	if token != "" {
		msg = strings.ReplaceAll(msg, token, "<redacted-token>")
	}
	msg = dbxTokenLE.ReplaceAllString(msg, "access_token=<redacted>")
	if len(msg) > 500 {
		msg = msg[:500]
	}
	return msg
}
