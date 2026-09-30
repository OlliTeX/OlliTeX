package registrationpage

import "testing"

// TestBcryptRoundsFloor — Part B B8: env floor >= 10.
func TestBcryptRoundsFloor(t *testing.T) {
	cases := map[string]int{"4": 12, "9": 12, "10": 10, "14": 14, "": 12}
	for env, want := range cases {
		t.Setenv("BCRYPT_ROUNDS", env)
		if got := bcryptRounds(); got != want {
			t.Fatalf("BCRYPT_ROUNDS=%q: got %d, want %d", env, got, want)
		}
	}
}
