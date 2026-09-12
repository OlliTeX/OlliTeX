package githubinterface

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// --- small helpers (1:1 with server.mjs free functions) ---------------------

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// scrubDetail removes a secret and truncates to 500 chars (P0-4).
func scrubDetail(text, secret string) string {
	detail := text
	if secret != "" {
		detail = strings.ReplaceAll(detail, secret, "")
	}
	if len(detail) > 500 {
		detail = detail[:500]
	}
	return detail
}

func assertGitServerUrl(serverUrl string) string {
	u, err := url.Parse(serverUrl)
	if err != nil {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return serverUrl
}

func isAllowedServerUrl(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return u.Hostname() != ""
}

func resolveWorkDir(workRoot, value string) (string, error) {
	if !strings.HasPrefix(value, "/") {
		return "", ghiStatusErr(400, "dir must be an absolute path within the service work root")
	}
	resolved := filepath.Clean(value)
	root := filepath.Clean(workRoot)
	if resolved != root && !strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
		return "", ghiStatusErr(400, "dir is outside the allowed githubinterface work directory")
	}
	return resolved, nil
}

var invalidRefRe = regexp.MustCompile(`[\s+:]`)

func validRef(ref string) bool {
	if ref == "" {
		return true
	}
	if strings.HasPrefix(ref, "-") {
		return false
	}
	return !invalidRefRe.MatchString(ref)
}

var nonAlnumDashRe = regexp.MustCompile(`[^a-zA-Z0-9-]`)

// identityEmail mirrors the /commit author-email fallback (1:1).
func identityEmail(name, email string) string {
	if email != "" {
		return email
	}
	cleaned := nonAlnumDashRe.ReplaceAllString(name, "")
	if cleaned == "" {
		cleaned = "overleaf"
	}
	return cleaned + "@localhost"
}

func ghiFirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func ghiOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
