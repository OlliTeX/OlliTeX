// U9: /admin, /home, hub-variant route patterns (Node Express case + slash
// tolerances pinned against the live e2e Node 2026-09-22).
package staticpages

import "testing"

func TestHomePattern(t *testing.T) {
	if !homeRe.MatchString("/home") {
		t.Fatal("/home must match")
	}
	if !homeRe.MatchString("/Home") || !homeRe.MatchString("/HOME") {
		t.Fatal("case-insensitive must hold (Express default)")
	}
	// Express is trailing-slash tolerant: /home/ matches too (pinned in the
	// U9 gate battery). Only genuinely different paths must fail:
	for _, p := range []string{"/", "/homes", "/home/x", "/x/home"} {
		if homeRe.MatchString(p) {
			t.Fatalf("must NOT match %q", p)
		}
	}
}
