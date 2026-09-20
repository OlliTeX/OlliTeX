package dropbox

import (
	"net/url"
	"strconv"

	"ollitex/go/services/web/core"
)

// dbxText — Express res.status(code).send('string') semantics (the dropbox
// 503/400 text pins): text/html charset, express weak ETag, x-powered-by.
func dbxText(res *core.Res, code int, msg string) {
	h := res.W.Header()
	for _, k := range []string{
		"Referrer-Policy", "X-Content-Type-Options", "X-Frame-Options",
		"Cache-Control", "Permissions-Policy", "Set-Cookie",
	} {
		h.Del(k)
	}
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("X-Powered-By", "Express")
	h.Set("ETag", core.EtagWeakBody(msg))
	h.Set("Content-Length", strconv.Itoa(len(msg)))
	res.W.WriteHeader(code)
	_, _ = res.W.Write([]byte(msg))
}

// urlPathDecode — Node decodeURIComponent with try/catch: malformed percent
// sequences keep the original string.
func urlPathDecode(s string) (string, bool) {
	out, err := url.PathUnescape(s)
	if err != nil {
		return s, false
	}
	return out, true
}

// dbxStateJSON — serialization of a stored state doc for
// GET .../dropbox/state. Sandbox pins: NO state docs exist (always the
// {"connected":false} branch), so the exact field order of the stored+
// enriched shape is live-only (Node spreads enrichment over the stored
// object). This renderer emits the stored fields in model order; documented
// in the P6.10 scope note.
func dbxStateJSON(state map[string]interface{}) string {
	var parts []string
	for _, k := range []string{"connected", "path", "remoteFiles", "lastSyncRev", "lastSyncVersion", "mergeStatus", "ownerId", "projectName", "projectPath", "lastSyncAt", "lastSyncError"} {
		v, ok := state[k]
		if !ok {
			continue
		}
		switch t := v.(type) {
		case bool:
			parts = append(parts, k+": "+t2s(t))
		case string:
			parts = append(parts, k+": "+q(t))
		default:
			continue // structured fields stay JSON-encoded via the caller (rare)
		}
	}
	out := ""
	for i, f := range parts {
		if i > 0 {
			out += ","
		}
		out += f
	}
	return "{" + out + "}"
}

func t2s(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func q(s string) string {
	b, _ := jsonQuoteString(s)
	return b
}
