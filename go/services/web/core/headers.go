package core

import (
	"net/http"
	"regexp"
)

// Web-profile baseline response headers, mirroring the webRouter-level
// middleware chain of services/web/app/src/infrastructure/Server.mjs:
//
//	webRouter.use(helmet(...))          (line ~322, AFTER csrf @ ~233)
//	no-cache wrapper middleware         (lines ~330–360)
//
// Pinned live 2026-09-13 (P3.1 gate battery): every webRouter response
// carries the helmet set (referrer-policy / x-content-type-options /
// x-download-options / x-frame-options / x-xss-protection /
// x-permitted-cross-domain-policies / cross-origin-opener-policy /
// cross-origin-resource-policy). The nocache set (Cache-Control / Expires
// / Pragma / Surrogate-Control) applies to LOGGED-IN responses and to
// project pages, with the project-file / project-blob / wiki exemptions;
// anonymous non-project responses are cacheable (no headers).
//
// X-Powered-By is set by the core Handler wrapper (express default, pinned
// P0 on /status). Permissions-Policy is NOT part of this baseline — Node
// adds it per rendered view only (pinned: JSON routes have none).

func (a *App) setWebBaseline(w http.ResponseWriter, r *http.Request, loggedIn bool) {
	h := w.Header()
	h.Set("Referrer-Policy", "origin-when-cross-origin")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Download-Options", "noopen")
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Set("X-XSS-Protection", "0")
	h.Set("X-Permitted-Cross-Domain-Policies", "none")
	h.Set("Cross-Origin-Opener-Policy", "same-origin-allow-popups")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	if noCacheFor(r.URL.Path) || loggedIn {
		h.Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")
		h.Set("Expires", "0")
		h.Set("Pragma", "no-cache")
		h.Set("Surrogate-Control", "no-store")
	}
}

var (
	projectPageRe = regexp.MustCompile(`^/project/[a-f0-9]{24}$`)
	projectFileRe = regexp.MustCompile(`^/project/[a-f0-9]{24}/file/[a-f0-9]{24}$`)
	projectBlobRe = regexp.MustCompile(`^/project/[a-f0-9]{24}/blob/[a-f0-9]{40}$`)
	wikiContentRe = regexp.MustCompile(`^/learn(-scripts)?(/|$)`)
)

// noCacheFor mirrors the Server.mjs no-cache wrapper condition for the
// ANONYMOUS branch (project pages only; file/blob/wiki exempt).
func noCacheFor(path string) bool {
	switch {
	case projectFileRe.MatchString(path),
		projectBlobRe.MatchString(path),
		wikiContentRe.MatchString(path):
		return false
	}
	return projectPageRe.MatchString(path)
}

// PinnedPermissionsPolicy is the HttpPermissionsPolicy header value — the
// fixed default policy of settings (useHttpPermissionsPolicy, no custom
// policy env in CE deployments). Rendered views only (pinned live:
// editor-state JSON has none, the 500 view does).
const PinnedPermissionsPolicy = "accelerometer=(), browsing-topics=(), camera=(), display-capture=(), encrypted-media=(), gamepad=(), geolocation=(), gyroscope=(), hid=(), identity-credentials-get=(), idle-detection=(), local-fonts=(), magnetometer=(), midi=(), otp-credentials=(), picture-in-picture=(), screen-wake-lock=(), serial=(), storage-access=(), usb=(), window-management=(), xr-spatial-tracking=(), autoplay=(self \"https://videos.ctfassets.net\"), fullscreen=(self), on-device-speech-recognition=(self), payment=(self \"https://js.stripe.com\")"
