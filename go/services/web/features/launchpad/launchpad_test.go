package launchpad

import (
	"regexp"
	"strings"
	"testing"
)

// ---- pins: Node AuthenticationManager / EmailHelper / DiffHelper ----------
// Values computed from the Node sources (validatePassword/validateEmail/
// stringSimilarity/exceedsMaximumLengthRatio) — the launchpad oracle
// battery will re-pin them through the live API.

func TestAuthMethod(t *testing.T) {
	if got := authMethod(); got != "ldap" {
		t.Fatalf("authMethod = %q, want ldap (fork SSO module pinned)", got)
	}
}

func TestParseEmail(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"E2E-Launchpad@E2E.TEST", "e2e-launchpad@e2e.test"}, // trim+lower applied
		{"  a_b.c@sub.domain.io  ", "a_b.c@sub.domain.io"},
		{"no-at-sign", ""},
		{"a@b", ""},                               // needs ≥2-letter TLD
		{"a@[1.2.3.4]", "a@[1.2.3.4]"},            // IP literal (brackets required)
		{"\"quoted\"@x.com", ""},                  // quotes excluded
		{strings.Repeat("x", 250) + "@x.com", ""}, // >254
		{"", ""},
	}
	for _, c := range cases {
		if got := parseEmail(c.in); got != c.want {
			t.Errorf("parseEmail(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidateEmail(t *testing.T) {
	if msg := validateEmail("bad"); msg != "email not valid" {
		t.Errorf("validateEmail(bad) = %q, want the Node InvalidEmailError text", msg)
	}
	if msg := validateEmail("ok@e2e.test"); msg != "" {
		t.Errorf("validateEmail(ok) = %q, want empty", msg)
	}
}

func TestValidatePasswordRules(t *testing.T) {
	email := "user@example.com"
	cases := []struct {
		pw, want string
	}{
		{"abc", "password is too short"},
		{strings.Repeat("1", 73), "password is too long"},        // 73 > max 72
		{"pipe|slash", "password contains an invalid character"}, // | not in the allowed set
		{"userpass!", "password contains part of email address"},
		{"sueelxmpae", "password is too similar to email address"},     // similar to full email, no substring
		{"userrrppassssss", "password contains part of email address"}, // contiguous 'user' substring
		{"Ol-Fixture-9x7K", ""},
		{"a1B2c3D4e5", ""},
		{"p@$$w0rd!x", ""},
	}
	for _, c := range cases {
		got := validatePassword(c.pw, email)
		if got != c.want {
			t.Errorf("validatePassword(%q, %q) = %q, want %q", c.pw, email, got, c.want)
		}
	}
}

// * and ! ARE in the allowed symbol set — fix the case table above.
func TestValidatePasswordAllowedSymbols(t *testing.T) {
	if msg := validatePassword("star*bang!x", "user@example.com"); msg != "" {
		t.Fatalf("* ! are allowed symbols; got %q", msg)
	}
	if msg := validatePassword("pipe|slash", "user@example.com"); msg != "password contains an invalid character" {
		t.Fatalf("| is disallowed; got %q", msg)
	}
}

func TestValidatePasswordOrderPinned(t *testing.T) {
	// Length beats char set beats email-part (Node short-circuit order).
	if got := validatePassword("ab!", "user@example.com"); got != "password is too short" {
		t.Errorf("short beats other rules; got %q", got)
	}
}

// ---- similarity (DiffHelper.stringSimilarity, multiset ratio) -------------

func TestStringSimilarityPins(t *testing.T) {
	cases := []struct {
		a, b string
		want float64
	}{
		{"abcd", "abcd", 1.00},
		{"abcd", "wxyz", 0.00},
		{"abcd", "abcd1", 0.88}, // 2*4/9 = 0.888 → floor2 = 0.88
		{"user@example.com", "user@example.com", 1.00},
		{"userpass", "user", 0.66}, // 2*4/12 = 0.666 → 0.66
		{"", "", 1.00},
	}
	for _, c := range cases {
		if got := stringSimilarity(c.a, c.b); got != c.want {
			t.Errorf("stringSimilarity(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestExceedsMaxLenRatio(t *testing.T) {
	// Node: ratio = maxSim/2 * pwLen; exempt iff pwLen ≥ 10*partLen && partLen < ratio.
	if !exceedsMaxLenRatio(100, 5) {
		t.Error("100 vs 5: 100 ≥ 50 && 5 < 35 → exempt (true)")
	}
	if exceedsMaxLenRatio(35, 5) {
		t.Error("35 vs 5: 35 < 50 → not exempt")
	}
	if exceedsMaxLenRatio(60, 10) {
		t.Error("60 vs 10: 60 < 100 → not exempt (ratio bound fails too)")
	}
}

// ---- password-too-similar end-to-end (pinned shapes) ------------------------

func TestPasswordTooSimilar(t *testing.T) {
	email := "e2e-launchpad@e2e.test"
	cases := []struct {
		pw   string
		want bool
	}{
		{"e2e-launchpad", true},                            // near-identical
		{"e2elaunchpadx", true},                            // similar to the local part
		{"Xy9$mQz!wK2", false},                             // unrelated
		{"e2e-launchpad" + strings.Repeat("z", 95), false}, // ≥10× length exemption
	}
	for _, c := range cases {
		if got := passwordTooSimilar(c.pw, email); got != c.want {
			t.Errorf("passwordTooSimilar(%q, %q) = %v, want %v", c.pw, email, got, c.want)
		}
	}
}

// ---- misc shapes ------------------------------------------------------------

func TestReversedHostname(t *testing.T) {
	if got := reversedHostname("a@e2e.test"); got != "tset.e2e" {
		t.Errorf("reversedHostname = %q, want tset.e2e", got)
	}
	if got := reversedHostname("plain"); got != "nialp" {
		t.Errorf("no-@ case = %q, want nialp (JS splits to ['plain'])", got)
	}
}

func TestRandomUUIDShape(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	for i := 0; i < 20; i++ {
		if !re.MatchString(randomUUID()) {
			t.Fatalf("randomUUID shape: %q", randomUUID())
		}
	}
}

func TestHoldingAccountFalse(t *testing.T) {
	if holdingAccountFalse(nil) {
		t.Error("nil → false")
	}
}
