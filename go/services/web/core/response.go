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

// JSON mirrors express res.json: Content-Type application/json;
// charset=utf-8 + weak ETag on the serialized body (pinned P3.1 on the
// editor-state and {"success":true} responses).
func (r *Res) JSON(code int, b []byte) {
	r.W.Header().Set("Content-Type", "application/json; charset=utf-8")
	r.W.Header().Set("ETag", EtagWeakBody(string(b)))
	r.W.Header().Set("Content-Length", fmt.Sprint(len(b)))
	r.W.WriteHeader(code)
	_, _ = r.W.Write(b)
}

// BareWrite emits a response with NO web-baseline headers. Mirrors Node's
// ordering for express.json (body-parser) REJECTION 400s: the parser runs
// before the csrf/helmet middleware, so the 400 {} for a non-object JSON
// root (number/string/boolean) carries only content-type / x-powered-by /
// etag / content-length (pinned live 2026-09-14 P3.1: `5`, `"str"`,
// `true` → 400 `{}` with no CSP / nosniff / referrer set), while
// object/array roots reach zod and get the verbose 400.
func (r *Res) BareWrite(code int, body []byte) {
	h := r.W.Header()
	for _, k := range []string{
		"Referrer-Policy",
		"X-Content-Type-Options",
		"X-Download-Options",
		"X-Frame-Options",
		"X-XSS-Protection",
		"X-Permitted-Cross-Domain-Policies",
		"Cross-Origin-Opener-Policy",
		"Cross-Origin-Resource-Policy",
		"Cache-Control",
		"Expires",
		"Pragma",
		"Surrogate-Control",
		"Permissions-Policy",
		"Set-Cookie",
		"Content-Security-Policy",
	} {
		h.Del(k)
	}
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("X-Powered-By", "Express")
	h.Set("ETag", EtagWeakBody(string(body)))
	h.Set("Content-Length", fmt.Sprint(len(body)))
	r.W.WriteHeader(code)
	_, _ = r.W.Write(body)
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

// NoContent mirrors modern express res.sendStatus(204): status only —
// empty body, no Content-Type/ETag/Content-Length (node-pinned P4.10a).
func (r *Res) NoContent() {
	r.W.WriteHeader(204)
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
// Accept-based body negotiation. Pinned live against the running Node
// stack (P3.1, 2026-09-14) — the complete matrix:
//
//	Accept contains text/html          → html body, text/html CT
//	else text/*, text/plain, */*       → plain body, text/plain CT
//	no Accept header at all            → plain body, text/plain CT
//	else (application/json, ...xml)    → EMPTY body, NO Content-Type
//
// All 302s carry Vary: Accept and Location; NO ETag (express redirect
// does not run the etag middleware).
//
//	html : <p>Found. Redirecting to /admin#x</p>
//	plain: Found. Redirecting to /admin#x
func (r *Res) Redirect(req *http.Request, code int, url string) {
	msg := redirectMessage(code)
	hasHTML := false
	hasText := false
	for _, part := range strings.Split(req.Header.Get("Accept"), ",") {
		typ := strings.TrimSpace(part)
		if i := strings.IndexByte(typ, ';'); i >= 0 {
			typ = strings.TrimSpace(typ[:i])
		}
		if typ == "text/html" {
			hasHTML = true
		}
		if strings.HasPrefix(typ, "text/") || typ == "*/*" {
			hasText = true
		}
	}
	var body string
	var ct string
	switch {
	case hasHTML:
		body = "<p>" + msg + ". Redirecting to " + url + "</p>"
		ct = "text/html; charset=utf-8"
	case hasText:
		body = msg + ". Redirecting to " + url
		ct = "text/plain; charset=utf-8"
	case req.Header.Get("Accept") == "":
		body = msg + ". Redirecting to " + url
		ct = "text/plain; charset=utf-8"
	default:
		body = ""
		ct = ""
	}
	r.W.Header().Set("Location", url)
	if ct != "" {
		r.W.Header().Set("Content-Type", ct)
	} else {
		// suppress net/http content sniffing for the empty-body case
		r.W.Header().Set("Content-Type", "")
	}
	r.W.Header().Set("Vary", "Accept")
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
