// Package wakatime — wakapi (self-hosted WakaTime-compatible) client
// helpers: connection checks (hub "Check connection") and per-user
// auto-provisioning via the v2 form flow (admin section, owner request
// G, 2026-10-09).
//
// Server-side key surface (verified live 2026-10-09 against wakapi
// v2.18, the deployed image):
//
//   - the v1-style REST key endpoints (/api/v0/user/api-key,
//     /api/v0/user) are NOT present in v2 — the only key surface is the
//     settings page's "reset API key" (POST /settings
//     {action: reset_apikey}), which renders the key into HTML
//     (id="api-key-container").
//   - signup is a form POST (POST /signup {username, email, password,
//     password_repeat[, captcha_id]}); 302 = created, 409 = already
//     exists (both fine for idempotent provisioning).
//   - signup does NOT auto-authenticate (v2): the flow is
//     signup → login (POST /login {username, password}, sets the
//     wakapi_auth cookie) → reset_apikey (POST /settings {action:
//     reset_apikey}) → scrape the key from the settings HTML.
//   - email validation requires an MX record unless
//     WAKAPI_MAIL_SKIP_VERIFY_MX_RECORD=true (set for this internal
//     instance — internal domains like *.dev.local have no MX).
//
// The generated wakapi account is a pure relay credential: the browser
// never talks to wakapi (heartbeats go through the wakatime feature's
// relay endpoint) and the API key is stored encrypted like any user
// pasted key. The generated password is kept only long enough to log in
// — never rendered or logged.

package wakatime

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var apiKeyInHTML = regexp.MustCompile(`id="api-key-container"[^>]*value="([A-Za-z0-9\-_]+)"`)

// ProvisionRequest — admin auto-provision input.
type ProvisionRequest struct {
	// ServerBase — the wakapi base URL (edge: https://…/wakapi).
	ServerBase string
	// Username — wakapi username (the OlliTeX email's local part).
	Username string
	// Email — the OlliTeX user's email (wakapi account email).
	Email string
	// Password — explicit override; empty = derived deterministically
	// from InstanceSecret + Email (stableProvisionPassword).
	Password string
	// InstanceSecret — the wakatime credential-encryptor password
	// (WAKATIME_TOKEN_CIPHER_PASSWORD or the bootstrap file) — the key
	// for stable password derivation.
	InstanceSecret string
}

// provisionResult — the fresh wakapi API key (+ the provision password,
// in-memory only, for the optional /api/v0 compatibility path).
type provisionResult struct {
	Key      string
	Password string
}

var provisionClient = func() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Jar:     jar,
		Timeout: 25 * time.Second,
	}
}()

func normBase(raw string) (string, error) {
	b := strings.TrimSpace(raw)
	if b == "" {
		return "", fmt.Errorf("server base URL is empty")
	}
	if !strings.HasPrefix(b, "http://") && !strings.HasPrefix(b, "https://") {
		b = "https://" + b
	}
	u, err := url.Parse(b)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid wakapi server URL: %q", raw)
	}
	return strings.TrimSuffix(u.String(), "/"), nil
}

// stableProvisionPassword — a DETERMINISTIC wakapi account password
// (hmac-sha256(instanceSecret, "wakapi:"+email) → 20 url-safe chars; ≥6
// = wakapi's minimum). Identical on every re-provision for the same
// (instance, user): required for the 409 already-exists path, where the
// original generated password is unrecoverable and wakapi v2 has no
// "reset password without the old one" surface.
func stableProvisionPassword(secret, email string) string {
	if secret == "" {
		secret = "ollitex-wakatime-fallback"
	}
	h := hmac.New(sha256.New, []byte(secret))
	_, _ = io.WriteString(h, "wakapi:")
	_, _ = io.WriteString(h, strings.ToLower(strings.TrimSpace(email)))
	d := h.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(d)[:20]
}

// genPassword — 24 url-safe random bytes (base64 32 chars, no padding).
func genPassword() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}



func formPost(c *http.Client, u string, form url.Values) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.Do(req)
}

