package views

import (
	"strings"
	"testing"
)

// Audit H2 (problems_30092026): baked e2e capture artifacts must never be
// served. The literals below are byte-copied from the captured skeletons.
func TestSanitizeCaptureArtifacts_CsrfEscapedForm(t *testing.T) {
	// byte-copied from the captured skeleton (Go-source escaping shown):
	in := `...name=\"_csrf\" type=\"hidden\" value=\"kqYCGy4j-ONpvNUHE2zYTR4ObW-LAy0cjEiQ\"...`
	out := sanitizeCaptureArtifacts(in, "TOKEN123-XXXX", "http://h")
	if strings.Contains(out, "kqYCGy4j") {
		t.Fatalf("stale csrf literal survived: %q", out)
	}
	if !strings.Contains(out, `value=\"TOKEN123-XXXX\"`) {
		t.Fatalf("session token not injected: %q", out)
	}
}

func TestSanitizeCaptureArtifacts_CsrfRawForm(t *testing.T) {
	in := `<input name="_csrf" type="hidden" value="3XOEqz0Z-13MTXD-X-3Q4CjCo27Qh2w0ZXwo">`
	out := sanitizeCaptureArtifacts(in, "AAAAbbbb-cccc", "")
	if strings.Contains(out, "3XOEqz0Z") {
		t.Fatalf("stale csrf literal survived: %q", out)
	}
	if !strings.Contains(out, `value="AAAAbbbb-cccc"`) {
		t.Fatalf("session token not injected: %q", out)
	}
}

func TestSanitizeCaptureArtifacts_E2eEmails(t *testing.T) {
	in := `&quot;email&quot;:&quot;e2e-user@e2e.test&quot;` +
		` admin@e2e.test ` +
		`someone@e2e.test`
	out := sanitizeCaptureArtifacts(in, "T", "")
	if strings.Contains(out, "@e2e.test") {
		t.Fatalf("e2e email survived: %q", out)
	}
}

func TestSanitizeCaptureArtifacts_ProjectIDs(t *testing.T) {
	in := `/project/6aa4b8c973ef0e5094f4cc02/sharing-updates` +
		` http://127.0.0.1:7420/1234567890abcdefgh`
	out := sanitizeCaptureArtifacts(in, "T", "http://h")
	for _, bad := range []string{"6aa4b8c973ef0e5094f4cc02", "1234567890abcdefgh", "127.0.0.1:7420"} {
		if strings.Contains(out, bad) {
			t.Fatalf("baked artifact %q survived: %q", bad, out)
		}
	}
}

func TestSanitizeCaptureArtifacts_EmptyOriginFallback(t *testing.T) {
	in := `<link href="http://127.0.0.1:7420/login" rel="alternate">`
	out := sanitizeCaptureArtifacts(in, "T", "")
	if strings.Contains(out, "127.0.0.1:7420") {
		t.Fatalf("captured origin survived with empty request origin: %q", out)
	}
	if !strings.Contains(out, `href="/login"`) {
		t.Fatalf("expected root-relative href: %q", out)
	}
}

func TestSanitizeCaptureArtifacts_LeavesSlotFormIntact(t *testing.T) {
	// the legitimate slot (already session-resolved by finalize before this
	// pass) must not be double-processed into a broken value
	in := `_csrf\" type=\"hidden\" value="TOKEN-OK"`
	out := sanitizeCaptureArtifacts(in, "TOKEN-OK", "")
	if out != in {
		t.Fatalf("already-resolved token was mangled: %q", out)
	}
}
