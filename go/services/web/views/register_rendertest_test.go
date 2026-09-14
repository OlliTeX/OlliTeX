package views

import (
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

var (
	regNonceRe = regexp.MustCompile(`nonce="[^"]+"`)
	regCSRFRe  = regexp.MustCompile(`ol-csrfToken" content="[^"]+"`)
)

func normRegister(s string) string {
	s = regNonceRe.ReplaceAllString(s, `nonce="N"`)
	s = regCSRFRe.ReplaceAllString(s, `ol-csrfToken" content="C"`)
	return s
}

// TestRegisterAnonParity — render the anonymous /register page and compare
// byte-for-byte (nonce+csrf normalised) to the Node capture.
func TestRegisterAnonParity(t *testing.T) {
	capB, err := os.ReadFile("testdata/register_anon.html")
	if err != nil {
		t.Skipf("capture missing: %v", err)
	}
	d := PageData{CSRFToken: "tokval123456", Nonce: "nonceval1234", Origin: "http://127.0.0.1:7420"}
	w := httptest.NewRecorder()
	RegisterPage(w, d)
	got := normRegister(w.Body.String())
	capN := normRegister(string(capB))
	if got == capN {
		return
	}
	i := 0
	for i < len(got) && i < len(capN) && got[i] == capN[i] {
		i++
	}
	lo, hi := i-70, i+70
	if lo < 0 {
		lo = 0
	}
	if hi > len(got) {
		hi = len(got)
	}
	if hi > len(capN) {
		hi = len(capN)
	}
	t.Fatalf("register mismatch at %d (got %dB cap %dB)\nGOT  ...%q\nCAP  ...%q",
		i, len(got), len(capN), got[lo:hi], capN[lo:hi])
}

// TestRegisterLoggedInParity — logged-in render fills the 3 user fields.
func TestRegisterLoggedInFill(t *testing.T) {
	d := PageData{CSRFToken: "c", Nonce: "n", Origin: "http://127.0.0.1:7420",
		UserEmail: "someone@e2e.test", UserID: "6aa4b8b573ef0e5094f4cbc0"}
	w := httptest.NewRecorder()
	RegisterPage(w, d)
	out := w.Body.String()
	if !strings.Contains(out, `ol-usersEmail" content="someone@e2e.test"`) {
		t.Errorf("usersEmail not filled")
	}
	if !strings.Contains(out, `ol-user_id" content="6aa4b8b573ef0e5094f4cbc0"`) {
		t.Errorf("user_id content not filled")
	}
	if !strings.Contains(out, `sessionUser&quot;:{&quot;email&quot;:&quot;someone@e2e.test&quot;}`) {
		t.Errorf("sessionUser fragment not filled")
	}
}
