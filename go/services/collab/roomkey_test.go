package collab

import "testing"

func TestRoomKeyParse(t *testing.T) {
	const pid = "6abd00e37a29eb9a30056209"
	const did = "6abd00e37a29eb9a3005620b"
	cases := []struct {
		room, wantPID, wantDoc string
		valid                  bool
	}{
		{pid, pid, "", true},
		{pid + "-" + did, pid, did, true},
		{"", "", "", false},
		{"short", "", "", false},
		{did + "-" + pid, did, pid, true}, // a doc room of project <did> (still valid shape)
		{pid + "-" + did + "-x", "", "", false},
		{"ZZ-" + pid, "", "", false},
		{pid + "X", "", "", false},
	}
	for _, c := range cases {
		if got := ValidRoomName(c.room); got != c.valid {
			t.Fatalf("ValidRoomName(%q) = %v, want %v", c.room, got, c.valid)
		}
		if got := RoomProject(c.room); got != c.wantPID {
			t.Fatalf("RoomProject(%q) = %q, want %q", c.room, got, c.wantPID)
		}
		if got := RoomDoc(c.room); got != c.wantDoc {
			t.Fatalf("RoomDoc(%q) = %q, want %q", c.room, got, c.wantDoc)
		}
	}
}

func TestRoomFor(t *testing.T) {
	const pid = "6abd00e37a29eb9a30056209"
	const root = "6abd00e37a29eb9a3005620a"
	const other = "6abd00e37a29eb9a3005620b"
	if got := RoomFor(pid, "", root); got != pid {
		t.Fatalf("RoomFor(pid, empty, root) = %q, want the root room %q", got, pid)
	}
	if got := RoomFor(pid, root, root); got != pid {
		t.Fatalf("RoomFor(pid, root, root) = %q, want the root room %q", got, pid)
	}
	if got := RoomFor(pid, other, root); got != pid+"-"+other {
		t.Fatalf("RoomFor(pid, other, root) = %q, want %q", got, pid+"-"+other)
	}
	if got := RoomFor(pid, other, ""); got != pid+"-"+other {
		t.Fatalf("RoomFor(pid, other, no-root) = %q, want %q", got, pid+"-"+other)
	}
}
