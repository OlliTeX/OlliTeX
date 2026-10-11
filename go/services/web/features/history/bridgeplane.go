// bridgeplane.go — the git-bridge plane (web /api/v0/docs surface) served
// from the Yjs version log.
//
// Root cause this serves (board TODO-ff7d6b1a): the D19 hard cut replaced
// the OT/yops content plane with per-room Yjs update logs (ydoc/head/upds),
// so the legacy V2 flush pipeline (redis ops queue -> project-history
// ProcessUpdatesForProject) has NO producer in this fork — project-history
// /version stays at 0 for every project, and the bridge (which reads
// /api/v0/docs/:pid -> project-history V2) can never materialise a repo.
//
// This plane reuses the exact machinery the (live-verified) history panel
// uses for its unified version timeline (024 Option B):
//
//	projectDocRooms            the project's rooms (root + per-doc)
//	buildUnifiedFeedMulti      the unified 1..N timeline (rooms + tree ops)
//	textAt                     a room's Y.Text content after version v
//
// Semantics pinned (git plane):
//
//	BridgeLatest      -> the latest unified project version (N >= 1) + the
//	                    authoring timestamp + the most recent author uid
//	                    ("" when unknown), or ok=false when the project has
//	                    no Yjs versions at all.
//	BridgeSnapshotAt  -> the project's file content at unified version N:
//	                    CURRENT tree paths (v1 — a HEAD clone is exact; older
//	                    N gets the current paths, per-room content at-or-
//	                    before N), per-room text via textAt.
package history

import (
	"context"
	"strings"
	"time"

	"ollitex/go/services/collab"

	"github.com/reearth/ygo/persistence"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// bridgeSetup — the shared setup (store, vlog, seek, rooms, unified feed).
type bridgeSetup struct {
	st       persistence.VersionedPersistence
	vlog     collab.Log
	rooms    []projectRoom
	rootPath string
	feed     mergedFeed
}

// setupBridge — store + vlog + seek + rooms + unified feed.
func setupBridge(ctx context.Context, db *mongo.Database, pid string) (*bridgeSetup, bool) {
	b := &bridgeSetup{}
	st, err := collab.NewMongoStore(ctx, db)
	if err != nil {
		return nil, false
	}
	b.st = st
	var vlog collab.Log
	if vl, verr := collab.NewMongoVersionLog(ctx, db); verr == nil {
		b.vlog = vl
	}
	seek := MongoDocSeeker(db)
	rooms, rerr := projectDocRooms(ctx, st, vlog, seek, pid)
	if rerr != nil {
		return nil, false
	}
	b.rooms = rooms
	b.rootPath = rootDocPathname(ctx, seek, pid)
	var yops []YopMeta
	if ylog, yerr := NewMongoYopLog(ctx, db); yerr == nil {
		if ops, oerr := ylog.List(ctx, pid); oerr == nil {
			yops = ops
		}
	}
	b.feed = buildUnifiedFeedMulti(rooms, b.rootPath, yops)
	return b, true
}

// BridgeLatest — the latest unified project version (>= 1), its
// timestamp and the most recent author uid ("" when unknown); ok=false
// when the project has no Yjs versions at all.
func BridgeLatest(ctx context.Context, db *mongo.Database, pid string) (int, time.Time, string, bool) {
	b, ok := setupBridge(ctx, db, pid)
	if !ok || len(b.feed) == 0 {
		return 0, time.Time{}, "", false
	}
	last := b.feed[len(b.feed)-1]
	author := ""
	if uids, has := metaUsers(last.Meta); has && len(uids) > 0 {
		if s, ok2 := uids[0].(string); ok2 {
			author = s
		}
	}
	return len(b.feed), time.UnixMilli(last.Ts).UTC(), author, true
}

// BridgeSnapshotAt — the project's file content at unified version N
// (clamped to the feed size): CURRENT tree paths (v1 — a HEAD clone is
// exact; older N gets the current paths) with per-room text at-or-before
// N. ok=false when the project has no Yjs versions or N < 1.
func BridgeSnapshotAt(ctx context.Context, db *mongo.Database, pid string, n int) (map[string]string, bool) {
	b, ok := setupBridge(ctx, db, pid)
	if !ok || len(b.feed) == 0 || n < 1 {
		return nil, false
	}
	if n > len(b.feed) {
		n = len(b.feed)
	}
	// Per-room largest room-version whose unified index is <= N.
	roomV := map[string]int{}
	for _, row := range b.feed {
		if row.UnifiedV > n {
			break
		}
		if row.Room != "" && row.V > roomV[row.Room] {
			roomV[row.Room] = row.V
		}
	}
	out := map[string]string{}
	for _, r := range b.rooms {
		path := r.Pathname
		if r.IsRoot {
			path = b.rootPath
		}
		path = strings.TrimPrefix(path, "/")
		if path == "" {
			continue
		}
		content, err := textAt(ctx, b.st, r.Room, roomV[r.Room])
		if err != nil {
			content = ""
		}
		out[path] = content
	}
	return out, true
}

// metaUsers — the vendor meta "users" array (uid strings).
func metaUsers(meta map[string]any) ([]any, bool) {
	v, ok := meta["users"]
	if !ok {
		return nil, false
	}
	arr, ok2 := v.([]any)
	return arr, ok2
}
