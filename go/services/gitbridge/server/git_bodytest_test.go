package server

import (
	"os"
	"testing"
)

func TestGitBodyBytesParity(t *testing.T) {
	cases := []struct {
		name string
		got  []byte
		want string
	}{
		{"upadv_miss", errBodyUploadAdv, "/tmp/jwire/cap/upadv_miss.body"},
		{"rcadv_miss", errBodyReceiveAdv, "/tmp/jwire/cap/rcadv_miss.body"},
		{"post_upcot", pktErrBody, "/tmp/jwire/cap/post_upcot.body"},
		{"post_rccot", pktErrBody, "/tmp/jwire/cap/post_rccot.body"},
	}
	for _, c := range cases {
		want, err := os.ReadFile(c.want)
		if err != nil {
			// Environment-dependent golden fixtures (captured from a live
			// Java bridge on the author's machine); skip where absent.
			t.Skipf("%s: fixture not present in this environment: %v", c.name, err)
		}
		if string(c.got) != string(want) {
			t.Errorf("%s: mismatch: got %x want %x", c.name, c.got, want)
		}
	}
	if got := productionErrorBody(403); string(got) != "{\"message\":\"HTTP error 403\"}" {
		t.Errorf("grid403: %q", got)
	}
}
