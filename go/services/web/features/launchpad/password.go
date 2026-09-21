package launchpad

import (
	"math"
	"regexp"
	"strings"
)

// validatePassword mirrors AuthenticationManager.validatePassword with
// this deployment's Settings: passwordStrengthOptions UNSET (no
// OVERLEAF_PASSWORD_VALIDATION_* env — pinned on the e2e stack) →
// allowAnyChars=false, min=8, max=72. Returns the Node
// InvalidPasswordError message, or "" when valid.
//
// Node order (pinned): length min → length max → char set →
// contains-email parts → similarity.
func validatePassword(password, email string) string {
	const minLen, maxLen = 8, 72
	if len(password) < minLen {
		return "password is too short"
	}
	if len(password) > maxLen {
		return "password is too long"
	}
	if !passwordCharsValid(password) {
		return "password contains an invalid character"
	}
	if email != "" {
		startOfEmail := emailSplitAt0(email)
		if strings.Contains(password, email) ||
			strings.Contains(password, startOfEmail) ||
			strings.Contains(email, password) {
			return "password contains part of email address"
		}
		if passwordTooSimilar(password, email) {
			return "password is too similar to email address"
		}
	}
	return ""
}

// passwordCharsValid mirrors _passwordCharactersAreValid with the DEFAULT
// char sets (Settings.passwordStrengthOptions.chars unset — pinned).
func passwordCharsValid(password string) bool {
	var digits, letters, lettersUp, symbols string = "1234567890",
		"abcdefghijklmnopqrstuvwxyz",
		"ABCDEFGHIJKLMNOPQRSTUVWXYZ",
		`@#$%^&*()-_=+[]{};:<>/?!£€.,`
	for i := 0; i < len(password); i++ {
		c := password[i]
		if !stringsContainsByte(digits, c) &&
			!stringsContainsByte(letters, c) &&
			!stringsContainsByte(lettersUp, c) &&
			!strings.Contains(symbols, string(c)) {
			return false
		}
	}
	return true
}

func stringsContainsByte(s string, c byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return true
		}
	}
	return false
}

// stringsContainsRune — the £/€ members need rune semantics.

// emailSplitAt0 — JS email.split('@')[0].
func emailSplitAt0(email string) string {
	i := strings.Index(email, "@")
	if i < 0 {
		return email
	}
	return email[:i]
}

// ---- similarity (DiffHelper.stringSimilarity + AuthenticationManager) -----

const maxSimilarity = 0.7 // AuthenticationManager.MAX_SIMILARITY

// exceedsMaximumLengthRatio mirrors _exceedsMaximumLengthLengthRatio:
// skip the (expensive) similarity check when the password is ≥10× longer
// than the email part AND the part is shorter than the bound.
func exceedsMaxLenRatio(pwLen, partLen int) bool {
	bound := (maxSimilarity / 2) * float64(pwLen)
	return pwLen >= 10*partLen && float64(partLen) < bound
}

// stringSimilarity mirrors DiffHelper.stringSimilarity
// (difflib.QuickRatio-style multiset overlap ratio, 2-decimal floor).
func stringSimilarity(a, b string) float64 {
	if len(a) > 254 || len(b) > 254 {
		return 0 // Node throws; the caller catches and skips the check
	}
	fullB := map[rune]int{}
	for _, e := range b {
		fullB[e]++
	}
	avail := map[rune]int{}
	matches := 0
	for _, e := range a {
		n, ok := avail[e]
		if !ok {
			n = fullB[e]
		}
		avail[e] = n - 1
		if n > 0 {
			matches++
		}
	}
	length := len([]rune(a)) + len([]rune(b)) // JS .length ≈ UTF-16 units
	if length == 0 {
		return 1.0
	}
	ratio := (2.0 * float64(matches)) / float64(length)
	return math.Floor(ratio*100) / 100
}

var nonWordsRE = regexp.MustCompile(`\W+`)

// passwordTooSimilar mirrors _validatePasswordNotTooSimilar: every
// (non-length-exempt) part of [email, email.split(/\W+/), email.split(/@/)]
// with similarity > 0.7 → too similar.
func passwordTooSimilar(password, email string) bool {
	pw, em := toLower(password), toLower(email)
	parts := []string{em}
	parts = append(parts, nonWordsRE.Split(em, -1)...)
	parts = append(parts, strings.Split(em, "@")...)
	for _, part := range parts {
		if exceedsMaxLenRatio(len(pw), len(part)) {
			continue
		}
		if stringSimilarity(pw, part) > maxSimilarity {
			return true
		}
	}
	return false
}

// toLower — ASCII toLowerCase (JS toLowerCase on the email/password
// fixture alphabet; full Unicode equivalence is oracle-irrelevant).
func toLower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
