package core

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
)

// ---------- response helpers (Express-parity) ----------

// Res is the minimal response wrapper feature handlers receive.
type Res struct{ W http.ResponseWriter }

// PlainText mirrors Response.mjs plainTextResponse(res, body):
//
//	X-Content-Type-Options: nosniff
//	Content-Type: text/plain; charset=utf-8
//	ETag: W/"…"                (express res.send)
//	body (no trailing newline)
//
// Used by /status (pinned: 200, 18-byte body "web is alive (web)").
func (r *Res) PlainText(code int, body string) {
	r.W.Header().Set("X-Content-Type-Options", "nosniff")
	r.W.Header().Set("Content-Type", "text/plain; charset=utf-8")
	r.W.Header().Set("ETag", EtagWeakBody(body))
	r.W.Header().Set("Content-Length", fmt.Sprint(len(body)))
	r.W.WriteHeader(code)
	_, _ = r.W.Write([]byte(body))
}

// SendStatus mirrors express res.sendStatus(code):
// res.status(code).type('txt').end(statusMessage[code]) — NO nosniff,
// body = the status text ("OK", "Forbidden", "Internal Server Error", …).
func (r *Res) SendStatus(code int) {
	msg := http.StatusText(code)
	if msg == "" {
		msg = "Unknown"
	}
	r.W.Header().Set("Content-Type", "text/plain; charset=utf-8")
	r.W.Header().Set("ETag", EtagWeakBody(msg))
	r.W.Header().Set("Content-Length", fmt.Sprint(len(msg)))
	r.W.WriteHeader(code)
	_, _ = r.W.Write([]byte(msg))
}

var redirectMessages = map[int]string{
	301: "Moved Permanently",
	303: "See Other",
	307: "Temporary Redirect",
	308: "Permanent Redirect",
}

func redirectMessage(code int) string {
	if m, ok := redirectMessages[code]; ok {
		return m
	}
	return http.StatusText(code) // 302 → "Found"
}

func escapeHtml(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;").Replace(s)
}

// Redirect30x mirrors express res.redirect(code, url) including the
// Accept-based body negotiation (express .format order text → html →
// default), per express 4.22.1 source. Pinned live: 302 with
// Accept */* → text/plain body "Found. Redirecting to /login" (28 B).
func (r *Res) Redirect(req *http.Request, code int, url string) {
	msg := redirectMessage(code)
	accept := req.Header.Get("Accept")
	hasHTML := strings.Contains(accept, "text/html")
	hasText := strings.Contains(accept, "text/") || strings.Contains(accept, "*/*")
	var body string
	var ct string
	switch {
	case hasHTML && (accept == "" || strings.Contains(accept, "text/html")):
		// .format picks by Accept ORDER; browsers send text/html first →
		// html body (pinned against a browser probe in the P0 gate).
		body = "<p>" + msg + ". Redirecting to " + escapeHtml(url) + "</p>"
		ct = "text/html; charset=utf-8"
	case hasText:
		body = msg + ". Redirecting to " + url
		ct = "text/plain; charset=utf-8"
	default:
		body = ""
		ct = "text/plain; charset=utf-8"
	}
	r.W.Header().Set("Location", url)
	r.W.Header().Set("Content-Type", ct)
	if body != "" {
		r.W.Header().Set("ETag", EtagWeakBody(body))
	}
	r.W.Header().Set("Content-Length", fmt.Sprint(len(body)))
	r.W.WriteHeader(code)
	if body != "" {
		_, _ = r.W.Write([]byte(body))
	}
}

// ---------- CSP (infrastructure/CSP.mjs parity) ----------

// CSPDefaultPolicy is buildDefaultPolicy(reportUri=undefined) — the header
// set on every response by app.use(csp) (pinned live: GET /status).
const CSPDefaultPolicy = "base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'"

// CSPViewPolicy is buildViewPolicy for non-excluded rendered views
// (per-render random nonce, 16 random bytes base64 — express-session
// style, exactly crypto.randomBytes(16).toString('base64')).
func CSPViewPolicy(nonce string) string {
	return fmt.Sprintf(
		`script-src 'nonce-%s' 'unsafe-inline' 'strict-dynamic' https: 'report-sample'; object-src 'none'; base-uri 'none'`,
		nonce)
}

// NewCspNonce is the per-render scriptNonce (crypto.randomBytes(16) →
// standard base64, 24 chars).
func NewCspNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// ---------- misc parity helpers ----------

// IsAllowedOrigin mirrors Settings.allowedOrigins.includes(origin).
func (c *Config) IsAllowedOrigin(origin string) bool {
	if origin == "" {
		return true
	}
	for _, a := range c.AllowedOrigins {
		if a == origin {
			return true
		}
	}
	return false
}

// ClientIP resolves request source IP behind the trusted proxy chain
// (behindProxy + trustedProxyIps='loopback' + nginx X-Forwarded-For).
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		return host[:i]
	}
	return host
}
