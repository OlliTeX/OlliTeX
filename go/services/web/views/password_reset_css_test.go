package views

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPasswordResetSharedCSS — 2026-09-27 (owner live report, psintern):
// /user/password/reset rendered UNSTYLED while /login was styled. Root
// cause: passwordResetHTML was the only auth-React page WITHOUT the
// \x01SHARED:CSS:<entry>\x02 hook, so the shared chunk stylesheets
// (fixture: 9663-*.css, prod: 9663-a7a4b91ea9fb30ea5876.css) were never
// linked. The fixture assetfix universe declares the entry's shared chunks
// [1772,7189,9663] and ships the 9663 css — rendering the reset page MUST
// now link it (same mechanism the D39a pinned SHARED:CSS resolver provides
// to /login and /register).
func TestPasswordResetSharedCSS(t *testing.T) {
	// template-level pin first (the hook must exist in the constant)
	if !strings.Contains(passwordResetHTML, "\x01SHARED:CSS:pages/auth/password-reset.js\x02") {
		t.Fatalf("passwordResetHTML lacks the SHARED:CSS hook for entries/auth (the /reset-CSS-missing regression)")
	}
	d := PageData{CSRFToken: "tokval123456", Nonce: "nonceval1234", Origin: "http://127.0.0.1:7420"}
	w := httptest.NewRecorder()
	PasswordResetPage(w, d)
	out := w.Body.String()
	if !strings.Contains(out, `<link rel="stylesheet" href="/stylesheets/9663-cccc333333333333333c.css">`) {
		t.Fatalf("rendered /user/password/reset page does not link the shared chunk CSS (9663): the page would render unstyled. head section:\n%s",
			extractHead(out))
	}
	// link order (owner-visible): the chunk css BEFORE main-style.css (same as
	// /login parity — the chunk css must apply before the base layer wins).
	baseIdx := strings.Index(out, `id="main-stylesheet"`)
	chunkIdx := strings.Index(out, `/stylesheets/9663-cccc333333333333333c.css`)
	if baseIdx < 0 || chunkIdx < 0 || chunkIdx > baseIdx {
		t.Fatalf("chunk-CSS order: chunk@%d mainStyle@%d (chunk must precede)", chunkIdx, baseIdx)
	}
}

func extractHead(s string) string {
	i := strings.Index(s, "<head>")
	j := strings.Index(s, "</head>")
	if i < 0 || j < 0 || j > len(s) {
		return s[:200]
	}
	return s[i:j]
}
