// Package views renders the Go-ported web shell pages with byte parity to
// the Node service (contract: pages_data.go, captured from the running
// stack). Static skeletons + per-request slot substitution:
//
//	\x01CSRF\x02  → session csrf token (csurf contract, P0 pin)
//	\x01NONCE\x02 → per-request base64 nonce (16 random bytes, std encoding)
//
// Env note: the skeletons embed the capture origin (e2e); the renderer
// rewrites it to the current request origin so any deployment renders the
// same page for its own host.
package views

import (
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
)

const (
	slotCSRF    = "\x01CSRF\x02"
	slotNonce   = "\x01NONCE\x02"
	slotPath    = "\x01PATH\x02"
	slotOLUsers = "\x01OLUSERS\x02"
	slotOLUID   = "\x01OLUID\x02"
	// origin captured from the e2e fixtures (rewritten per request).
	capturedOrigin = "http://127.0.0.1:7420"
)

var nonceEnc = base64.StdEncoding

// Nonce = 16 random bytes, std base64 (22 chars) — same shape as Node's
// per-request CSP nonce (value is random; gates normalise it).
func NewNonce() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return nonceEnc.EncodeToString(b[:])
}

// PageData carries per-request dynamic values into the skeletons.
type PageData struct {
	CSRFToken string // session csrf token ("" → anonymous session must not appear here)
	Nonce     string
	Origin    string // "http://host" of the current request
	// CSP — Node sets a PER-LAYOUT policy (pinned live 2026-09-13):
	// React app pages (login/register) allow nonce-script + strict-dynamic;
	// plain views (logout/restricted/404) carry the restrictive policy.
	CSP string
	// 404-only dynamic slots (the skeleton was captured logged-in):
	Path      string // request path (alternate link)
	UserEmail string // session user email (Node: ol-usersEmail + navbar pill)
	UserID    string // session user id (ol-user_id)
}

func (p PageData) finalize(html string) string {
	out := strings.ReplaceAll(html, slotCSRF, p.CSRFToken)
	out = strings.ReplaceAll(out, slotNonce, p.Nonce)
	if p.Path != "" {
		out = strings.ReplaceAll(out, slotPath, p.Path)
	} else {
		out = strings.ReplaceAll(out, slotPath, "/")
	}
	out = strings.ReplaceAll(out, slotOLUsers, p.UserEmail)
	out = strings.ReplaceAll(out, slotOLUID, p.UserID)
	orig := originOf(p.Origin)
	if orig != "" {
		out = strings.ReplaceAll(out, capturedOrigin, orig)
	}
	return out
}

// originOf normalizes "http://host" / "https://host" (strips paths).
func originOf(u string) string {
	scheme := "http"
	rest := u
	switch {
	case len(u) > 8 && u[:8] == "https://":
		scheme, rest = "https", u[8:]
	case len(u) > 7 && u[:7] == "http://":
		rest = u[7:]
	default:
		return u
	}
	for i := 0; i < len(rest); i++ {
		if rest[i] == '/' {
			return scheme + "://" + rest[:i]
		}
	}
	return scheme + "://" + rest
}

// Page renders a shell page (HTML, no ETag — Node sets none on views).
const cspRestrictive = "base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'"

// cspReact — Node's React-layout CSP (pinned live on /login + /register).
func cspReact(nonce string) string {
	return "script-src 'nonce-" + nonce + "' 'unsafe-inline' 'strict-dynamic' https: 'report-sample'; object-src 'none'; base-uri 'none'"
}

func Page(w http.ResponseWriter, d PageData, skeleton string) {
	csp := d.CSP
	if csp == "" {
		csp = cspRestrictive
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", csp)
	w.Header().Set("X-Powered-By", "Express")
	w.WriteHeader(200)
	_, _ = io.WriteString(w, d.finalize(skeleton))
}

// StatusPage for 404/500-style views (Node: res.status(404).render(...)).
func StatusPage(w http.ResponseWriter, d PageData, status int, skeleton string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'")
	w.Header().Set("X-Powered-By", "Express")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, d.finalize(skeleton))
}

// LoginPage / RegisterPage / LogoutConfirmation / Restricted / NotFound.
func LoginPage(w http.ResponseWriter, d PageData) { d.CSP = cspReact(d.Nonce); Page(w, d, loginHTML) }
func RegisterPage(w http.ResponseWriter, d PageData) {
	d.CSP = cspReact(d.Nonce)
	Page(w, d, registerHTML)
}
func LogoutPage(w http.ResponseWriter, d PageData)     { Page(w, d, logoutHTML) }
func RestrictedPage(w http.ResponseWriter, d PageData) { Page(w, d, restrictedHTML) }
func NotFoundPage(w http.ResponseWriter, d PageData)   { StatusPage(w, d, 404, notFoundHTML) }
