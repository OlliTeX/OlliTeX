//go:build !windows

package dockerrunner

import (
	"os"
	"syscall"
	"testing"
)

// 2026-10-06 (owner: compile "Cannot write file 'output.aux'"): the compile
// sandbox dir must end up writable by the compile container's unprivileged
// user. The compile images ship the Debian-standard www-data = uid 33, and
// the RUNNER container may or may not know that name in /etc/passwd, so
// resolveUserIDs must return uid 33 for "www-data" whether the name resolves
// natively or falls back. Pin both branches here.
func TestResolveUserIDsNumericPassthrough(t *testing.T) {
	uid, gid := resolveUserIDs("33")
	if uid != 33 || gid != 33 {
		t.Fatalf("numeric passthrough: got (%d,%d), want (33,33)", uid, gid)
	}
}

func TestResolveUserIDsWwwDataIsUid33(t *testing.T) {
	uid, gid := resolveUserIDs("www-data")
	// Both the native-lookup path (33:65533 or 33:33) and the fallback path
	// (33:33) must yield owner uid 33 — that is the uid the unprivileged
	// compile container runs as, so any other value defeats the chown.
	if uid != 33 {
		t.Fatalf("www-data must resolve to uid 33 (got %d, gid %d)", uid, gid)
	}
}

// ensureCompileDirOwnership is best-effort and only meaningful when the
// process can chown (root). Pin that it leaves the target owned by the
// compile user when run as root; skip otherwise (CI/containers vary).
func TestEnsureCompileDirOwnershipChownsToCompileUser(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("chown requires root")
	}
	dir := t.TempDir()
	ensureCompileDirOwnership(dir, "www-data")
	st, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	uid := st.Sys().(*syscall.Stat_t).Uid
	if int(uid) != 33 {
		t.Fatalf("dir uid = %d, want 33 (www-data) after ensureCompileDirOwnership", uid)
	}
}
