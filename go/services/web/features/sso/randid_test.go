package sso

import (
	"regexp"
	"testing"
)

// TestRandIDNoPanic — regression for the live 500 (2026-10-09): the old
// seed-walk overflowed int64 and Go's signed % produced negative indices
// → "index out of range [-5]" panic inside adminAddProvider.
func TestRandIDNoPanic(t *testing.T) {
	re := regexp.MustCompile(`^[A-Za-z0-9]{15}$`)
	seen := map[string]bool{}
	for i := 0; i < 2000; i++ {
		id := randID()
		if !re.MatchString(id) {
			t.Fatalf("randID malformed: %q", id)
		}
		if seen[id] {
			t.Fatalf("randID collision at iteration %d: %q", i, id)
		}
		seen[id] = true
	}
}
