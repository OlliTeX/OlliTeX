package tokenaccess

import "testing"

func TestTokenShapes(t *testing.T) {
	if !rwPagePat.MatchString("/1234567890abcdefgh") {
		t.Fatal("rw page pattern")
	}
	if !roPagePat.MatchString("/read/abcdefghijkl") {
		t.Fatal("ro page pattern")
	}
	if rwGrantPat.MatchString("/read/abcdefghijkl/grant") {
		t.Fatal("ro grant must not match rw grant pattern")
	}
	if !roGrantPat.MatchString("/read/abcdefghijkl/grant") {
		t.Fatal("ro grant pattern")
	}
	// sharing-updates gates (disjoint: page vs join vs view)
	m := shareJoinPat.FindStringSubmatch("/project/6aa4b8c973ef0e5094f4cc02/sharing-updates/join")
	if m == nil || m[1] != "6aa4b8c973ef0e5094f4cc02" {
		t.Fatalf("shareJoinPat join: %v", m)
	}
	m = shareViewPat.FindStringSubmatch("/project/6aa4b8c973ef0e5094f4cc02/sharing-updates/view")
	if m == nil || m[1] != "6aa4b8c973ef0e5094f4cc02" {
		t.Fatalf("shareViewPat view: %v", m)
	}
	m = sharePagePat.FindStringSubmatch("/project/6aa4b8c973ef0e5094f4cc02/sharing-updates")
	if m == nil || m[1] != "6aa4b8c973ef0e5094f4cc02" {
		t.Fatalf("sharePagePat page: %v", m)
	}
	if sharePagePat.MatchString("/project/6aa4b8c973ef0e5094f4cc02/sharing-updates/join") ||
		shareJoinPat.MatchString("/project/6aa4b8c973ef0e5094f4cc02/sharing-updates") {
		t.Fatal("sharing-updates patterns must be disjoint")
	}
}

func TestDigitPrefix(t *testing.T) {
	if got := digitPrefix("1234567890abcdefgh"); got != "1234567890" {
		t.Fatalf("digitPrefix: %q", got)
	}
	if got := digitPrefix("99zzzz"); got != "99" {
		t.Fatalf("digitPrefix: %q", got)
	}
}

func TestRankOrdering(t *testing.T) {
	if rankNum("owner") <= rankNum("readAndWrite") ||
		rankNum("readAndWrite") <= rankNum("readOnly") ||
		rankNum("readOnly") <= rankNum("none") {
		t.Fatal("rank ordering")
	}
}
