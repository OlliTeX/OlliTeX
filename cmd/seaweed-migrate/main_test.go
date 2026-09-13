package main

import "testing"

func TestKeyFromFlatName(t *testing.T) {
	pid := "654abc1234def56789012345"
	did := "aaaaaaaaaaaaaaaaaaaaaaaa"
	other := "bbbbbbbbbbbbbbbbbbbbbbbb"

	cases := []struct {
		name  string
		key   string
		exact bool
	}{
		{pid + "_" + did, pid + "/" + did, true},
		{pid + "_main.tex", pid + "/main.tex", true},
		{pid + "_img_logo.png", pid + "/img/logo.png", true}, // heuristic split
	}
	for _, c := range cases {
		key, exact, err := keyFromFlatName(c.name)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if key != c.key {
			t.Errorf("%s → %q, want %q", c.name, key, c.key)
		}
		if exact != c.exact {
			t.Errorf("%s exact = %v, want %v", c.name, exact, c.exact)
		}
	}

	// round-trip: flat(key) == the flat layout
	if got := flatFromKey(other + "/" + pid); got != other+"_"+pid {
		t.Errorf("flatFromKey = %q", got)
	}
	if got := flatFromKey(pid + "/img/logo.png"); got != pid+"_img_logo.png" {
		t.Errorf("flatFromKey = %q", got)
	}
}

func TestKeyFromFlatNameGarbage(t *testing.T) {
	if _, _, err := keyFromFlatName("no-here-prefix"); err == nil {
		t.Fatal("want error for a name without a 24-hex prefix")
	}
	if _, _, err := keyFromFlatName("654abc1234def56789012345"); err == nil {
		t.Fatal("want error for a bare project id")
	}
}