func readAll(r io.Reader, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, limit))
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i > 0 {
		s = s[:i]
	}
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	if i := strings.Index(s, "<"); i > 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// apiBaseFor — the wakapi instance serves the WakaTime-compatible API under
// /api/compat/wakatime/v1 (verified live against wakapi v2, 2026-10-10: a
// bare /wakapi base 404s on /users/current). So stored relay credentials
// for the instance account carry the FULL API prefix; official wakatime.com
// URLs (and anything that already carries an /api/ prefix) are untouched.
func apiBaseFor(base string) string {
	if strings.Contains(base, "/wakapi") && !strings.Contains(base, "/api/") {
		return strings.TrimSuffix(base, "/") + "/api/compat/wakatime/v1"
	}
	return base
}

// provisionAccount — the three-step v2 form flow (idempotent: a
// "user already existing" signup answer is re-used via login).
func provisionAccount(_ context.Context, p ProvisionRequest) (*provisionResult, error) {
	base, err := normBase(p.ServerBase)
	if err != nil {
		return nil, err
	}
	if p.Email == "" {
		return nil, fmt.Errorf("email is required (the OlliTeX user's email)")
	}
	if p.Username == "" {
		local := strings.SplitN(strings.TrimSpace(p.Email), "@", 2)[0]
		if local != "" {
			p.Username = local
		} else {
			return nil, fmt.Errorf("username is required (use the OlliTeX email local part)")
		}
	}
	if p.Password == "" {
		// Deterministic: identical on every re-provision for the same
		// (instance, user) — required for the 409 (already-exists) path
		// where the original generated password is unrecoverable.
		p.Password = stableProvisionPassword(p.InstanceSecret, p.Email)
	}

	c := provisionClient
	c.Jar, err = cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	// ── 1. signup (302/created or 409 already-exists are both fine) ────
	sf := url.Values{}
	sf.Set("username", p.Username)
	sf.Set("email", p.Email)
	sf.Set("password", p.Password)
	sf.Set("password_repeat", p.Password)
	sf.Set("captcha_id", "")
	sf.Set("location", "")
	sf.Set("invite_code", "")
	resp, err := formPost(c, base+"/signup", sf)
	if err != nil {
		return nil, fmt.Errorf("signup request failed: %w", err)
	}
	bb, _ := readAll(resp.Body, 1<<20)
	_ = resp.Body.Close()
	if resp.StatusCode > 299 && resp.StatusCode != http.StatusConflict {
		return nil, fmt.Errorf("signup failed (HTTP %d): %s",
			resp.StatusCode, firstLine(string(bb)))
	}

	// ── 2. login (v2 signup does not auto-authenticate) ────────────────
	lf := url.Values{}
	lf.Set("username", p.Username)
	lf.Set("password", p.Password)
	lr, err := formPost(c, base+"/login", lf)
	if err != nil {
		return nil, fmt.Errorf("login request failed: %w", err)
	}
	lb, _ := readAll(lr.Body, 1<<20)
	_ = lr.Body.Close()
	if lr.StatusCode < 200 || lr.StatusCode > 299 {
		return nil, fmt.Errorf("login failed (HTTP %d): %s",
			lr.StatusCode, firstLine(string(lb)))
	}

	// ── 3. reset the primary API key → key rendered in settings HTML ───
	kf := url.Values{}
	kf.Set("action", "reset_apikey")
	kr, err := formPost(c, base+"/settings", kf)
	if err != nil {
		return nil, fmt.Errorf("api-key reset request failed: %w", err)
	}
	kb, _ := readAll(kr.Body, 2<<20)
	_ = kr.Body.Close()
	if kr.StatusCode < 200 || kr.StatusCode > 299 {
		return nil, fmt.Errorf("api-key reset failed (HTTP %d): %s",
			kr.StatusCode, firstLine(string(kb)))
	}
	m := apiKeyInHTML.FindSubmatch(kb)
	if m == nil {
		return nil, fmt.Errorf("api key not found in wakapi settings response " +
			"(page may have changed — check WAKAPI version)")
	}
	return &provisionResult{Key: string(m[1]), Password: p.Password}, nil
}
