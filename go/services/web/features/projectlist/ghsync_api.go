// Package projectlist — gsync (GitHub/project-provider sync) exported seam.
//
// Node: the GitHubSyncController import path does createProjectFromZip(zip)
// (services/web/app/src/Features/Project/ProjectEntityUpdateHandler
// createProjectFromZip) — same engine as POST /project/new/zip. gsync
// (go/services/web/features/ghsync) reuses it here with an in-memory entry
// list (bridge /clone already extracted the repo).
package projectlist

import (
	"ollitex/go/services/web/core"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// ImportEntry — one repo file as a project import entry (path is the
// slash-separated relative path, no leading slash).
type ImportEntry struct {
	Path string
	Data []byte
}

// ImportFromEntries — nzipCreate with an explicit project name (gsync
// passes the repo name; Node used the repo name for the created project).
// Blank = zero entries (empty repo → Node blank project path).
// Returns the project id; ok=false → the failure wire already happened
// (failImported 500 `{"success":false,"error":"Upload failed"}`).
func ImportFromEntries(a *core.App, cxt *core.Cxt, uid, name string, entries []ImportEntry) (bson.ObjectID, bool) {
	in := make([]nzipEntry, 0, len(entries))
	for _, e := range entries {
		in = append(in, nzipEntry{path: e.Path, data: e.Data})
	}
	return nzipCreate(a, cxt, uid, name, "", in, func() {})
}
