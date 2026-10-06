package dockerrunner

import (
	"os"
	"os/user"
	"strconv"
)

// ensureCompileDirOwnership best-effort chowns the host directory that is
// bind-mounted into the compile container as /compile so the container's
// (unprivileged) DockerUser can write build output (output.aux/pdf).
//
// 2026-10-07 (owner: compile produced no PDF): the sandbox compile dir on
// this host was root-owned (docker-daemon bind auto-creates as root; the
// compile service itself runs as root) while the compile container runs as
// www-data → "Cannot write file 'output.aux'" even once the sources were
// there. Node's DockerRunner chowned; the port omitted it. Best-effort:
// skipped when the uid cannot be resolved or the process lacks the
// permission (non-root); never fails the compile.
func ensureCompileDirOwnership(directory, dockerUser string) {
	if directory == "" || dockerUser == "" {
		return
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return
	}
	uid, gid := resolveUserIDs(dockerUser)
	if uid < 0 {
		return
	}
	_ = os.Chown(directory, uid, gid) // best-effort
}

// resolveUserIDs accepts a numeric uid (gid identical) or a username from
// the password database; returns (-1,-1) when unresolvable.
func resolveUserIDs(name string) (uid, gid int) {
	if n, err := strconv.Atoi(name); err == nil {
		return n, n
	}
	u, err := user.Lookup(name)
	if err != nil {
		// 2026-10-06 (live: "Cannot write file 'output.aux'"): the RUNNER
		// container (alpine base) has no www-data in /etc/passwd, so the
		// lookup fails and the ownership chown is silently skipped while the
		// COMPILE container (texlive, Debian) runs as uid 33. The texlive
		// images ship the Debian-standard www-data=33; fall back for that
		// well-known name instead of silently skipping the ownership fix.
		if name == "www-data" {
			return 33, 33
		}
		return -1, -1
	}
	uid, _ = strconv.Atoi(u.Uid)
	gid, _ = strconv.Atoi(u.Gid)
	if uid < 0 {
		uid = 0
	}
	if gid < 0 {
		gid = 0
	}
	return uid, gid
}
