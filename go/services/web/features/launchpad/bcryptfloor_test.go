package launchpad

import "testing"

// TestBcryptRoundsFloor — Part B B8: the BCryptRounds env floor is >= 10
// (cost 4 is well below bcrypt's documented minimum viability; an operator
// setting it that low silently weakens every new hash).
func TestBcryptRoundsFloor(t *testing.T) {
	cases := map[string]int{
		"4":  12, // below floor -> default
		"9":  12, // below floor -> default
		"10": 10, // exactly at floor -> honored
		"14": 14, // above floor -> honored
		"":   12, // unset -> default
	}
	for env, want := range cases {
		t.Setenv("BCRYPT_ROUNDS", env)
		if got := bcryptRounds(); got != want {
			t.Fatalf("BCRYPT_ROUNDS=%q: got %d, want %d", env, got, want)
		}
	}
}
